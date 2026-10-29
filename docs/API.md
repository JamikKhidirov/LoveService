# API reference

![Request flow](assets/request-flow.svg)

Base URL: `http://localhost:8080`. All JSON endpoints are under `/api/v1`.

Successful responses:

```json
{ "data": { } }
```

Errors:

```json
{ "error": { "code": "validation_error", "message": "…", "details": { "field": "email" } } }
```

Authentication: `Authorization: Bearer <access_token>`.

- Access token TTL: **15 minutes** (`ACCESS_TTL`).
- Refresh token TTL: **30 days** (`REFRESH_TTL`), rotated on every refresh.
- In non-production, verification/reset codes are returned as `dev_code`.

## Health & docs

| Method | Path | Description |
|---|---|---|
| GET | `/health` | Liveness |
| GET | `/ready` | Readiness (Postgres ping) |
| GET | `/metrics` | Prometheus metrics |
| GET | `/docs` | Swagger UI |
| GET | `/openapi.yaml` | OpenAPI 3.1 document |

## Auth

| Method | Path | Auth | Description |
|---|---|---|---|
| POST | `/api/v1/auth/register` | — | Create an account |
| POST | `/api/v1/auth/login` | — | Email + password login |
| POST | `/api/v1/auth/refresh` | — | Rotate refresh token |
| POST | `/api/v1/auth/logout` | — | Revoke refresh token |
| POST | `/api/v1/auth/verify-email` | — | Verify with 6-digit code |
| POST | `/api/v1/auth/resend-verification` | — | Re-send verification code |
| POST | `/api/v1/auth/forgot-password` | — | Request reset code |
| POST | `/api/v1/auth/reset-password` | — | Reset with code |
| GET | `/api/v1/auth/me` | Bearer | Current user + profile |
| POST | `/api/v1/auth/change-password` | Bearer | Change password |
| POST | `/api/v1/auth/logout-all` | Bearer | Revoke every session (all devices) |

### Register

```bash
curl -sX POST http://localhost:8080/api/v1/auth/register \
  -H 'Content-Type: application/json' \
  -d '{
    "email": "new.user@example.com",
    "password": "StrongPass123!",
    "display_name": "Новый пользователь",
    "birth_date": "1997-06-15",
    "gender": "female",
    "phone": "+79990001122",
    "preferred_genders": ["male"]
  }'
```

### Login

```bash
curl -sX POST http://localhost:8080/api/v1/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"email":"demo@loveservice.app","password":"LoveDemo123!"}'
```

## Profile

| Method | Path | Description |
|---|---|---|
| GET | `/api/v1/profiles/me` | My profile |
| PATCH | `/api/v1/profiles/me` | Update profile fields |
| DELETE | `/api/v1/profiles/me` | Delete account + media |
| POST | `/api/v1/profiles/me/location` | Send `{lat,lng}` from the phone |
| GET/PATCH | `/api/v1/profiles/me/preferences` | Age/gender/radius/global mode |
| GET/PATCH | `/api/v1/profiles/me/settings` | Notifications/privacy/language |
| GET | `/api/v1/profiles/{userID}` | Public profile |

### Update location

```bash
curl -sX POST http://localhost:8080/api/v1/profiles/me/location \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"lat":55.7558,"lng":37.6173}'
```

## Media

| Method | Path | Description |
|---|---|---|
| GET | `/api/v1/profiles/me/photos` | List media |
| POST | `/api/v1/profiles/me/photos` | Multipart upload (`file`) |
| DELETE | `/api/v1/profiles/me/photos/{mediaID}` | Delete media |
| PATCH | `/api/v1/profiles/me/photos/{mediaID}/primary` | Set primary photo |

Allowed: `jpg jpeg png webp gif` (max 9) and `mp4 webm mov m4v` (max 2).

```bash
curl -sX POST http://localhost:8080/api/v1/profiles/me/photos \
  -H "Authorization: Bearer $TOKEN" \
  -F 'file=@photo.jpg'
```

Uploaded objects are stored in the MinIO bucket `love-media`; the returned URL is
publicly readable (bucket policy is created by the API).

## Discovery

| Method | Path | Description |
|---|---|---|
| GET | `/api/v1/discovery` | Nearby discovery, auto-expanding |
| GET | `/api/v1/discovery/nearby` | Alias |
| GET | `/api/v1/discovery/nearby/count` | Candidate count |
| GET | `/api/v1/discovery/likes` | Incoming likes |
| GET | `/api/v1/discovery/likes/sent` | Sent likes |
| GET | `/api/v1/discovery/stats` | Badge counters |

Query parameters: `lat`, `lng`, `radius_km`, `min_age`, `max_age`,
`gender` (repeatable) or `genders` (comma-separated), `limit`, `offset`,
`expand` (default `true`).

```bash
curl -s "http://localhost:8080/api/v1/discovery?lat=55.7558&lng=37.6173&radius_km=10&expand=true&limit=20" \
  -H "Authorization: Bearer $TOKEN"
```

Response:

```json
{
  "data": {
    "profiles": [ { "user_id": "…", "display_name": "Анна", "age": 27, "distance_km": 1.8 } ],
    "radius_km": 10,
    "expanded_to_radius_km": 12.5,
    "next_offset": 20,
    "has_more": false,
    "total_candidates": 7
  }
}
```

### Radius expansion

`expand=true` walks tiers until `limit` candidates are found:

`5, 12.5, 30, 75, 150, 300, 750, 1500, 3000, 8000, 20000` km
(starting at the requested radius). `expanded_to_radius_km` reports the tier that
produced the page.

## Swipes and matches

| Method | Path | Description |
|---|---|---|
| POST | `/api/v1/swipes` | `like`, `dislike`, `superlike` |
| POST | `/api/v1/swipes/rewind` | Undo the last swipe (5-minute window) |
| GET | `/api/v1/swipes/limits` | Daily counters |
| GET | `/api/v1/matches` | Matches |
| GET | `/api/v1/matches/{matchID}` | Match + peer |
| DELETE | `/api/v1/matches/{matchID}` | Unmatch |

```bash
curl -sX POST http://localhost:8080/api/v1/swipes \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"target_user_id":"<uuid>","action":"like"}'
```

A reciprocal like atomically creates a match, a conversation and a
`match.created` outbox event.

## Chat

| Method | Path | Description |
|---|---|---|
| GET | `/api/v1/conversations` | Conversations + last message |
| GET | `/api/v1/conversations/{id}` | One conversation |
| GET | `/api/v1/conversations/{id}/messages` | Messages (`before`, `limit`) |
| POST | `/api/v1/conversations/{id}/messages` | Send message |
| POST | `/api/v1/messages` | Send message with `conversation_id` |
| POST | `/api/v1/conversations/{id}/read` | Read receipts |
| POST | `/api/v1/conversations/{id}/typing` | Typing indicator |

## Notifications

| Method | Path | Description |
|---|---|---|
| GET | `/api/v1/notifications` | List (`before`, `limit`) |
| GET | `/api/v1/notifications/unread-count` | Unread count |
| POST | `/api/v1/notifications/{id}/read` | Mark one read |
| POST | `/api/v1/notifications/read-all` | Mark all read |

## Safety and devices

| Method | Path | Description |
|---|---|---|
| GET | `/api/v1/blocks` | Blocked users |
| POST | `/api/v1/blocks` | Block `{user_id}` |
| DELETE | `/api/v1/blocks/{userID}` | Unblock |
| POST | `/api/v1/reports` | Report a user |
| POST | `/api/v1/devices` | Register push token |
| GET | `/api/v1/devices` | List my push tokens |
| DELETE | `/api/v1/devices?token=…` | Remove push token |

## Privacy

| Method | Path | Description |
|---|---|---|
| GET | `/api/v1/account/export` | Download my personal data |

## Admin (moderator + admin)

All admin calls are audited. The `moderator` role can read and triage; the
`admin` role can additionally change accounts, media and run maintenance.

| Method | Path | Role | Description |
|---|---|---|---|
| GET | `/api/v1/admin/stats` | moderator/admin | Platform counters |
| GET | `/api/v1/admin/users` | moderator/admin | Search/filter users |
| GET | `/api/v1/admin/users/{userID}` | moderator/admin | Account detail (`reason`) |
| PATCH | `/api/v1/admin/users/{userID}/status` | admin | Ban/deactivate/reactivate |
| PATCH | `/api/v1/admin/users/{userID}/role` | admin | Change role |
| PATCH | `/api/v1/admin/users/{userID}/verify` | admin | Verification badge |
| GET | `/api/v1/admin/reports` | moderator/admin | Abuse reports |
| PATCH | `/api/v1/admin/reports/{reportID}` | moderator/admin | Triage report |
| DELETE | `/api/v1/admin/photos/{photoID}` | admin | Remove media (`reason`) |
| POST | `/api/v1/admin/notifications/broadcast` | admin | In-app broadcast |
| POST | `/api/v1/admin/maintenance/purge` | admin | Retention purge (`reason`) |
| GET | `/api/v1/admin/audit-logs` | admin | Append-only audit log |

## Technical admin — lawful access only ⚠️

These endpoints disclose another person's data to a **technical administrator**
for verified requests from law-enforcement / national-security authorities.
Requires role `admin`. Every call is audited with actor, IP, request id, reason
and the declared legal basis.

| Method | Path | Description |
|---|---|---|
| POST | `/api/v1/admin/legal-requests` | Register case number, authority, country, legal basis, target, scopes |
| GET | `/api/v1/admin/legal-requests` | List requests (audited) |
| GET | `/api/v1/admin/legal-requests/{id}` | Read a request (audited) |
| POST | `/api/v1/admin/legal-requests/{id}/approve` | Second-admin approval |
| POST | `/api/v1/admin/legal-requests/{id}/export` | Export package + SHA-256 |

Scopes: `account`, `profile`, `media`, `matches`, `messages`, `swipes`,
`notifications`, `reports`.

```bash
# 1. Register the legal request
curl -sX POST http://localhost:8080/api/v1/admin/legal-requests \
  -H "Authorization: Bearer $ADMIN_TOKEN" -H 'Content-Type: application/json' \
  -d '{
    "case_number": "CASE-2026-000123",
    "authority": "National Police / Cybercrime Unit",
    "country": "RU",
    "legal_basis": "Court order #1234 dated 2026-01-10",
    "target_user_id": "<uuid>",
    "scopes": ["account","profile","media","matches","messages"],
    "reason": "Formal written request received"
  }'

# 2. Approve (must be a different admin when second approval is enabled)
curl -sX POST http://localhost:8080/api/v1/admin/legal-requests/<id>/approve \
  -H "Authorization: Bearer $ADMIN2_TOKEN" -H 'Content-Type: application/json' \
  -d '{"reason":"Legal basis verified"}'

# 3. Export
curl -sX POST http://localhost:8080/api/v1/admin/legal-requests/<id>/export \
  -H "Authorization: Bearer $ADMIN_TOKEN" -H 'Content-Type: application/json' \
  -d '{"reason":"Disclosure prepared for the requesting authority"}'
```

Exports never include `password_hash`, refresh tokens or email codes. The
exported JSON's SHA-256 is stored in `legal_requests.export_sha256` and in the
audit log.

## Error codes

| HTTP | Code | Meaning |
|---|---|---|
| 401 | `unauthorized`, `invalid_token`, `invalid_credentials` | Authentication failed |
| 403 | `forbidden`, `blocked`, `conversation_closed`, `account_inactive` | Not allowed |
| 404 | `not_found` | Missing resource |
| 409 | `email_taken`, `already_swiped`, `conflict` | Conflict |
| 422 | `validation_error`, `self_action`, `media_limit_reached` | Invalid input |
| 429 | `rate_limited`, `daily_limit_reached` | Too many requests |
| 500 | `internal_error` | Server error |
