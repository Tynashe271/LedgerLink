DROP TABLE IF EXISTS exports;

ALTER TABLE approvals DROP COLUMN IF EXISTS decided_amount;

ALTER TABLE transactions DROP CONSTRAINT IF EXISTS fk_transactions_reversed_by;
ALTER TABLE transactions DROP CONSTRAINT IF EXISTS fk_transactions_reverses;
ALTER TABLE transfer_lines DROP COLUMN IF EXISTS amount_received;
ALTER TABLE transfers DROP COLUMN IF EXISTS exchange_rate;
ALTER TABLE transfers DROP COLUMN IF EXISTS currency_code;

ALTER TABLE stock_movements DROP CONSTRAINT stock_movements_movement_type_check;
ALTER TABLE stock_movements ADD CONSTRAINT stock_movements_movement_type_check
    CHECK (movement_type IN ('receiving', 'sale', 'waste', 'count_adjustment',
                              'transfer_out', 'transfer_in', 'recipe_consumption'));
