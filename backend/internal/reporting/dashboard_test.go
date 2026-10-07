package reporting

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/ledgerlink/branchledger/backend/internal/tenancy"
)

func d(s string) decimal.Decimal {
	v, err := decimal.NewFromString(s)
	if err != nil {
		panic(err)
	}
	return v
}

// fakeStore lets the arithmetic and scope-checking in Service.Dashboard be
// tested without a database, matching the pattern used for
// accounting.Service's tests against a fake Repository.
type fakeStore struct {
	periods  PeriodTotals
	balances BalanceTotals
	currency string

	branchBreakdown      []BranchPeriodTotal
	expenseBreakdown     []ExpenseCategory
	dailyTrend           []DailyPoint
	pendingApprovals     int
	cashDifferenceAlerts []CashDifferenceAlert
	rejectedOperations   []RejectedOperationAlert
	largeTransactions    []LargeTransactionAlert
	lowStock             []LowStockAlert
	overdueDebts         []OverdueDebtAlert
}

func (f *fakeStore) PeriodMovements(ctx context.Context, companyID uuid.UUID, branchID *uuid.UUID, allowed []uuid.UUID, from, to time.Time) (PeriodTotals, error) {
	return f.periods, nil
}
func (f *fakeStore) BalancesAsOf(ctx context.Context, companyID uuid.UUID, branchID *uuid.UUID, allowed []uuid.UUID, to time.Time) (BalanceTotals, error) {
	return f.balances, nil
}
func (f *fakeStore) ReportingCurrency(ctx context.Context, companyID uuid.UUID) (string, error) {
	return f.currency, nil
}
func (f *fakeStore) BranchFreshness(ctx context.Context, companyID uuid.UUID, branchID *uuid.UUID, allowed []uuid.UUID) ([]BranchFreshness, error) {
	recent := time.Now().UTC().Add(-1 * time.Hour)
	stale := time.Now().UTC().Add(-48 * time.Hour)
	return []BranchFreshness{
		{BranchID: uuid.New(), BranchName: "Main", LastSyncAt: &recent},
		{BranchID: uuid.New(), BranchName: "Stale Branch", LastSyncAt: &stale},
		{BranchID: uuid.New(), BranchName: "Never Synced", LastSyncAt: nil},
	}, nil
}
func (f *fakeStore) BranchBreakdown(ctx context.Context, companyID uuid.UUID, allowed []uuid.UUID, from, to time.Time) ([]BranchPeriodTotal, error) {
	return f.branchBreakdown, nil
}
func (f *fakeStore) ExpenseBreakdown(ctx context.Context, companyID uuid.UUID, branchID *uuid.UUID, allowed []uuid.UUID, from, to time.Time) ([]ExpenseCategory, error) {
	return f.expenseBreakdown, nil
}
func (f *fakeStore) DailyRevenueTrend(ctx context.Context, companyID uuid.UUID, branchID *uuid.UUID, allowed []uuid.UUID, from, to time.Time) ([]DailyPoint, error) {
	return f.dailyTrend, nil
}
func (f *fakeStore) PendingApprovalsCount(ctx context.Context, companyID uuid.UUID, branchID *uuid.UUID, allowed []uuid.UUID) (int, error) {
	return f.pendingApprovals, nil
}
func (f *fakeStore) CashDifferenceAlerts(ctx context.Context, companyID uuid.UUID, branchID *uuid.UUID, allowed []uuid.UUID) ([]CashDifferenceAlert, error) {
	return f.cashDifferenceAlerts, nil
}
func (f *fakeStore) RejectedOperationAlerts(ctx context.Context, companyID uuid.UUID, branchID *uuid.UUID, allowed []uuid.UUID) ([]RejectedOperationAlert, error) {
	return f.rejectedOperations, nil
}
func (f *fakeStore) LargeTransactionAlerts(ctx context.Context, companyID uuid.UUID, branchID *uuid.UUID, allowed []uuid.UUID, from, to time.Time) ([]LargeTransactionAlert, error) {
	return f.largeTransactions, nil
}
func (f *fakeStore) LowStockAlerts(ctx context.Context, companyID uuid.UUID, branchID *uuid.UUID, allowed []uuid.UUID) ([]LowStockAlert, error) {
	return f.lowStock, nil
}
func (f *fakeStore) OverdueDebtAlerts(ctx context.Context, companyID uuid.UUID, branchID *uuid.UUID, allowed []uuid.UUID) ([]OverdueDebtAlert, error) {
	return f.overdueDebts, nil
}

// TestGrossAndNetProfitArithmetic matches the architecture's definitions
// (section 5.13): gross profit = revenue - cost of sales; net profit =
// gross profit - operating expenses.
func TestGrossAndNetProfitArithmetic(t *testing.T) {
	store := &fakeStore{
		periods: PeriodTotals{
			Revenue:           d("200"),
			CostOfSales:       d("120"),
			OperatingExpenses: d("30"),
		},
		currency: "USD",
	}
	svc := NewService(store)
	scope := tenancy.Scope{CompanyID: uuid.New(), Role: tenancy.RoleOwner}

	result, err := svc.Dashboard(context.Background(), scope, Query{From: time.Now(), To: time.Now()})
	if err != nil {
		t.Fatalf("Dashboard: %v", err)
	}

	if !result.GrossProfit.Equal(d("80")) {
		t.Errorf("gross profit: want 80, got %s", result.GrossProfit)
	}
	if !result.NetProfit.Equal(d("50")) {
		t.Errorf("net profit: want 50, got %s", result.NetProfit)
	}
}

// TestDashboardRejectsOutOfScopeBranch matches AT04-style enforcement: a
// branch-restricted caller requesting a branch_id outside their scope must
// be rejected, not silently widened or ignored.
func TestDashboardRejectsOutOfScopeBranch(t *testing.T) {
	store := &fakeStore{currency: "USD"}
	svc := NewService(store)

	allowedBranch := uuid.New()
	otherBranch := uuid.New()
	scope := tenancy.Scope{
		CompanyID: uuid.New(), Role: tenancy.RoleBranchManager,
		BranchScope: []uuid.UUID{allowedBranch},
	}

	_, err := svc.Dashboard(context.Background(), scope, Query{BranchID: &otherBranch, From: time.Now(), To: time.Now()})
	if err == nil {
		t.Fatal("expected an error requesting a branch outside the caller's scope, got nil")
	}
}

// TestStaleBranchFlagging matches "A stale branch gets a visible badge"
// (System Documentation section 5.10): never synced or synced more than
// StaleAfter ago must both be flagged.
func TestStaleBranchFlagging(t *testing.T) {
	store := &fakeStore{currency: "USD"}
	svc := NewService(store)
	scope := tenancy.Scope{CompanyID: uuid.New(), Role: tenancy.RoleOwner}

	result, err := svc.Dashboard(context.Background(), scope, Query{From: time.Now(), To: time.Now()})
	if err != nil {
		t.Fatalf("Dashboard: %v", err)
	}

	want := map[string]bool{"Main": false, "Stale Branch": true, "Never Synced": true}
	for _, b := range result.Branches {
		if b.Stale != want[b.BranchName] {
			t.Errorf("branch %q: want stale=%v, got %v", b.BranchName, want[b.BranchName], b.Stale)
		}
	}
}

// TestBranchBreakdownOnlyPopulatedForAllBranchesView matches "Branch bars
// show performance comparisons" (User Manual) — a single-branch view has
// nothing to compare against itself, so the field stays empty rather than a
// one-item list.
func TestBranchBreakdownOnlyPopulatedForAllBranchesView(t *testing.T) {
	branchID := uuid.New()
	store := &fakeStore{
		currency: "USD",
		branchBreakdown: []BranchPeriodTotal{
			{BranchID: branchID, BranchName: "Main", Revenue: d("100"), Expenses: d("30")},
		},
	}
	svc := NewService(store)
	ownerScope := tenancy.Scope{CompanyID: uuid.New(), Role: tenancy.RoleOwner}

	allBranches, err := svc.Dashboard(context.Background(), ownerScope, Query{From: time.Now(), To: time.Now()})
	if err != nil {
		t.Fatalf("Dashboard (all branches): %v", err)
	}
	if len(allBranches.BranchBreakdown) != 1 || !allBranches.BranchBreakdown[0].NetProfit.Equal(d("70")) {
		t.Fatalf("expected one branch with net profit 70, got %+v", allBranches.BranchBreakdown)
	}

	oneBranch, err := svc.Dashboard(context.Background(), ownerScope, Query{BranchID: &branchID, From: time.Now(), To: time.Now()})
	if err != nil {
		t.Fatalf("Dashboard (single branch): %v", err)
	}
	if len(oneBranch.BranchBreakdown) != 0 {
		t.Fatalf("expected no branch breakdown for a single-branch view, got %+v", oneBranch.BranchBreakdown)
	}
}

// TestAlertsPassThroughFromStore confirms pending approvals and cash
// difference alerts reach the dashboard result unmodified.
func TestAlertsPassThroughFromStore(t *testing.T) {
	store := &fakeStore{
		currency:         "USD",
		pendingApprovals: 3,
		cashDifferenceAlerts: []CashDifferenceAlert{
			{BranchName: "Main", Discrepancy: d("-5")},
		},
	}
	svc := NewService(store)
	scope := tenancy.Scope{CompanyID: uuid.New(), Role: tenancy.RoleOwner}

	result, err := svc.Dashboard(context.Background(), scope, Query{From: time.Now(), To: time.Now()})
	if err != nil {
		t.Fatalf("Dashboard: %v", err)
	}
	if result.PendingApprovalsCount != 3 {
		t.Errorf("pending approvals: want 3, got %d", result.PendingApprovalsCount)
	}
	if len(result.CashDifferenceAlerts) != 1 || !result.CashDifferenceAlerts[0].Discrepancy.Equal(d("-5")) {
		t.Errorf("cash difference alerts: got %+v", result.CashDifferenceAlerts)
	}
}

// TestNegativeBalanceFlagged matches the manager overview's "needs
// attention" panel: a negative cash or bank balance should never happen in
// a cash business and must be flagged, not silently displayed as a number.
func TestNegativeBalanceFlagged(t *testing.T) {
	store := &fakeStore{
		currency: "USD",
		balances: BalanceTotals{Cash: d("-10"), Bank: d("500")},
	}
	svc := NewService(store)
	scope := tenancy.Scope{CompanyID: uuid.New(), Role: tenancy.RoleOwner}

	result, err := svc.Dashboard(context.Background(), scope, Query{From: time.Now(), To: time.Now()})
	if err != nil {
		t.Fatalf("Dashboard: %v", err)
	}
	if !result.NegativeCashBalance {
		t.Error("expected NegativeCashBalance = true for cash balance -10")
	}
	if result.NegativeBankBalance {
		t.Error("expected NegativeBankBalance = false for bank balance 500")
	}
}

// TestLowStockAndOverdueDebtAlertsPassThrough confirms the two
// prerequisite-feature alerts (low stock needs a real product catalog;
// overdue debts need a customer + due date) reach the dashboard result.
func TestLowStockAndOverdueDebtAlertsPassThrough(t *testing.T) {
	dueDate := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	store := &fakeStore{
		currency: "USD",
		lowStock: []LowStockAlert{
			{BranchName: "Main", ProductName: "Mealie Meal 10kg", Quantity: d("2"), ReorderPoint: d("5"), Unit: "bag"},
		},
		overdueDebts: []OverdueDebtAlert{
			{CustomerName: "Chipo Traders", OutstandingAmount: d("340"), OldestDueDate: dueDate},
		},
	}
	svc := NewService(store)
	scope := tenancy.Scope{CompanyID: uuid.New(), Role: tenancy.RoleOwner}

	result, err := svc.Dashboard(context.Background(), scope, Query{From: time.Now(), To: time.Now()})
	if err != nil {
		t.Fatalf("Dashboard: %v", err)
	}
	if len(result.LowStockAlerts) != 1 || result.LowStockAlerts[0].ProductName != "Mealie Meal 10kg" {
		t.Errorf("low stock alerts: got %+v", result.LowStockAlerts)
	}
	if len(result.OverdueDebtAlerts) != 1 || !result.OverdueDebtAlerts[0].OutstandingAmount.Equal(d("340")) {
		t.Errorf("overdue debt alerts: got %+v", result.OverdueDebtAlerts)
	}
}
