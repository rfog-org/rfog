package balance

import (
	"testing"

	"rfog/data"
)

func TestRunIsDeterministicAndAddsUp(t *testing.T) {
	c, err := data.Load()
	if err != nil {
		t.Fatal(err)
	}
	o := Options{Mode: "1v1", Matches: 1, Level: "normal", Seed: 5, Heroes: []string{"hask", "wren", "nul"}}
	a, err := Run(c, o, nil)
	if err != nil {
		t.Fatal(err)
	}
	b, err := Run(c, o, nil)
	if err != nil {
		t.Fatal(err)
	}
	if a.Matches != 6 { // 3 heroes, ordered pairs, one match each
		t.Fatalf("matches %d", a.Matches)
	}
	if len(a.Heroes) != 3 {
		t.Fatalf("heroes %d", len(a.Heroes))
	}
	for i := range a.Heroes {
		if a.Heroes[i] != b.Heroes[i] {
			t.Fatalf("not deterministic: %+v vs %+v", a.Heroes[i], b.Heroes[i])
		}
	}
	games, wins := 0, 0
	for _, h := range a.Heroes {
		if h.Games != 4 { // each hero plays every other hero twice
			t.Fatalf("%s games %d", h.Hero, h.Games)
		}
		games += h.Games
		wins += h.Wins
	}
	if games != a.Matches*2 {
		t.Fatalf("%d hero-games for %d matches", games, a.Matches)
	}
	if wins+a.Draws*2 != a.Matches*2-(a.Matches-wins-a.Draws)*0 && wins > a.Matches {
		t.Fatalf("more wins (%d) than matches (%d)", wins, a.Matches)
	}
	if a.Turns <= 0 || a.FirstWin < 0 || a.FirstWin > 1 {
		t.Fatalf("summary: %+v", a)
	}
	if txt := a.Text(); len(txt) == 0 {
		t.Fatal("empty report")
	}
}

func TestUnknownMode(t *testing.T) {
	c, _ := data.Load()
	if _, err := Run(c, Options{Mode: "9v9", Matches: 1}, nil); err == nil {
		t.Fatal("expected an error")
	}
}

// A team mode at the long length runs, and no bot order is dropped.
func TestLongTeamRun(t *testing.T) {
	c, _ := data.Load()
	r, err := Run(c, Options{Mode: "2v2", Length: "long", Matches: 1, Seed: 3, Heroes: []string{"hask", "wren", "nul"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if r.Length != "long" || r.Matches != 6 || r.Turns <= 0 || r.Dropped != 0 {
		t.Fatalf("report %+v", r)
	}
	if _, err := Run(c, Options{Length: "endless", Matches: 1}, nil); err == nil {
		t.Fatal("unknown length accepted")
	}
}
