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
	profile       CompanyProfile
	branches      map[uuid.UUID]Branch
	accounts      map[string]Account // keyed by code
	devices       map[uuid.UUID]Device
	memberships   map[uuid.UUID]Membership
	validUsers    map[uuid.UUID]bool
	validBranches map[uuid.UUID]bool
	auditEvents   []string
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		branches:      map[uuid.UUID]Branch{},
		accounts:      map[string]Account{},
		devices:       map[uuid.UUID]Device{},
		validUsers:    map[uuid.UUID]bool{},
		validBranches: map[uuid.UUID]bool{},
		memberships:   map[uuid.UUID]Membership{},
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

func (f *fakeStore) ListBranches(ctx context.Context, companyID uuid.UUID) ([]Branch, error) {
	var out []Branch
	for _, b := range f.branches {
		out = append(out, b)
	}
	return out, nil
}
func (f *fakeStore) BranchCodeExists(ctx context.Context, companyID uuid.UUID, code string) (bool, error) {
	for _, b := range f.branches {
		if b.Code == code {
			return true, nil
		}
	}
	return false, nil
}
func (f *fakeStore) HasMainBranch(ctx context.Context, companyID uuid.UUID) (bool, error) {
	for _, b := range f.branches {
		if b.IsMainBranch {
			return true, nil
		}
	}
	return false, nil
}
func (f *fakeStore) InsertBranch(ctx context.Context, companyID uuid.UUID, in AddBranchInput) (uuid.UUID, error) {
	id := uuid.New()
	f.branches[id] = Branch{ID: id, Name: in.Name, Code: in.Code, Category: in.Category, IsMainBranch: in.IsMainBranch, Status: "active"}
	return id, nil
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
func (f *fakeStore) InsertDevice(ctx context.Context, companyID uuid.UUID, in EnrollDeviceInput, leaseExpiresAt time.Time) (Device, error) {
	d := Device{ID: uuid.New(), BranchID: in.BranchID, UserID: in.UserID, Label: in.Label, LeaseExpiresAt: leaseExpiresAt}
	f.devices[d.ID] = d
	return d, nil
}
func (f *fakeStore) UserBelongsToCompany(ctx context.Context, companyID, userID uuid.UUID) (bool, error) {
	return f.validUsers[userID], nil
}
func (f *fakeStore) BranchBelongsToCompany(ctx context.Context, companyID, branchID uuid.UUID) (bool, error) {
	return f.validBranches[branchID], nil
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
func (f *fakeStore) CreateUserAndMembership(ctx context.Context, companyID uuid.UUID, in CreateUserInput, passwordHash string) (Membership, error) {
	for _, m := range f.memberships {
		if m.UserEmail == in.Email {
			return Membership{}, ErrDuplicateEmail
		}
	}
	id := uuid.New()
	var branchNames []string
	for range in.BranchScope {
		branchNames = append(branchNames, "branch")
	}
	m := Membership{ID: id, UserID: uuid.New(), UserName: in.FullName, UserEmail: in.Email, Role: in.Role, BranchNames: branchNames, ApprovalLimit: in.ApprovalLimit, IsActive: true}
	f.memberships[id] = m
	return m, nil
}
func (f *fakeStore) RecordAuditEvent(ctx context.Context, companyID, actorUserID uuid.UUID, eventType, recordType string, recordID uuid.UUID, details map[string]any) error {
	f.auditEvents = append(f.auditEvents, eventType)
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

func TestEnrollDeviceValidatesTenancyAndPermission(t *testing.T) {
	store := newFakeStore()
	svc := NewService(store)
	branchID, userID := uuid.New(), uuid.New()
	store.validBranches[branchID] = true
	store.validUsers[userID] = true

	if _, err := svc.EnrollDevice(context.Background(), scopeWith(tenancy.RoleStaff, nil), EnrollDeviceInput{BranchID: branchID, UserID: userID, Label: "Till 1"}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("staff: expected ErrForbidden, got %v", err)
	}

	foreignBranch := uuid.New() // not in store.validBranches
	if _, err := svc.EnrollDevice(context.Background(), scopeWith(tenancy.RoleOwner, nil), EnrollDeviceInput{BranchID: foreignBranch, UserID: userID, Label: "Till 1"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign branch: expected ErrNotFound, got %v", err)
	}

	foreignUser := uuid.New() // not in store.validUsers
	if _, err := svc.EnrollDevice(context.Background(), scopeWith(tenancy.RoleOwner, nil), EnrollDeviceInput{BranchID: branchID, UserID: foreignUser, Label: "Till 1"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign user: expected ErrNotFound, got %v", err)
	}

	dv, err := svc.EnrollDevice(context.Background(), scopeWith(tenancy.RoleOwner, nil), EnrollDeviceInput{BranchID: branchID, UserID: userID, Label: "Till 1"})
	if err != nil {
		t.Fatalf("owner EnrollDevice: %v", err)
	}
	if dv.Label != "Till 1" || dv.LeaseExpiresAt.Before(time.Now()) {
		t.Fatalf("enrolled device looks wrong: %+v", dv)
	}
}

func TestAddBranchOnlyOwnerAndOneMain(t *testing.T) {
	store := newFakeStore()
	svc := NewService(store)

	if _, err := svc.AddBranch(context.Background(), scopeWith(tenancy.RoleAccountant, nil), AddBranchInput{Name: "North", Code: "NORTH", Category: "retail_wholesale"}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("accountant: expected ErrForbidden, got %v", err)
	}

	main, err := svc.AddBranch(context.Background(), scopeWith(tenancy.RoleOwner, nil), AddBranchInput{Name: "Main", Code: "MAIN", Category: "retail_wholesale", IsMainBranch: true})
	if err != nil {
		t.Fatalf("owner AddBranch (main): %v", err)
	}
	if !main.IsMainBranch {
		t.Fatalf("expected IsMainBranch=true: %+v", main)
	}

	if _, err := svc.AddBranch(context.Background(), scopeWith(tenancy.RoleOwner, nil), AddBranchInput{Name: "Second main", Code: "MAIN2", Category: "retail_wholesale", IsMainBranch: true}); !errors.Is(err, ErrMainBranchExists) {
		t.Fatalf("second main branch: expected ErrMainBranchExists, got %v", err)
	}

	if _, err := svc.AddBranch(context.Background(), scopeWith(tenancy.RoleOwner, nil), AddBranchInput{Name: "Dup code", Code: "MAIN", Category: "retail_wholesale"}); !errors.Is(err, ErrDuplicateBranchCode) {
		t.Fatalf("duplicate code: expected ErrDuplicateBranchCode, got %v", err)
	}

	sub, err := svc.AddBranch(context.Background(), scopeWith(tenancy.RoleOwner, nil), AddBranchInput{Name: "Sub", Code: "SUB", Category: "retail_wholesale"})
	if err != nil {
		t.Fatalf("owner AddBranch (sub): %v", err)
	}
	if sub.IsMainBranch {
		t.Fatalf("expected IsMainBranch=false: %+v", sub)
	}
}

func TestCreateUserValidatesRoleBranchAndPermission(t *testing.T) {
	store := newFakeStore()
	svc := NewService(store)
	branchID := uuid.New()
	store.validBranches[branchID] = true

	in := CreateUserInput{Email: "new@example.com", FullName: "New Person", Role: "staff", BranchScope: []uuid.UUID{branchID}}

	if _, err := svc.CreateUser(context.Background(), scopeWith(tenancy.RoleAccountant, nil), in); !errors.Is(err, ErrForbidden) {
		t.Fatalf("accountant: expected ErrForbidden, got %v", err)
	}

	badRole := in
	badRole.Role = "platform_admin"
	if _, err := svc.CreateUser(context.Background(), scopeWith(tenancy.RoleOwner, nil), badRole); !errors.Is(err, ErrInvalidRole) {
		t.Fatalf("platform_admin role: expected ErrInvalidRole, got %v", err)
	}

	badBranch := in
	badBranch.BranchScope = []uuid.UUID{uuid.New()} // not in store.validBranches
	if _, err := svc.CreateUser(context.Background(), scopeWith(tenancy.RoleOwner, nil), badBranch); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign branch: expected ErrNotFound, got %v", err)
	}

	m, err := svc.CreateUser(context.Background(), scopeWith(tenancy.RoleOwner, nil), in)
	if err != nil {
		t.Fatalf("owner CreateUser: %v", err)
	}
	if m.UserEmail != "new@example.com" || m.Role != "staff" {
		t.Fatalf("created membership looks wrong: %+v", m)
	}

	if _, err := svc.CreateUser(context.Background(), scopeWith(tenancy.RoleOwner, nil), in); !errors.Is(err, ErrDuplicateEmail) {
		t.Fatalf("duplicate email: expected ErrDuplicateEmail, got %v", err)
	}
}
