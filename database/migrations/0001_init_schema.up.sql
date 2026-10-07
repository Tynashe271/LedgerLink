-- BranchLedger initial schema
-- Implements the data model in BranchLedger_Architecture.pdf (Data model and database
-- constraints) and BranchLedger_System_Documentation (Requirements Analysis / System Design).
-- All tenant tables are scoped by company_id (and usually branch_id) to prevent cross-company
-- references, per "Use company-scoped foreign keys to prevent cross-company references."

CREATE EXTENSION IF NOT EXISTS pgcrypto; -- gen_random_uuid()
CREATE EXTENSION IF NOT EXISTS citext;   -- case-insensitive email

-- ---------------------------------------------------------------------------
-- Companies and branches
-- ---------------------------------------------------------------------------

CREATE TABLE companies (
    id                  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name                varchar(160) NOT NULL,
    primary_category    varchar(40) NOT NULL,
    secondary_categories text[] NOT NULL DEFAULT '{}',
    reporting_currency  char(3) NOT NULL,
    timezone            varchar(64) NOT NULL DEFAULT 'Africa/Harare',
    financial_year_start date NOT NULL,
    status              varchar(20) NOT NULL DEFAULT 'active'
                            CHECK (status IN ('active', 'suspended', 'cancelled')),
    created_at          timestamptz NOT NULL DEFAULT now(),
    updated_at          timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE branches (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    company_id      uuid NOT NULL REFERENCES companies(id),
    name            varchar(160) NOT NULL,
    code            varchar(32) NOT NULL,
    category        varchar(40) NOT NULL, -- overrides company primary_category defaults
    is_main_branch  boolean NOT NULL DEFAULT false,
    status          varchar(20) NOT NULL DEFAULT 'active'
                        CHECK (status IN ('active', 'suspended', 'closed')),
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),
    UNIQUE (company_id, code)
);

CREATE INDEX idx_branches_company ON branches (company_id);

-- Allowed transaction currencies per company, with exchange rate history.
CREATE TABLE company_currencies (
    company_id      uuid NOT NULL REFERENCES companies(id),
    currency_code   char(3) NOT NULL,
    is_reporting    boolean NOT NULL DEFAULT false,
    PRIMARY KEY (company_id, currency_code)
);

CREATE TABLE exchange_rates (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    company_id      uuid NOT NULL REFERENCES companies(id),
    currency_code   char(3) NOT NULL,
    reporting_units_per_unit numeric(18,6) NOT NULL CHECK (reporting_units_per_unit > 0),
    effective_from  timestamptz NOT NULL,
    created_at      timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX idx_exchange_rates_lookup ON exchange_rates (company_id, currency_code, effective_from DESC);

-- ---------------------------------------------------------------------------
-- Users, memberships, devices
-- ---------------------------------------------------------------------------

CREATE TABLE users (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    email           citext NOT NULL UNIQUE,
    password_hash   text NOT NULL,
    full_name       varchar(160) NOT NULL,
    status          varchar(20) NOT NULL DEFAULT 'active'
                        CHECK (status IN ('active', 'locked', 'disabled')),
    failed_login_count smallint NOT NULL DEFAULT 0,
    locked_until    timestamptz,
    access_version  integer NOT NULL DEFAULT 1, -- bumped on permission change; sync devices recheck
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE memberships (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id         uuid NOT NULL REFERENCES users(id),
    company_id      uuid NOT NULL REFERENCES companies(id),
    role            varchar(30) NOT NULL
                        CHECK (role IN ('owner', 'general_manager', 'accountant',
                                         'branch_manager', 'staff', 'platform_admin')),
    branch_scope    uuid[] NOT NULL DEFAULT '{}', -- empty = all branches (owner/GM/accountant)
    approval_limit  numeric(18,2),
    is_active       boolean NOT NULL DEFAULT true,
    created_at      timestamptz NOT NULL DEFAULT now(),
    UNIQUE (user_id, company_id)
);

CREATE INDEX idx_memberships_company ON memberships (company_id);

CREATE TABLE devices (
    id                  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    company_id          uuid NOT NULL REFERENCES companies(id),
    branch_id           uuid NOT NULL REFERENCES branches(id),
    user_id             uuid NOT NULL REFERENCES users(id),
    label               varchar(120) NOT NULL,
    is_offline_writer    boolean NOT NULL DEFAULT false, -- designated single offline writer per branch
    enrolled_at         timestamptz NOT NULL DEFAULT now(),
    lease_expires_at    timestamptz NOT NULL,
    revoked_at          timestamptz,
    last_sync_at        timestamptz,
    created_at          timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX idx_devices_company_branch ON devices (company_id, branch_id);

-- Enforce at most one active designated offline writer per branch.
CREATE UNIQUE INDEX uq_devices_single_offline_writer
    ON devices (branch_id)
    WHERE is_offline_writer AND revoked_at IS NULL;

-- ---------------------------------------------------------------------------
-- Chart of accounts and periods
-- ---------------------------------------------------------------------------

CREATE TABLE accounts (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    company_id      uuid NOT NULL REFERENCES companies(id),
    code            varchar(20) NOT NULL,
    name            varchar(160) NOT NULL,
    account_type    varchar(20) NOT NULL
                        CHECK (account_type IN ('asset', 'liability', 'equity', 'income', 'expense')),
    is_cash_like    boolean NOT NULL DEFAULT false, -- cash/bank/mobile money for close calculations
    is_active       boolean NOT NULL DEFAULT true,
    created_at      timestamptz NOT NULL DEFAULT now(),
    UNIQUE (company_id, code)
);

CREATE INDEX idx_accounts_company ON accounts (company_id, account_type);

CREATE TABLE periods (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    company_id      uuid NOT NULL REFERENCES companies(id),
    starts_on       date NOT NULL,
    ends_on         date NOT NULL,
    status          varchar(20) NOT NULL DEFAULT 'open'
                        CHECK (status IN ('open', 'locked')),
    locked_at       timestamptz,
    locked_by       uuid REFERENCES users(id),
    created_at      timestamptz NOT NULL DEFAULT now(),
    CHECK (ends_on > starts_on)
);

CREATE INDEX idx_periods_company_range ON periods (company_id, starts_on, ends_on);
-- Prevent overlapping periods per company (checked additionally in application code
-- since exclusion constraints need btree_gist; enforced here for date ranges).
CREATE EXTENSION IF NOT EXISTS btree_gist;
ALTER TABLE periods ADD CONSTRAINT no_overlapping_periods
    EXCLUDE USING gist (company_id WITH =, daterange(starts_on, ends_on, '[]') WITH &&);

-- ---------------------------------------------------------------------------
-- Idempotent operations (sync push targets) and outbox
-- ---------------------------------------------------------------------------

-- Every client-submitted command lands here first keyed by (company_id, operation_id),
-- giving duplicate-safe at-least-once processing per the synchronisation sequence.
CREATE TABLE operations (
    company_id          uuid NOT NULL REFERENCES companies(id),
    operation_id        uuid NOT NULL,
    branch_id           uuid NOT NULL REFERENCES branches(id),
    device_id           uuid REFERENCES devices(id),
    actor_user_id        uuid NOT NULL REFERENCES users(id),
    command_type        varchar(40) NOT NULL,
    payload_hash        text NOT NULL,
    status               varchar(20) NOT NULL DEFAULT 'accepted'
                            CHECK (status IN ('accepted', 'rejected')),
    result_record_type  varchar(40),
    result_record_id    uuid,
    result_version      integer,
    error_code           varchar(60),
    client_submitted_at  timestamptz NOT NULL,
    server_received_at   timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (company_id, operation_id)
);

CREATE TABLE outbox_jobs (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    company_id      uuid NOT NULL REFERENCES companies(id),
    job_type        varchar(40) NOT NULL, -- e.g. export, alert, attachment_process, summary_refresh
    payload         jsonb NOT NULL,
    status          varchar(20) NOT NULL DEFAULT 'pending'
                        CHECK (status IN ('pending', 'processing', 'done', 'failed')),
    attempts        integer NOT NULL DEFAULT 0,
    idempotency_key text NOT NULL,
    available_at    timestamptz NOT NULL DEFAULT now(),
    last_error      text,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),
    UNIQUE (company_id, job_type, idempotency_key)
);

CREATE INDEX idx_outbox_jobs_poll ON outbox_jobs (status, available_at) WHERE status IN ('pending', 'processing');

-- Change feed for sync pull: every committed change a client may need, in cursor order.
CREATE TABLE sync_changes (
    cursor          bigserial PRIMARY KEY,
    company_id      uuid NOT NULL REFERENCES companies(id),
    branch_id       uuid NOT NULL REFERENCES branches(id),
    record_type     varchar(40) NOT NULL,
    record_id       uuid NOT NULL,
    record_version  integer NOT NULL,
    change_kind     varchar(20) NOT NULL CHECK (change_kind IN ('created', 'updated', 'reversed')),
    occurred_at     timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX idx_sync_changes_company_cursor ON sync_changes (company_id, cursor);

-- ---------------------------------------------------------------------------
-- Counterparties (customers and suppliers)
-- ---------------------------------------------------------------------------

CREATE TABLE counterparties (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    company_id      uuid NOT NULL REFERENCES companies(id),
    kind            varchar(10) NOT NULL CHECK (kind IN ('customer', 'supplier')),
    name            varchar(160) NOT NULL,
    contact         varchar(160),
    is_active       boolean NOT NULL DEFAULT true,
    created_at      timestamptz NOT NULL DEFAULT now(),
    UNIQUE (company_id, kind, name)
);

CREATE INDEX idx_counterparties_company ON counterparties (company_id, kind);

-- ---------------------------------------------------------------------------
-- Transactions (source documents) and journals (postings)
-- ---------------------------------------------------------------------------

CREATE TABLE transactions (
    id                  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    company_id          uuid NOT NULL REFERENCES companies(id),
    branch_id           uuid NOT NULL REFERENCES branches(id),
    operation_id        uuid NOT NULL,
    document_type       varchar(20) NOT NULL
                            CHECK (document_type IN ('sale', 'purchase', 'expense', 'receipt',
                                                       'payment', 'transfer', 'stock_adjustment',
                                                       'close')),
    document_date       date NOT NULL,
    currency_code       char(3) NOT NULL,
    exchange_rate       numeric(18,6) NOT NULL DEFAULT 1,
    counterparty_id     uuid REFERENCES counterparties(id),
    payment_account_id  uuid REFERENCES accounts(id),
    source_reference     varchar(100),
    explanation          varchar(2000),
    status               varchar(20) NOT NULL DEFAULT 'draft'
                            CHECK (status IN ('draft', 'awaiting_approval', 'posted',
                                                'reversed', 'rejected')),
    revision             integer NOT NULL DEFAULT 1,
    created_by           uuid NOT NULL REFERENCES users(id),
    created_at           timestamptz NOT NULL DEFAULT now(),
    posted_at            timestamptz,
    reversed_by_transaction_id uuid REFERENCES transactions(id),
    reverses_transaction_id    uuid REFERENCES transactions(id),
    UNIQUE (company_id, operation_id)
);

CREATE INDEX idx_transactions_company_branch_date ON transactions (company_id, branch_id, document_date);
CREATE INDEX idx_transactions_company_status ON transactions (company_id, status);
-- Supplier-invoice duplicate detection: same supplier + reference flagged before posting.
CREATE INDEX idx_transactions_duplicate_check
    ON transactions (company_id, counterparty_id, source_reference)
    WHERE document_type = 'purchase';

CREATE TABLE transaction_lines (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    transaction_id  uuid NOT NULL REFERENCES transactions(id),
    line_no         integer NOT NULL,
    product_id      uuid, -- FK added after products table
    description     varchar(500),
    quantity        numeric(18,3) NOT NULL DEFAULT 1 CHECK (quantity > 0),
    unit_price      numeric(18,4) NOT NULL DEFAULT 0 CHECK (unit_price >= 0),
    discount        numeric(18,4) NOT NULL DEFAULT 0,
    tax_code        varchar(20),
    line_net        numeric(18,2) NOT NULL,
    UNIQUE (transaction_id, line_no)
);

-- Authoritative balanced journal committed by Go. Immutable once posted.
CREATE TABLE journals (
    id                  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    company_id          uuid NOT NULL REFERENCES companies(id),
    branch_id           uuid NOT NULL REFERENCES branches(id),
    source_transaction_id uuid NOT NULL REFERENCES transactions(id),
    period_id           uuid NOT NULL REFERENCES periods(id),
    posted_at           timestamptz NOT NULL DEFAULT now(),
    posted_by           uuid NOT NULL REFERENCES users(id),
    reporting_currency  char(3) NOT NULL,
    created_at          timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX idx_journals_company_branch_date ON journals (company_id, branch_id, posted_at);
CREATE INDEX idx_journals_source ON journals (source_transaction_id);

CREATE TABLE journal_lines (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    journal_id      uuid NOT NULL REFERENCES journals(id),
    company_id      uuid NOT NULL REFERENCES companies(id),
    account_id      uuid NOT NULL REFERENCES accounts(id),
    side            varchar(6) NOT NULL CHECK (side IN ('debit', 'credit')),
    original_amount numeric(18,2) NOT NULL CHECK (original_amount > 0),
    reporting_amount numeric(18,2) NOT NULL CHECK (reporting_amount > 0),
    line_no         integer NOT NULL,
    UNIQUE (journal_id, line_no)
);

CREATE INDEX idx_journal_lines_account ON journal_lines (company_id, account_id);

-- transaction_lines.product_id is left unconstrained here; the FK to products
-- is added below once that table exists.

-- ---------------------------------------------------------------------------
-- Inventory: products, recipes, stock movements
-- ---------------------------------------------------------------------------

CREATE TABLE products (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    company_id      uuid NOT NULL REFERENCES companies(id),
    sku             varchar(60) NOT NULL,
    name            varchar(160) NOT NULL,
    unit            varchar(20) NOT NULL, -- kg, each, litre, etc.
    unit_precision  smallint NOT NULL DEFAULT 3,
    kind            varchar(20) NOT NULL DEFAULT 'stocked'
                        CHECK (kind IN ('stocked', 'recipe', 'service')),
    is_active       boolean NOT NULL DEFAULT true,
    created_at      timestamptz NOT NULL DEFAULT now(),
    UNIQUE (company_id, sku)
);

CREATE INDEX idx_products_company ON products (company_id);

ALTER TABLE transaction_lines
    ADD CONSTRAINT fk_transaction_lines_product
    FOREIGN KEY (product_id) REFERENCES products(id);

CREATE TABLE recipes (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    company_id      uuid NOT NULL REFERENCES companies(id),
    product_id      uuid NOT NULL REFERENCES products(id), -- the menu item produced
    version         integer NOT NULL DEFAULT 1,
    serving_yield   numeric(18,3) NOT NULL DEFAULT 1,
    effective_from  timestamptz NOT NULL DEFAULT now(),
    effective_to    timestamptz,
    created_at      timestamptz NOT NULL DEFAULT now(),
    UNIQUE (product_id, version)
);

CREATE INDEX idx_recipes_product_effective ON recipes (product_id, effective_from);

CREATE TABLE recipe_lines (
    id                  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    recipe_id           uuid NOT NULL REFERENCES recipes(id),
    ingredient_product_id uuid NOT NULL REFERENCES products(id),
    quantity_per_serving numeric(18,4) NOT NULL CHECK (quantity_per_serving > 0)
);

-- Weighted-average costing ledger, one row per movement (receiving, sale, waste, transfer, count).
CREATE TABLE stock_movements (
    id                  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    company_id          uuid NOT NULL REFERENCES companies(id),
    branch_id           uuid NOT NULL REFERENCES branches(id),
    product_id          uuid NOT NULL REFERENCES products(id),
    -- Deferred because the posting engine issues stock while deriving the
    -- journal, before the source transaction row is inserted later in the
    -- same database transaction; both land before commit regardless.
    source_transaction_id uuid REFERENCES transactions(id) DEFERRABLE INITIALLY DEFERRED,
    movement_type       varchar(20) NOT NULL
                            CHECK (movement_type IN ('receiving', 'sale', 'waste', 'count_adjustment',
                                                       'transfer_out', 'transfer_in', 'recipe_consumption')),
    quantity_delta      numeric(18,3) NOT NULL, -- signed: + increases stock, - decreases
    unit_cost           numeric(18,4) NOT NULL, -- moving weighted-average cost at time of movement
    running_quantity    numeric(18,3) NOT NULL, -- balance after this movement
    running_value       numeric(18,2) NOT NULL, -- valuation after this movement
    reason              varchar(500),
    recipe_version      integer,
    occurred_at         timestamptz NOT NULL DEFAULT now(),
    created_by          uuid NOT NULL REFERENCES users(id)
);

CREATE INDEX idx_stock_movements_branch_product_time
    ON stock_movements (company_id, branch_id, product_id, occurred_at);

-- ---------------------------------------------------------------------------
-- Transfers (branch-to-branch cash or stock)
-- ---------------------------------------------------------------------------

CREATE TABLE transfers (
    id                  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    company_id          uuid NOT NULL REFERENCES companies(id),
    transfer_type       varchar(10) NOT NULL CHECK (transfer_type IN ('cash', 'stock')),
    sender_branch_id    uuid NOT NULL REFERENCES branches(id),
    receiver_branch_id  uuid NOT NULL REFERENCES branches(id),
    dispatch_transaction_id uuid REFERENCES transactions(id),
    receipt_transaction_id  uuid REFERENCES transactions(id),
    status               varchar(20) NOT NULL DEFAULT 'dispatched'
                            CHECK (status IN ('dispatched', 'partially_received', 'received', 'disputed')),
    created_at           timestamptz NOT NULL DEFAULT now(),
    CHECK (sender_branch_id <> receiver_branch_id)
);

CREATE INDEX idx_transfers_company_status ON transfers (company_id, status);

CREATE TABLE transfer_lines (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    transfer_id     uuid NOT NULL REFERENCES transfers(id),
    product_id      uuid REFERENCES products(id), -- null for cash transfers
    quantity_sent   numeric(18,3),
    quantity_received numeric(18,3) NOT NULL DEFAULT 0,
    amount          numeric(18,2) -- cash transfers
);

-- ---------------------------------------------------------------------------
-- Approvals, daily closes
-- ---------------------------------------------------------------------------

CREATE TABLE approvals (
    id                  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    company_id          uuid NOT NULL REFERENCES companies(id),
    transaction_id      uuid NOT NULL REFERENCES transactions(id),
    requested_by        uuid NOT NULL REFERENCES users(id),
    requested_at        timestamptz NOT NULL DEFAULT now(),
    decided_by          uuid REFERENCES users(id),
    decided_at          timestamptz,
    decision             varchar(20) CHECK (decision IN ('approved', 'rejected', 'escalated')),
    reason               varchar(2000),
    record_version_at_request integer NOT NULL
);

CREATE INDEX idx_approvals_company_pending ON approvals (company_id) WHERE decision IS NULL;

CREATE TABLE daily_closes (
    id                  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    company_id          uuid NOT NULL REFERENCES companies(id),
    branch_id           uuid NOT NULL REFERENCES branches(id),
    close_date          date NOT NULL,
    currency_code       char(3) NOT NULL,
    opening_float       numeric(18,2) NOT NULL,
    expected_cash       numeric(18,2) NOT NULL,
    counted_cash        numeric(18,2) NOT NULL,
    discrepancy         numeric(18,2) GENERATED ALWAYS AS (counted_cash - expected_cash) STORED,
    explanation         varchar(2000),
    status               varchar(20) NOT NULL DEFAULT 'open'
                            CHECK (status IN ('open', 'submitted', 'returned', 'approved')),
    submitted_by         uuid REFERENCES users(id),
    submitted_at          timestamptz,
    approved_by           uuid REFERENCES users(id),
    approved_at            timestamptz,
    revision              integer NOT NULL DEFAULT 1,
    created_at             timestamptz NOT NULL DEFAULT now(),
    UNIQUE (branch_id, close_date, currency_code, revision)
);

CREATE INDEX idx_daily_closes_company_branch_date ON daily_closes (company_id, branch_id, close_date);

-- ---------------------------------------------------------------------------
-- Attachments, audit trail
-- ---------------------------------------------------------------------------

CREATE TABLE attachments (
    id                  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    company_id          uuid NOT NULL REFERENCES companies(id),
    transaction_id      uuid REFERENCES transactions(id),
    object_key          text NOT NULL, -- generated object storage name, never the original filename
    original_filename   varchar(255) NOT NULL,
    content_type        varchar(100) NOT NULL,
    size_bytes          bigint NOT NULL CHECK (size_bytes > 0),
    checksum_sha256     char(64) NOT NULL,
    upload_status        varchar(20) NOT NULL DEFAULT 'pending'
                            CHECK (upload_status IN ('pending', 'stored', 'quarantined', 'rejected')),
    uploaded_by           uuid NOT NULL REFERENCES users(id),
    created_at             timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX idx_attachments_company ON attachments (company_id);

CREATE TABLE audit_events (
    id              bigserial PRIMARY KEY,
    company_id      uuid NOT NULL REFERENCES companies(id),
    actor_user_id   uuid REFERENCES users(id),
    event_type      varchar(60) NOT NULL, -- login, access_denied, role_change, export, reversal, etc.
    record_type     varchar(40),
    record_id       uuid,
    details         jsonb NOT NULL DEFAULT '{}',
    server_time     timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX idx_audit_events_company_time ON audit_events (company_id, server_time);

-- ---------------------------------------------------------------------------
-- Sessions (Laravel owns login; Go tracks delegated-identity revocation state)
-- ---------------------------------------------------------------------------

CREATE TABLE revoked_sessions (
    session_id      text PRIMARY KEY,
    user_id         uuid NOT NULL REFERENCES users(id),
    revoked_at      timestamptz NOT NULL DEFAULT now()
);
