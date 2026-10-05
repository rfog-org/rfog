package engine_test

import (
	"testing"

	"rfog/engine"
)

// arena is a match with every deployed unit removed, for hand-built scenes.
type arena struct {
	t *testing.T
	c *engine.Content
	s engine.State
}

func newArena(t *testing.T, heroA, heroB string, deterministic bool) *arena {
	t.Helper()
	c := content(t)
	st := setup1v1(11, heroA, heroB)
	st.Deterministic = deterministic
	s := newMatch(t, c, st)
	for i := range s.Units {
		engine.RemoveUnit(&s, s.Units[i].ID)
	}
	return &arena{t: t, c: c, s: s}
}

// add revives or adds a unit of kind for owner at p.
func (a *arena) add(owner int, kind string, x, y int) *engine.Unit {
	a.t.Helper()
	p := engine.Pos{X: x, Y: y}
	// Reuse the existing commander for hero kinds so Player.Commander stays valid.
	for i := range a.s.Units {
		u := &a.s.Units[i]
		if u.Owner == owner && u.Kind == kind && u.Dead {
			u.Dead = false
			u.HP = u.MaxHP
			u.Pos = p
			u.RespawnIn = 0
			u.Level = 4 // arena heroes have every ability unlocked; tests lower it as needed
			return u
		}
	}
	u, err := engine.AddUnit(a.c, &a.s, owner, kind, p)
	if err != nil {
		a.t.Fatal(err)
	}
	return u
}

func (a *arena) step(o0, o1 []engine.Order) []engine.Event {
	a.t.Helper()
	var ev []engine.Event
	a.s, ev = engine.Step(a.c, a.s, map[int][]engine.Order{0: o0, 1: o1}, a.s.Match.Seed)
	checkInvariants(a.t, a.c, &a.s, ev)
	return ev
}

func (a *arena) u(id int) *engine.Unit { return a.s.Unit(id) }

func (a *arena) setTile(x, y int, terrain string, z int) {
	a.s.Board.Tiles[y*a.s.Board.W+x] = engine.Tile{Terrain: terrain, Z: z}
}

func find(evs []engine.Event, kind string) []engine.Event {
	var out []engine.Event
	for _, e := range evs {
		if e.Kind == kind {
			out = append(out, e)
		}
	}
	return out
}

func mv(id int, path ...engine.Pos) engine.Order {
	return engine.Order{UnitID: id, Action: engine.ActMove, Path: path}
}
func atk(id, target int) engine.Order {
	return engine.Order{UnitID: id, Action: engine.ActAttack, TargetU: target}
}
func castU(id int, ab string, target int) engine.Order {
	return engine.Order{UnitID: id, Action: engine.ActAbility, Ability: ab, TargetU: target}
}
func castT(id int, ab string, x, y int) engine.Order {
	return engine.Order{UnitID: id, Action: engine.ActAbility, Ability: ab, Target: engine.Pos{X: x, Y: y}}
}
func castS(id int, ab string) engine.Order {
	return engine.Order{UnitID: id, Action: engine.ActAbility, Ability: ab}
}
func P(x, y int) engine.Pos { return engine.Pos{X: x, Y: y} }

func TestBasicAttackDeterministic(t *testing.T) {
	a := newArena(t, "hask", "wren", true)
	h := a.add(0, "hask", 5, 3)
	l := a.add(1, "lineman", 6, 3)
	ev := a.step([]engine.Order{atk(h.ID, l.ID)}, nil)
	att := find(ev, engine.EvAttacked)
	if len(att) != 1 || att[0].Hits != 2 || att[0].Amount != 4 {
		t.Fatalf("attack event %+v", att)
	}
	if a.u(l.ID).HP != 4 {
		t.Fatalf("lineman HP %d, want 4", a.u(l.ID).HP)
	}
}

func TestAttackPreviewModifiers(t *testing.T) {
	a := newArena(t, "wren", "hask", true)
	w := a.add(0, "wren", 2, 4)
	l := a.add(1, "lineman", 3, 4) // cover tile at (3,4)
	if a.s.Board.At(P(3, 4)).Terrain != engine.TerrainCover {
		t.Fatal("expected cover at (3,4)")
	}
	dice, tn := engine.AttackPreview(a.c, &a.s, w, l, l.Pos)
	if dice != 3 || tn != 4 {
		t.Fatalf("adjacent: dice %d tn %d, want 3/4 (no cover when adjacent)", dice, tn)
	}
	w.Pos = P(0, 4)
	dice, tn = engine.AttackPreview(a.c, &a.s, w, l, l.Pos)
	if tn != 5 {
		t.Fatalf("cover: tn %d, want 5", tn)
	}
	l.Held = true
	_, tn = engine.AttackPreview(a.c, &a.s, w, l, l.Pos)
	if tn != 6 {
		t.Fatalf("cover+hold(lineman +2) capped: tn %d, want 6", tn)
	}
	// Ranged unit adjacent loses a die.
	r := a.add(0, "ranged", 4, 4)
	l.Held = false
	dice, _ = engine.AttackPreview(a.c, &a.s, r, l, l.Pos)
	if dice != 1 {
		t.Fatalf("ranged adjacent dice %d, want 1", dice)
	}
	// Mark adds dice.
	l.Status = append(l.Status, engine.Status{Kind: "mark", Magnitude: 2, Turns: 2})
	dice, _ = engine.AttackPreview(a.c, &a.s, w, l, l.Pos)
	if dice != 5 {
		t.Fatalf("marked dice %d, want 5", dice)
	}
}

func TestDiceRollsAreLogged(t *testing.T) {
	a := newArena(t, "hask", "wren", false)
	h := a.add(0, "hask", 5, 3)
	l := a.add(1, "lineman", 6, 3)
	ev := a.step([]engine.Order{atk(h.ID, l.ID)}, nil)
	att := find(ev, engine.EvAttacked)[0]
	if len(att.Dice) != 4 {
		t.Fatalf("want 4 dice, got %v", att.Dice)
	}
	hits := 0
	for _, d := range att.Dice {
		if d < 1 || d > 6 {
			t.Fatalf("bad die %d", d)
		}
		if d >= 4 {
			hits++
		}
	}
	if hits != att.Hits {
		t.Fatalf("hits %d vs dice %v", att.Hits, att.Dice)
	}
}

func TestSimultaneousAttacksSameINI(t *testing.T) {
	a := newArena(t, "hask", "wren", true)
	// Two linemen (INI 2) with 1 HP each attack each other: both die.
	x := a.add(0, "lineman", 5, 5)
	y := a.add(1, "lineman", 6, 5)
	x.HP, y.HP = 1, 1
	a.step([]engine.Order{atk(x.ID, y.ID)}, []engine.Order{atk(y.ID, x.ID)})
	if a.u(x.ID).Alive() || a.u(y.ID).Alive() {
		t.Fatal("same-INI attacks must be simultaneous")
	}
}

func TestHigherINIKillsFirst(t *testing.T) {
	a := newArena(t, "hask", "wren", true)
	r := a.add(0, "runner", 5, 5) // INI 5, ATK 3 vs DEF 4 -> 2 hits (1.5 rounds up)
	l := a.add(1, "lineman", 6, 5)
	l.HP = 2
	r.HP = 1
	a.step([]engine.Order{atk(r.ID, l.ID)}, []engine.Order{atk(l.ID, r.ID)})
	if a.u(l.ID).Alive() {
		t.Fatal("lineman should die")
	}
	if !a.u(r.ID).Alive() {
		t.Fatal("dead units do not fire")
	}
}

func TestCrackPushIntoWall(t *testing.T) {
	a := newArena(t, "hask", "wren", true)
	// wall at (10,0); place target at (9,0) and hask at (8,0): push east into the wall.
	h := a.add(0, "hask", 8, 0)
	l := a.add(1, "lineman", 9, 0)
	ev := a.step([]engine.Order{castU(h.ID, "crack", l.ID)}, nil)
	if a.u(l.ID).Pos != P(9, 0) {
		t.Fatalf("target moved to %v", a.u(l.ID).Pos)
	}
	// 2 ability damage + 1 impact.
	if a.u(l.ID).HP != 3 {
		t.Fatalf("HP %d, want 3", a.u(l.ID).HP)
	}
	if len(find(ev, engine.EvPushed)) != 0 {
		t.Fatal("no push event expected when fully blocked")
	}
}

func TestPushMovesAndFalls(t *testing.T) {
	a := newArena(t, "hask", "wren", true)
	// Central block: (9,5) z2, (8,5) z1, (7,5) z0. Push from (10,5) westward.
	h := a.add(0, "hask", 10, 5)
	l := a.add(1, "lineman", 9, 5)
	l.HP = 6
	ev := a.step([]engine.Order{castU(h.ID, "crack", l.ID)}, nil)
	if got := a.u(l.ID).Pos; got != P(7, 5) {
		t.Fatalf("pushed to %v, want (7,5)", got)
	}
	// 2 damage + fall from z2 to z0 (drop 2 -> 1 damage) = 3.
	if a.u(l.ID).HP != 3 {
		t.Fatalf("HP %d, want 3 (2 crack + 1 fall)", a.u(l.ID).HP)
	}
	if len(find(ev, engine.EvPushed)) != 1 {
		t.Fatal("expected one Pushed event")
	}
	// A push cannot climb more than push_max_climb.
	a2 := newArena(t, "hask", "wren", true)
	h2 := a2.add(0, "hask", 6, 5)
	l2 := a2.add(1, "lineman", 7, 5) // z0; (8,5) is z1 ok, (9,5) is z2: +1 ok
	a2.step([]engine.Order{castU(h2.ID, "crack", l2.ID)}, nil)
	if got := a2.u(l2.ID).Pos; got != P(9, 5) {
		t.Fatalf("pushed uphill to %v, want (9,5)", got)
	}
}

func TestDelayedAbilityTelegraphs(t *testing.T) {
	a := newArena(t, "wren", "hask", true)
	w := a.add(0, "wren", 4, 6)
	l := a.add(1, "lineman", 9, 6)
	ev := a.step([]engine.Order{castT(w.ID, "long_shot", 9, 6)}, nil)
	if len(find(ev, engine.EvTelegraph)) != 1 || len(a.s.Delayed) != 1 {
		t.Fatal("expected a telegraph and one delayed ability")
	}
	if a.u(l.ID).HP != 6 {
		t.Fatal("delayed ability must not resolve on cast turn")
	}
	if a.u(w.ID).Cooldowns["long_shot"] != 1 {
		t.Fatalf("cooldown %d, want 1 (3, minus L3 reduction, minus one tick)", a.u(w.ID).Cooldowns["long_shot"])
	}
	ev = a.step(nil, nil)
	if a.u(l.ID).HP != 2 {
		t.Fatalf("HP %d after long shot, want 2", a.u(l.ID).HP)
	}
	if len(a.s.Delayed) != 0 {
		t.Fatal("delayed ability should be consumed")
	}
	if len(find(ev, engine.EvAbilityResolved)) != 1 {
		t.Fatal("expected AbilityResolved")
	}
	// Lands on the recorded tile even if the caster died meanwhile: move away first.
	a2 := newArena(t, "wren", "hask", true)
	w2 := a2.add(0, "wren", 4, 6)
	l2 := a2.add(1, "lineman", 9, 6)
	a2.step([]engine.Order{castT(w2.ID, "long_shot", 9, 6)}, []engine.Order{mv(l2.ID, P(10, 6))})
	a2.step(nil, nil)
	if a2.u(l2.ID).HP != 6 {
		t.Fatal("unit that left the tile should not be hit")
	}
}

func TestMovementContest(t *testing.T) {
	a := newArena(t, "hask", "wren", true)
	r := a.add(0, "runner", 5, 5)  // INI 5
	l := a.add(1, "lineman", 7, 5) // INI 2
	ev := a.step([]engine.Order{mv(r.ID, P(6, 5))}, []engine.Order{mv(l.ID, P(6, 5))})
	if a.u(r.ID).Pos != P(6, 5) {
		t.Fatalf("runner should win the tile, at %v", a.u(r.ID).Pos)
	}
	if a.u(l.ID).Pos != P(7, 5) {
		t.Fatalf("lineman should stop short, at %v", a.u(l.ID).Pos)
	}
	if len(find(ev, engine.EvMoved)) != 1 {
		t.Fatal("only one Moved event expected")
	}
	// Same INI: both stop short.
	b := newArena(t, "hask", "wren", true)
	x := b.add(0, "lineman", 5, 5)
	y := b.add(1, "lineman", 7, 5)
	b.step([]engine.Order{mv(x.ID, P(6, 5))}, []engine.Order{mv(y.ID, P(6, 5))})
	if b.u(x.ID).Pos != P(5, 5) || b.u(y.ID).Pos != P(7, 5) {
		t.Fatal("same-INI contest should stop both")
	}
	// Loser stops on the last free tile of its path.
	c := newArena(t, "hask", "wren", true)
	x = c.add(0, "runner", 4, 5)
	y = c.add(1, "lineman", 8, 5)
	c.step([]engine.Order{mv(x.ID, P(5, 5), P(6, 5))}, []engine.Order{mv(y.ID, P(7, 5), P(6, 5))})
	if c.u(x.ID).Pos != P(6, 5) || c.u(y.ID).Pos != P(7, 5) {
		t.Fatalf("got %v and %v", c.u(x.ID).Pos, c.u(y.ID).Pos)
	}
}

func TestOverwatchShoots(t *testing.T) {
	a := newArena(t, "hask", "wren", true)
	r := a.add(0, "ranged", 5, 5) // RNG 5
	l := a.add(1, "lineman", 12, 5)
	ev := a.step([]engine.Order{{UnitID: r.ID, Action: engine.ActOverwatch}}, []engine.Order{mv(l.ID, P(11, 5), P(10, 5))})
	att := find(ev, engine.EvAttacked)
	if len(att) != 1 || att[0].Reason != "overwatch" {
		t.Fatalf("expected one overwatch shot, got %+v", att)
	}
	if att[0].To != P(10, 5) {
		t.Fatalf("shot at %v, want first tile in range (10,5)", att[0].To)
	}
	if a.u(r.ID).Overwatch {
		t.Fatal("overwatch should clear at end of turn")
	}
	// Overwatch forfeits attack: unit with overwatch + attack orders rejected.
	errs := engine.Validate(a.c, &a.s, 0, []engine.Order{{UnitID: r.ID, Action: engine.ActOverwatch}, atk(r.ID, l.ID)})
	if len(errs) == 0 {
		t.Fatal("expected combo rejection")
	}
}

func TestOverwatchNetMultipleShots(t *testing.T) {
	a := newArena(t, "wren", "hask", true)
	w := a.add(0, "wren", 17, 1)
	l1 := a.add(1, "lineman", 17, 10)
	l2 := a.add(1, "lineman", 18, 10)
	ev := a.step([]engine.Order{castS(w.ID, "overwatch_net")}, []engine.Order{mv(l1.ID, P(17, 9)), mv(l2.ID, P(18, 9))})
	if n := len(find(ev, engine.EvAttacked)); n != 2 {
		t.Fatalf("want 2 shots at range 9, got %d", n)
	}
}

func TestSpawnAndExpire(t *testing.T) {
	a := newArena(t, "tally", "hask", true)
	ty := a.add(0, "tally", 5, 5)
	ev := a.step([]engine.Order{castT(ty.ID, "spin_up", 6, 5)}, nil)
	sp := find(ev, engine.EvSpawned)
	if len(sp) != 1 {
		t.Fatal("expected one spawn")
	}
	jb := a.u(sp[0].Unit)
	if jb.Kind != "junkbot" || jb.Pos != P(6, 5) || jb.Owner != 0 || jb.Team != 0 {
		t.Fatalf("bad junkbot %+v", jb)
	}
	life := a.c.Units["junkbot"].Expires
	if jb.ExpiresIn != life-1 {
		t.Fatalf("expires in %d after first tick, want %d", jb.ExpiresIn, life-1)
	}
	for i := 0; i < life-1; i++ {
		ev = a.step(nil, nil)
	}
	if a.u(jb.ID).Alive() || len(find(ev, engine.EvExpired)) != 1 {
		t.Fatal("junkbot should expire")
	}
	// Swarm spawns 3 around the tile.
	b := newArena(t, "tally", "hask", true)
	ty = b.add(0, "tally", 5, 5)
	b.step([]engine.Order{castT(ty.ID, "swarm", 7, 5)}, nil)
	ev = b.step(nil, nil)
	if n := len(find(ev, engine.EvSpawned)); n != 3 {
		t.Fatalf("swarm spawned %d", n)
	}
}

func TestScuttleAndOverclock(t *testing.T) {
	a := newArena(t, "tally", "hask", true)
	ty := a.add(0, "tally", 5, 5)
	jb := a.add(0, "junkbot", 7, 5)
	l := a.add(1, "lineman", 8, 5)
	far := a.add(1, "lineman", 12, 5)
	a.step([]engine.Order{castS(ty.ID, "overclock")}, nil)
	if a.u(jb.ID).Eff("atk") != 3 || a.u(jb.ID).Eff("mv") != 6 {
		t.Fatalf("overclock: atk %d mv %d", a.u(jb.ID).Eff("atk"), a.u(jb.ID).Eff("mv"))
	}
	if a.u(ty.ID).Eff("atk") != 2 {
		t.Fatal("overclock must only affect junkbots")
	}
	ev := a.step([]engine.Order{castU(ty.ID, "scuttle", jb.ID)}, nil)
	if a.u(jb.ID).Alive() {
		t.Fatal("junkbot should be sacrificed")
	}
	if a.u(l.ID).HP != 4 || a.u(far.ID).HP != 6 {
		t.Fatalf("scuttle damage: near %d far %d", a.u(l.ID).HP, a.u(far.ID).HP)
	}
	if len(find(ev, engine.EvDied)) != 1 {
		t.Fatal("expected Died event for sacrifice")
	}
	if a.u(ty.ID).XP != 0 {
		t.Fatal("sacrifice must not award XP")
	}
}

func TestCommanderDeathRespawnAndXP(t *testing.T) {
	a := newArena(t, "hask", "wren", true)
	h := a.add(0, "hask", 5, 5)
	w := a.add(1, "wren", 6, 5)
	h.Level, w.Level = 1, 1
	w.HP = 1
	ev := a.step([]engine.Order{atk(h.ID, w.ID)}, nil)
	if a.u(w.ID).Alive() {
		t.Fatal("wren should die")
	}
	if a.s.Teams[0].Score != 2 {
		t.Fatalf("team 0 score %d, want 2 for commander kill", a.s.Teams[0].Score)
	}
	if a.u(h.ID).XP != 2 || a.u(h.ID).Level != 2 {
		t.Fatalf("hask XP %d level %d, want 2/2", a.u(h.ID).XP, a.u(h.ID).Level)
	}
	if len(find(ev, engine.EvLevelUp)) != 1 {
		t.Fatal("expected LevelUp")
	}
	if a.u(w.ID).RespawnIn != 3 {
		t.Fatalf("respawn in %d, want 3", a.u(w.ID).RespawnIn)
	}
	var re []engine.Event
	for i := 0; i < 3; i++ {
		ev = a.step(nil, nil)
		re = append(re, find(ev, engine.EvRespawned)...)
	}
	if len(re) != 1 || !a.u(w.ID).Alive() || a.u(w.ID).HP != a.u(w.ID).MaxHP {
		t.Fatalf("wren should respawn at full HP after 3 turns; alive=%v", a.u(w.ID).Alive())
	}
	if a.u(w.ID).Pos.X < a.s.Board.W-a.s.Board.DeployCols {
		t.Fatalf("respawned outside deploy zone at %v", a.u(w.ID).Pos)
	}
	if a.u(w.ID).Level != 1 {
		t.Fatal("level kept")
	}
}

func TestObjectivesAndWin(t *testing.T) {
	a := newArena(t, "hask", "wren", true)
	l := a.add(0, "lineman", 7, 2) // west objective
	ev := a.step(nil, nil)
	if a.s.Teams[0].Score != 1 || len(find(ev, engine.EvObjectiveScored)) != 1 {
		t.Fatalf("score %d", a.s.Teams[0].Score)
	}
	if a.s.Objectives[0].Holder != 0 {
		t.Fatal("holder not recorded")
	}
	// Contested objective scores nothing.
	e := a.add(1, "lineman", 9, 6) // core tile
	f := a.add(0, "runner", 10, 5) // core tile
	a.step(nil, nil)
	if a.s.Teams[0].Score != 2 || a.s.Teams[1].Score != 0 {
		t.Fatalf("contested core should not score: %d-%d", a.s.Teams[0].Score, a.s.Teams[1].Score)
	}
	_ = e
	_ = f
	_ = l
	// Commander adjacent to a scoring unit gains XP.
	h := a.add(0, "hask", 8, 2) // adjacent to the lineman on the objective
	a.step(nil, nil)
	if a.u(h.ID).XP != 1 {
		t.Fatalf("commander XP %d, want 1", a.u(h.ID).XP)
	}
	// Reaching win_score ends the match.
	a.s.Teams[0].Score = a.s.Match.WinScore - 1
	ev = a.step(nil, nil)
	if !a.s.Ended() || a.s.Match.Winner != 0 || a.s.Match.Result != "score" {
		t.Fatalf("expected team 0 win by score: %+v", a.s.Match)
	}
	if len(find(ev, engine.EvMatchEnd)) != 1 {
		t.Fatal("expected MatchEnd")
	}
	// Steps after the end are no-ops.
	ev = a.step(nil, nil)
	if len(ev) != 0 {
		t.Fatal("step after end must do nothing")
	}
}

func TestTurnLimitTiebreaks(t *testing.T) {
	a := newArena(t, "hask", "wren", true)
	a.s.Match.Turn = a.s.Match.MaxTurns
	a.step(nil, nil)
	if !a.s.Ended() || a.s.Match.Winner != -1 || a.s.Match.Result != "draw" {
		t.Fatalf("expected draw: %+v", a.s.Match)
	}
	b := newArena(t, "hask", "wren", true)
	b.s.Match.Turn = b.s.Match.MaxTurns
	b.s.Teams[1].Kills = 1
	b.step(nil, nil)
	if b.s.Match.Winner != 1 || b.s.Match.Result != "kills" {
		t.Fatalf("expected kills tiebreak: %+v", b.s.Match)
	}
}

func TestValidateRejects(t *testing.T) {
	a := newArena(t, "hask", "wren", true)
	h := a.add(0, "hask", 5, 5)
	l := a.add(0, "lineman", 5, 6)
	w := a.add(1, "wren", 15, 5)
	h.Level = 1
	bad := []engine.Order{
		atk(w.ID, h.ID),   // not your unit
		mv(l.ID, P(5, 6)), // not adjacent (same tile)
		mv(l.ID, P(6, 6), P(7, 6), P(8, 6), P(9, 6), P(10, 6)), // too far
		atk(h.ID, w.ID),             // out of range / not visible
		castU(h.ID, "breach", w.ID), // not unlocked (L4)
		castT(h.ID, "no_such", 1, 1),
		mv(h.ID, P(6, 5)), mv(h.ID, P(4, 5)), // two moves for commander
	}
	errs := engine.Validate(a.c, &a.s, 0, bad)
	if len(errs) < 7 {
		t.Fatalf("expected >=7 errors, got %d: %v", len(errs), errs)
	}
	good := []engine.Order{mv(h.ID, P(6, 5)), castS(h.ID, "brace"), mv(l.ID, P(6, 6))}
	if errs := engine.Validate(a.c, &a.s, 0, good); len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	// Rooted units cannot move; silenced cannot cast; cooldowns block.
	a.u(l.ID).Status = []engine.Status{{Kind: "root", Magnitude: 1, Turns: 1}}
	a.u(h.ID).Status = []engine.Status{{Kind: "silence", Magnitude: 1, Turns: 1}}
	errs = engine.Validate(a.c, &a.s, 0, []engine.Order{mv(l.ID, P(6, 6)), castS(h.ID, "brace")})
	if len(errs) != 2 {
		t.Fatalf("root/silence: %v", errs)
	}
	a.u(h.ID).Status = nil
	a.u(h.ID).Cooldowns["brace"] = 2
	if errs := engine.Validate(a.c, &a.s, 0, []engine.Order{castS(h.ID, "brace")}); len(errs) != 1 {
		t.Fatalf("cooldown: %v", errs)
	}
	// Rejected orders surface as events in Step.
	ev := a.step([]engine.Order{atk(w.ID, h.ID)}, nil)
	if len(find(ev, engine.EvOrderRejected)) != 1 {
		t.Fatal("expected OrderRejected")
	}
}

func TestLungeApproachesAndStrikes(t *testing.T) {
	a := newArena(t, "hask", "wren", true)
	h := a.add(0, "hask", 3, 5)
	l := a.add(1, "lineman", 7, 5)
	ev := a.step([]engine.Order{castU(h.ID, "lunge", l.ID)}, nil)
	// It closes to its own attack range (whatever the data says that is),
	// no further than the approach allows, and strikes.
	hp := a.u(h.ID)
	if !engine.CanAttack(a.c, &a.s, hp, hp.Pos, a.u(l.ID)) || engine.Dist(hp.Pos, P(3, 5)) > 3 || hp.Pos == P(3, 5) {
		t.Fatalf("hask at %v, want within its range of the target and at most 3 away", hp.Pos)
	}
	if len(find(ev, engine.EvAttacked)) != 1 || a.u(l.ID).HP >= a.u(l.ID).MaxHP {
		t.Fatalf("lunge strike: HP %d", a.u(l.ID).HP)
	}
}

func TestStatusEffects(t *testing.T) {
	a := newArena(t, "nul", "mott", true)
	n := a.add(0, "nul", 5, 5)
	l := a.add(1, "lineman", 8, 5)
	// Cut Line roots for 1 turn: the move in the same turn fails, next turn works.
	ev := a.step([]engine.Order{castU(n.ID, "cut_line", l.ID)}, []engine.Order{mv(l.ID, P(9, 5))})
	if a.u(l.ID).Pos != P(8, 5) {
		t.Fatal("rooted unit moved")
	}
	if len(find(ev, engine.EvOrderRejected)) != 1 {
		t.Fatal("expected rejection for rooted move")
	}
	if a.u(l.ID).Rooted() {
		t.Fatal("root(1) should expire at end of turn")
	}
	// Static field slows and reveals.
	a.step([]engine.Order{castT(n.ID, "static_field", 8, 5)}, nil)
	a.step(nil, nil)
	if a.u(l.ID).Eff("mv") != 2 {
		t.Fatalf("slowed mv %d, want 2", a.u(l.ID).Eff("mv"))
	}
	hasReveal := false
	for _, e := range a.s.Effects {
		if e.Kind == "reveal" && e.Team == 0 {
			hasReveal = true
		}
	}
	if !hasReveal {
		t.Fatal("expected reveal effect")
	}
	// Blackout: INI -2 and silence on everyone in area, including caster's own.
	b := newArena(t, "nul", "mott", true)
	n = b.add(0, "nul", 5, 5)
	m := b.add(1, "mott", 9, 5)
	b.step([]engine.Order{castT(n.ID, "blackout", 9, 5)}, nil)
	b.step(nil, nil)
	b.step(nil, nil)
	if b.u(m.ID).Eff("ini") != 1 || !b.u(m.ID).Silenced() {
		t.Fatalf("blackout: ini %d silenced %v", b.u(m.ID).Eff("ini"), b.u(m.ID).Silenced())
	}
}

func TestSmokeBlocksLOSAndPingReveals(t *testing.T) {
	a := newArena(t, "mott", "nul", true)
	m := a.add(0, "mott", 8, 5)
	r := a.add(0, "ranged", 4, 5)
	l := a.add(1, "lineman", 9, 5)
	if !a.s.LOS(r.Pos, l.Pos) {
		t.Fatal("expected LOS on open ground")
	}
	a.s.Effects = append(a.s.Effects, engine.TileEffect{Kind: "smoke", Tiles: []engine.Pos{P(7, 5)}, Turns: 2, Team: -1})
	if a.s.LOS(r.Pos, l.Pos) {
		t.Fatal("smoke should block LOS")
	}
	a.s.Effects = nil
	// Wren far away is not visible; Ping reveals it.
	w := a.add(1, "nul", 16, 5)
	v := engine.View(a.c, a.s, 0)
	if v.Unit(w.ID) != nil {
		t.Fatal("enemy should be hidden")
	}
	a.step([]engine.Order{castT(m.ID, "ping", 15, 5)}, nil)
	v = engine.View(a.c, a.s, 0)
	if v.Unit(w.ID) == nil {
		t.Fatal("ping should reveal the enemy commander")
	}
	// Sightings: after the reveal expires the ghost remains.
	a.step(nil, nil)
	a.step(nil, nil)
	v = engine.View(a.c, a.s, 0)
	if v.Unit(w.ID) != nil {
		t.Fatal("enemy should be hidden again")
	}
	found := false
	for _, sg := range v.Sightings {
		if sg.UnitID == w.ID && sg.Team == 0 {
			found = true
		}
	}
	if !found {
		t.Fatal("expected a last-seen sighting")
	}
	for _, sg := range engine.View(a.c, a.s, 1).Sightings {
		if sg.Team != 1 {
			t.Fatal("view leaks other team's sightings")
		}
	}
}

func TestHealShieldTeleport(t *testing.T) {
	a := newArena(t, "mott", "hask", true)
	m := a.add(0, "mott", 5, 5)
	l := a.add(0, "lineman", 7, 5)
	l.HP = 2
	a.step([]engine.Order{castU(m.ID, "patch", l.ID)}, nil)
	want := 2 + a.c.Abilities["patch"].Effects[0].N // whatever Patch heals in data
	if want > l.MaxHP {
		want = l.MaxHP
	}
	if a.u(l.ID).HP != want {
		t.Fatalf("patch HP %d, want %d", a.u(l.ID).HP, want)
	}
	// Relay brings ally adjacent.
	a.u(l.ID).Pos = P(10, 5)
	a.step([]engine.Order{castU(m.ID, "relay", l.ID)}, nil)
	if !engine.Adjacent(a.u(l.ID).Pos, a.u(m.ID).Pos) {
		t.Fatalf("relay left ally at %v", a.u(l.ID).Pos)
	}
	// Brace shields absorb damage before HP.
	b := newArena(t, "hask", "wren", true)
	h := b.add(0, "hask", 5, 5)
	e := b.add(1, "lineman", 6, 5)
	b.step([]engine.Order{castS(h.ID, "brace")}, []engine.Order{atk(e.ID, h.ID)})
	if b.u(h.ID).HP != b.u(h.ID).MaxHP || b.u(h.ID).Shield() != 1 {
		t.Fatalf("shield: HP %d shield %d", b.u(h.ID).HP, b.u(h.ID).Shield())
	}
	// Relocate to a visible tile; teleport into an occupied tile is rejected.
	c := newArena(t, "wren", "hask", true)
	w := c.add(0, "wren", 5, 5)
	c.step([]engine.Order{castT(w.ID, "relocate", 8, 5)}, nil)
	if c.u(w.ID).Pos != P(8, 5) {
		t.Fatal("relocate failed")
	}
	x := c.add(0, "lineman", 9, 5)
	c.u(w.ID).Cooldowns = map[string]int{}
	if errs := engine.Validate(c.c, &c.s, 0, []engine.Order{castT(w.ID, "relocate", x.Pos.X, x.Pos.Y)}); len(errs) != 1 {
		t.Fatalf("occupied teleport: %v", errs)
	}
}

func TestSurgeAndCooldownReduction(t *testing.T) {
	a := newArena(t, "mott", "hask", true)
	m := a.add(0, "mott", 1, 10)
	l := a.add(0, "lineman", 0, 11)
	// Surge resolves before movement, so a 6-tile path (MV 4+2) succeeds this turn.
	path := []engine.Pos{P(1, 11), P(2, 11), P(3, 11), P(4, 11), P(5, 11), P(6, 11)}
	if errs := engine.Validate(a.c, &a.s, 0, []engine.Order{mv(l.ID, path...)}); len(errs) != 1 {
		t.Fatalf("client-side validation should still reject 6 tiles at MV 4: %v", errs)
	}
	a.step([]engine.Order{castS(m.ID, "surge"), mv(l.ID, path...)}, nil)
	if a.u(l.ID).Pos != P(6, 11) {
		t.Fatalf("surged lineman at %v, want (6,11)", a.u(l.ID).Pos)
	}
	// cd 5, L3+ reduction -1 = 4, minus the end-of-turn tick = 3.
	if a.u(m.ID).Cooldowns["surge"] != 3 {
		t.Fatalf("cooldown %d, want 3", a.u(m.ID).Cooldowns["surge"])
	}
	if a.u(l.ID).Eff("ini") != 2 || a.u(l.ID).Eff("mv") != 4 {
		t.Fatal("surge (1 turn) should expire at end of the cast turn")
	}
}

func TestBreachAreaFromRecordedTile(t *testing.T) {
	a := newArena(t, "hask", "wren", true)
	h := a.add(0, "hask", 5, 5)
	h.Level = 4
	e1 := a.add(1, "lineman", 6, 5)
	e2 := a.add(1, "lineman", 6, 6)
	own := a.add(0, "lineman", 4, 5)
	a.step([]engine.Order{castS(h.ID, "breach")}, nil)
	a.step(nil, nil)
	if a.u(e1.ID).HP != 3 || a.u(e2.ID).HP != 3 {
		t.Fatalf("breach damage: %d %d", a.u(e1.ID).HP, a.u(e2.ID).HP)
	}
	if a.u(own.ID).HP != 6 {
		t.Fatal("breach must not hit allies")
	}
	if a.u(e1.ID).Pos != P(7, 5) || a.u(e2.ID).Pos != P(7, 7) {
		t.Fatalf("breach push: %v %v", a.u(e1.ID).Pos, a.u(e2.ID).Pos)
	}
}

func TestHeightRangeAndMoveCost(t *testing.T) {
	a := newArena(t, "wren", "hask", true)
	r := a.add(0, "ranged", 10, 5) // z2
	l := a.add(1, "lineman", 16, 5)
	if !engine.CanAttack(a.c, &a.s, r, r.Pos, l) {
		t.Fatal("height should grant +1 range (6)")
	}
	l.Pos = P(17, 5)
	if engine.CanAttack(a.c, &a.s, r, r.Pos, l) {
		t.Fatal("range 7 should be too far")
	}
	// Climbing costs: lineman (MV 4, JMP 1) from (7,5) z0 -> (8,5) z1 -> (9,5) z2 = 1+1 + 1+1 = 4.
	x := a.add(1, "lineman", 7, 5)
	cost, why := engine.PathCost(a.c, &a.s, x, []engine.Pos{P(8, 5), P(9, 5)})
	if why != "" || cost != 4 {
		t.Fatalf("cost %d (%s), want 4", cost, why)
	}
	// Runner ignores height cost.
	rn := a.add(1, "runner", 7, 6)
	cost, why = engine.PathCost(a.c, &a.s, rn, []engine.Pos{P(8, 6), P(9, 6)})
	if why != "" || cost != 2 {
		t.Fatalf("runner cost %d (%s), want 2", cost, why)
	}
	// JMP limit: lineman cannot step z0 -> z2 directly.
	a.setTile(7, 4, engine.TerrainOpen, 0)
	a.setTile(8, 4, engine.TerrainOpen, 3)
	y := a.add(1, "lineman", 7, 4)
	if _, why := engine.PathCost(a.c, &a.s, y, []engine.Pos{P(8, 4)}); why == "" {
		t.Fatal("jump too high should be illegal")
	}
	reach := engine.Reachable(a.c, &a.s, y)
	if _, ok := reach[P(8, 4)]; ok {
		t.Fatal("reachable includes an illegal climb")
	}
	if _, ok := reach[P(6, 4)]; !ok {
		t.Fatal("reachable missing an adjacent open tile")
	}
}

func TestTauntAndCloakPrimitives(t *testing.T) {
	a := newArena(t, "hask", "wren", true)
	// Inject a synthetic ability to exercise the primitives without data changes.
	a.c.Abilities["test_taunt"] = engine.AbilityDef{ID: "test_taunt", Target: engine.TargetUnit, Range: 3, Filter: engine.FilterEnemies,
		Effects: []engine.EffectDef{{Kind: "taunt", Turns: 2}, {Kind: "cloak", Turns: 2}, {Kind: "pull", Dist: 3}}}
	h := a.c.Heroes["hask"]
	h.Abilities = map[string]string{"q": "test_taunt", "w": "brace", "e": "crack", "r": "breach"}
	a.c.Heroes["hask"] = h
	hk := a.add(0, "hask", 5, 5)
	decoy := a.add(0, "lineman", 8, 6)
	e := a.add(1, "lineman", 8, 5)
	// Enemy attacks the decoy but is taunted onto hask; pull brings it adjacent.
	ev := a.step([]engine.Order{castU(hk.ID, "test_taunt", e.ID)}, []engine.Order{atk(e.ID, decoy.ID)})
	if a.u(e.ID).Pos != P(6, 5) {
		t.Fatalf("pull left enemy at %v", a.u(e.ID).Pos)
	}
	att := find(ev, engine.EvAttacked)
	if len(att) != 1 || att[0].Target != hk.ID {
		t.Fatalf("taunt should redirect the attack: %+v", att)
	}
	if !a.u(e.ID).Cloaked() {
		t.Fatal("cloak missing")
	}
	// Cloaked enemy adjacent to a friendly is visible; move the friendly away and it vanishes.
	if viewUnit(a, 0, e.ID) == nil {
		t.Fatal("adjacent cloaked unit should be visible")
	}
	a.u(hk.ID).Pos = P(2, 5)
	a.u(decoy.ID).Pos = P(2, 6)
	if viewUnit(a, 0, e.ID) != nil {
		t.Fatal("cloaked unit at distance should be hidden")
	}
}

func TestFilterEvents(t *testing.T) {
	a := newArena(t, "hask", "wren", true)
	h := a.add(0, "hask", 2, 5)
	e1 := a.add(1, "lineman", 17, 5)
	e2 := a.add(1, "lineman", 17, 6)
	e2.HP = 1
	ev := a.step([]engine.Order{mv(h.ID, P(3, 5))}, []engine.Order{atk(e1.ID, e2.ID)})
	mine := engine.FilterEvents(a.c, &a.s, 0, ev)
	for _, e := range mine {
		if e.Kind == engine.EvAttacked || e.Kind == engine.EvDamaged {
			t.Fatalf("team 0 should not see far-away enemy attack: %+v", e)
		}
	}
	if len(find(mine, engine.EvMoved)) != 1 {
		t.Fatal("own move missing")
	}
	theirs := engine.FilterEvents(a.c, &a.s, 1, ev)
	if len(find(theirs, engine.EvMoved)) != 0 {
		t.Fatal("team 1 should not see the hidden move")
	}
}

func viewUnit(a *arena, player, id int) *engine.Unit {
	v := engine.View(a.c, a.s, player)
	return v.Unit(id)
}

// Regression: spawning appends to s.Units; orders resolved afterwards must
// not write through stale pointers.
func TestSpawnThenMoveSameTurn(t *testing.T) {
	a := newArena(t, "tally", "hask", true)
	ty := a.add(0, "tally", 5, 5)
	l := a.add(0, "lineman", 5, 7)
	a.step([]engine.Order{castT(ty.ID, "spin_up", 6, 5), mv(ty.ID, P(4, 5), P(3, 5))}, nil)
	if a.u(ty.ID).Pos != P(3, 5) {
		t.Fatalf("tally at %v after spawn+move, want (3,5)", a.u(ty.ID).Pos)
	}
	a.step([]engine.Order{castT(ty.ID, "spin_up", 3, 6), mv(l.ID, P(6, 7))}, nil)
	if a.u(l.ID).Pos != P(6, 7) {
		t.Fatalf("lineman at %v, want (6,7)", a.u(l.ID).Pos)
	}
}
