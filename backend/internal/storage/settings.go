package storage

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/shopspring/decimal"

	"github.com/ledgerlink/branchledger/backend/internal/settings"
)

// Settings returns a view of DB implementing settings.Store.
func (db *DB) Settings() *settingsRepo { return &settingsRepo{db: db} }

type settingsRepo struct{ db *DB }

// --- Company profile ----------------------------------------------------------

func (r *settingsRepo) GetCompanyProfile(ctx context.Context, companyID uuid.UUID) (settings.CompanyProfile, bool, error) {
	var p settings.CompanyProfile
	err := r.db.pool.QueryRow(ctx, `
		SELECT id, name, primary_category, reporting_currency, timezone, financial_year_start, status
		FROM companies WHERE id = $1`, companyID,
	).Scan(&p.ID, &p.Name, &p.PrimaryCategory, &p.ReportingCurrency, &p.Timezone, &p.FinancialYearStart, &p.Status)
	if errors.Is(err, pgx.ErrNoRows) {
		return settings.CompanyProfile{}, false, nil
	}
	return p, err == nil, err
}

func (r *settingsRepo) UpdateCompanyProfile(ctx context.Context, companyID uuid.UUID, in settings.UpdateCompanyInput) error {
	_, err := r.db.pool.Exec(ctx, `
		UPDATE companies
		SET name = $2, primary_category = $3, timezone = $4, financial_year_start = $5, updated_at = now()
		WHERE id = $1`,
		companyID, in.Name, in.PrimaryCategory, in.Timezone, in.FinancialYearStart)
	return err
}

// --- Chart of accounts ----------------------------------------------------

func (r *settingsRepo) ListAccounts(ctx context.Context, companyID uuid.UUID) ([]settings.Account, error) {
	rows, err := r.db.pool.Query(ctx, `
		SELECT id, code, name, account_type, is_cash_like, is_active
		FROM accounts WHERE company_id = $1 ORDER BY code`, companyID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []settings.Account
	for rows.Next() {
		var a settings.Account
		if err := rows.Scan(&a.ID, &a.Code, &a.Name, &a.AccountType, &a.IsCashLike, &a.IsActive); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (r *settingsRepo) AccountCodeExists(ctx context.Context, companyID uuid.UUID, code string) (bool, error) {
	var exists bool
	err := r.db.pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM accounts WHERE company_id = $1 AND code = $2)`, companyID, code,
	).Scan(&exists)
	return exists, err
}

func (r *settingsRepo) InsertAccount(ctx context.Context, companyID uuid.UUID, in settings.AddAccountInput) (uuid.UUID, error) {
	var id uuid.UUID
	err := r.db.pool.QueryRow(ctx, `
		INSERT INTO accounts (company_id, code, name, account_type, is_cash_like)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id`,
		companyID, in.Code, in.Name, in.AccountType, in.IsCashLike,
	).Scan(&id)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return uuid.Nil, settings.ErrDuplicateCode
		}
		return uuid.Nil, err
	}
	return id, nil
}

// --- Devices -------------------------------------------------------------

func (r *settingsRepo) ListDevices(ctx context.Context, companyID uuid.UUID) ([]settings.Device, error) {
	rows, err := r.db.pool.Query(ctx, `
		SELECT d.id, d.branch_id, b.name, d.user_id, u.full_name, d.label, d.is_offline_writer,
		       d.enrolled_at, d.lease_expires_at, d.revoked_at, d.last_sync_at
		FROM devices d
		JOIN branches b ON b.id = d.branch_id
		JOIN users u ON u.id = d.user_id
		WHERE d.company_id = $1
		ORDER BY b.name, d.enrolled_at DESC`, companyID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []settings.Device
	for rows.Next() {
		var dv settings.Device
		if err := rows.Scan(&dv.ID, &dv.BranchID, &dv.BranchName, &dv.UserID, &dv.UserName, &dv.Label,
			&dv.IsOfflineWriter, &dv.EnrolledAt, &dv.LeaseExpiresAt, &dv.RevokedAt, &dv.LastSyncAt); err != nil {
			return nil, err
		}
		out = append(out, dv)
	}
	return out, rows.Err()
}

func (r *settingsRepo) GetDevice(ctx context.Context, companyID, deviceID uuid.UUID) (settings.Device, bool, error) {
	var dv settings.Device
	err := r.db.pool.QueryRow(ctx, `
		SELECT d.id, d.branch_id, b.name, d.user_id, u.full_name, d.label, d.is_offline_writer,
		       d.enrolled_at, d.lease_expires_at, d.revoked_at, d.last_sync_at
		FROM devices d
		JOIN branches b ON b.id = d.branch_id
		JOIN users u ON u.id = d.user_id
		WHERE d.company_id = $1 AND d.id = $2`, companyID, deviceID,
	).Scan(&dv.ID, &dv.BranchID, &dv.BranchName, &dv.UserID, &dv.UserName, &dv.Label,
		&dv.IsOfflineWriter, &dv.EnrolledAt, &dv.LeaseExpiresAt, &dv.RevokedAt, &dv.LastSyncAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return settings.Device{}, false, nil
	}
	return dv, err == nil, err
}

func (r *settingsRepo) RevokeDevice(ctx context.Context, companyID, deviceID uuid.UUID) error {
	_, err := r.db.pool.Exec(ctx, `
		UPDATE devices SET revoked_at = now(), is_offline_writer = false
		WHERE company_id = $1 AND id = $2 AND revoked_at IS NULL`, companyID, deviceID)
	return err
}

// SetOfflineWriter runs both updates in one transaction so the branch is
// never left with two (or, mid-update, momentarily zero in a way a
// concurrent reader could double-assign against) offline writers.
func (r *settingsRepo) SetOfflineWriter(ctx context.Context, companyID, deviceID uuid.UUID) error {
	tx, err := r.db.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var branchID uuid.UUID
	if err := tx.QueryRow(ctx, `SELECT branch_id FROM devices WHERE company_id = $1 AND id = $2`,
		companyID, deviceID).Scan(&branchID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE devices SET is_offline_writer = false WHERE company_id = $1 AND branch_id = $2`,
		companyID, branchID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE devices SET is_offline_writer = true WHERE company_id = $1 AND id = $2`,
		companyID, deviceID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// --- Memberships / approval limits --------------------------------------------

func (r *settingsRepo) ListMemberships(ctx context.Context, companyID uuid.UUID) ([]settings.Membership, error) {
	rows, err := r.db.pool.Query(ctx, `
		SELECT m.id, u.id, u.full_name, u.email, m.role, m.approval_limit, m.is_active,
		       COALESCE((SELECT array_agg(b.name ORDER BY b.name) FROM branches b WHERE b.id = ANY(m.branch_scope)), '{}')
		FROM memberships m
		JOIN users u ON u.id = m.user_id
		WHERE m.company_id = $1
		ORDER BY u.full_name`, companyID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []settings.Membership
	for rows.Next() {
		m, err := scanMembershipRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (r *settingsRepo) GetMembership(ctx context.Context, companyID, membershipID uuid.UUID) (settings.Membership, bool, error) {
	m, err := scanMembershipRow(r.db.pool.QueryRow(ctx, `
		SELECT m.id, u.id, u.full_name, u.email, m.role, m.approval_limit, m.is_active,
		       COALESCE((SELECT array_agg(b.name ORDER BY b.name) FROM branches b WHERE b.id = ANY(m.branch_scope)), '{}')
		FROM memberships m
		JOIN users u ON u.id = m.user_id
		WHERE m.company_id = $1 AND m.id = $2`, companyID, membershipID))
	if errors.Is(err, pgx.ErrNoRows) {
		return settings.Membership{}, false, nil
	}
	return m, err == nil, err
}

func scanMembershipRow(rw row) (settings.Membership, error) {
	var m settings.Membership
	var limit *decimal.Decimal
	err := rw.Scan(&m.ID, &m.UserID, &m.UserName, &m.UserEmail, &m.Role, &limit, &m.IsActive, &m.BranchNames)
	m.ApprovalLimit = limit
	return m, err
}

func (r *settingsRepo) SetApprovalLimit(ctx context.Context, companyID, membershipID uuid.UUID, limit *decimal.Decimal) error {
	_, err := r.db.pool.Exec(ctx, `
		UPDATE memberships SET approval_limit = $3 WHERE company_id = $1 AND id = $2`,
		companyID, membershipID, limit)
	return err
}
