package balance

import (
	"testing"

	"rfog/bots"
	"rfog/data"
	"rfog/engine"
)

// In a mirror match (same hero both sides) nothing but the side differs,
// so side A should win about half the decided games. Row-major tie-breaks
// in pathing and a line of sight that rounded differently for each side
// once made this 2 in 60 for wren.
func TestMirrorMatchesAreFair(t *testing.T) {
	if testing.Short() {
		t.Skip("plays 100 matches")
	}
	c, err := data.Load()
	if err != nil {
		t.Fatal(err)
	}
	aWins, decided := 0, 0
	for _, h := range c.HeroIDs() {
		for n := 0; n < 20; n++ {
			seed := uint64(1000 + n)
			st := engine.Setup{ID: "mirror", Mode: "1v1", Seed: seed}
			st.Players = []engine.SetupPlayer{{Name: h, Hero: h}, {Name: h, Hero: h}}
			s, err := engine.NewMatch(c, st)
			if err != nil {
				t.Fatal(err)
			}
			bs := []*bots.Bot{bots.New(bots.Normal, seed), bots.New(bots.Normal, seed+7919)}
			for !s.Ended() {
				orders := map[int][]engine.Order{}
				for k := range bs {
					v := engine.View(c, s, k)
					orders[k] = bs[k].Orders(c, &v, k)
				}
				s, _ = engine.Step(c, s, orders, s.Match.Seed)
			}
			if s.Match.Winner >= 0 {
				decided++
			}
			if s.Match.Winner == 0 {
				aWins++
			}
		}
	}
	rate := float64(aWins) / float64(decided)
	t.Logf("side A wins %d of %d decided mirror matches (%.0f%%)", aWins, decided, rate*100)
	if rate < 0.35 || rate > 0.65 {
		t.Fatalf("side A wins %.0f%% of mirror matches: the rules favour a side", rate*100)
	}
}
