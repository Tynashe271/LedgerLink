package storage

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/ledgerlink/branchledger/backend/internal/reporting"
)

// TrialBalanceLines sums posted journal lines per account through asOf,
// across every active account rather than the four balance-sheet accounts
// BalancesAsOf shows. A LEFT JOIN from accounts means an account with no
// matching activity still appears with a zero balance, since a trial
// balance is a review of the whole chart, not just the accounts that moved.
func (db *DB) TrialBalanceLines(ctx context.Context, companyID uuid.UUID, branchID *uuid.UUID, allowedBranches []uuid.UUID, asOf time.Time) ([]reporting.TrialBalanceLine, error) {
	clause, extraArgs := branchScopeClause(branchID, allowedBranches, 3)
	args := []any{companyID, asOf}
	args = append(args, extraArgs...)

	rows, err := db.pool.Query(ctx, `
		SELECT a.code, a.name, a.account_type,
		       GREATEST(COALESCE(SUM(CASE WHEN jl.side = 'debit' THEN jl.reporting_amount
		                                   WHEN jl.side = 'credit' THEN -jl.reporting_amount END), 0), 0) AS debit,
		       GREATEST(-COALESCE(SUM(CASE WHEN jl.side = 'debit' THEN jl.reporting_amount
		                                    WHEN jl.side = 'credit' THEN -jl.reporting_amount END), 0), 0) AS credit
		FROM accounts a
		LEFT JOIN journals j ON j.company_id = a.company_id AND j.posted_at <= $2 `+clause+`
		LEFT JOIN journal_lines jl ON jl.journal_id = j.id AND jl.account_id = a.id
		WHERE a.company_id = $1 AND a.is_active
		GROUP BY a.code, a.name, a.account_type
		ORDER BY a.code`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []reporting.TrialBalanceLine
	for rows.Next() {
		var l reporting.TrialBalanceLine
		if err := rows.Scan(&l.AccountCode, &l.AccountName, &l.AccountType, &l.Debit, &l.Credit); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// AgeingSourceRows lists every customer with a positive outstanding balance
// and at least one due credit sale posted by asOf — the same aggregate
// balance/oldest-due-date approximation as OverdueDebtAlerts (that method's
// doc comment explains why), but across every due sale through asOf rather
// than only ones already overdue right now, and with no row limit: Ageing
// needs the whole schedule, not just a top-20 alert list.
func (db *DB) AgeingSourceRows(ctx context.Context, companyID uuid.UUID, branchID *uuid.UUID, allowedBranches []uuid.UUID, asOf time.Time) ([]reporting.OverdueDebtAlert, error) {
	clause, extraArgs := branchIDClause("t.branch_id", branchID, allowedBranches, 3)
	args := []any{companyID, asOf}
	args = append(args, extraArgs...)

	rows, err := db.pool.Query(ctx, `
		SELECT c.name, COALESCE(sales.total, 0) - COALESCE(receipts.total, 0) AS balance, oldest.due_date
		FROM counterparties c
		JOIN LATERAL (
			SELECT MIN(t.due_date) AS due_date
			FROM transactions t
			WHERE t.counterparty_id = c.id AND t.document_type = 'sale' AND t.status = 'posted'
			  AND t.due_date IS NOT NULL AND t.posted_at <= $2 `+clause+`
		) oldest ON true
		LEFT JOIN LATERAL (
			SELECT COALESCE(SUM(l.line_net), 0) AS total
			FROM transactions t JOIN transaction_lines l ON l.transaction_id = t.id
			WHERE t.counterparty_id = c.id AND t.document_type = 'sale' AND t.status = 'posted' AND t.posted_at <= $2
		) sales ON true
		LEFT JOIN LATERAL (
			SELECT COALESCE(SUM(l.line_net), 0) AS total
			FROM transactions t JOIN transaction_lines l ON l.transaction_id = t.id
			WHERE t.counterparty_id = c.id AND t.document_type = 'receipt' AND t.status = 'posted' AND t.posted_at <= $2
		) receipts ON true
		WHERE c.company_id = $1 AND c.kind = 'customer' AND oldest.due_date IS NOT NULL
		  AND COALESCE(sales.total, 0) - COALESCE(receipts.total, 0) > 0
		ORDER BY oldest.due_date`, args...)
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
