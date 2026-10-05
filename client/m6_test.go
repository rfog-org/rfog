package client

import (
	"context"
	"strings"
	"testing"
	"time"

	"rfog/engine"
	"rfog/proto"
	"rfog/server/store"
)

// TestTutorialCoaches walks the guided match: each lesson must clear when
// the player does the thing it asks for, and the match itself must play.
func TestTutorialCoaches(t *testing.T) {
	a := testApp(t)
	s, err := newTutorial(a)
	if err != nil {
		t.Fatal(err)
	}
	a.screen = s
	m := s.(*matchScreen)
	if m.coach == nil || m.coach.step != 0 {
		t.Fatal("no coach")
	}
	checkFrame(t, a)
	if !strings.Contains(stripEscapes(a.View()), "YOUR SQUAD") {
		t.Fatalf("first lesson missing:\n%s", stripEscapes(a.View()))
	}
	// Lesson 1: select a unit.
	u := m.myUnits()[0]
	m.cur = u.Pos
	m.confirm(a)
	m.coach.advance(m)
	if m.coach.step != 1 {
		t.Fatalf("step %d after selecting", m.coach.step)
	}
	// Lesson 2: plan a move.
	m.startMove(a)
	if m.mode != "move" {
		t.Fatal("move mode")
	}
	for p := range m.reach {
		m.cur = p
		break
	}
	m.confirm(a)
	m.coach.advance(m)
	if m.coach.step != 2 {
		t.Fatalf("step %d after moving", m.coach.step)
	}
	checkFrame(t, a)
	// Lesson 3: commit the turn.
	press(a, " ")
	for i := 0; i < 40 && m.phase == "anim"; i++ {
		ticks(a, 1)
	}
	press(a, ".", "enter")
	m.coach.advance(m)
	if m.coach.step < 3 {
		t.Fatalf("step %d after the first turn", m.coach.step)
	}
	// Play on to the end of the coached turns; the panel must finish.
	for turn := 0; turn < 6 && m.phase != "end"; turn++ {
		for _, u := range m.myUnits() {
			m.sel, m.cur = u.ID, u.Pos
			m.simpleOrder(a, engine.ActHold)
		}
		press(a, " ")
		for i := 0; i < 40 && m.phase == "anim"; i++ {
			ticks(a, 1)
		}
		press(a, ".", "enter")
		checkFrame(t, a)
	}
	// (Holding every turn can lose before the last lesson: that is fine.)
	if !m.coach.done && m.phase != "end" {
		t.Fatalf("coach still on step %d at turn %d", m.coach.step, m.vs.Match.Turn)
	}
	if !strings.Contains(stripEscapes(a.View()), "tutorial complete") && m.phase != "end" {
		t.Fatal("no completion note")
	}
}

// TestSpectateAndChat: one client plays a casual match against a bot,
// another watches it from the lobby and chats.
func TestSpectateAndChat(t *testing.T) {
	st, err := store.Open(context.Background(), "sqlite::memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	srv, cancel := newM3Server(t, st, 60)
	defer cancel()

	hp := newHarness(t, srv, "player")
	hp.run(hp.a.enter(newOnlineScreen(hp.a), nil))
	if !hp.pump(hp.screenIs("online-menu"), 5*time.Second) {
		t.Fatal("player not connected")
	}
	hp.key("k") // blitz -> casual (bot backfill)
	hp.key("enter")
	if !hp.pump(func() bool { d, ok := hp.a.screen.(*draftScreen); return ok && len(d.st.Heroes) > 0 }, 5*time.Second) {
		t.Fatalf("draft: %T", hp.a.screen)
	}
	hp.key("enter") // ban
	if !hp.pump(func() bool { d, ok := hp.a.screen.(*draftScreen); return ok && d.st.Kind == "pick" && d.st.Turn == 0 }, 5*time.Second) {
		t.Fatalf("pick: %T", hp.a.screen)
	}
	for j := 0; j < 5; j++ {
		d := hp.a.screen.(*draftScreen)
		legal := true
		for _, b := range d.st.Banned {
			if b == d.st.Heroes[d.sel] {
				legal = false
			}
		}
		if legal {
			break
		}
		hp.key("j")
	}
	hp.key("enter")
	if !hp.pump(hp.screenIs("match-orders"), 10*time.Second) {
		t.Fatalf("match: %T", hp.a.screen)
	}

	// Watcher: online menu -> w -> live list -> enter.
	hw := newHarness(t, srv, "watcher")
	hw.run(hw.a.enter(newOnlineScreen(hw.a), nil))
	if !hw.pump(hw.screenIs("online-menu"), 5*time.Second) {
		t.Fatal("watcher not connected")
	}
	hw.key("w")
	// The match has a bot in it (casual backfill): the watch list leaves
	// it out, so the watcher asks for it by id.
	if !hw.pump(func() bool { l, ok := hw.a.screen.(*lobbyScreen); return ok && l.loaded }, 5*time.Second) {
		t.Fatalf("lobby: %+v", hw.a.screen)
	}
	if l := hw.a.screen.(*lobbyScreen); len(l.rows) != 0 {
		t.Fatalf("watch list shows a game against bots: %+v", l.rows)
	}
	checkFrame(t, hw.a)
	_ = hw.a.net.send(proto.TSpectate, proto.Spectate{Match: hp.a.rm.id})
	if !hw.pump(func() bool { m, ok := hw.a.screen.(*matchScreen); return ok && m.src.Watching() }, 5*time.Second) {
		t.Fatalf("spectate: %T", hw.a.screen)
	}
	mw := hw.a.screen.(*matchScreen)
	if len(mw.vs.Units) != 10 {
		t.Fatalf("watcher sees %d units (should be the whole board)", len(mw.vs.Units))
	}
	checkFrame(t, hw.a)

	// The player commits a turn; the watcher animates it (casual is live).
	mp := hp.a.screen.(*matchScreen)
	mp.simpleOrder(hp.a, engine.ActHold)
	hp.key(" ")
	if !hw.pump(func() bool {
		m, ok := hw.a.screen.(*matchScreen)
		return ok && (m.vs.Match.Turn > 1 || m.phase == "anim")
	}, 10*time.Second) {
		t.Fatalf("watcher did not advance: %+v", hw.a.screen)
	}
	checkFrame(t, hw.a)

	// The watcher chats; the player sees it in the log.
	mw = hw.a.screen.(*matchScreen)
	mw.say(hw.a, "good luck")
	if !hp.pump(func() bool {
		m, ok := hp.a.screen.(*matchScreen)
		if !ok {
			return false
		}
		for _, l := range m.log {
			if strings.Contains(l, "good luck") {
				return true
			}
		}
		return false
	}, 5*time.Second) {
		t.Fatalf("chat not delivered: %+v", hp.a.screen.(*matchScreen).log)
	}
	// Leaving the watch goes back to the live list (then the menu) and
	// never touches the match being played.
	hw.key("Q")
	if _, ok := hw.a.screen.(*lobbyScreen); !ok {
		t.Fatalf("watcher exit: %T", hw.a.screen)
	}
	hw.key("esc")
	if !hw.pump(hw.screenIs("online-menu"), 5*time.Second) {
		t.Fatalf("watcher menu: %T", hw.a.screen)
	}
	if hp.a.screen.(*matchScreen).phase == "end" {
		t.Fatal("the player's match ended when the watcher left")
	}
}

func stripEscapes(s string) string {
	var b strings.Builder
	in := false
	for _, r := range s {
		switch {
		case r == '\x1b':
			in = true
		case in && r == 'm':
			in = false
		case !in:
			b.WriteRune(r)
		}
	}
	return b.String()
}
