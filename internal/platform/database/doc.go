// Package database is the sqlc generation boundary for explicit PostgreSQL
// queries. Generated code belongs under database/generated and is consumed by
// domain repositories rather than leaking driver details into HTTP handlers.
package database

//go:generate sqlc generate
