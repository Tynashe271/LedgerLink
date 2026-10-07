package httpapi

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/ledgerlink/branchledger/backend/internal/catalog"
)

// --- GET/POST /api/v1/products -----------------------------------------------

type productResponse struct {
	ID            uuid.UUID `json:"id"`
	SKU           string    `json:"sku"`
	Name          string    `json:"name"`
	Unit          string    `json:"unit"`
	UnitPrecision int       `json:"unit_precision"`
	Kind          string    `json:"kind"`
	ReorderPoint  string    `json:"reorder_point"`
}

func handleListProducts(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scope, ok := requireScope(w, r)
		if !ok {
			return
		}
		products, err := deps.CatalogSvc.ListProducts(r.Context(), scope)
		if err != nil {
			log.Printf("request_id=%s list_products error: %v", scope.RequestID, err)
			writeError(w, http.StatusInternalServerError, "internal_error")
			return
		}
		out := make([]productResponse, 0, len(products))
		for _, p := range products {
			out = append(out, productResponse{
				ID: p.ID, SKU: p.SKU, Name: p.Name, Unit: p.Unit, UnitPrecision: p.UnitPrecision,
				Kind: p.Kind, ReorderPoint: p.ReorderPoint.StringFixed(2),
			})
		}
		writeJSON(w, http.StatusOK, out)
	}
}

type createProductRequest struct {
	SKU           string          `json:"sku"`
	Name          string          `json:"name"`
	Unit          string          `json:"unit"`
	UnitPrecision int             `json:"unit_precision"`
	Kind          string          `json:"kind"`
	ReorderPoint  decimal.Decimal `json:"reorder_point"`
}

func handleCreateProduct(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scope, ok := requireScope(w, r)
		if !ok {
			return
		}
		var req createProductRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "malformed_request")
			return
		}
		id, err := deps.CatalogSvc.CreateProduct(r.Context(), scope, catalog.CreateProductInput{
			SKU: req.SKU, Name: req.Name, Unit: req.Unit, UnitPrecision: req.UnitPrecision,
			Kind: req.Kind, ReorderPoint: req.ReorderPoint,
		})
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_product")
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{"id": id})
	}
}

// --- GET/POST /api/v1/customers -----------------------------------------------

type customerResponse struct {
	ID      uuid.UUID `json:"id"`
	Name    string    `json:"name"`
	Contact string    `json:"contact,omitempty"`
}

func handleListCustomers(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scope, ok := requireScope(w, r)
		if !ok {
			return
		}
		customers, err := deps.CatalogSvc.ListCustomers(r.Context(), scope)
		if err != nil {
			log.Printf("request_id=%s list_customers error: %v", scope.RequestID, err)
			writeError(w, http.StatusInternalServerError, "internal_error")
			return
		}
		out := make([]customerResponse, 0, len(customers))
		for _, c := range customers {
			out = append(out, customerResponse{ID: c.ID, Name: c.Name, Contact: c.Contact})
		}
		writeJSON(w, http.StatusOK, out)
	}
}

type createCustomerRequest struct {
	Name    string `json:"name"`
	Contact string `json:"contact"`
}

func handleCreateCustomer(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scope, ok := requireScope(w, r)
		if !ok {
			return
		}
		var req createCustomerRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "malformed_request")
			return
		}
		id, err := deps.CatalogSvc.CreateCustomer(r.Context(), scope, req.Name, req.Contact)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_customer")
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{"id": id})
	}
}
