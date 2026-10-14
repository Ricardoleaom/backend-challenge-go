CREATE TABLE ledger_entries (
    id UUID PRIMARY KEY,
    wallet_id UUID NOT NULL REFERENCES wallets(id),
    transaction_id UUID NOT NULL REFERENCES transactions(id),
    amount_delta_minor_units BIGINT NOT NULL,
    balance_after_minor_units BIGINT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT ledger_entries_non_zero_delta
        CHECK (amount_delta_minor_units <> 0),

    CONSTRAINT ledger_entries_balance_non_negative
        CHECK (balance_after_minor_units >= 0),

    CONSTRAINT ledger_entries_transaction_unique
        UNIQUE (transaction_id)
);

CREATE INDEX idx_ledger_entries_wallet_created
    ON ledger_entries (wallet_id, created_at, id);

CREATE FUNCTION prevent_ledger_entry_mutation()
RETURNS TRIGGER AS $$
BEGIN
    RAISE EXCEPTION 'ledger entries are append-only';
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER ledger_entries_immutable
BEFORE UPDATE OR DELETE ON ledger_entries
FOR EACH ROW
EXECUTE FUNCTION prevent_ledger_entry_mutation();