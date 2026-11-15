-- Tokens are stored as a SHA-256 hash, so a copy of the database signs
-- nobody in. hashed marks a converted row. Rows in the clear (0) are
-- hashed in Go when the store opens (upgradeTokens), as on SQLite.
ALTER TABLE tokens ADD COLUMN IF NOT EXISTS hashed INTEGER NOT NULL DEFAULT 0;
