package server

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"rfog/proto"
	"rfog/server/store"
)

// Session is one authenticated player. It outlives connections: a player
// who drops and reconnects gets the same session and rejoins their match.
// A session holds at most one timed match but any number of async (daily)
// matches.
type Session struct {
	srv    *Server
	player store.Player

	mu       sync.Mutex
	conn     *Conn
	matches  map[string]*Match
	queue    string    // lobby key being queued for, "" if none
	since    time.Time // queue start
	hero     string    // preferred hero for the draft
	rating   float64   // rating in the queued control (matchmaking)
	watching *Match    // match being spectated, if any
}

func (se *Session) attach(c *Conn) {
	se.mu.Lock()
	old := se.conn
	se.conn = c
	se.mu.Unlock()
	if old != nil {
		// One place at a time: the newer device takes over, and the older
		// one is told why, so it does not reconnect and take it back.
		_ = old.Send(proto.TError, proto.Error{Code: proto.ErrDisplaced, Msg: "continued on another device"})
		old.closeAfter()
	}
	if m := se.currentMatch(); m != nil {
		m.reconnect(se)
	}
}

func (se *Session) detach(c *Conn) {
	se.mu.Lock()
	if se.conn != c {
		se.mu.Unlock()
		return
	}
	se.conn = nil
	ms := se.matchList()
	w := se.watching
	se.watching = nil
	se.mu.Unlock()
	if w != nil {
		w.removeSpectator(se)
	}
	se.srv.lobby.leave(se)
	for _, m := range ms {
		m.disconnect(se)
	}
}

func (se *Session) connected() bool {
	se.mu.Lock()
	defer se.mu.Unlock()
	return se.conn != nil
}

// send delivers a message if a connection is attached; a dropped player
// simply misses it and catches up on reconnect.
func (se *Session) send(t string, body any) {
	se.mu.Lock()
	c := se.conn
	se.mu.Unlock()
	if c == nil {
		return
	}
	if err := c.Send(t, body); err != nil {
		c.Close()
	}
}

// matchList returns the session's matches in a stable order (caller holds mu).
func (se *Session) matchList() []*Match {
	var out []*Match
	for _, m := range se.matches {
		out = append(out, m)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].id < out[j].id })
	return out
}

// currentMatch is the timed match the player is in, if any. Async matches
// never count: they are entered explicitly with Join.
func (se *Session) currentMatch() *Match {
	se.mu.Lock()
	defer se.mu.Unlock()
	for _, m := range se.matchList() {
		if !m.tc.Async {
			return m
		}
	}
	return nil
}

// spectate starts watching m, leaving any previous match.
func (se *Session) spectate(m *Match) {
	se.mu.Lock()
	old := se.watching
	se.watching = m
	se.mu.Unlock()
	if old != nil && old != m {
		old.removeSpectator(se)
	}
	if m != nil {
		m.addSpectator(se)
	}
}

// watchedMatch is the match being spectated, if any.
func (se *Session) watchedMatch() *Match {
	se.mu.Lock()
	defer se.mu.Unlock()
	return se.watching
}

// stopSpectating clears the watch if it is this match (called when a
// watched match ends).
func (se *Session) stopSpectating(m *Match) {
	se.mu.Lock()
	if se.watching == m {
		se.watching = nil
	}
	se.mu.Unlock()
}

func (se *Session) matchByID(id string) *Match {
	se.mu.Lock()
	defer se.mu.Unlock()
	return se.matches[id]
}

func (se *Session) addMatch(m *Match) {
	se.mu.Lock()
	se.matches[m.id] = m
	se.mu.Unlock()
}

func (se *Session) removeMatch(m *Match) {
	se.mu.Lock()
	delete(se.matches, m.id)
	se.mu.Unlock()
}

// liveList summarises every match the player is in.
func (se *Session) liveList() []proto.LiveMatch {
	se.mu.Lock()
	ms := se.matchList()
	se.mu.Unlock()
	var out []proto.LiveMatch
	for _, m := range ms {
		out = append(out, m.summary(se))
	}
	return out
}

func (se *Session) handle(f proto.Frame) error {
	ctx := context.Background()
	switch f.T {
	case proto.TPing:
		se.send(proto.TPong, nil)
		return nil
	case proto.TAccount:
		var a proto.Account
		if err := f.As(&a); err != nil {
			return err
		}
		return se.account(ctx, a)
	case proto.TChallenge:
		var c proto.Challenge
		if err := f.As(&c); err != nil {
			return err
		}
		return se.challenge(c)
	case proto.TQueue:
		var q proto.Queue
		if err := f.As(&q); err != nil {
			return err
		}
		if se.currentMatch() != nil {
			return errors.New("already in a match")
		}
		// A clock, rated or casual; older clients' names map onto clocks.
		mode := q.Mode
		if _, ok := se.srv.cfg.TimeControls[mode]; !ok {
			if m, legacy := legacyControls[mode]; legacy {
				mode = m
			}
		} else if !q.Rated && !strings.HasSuffix(mode, CasualSuffix) {
			if _, ok := se.srv.cfg.TimeControls[mode+CasualSuffix]; ok && !q.Legacy() {
				mode += CasualSuffix
			}
		}
		if q.Rated && se.player.Guest {
			return errors.New("log in to play rated")
		}
		q.Mode = mode
		tc, ok := se.srv.cfg.TimeControls[q.Mode]
		if !ok {
			return errors.New("unknown mode " + q.Mode)
		}
		if q.Size == "" {
			q.Size = "1v1"
		}
		if _, ok := se.srv.c.Rules.Modes[q.Size]; !ok {
			return errors.New("unknown size " + q.Size)
		}
		if se.player.Guest && !tc.Guests {
			return errors.New("log in to play rated")
		}
		se.mu.Lock()
		se.hero = q.Hero
		se.mu.Unlock()
		se.srv.lobby.join(se, q.Mode, q.Size)
		return nil
	case proto.TCancel:
		se.srv.lobby.leave(se)
		se.send(proto.TQueueStatus, proto.QueueStatus{Mode: "", Waiting: 0})
		return nil
	case proto.TDraft:
		var d proto.DraftAction
		if err := f.As(&d); err != nil {
			return err
		}
		m := se.matchByID(d.Match)
		if m == nil {
			return errors.New("not in that match")
		}
		return m.draftAction(se, d)
	case proto.TOrders:
		var o proto.Orders
		if err := f.As(&o); err != nil {
			return err
		}
		m := se.matchByID(o.Match)
		if m == nil {
			return errors.New("not in that match")
		}
		return m.submitOrders(se, o)
	case proto.TChat:
		var c proto.Chat
		if err := f.As(&c); err != nil {
			return err
		}
		m := se.matchByID(c.Match)
		if m == nil {
			m = se.currentMatch()
		}
		if m == nil {
			m = se.watchedMatch() // spectators talk to the match they watch
		}
		if m != nil {
			m.chat(se, c.Text)
		}
		return nil
	case proto.TLeave:
		var j proto.Join
		_ = f.As(&j)
		m := se.matchByID(j.Match)
		if m == nil {
			m = se.currentMatch()
		}
		if m == nil {
			// No id and no timed match: an only async match is unambiguous.
			se.mu.Lock()
			if len(se.matches) == 1 {
				for _, x := range se.matches {
					m = x
				}
			}
			se.mu.Unlock()
		}
		if m != nil {
			m.leave(se)
		}
		return nil
	case proto.TJoin:
		var j proto.Join
		if err := f.As(&j); err != nil {
			return err
		}
		m := se.matchByID(j.Match)
		if m == nil {
			return errors.New("not in that match")
		}
		m.reconnect(se)
		return nil
	case proto.TLive:
		se.send(proto.TLiveList, proto.LiveList{Matches: se.liveList()})
		return nil
	case proto.TLobby:
		se.send(proto.TLobbyList, proto.LobbyList{Matches: se.srv.lobbyList()})
		return nil
	case proto.TSpectate:
		var sp proto.Spectate
		if err := f.As(&sp); err != nil {
			return err
		}
		if sp.Match == "" {
			se.spectate(nil)
			return nil
		}
		m := se.srv.match(sp.Match)
		if m == nil {
			return errors.New("no such live match")
		}
		if se.matchByID(sp.Match) != nil {
			return errors.New("you are playing that match")
		}
		se.spectate(m)
		return nil
	case proto.TUnspec:
		se.spectate(nil)
		return nil
	case proto.THistory:
		var h proto.History
		_ = f.As(&h)
		if h.Limit <= 0 || h.Limit > 50 {
			h.Limit = 20
		}
		ms, err := se.srv.st.History(ctx, se.player.ID, h.Limit)
		if err != nil {
			return err
		}
		list := proto.HistoryList{Matches: []proto.HistoryMatch{}}
		for _, m := range ms {
			list.Matches = append(list.Matches, historyMatch(m, se.player.ID))
		}
		se.send(proto.THistoryList, list)
		return nil
	case proto.TReplayGet:
		var r proto.ReplayGet
		if err := f.As(&r); err != nil {
			return err
		}
		b, err := se.srv.st.Replay(ctx, r.Match)
		if errors.Is(err, store.ErrNotFound) {
			return errors.New("no replay for that match")
		}
		if err != nil {
			return err
		}
		se.send(proto.TReplayData, proto.ReplayData{Match: r.Match, Replay: b})
		return nil
	case proto.TLadder:
		var l proto.Ladder
		_ = f.As(&l)
		if l.Mode == "" {
			l.Mode = "blitz"
		}
		rows, err := se.srv.st.Leaderboard(ctx, l.Mode, 20)
		if err != nil {
			return err
		}
		list := proto.LadderList{Mode: l.Mode, Rows: []proto.LadderRow{}}
		for _, r := range rows {
			list.Rows = append(list.Rows, proto.LadderRow{Name: r.Name, Rating: proto.RatingInfo{Rating: r.Rating.Rating, RD: r.Rating.RD, Games: r.Rating.Games, Wins: r.Rating.Wins}})
		}
		se.send(proto.TLadderList, list)
		return nil
	}
	return fmt.Errorf("unknown message %s", f.T)
}

func historyMatch(m store.Match, you string) proto.HistoryMatch {
	h := proto.HistoryMatch{Match: m.ID, Mode: m.Mode, Time: m.TimeCtl, Started: m.Started, Result: m.Result, Winner: -1, You: -1}
	if m.Ended != nil {
		h.Ended = *m.Ended
	}
	if m.Winner != nil {
		h.Winner = *m.Winner
	}
	for _, p := range m.Players {
		if p.PlayerID == you {
			h.You = p.Slot
		}
		h.Players = append(h.Players, proto.HistoryPlayer{Name: p.Name, Slot: p.Slot, Team: p.Team, Hero: p.Hero, Bot: p.Bot, Before: p.RatingBefore, After: p.RatingAfter})
	}
	return h
}

// versus names the opponents of a seat, for match summaries.
func versus(players []*mplayer, team int) string {
	var names []string
	for _, p := range players {
		if p.team != team {
			names = append(names, p.name)
		}
	}
	return strings.Join(names, ", ")
}
