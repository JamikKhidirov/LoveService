-- LoveService initial schema.
-- Postgres 15+ is required. gen_random_uuid() is provided by pgcrypto/core.

CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE IF NOT EXISTS schema_migrations (
    version    text PRIMARY KEY,
    applied_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS users (
    id                uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    email             text NOT NULL,
    phone             text,
    password_hash     text NOT NULL,
    role              text NOT NULL DEFAULT 'user',
    status            text NOT NULL DEFAULT 'active',
    email_verified_at timestamptz,
    created_at        timestamptz NOT NULL DEFAULT now(),
    updated_at        timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT users_role_check CHECK (role IN ('user', 'moderator', 'admin')),
    CONSTRAINT users_status_check CHECK (status IN ('active', 'deactivated', 'banned', 'deleted'))
);
CREATE UNIQUE INDEX IF NOT EXISTS users_email_lower_uidx ON users (lower(email));
CREATE UNIQUE INDEX IF NOT EXISTS users_phone_uidx ON users (phone) WHERE phone IS NOT NULL;

CREATE TABLE IF NOT EXISTS profiles (
    user_id             uuid PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    display_name        text NOT NULL,
    birth_date          date NOT NULL,
    gender              text NOT NULL,
    bio                 text NOT NULL DEFAULT '',
    city                text NOT NULL DEFAULT '',
    country             text NOT NULL DEFAULT '',
    lat                 double precision,
    lng                 double precision,
    location_updated_at timestamptz,
    height_cm           integer,
    job                 text NOT NULL DEFAULT '',
    education           text NOT NULL DEFAULT '',
    interests           text[] NOT NULL DEFAULT '{}',
    looking_for         text[] NOT NULL DEFAULT '{}',
    is_verified         boolean NOT NULL DEFAULT false,
    last_seen_at        timestamptz,
    created_at          timestamptz NOT NULL DEFAULT now(),
    updated_at          timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT profiles_gender_check CHECK (gender IN ('male', 'female', 'other')),
    CONSTRAINT profiles_lat_check CHECK (lat IS NULL OR (lat >= -90 AND lat <= 90)),
    CONSTRAINT profiles_lng_check CHECK (lng IS NULL OR (lng >= -180 AND lng <= 180)),
    CONSTRAINT profiles_height_check CHECK (height_cm IS NULL OR (height_cm >= 100 AND height_cm <= 250))
);
CREATE INDEX IF NOT EXISTS profiles_geo_idx ON profiles (lat, lng) WHERE lat IS NOT NULL AND lng IS NOT NULL;
CREATE INDEX IF NOT EXISTS profiles_gender_idx ON profiles (gender);

CREATE TABLE IF NOT EXISTS preferences (
    user_id           uuid PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    min_age           integer NOT NULL DEFAULT 18,
    max_age           integer NOT NULL DEFAULT 99,
    max_distance_km   integer NOT NULL DEFAULT 50,
    preferred_genders text[] NOT NULL DEFAULT '{female}',
    global_mode       boolean NOT NULL DEFAULT false,
    updated_at        timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT preferences_age_check CHECK (min_age >= 18 AND max_age <= 120 AND min_age <= max_age),
    CONSTRAINT preferences_distance_check CHECK (max_distance_km BETWEEN 1 AND 20000)
);

CREATE TABLE IF NOT EXISTS user_settings (
    user_id               uuid PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    notifications_enabled boolean NOT NULL DEFAULT true,
    discoverable          boolean NOT NULL DEFAULT true,
    show_distance         boolean NOT NULL DEFAULT true,
    show_online           boolean NOT NULL DEFAULT true,
    language              text NOT NULL DEFAULT 'ru',
    updated_at            timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT user_settings_language_check CHECK (language IN ('ru', 'en', 'es', 'de', 'fr'))
);

CREATE TABLE IF NOT EXISTS photos (
    id                uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id           uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    url               text NOT NULL,
    storage_key       text NOT NULL,
    media_type        text NOT NULL DEFAULT 'photo',
    position          integer NOT NULL DEFAULT 0,
    is_primary        boolean NOT NULL DEFAULT false,
    moderation_status text NOT NULL DEFAULT 'approved',
    created_at        timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT photos_type_check CHECK (media_type IN ('photo', 'video')),
    CONSTRAINT photos_moderation_check CHECK (moderation_status IN ('pending', 'approved', 'rejected'))
);
CREATE INDEX IF NOT EXISTS photos_user_idx ON photos (user_id, position);
CREATE UNIQUE INDEX IF NOT EXISTS photos_one_primary_uidx ON photos (user_id) WHERE is_primary;

CREATE TABLE IF NOT EXISTS swipes (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    swiper_id  uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    target_id  uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    action     text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT swipes_action_check CHECK (action IN ('like', 'dislike', 'superlike')),
    CONSTRAINT swipes_not_self_check CHECK (swiper_id <> target_id),
    CONSTRAINT swipes_unique_pair UNIQUE (swiper_id, target_id)
);
CREATE INDEX IF NOT EXISTS swipes_target_like_idx ON swipes (target_id, created_at DESC) WHERE action IN ('like', 'superlike');
CREATE INDEX IF NOT EXISTS swipes_swiper_idx ON swipes (swiper_id, created_at DESC);

CREATE TABLE IF NOT EXISTS matches (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_a       uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    user_b       uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at   timestamptz NOT NULL DEFAULT now(),
    unmatched_at timestamptz,
    CONSTRAINT matches_order_check CHECK (user_a < user_b),
    CONSTRAINT matches_unique_pair UNIQUE (user_a, user_b)
);
CREATE INDEX IF NOT EXISTS matches_user_a_idx ON matches (user_a);
CREATE INDEX IF NOT EXISTS matches_user_b_idx ON matches (user_b);

CREATE TABLE IF NOT EXISTS conversations (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    match_id        uuid NOT NULL UNIQUE REFERENCES matches(id) ON DELETE CASCADE,
    created_at      timestamptz NOT NULL DEFAULT now(),
    last_message_at timestamptz
);

CREATE TABLE IF NOT EXISTS messages (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    conversation_id uuid NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
    sender_id       uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    body            text NOT NULL DEFAULT '',
    type            text NOT NULL DEFAULT 'text',
    media_url       text,
    read_at         timestamptz,
    created_at      timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT messages_type_check CHECK (type IN ('text', 'image', 'video', 'system')),
    CONSTRAINT messages_body_check CHECK (char_length(body) <= 4000)
);
CREATE INDEX IF NOT EXISTS messages_conversation_idx ON messages (conversation_id, created_at DESC);
CREATE INDEX IF NOT EXISTS messages_unread_idx ON messages (conversation_id, read_at) WHERE read_at IS NULL;

CREATE TABLE IF NOT EXISTS notifications (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    type       text NOT NULL,
    title      text NOT NULL,
    body       text NOT NULL DEFAULT '',
    data       jsonb NOT NULL DEFAULT '{}'::jsonb,
    read_at    timestamptz,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS notifications_user_idx ON notifications (user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS notifications_unread_idx ON notifications (user_id) WHERE read_at IS NULL;

CREATE TABLE IF NOT EXISTS refresh_tokens (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash text NOT NULL UNIQUE,
    expires_at timestamptz NOT NULL,
    revoked_at timestamptz,
    user_agent text NOT NULL DEFAULT '',
    ip         text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS refresh_tokens_user_idx ON refresh_tokens (user_id);

CREATE TABLE IF NOT EXISTS email_codes (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    email       text NOT NULL,
    purpose     text NOT NULL,
    code_hash   text NOT NULL,
    expires_at  timestamptz NOT NULL,
    consumed_at timestamptz,
    attempts    integer NOT NULL DEFAULT 0,
    created_at  timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT email_codes_purpose_check CHECK (purpose IN ('verify_email', 'reset_password'))
);
CREATE INDEX IF NOT EXISTS email_codes_lookup_idx ON email_codes (email, purpose, created_at DESC);

CREATE TABLE IF NOT EXISTS blocks (
    blocker_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    blocked_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (blocker_id, blocked_id),
    CONSTRAINT blocks_not_self_check CHECK (blocker_id <> blocked_id)
);

CREATE TABLE IF NOT EXISTS reports (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    reporter_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    target_id   uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    reason      text NOT NULL,
    details     text NOT NULL DEFAULT '',
    status      text NOT NULL DEFAULT 'open',
    created_at  timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT reports_status_check CHECK (status IN ('open', 'reviewed', 'resolved', 'dismissed')),
    CONSTRAINT reports_not_self_check CHECK (reporter_id <> target_id)
);

CREATE TABLE IF NOT EXISTS devices (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    platform   text NOT NULL,
    token      text NOT NULL UNIQUE,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT devices_platform_check CHECK (platform IN ('ios', 'android', 'web'))
);

CREATE TABLE IF NOT EXISTS outbox_events (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    topic        text NOT NULL,
    event_type   text NOT NULL,
    aggregate_id text NOT NULL DEFAULT '',
    payload      jsonb NOT NULL,
    created_at   timestamptz NOT NULL DEFAULT now(),
    published_at timestamptz,
    attempts     integer NOT NULL DEFAULT 0,
    last_error   text NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS outbox_pending_idx ON outbox_events (created_at) WHERE published_at IS NULL;
