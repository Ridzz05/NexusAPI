CREATE TABLE IF NOT EXISTS attendance_identifiers (
    id TEXT PRIMARY KEY,
    member_id TEXT NOT NULL,
    identifier_type TEXT NOT NULL CHECK (identifier_type IN ('qr')),
    token_hash TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('active', 'revoked')),
    expires_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS attendance_identifiers_type_hash_uq
    ON attendance_identifiers (identifier_type, token_hash);

CREATE INDEX IF NOT EXISTS attendance_identifiers_member_status_idx
    ON attendance_identifiers (member_id, status);
