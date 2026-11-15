package bots_test

import (
	"testing"

	"rfog/bots"
	"rfog/data"
	"rfog/engine"
)

// play runs a bot match: one level for both seats, or one for each.
func play(t *testing.T, seed uint64, a, b string, level ...string) (engine.State, int) {
	t.Helper()
	c, err := data.Load()
	if err != nil {
		t.Fatal(err)
	}
	st := engine.Setup{ID: "bots", Mode: "1v1", Seed: seed,
		Players: []engine.SetupPlayer{{Name: "A", Hero: a}, {Name: "B", Hero: b}}}
	s, err := engine.NewMatch(c, st)
	if err != nil {
		t.Fatal(err)
	}
	b0, b1 := bots.New(level[0], seed), bots.New(level[len(level)-1], seed+1)
	turns := 0
	for !s.Ended() && turns < 30 {
		orders := map[int][]engine.Order{}
		v0, v1 := engine.View(c, s, 0), engine.View(c, s, 1)
		orders[0] = b0.Orders(c, &v0, 0)
		orders[1] = b1.Orders(c, &v1, 1)
		for pid, os := range orders {
			v := engine.View(c, s, pid)
			if errs := engine.Validate(c, &v, pid, os); len(errs) > 0 {
				t.Fatalf("bot %d issued illegal orders: %v", pid, errs)
			}
		}
		s, _ = engine.Step(c, s, orders, s.Match.Seed)
		turns++
	}
	// What a bot plans is what it plays: an order its own last filter had
	// to drop is an action the planner lost without knowing.
	if n := b0.Dropped + b1.Dropped; n > 0 && level[0] != bots.Easy {
		t.Errorf("%s vs %s seed %d: bots planned %d orders the engine refuses", a, b, seed, n)
	}
	return s, turns
}

func TestBotsFinishMatches(t *testing.T) {
	heroes := []string{"hask", "wren", "nul", "tally", "mott"}
	// Under net scoring a 0-0 draw is a legal result (a turn where both
	// sides hold as much pays nobody), so one scoreless match in five is
	// allowed; more means the bots are not playing the objectives.
	scoreless := 0
	for i, a := range heroes {
		b := heroes[(i+2)%len(heroes)]
		s, turns := play(t, uint64(100+i), a, b, bots.Normal)
		if !s.Ended() {
			t.Fatalf("%s vs %s: match did not end in %d turns", a, b, turns)
		}
		if s.Teams[0].Score+s.Teams[1].Score == 0 {
			scoreless++
			t.Logf("%s vs %s: nobody scored", a, b)
		}
	}
	if scoreless > 1 {
		t.Errorf("%d of %d matches scoreless", scoreless, len(heroes))
	}
}

func TestBotsDeterministic(t *testing.T) {
	s1, _ := play(t, 9, "hask", "wren", bots.Normal)
	s2, _ := play(t, 9, "hask", "wren", bots.Normal)
	if engine.Hash(s1) != engine.Hash(s2) {
		t.Fatal("bot play is not deterministic")
	}
	e1, _ := play(t, 9, "tally", "mott", bots.Easy)
	e2, _ := play(t, 9, "tally", "mott", bots.Easy)
	if engine.Hash(e1) != engine.Hash(e2) {
		t.Fatal("easy bot play is not deterministic")
	}
	h1, _ := play(t, 9, "volt", "kite", bots.Hard)
	h2, _ := play(t, 9, "volt", "kite", bots.Hard)
	if engine.Hash(h1) != engine.Hash(h2) {
		t.Fatal("hard bot play is not deterministic")
	}
}

// TestHardBeatsNormal: the same commander on both sides, hard in each seat
// in turn. Looking ahead has to be worth more than it costs.
func TestHardBeatsNormal(t *testing.T) {
	c, err := data.Load()
	if err != nil {
		t.Fatal(err)
	}
	won, lost := 0, 0
	for i, h := range c.HeroIDs() {
		for seat, levels := range [][]string{{bots.Hard, bots.Normal}, {bots.Normal, bots.Hard}} {
			s, _ := play(t, uint64(40+i), h, h, levels...)
			switch s.Match.Winner {
			case seat:
				won++
			case 1 - seat:
				lost++
			}
		}
	}
	t.Logf("hard %d, normal %d", won, lost)
	if won <= lost {
		t.Errorf("hard won %d and lost %d against normal", won, lost)
	}
}

func TestBotsFight(t *testing.T) {
	// Over a few seeds and matchups, bots must actually kill things.
	kills := 0
	pairs := [][2]string{{"hask", "nul"}, {"wren", "tally"}, {"mott", "hask"}, {"nul", "wren"}}
	for seed := uint64(1); seed <= 8; seed++ {
		p := pairs[seed%4]
		s, _ := play(t, seed, p[0], p[1], bots.Normal)
		kills += s.Teams[0].Kills + s.Teams[1].Kills
	}
	if kills == 0 {
		t.Fatal("bots never killed anything in eight matches")
	}
}

// TestBotsTeamModes runs bot-only matches in every team mode: the maps
// must deploy everyone and the bots must play to a finish.
func TestBotsTeamModes(t *testing.T) {
	c, err := data.Load()
	if err != nil {
		t.Fatal(err)
	}
	heroes := c.HeroIDs()
	for _, mode := range []string{"2v2", "3v3", "4v4", "5v5"} {
		n := c.Rules.Modes[mode].PlayersPerTeam * 2
		st := engine.Setup{ID: mode, Mode: mode, Seed: 7}
		for i := 0; i < n; i++ {
			st.Players = append(st.Players, engine.SetupPlayer{Name: "p", Hero: heroes[i%len(heroes)]})
		}
		s, err := engine.NewMatch(c, st)
		if err != nil {
			t.Fatalf("%s: %v", mode, err)
		}
		if s.Match.WinScore != c.Rules.Modes[mode].WinScore && c.Rules.Modes[mode].WinScore != 0 {
			t.Fatalf("%s: win score %d", mode, s.Match.WinScore)
		}
		table := bots.NewTable(bots.Normal, 7, n)
		turns, clashes := 0, 0
		for !s.Ended() && turns < 30 {
			orders := map[int][]engine.Order{}
			table.Orders(c, s, orders)
			// Teammates in different seats never pick the same square: two
			// allies walking into one square both stop short.
			taken := [2]map[engine.Pos]int{{}, {}}
			for i := 0; i < n; i++ {
				team := s.Player(i).Team
				v := engine.View(c, s, i)
				if errs := engine.Validate(c, &v, i, orders[i]); len(errs) > 0 {
					t.Fatalf("%s: bot %d illegal orders: %v", mode, i, errs)
				}
				for _, u := range s.TeamUnits(team) {
					if u.Owner != i {
						continue
					}
					d := engine.AttackFrom(c, u, orders[i])
					if d == u.Pos {
						continue
					}
					if seat, ok := taken[team][d]; ok && seat != i {
						clashes++
					}
					taken[team][d] = i
				}
			}
			s, _ = engine.Step(c, s, orders, s.Match.Seed)
			turns++
		}
		if clashes > 0 {
			t.Errorf("%s: allies ordered into the same square %d times", mode, clashes)
		}
		if table.Dropped() > 0 {
			t.Errorf("%s: bots planned %d orders the engine refuses", mode, table.Dropped())
		}
		if !s.Ended() {
			t.Fatalf("%s: did not end", mode)
		}
		if s.Teams[0].Score+s.Teams[1].Score == 0 {
			t.Errorf("%s: nobody scored", mode)
		}
	}
}

// Lineup fills the empty seats: never a hero twice on one team, the given
// picks kept, and in 1v1 the same draw the offline clients always made.
func TestLineup(t *testing.T) {
	c, err := data.Load()
	if err != nil {
		t.Fatal(err)
	}
	heroes := c.HeroIDs()
	for seed := uint64(1); seed < 40; seed++ {
		got, err := bots.Lineup(c, "1v1", []string{"hask"}, seed)
		if err != nil || len(got) != 2 || got[0] != "hask" || got[1] != heroes[engine.NewRNG(seed, 1).Intn(len(heroes))] {
			t.Fatalf("1v1 seed %d: %v %v", seed, got, err)
		}
		for _, mode := range []string{"2v2", "3v3", "5v5"} {
			per := c.Rules.Modes[mode].PlayersPerTeam
			got, err := bots.Lineup(c, mode, []string{"hask"}, seed)
			if err != nil || len(got) != 2*per || got[0] != "hask" {
				t.Fatalf("%s seed %d: %v %v", mode, seed, got, err)
			}
			for team := 0; team < 2; team++ {
				seen := map[string]bool{}
				for _, h := range got[team*per : (team+1)*per] {
					if seen[h] {
						t.Fatalf("%s seed %d: %s twice on team %d: %v", mode, seed, h, team, got)
					}
					seen[h] = true
				}
			}
		}
	}
	if _, err := bots.Lineup(c, "9v9", nil, 1); err == nil {
		t.Fatal("unknown mode accepted")
	}
}
