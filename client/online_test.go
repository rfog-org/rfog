package client

import (
	"context"
	"io"
	"log"
	"net"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"rfog/engine"
	"rfog/server"
	"rfog/server/store"
)

// harness runs an App the way tea.Program would: commands run in
// goroutines and their messages are fed back through Update.
type harness struct {
	t   *testing.T
	a   *App
	out chan tea.Msg
}

func newHarness(t *testing.T, srv *server.Server, name string) *harness {
	a := testApp(t)
	a.set.Name = name
	a.set.Token = ""
	a.nosave = true // never touch the real settings file
	a.dial = func(string) (net.Conn, error) { return srv.DialInternal(), nil }
	a.server = "internal"
	h := &harness{t: t, a: a, out: make(chan tea.Msg, 256)}
	return h
}

func (h *harness) run(cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	go func() {
		if m := cmd(); m != nil {
			h.out <- m
		}
	}()
}

func (h *harness) send(msg tea.Msg) {
	_, cmd := h.a.Update(msg)
	h.run(cmd)
}

func (h *harness) key(k string) {
	switch k {
	case "enter":
		h.send(tea.KeyMsg{Type: tea.KeyEnter})
	case " ":
		h.send(tea.KeyMsg{Type: tea.KeySpace})
	default:
		h.send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)})
	}
}

// pump delivers command results until pred holds or the timeout passes.
func (h *harness) pump(pred func() bool, timeout time.Duration) bool {
	deadline := time.After(timeout)
	for {
		if pred() {
			return true
		}
		select {
		case m := <-h.out:
			if b, ok := m.(tea.BatchMsg); ok {
				for _, c := range b {
					h.run(c)
				}
				continue
			}
			h.send(m)
		case <-deadline:
			return pred()
		}
	}
}

func (h *harness) screenIs(kind string) func() bool {
	return func() bool {
		switch kind {
		case "online-menu":
			o, ok := h.a.screen.(*onlineScreen)
			return ok && o.state == "menu"
		case "draft":
			_, ok := h.a.screen.(*draftScreen)
			return ok
		case "match-orders":
			m, ok := h.a.screen.(*matchScreen)
			return ok && m.phase == "orders"
		case "match-end":
			m, ok := h.a.screen.(*matchScreen)
			return ok && m.phase == "end"
		}
		return false
	}
}

func TestOnlineThroughScreens(t *testing.T) {
	c := testApp(t).c
	st, err := store.Open(context.Background(), "sqlite::memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	srv := server.New(c, st, server.Config{TimeControls: map[string]server.TimeControl{"blitz": {Turn: 30 * time.Second, Guests: true}}, DraftSeconds: 60, Logger: log.New(io.Discard, "", 0)})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go srv.Run(ctx)

	ha, hb := newHarness(t, srv, "alice"), newHarness(t, srv, "bob")
	for _, h := range []*harness{ha, hb} {
		h.run(h.a.enter(newOnlineScreen(h.a), nil))
		if !h.pump(h.screenIs("online-menu"), 5*time.Second) {
			t.Fatalf("%s: not connected: %+v", h.a.set.Name, h.a.screen)
		}
		h.key("enter") // queue blitz (default selection)
	}
	for _, h := range []*harness{ha, hb} {
		if !h.pump(h.screenIs("draft"), 5*time.Second) {
			t.Fatalf("%s: no draft: %+v", h.a.set.Name, h.a.screen)
		}
	}
	driveDraft(t, []*harness{ha, hb}, 4)
	for _, h := range []*harness{ha, hb} {
		if !h.pump(h.screenIs("match-orders"), 5*time.Second) {
			t.Fatalf("%s: match did not start: %T %+v", h.a.set.Name, h.a.screen, h.a.screen)
		}
	}
	ma, mb := ha.a.screen.(*matchScreen), hb.a.screen.(*matchScreen)
	if ma.vs.Match.Turn != 1 || mb.vs.Match.Turn != 1 || ma.deadline.IsZero() {
		t.Fatalf("turn/deadline: %d %d %v", ma.vs.Match.Turn, mb.vs.Match.Turn, ma.deadline)
	}
	// Both commit a hold for the commander; server resolves; both animate to turn 2.
	for _, h := range []*harness{ha, hb} {
		m := h.a.screen.(*matchScreen)
		m.simpleOrder(h.a, engine.ActHold)
		h.key(" ")
	}
	for _, h := range []*harness{ha, hb} {
		ok := h.pump(func() bool {
			m, ok := h.a.screen.(*matchScreen)
			return ok && m.phase == "orders" && m.vs.Match.Turn == 2
		}, 8*time.Second)
		if !ok {
			m := h.a.screen.(*matchScreen)
			t.Fatalf("%s: phase %q turn %d msg %q", h.a.set.Name, m.phase, m.vs.Match.Turn, m.msg)
		}
		checkFrame(t, h.a)
	}
	// Q asks first: one Q and then any other key keeps her in the match.
	ha.key("Q")
	if m, ok := ha.a.screen.(*matchScreen); !ok || !strings.Contains(m.msg, "FORFEIT") {
		t.Fatalf("first Q should warn about the forfeit: %T", ha.a.screen)
	}
	ha.key("x")
	if m, ok := ha.a.screen.(*matchScreen); !ok || m.quitArmed {
		t.Fatalf("another key should cancel the quit: %T", ha.a.screen)
	}
	// Alice leaves for real: Bob should reach the end card with a forfeit win.
	ha.key("Q")
	ha.key("Q")
	if !hb.pump(hb.screenIs("match-end"), 5*time.Second) {
		m := hb.a.screen.(*matchScreen)
		t.Fatalf("bob: phase %q", m.phase)
	}
	mb = hb.a.screen.(*matchScreen)
	if mb.final == nil || mb.final.Match.Result != "forfeit" {
		t.Fatalf("bob final: %+v", mb.final)
	}
	checkFrame(t, hb.a)
}

// driveDraft runs a human-only draft of steps actions: at step i, wait
// until every client sees i completed actions, then whoever's turn it is
// presses enter on a legal hero.
func driveDraft(t *testing.T, hs []*harness, steps int) {
	t.Helper()
	progress := func(h *harness) int {
		d, ok := h.a.screen.(*draftScreen)
		if !ok {
			return -1
		}
		return len(d.st.Banned) + len(d.st.Picks)
	}
	for i := 0; i < steps; i++ {
		for _, h := range hs {
			h.pump(func() bool { d, ok := h.a.screen.(*draftScreen); return ok && len(d.st.Heroes) > 0 && progress(h) >= i }, 3*time.Second)
		}
		acted := false
		for _, h := range hs {
			d, ok := h.a.screen.(*draftScreen)
			if !ok || len(d.st.Heroes) == 0 || d.st.Turn != d.rm.you || d.st.Kind == "done" {
				continue
			}
			for j := 0; j < 5; j++ {
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
			t.Logf("%s: %s %s", h.a.set.Name, d.st.Kind, d.st.Heroes[d.sel])
			h.key("enter")
			acted = true
			break
		}
		if !acted {
			t.Fatalf("step %d: nobody could act", i)
		}
	}
}
