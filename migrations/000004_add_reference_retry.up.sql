ALTER TABLE transactions
    ADD COLUMN reference_attempts INT NOT NULL DEFAULT 0,
    ADD COLUMN reference_next_attempt_at TIMESTAMPTZ;

CREATE INDEX idx_transactions_pending_reference
    ON transactions (reference_next_attempt_at)
    WHERE status = 'PENDING_REFERENCE';

-- A given original transaction may be reversed (REFUND or ROLLBACK)
-- successfully at most once, regardless of which of the two kinds does it.
CREATE UNIQUE INDEX idx_transactions_single_successful_reversal
    ON transactions (provider_id, reference_id)
    WHERE status = 'PROCESSED' AND kind IN ('REFUND', 'ROLLBACK');
