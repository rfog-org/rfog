package bots_test

import (
	"testing"

	"rfog/bots"
	"rfog/data"
	"rfog/engine"
)

func play(t *testing.T, seed uint64, a, b string, level string) (engine.State, int) {
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
	b0, b1 := bots.New(level, seed), bots.New(level, seed+1)
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
		var bs []*bots.Bot
		for i := 0; i < n; i++ {
			bs = append(bs, bots.New(bots.Normal, uint64(i)))
		}
		turns := 0
		for !s.Ended() && turns < 30 {
			orders := map[int][]engine.Order{}
			for i := 0; i < n; i++ {
				v := engine.View(c, s, i)
				orders[i] = bs[i].Orders(c, &v, i)
				if errs := engine.Validate(c, &v, i, orders[i]); len(errs) > 0 {
					t.Fatalf("%s: bot %d illegal orders: %v", mode, i, errs)
				}
			}
			s, _ = engine.Step(c, s, orders, s.Match.Seed)
			turns++
		}
		if !s.Ended() {
			t.Fatalf("%s: did not end", mode)
		}
		if s.Teams[0].Score+s.Teams[1].Score == 0 {
			t.Errorf("%s: nobody scored", mode)
		}
	}
}
