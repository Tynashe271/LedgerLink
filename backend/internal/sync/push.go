// Package sync implements the browser-outbox <-> server protocol from
// BranchLedger_Architecture.pdf ("Synchronisation sequence and conflicts"):
// bounded batch push with idempotent, independently-succeeding items, and
// cursor-based pull of authorised changes.
package sync

import (
	"context"

	"github.com/google/uuid"

	"github.com/ledgerlink/branchledger/backend/internal/accounting"
	"github.com/ledgerlink/branchledger/backend/internal/tenancy"
)

// MaxBatchSize bounds a single push request. The architecture proposes 100
// operations per sync batch as a configurable default (System Documentation
// section 9.3); oversized batches fail predictably rather than partially
// posting, per launch control #2 ("API quotas").
const MaxBatchSize = 100

// PushItem is one queued offline operation as submitted by the client outbox.
// DocumentType distinguishes which posting rule accounting.Service.Post should
// apply; today that covers the transaction-shaped commands (sale, purchase,
// expense, receipt, payment). Closes, approval decisions and transfer receipts
// follow the same per-item idempotent pattern but are posted through their own
// services (daily close, approvals, transfers), not shown here.
type PushItem struct {
	accounting.PostInput
}

// PushItemResult is returned per item so "Batch items can succeed
// independently" — one rejected or conflicting item never blocks the rest of
// the batch, and each accepted item remains a single atomic posting.
type PushItemResult struct {
	OperationID   uuid.UUID
	Accepted      bool
	TransactionID uuid.UUID
	Version       int
	ErrorCode     string // "conflict" | "rejected" | "" (empty when Accepted)
	Status        string // "posted" | "awaiting_approval" (empty when not Accepted)
}

// PushResult is the full response to POST /api/v1/sync/push.
type PushResult struct {
	Items []PushItemResult
}

// Pusher processes a push batch against the accounting posting boundary.
type Pusher struct {
	accountingSvc *accounting.Service
}

func NewPusher(accountingSvc *accounting.Service) *Pusher {
	return &Pusher{accountingSvc: accountingSvc}
}

// Push processes each item through the accounting transaction boundary in
// turn. Each item gets its own database transaction (via Service.Post), so one
// item's rollback never affects another's commit — matching "each accepted
// financial operation remains atomic" while allowing partial batch success.
func (p *Pusher) Push(ctx context.Context, scope tenancy.Scope, items []PushItem) (PushResult, error) {
	if len(items) > MaxBatchSize {
		return PushResult{}, ErrBatchTooLarge
	}

	results := make([]PushItemResult, 0, len(items))
	for _, item := range items {
		opID := item.PostInput.OperationID
		res, err := p.accountingSvc.Post(ctx, scope, item.PostInput)
		switch {
		case err == accounting.ErrConflict:
			results = append(results, PushItemResult{OperationID: opID, ErrorCode: "conflict"})
		case err == accounting.ErrOutOfScope:
			results = append(results, PushItemResult{OperationID: opID, ErrorCode: "out_of_scope"})
		case err != nil:
			results = append(results, PushItemResult{OperationID: opID, ErrorCode: "internal_error"})
		case !res.Accepted:
			results = append(results, PushItemResult{OperationID: opID, ErrorCode: res.ErrorCode})
		default:
			results = append(results, PushItemResult{
				OperationID: opID, Accepted: true,
				TransactionID: res.TransactionID, Version: res.Version, Status: res.Status,
			})
		}
	}
	return PushResult{Items: results}, nil
}

// ErrBatchTooLarge is returned when a push batch exceeds MaxBatchSize.
var ErrBatchTooLarge = &batchTooLargeError{}

type batchTooLargeError struct{}

func (*batchTooLargeError) Error() string { return "sync: push batch exceeds maximum size" }
