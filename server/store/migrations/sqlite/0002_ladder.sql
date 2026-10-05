-- M3: accounts, ratings, per-seat rating deltas, clocks in snapshots.
CREATE TABLE IF NOT EXISTS ratings (
    player_id TEXT NOT NULL REFERENCES players(id),
    mode      TEXT NOT NULL,
    rating    REAL NOT NULL,
    rd        REAL NOT NULL,
    vol       REAL NOT NULL,
    games     INTEGER NOT NULL DEFAULT 0,
    wins      INTEGER NOT NULL DEFAULT 0,
    updated   INTEGER NOT NULL,
    PRIMARY KEY (player_id, mode)
);
CREATE INDEX IF NOT EXISTS ratings_mode ON ratings(mode, rating DESC);

ALTER TABLE match_players ADD COLUMN name TEXT NOT NULL DEFAULT '';
ALTER TABLE match_players ADD COLUMN rating_before REAL;
ALTER TABLE match_players ADD COLUMN rating_after REAL;

-- Per-player clocks (deadlines, banks) so async matches survive restarts.
ALTER TABLE snapshots ADD COLUMN clock BLOB;
