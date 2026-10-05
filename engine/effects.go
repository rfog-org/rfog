package engine

// resolveAbility applies every effect of ab. caster may be dead (delayed
// abilities still resolve); team is the caster's team at cast time.
// origin is the recorded target tile; targetU the recorded target unit (0 = none).
func (x *ctx) resolveAbility(caster *Unit, team int, ab AbilityDef, origin Pos, targetU int) {
	s := x.s
	casterID := 0
	if caster != nil {
		casterID = caster.ID
	}
	// Unit-targeted abilities follow the unit if it moved since the cast.
	tgt := s.Unit(targetU)
	if ab.Target == TargetUnit && tgt != nil && tgt.Alive() {
		origin = tgt.Pos
	}
	x.emit(Event{Kind: EvAbilityResolved, Unit: casterID, Ability: ab.ID, To: origin, Target: targetU, Team: team})
	// A fake caster stand-in for filters when the caster is dead.
	ref := caster
	if ref == nil {
		ref = &Unit{Team: team, ID: -1}
	}
	for _, e := range ab.Effects {
		// Re-fetch pointers: a spawn effect appends to s.Units.
		if caster != nil {
			caster = s.Unit(caster.ID)
		}
		if targetU != 0 {
			tgt = s.Unit(targetU)
		}
		if ref.ID > 0 {
			ref = caster
		}
		radius := ab.Area
		if e.Radius != nil {
			radius = *e.Radius
		}
		filter := e.Filter
		if filter == "" {
			filter = ab.Filter
		}
		if filter == "" {
			switch ab.Target {
			case TargetSelf:
				filter = FilterSelf
			default:
				filter = FilterAll
			}
		}
		switch e.Kind {
		case "smoke":
			tiles := x.openTiles(s.Board.Area(origin, radius))
			s.Effects = append(s.Effects, TileEffect{Kind: "smoke", Tiles: tiles, Turns: e.Turns, Team: -1, Source: casterID})
			x.emit(Event{Kind: EvSmoke, Unit: casterID, Tiles: tiles, Amount: e.Turns})
		case "reveal":
			tiles := s.Board.Area(origin, radius)
			s.Effects = append(s.Effects, TileEffect{Kind: "reveal", Tiles: tiles, Turns: e.Turns, Team: team, Source: casterID})
			x.emit(Event{Kind: EvRevealed, Unit: casterID, Team: team, Tiles: tiles, Amount: e.Turns})
		case "spawn":
			x.spawn(ref, e, origin)
		case "teleport":
			x.teleport(caster, tgt, e, origin)
		case "approach":
			if caster != nil && caster.Alive() && tgt.Alive() {
				x.approach(caster, tgt, e.Dist)
			}
		case "strike":
			if caster != nil && caster.Alive() && tgt.Alive() && CanAttack(x.c, s, caster, caster.Pos, tgt) {
				hits := x.rollAttack(caster, tgt, tgt.Pos, ab.ID)
				x.damage(caster, tgt, hits, ab.ID)
			}
		case "sacrifice":
			if tgt.Alive() {
				x.kill(nil, tgt, "sacrificed")
			}
		case "overwatch":
			if caster != nil && caster.Alive() {
				caster.Overwatch = true
				caster.OverwatchRNG = e.RNG
				caster.OverwatchShots = maxInt(e.Shots, 1)
				x.emit(Event{Kind: EvOverwatch, Unit: caster.ID, Amount: caster.OverwatchShots, Ability: ab.ID})
			}
		default:
			for _, u := range x.subjects(ref, ab, radius, filter, origin, tgt) {
				x.applyUnitEffect(caster, ref, ab, e, u, origin)
			}
		}
	}
}

// openTiles filters out walls.
func (x *ctx) openTiles(ps []Pos) []Pos {
	var out []Pos
	for _, p := range ps {
		if x.s.Board.Passable(p) {
			out = append(out, p)
		}
	}
	return out
}

// subjects picks the units an effect touches.
func (x *ctx) subjects(ref *Unit, ab AbilityDef, radius int, filter string, origin Pos, tgt *Unit) []*Unit {
	s := x.s
	var out []*Unit
	pick := func(u *Unit) {
		if !u.Alive() || !matchFilter(filter, ref, u) {
			return
		}
		// The kind restriction limits the chosen target (validated at order
		// time) and "all"-targeted abilities; area effects hit any kind.
		if ab.Kind != "" && ab.Target == TargetAll && u.Kind != ab.Kind {
			return
		}
		out = append(out, u)
	}
	switch {
	case ab.Target == TargetAll:
		for _, u := range s.AliveUnits() {
			pick(u)
		}
	case radius > 0 || ab.Target == TargetTile:
		for _, p := range s.Board.AreaFor(origin, radius, ref.Team) {
			if u := s.UnitAt(p); u != nil {
				pick(u)
			}
		}
	case ab.Target == TargetUnit:
		if tgt != nil {
			pick(tgt)
		}
	case ab.Target == TargetSelf:
		if ref.ID > 0 {
			pick(ref)
		}
	}
	return out
}

// applyUnitEffect applies one per-unit primitive to u.
func (x *ctx) applyUnitEffect(caster, ref *Unit, ab AbilityDef, e EffectDef, u *Unit, origin Pos) {
	casterID := ref.ID
	if casterID < 0 {
		casterID = 0
	}
	switch e.Kind {
	case "damage":
		x.damage(caster, u, e.N, ab.ID)
	case "heal":
		before := u.HP
		u.HP = minInt(u.MaxHP, u.HP+e.N)
		x.emit(Event{Kind: EvHealed, Unit: casterID, Target: u.ID, Amount: u.HP - before, Ability: ab.ID})
	case "push", "pull":
		src := origin
		if ab.Target == TargetUnit && caster != nil {
			src = caster.Pos
		}
		x.push(caster, u, src, e.Dist, e.Kind == "pull", ab.ID)
	case "slow":
		x.addStatus(u, Status{Kind: "slow", Magnitude: e.MV, Turns: e.Turns, Source: casterID}, ab.ID)
	case "root", "cloak", "silence":
		x.addStatus(u, Status{Kind: e.Kind, Magnitude: 1, Turns: e.Turns, Source: casterID}, ab.ID)
	case "taunt":
		x.addStatus(u, Status{Kind: "taunt", Magnitude: 1, Turns: e.Turns, Source: casterID}, ab.ID)
	case "shield":
		x.addStatus(u, Status{Kind: "shield", Magnitude: e.N, Turns: e.Turns, Source: casterID}, ab.ID)
	case "mark":
		x.addStatus(u, Status{Kind: "mark", Magnitude: e.Dice, Turns: e.Turns, Source: casterID}, ab.ID)
	case "stat":
		x.addStatus(u, Status{Kind: "stat", Stat: e.Stat, Magnitude: e.Delta, Turns: e.Turns, Source: casterID}, ab.ID)
	}
}

func (x *ctx) addStatus(u *Unit, st Status, ability string) {
	if st.Turns == 0 {
		return
	}
	u.Status = append(u.Status, st)
	x.emit(Event{Kind: EvStatus, Unit: st.Source, Target: u.ID, Status: st.Kind, Amount: st.Magnitude, Hits: st.Turns, Ability: ability, Reason: st.Stat})
}

// push moves u away from (or toward, if pull) src by up to dist tiles.
func (x *ctx) push(caster, u *Unit, src Pos, dist int, pull bool, ability string) {
	s := x.s
	r := &x.c.Rules
	if src == u.Pos || dist <= 0 {
		return
	}
	dir := StepToward(src, u.Pos)
	if pull {
		dir = Pos{-dir.X, -dir.Y}
	}
	from := u.Pos
	cur := u.Pos
	blocked := false
	for i := 0; i < dist; i++ {
		next := Pos{cur.X + dir.X, cur.Y + dir.Y}
		if pull && next == src {
			break
		}
		if !s.Board.Passable(next) || s.UnitAt(next) != nil {
			blocked = true
			break
		}
		if s.Board.At(next).Z-s.Board.At(cur).Z > r.PushMaxClimb {
			blocked = true
			break
		}
		cur = next
	}
	if cur != from {
		u.Pos = cur
		u.Held = false
		x.emit(Event{Kind: EvPushed, Unit: u.ID, From: from, To: cur, Ability: ability})
		drop := s.Board.At(from).Z - s.Board.At(cur).Z
		if drop >= r.FallThreshold {
			x.damage(caster, u, (drop-1)*r.FallDamagePerZ, "fall")
		}
	}
	if blocked && !pull {
		x.damage(caster, u, r.PushBlockDamage, "impact")
	}
}

// spawn creates e.Count units of e.Unit at and around origin.
func (x *ctx) spawn(ref *Unit, e EffectDef, origin Pos) {
	def, ok := x.c.Units[e.Unit]
	if !ok {
		return
	}
	for _, p := range freeTilesNear(x.s, origin, maxInt(e.Count, 1), 3, ref.Team) {
		u := newUnit(x.s, ref.Owner, ref.Team, def.ID, def.Name, def.Stats, false)
		u.Pos = p
		u.ExpiresIn = def.Expires
		x.emit(Event{Kind: EvSpawned, Unit: u.ID, To: p, Reason: def.ID, Team: ref.Team})
	}
}

// teleport moves the caster or target unit.
func (x *ctx) teleport(caster, tgt *Unit, e EffectDef, origin Pos) {
	s := x.s
	var who *Unit
	switch e.Who {
	case "target":
		who = tgt
	default:
		who = caster
	}
	if who == nil || !who.Alive() {
		return
	}
	var dest Pos
	switch e.To {
	case "adjacent_caster":
		if caster == nil || !caster.Alive() {
			return
		}
		cands := freeTilesNear(s, caster.Pos, 1, 1, caster.Team)
		if len(cands) == 0 || who.ID == caster.ID {
			return
		}
		// Nearest free tile adjacent to caster, preferring one closest to who.
		best := Pos{-1, -1}
		for _, p := range s.Board.NeighborsFor(caster.Pos, caster.Team) {
			if !s.Board.Passable(p) || s.UnitAt(p) != nil {
				continue
			}
			if best.X < 0 || Dist(p, who.Pos) < Dist(best, who.Pos) {
				best = p
			}
		}
		if best.X < 0 {
			return
		}
		dest = best
	default:
		dest = origin
		if !s.Board.Passable(dest) || (s.UnitAt(dest) != nil && s.UnitAt(dest).ID != who.ID) {
			return
		}
	}
	from := who.Pos
	who.Pos = dest
	who.Held = false
	x.emit(Event{Kind: EvTeleported, Unit: who.ID, From: from, To: dest})
}

// approach moves caster to the free tile within dist that is in attack range
// of tgt and nearest to caster (ties in the caster's reading order).
func (x *ctx) approach(caster, tgt *Unit, dist int) {
	s := x.s
	if CanAttack(x.c, s, caster, caster.Pos, tgt) {
		return
	}
	best := Pos{-1, -1}
	bestD := 1 << 30
	for _, p := range s.Board.Area(caster.Pos, dist) {
		if !s.Board.Passable(p) || s.UnitAt(p) != nil {
			continue
		}
		if !CanAttack(x.c, s, caster, p, tgt) {
			continue
		}
		d := Dist(p, caster.Pos)
		if d < bestD || (d == bestD && LessFor(caster.Team, p, best)) {
			best, bestD = p, d
		}
	}
	if best.X < 0 {
		return
	}
	from := caster.Pos
	caster.Pos = best
	caster.Held = false
	x.emit(Event{Kind: EvTeleported, Unit: caster.ID, From: from, To: best, Reason: "approach"})
}
