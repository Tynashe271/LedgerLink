package storage

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/ledgerlink/branchledger/backend/internal/exports"
)

// Exports returns a view of DB implementing exports.Repository. Export
// requests are simple row writes outside the accounting transaction
// boundary — there is no posting, no idempotent command ledger to check —
// so, unlike accounting.Repository, this does not need a WithTx indirection.
func (db *DB) Exports() *exportsRepo { return &exportsRepo{db: db} }

type exportsRepo struct{ db *DB }

func (r *exportsRepo) Create(ctx context.Context, rec exports.Record) error {
	scopeJSON, err := json.Marshal(rec.Scope)
	if err != nil {
		return err
	}
	_, err = r.db.pool.Exec(ctx, `
		INSERT INTO exports (id, company_id, requested_by, export_type, scope, status, requested_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7)`,
		rec.ID, rec.CompanyID, rec.RequestedBy, rec.ExportType, scopeJSON, rec.Status, rec.RequestedAt)
	return err
}

func (r *exportsRepo) Get(ctx context.Context, companyID, exportID uuid.UUID) (exports.Record, bool, error) {
	var rec exports.Record
	var scopeJSON []byte
	row := r.db.pool.QueryRow(ctx, `
		SELECT id, company_id, requested_by, export_type, scope, status, object_key, row_count,
		       coalesce(error_code, ''), requested_at, completed_at, expires_at
		FROM exports WHERE company_id = $1 AND id = $2`, companyID, exportID)
	if err := row.Scan(&rec.ID, &rec.CompanyID, &rec.RequestedBy, &rec.ExportType, &scopeJSON, &rec.Status,
		&rec.ObjectKey, &rec.RowCount, &rec.ErrorCode, &rec.RequestedAt, &rec.CompletedAt, &rec.ExpiresAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return exports.Record{}, false, nil
		}
		return exports.Record{}, false, err
	}
	_ = json.Unmarshal(scopeJSON, &rec.Scope)
	return rec, true, nil
}

func (r *exportsRepo) EnqueueOutboxJob(ctx context.Context, companyID uuid.UUID, jobType, idempotencyKey string, payload map[string]any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = r.db.pool.Exec(ctx, `
		INSERT INTO outbox_jobs (company_id, job_type, payload, idempotency_key)
		VALUES ($1,$2,$3,$4)
		ON CONFLICT (company_id, job_type, idempotency_key) DO NOTHING`,
		companyID, jobType, body, idempotencyKey)
	return err
}
