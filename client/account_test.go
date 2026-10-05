package client

import (
	"context"
	tea "github.com/charmbracelet/bubbletea"
	"io"
	"log"
	"strings"
	"testing"
	"time"

	"rfog/server"
	"rfog/server/store"
)

// Through the screens: a guest saves its progress as an account and is
// shown its recovery code once; the account page then offers what an
// account can do; signing out returns to the login form; the forgotten-
// password form, with that code, signs back in and shows a fresh code.
func TestAccountThroughScreens(t *testing.T) {
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

	h := newHarness(t, srv, "drifter")
	h.run(h.a.enter(newOnlineScreen(h.a), nil))
	if !h.pump(h.screenIs("online-menu"), 5*time.Second) {
		t.Fatalf("not connected: %+v", h.a.screen)
	}
	o := h.a.screen.(*onlineScreen)
	if !h.a.net.welcome.Guest {
		t.Fatal("expected a guest")
	}

	// account page -> save my progress
	h.key("a")
	if o.state != "account" || acctActions(true)[0].id != "save" {
		t.Fatalf("account page: %q", o.state)
	}
	h.key("enter")        // save: name prefilled, focus on password
	h.typeText("glass42") // password
	h.key("enter")        // confirm
	if !h.pump(func() bool { return o.acct.code != "" }, 5*time.Second) {
		t.Fatalf("no recovery code shown: err %q", o.err)
	}
	code := o.acct.code
	if h.a.net.welcome.Guest || h.a.set.Account != "drifter" {
		t.Fatalf("still a guest: %+v", h.a.net.welcome)
	}
	if len(o.accountView(h.a)) == 0 {
		t.Fatal("no view")
	}
	h.key("enter") // noted
	if o.acct.code != "" {
		t.Fatal("code card stayed")
	}
	// The account's actions now; sign out of this device (third line).
	acts := acctActions(false)
	for i, a := range acts {
		if a.id == "logout" {
			o.acct.sel = i
		}
	}
	h.key("enter")
	if !h.pump(func() bool { return o.state == "login" && h.a.net == nil }, 5*time.Second) {
		t.Fatalf("not signed out: %q %q", o.state, o.err)
	}
	if h.a.set.Token != "" {
		t.Fatal("token kept after signing out")
	}

	// Forgot the password: recover with the code.
	o.form.focus = 5
	h.key("enter")
	if !o.form.recover {
		t.Fatal("no forgotten-password form")
	}
	o.form.name, o.form.focus = "drifter", 1
	h.typeText(code)
	h.key("enter")
	h.typeText("newline7")
	h.key("enter")
	h.key("enter") // set new password
	if !h.pump(func() bool { return h.a.net != nil && o.state == "recovery" }, 5*time.Second) {
		t.Fatalf("recovery did not sign in: %q %q", o.state, o.err)
	}
	if o.acct.code == "" || o.acct.code == code {
		t.Fatalf("no fresh code: %q", o.acct.code)
	}
	h.key("enter")
	if o.state != "menu" || h.a.net.welcome.Guest || h.a.net.welcome.Name != "drifter" {
		t.Fatalf("after recovery: %q %+v", o.state, h.a.net.welcome)
	}
}

// The online menu lists the server's clocks, rated by default for an
// account; c switches to casual, and the queue lands in that pool.
func TestOnlineClocksRatedOrCasual(t *testing.T) {
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
	h := newHarness(t, srv, "ticker")
	h.register(t, "secret1")
	o := h.a.screen.(*onlineScreen)
	if len(o.clocks) != len(server.Clocks) || o.modes[o.sel] != "30s" || !o.rated {
		t.Fatalf("menu: %d clocks, selected %q, rated %v", len(o.clocks), o.modes[o.sel], o.rated)
	}
	if v := h.a.View(); !strings.Contains(v, "blitz") || !strings.Contains(v, "rated") {
		t.Fatalf("menu does not show the clocks:\n%s", v)
	}
	h.key("c")
	if o.rated {
		t.Fatal("c did not switch to casual")
	}
	h.key("enter")
	if !h.pump(func() bool { return o.state == "queued" && o.status.Mode == "30s"+server.CasualSuffix }, 5*time.Second) {
		t.Fatalf("queued %q (state %s)", o.status.Mode, o.state)
	}
}

// Challenge a friend through the screens: one opens a challenge (f,
// enter) and reads the code; the other types it (f, down), sees who it is
// from, accepts, and both are in the draft.
func TestChallengeThroughScreens(t *testing.T) {
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
	ha, hb := newHarness(t, srv, "anna"), newHarness(t, srv, "ben")
	for _, h := range []*harness{ha, hb} {
		h.run(h.a.enter(newOnlineScreen(h.a), nil))
		if !h.pump(h.screenIs("online-menu"), 5*time.Second) {
			t.Fatal("not connected")
		}
	}
	oa := ha.a.screen.(*onlineScreen)
	ha.key("f")
	ha.key("enter")
	if !ha.pump(func() bool { return oa.ch.open != nil }, 5*time.Second) {
		t.Fatalf("no challenge: %q", oa.err)
	}
	code := oa.ch.open.Code
	ob := hb.a.screen.(*onlineScreen)
	hb.key("f")
	hb.keyType(tea.KeyDown)
	hb.typeText(strings.ToLower(code))
	hb.key("enter")
	if !hb.pump(func() bool { return ob.ch.offer != nil }, 5*time.Second) {
		t.Fatalf("no offer: %q", ob.err)
	}
	if ob.ch.offer.From != "anna" {
		t.Fatalf("offer from %q", ob.ch.offer.From)
	}
	hb.key("enter")
	for _, h := range []*harness{ha, hb} {
		if !h.pump(h.screenIs("draft"), 5*time.Second) {
			t.Fatalf("%s: no draft: %T", h.a.set.Name, h.a.screen)
		}
	}
}
