package client

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"rfog/bots"
	"rfog/engine"
)

// localMatch drives an offline match: humans and/or bots on one machine,
// or a replay being watched. It owns the full state; screens only ever get
// fog-filtered views, exactly as they would from a server.
type localMatch struct {
	c        *engine.Content
	s        engine.State
	setup    engine.Setup
	replay   *engine.Replay
	isBot    [2]bool
	bots     [2]*bots.Bot
	orders   map[int][]engine.Order
	commit   [2]bool
	watching bool // replay playback
	turnIdx  int
	level    string

	lastEvents []engine.Event
	preViews   map[int]engine.State // views before the last step
}

func newLocalMatch(c *engine.Content, st engine.Setup, isBot [2]bool, level string) (*localMatch, error) {
	s, err := engine.NewMatch(c, st)
	if err != nil {
		return nil, err
	}
	lm := &localMatch{c: c, s: s, setup: st, isBot: isBot, level: level, orders: map[int][]engine.Order{}, preViews: map[int]engine.State{}}
	lm.replay = engine.NewReplay(st, s)
	for i := range isBot {
		if isBot[i] {
			lm.bots[i] = bots.New(level, st.Seed+uint64(i)*7919)
			lm.s.Players[i].Name = "bot " + lm.s.Unit(lm.s.Players[i].Commander).Name
		}
	}
	return lm, nil
}

// newReplayMatch prepares a replay for spectating.
func newReplayMatch(c *engine.Content, rp *engine.Replay) *localMatch {
	return &localMatch{c: c, s: rp.Initial.Clone(), setup: rp.Setup, replay: rp, watching: true,
		orders: map[int][]engine.Order{}, preViews: map[int]engine.State{}}
}

// humans lists human player ids.
func (lm *localMatch) humans() []int {
	var out []int
	for i := range lm.isBot {
		if !lm.isBot[i] && !lm.watching {
			out = append(out, i)
		}
	}
	return out
}

func (lm *localMatch) hotseat() bool { return len(lm.humans()) == 2 }

// view returns a player's fog view; -1 is the spectator (full state).
func (lm *localMatch) view(player int) engine.State {
	if player < 0 {
		v := lm.s.Clone()
		var vis []engine.Pos
		for y := 0; y < v.Board.H; y++ {
			for x := 0; x < v.Board.W; x++ {
				vis = append(vis, engine.Pos{X: x, Y: y})
			}
		}
		v.Visible = vis
		return v
	}
	return engine.View(lm.c, lm.s, player)
}

// setOrders records a human player's committed orders.
func (lm *localMatch) setOrders(player int, orders []engine.Order) {
	lm.orders[player] = orders
	lm.commit[player] = true
}

func (lm *localMatch) allCommitted() bool {
	for _, p := range lm.humans() {
		if !lm.commit[p] {
			return false
		}
	}
	return true
}

// resolve asks bots for orders, steps the engine and records the replay.
// It returns the unfiltered events of the turn.
func (lm *localMatch) resolve() []engine.Event {
	lm.preViews = map[int]engine.State{}
	for _, p := range lm.humans() {
		lm.preViews[p] = lm.view(p)
	}
	if lm.watching || lm.hotseat() {
		lm.preViews[-1] = lm.view(-1)
	}
	if lm.watching {
		if lm.turnIdx >= len(lm.replay.Turns) || lm.s.Ended() {
			return nil
		}
		orders := lm.replay.Turns[lm.turnIdx]
		lm.turnIdx++
		var ev []engine.Event
		lm.s, ev = engine.Step(lm.c, lm.s, orders, lm.s.Match.Seed)
		lm.lastEvents = ev
		return ev
	}
	for i := range lm.isBot {
		if lm.isBot[i] {
			v := lm.view(i)
			lm.orders[i] = lm.bots[i].Orders(lm.c, &v, i)
		}
	}
	lm.replay.Record(lm.orders)
	var ev []engine.Event
	lm.s, ev = engine.Step(lm.c, lm.s, lm.orders, lm.s.Match.Seed)
	lm.lastEvents = ev
	lm.orders = map[int][]engine.Order{}
	lm.commit = [2]bool{}
	return ev
}

// saveReplay writes the replay to the data dir and returns the path.
func (lm *localMatch) SaveReplay() (string, error) {
	if lm.replay == nil {
		return "", fmt.Errorf("no replay")
	}
	lm.replay.FinalHash = engine.Hash(lm.s)
	b, err := lm.replay.Marshal()
	if err != nil {
		return "", err
	}
	name := fmt.Sprintf("%s-%s.json", time.Now().Format("20060102-150405"), lm.setup.ID)
	path := filepath.Join(DataDir(), name)
	return path, os.WriteFile(path, b, 0o644)
}

// ---- matchSource -----------------------------------------------------------

func (lm *localMatch) View(player int) engine.State { return lm.view(player) }
func (lm *localMatch) Humans() []int                { return lm.humans() }
func (lm *localMatch) Hotseat() bool                { return lm.hotseat() }
func (lm *localMatch) Watching() bool               { return lm.watching }
func (lm *localMatch) Deadline() time.Time          { return time.Time{} }
func (lm *localMatch) Setup() engine.Setup          { return lm.setup }
func (lm *localMatch) Leave()                       {}

func (lm *localMatch) Final() *engine.State {
	if !lm.s.Ended() {
		return nil
	}
	f := lm.s.Clone()
	f.Visible = nil
	return &f
}

func (lm *localMatch) Rematch(level string) (matchSource, error) {
	st := lm.setup
	st.Seed = st.Seed*6364136223846793005 + 1442695040888963407
	if level == "" {
		level = lm.level
	}
	return newLocalMatch(lm.c, st, lm.isBot, level)
}

// Commit stores the orders and, once every human has committed, resolves
// the turn immediately and returns it as a message.
func (lm *localMatch) Commit(player int, orders []engine.Order) tea.Cmd {
	lm.setOrders(player, orders)
	if !lm.allCommitted() {
		return nil
	}
	events := lm.resolve()
	msg := turnResolvedMsg{Views: map[int]turnView{}, Ended: lm.s.Ended()}
	if lm.hotseat() {
		// Both players are at one screen: the turn plays once, whole.
		// Together their units see everything either of them could, so
		// the full view hides nothing a hand-over would have protected.
		msg.Views[-1] = turnView{Pre: lm.preViews[-1], Post: lm.view(-1), Events: events}
	}
	for _, p := range lm.humans() {
		post := lm.view(p)
		team := post.Player(p).Team
		msg.Views[p] = turnView{Pre: lm.preViews[p], Post: post, Events: engine.FilterEvents(lm.c, &lm.s, team, events)}
	}
	return func() tea.Msg { return msg }
}

// Advance plays the next replay turn; nil when the replay is over.
func (lm *localMatch) Advance() *turnResolvedMsg {
	events := lm.resolve()
	if events == nil {
		return nil
	}
	return &turnResolvedMsg{Views: map[int]turnView{-1: {Pre: lm.preViews[-1], Post: lm.view(-1), Events: events}}, Ended: lm.s.Ended()}
}

func (lm *localMatch) Committed(player int) bool { return lm.commit[player] }
