package storage

import (
	"context"

	"github.com/google/uuid"

	"github.com/ledgerlink/branchledger/backend/internal/catalog"
)

// Catalog returns a view of DB implementing catalog.Store.
func (db *DB) Catalog() *catalogRepo { return &catalogRepo{db: db} }

type catalogRepo struct{ db *DB }

func (r *catalogRepo) ListProducts(ctx context.Context, companyID uuid.UUID) ([]catalog.Product, error) {
	rows, err := r.db.pool.Query(ctx, `
		SELECT id, company_id, sku, name, unit, unit_precision, kind, reorder_point, is_active
		FROM products WHERE company_id = $1 AND is_active ORDER BY name`, companyID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []catalog.Product
	for rows.Next() {
		var p catalog.Product
		if err := rows.Scan(&p.ID, &p.CompanyID, &p.SKU, &p.Name, &p.Unit, &p.UnitPrecision, &p.Kind, &p.ReorderPoint, &p.IsActive); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (r *catalogRepo) CreateProduct(ctx context.Context, p catalog.Product) (uuid.UUID, error) {
	var id uuid.UUID
	err := r.db.pool.QueryRow(ctx, `
		INSERT INTO products (company_id, sku, name, unit, unit_precision, kind, reorder_point, is_active)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8) RETURNING id`,
		p.CompanyID, p.SKU, p.Name, p.Unit, p.UnitPrecision, p.Kind, p.ReorderPoint, p.IsActive).Scan(&id)
	return id, err
}

func (r *catalogRepo) ListCustomers(ctx context.Context, companyID uuid.UUID) ([]catalog.Customer, error) {
	rows, err := r.db.pool.Query(ctx, `
		SELECT id, company_id, name, coalesce(contact, ''), is_active
		FROM counterparties WHERE company_id = $1 AND kind = 'customer' AND is_active ORDER BY name`, companyID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []catalog.Customer
	for rows.Next() {
		var c catalog.Customer
		if err := rows.Scan(&c.ID, &c.CompanyID, &c.Name, &c.Contact, &c.IsActive); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (r *catalogRepo) CreateCustomer(ctx context.Context, c catalog.Customer) (uuid.UUID, error) {
	var id uuid.UUID
	err := r.db.pool.QueryRow(ctx, `
		INSERT INTO counterparties (company_id, kind, name, contact, is_active)
		VALUES ($1,'customer',$2,$3,$4) RETURNING id`,
		c.CompanyID, c.Name, nullableString(c.Contact), c.IsActive).Scan(&id)
	return id, err
}

func (r *catalogRepo) ListSuppliers(ctx context.Context, companyID uuid.UUID) ([]catalog.Supplier, error) {
	rows, err := r.db.pool.Query(ctx, `
		SELECT id, company_id, name, coalesce(contact, ''), is_active
		FROM counterparties WHERE company_id = $1 AND kind = 'supplier' AND is_active ORDER BY name`, companyID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []catalog.Supplier
	for rows.Next() {
		var s catalog.Supplier
		if err := rows.Scan(&s.ID, &s.CompanyID, &s.Name, &s.Contact, &s.IsActive); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func (r *catalogRepo) CreateSupplier(ctx context.Context, s catalog.Supplier) (uuid.UUID, error) {
	var id uuid.UUID
	err := r.db.pool.QueryRow(ctx, `
		INSERT INTO counterparties (company_id, kind, name, contact, is_active)
		VALUES ($1,'supplier',$2,$3,$4) RETURNING id`,
		s.CompanyID, s.Name, nullableString(s.Contact), s.IsActive).Scan(&id)
	return id, err
}
