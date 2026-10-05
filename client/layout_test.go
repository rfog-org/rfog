package client

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"rfog/engine"
)

// phoneShapes are real device sizes the web client reports, plus the
// extremes: a small phone in portrait, a large one, both in landscape,
// a tablet, and the floor the client draws on.
var phoneShapes = [][2]int{
	{40, 18}, {44, 22}, {46, 24}, {49, 35}, {52, 40}, // portrait, small to large
	{90, 18}, {100, 20}, {110, 24}, {120, 22}, // landscape
	{80, 24}, {160, 48}, // a terminal, a tablet
}

// TestEveryShapePlays orders a turn at every shape, by key and by tap,
// and checks the frame fits and the panels show what they must.
func TestEveryShapePlays(t *testing.T) {
	for _, sz := range phoneShapes {
		a := testApp(t)
		a.w, a.h = sz[0], sz[1]
		a.applyTier()
		lm, err := newLocalMatch(a.c, engine.Setup{ID: "t", Mode: "1v1", Seed: 12, TimeControl: "bots",
			Players: []engine.SetupPlayer{{Name: "me", Hero: "tally"}, {Name: "bot", Hero: "mott"}}}, [2]bool{false, true}, "normal")
		if err != nil {
			t.Fatal(err)
		}
		a.screen = newMatchScreen(a, lm)
		m := a.screen.(*matchScreen)
		u := m.myUnits()[0]
		m.sel, m.cur = u.ID, u.Pos
		out := stripEscapes(a.View())
		checkFrame(t, a)
		// The unit and its HP are always visible somewhere.
		if !strings.Contains(out, u.Name) && !strings.Contains(out, strings.ToUpper(u.Name)) {
			t.Fatalf("%v: the selected unit is not named:\n%s", sz, out)
		}
		if !strings.Contains(out, "HP") {
			t.Fatalf("%v: no HP:\n%s", sz, out)
		}
		// Taps land on tiles at every shape.
		a.View()
		dest := engine.Pos{X: u.Pos.X + 1, Y: u.Pos.Y}
		x, y := m.cellAt(dest)
		deliver(a, tea.MouseMsg{X: x, Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
		if m.cur != dest {
			t.Fatalf("%v: tap at %d,%d put the cursor at %v, want %v", sz, x, y, m.cur, dest)
		}
		checkFrame(t, a)
	}
}

// TestPortraitVisibleWhereItFits: every shape with room shows the
// commander's portrait, so a player always sees who they command.
func TestPortraitVisibleWhereItFits(t *testing.T) {
	for _, sz := range phoneShapes {
		if sz[1] < 22 { // the shortest shapes spend their rows on the board
			continue
		}
		a := testApp(t)
		a.w, a.h = sz[0], sz[1]
		a.set.Tier = "t1"
		a.applyTier()
		lm, _ := newLocalMatch(a.c, engine.Setup{ID: "t", Mode: "1v1", Seed: 3, TimeControl: "bots",
			Players: []engine.SetupPlayer{{Name: "me", Hero: "wren"}, {Name: "bot", Hero: "nul"}}}, [2]bool{false, true}, "normal")
		a.screen = newMatchScreen(a, lm)
		m := a.screen.(*matchScreen)
		m.sel, m.cur = m.myUnits()[0].ID, m.myUnits()[0].Pos
		if !strings.Contains(a.View(), "▀") {
			t.Fatalf("%v: no portrait:\n%s", sz, stripEscapes(a.View()))
		}
	}
}

// TestMenusAreTappableEverywhere: every menu row is a target at least
// one row tall, and opening an item takes two taps on the same row.
func TestMenusAreTappableEverywhere(t *testing.T) {
	for _, sz := range phoneShapes {
		a := testApp(t)
		a.w, a.h = sz[0], sz[1]
		a.applyTier()
		a.screen = newMenuScreen()
		a.View()
		items := a.screen.(*menuScreen).items
		seen := map[int]bool{}
		for _, r := range a.rows {
			seen[r.id] = true
		}
		for i := range items {
			if !seen[i] {
				t.Fatalf("%v: menu item %d (%s) has no tap target", sz, i, items[i].label)
			}
		}
		// Two taps on the roster row open it.
		for _, r := range a.rows {
			if items[r.id].label == "roster" {
				x := sz[0] / 2
				if r.x1 < 1<<30 { // part of a row (the home grid's bottom row)
					x = (r.x0 + r.x1) / 2
				}
				deliver(a, tea.MouseMsg{X: x, Y: r.y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
				deliver(a, tea.MouseMsg{X: x, Y: r.y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
				break
			}
		}
		if _, ok := a.screen.(*rosterScreen); !ok {
			t.Fatalf("%v: taps did not open the roster (got %T)", sz, a.screen)
		}
	}
}
