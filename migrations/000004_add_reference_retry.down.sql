DROP INDEX idx_transactions_single_successful_reversal;
DROP INDEX idx_transactions_pending_reference;

ALTER TABLE transactions
    DROP COLUMN reference_attempts,
    DROP COLUMN reference_next_attempt_at;
