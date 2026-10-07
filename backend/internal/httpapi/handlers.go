package httpapi

import (
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/ledgerlink/branchledger/backend/internal/accounting"
	"github.com/ledgerlink/branchledger/backend/internal/exports"
	"github.com/ledgerlink/branchledger/backend/internal/tenancy"
)

// errorResponse matches the architecture's "stable error code" requirement:
// handlers never leak Go error strings or stack traces to the client
// (launch control #4).
type errorResponse struct {
	ErrorCode string `json:"error_code"`
}

func writeError(w http.ResponseWriter, status int, code string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(errorResponse{ErrorCode: code})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func requireScope(w http.ResponseWriter, r *http.Request) (tenancy.Scope, bool) {
	scope, ok := tenancy.FromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthenticated")
		return tenancy.Scope{}, false
	}
	return scope, true
}

// --- POST /api/v1/transactions ---------------------------------------------

type transactionLineRequest struct {
	ProductID   *uuid.UUID      `json:"product_id"`
	Description string          `json:"description"`
	Quantity    decimal.Decimal `json:"quantity"`
	UnitPrice   decimal.Decimal `json:"unit_price"`
	Discount    decimal.Decimal `json:"discount"`
	TaxCode     string          `json:"tax_code"`
}

type postTransactionRequest struct {
	OperationID      uuid.UUID                `json:"operation_id"`
	BranchID         uuid.UUID                `json:"branch_id"`
	DocumentType     string                   `json:"document_type"`
	DocumentDate     string                   `json:"document_date"` // YYYY-MM-DD, company-local
	CurrencyCode     string                   `json:"currency_code"`
	ExchangeRate     decimal.Decimal          `json:"exchange_rate"`
	CounterpartyID   *uuid.UUID               `json:"counterparty_id"`
	PaymentAccountID *uuid.UUID               `json:"payment_account_id"`
	SourceReference  string                   `json:"source_reference"`
	Explanation      string                   `json:"explanation"`
	Lines            []transactionLineRequest `json:"lines"`

	// Populated only when DocumentType == "transfer" (FR07 dispatch side).
	ReceiverBranchID *uuid.UUID `json:"receiver_branch_id,omitempty"`
	TransferType     string     `json:"transfer_type,omitempty"`

	// AdjustmentType is populated only when DocumentType == "stock_adjustment":
	// "waste" or "count" (see accounting.PostInput.AdjustmentType).
	AdjustmentType string `json:"adjustment_type,omitempty"`

	// DueDate matters only for a credit sale; YYYY-MM-DD, optional (defaults
	// to document_date + 30 days when the sale is on credit and this is
	// omitted).
	DueDate string `json:"due_date,omitempty"`
}

type transactionResponse struct {
	OperationID   uuid.UUID `json:"operation_id"`
	Accepted      bool      `json:"accepted"`
	TransactionID uuid.UUID `json:"transaction_id,omitempty"`
	Status        string    `json:"status,omitempty"` // "posted" | "awaiting_approval"
	Version       int       `json:"version,omitempty"`
	ErrorCode     string    `json:"error_code,omitempty"`
}

func decodePostInput(req postTransactionRequest) (accounting.PostInput, error) {
	date, err := time.Parse("2006-01-02", req.DocumentDate)
	if err != nil {
		return accounting.PostInput{}, err
	}
	rate := req.ExchangeRate
	if rate.IsZero() {
		rate = decimal.NewFromInt(1)
	}
	lines := make([]accounting.LineInput, 0, len(req.Lines))
	for _, l := range req.Lines {
		lines = append(lines, accounting.LineInput{
			ProductID: l.ProductID, Description: l.Description, Quantity: l.Quantity,
			UnitPrice: l.UnitPrice, Discount: l.Discount, TaxCode: l.TaxCode,
		})
	}
	var dueDate *time.Time
	isCreditSale := req.DocumentType == "sale" && req.CounterpartyID != nil && req.PaymentAccountID == nil
	if req.DueDate != "" {
		parsed, err := time.Parse("2006-01-02", req.DueDate)
		if err != nil {
			return accounting.PostInput{}, err
		}
		dueDate = &parsed
	} else if isCreditSale {
		defaultDue := date.AddDate(0, 0, 30)
		dueDate = &defaultDue
	}

	return accounting.PostInput{
		OperationID: req.OperationID, BranchID: req.BranchID, DocumentType: req.DocumentType,
		DocumentDate: date, CurrencyCode: req.CurrencyCode, ExchangeRate: rate,
		CounterpartyID: req.CounterpartyID, PaymentAccountID: req.PaymentAccountID,
		SourceReference: req.SourceReference, Explanation: req.Explanation, Lines: lines,
		ClientSubmittedAt: time.Now().UTC(),
		ReceiverBranchID:  req.ReceiverBranchID, TransferType: req.TransferType,
		AdjustmentType: req.AdjustmentType,
		DueDate:        dueDate,
	}, nil
}

func handlePostTransaction(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scope, ok := requireScope(w, r)
		if !ok {
			return
		}

		var req postTransactionRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "malformed_request")
			return
		}
		if req.OperationID == uuid.Nil || req.BranchID == uuid.Nil || req.DocumentType == "" || len(req.Lines) == 0 {
			writeError(w, http.StatusBadRequest, "missing_required_field")
			return
		}
		if req.DocumentType == "stock_adjustment" {
			if req.AdjustmentType != "waste" && req.AdjustmentType != "count" {
				writeError(w, http.StatusBadRequest, "missing_required_field")
				return
			}
			if req.AdjustmentType == "waste" && req.Explanation == "" {
				writeError(w, http.StatusBadRequest, "missing_required_field")
				return
			}
		}

		in, err := decodePostInput(req)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_document_date")
			return
		}

		res, err := deps.AccountingSvc.Post(r.Context(), scope, in)
		if err == accounting.ErrConflict {
			writeJSON(w, http.StatusConflict, transactionResponse{OperationID: req.OperationID, ErrorCode: "conflict"})
			return
		}
		if err == accounting.ErrOutOfScope {
			writeError(w, http.StatusForbidden, "out_of_scope")
			return
		}
		if err != nil {
			log.Printf("request_id=%s post_transaction error: %v", scope.RequestID, err)
			writeError(w, http.StatusInternalServerError, "internal_error")
			return
		}

		status := http.StatusCreated
		switch {
		case !res.Accepted:
			status = http.StatusUnprocessableEntity // understood, but rejected by a business rule
		case res.AlreadyProcessed:
			status = http.StatusOK
		}
		writeJSON(w, status, transactionResponse{
			OperationID: req.OperationID, Accepted: res.Accepted,
			TransactionID: res.TransactionID, Status: res.Status, Version: res.Version, ErrorCode: res.ErrorCode,
		})
	}
}

// --- POST /api/v1/sync/push --------------------------------------------------

type syncPushRequest struct {
	Items []postTransactionRequest `json:"items"`
}

func handleSyncPush(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scope, ok := requireScope(w, r)
		if !ok {
			return
		}

		var req syncPushRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "malformed_request")
			return
		}

		items := make([]syncPushItem, 0, len(req.Items))
		for _, raw := range req.Items {
			in, err := decodePostInput(raw)
			if err != nil {
				items = append(items, syncPushItem{operationID: raw.OperationID, decodeErr: true})
				continue
			}
			items = append(items, syncPushItem{operationID: raw.OperationID, input: in})
		}

		result, err := pushAll(r.Context(), deps, scope, items)
		if err != nil {
			writeError(w, http.StatusRequestEntityTooLarge, "batch_too_large")
			return
		}
		writeJSON(w, http.StatusOK, result)
	}
}

// --- GET /api/v1/sync/pull ---------------------------------------------------

func handleSyncPull(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scope, ok := requireScope(w, r)
		if !ok {
			return
		}
		cursor := r.URL.Query().Get("cursor")
		cursorInt, err := decodeCursorParamOpaque(cursor)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_cursor")
			return
		}
		result, err := deps.Puller.Pull(r.Context(), scope, cursorInt)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal_error")
			return
		}
		writeJSON(w, http.StatusOK, result)
	}
}

// --- POST /api/v1/transactions/{id}/reverse ----------------------------------

type reverseTransactionRequest struct {
	OperationID uuid.UUID `json:"operation_id"`
	BranchID    uuid.UUID `json:"branch_id"`
	Reason      string    `json:"reason"`
}

func handleReverseTransaction(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scope, ok := requireScope(w, r)
		if !ok {
			return
		}
		transactionID, err := uuid.Parse(chi.URLParam(r, "id"))
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_transaction_id")
			return
		}
		var req reverseTransactionRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "malformed_request")
			return
		}
		if req.OperationID == uuid.Nil || req.BranchID == uuid.Nil || req.Reason == "" {
			writeError(w, http.StatusBadRequest, "missing_required_field")
			return
		}

		res, err := deps.AccountingSvc.Reverse(r.Context(), scope, accounting.ReverseInput{
			OperationID: req.OperationID, TransactionID: transactionID, BranchID: req.BranchID,
			Reason: req.Reason, ClientSubmittedAt: time.Now().UTC(),
		})
		if err == accounting.ErrConflict {
			writeJSON(w, http.StatusConflict, transactionResponse{OperationID: req.OperationID, ErrorCode: "conflict"})
			return
		}
		if err == accounting.ErrOutOfScope {
			writeError(w, http.StatusForbidden, "out_of_scope")
			return
		}
		if err != nil {
			log.Printf("request_id=%s reverse_transaction error: %v", scope.RequestID, err)
			writeError(w, http.StatusInternalServerError, "internal_error")
			return
		}

		status := http.StatusCreated
		switch {
		case !res.Accepted:
			status = http.StatusUnprocessableEntity
		case res.AlreadyProcessed:
			status = http.StatusOK
		}
		writeJSON(w, status, transactionResponse{
			OperationID: req.OperationID, Accepted: res.Accepted, TransactionID: res.TransactionID,
			Status: res.Status, Version: res.Version, ErrorCode: res.ErrorCode,
		})
	}
}

// --- POST /api/v1/closes -----------------------------------------------------

type createCloseRequest struct {
	OperationID  uuid.UUID       `json:"operation_id"`
	BranchID     uuid.UUID       `json:"branch_id"`
	CloseDate    string          `json:"close_date"` // YYYY-MM-DD
	CurrencyCode string          `json:"currency_code"`
	OpeningFloat decimal.Decimal `json:"opening_float"`
	ExpectedCash decimal.Decimal `json:"expected_cash"`
	CountedCash  decimal.Decimal `json:"counted_cash"`
	Explanation  string          `json:"explanation"`
}

type closeResponse struct {
	OperationID uuid.UUID `json:"operation_id"`
	Accepted    bool      `json:"accepted"`
	CloseID     uuid.UUID `json:"close_id,omitempty"`
	Discrepancy string    `json:"discrepancy,omitempty"`
	ErrorCode   string    `json:"error_code,omitempty"`
}

func handleCreateClose(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scope, ok := requireScope(w, r)
		if !ok {
			return
		}
		var req createCloseRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "malformed_request")
			return
		}
		if req.OperationID == uuid.Nil || req.BranchID == uuid.Nil || req.CurrencyCode == "" {
			writeError(w, http.StatusBadRequest, "missing_required_field")
			return
		}
		closeDate, err := time.Parse("2006-01-02", req.CloseDate)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_close_date")
			return
		}

		res, err := deps.AccountingSvc.SubmitClose(r.Context(), scope, accounting.CloseInput{
			OperationID: req.OperationID, BranchID: req.BranchID, CloseDate: closeDate,
			CurrencyCode: req.CurrencyCode, OpeningFloat: req.OpeningFloat, ExpectedCash: req.ExpectedCash,
			CountedCash: req.CountedCash, Explanation: req.Explanation, ClientSubmittedAt: time.Now().UTC(),
		})
		if err == accounting.ErrConflict {
			writeJSON(w, http.StatusConflict, closeResponse{OperationID: req.OperationID, ErrorCode: "conflict"})
			return
		}
		if err == accounting.ErrOutOfScope {
			writeError(w, http.StatusForbidden, "out_of_scope")
			return
		}
		if err != nil {
			log.Printf("request_id=%s create_close error: %v", scope.RequestID, err)
			writeError(w, http.StatusInternalServerError, "internal_error")
			return
		}

		status := http.StatusCreated
		switch {
		case !res.Accepted:
			status = http.StatusUnprocessableEntity
		case res.AlreadyProcessed:
			status = http.StatusOK
		}
		writeJSON(w, status, closeResponse{
			OperationID: req.OperationID, Accepted: res.Accepted, CloseID: res.CloseID,
			Discrepancy: res.Discrepancy.StringFixed(2), ErrorCode: res.ErrorCode,
		})
	}
}

// --- POST /api/v1/approvals/{id}/decision ------------------------------------

type approvalDecisionRequest struct {
	OperationID     uuid.UUID `json:"operation_id"`
	BranchID        uuid.UUID `json:"branch_id"`
	Decision        string    `json:"decision"` // "approved" | "rejected"
	Reason          string    `json:"reason"`
	ExpectedVersion int       `json:"expected_version"`
}

func handleApprovalDecision(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scope, ok := requireScope(w, r)
		if !ok {
			return
		}
		approvalID, err := uuid.Parse(chi.URLParam(r, "id"))
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_approval_id")
			return
		}
		var req approvalDecisionRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "malformed_request")
			return
		}
		if req.OperationID == uuid.Nil || req.BranchID == uuid.Nil || (req.Decision != "approved" && req.Decision != "rejected") {
			writeError(w, http.StatusBadRequest, "missing_required_field")
			return
		}

		res, err := deps.AccountingSvc.DecideApproval(r.Context(), scope, accounting.ApprovalDecisionInput{
			OperationID: req.OperationID, ApprovalID: approvalID, BranchID: req.BranchID,
			Decision: req.Decision, Reason: req.Reason, ExpectedVersion: req.ExpectedVersion,
			ClientSubmittedAt: time.Now().UTC(),
		})
		if err == accounting.ErrConflict {
			writeJSON(w, http.StatusConflict, transactionResponse{OperationID: req.OperationID, ErrorCode: "conflict"})
			return
		}
		if err == accounting.ErrOutOfScope {
			writeError(w, http.StatusForbidden, "out_of_scope")
			return
		}
		if err != nil {
			log.Printf("request_id=%s approval_decision error: %v", scope.RequestID, err)
			writeError(w, http.StatusInternalServerError, "internal_error")
			return
		}

		status := http.StatusOK
		switch {
		case !res.Accepted:
			status = http.StatusUnprocessableEntity
		}
		writeJSON(w, status, transactionResponse{
			OperationID: req.OperationID, Accepted: res.Accepted, TransactionID: res.TransactionID,
			Status: res.Status, Version: res.Version, ErrorCode: res.ErrorCode,
		})
	}
}

// --- POST /api/v1/transfers/{id}/receive -------------------------------------

type transferReceiveRequest struct {
	OperationID    uuid.UUID       `json:"operation_id"`
	BranchID       uuid.UUID       `json:"branch_id"`
	ReceivedAmount decimal.Decimal `json:"received_amount"`
}

type transferReceiveResponse struct {
	OperationID    uuid.UUID `json:"operation_id"`
	Accepted       bool      `json:"accepted"`
	TransactionID  uuid.UUID `json:"transaction_id,omitempty"`
	TransferStatus string    `json:"transfer_status,omitempty"`
	ErrorCode      string    `json:"error_code,omitempty"`
}

func handleTransferReceive(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scope, ok := requireScope(w, r)
		if !ok {
			return
		}
		transferID, err := uuid.Parse(chi.URLParam(r, "id"))
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_transfer_id")
			return
		}
		var req transferReceiveRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "malformed_request")
			return
		}
		if req.OperationID == uuid.Nil || req.BranchID == uuid.Nil {
			writeError(w, http.StatusBadRequest, "missing_required_field")
			return
		}

		res, err := deps.AccountingSvc.ReceiveTransfer(r.Context(), scope, accounting.TransferReceiveInput{
			OperationID: req.OperationID, TransferID: transferID, BranchID: req.BranchID,
			ReceivedAmount: req.ReceivedAmount, ClientSubmittedAt: time.Now().UTC(),
		})
		if err == accounting.ErrConflict {
			writeJSON(w, http.StatusConflict, transferReceiveResponse{OperationID: req.OperationID, ErrorCode: "conflict"})
			return
		}
		if err == accounting.ErrOutOfScope {
			writeError(w, http.StatusForbidden, "out_of_scope")
			return
		}
		if err != nil {
			log.Printf("request_id=%s transfer_receive error: %v", scope.RequestID, err)
			writeError(w, http.StatusInternalServerError, "internal_error")
			return
		}

		status := http.StatusCreated
		switch {
		case !res.Accepted:
			status = http.StatusUnprocessableEntity
		case res.AlreadyProcessed:
			status = http.StatusOK
		}
		writeJSON(w, status, transferReceiveResponse{
			OperationID: req.OperationID, Accepted: res.Accepted, TransactionID: res.TransactionID,
			TransferStatus: res.TransferStatus, ErrorCode: res.ErrorCode,
		})
	}
}

// --- POST /api/v1/exports, GET /api/v1/exports/{id} --------------------------

type createExportRequest struct {
	ExportType string     `json:"export_type"`
	BranchID   *uuid.UUID `json:"branch_id"`
	From       string     `json:"from"`
	To         string     `json:"to"`
	Currency   string     `json:"currency"`
}

type exportResponse struct {
	ExportID  uuid.UUID  `json:"export_id"`
	Status    string     `json:"status"`
	RowCount  *int       `json:"row_count,omitempty"`
	ErrorCode string     `json:"error_code,omitempty"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
}

func handleCreateExport(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scope, ok := requireScope(w, r)
		if !ok {
			return
		}
		if !tenancy.Can(tenancy.ActionExportFinancialData, scope.Role, scope.Delegations, true) {
			writeError(w, http.StatusForbidden, "out_of_scope")
			return
		}
		var req createExportRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "malformed_request")
			return
		}
		if req.ExportType == "" {
			writeError(w, http.StatusBadRequest, "missing_required_field")
			return
		}

		from := time.Now().UTC().AddDate(0, 0, -30)
		to := time.Now().UTC()
		if req.From != "" {
			parsed, err := time.Parse("2006-01-02", req.From)
			if err != nil {
				writeError(w, http.StatusBadRequest, "invalid_from_date")
				return
			}
			from = parsed
		}
		if req.To != "" {
			parsed, err := time.Parse("2006-01-02", req.To)
			if err != nil {
				writeError(w, http.StatusBadRequest, "invalid_to_date")
				return
			}
			to = parsed
		}

		rec, err := deps.ExportsSvc.Create(r.Context(), scope, exports.CreateInput{
			ExportType: req.ExportType, BranchID: req.BranchID, From: from, To: to, Currency: req.Currency,
		})
		if err != nil {
			log.Printf("request_id=%s create_export error: %v", scope.RequestID, err)
			writeError(w, http.StatusInternalServerError, "internal_error")
			return
		}
		writeJSON(w, http.StatusAccepted, exportResponse{ExportID: rec.ID, Status: rec.Status})
	}
}

func handleGetExport(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scope, ok := requireScope(w, r)
		if !ok {
			return
		}
		exportID, err := uuid.Parse(chi.URLParam(r, "id"))
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_export_id")
			return
		}
		rec, found, err := deps.ExportsSvc.Get(r.Context(), scope, exportID)
		if err != nil {
			log.Printf("request_id=%s get_export error: %v", scope.RequestID, err)
			writeError(w, http.StatusInternalServerError, "internal_error")
			return
		}
		if !found {
			writeError(w, http.StatusNotFound, "not_found")
			return
		}
		writeJSON(w, http.StatusOK, exportResponse{
			ExportID: rec.ID, Status: rec.Status, RowCount: rec.RowCount,
			ErrorCode: rec.ErrorCode, ExpiresAt: rec.ExpiresAt,
		})
	}
}

func handleLiveness(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }
func handleReadiness(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }
}
