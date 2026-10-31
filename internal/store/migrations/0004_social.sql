-- Monetization, discovery ranking, device sessions, face verification,
-- profile statistics, top picks and chat gifts.
--
-- Everything in this migration is additive: existing rows keep working and old
-- clients simply ignore the new fields.

ALTER TABLE users ADD COLUMN IF NOT EXISTS premium_until timestamptz;
ALTER TABLE users ADD COLUMN IF NOT EXISTS elo_rating double precision NOT NULL DEFAULT 1000;
ALTER TABLE users ADD COLUMN IF NOT EXISTS coins integer NOT NULL DEFAULT 0;
ALTER TABLE users ADD COLUMN IF NOT EXISTS likes_unlocked_until timestamptz;

-- The ELO movement caused by a swipe is recorded so that rewind can undo it.
ALTER TABLE swipes ADD COLUMN IF NOT EXISTS elo_delta double precision NOT NULL DEFAULT 0;

-- The attractiveness rating is internal: it influences ordering but is not
-- exposed through the public profile payload.
ALTER TABLE users DROP CONSTRAINT IF EXISTS users_elo_rating_check;
ALTER TABLE users ADD CONSTRAINT users_elo_rating_check
    CHECK (elo_rating >= 100 AND elo_rating <= 3000);
ALTER TABLE users DROP CONSTRAINT IF EXISTS users_coins_check;
ALTER TABLE users ADD CONSTRAINT users_coins_check CHECK (coins >= 0);
CREATE INDEX IF NOT EXISTS users_elo_rating_idx ON users (elo_rating DESC);

-- ---------------------------------------------------------------------------
-- Auth sessions (device management)
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS auth_sessions (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id      uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    platform     text NOT NULL DEFAULT 'web',
    device_model text NOT NULL DEFAULT '',
    ip           text NOT NULL DEFAULT '',
    user_agent   text NOT NULL DEFAULT '',
    created_at   timestamptz NOT NULL DEFAULT now(),
    last_seen_at timestamptz NOT NULL DEFAULT now(),
    revoked_at   timestamptz
);
CREATE INDEX IF NOT EXISTS auth_sessions_user_idx ON auth_sessions (user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS auth_sessions_active_idx ON auth_sessions (user_id) WHERE revoked_at IS NULL;

ALTER TABLE refresh_tokens
    ADD COLUMN IF NOT EXISTS session_id uuid REFERENCES auth_sessions(id) ON DELETE SET NULL;
CREATE INDEX IF NOT EXISTS refresh_tokens_session_idx ON refresh_tokens (session_id);

-- ---------------------------------------------------------------------------
-- Face verification
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS face_verifications (
    id                  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id             uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    selfie_url          text NOT NULL,
    selfie_key          text NOT NULL,
    reference_photo_id  uuid REFERENCES photos(id) ON DELETE SET NULL,
    score               double precision NOT NULL DEFAULT 0,
    provider            text NOT NULL DEFAULT 'auto',
    status              text NOT NULL DEFAULT 'pending',
    reason              text NOT NULL DEFAULT '',
    created_at          timestamptz NOT NULL DEFAULT now(),
    reviewed_at         timestamptz,
    CONSTRAINT face_verifications_status_check
        CHECK (status IN ('pending', 'approved', 'rejected', 'manual_review', 'error'))
);
CREATE INDEX IF NOT EXISTS face_verifications_user_idx ON face_verifications (user_id, created_at DESC);

-- ---------------------------------------------------------------------------
-- Profile statistics
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS profile_views (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    viewer_id  uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    target_id  uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    viewed_on  date NOT NULL DEFAULT current_date,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT profile_views_not_self_check CHECK (viewer_id <> target_id),
    CONSTRAINT profile_views_daily_unique UNIQUE (viewer_id, target_id, viewed_on)
);
CREATE INDEX IF NOT EXISTS profile_views_target_idx ON profile_views (target_id, viewed_on DESC);

-- ---------------------------------------------------------------------------
-- Daily top picks
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS top_picks (
    user_id    uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    target_id  uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    for_date   date NOT NULL DEFAULT current_date,
    rank       integer NOT NULL,
    score      double precision NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, for_date, target_id),
    CONSTRAINT top_picks_not_self_check CHECK (user_id <> target_id)
);
CREATE INDEX IF NOT EXISTS top_picks_user_idx ON top_picks (user_id, for_date DESC, rank);

-- ---------------------------------------------------------------------------
-- Virtual gifts and the coin wallet
-- ---------------------------------------------------------------------------
ALTER TABLE messages ADD COLUMN IF NOT EXISTS metadata jsonb NOT NULL DEFAULT '{}'::jsonb;
ALTER TABLE messages DROP CONSTRAINT IF EXISTS messages_type_check;
ALTER TABLE messages ADD CONSTRAINT messages_type_check
    CHECK (type IN ('text', 'image', 'video', 'system', 'gift'));

CREATE TABLE IF NOT EXISTS gift_transactions (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    conversation_id uuid NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
    message_id      uuid REFERENCES messages(id) ON DELETE SET NULL,
    sender_id       uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    recipient_id    uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    gift_code       text NOT NULL,
    gift_name       text NOT NULL,
    coins           integer NOT NULL,
    created_at      timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT gift_transactions_coins_check CHECK (coins > 0)
);
CREATE INDEX IF NOT EXISTS gift_transactions_sender_idx ON gift_transactions (sender_id, created_at DESC);
CREATE INDEX IF NOT EXISTS gift_transactions_recipient_idx ON gift_transactions (recipient_id, created_at DESC);

CREATE TABLE IF NOT EXISTS coin_transactions (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id       uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    amount        integer NOT NULL,
    kind          text NOT NULL,
    reference     text NOT NULL DEFAULT '',
    balance_after integer NOT NULL,
    created_at    timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT coin_transactions_kind_check
        CHECK (kind IN ('topup', 'gift_sent', 'gift_received', 'likes_unlock'))
);
CREATE INDEX IF NOT EXISTS coin_transactions_user_idx ON coin_transactions (user_id, created_at DESC);
