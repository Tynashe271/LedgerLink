package storage

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/ledgerlink/branchledger/backend/internal/reconciliation"
)

// Reconciliation returns a view of DB implementing reconciliation.Store.
func (db *DB) Reconciliation() *reconciliationRepo { return &reconciliationRepo{db: db} }

type reconciliationRepo struct{ db *DB }

func (r *reconciliationRepo) ListCashLikeAccounts(ctx context.Context, companyID uuid.UUID) ([]reconciliation.CashLikeAccount, error) {
	rows, err := r.db.pool.Query(ctx, `
		SELECT id, code, name FROM accounts
		WHERE company_id = $1 AND is_cash_like AND is_active
		ORDER BY code`, companyID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []reconciliation.CashLikeAccount
	for rows.Next() {
		var a reconciliation.CashLikeAccount
		if err := rows.Scan(&a.ID, &a.Code, &a.Name); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (r *reconciliationRepo) GetCashLikeAccount(ctx context.Context, companyID, accountID uuid.UUID) (reconciliation.CashLikeAccount, bool, error) {
	var a reconciliation.CashLikeAccount
	err := r.db.pool.QueryRow(ctx, `
		SELECT id, code, name FROM accounts
		WHERE company_id = $1 AND id = $2 AND is_cash_like AND is_active`, companyID, accountID,
	).Scan(&a.ID, &a.Code, &a.Name)
	if errors.Is(err, pgx.ErrNoRows) {
		return reconciliation.CashLikeAccount{}, false, nil
	}
	return a, err == nil, err
}

func (r *reconciliationRepo) InsertStatementItem(ctx context.Context, companyID, createdBy uuid.UUID, item reconciliation.StatementItem) error {
	_, err := r.db.pool.Exec(ctx, `
		INSERT INTO statement_items (id, company_id, branch_id, account_id, statement_date, description,
		                              amount, external_reference, status, created_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,'unmatched',$9)
		ON CONFLICT (id) DO NOTHING`,
		item.ID, companyID, item.BranchID, item.AccountID, item.StatementDate, item.Description,
		item.Amount, nullableString(item.ExternalReference), createdBy)
	return err
}

func (r *reconciliationRepo) GetStatementItem(ctx context.Context, companyID, id uuid.UUID) (reconciliation.StatementItem, bool, error) {
	item, found, err := r.scanStatementItem(ctx, `
		SELECT si.id, si.branch_id, b.name, si.account_id, si.statement_date, si.description, si.amount,
		       COALESCE(si.external_reference, ''), si.status, si.matched_journal_line_id, si.created_at
		FROM statement_items si
		JOIN branches b ON b.id = si.branch_id
		WHERE si.company_id = $1 AND si.id = $2`, companyID, id)
	return item, found, err
}

func (r *reconciliationRepo) ListStatementItems(ctx context.Context, companyID uuid.UUID, allowedBranches []uuid.UUID, accountID uuid.UUID, from, to time.Time) ([]reconciliation.StatementItem, error) {
	args := []any{companyID, accountID, from, to}
	where := `si.company_id = $1 AND si.account_id = $2 AND si.statement_date >= $3 AND si.statement_date <= $4`
	if len(allowedBranches) > 0 {
		args = append(args, allowedBranches)
		where += ` AND si.branch_id = ANY($` + strconv.Itoa(len(args)) + `)`
	}

	rows, err := r.db.pool.Query(ctx, `
		SELECT si.id, si.branch_id, b.name, si.account_id, si.statement_date, si.description, si.amount,
		       COALESCE(si.external_reference, ''), si.status, si.matched_journal_line_id, si.created_at
		FROM statement_items si
		JOIN branches b ON b.id = si.branch_id
		WHERE `+where+`
		ORDER BY si.statement_date DESC, si.created_at DESC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []reconciliation.StatementItem
	for rows.Next() {
		item, err := scanStatementItemRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (r *reconciliationRepo) ListLedgerEntries(ctx context.Context, companyID uuid.UUID, allowedBranches []uuid.UUID, accountID uuid.UUID, from, to time.Time) ([]reconciliation.LedgerEntry, error) {
	args := []any{companyID, accountID, from, to}
	where := `jl.company_id = $1 AND jl.account_id = $2 AND t.document_date >= $3 AND t.document_date <= $4`
	if len(allowedBranches) > 0 {
		args = append(args, allowedBranches)
		where += ` AND j.branch_id = ANY($` + strconv.Itoa(len(args)) + `)`
	}

	rows, err := r.db.pool.Query(ctx, `
		SELECT jl.id, j.source_transaction_id, j.branch_id, b.name, t.document_type, t.document_date,
		       COALESCE(t.explanation, ''),
		       CASE WHEN jl.side = 'debit' THEN jl.original_amount ELSE -jl.original_amount END,
		       (si.id IS NOT NULL)
		FROM journal_lines jl
		JOIN journals j ON j.id = jl.journal_id
		JOIN transactions t ON t.id = j.source_transaction_id
		JOIN branches b ON b.id = j.branch_id
		LEFT JOIN statement_items si ON si.matched_journal_line_id = jl.id
		WHERE `+where+`
		ORDER BY t.document_date DESC, jl.id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []reconciliation.LedgerEntry
	for rows.Next() {
		e, err := scanLedgerEntryRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (r *reconciliationRepo) GetLedgerEntry(ctx context.Context, companyID, journalLineID uuid.UUID) (reconciliation.LedgerEntry, bool, error) {
	row := r.db.pool.QueryRow(ctx, `
		SELECT jl.id, j.source_transaction_id, j.branch_id, b.name, t.document_type, t.document_date,
		       COALESCE(t.explanation, ''),
		       CASE WHEN jl.side = 'debit' THEN jl.original_amount ELSE -jl.original_amount END,
		       (si.id IS NOT NULL)
		FROM journal_lines jl
		JOIN journals j ON j.id = jl.journal_id
		JOIN transactions t ON t.id = j.source_transaction_id
		JOIN branches b ON b.id = j.branch_id
		LEFT JOIN statement_items si ON si.matched_journal_line_id = jl.id
		WHERE jl.company_id = $1 AND jl.id = $2`, companyID, journalLineID)

	e, err := scanLedgerEntryRow(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return reconciliation.LedgerEntry{}, false, nil
	}
	return e, err == nil, err
}

func (r *reconciliationRepo) SetMatch(ctx context.Context, companyID, statementItemID, journalLineID uuid.UUID) error {
	tag, err := r.db.pool.Exec(ctx, `
		UPDATE statement_items SET status = 'matched', matched_journal_line_id = $3
		WHERE company_id = $1 AND id = $2 AND status = 'unmatched'`,
		companyID, statementItemID, journalLineID)
	if err != nil {
		var pgErr *pgconn.PgError
		// 23505 unique_violation: another statement item already claimed
		// this journal line between this request's read and this write.
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return reconciliation.ErrAlreadyMatched
		}
		return err
	}
	if tag.RowsAffected() == 0 {
		return reconciliation.ErrAlreadyMatched
	}
	return nil
}

func (r *reconciliationRepo) ClearMatch(ctx context.Context, companyID, statementItemID uuid.UUID) error {
	tag, err := r.db.pool.Exec(ctx, `
		UPDATE statement_items SET status = 'unmatched', matched_journal_line_id = NULL
		WHERE company_id = $1 AND id = $2 AND status = 'matched'`,
		companyID, statementItemID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return reconciliation.ErrNotUnmatched
	}
	return nil
}

// row is the subset of pgx.Rows/pgx.Row this package scans from — both
// satisfy it, so ListStatementItems/GetStatementItem (and the ledger-entry
// equivalents) share one scan function each instead of duplicating it.
type row interface {
	Scan(dest ...any) error
}

func (r *reconciliationRepo) scanStatementItem(ctx context.Context, sql string, args ...any) (reconciliation.StatementItem, bool, error) {
	item, err := scanStatementItemRow(r.db.pool.QueryRow(ctx, sql, args...))
	if errors.Is(err, pgx.ErrNoRows) {
		return reconciliation.StatementItem{}, false, nil
	}
	return item, err == nil, err
}

func scanStatementItemRow(rw row) (reconciliation.StatementItem, error) {
	var item reconciliation.StatementItem
	err := rw.Scan(&item.ID, &item.BranchID, &item.BranchName, &item.AccountID, &item.StatementDate,
		&item.Description, &item.Amount, &item.ExternalReference, &item.Status,
		&item.MatchedJournalLineID, &item.CreatedAt)
	return item, err
}

func scanLedgerEntryRow(rw row) (reconciliation.LedgerEntry, error) {
	var e reconciliation.LedgerEntry
	err := rw.Scan(&e.JournalLineID, &e.TransactionID, &e.BranchID, &e.BranchName, &e.DocumentType,
		&e.DocumentDate, &e.Description, &e.Amount, &e.Matched)
	return e, err
}
