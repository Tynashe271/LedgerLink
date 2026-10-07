DROP INDEX IF EXISTS idx_transactions_company_due_date;
ALTER TABLE transactions DROP COLUMN IF EXISTS due_date;
ALTER TABLE products DROP COLUMN IF EXISTS reorder_point;
