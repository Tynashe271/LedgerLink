package httpapi

import (
	"context"

	"github.com/google/uuid"

	"github.com/ledgerlink/branchledger/backend/internal/accounting"
	"github.com/ledgerlink/branchledger/backend/internal/sync"
	"github.com/ledgerlink/branchledger/backend/internal/tenancy"
)

// syncPushItem pairs a raw operation ID with its decoded PostInput, or a
// decode failure, so pushAll can report a stable per-item error without
// losing track of which operation ID it belongs to.
type syncPushItem struct {
	operationID uuid.UUID
	input       accounting.PostInput
	decodeErr   bool
}

func pushAll(ctx context.Context, deps Deps, scope tenancy.Scope, items []syncPushItem) (sync.PushResult, error) {
	if len(items) > sync.MaxBatchSize {
		return sync.PushResult{}, sync.ErrBatchTooLarge
	}

	pushItems := make([]sync.PushItem, 0, len(items))
	preResults := make([]sync.PushItemResult, 0)
	for _, it := range items {
		if it.decodeErr {
			preResults = append(preResults, sync.PushItemResult{OperationID: it.operationID, ErrorCode: "malformed_item"})
			continue
		}
		pushItems = append(pushItems, sync.PushItem{PostInput: it.input})
	}

	result, err := deps.Pusher.Push(ctx, scope, pushItems)
	if err != nil {
		return sync.PushResult{}, err
	}
	result.Items = append(preResults, result.Items...)
	return result, nil
}

func decodeCursorParamOpaque(s string) (int64, error) {
	return sync.DecodeCursor(s)
}
