package engine

import (
	"sort"
)

// ctx carries per-step working state.
type ctx struct {
	c         *Content
	s         *State
	rng       *RNG
	ev        []Event
	lastMoved []movedRec
	diedNow   map[int]bool // units that died this turn: no respawn tick yet
}

func (x *ctx) emit(e Event) {
	e.Turn = x.s.Match.Turn
	x.ev = append(x.ev, e)
	x.s.Log = append(x.s.Log, e)
}

// Step resolves one turn. It is pure: the input state is not modified, and
// the same (state, orders, seed) always yields the same output on every
// platform. Invalid orders are dropped and reported as OrderRejected events.
//
// Resolution order (SPEC §3.4):
//  1. instant abilities, descending INI; delayed abilities are cast (telegraphed)
//  2. delayed abilities whose timer reached 0 resolve at recorded targets
//  3. movement, simultaneous, contests by INI
//  4. overwatch shots
//  5. attacks, descending INI, same INI simultaneous
//  6. end-of-turn ticks: statuses, delayed timers, respawns, summons, cooldowns
//  7. objectives score, then win check
//
// Dice are consumed in exactly that order, per unit id within a band.
func Step(c *Content, in State, orders map[int][]Order, seed uint64) (State, []Event) {
	s := in.Clone()
	x := &ctx{c: c, s: &s, rng: NewRNG(seed, in.Match.Turn), diedNow: map[int]bool{}}
	if s.Ended() {
		return s, nil
	}
	x.emit(Event{Kind: EvTurnStart})

	valid := x.collect(orders)
	x.instantAbilities(valid)
	x.delayedAbilities()
	x.movement(valid)
	x.overwatch(valid)
	x.attacks(valid)
	x.endOfTurn()
	x.objectives()
	x.updateSightings()
	x.emit(Event{Kind: EvTurnEnd})
	if !x.winCheck() {
		s.Match.Turn++
		for i := range s.Players {
			s.Players[i].Committed = false
		}
	}
	return s, x.ev
}

// unitOrders is the validated order set for one unit. Units are referenced
// by id, never by pointer: spawning appends to s.Units and would invalidate
// any pointer held across it.
type unitOrders struct {
	id     int
	orders []Order
}

// collect validates orders in player order and returns them keyed by unit.
func (x *ctx) collect(orders map[int][]Order) map[int]*unitOrders {
	out := map[int]*unitOrders{}
	pids := make([]int, 0, len(orders))
	for pid := range orders {
		pids = append(pids, pid)
	}
	sort.Ints(pids)
	for _, pid := range pids {
		perUnit := map[int][]Order{}
		var order []int
		for _, o := range orders[pid] {
			o := o
			if err := checkOrder(x.c, x.s, pid, o); err != nil && !lenientMove(o, err) {
				x.emit(Event{Kind: EvOrderRejected, Unit: o.UnitID, Reason: err.Error(), Order: &o})
				continue
			}
			if _, ok := perUnit[o.UnitID]; !ok {
				order = append(order, o.UnitID)
			}
			perUnit[o.UnitID] = append(perUnit[o.UnitID], o)
		}
		for _, uid := range order {
			u := x.s.Unit(uid)
			os := perUnit[uid]
			if err := checkCombo(x.c, x.s, u, os); err != nil {
				for _, o := range os {
					o := o
					x.emit(Event{Kind: EvOrderRejected, Unit: uid, Reason: err.Error(), Order: &o})
				}
				continue
			}
			out[uid] = &unitOrders{id: uid, orders: os}
		}
	}
	// Apply hold and overwatch flags now so they cover this turn's resolution.
	for _, id := range orderedUnits(x.s, out) {
		u := x.s.Unit(id)
		for _, o := range out[id].orders {
			switch o.Action {
			case ActHold:
				u.Held = true
				x.emit(Event{Kind: EvHold, Unit: u.ID})
			case ActOverwatch:
				u.Overwatch = true
				u.OverwatchRNG = 0
				u.OverwatchShots = 1
				x.emit(Event{Kind: EvOverwatch, Unit: u.ID})
			}
		}
	}
	return out
}

// lenientMove accepts a move that only overspends movement points: instant
// abilities resolving before movement (Surge, Overclock, slows) change MV, so
// the movement phase truncates paths to what the unit can afford then.
func lenientMove(o Order, err error) bool {
	return o.Action == ActMove && err.Error() == "not enough movement"
}

// orderedUnits returns the ids of units with orders sorted by descending
// INI, id ascending. Callers re-fetch the unit by id when they use it.
func orderedUnits(s *State, valid map[int]*unitOrders) []int {
	var us []*Unit
	for _, uo := range valid {
		if u := s.Unit(uo.id); u != nil {
			us = append(us, u)
		}
	}
	byINI(us, s.TieTeam())
	ids := make([]int, len(us))
	for i, u := range us {
		ids[i] = u.ID
	}
	return ids
}

// ---- 1. abilities -------------------------------------------------------

func (x *ctx) instantAbilities(valid map[int]*unitOrders) {
	for _, id := range orderedUnits(x.s, valid) {
		for _, o := range valid[id].orders {
			if o.Action != ActAbility {
				continue
			}
			u := x.s.Unit(id) // re-fetch: a previous cast may have spawned units
			if !u.Alive() || u.Silenced() {
				x.emit(Event{Kind: EvOrderRejected, Unit: u.ID, Reason: "cannot cast", Ability: o.Ability})
				continue
			}
			ab := x.c.Abilities[o.Ability]
			if u.Cooldowns[ab.ID] > 0 {
				x.emit(Event{Kind: EvOrderRejected, Unit: u.ID, Reason: "on cooldown", Ability: o.Ability})
				continue
			}
			target, targetU := x.abilityOrigin(u, ab, o)
			u.Cooldowns[ab.ID] = x.cooldown(u, ab)
			x.emit(Event{Kind: EvAbilityCast, Unit: u.ID, Ability: ab.ID, To: target, Target: targetU, From: u.Pos})
			if ab.Delay > 0 {
				x.s.Delayed = append(x.s.Delayed, DelayedAbility{
					Caster: u.ID, Team: u.Team, Ability: ab.ID, Target: target, TargetU: targetU, Turns: ab.Delay,
				})
				x.emit(Event{Kind: EvTelegraph, Unit: u.ID, Ability: ab.ID, To: target,
					Tiles: x.s.Board.Area(target, ab.Area), Amount: ab.Delay})
				continue
			}
			x.resolveAbility(u, u.Team, ab, target, targetU)
		}
	}
}

// cooldown returns the cooldown to apply for u casting ab, after level bonuses.
func (x *ctx) cooldown(u *Unit, ab AbilityDef) int {
	cd := ab.Cooldown
	if u.IsCommander && u.Level >= x.c.Rules.CooldownReductionLevel {
		cd--
	}
	if cd < x.c.Rules.MinCooldown {
		cd = x.c.Rules.MinCooldown
	}
	return cd
}

// abilityOrigin records the target tile and unit for an order.
func (x *ctx) abilityOrigin(u *Unit, ab AbilityDef, o Order) (Pos, int) {
	switch ab.Target {
	case TargetSelf, TargetAll:
		return u.Pos, 0
	case TargetUnit:
		if t := x.s.Unit(o.TargetU); t != nil {
			return t.Pos, t.ID
		}
		return o.Target, o.TargetU
	}
	return o.Target, 0
}

// ---- 2. delayed ---------------------------------------------------------

func (x *ctx) delayedAbilities() {
	var keep []DelayedAbility
	for _, d := range x.s.Delayed {
		if d.Turns > 0 {
			keep = append(keep, d)
			continue
		}
		ab := x.c.Abilities[d.Ability]
		caster := x.s.Unit(d.Caster)
		x.resolveAbility(caster, d.Team, ab, d.Target, d.TargetU)
	}
	x.s.Delayed = keep
}

// ---- 3. movement --------------------------------------------------------

type mover struct {
	u    *Unit
	path []Pos
	idx  int // index into path of the currently claimed destination; -1 = stays
}

func (x *ctx) movement(valid map[int]*unitOrders) {
	s := x.s
	var movers []*mover
	for _, id := range orderedUnits(x.s, valid) {
		u := x.s.Unit(id)
		for _, o := range valid[id].orders {
			if o.Action != ActMove || !u.Alive() {
				continue
			}
			if u.Rooted() {
				x.emit(Event{Kind: EvOrderRejected, Unit: u.ID, Reason: "rooted"})
				continue
			}
			// Re-validate: abilities may have moved units or changed stats.
			path := o.Path
			if _, why := PathCost(x.c, s, u, path); why != "" {
				// Truncate to the longest legal prefix instead of dropping.
				path = x.longestLegalPrefix(u, path)
				if len(path) == 0 {
					x.emit(Event{Kind: EvOrderRejected, Unit: u.ID, Reason: why})
					continue
				}
			}
			movers = append(movers, &mover{u: u, path: path, idx: len(path) - 1})
		}
	}
	if len(movers) == 0 {
		return
	}
	moving := map[int]bool{}
	for _, m := range movers {
		moving[m.u.ID] = true
	}
	// Iteratively resolve destination contests.
	for iter := 0; iter < 64; iter++ {
		changed := false
		// Tiles held by units that are not (or no longer) moving.
		held := map[Pos]bool{}
		for _, u := range s.AliveUnits() {
			if !moving[u.ID] {
				held[u.Pos] = true
			}
		}
		for _, m := range movers {
			if m.idx < 0 {
				held[m.u.Pos] = true
			}
		}
		claims := map[Pos][]*mover{}
		for _, m := range movers {
			if m.idx < 0 {
				continue
			}
			claims[m.dest()] = append(claims[m.dest()], m)
		}
		for _, m := range movers {
			if m.idx < 0 {
				continue
			}
			d := m.dest()
			if held[d] {
				m.idx--
				changed = true
				continue
			}
			cl := claims[d]
			if len(cl) < 2 {
				continue
			}
			// Highest INI takes the tile; ties: everyone stops short.
			best, count := -1, 0
			for _, o := range cl {
				ini := o.u.Eff("ini")
				if ini > best {
					best, count = ini, 1
				} else if ini == best {
					count++
				}
			}
			if count > 1 || m.u.Eff("ini") < best {
				m.idx--
				changed = true
			}
		}
		if !changed {
			break
		}
		for _, m := range movers {
			if m.idx < 0 {
				moving[m.u.ID] = false
			}
		}
	}
	for _, m := range movers {
		if m.idx < 0 {
			continue
		}
		from := m.u.Pos
		path := m.path[:m.idx+1]
		m.u.Pos = path[len(path)-1]
		m.u.Held = false
		x.emit(Event{Kind: EvMoved, Unit: m.u.ID, From: from, To: m.u.Pos, Path: append([]Pos(nil), path...)})
		x.lastMoved = append(x.lastMoved, movedRec{m.u, from, path})
	}
}

func (m *mover) dest() Pos { return m.path[m.idx] }

func (x *ctx) longestLegalPrefix(u *Unit, path []Pos) []Pos {
	for n := len(path) - 1; n > 0; n-- {
		if _, why := PathCost(x.c, x.s, u, path[:n]); why == "" {
			return path[:n]
		}
	}
	return nil
}

// ---- 4. overwatch -------------------------------------------------------

type movedRec struct {
	u    *Unit
	from Pos
	path []Pos
}

func (x *ctx) overwatch(valid map[int]*unitOrders) {
	s := x.s
	var watchers []*Unit
	for _, u := range s.AliveUnits() {
		if u.Overwatch && u.OverwatchShots > 0 {
			watchers = append(watchers, u)
		}
	}
	byINI(watchers, s.TieTeam())
	for _, rec := range x.lastMoved {
		for _, w := range watchers {
			if !w.Alive() || w.OverwatchShots <= 0 || w.Team == rec.u.Team || !rec.u.Alive() {
				continue
			}
			rng := w.OverwatchRNG
			if rng <= 0 {
				rng = w.Eff("rng")
			}
			for _, p := range rec.path {
				r := rng
				if s.Board.At(w.Pos).Z > s.Board.At(p).Z {
					r += x.c.Rules.HeightRangeBonus
				}
				if Dist(w.Pos, p) > r || !s.LOS(w.Pos, p) {
					continue
				}
				w.OverwatchShots--
				hits := x.rollAttack(w, rec.u, p, "overwatch")
				x.damage(w, rec.u, hits, "overwatch")
				break
			}
		}
	}
}

// ---- 5. attacks ---------------------------------------------------------

func (x *ctx) attacks(valid map[int]*unitOrders) {
	type pending struct {
		a, t *Unit
	}
	ids := orderedUnits(x.s, valid)
	units := make([]*Unit, 0, len(ids))
	for _, id := range ids {
		units = append(units, x.s.Unit(id))
	}
	i := 0
	for i < len(units) {
		// Band of equal INI.
		band := units[i].Eff("ini")
		j := i
		for j < len(units) && units[j].Eff("ini") == band {
			j++
		}
		type roll struct {
			a, t *Unit
			hits int
		}
		var rolls []roll
		for _, u := range units[i:j] {
			for _, o := range valid[u.ID].orders {
				if o.Action != ActAttack {
					continue
				}
				if !u.Alive() || u.Overwatch {
					continue
				}
				t := x.s.Unit(o.TargetU)
				// Taunt redirects.
				if src := x.tauntSource(u); src != nil {
					t = src
				}
				if t == nil || !t.Alive() {
					x.emit(Event{Kind: EvOrderRejected, Unit: u.ID, Reason: "target gone"})
					continue
				}
				if !CanAttack(x.c, x.s, u, u.Pos, t) {
					x.emit(Event{Kind: EvOrderRejected, Unit: u.ID, Reason: "target out of range or line of sight"})
					continue
				}
				rolls = append(rolls, roll{u, t, x.rollAttack(u, t, t.Pos, "attack")})
			}
		}
		for _, r := range rolls {
			x.damage(r.a, r.t, r.hits, "attack")
		}
		i = j
	}
}

func (x *ctx) tauntSource(u *Unit) *Unit {
	for _, st := range u.Status {
		if st.Kind == "taunt" {
			if src := x.s.Unit(st.Source); src.Alive() {
				return src
			}
		}
	}
	return nil
}

// HitChance returns the probability that a single die hits a target number.
func HitChance(tn int) float64 {
	if tn > 6 {
		return 0
	}
	if tn < 1 {
		return 1
	}
	return float64(7-tn) / 6
}

// AttackPreview computes dice and target number for a planned attack, for
// the client's probability display. Position tp is where the target stands.
func AttackPreview(c *Content, s *State, a *Unit, t *Unit, tp Pos) (dice, tn int) {
	dice = a.Eff("atk") + t.StatusSum("mark", "")
	adjacent := Adjacent(a.Pos, tp)
	if d := unitDef(c, a); d != nil && adjacent {
		dice -= d.AdjacentDicePenalty
	}
	if dice < 0 {
		dice = 0
	}
	tn = t.Eff("def")
	if !adjacent && s.Board.At(tp).Terrain == TerrainCover {
		tn += c.Rules.CoverDefBonus
	}
	if t.Held {
		bonus := c.Rules.HoldDefBonus
		if d := unitDef(c, t); d != nil && d.HoldDefBonus > 0 {
			bonus = d.HoldDefBonus
		}
		tn += bonus
	}
	if tn > c.Rules.MaxDef {
		tn = c.Rules.MaxDef
	}
	return dice, tn
}

// ExpectedHits is the rounded-half-up expected value, used in deterministic mode.
func ExpectedHits(dice, tn int) int {
	// hits = dice * (7-tn) / 6, rounded half up.
	num := dice * (7 - tn)
	if num <= 0 {
		return 0
	}
	return (num*2 + 6) / 12
}

// rollAttack rolls dice for a against t standing on tp and emits Attacked.
func (x *ctx) rollAttack(a, t *Unit, tp Pos, why string) int {
	dice, tn := AttackPreview(x.c, x.s, a, t, tp)
	var faces []int
	hits := 0
	if x.s.Match.Deterministic {
		hits = ExpectedHits(dice, tn)
	} else {
		for i := 0; i < dice; i++ {
			f := x.rng.D6()
			faces = append(faces, f)
			if f >= tn {
				hits++
			}
		}
	}
	x.emit(Event{Kind: EvAttacked, Unit: a.ID, Target: t.ID, From: a.Pos, To: tp,
		Dice: faces, Hits: hits, Amount: tn, Reason: why})
	return hits
}

// damage applies n damage from src (may be nil) to t.
func (x *ctx) damage(src, t *Unit, n int, why string) {
	if n <= 0 || !t.Alive() {
		return
	}
	absorbed := 0
	for i := range t.Status {
		st := &t.Status[i]
		if st.Kind != "shield" || n <= 0 {
			continue
		}
		take := minInt(st.Magnitude, n)
		st.Magnitude -= take
		n -= take
		absorbed += take
	}
	// Drop exhausted shields.
	kept := t.Status[:0]
	for _, st := range t.Status {
		if st.Kind == "shield" && st.Magnitude <= 0 {
			continue
		}
		kept = append(kept, st)
	}
	t.Status = kept
	t.HP -= n
	if t.HP < 0 {
		t.HP = 0
	}
	srcID := 0
	if src != nil {
		srcID = src.ID
	}
	x.emit(Event{Kind: EvDamaged, Unit: srcID, Target: t.ID, Amount: n, Hits: absorbed, Reason: why, To: t.Pos})
	if t.HP == 0 {
		x.kill(src, t, why)
	}
}

// kill removes t from the board and credits src.
func (x *ctx) kill(src, t *Unit, why string) {
	r := &x.c.Rules
	t.Dead = true
	t.HP = 0
	t.Status = nil
	x.diedNow[t.ID] = true
	t.Overwatch = false
	t.Held = false
	x.emit(Event{Kind: EvDied, Unit: t.ID, Target: 0, From: t.Pos, Reason: why})
	if t.IsCommander {
		t.RespawnIn = minInt(r.RespawnBase+t.Level, r.RespawnCap)
		for i := range x.s.Teams {
			if x.s.Teams[i].ID != t.Team {
				x.s.Teams[i].Score += r.CommanderDeathPoints
				x.emit(Event{Kind: EvObjectiveScored, Team: x.s.Teams[i].ID, Amount: r.CommanderDeathPoints, Reason: "commander"})
			}
		}
	}
	if src == nil || src.Team == t.Team {
		return
	}
	if t.ExpiresIn > 0 && !r.SummonKillCredit {
		return // a summon is not a kill: it would feed the other side
	}
	src.Kills++
	x.s.Teams[src.Team].Kills++
	if p := x.s.Player(src.Owner); p != nil {
		if cmd := x.s.Unit(p.Commander); cmd != nil {
			xp := r.XPKillUnit
			if t.IsCommander {
				xp = r.XPKillCommander
			}
			x.gainXP(cmd, xp)
		}
	}
}

func (x *ctx) gainXP(cmd *Unit, n int) {
	if n <= 0 {
		return
	}
	cmd.XP += n
	lvl := 1
	for i, th := range x.c.Rules.LevelThresholds {
		if cmd.XP >= th {
			lvl = i + 1
		}
	}
	if lvl > cmd.Level {
		cmd.Level = lvl
		x.emit(Event{Kind: EvLevelUp, Unit: cmd.ID, Amount: lvl})
	}
}

// ---- 6. end of turn -----------------------------------------------------

func (x *ctx) endOfTurn() {
	s := x.s
	r := &x.c.Rules
	for i := range s.Units {
		u := &s.Units[i]
		if u.Alive() {
			kept := u.Status[:0]
			for _, st := range u.Status {
				if st.Turns > 0 {
					st.Turns--
				}
				if st.Turns == 0 {
					x.emit(Event{Kind: EvStatus, Unit: u.ID, Status: st.Kind, Reason: "expired"})
					continue
				}
				kept = append(kept, st)
			}
			u.Status = kept
			for k, v := range u.Cooldowns {
				if v > 0 {
					u.Cooldowns[k] = v - 1
				}
			}
			u.Overwatch = false
			u.OverwatchRNG = 0
			u.OverwatchShots = 0
			if u.ExpiresIn > 0 {
				u.ExpiresIn--
				if u.ExpiresIn == 0 {
					u.Dead = true
					u.HP = 0
					x.emit(Event{Kind: EvExpired, Unit: u.ID, From: u.Pos})
				}
			}
			continue
		}
		if u.IsCommander && u.RespawnIn > 0 && !x.diedNow[u.ID] {
			u.RespawnIn--
			if u.RespawnIn == 0 {
				if !x.respawn(u) {
					u.RespawnIn = 1 // retry next turn
				}
			}
		}
	}
	// Tile effects.
	kept := s.Effects[:0]
	for _, e := range s.Effects {
		e.Turns--
		if e.Turns > 0 {
			kept = append(kept, e)
		}
	}
	s.Effects = kept
	// Delayed timers.
	for i := range s.Delayed {
		if s.Delayed[i].Turns > 0 {
			s.Delayed[i].Turns--
		}
	}
	_ = r
}

func (x *ctx) respawn(u *Unit) bool {
	for _, p := range x.s.DeployTiles(u.Team) {
		if x.s.UnitAt(p) == nil {
			u.Pos = p
			u.Dead = false
			u.HP = u.MaxHP
			u.Status = nil
			u.Held = false
			x.emit(Event{Kind: EvRespawned, Unit: u.ID, To: p})
			return true
		}
	}
	return false
}

// ---- 7. objectives ------------------------------------------------------

func (x *ctx) objectives() {
	s := x.s
	// Work out who holds what first: under net scoring a point depends on
	// the whole board, not on one objective.
	held := map[int][]*Objective{}
	holdersOf := map[string][]*Unit{}
	for i := range s.Objectives {
		o := &s.Objectives[i]
		teams := map[int]bool{}
		var holders []*Unit
		for _, p := range o.Tiles {
			if u := s.UnitAt(p); u != nil {
				teams[u.Team] = true
				holders = append(holders, u)
			}
		}
		if len(teams) != 1 {
			o.Holder = -1
			continue
		}
		o.Holder = holders[0].Team
		held[o.Holder] = append(held[o.Holder], o)
		holdersOf[o.ID] = holders
	}
	lead := len(held[0]) - len(held[1])
	for i := range s.Objectives {
		o := &s.Objectives[i]
		if o.Holder < 0 {
			continue
		}
		team := o.Holder
		holders := holdersOf[o.ID]
		if x.c.Rules.Scoring == ScoreNet {
			// Only the team ahead scores, and only for its lead. The
			// points are attributed to its objectives in order, so the
			// events still say where they came from.
			ahead, n := 0, 0
			if lead > 0 {
				ahead, n = 0, lead
			} else if lead < 0 {
				ahead, n = 1, -lead
			} else {
				continue
			}
			if team != ahead {
				continue
			}
			paid := 0
			for _, ob := range held[ahead] {
				if paid >= n {
					break
				}
				if ob.ID == o.ID {
					break
				}
				paid++
			}
			if paid >= n {
				continue
			}
		}
		s.Teams[team].Score++
		x.emit(Event{Kind: EvObjectiveScored, Team: team, Amount: 1, Reason: o.ID, Tiles: o.Tiles})
		// Commander XP: a scoring unit adjacent to (or being) its commander.
		for _, pid := range s.Teams[team].Players {
			p := s.Player(pid)
			cmd := s.Unit(p.Commander)
			if cmd == nil || !cmd.Alive() {
				continue
			}
			for _, h := range holders {
				if h.ID == cmd.ID || Adjacent(h.Pos, cmd.Pos) {
					x.gainXP(cmd, x.c.Rules.XPObjectiveAdjacent)
					break
				}
			}
		}
	}
}

// winCheck ends the match if a team has won. Returns true if ended.
func (x *ctx) winCheck() bool {
	s := x.s
	reached := false
	for _, t := range s.Teams {
		if t.Score >= s.Match.WinScore {
			reached = true
		}
	}
	if !reached && s.Match.Turn < s.Match.MaxTurns {
		return false
	}
	a, b := s.Teams[0], s.Teams[1]
	winner, result := -1, "draw"
	switch {
	case a.Score != b.Score:
		winner = a.ID
		if b.Score > a.Score {
			winner = b.ID
		}
		result = "score"
		if !reached {
			result = "turns"
		}
	case a.Kills != b.Kills:
		winner = a.ID
		if b.Kills > a.Kills {
			winner = b.ID
		}
		result = "kills"
	}
	s.Match.Phase = PhaseEnded
	s.Match.Winner = winner
	s.Match.Result = result
	x.emit(Event{Kind: EvMatchEnd, Team: winner, Reason: result})
	return true
}

// updateSightings records last-seen positions of enemy units per team.
func (x *ctx) updateSightings() {
	s := x.s
	var kept []Sighting
	alive := map[int]bool{}
	for _, u := range s.AliveUnits() {
		alive[u.ID] = true
	}
	for _, sg := range s.Sightings {
		if alive[sg.UnitID] {
			kept = append(kept, sg)
		}
	}
	for _, t := range s.Teams {
		vis := s.VisibleTiles(t.ID, &x.c.Rules)
		for _, u := range s.AliveUnits() {
			if u.Team == t.ID || !s.CanSee(t.ID, u, vis) {
				continue
			}
			found := false
			for i := range kept {
				if kept[i].Team == t.ID && kept[i].UnitID == u.ID {
					kept[i].Pos = u.Pos
					kept[i].Turn = s.Match.Turn
					found = true
				}
			}
			if !found {
				kept = append(kept, Sighting{Team: t.ID, UnitID: u.ID, Pos: u.Pos, Turn: s.Match.Turn, Kind: u.Kind})
			}
		}
	}
	sort.SliceStable(kept, func(i, j int) bool {
		if kept[i].Team != kept[j].Team {
			return kept[i].Team < kept[j].Team
		}
		return kept[i].UnitID < kept[j].UnitID
	})
	s.Sightings = kept
}
