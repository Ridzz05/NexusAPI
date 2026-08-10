-- name: InsertEventOutbox :exec
INSERT INTO nexus_event_outbox (id, topic, payload, created_at)
VALUES (sqlc.arg('id'), sqlc.arg('topic'), sqlc.arg('payload')::jsonb, sqlc.arg('created_at'));

-- name: ListPendingEventOutbox :many
SELECT id, topic, payload::text, created_at
FROM nexus_event_outbox
WHERE published_at IS NULL
ORDER BY created_at ASC
FOR UPDATE SKIP LOCKED
LIMIT $1;

-- name: MarkEventOutboxFailure :exec
UPDATE nexus_event_outbox
SET attempts = attempts + 1,
    last_error = LEFT(sqlc.arg('last_error'), 500)
WHERE id = sqlc.arg('id');

-- name: MarkEventOutboxPublished :exec
UPDATE nexus_event_outbox
SET published_at = NOW(),
    attempts = attempts + 1,
    last_error = NULL
WHERE id = sqlc.arg('id');
