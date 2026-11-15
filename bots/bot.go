// Package bots implements computer players. The term is always "bots".
//
// The bot is rule-based: it scores every reachable tile by what it can
// attack from there, how close it gets to an objective, and how exposed it
// is, then picks abilities by a per-primitive value heuristic. Easy adds
// noise to that; hard plays several such plans a turn ahead and keeps the
// best. A bot only ever sees its own fog-filtered view, exactly like a
// human, and is deterministic for a given seed.
package bots

import (
	"math"
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
	Level   string
	Dropped int // planned orders the engine would have refused: a planner bug when not zero
	rng     *engine.RNG
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
	best := b.plan(c, v, player, 0)
	if b.Level != Hard {
		return best
	}
	// Hard looks ahead: the plan and looser variants of it are each played
	// a turn and a reply forward on what the bot can see, everyone else on
	// the normal plan, with the same dice for every candidate.
	normal := &Bot{Level: Normal}
	all := map[int][]engine.Order{}
	for _, q := range v.Players {
		if q.ID != player {
			all[q.ID] = normal.plan(c, v, q.ID, 0)
		}
	}
	bestV := math.Inf(-1)
	for k := 0; k < hardPlans; k++ {
		cand := best
		if k > 0 {
			cand = b.plan(c, v, player, hardJitter)
		}
		all[player] = cand
		val := 0.0
		for roll := uint64(0); roll < hardRolls; roll++ {
			s, _ := engine.Step(c, *v, all, roll)
			if !s.Ended() {
				next := map[int][]engine.Order{}
				for _, q := range s.Players {
					next[q.ID] = normal.plan(c, &s, q.ID, 0)
				}
				s, _ = engine.Step(c, s, next, roll)
			}
			val += worth(&s, p.Team)
		}
		if val > bestV {
			best, bestV = cand, val
		}
	}
	return best
}

// What hard spends a turn on: plans tried, dice rolled for each, and how far
// a variant's tile scores are shaken from the plain plan's (measured in
// DECISIONS: wider and deeper both cost more than they win).
const (
	hardPlans  = 8
	hardRolls  = 2
	hardJitter = 4.0
)

// worth is how good a position is for team: the match, then points, then
// health, a commander's counting half again.
func worth(s *engine.State, team int) float64 {
	side := func(t int) float64 {
		if t == team {
			return 1
		}
		return -1
	}
	v := 0.0
	if s.Ended() && s.Match.Winner >= 0 {
		v += 100 * side(s.Match.Winner)
	}
	for _, t := range s.Teams {
		v += side(t.ID) * (float64(t.Score)*6 + float64(t.Kills)*0.5)
	}
	for _, u := range s.AliveUnits() {
		hp := float64(u.HP)
		if u.IsCommander {
			hp *= 1.5
		}
		v += side(u.Team) * hp
	}
	return v
}

// plan is the rule-based planner: one pass over the player's units. jitter
// shakes its tile scores, which is how hard gets plans to choose between.
func (b *Bot) plan(c *engine.Content, v *engine.State, player int, jitter float64) []engine.Order {
	pl := &planner{c: c, v: v, player: player, team: v.Player(player).Team, bot: b, jitter: jitter}
	pl.prepare()
	pl.claimAllies()
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
	b.Dropped += len(out) - len(ok)
	return ok
}

type planner struct {
	c      *engine.Content
	v      *engine.State
	player int
	team   int
	bot    *Bot
	jitter float64

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

// claimAllies takes the squares that teammates in the seats before ours
// will walk (or jump, or spawn) into this turn, as a normal bot in their
// seat would plan it. Teammates share our view, so for a bot teammate this
// is exactly what it orders, and allies no longer pick the same square and
// both stop short. Seats after ours do the same for us; a human teammate
// is guessed at, which only costs a square they may not have wanted.
func (pl *planner) claimAllies() {
	for _, q := range pl.v.Teams[pl.team].Players {
		if q >= pl.player {
			break
		}
		os := (&Bot{Level: Normal}).plan(pl.c, pl.v, q, 0)
		for _, o := range os {
			u := pl.v.Unit(o.UnitID)
			if u == nil {
				continue
			}
			if d := engine.AttackFrom(pl.c, u, os); d != u.Pos {
				pl.claimed[d] = true
			}
			if ab := pl.c.Abilities[o.Ability]; o.Action == engine.ActAbility && ab.Instant() && profile(ab).spawn > 0 {
				pl.claimed[o.Target] = true
			}
		}
	}
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
		ab := pl.c.Abilities[o.Ability]
		if pr := profile(ab); ab.Instant() && pr.spawn > 0 {
			// The spawn lands before anyone moves: never on the tile this
			// commander is about to walk onto.
			pl.claimed[o.Target] = true
		} else if pr.teleportSelf {
			// The jump is the move: a walk planned from here would be
			// rejected once it has landed. It attacks from where it lands.
			pl.claimed[o.Target] = true
			if t, _ := pl.bestTarget(u, o.Target); t != nil {
				out = append(out, engine.Order{UnitID: u.ID, Action: engine.ActAttack, TargetU: t.ID, Target: t.Pos})
			}
			return out
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
		if pl.jitter > 0 {
			s += pl.jitter * float64(pl.bot.rng.Intn(201)-100) / 100
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
	spawn                int // units it puts on the board
	reveal, smoke        bool
	overwatch, strike    bool
	teleportSelf         bool
	teleportAlly         bool
	buff                 bool
	control              bool
	pin                  bool // root or slow: stops a unit getting somewhere
	pull, silence        bool
	cloak, taunt         bool
	radius               int // widest effect radius (a splash, a smoke)
}

func profile(ab engine.AbilityDef) abilityProfile {
	var p abilityProfile
	for _, e := range ab.Effects {
		if e.Radius != nil && *e.Radius > p.radius {
			p.radius = *e.Radius
		}
		switch e.Kind {
		case "damage":
			p.damage += e.N
			p.hostile = true
		case "push", "pull", "slow", "root", "silence", "mark", "taunt":
			p.hostile, p.control = true, true
			switch e.Kind {
			case "root", "slow":
				p.pin = true
			case "pull":
				p.pull = true
			case "silence":
				p.silence = true
			case "taunt":
				p.taunt = true
			}
		case "cloak":
			p.cloak = true
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
			p.spawn += e.Count
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
				p.teleportAlly, p.friendly = true, true
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
			// Each enemy in sight is one more that may walk into the net.
			return o, 2.5 + float64(len(pl.enemies))*0.5
		case pr.cloak:
			// Unseen by anyone not next to it: worth it under fire from range.
			if d := pl.danger(u, u.Pos); d > 1 && pl.countEnemies(u.Pos, 1) == 0 {
				return o, 1.5 + d
			}
			return o, 0
		case pr.taunt:
			// Their attacks turn on the caster: for a healthy one with
			// someone beside it to spare.
			if u.HP*2 > u.MaxHP && pl.countAllies(u.Pos, ab.Area+1) > 1 {
				return o, float64(pl.countEnemies(u.Pos, ab.Area)) * 1.4
			}
			return o, 0
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
					// A pull drags it off its point and into our reach.
					if pr.pull && engine.Dist(u.Pos, t.Pos) > 1 {
						v += float64(pl.countAllies(u.Pos, 2)) * 0.4
						if pl.v.Board.At(t.Pos).Terrain == engine.TerrainObjective {
							v += 1.5
						}
					}
					// A silence is worth the abilities it stops.
					if pr.silence {
						v += float64(len(engine.UnitAbilities(pl.c, t))) - 1.5
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
					// Or bring up one left behind: to the caster is nearer its goal.
					if g, ok := pl.goals[t.ID]; ok && t.ID != u.ID {
						if gain := engine.Dist(t.Pos, g) - engine.Dist(u.Pos, g); gain >= 4 && v == 0 {
							v = 2 + float64(gain)*0.1
						}
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
		cur, _ := pl.bestTarget(u, u.Pos)
		idle := cur == nil // nothing to attack from here
		here := pl.danger(u, u.Pos)
		var reach map[engine.Pos][]engine.Pos
		if pr.teleportSelf {
			reach = engine.Reachable(pl.c, pl.v, u)
		}
		for _, p := range pl.v.Board.AreaFor(u.Pos, ab.Range, pl.team) {
			if ab.NeedsVisible() && !vis[p] {
				continue
			}
			v := 0.0
			switch {
			case pr.spawn > 0:
				g, hasGoal := pl.goals[u.ID]
				// Toward the goal, but not on it: the caster is walking there.
				if pl.v.Board.Passable(p) && !pl.occupied[p] && !pl.claimed[p] && !(hasGoal && p == g) {
					v = 2 + float64(pr.spawn)
					if hasGoal {
						v -= float64(engine.Dist(p, g)) * 0.05
					}
				}
			case pr.teleportSelf:
				if !pl.v.Board.Passable(p) || pl.occupied[p] || pl.claimed[p] {
					break
				}
				if dodge := here - pl.danger(u, p); u.HP*2 <= u.MaxHP && dodge > 0 {
					// Attacks land after the jump: a hurt commander takes
					// it for the hits it leaves behind.
					v = 1 + dodge
				} else if pl.v.Board.At(u.Pos).Terrain == engine.TerrainObjective {
					// Already holding: a better firing spot is not worth the point.
				} else if _, walk := reach[p]; !walk && pl.v.Board.At(p).Terrain == engine.TerrainObjective {
					// Onto a point a walk would not reach this turn.
					v = 3 - pl.danger(u, p)*0.3
				} else if g, ok := pl.goals[u.ID]; ok && engine.Dist(p, g) > engine.Dist(u.Pos, g) {
					// Jumping away from the objective to get a shot loses it.
				} else if t, val := pl.bestTarget(u, p); t != nil && val > 1 && idle {
					v = val - pl.danger(u, p)*0.5
				}
				if pr.damage > 0 {
					// A jump that lands a blow (Arc Jump): the blow, less
					// what the landing costs it.
					v += float64(pl.countEnemies(p, pr.radius)*pr.damage) * 1.1
					if risk := pl.danger(u, p) - here; risk > 0 {
						v -= risk * 0.8
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
				// Worth the hits it stops, less the ones of ours it stops:
				// smoke blocks a line it lies on, not one that ends in it.
				cover := map[engine.Pos]bool{}
				for _, q := range pl.v.Board.Area(p, pr.radius) {
					cover[q] = true
				}
				cut := func(a, b engine.Pos) bool {
					for _, q := range engine.Trace(a, b) {
						if q != a && q != b && cover[q] {
							return true
						}
					}
					return false
				}
				for _, a := range pl.allies {
					for _, e := range pl.enemies {
						if !cut(e.Pos, a.Pos) || !cut(a.Pos, e.Pos) {
							continue
						}
						if engine.CanAttack(pl.c, pl.v, e, e.Pos, a) {
							dice, tn := engine.AttackPreview(pl.c, pl.v, e, a, a.Pos)
							v += float64(dice) * engine.HitChance(tn)
						}
						if engine.CanAttack(pl.c, pl.v, a, a.Pos, e) {
							dice, tn := engine.AttackPreview(pl.c, pl.v, a, e, e.Pos)
							v -= float64(dice) * engine.HitChance(tn)
						}
					}
				}
				if ab.Delay > 0 {
					v *= 0.7 // the lines will have moved
				}
			case pr.reveal:
				if vis[p] {
					break
				}
				// Light up where an enemy was last seen, or, with none in
				// sight, an objective we cannot see.
				for _, sg := range pl.v.Sightings {
					if sg.Pos == p {
						v = 2.4
					}
				}
				if v == 0 && (len(pl.enemies) == 0 || idle) && pl.v.Board.At(p).Terrain == engine.TerrainObjective {
					v = 2
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
