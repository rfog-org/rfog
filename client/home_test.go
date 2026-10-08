package client

import (
	"context"
	"io"
	"log"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"rfog/server"
	"rfog/server/store"
)

// The home grid's top bar is reachable by keyboard: up from the first
// tile, along to Learn, and enter opens it.
func TestHomeTopBar(t *testing.T) {
	a := testApp(t)
	a.w, a.h = 160, 48
	a.applyTier()
	a.screen = newMenuScreen()
	a.View()
	m := a.screen.(*menuScreen)
	press(a, "k")
	if got := m.items[m.sel].label; got != "play" {
		t.Fatalf("up from the first tile: %q, want play", got)
	}
	press(a, "l", "l")
	if got := m.items[m.sel].label; got != "learn" {
		t.Fatalf("right twice: %q, want learn", got)
	}
	press(a, "enter")
	if _, ok := a.screen.(*rosterScreen); !ok {
		t.Fatalf("enter on learn: %T, want the roster", a.screen)
	}
}

// Back from an online screen goes to the home grid on a big terminal and
// to the online menu on a small one.
func TestBackGoesHome(t *testing.T) {
	c := testApp(t).c
	st, err := store.Open(context.Background(), "sqlite::memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	srv := server.New(c, st, server.Config{Logger: log.New(io.Discard, "", 0)})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go srv.Run(ctx)

	for _, tc := range []struct {
		w, h int
		home bool
	}{{160, 48, true}, {80, 24, false}} {
		h := newHarness(t, srv, "walker")
		h.a.w, h.a.h = tc.w, tc.h
		h.a.applyTier()
		h.run(h.a.enter(newOnlineScreen(h.a), nil))
		if !h.pump(h.screenIs("online-menu"), 5*time.Second) {
			t.Fatalf("%dx%d: not connected: %T", tc.w, tc.h, h.a.screen)
		}
		h.key("L")
		if _, ok := h.a.screen.(*ladderScreen); !ok {
			t.Fatalf("%dx%d: L opened %T, want the leaderboard", tc.w, tc.h, h.a.screen)
		}
		h.send(tea.KeyMsg{Type: tea.KeyEsc})
		_, home := h.a.screen.(*menuScreen)
		if home != tc.home {
			t.Fatalf("%dx%d: back went to %T", tc.w, tc.h, h.a.screen)
		}
		h.a.disconnect()
	}
}
