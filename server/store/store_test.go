package store

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"
)

func TestSQLite(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, "sqlite::memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	p, err := s.CreateGuest(ctx, "zach")
	if err != nil {
		t.Fatal(err)
	}
	if got, err := s.GetPlayer(ctx, p.ID); err != nil || got.Name != "zach" || !got.Guest {
		t.Fatalf("get: %+v %v", got, err)
	}
	m := Match{ID: "m1", Mode: "1v1", Map: "relay", TimeCtl: "blitz", Seed: 42, Started: time.Now(),
		Players: []MatchPlayer{{PlayerID: p.ID, Slot: 0, Team: 0, Hero: "hask"}, {PlayerID: "bot", Slot: 1, Team: 1, Hero: "wren", Bot: true}}}
	if err := s.CreateMatch(ctx, m); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveSnapshot(ctx, Snapshot{MatchID: "m1", Turn: 1, State: []byte("{}"), Replay: []byte("{}")}); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveSnapshot(ctx, Snapshot{MatchID: "m1", Turn: 2, State: []byte("{}"), Replay: []byte("{}")}); err != nil {
		t.Fatal(err)
	}
	snaps, ms, err := s.LiveMatches(ctx)
	if err != nil || len(snaps) != 1 || snaps[0].Turn != 2 || len(ms) != 1 || len(ms[0].Players) != 2 {
		t.Fatalf("live: %v %+v %+v", err, snaps, ms)
	}
	if err := s.EndMatch(ctx, "m1", 0, "score", []byte("{}")); err != nil {
		t.Fatal(err)
	}
	snaps, _, _ = s.LiveMatches(ctx)
	if len(snaps) != 0 {
		t.Fatal("snapshot should be gone")
	}
	h, err := s.History(ctx, p.ID, 10)
	if err != nil || len(h) != 1 || h[0].Winner == nil || *h[0].Winner != 0 {
		t.Fatalf("history: %v %+v", err, h)
	}
}

func TestToken(t *testing.T) {
	ctx := context.Background()
	s, _ := Open(ctx, "sqlite::memory:")
	defer s.Close()
	p, _ := s.CreateGuest(ctx, "a")
	got, err := s.PlayerByToken(ctx, p.Token)
	if err != nil || got.ID != p.ID {
		t.Fatalf("%v %+v", err, got)
	}
	if _, err := s.PlayerByToken(ctx, "nope"); err != ErrNotFound {
		t.Fatal(err)
	}
}

func TestAccountsRatingsReplay(t *testing.T) {
	ctx := context.Background()
	s, _ := Open(ctx, "sqlite::memory:")
	defer s.Close()
	testAccountsRatingsReplay(t, ctx, s)
}

func testAccountsRatingsReplay(t *testing.T, ctx context.Context, s Store) {
	t.Helper()
	a, err := s.CreateAccount(ctx, "Alice", "hash")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateAccount(ctx, "alice", "x"); err != ErrExists {
		t.Fatalf("dup name: %v", err)
	}
	if got, err := s.PlayerByName(ctx, "ALICE"); err != nil || got.ID != a.ID || got.PassHash != "hash" || got.Guest {
		t.Fatalf("by name: %+v %v", got, err)
	}
	if _, err := s.PlayerByName(ctx, "nobody"); err != ErrNotFound {
		t.Fatal(err)
	}
	// Guests may share a name with each other.
	if _, err := s.CreateGuest(ctx, "bob"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateGuest(ctx, "bob"); err != nil {
		t.Fatal(err)
	}
	if err := s.AddToken(ctx, a.ID, "tok2"); err != nil {
		t.Fatal(err)
	}
	if got, err := s.PlayerByToken(ctx, "tok2"); err != nil || got.ID != a.ID {
		t.Fatalf("token: %+v %v", got, err)
	}

	if _, err := s.Rating(ctx, a.ID, "blitz"); err != ErrNotFound {
		t.Fatal(err)
	}
	if err := s.SetRating(ctx, a.ID, "blitz", Rating{Rating: 1600, RD: 200, Vol: 0.06, Games: 1, Wins: 1}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetRating(ctx, a.ID, "blitz", Rating{Rating: 1650, RD: 180, Vol: 0.06, Games: 2, Wins: 2}); err != nil {
		t.Fatal(err)
	}
	r, err := s.Rating(ctx, a.ID, "blitz")
	if err != nil || r.Rating != 1650 || r.Games != 2 {
		t.Fatalf("rating: %+v %v", r, err)
	}
	all, _ := s.Ratings(ctx, a.ID)
	if len(all) != 1 {
		t.Fatalf("ratings: %+v", all)
	}
	lb, err := s.Leaderboard(ctx, "blitz", 10)
	if err != nil || len(lb) != 1 || lb[0].Name != "Alice" {
		t.Fatalf("leaderboard: %+v %v", lb, err)
	}

	before := 1600.0
	m := Match{ID: "m2", Mode: "1v1", Map: "relay", TimeCtl: "blitz", Seed: 1, Started: time.Now(),
		Players: []MatchPlayer{{PlayerID: a.ID, Name: "Alice", Slot: 0, Hero: "hask", RatingBefore: &before}, {PlayerID: "bot", Name: "bot", Slot: 1, Team: 1, Hero: "wren", Bot: true}}}
	if err := s.CreateMatch(ctx, m); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveSnapshot(ctx, Snapshot{MatchID: "m2", Turn: 1, State: []byte("{}"), Replay: []byte("{}"), Clock: []byte(`{"a":1}`)}); err != nil {
		t.Fatal(err)
	}
	snaps, _, _ := s.LiveMatches(ctx)
	if len(snaps) != 1 || string(snaps[0].Clock) != `{"a":1}` {
		t.Fatalf("clock: %+v", snaps)
	}
	if _, err := s.Replay(ctx, "m2"); err != ErrNotFound {
		t.Fatalf("replay before end: %v", err)
	}
	if err := s.EndMatch(ctx, "m2", 0, "score", []byte(`{"v":1}`)); err != nil {
		t.Fatal(err)
	}
	if err := s.SetMatchRating(ctx, "m2", 0, 1600, 1650); err != nil {
		t.Fatal(err)
	}
	b, err := s.Replay(ctx, "m2")
	if err != nil || string(b) != `{"v":1}` {
		t.Fatalf("replay: %s %v", b, err)
	}
	h, err := s.History(ctx, a.ID, 10)
	if err != nil || len(h) != 1 || h[0].Players[0].Name != "Alice" || h[0].Players[0].RatingAfter == nil || *h[0].Players[0].RatingAfter != 1650 {
		t.Fatalf("history: %+v %v", h, err)
	}
	if _, err := s.GetMatch(ctx, "nope"); err != ErrNotFound {
		t.Fatal(err)
	}
}

// TestUpgradeFromM2 opens a database created with only the first migration
// (no schema_version table, as M2 servers wrote it) and expects the later
// migrations to apply cleanly.
func TestUpgradeFromM2(t *testing.T) {
	ctx := context.Background()
	path := t.TempDir() + "/old.db"
	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := migrations.ReadFile("migrations/sqlite/0001_init.sql")
	if _, err := db.ExecContext(ctx, string(b)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO players(id,name,guest,token,created,last_seen) VALUES('g_1','old',1,'t',?,?)`, time.Now().Unix(), time.Now().Unix()); err != nil {
		t.Fatal(err)
	}
	db.Close()
	s, err := Open(ctx, "sqlite://"+path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if p, err := s.PlayerByToken(ctx, "t"); err != nil || p.Name != "old" {
		t.Fatalf("old player: %+v %v", p, err)
	}
	if err := s.SetRating(ctx, "g_1", "blitz", Rating{Rating: 1500, RD: 350, Vol: 0.06}); err != nil {
		t.Fatal(err)
	}
	// Reopening must not re-run migrations.
	s.Close()
	s2, err := Open(ctx, "sqlite://"+path)
	if err != nil {
		t.Fatal(err)
	}
	s2.Close()
}

// oldTokensDB writes a database as v0.0.4 left it (migrations 1 to 3) with
// tokens in the clear: one in use, one unused for 100 days.
func oldTokensDB(t *testing.T) string {
	t.Helper()
	ctx := context.Background()
	path := t.TempDir() + "/old.db"
	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := db.ExecContext(ctx, q, args...); err != nil {
			t.Fatal(err)
		}
	}
	for _, f := range []string{"0001_init.sql", "0002_ladder.sql", "0003_accounts.sql"} {
		b, _ := migrations.ReadFile("migrations/sqlite/" + f)
		exec(string(b))
	}
	exec(`CREATE TABLE schema_version (version INTEGER NOT NULL)`)
	exec(`INSERT INTO schema_version(version) VALUES(1),(2),(3)`)
	now, old := time.Now().Unix(), time.Now().Add(-100*24*time.Hour).Unix()
	exec(`INSERT INTO players(id,name,guest,token,created,last_seen) VALUES('p_1','old',0,'t_live',?,?)`, now, now)
	exec(`INSERT INTO tokens(token,player_id,created,used) VALUES('t_live','p_1',?,?),('t_stale','p_1',?,?)`, now, now, old, old)
	return path
}

// cleartext lists the token rows not stored as a hash, and counts the
// players rows still holding a token.
func cleartext(t *testing.T, s Store) (rows int, plain []string, players int) {
	t.Helper()
	db := s.(*sqlStore).db
	r, err := db.Query(`SELECT token, hashed FROM tokens`)
	if err != nil {
		t.Fatal(err)
	}
	for r.Next() {
		var tok string
		var hashed int
		if err := r.Scan(&tok, &hashed); err != nil {
			t.Fatal(err)
		}
		rows++
		if hashed != 1 || len(tok) != 64 {
			plain = append(plain, tok)
		}
	}
	r.Close()
	if err := db.QueryRow(`SELECT COUNT(*) FROM players WHERE token IS NOT NULL`).Scan(&players); err != nil {
		t.Fatal(err)
	}
	return rows, plain, players
}

// A database from before tokens were hashed upgrades in place: the token
// in use still signs in, none is left in the clear, the stale one is gone,
// and upgrading again (on the next start, or over a row an older binary
// wrote in the clear meanwhile) is harmless.
func TestTokenHashUpgrade(t *testing.T) {
	ctx := context.Background()
	path := oldTokensDB(t)
	s, err := Open(ctx, "sqlite://"+path)
	if err != nil {
		t.Fatal(err)
	}
	if p, err := s.PlayerByToken(ctx, "t_live"); err != nil || p.ID != "p_1" || p.Token != "t_live" {
		t.Fatalf("token in use after the upgrade: %+v %v", p, err)
	}
	if _, err := s.PlayerByToken(ctx, "t_stale"); err != ErrNotFound {
		t.Fatalf("stale token: %v", err)
	}
	if rows, plain, players := cleartext(t, s); rows != 1 || len(plain) != 0 || players != 0 {
		t.Fatalf("after the upgrade: %d rows, in the clear %q, %d players with a token", rows, plain, players)
	}
	now := time.Now().Unix()
	if _, err := s.(*sqlStore).db.Exec(`INSERT INTO tokens(token,player_id,created,used) VALUES('t_older','p_1',?,?)`, now, now); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := s.(*sqlStore).upgradeTokens(ctx); err != nil {
			t.Fatal(err)
		}
	}
	s.Close()
	s, err = Open(ctx, "sqlite://"+path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for _, tok := range []string{"t_live", "t_older"} {
		if _, err := s.PlayerByToken(ctx, tok); err != nil {
			t.Fatalf("%s after upgrading again: %v", tok, err)
		}
	}
	if rows, plain, players := cleartext(t, s); rows != 2 || len(plain) != 0 || players != 0 {
		t.Fatalf("after upgrading again: %d rows, in the clear %q, %d players with a token", rows, plain, players)
	}
}

// A token unused for TokenTTL stops working and is deleted, on use or when
// the player signs in elsewhere; one used within it works and is used again.
func TestTokenExpiry(t *testing.T) {
	ctx := context.Background()
	s, _ := Open(ctx, "sqlite::memory:")
	defer s.Close()
	db := s.(*sqlStore).db
	p, err := s.CreateAccount(ctx, "a", "x")
	if err != nil {
		t.Fatal(err)
	}
	used := func(tok string) (n int, at int64) {
		_ = db.QueryRow(`SELECT COUNT(*), COALESCE(MAX(used),0) FROM tokens WHERE token=?`, hashToken(tok)).Scan(&n, &at)
		return n, at
	}
	age := func(tok string, d time.Duration) {
		if _, err := db.Exec(`UPDATE tokens SET used=? WHERE token=?`, time.Now().Add(-d).Unix(), hashToken(tok)); err != nil {
			t.Fatal(err)
		}
	}
	for _, tok := range []string{"fresh", "idle"} {
		if err := s.AddToken(ctx, p.ID, tok); err != nil {
			t.Fatal(err)
		}
	}
	age("fresh", TokenTTL-time.Hour)
	if _, err := s.PlayerByToken(ctx, "fresh"); err != nil {
		t.Fatalf("token inside the limit: %v", err)
	}
	if _, at := used("fresh"); time.Since(time.Unix(at, 0)) > time.Minute {
		t.Fatal("use not recorded")
	}
	age(p.Token, TokenTTL+time.Hour)
	if _, err := s.PlayerByToken(ctx, p.Token); err != ErrNotFound {
		t.Fatalf("expired token: %v", err)
	}
	if n, _ := used(p.Token); n != 0 {
		t.Fatal("expired token kept")
	}
	age("idle", TokenTTL+time.Hour)
	if err := s.AddToken(ctx, p.ID, "phone"); err != nil {
		t.Fatal(err)
	}
	if n, _ := used("idle"); n != 0 {
		t.Fatal("expired token kept after a new sign-in")
	}
}

// TestPostgres runs the same checks against a real Postgres when
// RFOG_TEST_PG (a postgres:// URL) is set; the database must be empty.
func TestPostgres(t *testing.T) {
	url := os.Getenv("RFOG_TEST_PG")
	if url == "" {
		t.Skip("RFOG_TEST_PG not set")
	}
	ctx := context.Background()
	s, err := Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	testAccountsRatingsReplay(t, ctx, s)
}

func TestActivity(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, "sqlite::memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	g, _ := s.CreateGuest(ctx, "g")
	p, err := s.CreateAccount(ctx, "acct", "x")
	if err != nil {
		t.Fatal(err)
	}
	r := 1500.0
	human := Match{ID: "m1", Mode: "1v1", Map: "relay", TimeCtl: "blitz", Started: time.Now(),
		Players: []MatchPlayer{{PlayerID: g.ID, Slot: 0, Hero: "hask", RatingBefore: &r}, {PlayerID: p.ID, Slot: 1, Team: 1, Hero: "wren", RatingBefore: &r}}}
	bots := Match{ID: "m2", Mode: "1v1", Map: "relay", TimeCtl: "blitz", Started: time.Now(),
		Players: []MatchPlayer{{PlayerID: g.ID, Slot: 0, Hero: "hask"}, {PlayerID: "bot", Slot: 1, Team: 1, Hero: "wren", Bot: true}}}
	old := Match{ID: "m3", Mode: "1v1", Map: "relay", TimeCtl: "blitz", Started: time.Now().AddDate(0, 0, -40),
		Players: []MatchPlayer{{PlayerID: g.ID, Slot: 0, Hero: "hask"}}}
	for _, m := range []Match{human, bots, old} {
		if err := s.CreateMatch(ctx, m); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.EndMatch(ctx, "m1", 0, "score", nil); err != nil {
		t.Fatal(err)
	}
	a, err := s.Activity(ctx, time.Now().AddDate(0, 0, -6))
	if err != nil {
		t.Fatal(err)
	}
	if len(a.Days) != 7 {
		t.Fatalf("days: %d", len(a.Days))
	}
	d := a.Days[len(a.Days)-1]
	want := Day{Date: time.Now().UTC().Format("2006-01-02"), Accounts: 1, Guests: 1, Matches: 2, Finished: 1, Humans: 1, Rated: 1, Players: 2}
	if d != want {
		t.Fatalf("today: %+v, want %+v", d, want)
	}
	if a.Accounts != 1 || a.Seen != [3]int{2, 2, 2} {
		t.Fatalf("totals: %+v", a)
	}
}
