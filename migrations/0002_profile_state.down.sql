-- Test environments only: erases tombstone revisions before restoring 0001.
DROP TABLE idempotency_records;
UPDATE user_state SET draft_revision = 0 WHERE draft_ciphertext IS NULL;
ALTER TABLE user_state DROP CONSTRAINT draft_envelope_consistent;
ALTER TABLE user_state ADD CONSTRAINT draft_envelope_consistent CHECK (
    (draft_ciphertext IS NULL AND draft_algorithm_version IS NULL AND draft_key_version IS NULL AND draft_revision = 0)
    OR (draft_ciphertext IS NOT NULL AND octet_length(draft_ciphertext) >= 28 AND draft_algorithm_version IS NOT NULL AND draft_algorithm_version > 0 AND draft_key_version IS NOT NULL AND draft_key_version > 0 AND draft_revision > 0)
);
