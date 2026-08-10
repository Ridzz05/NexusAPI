# NexusAPI implementation rules

This document is the engineering companion to [`GOALS.md`](GOALS.md). It defines the boundaries that must remain true while the modular monolith grows.

## Architecture decisions

- Keep one deployable Go modular monolith with explicit domain and platform boundaries.
- Use PostgreSQL as the source of truth. Use `pgx` and generated `sqlc` queries for persistence; do not add an ORM.
- Keep authentication and authorization at the HTTP boundary and repeat domain authorization at write boundaries.
- Treat external systems as adapters. They may provide read models, but they do not silently become NexusAPI persistence.
- Use transactional outbox writes for events that must survive Redis or consumer outages.
- Keep global mutable state out of domain code. Dependencies, clocks, and clients are injected at construction time.
- Keep the published OpenAPI document at 3.0.3 until a consumer/tooling compatibility review supports a deliberate 3.1 migration; contract correctness takes priority over a cosmetic version bump.

## Security and correctness principles

- Fail closed on missing authentication, invalid configuration, unknown JSON fields, oversized bodies, and unavailable dependencies.
- Resolve real client IP only through trusted proxy CIDRs; never trust forwarding headers from an untrusted immediate peer.
- Attendance QR values are opaque high-entropy identifiers. Persist only a SHA-256 digest, resolve identity server-side, and never let a client-selected member become authoritative.
- Keep state changes and their outbox records in one serializable transaction with bounded retry for PostgreSQL serialization/deadlock errors.
- Return stable public error codes and request IDs without stack traces or internal error text. Do not log secrets, raw tokens, or request bodies.
- Bound timeouts, headers, query parameters, pagination, and tracked rate-limit clients.

## Delivery roadmap

1. Preserve the foundation and pass the local unit, race, vet, build, OpenAPI, and SQLC-drift checks.
2. Complete PostgreSQL-backed integration coverage for state transitions, identifier lifecycle, authorization, serialization retry, and outbox atomicity.
3. Add domain features only after their boundary, authorization, persistence queries, API contract, and operational checks are explicit.

## Definition of Done

A change is complete only when its behavior is implemented, its public contract and documentation agree with the runtime, relevant unit and integration tests exist, formatting/vet/build checks pass, migrations and generated SQL remain in sync, and the production security checklist has been reviewed. Docker-dependent checks must be run in CI or a Docker-capable environment when local Docker is unavailable.
