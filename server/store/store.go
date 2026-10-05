// Package store persists accounts, ratings, matches and live snapshots. One
// interface, two drivers behind one SQL implementation: SQLite (embedded,
// default for self-host) and Postgres (official server).
package store

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strings"
	"time"
)

//go:embed migrations/sqlite/*.sql migrations/postgres/*.sql
var migrations embed.FS

// MaxTokens is how many devices an account may be signed in on at once.
const MaxTokens = 20

// NameDeleted is the name a deleted player's seats show in past matches.
const NameDeleted = "(deleted)"

// ErrNotFound is returned when a row does not exist.
var ErrNotFound = errors.New("not found")

// ErrExists is returned when a unique name is already taken.
var ErrExists = errors.New("already exists")

type Player struct {
	ID       string
	Name     string
	Guest    bool
	Token    string // session token; reconnects present it
	PassHash string // argon2id encoded hash; empty for guests
	Recovery string // argon2id hash of the recovery code; empty for guests
	Created  time.Time
	LastSeen time.Time
}

// Rating is a Glicko-2 rating for one player in one mode.
type Rating struct {
	Rating float64
	RD     float64
	Vol    float64
	Games  int
	Wins   int
}

// Ranked is one leaderboard row.
type Ranked struct {
	PlayerID string
	Name     string
	Rating   Rating
}

type Match struct {
	ID      string
	Mode    string
	Map     string
	TimeCtl string
	Seed    uint64
	Started time.Time
	Ended   *time.Time
	Winner  *int
	Result  string
	Players []MatchPlayer
}

type MatchPlayer struct {
	PlayerID     string
	Name         string
	Slot         int
	Team         int
	Hero         string
	Bot          bool
	RatingBefore *float64
	RatingAfter  *float64
}

type Snapshot struct {
	MatchID string
	Turn    int
	State   []byte
	Replay  []byte
	Clock   []byte // per-player clocks (server-defined JSON); may be nil
	Updated time.Time
}

// Store is what the server needs. Every call is safe for concurrent use.
type Store interface {
	CreateGuest(ctx context.Context, name string) (Player, error)
	// CreateAccount registers a named account; ErrExists if the name is taken.
	CreateAccount(ctx context.Context, name, passHash string) (Player, error)
	// PlayerByName finds an account (never a guest) by case-insensitive name.
	PlayerByName(ctx context.Context, name string) (Player, error)
	GetPlayer(ctx context.Context, id string) (Player, error)
	PlayerByToken(ctx context.Context, token string) (Player, error)
	// AddToken signs a device in: one more token for the player (the
	// oldest beyond MaxTokens stop working). DropToken signs one device
	// out, DropTokens every device.
	AddToken(ctx context.Context, playerID, token string) error
	DropToken(ctx context.Context, playerID, token string) error
	DropTokens(ctx context.Context, playerID string) error
	// UpgradeGuest turns a guest into an account, keeping its id and so its
	// history and ratings: ErrExists if the name is taken, ErrNotFound if
	// the player is not a guest.
	UpgradeGuest(ctx context.Context, id, name, passHash, recovery string) error
	SetPassword(ctx context.Context, id, passHash string) error
	SetRecovery(ctx context.Context, id, recovery string) error
	// DeletePlayer removes a player: tokens, ratings and the row. Past
	// matches keep a seat named NameDeleted; replays are the caller's to
	// rewrite (SetReplay), since the store does not read them.
	DeletePlayer(ctx context.Context, id string) error
	SetReplay(ctx context.Context, matchID string, replay []byte) error
	Touch(ctx context.Context, id string) error

	// Rating returns ErrNotFound for an unrated player in that mode.
	Rating(ctx context.Context, playerID, mode string) (Rating, error)
	Ratings(ctx context.Context, playerID string) (map[string]Rating, error)
	SetRating(ctx context.Context, playerID, mode string, r Rating) error
	Leaderboard(ctx context.Context, mode string, limit int) ([]Ranked, error)

	CreateMatch(ctx context.Context, m Match) error
	EndMatch(ctx context.Context, id string, winner int, result string, replay []byte) error
	SetMatchRating(ctx context.Context, matchID string, slot int, before, after float64) error
	GetMatch(ctx context.Context, id string) (Match, error)
	// Replay returns the stored replay of an ended match.
	Replay(ctx context.Context, matchID string) ([]byte, error)
	SaveSnapshot(ctx context.Context, s Snapshot) error
	DeleteSnapshot(ctx context.Context, matchID string) error
	// LiveMatches returns snapshots of matches that never ended (crash recovery).
	LiveMatches(ctx context.Context) ([]Snapshot, []Match, error)
	History(ctx context.Context, playerID string, limit int) ([]Match, error)

	Close() error
}

// Open picks a driver from a URL: sqlite://path, sqlite::memory:, or
// postgres://user:pass@host/db.
func Open(ctx context.Context, url string) (Store, error) {
	switch {
	case strings.HasPrefix(url, "sqlite://"):
		return openSQLite(ctx, strings.TrimPrefix(url, "sqlite://"))
	case url == "" || url == "sqlite::memory:":
		return openSQLite(ctx, ":memory:")
	case strings.HasPrefix(url, "postgres://"), strings.HasPrefix(url, "postgresql://"):
		return openPostgres(ctx, url)
	}
	return nil, fmt.Errorf("unknown store url %q", url)
}

// migrate applies the dialect's embedded migrations in order, tracking the
// applied version in schema_version so non-idempotent statements (ALTER
// TABLE ADD COLUMN on SQLite) run once.
func migrate(ctx context.Context, db *sql.DB, dialect string) error {
	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_version (version INTEGER NOT NULL)`); err != nil {
		return err
	}
	var cur int
	var n sql.NullInt64
	if err := db.QueryRowContext(ctx, `SELECT MAX(version) FROM schema_version`).Scan(&n); err != nil {
		return err
	}
	if n.Valid {
		cur = int(n.Int64)
	}
	names, err := fs.Glob(migrations, "migrations/"+dialect+"/*.sql")
	if err != nil {
		return err
	}
	sort.Strings(names)
	for i, name := range names {
		v := i + 1
		if v <= cur {
			continue
		}
		b, err := migrations.ReadFile(name)
		if err != nil {
			return err
		}
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		// One statement per Exec: pgx's extended protocol rejects batches.
		for _, stmt := range strings.Split(string(b), ";") {
			if strings.TrimSpace(stripComments(stmt)) == "" {
				continue
			}
			if _, err := tx.ExecContext(ctx, stmt); err != nil {
				tx.Rollback()
				return fmt.Errorf("%s: %w", name, err)
			}
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO schema_version(version) VALUES(`+fmt.Sprint(v)+`)`); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

// stripComments drops -- line comments so a trailing comment is not run as
// an empty statement.
func stripComments(stmt string) string {
	var out []string
	for _, line := range strings.Split(stmt, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "--") {
			continue
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}
