package server

import (
	"context"
	"io"
	"log"
	"net"
	"testing"
	"time"

	"rfog/data"
	"rfog/engine"
	"rfog/proto"
	"rfog/server/store"
)

type tclient struct {
	t       *testing.T
	c       net.Conn
	in      chan proto.Frame
	name    string
	you     int
	tok     string
	welcome proto.Welcome
}

func dial(t *testing.T, s *Server, name, token string) *tclient {
	t.Helper()
	c := s.DialInternal()
	tc := &tclient{t: t, c: c, in: make(chan proto.Frame, 64), name: name}
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
	tc.send(proto.TAuth, proto.Auth{Name: name, Token: token})
	f := tc.expect(proto.TWelcome)
	var w proto.Welcome
	_ = f.As(&w)
	tc.tok = w.Token
	return tc
}

func (tc *tclient) send(t string, body any) {
	if err := proto.Encode(tc.c, t, body); err != nil {
		tc.t.Fatalf("%s send %s: %v", tc.name, t, err)
	}
}

// expect waits for a frame of type t, skipping others (queue status, chat).
func (tc *tclient) expect(t string) proto.Frame {
	tc.t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case f, ok := <-tc.in:
			if !ok {
				tc.t.Fatalf("%s: connection closed waiting for %s", tc.name, t)
			}
			if f.T == t {
				return f
			}
			if f.T == proto.TError {
				var e proto.Error
				_ = f.As(&e)
				tc.t.Logf("%s: error frame: %s %s", tc.name, e.Code, e.Msg)
			}
		case <-deadline:
			tc.t.Fatalf("%s: timeout waiting for %s", tc.name, t)
		}
	}
}

func newTestServer(t *testing.T, cfg Config) (*Server, context.CancelFunc) {
	t.Helper()
	c, err := data.Load()
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(context.Background(), "sqlite::memory:")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Logger == nil {
		cfg.Logger = log.New(io.Discard, "", 0)
	}
	s := New(c, st, cfg)
	ctx, cancel := context.WithCancel(context.Background())
	go s.Run(ctx)
	return s, func() { cancel(); st.Close() }
}

func TestFullMatch(t *testing.T) {
	s, stop := newTestServer(t, Config{TimeControls: map[string]TimeControl{"blitz": {Turn: 30 * time.Second, Guests: true}}})
	defer stop()
	a := dial(t, s, "alice", "")
	b := dial(t, s, "bob", "")
	a.send(proto.TQueue, proto.Queue{Mode: "blitz"})
	b.send(proto.TQueue, proto.Queue{Mode: "blitz"})
	var mf proto.MatchFound
	_ = a.expect(proto.TMatchFound).As(&mf)
	a.you = mf.You
	_ = b.expect(proto.TMatchFound).As(&mf)
	b.you = mf.You
	if a.you == b.you {
		t.Fatal("same slot")
	}
	clients := map[int]*tclient{a.you: a, b.you: b}

	// Draft: whoever's turn it is acts.
	for {
		var ds proto.DraftState
		_ = a.expect(proto.TDraftState).As(&ds)
		b.expect(proto.TDraftState)
		if ds.Kind == "done" {
			break
		}
		who := clients[ds.Turn]
		hero := ""
		for _, h := range ds.Heroes {
			ok := true
			for _, bn := range ds.Banned {
				if bn == h {
					ok = false
				}
			}
			if ok {
				hero = h
				break
			}
		}
		who.send(proto.TDraft, proto.DraftAction{Match: ds.Match, Kind: ds.Kind, Hero: hero})
		if len(ds.Picks) == 1 && ds.Kind == "pick" {
			break // last pick: play starts, no further DraftState
		}
	}
	// Second MatchFound arrives when play begins, then TurnStart.
	a.expect(proto.TMatchFound)
	b.expect(proto.TMatchFound)

	for turn := 1; turn <= 20; turn++ {
		var tsA, tsB proto.TurnStart
		_ = a.expect(proto.TTurnStart).As(&tsA)
		_ = b.expect(proto.TTurnStart).As(&tsB)
		if tsA.Turn != turn {
			t.Fatalf("turn %d, got %d", turn, tsA.Turn)
		}
		// Fog: A must not see B's units in their view at turn 1.
		if turn == 1 {
			for _, u := range tsA.View.Units {
				if u.Team != tsA.View.Player(a.you).Team {
					t.Fatalf("fog leak: A sees enemy unit %d", u.ID)
				}
			}
		}
		// Each side moves its commander one step toward the other side.
		for _, tc := range []*tclient{a, b} {
			ts := tsA
			if tc == b {
				ts = tsB
			}
			cmd := ts.View.Unit(ts.View.Player(tc.you).Commander)
			var orders []engine.Order
			if cmd != nil && cmd.Alive() {
				dx := 1
				if cmd.Pos.X > ts.View.Board.W/2 {
					dx = -1
				}
				to := engine.Pos{X: cmd.Pos.X + dx, Y: cmd.Pos.Y}
				if ts.View.Board.In(to) && ts.View.Board.Passable(to) && ts.View.UnitAt(to) == nil {
					orders = append(orders, engine.Order{UnitID: cmd.ID, Action: engine.ActMove, Path: []engine.Pos{to}})
				}
			}
			tc.send(proto.TOrders, proto.Orders{Match: ts.Match, Turn: ts.Turn, Orders: orders})
		}
		var rs proto.Resolved
		_ = a.expect(proto.TResolved).As(&rs)
		b.expect(proto.TResolved)
		if rs.Ended {
			var me proto.MatchEnd
			_ = a.expect(proto.TMatchEnd).As(&me)
			b.expect(proto.TMatchEnd)
			if me.Final.Visible != nil || !me.Final.Ended() {
				t.Fatalf("final state: %+v", me.Final.Match)
			}
			h, err := s.st.History(context.Background(), "", 5)
			_ = h
			if err != nil {
				t.Fatal(err)
			}
			return
		}
	}
	t.Fatal("match did not end in 20 turns")
}

func TestLeaveForfeitsAndReconnectResumes(t *testing.T) {
	s, stop := newTestServer(t, Config{TimeControls: map[string]TimeControl{"blitz": {Turn: 30 * time.Second, Guests: true}}, DraftSeconds: 1})
	defer stop()
	a := dial(t, s, "alice", "")
	b := dial(t, s, "bob", "")
	a.send(proto.TQueue, proto.Queue{Mode: "blitz"})
	b.send(proto.TQueue, proto.Queue{Mode: "blitz"})
	var mf proto.MatchFound
	_ = a.expect(proto.TMatchFound).As(&mf)
	b.expect(proto.TMatchFound)
	// Let the draft time out entirely (1s per step, 4 steps).
	var ts proto.TurnStart
	deadline := time.After(10 * time.Second)
	for {
		select {
		case f, ok := <-a.in:
			if !ok {
				t.Fatal("closed")
			}
			if f.T == proto.TTurnStart {
				_ = f.As(&ts)
				goto play
			}
		case <-deadline:
			t.Fatal("no turn start")
		}
	}
play:
	// A drops and reconnects with its token: it should get the same match back.
	a.c.Close()
	time.Sleep(50 * time.Millisecond)
	a2 := dial(t, s, "alice", a.tok)
	var mf2 proto.MatchFound
	_ = a2.expect(proto.TMatchFound).As(&mf2)
	if mf2.Match != ts.Match {
		t.Fatalf("resumed %s, want %s", mf2.Match, ts.Match)
	}
	var ts2 proto.TurnStart
	_ = a2.expect(proto.TTurnStart).As(&ts2)
	if ts2.Turn != ts.Turn {
		t.Fatalf("resumed turn %d, want %d", ts2.Turn, ts.Turn)
	}
	// B leaves: A wins by forfeit.
	b.send(proto.TLeave, nil)
	var me proto.MatchEnd
	_ = a2.expect(proto.TMatchEnd).As(&me)
	if me.Result != "forfeit" || me.Winner != mf2.Players[mf2.You].Team {
		t.Fatalf("end: %+v", me)
	}
}

func TestCasualBotBackfill(t *testing.T) {
	s, stop := newTestServer(t, Config{TimeControls: map[string]TimeControl{"casual": {Turn: 60 * time.Second, Backfill: true, Guests: true}}, BackfillAfter: 10 * time.Millisecond})
	defer stop()
	a := dial(t, s, "solo", "")
	a.send(proto.TQueue, proto.Queue{Mode: "casual", Hero: "wren"})
	var mf proto.MatchFound
	_ = a.expect(proto.TMatchFound).As(&mf)
	bot := false
	for _, p := range mf.Players {
		if p.Bot {
			bot = true
		}
	}
	if !bot {
		t.Fatalf("no bot: %+v", mf)
	}
	var ds proto.DraftState
	_ = a.expect(proto.TDraftState).As(&ds)
	if ds.Turn != mf.You || ds.Kind != "ban" {
		t.Fatalf("draft: %+v", ds)
	}
}
