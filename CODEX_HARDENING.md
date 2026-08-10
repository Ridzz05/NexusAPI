# CODEX_HARDENING.md

# NexusAPI — Foundation Hardening Execution Plan

## Purpose

Dokumen ini adalah execution brief untuk Codex Agent.

Tujuan fase ini adalah **menguatkan NexusAPI foundation sebelum menambahkan feature/domain baru**.

Jangan memperluas scope ke:

* Commerce migration;
* payment;
* member mutation;
* microservices;
* Kubernetes;
* GraphQL;
* WebSocket feature baru;
* AI integration;
* frontend.

Fokus hanya pada production hardening dan correctness.

---

# Current State

NexusAPI saat ini sudah memiliki:

* Go HTTP API;
* PostgreSQL;
* pgx;
* sqlc;
* Redis;
* JWT authentication;
* authorization foundation;
* OpenAPI;
* request ID;
* structured logging;
* rate limiting;
* panic recovery;
* graceful shutdown;
* Docker;
* Docker Compose;
* GitHub Actions;
* Loyal Fitness read adapter;
* Redis cache-aside;
* attendance domain;
* transactional outbox;
* PostgreSQL integration tests.

Current architecture **harus dipertahankan sebagai modular monolith**.

Jangan melakukan rewrite besar.

---

# Execution Priority

Kerjakan secara berurutan:

```text
P0-1  Trusted Proxy + Real Client IP
P0-2  Attendance QR Verification
P1-1  Production Docker Profile
P1-2  HTTP Server Hardening
P1-3  Middleware Cleanup
P1-4  Documentation Alignment
P1-5  OpenAPI Contract Review
P1-6  Regression & Integration Tests
P1-7  Final Production Audit
```

Setiap tahap harus selesai dan lolos test sebelum lanjut ke tahap berikutnya.

---

# P0-1 — Trusted Proxy and Real Client IP

## Problem

Rate limiter saat ini menggunakan:

```go
r.RemoteAddr
```

sebagai client identity.

Ketika NexusAPI berada di belakang reverse proxy seperti:

```text
Internet
   ↓
Caddy / Nginx
   ↓
NexusAPI
```

`RemoteAddr` dapat menunjuk ke IP reverse proxy.

Akibatnya banyak user dapat dianggap berasal dari satu IP yang sama dan berbagi rate-limit bucket.

Jangan menyelesaikan masalah ini dengan langsung mempercayai:

```text
X-Forwarded-For
X-Real-IP
Forwarded
```

karena header tersebut dapat dipalsukan oleh client.

---

## Goal

Implementasikan trusted proxy aware client IP resolution.

Tambahkan environment config:

```env
TRUSTED_PROXY_CIDRS=
```

Contoh:

```env
TRUSTED_PROXY_CIDRS=127.0.0.1/32,172.16.0.0/12
```

Production harus dapat mengatur CIDR reverse proxy yang dipercaya.

---

## Required Behavior

Algorithm:

```text
request
  ↓
get immediate peer from RemoteAddr
  ↓
is peer inside TRUSTED_PROXY_CIDRS?
  │
  ├── NO
  │    └── use RemoteAddr IP
  │
  └── YES
       └── inspect forwarding headers
              ↓
          determine original client
```

Prioritize standardized behavior where practical.

Do not trust proxy headers if immediate peer is not trusted.

---

## Suggested Package

Create reusable platform code:

```text
internal/platform/network/
```

Possible files:

```text
client_ip.go
client_ip_test.go
```

Expose something equivalent to:

```go
func ClientIP(r *http.Request, trusted []netip.Prefix) netip.Addr
```

Avoid coupling this logic specifically to rate limiting.

Future consumers may include:

* audit logs;
* security logs;
* abuse detection;
* authentication;
* request tracing.

---

## Rate Limiter Update

Rate limiter must consume resolved real client IP.

Do not duplicate proxy parsing inside rate-limiter code.

Expected flow:

```text
Request
   ↓
Client IP resolver
   ↓
Rate limiter
```

---

## Tests

Add tests for:

### Direct request

```text
RemoteAddr = 203.0.113.10
No trusted proxy

Expected:
203.0.113.10
```

### Trusted proxy

```text
RemoteAddr = 127.0.0.1
X-Forwarded-For = 203.0.113.20

Trusted:
127.0.0.1/32

Expected:
203.0.113.20
```

### Untrusted proxy spoof

```text
RemoteAddr = 203.0.113.50
X-Forwarded-For = 1.2.3.4

Trusted:
127.0.0.1/32

Expected:
203.0.113.50
```

### Multiple proxy chain

Handle safely and document the selected algorithm.

---

# P0-2 — Real Attendance QR Verification

## Problem

Current attendance flow accepts:

```json
{
  "member_id": "...",
  "qr_token": "...",
  "occurred_at": "..."
}
```

QR token is hashed and stored for audit, but it does not currently resolve or verify the member identity.

This allows client-provided `member_id` to become too authoritative.

For production attendance:

> Client must not be trusted to decide which member owns a QR token.

---

# Goal

Introduce an Attendance Identifier Registry.

QR token must resolve server-side into a member identity.

Target:

```text
QR token
   ↓
SHA-256
   ↓
Attendance Identifier Registry
   ↓
member_id
   ↓
attendance state transition
```

---

# Database Design

Create new migration.

Suggested table:

```sql
CREATE TABLE attendance_identifiers (
    id TEXT PRIMARY KEY,
    member_id TEXT NOT NULL,
    identifier_type TEXT NOT NULL,
    token_hash TEXT NOT NULL,
    status TEXT NOT NULL,
    expires_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL
);
```

Recommended constraints:

```sql
CHECK (
    identifier_type IN ('qr')
);

CHECK (
    status IN ('active', 'revoked')
);
```

Add unique constraint/index:

```sql
UNIQUE (identifier_type, token_hash)
```

Add lookup index:

```sql
(member_id, status)
```

Do NOT store raw QR token.

Store only:

```text
SHA-256 hash
```

or stronger HMAC strategy if required by future threat model.

For current implementation, SHA-256 is acceptable if tokens contain sufficient cryptographic entropy.

Document this assumption.

---

# QR Resolution

Implement:

```text
internal/attendance/
```

with an explicit identifier boundary.

Possible interface:

```go
type IdentifierResolver interface {
    ResolveQR(
        context.Context,
        string,
    ) (MemberIdentifier, error)
}
```

or repository abstraction appropriate to current architecture.

Do not put SQL inside the HTTP handler.

---

# Check-in Flow

Target flow:

```text
HTTP Request
   ↓
JWT authentication
   ↓
validate payload
   ↓
hash QR
   ↓
resolve identifier
   ↓
verify active / expiry
   ↓
derive member_id
   ↓
authorization
   ↓
Serializable DB transaction
   ↓
state update
   ↓
attendance event
   ↓
outbox event
   ↓
commit
```

---

# API Contract Change

Preferred request:

```json
{
  "qr_token": "opaque-token",
  "occurred_at": "2026-08-10T09:00:00Z"
}
```

For kiosk/staff flows, member identity should be derived from QR.

Avoid accepting `member_id` when QR is the identification mechanism.

If backward compatibility must temporarily remain:

```text
member_id
```

may only act as an assertion.

Server must verify:

```text
member_id == QR-resolved member
```

Otherwise return:

```text
409
```

or:

```text
422
```

with a stable error code.

Preferred long-term design:

**remove client-selected member ID from QR attendance command.**

---

# Required Errors

Introduce deterministic domain errors:

```text
ErrIdentifierNotFound
ErrIdentifierRevoked
ErrIdentifierExpired
ErrIdentifierMismatch
```

HTTP mapping:

```text
invalid QR        → 404 or 422
revoked QR        → 403
expired QR        → 403
member mismatch   → 409
```

Pick one stable contract and document it in OpenAPI.

Do not leak whether an arbitrary sensitive token exists beyond what is appropriate for the authenticated attendance device.

---

# Identifier Provisioning

Do not build full user-management UI.

Create domain/service foundation only.

Minimum internal functionality should allow:

```text
register identifier
revoke identifier
resolve identifier
```

Provisioning may initially be accessible via:

* integration tests;
* internal package;
* future admin integration.

Do not expose unnecessary public endpoints yet.

---

# Concurrency

Attendance state mutation must continue using:

```text
SERIALIZABLE
```

and bounded transaction retry.

Do not weaken current transaction guarantees.

---

# P1-1 — Production Docker Profile

## Problem

Development Compose currently publishes:

```text
5432
6379
```

to the host.

Useful locally.

Not desirable as a default production topology.

---

# Goal

Separate local development and production Compose behavior.

Preferred structure:

```text
compose.yaml
compose.dev.yaml
compose.prod.yaml
```

or:

```text
docker-compose.yml
docker-compose.override.yml
docker-compose.prod.yml
```

Choose the cleaner option for the repository.

---

# Development

Local development may expose:

```text
PostgreSQL :5432
Redis      :6379
API        :8080
```

for debugging.

---

# Production

Production must expose only what is necessary.

Target topology:

```text
Internet
   ↓
Caddy
   ↓
NexusAPI
   ↓
internal Docker network
   ├── PostgreSQL
   └── Redis
```

PostgreSQL and Redis must NOT expose public host ports.

Prefer:

```yaml
expose:
  - "5432"
```

rather than:

```yaml
ports:
  - "5432:5432"
```

for production.

---

# Add Production Example

Provide:

```text
deploy/compose.prod.yml
```

or equivalent.

Do not store real credentials.

Use environment variables.

---

# P1-2 — HTTP Server Hardening

Add:

```go
ReadHeaderTimeout
```

to `http.Server`.

Create environment config:

```env
HTTP_READ_HEADER_TIMEOUT=5s
```

Suggested defaults:

```text
ReadHeaderTimeout : 5s
ReadTimeout       : 10s
WriteTimeout      : 15s
IdleTimeout       : 60s
RequestTimeout    : 10s
ShutdownTimeout   : 10s
```

Ensure configuration validation rejects invalid negative/zero values where appropriate.

Add tests.

---

# Maximum Header Size

Review whether:

```go
MaxHeaderBytes
```

should be explicitly configured.

If added:

```env
HTTP_MAX_HEADER_BYTES=1048576
```

or a smaller justified value.

Do not introduce arbitrary limits without documenting them.

---

# P1-3 — Middleware Cleanup

## Problem

Current handler chain contains redundant panic-recovery boundaries.

Simplify middleware composition.

Target conceptual order:

```text
Request ID
   ↓
Security Headers
   ↓
Recovery
   ↓
Logging
   ↓
CORS
   ↓
Rate Limit
   ↓
Request Timeout
   ↓
Router
```

There should be one clear global panic recovery boundary unless a second recovery layer has a documented and tested reason.

---

# Middleware Requirements

Ensure:

* request ID exists before logs/errors;
* panic is logged with request ID;
* stack is only written to logs;
* response never exposes stack;
* timeout produces deterministic response;
* logs still capture timeout status;
* security headers exist on errors;
* CORS preflight remains correct;
* rate limiting includes deterministic `Retry-After`.

Add regression tests around chain ordering.

---

# P1-4 — Documentation Alignment

Current project documentation must be normalized.

Use:

```text
GOALS.md
IMPLEMENT.md
IMPLEMENTATION_STATUS.md
README.md
```

Responsibilities:

## `GOALS.md`

North-star architecture and product goals.

Do not use as status log.

---

## `IMPLEMENT.md`

Engineering rules, architecture decisions, implementation principles, roadmap, Definition of Done.

---

## `IMPLEMENTATION_STATUS.md`

Current implementation progress.

Must reflect the actual main branch.

Avoid text claiming unpublished local work.

Repository documentation should describe committed state.

---

## `README.md`

User/developer entrypoint:

```text
what NexusAPI is
quick start
architecture summary
configuration
Docker
development
deployment
API docs
```

---

# Documentation Naming

Rename:

```text
GOALS.md
```

to:

```text
GOALS.md
```

if it currently contains the project goals.

Preserve Git history where possible.

Ensure all references are updated.

---

# Status File

Update `IMPLEMENTATION_STATUS.md`.

Do not hard-code statements such as:

```text
last pushed commit is ...
```

unless there is a strong reason.

Prefer status based on capabilities rather than commit SHA.

Example:

```text
Foundation             DONE
JWT Auth               DONE
PostgreSQL             DONE
Redis                  DONE
Attendance QR Verify   DONE
Trusted Proxy          DONE
Production Compose     DONE
Loyal Fitness Writes   NOT STARTED
```

---

# P1-5 — OpenAPI Contract Review

Current OpenAPI version should be reviewed.

If no compatibility blocker exists, migrate:

```yaml
openapi: 3.0.3
```

to:

```yaml
openapi: 3.1.0
```

Do this only if schema validation and tooling remain green.

Do not perform a cosmetic version bump that breaks CI/tooling.

---

# Attendance Contract

Update schemas to reflect real QR verification.

Preferred:

```yaml
CheckCommand:
  type: object
  additionalProperties: false
  required:
    - qr_token
    - occurred_at
```

Do not require `member_id` for QR-based identification.

Document relevant attendance errors.

---

# Error Contract

Ensure all endpoints expose consistent envelopes.

Recommended:

```json
{
  "error": {
    "code": "identifier_not_found",
    "message": "attendance identifier is invalid"
  },
  "request_id": "..."
}
```

No database-specific error strings.

---

# Response Contract

Keep:

```text
request_id
```

available on success and failure.

Collection responses must preserve:

```text
data
meta.next_cursor
meta.has_more
request_id
```

---

# P1-6 — Regression and Integration Tests

After changes, minimum test matrix:

## Trusted Proxy

* direct request;
* trusted reverse proxy;
* untrusted spoofed forwarding header;
* multiple proxy chain;
* IPv4;
* IPv6.

---

## Rate Limiting

* same real IP shares bucket;
* different client IP receives separate bucket;
* trusted proxy does not collapse users;
* spoofed header does not bypass limit.

---

## Attendance Identifier

* valid QR;
* unknown QR;
* revoked QR;
* expired QR;
* duplicate token prevention;
* QR resolves correct member;
* mismatch rejected if compatibility member_id remains;
* raw QR never stored.

---

## Attendance State

* valid check-in;
* double check-in conflict;
* checkout before check-in conflict;
* valid checkout;
* manager/staff authorization;
* member cannot mutate another member;
* serialization retry remains working.

---

## Transactional Outbox

Verify:

```text
attendance state
attendance event
outbox event
```

are committed atomically.

If transaction fails:

```text
none
```

should persist.

---

## HTTP

Test:

* unknown route;
* wrong method;
* malformed JSON;
* oversized body;
* panic recovery;
* request timeout;
* CORS;
* security headers;
* readiness failure;
* authentication failure.

---

# Integration Tests

Keep:

```bash
go test -tags=integration ./...
```

against PostgreSQL.

Do not make unit tests depend on Docker.

---

# P1-7 — Final Production Audit

After implementation, run:

```bash
gofmt -w .
go vet ./...
go test ./...
go test -race ./...
go build ./...
```

Then run:

```bash
go test -tags=integration ./...
```

with PostgreSQL.

Run sqlc generation and verify no drift.

---

# Docker Validation

Build:

```bash
docker build -t nexus-api:hardening .
```

Then verify production Compose config.

Do not claim runtime verification if Docker is not available in the environment.

Report such limitation explicitly.

---

# Security Audit Checklist

Verify:

* [ ] no secrets committed;
* [ ] JWT secret validation remains strict;
* [ ] forwarded headers trusted only from configured proxies;
* [ ] raw QR token never persisted;
* [ ] PostgreSQL not publicly exposed in production compose;
* [ ] Redis not publicly exposed in production compose;
* [ ] unknown JSON fields rejected on sensitive commands;
* [ ] request body size limited;
* [ ] authorization enforced in domain/service layer where required;
* [ ] errors do not expose internals;
* [ ] request IDs appear in logs and responses;
* [ ] sensitive values are not logged.

---

# Architecture Rules

Do NOT:

* move attendance code into server handlers;
* couple QR lookup directly to HTTP;
* add ORM;
* add web framework unless truly necessary;
* add microservices;
* add message broker;
* add Kafka;
* introduce global mutable state;
* trust forwarded headers globally;
* remove transaction safety;
* expose Redis/PostgreSQL publicly;
* create generic abstractions without real callers.

---

# Keep Dependencies Minimal

Current dependency philosophy should remain.

Prefer standard library.

Current major infrastructure dependencies:

```text
pgx
go-redis
```

Adding another dependency requires justification.

For trusted proxy/IP parsing, prefer:

```go
net
net/netip
net/http
```

from the standard library.

---

# Domain Boundary Rule

HTTP:

```text
decode
validate transport format
authenticate
call service
translate error
serialize response
```

Domain/service:

```text
authorization
business rules
state transitions
identifier validation
transaction rules
```

Persistence:

```text
SQL
pgx
sqlc
```

Keep these responsibilities separate.

---

# Deliverables

Expected final repository changes:

```text
GOALS.md
IMPLEMENT.md
IMPLEMENTATION_STATUS.md

internal/platform/network/...
internal/platform/httpx/...
internal/attendance/...

internal/platform/migrations/<new attendance identifier migration>
internal/platform/database/queries/...

openapi/openapi.yaml

compose development config
production compose config

.env.example

README.md

tests
```

Exact paths may differ if the existing architecture has a better established location.

Do not reorganize working code solely to match this document.

---

# Definition of Done

This hardening milestone is complete only when:

1. trusted proxy aware client IP resolution exists;
2. rate limiting works correctly behind trusted proxies;
3. spoofed forwarded headers cannot bypass client identity;
4. QR token is genuinely resolved/verified;
5. attendance no longer trusts arbitrary member identity from client;
6. raw QR tokens are never persisted;
7. attendance transactions remain atomic;
8. transactional outbox still works;
9. production Docker topology does not expose Redis/PostgreSQL publicly;
10. `ReadHeaderTimeout` is configured;
11. redundant middleware recovery is cleaned up;
12. documentation structure is aligned;
13. OpenAPI matches implementation;
14. tests cover new security behavior;
15. CI passes;
16. integration tests pass;
17. Docker image builds successfully;
18. no new unnecessary architecture complexity is introduced.

---

# Expected Agent Workflow

Codex should:

```text
1. Read GOALS.md
2. Read IMPLEMENT.md
3. Read this hardening brief
4. Inspect current implementation
5. Inspect current tests
6. Implement P0-1
7. Test
8. Implement P0-2
9. Test
10. Continue P1 tasks sequentially
11. Run complete validation
12. Update documentation
13. Report exact completed work
14. Report remaining limitations
```

Do not state that something is verified unless it was actually verified.

---

# Commit Strategy

Prefer small logical commits.

Suggested sequence:

```text
fix: trust forwarded client IP only from configured proxies

feat: verify attendance QR identifiers server-side

chore: separate development and production compose profiles

fix: harden HTTP server timeouts

refactor: simplify HTTP middleware recovery chain

docs: align NexusAPI architecture documents

docs: update OpenAPI attendance contract

test: expand security and attendance regression coverage
```

Codex may combine commits where appropriate, but avoid one giant unreviewable commit.

---

# Final Report Format

At completion, report:

## Completed

List implemented changes.

## Security Changes

Explain security-relevant behavior.

## API Changes

List changed endpoint contracts.

## Database Changes

List migration/schema changes.

## Tests

Report actual commands executed and result.

## CI / Docker

Report what was verified.

## Breaking Changes

Explicitly state any.

## Remaining Work

Only real remaining items.

---

# Milestone Result

After this execution:

```text
NexusAPI v0.1 Foundation
```

should be safe enough to proceed into the next phase:

```text
Loyal Fitness real read integration
        ↓
member pagination
dashboard
PT session reads
finance summary
        ↓
attendance integration
        ↓
load testing
```

Do not begin those next-phase features during this hardening task unless required to validate the existing integration boundary.

---

# Guiding Principle

> Harden the foundation before increasing the traffic running through it.

NexusAPI should remain:

```text
boring
predictable
secure
observable
modular
easy to deploy
easy to debug
hard to misuse
```

Correctness and maintainability are more important than adding more features.
