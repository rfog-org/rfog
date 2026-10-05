package server

import (
	"context"
	"io"
	"log"
	"testing"
	"time"

	"rfog/data"
	"rfog/engine"
	"rfog/proto"
	"rfog/server/store"
)

// dialAuth is dial with a full Auth message; it returns the error frame's
// message instead of failing when the server refuses.
func dialAuth(t *testing.T, s *Server, a proto.Auth) (*tclient, string) {
	t.Helper()
	c := s.DialInternal()
	tc := &tclient{t: t, c: c, in: make(chan proto.Frame, 64), name: a.Name}
	go func() {
		for {
			f, err := proto.Decode(c)
			if err != nil {
				close(tc.in)
				return
			}
			tc.in <- f
		}
	}()
	tc.send(proto.THello, proto.Hello{Version: proto.Version, Client: "test"})
	tc.send(proto.TAuth, a)
	select {
	case f := <-tc.in:
		switch f.T {
		case proto.TWelcome:
			_ = f.As(&tc.welcome)
			tc.tok = tc.welcome.Token
			return tc, ""
		case proto.TError:
			var e proto.Error
			_ = f.As(&e)
			return nil, e.Msg
		}
		t.Fatalf("unexpected %s", f.T)
	case <-time.After(5 * time.Second):
		t.Fatal("timeout in auth")
	}
	return nil, ""
}

// expectError waits for an error frame.
func (tc *tclient) expectError() string {
	tc.t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case f, ok := <-tc.in:
			if !ok {
				tc.t.Fatal("closed")
			}
			if f.T == proto.TError {
				var e proto.Error
				_ = f.As(&e)
				return e.Msg
			}
		case <-deadline:
			tc.t.Fatal("no error frame")
		}
	}
}

// runDraft drives a whole draft from the point of view of reader (whose
// DraftState stream is consumed) with every human acting in turn. Humans
// pick the first legal hero. It returns the draft order seen.
func runDraft(t *testing.T, mf proto.MatchFound, clients map[int]*tclient, reader *tclient) []proto.DraftStep {
	t.Helper()
	human := map[int]bool{}
	for _, p := range mf.Players {
		if !p.Bot {
			human[p.Slot] = true
		}
	}
	var order []proto.DraftStep
	for {
		var ds proto.DraftState
		_ = reader.expect(proto.TDraftState).As(&ds)
		order = ds.Order
		idx := len(ds.Banned) + len(ds.Picks)
		last := -1
		for i, st := range ds.Order {
			if human[st.Slot] {
				last = i
			}
		}
		who := clients[ds.Turn]
		team := ds.Turn / (len(mf.Players) / 2)
		hero := ""
		for _, h := range ds.Heroes {
			ok := true
			for _, bn := range ds.Banned {
				if bn == h {
					ok = false
				}
			}
			for slot, p := range ds.Picks {
				if p == h && slot/(len(mf.Players)/2) == team {
					ok = false
				}
			}
			if ok {
				hero = h
				break
			}
		}
		who.send(proto.TDraft, proto.DraftAction{Match: ds.Match, Kind: ds.Kind, Hero: hero})
		if idx == last {
			return order
		}
	}
}

func TestTeamDraftAndPlay(t *testing.T) {
	s, stop := newTestServer(t, Config{TimeControls: map[string]TimeControl{"blitz": {Turn: 30 * time.Second, Guests: true}}})
	defer stop()
	names := []string{"a", "b", "c", "d"}
	var cs []*tclient
	for _, n := range names {
		c := dial(t, s, n, "")
		c.send(proto.TQueue, proto.Queue{Mode: "blitz", Size: "2v2"})
		cs = append(cs, c)
	}
	clients := map[int]*tclient{}
	var mf proto.MatchFound
	for _, c := range cs {
		_ = c.expect(proto.TMatchFound).As(&mf)
		c.you = mf.You
		clients[c.you] = c
	}
	if len(mf.Players) != 4 || mf.Mode != "2v2" {
		t.Fatalf("found: %+v", mf)
	}
	for _, p := range mf.Players {
		if p.Team != p.Slot/2 {
			t.Fatalf("team assignment: %+v", p)
		}
	}
	// First state: check the snake order and that a teammate mirror pick is refused.
	var ds proto.DraftState
	_ = clients[0].expect(proto.TDraftState).As(&ds)
	want := []proto.DraftStep{{Slot: 0, Kind: "ban"}, {Slot: 2, Kind: "ban"}, {Slot: 0, Kind: "pick"}, {Slot: 2, Kind: "pick"}, {Slot: 3, Kind: "pick"}, {Slot: 1, Kind: "pick"}}
	for i, st := range want {
		if ds.Order[i] != st {
			t.Fatalf("order %v, want %v", ds.Order, want)
		}
	}
	// Bans.
	clients[0].send(proto.TDraft, proto.DraftAction{Match: ds.Match, Kind: "ban", Hero: ds.Heroes[0]})
	_ = clients[0].expect(proto.TDraftState).As(&ds)
	clients[2].send(proto.TDraft, proto.DraftAction{Match: ds.Match, Kind: "ban", Hero: ds.Heroes[1]})
	_ = clients[0].expect(proto.TDraftState).As(&ds)
	// Slot 0 picks hero 2; slot 2 (other team) may mirror it; slot 3 picks hero 3; slot 1 may not take hero 2.
	clients[0].send(proto.TDraft, proto.DraftAction{Match: ds.Match, Kind: "pick", Hero: ds.Heroes[2]})
	_ = clients[0].expect(proto.TDraftState).As(&ds)
	clients[2].send(proto.TDraft, proto.DraftAction{Match: ds.Match, Kind: "pick", Hero: ds.Heroes[2]})
	_ = clients[0].expect(proto.TDraftState).As(&ds)
	if ds.Turn != 3 {
		t.Fatalf("turn %d", ds.Turn)
	}
	clients[3].send(proto.TDraft, proto.DraftAction{Match: ds.Match, Kind: "pick", Hero: ds.Heroes[3]})
	_ = clients[0].expect(proto.TDraftState).As(&ds)
	clients[1].send(proto.TDraft, proto.DraftAction{Match: ds.Match, Kind: "pick", Hero: ds.Heroes[2]})
	if msg := clients[1].expectError(); msg != "hero not available" {
		t.Fatalf("mirror pick: %q", msg)
	}
	clients[1].send(proto.TDraft, proto.DraftAction{Match: ds.Match, Kind: "pick", Hero: ds.Heroes[4]})

	// Play: everyone gets MatchFound again, then TurnStart. Teammates share vision.
	views := map[int]proto.TurnStart{}
	for _, c := range cs {
		c.expect(proto.TMatchFound)
		var ts proto.TurnStart
		_ = c.expect(proto.TTurnStart).As(&ts)
		views[c.you] = ts
	}
	if views[0].View.Board.W != 24 || views[0].View.Board.H != 14 {
		t.Fatalf("board %dx%d", views[0].View.Board.W, views[0].View.Board.H)
	}
	seen := func(v engine.State, owner int) bool {
		for _, u := range v.Units {
			if u.Owner == owner {
				return true
			}
		}
		return false
	}
	if !seen(views[0].View, 1) || !seen(views[1].View, 0) || seen(views[0].View, 2) {
		t.Fatal("team vision wrong")
	}
	if len(views[0].View.Visible) != len(views[1].View.Visible) {
		t.Fatal("teammates see different tiles")
	}
	for _, c := range cs {
		c.send(proto.TOrders, proto.Orders{Match: mf.Match, Turn: 1})
	}
	for _, c := range cs {
		var rs proto.Resolved
		_ = c.expect(proto.TResolved).As(&rs)
		if rs.Turn != 2 {
			t.Fatalf("turn %d", rs.Turn)
		}
	}
	// Slot 1 leaves: the match goes on, its seat holds.
	for _, c := range cs {
		c.expect(proto.TTurnStart)
	}
	clients[1].send(proto.TLeave, proto.Join{Match: mf.Match})
	for _, slot := range []int{0, 2, 3} {
		clients[slot].send(proto.TOrders, proto.Orders{Match: mf.Match, Turn: 2})
	}
	for _, slot := range []int{0, 2, 3} {
		var rs proto.Resolved
		_ = clients[slot].expect(proto.TResolved).As(&rs)
		if rs.Turn != 3 {
			t.Fatalf("after leave: turn %d", rs.Turn)
		}
	}
	// The leaver can queue again.
	clients[1].send(proto.TQueue, proto.Queue{Mode: "blitz"})
	var qs proto.QueueStatus
	_ = clients[1].expect(proto.TQueueStatus).As(&qs)
	if qs.Mode != "blitz" {
		t.Fatalf("queue: %+v", qs)
	}
}

func TestAccounts(t *testing.T) {
	s, stop := newTestServer(t, Config{})
	defer stop()
	a, msg := dialAuth(t, s, proto.Auth{Name: "alice", Password: "secret1", Register: true})
	if msg != "" || a.welcome.Guest {
		t.Fatalf("register: %q %+v", msg, a.welcome)
	}
	if _, msg := dialAuth(t, s, proto.Auth{Name: "Alice", Password: "other12", Register: true}); msg != "that name is taken" {
		t.Fatalf("dup: %q", msg)
	}
	if _, msg := dialAuth(t, s, proto.Auth{Name: "bob", Password: "abc", Register: true}); msg == "" {
		t.Fatal("short password accepted")
	}
	if _, msg := dialAuth(t, s, proto.Auth{Name: "alice"}); msg == "" {
		t.Fatal("guest may not take an account name")
	}
	if _, msg := dialAuth(t, s, proto.Auth{Name: "alice", Password: "wrong1"}); msg != errBadLogin.Error() {
		t.Fatalf("wrong password: %q", msg)
	}
	// Token reconnect keeps the account.
	a.c.Close()
	a2, msg := dialAuth(t, s, proto.Auth{Token: a.tok})
	if msg != "" || a2.welcome.Player != a.welcome.Player || a2.welcome.Guest {
		t.Fatalf("token: %q %+v", msg, a2.welcome)
	}
	// A password login is another device: a new token, and the first
	// device stays signed in.
	a3, msg := dialAuth(t, s, proto.Auth{Name: "ALICE", Password: "secret1"})
	if msg != "" || a3.welcome.Player != a.welcome.Player || a3.tok == a.tok {
		t.Fatalf("login: %q %+v", msg, a3.welcome)
	}
	if again, msg := dialAuth(t, s, proto.Auth{Token: a.tok}); msg != "" || again.welcome.Player != a.welcome.Player {
		t.Fatalf("first device signed out by a second login: %q", msg)
	}
	// Guests may not queue ranked.
	g := dial(t, s, "guest", "")
	g.send(proto.TQueue, proto.Queue{Mode: "blitz"})
	if msg := g.expectError(); msg != "log in to play rated" {
		t.Fatalf("guest ranked: %q", msg)
	}
	g.send(proto.TQueue, proto.Queue{Mode: "casual", Size: "9v9"})
	if msg := g.expectError(); msg != "unknown size 9v9" {
		t.Fatalf("size: %q", msg)
	}
}

func TestRatedForfeitHistoryReplay(t *testing.T) {
	s, stop := newTestServer(t, Config{DraftSeconds: 1})
	defer stop()
	a, _ := dialAuth(t, s, proto.Auth{Name: "alice", Password: "secret1", Register: true})
	b, _ := dialAuth(t, s, proto.Auth{Name: "bob", Password: "secret2", Register: true})
	a.send(proto.TQueue, proto.Queue{Mode: "blitz"})
	b.send(proto.TQueue, proto.Queue{Mode: "blitz"})
	var mf proto.MatchFound
	_ = a.expect(proto.TMatchFound).As(&mf)
	a.you = mf.You
	_ = b.expect(proto.TMatchFound).As(&mf)
	b.you = mf.You
	// Let the draft time out (1 s per step).
	a.expect(proto.TTurnStart)
	b.expect(proto.TTurnStart)
	b.send(proto.TLeave, nil)
	var me proto.MatchEnd
	_ = a.expect(proto.TMatchEnd).As(&me)
	if me.Result != "forfeit" || len(me.Ratings) != 2 || len(me.Replay) == 0 {
		t.Fatalf("end: result %s ratings %+v replay %d", me.Result, me.Ratings, len(me.Replay))
	}
	if me.Ratings[a.you].After <= 1500 || me.Ratings[b.you].After >= 1500 || me.Ratings[a.you].Before != 1500 {
		t.Fatalf("ratings: %+v", me.Ratings)
	}
	if _, err := engine.UnmarshalReplay(me.Replay); err != nil {
		t.Fatal(err)
	}
	// History and replay download.
	a.send(proto.THistory, proto.History{})
	var hl proto.HistoryList
	_ = a.expect(proto.THistoryList).As(&hl)
	if len(hl.Matches) != 1 || hl.Matches[0].You != a.you || hl.Matches[0].Result != "forfeit" {
		t.Fatalf("history: %+v", hl)
	}
	hp := hl.Matches[0].Players[a.you]
	if hp.Name != "alice" || hp.Before == nil || hp.After == nil || *hp.After <= *hp.Before {
		t.Fatalf("history player: %+v", hp)
	}
	a.send(proto.TReplayGet, proto.ReplayGet{Match: mf.Match})
	var rd proto.ReplayData
	_ = a.expect(proto.TReplayData).As(&rd)
	rp, err := engine.UnmarshalReplay(rd.Replay)
	if err != nil || rp.Setup.ID != mf.Match {
		t.Fatalf("replay: %v", err)
	}
	a.send(proto.TReplayGet, proto.ReplayGet{Match: "nope"})
	if msg := a.expectError(); msg != "no replay for that match" {
		t.Fatalf("missing replay: %q", msg)
	}
	// Ladder and Welcome ratings.
	a.send(proto.TLadder, proto.Ladder{Mode: "blitz"})
	var ll proto.LadderList
	_ = a.expect(proto.TLadderList).As(&ll)
	if len(ll.Rows) != 2 || ll.Rows[0].Name != "alice" || ll.Rows[0].Rating.Wins != 1 {
		t.Fatalf("ladder: %+v", ll)
	}
	a.c.Close()
	a2, _ := dialAuth(t, s, proto.Auth{Token: a.tok})
	if r, ok := a2.welcome.Ratings["blitz"]; !ok || r.Games != 1 || r.Rating <= 1500 {
		t.Fatalf("welcome ratings: %+v", a2.welcome.Ratings)
	}
}

func TestRapidBank(t *testing.T) {
	s, stop := newTestServer(t, Config{DraftSeconds: 1, TimeControls: map[string]TimeControl{"rapid": {Turn: 3 * time.Second, Bank: 10 * time.Second, Guests: true}}})
	defer stop()
	a := dial(t, s, "a", "")
	b := dial(t, s, "b", "")
	a.send(proto.TQueue, proto.Queue{Mode: "rapid"})
	b.send(proto.TQueue, proto.Queue{Mode: "rapid"})
	var mf proto.MatchFound
	_ = a.expect(proto.TMatchFound).As(&mf)
	b.expect(proto.TMatchFound)
	var ts proto.TurnStart
	_ = a.expect(proto.TTurnStart).As(&ts)
	b.expect(proto.TTurnStart)
	if ts.Bank != 0 {
		t.Fatalf("initial bank %d", ts.Bank)
	}
	a.send(proto.TOrders, proto.Orders{Match: mf.Match, Turn: 1})
	b.send(proto.TOrders, proto.Orders{Match: mf.Match, Turn: 1})
	a.expect(proto.TResolved)
	_ = a.expect(proto.TTurnStart).As(&ts)
	if ts.Bank < 2 || ts.Deadline.Before(time.Now().Add(4*time.Second)) {
		t.Fatalf("bank %d deadline in %v", ts.Bank, time.Until(ts.Deadline))
	}
}

func TestDailyAsync(t *testing.T) {
	c, err := data.Load()
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(context.Background(), "sqlite::memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	cfg := Config{Logger: log.New(io.Discard, "", 0), TimeControls: map[string]TimeControl{
		"daily": {Turn: time.Hour, Async: true, Ranked: true},
		"blitz": {Turn: 30 * time.Second, Ranked: true},
	}}
	s := New(c, st, cfg)
	ctx, cancel := context.WithCancel(context.Background())
	go s.Run(ctx)

	a, _ := dialAuth(t, s, proto.Auth{Name: "alice", Password: "secret1", Register: true})
	b, _ := dialAuth(t, s, proto.Auth{Name: "bob", Password: "secret2", Register: true})
	a.send(proto.TQueue, proto.Queue{Mode: "daily"})
	b.send(proto.TQueue, proto.Queue{Mode: "daily"})
	var mf proto.MatchFound
	_ = a.expect(proto.TMatchFound).As(&mf)
	a.you = mf.You
	_ = b.expect(proto.TMatchFound).As(&mf)
	b.you = mf.You
	runDraft(t, mf, map[int]*tclient{a.you: a, b.you: b}, a)
	a.expect(proto.TMatchFound)
	b.expect(proto.TMatchFound)
	var ts proto.TurnStart
	_ = a.expect(proto.TTurnStart).As(&ts)
	b.expect(proto.TTurnStart)
	if time.Until(ts.Deadline) < 50*time.Minute {
		t.Fatalf("deadline %v", ts.Deadline)
	}
	// A goes away; B commits; nothing resolves (async waits for A).
	a.c.Close()
	b.send(proto.TOrders, proto.Orders{Match: mf.Match, Turn: 1})
	select {
	case f := <-b.in:
		if f.T == proto.TResolved {
			t.Fatal("resolved without A")
		}
	case <-time.After(200 * time.Millisecond):
	}
	// A returns: not auto-resumed (async), but listed as live and joinable.
	// While a daily match is in flight, A may still queue for a timed match.
	a2, _ := dialAuth(t, s, proto.Auth{Token: a.tok})
	if a2.welcome.InMatch != "" || len(a2.welcome.Live) != 1 || !a2.welcome.Live[0].YourTurn || a2.welcome.Live[0].Versus != "bob" {
		t.Fatalf("welcome: %+v", a2.welcome)
	}
	a2.send(proto.TQueue, proto.Queue{Mode: "blitz"})
	a2.expect(proto.TQueueStatus)
	a2.send(proto.TCancel, nil)
	a2.send(proto.TJoin, proto.Join{Match: mf.Match})
	a2.expect(proto.TMatchFound)
	_ = a2.expect(proto.TTurnStart).As(&ts)
	if ts.Turn != 1 || ts.Resumed {
		t.Fatalf("join: %+v", ts.Turn)
	}
	a2.send(proto.TOrders, proto.Orders{Match: mf.Match, Turn: 1})
	a2.expect(proto.TResolved)
	b.expect(proto.TResolved)
	_ = a2.expect(proto.TTurnStart).As(&ts)
	deadline := ts.Deadline

	// Restart the server on the same store: the match is restored with its
	// clocks and continues without waiting for anyone.
	cancel()
	a2.c.Close()
	b.c.Close()
	time.Sleep(50 * time.Millisecond)
	s2 := New(c, st, cfg)
	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()
	go s2.Run(ctx2)
	time.Sleep(50 * time.Millisecond)
	b2, _ := dialAuth(t, s2, proto.Auth{Token: b.tok})
	if len(b2.welcome.Live) != 1 || b2.welcome.Live[0].Turn != 2 {
		t.Fatalf("restored live: %+v", b2.welcome.Live)
	}
	b2.send(proto.TJoin, proto.Join{Match: mf.Match})
	b2.expect(proto.TMatchFound)
	_ = b2.expect(proto.TTurnStart).As(&ts)
	if ts.Turn != 2 || !ts.Deadline.Equal(deadline) {
		t.Fatalf("restored turn %d deadline %v want %v", ts.Turn, ts.Deadline, deadline)
	}
	b2.send(proto.TOrders, proto.Orders{Match: mf.Match, Turn: 2})
	a3, _ := dialAuth(t, s2, proto.Auth{Token: a.tok})
	a3.send(proto.TJoin, proto.Join{Match: mf.Match})
	a3.expect(proto.TTurnStart)
	a3.send(proto.TOrders, proto.Orders{Match: mf.Match, Turn: 2})
	var rs proto.Resolved
	_ = a3.expect(proto.TResolved).As(&rs)
	if rs.Turn != 3 {
		t.Fatalf("turn %d", rs.Turn)
	}
}

func TestCasualTeamBackfill(t *testing.T) {
	s, stop := newTestServer(t, Config{TimeControls: map[string]TimeControl{"casual": {Turn: 60 * time.Second, Backfill: true, Guests: true}}, BackfillAfter: 10 * time.Millisecond})
	defer stop()
	a := dial(t, s, "solo", "")
	a.send(proto.TQueue, proto.Queue{Mode: "casual", Size: "3v3", Hero: "wren"})
	var mf proto.MatchFound
	_ = a.expect(proto.TMatchFound).As(&mf)
	bots := 0
	for _, p := range mf.Players {
		if p.Bot {
			bots++
		}
	}
	if len(mf.Players) != 6 || bots != 5 || mf.You != 0 {
		t.Fatalf("found: %+v", mf)
	}
	runDraft(t, mf, map[int]*tclient{0: a}, a)
	a.expect(proto.TMatchFound)
	var ts proto.TurnStart
	_ = a.expect(proto.TTurnStart).As(&ts)
	if ts.View.Board.W != 28 || len(ts.View.Players) != 6 {
		t.Fatalf("board %d players %d", ts.View.Board.W, len(ts.View.Players))
	}
}
