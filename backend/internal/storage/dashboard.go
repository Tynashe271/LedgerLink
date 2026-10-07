package storage

import (
	"context"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/ledgerlink/branchledger/backend/internal/reporting"
)

// branchScopeClause builds the shared "which branches may this query see"
// SQL fragment: a specific branch (already permission-checked by
// reporting.Service before this is called), else every branch in
// allowedBranches, else (both nil/empty) unrestricted within the company —
// matching an owner/accountant's company-wide view.
func branchScopeClause(branchID *uuid.UUID, allowedBranches []uuid.UUID, argStart int) (clause string, args []any) {
	switch {
	case branchID != nil:
		return "AND j.branch_id = $" + strconv.Itoa(argStart), []any{*branchID}
	case len(allowedBranches) > 0:
		return "AND j.branch_id = ANY($" + strconv.Itoa(argStart) + ")", []any{allowedBranches}
	default:
		return "", nil
	}
}

func (db *DB) ReportingCurrency(ctx context.Context, companyID uuid.UUID) (string, error) {
	var currency string
	err := db.pool.QueryRow(ctx, `SELECT reporting_currency FROM companies WHERE id = $1`, companyID).Scan(&currency)
	return currency, err
}

// PeriodMovements sums posted journal lines by account code prefix:
// '4' = revenue (income accounts, net credit — this naturally nets sales
// returns since they debit the same income-type account, matching section
// 5.5's "Sales Returns" entry), '5' = cost of sales / stock loss (net
// debit), anything else expense-typed = operating expenses (net debit).
// The code-prefix convention matches the chart seeded in
// database/seeds/dev_seed.sql; a real company-setup flow would carry an
// explicit account-role column instead of inferring it from the code.
func (db *DB) PeriodMovements(ctx context.Context, companyID uuid.UUID, branchID *uuid.UUID, allowedBranches []uuid.UUID, from, to time.Time) (reporting.PeriodTotals, error) {
	clause, extraArgs := branchScopeClause(branchID, allowedBranches, 4)
	args := []any{companyID, from, to}
	args = append(args, extraArgs...)

	query := `
		SELECT
			COALESCE(SUM(CASE WHEN a.code LIKE '4%' AND jl.side = 'credit' THEN jl.reporting_amount
			                  WHEN a.code LIKE '4%' AND jl.side = 'debit'  THEN -jl.reporting_amount
			                  ELSE 0 END), 0) AS revenue,
			COALESCE(SUM(CASE WHEN a.code LIKE '5%' AND jl.side = 'debit'  THEN jl.reporting_amount
			                  WHEN a.code LIKE '5%' AND jl.side = 'credit' THEN -jl.reporting_amount
			                  ELSE 0 END), 0) AS cost_of_sales,
			COALESCE(SUM(CASE WHEN a.account_type = 'expense' AND a.code NOT LIKE '5%' AND jl.side = 'debit'  THEN jl.reporting_amount
			                  WHEN a.account_type = 'expense' AND a.code NOT LIKE '5%' AND jl.side = 'credit' THEN -jl.reporting_amount
			                  ELSE 0 END), 0) AS operating_expenses
		FROM journal_lines jl
		JOIN accounts a ON a.id = jl.account_id
		JOIN journals j ON j.id = jl.journal_id
		WHERE jl.company_id = $1 AND j.posted_at >= $2 AND j.posted_at <= $3
		` + clause

	var t reporting.PeriodTotals
	row := db.pool.QueryRow(ctx, query, args...)
	if err := row.Scan(&t.Revenue, &t.CostOfSales, &t.OperatingExpenses); err != nil {
		return reporting.PeriodTotals{}, err
	}
	return t, nil
}

// BalancesAsOf sums posted journal lines for the four balance-sheet accounts
// the dashboard shows, cumulative through `to`. Asset accounts (cash, bank,
// receivable) increase on debit; the payable (liability) account increases
// on credit.
func (db *DB) BalancesAsOf(ctx context.Context, companyID uuid.UUID, branchID *uuid.UUID, allowedBranches []uuid.UUID, to time.Time) (reporting.BalanceTotals, error) {
	clause, extraArgs := branchScopeClause(branchID, allowedBranches, 3)
	args := []any{companyID, to}
	args = append(args, extraArgs...)

	query := `
		SELECT
			COALESCE(SUM(CASE WHEN a.code = '1000' AND jl.side = 'debit'  THEN jl.reporting_amount
			                  WHEN a.code = '1000' AND jl.side = 'credit' THEN -jl.reporting_amount ELSE 0 END), 0) AS cash,
			COALESCE(SUM(CASE WHEN a.code = '1010' AND jl.side = 'debit'  THEN jl.reporting_amount
			                  WHEN a.code = '1010' AND jl.side = 'credit' THEN -jl.reporting_amount ELSE 0 END), 0) AS bank,
			COALESCE(SUM(CASE WHEN a.code = '1100' AND jl.side = 'debit'  THEN jl.reporting_amount
			                  WHEN a.code = '1100' AND jl.side = 'credit' THEN -jl.reporting_amount ELSE 0 END), 0) AS receivables,
			COALESCE(SUM(CASE WHEN a.code = '2000' AND jl.side = 'credit' THEN jl.reporting_amount
			                  WHEN a.code = '2000' AND jl.side = 'debit'  THEN -jl.reporting_amount ELSE 0 END), 0) AS payables
		FROM journal_lines jl
		JOIN accounts a ON a.id = jl.account_id
		JOIN journals j ON j.id = jl.journal_id
		WHERE jl.company_id = $1 AND j.posted_at <= $2
		` + clause

	var t reporting.BalanceTotals
	row := db.pool.QueryRow(ctx, query, args...)
	if err := row.Scan(&t.Cash, &t.Bank, &t.Receivables, &t.Payables); err != nil {
		return reporting.BalanceTotals{}, err
	}
	return t, nil
}

func (db *DB) BranchFreshness(ctx context.Context, companyID uuid.UUID, branchID *uuid.UUID, allowedBranches []uuid.UUID) ([]reporting.BranchFreshness, error) {
	baseQuery := `
		SELECT b.id, b.name, MAX(sc.occurred_at)
		FROM branches b
		LEFT JOIN sync_changes sc ON sc.branch_id = b.id
		WHERE b.company_id = $1`

	var rows pgx.Rows
	var err error
	switch {
	case branchID != nil:
		rows, err = db.pool.Query(ctx, baseQuery+` AND b.id = $2 GROUP BY b.id, b.name ORDER BY b.name`, companyID, *branchID)
	case len(allowedBranches) > 0:
		rows, err = db.pool.Query(ctx, baseQuery+` AND b.id = ANY($2) GROUP BY b.id, b.name ORDER BY b.name`, companyID, allowedBranches)
	default:
		rows, err = db.pool.Query(ctx, baseQuery+` GROUP BY b.id, b.name ORDER BY b.name`, companyID)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []reporting.BranchFreshness
	for rows.Next() {
		var f reporting.BranchFreshness
		var lastSync *time.Time
		if err := rows.Scan(&f.BranchID, &f.BranchName, &lastSync); err != nil {
			return nil, err
		}
		f.LastSyncAt = lastSync
		out = append(out, f)
	}
	return out, rows.Err()
}

// BranchBreakdown is PeriodMovements' revenue/expense case split out per
// branch instead of summed across the company, for the manager overview's
// branch comparison bars.
func (db *DB) BranchBreakdown(ctx context.Context, companyID uuid.UUID, allowedBranches []uuid.UUID, from, to time.Time) ([]reporting.BranchPeriodTotal, error) {
	clause, extraArgs := branchScopeClause(nil, allowedBranches, 4)
	args := []any{companyID, from, to}
	args = append(args, extraArgs...)

	rows, err := db.pool.Query(ctx, `
		SELECT b.id, b.name,
			COALESCE(SUM(CASE WHEN a.code LIKE '4%' AND jl.side = 'credit' THEN jl.reporting_amount
			                  WHEN a.code LIKE '4%' AND jl.side = 'debit'  THEN -jl.reporting_amount
			                  ELSE 0 END), 0) AS revenue,
			COALESCE(SUM(CASE WHEN a.account_type = 'expense' AND jl.side = 'debit'  THEN jl.reporting_amount
			                  WHEN a.account_type = 'expense' AND jl.side = 'credit' THEN -jl.reporting_amount
			                  ELSE 0 END), 0) AS expenses
		FROM branches b
		JOIN journals j ON j.branch_id = b.id AND j.posted_at >= $2 AND j.posted_at <= $3
		JOIN journal_lines jl ON jl.journal_id = j.id
		JOIN accounts a ON a.id = jl.account_id
		WHERE b.company_id = $1 `+clause+`
		GROUP BY b.id, b.name
		ORDER BY revenue DESC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	// NetProfit is deliberately left zero here: Service.Dashboard derives it
	// from Revenue/Expenses, the same "arithmetic lives in one reviewable
	// place" rule PeriodTotals/BalanceTotals already follow, rather than
	// duplicating the subtraction in SQL.
	var out []reporting.BranchPeriodTotal
	for rows.Next() {
		var t reporting.BranchPeriodTotal
		if err := rows.Scan(&t.BranchID, &t.BranchName, &t.Revenue, &t.Expenses); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// ExpenseBreakdown totals operating expenses (every expense-type account
// except cost-of-sales/5xxx, matching PeriodMovements' own operating_expenses
// definition) by account, largest first.
func (db *DB) ExpenseBreakdown(ctx context.Context, companyID uuid.UUID, branchID *uuid.UUID, allowedBranches []uuid.UUID, from, to time.Time) ([]reporting.ExpenseCategory, error) {
	clause, extraArgs := branchScopeClause(branchID, allowedBranches, 4)
	args := []any{companyID, from, to}
	args = append(args, extraArgs...)

	rows, err := db.pool.Query(ctx, `
		SELECT a.name,
			COALESCE(SUM(CASE WHEN jl.side = 'debit' THEN jl.reporting_amount ELSE -jl.reporting_amount END), 0) AS amount
		FROM journal_lines jl
		JOIN accounts a ON a.id = jl.account_id
		JOIN journals j ON j.id = jl.journal_id
		WHERE jl.company_id = $1 AND j.posted_at >= $2 AND j.posted_at <= $3
		  AND a.account_type = 'expense' AND a.code NOT LIKE '5%' `+clause+`
		GROUP BY a.name
		HAVING COALESCE(SUM(CASE WHEN jl.side = 'debit' THEN jl.reporting_amount ELSE -jl.reporting_amount END), 0) <> 0
		ORDER BY amount DESC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []reporting.ExpenseCategory
	for rows.Next() {
		var c reporting.ExpenseCategory
		if err := rows.Scan(&c.AccountName, &c.Amount); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// DailyRevenueTrend sums posted revenue by document_date (the transaction's
// own operating date, not server receipt time) across [from, to].
func (db *DB) DailyRevenueTrend(ctx context.Context, companyID uuid.UUID, branchID *uuid.UUID, allowedBranches []uuid.UUID, from, to time.Time) ([]reporting.DailyPoint, error) {
	clause, extraArgs := branchScopeClause(branchID, allowedBranches, 4)
	args := []any{companyID, from, to}
	args = append(args, extraArgs...)

	rows, err := db.pool.Query(ctx, `
		SELECT t.document_date,
			COALESCE(SUM(CASE WHEN a.code LIKE '4%' AND jl.side = 'credit' THEN jl.reporting_amount
			                  WHEN a.code LIKE '4%' AND jl.side = 'debit'  THEN -jl.reporting_amount
			                  ELSE 0 END), 0) AS revenue
		FROM journal_lines jl
		JOIN accounts a ON a.id = jl.account_id
		JOIN journals j ON j.id = jl.journal_id
		JOIN transactions t ON t.id = j.source_transaction_id
		WHERE jl.company_id = $1 AND j.posted_at >= $2 AND j.posted_at <= $3
		  AND a.code LIKE '4%' `+clause+`
		GROUP BY t.document_date
		ORDER BY t.document_date`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []reporting.DailyPoint
	for rows.Next() {
		var p reporting.DailyPoint
		if err := rows.Scan(&p.Date, &p.Revenue); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// branchIDClause is branchScopeClause's logic against a query whose own
// table already carries branch_id directly (no "journals as j" join to
// reach through), used by PendingApprovalsCount and CashDifferenceAlerts.
func branchIDClause(column string, branchID *uuid.UUID, allowedBranches []uuid.UUID, argStart int) (clause string, args []any) {
	switch {
	case branchID != nil:
		return "AND " + column + " = $" + strconv.Itoa(argStart), []any{*branchID}
	case len(allowedBranches) > 0:
		return "AND " + column + " = ANY($" + strconv.Itoa(argStart) + ")", []any{allowedBranches}
	default:
		return "", nil
	}
}

// PendingApprovalsCount counts transactions currently gated at
// awaiting_approval (section 5.4: "Above-limit requests enter Awaiting
// approval").
func (db *DB) PendingApprovalsCount(ctx context.Context, companyID uuid.UUID, branchID *uuid.UUID, allowedBranches []uuid.UUID) (int, error) {
	clause, extraArgs := branchIDClause("branch_id", branchID, allowedBranches, 2)
	args := []any{companyID}
	args = append(args, extraArgs...)

	var count int
	err := db.pool.QueryRow(ctx, `
		SELECT count(*) FROM transactions
		WHERE company_id = $1 AND status = 'awaiting_approval' `+clause, args...).Scan(&count)
	return count, err
}

// CashDifferenceAlerts lists submitted (not yet approved) daily closes with
// a nonzero discrepancy, most recent first — User Manual: "Cash differences
// remain visible and do not disappear through editing totals."
func (db *DB) CashDifferenceAlerts(ctx context.Context, companyID uuid.UUID, branchID *uuid.UUID, allowedBranches []uuid.UUID) ([]reporting.CashDifferenceAlert, error) {
	clause, extraArgs := branchIDClause("dc.branch_id", branchID, allowedBranches, 2)
	args := []any{companyID}
	args = append(args, extraArgs...)

	rows, err := db.pool.Query(ctx, `
		SELECT b.name, dc.close_date, dc.currency_code, dc.discrepancy
		FROM daily_closes dc
		JOIN branches b ON b.id = dc.branch_id
		WHERE dc.company_id = $1 AND dc.status = 'submitted' AND dc.discrepancy <> 0 `+clause+`
		ORDER BY dc.close_date DESC
		LIMIT 20`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []reporting.CashDifferenceAlert
	for rows.Next() {
		var a reporting.CashDifferenceAlert
		if err := rows.Scan(&a.BranchName, &a.CloseDate, &a.Currency, &a.Discrepancy); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// RejectedOperationAlerts lists recently rejected commands (period closed,
// insufficient stock, conflict, ...) so a manager sees what staff tried and
// failed to record — the rejection is already durable via RecordRejection;
// this just surfaces it.
func (db *DB) RejectedOperationAlerts(ctx context.Context, companyID uuid.UUID, branchID *uuid.UUID, allowedBranches []uuid.UUID) ([]reporting.RejectedOperationAlert, error) {
	clause, extraArgs := branchIDClause("o.branch_id", branchID, allowedBranches, 2)
	args := []any{companyID}
	args = append(args, extraArgs...)

	rows, err := db.pool.Query(ctx, `
		SELECT b.name, o.command_type, coalesce(o.error_code, ''), o.server_received_at
		FROM operations o
		JOIN branches b ON b.id = o.branch_id
		WHERE o.company_id = $1 AND o.status = 'rejected'
		  AND o.server_received_at >= now() - interval '7 days' `+clause+`
		ORDER BY o.server_received_at DESC
		LIMIT 20`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []reporting.RejectedOperationAlert
	for rows.Next() {
		var a reporting.RejectedOperationAlert
		if err := rows.Scan(&a.BranchName, &a.CommandType, &a.ErrorCode, &a.OccurredAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// LargeTransactionAlerts flags posted transactions well above the period's
// typical size: more than 3x the period average, with guards against noise
// on a thin or low-value sample (fewer than 5 posted transactions, or an
// amount under 50 reporting units, never triggers this).
func (db *DB) LargeTransactionAlerts(ctx context.Context, companyID uuid.UUID, branchID *uuid.UUID, allowedBranches []uuid.UUID, from, to time.Time) ([]reporting.LargeTransactionAlert, error) {
	clause, extraArgs := branchScopeClause(branchID, allowedBranches, 4)
	args := []any{companyID, from, to}
	args = append(args, extraArgs...)

	rows, err := db.pool.Query(ctx, `
		WITH totals AS (
			SELECT t.id, t.branch_id, t.document_type, t.document_date,
			       COALESCE(SUM(l.line_net), 0) AS amount
			FROM transactions t
			LEFT JOIN transaction_lines l ON l.transaction_id = t.id
			JOIN journals j ON j.source_transaction_id = t.id
			WHERE t.company_id = $1 AND t.status = 'posted' AND t.document_date BETWEEN $2 AND $3 `+clause+`
			GROUP BY t.id, t.branch_id, t.document_type, t.document_date
		),
		stats AS (
			SELECT AVG(amount) AS avg_amount, COUNT(*) AS n FROM totals WHERE amount > 0
		)
		SELECT b.name, totals.document_type, totals.amount, totals.document_date
		FROM totals
		JOIN branches b ON b.id = totals.branch_id
		CROSS JOIN stats
		WHERE stats.n >= 5 AND totals.amount > 3 * stats.avg_amount AND totals.amount > 50
		ORDER BY totals.amount DESC
		LIMIT 10`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []reporting.LargeTransactionAlert
	for rows.Next() {
		var a reporting.LargeTransactionAlert
		if err := rows.Scan(&a.BranchName, &a.DocumentType, &a.Amount, &a.Date); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// LowStockAlerts lists stocked products whose latest tracked quantity at a
// branch is at or below their reorder point. "Latest" is the most recent
// stock_movements row for that (branch, product) pair — the same running
// position the accounting engine itself reads before issuing or receiving
// stock.
func (db *DB) LowStockAlerts(ctx context.Context, companyID uuid.UUID, branchID *uuid.UUID, allowedBranches []uuid.UUID) ([]reporting.LowStockAlert, error) {
	clause, extraArgs := branchIDClause("sm.branch_id", branchID, allowedBranches, 2)
	args := []any{companyID}
	args = append(args, extraArgs...)

	rows, err := db.pool.Query(ctx, `
		WITH latest AS (
			SELECT DISTINCT ON (sm.branch_id, sm.product_id)
				sm.branch_id, sm.product_id, sm.running_quantity
			FROM stock_movements sm
			WHERE sm.company_id = $1 `+clause+`
			ORDER BY sm.branch_id, sm.product_id, sm.occurred_at DESC, sm.id DESC
		)
		SELECT b.name, p.name, latest.running_quantity, p.reorder_point, p.unit
		FROM latest
		JOIN branches b ON b.id = latest.branch_id
		JOIN products p ON p.id = latest.product_id
		WHERE latest.running_quantity <= p.reorder_point
		ORDER BY latest.running_quantity ASC
		LIMIT 20`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []reporting.LowStockAlert
	for rows.Next() {
		var a reporting.LowStockAlert
		if err := rows.Scan(&a.BranchName, &a.ProductName, &a.Quantity, &a.ReorderPoint, &a.Unit); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// OverdueDebtAlerts lists customers with a positive outstanding balance
// (posted credit sales minus posted receipts, company-wide — receipts
// aren't branch-specific) who also have at least one credit sale past its
// due date in scope. See the field's doc comment in reporting.Result for why
// this approximates rather than tracks per-invoice settlement.
func (db *DB) OverdueDebtAlerts(ctx context.Context, companyID uuid.UUID, branchID *uuid.UUID, allowedBranches []uuid.UUID) ([]reporting.OverdueDebtAlert, error) {
	clause, extraArgs := branchIDClause("t.branch_id", branchID, allowedBranches, 2)
	args := []any{companyID}
	args = append(args, extraArgs...)

	rows, err := db.pool.Query(ctx, `
		SELECT c.name, COALESCE(sales.total, 0) - COALESCE(receipts.total, 0) AS balance, oldest.due_date
		FROM counterparties c
		JOIN LATERAL (
			SELECT MIN(t.due_date) AS due_date
			FROM transactions t
			WHERE t.counterparty_id = c.id AND t.document_type = 'sale' AND t.status = 'posted'
			  AND t.due_date < CURRENT_DATE `+clause+`
		) oldest ON true
		LEFT JOIN LATERAL (
			SELECT COALESCE(SUM(l.line_net), 0) AS total
			FROM transactions t JOIN transaction_lines l ON l.transaction_id = t.id
			WHERE t.counterparty_id = c.id AND t.document_type = 'sale' AND t.status = 'posted'
		) sales ON true
		LEFT JOIN LATERAL (
			SELECT COALESCE(SUM(l.line_net), 0) AS total
			FROM transactions t JOIN transaction_lines l ON l.transaction_id = t.id
			WHERE t.counterparty_id = c.id AND t.document_type = 'receipt' AND t.status = 'posted'
		) receipts ON true
		WHERE c.company_id = $1 AND c.kind = 'customer' AND oldest.due_date IS NOT NULL
		  AND COALESCE(sales.total, 0) - COALESCE(receipts.total, 0) > 0
		ORDER BY oldest.due_date
		LIMIT 20`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []reporting.OverdueDebtAlert
	for rows.Next() {
		var a reporting.OverdueDebtAlert
		if err := rows.Scan(&a.CustomerName, &a.OutstandingAmount, &a.OldestDueDate); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}
