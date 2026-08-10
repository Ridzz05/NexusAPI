-- name: GetAttendanceMemberState :one
SELECT state
FROM attendance_member_state
WHERE member_id = $1
FOR UPDATE;

-- name: UpsertAttendanceMemberState :exec
INSERT INTO attendance_member_state (member_id, state, updated_at)
VALUES ($1, $2, $3)
ON CONFLICT (member_id) DO UPDATE SET
    state = EXCLUDED.state,
    updated_at = EXCLUDED.updated_at;

-- name: UpsertAttendanceDeviceHeartbeat :exec
INSERT INTO attendance_device_heartbeats (device_id, actor_subject, firmware_version, observed_at, created_at)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (device_id) DO UPDATE SET
    actor_subject = EXCLUDED.actor_subject,
    firmware_version = EXCLUDED.firmware_version,
    observed_at = EXCLUDED.observed_at,
    created_at = EXCLUDED.created_at;

-- name: InsertAttendanceEvent :exec
INSERT INTO attendance_events (id, kind, member_id, device_id, actor_subject, qr_token_hash, occurred_at, created_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8);

-- name: GetAttendanceIdentifierByHash :one
SELECT id, member_id, identifier_type, token_hash, status, expires_at, created_at, updated_at
FROM attendance_identifiers
WHERE identifier_type = 'qr' AND token_hash = $1;

-- name: InsertAttendanceIdentifier :exec
INSERT INTO attendance_identifiers (
    id, member_id, identifier_type, token_hash, status, expires_at, created_at, updated_at
)
VALUES ($1, $2, 'qr', $3, 'active', $4, $5, $6);

-- name: RevokeAttendanceIdentifier :execrows
UPDATE attendance_identifiers
SET status = 'revoked', updated_at = $2
WHERE id = $1;
