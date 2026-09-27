-- Preserve the draft revision after deletion to prevent a stale revision 0
-- write from recreating a draft. Existing 0001 databases can apply this.
ALTER TABLE user_state DROP CONSTRAINT draft_envelope_consistent;
ALTER TABLE user_state ADD CONSTRAINT draft_envelope_consistent CHECK (
    (draft_ciphertext IS NULL AND draft_algorithm_version IS NULL AND draft_key_version IS NULL AND draft_revision >= 0)
    OR (draft_ciphertext IS NOT NULL AND octet_length(draft_ciphertext) >= 28 AND draft_algorithm_version IS NOT NULL AND draft_algorithm_version > 0 AND draft_key_version IS NOT NULL AND draft_key_version > 0 AND draft_revision > 0)
);

CREATE TABLE idempotency_records (
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    key UUID NOT NULL,
    request_hash BYTEA NOT NULL CHECK (octet_length(request_hash) = 32),
    profile_revision BIGINT NOT NULL CHECK (profile_revision > 0),
    result_ciphertext BYTEA NOT NULL CHECK (octet_length(result_ciphertext) >= 28),
    result_algorithm_version SMALLINT NOT NULL CHECK (result_algorithm_version > 0),
    result_key_version INTEGER NOT NULL CHECK (result_key_version > 0),
    expires_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (user_id, key)
);
CREATE INDEX idempotency_expires_at_idx ON idempotency_records(expires_at);
