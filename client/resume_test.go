package client

import (
	"context"
	"io"
	"log"
	"strings"
	"testing"
	"time"

	"rfog/server"
	"rfog/server/store"
)

// Resumable and interchangeable: a match started on one client is shown
// as "in progress" when the game is opened on another, and r rejoins it
// there; the first client is told the game continued elsewhere.
func TestMenuShowsMatchInProgressAndRejoins(t *testing.T) {
	c := testApp(t).c
	st, err := store.Open(context.Background(), "sqlite::memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	srv := server.New(c, st, server.Config{
		TimeControls:  map[string]server.TimeControl{"casual": {Turn: 60 * time.Second, Backfill: true, Guests: true}},
		BackfillAfter: 10 * time.Millisecond, DraftSeconds: 60, Logger: log.New(io.Discard, "", 0)})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go srv.Run(ctx)

	// First client: queue casual, a bot fills the other seat, draft, play.
	h1 := newHarness(t, srv, "roamer")
	h1.run(h1.a.enter(newOnlineScreen(h1.a), nil))
	if !h1.pump(h1.screenIs("online-menu"), 5*time.Second) {
		t.Fatal("not connected")
	}
	h1.a.screen.(*onlineScreen).sel = 0 // casual
	h1.key("enter")
	if !h1.pump(h1.screenIs("draft"), 5*time.Second) {
		t.Fatalf("no draft: %T", h1.a.screen)
	}
	driveDraft(t, []*harness{h1}, 2)
	if !h1.pump(h1.screenIs("match-orders"), 10*time.Second) {
		t.Fatalf("match did not start: %T", h1.a.screen)
	}
	token := h1.a.set.Token

	// Second client, same player (same token), opens the game: the menu
	// signs in quietly and says a match is in progress.
	h2 := newHarness(t, srv, "roamer")
	h2.a.set.Token = token
	h2.run(h2.a.enter(newMenuScreen(), nil))
	if !h2.pump(func() bool { return h2.a.net != nil && h2.a.net.welcome.InMatch != "" }, 5*time.Second) {
		t.Fatal("menu did not find the match in progress")
	}
	if v := h2.a.View(); !strings.Contains(v, "match in progress") {
		t.Fatalf("no banner:\n%s", v)
	}
	// The first client hears it continued elsewhere.
	if !h1.pump(func() bool { return h1.a.displaced }, 5*time.Second) {
		t.Fatal("first client not told it was displaced")
	}
	// r rejoins on the second client.
	h2.key("r")
	if !h2.pump(h2.screenIs("match-orders"), 10*time.Second) {
		t.Fatalf("did not rejoin: %T %+v", h2.a.screen, h2.a.screen)
	}
	checkFrame(t, h2.a)
}
