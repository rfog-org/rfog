package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	_ "github.com/jackc/pgx/v5/stdlib"
	sqlite3 "modernc.org/sqlite"
)

// sqlStore is the one implementation; the dialect only changes placeholders,
// the migration set and how unique-violation errors look.
type sqlStore struct {
	db      *sql.DB
	dialect string // sqlite | postgres
}

func openSQLite(ctx context.Context, path string) (Store, error) {
	dsn := path
	if path != ":memory:" {
		dsn = "file:" + path + "?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)"
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	// One connection: SQLite serialises writes anyway and this keeps
	// :memory: databases from vanishing between pool connections.
	db.SetMaxOpenConns(1)
	if err := migrate(ctx, db, "sqlite"); err != nil {
		db.Close()
		return nil, err
	}
	return &sqlStore{db: db, dialect: "sqlite"}, nil
}

func openPostgres(ctx context.Context, url string) (Store, error) {
	db, err := sql.Open("pgx", url)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(8)
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, err
	}
	if err := migrate(ctx, db, "postgres"); err != nil {
		db.Close()
		return nil, err
	}
	return &sqlStore{db: db, dialect: "postgres"}, nil
}

func (s *sqlStore) Close() error { return s.db.Close() }

// q rewrites ? placeholders to $n for Postgres. Queries never contain a
// literal question mark.
func (s *sqlStore) q(query string) string {
	if s.dialect != "postgres" {
		return query
	}
	var b strings.Builder
	n := 0
	for _, r := range query {
		if r == '?' {
			n++
			b.WriteString("$" + strconv.Itoa(n))
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

func (s *sqlStore) exec(ctx context.Context, query string, args ...any) error {
	_, err := s.db.ExecContext(ctx, s.q(query), args...)
	return err
}

func (s *sqlStore) isUnique(err error) bool {
	var pg *pgconn.PgError
	if errors.As(err, &pg) {
		return pg.Code == "23505"
	}
	var se *sqlite3.Error
	if errors.As(err, &se) {
		return se.Code() == 2067 || se.Code() == 1555 // SQLITE_CONSTRAINT_UNIQUE / PRIMARYKEY
	}
	return strings.Contains(strings.ToLower(err.Error()), "unique")
}

func newID(prefix string) string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return prefix + hex.EncodeToString(b[:])
}

// NewToken returns a fresh session token.
func NewToken() string { return newID("t_") + newID("") }

// ---- players --------------------------------------------------------------

func (s *sqlStore) CreateGuest(ctx context.Context, name string) (Player, error) {
	now := time.Now()
	p := Player{ID: newID("g_"), Name: name, Guest: true, Token: NewToken(), Created: now, LastSeen: now}
	err := s.exec(ctx, `INSERT INTO players(id,name,guest,token,created,last_seen) VALUES(?,?,1,?,?,?)`,
		p.ID, p.Name, p.Token, now.Unix(), now.Unix())
	if err == nil {
		err = s.AddToken(ctx, p.ID, p.Token)
	}
	return p, err
}

func (s *sqlStore) CreateAccount(ctx context.Context, name, passHash string) (Player, error) {
	now := time.Now()
	p := Player{ID: newID("p_"), Name: name, PassHash: passHash, Token: NewToken(), Created: now, LastSeen: now}
	err := s.exec(ctx, `INSERT INTO players(id,name,guest,pass_hash,token,created,last_seen) VALUES(?,?,0,?,?,?,?)`,
		p.ID, p.Name, p.PassHash, p.Token, now.Unix(), now.Unix())
	if err != nil && s.isUnique(err) {
		return Player{}, ErrExists
	}
	if err == nil {
		err = s.AddToken(ctx, p.ID, p.Token)
	}
	return p, err
}

func (s *sqlStore) PlayerByName(ctx context.Context, name string) (Player, error) {
	return s.player(ctx, `lower(name)=lower(?) AND guest=0`, name)
}

func (s *sqlStore) GetPlayer(ctx context.Context, id string) (Player, error) {
	return s.player(ctx, `id=?`, id)
}

func (s *sqlStore) PlayerByToken(ctx context.Context, token string) (Player, error) {
	if token == "" {
		return Player{}, ErrNotFound
	}
	p, err := s.player(ctx, `id=(SELECT player_id FROM tokens WHERE token=?)`, token)
	if err == nil {
		p.Token = token
		_ = s.exec(ctx, `UPDATE tokens SET used=? WHERE token=?`, time.Now().Unix(), token)
	}
	return p, err
}

func (s *sqlStore) player(ctx context.Context, where string, arg any) (Player, error) {
	var p Player
	var guest int
	var token, hash, rec sql.NullString
	var created, seen int64
	err := s.db.QueryRowContext(ctx, s.q(`SELECT id,name,guest,token,pass_hash,recovery,created,last_seen FROM players WHERE `+where), arg).
		Scan(&p.ID, &p.Name, &guest, &token, &hash, &rec, &created, &seen)
	if errors.Is(err, sql.ErrNoRows) {
		return p, ErrNotFound
	}
	p.Guest = guest == 1
	p.Token = token.String
	p.PassHash = hash.String
	p.Recovery = rec.String
	p.Created, p.LastSeen = time.Unix(created, 0), time.Unix(seen, 0)
	return p, err
}

func (s *sqlStore) AddToken(ctx context.Context, playerID, token string) error {
	now := time.Now().Unix()
	if err := s.exec(ctx, `INSERT INTO tokens(token,player_id,created,used) VALUES(?,?,?,?)`, token, playerID, now, now); err != nil {
		return err
	}
	// Keep the newest MaxTokens: a device not seen for a long while signs in again.
	return s.exec(ctx, `DELETE FROM tokens WHERE player_id=? AND token NOT IN
		(SELECT token FROM tokens WHERE player_id=? ORDER BY used DESC, created DESC LIMIT `+strconv.Itoa(MaxTokens)+`)`, playerID, playerID)
}

func (s *sqlStore) DropToken(ctx context.Context, playerID, token string) error {
	return s.exec(ctx, `DELETE FROM tokens WHERE token=? AND player_id=?`, token, playerID)
}

func (s *sqlStore) DropTokens(ctx context.Context, playerID string) error {
	return s.exec(ctx, `DELETE FROM tokens WHERE player_id=?`, playerID)
}

func (s *sqlStore) UpgradeGuest(ctx context.Context, id, name, passHash, recovery string) error {
	res, err := s.db.ExecContext(ctx, s.q(`UPDATE players SET guest=0, name=?, pass_hash=?, recovery=? WHERE id=? AND guest=1`),
		name, passHash, recovery, id)
	if err != nil {
		if s.isUnique(err) {
			return ErrExists
		}
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *sqlStore) SetPassword(ctx context.Context, id, passHash string) error {
	return s.exec(ctx, `UPDATE players SET pass_hash=? WHERE id=? AND guest=0`, passHash, id)
}

func (s *sqlStore) SetRecovery(ctx context.Context, id, recovery string) error {
	return s.exec(ctx, `UPDATE players SET recovery=? WHERE id=? AND guest=0`, recovery, id)
}

func (s *sqlStore) DeletePlayer(ctx context.Context, id string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	for _, q := range []string{
		`DELETE FROM tokens WHERE player_id=?`,
		`DELETE FROM ratings WHERE player_id=?`,
		`UPDATE match_players SET name='` + NameDeleted + `', player_id='' WHERE player_id=?`,
		`DELETE FROM players WHERE id=?`,
	} {
		if _, err := tx.ExecContext(ctx, s.q(q), id); err != nil {
			tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

func (s *sqlStore) SetReplay(ctx context.Context, matchID string, replay []byte) error {
	return s.exec(ctx, `UPDATE matches SET replay=? WHERE id=?`, replay, matchID)
}

func (s *sqlStore) Touch(ctx context.Context, id string) error {
	return s.exec(ctx, `UPDATE players SET last_seen=? WHERE id=?`, time.Now().Unix(), id)
}

// ---- ratings --------------------------------------------------------------

func (s *sqlStore) Rating(ctx context.Context, playerID, mode string) (Rating, error) {
	var r Rating
	err := s.db.QueryRowContext(ctx, s.q(`SELECT rating,rd,vol,games,wins FROM ratings WHERE player_id=? AND mode=?`), playerID, mode).
		Scan(&r.Rating, &r.RD, &r.Vol, &r.Games, &r.Wins)
	if errors.Is(err, sql.ErrNoRows) {
		return r, ErrNotFound
	}
	return r, err
}

func (s *sqlStore) Ratings(ctx context.Context, playerID string) (map[string]Rating, error) {
	rows, err := s.db.QueryContext(ctx, s.q(`SELECT mode,rating,rd,vol,games,wins FROM ratings WHERE player_id=?`), playerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]Rating{}
	for rows.Next() {
		var mode string
		var r Rating
		if err := rows.Scan(&mode, &r.Rating, &r.RD, &r.Vol, &r.Games, &r.Wins); err != nil {
			return nil, err
		}
		out[mode] = r
	}
	return out, rows.Err()
}

func (s *sqlStore) SetRating(ctx context.Context, playerID, mode string, r Rating) error {
	return s.exec(ctx, `INSERT INTO ratings(player_id,mode,rating,rd,vol,games,wins,updated) VALUES(?,?,?,?,?,?,?,?)
		ON CONFLICT(player_id,mode) DO UPDATE SET rating=excluded.rating,rd=excluded.rd,vol=excluded.vol,games=excluded.games,wins=excluded.wins,updated=excluded.updated`,
		playerID, mode, r.Rating, r.RD, r.Vol, r.Games, r.Wins, time.Now().Unix())
}

func (s *sqlStore) Leaderboard(ctx context.Context, mode string, limit int) ([]Ranked, error) {
	rows, err := s.db.QueryContext(ctx, s.q(`SELECT r.player_id,p.name,r.rating,r.rd,r.vol,r.games,r.wins FROM ratings r JOIN players p ON p.id=r.player_id
		WHERE r.mode=? ORDER BY r.rating DESC LIMIT ?`), mode, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Ranked
	for rows.Next() {
		var k Ranked
		if err := rows.Scan(&k.PlayerID, &k.Name, &k.Rating.Rating, &k.Rating.RD, &k.Rating.Vol, &k.Rating.Games, &k.Rating.Wins); err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

// ---- matches --------------------------------------------------------------

func (s *sqlStore) CreateMatch(ctx context.Context, m Match) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, s.q(`INSERT INTO matches(id,mode,map,time_ctl,seed,started) VALUES(?,?,?,?,?,?)`),
		m.ID, m.Mode, m.Map, m.TimeCtl, int64(m.Seed), m.Started.Unix()); err != nil {
		return err
	}
	for _, p := range m.Players {
		bot := 0
		if p.Bot {
			bot = 1
		}
		if _, err := tx.ExecContext(ctx, s.q(`INSERT INTO match_players(match_id,player_id,name,slot,team,hero,bot,rating_before) VALUES(?,?,?,?,?,?,?,?)`),
			m.ID, p.PlayerID, p.Name, p.Slot, p.Team, p.Hero, bot, p.RatingBefore); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *sqlStore) EndMatch(ctx context.Context, id string, winner int, result string, replay []byte) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, s.q(`UPDATE matches SET ended=?,winner=?,result=?,replay=? WHERE id=?`),
		time.Now().Unix(), winner, result, replay, id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, s.q(`DELETE FROM snapshots WHERE match_id=?`), id); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *sqlStore) SetMatchRating(ctx context.Context, matchID string, slot int, before, after float64) error {
	return s.exec(ctx, `UPDATE match_players SET rating_before=?,rating_after=? WHERE match_id=? AND slot=?`, before, after, matchID, slot)
}

func (s *sqlStore) Replay(ctx context.Context, matchID string) ([]byte, error) {
	var b []byte
	err := s.db.QueryRowContext(ctx, s.q(`SELECT replay FROM matches WHERE id=? AND ended IS NOT NULL`), matchID).Scan(&b)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && len(b) == 0) {
		return nil, ErrNotFound
	}
	return b, err
}

func (s *sqlStore) SaveSnapshot(ctx context.Context, sn Snapshot) error {
	return s.exec(ctx, `INSERT INTO snapshots(match_id,turn,state,replay,clock,updated) VALUES(?,?,?,?,?,?)
		ON CONFLICT(match_id) DO UPDATE SET turn=excluded.turn,state=excluded.state,replay=excluded.replay,clock=excluded.clock,updated=excluded.updated`,
		sn.MatchID, sn.Turn, sn.State, sn.Replay, sn.Clock, time.Now().Unix())
}

func (s *sqlStore) DeleteSnapshot(ctx context.Context, matchID string) error {
	return s.exec(ctx, `DELETE FROM snapshots WHERE match_id=?`, matchID)
}

func (s *sqlStore) LiveMatches(ctx context.Context) ([]Snapshot, []Match, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT match_id,turn,state,replay,clock,updated FROM snapshots`)
	if err != nil {
		return nil, nil, err
	}
	var snaps []Snapshot
	for rows.Next() {
		var sn Snapshot
		var upd int64
		if err := rows.Scan(&sn.MatchID, &sn.Turn, &sn.State, &sn.Replay, &sn.Clock, &upd); err != nil {
			rows.Close()
			return nil, nil, err
		}
		sn.Updated = time.Unix(upd, 0)
		snaps = append(snaps, sn)
	}
	rows.Close()
	var matches []Match
	for _, sn := range snaps {
		m, err := s.GetMatch(ctx, sn.MatchID)
		if err != nil {
			return nil, nil, err
		}
		matches = append(matches, m)
	}
	return snaps, matches, nil
}

func (s *sqlStore) GetMatch(ctx context.Context, id string) (Match, error) {
	var m Match
	var seed, started int64
	var ended, winner sql.NullInt64
	var result sql.NullString
	err := s.db.QueryRowContext(ctx, s.q(`SELECT id,mode,map,time_ctl,seed,started,ended,winner,result FROM matches WHERE id=?`), id).
		Scan(&m.ID, &m.Mode, &m.Map, &m.TimeCtl, &seed, &started, &ended, &winner, &result)
	if errors.Is(err, sql.ErrNoRows) {
		return m, ErrNotFound
	}
	if err != nil {
		return m, err
	}
	m.Seed, m.Started, m.Result = uint64(seed), time.Unix(started, 0), result.String
	if ended.Valid {
		t := time.Unix(ended.Int64, 0)
		m.Ended = &t
	}
	if winner.Valid {
		w := int(winner.Int64)
		m.Winner = &w
	}
	rows, err := s.db.QueryContext(ctx, s.q(`SELECT player_id,name,slot,team,hero,bot,rating_before,rating_after FROM match_players WHERE match_id=? ORDER BY slot`), id)
	if err != nil {
		return m, err
	}
	defer rows.Close()
	for rows.Next() {
		var p MatchPlayer
		var bot int
		var before, after sql.NullFloat64
		if err := rows.Scan(&p.PlayerID, &p.Name, &p.Slot, &p.Team, &p.Hero, &bot, &before, &after); err != nil {
			return m, err
		}
		p.Bot = bot == 1
		if before.Valid {
			v := before.Float64
			p.RatingBefore = &v
		}
		if after.Valid {
			v := after.Float64
			p.RatingAfter = &v
		}
		m.Players = append(m.Players, p)
	}
	return m, rows.Err()
}

func (s *sqlStore) History(ctx context.Context, playerID string, limit int) ([]Match, error) {
	rows, err := s.db.QueryContext(ctx, s.q(`SELECT m.id FROM matches m JOIN match_players mp ON mp.match_id=m.id
		WHERE mp.player_id=? AND m.ended IS NOT NULL ORDER BY m.ended DESC, m.id LIMIT ?`), playerID, limit)
	if err != nil {
		return nil, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	var out []Match
	for _, id := range ids {
		m, err := s.GetMatch(ctx, id)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, nil
}
