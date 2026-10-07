package accounting

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/ledgerlink/branchledger/backend/internal/inventory"
	"github.com/ledgerlink/branchledger/backend/internal/tenancy"
)

// ErrTransactionNotPosted covers "no such transaction" and "not currently
// posted" alike — a caller cannot distinguish a foreign company's
// transaction from one that never existed (AT04).
var ErrTransactionNotPosted = errors.New("accounting: transaction is not a posted transaction in this company")

// ReverseInput is what POST /transactions/{id}/reverse gathers from a
// validated request. BranchID is supplied by the client (the screen showing
// the transaction already knows it) rather than looked up first, so a
// rejected reversal — including "no such transaction" — still has a branch
// to record its idempotency/rejection row against; the transaction's actual
// branch is independently re-checked once loaded.
type ReverseInput struct {
	OperationID       uuid.UUID
	TransactionID     uuid.UUID
	BranchID          uuid.UUID
	Reason            string
	ClientSubmittedAt time.Time
}

// Reverse implements FR10 / "Corrections and period locks": a posted
// transaction is never edited or deleted. Instead a new, linked reversal
// transaction posts the mirror-image journal (Reversal in journal.go) and
// restores any stock quantity the original movement touched. The original
// row only ever gains a reversed_by_transaction_id and status "reversed" —
// nothing about it is overwritten. Reversal may happen only once per
// transaction: a second attempt finds the original already "reversed", not
// "posted", and is rejected the same way a foreign transaction ID is.
func (s *Service) Reverse(ctx context.Context, scope tenancy.Scope, in ReverseInput) (PostResult, error) {
	if err := scope.RequireBranch(in.BranchID); err != nil {
		return PostResult{}, ErrOutOfScope
	}
	if !tenancy.Can(tenancy.ActionReversePosting, scope.Role, scope.Delegations, true) {
		return PostResult{}, ErrOutOfScope
	}

	payloadHash := hashAny(struct {
		TransactionID uuid.UUID
		Reason        string
	}{in.TransactionID, in.Reason})

	var result PostResult
	err := s.repo.WithTx(ctx, func(ctx context.Context, tx Tx) error {
		existing, found, err := tx.FindOperation(ctx, scope.CompanyID, in.OperationID)
		if err != nil {
			return fmt.Errorf("find operation: %w", err)
		}
		if found {
			if existing.PayloadHash != payloadHash {
				return ErrConflict
			}
			result = PostResult{
				Accepted: existing.Status == "accepted", TransactionID: existing.ResultRecordID,
				Version: existing.ResultVersion, ErrorCode: existing.ErrorCode, AlreadyProcessed: true,
			}
			return nil
		}

		original, originalJournal, ok, err := tx.GetPostedTransaction(ctx, scope.CompanyID, in.TransactionID)
		if err != nil {
			return fmt.Errorf("get posted transaction: %w", err)
		}
		if !ok || original.BranchID != in.BranchID {
			return rejectionError{code: "transaction_not_posted", cause: ErrTransactionNotPosted}
		}

		// A reversal posts today, against whatever period is currently open
		// — the original's own period may since have been locked, which is
		// exactly why a correction must be a new entry rather than an edit.
		reversalDate := time.Now().UTC()
		period, ok, err := tx.GetOpenPeriod(ctx, scope.CompanyID, reversalDate)
		if err != nil {
			return fmt.Errorf("get open period: %w", err)
		}
		if !ok {
			return rejectionError{code: "period_closed", cause: ErrPeriodClosed}
		}

		reversalTxID := uuid.New()
		reversalJournal, err := Reversal(reversalTxID, originalJournal)
		if err != nil {
			return fmt.Errorf("build reversal journal: %w", err)
		}

		if err := tx.SaveTransaction(ctx, TransactionRecord{
			ID: reversalTxID, CompanyID: scope.CompanyID, BranchID: original.BranchID,
			OperationID: in.OperationID, DocumentType: original.DocumentType, DocumentDate: reversalDate,
			CurrencyCode: original.CurrencyCode, ExchangeRate: original.ExchangeRate,
			CounterpartyID: original.CounterpartyID, PaymentAccountID: original.PaymentAccountID,
			SourceReference: original.SourceReference, Explanation: in.Reason,
			Status: "posted", Revision: 1, CreatedBy: scope.UserID,
			ReversesTransactionID: &original.ID,
		}); err != nil {
			return fmt.Errorf("save reversal transaction: %w", err)
		}

		journalID, err := tx.SaveJournal(ctx, period.ID, reversalJournal)
		if err != nil {
			return fmt.Errorf("save reversal journal: %w", err)
		}

		// Restore (or remove) exactly the stock quantity and value the
		// original posting moved, at the same unit cost, so the
		// weighted-average ledger ends up exactly where it would have been
		// had the original never posted.
		movements, err := tx.GetStockMovementsByTransaction(ctx, scope.CompanyID, original.ID)
		if err != nil {
			return fmt.Errorf("get original stock movements: %w", err)
		}
		for _, m := range movements {
			position, err := tx.GetStockPosition(ctx, scope.CompanyID, m.BranchID, m.ProductID)
			if err != nil {
				return fmt.Errorf("get stock position: %w", err)
			}
			inverseDelta := m.QuantityDelta.Neg()
			updated := inventory.Position{
				Quantity: position.Quantity.Add(inverseDelta),
				Value:    position.Value.Add(inverseDelta.Mul(m.UnitCost).Round(2)),
			}
			if err := tx.SaveStockMovement(ctx, StockMovementRecord{
				ID: uuid.New(), CompanyID: scope.CompanyID, BranchID: m.BranchID, ProductID: m.ProductID,
				SourceTransactionID: reversalTxID, MovementType: "reversal", QuantityDelta: inverseDelta,
				UnitCost: m.UnitCost, RunningQuantity: updated.Quantity, RunningValue: updated.Value,
				Reason: "reversal of " + original.ID.String(), CreatedBy: scope.UserID,
			}); err != nil {
				return fmt.Errorf("save reversal stock movement: %w", err)
			}
		}

		if err := tx.MarkTransactionReversed(ctx, original.ID, reversalTxID); err != nil {
			return fmt.Errorf("mark original reversed: %w", err)
		}

		if err := tx.RecordAuditEvent(ctx, scope.CompanyID, scope.UserID, "transaction_reversed",
			"transaction", reversalTxID, map[string]any{"reverses": original.ID, "reason": in.Reason, "journal_id": journalID}); err != nil {
			return fmt.Errorf("record audit event: %w", err)
		}
		if err := tx.RecordSyncChange(ctx, scope.CompanyID, original.BranchID, "transaction", original.ID, 1, "reversed"); err != nil {
			return fmt.Errorf("record sync change (original): %w", err)
		}
		if err := tx.RecordSyncChange(ctx, scope.CompanyID, original.BranchID, "transaction", reversalTxID, 1, "created"); err != nil {
			return fmt.Errorf("record sync change (reversal): %w", err)
		}
		if err := tx.EnqueueOutboxJob(ctx, scope.CompanyID, "summary_refresh", reversalTxID.String(),
			map[string]any{"branch_id": original.BranchID, "transaction_id": reversalTxID}); err != nil {
			return fmt.Errorf("enqueue outbox job: %w", err)
		}

		result = PostResult{Accepted: true, TransactionID: reversalTxID, Version: 1, Status: "posted"}
		return tx.RecordOperation(ctx, OperationRecord{
			CompanyID: scope.CompanyID, OperationID: in.OperationID, BranchID: original.BranchID,
			ActorUserID: scope.UserID, CommandType: "reverse", PayloadHash: payloadHash, Status: "accepted",
			ResultRecordType: "transaction", ResultRecordID: reversalTxID, ResultVersion: 1,
			ClientSubmittedAt: in.ClientSubmittedAt,
		})
	})

	return s.finishPost(ctx, scope, in.OperationID, in.BranchID, "reverse", payloadHash, in.ClientSubmittedAt, result, err)
}
