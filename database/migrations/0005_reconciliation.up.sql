-- Cash and bank reconciliation (FR06: "Matches and unresolved differences
-- are reported"; screen catalogue "Reconciliation": "Match statement items
-- and resolve" / "Matched and outstanding entries"). A statement_item is a
-- line the owner/accountant typed in from an actual bank/mobile-money
-- statement or a physical cash count; it is matched against one posted
-- journal_lines row on the same cash-like account. This is a side ledger of
-- matches, not a posting path — it never creates or changes a journal.
CREATE TABLE statement_items (
    id                      uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    company_id              uuid NOT NULL REFERENCES companies(id),
    branch_id               uuid NOT NULL REFERENCES branches(id),
    account_id              uuid NOT NULL REFERENCES accounts(id),
    statement_date          date NOT NULL,
    description             varchar(500) NOT NULL,
    amount                  numeric(18,2) NOT NULL, -- signed: + deposit/inflow, - withdrawal/outflow
    external_reference      varchar(100),
    status                  varchar(10) NOT NULL DEFAULT 'unmatched'
                                CHECK (status IN ('unmatched', 'matched')),
    -- A given posted ledger entry backs at most one statement item: the
    -- UNIQUE constraint is what actually prevents two statement lines from
    -- both claiming the same journal_lines row.
    matched_journal_line_id uuid REFERENCES journal_lines(id),
    created_by              uuid NOT NULL REFERENCES users(id),
    created_at              timestamptz NOT NULL DEFAULT now(),
    UNIQUE (matched_journal_line_id)
);

CREATE INDEX idx_statement_items_company_account_status
    ON statement_items (company_id, account_id, status);
CREATE INDEX idx_statement_items_branch
    ON statement_items (branch_id);
