// Package catalog implements company reference data that other features
// depend on but the architecture's 10-route API table never separately
// specified: products (so a sale/purchase line can reference real tracked
// stock instead of free text) and customers (so a credit sale can be
// attributed to someone whose outstanding balance can be aged).
package catalog

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/ledgerlink/branchledger/backend/internal/tenancy"
)

// Product mirrors the `products` table.
type Product struct {
	ID            uuid.UUID
	CompanyID     uuid.UUID
	SKU           string
	Name          string
	Unit          string
	UnitPrecision int
	Kind          string // "stocked" | "recipe" | "service"
	ReorderPoint  decimal.Decimal
	IsActive      bool
}

// Customer mirrors a `counterparties` row of kind "customer".
type Customer struct {
	ID        uuid.UUID
	CompanyID uuid.UUID
	Name      string
	Contact   string
	IsActive  bool
}

// Supplier mirrors a `counterparties` row of kind "supplier" — same table
// and shape as Customer, kept as its own type so the API and callers never
// confuse which kind a given ID belongs to. A purchase's counterparty_id
// must be a supplier for the duplicate-invoice check (migration 0001's
// idx_transactions_duplicate_check) to mean anything: before this, nothing
// ever let a purchase set counterparty_id at all, so that index never
// matched a row.
type Supplier struct {
	ID        uuid.UUID
	CompanyID uuid.UUID
	Name      string
	Contact   string
	IsActive  bool
}

// Store is what Service needs from persistence.
type Store interface {
	ListProducts(ctx context.Context, companyID uuid.UUID) ([]Product, error)
	CreateProduct(ctx context.Context, p Product) (uuid.UUID, error)
	ListCustomers(ctx context.Context, companyID uuid.UUID) ([]Customer, error)
	CreateCustomer(ctx context.Context, c Customer) (uuid.UUID, error)
	ListSuppliers(ctx context.Context, companyID uuid.UUID) ([]Supplier, error)
	CreateSupplier(ctx context.Context, s Supplier) (uuid.UUID, error)
}

type Service struct {
	store Store
}

func NewService(store Store) *Service { return &Service{store: store} }

func (s *Service) ListProducts(ctx context.Context, scope tenancy.Scope) ([]Product, error) {
	return s.store.ListProducts(ctx, scope.CompanyID)
}

// CreateProductInput is what POST /api/v1/products gathers.
type CreateProductInput struct {
	SKU           string
	Name          string
	Unit          string
	UnitPrecision int
	Kind          string
	ReorderPoint  decimal.Decimal
}

func (s *Service) CreateProduct(ctx context.Context, scope tenancy.Scope, in CreateProductInput) (uuid.UUID, error) {
	if in.SKU == "" || in.Name == "" || in.Unit == "" {
		return uuid.Nil, fmt.Errorf("catalog: sku, name and unit are required")
	}
	kind := in.Kind
	if kind == "" {
		kind = "stocked"
	}
	precision := in.UnitPrecision
	if precision <= 0 {
		precision = 3
	}
	reorderPoint := in.ReorderPoint
	if reorderPoint.IsZero() {
		reorderPoint = decimal.NewFromInt(5)
	}
	return s.store.CreateProduct(ctx, Product{
		CompanyID: scope.CompanyID, SKU: in.SKU, Name: in.Name, Unit: in.Unit,
		UnitPrecision: precision, Kind: kind, ReorderPoint: reorderPoint, IsActive: true,
	})
}

func (s *Service) ListCustomers(ctx context.Context, scope tenancy.Scope) ([]Customer, error) {
	return s.store.ListCustomers(ctx, scope.CompanyID)
}

func (s *Service) CreateCustomer(ctx context.Context, scope tenancy.Scope, name, contact string) (uuid.UUID, error) {
	if name == "" {
		return uuid.Nil, fmt.Errorf("catalog: customer name is required")
	}
	return s.store.CreateCustomer(ctx, Customer{CompanyID: scope.CompanyID, Name: name, Contact: contact, IsActive: true})
}

func (s *Service) ListSuppliers(ctx context.Context, scope tenancy.Scope) ([]Supplier, error) {
	return s.store.ListSuppliers(ctx, scope.CompanyID)
}

func (s *Service) CreateSupplier(ctx context.Context, scope tenancy.Scope, name, contact string) (uuid.UUID, error) {
	if name == "" {
		return uuid.Nil, fmt.Errorf("catalog: supplier name is required")
	}
	return s.store.CreateSupplier(ctx, Supplier{CompanyID: scope.CompanyID, Name: name, Contact: contact, IsActive: true})
}
