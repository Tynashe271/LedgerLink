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

	"github.com/ledgerlink/branchledger/backend/internal/reconciliation"
	"github.com/ledgerlink/branchledger/backend/internal/tenancy"
)

// --- GET /api/v1/reconciliation -----------------------------------------------
//
// Serves the Reconciliation screen's "Match statement items and resolve"
// (System Documentation 5.2): the account picker, and both statement items
// and ledger entries — matched and outstanding alike — for one account and
// date range.

type cashLikeAccountResponse struct {
	ID   uuid.UUID `json:"id"`
	Code string    `json:"code"`
	Name string    `json:"name"`
}

type statementItemResponse struct {
	ID                   uuid.UUID  `json:"id"`
	BranchID             uuid.UUID  `json:"branch_id"`
	BranchName           string     `json:"branch_name"`
	AccountID            uuid.UUID  `json:"account_id"`
	StatementDate        string     `json:"statement_date"`
	Description          string     `json:"description"`
	Amount               string     `json:"amount"`
	ExternalReference    string     `json:"external_reference,omitempty"`
	Status               string     `json:"status"`
	MatchedJournalLineID *uuid.UUID `json:"matched_journal_line_id,omitempty"`
	CreatedAt            time.Time  `json:"created_at"`
}

type ledgerEntryResponse struct {
	JournalLineID uuid.UUID `json:"journal_line_id"`
	TransactionID uuid.UUID `json:"transaction_id"`
	BranchID      uuid.UUID `json:"branch_id"`
	BranchName    string    `json:"branch_name"`
	DocumentType  string    `json:"document_type"`
	DocumentDate  string    `json:"document_date"`
	Description   string    `json:"description"`
	Amount        string    `json:"amount"`
	Matched       bool      `json:"matched"`
}

type reconciliationOverviewResponse struct {
	Accounts       []cashLikeAccountResponse `json:"accounts"`
	AccountID      *uuid.UUID                `json:"account_id,omitempty"`
	From           string                    `json:"from"`
	To             string                    `json:"to"`
	StatementItems []statementItemResponse   `json:"statement_items"`
	LedgerEntries  []ledgerEntryResponse     `json:"ledger_entries"`
}

func toStatementItemResponse(s reconciliation.StatementItem) statementItemResponse {
	return statementItemResponse{
		ID: s.ID, BranchID: s.BranchID, BranchName: s.BranchName, AccountID: s.AccountID,
		StatementDate: s.StatementDate.Format("2006-01-02"), Description: s.Description,
		Amount: s.Amount.StringFixed(2), ExternalReference: s.ExternalReference, Status: s.Status,
		MatchedJournalLineID: s.MatchedJournalLineID, CreatedAt: s.CreatedAt,
	}
}

func toLedgerEntryResponse(e reconciliation.LedgerEntry) ledgerEntryResponse {
	return ledgerEntryResponse{
		JournalLineID: e.JournalLineID, TransactionID: e.TransactionID, BranchID: e.BranchID,
		BranchName: e.BranchName, DocumentType: e.DocumentType, DocumentDate: e.DocumentDate.Format("2006-01-02"),
		Description: e.Description, Amount: e.Amount.StringFixed(2), Matched: e.Matched,
	}
}

// handleReconciliationOverview serves GET /api/v1/reconciliation. Query
// params: account_id (omit to just list the account picker), from, to
// (YYYY-MM-DD, default the last 30 days).
func handleReconciliationOverview(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scope, ok := requireScope(w, r)
		if !ok {
			return
		}
		q := r.URL.Query()

		accounts, err := deps.ReconciliationSvc.CashLikeAccounts(r.Context(), scope)
		if err != nil {
			log.Printf("request_id=%s reconciliation_accounts error: %v", scope.RequestID, err)
			writeError(w, http.StatusInternalServerError, "internal_error")
			return
		}
		accountsOut := make([]cashLikeAccountResponse, 0, len(accounts))
		for _, a := range accounts {
			accountsOut = append(accountsOut, cashLikeAccountResponse{ID: a.ID, Code: a.Code, Name: a.Name})
		}

		resp := reconciliationOverviewResponse{Accounts: accountsOut, StatementItems: []statementItemResponse{}, LedgerEntries: []ledgerEntryResponse{}}

		rawAccountID := q.Get("account_id")
		if rawAccountID == "" {
			writeJSON(w, http.StatusOK, resp)
			return
		}
		accountID, err := uuid.Parse(rawAccountID)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_account_id")
			return
		}

		to := time.Now().UTC()
		from := to.AddDate(0, 0, -30)
		if raw := q.Get("from"); raw != "" {
			parsed, err := time.Parse("2006-01-02", raw)
			if err != nil {
				writeError(w, http.StatusBadRequest, "invalid_from_date")
				return
			}
			from = parsed
		}
		if raw := q.Get("to"); raw != "" {
			parsed, err := time.Parse("2006-01-02", raw)
			if err != nil {
				writeError(w, http.StatusBadRequest, "invalid_to_date")
				return
			}
			to = parsed
		}

		items, entries, err := deps.ReconciliationSvc.Overview(r.Context(), scope, accountID, from, to)
		if errors.Is(err, reconciliation.ErrAccountNotCashLike) {
			writeError(w, http.StatusBadRequest, "account_not_cash_like")
			return
		}
		if err != nil {
			log.Printf("request_id=%s reconciliation_overview error: %v", scope.RequestID, err)
			writeError(w, http.StatusInternalServerError, "internal_error")
			return
		}

		resp.AccountID = &accountID
		resp.From, resp.To = from.Format("2006-01-02"), to.Format("2006-01-02")
		for _, item := range items {
			resp.StatementItems = append(resp.StatementItems, toStatementItemResponse(item))
		}
		for _, e := range entries {
			resp.LedgerEntries = append(resp.LedgerEntries, toLedgerEntryResponse(e))
		}
		writeJSON(w, http.StatusOK, resp)
	}
}

// --- POST /api/v1/reconciliation/items -----------------------------------------

type addStatementItemRequest struct {
	ID                uuid.UUID       `json:"id"`
	BranchID          uuid.UUID       `json:"branch_id"`
	AccountID         uuid.UUID       `json:"account_id"`
	StatementDate     string          `json:"statement_date"`
	Description       string          `json:"description"`
	Amount            decimal.Decimal `json:"amount"`
	ExternalReference string          `json:"external_reference"`
}

func handleAddStatementItem(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scope, ok := requireScope(w, r)
		if !ok {
			return
		}
		var req addStatementItemRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "malformed_request")
			return
		}
		if req.ID == uuid.Nil || req.BranchID == uuid.Nil || req.AccountID == uuid.Nil || req.Description == "" || req.Amount.IsZero() {
			writeError(w, http.StatusBadRequest, "missing_required_field")
			return
		}
		statementDate, err := time.Parse("2006-01-02", req.StatementDate)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_statement_date")
			return
		}

		item, err := deps.ReconciliationSvc.AddStatementItem(r.Context(), scope, reconciliation.AddStatementItemInput{
			ID: req.ID, BranchID: req.BranchID, AccountID: req.AccountID, StatementDate: statementDate,
			Description: req.Description, Amount: req.Amount, ExternalReference: req.ExternalReference,
		})
		if errors.Is(err, reconciliation.ErrOutOfScope) {
			writeError(w, http.StatusForbidden, "out_of_scope")
			return
		}
		if errors.Is(err, reconciliation.ErrAccountNotCashLike) {
			writeError(w, http.StatusBadRequest, "account_not_cash_like")
			return
		}
		if err != nil {
			log.Printf("request_id=%s add_statement_item error: %v", scope.RequestID, err)
			writeError(w, http.StatusInternalServerError, "internal_error")
			return
		}
		writeJSON(w, http.StatusCreated, toStatementItemResponse(item))
	}
}

// --- POST /api/v1/reconciliation/items/{id}/match ------------------------------

type matchStatementItemRequest struct {
	JournalLineID uuid.UUID `json:"journal_line_id"`
}

func handleMatchStatementItem(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scope, ok := requireScope(w, r)
		if !ok {
			return
		}
		itemID, err := uuid.Parse(chi.URLParam(r, "id"))
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_statement_item_id")
			return
		}
		var req matchStatementItemRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "malformed_request")
			return
		}
		if req.JournalLineID == uuid.Nil {
			writeError(w, http.StatusBadRequest, "missing_required_field")
			return
		}

		err = deps.ReconciliationSvc.Match(r.Context(), scope, itemID, req.JournalLineID)
		writeMatchResult(w, scope, err)
	}
}

// --- POST /api/v1/reconciliation/items/{id}/unmatch -----------------------------

func handleUnmatchStatementItem(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scope, ok := requireScope(w, r)
		if !ok {
			return
		}
		itemID, err := uuid.Parse(chi.URLParam(r, "id"))
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_statement_item_id")
			return
		}
		err = deps.ReconciliationSvc.Unmatch(r.Context(), scope, itemID)
		writeMatchResult(w, scope, err)
	}
}

// --- POST /api/v1/reconciliation/auto-match -------------------------------------

type autoMatchRequest struct {
	AccountID uuid.UUID `json:"account_id"`
	From      string    `json:"from"`
	To        string    `json:"to"`
}

type autoMatchResponse struct {
	Matched int `json:"matched"`
}

func handleAutoMatch(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scope, ok := requireScope(w, r)
		if !ok {
			return
		}
		var req autoMatchRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "malformed_request")
			return
		}
		if req.AccountID == uuid.Nil {
			writeError(w, http.StatusBadRequest, "missing_required_field")
			return
		}
		from, err := time.Parse("2006-01-02", req.From)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_from_date")
			return
		}
		to, err := time.Parse("2006-01-02", req.To)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_to_date")
			return
		}

		matched, err := deps.ReconciliationSvc.AutoMatch(r.Context(), scope, req.AccountID, from, to)
		if errors.Is(err, reconciliation.ErrAccountNotCashLike) {
			writeError(w, http.StatusBadRequest, "account_not_cash_like")
			return
		}
		if err != nil {
			log.Printf("request_id=%s auto_match error: %v", scope.RequestID, err)
			writeError(w, http.StatusInternalServerError, "internal_error")
			return
		}
		writeJSON(w, http.StatusOK, autoMatchResponse{Matched: matched})
	}
}

type matchResultResponse struct {
	Accepted  bool   `json:"accepted"`
	ErrorCode string `json:"error_code,omitempty"`
}

// writeMatchResult maps Service.Match/Unmatch's error (nil on success) to
// the HTTP response, shared by the match and unmatch handlers.
func writeMatchResult(w http.ResponseWriter, scope tenancy.Scope, err error) {
	switch {
	case err == nil:
		writeJSON(w, http.StatusOK, matchResultResponse{Accepted: true})
	case errors.Is(err, reconciliation.ErrOutOfScope):
		writeError(w, http.StatusForbidden, "out_of_scope")
	case errors.Is(err, reconciliation.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found")
	case errors.Is(err, reconciliation.ErrAlreadyMatched):
		writeJSON(w, http.StatusConflict, matchResultResponse{ErrorCode: "already_matched"})
	case errors.Is(err, reconciliation.ErrNotUnmatched):
		writeJSON(w, http.StatusConflict, matchResultResponse{ErrorCode: "not_matched"})
	case errors.Is(err, reconciliation.ErrAmountMismatch):
		writeJSON(w, http.StatusUnprocessableEntity, matchResultResponse{ErrorCode: "amount_mismatch"})
	default:
		log.Printf("request_id=%s reconciliation match/unmatch error: %v", scope.RequestID, err)
		writeError(w, http.StatusInternalServerError, "internal_error")
	}
}
