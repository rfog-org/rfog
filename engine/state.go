package engine

import (
	"fmt"
	"sort"
)

// Setup describes how to create a match.
type Setup struct {
	ID            string
	Mode          string // 1v1, 2v2, ...
	Map           string // "" = mode default
	Seed          uint64
	TimeControl   string
	Deterministic bool
	// Players in order; team assignment alternates by PlayersPerTeam blocks:
	// players [0..n) are team 0, [n..2n) team 1.
	Players []SetupPlayer
}

// SetupPlayer is one participant's choice.
type SetupPlayer struct {
	Name string
	Hero string
}

// NewMatch builds the initial state. Deployment is automatic: each player's
// commander and units are placed in their team's deploy columns.
func NewMatch(c *Content, st Setup) (State, error) {
	mode, ok := c.Rules.Modes[st.Mode]
	if !ok {
		return State{}, fmt.Errorf("unknown mode %q", st.Mode)
	}
	mapID := st.Map
	if mapID == "" {
		mapID = mode.Map
	}
	m, ok := c.Maps[mapID]
	if !ok {
		return State{}, fmt.Errorf("unknown map %q", mapID)
	}
	if len(st.Players) != mode.PlayersPerTeam*2 {
		return State{}, fmt.Errorf("mode %s needs %d players, got %d", st.Mode, mode.PlayersPerTeam*2, len(st.Players))
	}
	winScore := c.Rules.WinScore
	if mode.WinScore > 0 {
		winScore = mode.WinScore
	}
	s := State{
		Match: MatchMeta{
			ID:            st.ID,
			Mode:          st.Mode,
			Map:           mapID,
			Turn:          1,
			Phase:         PhaseOrders,
			Seed:          st.Seed,
			TimeControl:   st.TimeControl,
			Deterministic: st.Deterministic,
			MaxTurns:      c.Rules.MaxTurns,
			WinScore:      winScore,
			Winner:        -1,
		},
		Board: Board{
			W: m.Width, H: m.Height, DeployCols: m.DeployCols,
			Tiles: append([]Tile(nil), m.Tiles...),
		},
		Teams:      []Team{{ID: 0}, {ID: 1}},
		NextUnitID: 1,
	}
	for _, o := range m.Objectives {
		s.Objectives = append(s.Objectives, Objective{ID: o.ID, Tiles: append([]Pos(nil), o.Tiles...), Holder: -1})
	}
	for i, sp := range st.Players {
		team := i / mode.PlayersPerTeam
		p := Player{ID: i, Team: team, Name: sp.Name, Connected: true, Commander: -1}
		s.Players = append(s.Players, p)
		s.Teams[team].Players = append(s.Teams[team].Players, i)
	}
	// Deployment: gather each team's free deploy tiles in a fixed order
	// (near the vertical centre first), then assign units.
	for i, sp := range st.Players {
		team := i / mode.PlayersPerTeam
		h, ok := c.Heroes[sp.Hero]
		if !ok {
			return State{}, fmt.Errorf("player %d: unknown hero %q", i, sp.Hero)
		}
		cmd := newUnit(&s, i, team, h.ID, h.Name, h.Stats, true)
		cmd.Level = 1
		cmd.Cooldowns = map[string]int{}
		s.Players[i].Commander = cmd.ID
		if err := deploy(&s, cmd); err != nil {
			return State{}, err
		}
		for k := 0; k < mode.Units; k++ {
			ud := c.Units[c.Rules.UnitSet[k]]
			u := newUnit(&s, i, team, ud.ID, ud.Name, ud.Stats, false)
			u.Cooldowns = map[string]int{}
			if err := deploy(&s, u); err != nil {
				return State{}, err
			}
		}
	}
	return s, nil
}

func newUnit(s *State, owner, team int, kind, name string, st Stats, cmd bool) *Unit {
	u := Unit{
		ID: s.NextUnitID, Owner: owner, Team: team, Kind: kind, Name: name,
		IsCommander: cmd,
		HP:          st.HP, MaxHP: st.HP, MV: st.MV, JMP: st.JMP, RNG: st.RNG,
		ATK: st.ATK, DEF: st.DEF, INI: st.INI, VIS: st.VIS,
		Cooldowns: map[string]int{},
	}
	s.NextUnitID++
	s.Units = append(s.Units, u)
	return &s.Units[len(s.Units)-1]
}

// DeployTiles returns a team's deploy tiles in deterministic preference order:
// commander-friendly ordering from the centre row outward, inner column first.
func (s *State) DeployTiles(team int) []Pos {
	b := &s.Board
	var cols []int
	if team == 0 {
		for x := b.DeployCols - 1; x >= 0; x-- {
			cols = append(cols, x)
		}
	} else {
		for x := b.W - b.DeployCols; x < b.W; x++ {
			cols = append(cols, x)
		}
	}
	var rows []int
	mid := (b.H - 1) / 2
	for d := 0; d <= b.H; d++ {
		if mid-d >= 0 {
			rows = append(rows, mid-d)
		}
		if d > 0 && mid+d < b.H {
			rows = append(rows, mid+d)
		}
		if len(rows) >= b.H {
			break
		}
	}
	if team != 0 {
		// Maps are symmetric under 180-degree rotation, so team 1 deploys
		// on the rotated tiles: same distances to the same features.
		for i, y := range rows {
			rows[i] = b.H - 1 - y
		}
	}
	var out []Pos
	for _, x := range cols {
		for _, y := range rows {
			p := Pos{x, y}
			if b.Passable(p) {
				out = append(out, p)
			}
		}
	}
	return out
}

func deploy(s *State, u *Unit) error {
	for _, p := range s.DeployTiles(u.Team) {
		if s.UnitAt(p) == nil {
			u.Pos = p
			return nil
		}
	}
	return fmt.Errorf("no free deploy tile for team %d", u.Team)
}

// Unit returns the unit with the given id, or nil.
func (s *State) Unit(id int) *Unit {
	for i := range s.Units {
		if s.Units[i].ID == id {
			return &s.Units[i]
		}
	}
	return nil
}

// Alive reports whether a unit is on the board.
func (u *Unit) Alive() bool { return u != nil && !u.Dead && u.HP > 0 }

// UnitAt returns the living unit standing on p, or nil.
func (s *State) UnitAt(p Pos) *Unit {
	for i := range s.Units {
		u := &s.Units[i]
		if u.Alive() && u.Pos == p {
			return u
		}
	}
	return nil
}

// Player returns the player with id, or nil.
func (s *State) Player(id int) *Player {
	for i := range s.Players {
		if s.Players[i].ID == id {
			return &s.Players[i]
		}
	}
	return nil
}

// Has reports whether a unit has a status of kind.
func (u *Unit) Has(kind string) bool {
	for _, st := range u.Status {
		if st.Kind == kind {
			return true
		}
	}
	return false
}

// StatusSum sums magnitudes of statuses of kind (optionally for a stat).
func (u *Unit) StatusSum(kind, stat string) int {
	n := 0
	for _, st := range u.Status {
		if st.Kind == kind && (stat == "" || st.Stat == stat) {
			n += st.Magnitude
		}
	}
	return n
}

// Eff returns a unit's effective stat after temporary modifiers.
func (u *Unit) Eff(stat string) int {
	base := 0
	switch stat {
	case "hp":
		return u.HP
	case "mv":
		base = u.MV - u.StatusSum("slow", "")
	case "jmp":
		base = u.JMP
	case "rng":
		base = u.RNG
	case "atk":
		base = u.ATK
	case "def":
		base = u.DEF
	case "ini":
		base = u.INI
	case "vis":
		base = u.VIS
	}
	base += u.StatusSum("stat", stat)
	if base < 0 {
		base = 0
	}
	return base
}

// Shield returns the total shield points on a unit.
func (u *Unit) Shield() int { return u.StatusSum("shield", "") }

// Rooted reports whether the unit may not move.
func (u *Unit) Rooted() bool { return u.Has("root") }

// Silenced reports whether the unit may not cast.
func (u *Unit) Silenced() bool { return u.Has("silence") }

// Cloaked reports whether the unit is hidden from non-adjacent enemies.
func (u *Unit) Cloaked() bool { return u.Has("cloak") }

// AliveUnits returns living units sorted by id.
func (s *State) AliveUnits() []*Unit {
	var out []*Unit
	for i := range s.Units {
		if s.Units[i].Alive() {
			out = append(out, &s.Units[i])
		}
	}
	return out
}

// TeamUnits returns living units of a team sorted by id.
func (s *State) TeamUnits(team int) []*Unit {
	var out []*Unit
	for _, u := range s.AliveUnits() {
		if u.Team == team {
			out = append(out, u)
		}
	}
	return out
}

// TieTeam is the team whose units go first among equal INI this turn. It
// alternates every turn, and the match seed picks who has it on turn 1.
// Team 0 deploys first and so holds the lower unit ids: breaking ties by
// id alone handed it every contested instant ability, and alternating from
// a fixed start still handed it the odd turns, where first contact tends
// to fall.
func (s *State) TieTeam() int {
	first := NewRNG(s.Match.Seed, -1).Intn(2)
	turn := s.Match.Turn
	if turn < 1 {
		turn = 1
	}
	return (turn - 1 + first) % 2
}

// byINI sorts units by descending INI, then the turn's tie team first, then
// ascending id (simultaneity, where the rules have it, is handled by the
// caller grouping equal INI).
func byINI(us []*Unit, tie int) {
	sort.SliceStable(us, func(i, j int) bool {
		a, b := us[i].Eff("ini"), us[j].Eff("ini")
		if a != b {
			return a > b
		}
		if ti, tj := us[i].Team == tie, us[j].Team == tie; ti != tj {
			return ti
		}
		return us[i].ID < us[j].ID
	})
}

// Smoked reports whether tile p is under smoke.
func (s *State) Smoked(p Pos) bool {
	for _, e := range s.Effects {
		if e.Kind != "smoke" {
			continue
		}
		for _, t := range e.Tiles {
			if t == p {
				return true
			}
		}
	}
	return false
}

// LOS reports whether a unit at a (height za) can see b (height zb).
// Blocked by walls, smoke and tiles whose z >= max(za, zb)+1 strictly between.
// Smoke on the target tile also blocks unless the tiles are adjacent.
func (s *State) LOS(a, b Pos) bool {
	if !s.Board.In(a) || !s.Board.In(b) {
		return false
	}
	if a == b {
		return true
	}
	za, zb := s.Board.At(a).Z, s.Board.At(b).Z
	limit := maxInt(za, zb) + 1
	clear := func(line []Pos) bool {
		for _, p := range line {
			t := s.Board.At(p)
			if t.Terrain == TerrainWall || t.Z >= limit || s.Smoked(p) {
				return false
			}
		}
		return true
	}
	// Sight is mutual and the same for both sides of a rotated map: a line
	// that grazes a corner is clear if either way of rounding it is.
	if !clear(Trace(a, b)) && !clear(Trace(b, a)) {
		return false
	}
	if s.Board.At(b).Terrain == TerrainWall {
		return false
	}
	if s.Smoked(b) && !Adjacent(a, b) {
		return false
	}
	return true
}

// Vision returns the unit's vision radius including height bonus.
func (s *State) Vision(u *Unit, r *Rules) int {
	return u.Eff("vis") + s.Board.At(u.Pos).Z*r.VisionPerZ
}

// VisibleTiles computes the set of tiles a team can currently see.
func (s *State) VisibleTiles(team int, r *Rules) map[Pos]bool {
	vis := map[Pos]bool{}
	for _, u := range s.TeamUnits(team) {
		rad := s.Vision(u, r)
		for _, p := range s.Board.Area(u.Pos, rad) {
			if vis[p] {
				continue
			}
			if s.LOS(u.Pos, p) {
				vis[p] = true
			}
		}
	}
	for _, e := range s.Effects {
		if e.Kind == "reveal" && e.Team == team {
			for _, p := range e.Tiles {
				vis[p] = true
			}
		}
	}
	return vis
}

// CanSee reports whether team can see unit u given a visible-tile set.
func (s *State) CanSee(team int, u *Unit, vis map[Pos]bool) bool {
	if u.Team == team {
		return true
	}
	if !vis[u.Pos] {
		return false
	}
	if u.Cloaked() {
		for _, f := range s.TeamUnits(team) {
			if Adjacent(f.Pos, u.Pos) {
				return true
			}
		}
		return false
	}
	return true
}

// Ended reports whether the match is over.
func (s *State) Ended() bool { return s.Match.Phase == PhaseEnded }

// Clone deep-copies the state.
func (s State) Clone() State {
	c := s
	c.Board.Tiles = append([]Tile(nil), s.Board.Tiles...)
	c.Teams = make([]Team, len(s.Teams))
	for i, t := range s.Teams {
		c.Teams[i] = t
		c.Teams[i].Players = append([]int(nil), t.Players...)
	}
	c.Players = append([]Player(nil), s.Players...)
	c.Units = make([]Unit, len(s.Units))
	for i, u := range s.Units {
		c.Units[i] = u
		c.Units[i].Status = append([]Status(nil), u.Status...)
		c.Units[i].Cooldowns = make(map[string]int, len(u.Cooldowns))
		for k, v := range u.Cooldowns {
			c.Units[i].Cooldowns[k] = v
		}
	}
	c.Effects = make([]TileEffect, len(s.Effects))
	for i, e := range s.Effects {
		c.Effects[i] = e
		c.Effects[i].Tiles = append([]Pos(nil), e.Tiles...)
	}
	c.Delayed = append([]DelayedAbility(nil), s.Delayed...)
	c.Objectives = make([]Objective, len(s.Objectives))
	for i, o := range s.Objectives {
		c.Objectives[i] = o
		c.Objectives[i].Tiles = append([]Pos(nil), o.Tiles...)
	}
	c.Sightings = append([]Sighting(nil), s.Sightings...)
	c.Log = append([]Event(nil), s.Log...)
	return c
}

// AddUnit places a new unit of kind (a unit id or hero id) for owner at p.
// Used by tests, the tutorial and tools; normal matches deploy via NewMatch.
func AddUnit(c *Content, s *State, owner int, kind string, p Pos) (*Unit, error) {
	pl := s.Player(owner)
	if pl == nil {
		return nil, fmt.Errorf("no player %d", owner)
	}
	if h, ok := c.Heroes[kind]; ok {
		u := newUnit(s, owner, pl.Team, h.ID, h.Name, h.Stats, true)
		u.Level = 1
		u.Pos = p
		return u, nil
	}
	d, ok := c.Units[kind]
	if !ok {
		return nil, fmt.Errorf("unknown unit kind %q", kind)
	}
	u := newUnit(s, owner, pl.Team, d.ID, d.Name, d.Stats, false)
	u.Pos = p
	u.ExpiresIn = d.Expires
	return u, nil
}

// RemoveUnit takes a unit off the board without awarding anything.
func RemoveUnit(s *State, id int) {
	if u := s.Unit(id); u != nil {
		u.Dead = true
		u.HP = 0
	}
}
