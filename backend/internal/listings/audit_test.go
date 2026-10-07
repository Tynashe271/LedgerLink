package listings

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/ledgerlink/branchledger/backend/internal/tenancy"
)

type fakeAuditStore struct {
	events []AuditEvent
}

func (f *fakeAuditStore) ListTransactions(ctx context.Context, companyID uuid.UUID, allowed []uuid.UUID, fl TransactionFilter) ([]TransactionSummary, error) {
	return nil, nil
}
func (f *fakeAuditStore) ListPendingApprovals(ctx context.Context, companyID uuid.UUID, allowed []uuid.UUID) ([]ApprovalSummary, error) {
	return nil, nil
}
func (f *fakeAuditStore) ListOpenTransfers(ctx context.Context, companyID uuid.UUID, allowed []uuid.UUID) ([]TransferSummary, error) {
	return nil, nil
}
func (f *fakeAuditStore) ListStockPositions(ctx context.Context, companyID uuid.UUID, allowed []uuid.UUID) ([]StockPositionSummary, error) {
	return nil, nil
}
func (f *fakeAuditStore) ListAuditEvents(ctx context.Context, companyID uuid.UUID, from, to time.Time, limit int) ([]AuditEvent, error) {
	return f.events, nil
}

var _ Store = (*fakeAuditStore)(nil)

func TestAuditEventsRestrictedToCompanyWideReadRoles(t *testing.T) {
	store := &fakeAuditStore{events: []AuditEvent{{ID: 1, EventType: "login"}}}
	svc := NewService(store)
	ctx := context.Background()
	from, to := time.Now().AddDate(0, 0, -30), time.Now()

	for _, role := range []tenancy.Role{tenancy.RoleGeneralManager, tenancy.RoleBranchManager, tenancy.RoleStaff} {
		scope := tenancy.Scope{CompanyID: uuid.New(), Role: role}
		if _, err := svc.AuditEvents(ctx, scope, from, to, 100); !errors.Is(err, ErrCompanyWideReadRequired) {
			t.Errorf("role %s: expected ErrCompanyWideReadRequired, got %v", role, err)
		}
	}

	for _, role := range []tenancy.Role{tenancy.RoleOwner, tenancy.RoleAccountant} {
		scope := tenancy.Scope{CompanyID: uuid.New(), Role: role}
		events, err := svc.AuditEvents(ctx, scope, from, to, 100)
		if err != nil {
			t.Errorf("role %s: unexpected error %v", role, err)
		}
		if len(events) != 1 {
			t.Errorf("role %s: expected the fake store's 1 event to pass through, got %d", role, len(events))
		}
	}
}
