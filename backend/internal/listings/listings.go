// Package listings implements the read-only list views the screen catalogue
// (BranchLedger_System_Documentation section 5.2: Transactions, Approvals,
// Stock/transfers) needs and the architecture's 10-route API contract table
// never separately specified. These are plain scoped reads — no posting
// boundary, no idempotency ledger — so, unlike internal/accounting, there is
// no WithTx indirection here.
package listings

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/ledgerlink/branchledger/backend/internal/tenancy"
)

// TransactionSummary is one row of GET /api/v1/transactions.
type TransactionSummary struct {
	ID              uuid.UUID
	BranchID        uuid.UUID
	BranchName      string
	DocumentType    string
	DocumentDate    time.Time
	CurrencyCode    string
	Status          string
	NetAmount       decimal.Decimal
	SourceReference string
	Explanation     string
}

// TransactionFilter narrows GET /api/v1/transactions.
type TransactionFilter struct {
	BranchID     *uuid.UUID
	DocumentType string // "" = any
	Status       string // "" = any
	From         time.Time
	To           time.Time
	Limit        int
}

// ApprovalSummary is one row of GET /api/v1/approvals.
type ApprovalSummary struct {
	ApprovalID             uuid.UUID
	TransactionID          uuid.UUID
	BranchID               uuid.UUID
	BranchName             string
	DocumentType           string
	DocumentDate           time.Time
	NetAmount              decimal.Decimal
	CurrencyCode           string
	Explanation            string
	RequestedByName        string
	RequestedAt            time.Time
	RecordVersionAtRequest int
}

// TransferSummary is one row of GET /api/v1/transfers.
type TransferSummary struct {
	ID                 uuid.UUID
	TransferType       string
	SenderBranchName   string
	ReceiverBranchID   uuid.UUID
	ReceiverBranchName string
	Status             string
	Amount             decimal.Decimal
	ReceivedAmount     decimal.Decimal
	CurrencyCode       string
	DispatchedAt       time.Time
}

// StockPositionSummary is one row of GET /api/v1/stock: a product's current
// on-hand balance and valuation at one branch, taken from its latest
// recorded stock movement there. Position is tracked per (branch, product)
// pair, never collapsed to one branch — the owner/manager view must show
// every sub-branch's stock, not just one.
type StockPositionSummary struct {
	ProductID   uuid.UUID
	SKU         string
	ProductName string
	Unit        string
	BranchID    uuid.UUID
	BranchName  string
	Quantity    decimal.Decimal
	Value       decimal.Decimal
}

// Store is the read-only aggregation these listings need from Postgres.
type Store interface {
	ListTransactions(ctx context.Context, companyID uuid.UUID, allowedBranches []uuid.UUID, f TransactionFilter) ([]TransactionSummary, error)
	ListPendingApprovals(ctx context.Context, companyID uuid.UUID, allowedBranches []uuid.UUID) ([]ApprovalSummary, error)
	ListOpenTransfers(ctx context.Context, companyID uuid.UUID, allowedBranches []uuid.UUID) ([]TransferSummary, error)
	ListStockPositions(ctx context.Context, companyID uuid.UUID, allowedBranches []uuid.UUID) ([]StockPositionSummary, error)
}

type Service struct {
	store Store
}

func NewService(store Store) *Service { return &Service{store: store} }

// Transactions lists transactions in the caller's scope. A branch-restricted
// role's explicit branch_id is checked against their scope the same way
// reporting.Service.Dashboard checks it; requesting no branch_id lists only
// the branches they're allowed to see.
func (s *Service) Transactions(ctx context.Context, scope tenancy.Scope, f TransactionFilter) ([]TransactionSummary, error) {
	if f.BranchID != nil {
		if err := scope.RequireBranch(*f.BranchID); err != nil {
			return nil, err
		}
	}
	if f.Limit <= 0 || f.Limit > 200 {
		f.Limit = 50
	}
	return s.store.ListTransactions(ctx, scope.CompanyID, scope.BranchScope, f)
}

// PendingApprovals lists transactions currently awaiting a decision, in the
// caller's scope.
func (s *Service) PendingApprovals(ctx context.Context, scope tenancy.Scope) ([]ApprovalSummary, error) {
	return s.store.ListPendingApprovals(ctx, scope.CompanyID, scope.BranchScope)
}

// OpenTransfers lists transfers not yet fully received, in the caller's
// scope (either side — a branch manager needs to see what's in transit both
// to and from their branch).
func (s *Service) OpenTransfers(ctx context.Context, scope tenancy.Scope) ([]TransferSummary, error) {
	return s.store.ListOpenTransfers(ctx, scope.CompanyID, scope.BranchScope)
}

// StockPositions lists the current on-hand balance and valuation for every
// product with a recorded stock movement, in the caller's scope — every
// branch for an unrestricted role, matching the dashboard's company-wide
// aggregate rather than narrowing to one branch.
func (s *Service) StockPositions(ctx context.Context, scope tenancy.Scope) ([]StockPositionSummary, error) {
	return s.store.ListStockPositions(ctx, scope.CompanyID, scope.BranchScope)
}
