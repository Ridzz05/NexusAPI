-- This directory is the sqlc boundary for domain queries.
-- Domain modules should add explicit, bounded queries here rather than using SELECT *.

-- name: ListAppliedMigrations :many
SELECT version, applied_at
FROM nexus_schema_migrations
ORDER BY applied_at DESC
LIMIT $1;
