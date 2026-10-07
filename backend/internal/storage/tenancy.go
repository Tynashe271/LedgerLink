package storage

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"

	"github.com/ledgerlink/branchledger/backend/internal/accounting"
)

// BranchInCompany implements accounting.Repository: a direct, non-transactional
// read, since it is deliberately used before any write transaction opens.
func (db *DB) BranchInCompany(ctx context.Context, companyID, branchID uuid.UUID) (bool, error) {
	var exists bool
	err := db.pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM branches WHERE id = $1 AND company_id = $2)`,
		branchID, companyID).Scan(&exists)
	return exists, err
}

// RecordDenialAudit implements accounting.Repository: a standalone insert that
// commits on its own, independent of (and therefore surviving) any posting
// transaction that gets rolled back because of the denial it documents.
func (db *DB) RecordDenialAudit(ctx context.Context, companyID, actorUserID uuid.UUID, eventType, recordType string, recordID uuid.UUID, details map[string]any) error {
	payload, err := json.Marshal(details)
	if err != nil {
		return err
	}
	_, err = db.pool.Exec(ctx, `
		INSERT INTO audit_events (company_id, actor_user_id, event_type, record_type, record_id, details)
		VALUES ($1,$2,$3,$4,$5,$6)`,
		companyID, actorUserID, eventType, recordType, recordID, payload)
	return err
}

// RecordRejection implements accounting.Repository: it persists a rejected
// operation in its own autocommit statement, run after the posting attempt
// that triggered it has already rolled back — so the rejection record is the
// only trace of that attempt, and a retry with the same operation ID finds it
// via FindOperation and returns the same rejection rather than re-deriving it.
func (db *DB) RecordRejection(ctx context.Context, rec accounting.OperationRecord) error {
	var errorCode *string
	if rec.ErrorCode != "" {
		errorCode = &rec.ErrorCode
	}
	_, err := db.pool.Exec(ctx, `
		INSERT INTO operations (company_id, operation_id, branch_id, device_id, actor_user_id,
		                         command_type, payload_hash, status, error_code, client_submitted_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
		rec.CompanyID, rec.OperationID, rec.BranchID, rec.DeviceID, rec.ActorUserID,
		rec.CommandType, rec.PayloadHash, rec.Status, errorCode, rec.ClientSubmittedAt)
	return err
}
