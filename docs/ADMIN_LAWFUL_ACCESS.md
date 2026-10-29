# Admin and lawful-access (technical admin) access

![Lawful access workflow](assets/lawful-access.svg)

> **Restricted capability.** These endpoints exist solely to let the platform
> operator comply with valid legal requests from competent authorities. They
> must not be used for private investigation, marketing, or any purpose without a
> lawful basis.

## Roles

| Role | Capabilities |
|---|---|
| `user` | Own data only (+ self-export) |
| `moderator` | Read platform stats, search users, inspect accounts, triage reports |
| `admin` | Everything above + status/role changes, media removal, broadcasts, purge, audit log, **lawful-access requests and exports** |

Roles are embedded in the signed JWT (`role` claim) and enforced by route-group
middleware. Changing a role takes effect on the next access-token refresh
(≤ 15 minutes); a ban revokes all refresh tokens immediately and blocks login.

## Audit trail

Every admin call writes one row to `admin_audit_logs`:

| Field | Meaning |
|---|---|
| `admin_id` | Acting administrator |
| `action` | e.g. `admin.user.view`, `legal.request.export` |
| `target_type`, `target_id` | What was accessed |
| `reason` | Operator-provided justification |
| `legal_basis` | Authority / court order reference |
| `request_id` | Correlates with the HTTP `X-Request-ID` |
| `ip`, `user_agent` | Request origin |
| `metadata` | Scope, status, SHA-256, counts… |
| `created_at` | Timestamp |

The table is append-only by application policy. Restrict direct database access
and ship the rows to immutable storage if your compliance regime requires it.

## Lawful-access workflow

```
create legal request ──▶ pending ──▶ approve (different admin) ──▶ approved
                                                                     │
                                                                     ▼
                                                        export + SHA-256 ──▶ exported
```

1. **Create** `POST /api/v1/admin/legal-requests`
   - `case_number`, `authority`, `country`, `legal_basis` are mandatory.
   - `target_user_id` and the smallest necessary `scopes` are recorded.
   - Scopes: `account`, `profile`, `media`, `matches`, `messages`, `swipes`,
     `notifications`, `reports`.
2. **Approve** `POST /api/v1/admin/legal-requests/{id}/approve`
   - Enabled by `ADMIN_REQUIRE_SECOND_APPROVAL=true` (default).
   - The creator cannot approve their own request.
3. **Export** `POST /api/v1/admin/legal-requests/{id}/export`
   - Only for `approved`/`exported` requests.
   - Returns a JSON package with export metadata and the requested scopes.
   - The package's SHA-256 is stored in `legal_requests.export_sha256` and in the
     audit log, so a disclosed copy can be verified later.
4. **Verify** `GET /api/v1/admin/legal-requests` / `GET /api/v1/admin/audit-logs`.

## Data minimisation

Exports **never** contain:

- password hashes,
- refresh tokens or their hashes,
- email verification / password-reset codes,
- device push tokens (unless explicitly required by a scope in a future version).

They contain only the requested scopes. Use the narrowest scope set that satisfies
the request.

## Recommended operating rules

1. Two administrators minimum; second approval enabled in production.
2. Admin API reachable only from a VPN/SSO gateway or allow-listed networks.
3. Verify the authority and the legal basis out-of-band before approving.
4. Record the case number exactly as received.
5. Store exported packages in an access-controlled evidence store.
6. Retain audit logs and legal requests per statutory retention; do not delete
   them on the normal user-data schedule.
7. Review audit logs periodically for unusual access patterns.
8. Publish a law-enforcement request policy and a transparency report if required
   by your jurisdiction.

## Swagger / Postman labelling

- Swagger UI groups these operations under the **TechnicalAdmin** tag with the
  description “Lawful access only”.
- Each operation carries the warning description.
- The Postman collection folder is named
  **“Technical Admin / Lawful Access”** with a restricted-use description and the
  warning on each request.

## Endpoint summary

| Method | Endpoint | Role |
|---|---|---|
| POST | `/api/v1/admin/legal-requests` | admin |
| GET | `/api/v1/admin/legal-requests` | admin |
| GET | `/api/v1/admin/legal-requests/{id}` | admin |
| POST | `/api/v1/admin/legal-requests/{id}/approve` | admin (different person) |
| POST | `/api/v1/admin/legal-requests/{id}/export` | admin |
| GET | `/api/v1/admin/audit-logs` | admin |
| GET | `/api/v1/account/export` | any authenticated user (own data) |
