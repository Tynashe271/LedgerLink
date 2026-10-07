package httpapi

import (
	"errors"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/ledgerlink/branchledger/backend/internal/listings"
)

// --- GET /api/v1/transactions -------------------------------------------------

type transactionSummaryResponse struct {
	ID              uuid.UUID `json:"id"`
	BranchID        uuid.UUID `json:"branch_id"`
	BranchName      string    `json:"branch_name"`
	DocumentType    string    `json:"document_type"`
	DocumentDate    string    `json:"document_date"`
	CurrencyCode    string    `json:"currency_code"`
	Status          string    `json:"status"`
	NetAmount       string    `json:"net_amount"`
	SourceReference string    `json:"source_reference,omitempty"`
	Explanation     string    `json:"explanation,omitempty"`
}

// handleListTransactions serves GET /api/v1/transactions — the Transactions
// screen's "Search, create, view, reverse" (System Documentation 5.2).
// Query params: branch_id, document_type, status, from, to (YYYY-MM-DD,
// default the last 30 days), limit (default 50, max 200).
func handleListTransactions(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scope, ok := requireScope(w, r)
		if !ok {
			return
		}
		q := r.URL.Query()

		var branchID *uuid.UUID
		if raw := q.Get("branch_id"); raw != "" {
			id, err := uuid.Parse(raw)
			if err != nil {
				writeError(w, http.StatusBadRequest, "invalid_branch_id")
				return
			}
			branchID = &id
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

		limit := 50
		results, err := deps.ListingsSvc.Transactions(r.Context(), scope, listings.TransactionFilter{
			BranchID: branchID, DocumentType: q.Get("document_type"), Status: q.Get("status"),
			From: from, To: to, Limit: limit,
		})
		if err != nil {
			log.Printf("request_id=%s list_transactions error: %v", scope.RequestID, err)
			writeError(w, http.StatusInternalServerError, "internal_error")
			return
		}

		out := make([]transactionSummaryResponse, 0, len(results))
		for _, t := range results {
			out = append(out, transactionSummaryResponse{
				ID: t.ID, BranchID: t.BranchID, BranchName: t.BranchName, DocumentType: t.DocumentType,
				DocumentDate: t.DocumentDate.Format("2006-01-02"), CurrencyCode: t.CurrencyCode, Status: t.Status,
				NetAmount: t.NetAmount.StringFixed(2), SourceReference: t.SourceReference, Explanation: t.Explanation,
			})
		}
		writeJSON(w, http.StatusOK, out)
	}
}

// --- GET /api/v1/approvals ----------------------------------------------------

type approvalSummaryResponse struct {
	ApprovalID             uuid.UUID `json:"approval_id"`
	TransactionID          uuid.UUID `json:"transaction_id"`
	BranchID               uuid.UUID `json:"branch_id"`
	BranchName             string    `json:"branch_name"`
	DocumentType           string    `json:"document_type"`
	DocumentDate           string    `json:"document_date"`
	NetAmount              string    `json:"net_amount"`
	CurrencyCode           string    `json:"currency_code"`
	Explanation            string    `json:"explanation,omitempty"`
	RequestedByName        string    `json:"requested_by_name"`
	RequestedAt            time.Time `json:"requested_at"`
	RecordVersionAtRequest int       `json:"record_version_at_request"`
}

// handleListApprovals serves GET /api/v1/approvals — the Approvals screen's
// "Inspect, decide and escalate" (System Documentation 5.2).
func handleListApprovals(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scope, ok := requireScope(w, r)
		if !ok {
			return
		}
		results, err := deps.ListingsSvc.PendingApprovals(r.Context(), scope)
		if err != nil {
			log.Printf("request_id=%s list_approvals error: %v", scope.RequestID, err)
			writeError(w, http.StatusInternalServerError, "internal_error")
			return
		}
		out := make([]approvalSummaryResponse, 0, len(results))
		for _, a := range results {
			out = append(out, approvalSummaryResponse{
				ApprovalID: a.ApprovalID, TransactionID: a.TransactionID, BranchID: a.BranchID, BranchName: a.BranchName,
				DocumentType: a.DocumentType, DocumentDate: a.DocumentDate.Format("2006-01-02"),
				NetAmount: a.NetAmount.StringFixed(2), CurrencyCode: a.CurrencyCode, Explanation: a.Explanation,
				RequestedByName: a.RequestedByName, RequestedAt: a.RequestedAt, RecordVersionAtRequest: a.RecordVersionAtRequest,
			})
		}
		writeJSON(w, http.StatusOK, out)
	}
}

// --- GET /api/v1/transfers ----------------------------------------------------

type transferSummaryResponse struct {
	ID                 uuid.UUID `json:"id"`
	TransferType       string    `json:"transfer_type"`
	SenderBranchName   string    `json:"sender_branch_name"`
	ReceiverBranchID   uuid.UUID `json:"receiver_branch_id"`
	ReceiverBranchName string    `json:"receiver_branch_name"`
	Status             string    `json:"status"`
	Amount             string    `json:"amount"`
	ReceivedAmount     string    `json:"received_amount"`
	RemainingAmount    string    `json:"remaining_amount"`
	CurrencyCode       string    `json:"currency_code"`
	DispatchedAt       time.Time `json:"dispatched_at"`
}

// handleListTransfers serves GET /api/v1/transfers — "Stock [and money]
// Count, adjust, waste and transfer" / the Branch overview's transfer
// in-transit visibility (System Documentation 5.2, 5.7).
func handleListTransfers(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scope, ok := requireScope(w, r)
		if !ok {
			return
		}
		results, err := deps.ListingsSvc.OpenTransfers(r.Context(), scope)
		if err != nil {
			log.Printf("request_id=%s list_transfers error: %v", scope.RequestID, err)
			writeError(w, http.StatusInternalServerError, "internal_error")
			return
		}
		out := make([]transferSummaryResponse, 0, len(results))
		for _, t := range results {
			out = append(out, transferSummaryResponse{
				ID: t.ID, TransferType: t.TransferType, SenderBranchName: t.SenderBranchName,
				ReceiverBranchID: t.ReceiverBranchID, ReceiverBranchName: t.ReceiverBranchName, Status: t.Status,
				Amount: t.Amount.StringFixed(2), ReceivedAmount: t.ReceivedAmount.StringFixed(2),
				RemainingAmount: t.Amount.Sub(t.ReceivedAmount).StringFixed(2), CurrencyCode: t.CurrencyCode,
				DispatchedAt: t.DispatchedAt,
			})
		}
		writeJSON(w, http.StatusOK, out)
	}
}

// --- GET /api/v1/stock --------------------------------------------------------

type stockPositionResponse struct {
	ProductID   uuid.UUID `json:"product_id"`
	SKU         string    `json:"sku"`
	ProductName string    `json:"product_name"`
	Unit        string    `json:"unit"`
	BranchID    uuid.UUID `json:"branch_id"`
	BranchName  string    `json:"branch_name"`
	Quantity    string    `json:"quantity"`
	Value       string    `json:"value"`
	UnitCost    string    `json:"unit_cost"`
}

// handleListStock serves GET /api/v1/stock — the Stock screen's "Count,
// adjust, waste and transfer" (System Documentation 5.2), listing every
// product's current on-hand balance and valuation at every branch in the
// caller's scope.
func handleListStock(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scope, ok := requireScope(w, r)
		if !ok {
			return
		}
		results, err := deps.ListingsSvc.StockPositions(r.Context(), scope)
		if err != nil {
			log.Printf("request_id=%s list_stock error: %v", scope.RequestID, err)
			writeError(w, http.StatusInternalServerError, "internal_error")
			return
		}
		out := make([]stockPositionResponse, 0, len(results))
		for _, s := range results {
			unitCost := "0.0000"
			if !s.Quantity.IsZero() {
				unitCost = s.Value.Div(s.Quantity).Round(4).String()
			}
			out = append(out, stockPositionResponse{
				ProductID: s.ProductID, SKU: s.SKU, ProductName: s.ProductName, Unit: s.Unit,
				BranchID: s.BranchID, BranchName: s.BranchName,
				Quantity: s.Quantity.StringFixed(3), Value: s.Value.StringFixed(2), UnitCost: unitCost,
			})
		}
		writeJSON(w, http.StatusOK, out)
	}
}

// --- GET /api/v1/audit-events --------------------------------------------------

type auditEventResponse struct {
	ID         int64          `json:"id"`
	ActorName  string         `json:"actor_name,omitempty"`
	EventType  string         `json:"event_type"`
	RecordType string         `json:"record_type,omitempty"`
	RecordID   *uuid.UUID     `json:"record_id,omitempty"`
	Details    map[string]any `json:"details"`
	ServerTime time.Time      `json:"server_time"`
}

// handleListAuditEvents serves GET /api/v1/audit-events — section 7.5's
// audit evidence requirement, restricted to tenancy.CompanyWideReadRoles.
// Query params: from, to (YYYY-MM-DD, default the last 30 days), limit
// (default 100, max 200).
func handleListAuditEvents(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scope, ok := requireScope(w, r)
		if !ok {
			return
		}
		q := r.URL.Query()

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
		to = to.Add(24*time.Hour - time.Nanosecond)

		limit := 100
		if raw := q.Get("limit"); raw != "" {
			if parsed, err := strconv.Atoi(raw); err == nil {
				limit = parsed
			}
		}

		results, err := deps.ListingsSvc.AuditEvents(r.Context(), scope, from, to, limit)
		if errors.Is(err, listings.ErrCompanyWideReadRequired) {
			writeError(w, http.StatusForbidden, "out_of_scope")
			return
		}
		if err != nil {
			log.Printf("request_id=%s list_audit_events error: %v", scope.RequestID, err)
			writeError(w, http.StatusInternalServerError, "internal_error")
			return
		}

		out := make([]auditEventResponse, 0, len(results))
		for _, e := range results {
			out = append(out, auditEventResponse{
				ID: e.ID, ActorName: e.ActorName, EventType: e.EventType, RecordType: e.RecordType,
				RecordID: e.RecordID, Details: e.Details, ServerTime: e.ServerTime,
			})
		}
		writeJSON(w, http.StatusOK, out)
	}
}
