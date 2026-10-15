CREATE UNIQUE INDEX idx_wallets_player_currency_unique
    ON wallets (player_id, currency);
