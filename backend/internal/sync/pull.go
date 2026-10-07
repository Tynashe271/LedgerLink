package sync

import (
	"context"

	"github.com/google/uuid"

	"github.com/ledgerlink/branchledger/backend/internal/tenancy"
)

// Change is one row of the authorised change feed, matching `sync_changes`.
type Change struct {
	Cursor         int64
	BranchID       uuid.UUID
	RecordType     string
	RecordID       uuid.UUID
	RecordVersion  int
	ChangeKind     string // created | updated | reversed
}

// PullResult is the response to GET /api/v1/sync/pull?cursor=...
type PullResult struct {
	Changes    []Change
	NextCursor string // opaque to the client; echoed back verbatim next call
}

// PullStore is the read side sync needs: authorised changes since a cursor,
// scoped to the caller's company and branches.
type PullStore interface {
	ChangesSince(ctx context.Context, scope tenancy.Scope, cursor int64, limit int) ([]Change, error)
}

// Puller serves GET /api/v1/sync/pull.
type Puller struct {
	store PullStore
}

func NewPuller(store PullStore) *Puller {
	return &Puller{store: store}
}

// DefaultPullLimit bounds one pull page, matching the architecture's keyset
// pagination requirement for long record lists.
const DefaultPullLimit = 500

// Pull returns authorised changes after cursor (0 means "from the
// beginning"), already filtered to the caller's branch scope by the store
// implementation, plus the next cursor to request on the following pull.
// Applying the batch and the new cursor together, atomically, is the client's
// responsibility (architecture step 7: "Apply change batch and cursor
// atomically").
func (p *Puller) Pull(ctx context.Context, scope tenancy.Scope, cursor int64) (PullResult, error) {
	changes, err := p.store.ChangesSince(ctx, scope, cursor, DefaultPullLimit)
	if err != nil {
		return PullResult{}, err
	}
	next := cursor
	if len(changes) > 0 {
		next = changes[len(changes)-1].Cursor
	}
	return PullResult{Changes: changes, NextCursor: encodeCursor(next)}, nil
}
