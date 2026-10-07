// Package httpapi wires the HTTP surface described in
// BranchLedger_Architecture.pdf ("API contracts and state transitions") to the
// domain services. Handlers only decode/encode and enforce transport-level
// concerns (batch size, body limits); all business rules live in
// internal/accounting, internal/sync, internal/tenancy.
package httpapi

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/ledgerlink/branchledger/backend/internal/accounting"
	"github.com/ledgerlink/branchledger/backend/internal/auth"
	"github.com/ledgerlink/branchledger/backend/internal/catalog"
	"github.com/ledgerlink/branchledger/backend/internal/exports"
	"github.com/ledgerlink/branchledger/backend/internal/listings"
	"github.com/ledgerlink/branchledger/backend/internal/reporting"
	"github.com/ledgerlink/branchledger/backend/internal/sync"
)

// MaxRequestBodyBytes enforces launch control #34 ("Request body bounds"):
// large or deeply nested requests fail early rather than being buffered
// without limit. The architecture proposes 1 MiB as the default JSON request
// limit.
const MaxRequestBodyBytes = 1 << 20 // 1 MiB

// Deps bundles everything the router needs to construct handlers. Built in
// cmd/api/main.go once the storage layer and services are wired up.
type Deps struct {
	Verifier      *auth.Verifier
	AccountingSvc *accounting.Service
	Pusher        *sync.Pusher
	Puller        *sync.Puller
	ReportingSvc  *reporting.Service
	ExportsSvc    *exports.Service
	ListingsSvc   *listings.Service
	CatalogSvc    *catalog.Service
}

// NewRouter builds the complete /api/v1 surface. Every route requires a valid
// delegation token (RequireDelegation) — there is no anonymous route here;
// login and session establishment belong to the Laravel gateway, not Go.
func NewRouter(deps Deps) http.Handler {
	r := chi.NewRouter()

	r.Use(middleware.RequestID)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(30 * time.Second)) // propagated context deadline, per launch control #8
	r.Use(bodyLimit(MaxRequestBodyBytes))

	r.Route("/api/v1", func(r chi.Router) {
		r.Use(auth.RequireDelegation(deps.Verifier))

		r.Post("/sync/push", handleSyncPush(deps))
		r.Get("/sync/pull", handleSyncPull(deps))
		r.Get("/dashboard", handleDashboard(deps))

		r.Get("/transactions", handleListTransactions(deps))
		r.Post("/transactions", handlePostTransaction(deps))
		r.Post("/transactions/{id}/reverse", handleReverseTransaction(deps))

		r.Get("/approvals", handleListApprovals(deps))
		r.Get("/transfers", handleListTransfers(deps))
		r.Get("/stock", handleListStock(deps))

		r.Get("/products", handleListProducts(deps))
		r.Post("/products", handleCreateProduct(deps))
		r.Get("/customers", handleListCustomers(deps))
		r.Post("/customers", handleCreateCustomer(deps))

		r.Post("/closes", handleCreateClose(deps))
		r.Post("/approvals/{id}/decision", handleApprovalDecision(deps))
		r.Post("/transfers/{id}/receive", handleTransferReceive(deps))

		r.Post("/exports", handleCreateExport(deps))
		r.Get("/exports/{id}", handleGetExport(deps))
	})

	r.Get("/healthz", handleLiveness)
	r.Get("/readyz", handleReadiness(deps))

	return r
}

// bodyLimit wraps the request body in http.MaxBytesReader so an oversized
// payload fails with a clear error instead of unbounded memory growth.
func bodyLimit(n int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.Body = http.MaxBytesReader(w, r.Body, n)
			next.ServeHTTP(w, r)
		})
	}
}
