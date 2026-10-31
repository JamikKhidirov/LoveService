# API reference

![Request flow](assets/request-flow.svg)

Base URL: `http://localhost:8080`. All JSON endpoints are under `/api/v1`.

Version **1.2** adds monetization (“who liked me”, Premium, coins and virtual
gifts), ELO attractiveness ranking, device/session management, automatic face
verification, profile statistics and daily top picks.

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
| GET | `/api/v1/discovery/likes` | Incoming likes (hidden until Premium/unlock) |
| GET | `/api/v1/discovery/likes/sent` | Sent likes |
| GET | `/api/v1/discovery/stats` | Badge counters |
| GET | `/api/v1/recommendations/top-picks` | Daily top picks (4–5) |
| POST | `/api/v1/discovery/likes/unlock` | Buy a 24h “who liked me” reveal (100 coins) |

Query parameters: `lat`, `lng`, `radius_km`, `min_age`, `max_age`,
`gender` (repeatable) or `genders` (comma-separated), `limit`, `offset`,
`expand` (default `true`), `sort` (`balanced` by default, `distance` or
`rating`).

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

### Ranking (ELO)

Every profile has an internal attractiveness rating (`elo_rating`, starting at
1000). Likes and superlikes raise the target's rating, dislikes lower it slightly;
likes from highly-rated users count more. `sort=rating` orders by this rating,
`sort=balanced` (default) blends distance and rating. The rating is never exposed
in public payloads.

## Profile statistics

| Method | Path | Description |
|---|---|---|
| GET | `/api/v1/profile/stats` | Views (total / last 7 days), likes received and matches |

A view is recorded at most once per viewer/target/day when
`GET /api/v1/profiles/{userID}` is called.

```json
{
  "data": {
    "profile_views_total": 412,
    "profile_views_week": 38,
    "likes_received_week": 12,
    "likes_received_total": 94,
    "likes_you": 12,
    "matches_week": 3,
    "matches_total": 21
  }
}
```

## Top picks

`GET /api/v1/recommendations/top-picks` (alias `/api/v1/discovery/top-picks`)
returns 4–5 profiles, computed once per user and day and stored in `top_picks`.
The compatibility score blends shared interests, distance, verification and
profile completeness.

## Who liked me, Premium and coins

| Method | Path | Description |
|---|---|---|
| GET | `/api/v1/discovery/likes` | Free users receive `premium_required=true` + a counter |
| POST | `/api/v1/discovery/likes/unlock` | Spend 100 coins for a 24h reveal |
| GET | `/api/v1/premium` | Premium/coin/lock status |
| POST | `/api/v1/premium/subscribe` | Mock subscription (`month` or `year`) |
| GET | `/api/v1/wallet` | Coin balance + recent transactions |
| POST | `/api/v1/wallet/topup` | Mock coin purchase (`coins`, max 10 000) |

Free response:

```json
{
  "data": {
    "items": [],
    "count": 7,
    "premium_required": true,
    "premium_active": false,
    "unlock_cost_coins": 100
  }
}
```

Premium members (or users with an active 24h unlock) receive the profiles in
`items`. Premium also unlocks the list without spending coins. The subscription
and top-up endpoints are mocks for the demo; production must verify store or
payment-provider receipts first.

## Virtual gifts

| Method | Path | Description |
|---|---|---|
| GET | `/api/v1/gifts/catalog` | Fixed catalogue and coin prices |
| POST | `/api/v1/chat/{conversationID}/gifts` | Send a gift (alias under `/conversations/...`) |
| POST | `/api/v1/chat/{conversationID}/media` | Send a photo/video (multipart `file`) |

Catalogue: `rose` 10, `heart` 25, `teddy` 50, `ring` 200, `crown` 500 coins.
The sender is debited and the recipient credited atomically; the chat message has
`type=gift` and a `metadata` object.

```bash
curl -sX POST http://localhost:8080/api/v1/chat/<conversation-id>/gifts \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"gift_code":"rose"}'
```

## Face verification

| Method | Path | Description |
|---|---|---|
| GET | `/api/v1/verification/face` | Latest attempt (or `unverified`) |
| POST | `/api/v1/verification/face` | Compare a selfie with the primary photo |

```bash
curl -sX POST http://localhost:8080/api/v1/verification/face \
  -H "Authorization: Bearer $TOKEN" \
  -F 'file=@selfie.jpg'
```

The server stores the selfie, calls the provider configured through
`FACE_VERIFY_URL` / `FACE_VERIFY_API_KEY` and approves when the score reaches
`FACE_VERIFY_THRESHOLD` (default `0.75`). An empty URL selects the development
auto-approver. Approval sets `profiles.is_verified` and emits `user.verified`.

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
| POST | `/api/v1/conversations/{id}/media` | Send a photo/video (`file`) |
| POST | `/api/v1/conversations/{id}/gifts` | Send a coin gift |

Chat message types: `text`, `image`, `video`, `gift`, `system`. Gift messages
carry the gift payload in `metadata`.

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

### Device & session management

Each login creates an `auth_sessions` row. Access tokens carry the session id in
the `sid` claim, so revoking one row logs that device out immediately.

| Method | Path | Description |
|---|---|---|
| GET | `/api/v1/sessions` | Active devices: IP, phone model, platform, login date, `current` |
| DELETE | `/api/v1/sessions/{sessionID}` | Log out one device |
| POST | `/api/v1/auth/logout-all` | Log out everywhere (all sessions) |

Phones and native clients should send `X-Device-Platform: ios|android` and
`X-Device-Model: iPhone 15 Pro` on login/register so the list is readable:

```bash
curl -sX POST http://localhost:8080/api/v1/auth/login \
  -H 'Content-Type: application/json' \
  -H 'X-Device-Platform: android' \
  -H 'X-Device-Model: Pixel 8' \
  -d '{"email":"demo@loveservice.app","password":"LoveDemo123!"}'

curl -s http://localhost:8080/api/v1/sessions \
  -H "Authorization: Bearer $TOKEN"
```

```json
{
  "data": {
    "count": 2,
    "items": [
      {
        "id": "…",
        "platform": "android",
        "device_model": "Pixel 8",
        "ip": "203.0.113.10",
        "created_at": "2026-10-31T09:12:44Z",
        "last_seen_at": "2026-10-31T09:40:02Z",
        "current": true
      }
    ]
  }
}
```

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
| 402 | `premium_required`, `insufficient_coins` | Paywall or empty wallet |
| 403 | `forbidden`, `blocked`, `conversation_closed`, `account_inactive` | Not allowed |
| 404 | `not_found` | Missing resource |
| 409 | `email_taken`, `already_swiped`, `conflict` | Conflict |
| 422 | `validation_error`, `self_action`, `media_limit_reached`, `photo_required` | Invalid input |
| 429 | `rate_limited`, `daily_limit_reached` | Too many requests |
| 500 | `internal_error` | Server error |
| 502 | `verification_failed` | Face-verification provider error |
