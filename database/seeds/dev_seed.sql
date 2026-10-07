-- Development/demo seed: one company "Alpha Traders" with a main branch, a
-- grocery-style chart of accounts, an open financial period, and an owner
-- user. Mirrors the fictional tenant "Alpha" described in
-- BranchLedger_System_Documentation section 7.4.

INSERT INTO companies (id, name, primary_category, reporting_currency, financial_year_start)
VALUES ('11111111-1111-1111-1111-111111111111', 'Alpha Traders', 'retail_wholesale', 'USD', '2026-01-01');

INSERT INTO branches (id, company_id, name, code, category, is_main_branch)
VALUES ('22222222-2222-2222-2222-222222222222', '11111111-1111-1111-1111-111111111111',
        'Main Branch', 'MAIN', 'retail_wholesale', true);

INSERT INTO company_currencies (company_id, currency_code, is_reporting)
VALUES ('11111111-1111-1111-1111-111111111111', 'USD', true);

INSERT INTO periods (company_id, starts_on, ends_on, status)
VALUES ('11111111-1111-1111-1111-111111111111', '2026-01-01', '2026-12-31', 'open');

-- Standard chart of accounts codes expected by storage.txImpl.GetChartAccounts.
INSERT INTO accounts (company_id, code, name, account_type, is_cash_like) VALUES
    ('11111111-1111-1111-1111-111111111111', '1000', 'Cash',                'asset',     true),
    ('11111111-1111-1111-1111-111111111111', '1010', 'Bank',                'asset',     true),
    ('11111111-1111-1111-1111-111111111111', '1100', 'Accounts Receivable', 'asset',     false),
    ('11111111-1111-1111-1111-111111111111', '1200', 'Inventory',           'asset',     false),
    ('11111111-1111-1111-1111-111111111111', '1300', 'Funds In Transit',    'asset',     false),
    ('11111111-1111-1111-1111-111111111111', '1500', 'Fixed Assets',        'asset',     false),
    ('11111111-1111-1111-1111-111111111111', '2000', 'Accounts Payable',    'liability', false),
    ('11111111-1111-1111-1111-111111111111', '3000', 'Owner Capital',       'equity',    false),
    ('11111111-1111-1111-1111-111111111111', '4000', 'Sales Revenue',       'income',    false),
    ('11111111-1111-1111-1111-111111111111', '4100', 'Sales Returns',       'income',    false),
    ('11111111-1111-1111-1111-111111111111', '5000', 'Cost of Sales',       'expense',   false),
    ('11111111-1111-1111-1111-111111111111', '5100', 'Stock Loss Expense',  'expense',   false),
    ('11111111-1111-1111-1111-111111111111', '6000', 'Operating Expense',   'expense',   false);

-- Password is "BranchLedger!2026" (bcrypt, cost 12) — a throwaway dev-only
-- credential. Never reuse this hash or password outside a local database.
INSERT INTO users (id, email, password_hash, full_name)
VALUES ('33333333-3333-3333-3333-333333333333', 'owner@alpha.example',
        '$2y$12$gfiMjGePYnpD40HN.axVaOZ3YbNSgJMcI6RrDOTotGspe4JdDGxVi', 'Alpha Owner');

INSERT INTO memberships (user_id, company_id, role, branch_scope)
VALUES ('33333333-3333-3333-3333-333333333333', '11111111-1111-1111-1111-111111111111', 'owner', '{}');

-- A product so a sale line can exercise the weighted-average costing path.
INSERT INTO products (id, company_id, sku, name, unit)
VALUES ('44444444-4444-4444-4444-444444444444', '11111111-1111-1111-1111-111111111111',
        'SKU-001', 'Bag of Mealie Meal', 'each');

INSERT INTO stock_movements (company_id, branch_id, product_id, movement_type,
                              quantity_delta, unit_cost, running_quantity, running_value, created_by)
VALUES ('11111111-1111-1111-1111-111111111111', '22222222-2222-2222-2222-222222222222',
        '44444444-4444-4444-4444-444444444444', 'receiving', 20, 6.00, 20, 120.00,
        '33333333-3333-3333-3333-333333333333');
