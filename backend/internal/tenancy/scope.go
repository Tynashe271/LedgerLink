// Package tenancy carries the authenticated company/branch/user scope through
// every request. Per the architecture doc: "Go independently validates the
// delegated identity and scopes. Reject client-provided identity headers." —
// so a Scope is only ever constructed by the auth package after verifying the
// Laravel gateway's signed delegation, never read directly off arbitrary
// request headers by handlers.
package tenancy

import (
	"context"
	"fmt"
	"slices"

	"github.com/google/uuid"
)

// Role mirrors the roles in BranchLedger_System_Documentation section 4.4.
type Role string

const (
	RoleOwner           Role = "owner"
	RoleGeneralManager  Role = "general_manager"
	RoleAccountant      Role = "accountant"
	RoleBranchManager   Role = "branch_manager"
	RoleStaff           Role = "staff"
	RolePlatformAdmin   Role = "platform_admin"
)

// Scope is the verified identity and access boundary for one request: which
// company, which user, which role, and which branches that role may act on
// (empty BranchScope means unrestricted within the company, matching owner /
// general manager / accountant level access in the permission matrix).
type Scope struct {
	CompanyID     uuid.UUID
	UserID        uuid.UUID
	Role          Role
	BranchScope   []uuid.UUID // empty = all branches in the company
	AccessVersion int         // must match the user's current access_version
	RequestID     string
	DeviceID      *uuid.UUID
	// Delegations carries the explicit per-action grants the matrix in
	// permissions.go calls "needsDelegation" (e.g. an owner's permission to
	// reverse postings, or a general manager's company-wide view), on top of
	// whatever the role alone grants. nil/empty means none.
	Delegations Delegations
}

type scopeKey struct{}

// WithScope attaches a verified Scope to the context.
func WithScope(ctx context.Context, s Scope) context.Context {
	return context.WithValue(ctx, scopeKey{}, s)
}

// FromContext retrieves the verified Scope. Handlers must treat its absence as
// unauthenticated — there is no default/anonymous Scope value.
func FromContext(ctx context.Context) (Scope, bool) {
	s, ok := ctx.Value(scopeKey{}).(Scope)
	return s, ok
}

// AllowsBranch reports whether the scope may act on the given branch: either
// the role has unrestricted company-wide access (empty BranchScope) or the
// branch is explicitly listed. This is the "Assigned means both role
// permission and company/branch membership are required" rule (section 4.6).
func (s Scope) AllowsBranch(branchID uuid.UUID) bool {
	if len(s.BranchScope) == 0 {
		return true
	}
	return slices.Contains(s.BranchScope, branchID)
}

// RequireBranch returns an error unless the scope may act on branchID.
func (s Scope) RequireBranch(branchID uuid.UUID) error {
	if !s.AllowsBranch(branchID) {
		return fmt.Errorf("tenancy: company %s user %s is not scoped to branch %s", s.CompanyID, s.UserID, branchID)
	}
	return nil
}

// CompanyWideReadRoles are roles whose "View company totals" permission is
// unconditional per the permission matrix (section 4.6); general_manager's
// company-wide view is "Delegated" (requires an explicit grant captured
// separately, not implied by role alone).
var CompanyWideReadRoles = map[Role]bool{
	RoleOwner:      true,
	RoleAccountant: true,
}
