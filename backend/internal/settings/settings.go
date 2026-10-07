// Package settings implements the Settings screen (System Documentation
// 5.2: "Company, accounts, devices and limits" / "Authorised changes with
// audit"): the company profile, the chart of accounts, enrolled devices and
// per-user approval limits. Every write here is an administrative change,
// not an accounting posting — it never touches journals or transactions.
package settings

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/ledgerlink/branchledger/backend/internal/tenancy"
)

var (
	ErrForbidden     = errors.New("settings: caller is not authorised for this change")
	ErrNotFound      = errors.New("settings: not found")
	ErrDuplicateCode = errors.New("settings: an account with this code already exists")
)

// --- Company profile ---------------------------------------------------------

// CompanyProfile mirrors the editable fields of `companies`. ReportingCurrency
// is deliberately not editable here: changing it after journals have posted
// would silently invalidate every historical reporting-currency amount —
// architecture section 5.9 requires a reviewed conversion, not a bare field
// edit, and that conversion isn't implemented.
type CompanyProfile struct {
	ID                 uuid.UUID
	Name               string
	PrimaryCategory    string
	ReportingCurrency  string
	Timezone           string
	FinancialYearStart time.Time
	Status             string
}

// UpdateCompanyInput is what Service.UpdateCompany accepts.
type UpdateCompanyInput struct {
	Name               string
	PrimaryCategory    string
	Timezone           string
	FinancialYearStart time.Time
}

// --- Chart of accounts --------------------------------------------------------

// Account mirrors one row of `accounts`.
type Account struct {
	ID          uuid.UUID
	Code        string
	Name        string
	AccountType string
	IsCashLike  bool
	IsActive    bool
}

// AddAccountInput is what Service.AddAccount accepts. There is no edit or
// delete here — a posted journal's account_id must never be left dangling,
// and a code already embedded in storage.txImpl.GetChartAccounts's byCode
// map must never silently change meaning (see that function's comment);
// only adding a brand new account is safe without a reviewed migration.
type AddAccountInput struct {
	Code        string
	Name        string
	AccountType string // asset | liability | equity | income | expense
	IsCashLike  bool
}

// --- Devices -------------------------------------------------------------

// Device mirrors one row of `devices`, joined for display.
type Device struct {
	ID              uuid.UUID
	BranchID        uuid.UUID
	BranchName      string
	UserID          uuid.UUID
	UserName        string
	Label           string
	IsOfflineWriter bool
	EnrolledAt      time.Time
	LeaseExpiresAt  time.Time
	RevokedAt       *time.Time
	LastSyncAt      *time.Time
}

// --- Memberships / approval limits --------------------------------------------

// Membership mirrors one row of `memberships`, joined for display. Role and
// branch scope are read-only here — see Service.SetApprovalLimit's comment
// for why only the limit itself is editable in this pass. BranchNames is
// empty for an unrestricted membership ("All branches"), same convention as
// an empty tenancy.Scope.BranchScope.
type Membership struct {
	ID            uuid.UUID
	UserID        uuid.UUID
	UserName      string
	UserEmail     string
	Role          string
	BranchNames   []string
	ApprovalLimit *decimal.Decimal
	IsActive      bool
}

// Store is the persistence this service needs from Postgres.
type Store interface {
	GetCompanyProfile(ctx context.Context, companyID uuid.UUID) (CompanyProfile, bool, error)
	UpdateCompanyProfile(ctx context.Context, companyID uuid.UUID, in UpdateCompanyInput) error

	ListAccounts(ctx context.Context, companyID uuid.UUID) ([]Account, error)
	AccountCodeExists(ctx context.Context, companyID uuid.UUID, code string) (bool, error)
	InsertAccount(ctx context.Context, companyID uuid.UUID, in AddAccountInput) (uuid.UUID, error)

	ListDevices(ctx context.Context, companyID uuid.UUID) ([]Device, error)
	GetDevice(ctx context.Context, companyID, deviceID uuid.UUID) (Device, bool, error)
	RevokeDevice(ctx context.Context, companyID, deviceID uuid.UUID) error
	// SetOfflineWriter designates deviceID the sole offline writer for its
	// branch, clearing the flag on every other device at that branch in the
	// same statement — architecture: "one designated offline writer per
	// branch," never two at once even momentarily.
	SetOfflineWriter(ctx context.Context, companyID, deviceID uuid.UUID) error

	ListMemberships(ctx context.Context, companyID uuid.UUID) ([]Membership, error)
	GetMembership(ctx context.Context, companyID, membershipID uuid.UUID) (Membership, bool, error)
	SetApprovalLimit(ctx context.Context, companyID, membershipID uuid.UUID, limit *decimal.Decimal) error
}

type Service struct {
	store Store
}

func NewService(store Store) *Service { return &Service{store: store} }

// canManageCompany is deliberately a plain role check, not a
// tenancy.Can(...) matrix lookup: section 4.6's matrix has no entry for
// company-profile edits (as opposed to user/device administration or the
// chart of accounts, which it does cover), and company identity/fiscal
// configuration is foundational enough that inventing an ungrounded
// permission for it would be less honest than a direct, visible role check.
func canManageCompany(role tenancy.Role) bool {
	return role == tenancy.RoleOwner
}

func (s *Service) CompanyProfile(ctx context.Context, scope tenancy.Scope) (CompanyProfile, error) {
	profile, ok, err := s.store.GetCompanyProfile(ctx, scope.CompanyID)
	if err != nil {
		return CompanyProfile{}, err
	}
	if !ok {
		return CompanyProfile{}, ErrNotFound
	}
	return profile, nil
}

func (s *Service) UpdateCompany(ctx context.Context, scope tenancy.Scope, in UpdateCompanyInput) error {
	if !canManageCompany(scope.Role) {
		return ErrForbidden
	}
	return s.store.UpdateCompanyProfile(ctx, scope.CompanyID, in)
}

func (s *Service) Accounts(ctx context.Context, scope tenancy.Scope) ([]Account, error) {
	return s.store.ListAccounts(ctx, scope.CompanyID)
}

// AddAccount appends a new account to the chart. Gated by
// ActionChangeChartOfAccounts (Owner needs an explicit delegation,
// Accountant is allowed outright — section 4.6's matrix exactly).
func (s *Service) AddAccount(ctx context.Context, scope tenancy.Scope, in AddAccountInput) (Account, error) {
	if !tenancy.Can(tenancy.ActionChangeChartOfAccounts, scope.Role, scope.Delegations, true) {
		return Account{}, ErrForbidden
	}
	exists, err := s.store.AccountCodeExists(ctx, scope.CompanyID, in.Code)
	if err != nil {
		return Account{}, err
	}
	if exists {
		return Account{}, ErrDuplicateCode
	}
	id, err := s.store.InsertAccount(ctx, scope.CompanyID, in)
	if err != nil {
		return Account{}, err
	}
	return Account{ID: id, Code: in.Code, Name: in.Name, AccountType: in.AccountType, IsCashLike: in.IsCashLike, IsActive: true}, nil
}

func (s *Service) Devices(ctx context.Context, scope tenancy.Scope) ([]Device, error) {
	return s.store.ListDevices(ctx, scope.CompanyID)
}

// RevokeDevice and SetOfflineWriter are both gated by ActionManageUsers —
// device administration is user/membership administration, same matrix
// entry (Owner allowed, general_manager needs a delegation, branch_manager
// may act within their own branch per the matrix's branchOnly grant; this
// service passes actingOnOwnBranch=true uniformly, the same simplification
// handleCreateExport already makes for ActionExportFinancialData, rather
// than resolving a specific device's branch against the caller's scope).
func (s *Service) RevokeDevice(ctx context.Context, scope tenancy.Scope, deviceID uuid.UUID) error {
	if !tenancy.Can(tenancy.ActionManageUsers, scope.Role, scope.Delegations, true) {
		return ErrForbidden
	}
	if _, ok, err := s.store.GetDevice(ctx, scope.CompanyID, deviceID); err != nil {
		return err
	} else if !ok {
		return ErrNotFound
	}
	return s.store.RevokeDevice(ctx, scope.CompanyID, deviceID)
}

func (s *Service) SetOfflineWriter(ctx context.Context, scope tenancy.Scope, deviceID uuid.UUID) error {
	if !tenancy.Can(tenancy.ActionManageUsers, scope.Role, scope.Delegations, true) {
		return ErrForbidden
	}
	if _, ok, err := s.store.GetDevice(ctx, scope.CompanyID, deviceID); err != nil {
		return err
	} else if !ok {
		return ErrNotFound
	}
	return s.store.SetOfflineWriter(ctx, scope.CompanyID, deviceID)
}

func (s *Service) Memberships(ctx context.Context, scope tenancy.Scope) ([]Membership, error) {
	return s.store.ListMemberships(ctx, scope.CompanyID)
}

// SetApprovalLimit changes one membership's spending approval threshold —
// the "limits" in "Company, accounts, devices and limits". Unlike devices,
// this is deliberately restricted to Owner/delegated-general_manager only
// (no branchOnly path): a branch manager editing another user's approval
// authority is a materially different risk than managing a device
// physically at their own branch, and the matrix's branchOnly grant for
// ActionManageUsers doesn't specifically contemplate it. Role and
// branch_scope stay read-only here — reassigning either is full user
// management, a separate, larger feature this pass doesn't attempt.
func (s *Service) SetApprovalLimit(ctx context.Context, scope tenancy.Scope, membershipID uuid.UUID, limit *decimal.Decimal) error {
	if scope.Role != tenancy.RoleOwner && !(scope.Role == tenancy.RoleGeneralManager && scope.Delegations[tenancy.ActionManageUsers]) {
		return ErrForbidden
	}
	if _, ok, err := s.store.GetMembership(ctx, scope.CompanyID, membershipID); err != nil {
		return err
	} else if !ok {
		return ErrNotFound
	}
	return s.store.SetApprovalLimit(ctx, scope.CompanyID, membershipID, limit)
}
