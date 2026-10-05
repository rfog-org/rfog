// Package webplay is the game as a graphical client runs it: a match
// against bots held in memory, and the questions a board asks while the
// player plans (where can this unit go, what can it hit, is this order
// legal), answered by the engine itself. It has no drawing of its own:
// the browser draws, this package decides. Compiled to WebAssembly
// (cmd/wasm) it runs on the player's device, so a tap is answered at
// once; the rules are the same engine the server and the terminal run.
package webplay

import (
	"errors"
	"fmt"

	"rfog/bots"
	"rfog/data"
	"rfog/engine"
)

// Game is one offline match: the player against a bot.
type Game struct {
	c      *engine.Content
	s      engine.State
	human  int
	bot    *bots.Bot
	botID  int
	heroes []string
	level  string
	seed   uint64
	// remote is a match the server runs: s is the player's fogged view as
	// the server sent it, used only to preview and check orders.
	remote bool
	// rec records the match as it is played, for its replay.
	rec *engine.Replay
	// hotseat is two players at one device: human is whoever plans now,
	// held the first player's orders while the second plans.
	hotseat bool
	held    []engine.Order
}

// NewHotseat starts a 1v1 match for two players at one device: player 0
// plans, hands over, player 1 plans, and the turn plays out whole.
func NewHotseat(c *engine.Content, heroA, heroB string, seed uint64) (*Game, error) {
	if c == nil {
		return nil, errors.New("no content")
	}
	st := engine.Setup{ID: "hotseat", Mode: "1v1", Seed: seed, TimeControl: "bots",
		Players: []engine.SetupPlayer{{Name: "player 1", Hero: heroA}, {Name: "player 2", Hero: heroB}}}
	s, err := engine.NewMatch(c, st)
	if err != nil {
		return nil, err
	}
	return &Game{c: c, s: s, heroes: c.HeroIDs(), seed: seed, rec: engine.NewReplay(st, s), hotseat: true}, nil
}

// Planner is the player whose orders are next (always 0 against a bot).
func (g *Game) Planner() int { return g.human }

// Remote is an online match as the page holds it: the view the server
// sent (already fogged) for player you, for previews and order checks.
// The server resolves turns; Commit refuses.
func Remote(c *engine.Content, view engine.State, you int) *Game {
	return &Game{c: c, s: view, human: you, remote: true, heroes: c.HeroIDs()}
}

// Content is the rule set, for names, stats and ability text.
func Content() (*engine.Content, error) { return data.Load() }

// NewGame starts a 1v1 match: hero for the player, botHero for the bot
// ("" = picked from the seed), at a bot level (easy, normal, hard).
func NewGame(c *engine.Content, hero, botHero, level string, seed uint64) (*Game, error) {
	if c == nil {
		return nil, errors.New("no content")
	}
	heroes := c.HeroIDs()
	if hero == "" {
		hero = heroes[0]
	}
	if botHero == "" {
		botHero = heroes[engine.NewRNG(seed, 1).Intn(len(heroes))]
	}
	st := engine.Setup{ID: "web", Mode: "1v1", Seed: seed, TimeControl: "bots",
		Players: []engine.SetupPlayer{{Name: "you", Hero: hero}, {Name: "bot " + botHero, Hero: botHero}}}
	s, err := engine.NewMatch(c, st)
	if err != nil {
		return nil, err
	}
	return &Game{c: c, s: s, human: 0, bot: bots.New(level, seed+7919), botID: 1, heroes: heroes, level: level, seed: seed, rec: engine.NewReplay(st, s)}, nil
}

// Replay is the match so far as a replay, the format the terminal saves
// and the server stores.
func (g *Game) Replay() *engine.Replay { return g.rec }

// Save is a match in progress, as the page keeps it so a reload (a phone
// that dropped the tab in the background) comes back to the same game.
type Save struct {
	Rules string       `json:"rules"` // Content.Fingerprint() it was played under
	Level string       `json:"level"`
	Seed  uint64       `json:"seed"`
	State engine.State `json:"state"`
	// Replay is the match so far (older saves have none).
	Replay *engine.Replay `json:"replay,omitempty"`
}

// Save is the match as it stands.
func (g *Game) Save() Save {
	return Save{Rules: g.c.Fingerprint(), Level: g.level, Seed: g.seed, State: g.s, Replay: g.rec}
}

// ErrOldRules is a save made under different rules: the game has been
// updated since, and a match is never continued under rules it did not
// start with.
var ErrOldRules = errors.New("the rules have been updated since this match was saved")

// Load resumes a saved match. The bot is reseeded from the turn, so its
// choices after a reload may differ from the ones it would have made; the
// rules and the state are exactly as saved.
func Load(c *engine.Content, sv Save) (*Game, error) {
	if c == nil {
		return nil, errors.New("no content")
	}
	if sv.Rules != c.Fingerprint() {
		return nil, ErrOldRules
	}
	if len(sv.State.Players) != 2 || sv.State.Ended() {
		return nil, errors.New("no match to resume")
	}
	turn := uint64(sv.State.Match.Turn)
	return &Game{c: c, s: sv.State, human: 0, bot: bots.New(sv.Level, sv.Seed+7919+turn), botID: 1,
		heroes: c.HeroIDs(), level: sv.Level, seed: sv.Seed, rec: sv.Replay}, nil
}

// View is the match as the player may see it (fog applied).
func (g *Game) View() engine.State {
	if g.remote {
		return g.s
	}
	return engine.View(g.c, g.s, g.human)
}

// Move is a tile a unit can reach and the path there.
type Move struct {
	To   engine.Pos   `json:"to"`
	Path []engine.Pos `json:"path"`
}

// Moves are where the unit can go this turn (empty for someone else's).
func (g *Game) Moves(unitID int) []Move {
	v := g.View()
	u := v.Unit(unitID)
	if u == nil || u.Owner != g.human || !u.Alive() {
		return nil
	}
	var out []Move
	for p, path := range engine.Reachable(g.c, &v, u) {
		if p == u.Pos {
			continue
		}
		out = append(out, Move{To: p, Path: path})
	}
	return out
}

// Target is an enemy the unit can attack, and the odds: Dice rolled,
// each hitting on TN or more, Chance per die.
type Target struct {
	Unit   int        `json:"unit"`
	At     engine.Pos `json:"at"`
	Dice   int        `json:"dice"`
	TN     int        `json:"tn"`
	Chance float64    `json:"chance"`
}

// Targets are the enemies the unit can attack standing at from (its
// planned destination, or where it is).
func (g *Game) Targets(unitID int, from engine.Pos) []Target {
	v := g.View()
	u := v.Unit(unitID)
	if u == nil || u.Owner != g.human || !u.Alive() {
		return nil
	}
	var out []Target
	for i := range v.Units {
		t := &v.Units[i]
		if t.Team == u.Team || !t.Alive() || !engine.CanAttack(g.c, &v, u, from, t) {
			continue
		}
		dice, tn := engine.AttackPreview(g.c, &v, u, t, t.Pos)
		out = append(out, Target{Unit: t.ID, At: t.Pos, Dice: dice, TN: tn, Chance: engine.HitChance(tn)})
	}
	return out
}

// AbilityTarget is a legal target for an ability: a tile, and the unit
// on it when the ability targets units.
type AbilityTarget struct {
	At   engine.Pos `json:"at"`
	Unit int        `json:"unit,omitempty"`
}

// AbilityTargets are where the unit can aim ability id this turn, with
// the orders already planned (pending) taken into account. Self and
// whole-team abilities have one target: the unit itself. The engine's own
// validation decides each candidate.
func (g *Game) AbilityTargets(unitID int, id string, pending []engine.Order) ([]AbilityTarget, error) {
	v := g.View()
	u := v.Unit(unitID)
	if u == nil || u.Owner != g.human || !u.Alive() {
		return nil, errors.New("not your unit")
	}
	ab, ok := g.c.Abilities[id]
	if !ok {
		return nil, fmt.Errorf("no ability %q", id)
	}
	try := func(o engine.Order) bool {
		orders := append(append([]engine.Order(nil), without(pending, unitID)...), o)
		return len(engine.Validate(g.c, &v, g.human, orders)) == 0
	}
	base := engine.Order{UnitID: u.ID, Action: engine.ActAbility, Ability: id}
	var out []AbilityTarget
	switch ab.Target {
	case engine.TargetSelf, engine.TargetAll:
		o := base
		o.TargetU, o.Target = u.ID, u.Pos
		if try(o) {
			out = append(out, AbilityTarget{At: u.Pos, Unit: u.ID})
		}
	case engine.TargetUnit:
		for i := range v.Units {
			t := &v.Units[i]
			if !t.Alive() {
				continue
			}
			o := base
			o.TargetU, o.Target = t.ID, t.Pos
			if try(o) {
				out = append(out, AbilityTarget{At: t.Pos, Unit: t.ID})
			}
		}
	default:
		for y := 0; y < v.Board.H; y++ {
			for x := 0; x < v.Board.W; x++ {
				o := base
				o.Target = engine.Pos{X: x, Y: y}
				if try(o) {
					out = append(out, AbilityTarget{At: o.Target})
				}
			}
		}
	}
	return out, nil
}

// Check reports what is wrong with a set of orders, if anything.
func (g *Game) Check(orders []engine.Order) []string {
	v := g.View()
	var out []string
	for _, err := range engine.Validate(g.c, &v, g.human, orders) {
		out = append(out, err.Error())
	}
	return out
}

// Turn is one resolved turn as the player saw it: the view before and
// after, and the events between (fog applied), to animate.
type Turn struct {
	Pre    engine.State   `json:"pre"`
	Post   engine.State   `json:"post"`
	Events []engine.Event `json:"events"`
	Ended  bool           `json:"ended"`
	Winner int            `json:"winner"` // team, -1 = draw (when ended)
	Result string         `json:"result,omitempty"`
	// Hotseat: Pass is a hand-over (nothing resolved; Post is the next
	// player's view); Whole is a turn shown unfogged.
	Pass  bool `json:"pass,omitempty"`
	Whole bool `json:"whole,omitempty"`
}

// Commit plays the turn: the player's orders, the bot's, the engine's
// resolution. Illegal orders are refused whole, as the server would.
func (g *Game) Commit(orders []engine.Order) (Turn, error) {
	if g.remote {
		return Turn{}, errors.New("online: the server resolves the turn")
	}
	if g.s.Ended() {
		return Turn{}, errors.New("the match is over")
	}
	if errs := g.Check(orders); len(errs) > 0 {
		return Turn{}, errors.New(errs[0])
	}
	if g.hotseat {
		return g.commitHotseat(orders)
	}
	pre := g.View()
	bv := engine.View(g.c, g.s, g.botID)
	all := map[int][]engine.Order{g.human: orders, g.botID: g.bot.Orders(g.c, &bv, g.botID)}
	var events []engine.Event
	if g.rec != nil {
		g.rec.Record(all)
	}
	g.s, events = engine.Step(g.c, g.s, all, g.s.Match.Seed)
	team := g.s.Player(g.human).Team
	t := Turn{Pre: pre, Post: g.View(), Events: engine.FilterEvents(g.c, &g.s, team, events), Ended: g.s.Ended()}
	if t.Ended {
		t.Winner, t.Result = g.s.Match.Winner, g.s.Match.Result
	}
	return t, nil
}

// commitHotseat holds the first player's orders and hands over; the
// second player's resolve the turn, shown whole (both players are at the
// one screen). Pass marks a hand-over with nothing resolved.
func (g *Game) commitHotseat(orders []engine.Order) (Turn, error) {
	if g.human == 0 {
		g.held, g.human = orders, 1
		return Turn{Pass: true, Post: g.View()}, nil
	}
	all := map[int][]engine.Order{0: g.held, 1: orders}
	g.held, g.human = nil, 0
	pre := g.s.Clone()
	if g.rec != nil {
		g.rec.Record(all)
	}
	var events []engine.Event
	g.s, events = engine.Step(g.c, g.s, all, g.s.Match.Seed)
	t := Turn{Pre: pre, Post: g.s.Clone(), Events: events, Ended: g.s.Ended(), Whole: true}
	if t.Ended {
		t.Winner, t.Result = g.s.Match.Winner, g.s.Match.Result
	}
	return t, nil
}

// without is orders minus those of one unit.
func without(orders []engine.Order, unitID int) []engine.Order {
	var out []engine.Order
	for _, o := range orders {
		if o.UnitID != unitID {
			out = append(out, o)
		}
	}
	return out
}

// Viewer steps through a replay: every turn's state before and after and
// its events, unfogged, as a replay board shows them.
type Viewer struct {
	Setup  engine.Setup
	states []engine.State
	events [][]engine.Event
}

// ReplayTurn is one turn of a replay.
type ReplayTurn struct {
	Turn   int            `json:"turn"`
	Pre    engine.State   `json:"pre"`
	Post   engine.State   `json:"post"`
	Events []engine.Event `json:"events"`
}

// LoadReplay runs a replay through the rules, keeping every turn. A replay
// made under other rules may stop early (orders no longer valid); the
// turns up to there are kept.
func LoadReplay(c *engine.Content, b []byte) (*Viewer, error) {
	r, err := engine.UnmarshalReplay(b)
	if err != nil {
		return nil, err
	}
	if r.Version != engine.ReplayVersion {
		return nil, fmt.Errorf("replay version %d, want %d", r.Version, engine.ReplayVersion)
	}
	v := &Viewer{Setup: r.Setup}
	s := r.Initial.Clone()
	v.states = append(v.states, s)
	for _, orders := range r.Turns {
		if s.Ended() {
			break
		}
		var ev []engine.Event
		s, ev = engine.Step(c, s, orders, s.Match.Seed)
		v.states = append(v.states, s)
		v.events = append(v.events, ev)
	}
	return v, nil
}

// Turns is how many turns the replay has.
func (v *Viewer) Turns() int { return len(v.events) }

// Start is the board before the first turn.
func (v *Viewer) Start() engine.State { return v.states[0] }

// Turn is turn i (0-based): the board before and after, and what happened.
func (v *Viewer) Turn(i int) (ReplayTurn, error) {
	if i < 0 || i >= len(v.events) {
		return ReplayTurn{}, errors.New("no such turn")
	}
	return ReplayTurn{Turn: i + 1, Pre: v.states[i], Post: v.states[i+1], Events: v.events[i]}, nil
}
