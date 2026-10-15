ALTER TABLE transactions
    ADD COLUMN reference_external_id TEXT;

COMMENT ON COLUMN transactions.reference_external_id IS
    'Raw referenceExternalTransactionId as given by the caller, before resolution.';
COMMENT ON COLUMN transactions.reference_id IS
    'Internal ID of the referenced transaction, once resolved.';
