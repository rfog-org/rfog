package webplay

import (
	"encoding/json"
	"testing"

	"rfog/engine"
)

// A whole match plays through the API a browser uses: moves, targets,
// ability targets, legal orders, turns until the end.
func TestGamePlaysToTheEnd(t *testing.T) {
	c, err := Content()
	if err != nil {
		t.Fatal(err)
	}
	g, err := NewGame(c, "hask", "wren", "normal", 42)
	if err != nil {
		t.Fatal(err)
	}
	v := g.View()
	if v.Board.W != 8 || v.Board.H != 8 {
		t.Fatalf("1v1 board %dx%d, want the 8x8", v.Board.W, v.Board.H)
	}
	for turn := 0; turn < 20 && !g.s.Ended(); turn++ {
		v := g.View()
		var orders []engine.Order
		for i := range v.Units {
			u := &v.Units[i]
			if u.Owner != 0 || !u.Alive() {
				continue
			}
			if ts := g.Targets(u.ID, u.Pos); len(ts) > 0 {
				orders = append(orders, engine.Order{UnitID: u.ID, Action: engine.ActAttack, TargetU: ts[0].Unit, Target: ts[0].At})
				continue
			}
			if ms := g.Moves(u.ID); len(ms) > 0 {
				best := ms[0]
				for _, m := range ms {
					if m.To.X > best.To.X {
						best = m
					}
				}
				o := engine.Order{UnitID: u.ID, Action: engine.ActMove, Path: best.Path}
				if len(g.Check(append(orders, o))) == 0 {
					orders = append(orders, o)
				}
			}
		}
		if errs := g.Check(orders); len(errs) > 0 {
			t.Fatalf("turn %d: own orders refused: %v", turn, errs)
		}
		tr, err := g.Commit(orders)
		if err != nil {
			t.Fatal(err)
		}
		if tr.Post.Match.Turn <= tr.Pre.Match.Turn && !tr.Ended {
			t.Fatalf("turn did not advance: %d -> %d", tr.Pre.Match.Turn, tr.Post.Match.Turn)
		}
	}
	if !g.s.Ended() {
		t.Fatal("match did not end")
	}
}

// Ability targets come from the engine's validation: a commander's
// self-cast has one target (itself), a unit-targeted one lists units.
func TestAbilityTargets(t *testing.T) {
	c, _ := Content()
	g, _ := NewGame(c, "hask", "wren", "normal", 1)
	v := g.View()
	cmd := v.Unit(v.Player(0).Commander)
	ts, err := g.AbilityTargets(cmd.ID, "brace", nil)
	if err != nil || len(ts) != 1 || ts[0].Unit != cmd.ID {
		t.Fatalf("brace targets: %+v %v", ts, err)
	}
	if _, err := g.AbilityTargets(cmd.ID, "nonsense", nil); err == nil {
		t.Fatal("unknown ability accepted")
	}
}

// A saved match resumes where it was (through JSON, as the page keeps
// it), and a save from other rules is refused.
func TestSaveAndLoad(t *testing.T) {
	c, err := Content()
	if err != nil {
		t.Fatal(err)
	}
	g, err := NewGame(c, "", "", "normal", 11)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3 && !g.s.Ended(); i++ {
		if _, err := g.Commit(nil); err != nil {
			t.Fatal(err)
		}
	}
	if g.s.Ended() {
		t.Skip("match ended before it could be saved")
	}
	b, err := json.Marshal(g.Save())
	if err != nil {
		t.Fatal(err)
	}
	var sv Save
	if err := json.Unmarshal(b, &sv); err != nil {
		t.Fatal(err)
	}
	h, err := Load(c, sv)
	if err != nil {
		t.Fatal(err)
	}
	if engine.Hash(h.s) != engine.Hash(g.s) {
		t.Fatal("resumed state differs from the saved one")
	}
	if _, err := h.Commit(nil); err != nil {
		t.Fatalf("resumed match does not play: %v", err)
	}
	sv.Rules = "other"
	if _, err := Load(c, sv); err != ErrOldRules {
		t.Fatalf("save under other rules: %v, want ErrOldRules", err)
	}
}

// Online, the page previews against the server's fogged view: moves and
// targets come from the view, and a turn is never resolved locally.
func TestRemotePreviews(t *testing.T) {
	c, err := Content()
	if err != nil {
		t.Fatal(err)
	}
	g, err := NewGame(c, "", "", "normal", 5)
	if err != nil {
		t.Fatal(err)
	}
	view := g.View()
	b, _ := json.Marshal(view)
	var v engine.State
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatal(err)
	}
	r := Remote(c, v, 0)
	var mine *engine.Unit
	for i := range v.Units {
		if v.Units[i].Owner == 0 {
			mine = &v.Units[i]
			break
		}
	}
	if mine == nil || len(r.Moves(mine.ID)) == 0 {
		t.Fatal("no moves previewed from the server's view")
	}
	if len(r.Moves(mine.ID)) != len(g.Moves(mine.ID)) {
		t.Fatalf("remote moves %d, local %d", len(r.Moves(mine.ID)), len(g.Moves(mine.ID)))
	}
	if _, err := r.Commit(nil); err == nil {
		t.Fatal("a remote game resolved a turn")
	}
}

// A game records its replay as it goes; the viewer steps through it and
// lands where the game did.
func TestReplayOfAGame(t *testing.T) {
	c, err := Content()
	if err != nil {
		t.Fatal(err)
	}
	g, err := NewGame(c, "hask", "wren", "normal", 9)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 4 && !g.s.Ended(); i++ {
		if _, err := g.Commit(nil); err != nil {
			t.Fatal(err)
		}
	}
	b, err := g.Replay().Marshal()
	if err != nil {
		t.Fatal(err)
	}
	v, err := LoadReplay(c, b)
	if err != nil {
		t.Fatal(err)
	}
	if v.Turns() != len(g.Replay().Turns) || v.Turns() == 0 {
		t.Fatalf("turns %d, recorded %d", v.Turns(), len(g.Replay().Turns))
	}
	last, err := v.Turn(v.Turns() - 1)
	if err != nil {
		t.Fatal(err)
	}
	if engine.Hash(last.Post) != engine.Hash(g.s) {
		t.Fatal("the replay does not end where the game did")
	}
	if _, err := v.Turn(v.Turns()); err == nil {
		t.Fatal("a turn past the end")
	}
}

// Hotseat: the first player's commit hands over (nothing resolves), the
// second's plays the turn whole, and each plans only their own units.
func TestHotseat(t *testing.T) {
	c, err := Content()
	if err != nil {
		t.Fatal(err)
	}
	g, err := NewHotseat(c, "hask", "wren", 9)
	if err != nil {
		t.Fatal(err)
	}
	advance := func(p int) []engine.Order {
		if g.Planner() != p {
			t.Fatalf("planner %d, want %d", g.Planner(), p)
		}
		v := g.View()
		var orders []engine.Order
		for i := range v.Units {
			u := &v.Units[i]
			if u.Owner != p || !u.Alive() {
				continue
			}
			if ms := g.Moves(u.ID); len(ms) > 0 {
				o := engine.Order{UnitID: u.ID, Action: engine.ActMove, Path: ms[0].Path}
				if len(g.Check(append(orders, o))) == 0 {
					orders = append(orders, o)
				}
			}
		}
		if len(orders) == 0 {
			t.Fatalf("player %d has no moves", p)
		}
		return orders
	}
	for turn := 1; turn <= 3; turn++ {
		tr, err := g.Commit(advance(0))
		if err != nil {
			t.Fatal(err)
		}
		if !tr.Pass || g.s.Match.Turn != turn {
			t.Fatalf("turn %d: first commit resolved (pass %v, turn %d)", turn, tr.Pass, g.s.Match.Turn)
		}
		tr, err = g.Commit(advance(1))
		if err != nil {
			t.Fatal(err)
		}
		if tr.Pass || !tr.Whole || g.s.Match.Turn != turn+1 || len(tr.Events) == 0 {
			t.Fatalf("turn %d: second commit did not resolve whole (turn %d, %d events)", turn, g.s.Match.Turn, len(tr.Events))
		}
	}
	if n := len(g.Replay().Turns); n != 3 {
		t.Fatalf("replay has %d turns, want 3", n)
	}
}
