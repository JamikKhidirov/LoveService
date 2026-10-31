# Tests

![Request flow](assets/request-flow.svg)

The project keeps production code under `internal/` and **all tests in a separate
`tests/` tree** that mirrors it. Tests are black-box (`package <name>_test`) and
use only exported APIs, so they validate the same surface a client sees.

## Layout

| Test package | Covers |
|---|---|
| `tests/internal/app` | composition root fails fast on invalid configuration |
| `tests/internal/cache` | in-memory fallback, counters, rate limits, presence, JSON cache |
| `tests/internal/config` | env parsing, production secret/CORS guards, invalid fallbacks |
| `tests/internal/domain` | haversine distance, radius tiers, clamping, models, sentinel errors, ELO maths, gift catalogue |
| `tests/internal/events` | event encoding, local bus async delivery/panic safety, outbox publisher |
| `tests/internal/mailer` | dev log mailer and SMTP selection |
| `tests/internal/observability` | logger construction, metrics middleware, idempotent init |
| `tests/internal/security` | JWT issue/parse/expiry/foreign signature, bcrypt, every validator |
| `tests/internal/service` | constructors, swipe validation, admin validation, legal-request validation, premium plans, gifts |
| `tests/internal/storage` | local storage put/delete, traversal rejection, URL building |
| `tests/internal/store` | migrations + ping against Postgres *(integration tag)* |
| `tests/internal/seed` | demo + staff seed idempotency *(integration tag)* |
| `tests/internal/transport/httpapi` | health/ready/docs/openapi, auth gate, 404 |
| `tests/internal/transport/ws` | hub fan-out safety, presence, handshake rejection |
| `tests/integration` | real HTTP flows: health/docs, demo flow, media, admin surface, WebSocket, registration/refresh/logout, swipe → match → chat, rewind, block/report, legal-request workflow *(integration tag)* |
| `tests/docs` | OpenAPI markers, Postman JSON validity, feature docs, compose services |

## Commands

```bash
# Fast suite: no database, Redis, Kafka or MinIO required.
go test ./...
go vet ./...

# Integration suite: requires `docker compose up -d --build`.
go test -tags=integration ./tests/integration/... -v
go test -tags=integration ./tests/internal/store/... ./tests/internal/seed/...

# Full smoke test (HTTP + E2E)
scripts/smoke.sh
```

Environment variables for integration tests:

| Variable | Default |
|---|---|
| `LOVESERVICE_BASE_URL` | `http://localhost:8080` |
| `DATABASE_URL` | `postgres://love:love@localhost:5432/love?sslmode=disable` |

## What the integration suite checks

1. `/health`, `/ready`, `/docs`, `/openapi.yaml` return 200.
2. Demo login returns tokens and a profile.
3. Location update + discovery with expanding radius succeed.
4. Chat, matches, notifications, swipe limits and self-export return data.
5. Media upload goes to MinIO, the public URL is readable, then deletion works.
6. Admin stats, user detail, reports, audit log and legal-request listing work.
7. Moderator is rejected from admin-only routes with `403`.
8. WebSocket handshake accepts the JWT and answers `ping` with `pong`.
9. Unauthenticated requests are rejected with `401`.
10. Registration → email verification → login → refresh rotation → logout revokes the refresh token.
11. Two users swipe each other, a match and conversation appear, a message is delivered and read receipts work.
12. Rewind refuses matched swipes, undoes a plain swipe and lets the profile be swiped again.
13. Blocking hides interactions, reports reach staff, swipe limits stay consistent.
14. A legal request is created, cannot be exported while pending, and the creation is audited.
15. Device sessions list the current login, a revoked session stops working, and logout-all invalidates every device.
16. Premium/unlock gates “who liked me”, coins can be topped up, gifts move value between wallets and appear as `type=gift` messages.

## Adding a test

Mirror the package path under `tests/internal/`, use the external `_test`
package, and prefer table-driven cases. Database-backed tests must start with:

```go
//go:build integration

package store_test
```

so `go test ./...` stays fast and infrastructure-free.
