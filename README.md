# NexusAPI

Reusable, API-first backend foundation for mobile, web, desktop, kiosk, device, and internal clients.

The project follows [`GOALS.md`](GOALS.md) and the engineering rules in [`IMPLEMENT.md`](IMPLEMENT.md): a modular monolith, PostgreSQL-first persistence, explicit authorization, bounded collection access, structured observability, and incremental adapters for existing systems. The current hardening scope is recorded in [`CODEX_HARDENING.md`](CODEX_HARDENING.md).

## Quick start

Requirements: Go 1.26+ and Docker Compose.

```bash
cp .env.example .env
docker compose up -d postgres redis
set -a; source .env; set +a
go run ./cmd/api
```

For a standalone binary:

```bash
go build -o nexus-api ./cmd/api
./nexus-api
```

Verify the process:

```bash
curl -i http://localhost:8080/healthz
curl -i http://localhost:8080/readyz
curl http://localhost:8080/openapi.yaml
```

The `/api/v1/*` routes are private by default. They require a signed HS256 bearer token with `sub` and `exp` claims. Set `JWT_ISSUER` and `JWT_AUDIENCE` in production when the identity provider contract defines them. NexusAPI does not issue tokens; token issuance belongs to the identity system or an integration adapter.

The Loyal Fitness read adapter is optional and enabled by setting `LOYAL_FITNESS_BASE_URL`. It sends a bounded, role-scoped request to the source service and translates the response into the NexusAPI contract. Redis is used as a cache-aside layer; cache failures fall back to the source read instead of changing the API result.

The adapter expects the source service to expose the same read paths under the configured base URL:

```text
GET /api/v1/members?actor_subject=...&actor_roles=...&actor_scope=self|all&q=...&status=...&limit=...&cursor=...
GET /api/v1/pt-sessions?actor_subject=...&actor_roles=...&actor_scope=self|all&status=...&from=...&to=...&limit=...&cursor=...
GET /api/v1/finance/summary?actor_subject=...&actor_roles=...&actor_scope=self|all
GET /api/v1/mobile/dashboard?actor_subject=...&actor_roles=...&actor_scope=self|all
```

Responses use the NexusAPI JSON envelope. Error responses have the stable shape `{"error":{"code":"...","message":"..."},"request_id":"..."}`. The source service remains responsible for enforcing the actor scope before returning data.

Attendance/kiosk commands are exposed as a strict contract:

```text
POST /api/v1/attendance/check-in
POST /api/v1/attendance/check-out
POST /api/v1/devices/heartbeat
```

They reject unknown JSON fields and oversized bodies. The new attendance domain persists events and member state in NexusAPI PostgreSQL tables after startup migrations; this does not move or mutate any legacy Laravel write path. Attendance resolves `qr_token` through an internal identifier registry before selecting a member. `member_id` is accepted only as an optional compatibility assertion and must match the server-resolved member. Unknown identifiers return `422`, revoked or expired identifiers return `403`, and mismatched assertions return `409`. QR tokens are stored only as SHA-256 hashes. SHA-256 is appropriate only because QR values must be high-entropy opaque tokens.

After a successful attendance transaction, NexusAPI enqueues the event in the same PostgreSQL transaction and a background dispatcher publishes it on Redis topic `nexus.attendance.events`. Delivery is at-least-once: consumers must deduplicate by event ID. PostgreSQL remains authoritative and Redis failures do not lose the event.

## Development commands

```bash
go fmt ./...
go vet ./...
go test ./...
go build ./...
```

When `sqlc` is installed, regenerate typed PostgreSQL query code with:

```bash
make generate
```

The runtime image does not install developer generators; generated query code is committed with the application changes that require it.

The same checks run in GitHub Actions. SQL migrations are embedded in the binary and applied transactionally at startup. PostgreSQL access is intended to be generated through `pgx` and `sqlc`; `sqlc.yaml` defines the generation boundary so domain modules do not reach into driver details.

GitHub Actions also runs PostgreSQL-backed integration tests against PostgreSQL 16. Locally, run them with `DATABASE_URL` set and `go test -tags=integration ./...`.

## Architecture

```text
cmd/api                         process entrypoint
internal/server                 HTTP routes and transport composition
internal/access                 authentication and authorization seams
internal/platform               config, HTTP primitives, PostgreSQL, Redis, migrations
internal/integration            adapters for existing systems
internal/member                 future member read model
internal/commerce               future commerce boundary
internal/attendance             future attendance boundary
internal/reporting              future reporting boundary
openapi/openapi.yaml             versioned public API contract
```

Loyal Fitness endpoints are exposed as stable v1 routes, but their read model is deliberately an adapter seam. Until a source URL is configured, those routes return a deterministic `integration_unavailable` error rather than fabricating data. The source service must implement the adapter request/response contract in `internal/integration/loyalfitness/http_reader.go`.

## Configuration and security

Secrets are environment-only and are never logged. Production startup rejects missing, weak, or documented placeholder JWT secrets and missing database/Redis URLs. CORS origins must be explicit in production. Every request receives a request ID, and error responses never expose stack traces or internal error text.

Set `TRUSTED_PROXY_CIDRS` only to the network ranges of reverse proxies that are actually controlled by the deployment. Forwarding headers are ignored for direct clients. `HTTP_READ_HEADER_TIMEOUT` defaults to 5 seconds and `HTTP_MAX_HEADER_BYTES` defaults to 1 MiB; both are validated at startup.

Startup migrations take a PostgreSQL advisory lock, so concurrent API instances cannot apply the same migration simultaneously. Attendance state changes use serializable transactions with bounded retry for PostgreSQL serialization failures and deadlocks.

## Docker

Run the complete local environment with:

```bash
docker compose up -d --build
```

The production image runs as a non-root user and exposes port 8080. PostgreSQL and Redis are health-checked before the API is started.

For a non-container Linux VPS, install the binary as `/usr/local/bin/nexus-api`, place environment values in `/etc/nexus-api/nexus-api.env`, create the `nexus` service user, enable [`deploy/nexus-api.service`](deploy/nexus-api.service), and configure [`deploy/Caddyfile.example`](deploy/Caddyfile.example) as the reverse proxy.

For a containerized production profile, copy `.env.production.example` to `.env.production`, replace every placeholder, and run from the repository root:

```bash
docker compose --env-file .env.production -f deploy/compose.prod.yml up -d --build
```

The production profile exposes only Caddy on ports 80/443. PostgreSQL and Redis are attached to an internal Docker network and have no host port bindings. Keep `TRUSTED_PROXY_CIDRS` aligned with the configured Caddy network if the Compose subnet is changed.
