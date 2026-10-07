// Package reporting implements the owner/manager dashboard described in
// BranchLedger_Architecture.pdf ("GET /api/v1/dashboard: Branch/date/currency
// scope and freshness metadata") and BranchLedger_System_Documentation
// section 5.8/5.10: revenue, gross/net profit, cash and bank balances,
// receivables, payables and branch sync freshness, all computed from posted
// journals only — never from provisional/offline-pending records.
package reporting

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/ledgerlink/branchledger/backend/internal/tenancy"
)

// Query is the caller's requested scope for one dashboard read.
type Query struct {
	// BranchID narrows to one branch; nil means "every branch the caller's
	// scope allows" (architecture: owner/accountant see the whole company,
	// a branch-restricted role is still limited to their own branches).
	BranchID *uuid.UUID
	From     time.Time
	To       time.Time
}

// BranchFreshness reports when a branch's offline device(s) last reached the
// server, so a stale branch can be flagged rather than silently presenting
// an incomplete company total as current (System Documentation section
// 5.10: "A stale branch gets a visible badge and company results become
// incomplete").
type BranchFreshness struct {
	BranchID   uuid.UUID
	BranchName string
	LastSyncAt *time.Time
	Stale      bool
}

// Result is the full response to GET /api/v1/dashboard. All money figures
// are in the company's reporting currency. Revenue/CostOfSales/
// OperatingExpenses/NetProfit are period movements (From..To inclusive);
// the balance-sheet figures (cash, bank, receivables, payables) are
// as-of-To cumulative balances, matching the distinction in section 5.13
// ("Profit and loss uses period movements. Balance sheet uses cumulative
// balances at the selected date").
type Result struct {
	CompanyID         uuid.UUID
	ScopeBranchID     *uuid.UUID
	From              time.Time
	To                time.Time
	ReportingCurrency string
	GeneratedAt       time.Time

	Revenue           decimal.Decimal
	CostOfSales       decimal.Decimal
	GrossProfit       decimal.Decimal
	OperatingExpenses decimal.Decimal
	NetProfit         decimal.Decimal

	CashBalance decimal.Decimal
	BankBalance decimal.Decimal
	Receivables decimal.Decimal
	Payables    decimal.Decimal

	Branches []BranchFreshness

	// BranchBreakdown is per-branch revenue/expenses/net profit for the same
	// period, powering the manager overview's branch comparison bars
	// (User Manual: "Branch bars show performance comparisons"). Populated
	// only for an all-branches view (Query.BranchID == nil) — a single-branch
	// view has nothing to compare against itself.
	BranchBreakdown []BranchPeriodTotal

	// ExpenseBreakdown is operating expense totals by account for the period
	// ("Expense breakdowns show where spending occurs"), largest first.
	ExpenseBreakdown []ExpenseCategory

	// DailyTrend is daily revenue across the requested range ("Trend lines
	// show movement over time"), one point per calendar day from From to To.
	DailyTrend []DailyPoint

	// PendingApprovalsCount is the number of transactions currently Awaiting
	// approval in scope ("Check ... pending approvals").
	PendingApprovalsCount int

	// CashDifferenceAlerts lists submitted-but-not-yet-approved daily closes
	// with a nonzero discrepancy ("Check ... cash differences").
	CashDifferenceAlerts []CashDifferenceAlert

	// RejectedOperationAlerts lists recently rejected postings (period
	// closed, insufficient stock, unsupported document type, ...) so a
	// manager sees what staff tried and failed to record, not just what
	// posted. The rejection itself is already durable (RecordRejection);
	// this is purely visibility.
	RejectedOperationAlerts []RejectedOperationAlert

	// LargeTransactionAlerts flags postings well above the period's typical
	// size — a lightweight anomaly signal, not a hard rule (small
	// businesses have legitimately large one-off transactions).
	LargeTransactionAlerts []LargeTransactionAlert

	// NegativeCashBalance / NegativeBankBalance flag a balance-sheet state
	// that should never happen in a cash business and needs investigation.
	NegativeCashBalance bool
	NegativeBankBalance bool

	// LowStockAlerts lists stocked products at or below their reorder point,
	// by branch.
	LowStockAlerts []LowStockAlert

	// OverdueDebtAlerts lists customers with a positive outstanding balance
	// (posted credit sales minus posted receipts) who also have at least one
	// credit sale past its due date. This approximates which invoice is
	// unpaid rather than tracking per-invoice settlement (receipts aren't
	// allocated to specific sales in this release) — the balance is exact,
	// the "oldest overdue" date is a reasonable but not precise proxy for
	// which sale(s) it belongs to.
	OverdueDebtAlerts []OverdueDebtAlert
}

// LowStockAlert is one product at or below its reorder point at a branch.
type LowStockAlert struct {
	BranchName   string
	ProductName  string
	Quantity     decimal.Decimal
	ReorderPoint decimal.Decimal
	Unit         string
}

// OverdueDebtAlert is one customer with an outstanding balance and at least
// one credit sale past due.
type OverdueDebtAlert struct {
	CustomerName      string
	OutstandingAmount decimal.Decimal
	OldestDueDate     time.Time
}

// RejectedOperationAlert is one recently rejected command.
type RejectedOperationAlert struct {
	BranchName  string
	CommandType string
	ErrorCode   string
	OccurredAt  time.Time
}

// LargeTransactionAlert is one transaction well above the period's typical
// size.
type LargeTransactionAlert struct {
	BranchName   string
	DocumentType string
	Amount       decimal.Decimal
	Date         time.Time
}

// BranchPeriodTotal is one branch's period performance for the comparison
// bars.
type BranchPeriodTotal struct {
	BranchID   uuid.UUID
	BranchName string
	Revenue    decimal.Decimal
	Expenses   decimal.Decimal
	NetProfit  decimal.Decimal
}

// ExpenseCategory is one expense account's period total.
type ExpenseCategory struct {
	AccountName string
	Amount      decimal.Decimal
}

// DailyPoint is one day's revenue, for the trend line.
type DailyPoint struct {
	Date    time.Time
	Revenue decimal.Decimal
}

// CashDifferenceAlert is one branch/day/currency close whose counted cash
// didn't match expected cash and hasn't been resolved by approval yet.
type CashDifferenceAlert struct {
	BranchName  string
	CloseDate   time.Time
	Currency    string
	Discrepancy decimal.Decimal
}

// Store is the read-only aggregation the dashboard needs from Postgres.
// Defined here (owned by the domain package) so the handler/service stay
// testable against a fake independent of the database, matching the pattern
// in internal/accounting.
type Store interface {
	// PeriodMovements sums posted journal-line reporting_amount by account
	// code prefix within [from, to], scoped to companyID and (if non-nil)
	// branchID, additionally restricted to allowedBranches when the caller's
	// role is branch-limited (empty allowedBranches means unrestricted).
	PeriodMovements(ctx context.Context, companyID uuid.UUID, branchID *uuid.UUID, allowedBranches []uuid.UUID, from, to time.Time) (PeriodTotals, error)

	// BalancesAsOf returns cumulative account balances as of `to`, same
	// scoping rules as PeriodMovements.
	BalancesAsOf(ctx context.Context, companyID uuid.UUID, branchID *uuid.UUID, allowedBranches []uuid.UUID, to time.Time) (BalanceTotals, error)

	// ReportingCurrency returns the company's configured reporting currency.
	ReportingCurrency(ctx context.Context, companyID uuid.UUID) (string, error)

	// BranchFreshness lists branches in scope with their last sync_changes
	// timestamp.
	BranchFreshness(ctx context.Context, companyID uuid.UUID, branchID *uuid.UUID, allowedBranches []uuid.UUID) ([]BranchFreshness, error)

	// BranchBreakdown returns per-branch revenue/expenses for [from, to],
	// scoped to allowedBranches (empty = unrestricted). Called only when the
	// caller asked for an all-branches view.
	BranchBreakdown(ctx context.Context, companyID uuid.UUID, allowedBranches []uuid.UUID, from, to time.Time) ([]BranchPeriodTotal, error)

	// ExpenseBreakdown returns operating-expense totals by account for
	// [from, to], same scoping as PeriodMovements.
	ExpenseBreakdown(ctx context.Context, companyID uuid.UUID, branchID *uuid.UUID, allowedBranches []uuid.UUID, from, to time.Time) ([]ExpenseCategory, error)

	// DailyRevenueTrend returns one revenue total per calendar day in
	// [from, to], same scoping as PeriodMovements.
	DailyRevenueTrend(ctx context.Context, companyID uuid.UUID, branchID *uuid.UUID, allowedBranches []uuid.UUID, from, to time.Time) ([]DailyPoint, error)

	// PendingApprovalsCount counts transactions currently awaiting_approval
	// in scope.
	PendingApprovalsCount(ctx context.Context, companyID uuid.UUID, branchID *uuid.UUID, allowedBranches []uuid.UUID) (int, error)

	// CashDifferenceAlerts lists submitted (not yet approved) daily closes
	// with a nonzero discrepancy, in scope, most recent first.
	CashDifferenceAlerts(ctx context.Context, companyID uuid.UUID, branchID *uuid.UUID, allowedBranches []uuid.UUID) ([]CashDifferenceAlert, error)

	// RejectedOperationAlerts lists recently rejected commands in scope,
	// most recent first.
	RejectedOperationAlerts(ctx context.Context, companyID uuid.UUID, branchID *uuid.UUID, allowedBranches []uuid.UUID) ([]RejectedOperationAlert, error)

	// LargeTransactionAlerts lists transactions in [from, to] well above the
	// period's typical posted-transaction size, in scope.
	LargeTransactionAlerts(ctx context.Context, companyID uuid.UUID, branchID *uuid.UUID, allowedBranches []uuid.UUID, from, to time.Time) ([]LargeTransactionAlert, error)

	// LowStockAlerts lists stocked products at or below their reorder point,
	// in scope.
	LowStockAlerts(ctx context.Context, companyID uuid.UUID, branchID *uuid.UUID, allowedBranches []uuid.UUID) ([]LowStockAlert, error)

	// OverdueDebtAlerts lists customers with an outstanding balance and a
	// credit sale past due, in scope.
	OverdueDebtAlerts(ctx context.Context, companyID uuid.UUID, branchID *uuid.UUID, allowedBranches []uuid.UUID) ([]OverdueDebtAlert, error)

	// TrialBalanceLines returns every active account's net debit/credit
	// balance from posted journal lines through `asOf`, same scoping as
	// BalancesAsOf but across the whole chart of accounts rather than the
	// four balance-sheet accounts the dashboard shows.
	TrialBalanceLines(ctx context.Context, companyID uuid.UUID, branchID *uuid.UUID, allowedBranches []uuid.UUID, asOf time.Time) ([]TrialBalanceLine, error)

	// AgeingSourceRows lists every customer with a positive outstanding
	// balance and at least one due credit sale through `asOf`, same shape
	// and the same approximation as OverdueDebtAlerts (see that method's doc
	// comment) but unfiltered by whether the oldest due date has actually
	// passed yet — Service.Ageing buckets that itself.
	AgeingSourceRows(ctx context.Context, companyID uuid.UUID, branchID *uuid.UUID, allowedBranches []uuid.UUID, asOf time.Time) ([]OverdueDebtAlert, error)
}

// PeriodTotals and BalanceTotals are the raw aggregates the store returns;
// Service derives the named Result fields (gross/net profit, etc.) from
// them so the arithmetic lives in one reviewable place rather than in SQL.
type PeriodTotals struct {
	Revenue           decimal.Decimal
	CostOfSales       decimal.Decimal
	OperatingExpenses decimal.Decimal
}

type BalanceTotals struct {
	Cash        decimal.Decimal
	Bank        decimal.Decimal
	Receivables decimal.Decimal
	Payables    decimal.Decimal
}

// StaleAfter is how long since a branch's last sync before the dashboard
// flags it. Proposed, not yet validated against pilot measurements
// (architecture: "Performance targets ... remain proposed until pilot
// measurements establish an operating envelope").
const StaleAfter = 24 * time.Hour

type Service struct {
	store Store
}

func NewService(store Store) *Service {
	return &Service{store: store}
}

// Dashboard computes the manager overview for the caller's scope. A
// branch-restricted role requesting a specific branch_id outside their
// scope is rejected; requesting no branch_id aggregates only the branches
// they're allowed to see (never silently widened to the whole company).
func (s *Service) Dashboard(ctx context.Context, scope tenancy.Scope, q Query) (Result, error) {
	if q.BranchID != nil {
		if err := scope.RequireBranch(*q.BranchID); err != nil {
			return Result{}, fmt.Errorf("reporting: %w", err)
		}
	}

	currency, err := s.store.ReportingCurrency(ctx, scope.CompanyID)
	if err != nil {
		return Result{}, fmt.Errorf("reporting: get reporting currency: %w", err)
	}

	periods, err := s.store.PeriodMovements(ctx, scope.CompanyID, q.BranchID, scope.BranchScope, q.From, q.To)
	if err != nil {
		return Result{}, fmt.Errorf("reporting: period movements: %w", err)
	}

	balances, err := s.store.BalancesAsOf(ctx, scope.CompanyID, q.BranchID, scope.BranchScope, q.To)
	if err != nil {
		return Result{}, fmt.Errorf("reporting: balances: %w", err)
	}

	branches, err := s.store.BranchFreshness(ctx, scope.CompanyID, q.BranchID, scope.BranchScope)
	if err != nil {
		return Result{}, fmt.Errorf("reporting: branch freshness: %w", err)
	}
	now := time.Now().UTC()
	for i := range branches {
		if branches[i].LastSyncAt == nil || now.Sub(*branches[i].LastSyncAt) > StaleAfter {
			branches[i].Stale = true
		}
	}

	grossProfit := periods.Revenue.Sub(periods.CostOfSales)
	netProfit := grossProfit.Sub(periods.OperatingExpenses)

	var branchBreakdown []BranchPeriodTotal
	if q.BranchID == nil {
		branchBreakdown, err = s.store.BranchBreakdown(ctx, scope.CompanyID, scope.BranchScope, q.From, q.To)
		if err != nil {
			return Result{}, fmt.Errorf("reporting: branch breakdown: %w", err)
		}
		for i := range branchBreakdown {
			branchBreakdown[i].NetProfit = branchBreakdown[i].Revenue.Sub(branchBreakdown[i].Expenses)
		}
	}

	expenseBreakdown, err := s.store.ExpenseBreakdown(ctx, scope.CompanyID, q.BranchID, scope.BranchScope, q.From, q.To)
	if err != nil {
		return Result{}, fmt.Errorf("reporting: expense breakdown: %w", err)
	}

	dailyTrend, err := s.store.DailyRevenueTrend(ctx, scope.CompanyID, q.BranchID, scope.BranchScope, q.From, q.To)
	if err != nil {
		return Result{}, fmt.Errorf("reporting: daily trend: %w", err)
	}

	pendingApprovals, err := s.store.PendingApprovalsCount(ctx, scope.CompanyID, q.BranchID, scope.BranchScope)
	if err != nil {
		return Result{}, fmt.Errorf("reporting: pending approvals: %w", err)
	}

	cashDifferences, err := s.store.CashDifferenceAlerts(ctx, scope.CompanyID, q.BranchID, scope.BranchScope)
	if err != nil {
		return Result{}, fmt.Errorf("reporting: cash difference alerts: %w", err)
	}

	rejectedOps, err := s.store.RejectedOperationAlerts(ctx, scope.CompanyID, q.BranchID, scope.BranchScope)
	if err != nil {
		return Result{}, fmt.Errorf("reporting: rejected operation alerts: %w", err)
	}

	largeTxns, err := s.store.LargeTransactionAlerts(ctx, scope.CompanyID, q.BranchID, scope.BranchScope, q.From, q.To)
	if err != nil {
		return Result{}, fmt.Errorf("reporting: large transaction alerts: %w", err)
	}

	lowStock, err := s.store.LowStockAlerts(ctx, scope.CompanyID, q.BranchID, scope.BranchScope)
	if err != nil {
		return Result{}, fmt.Errorf("reporting: low stock alerts: %w", err)
	}

	overdueDebts, err := s.store.OverdueDebtAlerts(ctx, scope.CompanyID, q.BranchID, scope.BranchScope)
	if err != nil {
		return Result{}, fmt.Errorf("reporting: overdue debt alerts: %w", err)
	}

	return Result{
		CompanyID:         scope.CompanyID,
		ScopeBranchID:     q.BranchID,
		From:              q.From,
		To:                q.To,
		ReportingCurrency: currency,
		GeneratedAt:       now,

		Revenue:           periods.Revenue,
		CostOfSales:       periods.CostOfSales,
		GrossProfit:       grossProfit,
		OperatingExpenses: periods.OperatingExpenses,
		NetProfit:         netProfit,

		CashBalance: balances.Cash,
		BankBalance: balances.Bank,
		Receivables: balances.Receivables,
		Payables:    balances.Payables,

		Branches: branches,

		BranchBreakdown:       branchBreakdown,
		ExpenseBreakdown:      expenseBreakdown,
		DailyTrend:            dailyTrend,
		PendingApprovalsCount: pendingApprovals,
		CashDifferenceAlerts:  cashDifferences,

		RejectedOperationAlerts: rejectedOps,
		LargeTransactionAlerts:  largeTxns,
		NegativeCashBalance:     balances.Cash.IsNegative(),
		NegativeBankBalance:     balances.Bank.IsNegative(),
		LowStockAlerts:          lowStock,
		OverdueDebtAlerts:       overdueDebts,
	}, nil
}
