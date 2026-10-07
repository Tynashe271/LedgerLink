package settings

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/ledgerlink/branchledger/backend/internal/tenancy"
)

type fakeStore struct {
	profile     CompanyProfile
	accounts    map[string]Account // keyed by code
	devices     map[uuid.UUID]Device
	memberships map[uuid.UUID]Membership
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		accounts:    map[string]Account{},
		devices:     map[uuid.UUID]Device{},
		memberships: map[uuid.UUID]Membership{},
	}
}

func (f *fakeStore) GetCompanyProfile(ctx context.Context, companyID uuid.UUID) (CompanyProfile, bool, error) {
	return f.profile, true, nil
}
func (f *fakeStore) UpdateCompanyProfile(ctx context.Context, companyID uuid.UUID, in UpdateCompanyInput) error {
	f.profile.Name = in.Name
	f.profile.PrimaryCategory = in.PrimaryCategory
	f.profile.Timezone = in.Timezone
	f.profile.FinancialYearStart = in.FinancialYearStart
	return nil
}

func (f *fakeStore) ListAccounts(ctx context.Context, companyID uuid.UUID) ([]Account, error) {
	var out []Account
	for _, a := range f.accounts {
		out = append(out, a)
	}
	return out, nil
}
func (f *fakeStore) AccountCodeExists(ctx context.Context, companyID uuid.UUID, code string) (bool, error) {
	_, ok := f.accounts[code]
	return ok, nil
}
func (f *fakeStore) InsertAccount(ctx context.Context, companyID uuid.UUID, in AddAccountInput) (uuid.UUID, error) {
	id := uuid.New()
	f.accounts[in.Code] = Account{ID: id, Code: in.Code, Name: in.Name, AccountType: in.AccountType, IsCashLike: in.IsCashLike, IsActive: true}
	return id, nil
}

func (f *fakeStore) ListDevices(ctx context.Context, companyID uuid.UUID) ([]Device, error) {
	var out []Device
	for _, d := range f.devices {
		out = append(out, d)
	}
	return out, nil
}
func (f *fakeStore) GetDevice(ctx context.Context, companyID, deviceID uuid.UUID) (Device, bool, error) {
	d, ok := f.devices[deviceID]
	return d, ok, nil
}
func (f *fakeStore) RevokeDevice(ctx context.Context, companyID, deviceID uuid.UUID) error {
	d := f.devices[deviceID]
	now := time.Now().UTC()
	d.RevokedAt = &now
	d.IsOfflineWriter = false
	f.devices[deviceID] = d
	return nil
}
func (f *fakeStore) SetOfflineWriter(ctx context.Context, companyID, deviceID uuid.UUID) error {
	target := f.devices[deviceID]
	for id, d := range f.devices {
		if d.BranchID == target.BranchID {
			d.IsOfflineWriter = false
			f.devices[id] = d
		}
	}
	target.IsOfflineWriter = true
	f.devices[deviceID] = target
	return nil
}

func (f *fakeStore) ListMemberships(ctx context.Context, companyID uuid.UUID) ([]Membership, error) {
	var out []Membership
	for _, m := range f.memberships {
		out = append(out, m)
	}
	return out, nil
}
func (f *fakeStore) GetMembership(ctx context.Context, companyID, membershipID uuid.UUID) (Membership, bool, error) {
	m, ok := f.memberships[membershipID]
	return m, ok, nil
}
func (f *fakeStore) SetApprovalLimit(ctx context.Context, companyID, membershipID uuid.UUID, limit *decimal.Decimal) error {
	m := f.memberships[membershipID]
	m.ApprovalLimit = limit
	f.memberships[membershipID] = m
	return nil
}

var _ Store = (*fakeStore)(nil)

func d(s string) decimal.Decimal {
	v, err := decimal.NewFromString(s)
	if err != nil {
		panic(err)
	}
	return v
}

func scopeWith(role tenancy.Role, delegations tenancy.Delegations) tenancy.Scope {
	return tenancy.Scope{CompanyID: uuid.New(), UserID: uuid.New(), Role: role, Delegations: delegations}
}

func TestUpdateCompanyOnlyOwner(t *testing.T) {
	store := newFakeStore()
	svc := NewService(store)

	for _, role := range []tenancy.Role{tenancy.RoleGeneralManager, tenancy.RoleAccountant, tenancy.RoleBranchManager, tenancy.RoleStaff} {
		if err := svc.UpdateCompany(context.Background(), scopeWith(role, nil), UpdateCompanyInput{Name: "New name"}); !errors.Is(err, ErrForbidden) {
			t.Errorf("role %s: expected ErrForbidden, got %v", role, err)
		}
	}
	if err := svc.UpdateCompany(context.Background(), scopeWith(tenancy.RoleOwner, nil), UpdateCompanyInput{Name: "New name"}); err != nil {
		t.Fatalf("owner UpdateCompany: %v", err)
	}
	if store.profile.Name != "New name" {
		t.Fatalf("company name not updated: %+v", store.profile)
	}
}

func TestAddAccountPermissions(t *testing.T) {
	store := newFakeStore()
	svc := NewService(store)
	in := AddAccountInput{Code: "7000", Name: "Donations Received", AccountType: "income"}

	// Accountant is allowed outright.
	if _, err := svc.AddAccount(context.Background(), scopeWith(tenancy.RoleAccountant, nil), in); err != nil {
		t.Fatalf("accountant AddAccount: %v", err)
	}

	// Owner needs an explicit delegation per the matrix.
	if _, err := svc.AddAccount(context.Background(), scopeWith(tenancy.RoleOwner, nil), AddAccountInput{Code: "7100", Name: "x", AccountType: "income"}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("owner without delegation: expected ErrForbidden, got %v", err)
	}
	delegated := scopeWith(tenancy.RoleOwner, tenancy.Delegations{tenancy.ActionChangeChartOfAccounts: true})
	if _, err := svc.AddAccount(context.Background(), delegated, AddAccountInput{Code: "7100", Name: "x", AccountType: "income"}); err != nil {
		t.Fatalf("owner with delegation: %v", err)
	}

	// Branch manager and staff are never allowed.
	for _, role := range []tenancy.Role{tenancy.RoleBranchManager, tenancy.RoleStaff} {
		if _, err := svc.AddAccount(context.Background(), scopeWith(role, nil), AddAccountInput{Code: "7200", Name: "x", AccountType: "income"}); !errors.Is(err, ErrForbidden) {
			t.Errorf("role %s: expected ErrForbidden, got %v", role, err)
		}
	}
}

func TestAddAccountRejectsDuplicateCode(t *testing.T) {
	store := newFakeStore()
	svc := NewService(store)
	in := AddAccountInput{Code: "7000", Name: "Donations Received", AccountType: "income"}
	scope := scopeWith(tenancy.RoleAccountant, nil)

	if _, err := svc.AddAccount(context.Background(), scope, in); err != nil {
		t.Fatalf("first AddAccount: %v", err)
	}
	if _, err := svc.AddAccount(context.Background(), scope, in); !errors.Is(err, ErrDuplicateCode) {
		t.Fatalf("expected ErrDuplicateCode, got %v", err)
	}
}

func TestDeviceActionsRequireManageUsers(t *testing.T) {
	store := newFakeStore()
	svc := NewService(store)
	deviceID := uuid.New()
	branchID := uuid.New()
	store.devices[deviceID] = Device{ID: deviceID, BranchID: branchID, Label: "Till 1"}

	if err := svc.RevokeDevice(context.Background(), scopeWith(tenancy.RoleStaff, nil), deviceID); !errors.Is(err, ErrForbidden) {
		t.Fatalf("staff RevokeDevice: expected ErrForbidden, got %v", err)
	}
	if err := svc.SetOfflineWriter(context.Background(), scopeWith(tenancy.RoleAccountant, nil), deviceID); !errors.Is(err, ErrForbidden) {
		t.Fatalf("accountant SetOfflineWriter: expected ErrForbidden, got %v", err)
	}
	if err := svc.SetOfflineWriter(context.Background(), scopeWith(tenancy.RoleOwner, nil), deviceID); err != nil {
		t.Fatalf("owner SetOfflineWriter: %v", err)
	}
	if !store.devices[deviceID].IsOfflineWriter {
		t.Fatalf("device was not set as offline writer: %+v", store.devices[deviceID])
	}
	if err := svc.RevokeDevice(context.Background(), scopeWith(tenancy.RoleOwner, nil), deviceID); err != nil {
		t.Fatalf("owner RevokeDevice: %v", err)
	}
	if store.devices[deviceID].RevokedAt == nil || store.devices[deviceID].IsOfflineWriter {
		t.Fatalf("device was not revoked / offline-writer not cleared: %+v", store.devices[deviceID])
	}
}

func TestSetApprovalLimitPermissions(t *testing.T) {
	store := newFakeStore()
	svc := NewService(store)
	membershipID := uuid.New()
	store.memberships[membershipID] = Membership{ID: membershipID, Role: "branch_manager"}
	limit := d("500")

	// Accountant and branch_manager (even though ActionManageUsers grants
	// branch_manager "branchOnly" generically) are denied here specifically —
	// see SetApprovalLimit's doc comment for why limits don't get that path.
	for _, role := range []tenancy.Role{tenancy.RoleAccountant, tenancy.RoleBranchManager, tenancy.RoleStaff} {
		if err := svc.SetApprovalLimit(context.Background(), scopeWith(role, nil), membershipID, &limit); !errors.Is(err, ErrForbidden) {
			t.Errorf("role %s: expected ErrForbidden, got %v", role, err)
		}
	}

	// general_manager needs an explicit delegation.
	if err := svc.SetApprovalLimit(context.Background(), scopeWith(tenancy.RoleGeneralManager, nil), membershipID, &limit); !errors.Is(err, ErrForbidden) {
		t.Fatalf("general_manager without delegation: expected ErrForbidden, got %v", err)
	}
	delegated := scopeWith(tenancy.RoleGeneralManager, tenancy.Delegations{tenancy.ActionManageUsers: true})
	if err := svc.SetApprovalLimit(context.Background(), delegated, membershipID, &limit); err != nil {
		t.Fatalf("general_manager with delegation: %v", err)
	}

	if err := svc.SetApprovalLimit(context.Background(), scopeWith(tenancy.RoleOwner, nil), membershipID, &limit); err != nil {
		t.Fatalf("owner SetApprovalLimit: %v", err)
	}
	if !store.memberships[membershipID].ApprovalLimit.Equal(limit) {
		t.Fatalf("approval limit not updated: %+v", store.memberships[membershipID])
	}
}
