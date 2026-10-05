package engine

// View returns the fog-filtered state a player may see. Hidden information
// never leaves the server: enemy units outside vision are removed, enemy
// cooldowns are zeroed, other teams' reveal effects are dropped, sightings
// are limited to the player's team, and the log is filtered.
// View.Visible lists the tiles currently visible to the player's team.
func View(c *Content, s State, player int) State {
	p := s.Player(player)
	if p == nil {
		return State{Match: s.Match}
	}
	team := p.Team
	v := s.Clone()
	vis := s.VisibleTiles(team, &c.Rules)
	var units []Unit
	for _, u := range v.Units {
		if u.Team == team || !u.Alive() || s.CanSee(team, &u, vis) {
			if u.Team != team {
				u.Cooldowns = map[string]int{}
				u.Status = visibleStatuses(u.Status)
			}
			units = append(units, u)
		}
	}
	v.Units = units
	var effects []TileEffect
	for _, e := range v.Effects {
		if e.Kind == "reveal" && e.Team != team {
			continue
		}
		effects = append(effects, e)
	}
	v.Effects = effects
	var sightings []Sighting
	for _, sg := range v.Sightings {
		if sg.Team == team {
			sightings = append(sightings, sg)
		}
	}
	v.Sightings = sightings
	v.Log = FilterEvents(c, &s, team, v.Log)
	v.Visible = make([]Pos, 0, len(vis))
	for pos := range vis {
		v.Visible = append(v.Visible, pos)
	}
	v.Visible = SortedPos(v.Visible)
	return v
}

// visibleStatuses drops nothing today; enemy statuses are public information
// (they were applied by visible actions). Kept as a hook.
func visibleStatuses(st []Status) []Status { return st }

// FilterEvents keeps the events a team is allowed to see from a turn's log:
// anything involving a friendly unit, a visible tile at the end of the turn,
// or global information (turn markers, scores, telegraphs, match end).
// The filter is conservative: enemy actions on unseen tiles are hidden.
func FilterEvents(c *Content, s *State, team int, evs []Event) []Event {
	vis := s.VisibleTiles(team, &c.Rules)
	friendly := func(id int) bool {
		u := s.Unit(id)
		return u != nil && u.Team == team
	}
	seen := func(p Pos) bool { return vis[p] }
	var out []Event
	for _, e := range evs {
		keep := false
		switch e.Kind {
		case EvTurnStart, EvTurnEnd, EvMatchEnd, EvObjectiveScored, EvTelegraph, EvLevelUp, EvRespawned, EvSmoke:
			keep = true
		case EvOrderRejected:
			keep = friendly(e.Unit)
		case EvRevealed:
			keep = e.Team == team
		default:
			keep = friendly(e.Unit) || friendly(e.Target) || seen(e.To) || seen(e.From)
		}
		if keep {
			out = append(out, e)
		}
	}
	return out
}
