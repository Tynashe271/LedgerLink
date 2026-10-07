package accounting

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/ledgerlink/branchledger/backend/internal/inventory"
)

// Repository is everything the posting service needs from persistence. It is
// implemented by internal/storage against PostgreSQL; defining it here (owned
// by the domain package, not the storage package) keeps the accounting
// transaction boundary testable with a fake, independent of any database.
//
// Every method that mutates state is expected to run inside the single
// database transaction opened by WithTx, matching the architecture's
// "Accounting transaction boundary": begin, check period and version, derive
// lines, validate, apply stock valuation, persist, write audit and outbox,
// commit; rollback on any failure.
type Repository interface {
	// WithTx runs fn inside one database transaction and commits only if fn
	// returns nil; any error rolls back the complete attempted posting.
	WithTx(ctx context.Context, fn func(ctx context.Context, tx Tx) error) error

	// BranchInCompany reports whether branchID belongs to companyID. It runs
	// outside any posting transaction so the scope check happens, and can be
	// audited, before anything is opened that a later rollback would undo.
	BranchInCompany(ctx context.Context, companyID, branchID uuid.UUID) (bool, error)

	// RecordDenialAudit durably records a denied-access attempt in its own
	// commit, independent of the (rolled-back) attempt it documents.
	// Security events must survive the failure they describe — "Audit
	// login, access denials, role changes and exports" (launch control #38)
	// is an unconditional requirement, not contingent on the denied action
	// having otherwise succeeded.
	RecordDenialAudit(ctx context.Context, companyID, actorUserID uuid.UUID, eventType, recordType string, recordID uuid.UUID, details map[string]any) error

	// RecordRejection durably records a business-rule rejection (period
	// closed, insufficient stock, ...) in its own commit, after the posting
	// attempt that triggered it has already rolled back in full. This is
	// what makes a retry of a rejected operation ID return the same
	// rejection instead of re-attempting a posting from scratch.
	RecordRejection(ctx context.Context, rec OperationRecord) error
}

// Tx is the subset of repository operations valid inside a posting
// transaction.
type Tx interface {
	// FindOperation looks up a previously recorded operation by (companyID,
	// operationID). ok=false means this operation has never been seen.
	FindOperation(ctx context.Context, companyID, operationID uuid.UUID) (OperationRecord, bool, error)

	// RecordOperation durably stores the outcome of processing an operation,
	// making retries with the same operation ID return this same result
	// instead of posting twice (the architecture's at-least-once + idempotent
	// processing rule).
	RecordOperation(ctx context.Context, rec OperationRecord) error

	// GetOpenPeriod returns the open accounting period covering date, or
	// ok=false if the date falls in a locked or undefined period.
	GetOpenPeriod(ctx context.Context, companyID uuid.UUID, date time.Time) (Period, bool, error)

	// GetChartAccounts resolves the company's standard account IDs used by
	// the posting rules in posting_rules.go.
	GetChartAccounts(ctx context.Context, companyID uuid.UUID) (ChartAccounts, error)

	// GetStockPosition returns the current weighted-average position for a
	// product at a branch, used to cost sale/waste/transfer issues.
	GetStockPosition(ctx context.Context, companyID, branchID, productID uuid.UUID) (inventory.Position, error)

	// PurchaseInvoiceExists reports whether a posted purchase already exists
	// for this supplier and source reference (migration 0001's
	// idx_transactions_duplicate_check exists to make exactly this query
	// fast). User Manual: "Check possible duplicates before recording."
	PurchaseInvoiceExists(ctx context.Context, companyID, counterpartyID uuid.UUID, sourceReference string) (bool, error)

	// SaveStockMovement persists one inventory movement row and the position
	// it produced.
	SaveStockMovement(ctx context.Context, m StockMovementRecord) error

	// SaveTransaction persists the source document at status "posted" (or
	// "awaiting_approval" if the service routed it there before reaching
	// posting) together with its lines.
	SaveTransaction(ctx context.Context, t TransactionRecord) error

	// SaveJournal persists the balanced journal and its lines, linked to the
	// source transaction and the open period.
	SaveJournal(ctx context.Context, periodID uuid.UUID, j Journal) (journalID uuid.UUID, err error)

	// RecordAuditEvent appends an immutable audit row.
	RecordAuditEvent(ctx context.Context, companyID uuid.UUID, actorUserID uuid.UUID, eventType string, recordType string, recordID uuid.UUID, details map[string]any) error

	// EnqueueOutboxJob schedules background work (exports, alerts, attachment
	// processing) to run after this commit, per "External effects ... do not
	// occur inside the financial database commit."
	EnqueueOutboxJob(ctx context.Context, companyID uuid.UUID, jobType, idempotencyKey string, payload map[string]any) error

	// RecordSyncChange appends a row to the change feed that sync pull reads,
	// so other devices learn about this posting on their next pull.
	RecordSyncChange(ctx context.Context, companyID, branchID uuid.UUID, recordType string, recordID uuid.UUID, version int, kind string) error

	// GetPostedTransaction loads a transaction that is currently "posted"
	// (not draft, not already reversed, not awaiting approval) together with
	// its journal, so Reverse can build the exact mirror-image posting.
	// ok=false covers both "no such transaction" and "not currently posted"
	// — Reverse treats both as the same rejection.
	GetPostedTransaction(ctx context.Context, companyID, transactionID uuid.UUID) (TransactionRecord, Journal, bool, error)

	// GetStockMovementsByTransaction returns the stock movements an original
	// posting caused, so Reverse can undo exactly the quantity and value they
	// moved.
	GetStockMovementsByTransaction(ctx context.Context, companyID, transactionID uuid.UUID) ([]StockMovementRecord, error)

	// MarkTransactionReversed flips the original transaction to status
	// "reversed" and links it to the reversal transaction just posted.
	// Posted entries stay immutable otherwise — this is the only field a
	// reversal changes on the original row.
	MarkTransactionReversed(ctx context.Context, originalTransactionID, reversalTransactionID uuid.UUID) error

	// GetApprovalLimit returns the actor's configured per-request spending
	// limit from their membership (nil = no configured limit, i.e.
	// unrestricted), used to decide whether an expense posts immediately or
	// is routed to Awaiting approval.
	GetApprovalLimit(ctx context.Context, companyID, userID uuid.UUID) (*decimal.Decimal, error)

	// SaveAwaitingApproval persists a transaction at status
	// "awaiting_approval" (no journal, no stock movement — nothing posts
	// until a decision is recorded) and the approval request that references
	// it, atomically.
	SaveAwaitingApproval(ctx context.Context, t TransactionRecord, approval ApprovalRequestRecord) (transactionID, approvalID uuid.UUID, err error)

	// GetApprovalRequest loads a pending approval request and the full
	// transaction it gates, scoped to the company. ok=false if the approval
	// does not exist, belongs to another company, or has already been
	// decided.
	GetApprovalRequest(ctx context.Context, companyID, approvalID uuid.UUID) (ApprovalRequestRecord, TransactionRecord, bool, error)

	// RecordApprovalDecision persists the decision on the approvals row
	// itself (decided_by, decision, reason) — never who requested it, which
	// stays fixed. Called exactly once per approval, inside the same
	// transaction as the resulting MarkTransactionPosted/Rejected call.
	RecordApprovalDecision(ctx context.Context, approvalID, decidedBy uuid.UUID, decision, reason string) error

	// MarkTransactionPosted transitions an awaiting-approval transaction to
	// "posted" once an approver has accepted it, stamping posted_at.
	MarkTransactionPosted(ctx context.Context, transactionID uuid.UUID) error

	// MarkTransactionRejected transitions an awaiting-approval transaction to
	// "rejected"; no journal is ever written for it.
	MarkTransactionRejected(ctx context.Context, transactionID uuid.UUID) error

	// CreateTransfer inserts the transfers (+ transfer_lines) row for a
	// dispatch that has already been saved via SaveTransaction/SaveJournal in
	// the same database transaction — always at status "dispatched".
	CreateTransfer(ctx context.Context, rec TransferRecord) (transferID uuid.UUID, err error)

	// GetTransferForReceipt loads a dispatched or partially-received transfer
	// scoped to the company, so ReceiveTransfer can validate the receiving
	// branch and the remaining unreceived balance.
	GetTransferForReceipt(ctx context.Context, companyID, transferID uuid.UUID) (TransferRecord, bool, error)

	// UpdateTransferReceipt records a (possibly partial) receipt against a
	// transfer: the newly received amount, the receiving transaction that
	// was just saved via SaveTransaction/SaveJournal, and the resulting
	// status ("partially_received" or "received").
	UpdateTransferReceipt(ctx context.Context, transferID, receiptTransactionID uuid.UUID, receivedAmount decimal.Decimal, newStatus string) error

	// GetLatestClose returns the latest daily-close revision for (branch,
	// date, currency), so SubmitClose can enforce "one active close revision
	// per branch day currency" and compute the next revision on resubmission.
	GetLatestClose(ctx context.Context, companyID, branchID uuid.UUID, closeDate time.Time, currencyCode string) (DailyCloseRecord, bool, error)

	// SaveDailyClose persists a submitted close (count, expected cash,
	// discrepancy) at the given revision.
	SaveDailyClose(ctx context.Context, rec DailyCloseRecord) (uuid.UUID, error)
}

// ApprovalRequestRecord mirrors the `approvals` table.
type ApprovalRequestRecord struct {
	ID                     uuid.UUID
	CompanyID              uuid.UUID
	TransactionID          uuid.UUID
	RequestedBy            uuid.UUID
	RecordVersionAtRequest int
}

// TransferRecord mirrors the `transfers` table plus its lines.
type TransferRecord struct {
	ID                    uuid.UUID
	CompanyID             uuid.UUID
	TransferType          string // "cash" | "stock"
	SenderBranchID        uuid.UUID
	ReceiverBranchID      uuid.UUID
	DispatchTransactionID uuid.UUID
	ReceiptTransactionID  *uuid.UUID
	Status                string
	Amount                decimal.Decimal // the dispatched amount, in its original currency
	ReceivedAmount        decimal.Decimal // cumulative amount received so far, same currency
	CurrencyCode          string
	ExchangeRate          decimal.Decimal
}

// DailyCloseRecord mirrors the `daily_closes` table.
type DailyCloseRecord struct {
	ID           uuid.UUID
	CompanyID    uuid.UUID
	BranchID     uuid.UUID
	CloseDate    time.Time
	CurrencyCode string
	OpeningFloat decimal.Decimal
	ExpectedCash decimal.Decimal
	CountedCash  decimal.Decimal
	Discrepancy  decimal.Decimal
	Explanation  string
	Status       string
	Revision     int
	SubmittedBy  uuid.UUID
}

// OperationRecord mirrors the `operations` table: the idempotency ledger keyed
// by (company_id, operation_id).
type OperationRecord struct {
	CompanyID         uuid.UUID
	OperationID       uuid.UUID
	BranchID          uuid.UUID
	DeviceID          *uuid.UUID
	ActorUserID       uuid.UUID
	CommandType       string
	PayloadHash       string
	Status            string // "accepted" | "rejected"
	ResultRecordType  string
	ResultRecordID    uuid.UUID
	ResultVersion     int
	ErrorCode         string
	ClientSubmittedAt time.Time
}

// Period mirrors the `periods` table.
type Period struct {
	ID        uuid.UUID
	CompanyID uuid.UUID
	StartsOn  time.Time
	EndsOn    time.Time
	Status    string
}

// TransactionRecord mirrors the `transactions` table plus its lines, as
// persisted after posting succeeds.
type TransactionRecord struct {
	ID               uuid.UUID
	CompanyID        uuid.UUID
	BranchID         uuid.UUID
	OperationID      uuid.UUID
	DocumentType     string
	DocumentDate     time.Time
	CurrencyCode     string
	ExchangeRate     decimal.Decimal
	CounterpartyID   *uuid.UUID
	PaymentAccountID *uuid.UUID
	SourceReference  string
	Explanation      string
	Status           string
	Revision         int
	CreatedBy        uuid.UUID
	Lines            []TransactionLineRecord
	// ReversesTransactionID is set only on a reversal transaction, linking it
	// back to the original it mirrors (the original's own
	// reversed_by_transaction_id is set separately via
	// MarkTransactionReversed once the reversal's ID is known).
	ReversesTransactionID *uuid.UUID
	// DueDate matters only for a credit sale; nil otherwise.
	DueDate *time.Time
}

// TransactionLineRecord mirrors `transaction_lines`.
type TransactionLineRecord struct {
	LineNo      int
	ProductID   *uuid.UUID
	Description string
	Quantity    decimal.Decimal
	UnitPrice   decimal.Decimal
	Discount    decimal.Decimal
	TaxCode     string
	LineNet     decimal.Decimal
}

// StockMovementRecord mirrors `stock_movements`.
type StockMovementRecord struct {
	ID                  uuid.UUID
	CompanyID           uuid.UUID
	BranchID            uuid.UUID
	ProductID           uuid.UUID
	SourceTransactionID uuid.UUID
	MovementType        string
	QuantityDelta       decimal.Decimal
	UnitCost            decimal.Decimal
	RunningQuantity     decimal.Decimal
	RunningValue        decimal.Decimal
	Reason              string
	CreatedBy           uuid.UUID
}
