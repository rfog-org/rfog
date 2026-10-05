package server

import (
	"testing"
	"time"

	"rfog/proto"
)

// TestSpectateUnranked: an unranked match streams live to a watcher, who
// can chat with the players and is dropped when the match ends.
func TestSpectateUnranked(t *testing.T) {
	s, stop := newTestServer(t, Config{DraftSeconds: 1, TimeControls: map[string]TimeControl{
		"casual": {Turn: 30 * time.Second, Guests: true},
	}})
	defer stop()
	a, b := dial(t, s, "alice", ""), dial(t, s, "bob", "")
	a.send(proto.TQueue, proto.Queue{Mode: "casual"})
	b.send(proto.TQueue, proto.Queue{Mode: "casual"})
	var mf proto.MatchFound
	_ = a.expect(proto.TMatchFound).As(&mf)
	a.you = mf.You
	_ = b.expect(proto.TMatchFound).As(&mf)
	b.you = mf.You

	// A third client watches: the lobby lists the match, live, not delayed.
	w := dial(t, s, "watcher", "")
	w.send(proto.TLobby, nil)
	var ll proto.LobbyList
	_ = w.expect(proto.TLobbyList).As(&ll)
	if len(ll.Matches) != 1 || ll.Matches[0].Match != mf.Match || ll.Matches[0].Delayed {
		t.Fatalf("lobby: %+v", ll)
	}
	w.send(proto.TSpectate, proto.Spectate{Match: mf.Match})
	// Draft times out; play starts; the watcher gets frames after each turn.
	a.expect(proto.TTurnStart)
	b.expect(proto.TTurnStart)
	a.send(proto.TOrders, proto.Orders{Match: mf.Match, Turn: 1})
	b.send(proto.TOrders, proto.Orders{Match: mf.Match, Turn: 1})
	var ss proto.SpecState
	_ = w.expect(proto.TSpecState).As(&ss)
	if ss.Turn != 2 || ss.Delayed || ss.View.Visible != nil || len(ss.View.Units) != 10 {
		t.Fatalf("spec state: turn %d delayed %v units %d visible %v", ss.Turn, ss.Delayed, len(ss.View.Units), ss.View.Visible != nil)
	}
	if len(ss.Events) == 0 {
		t.Fatal("no events for the watcher")
	}
	// Chat reaches players and watchers, tagged.
	w.send(proto.TChat, proto.Chat{Match: mf.Match, Text: "nice hold"})
	var c proto.Chat
	_ = a.expect(proto.TChatMsg).As(&c)
	if c.From != "watcher (watching)" || c.Text != "nice hold" {
		t.Fatalf("chat: %+v", c)
	}
	// A player leaving ends the match; the watcher gets a final frame.
	b.send(proto.TLeave, proto.Join{Match: mf.Match})
	_ = w.expect(proto.TSpecState).As(&ss)
	if !ss.Ended || ss.Result != "forfeit" {
		t.Fatalf("final frame: %+v", ss.Ended)
	}
	// The match is over: it can no longer be watched, and the lobby is empty.
	a2 := dial(t, s, "carol", "")
	a2.send(proto.TSpectate, proto.Spectate{Match: mf.Match})
	if msg := a2.expectError(); msg != "no such live match" {
		t.Fatalf("ended match: %q", msg)
	}
	a2.send(proto.TLobby, nil)
	_ = a2.expect(proto.TLobbyList).As(&ll)
	if len(ll.Matches) != 0 {
		t.Fatalf("lobby after the end: %+v", ll)
	}
}

// TestSpectateRankedIsDelayed: ranked watchers see the previous turn, so
// a watcher cannot relay fog information in time to matter.
func TestSpectateRankedIsDelayed(t *testing.T) {
	s, stop := newTestServer(t, Config{DraftSeconds: 1, TimeControls: map[string]TimeControl{
		"blitz": {Turn: 30 * time.Second, Ranked: true, Guests: true},
	}})
	defer stop()
	a, b := dial(t, s, "alice", ""), dial(t, s, "bob", "")
	a.send(proto.TQueue, proto.Queue{Mode: "blitz"})
	b.send(proto.TQueue, proto.Queue{Mode: "blitz"})
	var mf proto.MatchFound
	_ = a.expect(proto.TMatchFound).As(&mf)
	b.expect(proto.TMatchFound)
	w := dial(t, s, "watcher", "")
	w.send(proto.TSpectate, proto.Spectate{Match: mf.Match})
	a.expect(proto.TTurnStart)
	b.expect(proto.TTurnStart)
	for turn := 1; turn <= 3; turn++ {
		a.send(proto.TOrders, proto.Orders{Match: mf.Match, Turn: turn})
		b.send(proto.TOrders, proto.Orders{Match: mf.Match, Turn: turn})
		a.expect(proto.TResolved)
		b.expect(proto.TResolved)
		a.expect(proto.TTurnStart)
		b.expect(proto.TTurnStart)
	}
	var ss proto.SpecState
	_ = w.expect(proto.TSpecState).As(&ss)
	if !ss.Delayed {
		t.Fatal("ranked spectating should be delayed")
	}
	if ss.Turn >= ss.Live {
		t.Fatalf("watcher sees turn %d while players are on %d", ss.Turn, ss.Live)
	}
}
