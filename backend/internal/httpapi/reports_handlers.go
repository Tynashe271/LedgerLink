package httpapi

import (
	"log"
	"net/http"
	"time"

	"github.com/google/uuid"
)

// parseAsOf reads the "as_of" query param (default today, inclusive of the
// whole day — see handleDashboard's identical adjustment for why).
func parseAsOf(w http.ResponseWriter, r *http.Request) (time.Time, bool) {
	raw := r.URL.Query().Get("as_of")
	asOf := time.Now().UTC().Truncate(24 * time.Hour)
	if raw != "" {
		parsed, err := time.Parse("2006-01-02", raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_as_of_date")
			return time.Time{}, false
		}
		asOf = parsed
	}
	return asOf.Add(24*time.Hour - time.Nanosecond), true
}

func parseReportBranchID(w http.ResponseWriter, r *http.Request) (*uuid.UUID, bool) {
	raw := r.URL.Query().Get("branch_id")
	if raw == "" {
		return nil, true
	}
	id, err := uuid.Parse(raw)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_branch_id")
		return nil, false
	}
	return &id, true
}

// --- GET /api/v1/reports/trial-balance ----------------------------------------

type trialBalanceLineResponse struct {
	AccountCode string `json:"account_code"`
	AccountName string `json:"account_name"`
	AccountType string `json:"account_type"`
	Debit       string `json:"debit"`
	Credit      string `json:"credit"`
}

type trialBalanceResponse struct {
	CompanyID         uuid.UUID                  `json:"company_id"`
	BranchID          *uuid.UUID                 `json:"branch_id,omitempty"`
	AsOf              string                     `json:"as_of"`
	ReportingCurrency string                     `json:"reporting_currency"`
	GeneratedAt       time.Time                  `json:"generated_at"`
	PostingBasis      string                     `json:"posting_basis"`
	Lines             []trialBalanceLineResponse `json:"lines"`
	TotalDebits       string                     `json:"total_debits"`
	TotalCredits      string                     `json:"total_credits"`
}

// handleTrialBalance serves GET /api/v1/reports/trial-balance — the Reports
// screen's "Filter, export and drill down" (System Documentation 5.2).
// Query params: branch_id, as_of (YYYY-MM-DD, default today).
func handleTrialBalance(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scope, ok := requireScope(w, r)
		if !ok {
			return
		}
		branchID, ok := parseReportBranchID(w, r)
		if !ok {
			return
		}
		asOf, ok := parseAsOf(w, r)
		if !ok {
			return
		}

		result, err := deps.ReportingSvc.TrialBalance(r.Context(), scope, branchID, asOf)
		if err != nil {
			log.Printf("request_id=%s trial_balance error: %v", scope.RequestID, err)
			writeError(w, http.StatusForbidden, "out_of_scope")
			return
		}

		lines := make([]trialBalanceLineResponse, 0, len(result.Lines))
		for _, l := range result.Lines {
			lines = append(lines, trialBalanceLineResponse{
				AccountCode: l.AccountCode, AccountName: l.AccountName, AccountType: l.AccountType,
				Debit: l.Debit.StringFixed(2), Credit: l.Credit.StringFixed(2),
			})
		}
		writeJSON(w, http.StatusOK, trialBalanceResponse{
			CompanyID: result.CompanyID, BranchID: result.ScopeBranchID, AsOf: result.AsOf.Format("2006-01-02"),
			ReportingCurrency: result.ReportingCurrency, GeneratedAt: result.GeneratedAt, PostingBasis: result.PostingBasis,
			Lines: lines, TotalDebits: result.TotalDebits.StringFixed(2), TotalCredits: result.TotalCredits.StringFixed(2),
		})
	}
}

// --- GET /api/v1/reports/ageing ------------------------------------------------

type ageingRowResponse struct {
	CustomerName  string `json:"customer_name"`
	OldestDueDate string `json:"oldest_due_date"`
	Total         string `json:"total"`
	Current       string `json:"current"`
	Days1To30     string `json:"days_1_to_30"`
	Days31To60    string `json:"days_31_to_60"`
	Days61To90    string `json:"days_61_to_90"`
	DaysOver90    string `json:"days_over_90"`
}

type ageingResponse struct {
	CompanyID         uuid.UUID           `json:"company_id"`
	BranchID          *uuid.UUID          `json:"branch_id,omitempty"`
	AsOf              string              `json:"as_of"`
	ReportingCurrency string              `json:"reporting_currency"`
	GeneratedAt       time.Time           `json:"generated_at"`
	PostingBasis      string              `json:"posting_basis"`
	Rows              []ageingRowResponse `json:"rows"`
	TotalOutstanding  string              `json:"total_outstanding"`
}

// handleAgeing serves GET /api/v1/reports/ageing. Query params: branch_id,
// as_of (YYYY-MM-DD, default today).
func handleAgeing(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scope, ok := requireScope(w, r)
		if !ok {
			return
		}
		branchID, ok := parseReportBranchID(w, r)
		if !ok {
			return
		}
		asOf, ok := parseAsOf(w, r)
		if !ok {
			return
		}

		result, err := deps.ReportingSvc.Ageing(r.Context(), scope, branchID, asOf)
		if err != nil {
			log.Printf("request_id=%s ageing error: %v", scope.RequestID, err)
			writeError(w, http.StatusForbidden, "out_of_scope")
			return
		}

		rows := make([]ageingRowResponse, 0, len(result.Rows))
		for _, row := range result.Rows {
			rows = append(rows, ageingRowResponse{
				CustomerName: row.CustomerName, OldestDueDate: row.OldestDueDate.Format("2006-01-02"),
				Total: row.OutstandingTotal.StringFixed(2), Current: row.Current.StringFixed(2),
				Days1To30: row.Days1To30.StringFixed(2), Days31To60: row.Days31To60.StringFixed(2),
				Days61To90: row.Days61To90.StringFixed(2), DaysOver90: row.DaysOver90.StringFixed(2),
			})
		}
		writeJSON(w, http.StatusOK, ageingResponse{
			CompanyID: result.CompanyID, BranchID: result.ScopeBranchID, AsOf: result.AsOf.Format("2006-01-02"),
			ReportingCurrency: result.ReportingCurrency, GeneratedAt: result.GeneratedAt, PostingBasis: result.PostingBasis,
			Rows: rows, TotalOutstanding: result.TotalOutstanding.StringFixed(2),
		})
	}
}
