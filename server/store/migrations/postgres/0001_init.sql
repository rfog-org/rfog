CREATE TABLE IF NOT EXISTS players (
    id         TEXT PRIMARY KEY,
    name       TEXT NOT NULL,
    guest      INTEGER NOT NULL DEFAULT 1,
    pass_hash  TEXT,
    token      TEXT,
    created    BIGINT NOT NULL,
    last_seen  BIGINT NOT NULL
);
CREATE UNIQUE INDEX IF NOT EXISTS players_token ON players(token);
CREATE UNIQUE INDEX IF NOT EXISTS players_name ON players(lower(name)) WHERE guest = 0;

CREATE TABLE IF NOT EXISTS matches (
    id        TEXT PRIMARY KEY,
    mode      TEXT NOT NULL,
    map       TEXT NOT NULL,
    time_ctl  TEXT NOT NULL,
    seed      BIGINT NOT NULL,
    started   BIGINT NOT NULL,
    ended     BIGINT,
    winner    INTEGER,
    result    TEXT,
    replay    BYTEA
);

CREATE TABLE IF NOT EXISTS match_players (
    match_id  TEXT NOT NULL REFERENCES matches(id),
    player_id TEXT NOT NULL,
    slot      INTEGER NOT NULL,
    team      INTEGER NOT NULL,
    hero      TEXT NOT NULL,
    bot       INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (match_id, slot)
);
CREATE INDEX IF NOT EXISTS match_players_player ON match_players(player_id);

CREATE TABLE IF NOT EXISTS snapshots (
    match_id  TEXT PRIMARY KEY REFERENCES matches(id),
    turn      INTEGER NOT NULL,
    state     BYTEA NOT NULL,
    replay    BYTEA NOT NULL,
    updated   BIGINT NOT NULL
);
