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
	if _, err := db.ExecContext(ctx, `INSERT INTO players(id,name,guest,token,created,last_seen) VALUES('g_1','old',1,'t',0,0)`); err != nil {
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
