package engine_test

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"rfog/data"
	"rfog/engine"
)

var update = flag.Bool("update", false, "rewrite golden replay files")

func content(t testing.TB) *engine.Content {
	t.Helper()
	c, err := data.Load()
	if err != nil {
		t.Fatalf("load data: %v", err)
	}
	// The mechanics tests place units against relay's walls and cover, and
	// the golden replays were recorded on it: pin it, whatever map 1v1
	// plays on now.
	m := c.Rules.Modes["1v1"]
	m.Map = "relay"
	c.Rules.Modes["1v1"] = m
	return c
}

func setup1v1(seed uint64, a, b string) engine.Setup {
	return engine.Setup{ID: "test", Mode: "1v1", Seed: seed,
		Players: []engine.SetupPlayer{{Name: "A", Hero: a}, {Name: "B", Hero: b}}}
}

func newMatch(t testing.TB, c *engine.Content, st engine.Setup) engine.State {
	t.Helper()
	s, err := engine.NewMatch(c, st)
	if err != nil {
		t.Fatalf("new match: %v", err)
	}
	return s
}

// playRandom runs a full match with random legal orders and returns the
// replay and final state.
func playRandom(t testing.TB, c *engine.Content, st engine.Setup) (*engine.Replay, engine.State) {
	t.Helper()
	s := newMatch(t, c, st)
	rp := engine.NewReplay(st, s)
	rng := engine.NewRNG(st.Seed, 9999)
	for turn := 0; turn < 40 && !s.Ended(); turn++ {
		orders := map[int][]engine.Order{}
		for _, p := range s.Players {
			orders[p.ID] = engine.RandomOrders(c, &s, p.ID, rng)
		}
		rp.Record(orders)
		var ev []engine.Event
		s, ev = engine.Step(c, s, orders, s.Match.Seed)
		checkInvariants(t, c, &s, ev)
	}
	rp.FinalHash = engine.Hash(s)
	return rp, s
}

func checkInvariants(t testing.TB, c *engine.Content, s *engine.State, ev []engine.Event) {
	t.Helper()
	seen := map[engine.Pos]int{}
	for _, u := range s.Units {
		if u.HP < 0 {
			t.Fatalf("unit %d HP %d < 0", u.ID, u.HP)
		}
		if u.HP > u.MaxHP {
			t.Fatalf("unit %d HP %d > max %d", u.ID, u.HP, u.MaxHP)
		}
		if !u.Alive() {
			continue
		}
		if !s.Board.Passable(u.Pos) {
			t.Fatalf("unit %d stands on wall/off-board at %v", u.ID, u.Pos)
		}
		if o, dup := seen[u.Pos]; dup {
			t.Fatalf("units %d and %d share tile %v (turn %d)", o, u.ID, u.Pos, s.Match.Turn)
		}
		seen[u.Pos] = u.ID
	}
	for _, e := range ev {
		if e.Kind == engine.EvObjectiveScored && e.Amount <= 0 {
			t.Fatalf("non-positive score event")
		}
	}
}

func TestLoadContent(t *testing.T) {
	c := content(t)
	if len(c.Heroes) != 10 {
		t.Fatalf("want 10 heroes, got %d", len(c.Heroes))
	}
	if len(c.Units) != 6 {
		t.Fatalf("want 6 units (4 + junkbot + drone), got %d", len(c.Units))
	}
	if _, ok := c.Maps["relay"]; !ok {
		t.Fatal("relay map missing")
	}
	for id, h := range c.Heroes {
		for _, k := range []string{"q", "w", "e", "r"} {
			if _, ok := h.Abilities[k]; !ok {
				t.Errorf("hero %s missing ability %s", id, k)
			}
		}
	}
}

func TestMapSymmetric(t *testing.T) {
	c := content(t)
	want := map[string]int{"relay": 3, "relay24": 3, "relay28": 5, "relay32": 5}
	for id, nobj := range want {
		m := c.Maps[id]
		for y := 0; y < m.Height; y++ {
			for x := 0; x < m.Width; x++ {
				a := m.Tiles[y*m.Width+x]
				b := m.Tiles[(m.Height-1-y)*m.Width+(m.Width-1-x)]
				if a != b {
					t.Errorf("%s: tile (%d,%d)=%v not symmetric with (%d,%d)=%v", id, x, y, a, m.Width-1-x, m.Height-1-y, b)
				}
			}
		}
		if len(m.Objectives) != nobj {
			t.Fatalf("%s: want %d objectives, got %d", id, nobj, len(m.Objectives))
		}
	}
}

func TestNewMatchDeploys(t *testing.T) {
	c := content(t)
	s := newMatch(t, c, setup1v1(1, "hask", "wren"))
	if len(s.Units) != 10 {
		t.Fatalf("want 10 units, got %d", len(s.Units))
	}
	for _, u := range s.Units {
		if u.Team == 0 && u.Pos.X >= s.Board.DeployCols {
			t.Errorf("team 0 unit %d deployed at %v", u.ID, u.Pos)
		}
		if u.Team == 1 && u.Pos.X < s.Board.W-s.Board.DeployCols {
			t.Errorf("team 1 unit %d deployed at %v", u.ID, u.Pos)
		}
	}
	checkInvariants(t, c, &s, nil)
}

func TestDeterminism(t *testing.T) {
	c := content(t)
	heroes := c.HeroIDs()
	for seed := uint64(1); seed <= 6; seed++ {
		st := setup1v1(seed, heroes[int(seed)%len(heroes)], heroes[int(seed+2)%len(heroes)])
		rp1, s1 := playRandom(t, c, st)
		rp2, s2 := playRandom(t, c, st)
		if engine.Hash(s1) != engine.Hash(s2) {
			t.Fatalf("seed %d: two runs differ", seed)
		}
		if len(rp1.Turns) != len(rp2.Turns) {
			t.Fatalf("seed %d: turn counts differ", seed)
		}
		// Replaying the recorded orders reproduces the state.
		s3, _, err := rp1.Run(c)
		if err != nil {
			t.Fatalf("seed %d: replay: %v", seed, err)
		}
		if engine.Hash(s3) != engine.Hash(s1) {
			t.Fatalf("seed %d: replay hash differs", seed)
		}
		if !s1.Ended() {
			t.Fatalf("seed %d: match did not end within 40 turns (turn %d)", seed, s1.Match.Turn)
		}
	}
}

func TestReplayRoundTrip(t *testing.T) {
	c := content(t)
	rp, s := playRandom(t, c, setup1v1(42, "tally", "mott"))
	b, err := rp.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	rp2, err := engine.UnmarshalReplay(b)
	if err != nil {
		t.Fatal(err)
	}
	s2, _, err := rp2.Run(c)
	if err != nil {
		t.Fatal(err)
	}
	if engine.Hash(s2) != engine.Hash(s) {
		t.Fatal("round-tripped replay differs")
	}
}

func TestStepIsPure(t *testing.T) {
	c := content(t)
	s := newMatch(t, c, setup1v1(7, "hask", "nul"))
	before := engine.Hash(s)
	rng := engine.NewRNG(7, 1)
	orders := map[int][]engine.Order{0: engine.RandomOrders(c, &s, 0, rng), 1: engine.RandomOrders(c, &s, 1, rng)}
	engine.Step(c, s, orders, s.Match.Seed)
	if engine.Hash(s) != before {
		t.Fatal("Step mutated its input")
	}
}

func TestScoreMonotonic(t *testing.T) {
	c := content(t)
	s := newMatch(t, c, setup1v1(3, "wren", "hask"))
	rng := engine.NewRNG(3, 1)
	for !s.Ended() {
		orders := map[int][]engine.Order{}
		for _, p := range s.Players {
			orders[p.ID] = engine.RandomOrders(c, &s, p.ID, rng)
		}
		prev := []int{s.Teams[0].Score, s.Teams[1].Score}
		s, _ = engine.Step(c, s, orders, s.Match.Seed)
		for i, tm := range s.Teams {
			if tm.Score < prev[i] {
				t.Fatalf("score decreased for team %d", i)
			}
		}
	}
}

// Golden replays: generated once with -update, then the final hash must
// never change unless the rules change on purpose.
func TestGoldenReplays(t *testing.T) {
	c := content(t)
	dir := "testdata"
	cases := []struct {
		name string
		st   engine.Setup
	}{
		{"hask_vs_wren", setup1v1(101, "hask", "wren")},
		{"nul_vs_tally", setup1v1(202, "nul", "tally")},
		{"mott_vs_hask", setup1v1(303, "mott", "hask")},
		{"deterministic_mode", func() engine.Setup { s := setup1v1(404, "tally", "wren"); s.Deterministic = true; return s }()},
	}
	for _, tc := range cases {
		path := filepath.Join(dir, tc.name+".replay.json")
		if *update {
			rp, _ := playRandom(t, c, tc.st)
			b, err := rp.Marshal()
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, b, 0o644); err != nil {
				t.Fatal(err)
			}
			continue
		}
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("%s: %v (run with -update to generate)", path, err)
		}
		rp, err := engine.UnmarshalReplay(b)
		if err != nil {
			t.Fatal(err)
		}
		s, _, err := rp.Run(c)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if got := engine.Hash(s); got != rp.FinalHash {
			t.Errorf("%s: final hash %s, golden %s", tc.name, got, rp.FinalHash)
		}
	}
}

func TestViewHidesEnemies(t *testing.T) {
	c := content(t)
	s := newMatch(t, c, setup1v1(5, "hask", "wren"))
	v := engine.View(c, s, 0)
	for _, u := range v.Units {
		if u.Team == 1 {
			t.Fatalf("enemy unit %d visible at deploy distance", u.ID)
		}
	}
	if len(v.Visible) == 0 {
		t.Fatal("no visible tiles")
	}
	for _, u := range v.Units {
		if u.Team != 0 {
			continue
		}
		found := false
		for _, p := range v.Visible {
			if p == u.Pos {
				found = true
			}
		}
		if !found {
			t.Fatalf("own unit %d at %v not in visible set", u.ID, u.Pos)
		}
	}
}

func TestExpectedHits(t *testing.T) {
	cases := []struct{ dice, tn, want int }{
		{4, 4, 2}, // 4 * 0.5 = 2
		{3, 5, 1}, // 3 * 1/3 = 1
		{3, 4, 2}, // 1.5 rounds up to 2
		{2, 6, 1}, // 0.33 rounds to 0? no: 2/6 = 0.33 -> 0
		{0, 4, 0},
		{5, 7, 0},
	}
	cases[3].want = 0
	for _, tc := range cases {
		if got := engine.ExpectedHits(tc.dice, tc.tn); got != tc.want {
			t.Errorf("ExpectedHits(%d,%d) = %d, want %d", tc.dice, tc.tn, got, tc.want)
		}
	}
}

func TestLineSymmetric(t *testing.T) {
	a, b := engine.Pos{X: 1, Y: 2}, engine.Pos{X: 9, Y: 5}
	l1 := engine.Line(a, b)
	l2 := engine.Line(b, a)
	if len(l1) != len(l2) {
		t.Fatal("lengths differ")
	}
	for i := range l1 {
		if l1[i] != l2[len(l2)-1-i] {
			t.Fatalf("line not symmetric: %v vs %v", l1, l2)
		}
	}
	if strings.Contains("", "x") {
		t.Fatal("unreachable")
	}
}
