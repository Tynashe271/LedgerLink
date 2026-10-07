package tenancy

import "testing"

// TestPermissionMatrix spot-checks the table in
// BranchLedger_System_Documentation section 4.6, including the rule that
// nobody may ever approve their own request and that platform admins have no
// routine financial access.
func TestPermissionMatrix(t *testing.T) {
	cases := []struct {
		name       string
		action     Action
		role       Role
		delegated  bool
		ownBranch  bool
		want       bool
	}{
		{"owner sees company totals", ActionViewCompanyTotals, RoleOwner, false, false, true},
		{"GM needs delegation for company totals", ActionViewCompanyTotals, RoleGeneralManager, false, false, false},
		{"GM with delegation sees company totals", ActionViewCompanyTotals, RoleGeneralManager, true, false, true},
		{"staff cannot view company totals", ActionViewCompanyTotals, RoleStaff, true, false, false},
		{"nobody approves their own request, even owner", ActionApproveOwnRequest, RoleOwner, true, true, false},
		{"accountant reverses posting unconditionally", ActionReversePosting, RoleAccountant, false, false, true},
		{"staff never reverses posting", ActionReversePosting, RoleStaff, true, false, false},
		{"branch manager manages users only on own branch", ActionManageUsers, RoleBranchManager, false, true, true},
		{"branch manager cannot manage users off-branch", ActionManageUsers, RoleBranchManager, false, false, false},
		{"platform admin has no financial access", ActionViewCompanyTotals, RolePlatformAdmin, true, true, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			delegations := Delegations{tc.action: tc.delegated}
			got := Can(tc.action, tc.role, delegations, tc.ownBranch)
			if got != tc.want {
				t.Errorf("Can(%s, %s, delegated=%v, ownBranch=%v) = %v, want %v",
					tc.action, tc.role, tc.delegated, tc.ownBranch, got, tc.want)
			}
		})
	}
}
