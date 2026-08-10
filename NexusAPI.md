# GOALS.md

# NexusAPI — Project Goals

## Mission

Membangun reusable backend API platform berbasis Go yang dapat menjadi communication layer untuk berbagai aplikasi dan sistem.

NexusAPI harus dapat digunakan oleh:

* mobile apps;
* web apps;
* desktop apps;
* kiosks;
* IoT/device clients;
* internal services;
* external integrations.

Loyal Fitness menjadi implementation target pertama, tetapi NexusAPI tidak boleh bergantung secara arsitektural pada Loyal Fitness.

---

# Primary Goals

## G1 — Build a Reusable API Foundation

Buat foundation REST API yang dapat digunakan kembali pada project berikutnya tanpa perlu membangun backend dari nol.

Foundation harus menyediakan:

* HTTP server;
* routing;
* configuration;
* logging;
* database;
* Redis;
* authentication;
* authorization;
* standardized response;
* error handling;
* pagination;
* OpenAPI;
* testing;
* Docker;
* CI/CD foundation.

---

## G2 — Maintain Strong Domain Boundaries

Gunakan modular monolith.

Domain harus terpisah secara jelas tanpa menghasilkan distributed-system complexity.

Target:

```text
internal/
├── access/
├── member/
├── commerce/
├── attendance/
├── reporting/
└── ...
```

Setiap module harus dapat berkembang secara independen secara konseptual.

---

## G3 — API First

Semua client harus berkomunikasi menggunakan contract yang jelas.

OpenAPI menjadi source of truth untuk public API.

Target consumer:

```text
Flutter
React
Next.js
native mobile
external client
```

Frontend tidak boleh bergantung pada implementation detail backend.

---

## G4 — Production Ready by Default

Project harus aman digunakan pada VPS production tanpa redesign besar.

Target minimum:

* graceful shutdown;
* connection pooling;
* database migrations;
* structured logging;
* request timeout;
* CORS;
* rate limiting;
* health checks;
* environment config;
* secure secret handling;
* Docker production image.

---

## G5 — PostgreSQL First

Gunakan PostgreSQL sebagai production database utama.

Database strategy harus mengutamakan:

* correctness;
* indexing;
* transaction safety;
* explicit SQL;
* predictable performance.

Gunakan:

```text
pgx
+
sqlc
```

sebagai default database stack.

---

## G6 — Avoid Unbounded Data Access

Collection endpoint harus menggunakan pagination.

Tidak boleh ada production API pattern:

```sql
SELECT * FROM large_table;
```

tanpa limit yang jelas.

Prefer:

```text
cursor pagination
```

untuk dataset besar.

---

## G7 — Scale Through Architecture Before Infrastructure

Sebelum menambah server, optimalkan:

1. query;
2. index;
3. pagination;
4. caching;
5. connection pooling;
6. asynchronous processing.

Jangan menggunakan distributed architecture untuk menyelesaikan masalah yang sebenarnya berasal dari query buruk.

---

## G8 — Keep Deployment Simple

NexusAPI harus dapat dijalankan dengan:

```bash
./nexus-api
```

atau:

```bash
docker compose up -d
```

Deployment pertama tidak membutuhkan Kubernetes.

Target utama:

```text
Linux VPS
Docker
systemd
Caddy/Nginx
```

---

## G9 — Incrementally Integrate Existing Systems

NexusAPI tidak dimaksudkan untuk memaksa rewrite aplikasi lama.

Gunakan adapter / migration layer.

Contoh Loyal Fitness:

```text
Mobile
  ↓
NexusAPI
  ↓
PostgreSQL / Redis / Laravel
```

Migrasikan domain hanya ketika memberikan manfaat nyata.

---

## G10 — Prefer Read Migration Before Write Migration

Untuk existing systems, prioritaskan:

```text
read API
reporting
dashboard
search
listing
realtime feed
```

sebelum memindahkan:

```text
payment
checkout
inventory mutation
membership entitlement
financial mutation
```

Write path dengan business invariant tinggi membutuhkan parity test sebelum migration.

---

## G11 — Be Fast, Not Reckless

Performance adalah goal.

Tetapi correctness memiliki prioritas lebih tinggi.

Urutan prioritas:

```text
Correctness
Security
Maintainability
Observability
Performance
Developer convenience
```

Performance optimization harus berdasarkan evidence atau architectural necessity.

---

## G12 — Minimize Dependencies

Gunakan Go standard library jika cukup.

Dependency hanya ditambahkan bila:

* memberikan manfaat signifikan;
* mature;
* maintained;
* tidak menduplikasi kemampuan sederhana standard library.

Dependency count bukan achievement.

---

## G13 — Maintain Excellent Developer Experience

Developer baru harus dapat:

```bash
git clone
cp .env.example .env
docker compose up -d
go run ./cmd/api
```

dan mendapatkan environment development yang usable.

Dokumentasi setup harus tetap sinkron dengan implementation.

---

## G14 — Build for Observability

System harus mudah ditelusuri ketika error.

Setiap request memiliki:

```text
request_id
latency
HTTP method
route
status
```

Observability bukan feature tambahan setelah production.

Ia bagian dari core foundation.

---

## G15 — Security Is a Default

Semua endpoint dianggap private kecuali secara eksplisit dibuat public.

Jangan:

* log token;
* expose stack trace;
* commit secret;
* trust arbitrary client field;
* interpolate dynamic SQL secara tidak aman.

Authorization harus explicit.

---

# Loyal Fitness Goals

Untuk integration pertama dengan Loyal Fitness:

## Phase LF-1

Bangun read-only API:

```http
GET /api/v1/users/me
GET /api/v1/mobile/dashboard
GET /api/v1/members
GET /api/v1/pt-sessions
GET /api/v1/finance/summary
```

## Phase LF-2

Tambahkan:

```text
search
filter
cursor pagination
role scope
cache
```

## Phase LF-3

Tambahkan attendance / kiosk foundation.

Target:

```text
QR events
check-in
check-out
device heartbeat
realtime notifications
```

## Phase LF-4

Evaluasi migration domain lain berdasarkan profiling.

Jangan migrasikan Commerce hanya karena Go tersedia.

---

# Performance Goals

Target awal bukan benchmark marketing.

Target architecture:

* tidak ada unbounded collection endpoint;
* tidak ada obvious N+1 query;
* database connection pool dikonfigurasi;
* API tetap responsive pada concurrent request;
* expensive aggregation dapat di-cache;
* application dapat di-horizontal scale jika kelak diperlukan.

Load test harus dibuat setelah endpoint representatif tersedia.

---

# Reliability Goals

API harus:

* survive malformed input;
* recover dari panic pada request tanpa menjatuhkan process;
* fail fast saat startup config invalid;
* shutdown gracefully;
* menggunakan transaction pada multi-record mutation;
* memberikan deterministic error response.

---

# API Compatibility Goal

Public API V1 harus stabil.

Perubahan breaking tidak boleh dilakukan diam-diam.

Gunakan:

```text
/api/v1
/api/v2
```

ketika perubahan contract benar-benar breaking.

---

# Testing Goal

Critical paths harus memiliki regression test.

Minimum CI:

```bash
go fmt
go vet
go test ./...
go build ./...
```

Setiap bug penting yang diperbaiki sebaiknya menghasilkan regression test.

---

# Explicit Non-Goals

Pada fase awal kita TIDAK mengejar:

* Kubernetes;
* service mesh;
* dozens of microservices;
* Kafka;
* CQRS everywhere;
* event sourcing;
* GraphQL;
* multi-region deployment;
* distributed database;
* premature abstraction.

Semua hal tersebut hanya boleh dipertimbangkan jika kebutuhan nyata membenarkannya.

---

# Success Criteria

NexusAPI dianggap berhasil ketika:

1. dapat digunakan oleh lebih dari satu project;
2. API contract tidak terikat framework frontend;
3. deployment ke VPS sederhana;
4. module dapat ditambah tanpa merusak seluruh codebase;
5. integration test mudah dibuat;
6. database access predictable;
7. API aman untuk exposure production;
8. mobile dan web dapat menggunakan backend yang sama;
9. existing application dapat bermigrasi secara bertahap;
10. system tetap understandable oleh developer lain.

---

# Long-Term Direction

Target evolusi:

```text
Phase 1
Reusable REST API Foundation

        ↓

Phase 2
Loyal Fitness Mobile API

        ↓

Phase 3
Reporting + Realtime + Device API

        ↓

Phase 4
Reusable internal platform

        ↓

Phase 5
Selective domain extraction when justified
```

Possible future topology:

```text
                    NexusAPI
                       │
        ┌──────────────┼──────────────┐
        │              │              │
     Mobile           Web          Devices
        │              │              │
        └──────────────┼──────────────┘
                       │
          ┌────────────┼────────────┐
          │            │            │
      PostgreSQL     Redis       Services
                                   │
                          ┌────────┴────────┐
                          │                 │
                       Laravel          External
                       Systems            APIs
```

---

# Guiding Principle

> Build the boring foundation extremely well.

Kita tidak sedang mencoba membuat backend yang terlihat paling kompleks.

Kita sedang membangun backend yang:

**mudah dikembangkan, sulit dirusak, mudah di-deploy, dan masih nyaman digunakan bertahun-tahun ke depan.**
