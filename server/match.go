package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"sort"
	"sync"
	"time"

	"rfog/bots"
	"rfog/engine"
	"rfog/proto"
	"rfog/server/rating"
	"rfog/server/store"
)

// mplayer is one seat in a live match: a session (human) or a bot.
type mplayer struct {
	slot, team int
	id, name   string
	hero       string
	sess       *Session
	bot        *bots.Bot
	discTurns  int          // consecutive turns spent disconnected
	left       bool         // quit a team match; the seat holds from here on
	before     store.Rating // rating when the match started (ranked)
}

func (p *mplayer) human() bool { return p.bot == nil }

func (p *mplayer) online() bool { return p.sess != nil && p.sess.connected() }

// clock is the per-player turn clock. Each human has their own deadline:
// the base turn time plus whatever they banked (rapid). It is snapshotted
// so async matches keep their deadlines across restarts.
type clock struct {
	Start    time.Time             `json:"start"`
	Deadline map[int]time.Time     `json:"deadline"`
	Bank     map[int]time.Duration `json:"bank"`
}

// Match is a live match on the server. All state changes happen under mu;
// timers call back into it.
type Match struct {
	srv     *Server
	id      string
	mode    string // 1v1 .. 5v5
	timeCtl string
	tc      TimeControl
	players []*mplayer

	mu         sync.Mutex
	phase      string // draft | play | ended
	spectators map[*Session]bool
	// prev is the state spectators of a ranked match see: one turn behind.
	prev       engine.State
	prevEvents []engine.Event
	prevTurn   int
	draft      *draft
	setup      engine.Setup
	s          engine.State
	replay     *engine.Replay
	orders     map[int][]engine.Order
	commit     map[int]bool
	clk        clock
	deadline   time.Time // draft deadline
	timer      *time.Timer
	stopped    bool
}

// startMatch seats the sessions (plus nbots bots) and begins the draft.
// Seats [0,n) are team 0 and [n,2n) team 1, as the engine assigns them.
func (s *Server) startMatch(control, size string, sessions []*Session, nbots int) {
	heroes := s.c.HeroIDs()
	per := s.c.Rules.Modes[size].PlayersPerTeam
	m := &Match{srv: s, id: newMatchID(), mode: size, timeCtl: control, tc: s.cfg.TimeControls[control],
		orders: map[int][]engine.Order{}, commit: map[int]bool{}, spectators: map[*Session]bool{}}
	slot := 0
	for _, se := range sessions {
		m.players = append(m.players, &mplayer{slot: slot, team: slot / per, id: se.player.ID, name: se.player.Name, sess: se})
		slot++
	}
	for i := 0; i < nbots; i++ {
		seed := uint64(time.Now().UnixNano()) + uint64(i)
		m.players = append(m.players, &mplayer{slot: slot, team: slot / per, id: "bot", name: "bot", bot: bots.New("normal", seed)})
		slot++
	}
	s.mu.Lock()
	s.matches[m.id] = m
	s.mu.Unlock()
	for _, p := range m.players {
		if p.sess != nil {
			p.sess.addMatch(m)
		}
	}
	m.mu.Lock()
	m.phase = "draft"
	m.draft = newDraft(heroes, len(m.players), per)
	m.broadcastLocked(proto.TMatchFound, m.foundMsg(-1), true)
	m.mu.Unlock()
	m.advanceDraft()
	s.log.Printf("match %s: %s %s, %d players (%d bots)", m.id, control, size, len(m.players), nbots)
}

func newMatchID() string {
	return fmt.Sprintf("m%08x", rand.Uint32())
}

func (m *Match) foundMsg(you int) proto.MatchFound {
	mf := proto.MatchFound{Match: m.id, Mode: m.mode, Time: m.timeCtl, Async: m.tc.Async, You: you}
	for _, p := range m.players {
		mf.Players = append(mf.Players, proto.MatchPlayer{ID: p.id, Name: p.name, Team: p.team, Slot: p.slot, Bot: !p.human()})
	}
	return mf
}

// broadcastLocked sends to every human. perPlayer=true rewrites MatchFound.You.
func (m *Match) broadcastLocked(t string, body any, perPlayer bool) {
	for _, p := range m.players {
		if p.sess == nil {
			continue
		}
		if perPlayer {
			if mf, ok := body.(proto.MatchFound); ok {
				mf.You = p.slot
				p.sess.send(t, mf)
				continue
			}
		}
		p.sess.send(t, body)
	}
}

func (m *Match) seat(se *Session) *mplayer {
	for _, p := range m.players {
		if p.sess == se {
			return p
		}
	}
	return nil
}

// summary describes the match to one of its players (for live lists).
func (m *Match) summary(se *Session) proto.LiveMatch {
	m.mu.Lock()
	defer m.mu.Unlock()
	lm := proto.LiveMatch{Match: m.id, Mode: m.mode, Time: m.timeCtl, Phase: m.phase}
	p := m.seat(se)
	if p == nil {
		return lm
	}
	lm.Versus = versus(m.players, p.team)
	switch m.phase {
	case "draft":
		lm.Deadline = m.deadline
		lm.YourTurn = !m.draft.done() && m.draft.order[m.draft.i].Slot == p.slot
	case "play":
		lm.Turn = m.s.Match.Turn
		lm.Deadline = m.clk.Deadline[p.slot]
		lm.YourTurn = !m.commit[p.slot]
	}
	return lm
}

// ---- draft --------------------------------------------------------------

// draft is the ban/pick sequence. 1v1: A bans, B bans, A picks, B picks.
// Team modes: one ban per team (its first seat), then picks snake between
// teams (A B B A A B ...), each team filling its seats in order. Mirror
// picks within a team are illegal; across teams they are fine.
type draft struct {
	heroes []string
	banned []string
	picks  map[int]string
	order  []proto.DraftStep
	teamOf func(slot int) int
	i      int
}

func newDraft(heroes []string, n, per int) *draft {
	d := &draft{heroes: heroes, picks: map[int]string{}, teamOf: func(slot int) int { return slot / per }}
	d.order = append(d.order, proto.DraftStep{Slot: 0, Kind: "ban"}, proto.DraftStep{Slot: per, Kind: "ban"})
	next := []int{0, per} // next seat to pick per team
	for i := 0; i < n; i++ {
		team := 0
		if i%4 == 1 || i%4 == 2 {
			team = 1
		}
		if next[team] >= (team+1)*per { // team full: the other team picks
			team = 1 - team
		}
		d.order = append(d.order, proto.DraftStep{Slot: next[team], Kind: "pick"})
		next[team]++
	}
	return d
}

func (d *draft) done() bool { return d.i >= len(d.order) }

// legal reports whether slot may take hero now.
func (d *draft) legal(slot int, hero string) bool {
	for _, b := range d.banned {
		if b == hero {
			return false
		}
	}
	for s, h := range d.picks {
		if h == hero && d.teamOf(s) == d.teamOf(slot) {
			return false
		}
	}
	for _, h := range d.heroes {
		if h == hero {
			return true
		}
	}
	return false
}

func (d *draft) state(id string, deadline time.Time) proto.DraftState {
	st := proto.DraftState{Match: id, Heroes: d.heroes, Banned: d.banned, Picks: d.picks, Order: d.order, Deadline: deadline, Kind: "done", Turn: -1}
	if !d.done() {
		st.Kind, st.Turn = d.order[d.i].Kind, d.order[d.i].Slot
	}
	return st
}

func (m *Match) draftSeconds() time.Duration {
	if m.tc.Async {
		return m.tc.Turn
	}
	return time.Duration(m.srv.cfg.DraftSeconds) * time.Second
}

// advanceDraft runs bot steps, arms the timer for humans, or starts play.
func (m *Match) advanceDraft() {
	m.mu.Lock()
	for !m.draft.done() {
		step := m.draft.order[m.draft.i]
		p := m.players[step.Slot]
		if p.human() {
			break
		}
		m.applyDraftLocked(step.Slot, step.Kind, m.randomHeroLocked(step.Slot))
	}
	if m.draft.done() {
		m.mu.Unlock()
		m.startPlay()
		return
	}
	m.deadline = time.Now().Add(m.draftSeconds())
	m.armTimerLocked(m.deadline, m.draftTimeout)
	m.broadcastLocked(proto.TDraftState, m.draft.state(m.id, m.deadline), false)
	m.mu.Unlock()
}

func (m *Match) randomHeroLocked(slot int) string {
	var legal []string
	for _, h := range m.draft.heroes {
		if m.draft.legal(slot, h) {
			legal = append(legal, h)
		}
	}
	if len(legal) == 0 {
		return m.draft.heroes[0]
	}
	if p := m.players[slot]; p.sess != nil {
		p.sess.mu.Lock()
		pref := p.sess.hero
		p.sess.mu.Unlock()
		if pref != "" && m.draft.legal(slot, pref) {
			return pref
		}
	}
	return legal[rand.Intn(len(legal))]
}

func (m *Match) applyDraftLocked(slot int, kind, hero string) {
	switch kind {
	case "ban":
		m.draft.banned = append(m.draft.banned, hero)
	case "pick":
		m.draft.picks[slot] = hero
		m.players[slot].hero = hero
	}
	m.draft.i++
}

func (m *Match) draftAction(se *Session, d proto.DraftAction) error {
	m.mu.Lock()
	if m.phase != "draft" {
		m.mu.Unlock()
		return errors.New("not drafting")
	}
	p := m.seat(se)
	step := m.draft.order[m.draft.i]
	if p == nil || step.Slot != p.slot {
		m.mu.Unlock()
		return errors.New("not your turn")
	}
	if step.Kind != d.Kind {
		m.mu.Unlock()
		return fmt.Errorf("expected %s", step.Kind)
	}
	if !m.draft.legal(p.slot, d.Hero) {
		m.mu.Unlock()
		return errors.New("hero not available")
	}
	m.applyDraftLocked(p.slot, d.Kind, d.Hero)
	m.mu.Unlock()
	m.advanceDraft()
	return nil
}

func (m *Match) draftTimeout() {
	m.mu.Lock()
	if m.phase != "draft" || m.draft.done() {
		m.mu.Unlock()
		return
	}
	step := m.draft.order[m.draft.i]
	m.applyDraftLocked(step.Slot, step.Kind, m.randomHeroLocked(step.Slot))
	m.mu.Unlock()
	m.advanceDraft()
}

// ---- play ---------------------------------------------------------------

func (m *Match) startPlay() {
	m.mu.Lock()
	defer m.mu.Unlock()
	ctx := context.Background()
	m.setup = engine.Setup{ID: m.id, Mode: m.mode, Seed: uint64(time.Now().UnixNano()), TimeControl: m.timeCtl}
	for _, p := range m.players {
		name := p.name
		if !p.human() {
			name = "bot " + p.hero
			p.name = name
		}
		m.setup.Players = append(m.setup.Players, engine.SetupPlayer{Name: name, Hero: p.hero})
	}
	s, err := engine.NewMatch(m.srv.c, m.setup)
	if err != nil {
		m.srv.log.Printf("match %s: %v", m.id, err)
		m.endLocked(-1, "error", err.Error())
		return
	}
	m.s = s
	m.replay = engine.NewReplay(m.setup, s)
	rec := store.Match{ID: m.id, Mode: m.mode, Map: s.Match.Map, TimeCtl: m.timeCtl, Seed: m.setup.Seed, Started: time.Now()}
	for _, p := range m.players {
		mp := store.MatchPlayer{PlayerID: p.id, Name: p.name, Slot: p.slot, Team: p.team, Hero: p.hero, Bot: !p.human()}
		if m.tc.Ranked && p.human() {
			p.before = m.srv.ratingFor(ctx, p.id, m.timeCtl)
			r := p.before.Rating
			mp.RatingBefore = &r
		}
		rec.Players = append(rec.Players, mp)
	}
	if err := m.srv.st.CreateMatch(ctx, rec); err != nil {
		m.srv.log.Printf("match %s: store: %v", m.id, err)
	}
	m.phase = "play"
	m.clk = clock{Deadline: map[int]time.Time{}, Bank: map[int]time.Duration{}}
	m.broadcastLocked(proto.TMatchFound, m.foundMsg(-1), true)
	m.beginTurnLocked()
}

// restoreMatch rebuilds a match from a snapshot; players are attached by the caller.
func restoreMatch(s *Server, meta store.Match, st engine.State, rp *engine.Replay, clk []byte) *Match {
	m := &Match{srv: s, id: meta.ID, mode: meta.Mode, timeCtl: meta.TimeCtl, tc: s.cfg.TimeControls[meta.TimeCtl], orders: map[int][]engine.Order{}, commit: map[int]bool{},
		spectators: map[*Session]bool{}, setup: rp.Setup, s: st, replay: rp, phase: "play"}
	for _, p := range meta.Players {
		mp := &mplayer{slot: p.Slot, team: p.Team, id: p.PlayerID, name: p.Name, hero: p.Hero}
		if p.Bot {
			mp.bot = bots.New("normal", rp.Setup.Seed+uint64(p.Slot)*7919)
		}
		if mp.name == "" && p.Slot < len(st.Players) {
			mp.name = st.Players[p.Slot].Name
		}
		if p.RatingBefore != nil {
			mp.before = s.ratingFor(context.Background(), p.PlayerID, meta.TimeCtl)
			mp.before.Rating = *p.RatingBefore
		}
		m.players = append(m.players, mp)
	}
	sort.Slice(m.players, func(i, j int) bool { return m.players[i].slot < m.players[j].slot })
	if m.tc.Turn == 0 { // control no longer configured: keep the match playable
		m.tc.Turn = time.Minute
	}
	if len(clk) > 0 {
		_ = json.Unmarshal(clk, &m.clk)
	}
	if m.clk.Deadline == nil {
		m.clk.Deadline = map[int]time.Time{}
	}
	if m.clk.Bank == nil {
		m.clk.Bank = map[int]time.Duration{}
	}
	return m
}

// startRestored resumes a restored match. Async matches continue on their
// stored clocks; timed matches wait for players to come back and are
// abandoned if nobody does in time.
func (m *Match) startRestored() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.tc.Async && !m.clk.Start.IsZero() {
		m.armClockLocked()
		return
	}
	m.deadline = time.Now().Add(m.srv.cfg.AbandonAfter)
	m.armTimerLocked(m.deadline, func() {
		m.mu.Lock()
		defer m.mu.Unlock()
		if m.phase != "play" {
			return
		}
		anyone := false
		for _, p := range m.players {
			if p.online() {
				anyone = true
			}
		}
		if !anyone {
			m.endLocked(-1, "abandoned", "server restarted and nobody returned")
			return
		}
		m.beginTurnLocked()
	})
}

// beginTurnLocked resets orders, starts every human's clock and tells them.
func (m *Match) beginTurnLocked() {
	m.orders = map[int][]engine.Order{}
	m.commit = map[int]bool{}
	m.clk.Start = time.Now()
	m.clk.Deadline = map[int]time.Time{}
	for _, p := range m.players {
		if p.human() {
			m.clk.Deadline[p.slot] = m.clk.Start.Add(m.tc.Turn + m.clk.Bank[p.slot])
		}
	}
	m.snapshotLocked()
	m.armClockLocked()
	for _, p := range m.players {
		if p.sess != nil {
			p.sess.send(proto.TTurnStart, m.turnStartLocked(p, false))
		}
	}
}

func (m *Match) turnStartLocked(p *mplayer, resumed bool) proto.TurnStart {
	return proto.TurnStart{Match: m.id, Turn: m.s.Match.Turn, Deadline: m.clk.Deadline[p.slot], Bank: int(m.clk.Bank[p.slot].Seconds()),
		View: engine.View(m.srv.c, m.s, p.slot), Resumed: resumed}
}

// blocking reports whether the turn is waiting on this seat: a human who
// has not committed and is either online or playing async.
func (m *Match) blocking(p *mplayer) bool {
	return p.human() && !p.left && !m.commit[p.slot] && (p.online() || m.tc.Async)
}

// armClockLocked arms the timer for the earliest deadline still awaited.
func (m *Match) armClockLocked() {
	var at time.Time
	for _, p := range m.players {
		if !p.human() || p.left || m.commit[p.slot] {
			continue
		}
		d := m.clk.Deadline[p.slot]
		if at.IsZero() || d.Before(at) {
			at = d
		}
	}
	if at.IsZero() {
		at = time.Now()
	}
	m.armTimerLocked(at, m.turnTimeout)
}

func (m *Match) armTimerLocked(at time.Time, f func()) {
	if m.timer != nil {
		m.timer.Stop()
	}
	if m.stopped {
		return
	}
	m.timer = time.AfterFunc(time.Until(at), f)
}

func (m *Match) submitOrders(se *Session, o proto.Orders) error {
	m.mu.Lock()
	if m.phase != "play" {
		m.mu.Unlock()
		return errors.New("match not in play")
	}
	p := m.seat(se)
	if p == nil {
		m.mu.Unlock()
		return errors.New("not seated")
	}
	if o.Turn != m.s.Match.Turn {
		m.mu.Unlock()
		return fmt.Errorf("orders for turn %d, match is on %d", o.Turn, m.s.Match.Turn)
	}
	if m.commit[p.slot] {
		m.mu.Unlock()
		return errors.New("orders already committed")
	}
	if errs := engine.Validate(m.srv.c, &m.s, p.slot, o.Orders); len(errs) > 0 {
		m.mu.Unlock()
		return errs[0]
	}
	m.orders[p.slot] = o.Orders
	m.commit[p.slot] = true
	// Banking (rapid): unused base time is saved, capped; time spent past
	// the base comes out of the bank.
	if m.tc.Bank > 0 {
		used := time.Since(m.clk.Start)
		bank := m.clk.Bank[p.slot] + (m.tc.Turn - used)
		if bank > m.tc.Bank {
			bank = m.tc.Bank
		}
		if bank < 0 {
			bank = 0
		}
		m.clk.Bank[p.slot] = bank
	}
	m.mu.Unlock()
	m.maybeResolve()
	return nil
}

// turnTimeout fires at the earliest deadline: everyone past theirs holds.
func (m *Match) turnTimeout() {
	m.mu.Lock()
	if m.phase != "play" {
		m.mu.Unlock()
		return
	}
	now := time.Now()
	for _, p := range m.players {
		if !p.human() || m.commit[p.slot] {
			continue
		}
		if !m.clk.Deadline[p.slot].After(now) {
			m.commit[p.slot] = true
			m.clk.Bank[p.slot] = 0
		}
	}
	m.mu.Unlock()
	m.maybeResolve()
}

// maybeResolve steps the turn if nobody is still being waited on, else
// re-arms the clock for the next deadline.
func (m *Match) maybeResolve() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.phase != "play" {
		return
	}
	for _, p := range m.players {
		if m.blocking(p) {
			m.armClockLocked()
			return
		}
	}
	m.resolveLocked()
}

// resolveLocked steps the engine once and tells everyone what happened.
func (m *Match) resolveLocked() {
	if m.timer != nil {
		m.timer.Stop()
	}
	for _, p := range m.players {
		if !p.human() {
			v := engine.View(m.srv.c, m.s, p.slot)
			m.orders[p.slot] = p.bot.Orders(m.srv.c, &v, p.slot)
		}
	}
	m.replay.Record(m.orders)
	var events []engine.Event
	m.s, events = engine.Step(m.srv.c, m.s, m.orders, m.s.Match.Seed)
	// Disconnect bookkeeping: a human who spent this whole turn away holds;
	// once every human on a team has been away GraceTurns the team forfeits.
	// Async matches never count absence.
	for _, p := range m.players {
		if !p.human() || m.tc.Async {
			continue
		}
		if p.online() {
			p.discTurns = 0
		} else {
			p.discTurns++
		}
	}
	for _, p := range m.players {
		if p.sess == nil {
			continue
		}
		v := engine.View(m.srv.c, m.s, p.slot)
		p.sess.send(proto.TResolved, proto.Resolved{Match: m.id, Turn: v.Match.Turn, Events: engine.FilterEvents(m.srv.c, &m.s, p.team, events), View: v, Ended: m.s.Ended()})
	}
	m.pushSpectatorsLocked(events)
	if m.s.Ended() {
		m.endLocked(m.s.Match.Winner, m.s.Match.Result, "")
		return
	}
	if !m.tc.Async {
		gone := [2]bool{true, true}
		humans := [2]bool{}
		for _, p := range m.players {
			if !p.human() {
				continue
			}
			humans[p.team] = true
			if p.discTurns < m.srv.cfg.GraceTurns {
				gone[p.team] = false
			}
		}
		for t := 0; t < 2; t++ {
			if humans[t] && gone[t] {
				if humans[1-t] && gone[1-t] {
					m.endLocked(-1, "abandoned", "everyone disconnected")
				} else {
					m.endLocked(1-t, "forfeit", "team "+fmt.Sprint(t)+" disconnected")
				}
				return
			}
		}
	}
	m.beginTurnLocked()
}

// ---- spectators ---------------------------------------------------------

// specDelayed reports whether spectators of this match lag one turn.
// Ranked matches are delayed so a spectator cannot relay fog information
// to a player in real time; unranked matches stream live.
func (m *Match) specDelayed() bool { return m.tc.Ranked }

// specStateLocked builds the frame spectators may see right now.
func (m *Match) specStateLocked() (proto.SpecState, bool) {
	st := proto.SpecState{Match: m.id, Mode: m.mode, Time: m.timeCtl, Live: m.s.Match.Turn, Delayed: m.specDelayed()}
	for _, p := range m.players {
		st.Players = append(st.Players, proto.MatchPlayer{ID: p.id, Name: p.name, Team: p.team, Slot: p.slot, Bot: !p.human()})
	}
	if m.phase == "draft" {
		return st, false
	}
	// prev is the newest frame spectators are allowed to see: for a delayed
	// (ranked) match it lags one turn behind the players, for an unranked
	// one it is the turn that just resolved.
	src, turn, events := m.prev, m.prevTurn, m.prevEvents
	if m.phase == "ended" {
		src, turn, events = m.s, m.s.Match.Turn, nil
	} else if turn == 0 {
		if m.specDelayed() {
			return st, false // nothing has resolved yet; showing the start would be live
		}
		src, turn, events = m.s, m.s.Match.Turn, nil
	}
	v := src.Clone()
	v.Visible = nil // spectators see the whole board
	v.Log = nil
	st.View, st.Turn, st.Events = v, turn, events
	st.Ended = m.phase == "ended"
	st.Result = m.s.Match.Result
	return st, true
}

// pushSpectatorsLocked sends the latest allowed frame to every spectator.
func (m *Match) pushSpectatorsLocked(events []engine.Event) {
	remember := func() { m.prev, m.prevEvents, m.prevTurn = m.s.Clone(), events, m.s.Match.Turn }
	if !m.specDelayed() {
		remember() // unranked: what just happened is publishable at once
	} else {
		defer remember() // ranked: publish the previous turn, then keep this one
	}
	if len(m.spectators) == 0 {
		return
	}
	st, ok := m.specStateLocked()
	if !ok {
		return
	}
	for se := range m.spectators {
		se.send(proto.TSpecState, st)
	}
}

// addSpectator attaches a watcher and sends it the current frame.
func (m *Match) addSpectator(se *Session) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.seat(se) != nil {
		return // players watch their own match by playing it
	}
	m.spectators[se] = true
	if st, ok := m.specStateLocked(); ok {
		se.send(proto.TSpecState, st)
	}
}

func (m *Match) removeSpectator(se *Session) {
	m.mu.Lock()
	delete(m.spectators, se)
	m.mu.Unlock()
}

// lobbyEntry summarises the match for the spectate list.
func (m *Match) lobbyEntry() proto.LobbyMatch {
	m.mu.Lock()
	defer m.mu.Unlock()
	e := proto.LobbyMatch{Match: m.id, Mode: m.mode, Time: m.timeCtl, Phase: m.phase, Delayed: m.specDelayed()}
	if m.phase == "play" {
		e.Turn = m.s.Match.Turn
		if len(m.s.Teams) >= 2 {
			e.Score = [2]int{m.s.Teams[0].Score, m.s.Teams[1].Score}
		}
	}
	for _, p := range m.players {
		name := p.name
		if p.hero != "" {
			name += " (" + p.hero + ")"
		}
		e.Players = append(e.Players, name)
		if !p.human() {
			e.Bots = true
		}
	}
	return e
}

func (m *Match) snapshotLocked() {
	st, err := json.Marshal(m.s)
	if err != nil {
		return
	}
	rp, err := m.replay.Marshal()
	if err != nil {
		return
	}
	clk, _ := json.Marshal(m.clk)
	if err := m.srv.st.SaveSnapshot(context.Background(), store.Snapshot{MatchID: m.id, Turn: m.s.Match.Turn, State: st, Replay: rp, Clock: clk}); err != nil {
		m.srv.log.Printf("match %s: snapshot: %v", m.id, err)
	}
}

func (m *Match) endLocked(winner int, result, reason string) {
	if m.phase == "ended" {
		return
	}
	m.phase = "ended"
	if m.timer != nil {
		m.timer.Stop()
	}
	// Unregister first: nobody may join or start watching a match that is
	// on its way out (its last frames are still going to those already in).
	m.srv.removeMatch(m.id)
	m.s.Match.Phase = engine.PhaseEnded
	m.s.Match.Winner = winner
	m.s.Match.Result = result
	var rp []byte
	if m.replay != nil {
		m.replay.FinalHash = engine.Hash(m.s)
		rp, _ = m.replay.Marshal()
	}
	ctx := context.Background()
	if err := m.srv.st.EndMatch(ctx, m.id, winner, result, rp); err != nil {
		m.srv.log.Printf("match %s: end: %v", m.id, err)
	}
	deltas := m.rateLocked(ctx, winner, result)
	final := m.s.Clone()
	final.Visible = nil
	if len(m.spectators) > 0 {
		st, ok := m.specStateLocked()
		if ok {
			for se := range m.spectators {
				se.send(proto.TSpecState, st)
				se.stopSpectating(m)
			}
		}
		m.spectators = map[*Session]bool{}
	}
	for _, p := range m.players {
		if p.sess != nil {
			p.sess.send(proto.TMatchEnd, proto.MatchEnd{Match: m.id, Winner: winner, Result: result, Final: final, Reason: reason, Ratings: deltas, Replay: rp})
			p.sess.removeMatch(m)
		}
	}
	m.srv.log.Printf("match %s: ended, winner %d by %s %s", m.id, winner, result, reason)
}

// rateLocked applies Glicko-2 after a ranked match: each human plays one
// result against a composite of the enemy humans (mean rating and RD).
// Matches that never really happened (abandoned, error) are not rated.
func (m *Match) rateLocked(ctx context.Context, winner int, result string) map[int]proto.RatingDelta {
	if !m.tc.Ranked || result == "abandoned" || result == "error" || m.replay == nil {
		return nil
	}
	var teams [2][]*mplayer
	for _, p := range m.players {
		if p.human() {
			teams[p.team] = append(teams[p.team], p)
		}
	}
	if len(teams[0]) == 0 || len(teams[1]) == 0 {
		return nil
	}
	composite := func(ps []*mplayer) rating.Rating {
		var r, rd float64
		for _, p := range ps {
			r += p.before.Rating
			rd += p.before.RD
		}
		return rating.Rating{R: r / float64(len(ps)), RD: rd / float64(len(ps))}
	}
	deltas := map[int]proto.RatingDelta{}
	for t := 0; t < 2; t++ {
		opp := composite(teams[1-t])
		score := 0.5
		if winner == t {
			score = 1
		} else if winner == 1-t {
			score = 0
		}
		for _, p := range teams[t] {
			old := rating.Rating{R: p.before.Rating, RD: p.before.RD, Vol: p.before.Vol}
			nu := rating.Update(old, []rating.Result{{Opp: opp, Score: score}})
			rec := store.Rating{Rating: nu.R, RD: nu.RD, Vol: nu.Vol, Games: p.before.Games + 1, Wins: p.before.Wins}
			if score == 1 {
				rec.Wins++
			}
			if err := m.srv.st.SetRating(ctx, p.id, m.srv.ratingKey(m.timeCtl), rec); err != nil {
				m.srv.log.Printf("match %s: rating %s: %v", m.id, p.id, err)
			}
			if err := m.srv.st.SetMatchRating(ctx, m.id, p.slot, old.R, nu.R); err != nil {
				m.srv.log.Printf("match %s: rating %s: %v", m.id, p.id, err)
			}
			deltas[p.slot] = proto.RatingDelta{Before: old.R, After: nu.R}
		}
	}
	return deltas
}

func (m *Match) stop() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stopped = true
	if m.timer != nil {
		m.timer.Stop()
	}
}

// ---- connection events --------------------------------------------------

// reconnect resends everything a (re)joining player needs to continue.
func (m *Match) reconnect(se *Session) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p := m.seat(se)
	if p == nil {
		return
	}
	se.send(proto.TMatchFound, m.foundMsg(p.slot))
	switch m.phase {
	case "draft":
		se.send(proto.TDraftState, m.draft.state(m.id, m.deadline))
	case "play":
		se.send(proto.TTurnStart, m.turnStartLocked(p, m.commit[p.slot]))
	}
}

func (m *Match) disconnect(se *Session) {
	// Timed matches: resolve() counts turns spent away and the clock still
	// runs. If every human is gone during a timed draft, end it. Async
	// matches do not care.
	m.mu.Lock()
	if m.tc.Async {
		m.mu.Unlock()
		return
	}
	defer m.mu.Unlock()
	if m.phase != "draft" {
		return
	}
	for _, p := range m.players {
		if p.human() && p.online() {
			return
		}
	}
	m.endLocked(-1, "abandoned", "everyone left the draft")
}

// leave is an explicit exit: in 1v1 the opponent wins by forfeit; in team
// modes the seat keeps holding for the rest of the match.
func (m *Match) leave(se *Session) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p := m.seat(se)
	if p == nil || m.phase == "ended" {
		return
	}
	if len(m.players) == 2 {
		m.endLocked(1-p.team, "forfeit", p.name+" left")
		return
	}
	p.sess.removeMatch(m)
	p.sess = nil
	p.left = true
	if m.phase == "play" {
		go m.maybeResolve()
	}
}

func (m *Match) chat(se *Session, text string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(text) > 200 {
		text = text[:200]
	}
	msg := proto.Chat{Match: m.id, From: se.player.Name, Text: text}
	if m.seat(se) == nil {
		msg.From = se.player.Name + " (watching)"
	}
	m.broadcastLocked(proto.TChatMsg, msg, false)
	for sp := range m.spectators {
		sp.send(proto.TChatMsg, msg)
	}
}
