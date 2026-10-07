package accounting

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/ledgerlink/branchledger/backend/internal/inventory"
	"github.com/ledgerlink/branchledger/backend/internal/tenancy"
)

var (
	// ErrConflict is returned when an operation ID is reused with a different
	// payload — the architecture's "HTTP 409 represents an incompatible
	// revision or reused key with different content."
	ErrConflict = errors.New("accounting: operation conflict")
	// ErrPeriodClosed covers both a locked period and no period defined for
	// the document date.
	ErrPeriodClosed = errors.New("accounting: posting period is not open")
	ErrOutOfScope    = errors.New("accounting: branch is outside the caller's scope")
	ErrUnknownDocType = errors.New("accounting: unsupported document type")
	// ErrUnsupportedTransferType covers transfer_type values this
	// implementation does not yet handle (only "cash" posts; see
	// deriveJournal's "transfer" case).
	ErrUnsupportedTransferType = errors.New("accounting: unsupported transfer type")
	ErrMissingReceiverBranch   = errors.New("accounting: a transfer requires a receiver branch")
	// ErrUnknownAdjustmentType covers an AdjustmentType other than "waste" or
	// "count" on a stock_adjustment document (see deriveJournal's
	// "stock_adjustment" case).
	ErrUnknownAdjustmentType = errors.New("accounting: unsupported stock adjustment type")
	// ErrNoStockChange is returned when every line of a stock-count
	// adjustment matches the recorded position — nothing to post, since
	// BuildJournal rejects a zero-amount journal outright.
	ErrNoStockChange = errors.New("accounting: stock count matches the recorded position")
)

// PostInput is what an API handler gathers from a validated request before
// calling Service.Post. It intentionally does not include server-authoritative
// fields (totals, account IDs) — those are derived inside the transaction
// boundary, never trusted from the client, per "The server calculates
// authoritative totals and rejects client mismatches."
type PostInput struct {
	OperationID      uuid.UUID
	BranchID         uuid.UUID
	DeviceID         *uuid.UUID
	DocumentType     string // sale | purchase | expense | receipt | payment
	DocumentDate     time.Time
	CurrencyCode     string
	ExchangeRate     decimal.Decimal // reporting units per original unit
	CounterpartyID   *uuid.UUID
	PaymentAccountID *uuid.UUID
	SourceReference  string
	Explanation      string
	Lines            []LineInput
	ClientSubmittedAt time.Time

	// DueDate matters only for a credit sale (CounterpartyID set, no
	// PaymentAccountID) — when the customer's payment is expected, for
	// overdue-debt alerting. Nil for cash transactions.
	DueDate *time.Time

	// ReceiverBranchID and TransferType are populated only when
	// DocumentType == "transfer": the dispatch side of a branch transfer
	// (architecture FR07). BranchID is the sending branch; the net amount
	// (as for every other document type) comes from Lines. Only "cash"
	// transfers are implemented — see deriveJournal's "transfer" case.
	ReceiverBranchID *uuid.UUID
	TransferType     string

	// AdjustmentType is populated only when DocumentType == "stock_adjustment":
	// "waste" (Lines[].Quantity is the quantity wasted, Explanation is the
	// required reason) or "count" (Lines[].Quantity is the physical count
	// result for that product, compared against the recorded position via
	// inventory.CountAdjustment). See deriveJournal's "stock_adjustment" case.
	AdjustmentType string
}

// LineInput is one transaction line as submitted by the client.
type LineInput struct {
	ProductID   *uuid.UUID
	Description string
	Quantity    decimal.Decimal
	UnitPrice   decimal.Decimal
	Discount    decimal.Decimal
	TaxCode     string
}

// PostResult is what the sync/push and /transactions endpoints return.
type PostResult struct {
	Accepted         bool
	TransactionID    uuid.UUID
	Version          int
	ErrorCode        string
	AlreadyProcessed bool   // true when this was a duplicate-safe replay
	Status           string // "posted" | "awaiting_approval" (empty on a rejection)
}

// Service implements the accounting transaction boundary.
type Service struct {
	repo Repository
}

func NewService(repo Repository) *Service {
	return &Service{repo: repo}
}

// Post runs the full accounting transaction boundary for one guided command:
// idempotency check, period/version validation, journal derivation via the
// posting rules, stock valuation, persistence, audit, outbox, and commit —
// or a complete rollback if any step fails. This is the only path by which a
// transaction becomes Posted.
func (s *Service) Post(ctx context.Context, scope tenancy.Scope, in PostInput) (PostResult, error) {
	if err := scope.RequireBranch(in.BranchID); err != nil {
		return PostResult{}, ErrOutOfScope
	}

	// 0. The branch must actually belong to the caller's company. The token's
	// own BranchScope (checked above) is an application-level ACL for
	// branch-restricted roles; this is the authoritative, database-backed
	// tenancy check that catches a cross-company or nonexistent branch ID
	// regardless of role (AT04: "Cross-company branch ID -> Access denied
	// and audited"). It runs, and is audited, before any posting transaction
	// opens, so the audit record survives even though nothing else does.
	inCompany, err := s.repo.BranchInCompany(ctx, scope.CompanyID, in.BranchID)
	if err != nil {
		return PostResult{}, fmt.Errorf("check branch company: %w", err)
	}
	if !inCompany {
		if auditErr := s.repo.RecordDenialAudit(ctx, scope.CompanyID, scope.UserID, "access_denied",
			"branch", in.BranchID, map[string]any{"reason": "branch_not_in_company", "operation_id": in.OperationID}); auditErr != nil {
			return PostResult{}, fmt.Errorf("record access denial: %w", auditErr)
		}
		return PostResult{}, ErrOutOfScope
	}

	payloadHash := hashPayload(in)
	var result PostResult

	err = s.repo.WithTx(ctx, func(ctx context.Context, tx Tx) error {
		// 1. Idempotency: a prior identical operation returns its original
		// result; a prior operation with the same ID but different content is
		// a conflict. Never a second posting either way.
		existing, found, err := tx.FindOperation(ctx, scope.CompanyID, in.OperationID)
		if err != nil {
			return fmt.Errorf("find operation: %w", err)
		}
		if found {
			if existing.PayloadHash != payloadHash {
				return ErrConflict
			}
			result = PostResult{
				Accepted:         existing.Status == "accepted",
				TransactionID:    existing.ResultRecordID,
				Version:          existing.ResultVersion,
				ErrorCode:        existing.ErrorCode,
				AlreadyProcessed: true,
			}
			return nil
		}

		// 2. Posting period must be open for the document date.
		period, ok, err := tx.GetOpenPeriod(ctx, scope.CompanyID, in.DocumentDate)
		if err != nil {
			return fmt.Errorf("get open period: %w", err)
		}
		if !ok {
			return rejectionError{code: "period_closed", cause: ErrPeriodClosed}
		}

		// 3. Resolve the chart of accounts and compute line totals. The server
		// is authoritative for totals; client-sent totals, if any, are never
		// trusted (validated upstream in the HTTP layer).
		accts, err := tx.GetChartAccounts(ctx, scope.CompanyID)
		if err != nil {
			return fmt.Errorf("get chart accounts: %w", err)
		}

		txID := uuid.New()
		netAmount := decimal.Zero
		lineRecords := make([]TransactionLineRecord, 0, len(in.Lines))
		for i, l := range in.Lines {
			lineNet := l.Quantity.Mul(l.UnitPrice).Sub(l.Discount).Round(2)
			netAmount = netAmount.Add(lineNet)
			lineRecords = append(lineRecords, TransactionLineRecord{
				LineNo: i + 1, ProductID: l.ProductID, Description: l.Description,
				Quantity: l.Quantity, UnitPrice: l.UnitPrice, Discount: l.Discount,
				TaxCode: l.TaxCode, LineNet: lineNet,
			})
		}

		// 3b. "Above-limit requests enter Awaiting approval" (section 5.4):
		// an expense above the actor's configured approval_limit never
		// derives or posts a journal here at all. It is saved as-is and
		// waits for POST /approvals/{id}/decision, which re-derives and
		// posts the same expense journal once an independent approver
		// accepts it (DecideApproval in approval.go). A nil limit means the
		// actor's membership carries no configured limit, i.e. unrestricted.
		if in.DocumentType == "expense" {
			limit, err := tx.GetApprovalLimit(ctx, scope.CompanyID, scope.UserID)
			if err != nil {
				return fmt.Errorf("get approval limit: %w", err)
			}
			if limit != nil && netAmount.GreaterThan(*limit) {
				transactionID, _, err := tx.SaveAwaitingApproval(ctx, TransactionRecord{
					ID: txID, CompanyID: scope.CompanyID, BranchID: in.BranchID,
					OperationID: in.OperationID, DocumentType: in.DocumentType, DocumentDate: in.DocumentDate,
					CurrencyCode: in.CurrencyCode, ExchangeRate: in.ExchangeRate, CounterpartyID: in.CounterpartyID,
					PaymentAccountID: in.PaymentAccountID, SourceReference: in.SourceReference, Explanation: in.Explanation,
					Status: "awaiting_approval", Revision: 1, CreatedBy: scope.UserID, Lines: lineRecords,
				}, ApprovalRequestRecord{
					CompanyID: scope.CompanyID, TransactionID: txID, RequestedBy: scope.UserID,
					RecordVersionAtRequest: 1,
				})
				if err != nil {
					return fmt.Errorf("save awaiting-approval transaction: %w", err)
				}
				if err := tx.RecordAuditEvent(ctx, scope.CompanyID, scope.UserID, "transaction_awaiting_approval",
					"transaction", transactionID, map[string]any{"document_type": in.DocumentType, "amount": netAmount.String()}); err != nil {
					return fmt.Errorf("record audit event: %w", err)
				}
				if err := tx.RecordSyncChange(ctx, scope.CompanyID, in.BranchID, "transaction", transactionID, 1, "created"); err != nil {
					return fmt.Errorf("record sync change: %w", err)
				}
				result = PostResult{Accepted: true, TransactionID: transactionID, Version: 1, Status: "awaiting_approval"}
				return tx.RecordOperation(ctx, OperationRecord{
					CompanyID: scope.CompanyID, OperationID: in.OperationID, BranchID: in.BranchID,
					DeviceID: in.DeviceID, ActorUserID: scope.UserID, CommandType: in.DocumentType,
					PayloadHash: payloadHash, Status: "accepted", ResultRecordType: "transaction",
					ResultRecordID: transactionID, ResultVersion: 1, ClientSubmittedAt: in.ClientSubmittedAt,
				})
			}
		}

		// 4. Derive the balanced journal per the posting catalogue, applying
		// stock valuation (weighted-average issue) for document types that
		// move inventory.
		journal, costAmount, err := s.deriveJournal(ctx, tx, scope, txID, in, accts, netAmount)
		if err != nil {
			if errors.Is(err, inventory.ErrInsufficientStock) {
				return rejectionError{code: "insufficient_stock", cause: err}
			}
			if errors.Is(err, ErrUnknownDocType) || errors.Is(err, ErrUnsupportedTransferType) || errors.Is(err, ErrMissingReceiverBranch) || errors.Is(err, ErrUnknownAdjustmentType) {
				return rejectionError{code: "unsupported_document_type", cause: err}
			}
			if errors.Is(err, ErrNoStockChange) {
				return rejectionError{code: "no_stock_change", cause: err}
			}
			return fmt.Errorf("derive journal: %w", err)
		}
		_ = costAmount

		// 5. Persist source transaction, journal, and (if applicable) stock
		// movement, all inside this one database transaction.
		if err := tx.SaveTransaction(ctx, TransactionRecord{
			ID: txID, CompanyID: scope.CompanyID, BranchID: in.BranchID,
			OperationID: in.OperationID, DocumentType: in.DocumentType, DocumentDate: in.DocumentDate,
			CurrencyCode: in.CurrencyCode, ExchangeRate: in.ExchangeRate, CounterpartyID: in.CounterpartyID,
			PaymentAccountID: in.PaymentAccountID, SourceReference: in.SourceReference, Explanation: in.Explanation,
			Status: "posted", Revision: 1, CreatedBy: scope.UserID, Lines: lineRecords, DueDate: in.DueDate,
		}); err != nil {
			return fmt.Errorf("save transaction: %w", err)
		}

		journalID, err := tx.SaveJournal(ctx, period.ID, journal)
		if err != nil {
			return fmt.Errorf("save journal: %w", err)
		}

		// 5b. A transfer dispatch also creates the transfers row the
		// receiving side will complete via ReceiveTransfer (transfer.go).
		if in.DocumentType == "transfer" {
			if _, err := tx.CreateTransfer(ctx, TransferRecord{
				CompanyID: scope.CompanyID, TransferType: in.TransferType, SenderBranchID: in.BranchID,
				ReceiverBranchID: *in.ReceiverBranchID, DispatchTransactionID: txID, Status: "dispatched",
				Amount: netAmount, ReceivedAmount: decimal.Zero,
				CurrencyCode: in.CurrencyCode, ExchangeRate: in.ExchangeRate,
			}); err != nil {
				return fmt.Errorf("create transfer: %w", err)
			}
		}

		// 6. Audit trail, change feed for sync pull, and background outbox —
		// all inside the same commit boundary per "writes audit and outbox
		// rows, stores the operation result and commits."
		if err := tx.RecordAuditEvent(ctx, scope.CompanyID, scope.UserID, "transaction_posted",
			"transaction", txID, map[string]any{"document_type": in.DocumentType, "journal_id": journalID}); err != nil {
			return fmt.Errorf("record audit event: %w", err)
		}
		if err := tx.RecordSyncChange(ctx, scope.CompanyID, in.BranchID, "transaction", txID, 1, "created"); err != nil {
			return fmt.Errorf("record sync change: %w", err)
		}
		if err := tx.EnqueueOutboxJob(ctx, scope.CompanyID, "summary_refresh", txID.String(),
			map[string]any{"branch_id": in.BranchID, "transaction_id": txID}); err != nil {
			return fmt.Errorf("enqueue outbox job: %w", err)
		}

		result = PostResult{Accepted: true, TransactionID: txID, Version: 1, Status: "posted"}

		return tx.RecordOperation(ctx, OperationRecord{
			CompanyID: scope.CompanyID, OperationID: in.OperationID, BranchID: in.BranchID,
			DeviceID: in.DeviceID, ActorUserID: scope.UserID, CommandType: in.DocumentType,
			PayloadHash: payloadHash, Status: "accepted", ResultRecordType: "transaction",
			ResultRecordID: txID, ResultVersion: 1, ClientSubmittedAt: in.ClientSubmittedAt,
		})
	})

	if err != nil {
		if errors.Is(err, ErrConflict) {
			return PostResult{}, ErrConflict
		}

		if rejErr, ok := errors.AsType[rejectionError](err); ok {
			// The posting attempt rolled back in full — including any
			// partial stock movements issued before the failure point — so
			// nothing about the attempted posting survives. The rejection
			// itself is then recorded in its own, separate commit: a
			// rejected operation must be durably idempotent (a retry with
			// the same operation ID returns the same rejection) without
			// ever persisting a partial posting alongside it.
			if recErr := s.repo.RecordRejection(ctx, OperationRecord{
				CompanyID: scope.CompanyID, OperationID: in.OperationID, BranchID: in.BranchID,
				DeviceID: in.DeviceID, ActorUserID: scope.UserID, CommandType: in.DocumentType,
				PayloadHash: payloadHash, Status: "rejected", ErrorCode: rejErr.code,
				ClientSubmittedAt: in.ClientSubmittedAt,
			}); recErr != nil {
				return PostResult{}, fmt.Errorf("record rejected operation: %w", recErr)
			}
			return PostResult{Accepted: false, ErrorCode: rejErr.code}, nil
		}

		return PostResult{}, err
	}
	return result, nil
}

// rejectionError marks a business-rule rejection (as opposed to an
// infrastructure failure): the posting transaction rolls back because of it,
// and the caller records a standalone rejection afterward. It is never
// returned to an HTTP caller directly — only its code is, via PostResult.
type rejectionError struct {
	code  string
	cause error
}

func (r rejectionError) Error() string { return fmt.Sprintf("accounting: rejected (%s): %v", r.code, r.cause) }
func (r rejectionError) Unwrap() error { return r.cause }

// deriveJournal dispatches to the posting rule matching DocumentType,
// resolving stock cost via the weighted-average ledger where the document
// moves inventory. It returns the balanced journal and the cost-of-sales
// amount (zero where not applicable).
func (s *Service) deriveJournal(ctx context.Context, tx Tx, scope tenancy.Scope, txID uuid.UUID, in PostInput, accts ChartAccounts, netAmount decimal.Decimal) (Journal, decimal.Decimal, error) {
	reportingCurrency := accts.ReportingCurrency()

	switch in.DocumentType {
	case "expense":
		paymentAccount := accts.Cash
		if in.PaymentAccountID != nil {
			paymentAccount = *in.PaymentAccountID
		}
		j, err := BuildJournal(txID, in.CurrencyCode, reportingCurrency, in.ExchangeRate, []Leg{
			{AccountID: accts.OperatingExpense, Side: Debit, Amount: netAmount},
			{AccountID: paymentAccount, Side: Credit, Amount: netAmount},
		})
		return j, decimal.Zero, err

	case "sale":
		costAmount, err := s.issueStockForLines(ctx, tx, scope, txID, in)
		if err != nil {
			return Journal{}, decimal.Zero, err
		}
		isCredit := in.CounterpartyID != nil && in.PaymentAccountID == nil
		if isCredit {
			j, err := CreditSale(txID, accts, netAmount, costAmount, in.CurrencyCode, reportingCurrency, in.ExchangeRate)
			return j, costAmount, err
		}
		j, err := CashSale(txID, accts, netAmount, costAmount, in.CurrencyCode, reportingCurrency, in.ExchangeRate)
		return j, costAmount, err

	case "purchase":
		// Receive stock for every stocked line at its purchase cost before
		// posting — previously this only moved the Inventory *value* account
		// and never touched a product's actual tracked quantity, so nothing
		// a purchase "received" ever became sellable stock or fed a
		// low-stock alert.
		if err := s.receiveStockForLines(ctx, tx, scope, txID, in); err != nil {
			return Journal{}, decimal.Zero, err
		}
		j, err := CreditPurchase(txID, accts, netAmount, in.CurrencyCode, reportingCurrency, in.ExchangeRate)
		return j, decimal.Zero, err

	case "receipt":
		j, err := CustomerReceipt(txID, accts, netAmount, in.CurrencyCode, reportingCurrency, in.ExchangeRate)
		return j, decimal.Zero, err

	case "payment":
		j, err := SupplierPayment(txID, accts, netAmount, in.CurrencyCode, reportingCurrency, in.ExchangeRate)
		return j, decimal.Zero, err

	case "transfer":
		// Only cash transfers post here; a stock transfer additionally needs
		// issuing stock at the sender and (on receipt) receiving it back at
		// the same cost, which ReceiveTransfer does not yet implement, so it
		// is rejected rather than silently mishandled.
		if in.TransferType != "cash" {
			return Journal{}, decimal.Zero, ErrUnsupportedTransferType
		}
		if in.ReceiverBranchID == nil {
			return Journal{}, decimal.Zero, ErrMissingReceiverBranch
		}
		j, err := TransferDispatch(txID, accts, netAmount, in.CurrencyCode, reportingCurrency, in.ExchangeRate)
		return j, decimal.Zero, err

	case "stock_adjustment":
		switch in.AdjustmentType {
		case "waste":
			lossAmount, err := s.wasteStockForLines(ctx, tx, scope, txID, in)
			if err != nil {
				return Journal{}, decimal.Zero, err
			}
			j, err := StockWaste(txID, accts, lossAmount, in.CurrencyCode, reportingCurrency, in.ExchangeRate)
			return j, decimal.Zero, err

		case "count":
			valueChange, err := s.countStockForLines(ctx, tx, scope, txID, in)
			if err != nil {
				return Journal{}, decimal.Zero, err
			}
			j, err := StockCountAdjustment(txID, accts, valueChange, in.CurrencyCode, reportingCurrency, in.ExchangeRate)
			return j, decimal.Zero, err

		default:
			return Journal{}, decimal.Zero, ErrUnknownAdjustmentType
		}

	default:
		return Journal{}, decimal.Zero, ErrUnknownDocType
	}
}

// receiveStockForLines walks a purchase's lines, receiving stock for every
// stocked product line (ProductID set) at its purchase cost, folding it into
// the branch's moving weighted average (architecture: ten units at 5 and ten
// at 7 give a cost pool of 120 for 20 units, average 6). A line with no
// product_id (a service purchase) is skipped, same as issueStockForLines.
func (s *Service) receiveStockForLines(ctx context.Context, tx Tx, scope tenancy.Scope, txID uuid.UUID, in PostInput) error {
	for _, l := range in.Lines {
		if l.ProductID == nil {
			continue
		}
		position, err := tx.GetStockPosition(ctx, scope.CompanyID, in.BranchID, *l.ProductID)
		if err != nil {
			return fmt.Errorf("get stock position: %w", err)
		}
		updated, err := position.Receive(l.Quantity, l.UnitPrice)
		if err != nil {
			return err
		}
		if err := tx.SaveStockMovement(ctx, StockMovementRecord{
			ID: uuid.New(), CompanyID: scope.CompanyID, BranchID: in.BranchID, ProductID: *l.ProductID,
			SourceTransactionID: txID, MovementType: "receiving", QuantityDelta: l.Quantity,
			UnitCost: l.UnitPrice, RunningQuantity: updated.Quantity, RunningValue: updated.Value,
			CreatedBy: scope.UserID,
		}); err != nil {
			return fmt.Errorf("save stock movement: %w", err)
		}
	}
	return nil
}

// issueStockForLines walks a sale's lines, issuing stock at the branch's
// current weighted-average cost for every stocked product line, and persists
// each resulting stock movement. It returns the total cost of sales.
func (s *Service) issueStockForLines(ctx context.Context, tx Tx, scope tenancy.Scope, txID uuid.UUID, in PostInput) (decimal.Decimal, error) {
	totalCost := decimal.Zero
	for _, l := range in.Lines {
		if l.ProductID == nil {
			continue // service line, no stock movement
		}
		position, err := tx.GetStockPosition(ctx, scope.CompanyID, in.BranchID, *l.ProductID)
		if err != nil {
			return decimal.Zero, fmt.Errorf("get stock position: %w", err)
		}
		updated, issuedCost, err := position.Issue(l.Quantity)
		if err != nil {
			return decimal.Zero, err
		}
		totalCost = totalCost.Add(issuedCost)
		if err := tx.SaveStockMovement(ctx, StockMovementRecord{
			ID: uuid.New(), CompanyID: scope.CompanyID, BranchID: in.BranchID, ProductID: *l.ProductID,
			SourceTransactionID: txID, MovementType: "sale", QuantityDelta: l.Quantity.Neg(),
			UnitCost: position.UnitCost(), RunningQuantity: updated.Quantity, RunningValue: updated.Value,
			CreatedBy: scope.UserID,
		}); err != nil {
			return decimal.Zero, fmt.Errorf("save stock movement: %w", err)
		}
	}
	return totalCost, nil
}

// wasteStockForLines walks a stock_adjustment("waste") transaction's lines,
// issuing stock at the branch's current weighted-average cost for every
// stocked product line (the same mechanics as issueStockForLines, but
// recorded as movement_type "waste" with the transaction's Explanation
// carried as the required reason — architecture section 5.7, "Waste: Record
// reason, quantity and approved loss"), and returns the total cost wasted.
func (s *Service) wasteStockForLines(ctx context.Context, tx Tx, scope tenancy.Scope, txID uuid.UUID, in PostInput) (decimal.Decimal, error) {
	totalLoss := decimal.Zero
	for _, l := range in.Lines {
		if l.ProductID == nil {
			continue
		}
		position, err := tx.GetStockPosition(ctx, scope.CompanyID, in.BranchID, *l.ProductID)
		if err != nil {
			return decimal.Zero, fmt.Errorf("get stock position: %w", err)
		}
		updated, issuedCost, err := position.Issue(l.Quantity)
		if err != nil {
			return decimal.Zero, err
		}
		totalLoss = totalLoss.Add(issuedCost)
		if err := tx.SaveStockMovement(ctx, StockMovementRecord{
			ID: uuid.New(), CompanyID: scope.CompanyID, BranchID: in.BranchID, ProductID: *l.ProductID,
			SourceTransactionID: txID, MovementType: "waste", QuantityDelta: l.Quantity.Neg(),
			UnitCost: position.UnitCost(), RunningQuantity: updated.Quantity, RunningValue: updated.Value,
			Reason: in.Explanation, CreatedBy: scope.UserID,
		}); err != nil {
			return decimal.Zero, fmt.Errorf("save stock movement: %w", err)
		}
	}
	if totalLoss.IsZero() {
		return decimal.Zero, ErrNoStockChange
	}
	return totalLoss, nil
}

// countStockForLines walks a stock_adjustment("count") transaction's lines,
// comparing each line's Quantity (the physical count result) against the
// branch's recorded position via inventory.CountAdjustment (architecture
// section 5.7, "Stock count: Compare counted and expected stock at a defined
// cut-off"). A line whose count matches the recorded position produces no
// movement. It returns the net signed valuation change across every line —
// negative for a net shortfall, positive for a net surplus — which
// StockCountAdjustment posts as a single journal.
func (s *Service) countStockForLines(ctx context.Context, tx Tx, scope tenancy.Scope, txID uuid.UUID, in PostInput) (decimal.Decimal, error) {
	netChange := decimal.Zero
	for _, l := range in.Lines {
		if l.ProductID == nil {
			continue
		}
		position, err := tx.GetStockPosition(ctx, scope.CompanyID, in.BranchID, *l.ProductID)
		if err != nil {
			return decimal.Zero, fmt.Errorf("get stock position: %w", err)
		}
		deltaQty, deltaValue := inventory.CountAdjustment(position, l.Quantity)
		if deltaQty.IsZero() {
			continue
		}
		netChange = netChange.Add(deltaValue)
		updated := inventory.Position{Quantity: l.Quantity, Value: position.Value.Add(deltaValue)}
		if err := tx.SaveStockMovement(ctx, StockMovementRecord{
			ID: uuid.New(), CompanyID: scope.CompanyID, BranchID: in.BranchID, ProductID: *l.ProductID,
			SourceTransactionID: txID, MovementType: "count_adjustment", QuantityDelta: deltaQty,
			UnitCost: position.UnitCost(), RunningQuantity: updated.Quantity, RunningValue: updated.Value,
			Reason: in.Explanation, CreatedBy: scope.UserID,
		}); err != nil {
			return decimal.Zero, fmt.Errorf("save stock movement: %w", err)
		}
	}
	if netChange.IsZero() {
		return decimal.Zero, ErrNoStockChange
	}
	return netChange, nil
}

// hashPayload computes a stable hash of the caller-controlled parts of a
// PostInput, used to tell "same operation ID, same content" (safe replay)
// apart from "same operation ID, different content" (409 conflict).
func hashPayload(in PostInput) string {
	type stable struct {
		BranchID        uuid.UUID
		DocumentType    string
		DocumentDate    time.Time
		CurrencyCode    string
		ExchangeRate    string
		CounterpartyID  *uuid.UUID
		PaymentAccountID *uuid.UUID
		SourceReference string
		Explanation     string
		Lines           []LineInput
	}
	b, _ := json.Marshal(stable{
		BranchID: in.BranchID, DocumentType: in.DocumentType, DocumentDate: in.DocumentDate,
		CurrencyCode: in.CurrencyCode, ExchangeRate: in.ExchangeRate.String(), CounterpartyID: in.CounterpartyID,
		PaymentAccountID: in.PaymentAccountID, SourceReference: in.SourceReference, Explanation: in.Explanation,
		Lines: in.Lines,
	})
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// hashAny is hashPayload generalized to any caller-controlled request shape,
// used by Reverse/SubmitClose/DecideApproval/ReceiveTransfer the same way
// hashPayload is used by Post: to tell a safe retry (identical payload) apart
// from a reused operation ID with different content (409 conflict).
func hashAny(v any) string {
	b, _ := json.Marshal(v)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// finishPost is the shared tail of every WithTx-wrapped command (Post,
// Reverse, SubmitClose, DecideApproval, dispatch/receive a transfer): a
// rejectionError rolls the attempt back in full and then gets its own
// durable, independently idempotent record via RecordRejection; ErrConflict
// passes straight through; any other error is an infrastructure failure the
// caller logs and reports as 500.
func (s *Service) finishPost(ctx context.Context, scope tenancy.Scope, operationID, branchID uuid.UUID, commandType, payloadHash string, clientSubmittedAt time.Time, result PostResult, txErr error) (PostResult, error) {
	if txErr == nil {
		return result, nil
	}
	if errors.Is(txErr, ErrConflict) {
		return PostResult{}, ErrConflict
	}
	if rejErr, ok := errors.AsType[rejectionError](txErr); ok {
		if recErr := s.repo.RecordRejection(ctx, OperationRecord{
			CompanyID: scope.CompanyID, OperationID: operationID, BranchID: branchID,
			ActorUserID: scope.UserID, CommandType: commandType, PayloadHash: payloadHash,
			Status: "rejected", ErrorCode: rejErr.code, ClientSubmittedAt: clientSubmittedAt,
		}); recErr != nil {
			return PostResult{}, fmt.Errorf("record rejected operation: %w", recErr)
		}
		return PostResult{Accepted: false, ErrorCode: rejErr.code}, nil
	}
	return PostResult{}, txErr
}

// ReportingCurrency is attached to ChartAccounts via a small helper rather
// than a stored field, since the resolved accounts always belong to one
// company whose reporting currency the repository already knows when it
// builds the ChartAccounts value.
func (c ChartAccounts) ReportingCurrency() string { return c.reportingCurrency }
