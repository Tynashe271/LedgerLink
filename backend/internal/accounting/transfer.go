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
	ErrTransferNotFound       = errors.New("accounting: no dispatched transfer for this company and receiving branch")
	ErrTransferAlreadyReceived = errors.New("accounting: transfer has already been fully received")
	ErrReceiptExceedsRemaining = errors.New("accounting: received amount exceeds the transfer's remaining in-transit balance")
	ErrInvalidReceivedAmount   = errors.New("accounting: received amount must be positive")
)

// TransferReceiveInput is what POST /transfers/{id}/receive gathers.
// BranchID is the receiving branch, supplied by the client for the same
// reason ReverseInput carries one (see its doc comment). ReceivedAmount may
// be less than the transfer's remaining balance — a partial receipt leaves
// the residual visible as still in-transit (architecture section 5.7 /
// AT08), never silently completing the transfer early.
type TransferReceiveInput struct {
	OperationID       uuid.UUID
	TransferID        uuid.UUID
	BranchID          uuid.UUID
	ReceivedAmount    decimal.Decimal
	ClientSubmittedAt time.Time
}

// TransferReceiveResult is what POST /transfers/{id}/receive returns.
type TransferReceiveResult struct {
	Accepted         bool
	TransactionID    uuid.UUID
	TransferStatus   string // "partially_received" | "received"
	Version          int
	ErrorCode        string
	AlreadyProcessed bool
}

// ReceiveTransfer implements the receiving half of FR07: it posts "Transfer
// receipt" (Dr Receiver cash / Cr Funds in transit) for exactly the amount
// confirmed now, and updates the transfer's cumulative received amount and
// status. Consolidated clearing reaches zero only once the sum of all
// receipts equals the dispatched amount (architecture section 5.5's
// transfer worked example).
func (s *Service) ReceiveTransfer(ctx context.Context, scope tenancy.Scope, in TransferReceiveInput) (TransferReceiveResult, error) {
	if err := scope.RequireBranch(in.BranchID); err != nil {
		return TransferReceiveResult{}, ErrOutOfScope
	}

	payloadHash := hashAny(in)
	var result TransferReceiveResult

	err := s.repo.WithTx(ctx, func(ctx context.Context, tx Tx) error {
		existing, found, err := tx.FindOperation(ctx, scope.CompanyID, in.OperationID)
		if err != nil {
			return fmt.Errorf("find operation: %w", err)
		}
		if found {
			if existing.PayloadHash != payloadHash {
				return ErrConflict
			}
			result = TransferReceiveResult{
				Accepted: existing.Status == "accepted", TransactionID: existing.ResultRecordID,
				Version: existing.ResultVersion, ErrorCode: existing.ErrorCode, AlreadyProcessed: true,
			}
			return nil
		}

		transfer, ok, err := tx.GetTransferForReceipt(ctx, scope.CompanyID, in.TransferID)
		if err != nil {
			return fmt.Errorf("get transfer: %w", err)
		}
		if !ok || transfer.ReceiverBranchID != in.BranchID {
			return rejectionError{code: "transfer_not_found", cause: ErrTransferNotFound}
		}
		if transfer.Status == "received" {
			return rejectionError{code: "transfer_already_received", cause: ErrTransferAlreadyReceived}
		}
		if in.ReceivedAmount.LessThanOrEqual(decimal.Zero) {
			return rejectionError{code: "invalid_received_amount", cause: ErrInvalidReceivedAmount}
		}
		remaining := transfer.Amount.Sub(transfer.ReceivedAmount)
		if in.ReceivedAmount.GreaterThan(remaining) {
			return rejectionError{code: "receipt_exceeds_remaining", cause: ErrReceiptExceedsRemaining}
		}

		receiptDate := time.Now().UTC()
		period, ok, err := tx.GetOpenPeriod(ctx, scope.CompanyID, receiptDate)
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

		receiptTxID := uuid.New()
		journal, err := TransferReceipt(receiptTxID, accts, in.ReceivedAmount, transfer.CurrencyCode, accts.ReportingCurrency(), transfer.ExchangeRate)
		if err != nil {
			return fmt.Errorf("build transfer receipt journal: %w", err)
		}

		if err := tx.SaveTransaction(ctx, TransactionRecord{
			ID: receiptTxID, CompanyID: scope.CompanyID, BranchID: in.BranchID,
			OperationID: in.OperationID, DocumentType: "transfer", DocumentDate: receiptDate,
			CurrencyCode: transfer.CurrencyCode, ExchangeRate: transfer.ExchangeRate,
			Explanation: "Transfer receipt", Status: "posted", Revision: 1, CreatedBy: scope.UserID,
		}); err != nil {
			return fmt.Errorf("save receipt transaction: %w", err)
		}
		journalID, err := tx.SaveJournal(ctx, period.ID, journal)
		if err != nil {
			return fmt.Errorf("save receipt journal: %w", err)
		}

		newReceivedTotal := transfer.ReceivedAmount.Add(in.ReceivedAmount)
		newStatus := "partially_received"
		if newReceivedTotal.Equal(transfer.Amount) {
			newStatus = "received"
		}
		if err := tx.UpdateTransferReceipt(ctx, transfer.ID, receiptTxID, in.ReceivedAmount, newStatus); err != nil {
			return fmt.Errorf("update transfer receipt: %w", err)
		}

		if err := tx.RecordAuditEvent(ctx, scope.CompanyID, scope.UserID, "transfer_received", "transfer", transfer.ID,
			map[string]any{"amount": in.ReceivedAmount.String(), "status": newStatus, "journal_id": journalID}); err != nil {
			return fmt.Errorf("record audit event: %w", err)
		}
		if err := tx.RecordSyncChange(ctx, scope.CompanyID, in.BranchID, "transaction", receiptTxID, 1, "created"); err != nil {
			return fmt.Errorf("record sync change (transaction): %w", err)
		}
		if err := tx.RecordSyncChange(ctx, scope.CompanyID, in.BranchID, "transfer", transfer.ID, 1, "updated"); err != nil {
			return fmt.Errorf("record sync change (transfer): %w", err)
		}
		if err := tx.EnqueueOutboxJob(ctx, scope.CompanyID, "summary_refresh", "transfer-"+receiptTxID.String(),
			map[string]any{"branch_id": in.BranchID, "transaction_id": receiptTxID}); err != nil {
			return fmt.Errorf("enqueue outbox job: %w", err)
		}

		result = TransferReceiveResult{Accepted: true, TransactionID: receiptTxID, TransferStatus: newStatus, Version: 1}
		return tx.RecordOperation(ctx, OperationRecord{
			CompanyID: scope.CompanyID, OperationID: in.OperationID, BranchID: in.BranchID,
			ActorUserID: scope.UserID, CommandType: "transfer_receive", PayloadHash: payloadHash, Status: "accepted",
			ResultRecordType: "transaction", ResultRecordID: receiptTxID, ResultVersion: 1,
			ClientSubmittedAt: in.ClientSubmittedAt,
		})
	})

	if err != nil {
		if errors.Is(err, ErrConflict) {
			return TransferReceiveResult{}, ErrConflict
		}
		if rejErr, ok := errors.AsType[rejectionError](err); ok {
			if recErr := s.repo.RecordRejection(ctx, OperationRecord{
				CompanyID: scope.CompanyID, OperationID: in.OperationID, BranchID: in.BranchID,
				ActorUserID: scope.UserID, CommandType: "transfer_receive", PayloadHash: payloadHash,
				Status: "rejected", ErrorCode: rejErr.code, ClientSubmittedAt: in.ClientSubmittedAt,
			}); recErr != nil {
				return TransferReceiveResult{}, fmt.Errorf("record rejected operation: %w", recErr)
			}
			return TransferReceiveResult{Accepted: false, ErrorCode: rejErr.code}, nil
		}
		return TransferReceiveResult{}, err
	}
	return result, nil
}
