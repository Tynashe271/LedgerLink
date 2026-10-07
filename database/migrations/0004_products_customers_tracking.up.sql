-- Supports two features that need real prerequisite data, not just a new
-- alert query: low-stock alerts need actual products with tracked stock
-- (products already existed, but nothing posted "receiving" stock
-- movements, and nothing let a user pick a product on a sale — so no stock
-- was ever actually tracked through the UI); overdue-debt alerts need a due
-- date on credit sales and a way to record which customer a receipt settles.

-- A simple per-product reorder point. No per-branch override in this pass —
-- one company-wide threshold is enough to make the alert meaningful without
-- a full replenishment-planning feature.
ALTER TABLE products ADD COLUMN reorder_point numeric(18,3) NOT NULL DEFAULT 5;

-- Only meaningful for credit sales (and, later, purchases on credit terms);
-- null for cash transactions. Defaulted client-side to document_date + 30
-- days, editable per sale.
ALTER TABLE transactions ADD COLUMN due_date date;

CREATE INDEX idx_transactions_company_due_date ON transactions (company_id, due_date)
    WHERE due_date IS NOT NULL;
