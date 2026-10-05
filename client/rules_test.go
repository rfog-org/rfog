package client

import (
	"context"
	"io"
	"log"
	"testing"
	"time"

	"rfog/data"
	"rfog/server"
	"rfog/server/store"
)

// A client whose embedded rules differ from the server's plays online by
// the server's, and gets its own back when it disconnects.
func TestOnlineUsesServerRules(t *testing.T) {
	for _, patched := range []bool{false, true} {
		c, err := data.Load()
		if err != nil {
			t.Fatal(err)
		}
		if patched {
			r := c.Rules
			r.WinScore += 3
			c.Rules = r
		}
		st, _ := store.Open(context.Background(), "sqlite::memory:")
		srv := server.New(c, st, server.Config{Logger: log.New(io.Discard, "", 0)})
		ctx, cancel := context.WithCancel(context.Background())
		go srv.Run(ctx)

		h := newHarness(t, srv, "carol")
		local := h.a.c
		h.run(h.a.enter(newOnlineScreen(h.a), nil))
		if !h.pump(h.screenIs("online-menu"), 5*time.Second) {
			t.Fatalf("not connected: %+v", h.a.screen)
		}
		w := h.a.net.welcome
		if w.Rules != c.Fingerprint() {
			t.Fatalf("welcome rules %q, want %q", w.Rules, c.Fingerprint())
		}
		if got := w.Content != nil; got != patched {
			t.Fatalf("patched=%v: content sent=%v", patched, got)
		}
		if h.a.c.Rules.WinScore != c.Rules.WinScore {
			t.Fatalf("patched=%v: online win score %d, server %d", patched, h.a.c.Rules.WinScore, c.Rules.WinScore)
		}
		if h.a.c.Fingerprint() != c.Fingerprint() {
			t.Fatalf("patched=%v: online rules fingerprint differs from server", patched)
		}
		h.a.disconnect()
		if h.a.c != local {
			t.Fatalf("patched=%v: local rules not restored", patched)
		}
		cancel()
	}
}
