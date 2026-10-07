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

// CloseInput is what POST /closes gathers from a validated request. Per
// section 5.6, ExpectedCash is itself derived from posted cash movements —
// the server, not the client, is authoritative for it — so the client sends
// only what a physical count actually produces: the opening float it was
// told and the counted cash. Clients may still display their own expected
// figure for the cashier to compare against before submitting; it is never
// trusted.
type CloseInput struct {
	OperationID       uuid.UUID
	BranchID          uuid.UUID
	CloseDate         time.Time
	CurrencyCode      string
	OpeningFloat      decimal.Decimal
	ExpectedCash      decimal.Decimal
	CountedCash       decimal.Decimal
	Explanation       string
	ClientSubmittedAt time.Time
}

// CloseResult is what POST /closes returns.
type CloseResult struct {
	Accepted         bool
	CloseID          uuid.UUID
	Discrepancy      decimal.Decimal
	ErrorCode        string
	AlreadyProcessed bool
}

// SubmitClose implements FR05 "Daily close and approvals": the cashier's
// count becomes a close record at status "submitted" — immediately visible
// and traceable, same as the architecture requires ("Shortage remains
// visible until approved resolution") — never silently adjusted or merged
// into the next day's figures. A branch/day/currency that already has a
// submitted-or-approved close gets the next revision instead of overwriting
// the prior one (the unique index on (branch_id, close_date, currency_code,
// revision) enforces this is never ambiguous).
func (s *Service) SubmitClose(ctx context.Context, scope tenancy.Scope, in CloseInput) (CloseResult, error) {
	if err := scope.RequireBranch(in.BranchID); err != nil {
		return CloseResult{}, ErrOutOfScope
	}
	if !tenancy.Can(tenancy.ActionCreateDailyRecord, scope.Role, scope.Delegations, true) {
		return CloseResult{}, ErrOutOfScope
	}

	payloadHash := hashAny(in)
	var result CloseResult

	err := s.repo.WithTx(ctx, func(ctx context.Context, tx Tx) error {
		existing, found, err := tx.FindOperation(ctx, scope.CompanyID, in.OperationID)
		if err != nil {
			return fmt.Errorf("find operation: %w", err)
		}
		if found {
			if existing.PayloadHash != payloadHash {
				return ErrConflict
			}
			result = CloseResult{
				Accepted: existing.Status == "accepted", CloseID: existing.ResultRecordID,
				Discrepancy: in.CountedCash.Sub(in.ExpectedCash), ErrorCode: existing.ErrorCode,
				AlreadyProcessed: true,
			}
			return nil
		}

		latest, hasLatest, err := tx.GetLatestClose(ctx, scope.CompanyID, in.BranchID, in.CloseDate, in.CurrencyCode)
		if err != nil {
			return fmt.Errorf("get latest close: %w", err)
		}
		revision := 1
		if hasLatest {
			// "One active close revision per branch day currency": an
			// already-submitted or approved revision blocks a fresh
			// submission outright rather than silently superseding it. Only
			// a "returned" revision may be corrected and resubmitted
			// (section 5.6 state table).
			if latest.Status != "returned" {
				return rejectionError{code: "close_already_submitted", cause: fmt.Errorf("accounting: branch %s already has a %s close for %s %s",
					in.BranchID, latest.Status, in.CloseDate.Format("2006-01-02"), in.CurrencyCode)}
			}
			revision = latest.Revision + 1
		}

		discrepancy := in.CountedCash.Sub(in.ExpectedCash)
		closeID, err := tx.SaveDailyClose(ctx, DailyCloseRecord{
			CompanyID: scope.CompanyID, BranchID: in.BranchID, CloseDate: in.CloseDate,
			CurrencyCode: in.CurrencyCode, OpeningFloat: in.OpeningFloat, ExpectedCash: in.ExpectedCash,
			CountedCash: in.CountedCash, Discrepancy: discrepancy, Explanation: in.Explanation,
			Status: "submitted", Revision: revision, SubmittedBy: scope.UserID,
		})
		if err != nil {
			return fmt.Errorf("save daily close: %w", err)
		}

		if err := tx.RecordAuditEvent(ctx, scope.CompanyID, scope.UserID, "close_submitted", "daily_close", closeID,
			map[string]any{"branch_id": in.BranchID, "close_date": in.CloseDate, "discrepancy": discrepancy.String(), "revision": revision}); err != nil {
			return fmt.Errorf("record audit event: %w", err)
		}
		if err := tx.RecordSyncChange(ctx, scope.CompanyID, in.BranchID, "daily_close", closeID, revision, "created"); err != nil {
			return fmt.Errorf("record sync change: %w", err)
		}

		result = CloseResult{Accepted: true, CloseID: closeID, Discrepancy: discrepancy}
		return tx.RecordOperation(ctx, OperationRecord{
			CompanyID: scope.CompanyID, OperationID: in.OperationID, BranchID: in.BranchID,
			ActorUserID: scope.UserID, CommandType: "close", PayloadHash: payloadHash, Status: "accepted",
			ResultRecordType: "daily_close", ResultRecordID: closeID, ResultVersion: revision,
			ClientSubmittedAt: in.ClientSubmittedAt,
		})
	})

	if err != nil {
		if errors.Is(err, ErrConflict) {
			return CloseResult{}, ErrConflict
		}
		if rejErr, ok := errors.AsType[rejectionError](err); ok {
			if recErr := s.repo.RecordRejection(ctx, OperationRecord{
				CompanyID: scope.CompanyID, OperationID: in.OperationID, BranchID: in.BranchID,
				ActorUserID: scope.UserID, CommandType: "close", PayloadHash: payloadHash,
				Status: "rejected", ErrorCode: rejErr.code, ClientSubmittedAt: in.ClientSubmittedAt,
			}); recErr != nil {
				return CloseResult{}, fmt.Errorf("record rejected operation: %w", recErr)
			}
			return CloseResult{Accepted: false, ErrorCode: rejErr.code}, nil
		}
		return CloseResult{}, err
	}
	return result, nil
}
