-- Administrative access, audit trail and legal (law-enforcement) requests.
-- The audit log is append-only by application policy.

CREATE TABLE IF NOT EXISTS admin_audit_logs (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    admin_id    uuid REFERENCES users(id) ON DELETE SET NULL,
    action      text NOT NULL,
    target_type text NOT NULL DEFAULT '',
    target_id   text NOT NULL DEFAULT '',
    reason      text NOT NULL DEFAULT '',
    legal_basis text NOT NULL DEFAULT '',
    request_id  text NOT NULL DEFAULT '',
    ip          text NOT NULL DEFAULT '',
    user_agent  text NOT NULL DEFAULT '',
    metadata    jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS admin_audit_logs_admin_idx ON admin_audit_logs (admin_id, created_at DESC);
CREATE INDEX IF NOT EXISTS admin_audit_logs_target_idx ON admin_audit_logs (target_type, target_id, created_at DESC);

CREATE TABLE IF NOT EXISTS legal_requests (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    created_by      uuid REFERENCES users(id) ON DELETE SET NULL,
    approved_by     uuid REFERENCES users(id) ON DELETE SET NULL,
    case_number     text NOT NULL,
    authority       text NOT NULL,
    country         text NOT NULL,
    legal_basis     text NOT NULL,
    target_user_id  uuid REFERENCES users(id) ON DELETE SET NULL,
    scopes          text[] NOT NULL DEFAULT '{}',
    status          text NOT NULL DEFAULT 'pending',
    export_sha256   text NOT NULL DEFAULT '',
    created_at      timestamptz NOT NULL DEFAULT now(),
    approved_at     timestamptz,
    completed_at    timestamptz,
    CONSTRAINT legal_requests_status_check CHECK (status IN ('pending', 'approved', 'exported', 'rejected'))
);
CREATE INDEX IF NOT EXISTS legal_requests_target_idx ON legal_requests (target_user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS legal_requests_status_idx ON legal_requests (status, created_at DESC);
