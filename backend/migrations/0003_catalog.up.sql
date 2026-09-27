-- Immutable catalog snapshots. Only the active version is read by the API.
CREATE TABLE catalog_versions (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    source_sha256 BYTEA NOT NULL UNIQUE CHECK (octet_length(source_sha256) = 32),
    is_demo BOOLEAN NOT NULL,
    imported_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE catalog_active (
    is_demo BOOLEAN PRIMARY KEY,
    version_id BIGINT NOT NULL REFERENCES catalog_versions(id)
);

CREATE TABLE opportunities (
    catalog_version_id BIGINT NOT NULL REFERENCES catalog_versions(id) ON DELETE CASCADE,
    id TEXT NOT NULL,
    payload JSONB NOT NULL,
    publication_status TEXT NOT NULL CHECK (publication_status IN ('draft','published','archived')),
    availability TEXT NOT NULL CHECK (availability IN ('open','closed','unknown')),
    is_demo BOOLEAN NOT NULL,
    PRIMARY KEY (catalog_version_id, id)
);
CREATE INDEX opportunities_version_status_idx ON opportunities(catalog_version_id, publication_status, availability);
