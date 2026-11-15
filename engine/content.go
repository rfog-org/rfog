package engine

import (
	"fmt"
	"sort"
)

// Stats is the shared stat block for units and heroes.
type Stats struct {
	HP, MV, JMP, RNG, ATK, DEF, INI, VIS int
}

// UnitDef defines a non-commander unit kind.
type UnitDef struct {
	ID   string
	Name string
	Stats
	Abilities []string
	// Traits, all data-driven.
	HoldDefBonus        int  // 0 = use rules default
	AdjacentDicePenalty int  // dice removed when target adjacent
	IgnoreHeightCost    bool // moving up costs nothing extra
	Expires             int  // summon lifetime in turns; 0 = permanent
}

// Faction is a side's identity: flavour plus whether its heroes are in
// play. v0 ships one enabled faction; a second is data-only scaffolding.
type Faction struct {
	ID      string
	Name    string
	Blurb   string
	Enabled bool
}

// HeroDef defines a commander.
type HeroDef struct {
	ID      string
	Name    string
	Faction string
	Class   string
	Role    string
	Stats
	Abilities map[string]string // key (q w e r) -> ability id
}

// Target types.
const (
	TargetSelf = "self"
	TargetUnit = "unit"
	TargetTile = "tile"
	TargetAll  = "all"
)

// Filters.
const (
	FilterEnemies = "enemies"
	FilterAllies  = "allies"
	FilterAll     = "all"
	FilterSelf    = "self"
)

// EffectDef is one primitive inside an ability.
type EffectDef struct {
	Kind   string
	N      int
	Dist   int
	Radius *int // nil = use ability area
	Turns  int
	Stat   string
	Delta  int
	Unit   string
	Count  int
	Dice   int
	MV     int
	RNG    int
	Shots  int
	Who    string // teleport: self | target
	To     string // teleport: target | adjacent_caster
	Filter string // override ability filter
}

// AbilityDef is a composable ability.
type AbilityDef struct {
	ID       string
	Name     string
	Key      string // q w e r for heroes; unit abilities have none
	Target   string
	Range    int
	Area     int
	Filter   string
	Kind     string // restrict to unit kind (target all / unit)
	Delay    int
	Cooldown int
	Visible  *bool // tile target must be visible; default true
	Effects  []EffectDef
}

// NeedsVisible reports whether the target tile must be visible to the caster's team.
func (a AbilityDef) NeedsVisible() bool {
	return a.Visible == nil || *a.Visible
}

// Instant reports whether an ability resolves in the same turn it is cast.
func (a AbilityDef) Instant() bool { return a.Delay == 0 }

// MapDef is an authored map.
type MapDef struct {
	ID         string
	Name       string
	Width      int
	Height     int
	DeployCols int
	Tiles      []Tile
	Objectives []Objective
}

// ModeDef describes a game mode.
type ModeDef struct {
	PlayersPerTeam int
	Units          int
	Map            string
	WinScore       int // 0 = the global win_score
}

// LengthDef is a match length other than the standard one: the turn limit
// and the score to win, scaled. The standard length is the rules as
// written (max_turns, win_score and each mode's override) and has no entry.
type LengthDef struct {
	Name     string
	MaxTurns int
	// WinScorePercent scales the mode's score to win (rounded half up).
	WinScorePercent int
}

// LengthStandard is the match length the rules describe; Setup.Length ""
// means the same.
const LengthStandard = "standard"

// Scoring rules for objectives (SPEC §3.1 leaves the formula open).
const (
	// ScoreEach: every objective a team holds alone scores a point a turn.
	ScoreEach = "each"
	// ScoreNet: only the lead counts — a team scores the number of
	// objectives it holds beyond what the other team holds. Holding your
	// own half is then worth nothing on its own, so the map has to be
	// contested for anyone to win.
	ScoreNet = "net"
)

// Rules are the global balance numbers.
type Rules struct {
	MaxTurns               int
	WinScore               int
	CommanderOrders        int
	CommanderDeathPoints   int
	RespawnBase            int
	RespawnCap             int
	CoverDefBonus          int
	HoldDefBonus           int
	MaxDef                 int
	HeightRangeBonus       int
	VisionPerZ             int
	FallThreshold          int
	FallDamagePerZ         int
	PushBlockDamage        int
	PushMaxClimb           int
	XPKillUnit             int
	XPKillCommander        int
	XPObjectiveAdjacent    int
	SummonKillCredit       bool // killing a summon (Expires > 0) counts as a kill and gives XP
	LevelThresholds        []int
	CooldownReductionLevel int
	MinCooldown            int
	Unlock                 map[string]int
	Scoring                string // each | net
	Modes                  map[string]ModeDef
	UnitSet                []string
	// Lengths are the longer (or shorter) matches on offer, by id.
	Lengths map[string]LengthDef `json:",omitempty"`
}

// Content is everything loaded from data files.
type Content struct {
	Rules     Rules
	Units     map[string]UnitDef
	Heroes    map[string]HeroDef
	Abilities map[string]AbilityDef
	Maps      map[string]MapDef
	Factions  map[string]Faction
}

// Validate cross-checks references between definitions.
func (c *Content) Validate() error {
	if len(c.HeroIDs()) == 0 {
		return fmt.Errorf("no heroes in play: every faction is disabled")
	}
	if c.Rules.MaxTurns <= 0 || c.Rules.WinScore <= 0 {
		return fmt.Errorf("rules: max_turns and win_score must be positive")
	}
	if len(c.Rules.LevelThresholds) == 0 {
		return fmt.Errorf("rules: level_thresholds empty")
	}
	for _, h := range c.Heroes {
		if h.Faction != "" {
			if _, ok := c.Factions[h.Faction]; !ok {
				return fmt.Errorf("hero %s: unknown faction %q", h.ID, h.Faction)
			}
		}
		for k, id := range h.Abilities {
			if _, ok := c.Abilities[id]; !ok {
				return fmt.Errorf("hero %s: ability %s (%s) not defined", h.ID, id, k)
			}
			if _, ok := c.Rules.Unlock[k]; !ok {
				return fmt.Errorf("hero %s: ability key %q has no unlock level", h.ID, k)
			}
		}
	}
	for _, u := range c.Units {
		for _, id := range u.Abilities {
			if _, ok := c.Abilities[id]; !ok {
				return fmt.Errorf("unit %s: ability %s not defined", u.ID, id)
			}
		}
	}
	for _, a := range c.Abilities {
		switch a.Target {
		case TargetSelf, TargetUnit, TargetTile, TargetAll:
		default:
			return fmt.Errorf("ability %s: bad target %q", a.ID, a.Target)
		}
		for _, e := range a.Effects {
			if !knownEffect(e.Kind) {
				return fmt.Errorf("ability %s: unknown effect %q", a.ID, e.Kind)
			}
			if e.Kind == "spawn" {
				if _, ok := c.Units[e.Unit]; !ok {
					return fmt.Errorf("ability %s: spawn unit %q not defined", a.ID, e.Unit)
				}
			}
		}
	}
	for _, u := range c.Rules.UnitSet {
		if _, ok := c.Units[u]; !ok {
			return fmt.Errorf("rules unit_set: unit %q not defined", u)
		}
	}
	for name, m := range c.Rules.Modes {
		if m.Units > len(c.Rules.UnitSet) {
			return fmt.Errorf("mode %s: %d units but unit_set has %d", name, m.Units, len(c.Rules.UnitSet))
		}
	}
	for id, l := range c.Rules.Lengths {
		if id == LengthStandard || id == "" {
			return fmt.Errorf("length %q: the standard length is the rules as written", id)
		}
		if l.MaxTurns <= 0 || l.WinScorePercent <= 0 {
			return fmt.Errorf("length %s: max_turns and win_score_percent must be positive", id)
		}
	}
	for _, m := range c.Maps {
		if len(m.Tiles) != m.Width*m.Height {
			return fmt.Errorf("map %s: %d tiles for %dx%d", m.ID, len(m.Tiles), m.Width, m.Height)
		}

		for y := 0; y < m.Height; y++ {
			for x := 0; x < m.Width; x++ {
				i := y*m.Width + x
				rx, ry := m.Width-1-x, m.Height-1-y
				ri := ry*m.Width + rx

				if m.Tiles[i] != m.Tiles[ri] {
					return fmt.Errorf(
						"map %s: tile (%d,%d) differs from its 180-degree counterpart (%d,%d)",
						m.ID, x, y, rx, ry,
					)
				}
			}
		}
		for _, o := range m.Objectives {
			for _, p := range o.Tiles {
				if p.X < 0 || p.Y < 0 || p.X >= m.Width || p.Y >= m.Height {
					return fmt.Errorf("map %s: objective %s tile %v off board", m.ID, o.ID, p)
				}
				if m.Tiles[p.Y*m.Width+p.X].Terrain != TerrainObjective {
					return fmt.Errorf("map %s: objective %s tile %v is not objective terrain", m.ID, o.ID, p)
				}
			}
		}
	}
	return nil
}

var effectKinds = map[string]bool{
	"damage": true, "heal": true, "push": true, "pull": true, "reveal": true,
	"cloak": true, "smoke": true, "slow": true, "root": true, "shield": true,
	"teleport": true, "stat": true, "overwatch": true, "mark": true, "spawn": true,
	"silence": true, "taunt": true, "approach": true, "strike": true, "sacrifice": true,
}

func knownEffect(k string) bool { return effectKinds[k] }

// HeroIDs returns hero ids in sorted order.
// HeroIDs lists the heroes that can be drafted: every hero of an enabled
// faction. Heroes of a disabled faction stay in the data files, out of
// play, until the faction is switched on.
func (c *Content) HeroIDs() []string {
	ids := make([]string, 0, len(c.Heroes))
	for id, h := range c.Heroes {
		if f, ok := c.Factions[h.Faction]; ok && !f.Enabled {
			continue
		}
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// LengthIDs lists the match lengths on offer: standard first, then the
// others by turn limit.
func (c *Content) LengthIDs() []string {
	ids := make([]string, 0, len(c.Rules.Lengths))
	for id := range c.Rules.Lengths {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool {
		a, b := c.Rules.Lengths[ids[i]], c.Rules.Lengths[ids[j]]
		if a.MaxTurns != b.MaxTurns {
			return a.MaxTurns < b.MaxTurns
		}
		return ids[i] < ids[j]
	})
	return append([]string{LengthStandard}, ids...)
}

// MatchLength is the turn limit and the score to win of a mode played at a
// length ("" or "standard" is the rules as written).
func (c *Content) MatchLength(mode, length string) (maxTurns, winScore int, err error) {
	m, ok := c.Rules.Modes[mode]
	if !ok {
		return 0, 0, fmt.Errorf("unknown mode %q", mode)
	}
	maxTurns, winScore = c.Rules.MaxTurns, c.Rules.WinScore
	if m.WinScore > 0 {
		winScore = m.WinScore
	}
	if length == "" || length == LengthStandard {
		return maxTurns, winScore, nil
	}
	l, ok := c.Rules.Lengths[length]
	if !ok {
		return 0, 0, fmt.Errorf("unknown length %q", length)
	}
	return l.MaxTurns, (winScore*l.WinScorePercent + 50) / 100, nil
}

// FactionIDs lists factions in a stable order.
func (c *Content) FactionIDs() []string {
	ids := make([]string, 0, len(c.Factions))
	for id := range c.Factions {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// AbilityKey returns the key (q/w/e/r) for a hero's ability, or "".
func (c *Content) AbilityKey(hero, ability string) string {
	h, ok := c.Heroes[hero]
	if !ok {
		return ""
	}
	for k, id := range h.Abilities {
		if id == ability {
			return k
		}
	}
	return ""
}
