package render

import "testing"

// figureDark is the figures' dark mass (assets/units.go, 'd'): what a
// square has to stand out from for a unit to read.
const figureDark = "#161b21"

// Every board must show the figures, keep its checker, set walls apart,
// and keep its board colours distinct in a 256-colour terminal too.
func TestBoardsReadable(t *testing.T) {
	for _, b := range Boards {
		if c := Contrast(b.Dark, figureDark); c < 2.2 {
			t.Errorf("%s: dark squares %.2f:1 against the figures, want >= 2.2", b.Name, c)
		}
		if c := Contrast(b.Light, b.Dark); c < 1.25 || c > 2 {
			t.Errorf("%s: light/dark %.2f:1, want 1.25 to 2", b.Name, c)
		}
		if c := Contrast(b.Dark, b.Wall); c < 2 {
			t.Errorf("%s: walls %.2f:1 against dark squares, want >= 2", b.Name, c)
		}
		for _, th := range []Theme{GetTheme(b.Name), Theme256(GetTheme(b.Name))} {
			named := map[string]string{"light": th.Light, "dark": th.Board, "fog": th.Fog, "wall": th.Wall,
				"wall top": th.WallTop, "objective": th.ObjBg, "held A": th.ObjA, "held B": th.ObjB, "contested": th.ObjC}
			seen := map[string]string{}
			for name, c := range named {
				if other, ok := seen[c]; ok {
					t.Errorf("%s (%s): %s and %s are both %s", b.Name, th.Light, name, other, c)
				}
				seen[c] = name
			}
		}
	}
}

// Every board has a terminal theme and the names line up.
func TestBoardThemes(t *testing.T) {
	names := ThemeNames()
	for i, b := range Boards {
		if names[i] != b.Name {
			t.Fatalf("theme %d is %q, want board %q", i, names[i], b.Name)
		}
	}
	if names[len(names)-1] != "mono" {
		t.Fatalf("mono should be last: %v", names)
	}
	if GetTheme("no such theme").Name != "fibre" {
		t.Fatal("unknown themes should fall back to fibre")
	}
}
