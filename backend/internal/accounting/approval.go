package accounting

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/ledgerlink/branchledger/backend/internal/tenancy"
)

var (
	ErrApprovalNotFound  = errors.New("accounting: no pending approval request in this company")
	ErrSelfApproval      = errors.New("accounting: a user cannot approve their own request")
	ErrInvalidDecision   = errors.New("accounting: decision must be \"approved\" or \"rejected\"")
)

// ApprovalDecisionInput is what POST /approvals/{id}/decision gathers.
// BranchID is supplied by the client (the approvals screen already shows the
// requesting branch) the same way ReverseInput carries one — see that type's
// doc comment for why. ExpectedVersion is the transaction revision the
// approver saw when opening the request; a mismatch means someone else acted
// on it first (AT05: stale version -> conflict, refreshed request).
type ApprovalDecisionInput struct {
	OperationID       uuid.UUID
	ApprovalID        uuid.UUID
	BranchID          uuid.UUID
	Decision          string // "approved" | "rejected"
	Reason            string
	ExpectedVersion   int
	ClientSubmittedAt time.Time
}

// ApprovalDecisionResult is what POST /approvals/{id}/decision returns.
type ApprovalDecisionResult struct {
	Accepted         bool
	TransactionID    uuid.UUID
	Status           string // "posted" | "rejected"
	Version          int
	ErrorCode        string
	AlreadyProcessed bool
}

// DecideApproval implements the approval half of section 5.4/5.6: approving
// a request posts the exact expense journal that would have posted at
// submission time, had it been within the actor's limit; rejecting leaves
// the source transaction at "rejected" with its reason retained and no
// journal ever written. Only expenses are ever routed to Awaiting approval
// by this implementation (see Post's "3b" step in service.go), so this
// re-derivation only needs to handle that one posting rule.
func (s *Service) DecideApproval(ctx context.Context, scope tenancy.Scope, in ApprovalDecisionInput) (ApprovalDecisionResult, error) {
	if err := scope.RequireBranch(in.BranchID); err != nil {
		return ApprovalDecisionResult{}, ErrOutOfScope
	}
	if !tenancy.Can(tenancy.ActionApproveSpending, scope.Role, scope.Delegations, true) {
		return ApprovalDecisionResult{}, ErrOutOfScope
	}
	if in.Decision != "approved" && in.Decision != "rejected" {
		return ApprovalDecisionResult{}, ErrInvalidDecision
	}

	payloadHash := hashAny(in)
	var result ApprovalDecisionResult

	err := s.repo.WithTx(ctx, func(ctx context.Context, tx Tx) error {
		existing, found, err := tx.FindOperation(ctx, scope.CompanyID, in.OperationID)
		if err != nil {
			return fmt.Errorf("find operation: %w", err)
		}
		if found {
			if existing.PayloadHash != payloadHash {
				return ErrConflict
			}
			result = ApprovalDecisionResult{
				Accepted: existing.Status == "accepted", TransactionID: existing.ResultRecordID,
				Version: existing.ResultVersion, ErrorCode: existing.ErrorCode, AlreadyProcessed: true,
			}
			return nil
		}

		approval, txRecord, ok, err := tx.GetApprovalRequest(ctx, scope.CompanyID, in.ApprovalID)
		if err != nil {
			return fmt.Errorf("get approval request: %w", err)
		}
		if !ok || txRecord.BranchID != in.BranchID {
			return rejectionError{code: "approval_not_found", cause: ErrApprovalNotFound}
		}
		// "A user cannot approve their own request" — unconditional, no role
		// is exempt (permission matrix section 4.6, "Approve own request":
		// No, every column).
		if approval.RequestedBy == scope.UserID {
			return rejectionError{code: "self_approval_denied", cause: ErrSelfApproval}
		}
		if in.ExpectedVersion != txRecord.Revision {
			return ErrConflict
		}

		if in.Decision == "rejected" {
			if err := tx.MarkTransactionRejected(ctx, txRecord.ID); err != nil {
				return fmt.Errorf("mark transaction rejected: %w", err)
			}
			if err := tx.RecordApprovalDecision(ctx, approval.ID, scope.UserID, "rejected", in.Reason); err != nil {
				return fmt.Errorf("record approval decision: %w", err)
			}
			if err := tx.RecordAuditEvent(ctx, scope.CompanyID, scope.UserID, "approval_decided", "transaction", txRecord.ID,
				map[string]any{"decision": "rejected", "reason": in.Reason}); err != nil {
				return fmt.Errorf("record audit event: %w", err)
			}
			if err := tx.RecordSyncChange(ctx, scope.CompanyID, txRecord.BranchID, "transaction", txRecord.ID, txRecord.Revision, "updated"); err != nil {
				return fmt.Errorf("record sync change: %w", err)
			}
			result = ApprovalDecisionResult{Accepted: true, TransactionID: txRecord.ID, Status: "rejected", Version: txRecord.Revision}
			return tx.RecordOperation(ctx, OperationRecord{
				CompanyID: scope.CompanyID, OperationID: in.OperationID, BranchID: in.BranchID,
				ActorUserID: scope.UserID, CommandType: "approval_decision", PayloadHash: payloadHash, Status: "accepted",
				ResultRecordType: "transaction", ResultRecordID: txRecord.ID, ResultVersion: txRecord.Revision,
				ClientSubmittedAt: in.ClientSubmittedAt,
			})
		}

		// Approved: re-derive and post the same expense journal the
		// original submission would have posted, against the period
		// covering the expense's own document date (not today's), the same
		// way every other posting in this system is dated.
		period, ok, err := tx.GetOpenPeriod(ctx, scope.CompanyID, txRecord.DocumentDate)
		if err != nil {
			return fmt.Errorf("get open period: %w", err)
		}
		if !ok {
			return rejectionError{code: "period_closed", cause: ErrPeriodClosed}
		}
		accts, err := tx.GetChartAccounts(ctx, scope.CompanyID)
		if err != nil {
			return fmt.Errorf("get chart accounts: %w", err)
		}
		netAmount := decimal.Zero
		for _, l := range txRecord.Lines {
			netAmount = netAmount.Add(l.LineNet)
		}
		paymentAccount := accts.Cash
		if txRecord.PaymentAccountID != nil {
			paymentAccount = *txRecord.PaymentAccountID
		}
		journal, err := BuildJournal(txRecord.ID, txRecord.CurrencyCode, accts.ReportingCurrency(), txRecord.ExchangeRate, []Leg{
			{AccountID: accts.OperatingExpense, Side: Debit, Amount: netAmount},
			{AccountID: paymentAccount, Side: Credit, Amount: netAmount},
		})
		if err != nil {
			return fmt.Errorf("build approved expense journal: %w", err)
		}

		journalID, err := tx.SaveJournal(ctx, period.ID, journal)
		if err != nil {
			return fmt.Errorf("save journal: %w", err)
		}
		if err := tx.MarkTransactionPosted(ctx, txRecord.ID); err != nil {
			return fmt.Errorf("mark transaction posted: %w", err)
		}
		if err := tx.RecordApprovalDecision(ctx, approval.ID, scope.UserID, "approved", in.Reason); err != nil {
			return fmt.Errorf("record approval decision: %w", err)
		}
		if err := tx.RecordAuditEvent(ctx, scope.CompanyID, scope.UserID, "approval_decided", "transaction", txRecord.ID,
			map[string]any{"decision": "approved", "journal_id": journalID}); err != nil {
			return fmt.Errorf("record audit event: %w", err)
		}
		if err := tx.RecordSyncChange(ctx, scope.CompanyID, txRecord.BranchID, "transaction", txRecord.ID, txRecord.Revision, "updated"); err != nil {
			return fmt.Errorf("record sync change: %w", err)
		}
		if err := tx.EnqueueOutboxJob(ctx, scope.CompanyID, "summary_refresh", "approval-"+txRecord.ID.String(),
			map[string]any{"branch_id": txRecord.BranchID, "transaction_id": txRecord.ID}); err != nil {
			return fmt.Errorf("enqueue outbox job: %w", err)
		}

		result = ApprovalDecisionResult{Accepted: true, TransactionID: txRecord.ID, Status: "posted", Version: txRecord.Revision}
		return tx.RecordOperation(ctx, OperationRecord{
			CompanyID: scope.CompanyID, OperationID: in.OperationID, BranchID: in.BranchID,
			ActorUserID: scope.UserID, CommandType: "approval_decision", PayloadHash: payloadHash, Status: "accepted",
			ResultRecordType: "transaction", ResultRecordID: txRecord.ID, ResultVersion: txRecord.Revision,
			ClientSubmittedAt: in.ClientSubmittedAt,
		})
	})

	if err != nil {
		if errors.Is(err, ErrConflict) {
			return ApprovalDecisionResult{}, ErrConflict
		}
		if rejErr, ok := errors.AsType[rejectionError](err); ok {
			if recErr := s.repo.RecordRejection(ctx, OperationRecord{
				CompanyID: scope.CompanyID, OperationID: in.OperationID, BranchID: in.BranchID,
				ActorUserID: scope.UserID, CommandType: "approval_decision", PayloadHash: payloadHash,
				Status: "rejected", ErrorCode: rejErr.code, ClientSubmittedAt: in.ClientSubmittedAt,
			}); recErr != nil {
				return ApprovalDecisionResult{}, fmt.Errorf("record rejected operation: %w", recErr)
			}
			return ApprovalDecisionResult{Accepted: false, ErrorCode: rejErr.code}, nil
		}
		return ApprovalDecisionResult{}, err
	}
	return result, nil
}
