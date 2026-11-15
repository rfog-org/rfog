package server

import (
	"context"
	"encoding/binary"
	"io"
	"log"
	"net"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"rfog/data"
	"rfog/engine"
	"rfog/proto"
	"rfog/server/store"
)

// Strangers on the internet send what they like. These tests send it.

var soloControls = map[string]TimeControl{"casual": {Turn: 60 * time.Second, Backfill: true, Guests: true}}

// soloMatch queues a guest against a bot and drafts the first legal heroes,
// returning once the first turn starts.
func soloMatch(t *testing.T, s *Server, name string) (*tclient, proto.MatchFound, proto.TurnStart) {
	t.Helper()
	a := dial(t, s, name, "")
	a.send(proto.TQueue, proto.Queue{Mode: "casual"})
	var mf proto.MatchFound
	_ = a.expect(proto.TMatchFound).As(&mf)
	deadline := time.After(10 * time.Second)
	for {
		select {
		case f, ok := <-a.in:
			if !ok {
				t.Fatal("closed during the draft")
			}
			switch f.T {
			case proto.TDraftState:
				var ds proto.DraftState
				_ = f.As(&ds)
				if ds.Kind == "done" || ds.Turn != mf.You {
					continue
				}
				for _, h := range ds.Heroes {
					taken := false
					for _, b := range ds.Banned {
						taken = taken || b == h
					}
					if !taken {
						a.send(proto.TDraft, proto.DraftAction{Match: mf.Match, Kind: ds.Kind, Hero: h})
						break
					}
				}
			case proto.TTurnStart:
				var ts proto.TurnStart
				_ = f.As(&ts)
				return a, mf, ts
			}
		case <-deadline:
			t.Fatal("no turn start")
		}
	}
}

// expectNo fails if a frame of type typ arrives within d.
func (tc *tclient) expectNo(typ string, d time.Duration) {
	tc.t.Helper()
	deadline := time.After(d)
	for {
		select {
		case f, ok := <-tc.in:
			if !ok {
				return
			}
			if f.T == typ {
				tc.t.Fatalf("%s: unexpected %s", tc.name, typ)
			}
		case <-deadline:
			return
		}
	}
}

// expectClosed waits for the server to hang up.
func (tc *tclient) expectClosed() {
	tc.t.Helper()
	deadline := time.After(3 * time.Second)
	for {
		select {
		case _, ok := <-tc.in:
			if !ok {
				return
			}
		case <-deadline:
			tc.t.Fatalf("%s: connection still open", tc.name)
		}
	}
}

// Orders walking off the board, or more orders than there are units, are
// refused before the engine sees them. An attack made from a move's end
// off the board used to index past the board and panic the whole server.
func TestMalformedOrdersRefused(t *testing.T) {
	s, stop := newTestServer(t, Config{TimeControls: soloControls, BackfillAfter: 10 * time.Millisecond})
	defer stop()
	a, mf, ts := soloMatch(t, s, "mallory")
	m := s.match(mf.Match)
	var mine, theirs *engine.Unit
	m.mu.Lock()
	for i := range m.s.Units {
		u := &m.s.Units[i]
		if u.IsCommander && u.Owner == mf.You {
			mine = u
		} else if u.Owner != mf.You && u.Alive() && theirs == nil {
			theirs = u
		}
	}
	// Put an enemy next to the commander, so an attack on it gets as far
	// as asking where it is made from.
	theirs.Pos = engine.Pos{X: mine.Pos.X, Y: mine.Pos.Y + 1}
	if !m.s.Board.In(theirs.Pos) {
		theirs.Pos.Y = mine.Pos.Y - 1
	}
	me, target := mine.ID, theirs.ID
	m.mu.Unlock()

	a.send(proto.TOrders, proto.Orders{Match: mf.Match, Turn: ts.Turn, Orders: []engine.Order{
		{UnitID: me, Action: engine.ActMove, Path: []engine.Pos{{X: -40, Y: -40}}},
		{UnitID: me, Action: engine.ActAttack, TargetU: target},
	}})
	if msg := a.expectError(); msg != "order off the board" {
		t.Fatalf("off-board move: %q", msg)
	}
	a.send(proto.TOrders, proto.Orders{Match: mf.Match, Turn: ts.Turn, Orders: []engine.Order{
		{UnitID: me, Action: engine.ActAbility, Ability: "x", Target: engine.Pos{X: 1 << 30}},
	}})
	if msg := a.expectError(); msg != "order off the board" {
		t.Fatalf("off-board target: %q", msg)
	}
	many := make([]engine.Order, 300)
	for i := range many {
		many[i] = engine.Order{UnitID: me, Action: engine.ActHold}
	}
	a.send(proto.TOrders, proto.Orders{Match: mf.Match, Turn: ts.Turn, Orders: many})
	if msg := a.expectError(); msg != "too many orders" {
		t.Fatalf("300 orders: %q", msg)
	}
	// Still playing: real orders go through.
	a.send(proto.TOrders, proto.Orders{Match: mf.Match, Turn: ts.Turn})
	a.expect(proto.TResolved)
}

// panicStore panics on demand, standing in for any bug a request or a
// turn can reach.
type panicStore struct {
	store.Store
	history, snapshot atomic.Bool
}

func (p *panicStore) History(ctx context.Context, id string, limit int) ([]store.Match, error) {
	if p.history.Load() {
		panic("history")
	}
	return p.Store.History(ctx, id, limit)
}

func (p *panicStore) SaveSnapshot(ctx context.Context, sn store.Snapshot) error {
	if p.snapshot.Load() {
		panic("snapshot")
	}
	return p.Store.SaveSnapshot(ctx, sn)
}

func newPanicServer(t *testing.T, cfg Config) (*Server, *panicStore, func()) {
	t.Helper()
	c, err := data.Load()
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(context.Background(), "sqlite::memory:")
	if err != nil {
		t.Fatal(err)
	}
	ps := &panicStore{Store: st}
	cfg.PlaybackGrace = -1
	cfg.Logger = log.New(io.Discard, "", 0)
	s := New(c, ps, cfg)
	ctx, cancel := context.WithCancel(context.Background())
	go s.Run(ctx)
	return s, ps, func() { cancel(); st.Close() }
}

// A panic while handling a request is that request's error; the
// connection and the server carry on.
func TestPanicInRequestIsContained(t *testing.T) {
	s, ps, stop := newPanicServer(t, Config{})
	defer stop()
	a := dial(t, s, "crashy", "")
	ps.history.Store(true)
	a.send(proto.THistory, proto.History{})
	if msg := a.expectError(); msg != "server error" {
		t.Fatalf("error %q", msg)
	}
	a.send(proto.TPing, nil)
	a.expect(proto.TPong)
	ps.history.Store(false)
	dial(t, s, "next", "")
}

// A panic resolving a turn ends that match as an error, whether the turn
// was resolved by a player's orders or by a timer.
func TestPanicInMatchEndsIt(t *testing.T) {
	s, ps, stop := newPanicServer(t, Config{TimeControls: soloControls, BackfillAfter: 10 * time.Millisecond, DraftSeconds: 1})
	defer stop()
	a, mf, ts := soloMatch(t, s, "unlucky")
	ps.snapshot.Store(true)
	a.send(proto.TOrders, proto.Orders{Match: mf.Match, Turn: ts.Turn})
	var me proto.MatchEnd
	_ = a.expect(proto.TMatchEnd).As(&me)
	if me.Result != "error" {
		t.Fatalf("orders: result %q", me.Result)
	}

	// The draft times out on a timer and starts play there.
	b := dial(t, s, "unluckier", "")
	b.send(proto.TQueue, proto.Queue{Mode: "casual"})
	b.expect(proto.TMatchFound)
	_ = b.expect(proto.TMatchEnd).As(&me)
	if me.Result != "error" {
		t.Fatalf("timer: result %q", me.Result)
	}
	dial(t, s, "survivor", "")
}

// A draft action that lands after the last pick, while play is starting,
// is refused (it read past the end of the draft order and panicked).
func TestDraftActionAfterLastPick(t *testing.T) {
	s, stop := newTestServer(t, Config{})
	defer stop()
	a, b := &Session{srv: s, matches: map[string]*Match{}}, &Session{srv: s, matches: map[string]*Match{}}
	m := &Match{srv: s, id: "mlate", phase: "draft", draft: newDraft(s.c.HeroIDs(), 2, 1),
		players: []*mplayer{{slot: 0, team: 0, sess: a}, {slot: 1, team: 1, sess: b}}}
	m.draft.i = len(m.draft.order)
	if err := m.draftAction(a, proto.DraftAction{Match: m.id, Kind: "pick", Hero: s.c.HeroIDs()[0]}); err == nil {
		t.Fatal("late draft action accepted")
	}
}

// A connection that never says hello is closed.
func TestHandshakeTimeout(t *testing.T) {
	s, stop := newTestServer(t, Config{HandshakeTimeout: 100 * time.Millisecond})
	defer stop()
	c := s.DialInternal()
	defer c.Close()
	_ = c.SetReadDeadline(time.Now().Add(3 * time.Second))
	for {
		if _, err := proto.Decode(c); err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				t.Fatal("silent connection still open")
			}
			return
		}
	}
}

// A frame bigger than any client sends closes the connection at once,
// from its length alone, before or after signing in.
func TestHugeFramesRefused(t *testing.T) {
	s, stop := newTestServer(t, Config{})
	defer stop()
	c := s.DialInternal()
	defer c.Close()
	var hdr [4]byte
	binary.BigEndian.PutUint32(hdr[:], proto.MaxFrame)
	if _, err := c.Write(hdr[:]); err != nil {
		t.Fatal(err)
	}
	_ = c.SetReadDeadline(time.Now().Add(3 * time.Second))
	f, err := proto.Decode(c)
	if err != nil || f.T != proto.TError {
		t.Fatalf("before hello: %v %+v", err, f)
	}

	a := dial(t, s, "biggy", "")
	binary.BigEndian.PutUint32(hdr[:], proto.MaxClientFrame+1)
	if _, err := a.c.Write(hdr[:]); err != nil {
		t.Fatal(err)
	}
	a.expectClosed()
}

// A client that stops reading is dropped once a few MB wait for it, not
// after 256 frames of any size.
func TestSlowReaderDroppedByBytes(t *testing.T) {
	a, b := net.Pipe()
	defer a.Close()
	c := newConn(b)
	big := proto.Chat{Text: strings.Repeat("x", 1<<20)}
	for i := 0; i < 20; i++ {
		if err := c.Send(proto.TChatMsg, big); err != nil {
			return
		}
	}
	t.Fatal("20 MB queued for a client that reads nothing")
}

// The frame pace: a burst at once, then rate a second.
func TestLimiter(t *testing.T) {
	t0 := time.Unix(1000, 0)
	l := limiter{rate: 10, burst: 3, tokens: 3, last: t0}
	for i := 0; i < 3; i++ {
		if d := l.delay(t0); d != 0 {
			t.Fatalf("burst frame %d waits %v", i, d)
		}
	}
	if d := l.delay(t0); d != 100*time.Millisecond {
		t.Fatalf("fourth frame waits %v, want 100ms", d)
	}
	if d := l.delay(t0.Add(100 * time.Millisecond)); d != 100*time.Millisecond {
		t.Fatalf("fifth frame waits %v, want 100ms", d)
	}
	if d := l.delay(t0.Add(time.Hour)); d != 0 {
		t.Fatalf("after a rest: %v", d)
	}
}

// Chat reaches other players' terminals: no escapes, no line breaks, and
// a long line is cut between characters.
func TestChatText(t *testing.T) {
	if got := chatText("gg\x1b]52;c;aGk=\x07\r\nwp\x9b2J"); got != "gg]52;c;aGk=wp2J" {
		t.Fatalf("got %q", got)
	}
	long := chatText(strings.Repeat("é", 150))
	if len(long) != 200 || !strings.HasSuffix(long, "é") {
		t.Fatalf("cut to %d bytes: %q", len(long), long[len(long)-4:])
	}
	if chatText("\x00\x01") != "" {
		t.Fatal("control-only line kept")
	}
}

// A guest who leaves is forgotten; a player with a match is kept for it.
func TestIdleSessionsForgotten(t *testing.T) {
	s, stop := newTestServer(t, Config{TimeControls: soloControls, BackfillAfter: 10 * time.Millisecond})
	defer stop()
	count := func() int {
		s.mu.Lock()
		defer s.mu.Unlock()
		return len(s.sessions)
	}
	for i := 0; i < 5; i++ {
		dial(t, s, "drive-by", "").c.Close()
	}
	a, _, _ := soloMatch(t, s, "stayer")
	a.c.Close()
	deadline := time.Now().Add(3 * time.Second)
	for count() != 1 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if n := count(); n != 1 {
		t.Fatalf("%d sessions kept, want the one in a match", n)
	}
}

// Shutting down stops every match first: orders arriving after that are
// refused rather than resolved into a turn the store may no longer keep.
func TestStoppedMatchRefusesOrders(t *testing.T) {
	c, err := data.Load()
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(context.Background(), "sqlite::memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	s := New(c, st, Config{TimeControls: soloControls, BackfillAfter: 10 * time.Millisecond, Logger: log.New(io.Discard, "", 0)})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { s.Run(ctx); close(done) }()
	a, mf, ts := soloMatch(t, s, "late")
	cancel()
	<-done
	a.send(proto.TOrders, proto.Orders{Match: mf.Match, Turn: ts.Turn})
	if msg := a.expectError(); !strings.Contains(msg, "restarting") {
		t.Fatalf("error %q", msg)
	}
	a.expectNo(proto.TResolved, 200*time.Millisecond)
}

// Daily matches pair at once and last for days: two players queueing in a
// loop are stopped at MaxLive games each.
func TestDailyMatchesCapped(t *testing.T) {
	s, stop := newTestServer(t, Config{})
	defer stop()
	a := dial(t, s, "loop-a", "")
	b := dial(t, s, "loop-b", "")
	for i := 0; i < MaxLive; i++ {
		a.send(proto.TQueue, proto.Queue{Mode: "24h", Clock: true})
		b.send(proto.TQueue, proto.Queue{Mode: "24h", Clock: true})
		a.expect(proto.TMatchFound)
		b.expect(proto.TMatchFound)
	}
	a.send(proto.TQueue, proto.Queue{Mode: "24h", Clock: true})
	if msg := a.expectError(); !strings.Contains(msg, "finish one first") {
		t.Fatalf("game %d: %q", MaxLive+1, msg)
	}
}
