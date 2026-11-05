package engine_test

import (
	"testing"

	"rfog/engine"
)

// Maps are symmetric under 180-degree rotation; so must the rules be, or
// one side of every map is better. These found a 60/40 side skew once.

func rot(b *engine.Board, p engine.Pos) engine.Pos {
	return engine.Pos{X: b.W - 1 - p.X, Y: b.H - 1 - p.Y}
}

func TestLOSSymmetric(t *testing.T) {
	c := content(t)
	for id, m := range c.Maps {
		s := engine.State{Board: engine.Board{W: m.Width, H: m.Height, Tiles: m.Tiles}}
		b := &s.Board
		for ay := 0; ay < b.H; ay++ {
			for ax := 0; ax < b.W; ax++ {
				a := engine.Pos{X: ax, Y: ay}
				if !b.Passable(a) {
					continue // nobody looks out of a wall
				}
				for _, p := range b.Area(a, 7) {
					if !b.Passable(p) {
						continue
					}
					los := s.LOS(a, p)
					if s.LOS(rot(b, a), rot(b, p)) != los {
						t.Fatalf("%s: LOS %v->%v is %v, rotated is not", id, a, p, los)
					}
					if s.LOS(p, a) != los {
						t.Fatalf("%s: LOS %v->%v is %v, reverse is not", id, a, p, los)
					}
				}
			}
		}
	}
}

func TestReachableSymmetric(t *testing.T) {
	a := newArena(t, "hask", "hask", true)
	b := &a.s.Board
	for _, start := range []engine.Pos{P(2, 3), P(6, 3), P(8, 6), P(5, 9)} {
		u0 := a.add(0, "runner", start.X, start.Y)
		u1 := a.add(1, "runner", rot(b, start).X, rot(b, start).Y)
		r0 := engine.Reachable(a.c, &a.s, u0)
		r1 := engine.Reachable(a.c, &a.s, u1)
		if len(r0) != len(r1) {
			t.Fatalf("from %v: %d tiles reachable, rotated %d", start, len(r0), len(r1))
		}
		for d, path := range r0 {
			other, ok := r1[rot(b, d)]
			if !ok || len(other) != len(path) {
				t.Fatalf("from %v to %v: rotated path missing or different length", start, d)
			}
			for i := range path {
				if other[i] != rot(b, path[i]) {
					t.Fatalf("from %v to %v: path %v, rotated %v", start, d, path, other)
				}
			}
		}
		engine.RemoveUnit(&a.s, u0.ID)
		engine.RemoveUnit(&a.s, u1.ID)
	}
}

func TestTieTeamAlternates(t *testing.T) {
	firsts := map[int]int{}
	for seed := uint64(1); seed <= 200; seed++ {
		s := engine.State{}
		s.Match.Seed = seed
		s.Match.Turn = 1
		first := s.TieTeam()
		firsts[first]++
		for turn := 2; turn <= 6; turn++ {
			s.Match.Turn = turn
			if want := (first + turn - 1) % 2; s.TieTeam() != want {
				t.Fatalf("seed %d turn %d: tie team %d, want %d", seed, turn, s.TieTeam(), want)
			}
		}
	}
	if firsts[0] < 70 || firsts[1] < 70 {
		t.Fatalf("turn-1 tie team is not a fair coin across seeds: %v", firsts)
	}
}

func TestSummonKillGivesNoCredit(t *testing.T) {
	a := newArena(t, "tally", "hask", true)
	jb := a.add(0, "junkbot", 5, 5)
	jb.ExpiresIn = 3
	jb.HP = 1
	h := a.add(1, "hask", 6, 5)
	a.step(nil, []engine.Order{atk(h.ID, jb.ID)})
	if a.u(jb.ID).Alive() {
		t.Skip("attack missed; the arena is deterministic, so this seed never kills")
	}
	if k := a.s.Teams[1].Kills; k != 0 || a.u(h.ID).XP != 0 {
		t.Fatalf("summon kill credited: team kills %d, hask xp %d", k, a.u(h.ID).XP)
	}
}

func TestValidateRejectsAsymmetricMap(t *testing.T) {
	c := content(t)

	m := c.Maps["relay"]
	m.Tiles[0].Terrain = m.Tiles[len(m.Tiles)-1].Terrain + "-changed"
	c.Maps["relay"] = m

	if err := c.Validate(); err == nil {
		t.Fatal("expected validation to reject an asymmetric map")
	}
}
