package render

import (
	"io"
	"strings"
	"testing"

	"rfog/engine"
)

// A tall tile shows a unit's health in the row under it: full bar at full
// health, part of one when hurt, and nothing under an empty tile.
func TestHealthRowOnTallTiles(t *testing.T) {
	b := engine.Board{W: 3, H: 1, Tiles: make([]engine.Tile, 3)}
	sc := Scene{Board: &b, CellW: 4, CellH: 2, Units: []SceneUnit{
		{ID: 1, Team: 0, Kind: "lineman", Pos: engine.Pos{X: 0}, HP: 6, MaxHP: 6},
		{ID: 2, Team: 1, Kind: "lineman", Pos: engine.Pos{X: 1}, HP: 2, MaxHP: 6},
	}}
	st := NewStyles(themes["mono"], T0, io.Discard, true)
	lines := sc.RenderBoard(&st, GlyphsFor(T0), Viewport{W: 3, H: 1})
	if len(lines) != 2 {
		t.Fatalf("want 2 rows for one row of tall tiles, got %d", len(lines))
	}
	under := lines[1]
	if !strings.HasPrefix(under, "=== ") {
		t.Fatalf("full-health unit: want a full bar, got %q", under)
	}
	if got := under[4:8]; got != "=-- " {
		t.Fatalf("unit at 2/6: want one of three filled, got %q", got)
	}
	if strings.ContainsAny(under[8:], "=-") {
		t.Fatalf("empty tile has a bar: %q", under[8:])
	}
}

// The selected unit breathes (pointers on odd ambient ticks), last turn's
// steps show under the floor, and a scar marks where a unit fell.
func TestLingeringMotion(t *testing.T) {
	b := engine.Board{W: 3, H: 1, Tiles: make([]engine.Tile, 3)}
	sc := Scene{Board: &b, CellW: 4, CellH: 2, Selected: 1, Ambient: 1,
		Units: []SceneUnit{{ID: 1, Team: 0, Kind: "lineman", Pos: engine.Pos{X: 0}, HP: 6, MaxHP: 6}},
		Trail: map[engine.Pos]bool{{X: 1}: true},
		Scars: map[engine.Pos]bool{{X: 2}: true}}
	st := NewStyles(themes["mono"], T0, io.Discard, true)
	g := GlyphsFor(T0)
	lines := sc.RenderBoard(&st, g, Viewport{W: 3, H: 1})
	if !strings.HasPrefix(lines[0], g.PulseL) || !strings.Contains(lines[0][:4], g.PulseR) {
		t.Fatalf("selected unit on an odd tick should have pointers: %q", lines[0][:4])
	}
	if got := lines[1][4:8]; !strings.Contains(got, g.Step+g.Step) {
		t.Fatalf("trail tile's floor should show steps: %q", got)
	}
	if got := lines[0][8:12]; !strings.Contains(got, g.Dust) {
		t.Fatalf("scar tile should show dust: %q", got)
	}
	sc.Ambient = 2
	if lines := sc.RenderBoard(&st, g, Viewport{W: 3, H: 1}); strings.HasPrefix(lines[0], g.PulseL) {
		t.Fatal("on an even tick the pointers are gone (the breath out)")
	}
}

// A cable under a lit square (a target, an ability's area) is drawn in
// that square's style, not on the terminal's own background (it showed as
// a black bar across the square).
func TestCableInALitSquare(t *testing.T) {
	st := NewStyles(BoardTheme(Boards[0]), T2, io.Discard, false)
	var sc Scene
	for _, across := range []bool{true, false} {
		r := &runBuilder{}
		sc.addFibre(r, sTarget, &st, "      ", fibreSeg{h: across, v: !across, holder: 0}, across)
		lit := sc.wrapper(sTarget, &st).pre
		if got := r.String(); lit == "" || strings.Count(got, lit) != strings.Count(got, "\x1b[")/2 {
			t.Fatalf("across=%v: every run should open with the target style %q: %q", across, lit, got)
		}
	}
}

// A unit's reach is a dot in each square it can reach, on the square's
// own colours: a fill over most of an 8x8 board hid the board. On every
// board, at both colour depths, the dot stands out from both squares.
func TestReachIsDots(t *testing.T) {
	b := engine.Board{W: 4, H: 1, Tiles: make([]engine.Tile, 4)}
	for _, bd := range Boards {
		for _, th := range []Theme{BoardTheme(bd), Theme256(BoardTheme(bd))} {
			for _, sq := range []string{th.Light, th.Board} {
				if c := Contrast(th.Wall, sq); c < 2 {
					t.Errorf("%s: reach dot %s on %s is %.2f:1", bd.Name, th.Wall, sq, c)
				}
			}
			st := NewStyles(th, T2, io.Discard, false)
			sc := Scene{Board: &b, CellW: 8, CellH: 4, Highlight: map[engine.Pos]bool{{X: 1}: true, {X: 2}: true}}
			out := strings.Join(sc.RenderBoard(&st, GlyphsFor(T2), Viewport{W: 4, H: 1}), "\n")
			if n := strings.Count(out, GlyphsFor(T2).Reach); n != 2 {
				t.Fatalf("%s: %d reach dots, want 2", bd.Name, n)
			}
			if strings.Contains(out, sc.wrapper(sTarget, &st).pre) {
				t.Fatalf("%s: reach painted as a lit square", bd.Name)
			}
		}
	}
}
