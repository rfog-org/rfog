CREATE TABLE IF NOT EXISTS players (
    id         TEXT PRIMARY KEY,
    name       TEXT NOT NULL,
    guest      INTEGER NOT NULL DEFAULT 1,
    pass_hash  TEXT,
    token      TEXT,
    created    INTEGER NOT NULL,
    last_seen  INTEGER NOT NULL
);
CREATE UNIQUE INDEX IF NOT EXISTS players_token ON players(token);
CREATE UNIQUE INDEX IF NOT EXISTS players_name ON players(name COLLATE NOCASE) WHERE guest = 0;

CREATE TABLE IF NOT EXISTS matches (
    id        TEXT PRIMARY KEY,
    mode      TEXT NOT NULL,
    map       TEXT NOT NULL,
    time_ctl  TEXT NOT NULL,
    seed      INTEGER NOT NULL,
    started   INTEGER NOT NULL,
    ended     INTEGER,
    winner    INTEGER,
    result    TEXT,
    replay    BLOB
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

-- Live snapshots for crash recovery: one row per in-progress match,
-- replaced every turn and deleted at match end.
CREATE TABLE IF NOT EXISTS snapshots (
    match_id  TEXT PRIMARY KEY REFERENCES matches(id),
    turn      INTEGER NOT NULL,
    state     BLOB NOT NULL,
    replay    BLOB NOT NULL,
    updated   INTEGER NOT NULL
);
