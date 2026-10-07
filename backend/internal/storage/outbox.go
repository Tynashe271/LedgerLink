package storage

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"
)

// OutboxJob mirrors one claimed `outbox_jobs` row — "Go workers consume
// committed outbox jobs" (architecture, deployment and data paths).
type OutboxJob struct {
	ID         uuid.UUID
	CompanyID  uuid.UUID
	JobType    string
	Payload    map[string]any
	Attempts   int
}

// ClaimNextOutboxJob atomically claims one pending, due job (SELECT ... FOR
// UPDATE SKIP LOCKED so concurrent worker processes never claim the same
// row) and marks it "processing". found=false means nothing was due.
func (db *DB) ClaimNextOutboxJob(ctx context.Context) (OutboxJob, bool, error) {
	var job OutboxJob
	var payload []byte
	row := db.pool.QueryRow(ctx, `
		UPDATE outbox_jobs SET status = 'processing', attempts = attempts + 1, updated_at = now()
		WHERE id = (
			SELECT id FROM outbox_jobs
			WHERE status = 'pending' AND available_at <= now()
			ORDER BY available_at
			LIMIT 1
			FOR UPDATE SKIP LOCKED
		)
		RETURNING id, company_id, job_type, payload, attempts`)
	if err := row.Scan(&job.ID, &job.CompanyID, &job.JobType, &payload, &job.Attempts); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return OutboxJob{}, false, nil
		}
		return OutboxJob{}, false, err
	}
	_ = json.Unmarshal(payload, &job.Payload)
	return job, true, nil
}

func (db *DB) MarkOutboxJobDone(ctx context.Context, id uuid.UUID) error {
	_, err := db.pool.Exec(ctx, `UPDATE outbox_jobs SET status = 'done', updated_at = now() WHERE id = $1`, id)
	return err
}

// MarkOutboxJobFailed records the failure. Within maxAttempts it goes back to
// "pending" with an exponential backoff (bounded retries, per the worker's
// own TODO this replaces); beyond that it is parked at "failed" for an
// operator to inspect rather than retried forever.
func (db *DB) MarkOutboxJobFailed(ctx context.Context, id uuid.UUID, attempts int, errMsg string) error {
	const maxAttempts = 5
	status := "pending"
	availableAt := time.Now().UTC().Add(time.Duration(attempts) * 30 * time.Second)
	if attempts >= maxAttempts {
		status = "failed"
	}
	_, err := db.pool.Exec(ctx, `
		UPDATE outbox_jobs SET status = $1, available_at = $2, last_error = $3, updated_at = now() WHERE id = $4`,
		status, availableAt, errMsg, id)
	return err
}

// --- Export generation ------------------------------------------------------

// ExportMeta is the export row's scope, as the worker needs it to run the
// query that produces the file.
type ExportMeta struct {
	CompanyID  uuid.UUID
	ExportType string
	BranchID   *uuid.UUID
	From       time.Time
	To         time.Time
}

func (db *DB) GetExportMeta(ctx context.Context, exportID uuid.UUID) (ExportMeta, bool, error) {
	var companyID uuid.UUID
	var exportType string
	var scopeJSON []byte
	row := db.pool.QueryRow(ctx, `SELECT company_id, export_type, scope FROM exports WHERE id = $1`, exportID)
	if err := row.Scan(&companyID, &exportType, &scopeJSON); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ExportMeta{}, false, nil
		}
		return ExportMeta{}, false, err
	}
	var raw struct {
		BranchID *uuid.UUID `json:"branch_id"`
		From     string     `json:"from"`
		To       string     `json:"to"`
	}
	_ = json.Unmarshal(scopeJSON, &raw)
	meta := ExportMeta{CompanyID: companyID, ExportType: exportType, BranchID: raw.BranchID}
	meta.From, _ = time.Parse("2006-01-02", raw.From)
	meta.To, _ = time.Parse("2006-01-02", raw.To)
	return meta, true, nil
}

// ExportTransactionRow is one CSV row of a transactions export.
type ExportTransactionRow struct {
	ID           uuid.UUID
	BranchID     uuid.UUID
	DocumentType string
	DocumentDate time.Time
	CurrencyCode string
	Status       string
	NetAmount    decimal.Decimal
}

// QueryTransactionsForExport returns every transaction (any status — an
// export is a record of what happened, including rejections and reversals,
// not only what posted) in scope, newest first, for the worker to write out.
func (db *DB) QueryTransactionsForExport(ctx context.Context, companyID uuid.UUID, branchID *uuid.UUID, from, to time.Time) ([]ExportTransactionRow, error) {
	rows, err := db.pool.Query(ctx, `
		SELECT t.id, t.branch_id, t.document_type, t.document_date, t.currency_code, t.status,
		       coalesce(sum(l.line_net), 0)
		FROM transactions t
		LEFT JOIN transaction_lines l ON l.transaction_id = t.id
		WHERE t.company_id = $1 AND t.document_date BETWEEN $2 AND $3
		  AND ($4::uuid IS NULL OR t.branch_id = $4)
		GROUP BY t.id, t.branch_id, t.document_type, t.document_date, t.currency_code, t.status
		ORDER BY t.document_date DESC, t.id DESC`, companyID, from, to, branchID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []ExportTransactionRow
	for rows.Next() {
		var r ExportTransactionRow
		if err := rows.Scan(&r.ID, &r.BranchID, &r.DocumentType, &r.DocumentDate, &r.CurrencyCode, &r.Status, &r.NetAmount); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (db *DB) MarkExportReady(ctx context.Context, exportID uuid.UUID, objectKey string, rowCount int, expiresAt time.Time) error {
	_, err := db.pool.Exec(ctx, `
		UPDATE exports SET status = 'ready', object_key = $1, row_count = $2, completed_at = now(), expires_at = $3
		WHERE id = $4`, objectKey, rowCount, expiresAt, exportID)
	return err
}

func (db *DB) MarkExportFailed(ctx context.Context, exportID uuid.UUID, errorCode string) error {
	_, err := db.pool.Exec(ctx, `
		UPDATE exports SET status = 'failed', error_code = $1, completed_at = now() WHERE id = $2`, errorCode, exportID)
	return err
}
