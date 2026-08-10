# NexusAPI implementation status

This document records the current capability state against [`GOALS.md`](GOALS.md), [`IMPLEMENT.md`](IMPLEMENT.md), and the hardening brief. It distinguishes local evidence from checks that require external services; commit hashes and unpublished-work claims are intentionally omitted so the document remains valid after each release.

## Foundation

| Goal | Current evidence | Status |
| --- | --- | --- |
| HTTP server, routing, config | `cmd/api`, `internal/server`, `internal/config` | Implemented |
| Structured responses and errors | `internal/platform/httpx`, server contract tests | Implemented |
| Authentication and authorization | HS256 JWT validation, optional issuer/audience, role middleware | Implemented and unit-tested |
| PostgreSQL and Redis | pgx pool, Redis client, startup readiness | Implemented |
| Migrations and explicit SQL | embedded migrations, sqlc config and generated code | Implemented |
| Pagination and bounded filters | cursor pagination plus bounded member/PT filters | Implemented and unit-tested |
| Observability and resilience | request IDs, structured logs, timeouts, panic recovery, rate limiting | Implemented and unit-tested |
| Security defaults | secret validation including production placeholder rejection, trusted-proxy IP resolution, security headers, CORS, no raw QR storage | Implemented and unit-tested |
| API contract | versioned OpenAPI source served from the binary | Implemented and YAML-validated |
| Deployment | Docker, development Compose, and production Caddy/API profile with internal PostgreSQL/Redis | Implemented; live image/runtime requires Docker/VPS |
| CI/CD foundation | formatting, sqlc drift, Go checks, Docker build, PostgreSQL integration job | Configured |

## Loyal Fitness integration

The read adapter is intentionally source-agnostic. `LOYAL_FITNESS_BASE_URL` enables the HTTP adapter, Redis cache-aside, bounded filters, and actor scope propagation. The source service must implement the documented adapter contract; no source schema or credentials are present in this repository.

Attendance is a NexusAPI-owned domain boundary. Check-in, check-out, heartbeat, server-side QR identifier resolution, authorization, PostgreSQL state transitions, QR hashing, and transactional outbox delivery are implemented without migrating legacy Laravel write paths. QR registration and revocation remain internal capabilities; no public registry endpoint is exposed.

## Verification

The dependency-free verification suite is:

```bash
test -z "$(gofmt -l .)"
go test -race ./...
go vet -tags=integration ./...
go build ./...
```

The integration test package compiles locally and requires `DATABASE_URL` to execute. GitHub Actions provisions PostgreSQL 16 and runs `go test -tags=integration ./...` against it.
