package storage

import (
	"context"

	"github.com/jackc/pgx/v5"

	"github.com/ledgerlink/branchledger/backend/internal/sync"
	"github.com/ledgerlink/branchledger/backend/internal/tenancy"
)

// ChangesSince implements sync.PullStore: authorised changes after cursor,
// scoped to the caller's company and (when the role is branch-restricted)
// branches. Ordered by cursor for stable keyset pagination.
func (db *DB) ChangesSince(ctx context.Context, scope tenancy.Scope, cursor int64, limit int) ([]sync.Change, error) {
	var rows pgx.Rows
	var err error

	if len(scope.BranchScope) == 0 {
		rows, err = db.pool.Query(ctx, `
			SELECT cursor, branch_id, record_type, record_id, record_version, change_kind
			FROM sync_changes
			WHERE company_id = $1 AND cursor > $2
			ORDER BY cursor ASC LIMIT $3`, scope.CompanyID, cursor, limit)
	} else {
		rows, err = db.pool.Query(ctx, `
			SELECT cursor, branch_id, record_type, record_id, record_version, change_kind
			FROM sync_changes
			WHERE company_id = $1 AND cursor > $2 AND branch_id = ANY($3)
			ORDER BY cursor ASC LIMIT $4`, scope.CompanyID, cursor, scope.BranchScope, limit)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var changes []sync.Change
	for rows.Next() {
		var c sync.Change
		if err := rows.Scan(&c.Cursor, &c.BranchID, &c.RecordType, &c.RecordID, &c.RecordVersion, &c.ChangeKind); err != nil {
			return nil, err
		}
		changes = append(changes, c)
	}
	return changes, rows.Err()
}
