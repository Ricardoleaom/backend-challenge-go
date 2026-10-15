ALTER TABLE transactions
    ALTER COLUMN provider_id DROP NOT NULL,
    ALTER COLUMN external_id DROP NOT NULL,
    ALTER COLUMN idempotency_key DROP NOT NULL,
    ALTER COLUMN payload_hash DROP NOT NULL,
    ALTER COLUMN round_id DROP NOT NULL,
    ALTER COLUMN game_id DROP NOT NULL;

ALTER TABLE transactions
    DROP CONSTRAINT transactions_idempotency_unique,
    DROP CONSTRAINT transactions_external_id_unique,
    DROP CONSTRAINT transactions_kind_valid;

ALTER TABLE transactions
    ADD CONSTRAINT transactions_kind_valid
        CHECK (kind IN ('OPENING', 'BET', 'WIN', 'LOSS', 'REFUND', 'ROLLBACK')),
    ADD CONSTRAINT transactions_external_fields_required
        CHECK (
            kind = 'OPENING'
            OR (
                provider_id IS NOT NULL
                AND external_id IS NOT NULL
                AND idempotency_key IS NOT NULL
                AND payload_hash IS NOT NULL
                AND round_id IS NOT NULL
                AND game_id IS NOT NULL
            )
        );

CREATE UNIQUE INDEX idx_transactions_idempotency_unique
    ON transactions (provider_id, idempotency_key)
    WHERE kind != 'OPENING';

CREATE UNIQUE INDEX idx_transactions_external_id_unique
    ON transactions (provider_id, external_id)
    WHERE kind != 'OPENING';

-- One OPENING per wallet, preventing duplicate initial credit.
CREATE UNIQUE INDEX idx_transactions_single_opening
    ON transactions (wallet_id)
    WHERE kind = 'OPENING';
