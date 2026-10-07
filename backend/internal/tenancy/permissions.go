package tenancy

// Action is one of the actions in the detailed permission matrix
// (BranchLedger_System_Documentation section 4.6).
type Action string

const (
	ActionViewCompanyTotals   Action = "view_company_totals"
	ActionViewAssignedBranch  Action = "view_assigned_branch"
	ActionCreateDailyRecord   Action = "create_daily_record"
	ActionApproveOwnRequest   Action = "approve_own_request"
	ActionApproveSpending     Action = "approve_spending"
	ActionReversePosting      Action = "reverse_posting"
	ActionLockPeriod          Action = "lock_period"
	ActionManageUsers         Action = "manage_users"
	ActionExportFinancialData Action = "export_financial_data"
	ActionChangeChartOfAccounts Action = "change_chart_of_accounts"
)

// grant describes whether a role may perform an action outright, needs an
// explicit delegation on top of the role, or never may.
type grant int

const (
	denied grant = iota
	allowed
	needsDelegation
	branchOnly // manage_users / export: branch_manager may act within their own branch
)

// matrix transcribes the table in section 4.6 exactly. "Limit" (approve_spending
// for owner/general_manager/branch_manager) is modelled as needsDelegation too:
// the delegation record carries the actual currency limit.
var matrix = map[Action]map[Role]grant{
	ActionViewCompanyTotals: {
		RoleOwner: allowed, RoleGeneralManager: needsDelegation, RoleAccountant: allowed,
		RoleBranchManager: denied, RoleStaff: denied,
	},
	ActionViewAssignedBranch: {
		RoleOwner: allowed, RoleGeneralManager: allowed, RoleAccountant: allowed,
		RoleBranchManager: allowed, RoleStaff: allowed,
	},
	ActionCreateDailyRecord: {
		RoleOwner: allowed, RoleGeneralManager: allowed, RoleAccountant: allowed,
		RoleBranchManager: allowed, RoleStaff: allowed,
	},
	ActionApproveOwnRequest: {
		RoleOwner: denied, RoleGeneralManager: denied, RoleAccountant: denied,
		RoleBranchManager: denied, RoleStaff: denied,
	},
	ActionApproveSpending: {
		RoleOwner: needsDelegation, RoleGeneralManager: needsDelegation, RoleAccountant: needsDelegation,
		RoleBranchManager: needsDelegation, RoleStaff: denied,
	},
	ActionReversePosting: {
		RoleOwner: needsDelegation, RoleGeneralManager: needsDelegation, RoleAccountant: allowed,
		RoleBranchManager: needsDelegation, RoleStaff: denied,
	},
	ActionLockPeriod: {
		RoleOwner: needsDelegation, RoleGeneralManager: denied, RoleAccountant: allowed,
		RoleBranchManager: denied, RoleStaff: denied,
	},
	ActionManageUsers: {
		RoleOwner: allowed, RoleGeneralManager: needsDelegation, RoleAccountant: denied,
		RoleBranchManager: branchOnly, RoleStaff: denied,
	},
	ActionExportFinancialData: {
		RoleOwner: allowed, RoleGeneralManager: needsDelegation, RoleAccountant: allowed,
		RoleBranchManager: branchOnly, RoleStaff: denied,
	},
	ActionChangeChartOfAccounts: {
		RoleOwner: needsDelegation, RoleGeneralManager: denied, RoleAccountant: allowed,
		RoleBranchManager: denied, RoleStaff: denied,
	},
}

// Delegations is the set of explicit permission grants a membership carries on
// top of its role, keyed by Action. A delegation's presence (true) satisfies a
// needsDelegation entry in the matrix.
type Delegations map[Action]bool

// Can evaluates the permission matrix for a scope acting on a branch, given its
// explicit delegations. Platform administrators are deliberately excluded from
// this matrix: "Platform support has no routine financial access" — they must
// go through the separate, audited, time-limited support-access path instead.
func Can(action Action, role Role, delegations Delegations, actingOnOwnBranch bool) bool {
	if role == RolePlatformAdmin {
		return false
	}
	roleGrants, ok := matrix[action]
	if !ok {
		return false
	}
	switch roleGrants[role] {
	case allowed:
		return true
	case needsDelegation:
		return delegations[action]
	case branchOnly:
		return actingOnOwnBranch
	default:
		return false
	}
}
