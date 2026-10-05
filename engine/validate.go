package engine

import (
	"errors"
	"fmt"
)

// Validate reports which orders are illegal for a player. One error per bad
// order plus errors for illegal combinations. An empty result means all legal.
func Validate(c *Content, s *State, player int, orders []Order) []error {
	var errs []error
	p := s.Player(player)
	if p == nil {
		return []error{fmt.Errorf("unknown player %d", player)}
	}
	if s.Ended() {
		return []error{errors.New("match has ended")}
	}
	perUnit := map[int][]Order{}
	for i, o := range orders {
		if err := checkOrder(c, s, player, o); err != nil {
			errs = append(errs, fmt.Errorf("order %d (unit %d %s): %w", i, o.UnitID, o.Action, err))
			continue
		}
		perUnit[o.UnitID] = append(perUnit[o.UnitID], o)
	}
	for uid, os := range perUnit {
		if err := checkCombo(c, s, s.Unit(uid), os); err != nil {
			errs = append(errs, fmt.Errorf("unit %d: %w", uid, err))
		}
	}
	return errs
}

// checkCombo validates the set of orders for one unit.
func checkCombo(c *Content, s *State, u *Unit, os []Order) error {
	limit := 1
	if u.IsCommander {
		limit = c.Rules.CommanderOrders
	}
	if len(os) > limit {
		return fmt.Errorf("too many orders (%d > %d)", len(os), limit)
	}
	seen := map[string]int{}
	abilities := 0
	instants := 0
	for _, o := range os {
		seen[o.Action]++
		if o.Action == ActAbility {
			abilities++
			if c.Abilities[o.Ability].Instant() {
				instants++
			}
		}
	}
	for act, n := range seen {
		if n > 1 && act != ActAbility {
			return fmt.Errorf("duplicate %s order", act)
		}
	}
	if abilities > 1 && instants == 0 {
		return errors.New("two abilities in one turn need at least one instant")
	}
	if abilities > 1 && os[0].Ability == os[1].Ability {
		return errors.New("same ability twice")
	}
	if seen[ActOverwatch] > 0 && seen[ActAttack] > 0 {
		return errors.New("overwatch forfeits attack")
	}
	if seen[ActHold] > 0 && seen[ActMove] > 0 {
		return errors.New("hold and move conflict")
	}
	return nil
}

// checkOrder validates a single order in isolation.
func checkOrder(c *Content, s *State, player int, o Order) error {
	u := s.Unit(o.UnitID)
	if u == nil {
		return errors.New("no such unit")
	}
	if u.Owner != player {
		return errors.New("not your unit")
	}
	if !u.Alive() {
		return errors.New("unit is dead")
	}
	switch o.Action {
	case ActHold, ActOverwatch:
		if o.Action == ActOverwatch && (u.Eff("rng") <= 0 || u.Eff("atk") <= 0) {
			return errors.New("unit cannot attack")
		}
		return nil
	case ActMove:
		if u.Rooted() {
			return errors.New("rooted")
		}
		if len(o.Path) == 0 {
			return errors.New("empty path")
		}
		if _, why := PathCost(c, s, u, o.Path); why != "" {
			return errors.New(why)
		}
		return nil
	case ActAttack:
		t := s.Unit(o.TargetU)
		if t == nil || !t.Alive() {
			return errors.New("no such target")
		}
		if t.Team == u.Team {
			return errors.New("cannot attack allies")
		}
		vis := s.VisibleTiles(u.Team, &c.Rules)
		if !s.CanSee(u.Team, t, vis) {
			return errors.New("target not visible")
		}
		if !CanAttack(c, s, u, u.Pos, t) {
			return errors.New("target out of range or line of sight")
		}
		return nil
	case ActAbility:
		return checkAbility(c, s, u, o)
	}
	return fmt.Errorf("unknown action %q", o.Action)
}

// UnitAbilities returns the ability ids a unit may currently use (ignoring
// cooldowns): hero abilities by unlock level, unit abilities by definition.
func UnitAbilities(c *Content, u *Unit) []string {
	var out []string
	if u.IsCommander {
		h, ok := c.Heroes[u.Kind]
		if !ok {
			return nil
		}
		for _, k := range []string{"q", "w", "e", "r"} {
			id, ok := h.Abilities[k]
			if !ok {
				continue
			}
			if u.Level >= c.Rules.Unlock[k] {
				out = append(out, id)
			}
		}
		return out
	}
	if d := unitDef(c, u); d != nil {
		out = append(out, d.Abilities...)
	}
	return out
}

func hasAbility(c *Content, u *Unit, id string) bool {
	for _, a := range UnitAbilities(c, u) {
		if a == id {
			return true
		}
	}
	return false
}

// matchFilter reports whether unit t passes filter relative to caster.
func matchFilter(filter string, caster, t *Unit) bool {
	switch filter {
	case FilterEnemies:
		return t.Team != caster.Team
	case FilterAllies:
		return t.Team == caster.Team
	case FilterSelf:
		return t.ID == caster.ID
	}
	return true
}

func checkAbility(c *Content, s *State, u *Unit, o Order) error {
	ab, ok := c.Abilities[o.Ability]
	if !ok {
		return fmt.Errorf("unknown ability %q", o.Ability)
	}
	if !hasAbility(c, u, ab.ID) {
		return errors.New("ability not available")
	}
	if u.Cooldowns[ab.ID] > 0 {
		return fmt.Errorf("on cooldown (%d)", u.Cooldowns[ab.ID])
	}
	if u.Silenced() {
		return errors.New("silenced")
	}
	vis := s.VisibleTiles(u.Team, &c.Rules)
	switch ab.Target {
	case TargetSelf, TargetAll:
		return nil
	case TargetUnit:
		t := s.Unit(o.TargetU)
		if t == nil || !t.Alive() {
			return errors.New("no such target")
		}
		filter := ab.Filter
		if filter == "" {
			filter = FilterAll
		}
		if !matchFilter(filter, u, t) {
			return errors.New("target does not match ability filter")
		}
		if ab.Kind != "" && t.Kind != ab.Kind {
			return fmt.Errorf("target must be a %s", ab.Kind)
		}
		if Dist(u.Pos, t.Pos) > ab.Range {
			return errors.New("target out of range")
		}
		if !s.CanSee(u.Team, t, vis) {
			return errors.New("target not visible")
		}
		return nil
	case TargetTile:
		if !s.Board.In(o.Target) {
			return errors.New("target off board")
		}
		if Dist(u.Pos, o.Target) > ab.Range {
			return errors.New("target out of range")
		}
		if ab.NeedsVisible() && !vis[o.Target] {
			return errors.New("target tile not visible")
		}
		for _, e := range ab.Effects {
			switch e.Kind {
			case "teleport", "spawn":
				if !s.Board.Passable(o.Target) {
					return errors.New("target tile impassable")
				}
				if e.Kind == "teleport" && e.To == "target" && s.UnitAt(o.Target) != nil {
					return errors.New("target tile occupied")
				}
			}
		}
		return nil
	}
	return fmt.Errorf("bad target type %q", ab.Target)
}
