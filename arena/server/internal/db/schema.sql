CREATE TABLE IF NOT EXISTS users (
    id          BIGSERIAL PRIMARY KEY,
    name        TEXT NOT NULL UNIQUE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
-- Profile fields filled from the auth provider (reboot01 GraphQL) on login.
ALTER TABLE users ADD COLUMN IF NOT EXISTS email       TEXT;
ALTER TABLE users ADD COLUMN IF NOT EXISTS first_name  TEXT;
ALTER TABLE users ADD COLUMN IF NOT EXISTS last_name   TEXT;
ALTER TABLE users ADD COLUMN IF NOT EXISTS audit_ratio DOUBLE PRECISION;
ALTER TABLE users ADD COLUMN IF NOT EXISTS is_admin    BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE users ADD COLUMN IF NOT EXISTS is_blocked  BOOLEAN NOT NULL DEFAULT false;

-- Admin actions and other notable platform events, newest first in the UI.
CREATE TABLE IF NOT EXISTS audit_log (
    id          BIGSERIAL PRIMARY KEY,
    actor_name  TEXT NOT NULL,
    action      TEXT NOT NULL,
    detail      TEXT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS audit_log_created_idx ON audit_log(created_at DESC);

CREATE TABLE IF NOT EXISTS sessions (
    token       TEXT PRIMARY KEY,
    user_id     BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at  TIMESTAMPTZ NOT NULL
);
CREATE INDEX IF NOT EXISTS sessions_user_idx ON sessions(user_id);

CREATE TABLE IF NOT EXISTS bots (
    id          BIGSERIAL PRIMARY KEY,
    owner_id    BIGINT REFERENCES users(id),
    name        TEXT NOT NULL UNIQUE,
    -- binary | python | go | c | builtin
    language    TEXT NOT NULL,
    -- builtin: path inside the match image (robots/bender);
    -- uploaded: directory under DataDir containing an executable named "run".
    binary_path TEXT NOT NULL,
    binary_hash TEXT,
    -- pending | building | active | failed | inactive
    status      TEXT NOT NULL DEFAULT 'pending',
    build_log   TEXT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS maps (
    id      BIGSERIAL PRIMARY KEY,
    name    TEXT NOT NULL UNIQUE,
    -- path inside the match image, e.g. maps/map01
    path    TEXT NOT NULL,
    width   INT NOT NULL,
    height  INT NOT NULL
);

CREATE TABLE IF NOT EXISTS matches (
    id          BIGSERIAL PRIMARY KEY,
    bot_a_id    BIGINT NOT NULL REFERENCES bots(id),
    bot_b_id    BIGINT NOT NULL REFERENCES bots(id),
    map_id      BIGINT NOT NULL REFERENCES maps(id),
    -- queued | running | finished | error
    status      TEXT NOT NULL DEFAULT 'queued',
    winner_id   BIGINT REFERENCES bots(id),
    score_a     INT,
    score_b     INT,
    error       TEXT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    started_at  TIMESTAMPTZ,
    finished_at TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS matches_bot_a_idx ON matches(bot_a_id);
CREATE INDEX IF NOT EXISTS matches_bot_b_idx ON matches(bot_b_id);
CREATE INDEX IF NOT EXISTS matches_status_idx ON matches(status);

-- One row per half-turn: the engine alternates players, printing the board,
-- the piece given, and the move answered.
CREATE TABLE IF NOT EXISTS match_turns (
    match_id    BIGINT NOT NULL REFERENCES matches(id) ON DELETE CASCADE,
    turn_number INT NOT NULL,
    player      SMALLINT NOT NULL,       -- 1 or 2
    anfield     TEXT NOT NULL,           -- grid rows joined by \n, prefixes stripped
    piece       TEXT NOT NULL,           -- piece rows joined by \n
    move_x      INT NOT NULL,
    move_y      INT NOT NULL,
    PRIMARY KEY (match_id, turn_number)
);

CREATE TABLE IF NOT EXISTS tournaments (
    id          BIGSERIAL PRIMARY KEY,
    name        TEXT NOT NULL,
    -- round_robin | single_elim
    format      TEXT NOT NULL,
    -- running | finished | error
    status      TEXT NOT NULL DEFAULT 'running',
    -- NULL = rotate through all maps
    map_id      BIGINT REFERENCES maps(id),
    created_by  BIGINT REFERENCES users(id),
    winner_id   BIGINT REFERENCES bots(id) ON DELETE SET NULL,
    error       TEXT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    finished_at TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS tournament_bots (
    tournament_id BIGINT NOT NULL REFERENCES tournaments(id) ON DELETE CASCADE,
    bot_id        BIGINT NOT NULL REFERENCES bots(id) ON DELETE CASCADE,
    -- 1 = strongest at creation time (by rating); drives bracket placement
    seed          INT NOT NULL,
    PRIMARY KEY (tournament_id, bot_id)
);

ALTER TABLE matches ADD COLUMN IF NOT EXISTS tournament_id BIGINT REFERENCES tournaments(id) ON DELETE SET NULL;
-- Bracket coordinates for single_elim: winner of (round r, slot s) meets the
-- winner of (r, s XOR 1) in (r+1, s/2). NULL for round-robin matches.
ALTER TABLE matches ADD COLUMN IF NOT EXISTS tournament_round INT;
ALTER TABLE matches ADD COLUMN IF NOT EXISTS tournament_slot INT;
CREATE INDEX IF NOT EXISTS matches_tournament_idx ON matches(tournament_id);

CREATE TABLE IF NOT EXISTS rankings (
    bot_id          BIGINT PRIMARY KEY REFERENCES bots(id) ON DELETE CASCADE,
    rating          DOUBLE PRECISION NOT NULL DEFAULT 1200,
    wins            INT NOT NULL DEFAULT 0,
    losses          INT NOT NULL DEFAULT 0,
    draws           INT NOT NULL DEFAULT 0,
    matches_played  INT NOT NULL DEFAULT 0
);
