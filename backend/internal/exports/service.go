// Package exports implements POST/GET /api/v1/exports: "Creates background
// export job" / "Job state and authorised download" (architecture API
// contract table). Per "External effects such as emails, exports and
// uploads do not occur inside the financial database commit," creating an
// export only records the request and enqueues outbox work; the worker
// (cmd/worker) does the actual file generation afterward, with its own
// idempotency key, and this package's Get is how a client polls the result.
package exports

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/ledgerlink/branchledger/backend/internal/tenancy"
)

// Record mirrors the `exports` table.
type Record struct {
	ID          uuid.UUID
	CompanyID   uuid.UUID
	RequestedBy uuid.UUID
	ExportType  string
	Scope       map[string]any
	Status      string // "pending" | "processing" | "ready" | "failed" | "expired"
	ObjectKey   *string
	RowCount    *int
	ErrorCode   string
	RequestedAt time.Time
	CompletedAt *time.Time
	ExpiresAt   *time.Time
}

// Repository is what Service needs from persistence.
type Repository interface {
	Create(ctx context.Context, rec Record) error
	Get(ctx context.Context, companyID, exportID uuid.UUID) (Record, bool, error)
	EnqueueOutboxJob(ctx context.Context, companyID uuid.UUID, jobType, idempotencyKey string, payload map[string]any) error
}

// CreateInput is what POST /exports gathers from a validated request.
type CreateInput struct {
	ExportType string
	BranchID   *uuid.UUID
	From       time.Time
	To         time.Time
	Currency   string
}

type Service struct {
	repo Repository
}

func NewService(repo Repository) *Service { return &Service{repo: repo} }

// Create records the export request at status "pending" and enqueues the
// outbox job that actually generates it. Authorisation (launch control:
// "Export financial data" in the permission matrix) is checked by the
// handler before calling this, the same way every other export-adjacent
// check in this codebase happens at the HTTP boundary.
func (s *Service) Create(ctx context.Context, scope tenancy.Scope, in CreateInput) (Record, error) {
	rec := Record{
		ID: uuid.New(), CompanyID: scope.CompanyID, RequestedBy: scope.UserID, ExportType: in.ExportType,
		Scope: map[string]any{
			"branch_id": in.BranchID, "from": in.From.Format("2006-01-02"), "to": in.To.Format("2006-01-02"),
			"currency": in.Currency,
		},
		Status: "pending", RequestedAt: time.Now().UTC(),
	}
	if err := s.repo.Create(ctx, rec); err != nil {
		return Record{}, err
	}
	// idempotency_key = the export's own ID: a retried outbox dispatch of
	// the same job can never generate the file twice.
	if err := s.repo.EnqueueOutboxJob(ctx, scope.CompanyID, "export", rec.ID.String(),
		map[string]any{"export_id": rec.ID}); err != nil {
		return Record{}, err
	}
	return rec, nil
}

// Get returns the export's current state, scoped to the caller's company —
// "available only to authorised users" starts with never returning another
// company's export for a guessed ID.
func (s *Service) Get(ctx context.Context, scope tenancy.Scope, exportID uuid.UUID) (Record, bool, error) {
	return s.repo.Get(ctx, scope.CompanyID, exportID)
}
