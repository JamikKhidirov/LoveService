# LoveService — dating backend in Go

![LoveService](assets/hero.svg)

Production-shaped dating backend: registration, profiles, media uploads to
MinIO/S3, nearby discovery with an expanding radius, likes / dislikes /
superlikes, rewind, matches, real-time chat, notifications, blocks/reports,
moderation, and **audited lawful-access tooling for technical administrators**.

![Demo flow](assets/demo-flow.svg)

![Architecture](assets/architecture.svg)

## Quick start (Docker)

```bash
docker compose up -d --build
```

| Service | URL | Credentials |
|---|---|---|
| API | http://localhost:8080 | — |
| Swagger UI | http://localhost:8080/docs | Bearer token |
| OpenAPI YAML | http://localhost:8080/openapi.yaml | — |
| MinIO S3 | http://localhost:9000 | `minioadmin` / `minioadmin` |
| MinIO Console | http://localhost:9001 | `minioadmin` / `minioadmin` |
| Kafka UI | http://localhost:8081 | — |
| Postgres | `localhost:5432` | `love` / `love`, db `love` |
| Redis | `localhost:6379` | — |

Demo accounts (seeded automatically):

| Role | Email | Password |
|---|---|---|
| User | `demo@loveservice.app` | `LoveDemo123!` |
| Technical admin | `admin@loveservice.app` | `AdminDemo123!` |
| Moderator | `moderator@loveservice.app` | `ModeratorDemo123!` |

> Change `JWT_SECRET` and all demo passwords before exposing the service.

## What is implemented

- **Auth** — email/password registration, JWT access tokens (15 min), rotating
  opaque refresh tokens (30 days, hashed at rest), email verification codes,
  password reset, change password, logout.
- **Profiles** — bio, city, height, job, education, interests, looking-for,
  location, preferences (age/gender/radius/global mode), privacy settings.
- **Media** — multipart photo/video upload to MinIO (S3 API), 9 photos + 2 videos,
  primary photo, storage cleanup on delete.
- **Discovery** — haversine distance in SQL, age/gender filters, privacy-aware
  distance/online visibility, automatic radius expansion
  (5 → 12.5 → 30 → 75 → … km).
- **Swipes** — like/dislike/superlike, reciprocal-like match creation in one
  transaction, conversation creation, daily limits, rewind window.
- **Chat** — conversations, paginated messages, read receipts, typing events,
  WebSocket push plus REST fallback.
- **Notifications** — in-app notifications generated from domain events through a
  transactional outbox, Kafka fan-out (or in-process fallback), WebSocket push.
- **Safety** — blocks, reports, moderation queue.
- **Admin** — stats, user search/detail, ban/deactivate, role management,
  verification badge, report triage, photo removal, broadcasts, retention purge,
  audit log.
- **Technical admin / lawful access** — legal-request workflow, optional second-
  admin approval, scoped data export, SHA-256 fingerprint, full audit trail.
- **Privacy** — any user can export their own data.
- **Observability** — health/readiness, Prometheus metrics, structured logs.

## Documentation

| Document | Contents |
|---|---|
| [docs/API.md](docs/API.md) | Endpoint reference and examples |
| [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) | Components, data flow, design decisions |
| [docs/WEBSOCKET.md](docs/WEBSOCKET.md) | Real-time protocol and event catalogue |
| [docs/DEPLOYMENT.md](docs/DEPLOYMENT.md) | Docker, environment variables, mobile LAN setup |
| [docs/DEMO.md](docs/DEMO.md) | Animated walkthrough with demo credentials |
| [docs/TESTS.md](docs/TESTS.md) | Test layout, commands and coverage map |
| [docs/TEST_ACCOUNTS.md](docs/TEST_ACCOUNTS.md) | Seeded users and what to test |
| [docs/ADMIN_LAWFUL_ACCESS.md](docs/ADMIN_LAWFUL_ACCESS.md) | Lawful-access policy and workflow |
| [docs/postman_collection.json](docs/postman_collection.json) | Postman collection (Import → Collection) |
| [docs/openapi.yaml](docs/openapi.yaml) | OpenAPI 3.1 specification |

## Local development without Docker

```bash
cp .env.example .env
go mod tidy
go run ./cmd/api
```

Requires Postgres, Redis, Kafka (optional) and MinIO. `KAFKA_ENABLED=false` and
`STORAGE_DRIVER=local` let you run without those services.

## Tests

Unit and black-box tests live in a separate `tests/` package tree that mirrors
`internal/`:

```text
tests/
├── docs/                       OpenAPI + Postman + compose validation
├── internal/
│   ├── app/                    composition root
│   ├── cache/                   Redis fallback, limits, presence
│   ├── config/                  env parsing and production guards
│   ├── domain/                  geo, models, errors
│   ├── events/                  outbox publisher, local bus
│   ├── mailer/
│   ├── observability/           logger + metrics middleware
│   ├── security/                JWT, bcrypt, validators
│   ├── service/                 pure service validation paths
│   ├── storage/                 local media storage
│   ├── store/                   DB integration (build tag)
│   ├── seed/                    demo seed (build tag)
│   └── transport/httpapi|ws/    router, Swagger, WebSocket handshake
└── integration/                 full HTTP + WebSocket flow (build tag)
```

```bash
go test ./...                         # fast unit suite, no infrastructure
go vet ./...
go test -tags=integration ./tests/integration/...   # needs the Docker stack
```

Smoke test against a running stack:

```bash
scripts/smoke.sh          # macOS / Linux / Git Bash
.\scripts\smoke.ps1       # Windows PowerShell
```

CI runs both suites: `.github/workflows/ci.yml`.

## Legal notice

The administrative endpoints exist to support **lawful** requests from competent
authorities. They must only be used with a verified legal basis, minimum
necessary scope, and in accordance with applicable data-protection law. Every
use is recorded in an append-only audit log.
