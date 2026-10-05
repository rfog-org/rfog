-- Accounts that follow the player: a token per device (logging in on a
-- phone no longer signs the desktop out), and a recovery code, stored
-- hashed like a password, for setting a new password without an email.
CREATE TABLE IF NOT EXISTS tokens (
    token     TEXT PRIMARY KEY,
    player_id TEXT NOT NULL REFERENCES players(id),
    created   BIGINT NOT NULL,
    used      BIGINT NOT NULL
);
CREATE INDEX IF NOT EXISTS tokens_player ON tokens(player_id);

-- Every token that worked before keeps working.
INSERT INTO tokens(token, player_id, created, used)
    SELECT token, id, created, last_seen FROM players WHERE token IS NOT NULL AND token <> '';

ALTER TABLE players ADD COLUMN recovery TEXT;
