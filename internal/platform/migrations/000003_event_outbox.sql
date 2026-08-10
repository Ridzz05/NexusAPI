CREATE TABLE IF NOT EXISTS nexus_event_outbox (
    id TEXT PRIMARY KEY,
    topic TEXT NOT NULL,
    payload JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    published_at TIMESTAMPTZ,
    attempts INTEGER NOT NULL DEFAULT 0,
    last_error TEXT
);

CREATE INDEX IF NOT EXISTS nexus_event_outbox_pending_idx
    ON nexus_event_outbox (created_at ASC)
    WHERE published_at IS NULL;
