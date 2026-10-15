DROP INDEX idx_transactions_single_opening;
DROP INDEX idx_transactions_external_id_unique;
DROP INDEX idx_transactions_idempotency_unique;

ALTER TABLE transactions
    DROP CONSTRAINT transactions_external_fields_required,
    DROP CONSTRAINT transactions_kind_valid;

ALTER TABLE transactions
    ADD CONSTRAINT transactions_kind_valid
        CHECK (kind IN ('BET', 'WIN', 'LOSS', 'REFUND', 'ROLLBACK')),
    ADD CONSTRAINT transactions_idempotency_unique
        UNIQUE (provider_id, idempotency_key),
    ADD CONSTRAINT transactions_external_id_unique
        UNIQUE (provider_id, external_id);

ALTER TABLE transactions
    ALTER COLUMN provider_id SET NOT NULL,
    ALTER COLUMN external_id SET NOT NULL,
    ALTER COLUMN idempotency_key SET NOT NULL,
    ALTER COLUMN payload_hash SET NOT NULL,
    ALTER COLUMN round_id SET NOT NULL,
    ALTER COLUMN game_id SET NOT NULL;
