package engine

// RandomOrders produces a legal, random set of orders for a player. It is
// used by property tests and by the easy bot; the result depends only on the
// state and the generator, so it is deterministic.
func RandomOrders(c *Content, s *State, player int, rng *RNG) []Order {
	var out []Order
	p := s.Player(player)
	if p == nil {
		return nil
	}
	vis := s.VisibleTiles(p.Team, &c.Rules)
	var enemies []*Unit
	for _, u := range s.AliveUnits() {
		if u.Team != p.Team && s.CanSee(p.Team, u, vis) {
			enemies = append(enemies, u)
		}
	}
	for _, u := range s.AliveUnits() {
		if u.Owner != player {
			continue
		}
		n := 1
		if u.IsCommander {
			n = c.Rules.CommanderOrders
		}
		var mine []Order
		for i := 0; i < n; i++ {
			o, ok := randomOrder(c, s, u, enemies, vis, rng, mine)
			if !ok {
				continue
			}
			trial := append(append([]Order(nil), mine...), o)
			if checkCombo(c, s, u, trial) != nil {
				continue
			}
			mine = trial
		}
		out = append(out, mine...)
	}
	return out
}

func randomOrder(c *Content, s *State, u *Unit, enemies []*Unit, vis map[Pos]bool, rng *RNG, have []Order) (Order, bool) {
	// Weighted choice: move 4, attack 3, ability 3, hold 1, overwatch 1.
	roll := rng.Intn(12)
	var o Order
	switch {
	case roll < 4:
		reach := Reachable(c, s, u)
		if len(reach) == 0 {
			return o, false
		}
		var dests []Pos
		for p := range reach {
			dests = append(dests, p)
		}
		dests = SortedPos(dests)
		d := dests[rng.Intn(len(dests))]
		o = Order{UnitID: u.ID, Action: ActMove, Path: reach[d]}
	case roll < 7:
		var targets []*Unit
		for _, e := range enemies {
			if CanAttack(c, s, u, u.Pos, e) {
				targets = append(targets, e)
			}
		}
		if len(targets) == 0 {
			return o, false
		}
		t := targets[rng.Intn(len(targets))]
		o = Order{UnitID: u.ID, Action: ActAttack, TargetU: t.ID, Target: t.Pos}
	case roll < 10:
		abs := UnitAbilities(c, u)
		if len(abs) == 0 {
			return o, false
		}
		ab := c.Abilities[abs[rng.Intn(len(abs))]]
		o = Order{UnitID: u.ID, Action: ActAbility, Ability: ab.ID}
		switch ab.Target {
		case TargetUnit:
			var cands []*Unit
			for _, t := range s.AliveUnits() {
				if Dist(u.Pos, t.Pos) <= ab.Range {
					cands = append(cands, t)
				}
			}
			if len(cands) == 0 {
				return o, false
			}
			t := cands[rng.Intn(len(cands))]
			o.TargetU, o.Target = t.ID, t.Pos
		case TargetTile:
			area := s.Board.Area(u.Pos, ab.Range)
			o.Target = area[rng.Intn(len(area))]
		}
	case roll < 11:
		o = Order{UnitID: u.ID, Action: ActHold}
	default:
		o = Order{UnitID: u.ID, Action: ActOverwatch}
	}
	if err := checkOrder(c, s, u.Owner, o); err != nil {
		return o, false
	}
	return o, true
}
