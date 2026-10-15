ALTER TABLE ledger_entries
    DROP CONSTRAINT ledger_entries_balance_before_non_negative,
    DROP CONSTRAINT ledger_entries_direction_valid,
    DROP COLUMN balance_before_minor_units,
    DROP COLUMN direction;
