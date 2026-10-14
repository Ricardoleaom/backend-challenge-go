CREATE TABLE wallets (
    id UUID PRIMARY KEY,
    player_id UUID NOT NULL,
    currency CHAR(3) NOT NULL,
    balance_minor_units BIGINT NOT NULL DEFAULT 0,
    version BIGINT NOT NULL DEFAULT 1,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT wallets_balance_non_negative
        CHECK (balance_minor_units >= 0),

    CONSTRAINT wallets_version_positive
        CHECK (version >= 1),

    CONSTRAINT wallets_currency_format
        CHECK (currency ~ '^[A-Z]{3}$')
);

CREATE INDEX idx_wallets_player_id
    ON wallets (player_id);
