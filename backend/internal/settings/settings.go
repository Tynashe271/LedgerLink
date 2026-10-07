// Package settings implements the Settings screen (System Documentation
// 5.2: "Company, accounts, devices and limits" / "Authorised changes with
// audit"): the company profile, the chart of accounts, enrolled devices and
// per-user approval limits. Every write here is an administrative change,
// not an accounting posting — it never touches journals or transactions.
package settings

import (
	"context"
	"crypto/rand"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"golang.org/x/crypto/bcrypt"

	"github.com/ledgerlink/branchledger/backend/internal/tenancy"
)

var (
	ErrForbidden     = errors.New("settings: caller is not authorised for this change")
	ErrNotFound      = errors.New("settings: not found")
	ErrDuplicateCode = errors.New("settings: an account with this code already exists")
	// ErrDuplicateBranchCode and ErrMainBranchExists guard AddBranch — see
	// its doc comment.
	ErrDuplicateBranchCode = errors.New("settings: a branch with this code already exists")
	ErrMainBranchExists    = errors.New("settings: the company already has a main branch")
	// ErrDuplicateEmail covers CreateUser for an email already registered —
	// users.email is globally unique (citext), not just per company.
	ErrDuplicateEmail = errors.New("settings: a user with this email already exists")
	ErrInvalidRole    = errors.New("settings: role is not assignable through this form")
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

// --- Branches --------------------------------------------------------------

// Branch mirrors one row of `branches`.
type Branch struct {
	ID           uuid.UUID
	Name         string
	Code         string
	Category     string
	IsMainBranch bool
	Status       string
}

// AddBranchInput is what Service.AddBranch accepts. No edit/delete here,
// same reasoning as AddAccount — a branch_id is embedded everywhere once
// anything posts against it.
type AddBranchInput struct {
	Name         string
	Code         string
	Category     string
	IsMainBranch bool
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
// branch scope are set once at CreateUser and not editable afterward — see
// Service.SetApprovalLimit's comment for why only the limit can change on an
// existing membership. BranchNames is empty for an unrestricted membership
// ("All branches"), same convention as an empty tenancy.Scope.BranchScope.
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

// CreatableRoles excludes platform_admin — "Platform support has no routine
// financial access," so it's never assignable through this generic
// user-management form.
var CreatableRoles = map[string]bool{
	"owner": true, "general_manager": true, "accountant": true, "branch_manager": true, "staff": true,
}

// CreateUserInput is what Service.CreateUser accepts. Password is
// deliberately not a field here: the created user has no usable password
// until they go through the existing forgot-password flow themselves (see
// Service.CreateUser's doc comment) — nobody who creates the account, not
// even the owner, ever knows or sets their password.
type CreateUserInput struct {
	Email         string
	FullName      string
	Role          string
	BranchScope   []uuid.UUID // empty = unrestricted ("All branches")
	ApprovalLimit *decimal.Decimal
}

// EnrollDeviceInput is what Service.EnrollDevice accepts.
type EnrollDeviceInput struct {
	BranchID  uuid.UUID
	UserID    uuid.UUID
	Label     string
	LeaseDays int // defaulted to 7 by the service when <= 0
}

// Store is the persistence this service needs from Postgres.
type Store interface {
	GetCompanyProfile(ctx context.Context, companyID uuid.UUID) (CompanyProfile, bool, error)
	UpdateCompanyProfile(ctx context.Context, companyID uuid.UUID, in UpdateCompanyInput) error

	ListBranches(ctx context.Context, companyID uuid.UUID) ([]Branch, error)
	BranchCodeExists(ctx context.Context, companyID uuid.UUID, code string) (bool, error)
	HasMainBranch(ctx context.Context, companyID uuid.UUID) (bool, error)
	InsertBranch(ctx context.Context, companyID uuid.UUID, in AddBranchInput) (uuid.UUID, error)

	ListAccounts(ctx context.Context, companyID uuid.UUID) ([]Account, error)
	AccountCodeExists(ctx context.Context, companyID uuid.UUID, code string) (bool, error)
	InsertAccount(ctx context.Context, companyID uuid.UUID, in AddAccountInput) (uuid.UUID, error)

	ListDevices(ctx context.Context, companyID uuid.UUID) ([]Device, error)
	GetDevice(ctx context.Context, companyID, deviceID uuid.UUID) (Device, bool, error)
	// InsertDevice enrolls a new device. User Manual: "Initial enrolment
	// requires internet" — this is that enrolment step; there was
	// previously no way to create a device row at all, only to list or
	// revoke ones that already existed.
	InsertDevice(ctx context.Context, companyID uuid.UUID, in EnrollDeviceInput, leaseExpiresAt time.Time) (Device, error)
	// UserBelongsToCompany confirms userID has an active membership in
	// companyID, so a device can't be enrolled against a foreign or
	// nonexistent user.
	UserBelongsToCompany(ctx context.Context, companyID, userID uuid.UUID) (bool, error)
	// BranchBelongsToCompany is the authoritative, database-backed tenancy
	// check for a branch (the same kind accounting.Repository.BranchInCompany
	// does for postings) — Owner/general_manager's scope.BranchScope is
	// usually empty (unrestricted), so scope alone can't catch a
	// cross-company branch ID here the way it can a branch-restricted role.
	BranchBelongsToCompany(ctx context.Context, companyID, branchID uuid.UUID) (bool, error)
	RevokeDevice(ctx context.Context, companyID, deviceID uuid.UUID) error
	// SetOfflineWriter designates deviceID the sole offline writer for its
	// branch, clearing the flag on every other device at that branch in the
	// same statement — architecture: "one designated offline writer per
	// branch," never two at once even momentarily.
	SetOfflineWriter(ctx context.Context, companyID, deviceID uuid.UUID) error

	ListMemberships(ctx context.Context, companyID uuid.UUID) ([]Membership, error)
	GetMembership(ctx context.Context, companyID, membershipID uuid.UUID) (Membership, bool, error)
	SetApprovalLimit(ctx context.Context, companyID, membershipID uuid.UUID, limit *decimal.Decimal) error
	// CreateUserAndMembership inserts both rows in one transaction — an
	// orphaned user with no membership (or vice versa) must never happen.
	// passwordHash is an unusable random placeholder (see CreateUserInput's
	// doc comment); it is never a real password anyone chose.
	CreateUserAndMembership(ctx context.Context, companyID uuid.UUID, in CreateUserInput, passwordHash string) (Membership, error)

	// RecordAuditEvent appends one audit_events row — same shape as
	// accounting.Tx's method of the same name. Every write in this package
	// calls it: this screen's own copy says "every change here ... is
	// itself an authorised, auditable action," so it has to actually be one.
	RecordAuditEvent(ctx context.Context, companyID, actorUserID uuid.UUID, eventType, recordType string, recordID uuid.UUID, details map[string]any) error
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
	if err := s.store.UpdateCompanyProfile(ctx, scope.CompanyID, in); err != nil {
		return err
	}
	return s.store.RecordAuditEvent(ctx, scope.CompanyID, scope.UserID, "company_profile_updated", "company", scope.CompanyID,
		map[string]any{"name": in.Name, "primary_category": in.PrimaryCategory, "timezone": in.Timezone})
}

func (s *Service) Branches(ctx context.Context, scope tenancy.Scope) ([]Branch, error) {
	return s.store.ListBranches(ctx, scope.CompanyID)
}

// AddBranch appends a new branch. Owner-only, same reasoning as
// UpdateCompany — section 4.6's matrix has no dedicated action for company/
// branch structural setup (FR01), so this is a direct role check rather
// than an invented permission. Exactly one branch may be the main branch;
// a second is_main_branch=true request is rejected rather than silently
// demoting the existing one.
func (s *Service) AddBranch(ctx context.Context, scope tenancy.Scope, in AddBranchInput) (Branch, error) {
	if !canManageCompany(scope.Role) {
		return Branch{}, ErrForbidden
	}
	exists, err := s.store.BranchCodeExists(ctx, scope.CompanyID, in.Code)
	if err != nil {
		return Branch{}, err
	}
	if exists {
		return Branch{}, ErrDuplicateBranchCode
	}
	if in.IsMainBranch {
		hasMain, err := s.store.HasMainBranch(ctx, scope.CompanyID)
		if err != nil {
			return Branch{}, err
		}
		if hasMain {
			return Branch{}, ErrMainBranchExists
		}
	}
	id, err := s.store.InsertBranch(ctx, scope.CompanyID, in)
	if err != nil {
		return Branch{}, err
	}
	if err := s.store.RecordAuditEvent(ctx, scope.CompanyID, scope.UserID, "branch_added", "branch", id,
		map[string]any{"name": in.Name, "code": in.Code, "is_main_branch": in.IsMainBranch}); err != nil {
		return Branch{}, err
	}
	return Branch{ID: id, Name: in.Name, Code: in.Code, Category: in.Category, IsMainBranch: in.IsMainBranch, Status: "active"}, nil
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
	if err := s.store.RecordAuditEvent(ctx, scope.CompanyID, scope.UserID, "account_added", "account", id,
		map[string]any{"code": in.Code, "name": in.Name, "account_type": in.AccountType}); err != nil {
		return Account{}, err
	}
	return Account{ID: id, Code: in.Code, Name: in.Name, AccountType: in.AccountType, IsCashLike: in.IsCashLike, IsActive: true}, nil
}

func (s *Service) Devices(ctx context.Context, scope tenancy.Scope) ([]Device, error) {
	return s.store.ListDevices(ctx, scope.CompanyID)
}

// EnrollDevice creates a new device row — gated by ActionManageUsers, same
// as RevokeDevice/SetOfflineWriter.
func (s *Service) EnrollDevice(ctx context.Context, scope tenancy.Scope, in EnrollDeviceInput) (Device, error) {
	if !tenancy.Can(tenancy.ActionManageUsers, scope.Role, scope.Delegations, true) {
		return Device{}, ErrForbidden
	}
	if ok, err := s.store.BranchBelongsToCompany(ctx, scope.CompanyID, in.BranchID); err != nil {
		return Device{}, err
	} else if !ok {
		return Device{}, ErrNotFound
	}
	if ok, err := s.store.UserBelongsToCompany(ctx, scope.CompanyID, in.UserID); err != nil {
		return Device{}, err
	} else if !ok {
		return Device{}, ErrNotFound
	}
	leaseDays := in.LeaseDays
	if leaseDays <= 0 {
		leaseDays = 7
	}
	dv, err := s.store.InsertDevice(ctx, scope.CompanyID, in, time.Now().UTC().AddDate(0, 0, leaseDays))
	if err != nil {
		return Device{}, err
	}
	if err := s.store.RecordAuditEvent(ctx, scope.CompanyID, scope.UserID, "device_enrolled", "device", dv.ID,
		map[string]any{"label": in.Label, "branch_id": in.BranchID, "user_id": in.UserID}); err != nil {
		return Device{}, err
	}
	return dv, nil
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
	if err := s.store.RevokeDevice(ctx, scope.CompanyID, deviceID); err != nil {
		return err
	}
	return s.store.RecordAuditEvent(ctx, scope.CompanyID, scope.UserID, "device_revoked", "device", deviceID, nil)
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
	if err := s.store.SetOfflineWriter(ctx, scope.CompanyID, deviceID); err != nil {
		return err
	}
	return s.store.RecordAuditEvent(ctx, scope.CompanyID, scope.UserID, "device_offline_writer_set", "device", deviceID, nil)
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
	if err := s.store.SetApprovalLimit(ctx, scope.CompanyID, membershipID, limit); err != nil {
		return err
	}
	limitDetail := "unlimited"
	if limit != nil {
		limitDetail = limit.String()
	}
	return s.store.RecordAuditEvent(ctx, scope.CompanyID, scope.UserID, "approval_limit_changed", "membership", membershipID,
		map[string]any{"approval_limit": limitDetail})
}

// CreateUser enrolls a new user and their membership in one step —
// deliberately Owner/delegated-general_manager only, same gate as
// SetApprovalLimit (not devices' branchOnly path), since assigning a role
// and branch scope is at least as sensitive as editing an approval limit.
// The user is created with no usable password: password_hash is a random
// value nobody (not even the caller) ever sees, so the only way in is the
// existing forgot-password flow, which this does not trigger itself —
// telling the new user to request a reset themselves is a deliberate
// choice, not an oversight (letting an admin pick an initial password here
// would mean the admin knows a password they didn't actually consent to
// receive).
func (s *Service) CreateUser(ctx context.Context, scope tenancy.Scope, in CreateUserInput) (Membership, error) {
	if scope.Role != tenancy.RoleOwner && !(scope.Role == tenancy.RoleGeneralManager && scope.Delegations[tenancy.ActionManageUsers]) {
		return Membership{}, ErrForbidden
	}
	if !CreatableRoles[in.Role] {
		return Membership{}, ErrInvalidRole
	}
	for _, branchID := range in.BranchScope {
		if ok, err := s.store.BranchBelongsToCompany(ctx, scope.CompanyID, branchID); err != nil {
			return Membership{}, err
		} else if !ok {
			return Membership{}, ErrNotFound
		}
	}

	randomBytes := make([]byte, 32)
	if _, err := rand.Read(randomBytes); err != nil {
		return Membership{}, err
	}
	// A real bcrypt hash of a random value nobody (including this code) ever
	// retains — not an arbitrary placeholder string. Laravel's BcryptHasher
	// throws RuntimeException("This password does not use the Bcrypt
	// algorithm") for anything that isn't actually bcrypt-shaped, so it must
	// be a genuine hash; it simply can never match a real login attempt.
	hashBytes, err := bcrypt.GenerateFromPassword(randomBytes, bcrypt.DefaultCost)
	if err != nil {
		return Membership{}, err
	}
	// Go's bcrypt always tags its own output "$2a$"; PHP's password_get_info
	// (what Laravel's algorithm check calls) only recognises "$2y$" as
	// algoName "bcrypt" and rejects "$2a$" outright, even though the two are
	// byte-for-byte the same cipher for this ASCII random input — $2y$ exists
	// only to fix a $2a$ high-bit-character bug that never applies here.
	hash := "$2y$" + string(hashBytes[4:])

	m, err := s.store.CreateUserAndMembership(ctx, scope.CompanyID, in, hash)
	if err != nil {
		return Membership{}, err
	}
	if err := s.store.RecordAuditEvent(ctx, scope.CompanyID, scope.UserID, "role_change", "membership", m.ID,
		map[string]any{"action": "user_created", "email": in.Email, "role": in.Role}); err != nil {
		return Membership{}, err
	}
	return m, nil
}
