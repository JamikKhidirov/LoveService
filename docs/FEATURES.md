# Features: monetization, ranking, verification and sessions

This document describes the feature set added in **v1.2**. For request/response
examples see [API.md](API.md) and the live Swagger UI at `/docs`.

## Contents

- [Who liked me / Premium](#who-liked-me--premium)
- [Coin wallet and virtual gifts](#coin-wallet-and-virtual-gifts)
- [ELO attractiveness ranking](#elo-attractiveness-ranking)
- [Daily top picks](#daily-top-picks)
- [Profile statistics](#profile-statistics)
- [Device and session management](#device-and-session-management)
- [Face verification](#face-verification)
- [Configuration](#configuration)
- [Data model](#data-model)
- [Production checklist](#production-checklist)

## Who liked me / Premium

Free users only see **how many** people liked them:

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

The list is revealed in two ways:

1. **Premium** — `POST /api/v1/premium/subscribe` with `{"plan":"month"}` or
   `{"plan":"year"}`. Early renewal extends the current expiry instead of
   replacing it.
2. **One-off unlock** — `POST /api/v1/discovery/likes/unlock` spends 100 coins
   and reveals likes for 24 hours (`users.likes_unlocked_until`). The call is
   idempotent while an unlock or Premium is active.

`GET /api/v1/premium` returns the current state, coin balance, locked counter and
the price of the one-off unlock.

> `POST /premium/subscribe` and `POST /wallet/topup` are **mock** endpoints for
> the demo. In production they must only run after a verified App Store /
> Google Play / payment-provider receipt.

## Coin wallet and virtual gifts

The wallet is stored in `users.coins`; every movement is appended to
`coin_transactions` (`topup`, `gift_sent`, `gift_received`, `likes_unlock`).

| Gift | Code | Coins |
|---|---|---|
| 🌹 Rose | `rose` | 10 |
| ❤️ Heart | `heart` | 25 |
| 🧸 Teddy Bear | `teddy` | 50 |
| 💍 Ring | `ring` | 200 |
| 👑 Crown | `crown` | 500 |

`POST /api/v1/chat/{conversation_id}/gifts` with `{"gift_code":"rose"}`:

- validates the conversation and that both users are still matched;
- debits the sender and credits the recipient **in one transaction**;
- inserts a message with `type=gift` and `metadata` (`gift_code`, `gift_name`,
  `gift_emoji`, `coins`);
- writes `gift_transactions` plus two `coin_transactions` rows;
- emits `message.created` (and pushes `gift.received` over WebSocket).

Insufficient balance → `402 insufficient_coins`. Unknown gift → `422`.

Chat media uses `POST /api/v1/chat/{conversation_id}/media` (multipart `file`),
accepting the same image/video formats as profile media.

## ELO attractiveness ranking

Each user has `users.elo_rating` (default **1000**, clamped to 100–3000). It is
internal and never exposed in public profile payloads.

On every swipe the target's rating is updated:

```
expected = 1 / (1 + 10^((swiper_rating - target_rating) / 400))
next     = clamp(target_rating + K * (outcome - expected), 100, 3000)
```

| Action | Outcome | K |
|---|---|---|
| `like` | 1 | 16 |
| `superlike` | 1 | 24 |
| `dislike` | 0 | 8 |

The delta is stored in `swipes.elo_delta`, so `POST /api/v1/swipes/rewind`
reverses both the swipe and the rating change.

Discovery accepts `sort`:

- `balanced` (default) — blends distance and rating;
- `distance` — nearest first;
- `rating` — highest attractiveness first.

Profiles liked by many (and by highly-rated users) therefore climb the discovery
ordering.

## Daily top picks

`GET /api/v1/recommendations/top-picks` returns 4–5 profiles. The set is computed
once per user and day, stored in `top_picks`, and reused for the rest of the day.

The compatibility score is transparent and uses only profile data:

- 50% shared interests (Jaccard similarity);
- 30% proximity (clamped at 50 km);
- 10% verified badge;
- 10% profile completeness (photo + bio).

Requires the viewer's location (otherwise `422`). Alias:
`GET /api/v1/discovery/top-picks`.

## Profile statistics

`GET /api/v1/profile/stats` returns:

| Field | Meaning |
|---|---|
| `profile_views_total` | All profile views |
| `profile_views_week` | Views in the last 7 days |
| `likes_received_week` | Incoming likes in the last 7 days |
| `likes_received_total` | Incoming likes ever |
| `likes_you` | Not-yet-answered incoming likes |
| `matches_week` | Matches created in the last 7 days |
| `matches_total` | Active matches |

A view is written to `profile_views` at most once per viewer/target/day when
another user's profile is opened.

## Device and session management

Each login/registration creates an `auth_sessions` row:

| Column | Meaning |
|---|---|
| `platform` | `ios`, `android` or `web` (from `X-Device-Platform`, then User-Agent) |
| `device_model` | Free text from `X-Device-Model` |
| `ip` | Client IP |
| `user_agent` | Truncated User-Agent |
| `created_at` | Login date |
| `last_seen_at` | Touched at most once per minute |
| `revoked_at` | Set on logout/revoke |

Access tokens carry the session id in the `sid` claim; refresh tokens store
`session_id`. The auth middleware checks the session on every request, so:

- `DELETE /api/v1/sessions/{id}` logs out one device immediately;
- `POST /api/v1/auth/logout-all` revokes every session and bumps
  `users.token_version`, invalidating all access tokens;
- password change/reset does the same.

`GET /api/v1/sessions` lists active sessions newest-first and marks the caller
with `"current": true`.

## Face verification

`POST /api/v1/verification/face` accepts a multipart selfie (`file` or `selfie`):

1. the primary (or first approved) profile photo is loaded from storage;
2. the selfie is stored under `verifications/{user_id}/…`;
3. a pluggable `verification.Verifier` compares both images;
4. the attempt is recorded in `face_verifications`.

Statuses: `pending`, `approved`, `rejected`, `manual_review`, `error`. When the
score reaches `FACE_VERIFY_THRESHOLD` (default `0.75`) the profile gets
`is_verified=true` and a `user.verified` event is emitted. Missing profile photo
→ `422 photo_required`. Provider failure → `502 verification_failed` (the attempt
is still recorded with `status=error` for support).

Providers:

- `FACE_VERIFY_URL` empty → `AutoVerifier` (development/CI, score `0.99`);
- otherwise `HTTPVerifier` posts a multipart request with `selfie` and
  `reference` parts and expects `{"score": 0..1}` (or `confidence`).

`GET /api/v1/verification/face` returns the latest attempt or
`{"status":"unverified"}`.

## Configuration

| Variable | Default | Meaning |
|---|---|---|
| `FACE_VERIFY_URL` | empty | Provider endpoint; empty selects the auto-verifier |
| `FACE_VERIFY_API_KEY` | empty | Bearer token sent to the provider |
| `FACE_VERIFY_THRESHOLD` | `0.75` | Minimum similarity score for approval |
| `FACE_VERIFY_TIMEOUT` | `20s` | Provider timeout |
| `MAX_UPLOAD_MB` | `50` | Used for selfies and chat media too |

## Data model

Migration `internal/store/migrations/0004_social.sql` adds:

- `users.premium_until`, `likes_unlocked_until`, `coins`, `elo_rating`
- `swipes.elo_delta`
- `auth_sessions`, `refresh_tokens.session_id`
- `face_verifications`
- `profile_views`, `top_picks`
- `messages.metadata`, `gift_transactions`, `coin_transactions`

All changes are additive; older clients simply ignore the new fields.

## Production checklist

- Replace the mock subscription/top-up endpoints with verified store receipts.
- Configure a real face-comparison provider and keep `FACE_VERIFY_URL` private.
- Set `JWT_SECRET`, database and S3 credentials; disable demo seeding.
- Review the coin economy (prices, unlock cost, daily limits) before launch.
- Monitor `coin_transactions`, `gift_transactions` and `face_verifications` for
  abuse; gifts are user-to-user value transfers.
