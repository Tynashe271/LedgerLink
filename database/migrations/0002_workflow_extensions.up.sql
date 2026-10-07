-- Extends the initial schema to support the previously-stubbed workflow
-- endpoints: transaction reversal, approvals-gated posting, and exports.
-- Expand-and-contract: adds columns/tables and widens one CHECK constraint;
-- drops nothing that existing rows could depend on.

-- "reversal" lets Reverse() restore (or remove) exactly the stock quantity
-- and value an original posting moved, as its own distinct movement kind
-- rather than overloading "receiving" or "sale".
ALTER TABLE stock_movements DROP CONSTRAINT stock_movements_movement_type_check;
ALTER TABLE stock_movements ADD CONSTRAINT stock_movements_movement_type_check
    CHECK (movement_type IN ('receiving', 'sale', 'waste', 'count_adjustment',
                              'transfer_out', 'transfer_in', 'recipe_consumption',
                              'reversal'));

-- Cash transfers (the only transfer_type this release's ReceiveTransfer
-- implements) need a currency/rate to post the receipt journal in, and a
-- running received total distinct from quantity_received (which is stock
-- transfers' column). Reverse() similarly needs to link a reversal
-- transaction back to the one it reverses, matching reversed_by_transaction_id
-- on the original side.
ALTER TABLE transfers ADD COLUMN currency_code char(3);
ALTER TABLE transfers ADD COLUMN exchange_rate numeric(18,6) NOT NULL DEFAULT 1;
ALTER TABLE transfer_lines ADD COLUMN amount_received numeric(18,2) NOT NULL DEFAULT 0;

-- transactions.reverses_transaction_id / reversed_by_transaction_id already
-- exist from 0001 as plain uuid columns without an FK; add the FK now that
-- Reverse() actually writes them.
ALTER TABLE transactions ADD CONSTRAINT fk_transactions_reverses
    FOREIGN KEY (reverses_transaction_id) REFERENCES transactions(id);
ALTER TABLE transactions ADD CONSTRAINT fk_transactions_reversed_by
    FOREIGN KEY (reversed_by_transaction_id) REFERENCES transactions(id);

-- An expense above the actor's membership.approval_limit is saved without
-- posting (section 5.6 / 5.4 "Above-limit requests enter Awaiting approval").
-- approvals already references transactions; record_version_at_request guards
-- against approving a stale request (AT05), and decided_by <> requested_by is
-- enforced in application code (self-approval is denied for every role).
ALTER TABLE approvals ADD COLUMN decided_amount numeric(18,2);

-- Background export jobs (architecture: "Exports run in background, carry
-- scope and watermark, and are available only to authorised users through
-- expiring links"). Distinct from outbox_jobs, which is the generic
-- fire-and-forget work queue the worker drains; an export additionally needs
-- a user-visible status, a requester, and an expiring download reference.
CREATE TABLE exports (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    company_id      uuid NOT NULL REFERENCES companies(id),
    requested_by    uuid NOT NULL REFERENCES users(id),
    export_type     varchar(40) NOT NULL, -- e.g. transactions_csv, journal_csv
    scope           jsonb NOT NULL DEFAULT '{}', -- branch_id/from/to/currency as requested
    status          varchar(20) NOT NULL DEFAULT 'pending'
                        CHECK (status IN ('pending', 'processing', 'ready', 'failed', 'expired')),
    object_key      text,       -- generated storage name once ready; never a client-chosen name
    row_count       integer,
    error_code      varchar(60),
    requested_at    timestamptz NOT NULL DEFAULT now(),
    completed_at    timestamptz,
    expires_at      timestamptz
);

CREATE INDEX idx_exports_company ON exports (company_id, requested_at);
