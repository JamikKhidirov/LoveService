# Demo walkthrough

![How it works](assets/demo-flow.svg)

![Request flow](assets/request-flow.svg)

Everything below is seeded automatically when `SEED_DEMO=true`.

## Demo accounts

| Role | Email | Password | Notes |
|---|---|---|---|
| User | `demo@loveservice.app` | `LoveDemo123!` | Has matches, chats and incoming likes |
| Technical admin | `admin@loveservice.app` | `AdminDemo123!` | Lawful-access endpoints |
| Moderator | `moderator@loveservice.app` | `ModeratorDemo123!` | Reports, user lookup |

Other seeded profiles (same password `LoveDemo123!`):
`anna@`, `maria@`, `sofia@`, `elena@`, `kate@`, `polina@`, `dmitry@`, `alex@`,
`olga@loveservice.app`.

The demo user is pre-matched with Анна, Мария and София, and has pending likes
from Катя and Полина.

## 1. Sign in

```bash
TOKEN=$(curl -sX POST http://localhost:8080/api/v1/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"email":"demo@loveservice.app","password":"LoveDemo123!"}' \
  | jq -r '.data.tokens.access_token')
```

## 2. Discover nearby people

```bash
curl -s "http://localhost:8080/api/v1/discovery?lat=55.7558&lng=37.6173&radius_km=5&expand=true" \
  -H "Authorization: Bearer $TOKEN" | jq '.data | {radius_km, expanded_to_radius_km, count:(.profiles|length)}'
```

The radius grows until candidates are found. Distances are computed server-side
with haversine.

## 3. Like somebody

```bash
TARGET=$(curl -s "http://localhost:8080/api/v1/discovery?lat=55.7558&lng=37.6173" \
  -H "Authorization: Bearer $TOKEN" | jq -r '.data.profiles[0].user_id')

curl -sX POST http://localhost:8080/api/v1/swipes \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d "{\"target_user_id\":\"$TARGET\",\"action\":\"like\"}" | jq
```

If the like is reciprocal you get `matched: true`, a match and a conversation id.

## 4. Chat

```bash
CONV=$(curl -s http://localhost:8080/api/v1/conversations \
  -H "Authorization: Bearer $TOKEN" | jq -r '.data.items[0].id')

curl -sX POST "http://localhost:8080/api/v1/conversations/$CONV/messages" \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"body":"Привет! Как проходит день?"}' | jq
```

## 5. Real-time

```bash
wscat -c "ws://localhost:8080/api/v1/ws?token=$TOKEN"
> {"type":"typing","data":{"conversation_id":"<id>"}}
> {"type":"ping"}
```

Open a second session as Анна to see `message.new`, `typing` and `read` events in
both directions.

## 6. Upload a photo

```bash
curl -sX POST http://localhost:8080/api/v1/profiles/me/photos \
  -H "Authorization: Bearer $TOKEN" -F 'file=@photo.jpg' | jq
```

The returned URL points at MinIO (`http://localhost:9000/love-media/...`). Open
it in a browser — the bucket policy allows public reads for demo purposes.

## 7. Moderation

```bash
ADMIN=$(curl -sX POST http://localhost:8080/api/v1/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"email":"admin@loveservice.app","password":"AdminDemo123!"}' \
  | jq -r '.data.tokens.access_token')

curl -s http://localhost:8080/api/v1/admin/stats -H "Authorization: Bearer $ADMIN" | jq
curl -s "http://localhost:8080/api/v1/admin/users?search=demo" -H "Authorization: Bearer $ADMIN" | jq
```

## 8. Lawful-access workflow ⚠️

The technical administrator registers the legal basis before touching data.

```bash
# Create request
REQ=$(curl -sX POST http://localhost:8080/api/v1/admin/legal-requests \
  -H "Authorization: Bearer $ADMIN" -H 'Content-Type: application/json' \
  -d '{
    "case_number":"CASE-2026-000123",
    "authority":"National Police / Cybercrime Unit",
    "country":"RU",
    "legal_basis":"Court order #1234 dated 2026-01-10",
    "target_user_id":"'$TARGET'",
    "scopes":["account","profile","media","matches","messages"],
    "reason":"Formal written request received"
  }' | jq -r '.data.id')

# Approve (second admin when ADMIN_REQUIRE_SECOND_APPROVAL=true)
curl -sX POST "http://localhost:8080/api/v1/admin/legal-requests/$REQ/approve" \
  -H "Authorization: Bearer $ADMIN2" -H 'Content-Type: application/json' \
  -d '{"reason":"Legal basis verified"}' | jq '.data.status'

# Export
curl -sX POST "http://localhost:8080/api/v1/admin/legal-requests/$REQ/export" \
  -H "Authorization: Bearer $ADMIN" -H 'Content-Type: application/json' \
  -d '{"reason":"Disclosure prepared"}' | jq '{sha256:.data.sha256, status:.data.request.status}'

# Audit trail
curl -s http://localhost:8080/api/v1/admin/audit-logs -H "Authorization: Bearer $ADMIN" | jq '.data.items[0:5]'
```

> Demo stack sets `ADMIN_REQUIRE_SECOND_APPROVAL=true`. For a one-person demo,
> either create a second admin account, temporarily set the variable to `false`,
> or promote another seeded user via `PATCH /admin/users/{id}/role`.

## 9. Personal data export

```bash
curl -s http://localhost:8080/api/v1/account/export -H "Authorization: Bearer $TOKEN" | jq '.data | keys'
```

## 10. Who liked me, Premium and gifts

```bash
# Free users only see the counter
curl -s http://localhost:8080/api/v1/discovery/likes -H "Authorization: Bearer $TOKEN" | jq '.data'

# Reveal for 24 hours with 100 coins (top up first)
curl -sX POST http://localhost:8080/api/v1/wallet/topup \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"coins":500,"reference":"demo"}' | jq '.data.coins'
curl -sX POST http://localhost:8080/api/v1/discovery/likes/unlock -H "Authorization: Bearer $TOKEN" | jq
curl -s http://localhost:8080/api/v1/discovery/likes -H "Authorization: Bearer $TOKEN" | jq '.data | {count, premium_required}'

# Send a rose (10 coins)
curl -sX POST "http://localhost:8080/api/v1/chat/$CONV/gifts" \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"gift_code":"rose"}' | jq '.data | {gift, sender_coins}'
```

## 11. Top picks, stats and face verification

```bash
curl -s http://localhost:8080/api/v1/recommendations/top-picks -H "Authorization: Bearer $TOKEN" | jq '.data.items | map({rank, compatibility_score})'
curl -s http://localhost:8080/api/v1/profile/stats -H "Authorization: Bearer $TOKEN" | jq '.data'

# Multipart selfie; the dev provider auto-approves
curl -sX POST http://localhost:8080/api/v1/verification/face \
  -H "Authorization: Bearer $TOKEN" -F 'file=@photo.jpg' | jq '.data | {status, score, provider}'
```

## 12. Device sessions

```bash
curl -s http://localhost:8080/api/v1/sessions -H "Authorization: Bearer $TOKEN" | jq '.data.items[] | {id, platform, device_model, ip, created_at, current}'
```

`DELETE /api/v1/sessions/{id}` logs out one device; `POST /api/v1/auth/logout-all`
logs out everywhere.

## Postman

Import `docs/postman_collection.json`. Run **Auth → Login (demo)** first; the
test script stores the tokens in collection variables. The **Premium & Gifts**
folder covers monetization, and the **Technical Admin / Lawful Access** folder is
labelled with the lawful-access warning.
