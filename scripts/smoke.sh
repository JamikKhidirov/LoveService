#!/usr/bin/env sh
# LoveService end-to-end smoke test.
# Usage: BASE_URL=http://localhost:8080 sh scripts/smoke.sh
set -eu

BASE_URL="${BASE_URL:-http://localhost:8080}"
BASE_URL="${BASE_URL%/}"

say() { printf '%s\n' "$*"; }
fail() { say "ASSERT FAILED: $*"; exit 1; }

say "== health / docs =="
for path in /health /ready /docs /openapi.yaml; do
  code=$(curl -s -o /dev/null -w '%{http_code}' "$BASE_URL$path")
  [ "$code" = "200" ] || fail "$path returned $code"
  say "  OK $path"
done

say "== demo login =="
LOGIN=$(curl -s -X POST "$BASE_URL/api/v1/auth/login" \
  -H 'Content-Type: application/json' \
  -d '{"email":"demo@loveservice.app","password":"LoveDemo123!"}')
TOKEN=$(printf '%s' "$LOGIN" | sed -n 's/.*"access_token":"\([^"]*\)".*/\1/p')
[ -n "$TOKEN" ] || fail "no access token: $LOGIN"
say "  OK token acquired"

say "== profile + location =="
curl -s -X POST "$BASE_URL/api/v1/profiles/me/location" \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"lat":55.7558,"lng":37.6173}' >/dev/null
curl -s "$BASE_URL/api/v1/auth/me" -H "Authorization: Bearer $TOKEN" >/dev/null
say "  OK profile"

say "== discovery =="
curl -s "$BASE_URL/api/v1/discovery?lat=55.7558&lng=37.6173&radius_km=5&expand=true&limit=10" \
  -H "Authorization: Bearer $TOKEN" >/dev/null
say "  OK discovery"

say "== chat / notifications / export =="
for path in /api/v1/conversations /api/v1/matches /api/v1/notifications /api/v1/swipes/limits /api/v1/account/export; do
  code=$(curl -s -o /dev/null -w '%{http_code}' "$BASE_URL$path" -H "Authorization: Bearer $TOKEN")
  [ "$code" = "200" ] || fail "$path returned $code"
  say "  OK $path"
done

say "== technical admin =="
ADMIN_LOGIN=$(curl -s -X POST "$BASE_URL/api/v1/auth/login" \
  -H 'Content-Type: application/json' \
  -d '{"email":"admin@loveservice.app","password":"AdminDemo123!"}')
ADMIN=$(printf '%s' "$ADMIN_LOGIN" | sed -n 's/.*"access_token":"\([^"]*\)".*/\1/p')
[ -n "$ADMIN" ] || fail "no admin token: $ADMIN_LOGIN"
curl -s "$BASE_URL/api/v1/admin/stats" -H "Authorization: Bearer $ADMIN" >/dev/null
curl -s "$BASE_URL/api/v1/admin/audit-logs?limit=5" -H "Authorization: Bearer $ADMIN" >/dev/null
say "  OK admin + audit log"

say "== integration suite =="
go test -tags=integration ./tests/integration/... -count=1

say ""
say "ALL SMOKE CHECKS PASSED"
