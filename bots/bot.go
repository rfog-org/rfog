// Package bots implements computer players. The term is always "bots".
//
// The v0 bot is rule-based with a one-ply lookahead: it scores every
// reachable tile by what it can attack from there, how close it gets to an
// objective, and how exposed it is, then picks abilities by a per-primitive
// value heuristic. It only ever sees its own fog-filtered view, exactly like
// a human, and is deterministic for a given seed.
package bots

import (
	"sort"

	"rfog/engine"
)

// Levels.
const (
	Easy   = "easy"
	Normal = "normal"
	Hard   = "hard"
)

// Bot is one computer player.
type Bot struct {
	Level string
	rng   *engine.RNG
}

// New creates a bot. seed makes tie-breaks and easy-mode noise reproducible.
func New(level string, seed uint64) *Bot {
	if level == "" {
		level = Normal
	}
	return &Bot{Level: level, rng: engine.NewRNG(seed, 7)}
}

// Orders returns the bot's orders for player given the player's view.
func (b *Bot) Orders(c *engine.Content, v *engine.State, player int) []engine.Order {
	p := v.Player(player)
	if p == nil || v.Ended() {
		return nil
	}
	pl := &planner{c: c, v: v, player: player, team: p.Team, bot: b}
	pl.prepare()
	var out []engine.Order
	for _, u := range v.AliveUnits() {
		if u.Owner != player {
			continue
		}
		os := pl.unitOrders(u)
		if b.Level == Easy && b.rng.Intn(100) < 30 {
			ro := engine.RandomOrders(c, v, player, b.rng)
			var mine []engine.Order
			for _, o := range ro {
				if o.UnitID == u.ID {
					mine = append(mine, o)
				}
			}
			if len(mine) > 0 {
				os = mine
			}
		}
		out = append(out, os...)
	}
	// Final safety: drop anything the engine would reject. Orders are
	// checked against the set kept so far, not on their own: three legal
	// orders for one commander are still two too many.
	if errs := engine.Validate(c, v, player, out); len(errs) == 0 {
		return out
	}
	var ok []engine.Order
	for _, o := range out {
		trial := append(append([]engine.Order{}, ok...), o)
		if len(engine.Validate(c, v, player, trial)) == 0 {
			ok = append(ok, o)
		}
	}
	return ok
}

type planner struct {
	c      *engine.Content
	v      *engine.State
	player int
	team   int
	bot    *Bot

	enemies   []*engine.Unit
	allies    []*engine.Unit
	telegraph map[engine.Pos]bool
	goals     map[int]engine.Pos // unit id -> objective tile to head for
	occupied  map[engine.Pos]bool
	claimed   map[engine.Pos]bool // destinations already chosen this turn
}

func (pl *planner) prepare() {
	v := pl.v
	vis := map[engine.Pos]bool{}
	for _, p := range v.Visible {
		vis[p] = true
	}
	for _, u := range v.AliveUnits() {
		if u.Team == pl.team {
			pl.allies = append(pl.allies, u)
		} else {
			pl.enemies = append(pl.enemies, u)
		}
	}
	pl.telegraph = map[engine.Pos]bool{}
	for _, d := range v.Delayed {
		ab := pl.c.Abilities[d.Ability]
		for _, p := range v.Board.Area(d.Target, ab.Area) {
			pl.telegraph[p] = true
		}
	}
	pl.occupied = map[engine.Pos]bool{}
	for _, u := range v.AliveUnits() {
		pl.occupied[u.Pos] = true
	}
	pl.claimed = map[engine.Pos]bool{}
	pl.assignGoals()
}

// assignGoals gives each unit an objective tile: nearest first, spreading
// units so that every objective gets attention; the commander follows the
// centre objective unless it is the closest to another.
func (pl *planner) assignGoals() {
	v := pl.v
	pl.goals = map[int]engine.Pos{}
	type obj struct {
		tile   engine.Pos
		id     string
		weight int // units assigned
		enemy  bool
	}
	// Objectives and their tiles in our reading order, so that ties fall
	// the same way for both sides of a rotated map.
	objectives := make([]engine.Objective, len(v.Objectives))
	for i, o := range v.Objectives {
		o.Tiles = engine.SortedPosFor(o.Tiles, pl.team)
		objectives[i] = o
	}
	sort.SliceStable(objectives, func(i, j int) bool {
		return engine.LessFor(pl.team, objectives[i].Tiles[0], objectives[j].Tiles[0])
	})
	var objs []obj
	for _, o := range objectives {
		enemyOn := false
		for _, t := range o.Tiles {
			if u := v.UnitAt(t); u != nil && u.Team != pl.team {
				enemyOn = true
			}
		}
		// Use the objective tile nearest to our side as its anchor.
		objs = append(objs, obj{tile: o.Tiles[0], id: o.ID, enemy: enemyOn})
	}
	if len(objs) == 0 {
		return
	}
	// The whole team is assigned, in id order, and only our own units keep
	// their goals. Teammates share vision, so every bot on the team works
	// out the same plan and they spread over the objectives instead of
	// each sending everything to the nearest one.
	units := append([]*engine.Unit(nil), pl.allies...)
	sort.Slice(units, func(i, j int) bool { return units[i].ID < units[j].ID })
	for _, u := range units {
		best, bestScore := -1, 1<<30
		for i, o := range objs {
			// Nearest tile of the objective to the unit.
			d := 1 << 30
			for _, ob := range objectives {
				if ob.ID != o.id {
					continue
				}
				for _, t := range ob.Tiles {
					if dd := engine.Dist(u.Pos, t); dd < d {
						d = dd
					}
				}
			}
			score := d*2 + o.weight*5
			if o.enemy {
				score -= 2 // contest
			}
			if score < bestScore {
				best, bestScore = i, score
			}
		}
		if best >= 0 {
			objs[best].weight++
			// Aim at the objective tile nearest the unit.
			target := objs[best].tile
			for _, ob := range objectives {
				if ob.ID == objs[best].id {
					for _, t := range ob.Tiles {
						if engine.Dist(u.Pos, t) < engine.Dist(u.Pos, target) {
							target = t
						}
					}
				}
			}
			if u.Owner == pl.player {
				pl.goals[u.ID] = target
			}
		}
	}
}

// danger estimates incoming expected hits on a unit standing at p.
func (pl *planner) danger(u *engine.Unit, p engine.Pos) float64 {
	d := 0.0
	for _, e := range pl.enemies {
		if e.Eff("rng") <= 0 || e.Eff("atk") <= 0 {
			continue
		}
		r := e.Eff("rng") + e.Eff("mv")/2 // enemies can move first
		if engine.Dist(e.Pos, p) > r {
			continue
		}
		dice, tn := engine.AttackPreview(pl.c, pl.v, e, u, p)
		d += float64(dice) * engine.HitChance(tn)
	}
	if pl.telegraph[p] {
		d += 3
	}
	return d
}

// bestTarget returns the most valuable enemy attackable by u from p.
func (pl *planner) bestTarget(u *engine.Unit, p engine.Pos) (*engine.Unit, float64) {
	var best *engine.Unit
	bestV := 0.0
	for _, e := range pl.enemies {
		if !engine.CanAttack(pl.c, pl.v, u, p, e) {
			continue
		}
		dice, tn := engine.AttackPreview(pl.c, pl.v, u, e, e.Pos)
		exp := float64(dice) * engine.HitChance(tn)
		val := exp
		if exp >= float64(e.HP) {
			val += 3 // likely kill
		}
		if e.IsCommander {
			val += 1
		}
		val += float64(e.MaxHP-e.HP) * 0.2 // focus the wounded
		if val > bestV || (val == bestV && best != nil && e.ID < best.ID) {
			best, bestV = e, val
		}
	}
	return best, bestV
}

// unitOrders plans one unit (one order) or a commander (two).
func (pl *planner) unitOrders(u *engine.Unit) []engine.Order {
	out := pl.planUnit(u)
	// A commander may act twice, anything else once. The planner can
	// produce an ability, a move and an attack in the same turn, which is
	// one more than the rules allow.
	limit := 1
	if u.IsCommander {
		limit = pl.c.Rules.CommanderOrders
	}
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

func (pl *planner) planUnit(u *engine.Unit) []engine.Order {
	var out []engine.Order
	lowHP := u.HP*10 <= u.MaxHP*3
	// Abilities first: they resolve before movement.
	usedAbility := false
	if o, ok := pl.chooseAbility(u); ok {
		out = append(out, o)
		usedAbility = true
		if ab := pl.c.Abilities[o.Ability]; ab.Instant() && profile(ab).spawn {
			// The spawn lands before anyone moves: never on the tile this
			// commander is about to walk onto.
			pl.claimed[o.Target] = true
		}
	}
	if !u.IsCommander && usedAbility {
		return out
	}
	// Commander retreat.
	if u.IsCommander && lowHP && len(pl.enemies) > 0 {
		if o, ok := pl.retreat(u); ok {
			out = append(out, o)
			return out
		}
	}
	// Move / attack.
	moveTo, attackFrom, target := pl.planMove(u)
	if moveTo != nil {
		out = append(out, engine.Order{UnitID: u.ID, Action: engine.ActMove, Path: moveTo})
		pl.claimed[attackFrom] = true
	}
	if target != nil && (u.IsCommander || moveTo == nil) {
		out = append(out, engine.Order{UnitID: u.ID, Action: engine.ActAttack, TargetU: target.ID, Target: target.Pos})
		return out
	}
	if moveTo == nil && !usedAbility {
		// Nothing to do: hold on objectives, overwatch if ranged and enemies may come.
		onObjective := pl.v.Board.At(u.Pos).Terrain == engine.TerrainObjective
		if u.Eff("rng") >= 3 && u.Eff("atk") > 0 && len(pl.enemies)+len(pl.v.Sightings) > 0 && !onObjective {
			out = append(out, engine.Order{UnitID: u.ID, Action: engine.ActOverwatch})
		} else {
			out = append(out, engine.Order{UnitID: u.ID, Action: engine.ActHold})
		}
	}
	return out
}

// planMove scores staying put and every reachable tile. Returns the path (nil
// to stay), the resulting tile and the attack target from there (may be nil).
func (pl *planner) planMove(u *engine.Unit) ([]engine.Pos, engine.Pos, *engine.Unit) {
	reach := engine.Reachable(pl.c, pl.v, u)
	goal, hasGoal := pl.goals[u.ID]
	type cand struct {
		p     engine.Pos
		path  []engine.Pos
		score float64
		tgt   *engine.Unit
	}
	var cands []cand
	eval := func(p engine.Pos, path []engine.Pos) {
		tgt, val := pl.bestTarget(u, p)
		s := val * 2
		if hasGoal {
			s -= float64(engine.Dist(p, goal)) * 1.5
			if pl.v.Board.At(p).Terrain == engine.TerrainObjective {
				s += 4
			}
		}
		// A long-ranged commander can shoot from almost anywhere, so a
		// shot must not outbid standing on its objective: without this it
		// parks where it can fire and never holds anything.
		if u.IsCommander && u.Eff("rng") >= 3 && hasGoal && p != goal && pl.v.Board.At(p).Terrain != engine.TerrainObjective {
			s -= val
		}
		if pl.v.Board.At(p).Terrain == engine.TerrainCover {
			s += 0.5
		}
		s += float64(pl.v.Board.At(p).Z) * 0.3
		s -= pl.danger(u, p) * 0.8
		if u.IsCommander {
			s -= pl.danger(u, p) * 0.7
		}
		if pl.claimed[p] && p != u.Pos {
			s -= 10
		}
		if len(path) == 0 && u.Held {
			s += 0.3
		}
		cands = append(cands, cand{p, path, s, tgt})
	}
	eval(u.Pos, nil)
	var tiles []engine.Pos
	for p := range reach {
		tiles = append(tiles, p)
	}
	tiles = engine.SortedPosFor(tiles, pl.team)
	for _, p := range tiles {
		eval(p, reach[p])
	}
	best := cands[0]
	for _, c := range cands[1:] {
		if c.score > best.score+1e-9 {
			best = c
		}
	}
	if len(best.path) == 0 {
		return nil, u.Pos, best.tgt
	}
	// Non-commanders cannot move and attack; if staying and attacking is
	// nearly as good, prefer the attack.
	if !u.IsCommander && cands[0].tgt != nil && cands[0].score+1.0 >= best.score {
		return nil, u.Pos, cands[0].tgt
	}
	return best.path, best.p, best.tgt
}

// retreat moves the commander to the reachable tile farthest from enemies,
// biased toward its own deploy edge.
func (pl *planner) retreat(u *engine.Unit) (engine.Order, bool) {
	reach := engine.Reachable(pl.c, pl.v, u)
	if len(reach) == 0 {
		return engine.Order{}, false
	}
	homeX := 0
	if pl.team == 1 {
		homeX = pl.v.Board.W - 1
	}
	var bestP engine.Pos
	var bestPath []engine.Pos
	bestS := -1e9
	var tiles []engine.Pos
	for p := range reach {
		tiles = append(tiles, p)
	}
	for _, p := range engine.SortedPosFor(tiles, pl.team) {
		s := -pl.danger(u, p)*2 - float64(abs(p.X-homeX))*0.3
		for _, e := range pl.enemies {
			s += float64(engine.Dist(p, e.Pos)) * 0.2
		}
		if s > bestS {
			bestS, bestP, bestPath = s, p, reach[p]
		}
	}
	if bestS <= -pl.danger(u, u.Pos)*2 {
		return engine.Order{}, false
	}
	pl.claimed[bestP] = true
	return engine.Order{UnitID: u.ID, Action: engine.ActMove, Path: bestPath}, true
}

// chooseAbility evaluates each available ability and casts the best one if
// its value beats its cooldown cost.
func (pl *planner) chooseAbility(u *engine.Unit) (engine.Order, bool) {
	var best engine.Order
	bestV := 0.0
	for _, id := range engine.UnitAbilities(pl.c, u) {
		if u.Cooldowns[id] > 0 || u.Silenced() {
			continue
		}
		ab := pl.c.Abilities[id]
		o, val := pl.evalAbility(u, ab)
		if val <= 0 {
			continue
		}
		cost := 1.0 + float64(ab.Cooldown)*0.4
		if val >= cost && val > bestV {
			best, bestV = o, val
		}
	}
	if bestV == 0 {
		return engine.Order{}, false
	}
	if len(engine.Validate(pl.c, pl.v, pl.player, []engine.Order{best})) > 0 {
		return engine.Order{}, false
	}
	return best, true
}

// abilityProfile summarises what an ability does, from its primitives.
type abilityProfile struct {
	damage, heal, shield int
	hostile, friendly    bool
	spawn, reveal, smoke bool
	overwatch, strike    bool
	teleportSelf         bool
	teleportAlly         bool
	buff                 bool
	control              bool
	pin                  bool // root or slow: stops a unit getting somewhere
}

func profile(ab engine.AbilityDef) abilityProfile {
	var p abilityProfile
	for _, e := range ab.Effects {
		switch e.Kind {
		case "damage":
			p.damage += e.N
			p.hostile = true
		case "push", "pull", "slow", "root", "silence", "mark", "taunt":
			p.hostile, p.control = true, true
			if e.Kind == "root" || e.Kind == "slow" {
				p.pin = true
			}
		case "heal":
			p.heal += e.N
			p.friendly = true
		case "shield":
			p.shield += e.N
			p.friendly = true
		case "stat":
			if e.Delta > 0 {
				p.buff, p.friendly = true, true
			} else {
				p.hostile, p.control = true, true
			}
		case "spawn":
			p.spawn = true
		case "reveal":
			p.reveal = true
		case "smoke":
			p.smoke = true
		case "overwatch":
			p.overwatch = true
		case "strike", "approach":
			p.strike, p.hostile = true, true
		case "teleport":
			if e.Who == "target" {
				p.teleportAlly = true
			} else {
				p.teleportSelf = true
			}
		case "sacrifice":
			p.hostile = true
		}
	}
	return p
}

// evalAbility picks the best target for ab and returns the order and value.
func (pl *planner) evalAbility(u *engine.Unit, ab engine.AbilityDef) (engine.Order, float64) {
	pr := profile(ab)
	o := engine.Order{UnitID: u.ID, Action: engine.ActAbility, Ability: ab.ID}
	switch ab.Target {
	case engine.TargetSelf:
		switch {
		case pr.overwatch:
			// Worth it when enemies are visible but none attackable now.
			if len(pl.enemies) == 0 {
				return o, 0
			}
			if t, _ := pl.bestTarget(u, u.Pos); t != nil {
				return o, 0
			}
			return o, 2.5
		case pr.shield > 0:
			if pl.danger(u, u.Pos) > 0.5 {
				return o, 2.5
			}
			return o, 0
		case pr.damage > 0:
			// Self-centred area (Breach): count enemies around.
			n := pl.countEnemies(u.Pos, ab.Area)
			return o, float64(n*pr.damage) * 1.2
		}
		return o, 0
	case engine.TargetAll:
		if pr.buff {
			// Buff when a fight is on: enemies visible near allies.
			n := 0
			for _, a := range pl.allies {
				if ab.Kind != "" && a.Kind != ab.Kind {
					continue
				}
				for _, e := range pl.enemies {
					if engine.Dist(a.Pos, e.Pos) <= 6 {
						n++
						break
					}
				}
			}
			return o, float64(n) * 1.5
		}
		return o, 0
	case engine.TargetUnit:
		var best *engine.Unit
		bestV := 0.0
		for _, t := range pl.v.AliveUnits() {
			if engine.Dist(u.Pos, t.Pos) > ab.Range {
				continue
			}
			if ab.Kind != "" && t.Kind != ab.Kind {
				continue
			}
			v := 0.0
			switch {
			case pr.hostile && t.Team != pl.team:
				if pr.strike {
					dice, tn := engine.AttackPreview(pl.c, pl.v, u, t, t.Pos)
					v = float64(dice)*engine.HitChance(tn) + 1
				}
				v += float64(pr.damage) * 1.2
				if pr.control {
					v += 1.5
					// Pinning an enemy that could reach an objective
					// this turn is what a root or slow is for.
					if pr.pin && pl.nearObjective(t) {
						v += 1.5
					}
				}
				if t.IsCommander {
					v += 0.5
				}
				if pr.damage > 0 && pr.damage >= t.HP {
					v += 3
				}
				if ab.ID == "scuttle" {
					// Sacrifice value: enemies adjacent to the junkbot.
					v = float64(pl.countEnemies(t.Pos, 1)) * 2.0
					if t.Team != pl.team {
						v = 0
					}
				}
			case pr.friendly && t.Team == pl.team:
				missing := t.MaxHP - t.HP
				if pr.heal > 0 {
					h := minInt(pr.heal, missing)
					v = float64(h) * 1.0
					if t.IsCommander {
						v *= 1.3
					}
				}
				if pr.shield > 0 && pl.danger(t, t.Pos) > 0.5 {
					v += 2
				}
				if pr.teleportAlly {
					// Pull back a wounded ally under threat.
					if missing > 0 && pl.danger(t, t.Pos) > 1.0 && engine.Dist(u.Pos, t.Pos) > 1 && t.ID != u.ID {
						v = 2.5
					}
				}
			case ab.ID == "scuttle" && t.Team == pl.team:
				v = float64(pl.countEnemies(t.Pos, 1)) * 2.0
			}
			if v > bestV {
				best, bestV = t, v
			}
		}
		if best == nil {
			return o, 0
		}
		o.TargetU, o.Target = best.ID, best.Pos
		return o, bestV
	case engine.TargetTile:
		vis := map[engine.Pos]bool{}
		for _, p := range pl.v.Visible {
			vis[p] = true
		}
		var bestP engine.Pos
		bestV := 0.0
		for _, p := range pl.v.Board.AreaFor(u.Pos, ab.Range, pl.team) {
			if ab.NeedsVisible() && !vis[p] {
				continue
			}
			v := 0.0
			switch {
			case pr.spawn:
				g, hasGoal := pl.goals[u.ID]
				// Toward the goal, but not on it: the caster is walking there.
				if pl.v.Board.Passable(p) && !pl.occupied[p] && !pl.claimed[p] && !(hasGoal && p == g) {
					v = 3
					if hasGoal {
						v -= float64(engine.Dist(p, g)) * 0.05
					}
				}
			case pr.damage > 0 || pr.control:
				en := pl.countEnemies(p, ab.Area)
				al := pl.countAllies(p, ab.Area)
				v = float64(en*pr.damage)*1.1 + float64(en)*boolF(pr.control)*1.5
				if pr.damage > 0 && ab.Filter == engine.FilterAll {
					v -= float64(al*pr.damage) * 1.5
				}
				if ab.Delay > 0 {
					v *= 0.7 // they may step away
				}
			case pr.smoke:
				// Screen a wounded ally from ranged enemies.
				for _, a := range pl.allies {
					if a.HP < a.MaxHP && engine.Dist(a.Pos, p) <= 1 && pl.danger(a, a.Pos) > 1 {
						v = 2
					}
				}
			case pr.reveal:
				if len(pl.enemies) == 0 && !vis[p] {
					// Look toward the enemy side.
					far := 0
					if pl.team == 0 {
						far = p.X
					} else {
						far = pl.v.Board.W - 1 - p.X
					}
					v = 1.6 + float64(far)*0.01
				}
			case pr.teleportSelf:
				if !pl.v.Board.Passable(p) || pl.occupied[p] {
					break
				}
				if u.HP*10 <= u.MaxHP*3 {
					v = 3 - pl.danger(u, p)
				} else if pl.v.Board.At(u.Pos).Terrain == engine.TerrainObjective {
					// Already holding: a better firing spot is not worth the point.
				} else if g, ok := pl.goals[u.ID]; ok && engine.Dist(p, g) > engine.Dist(u.Pos, g) {
					// Jumping away from the objective to get a shot loses it.
				} else if t, val := pl.bestTarget(u, p); t != nil && val > 1 {
					if cur, _ := pl.bestTarget(u, u.Pos); cur == nil {
						v = val - pl.danger(u, p)*0.5
					}
				}
			}
			if v > bestV {
				bestP, bestV = p, v
			}
		}
		if bestV <= 0 {
			return o, 0
		}
		o.Target = bestP
		return o, bestV
	}
	return o, 0
}

// nearObjective reports whether u could step onto an objective this turn.
func (pl *planner) nearObjective(u *engine.Unit) bool {
	for _, o := range pl.v.Objectives {
		for _, t := range o.Tiles {
			if engine.Dist(u.Pos, t) <= u.Eff("mv") {
				return true
			}
		}
	}
	return false
}

func (pl *planner) countEnemies(c engine.Pos, r int) int {
	n := 0
	for _, e := range pl.enemies {
		if engine.Dist(e.Pos, c) <= r {
			n++
		}
	}
	return n
}

func (pl *planner) countAllies(c engine.Pos, r int) int {
	n := 0
	for _, a := range pl.allies {
		if engine.Dist(a.Pos, c) <= r {
			n++
		}
	}
	return n
}

func boolF(b bool) float64 {
	if b {
		return 1
	}
	return 0
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
