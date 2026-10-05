package rating

import (
	"math"
	"testing"
)

// The worked example from Glickman's "Example of the Glicko-2 system".
func TestPaperExample(t *testing.T) {
	r := Rating{R: 1500, RD: 200, Vol: 0.06}
	got := Update(r, []Result{
		{Opp: Rating{R: 1400, RD: 30}, Score: 1},
		{Opp: Rating{R: 1550, RD: 100}, Score: 0},
		{Opp: Rating{R: 1700, RD: 300}, Score: 0},
	})
	if math.Abs(got.R-1464.06) > 0.01 || math.Abs(got.RD-151.52) > 0.01 || math.Abs(got.Vol-0.05999) > 0.00001 {
		t.Fatalf("got %+v", got)
	}
}

func TestNoGamesGrowsRD(t *testing.T) {
	got := Update(Rating{R: 1500, RD: 50, Vol: 0.06}, nil)
	if got.R != 1500 || got.RD <= 50 {
		t.Fatalf("got %+v", got)
	}
}

func TestWinRaisesLossLowers(t *testing.T) {
	w := Update(Default, []Result{{Opp: Default, Score: 1}})
	l := Update(Default, []Result{{Opp: Default, Score: 0}})
	if w.R <= 1500 || l.R >= 1500 || w.RD >= 350 {
		t.Fatalf("win %+v loss %+v", w, l)
	}
	if p := Expected(w, l); p <= 0.5 {
		t.Fatalf("expected %v", p)
	}
}
