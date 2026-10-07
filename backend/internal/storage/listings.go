package storage

import (
	"context"
	"strconv"

	"github.com/google/uuid"

	"github.com/ledgerlink/branchledger/backend/internal/listings"
)

// Listings returns a view of DB implementing listings.Store.
func (db *DB) Listings() *listingsRepo { return &listingsRepo{db: db} }

type listingsRepo struct{ db *DB }

func (r *listingsRepo) ListTransactions(ctx context.Context, companyID uuid.UUID, allowedBranches []uuid.UUID, f listings.TransactionFilter) ([]listings.TransactionSummary, error) {
	args := []any{companyID, f.From, f.To}
	where := `t.company_id = $1 AND t.document_date >= $2 AND t.document_date <= $3`

	if f.BranchID != nil {
		args = append(args, *f.BranchID)
		where += ` AND t.branch_id = $` + strconv.Itoa(len(args))
	} else if len(allowedBranches) > 0 {
		args = append(args, allowedBranches)
		where += ` AND t.branch_id = ANY($` + strconv.Itoa(len(args)) + `)`
	}
	if f.DocumentType != "" {
		args = append(args, f.DocumentType)
		where += ` AND t.document_type = $` + strconv.Itoa(len(args))
	}
	if f.Status != "" {
		args = append(args, f.Status)
		where += ` AND t.status = $` + strconv.Itoa(len(args))
	}
	args = append(args, f.Limit)

	rows, err := r.db.pool.Query(ctx, `
		SELECT t.id, t.branch_id, b.name, t.document_type, t.document_date, t.currency_code, t.status,
		       COALESCE(SUM(l.line_net), 0), COALESCE(t.source_reference, ''), COALESCE(t.explanation, '')
		FROM transactions t
		JOIN branches b ON b.id = t.branch_id
		LEFT JOIN transaction_lines l ON l.transaction_id = t.id
		WHERE `+where+`
		GROUP BY t.id, t.branch_id, b.name, t.document_type, t.document_date, t.currency_code, t.status,
		         t.source_reference, t.explanation
		ORDER BY t.document_date DESC, t.created_at DESC
		LIMIT $`+strconv.Itoa(len(args)), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []listings.TransactionSummary
	for rows.Next() {
		var s listings.TransactionSummary
		if err := rows.Scan(&s.ID, &s.BranchID, &s.BranchName, &s.DocumentType, &s.DocumentDate, &s.CurrencyCode,
			&s.Status, &s.NetAmount, &s.SourceReference, &s.Explanation); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func (r *listingsRepo) ListPendingApprovals(ctx context.Context, companyID uuid.UUID, allowedBranches []uuid.UUID) ([]listings.ApprovalSummary, error) {
	args := []any{companyID}
	where := `a.company_id = $1 AND a.decision IS NULL`
	if len(allowedBranches) > 0 {
		args = append(args, allowedBranches)
		where += ` AND t.branch_id = ANY($` + strconv.Itoa(len(args)) + `)`
	}

	rows, err := r.db.pool.Query(ctx, `
		SELECT a.id, a.transaction_id, t.branch_id, b.name, t.document_type, t.document_date,
		       COALESCE(SUM(l.line_net), 0), t.currency_code, COALESCE(t.explanation, ''),
		       u.full_name, a.requested_at, a.record_version_at_request
		FROM approvals a
		JOIN transactions t ON t.id = a.transaction_id
		JOIN branches b ON b.id = t.branch_id
		JOIN users u ON u.id = a.requested_by
		LEFT JOIN transaction_lines l ON l.transaction_id = t.id
		WHERE `+where+`
		GROUP BY a.id, a.transaction_id, t.branch_id, b.name, t.document_type, t.document_date,
		         t.currency_code, t.explanation, u.full_name, a.requested_at, a.record_version_at_request
		ORDER BY a.requested_at`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []listings.ApprovalSummary
	for rows.Next() {
		var s listings.ApprovalSummary
		if err := rows.Scan(&s.ApprovalID, &s.TransactionID, &s.BranchID, &s.BranchName, &s.DocumentType,
			&s.DocumentDate, &s.NetAmount, &s.CurrencyCode, &s.Explanation, &s.RequestedByName,
			&s.RequestedAt, &s.RecordVersionAtRequest); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func (r *listingsRepo) ListOpenTransfers(ctx context.Context, companyID uuid.UUID, allowedBranches []uuid.UUID) ([]listings.TransferSummary, error) {
	args := []any{companyID}
	where := `tr.company_id = $1 AND tr.status IN ('dispatched', 'partially_received')`
	if len(allowedBranches) > 0 {
		args = append(args, allowedBranches)
		placeholder := `$` + strconv.Itoa(len(args))
		where += ` AND (tr.sender_branch_id = ANY(` + placeholder + `) OR tr.receiver_branch_id = ANY(` + placeholder + `))`
	}

	rows, err := r.db.pool.Query(ctx, `
		SELECT tr.id, tr.transfer_type, sb.name, tr.receiver_branch_id, rb.name, tr.status,
		       COALESCE(tl.amount, 0), COALESCE(tl.amount_received, 0), COALESCE(tr.currency_code, ''),
		       d.created_at
		FROM transfers tr
		JOIN branches sb ON sb.id = tr.sender_branch_id
		JOIN branches rb ON rb.id = tr.receiver_branch_id
		JOIN transactions d ON d.id = tr.dispatch_transaction_id
		LEFT JOIN transfer_lines tl ON tl.transfer_id = tr.id AND tl.product_id IS NULL
		WHERE `+where+`
		ORDER BY d.created_at DESC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []listings.TransferSummary
	for rows.Next() {
		var s listings.TransferSummary
		if err := rows.Scan(&s.ID, &s.TransferType, &s.SenderBranchName, &s.ReceiverBranchID, &s.ReceiverBranchName,
			&s.Status, &s.Amount, &s.ReceivedAmount, &s.CurrencyCode, &s.DispatchedAt); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// ListStockPositions returns each (branch, product) pair's latest recorded
// stock movement — DISTINCT ON picks the newest row per pair, never
// collapsing a product's multiple branches into one (BranchLedger is
// multi-branch: a company's stock view must show every sub-branch).
func (r *listingsRepo) ListStockPositions(ctx context.Context, companyID uuid.UUID, allowedBranches []uuid.UUID) ([]listings.StockPositionSummary, error) {
	args := []any{companyID}
	where := `sm.company_id = $1`
	if len(allowedBranches) > 0 {
		args = append(args, allowedBranches)
		where += ` AND sm.branch_id = ANY($` + strconv.Itoa(len(args)) + `)`
	}

	rows, err := r.db.pool.Query(ctx, `
		SELECT DISTINCT ON (sm.branch_id, sm.product_id)
		       p.id, p.sku, p.name, p.unit, sm.branch_id, b.name, sm.running_quantity, sm.running_value
		FROM stock_movements sm
		JOIN products p ON p.id = sm.product_id
		JOIN branches b ON b.id = sm.branch_id
		WHERE `+where+`
		ORDER BY sm.branch_id, sm.product_id, sm.occurred_at DESC, sm.id DESC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []listings.StockPositionSummary
	for rows.Next() {
		var s listings.StockPositionSummary
		if err := rows.Scan(&s.ProductID, &s.SKU, &s.ProductName, &s.Unit, &s.BranchID, &s.BranchName,
			&s.Quantity, &s.Value); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}
