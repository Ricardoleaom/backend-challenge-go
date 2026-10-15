CREATE TABLE inbox_messages (
    id UUID PRIMARY KEY,
    consumer_name TEXT NOT NULL,
    message_id TEXT NOT NULL,
    payload_hash TEXT NOT NULL,
    received_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    completed_at TIMESTAMPTZ,

    CONSTRAINT inbox_messages_unique
        UNIQUE (consumer_name, message_id)
);
