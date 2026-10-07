package httpapi

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/ledgerlink/branchledger/backend/internal/settings"
)

// writeSettingsErr maps a settings.Service error to the HTTP response,
// shared by every write handler in this file.
func writeSettingsErr(w http.ResponseWriter, requestID string, action string, err error) {
	switch {
	case errors.Is(err, settings.ErrForbidden):
		writeError(w, http.StatusForbidden, "out_of_scope")
	case errors.Is(err, settings.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found")
	case errors.Is(err, settings.ErrDuplicateCode):
		writeError(w, http.StatusConflict, "duplicate_code")
	case errors.Is(err, settings.ErrDuplicateBranchCode):
		writeError(w, http.StatusConflict, "duplicate_branch_code")
	case errors.Is(err, settings.ErrMainBranchExists):
		writeError(w, http.StatusConflict, "main_branch_exists")
	case errors.Is(err, settings.ErrDuplicateEmail):
		writeError(w, http.StatusConflict, "duplicate_email")
	case errors.Is(err, settings.ErrInvalidRole):
		writeError(w, http.StatusBadRequest, "invalid_role")
	default:
		log.Printf("request_id=%s %s error: %v", requestID, action, err)
		writeError(w, http.StatusInternalServerError, "internal_error")
	}
}

// --- Company profile ----------------------------------------------------------

type companyProfileResponse struct {
	ID                 uuid.UUID `json:"id"`
	Name               string    `json:"name"`
	PrimaryCategory    string    `json:"primary_category"`
	ReportingCurrency  string    `json:"reporting_currency"`
	Timezone           string    `json:"timezone"`
	FinancialYearStart string    `json:"financial_year_start"`
	Status             string    `json:"status"`
}

func toCompanyProfileResponse(p settings.CompanyProfile) companyProfileResponse {
	return companyProfileResponse{
		ID: p.ID, Name: p.Name, PrimaryCategory: p.PrimaryCategory, ReportingCurrency: p.ReportingCurrency,
		Timezone: p.Timezone, FinancialYearStart: p.FinancialYearStart.Format("2006-01-02"), Status: p.Status,
	}
}

func handleGetCompanyProfile(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scope, ok := requireScope(w, r)
		if !ok {
			return
		}
		profile, err := deps.SettingsSvc.CompanyProfile(r.Context(), scope)
		if err != nil {
			writeSettingsErr(w, scope.RequestID, "get_company_profile", err)
			return
		}
		writeJSON(w, http.StatusOK, toCompanyProfileResponse(profile))
	}
}

type updateCompanyRequest struct {
	Name               string `json:"name"`
	PrimaryCategory    string `json:"primary_category"`
	Timezone           string `json:"timezone"`
	FinancialYearStart string `json:"financial_year_start"`
}

func handleUpdateCompanyProfile(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scope, ok := requireScope(w, r)
		if !ok {
			return
		}
		var req updateCompanyRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "malformed_request")
			return
		}
		if req.Name == "" || req.PrimaryCategory == "" || req.Timezone == "" || req.FinancialYearStart == "" {
			writeError(w, http.StatusBadRequest, "missing_required_field")
			return
		}
		fyStart, err := time.Parse("2006-01-02", req.FinancialYearStart)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_financial_year_start")
			return
		}

		err = deps.SettingsSvc.UpdateCompany(r.Context(), scope, settings.UpdateCompanyInput{
			Name: req.Name, PrimaryCategory: req.PrimaryCategory, Timezone: req.Timezone, FinancialYearStart: fyStart,
		})
		if err != nil {
			writeSettingsErr(w, scope.RequestID, "update_company_profile", err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"accepted": true})
	}
}

// --- Branches --------------------------------------------------------------

type branchResponse struct {
	ID           uuid.UUID `json:"id"`
	Name         string    `json:"name"`
	Code         string    `json:"code"`
	Category     string    `json:"category"`
	IsMainBranch bool      `json:"is_main_branch"`
	Status       string    `json:"status"`
}

func toBranchResponse(b settings.Branch) branchResponse {
	return branchResponse{ID: b.ID, Name: b.Name, Code: b.Code, Category: b.Category, IsMainBranch: b.IsMainBranch, Status: b.Status}
}

func handleListSettingsBranches(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scope, ok := requireScope(w, r)
		if !ok {
			return
		}
		branches, err := deps.SettingsSvc.Branches(r.Context(), scope)
		if err != nil {
			writeSettingsErr(w, scope.RequestID, "list_branches", err)
			return
		}
		out := make([]branchResponse, 0, len(branches))
		for _, b := range branches {
			out = append(out, toBranchResponse(b))
		}
		writeJSON(w, http.StatusOK, out)
	}
}

type addBranchRequest struct {
	Name         string `json:"name"`
	Code         string `json:"code"`
	Category     string `json:"category"`
	IsMainBranch bool   `json:"is_main_branch"`
}

func handleAddBranch(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scope, ok := requireScope(w, r)
		if !ok {
			return
		}
		var req addBranchRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "malformed_request")
			return
		}
		if req.Name == "" || req.Code == "" || req.Category == "" {
			writeError(w, http.StatusBadRequest, "missing_required_field")
			return
		}
		branch, err := deps.SettingsSvc.AddBranch(r.Context(), scope, settings.AddBranchInput{
			Name: req.Name, Code: req.Code, Category: req.Category, IsMainBranch: req.IsMainBranch,
		})
		if err != nil {
			writeSettingsErr(w, scope.RequestID, "add_branch", err)
			return
		}
		writeJSON(w, http.StatusCreated, toBranchResponse(branch))
	}
}

// --- Chart of accounts ----------------------------------------------------

type accountResponse struct {
	ID          uuid.UUID `json:"id"`
	Code        string    `json:"code"`
	Name        string    `json:"name"`
	AccountType string    `json:"account_type"`
	IsCashLike  bool      `json:"is_cash_like"`
	IsActive    bool      `json:"is_active"`
}

func toAccountResponse(a settings.Account) accountResponse {
	return accountResponse{ID: a.ID, Code: a.Code, Name: a.Name, AccountType: a.AccountType, IsCashLike: a.IsCashLike, IsActive: a.IsActive}
}

func handleListSettingsAccounts(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scope, ok := requireScope(w, r)
		if !ok {
			return
		}
		accounts, err := deps.SettingsSvc.Accounts(r.Context(), scope)
		if err != nil {
			writeSettingsErr(w, scope.RequestID, "list_accounts", err)
			return
		}
		out := make([]accountResponse, 0, len(accounts))
		for _, a := range accounts {
			out = append(out, toAccountResponse(a))
		}
		writeJSON(w, http.StatusOK, out)
	}
}

type addAccountRequest struct {
	Code        string `json:"code"`
	Name        string `json:"name"`
	AccountType string `json:"account_type"`
	IsCashLike  bool   `json:"is_cash_like"`
}

var validAccountTypes = map[string]bool{"asset": true, "liability": true, "equity": true, "income": true, "expense": true}

func handleAddAccount(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scope, ok := requireScope(w, r)
		if !ok {
			return
		}
		var req addAccountRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "malformed_request")
			return
		}
		if req.Code == "" || req.Name == "" || !validAccountTypes[req.AccountType] {
			writeError(w, http.StatusBadRequest, "missing_required_field")
			return
		}

		account, err := deps.SettingsSvc.AddAccount(r.Context(), scope, settings.AddAccountInput{
			Code: req.Code, Name: req.Name, AccountType: req.AccountType, IsCashLike: req.IsCashLike,
		})
		if err != nil {
			writeSettingsErr(w, scope.RequestID, "add_account", err)
			return
		}
		writeJSON(w, http.StatusCreated, toAccountResponse(account))
	}
}

// --- Devices -------------------------------------------------------------

type deviceResponse struct {
	ID              uuid.UUID  `json:"id"`
	BranchID        uuid.UUID  `json:"branch_id"`
	BranchName      string     `json:"branch_name"`
	UserName        string     `json:"user_name"`
	Label           string     `json:"label"`
	IsOfflineWriter bool       `json:"is_offline_writer"`
	EnrolledAt      time.Time  `json:"enrolled_at"`
	LeaseExpiresAt  time.Time  `json:"lease_expires_at"`
	RevokedAt       *time.Time `json:"revoked_at,omitempty"`
	LastSyncAt      *time.Time `json:"last_sync_at,omitempty"`
}

func toDeviceResponse(d settings.Device) deviceResponse {
	return deviceResponse{
		ID: d.ID, BranchID: d.BranchID, BranchName: d.BranchName, UserName: d.UserName, Label: d.Label,
		IsOfflineWriter: d.IsOfflineWriter, EnrolledAt: d.EnrolledAt, LeaseExpiresAt: d.LeaseExpiresAt,
		RevokedAt: d.RevokedAt, LastSyncAt: d.LastSyncAt,
	}
}

func handleListDevices(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scope, ok := requireScope(w, r)
		if !ok {
			return
		}
		devices, err := deps.SettingsSvc.Devices(r.Context(), scope)
		if err != nil {
			writeSettingsErr(w, scope.RequestID, "list_devices", err)
			return
		}
		out := make([]deviceResponse, 0, len(devices))
		for _, d := range devices {
			out = append(out, toDeviceResponse(d))
		}
		writeJSON(w, http.StatusOK, out)
	}
}

type enrollDeviceRequest struct {
	BranchID  uuid.UUID `json:"branch_id"`
	UserID    uuid.UUID `json:"user_id"`
	Label     string    `json:"label"`
	LeaseDays int       `json:"lease_days"`
}

func handleEnrollDevice(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scope, ok := requireScope(w, r)
		if !ok {
			return
		}
		var req enrollDeviceRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "malformed_request")
			return
		}
		if req.BranchID == uuid.Nil || req.UserID == uuid.Nil || req.Label == "" {
			writeError(w, http.StatusBadRequest, "missing_required_field")
			return
		}
		device, err := deps.SettingsSvc.EnrollDevice(r.Context(), scope, settings.EnrollDeviceInput{
			BranchID: req.BranchID, UserID: req.UserID, Label: req.Label, LeaseDays: req.LeaseDays,
		})
		if err != nil {
			writeSettingsErr(w, scope.RequestID, "enroll_device", err)
			return
		}
		writeJSON(w, http.StatusCreated, toDeviceResponse(device))
	}
}

func handleRevokeDevice(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scope, ok := requireScope(w, r)
		if !ok {
			return
		}
		deviceID, err := uuid.Parse(chi.URLParam(r, "id"))
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_device_id")
			return
		}
		if err := deps.SettingsSvc.RevokeDevice(r.Context(), scope, deviceID); err != nil {
			writeSettingsErr(w, scope.RequestID, "revoke_device", err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"accepted": true})
	}
}

func handleSetOfflineWriter(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scope, ok := requireScope(w, r)
		if !ok {
			return
		}
		deviceID, err := uuid.Parse(chi.URLParam(r, "id"))
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_device_id")
			return
		}
		if err := deps.SettingsSvc.SetOfflineWriter(r.Context(), scope, deviceID); err != nil {
			writeSettingsErr(w, scope.RequestID, "set_offline_writer", err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"accepted": true})
	}
}

// --- Memberships / approval limits --------------------------------------------

type membershipResponse struct {
	ID            uuid.UUID `json:"id"`
	UserName      string    `json:"user_name"`
	UserEmail     string    `json:"user_email"`
	Role          string    `json:"role"`
	BranchNames   []string  `json:"branch_names"`
	ApprovalLimit *string   `json:"approval_limit,omitempty"`
	IsActive      bool      `json:"is_active"`
}

func toMembershipResponse(m settings.Membership) membershipResponse {
	resp := membershipResponse{
		ID: m.ID, UserName: m.UserName, UserEmail: m.UserEmail, Role: m.Role,
		BranchNames: m.BranchNames, IsActive: m.IsActive,
	}
	if m.ApprovalLimit != nil {
		s := m.ApprovalLimit.StringFixed(2)
		resp.ApprovalLimit = &s
	}
	return resp
}

func handleListMemberships(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scope, ok := requireScope(w, r)
		if !ok {
			return
		}
		memberships, err := deps.SettingsSvc.Memberships(r.Context(), scope)
		if err != nil {
			writeSettingsErr(w, scope.RequestID, "list_memberships", err)
			return
		}
		out := make([]membershipResponse, 0, len(memberships))
		for _, m := range memberships {
			out = append(out, toMembershipResponse(m))
		}
		writeJSON(w, http.StatusOK, out)
	}
}

type setApprovalLimitRequest struct {
	// ApprovalLimit is a pointer so an explicit null clears the limit
	// (unrestricted approval) rather than being indistinguishable from "not
	// provided" — Go's json package leaves a nil *string for a JSON null.
	ApprovalLimit *string `json:"approval_limit"`
}

func handleSetApprovalLimit(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scope, ok := requireScope(w, r)
		if !ok {
			return
		}
		membershipID, err := uuid.Parse(chi.URLParam(r, "id"))
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_membership_id")
			return
		}
		var req setApprovalLimitRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "malformed_request")
			return
		}
		var limit *decimal.Decimal
		if req.ApprovalLimit != nil {
			parsed, err := decimal.NewFromString(*req.ApprovalLimit)
			if err != nil {
				writeError(w, http.StatusBadRequest, "invalid_approval_limit")
				return
			}
			limit = &parsed
		}

		if err := deps.SettingsSvc.SetApprovalLimit(r.Context(), scope, membershipID, limit); err != nil {
			writeSettingsErr(w, scope.RequestID, "set_approval_limit", err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"accepted": true})
	}
}

type createUserRequest struct {
	Email         string      `json:"email"`
	FullName      string      `json:"full_name"`
	Role          string      `json:"role"`
	BranchScope   []uuid.UUID `json:"branch_scope"`
	ApprovalLimit *string     `json:"approval_limit"`
}

func handleCreateUser(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scope, ok := requireScope(w, r)
		if !ok {
			return
		}
		var req createUserRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "malformed_request")
			return
		}
		if req.Email == "" || req.FullName == "" || req.Role == "" {
			writeError(w, http.StatusBadRequest, "missing_required_field")
			return
		}
		var limit *decimal.Decimal
		if req.ApprovalLimit != nil {
			parsed, err := decimal.NewFromString(*req.ApprovalLimit)
			if err != nil {
				writeError(w, http.StatusBadRequest, "invalid_approval_limit")
				return
			}
			limit = &parsed
		}

		membership, err := deps.SettingsSvc.CreateUser(r.Context(), scope, settings.CreateUserInput{
			Email: req.Email, FullName: req.FullName, Role: req.Role, BranchScope: req.BranchScope, ApprovalLimit: limit,
		})
		if err != nil {
			writeSettingsErr(w, scope.RequestID, "create_user", err)
			return
		}
		writeJSON(w, http.StatusCreated, toMembershipResponse(membership))
	}
}
