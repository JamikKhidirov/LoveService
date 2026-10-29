# Architecture

![LoveService architecture](assets/architecture.svg)

LoveService is a **modular monolith** in Go. A single binary serves the REST API,
the WebSocket gateway and the outbox publisher; Postgres, Redis, Kafka and MinIO
are external services.

![Request flow](assets/request-flow.svg)

```
                    ┌────────────────────────────┐
   mobile app ─────▶ │  HTTP API (chi)            │
   admin tools ────▶ │  /api/v1/...  /docs  /ws   │
                    └───────────┬────────────────┘
                                │
        ┌───────────────────────┼──────────────────────────┐
        ▼                       ▼                          ▼
  ┌───────────┐          ┌─────────────┐             ┌─────────────┐
  │ Postgres  │          │   Redis     │             │  MinIO/S3   │
  │ 16        │          │ cache/pubsub│             │  media      │
  └─────┬─────┘          └──────┬──────┘             └─────────────┘
        │ outbox_events         │ websocket fan-out
        ▼                       ▲
  ┌───────────┐    Kafka    ┌────┴──────────┐
  │ publisher │────────────▶│ notification  │──▶ WebSocket / in-app
  └───────────┘  love.events │ consumer      │
                             └───────────────┘
```

## Packages

| Path | Responsibility |
|---|---|
| `cmd/api` | Process entrypoint, signal handling |
| `internal/app` | Dependency wiring, lifecycle, graceful shutdown |
| `internal/config` | Env parsing and defaults |
| `internal/domain` | Entities, errors, geo helpers (no transport/db imports) |
| `internal/security` | JWT, bcrypt, opaque tokens, validation |
| `internal/store` | pgx queries, transactions, embedded migrations |
| `internal/cache` | Redis: presence, counters, rate limits, pub/sub |
| `internal/storage` | Media abstraction: local FS or S3/MinIO |
| `internal/events` | Transactional outbox, Kafka bus, local bus, consumer |
| `internal/mailer` | SMTP or dev logger |
| `internal/service` | Business rules (auth, profile, discovery, swipe, chat, admin…) |
| `internal/transport/httpapi` | Routes, middleware, request/response mapping |
| `internal/transport/ws` | WebSocket hub, clients, Redis fan-out |
| `internal/observability` | Structured logs, Prometheus metrics |
| `internal/seed` | Idempotent demo data |

Dependency direction: `transport → service → store/domain`. The domain package
imports nothing from the application.

## Key design decisions

### Transactional outbox

A swipe, match, message or report writes its integration event to
`outbox_events` **in the same Postgres transaction** as the business data. A
background publisher polls unpublished rows and sends them to Kafka. If Kafka is
unavailable the service falls back to an in-process bus, so local development
works without Kafka and production never loses an event between the database and
the broker.

### Matches are atomic

`CreateSwipe` inserts the swipe, checks for a reciprocal like, and creates the
match + conversation in one transaction. Unique indexes on
`(user_a, user_b)` (ordered with `least`/`greatest`) make rematching idempotent.

### Notifications

`NotificationService.HandleEvent` consumes events and creates in-app
notifications, then pushes `notification.new` over the WebSocket hub. Users can
disable notifications in their settings; the check happens before insert.

### Presence and multi-instance WebSocket

Redis stores presence keys with a TTL refreshed by client pings. WebSocket events
are published to `love:ws:events`; every API instance subscribes and delivers to
its local sockets. A single instance also works without Redis (local delivery).

### Media

`storage.Storage` is an interface with two implementations:

- `S3` (MinIO) — used in Docker. The API waits for MinIO, creates the bucket and
  applies a public-read policy on startup, so no separate init container is
  needed.
- `Local` — used when `STORAGE_DRIVER=local`, served at `/media/*`.

`S3_PUBLIC_URL` controls the URL written into profiles. In Docker it is
`http://localhost:9000/love-media`; for a physical phone replace `localhost` with
the host's LAN IP.

### Discovery

Candidates are filtered in SQL (status, discoverability, blocks, already swiped,
gender, age) and ranked by a haversine expression. No PostGIS dependency is
required. The service walks radius tiers to implement “start nearby, then expand”.

### Security

- Passwords: bcrypt cost 12.
- Access tokens: HS256, issuer/audience validated, 15-minute TTL.
- Refresh tokens: 48 random bytes, only SHA-256 hashes stored, rotated on use,
  revoked on password change/reset and on ban.
- Email codes: 6 digits, hashed, 15-minute TTL, max 5 attempts.
- Rate limiting: Redis token bucket per IP (default 240 req/min).
- Admin roles are carried in the signed JWT and enforced per route group.
- Audit log is append-only by application policy; it records actor, action,
  target, reason, legal basis, IP, user agent and request id.

### Lawful access

Data disclosure is modelled as a first-class workflow:

1. An `admin` creates a **legal request** with case number, authority, country,
   legal basis, target and minimal scopes.
2. With `ADMIN_REQUIRE_SECOND_APPROVAL=true` (default), a *different* admin must
   approve it.
3. Export produces a JSON package and its SHA-256; both the request row and the
   audit log store the fingerprint.
4. Sensitive credentials (password hashes, refresh tokens, email codes) are never
   part of the export.

See [ADMIN_LAWFUL_ACCESS.md](ADMIN_LAWFUL_ACCESS.md).

## Data model (main tables)

- `users`, `profiles`, `preferences`, `user_settings`
- `photos`
- `swipes`, `matches`, `conversations`, `messages`
- `notifications`, `devices`, `blocks`, `reports`
- `refresh_tokens`, `email_codes`
- `outbox_events`
- `admin_audit_logs`, `legal_requests`

Migrations are embedded in `internal/store/migrations` and applied on startup
inside an advisory lock.
