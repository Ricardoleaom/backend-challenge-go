ALTER TABLE ledger_entries
    ADD COLUMN direction TEXT NOT NULL DEFAULT 'DEBIT',
    ADD COLUMN balance_before_minor_units BIGINT NOT NULL DEFAULT 0;

ALTER TABLE ledger_entries
    ALTER COLUMN direction DROP DEFAULT,
    ALTER COLUMN balance_before_minor_units DROP DEFAULT;

ALTER TABLE ledger_entries
    ADD CONSTRAINT ledger_entries_direction_valid
        CHECK (direction IN ('DEBIT', 'CREDIT')),
    ADD CONSTRAINT ledger_entries_balance_before_non_negative
        CHECK (balance_before_minor_units >= 0);
