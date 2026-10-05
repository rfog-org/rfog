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
