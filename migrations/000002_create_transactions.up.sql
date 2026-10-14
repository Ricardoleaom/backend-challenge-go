CREATE TABLE transactions (
    id UUID PRIMARY KEY,
    provider_id TEXT NOT NULL,
    external_id TEXT NOT NULL,
    idempotency_key TEXT NOT NULL,
    payload_hash TEXT NOT NULL,
    wallet_id UUID NOT NULL REFERENCES wallets(id),
    player_id UUID NOT NULL,
    round_id TEXT NOT NULL,
    game_id TEXT NOT NULL,
    kind TEXT NOT NULL,
    amount_minor_units BIGINT NOT NULL,
    reference_id TEXT,
    status TEXT NOT NULL,
    failure_code TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT transactions_idempotency_unique
        UNIQUE (provider_id, idempotency_key),

    CONSTRAINT transactions_external_id_unique
        UNIQUE (provider_id, external_id),

    CONSTRAINT transactions_kind_valid
        CHECK (kind IN ('BET', 'WIN', 'LOSS', 'REFUND', 'ROLLBACK')),

    CONSTRAINT transactions_status_valid
        CHECK (
            status IN (
                'PENDING',
                'PENDING_REFERENCE',
                'PROCESSED',
                'REJECTED',
                'FAILED'
            )
        ),

    CONSTRAINT transactions_amount_non_negative
        CHECK (amount_minor_units >= 0),

    CONSTRAINT transactions_payload_hash_not_empty
        CHECK (length(payload_hash) > 0)
);

CREATE INDEX idx_transactions_wallet_created
    ON transactions (wallet_id, created_at DESC);

CREATE INDEX idx_transactions_reference
    ON transactions (reference_id)
    WHERE reference_id IS NOT NULL;
