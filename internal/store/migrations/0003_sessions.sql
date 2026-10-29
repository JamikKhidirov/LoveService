-- Session invalidation support.
-- token_version is embedded in every access token. Incrementing it (logout-all,
-- password change/reset, forced logout) invalidates all previously issued
-- access tokens immediately, without waiting for the 15 minute TTL.

ALTER TABLE users ADD COLUMN IF NOT EXISTS token_version integer NOT NULL DEFAULT 0;
