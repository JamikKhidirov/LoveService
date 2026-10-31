# Deployment

## Docker Compose (recommended)

![Demo flow](assets/demo-flow.svg)

```bash
docker compose up -d --build
docker compose ps
docker compose logs -f api
```

Services:

| Container | Image | Ports |
|---|---|---|
| `love-api` | built from `Dockerfile` | 8080 |
| `love-postgres` | `postgres:16-alpine` | 5432 |
| `love-redis` | `redis:7-alpine` | 6379 |
| `love-kafka` | `apache/kafka:3.8.0` (KRaft) | 29092 (host) |
| `love-kafka-ui` | `provectuslabs/kafka-ui` | 8081 |
| `love-minio` | `bitnamilegacy/minio:2025.7.23-debian-12-r5` | 9000, 9001 |

The API waits for healthy Postgres/Redis/Kafka, retries the MinIO connection,
creates the `love-media` bucket, applies a public-read policy and runs migrations
and the demo seed.

### Reset everything

```bash
docker compose down -v
docker compose up -d --build
```

## Environment variables

| Variable | Default | Notes |
|---|---|---|
| `APP_ENV` | `development` | `production` hides `dev_code` |
| `HTTP_ADDR` | `:8080` | |
| `PUBLIC_BASE_URL` | `http://localhost:8080` | Swagger/links |
| `DATABASE_URL` | `postgres://love:love@localhost:5432/love?sslmode=disable` | |
| `REDIS_ADDR` | `localhost:6379` | |
| `KAFKA_ENABLED` | `true` | `false` → in-process bus |
| `KAFKA_BROKERS` | `localhost:29092` | `kafka:9092` in Docker |
| `JWT_SECRET` | dev value | **must** be ≥ 32 random bytes in production |
| `ACCESS_TTL` | `15m` | |
| `REFRESH_TTL` | `720h` | |
| `STORAGE_DRIVER` | `s3` | `local` for filesystem |
| `S3_ENDPOINT` | `localhost:9000` | `minio:9000` in Docker |
| `S3_PUBLIC_URL` | `http://localhost:9000/love-media` | URL written into profiles |
| `S3_BUCKET` | `love-media` | created automatically |
| `MAX_UPLOAD_MB` | `50` | |
| `AUTO_APPROVE_MEDIA` | `true` | `false` → moderation queue |
| `RATE_LIMIT_PER_MINUTE` | `240` | per IP via Redis |
| `SEED_DEMO` | `true` | |
| `DAILY_LIKE_LIMIT` | `100` | |
| `DAILY_SUPERLIKE_LIMIT` | `5` | |
| `REWIND_WINDOW` | `5m` | |
| `FACE_VERIFY_URL` | empty | Face-comparison provider; empty → dev auto-approver |
| `FACE_VERIFY_API_KEY` | empty | Bearer token for the provider |
| `FACE_VERIFY_THRESHOLD` | `0.75` | Minimum similarity score for approval |
| `FACE_VERIFY_TIMEOUT` | `20s` | Provider request timeout |
| `ADMIN_REQUIRE_SECOND_APPROVAL` | `true` | lawful-access four-eyes |
| `DOCS_DIR` | `./docs` | OpenAPI file location |

## Running on a phone (LAN)

1. Find the host LAN IP:

   ```powershell
   ipconfig
   ```

2. Set the API host and the S3 public URL to that IP:

   ```yaml
   # docker-compose.yml → api.environment
   PUBLIC_BASE_URL: http://192.168.1.10:8080
   S3_PUBLIC_URL: http://192.168.1.10:9000/love-media
   CORS_ORIGINS: "*"
   ```

   Then reboot the API:

   ```bash
   docker compose up -d api
   ```

3. Point the mobile app to `http://192.168.1.10:8080`.

Images already stored with `localhost` URLs keep the old host; re-upload them or
update the rows. For production set a real S3/CDN domain.

Android emulator: `10.0.2.2:8080`. iOS simulator: `localhost:8080`.

## Production checklist

- [ ] Strong `JWT_SECRET` (32+ random bytes), rotated keys supported via re-login.
- [ ] Postgres managed instance with backups and TLS.
- [ ] Redis with auth/TLS; separate logical DB for cache.
- [ ] Kafka with replication factor ≥ 3 (compose uses 1 for local).
- [ ] MinIO/S3 private bucket + CDN or signed URLs; update `S3_PUBLIC_URL`.
- [ ] SMTP credentials configured; `APP_ENV=production` (codes no longer returned).
- [ ] `SEED_DEMO=false`; remove/rotate demo accounts.
- [ ] `ADMIN_REQUIRE_SECOND_APPROVAL=true` and at least two admins.
- [ ] Restrict `/api/v1/admin/*` by network policy / VPN / SSO gateway.
- [ ] Ship logs and metrics to your observability stack (`/metrics`).
- [ ] Configure retention for `admin_audit_logs` and `legal_requests` per your
      legal obligations (these should be retained longer than normal data).
- [ ] Run `go test ./...`, `go vet ./...` and the smoke script in CI.

## Reverse proxy

The API expects `X-Forwarded-For` / `X-Real-IP` for rate limiting and audit logs
(chi `RealIP` middleware). Terminate TLS at the proxy and forward to `:8080`.
WebSocket upgrades need `Upgrade`/`Connection` headers forwarded (nginx does this
by default with `proxy_set_header Upgrade $http_upgrade`).
