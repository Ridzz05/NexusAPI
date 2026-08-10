CREATE TABLE IF NOT EXISTS attendance_member_state (
    member_id TEXT PRIMARY KEY,
    state TEXT NOT NULL CHECK (state IN ('checked_in', 'checked_out')),
    updated_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE IF NOT EXISTS attendance_device_heartbeats (
    device_id TEXT PRIMARY KEY,
    actor_subject TEXT NOT NULL,
    firmware_version TEXT NOT NULL DEFAULT '',
    observed_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE IF NOT EXISTS attendance_events (
    id TEXT PRIMARY KEY,
    kind TEXT NOT NULL CHECK (kind IN ('check_in', 'check_out', 'heartbeat')),
    member_id TEXT,
    device_id TEXT NOT NULL,
    actor_subject TEXT NOT NULL,
    qr_token_hash TEXT,
    occurred_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS attendance_events_member_occurred_idx
    ON attendance_events (member_id, occurred_at DESC);

CREATE INDEX IF NOT EXISTS attendance_events_device_occurred_idx
    ON attendance_events (device_id, occurred_at DESC);
