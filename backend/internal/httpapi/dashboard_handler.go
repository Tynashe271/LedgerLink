package httpapi

import (
	"log"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/ledgerlink/branchledger/backend/internal/accounting"
	"github.com/ledgerlink/branchledger/backend/internal/reporting"
)

// dashboardResponse is the wire shape for GET /api/v1/dashboard. Decimal
// fields marshal as JSON strings (shopspring/decimal's default), preserving
// exact precision the way every other money field in this API does.
type dashboardResponse struct {
	CompanyID         uuid.UUID               `json:"company_id"`
	BranchID          *uuid.UUID              `json:"branch_id,omitempty"`
	From              string                  `json:"from"`
	To                string                  `json:"to"`
	ReportingCurrency string                  `json:"reporting_currency"`
	GeneratedAt       time.Time               `json:"generated_at"`
	PostingBasis      string                  `json:"posting_basis"`

	Revenue           string `json:"revenue"`
	CostOfSales       string `json:"cost_of_sales"`
	GrossProfit       string `json:"gross_profit"`
	OperatingExpenses string `json:"operating_expenses"`
	NetProfit         string `json:"net_profit"`

	CashBalance string `json:"cash_balance"`
	BankBalance string `json:"bank_balance"`
	Receivables string `json:"receivables"`
	Payables    string `json:"payables"`

	Branches []dashboardBranch `json:"branches"`

	BranchBreakdown       []dashboardBranchTotal `json:"branch_breakdown,omitempty"`
	ExpenseBreakdown      []dashboardExpense      `json:"expense_breakdown"`
	DailyTrend            []dashboardTrendPoint   `json:"daily_trend"`
	PendingApprovalsCount int                     `json:"pending_approvals_count"`
	CashDifferenceAlerts  []dashboardCashAlert    `json:"cash_difference_alerts"`

	RejectedOperationAlerts []dashboardRejectedOp    `json:"rejected_operation_alerts"`
	LargeTransactionAlerts  []dashboardLargeTxn      `json:"large_transaction_alerts"`
	NegativeCashBalance     bool                     `json:"negative_cash_balance"`
	NegativeBankBalance     bool                     `json:"negative_bank_balance"`
	LowStockAlerts          []dashboardLowStock      `json:"low_stock_alerts"`
	OverdueDebtAlerts       []dashboardOverdueDebt   `json:"overdue_debt_alerts"`
}

type dashboardLowStock struct {
	BranchName   string `json:"branch_name"`
	ProductName  string `json:"product_name"`
	Quantity     string `json:"quantity"`
	ReorderPoint string `json:"reorder_point"`
	Unit         string `json:"unit"`
}

type dashboardOverdueDebt struct {
	CustomerName      string `json:"customer_name"`
	OutstandingAmount string `json:"outstanding_amount"`
	OldestDueDate     string `json:"oldest_due_date"`
}

type dashboardRejectedOp struct {
	BranchName  string    `json:"branch_name"`
	CommandType string    `json:"command_type"`
	ErrorCode   string    `json:"error_code"`
	OccurredAt  time.Time `json:"occurred_at"`
}

type dashboardLargeTxn struct {
	BranchName   string `json:"branch_name"`
	DocumentType string `json:"document_type"`
	Amount       string `json:"amount"`
	Date         string `json:"date"`
}

type dashboardBranch struct {
	BranchID   uuid.UUID  `json:"branch_id"`
	Name       string     `json:"name"`
	LastSyncAt *time.Time `json:"last_sync_at"`
	Stale      bool       `json:"stale"`
}

type dashboardBranchTotal struct {
	BranchID  uuid.UUID `json:"branch_id"`
	Name      string    `json:"name"`
	Revenue   string    `json:"revenue"`
	Expenses  string    `json:"expenses"`
	NetProfit string    `json:"net_profit"`
}

type dashboardExpense struct {
	AccountName string `json:"account_name"`
	Amount      string `json:"amount"`
}

type dashboardTrendPoint struct {
	Date    string `json:"date"`
	Revenue string `json:"revenue"`
}

type dashboardCashAlert struct {
	BranchName  string `json:"branch_name"`
	CloseDate   string `json:"close_date"`
	Currency    string `json:"currency"`
	Discrepancy string `json:"discrepancy"`
}

// handleDashboard serves GET /api/v1/dashboard: "Branch/date/currency scope
// and freshness metadata" (architecture API contract table). Query params:
// branch_id (optional UUID), from, to (YYYY-MM-DD, both optional — default
// to the caller's current day in UTC). Figures are always from posted
// journals only; see reporting.Result's doc comment for the period-vs-balance
// distinction.
func handleDashboard(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scope, ok := requireScope(w, r)
		if !ok {
			return
		}

		q := r.URL.Query()

		var branchID *uuid.UUID
		if raw := q.Get("branch_id"); raw != "" {
			id, err := uuid.Parse(raw)
			if err != nil {
				writeError(w, http.StatusBadRequest, "invalid_branch_id")
				return
			}
			branchID = &id
		}

		today := time.Now().UTC().Truncate(24 * time.Hour)
		from := today
		to := today
		if raw := q.Get("from"); raw != "" {
			parsed, err := time.Parse("2006-01-02", raw)
			if err != nil {
				writeError(w, http.StatusBadRequest, "invalid_from_date")
				return
			}
			from = parsed
		}
		if raw := q.Get("to"); raw != "" {
			parsed, err := time.Parse("2006-01-02", raw)
			if err != nil {
				writeError(w, http.StatusBadRequest, "invalid_to_date")
				return
			}
			to = parsed
		}
		// Make `to` inclusive of the whole day: a posted_at of 23:59 on `to`
		// must count, not just exactly midnight.
		to = to.Add(24*time.Hour - time.Nanosecond)
		if to.Before(from) {
			writeError(w, http.StatusBadRequest, "invalid_date_range")
			return
		}

		result, err := deps.ReportingSvc.Dashboard(r.Context(), scope, reporting.Query{
			BranchID: branchID, From: from, To: to,
		})
		if err == accounting.ErrOutOfScope {
			writeError(w, http.StatusForbidden, "out_of_scope")
			return
		}
		if err != nil {
			log.Printf("request_id=%s dashboard error: %v", scope.RequestID, err)
			writeError(w, http.StatusInternalServerError, "internal_error")
			return
		}

		branches := make([]dashboardBranch, 0, len(result.Branches))
		for _, b := range result.Branches {
			branches = append(branches, dashboardBranch{
				BranchID: b.BranchID, Name: b.BranchName, LastSyncAt: b.LastSyncAt, Stale: b.Stale,
			})
		}

		var branchBreakdown []dashboardBranchTotal
		for _, b := range result.BranchBreakdown {
			branchBreakdown = append(branchBreakdown, dashboardBranchTotal{
				BranchID: b.BranchID, Name: b.BranchName, Revenue: b.Revenue.StringFixed(2),
				Expenses: b.Expenses.StringFixed(2), NetProfit: b.NetProfit.StringFixed(2),
			})
		}

		expenseBreakdown := make([]dashboardExpense, 0, len(result.ExpenseBreakdown))
		for _, e := range result.ExpenseBreakdown {
			expenseBreakdown = append(expenseBreakdown, dashboardExpense{AccountName: e.AccountName, Amount: e.Amount.StringFixed(2)})
		}

		dailyTrend := make([]dashboardTrendPoint, 0, len(result.DailyTrend))
		for _, p := range result.DailyTrend {
			dailyTrend = append(dailyTrend, dashboardTrendPoint{Date: p.Date.Format("2006-01-02"), Revenue: p.Revenue.StringFixed(2)})
		}

		cashAlerts := make([]dashboardCashAlert, 0, len(result.CashDifferenceAlerts))
		for _, a := range result.CashDifferenceAlerts {
			cashAlerts = append(cashAlerts, dashboardCashAlert{
				BranchName: a.BranchName, CloseDate: a.CloseDate.Format("2006-01-02"),
				Currency: a.Currency, Discrepancy: a.Discrepancy.StringFixed(2),
			})
		}

		rejectedOps := make([]dashboardRejectedOp, 0, len(result.RejectedOperationAlerts))
		for _, r := range result.RejectedOperationAlerts {
			rejectedOps = append(rejectedOps, dashboardRejectedOp{
				BranchName: r.BranchName, CommandType: r.CommandType, ErrorCode: r.ErrorCode, OccurredAt: r.OccurredAt,
			})
		}

		largeTxns := make([]dashboardLargeTxn, 0, len(result.LargeTransactionAlerts))
		for _, l := range result.LargeTransactionAlerts {
			largeTxns = append(largeTxns, dashboardLargeTxn{
				BranchName: l.BranchName, DocumentType: l.DocumentType, Amount: l.Amount.StringFixed(2),
				Date: l.Date.Format("2006-01-02"),
			})
		}

		lowStock := make([]dashboardLowStock, 0, len(result.LowStockAlerts))
		for _, a := range result.LowStockAlerts {
			lowStock = append(lowStock, dashboardLowStock{
				BranchName: a.BranchName, ProductName: a.ProductName, Quantity: a.Quantity.StringFixed(2),
				ReorderPoint: a.ReorderPoint.StringFixed(2), Unit: a.Unit,
			})
		}

		overdueDebts := make([]dashboardOverdueDebt, 0, len(result.OverdueDebtAlerts))
		for _, a := range result.OverdueDebtAlerts {
			overdueDebts = append(overdueDebts, dashboardOverdueDebt{
				CustomerName: a.CustomerName, OutstandingAmount: a.OutstandingAmount.StringFixed(2),
				OldestDueDate: a.OldestDueDate.Format("2006-01-02"),
			})
		}

		writeJSON(w, http.StatusOK, dashboardResponse{
			CompanyID: result.CompanyID, BranchID: result.ScopeBranchID,
			From: result.From.Format("2006-01-02"), To: result.To.Format("2006-01-02"),
			ReportingCurrency: result.ReportingCurrency, GeneratedAt: result.GeneratedAt,
			PostingBasis: "posted",

			Revenue: result.Revenue.StringFixed(2), CostOfSales: result.CostOfSales.StringFixed(2),
			GrossProfit: result.GrossProfit.StringFixed(2), OperatingExpenses: result.OperatingExpenses.StringFixed(2),
			NetProfit: result.NetProfit.StringFixed(2),

			CashBalance: result.CashBalance.StringFixed(2), BankBalance: result.BankBalance.StringFixed(2),
			Receivables: result.Receivables.StringFixed(2), Payables: result.Payables.StringFixed(2),

			Branches: branches,

			BranchBreakdown: branchBreakdown, ExpenseBreakdown: expenseBreakdown, DailyTrend: dailyTrend,
			PendingApprovalsCount: result.PendingApprovalsCount, CashDifferenceAlerts: cashAlerts,

			RejectedOperationAlerts: rejectedOps, LargeTransactionAlerts: largeTxns,
			NegativeCashBalance: result.NegativeCashBalance, NegativeBankBalance: result.NegativeBankBalance,
			LowStockAlerts: lowStock, OverdueDebtAlerts: overdueDebts,
		})
	}
}
