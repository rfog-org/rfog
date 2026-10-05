// Package server is the game server: sessions, matchmaking, live matches.
// It is transport-agnostic; gateway listeners (TCP, SSH, WebSocket) hand it
// io.ReadWriteClosers speaking proto frames.
package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"sort"
	"strings"
	"sync"
	"time"

	"rfog/engine"
	"rfog/proto"
	"rfog/server/store"
)

// TimeControl describes one queue's clock and rules (SPEC §3.10).
type TimeControl struct {
	Turn     time.Duration // base time per turn
	Bank     time.Duration // cap on banked unused time (rapid); 0 = no banking
	Async    bool          // daily: deadlines persist, disconnects are normal, many at once
	Ranked   bool          // Glicko-2 ratings apply
	Backfill bool          // casual: bots fill empty seats after BackfillAfter
	Guests   bool          // guests may queue
	// Category is the rating pool ("" = the control's own id): clocks of
	// one speed share a rating, as a chess site groups 3+0 and 5+0 as blitz.
	Category string
}

// Clocks are the time controls, as a chess site offers them: seconds a
// turn (and a bank for the long ones), each in a rating category. Every
// clock can be played rated or casual (Queue.Rated): the rated control has
// the clock's id, the casual one the id plus CasualSuffix, where guests are
// welcome and bots fill empty seats after a wait.
var Clocks = []struct {
	ID       string
	Turn     time.Duration
	Bank     time.Duration
	Async    bool
	Category string
}{
	{"10s", 10 * time.Second, 0, false, "bullet"},
	{"15s", 15 * time.Second, 0, false, "bullet"},
	{"20s", 20 * time.Second, 0, false, "blitz"},
	{"30s", 30 * time.Second, 0, false, "blitz"},
	{"45s", 45 * time.Second, 0, false, "rapid"},
	{"60s", 60 * time.Second, 0, false, "rapid"},
	{"90s+", 90 * time.Second, 5 * time.Minute, false, "long-haul"},
	{"120s+", 120 * time.Second, 8 * time.Minute, false, "long-haul"},
	{"24h", 24 * time.Hour, 0, true, "daily"},
}

// CasualSuffix marks a clock's casual (unrated) control.
const CasualSuffix = "-casual"

// legacyControls are the time controls older clients ask for by name;
// they map onto the clocks (as rated or casual, as they meant).
var legacyControls = map[string]string{
	"bullet": "10s", "blitz": "30s", "rapid": "90s+", "daily": "24h", "casual": "60s" + CasualSuffix,
}

// DefaultTimeControls is every clock, rated and casual.
func DefaultTimeControls() map[string]TimeControl {
	out := map[string]TimeControl{}
	for _, c := range Clocks {
		out[c.ID] = TimeControl{Turn: c.Turn, Bank: c.Bank, Async: c.Async, Ranked: true, Category: c.Category}
		out[c.ID+CasualSuffix] = TimeControl{Turn: c.Turn, Bank: c.Bank, Async: c.Async, Backfill: !c.Async, Guests: true, Category: c.Category}
	}
	return out
}

// clocks lists the clocks this server offers (those in its config).
func (s *Server) clocks() []proto.ClockInfo {
	var out []proto.ClockInfo
	for _, c := range Clocks {
		if _, ok := s.cfg.TimeControls[c.ID]; ok {
			out = append(out, proto.ClockInfo{ID: c.ID, Turn: int(c.Turn / time.Second), Bank: int(c.Bank / time.Second), Category: c.Category, Async: c.Async})
		}
	}
	return out
}

// ratingKey is the rating pool a control's results go to.
func (s *Server) ratingKey(control string) string {
	if tc, ok := s.cfg.TimeControls[control]; ok && tc.Category != "" {
		return tc.Category
	}
	return control
}

// Config tunes the server. Zero values fall back to defaults.
type Config struct {
	MOTD          string
	DraftSeconds  int // per draft action (async controls use the turn time instead)
	GraceTurns    int // turns a disconnected player holds before forfeiting
	BackfillAfter time.Duration
	AbandonAfter  time.Duration // restored timed matches with nobody back end after this
	TimeControls  map[string]TimeControl
	// RatingWindow is the initial rating gap ranked 1v1 matchmaking accepts;
	// it widens by RatingWiden every second of waiting.
	RatingWindow float64
	RatingWiden  float64
	Logger       *log.Logger
}

func (c *Config) defaults() {
	if c.DraftSeconds == 0 {
		c.DraftSeconds = 60
	}
	if c.GraceTurns == 0 {
		c.GraceTurns = 2
	}
	if c.BackfillAfter == 0 {
		c.BackfillAfter = 60 * time.Second
	}
	if c.AbandonAfter == 0 {
		c.AbandonAfter = 2 * time.Minute
	}
	if c.TimeControls == nil {
		c.TimeControls = DefaultTimeControls()
	}
	if c.RatingWindow == 0 {
		c.RatingWindow = 150
	}
	if c.RatingWiden == 0 {
		c.RatingWiden = 10
	}
	if c.Logger == nil {
		c.Logger = log.Default()
	}
}

// Server holds all live state. One per process.
type Server struct {
	c     *engine.Content
	rules string // c.Fingerprint(), sent in every Welcome
	st    store.Store
	cfg   Config
	log   *log.Logger

	mu       sync.Mutex
	sessions map[string]*Session // player id -> session
	matches  map[string]*Match
	lobby    *lobby
	closed   bool
	// guards counts recent failed logins per account name (see guard).
	guards map[string]*guard
	// challenges are open challenges by code (see challenge.go).
	challenges map[string]*challenge
}

// New creates a server. Call Run to start the matchmaker.
func New(c *engine.Content, st store.Store, cfg Config) *Server {
	cfg.defaults()
	s := &Server{c: c, rules: c.Fingerprint(), st: st, cfg: cfg, log: cfg.Logger, sessions: map[string]*Session{}, matches: map[string]*Match{}, guards: map[string]*guard{}, challenges: map[string]*challenge{}}
	s.lobby = newLobby(s)
	return s
}

// Run restores live matches and runs the matchmaker until ctx ends.
func (s *Server) Run(ctx context.Context) error {
	if err := s.restore(ctx); err != nil {
		s.log.Printf("restore: %v", err)
	}
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			s.mu.Lock()
			s.closed = true
			var ms []*Match
			for _, m := range s.matches {
				ms = append(ms, m)
			}
			s.mu.Unlock()
			for _, m := range ms {
				m.stop()
			}
			return nil
		case <-t.C:
			s.lobby.tick()
			s.expireChallenges()
		}
	}
}

// Online is the number of connected sessions.
func (s *Server) Online() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, se := range s.sessions {
		if se.connected() {
			n++
		}
	}
	return n
}

// Serve runs one client connection to completion.
func (s *Server) Serve(rw io.ReadWriteCloser) {
	conn := newConn(rw)
	defer conn.Close()
	sess, err := s.handshake(conn)
	if err != nil {
		// Written directly: the queued writer might not drain before Close.
		_ = proto.Encode(rw, proto.TError, proto.Error{Code: "auth", Msg: err.Error()})
		return
	}
	sess.attach(conn)
	defer sess.detach(conn)
	for {
		f, err := conn.Recv()
		if err != nil {
			if !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrClosedPipe) {
				s.log.Printf("%s: connection closed: %v", sess.player.Name, err)
			}
			return
		}
		if err := sess.handle(f); err != nil {
			_ = conn.Send(proto.TError, proto.Error{Code: "bad_request", Msg: err.Error()})
		}
	}
}

// DialInternal returns a client-side connection to this server over an
// in-process pipe (used by SSH sessions).
func (s *Server) DialInternal() net.Conn {
	a, b := net.Pipe()
	go s.Serve(b)
	return a
}

func (s *Server) handshake(conn *Conn) (*Session, error) {
	f, err := conn.Recv()
	if err != nil {
		return nil, err
	}
	if f.T != proto.THello {
		return nil, errors.New("expected hello")
	}
	var h proto.Hello
	if err := f.As(&h); err != nil {
		return nil, err
	}
	if h.Version != proto.Version {
		return nil, fmt.Errorf("client protocol %d, server %d", h.Version, proto.Version)
	}
	f, err = conn.Recv()
	if err != nil {
		return nil, err
	}
	if f.T != proto.TAuth {
		return nil, errors.New("expected auth")
	}
	var a proto.Auth
	if err := f.As(&a); err != nil {
		return nil, err
	}
	ctx := context.Background()
	p, recovery, err := s.authenticate(ctx, a)
	if err != nil {
		return nil, err
	}
	_ = s.st.Touch(ctx, p.ID)
	sess := s.session(p)
	w := proto.Welcome{Player: p.ID, Name: p.Name, Guest: p.Guest, Token: p.Token, MOTD: s.cfg.MOTD, Online: s.Online() + 1, Rules: s.rules, Recovery: recovery, Clocks: s.clocks()}
	if h.Rules != s.rules {
		w.Content = s.c
	}
	if m := sess.currentMatch(); m != nil {
		w.InMatch = m.id
	}
	w.Live = sess.liveList()
	if rs, err := s.st.Ratings(ctx, p.ID); err == nil {
		w.Ratings = ratingInfos(rs)
	}
	// The last match, if it ended recently: a client opening after it
	// finished shows how it went, as a chess app does.
	if ms, err := s.st.History(ctx, p.ID, 1); err == nil && len(ms) == 1 && ms[0].Ended != nil && time.Since(*ms[0].Ended) < RecentMatch {
		h := historyMatch(ms[0], p.ID)
		w.Last = &h
	}
	return sess, conn.Send(proto.TWelcome, w)
}

// RecentMatch is how long after a match ends a returning player is shown
// its result.
const RecentMatch = 48 * time.Hour

// session returns the player's session, creating it if needed.
func (s *Server) session(p store.Player) *Session {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.sessions[p.ID]
	if !ok {
		sess = &Session{srv: s, player: p, matches: map[string]*Match{}}
		s.sessions[p.ID] = sess
	} else {
		sess.mu.Lock()
		sess.player = p // token may have rotated
		sess.mu.Unlock()
	}
	return sess
}

func ratingInfos(rs map[string]store.Rating) map[string]proto.RatingInfo {
	out := map[string]proto.RatingInfo{}
	for mode, r := range rs {
		out[mode] = proto.RatingInfo{Rating: r.Rating, RD: r.RD, Games: r.Games, Wins: r.Wins}
	}
	return out
}

func cleanName(n string) string {
	n = strings.TrimSpace(n)
	var b strings.Builder
	for _, r := range n {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-' || r == '.' {
			b.WriteRune(r)
		}
	}
	out := b.String()
	if len(out) > 16 {
		out = out[:16]
	}
	return out
}

// restore reloads in-progress matches from snapshots after a crash or
// restart. Players are disconnected; timed matches wait for them, async
// matches simply continue on their stored clocks.
func (s *Server) restore(ctx context.Context) error {
	snaps, metas, err := s.st.LiveMatches(ctx)
	if err != nil {
		return err
	}
	for i, sn := range snaps {
		var st engine.State
		if err := json.Unmarshal(sn.State, &st); err != nil {
			s.log.Printf("restore %s: bad state: %v", sn.MatchID, err)
			_ = s.st.DeleteSnapshot(ctx, sn.MatchID)
			continue
		}
		rp, err := engine.UnmarshalReplay(sn.Replay)
		if err != nil {
			s.log.Printf("restore %s: bad replay: %v", sn.MatchID, err)
			_ = s.st.DeleteSnapshot(ctx, sn.MatchID)
			continue
		}
		m := restoreMatch(s, metas[i], st, rp, sn.Clock)
		s.mu.Lock()
		s.matches[m.id] = m
		s.mu.Unlock()
		for _, mp := range m.players {
			if mp.bot != nil {
				continue
			}
			p, err := s.st.GetPlayer(ctx, mp.id)
			if err != nil {
				continue
			}
			sess := s.session(p)
			sess.addMatch(m)
			mp.sess = sess
		}
		m.startRestored()
		s.log.Printf("restored match %s at turn %d", m.id, st.Match.Turn)
	}
	return nil
}

// match returns a live match by id.
func (s *Server) match(id string) *Match {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.matches[id]
}

// lobbyList summarises every live match, newest ids last.
func (s *Server) lobbyList() []proto.LobbyMatch {
	s.mu.Lock()
	var ms []*Match
	for _, m := range s.matches {
		ms = append(ms, m)
	}
	s.mu.Unlock()
	out := make([]proto.LobbyMatch, 0, len(ms))
	for _, m := range ms {
		out = append(out, m.lobbyEntry())
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Match < out[j].Match })
	return out
}

func (s *Server) removeMatch(id string) {
	s.mu.Lock()
	delete(s.matches, id)
	s.mu.Unlock()
}

// Conn is one framed connection. Writes go through a buffered queue and a
// writer goroutine so that match logic never blocks on a slow client; a
// client that falls too far behind is dropped.
type Conn struct {
	rw     io.ReadWriteCloser
	out    chan []byte
	closed chan struct{}
	once   sync.Once
}

func newConn(rw io.ReadWriteCloser) *Conn {
	c := &Conn{rw: rw, out: make(chan []byte, 256), closed: make(chan struct{})}
	go c.writer()
	return c
}

func (c *Conn) writer() {
	for {
		select {
		case <-c.closed:
			return
		case b := <-c.out:
			if b == nil { // closeAfter: everything before it is written
				c.Close()
				return
			}
			if _, err := c.rw.Write(b); err != nil {
				c.Close()
				return
			}
		}
	}
}

func (c *Conn) Send(t string, body any) error {
	var buf bytes.Buffer
	if err := proto.Encode(&buf, t, body); err != nil {
		return err
	}
	select {
	case <-c.closed:
		return io.ErrClosedPipe
	case c.out <- buf.Bytes():
		return nil
	default:
		c.Close()
		return errors.New("client too slow")
	}
}

// closeAfter closes the connection once what is queued has been written,
// so a last message (why it is closing) reaches the client.
func (c *Conn) closeAfter() {
	select {
	case c.out <- nil:
	default:
		c.Close()
	}
}

func (c *Conn) Recv() (proto.Frame, error) { return proto.Decode(c.rw) }

func (c *Conn) Close() {
	c.once.Do(func() {
		close(c.closed)
		_ = c.rw.Close()
	})
}
