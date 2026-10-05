package client

import (
	"context"
	"io"
	"log"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"rfog/engine"
	"rfog/server"
	"rfog/server/store"
)

func newM3Server(t *testing.T, st store.Store, draftSeconds int) (*server.Server, context.CancelFunc) {
	t.Helper()
	c := testApp(t).c
	srv := server.New(c, st, server.Config{
		TimeControls: map[string]server.TimeControl{
			"blitz":  {Turn: 30 * time.Second, Ranked: true},
			"daily":  {Turn: time.Hour, Async: true, Ranked: true},
			"casual": {Turn: 60 * time.Second, Backfill: true, Guests: true},
		},
		DraftSeconds: draftSeconds, BackfillAfter: 10 * time.Millisecond, Logger: log.New(io.Discard, "", 0)})
	ctx, cancel := context.WithCancel(context.Background())
	go srv.Run(ctx)
	return srv, cancel
}

func (h *harness) typeText(s string) {
	for _, r := range s {
		h.send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
}

func (h *harness) keyType(k tea.KeyType) { h.send(tea.KeyMsg{Type: k}) }

// register drives the account form: connect as guest, open the form,
// type a password, choose "register".
func (h *harness) register(t *testing.T, password string) {
	t.Helper()
	h.run(h.a.enter(newOnlineScreen(h.a), nil))
	if !h.pump(h.screenIs("online-menu"), 5*time.Second) {
		t.Fatalf("%s: not connected", h.a.set.Name)
	}
	h.key("a") // the account page
	o := h.a.screen.(*onlineScreen)
	if o.state != "account" {
		t.Fatalf("account page: %+v", o)
	}
	h.keyType(tea.KeyDown) // "log in to another account"
	h.key("enter")
	if o.state != "login" || o.form.focus != 1 {
		t.Fatalf("form: %+v", o)
	}
	h.typeText(password)
	h.keyType(tea.KeyDown) // log in
	h.keyType(tea.KeyDown) // register
	h.key("enter")
	// A new account is shown its recovery code once, then the menu.
	if !h.pump(func() bool { o, ok := h.a.screen.(*onlineScreen); return ok && o.state == "recovery" }, 5*time.Second) {
		t.Fatalf("%s: register failed: %+v", h.a.set.Name, h.a.screen)
	}
	checkFrame(t, h.a)
	h.key("enter")
	if !h.pump(h.screenIs("online-menu"), 5*time.Second) {
		t.Fatalf("%s: no menu after the code: %+v", h.a.set.Name, h.a.screen)
	}
	if h.a.net.welcome.Guest || h.a.set.Account != h.a.set.Name || h.a.set.Token == "" {
		t.Fatalf("%s: welcome %+v settings %+v", h.a.set.Name, h.a.net.welcome, h.a.set)
	}
	checkFrame(t, h.a)
}

func TestAccountsDailyHistoryLadder(t *testing.T) {
	st, err := store.Open(context.Background(), "sqlite::memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	srv, cancel := newM3Server(t, st, 1)
	defer cancel()

	ha, hb := newHarness(t, srv, "alice"), newHarness(t, srv, "bob")
	ha.register(t, "secret1")
	hb.register(t, "secret2")

	// Wrong password shows the form again with an error, not a crash.
	hc := newHarness(t, srv, "alice")
	hc.a.set.Account = "alice"
	hc.run(hc.a.enter(newOnlineScreen(hc.a), nil))
	if !hc.pump(func() bool { o, ok := hc.a.screen.(*onlineScreen); return ok && o.state == "login" }, 5*time.Second) {
		t.Fatalf("stale login: %+v", hc.a.screen)
	}
	checkFrame(t, hc.a)

	// Both queue daily 1v1 and draft (async drafts never time out quickly).
	for _, h := range []*harness{ha, hb} {
		for i := 0; i < 3; i++ {
			h.key("j") // blitz -> bullet -> rapid -> daily
		}
		o := h.a.screen.(*onlineScreen)
		if o.modes[o.sel] != "daily" {
			t.Fatalf("selected %s", o.modes[o.sel])
		}
		h.key("enter")
	}
	for _, h := range []*harness{ha, hb} {
		if !h.pump(h.screenIs("draft"), 5*time.Second) {
			t.Fatalf("%s: no draft", h.a.set.Name)
		}
		checkFrame(t, h.a)
	}
	driveDraft(t, []*harness{ha, hb}, 4)
	for _, h := range []*harness{ha, hb} {
		if !h.pump(h.screenIs("match-orders"), 10*time.Second) {
			t.Fatalf("%s: match did not start: %T", h.a.set.Name, h.a.screen)
		}
	}
	// Alice commits, then Q leaves the screen without forfeiting (async).
	ma := ha.a.screen.(*matchScreen)
	ma.simpleOrder(ha.a, engine.ActHold)
	ha.key(" ")
	ha.key("Q")
	ha.key("Q") // the second Q confirms
	if !ha.pump(ha.screenIs("online-menu"), 3*time.Second) {
		t.Fatalf("alice: %T", ha.a.screen)
	}
	// Her live list shows the match waiting on the opponent.
	ha.key("d")
	ok := ha.pump(func() bool {
		l, ok := ha.a.screen.(*liveScreen)
		return ok && len(l.rows) == 1 && !l.rows[0].YourTurn
	}, 3*time.Second)
	if !ok {
		t.Fatalf("alice live: %+v", ha.a.screen)
	}
	checkFrame(t, ha.a)
	// Bob commits: the turn resolves; Bob animates to turn 2.
	mb := hb.a.screen.(*matchScreen)
	mb.simpleOrder(hb.a, engine.ActHold)
	hb.key(" ")
	if !hb.pump(func() bool {
		m, ok := hb.a.screen.(*matchScreen)
		return ok && m.phase == "orders" && m.vs.Match.Turn == 2
	}, 8*time.Second) {
		t.Fatalf("bob: %+v", hb.a.screen)
	}
	// Alice rejoins from the live list and lands on turn 2.
	ha.key("r")
	ha.pump(func() bool { l, ok := ha.a.screen.(*liveScreen); return ok && len(l.rows) == 1 && l.rows[0].YourTurn }, 3*time.Second)
	ha.key("enter")
	if !ha.pump(func() bool {
		m, ok := ha.a.screen.(*matchScreen)
		return ok && m.phase == "orders" && m.vs.Match.Turn == 2
	}, 5*time.Second) {
		t.Fatalf("alice rejoin: %T %+v", ha.a.screen, ha.a.screen)
	}
	checkFrame(t, ha.a)
	// Alice resigns from the command line: Bob wins by forfeit, rated.
	ma = ha.a.screen.(*matchScreen)
	ma.runCommand(ha.a, "resign")
	if !hb.pump(hb.screenIs("match-end"), 5*time.Second) {
		t.Fatalf("bob: %+v", hb.a.screen)
	}
	mb = hb.a.screen.(*matchScreen)
	if mb.final.Match.Result != "forfeit" || len(mb.ratings) != 2 || mb.ratings[mb.player].After <= 1500 {
		t.Fatalf("bob end: %s %+v", mb.final.Match.Result, mb.ratings)
	}
	checkFrame(t, hb.a)
	if !ha.pump(ha.screenIs("match-end"), 5*time.Second) {
		t.Fatalf("alice: %+v", ha.a.screen)
	}
	// Bob: end card -> online menu (rating shown) -> history -> replay playback.
	hb.key("enter")
	if !hb.pump(hb.screenIs("online-menu"), 3*time.Second) {
		t.Fatalf("bob menu: %T", hb.a.screen)
	}
	hb.key("H")
	if !hb.pump(func() bool { h, ok := hb.a.screen.(*historyScreen); return ok && h.loaded && len(h.rows) == 1 }, 5*time.Second) {
		t.Fatalf("bob history: %+v", hb.a.screen)
	}
	checkFrame(t, hb.a)
	hb.key("enter")
	if !hb.pump(func() bool { m, ok := hb.a.screen.(*matchScreen); return ok && m.src.Watching() }, 5*time.Second) {
		t.Fatalf("bob replay: %T", hb.a.screen)
	}
	checkFrame(t, hb.a)
	hb.key("Q")
	if _, ok := hb.a.screen.(*historyScreen); !ok {
		t.Fatalf("after replay: %T", hb.a.screen)
	}
	hb.key("esc")
	if !hb.pump(hb.screenIs("online-menu"), 3*time.Second) {
		t.Fatalf("bob menu: %T", hb.a.screen)
	}
	// Ladder for daily lists both accounts.
	hb.key("L")
	hb.key("h") // blitz -> daily
	if !hb.pump(func() bool { l, ok := hb.a.screen.(*ladderScreen); return ok && l.mode == "daily" && len(l.rows) == 2 }, 5*time.Second) {
		t.Fatalf("ladder: %+v", hb.a.screen)
	}
	checkFrame(t, hb.a)
	// Welcome ratings arrive on reconnect.
	hb.key("esc")
	hb.a.disconnect()
	hb.run(hb.a.enter(newOnlineScreen(hb.a), nil))
	if !hb.pump(hb.screenIs("online-menu"), 5*time.Second) {
		t.Fatalf("bob reconnect: %T", hb.a.screen)
	}
	if r, ok := hb.a.net.welcome.Ratings["daily"]; !ok || r.Wins != 1 {
		t.Fatalf("ratings: %+v", hb.a.net.welcome.Ratings)
	}
	checkFrame(t, hb.a)
}

func TestTeamDraftScreen(t *testing.T) {
	st, err := store.Open(context.Background(), "sqlite::memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	srv, cancel := newM3Server(t, st, 60)
	defer cancel()
	h := newHarness(t, srv, "solo")
	h.run(h.a.enter(newOnlineScreen(h.a), nil))
	if !h.pump(h.screenIs("online-menu"), 5*time.Second) {
		t.Fatal("not connected")
	}
	h.key("k") // blitz -> casual
	h.key("l") // 1v1 -> 2v2
	h.key("enter")
	if !h.pump(func() bool { d, ok := h.a.screen.(*draftScreen); return ok && len(d.st.Heroes) > 0 }, 5*time.Second) {
		t.Fatalf("draft: %T", h.a.screen)
	}
	d := h.a.screen.(*draftScreen)
	if d.rm.mode != "2v2" || len(d.rm.players) != 4 || d.st.Turn != 0 || d.st.Kind != "ban" {
		t.Fatalf("draft state: %+v %+v", d.rm, d.st)
	}
	checkFrame(t, h.a)
	h.key("enter") // ban; the bots ban and pick until it is our pick
	if !h.pump(func() bool { d, ok := h.a.screen.(*draftScreen); return ok && d.st.Kind == "pick" && d.st.Turn == 0 }, 5*time.Second) {
		t.Fatalf("pick: %+v", h.a.screen)
	}
	checkFrame(t, h.a)
	for j := 0; j < 5; j++ { // move off banned heroes (ours and the bot's, which is random)
		d = h.a.screen.(*draftScreen)
		legal := true
		for _, b := range d.st.Banned {
			if b == d.st.Heroes[d.sel] {
				legal = false
			}
		}
		if legal {
			break
		}
		h.key("j")
	}
	h.key("enter") // pick; the bots finish and play begins
	if !h.pump(h.screenIs("match-orders"), 10*time.Second) {
		t.Fatalf("match: %T", h.a.screen)
	}
	m := h.a.screen.(*matchScreen)
	if m.vs.Board.W != 24 || len(m.vs.Players) != 4 {
		t.Fatalf("board %d players %d", m.vs.Board.W, len(m.vs.Players))
	}
	own := 0
	for _, u := range m.vs.Units {
		if u.Owner == m.player {
			own++
		}
	}
	if own != 4 { // commander + 3 units in 2v2
		t.Fatalf("own units %d", own)
	}
	checkFrame(t, h.a)
	// The ally's units render but cannot be ordered.
	for _, u := range m.vs.Units {
		if u.Team == m.vs.Player(m.player).Team && u.Owner != m.player {
			m.cur = u.Pos
			m.confirm(h.a)
			if m.sel == u.ID {
				t.Fatal("selected an ally unit")
			}
			break
		}
	}
}
