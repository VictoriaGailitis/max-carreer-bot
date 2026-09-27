-- Only minimal, encrypted delivery tasks are retained. No raw MAX update,
-- message body, sender name, or recipient ID is stored in plaintext.
CREATE TABLE bot_inbox (
    id UUID PRIMARY KEY,
    event_key BYTEA NOT NULL UNIQUE CHECK (octet_length(event_key) = 32),
    recipient_lookup BYTEA NOT NULL CHECK (octet_length(recipient_lookup) = 32),
    payload_ciphertext BYTEA,
    payload_algorithm_version SMALLINT,
    payload_key_version INTEGER,
    status TEXT NOT NULL CHECK (status IN ('pending', 'processing', 'sent', 'failed')),
    attempts INTEGER NOT NULL DEFAULT 0 CHECK (attempts BETWEEN 0 AND 5),
    next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    locked_until TIMESTAMPTZ,
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    finished_at TIMESTAMPTZ,
    CONSTRAINT bot_inbox_payload CHECK (
        (status IN ('pending', 'processing') AND payload_ciphertext IS NOT NULL AND octet_length(payload_ciphertext) >= 28 AND payload_algorithm_version > 0 AND payload_key_version > 0)
        OR (status IN ('sent', 'failed') AND payload_ciphertext IS NULL AND payload_algorithm_version IS NULL AND payload_key_version IS NULL)
    )
);
CREATE INDEX bot_inbox_due_idx ON bot_inbox(next_attempt_at, created_at) WHERE status IN ('pending', 'processing');
CREATE INDEX bot_inbox_expiry_idx ON bot_inbox(expires_at);
