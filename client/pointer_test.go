package client

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"rfog/engine"
)

func click(a *App, x, y int) {
	deliver(a, tea.MouseMsg{X: x, Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
}

// screenCell finds a tile's screen position from the match screen's own
// mapping (the same one clicks go through).
func cellOf(m *matchScreen, p engine.Pos) (int, int) { return m.cellAt(p) }

// TestPointerPlaysAMatch: a whole turn ordered with taps only — no keys
// except the ones the button bar sends, which are the same key messages.
func TestPointerPlaysAMatch(t *testing.T) {
	for _, sz := range [][2]int{{80, 24}, {45, 28}, {85, 59}} {
		a := testApp(t)
		pinMap(a, "relay")
		a.w, a.h = sz[0], sz[1]
		a.applyTier()
		lm, err := newLocalMatch(a.c, engine.Setup{ID: "t", Mode: "1v1", Seed: 4, TimeControl: "bots",
			Players: []engine.SetupPlayer{{Name: "me", Hero: "hask"}, {Name: "bot", Hero: "wren"}}}, [2]bool{false, true}, "normal")
		if err != nil {
			t.Fatal(err)
		}
		a.screen = newMatchScreen(a, lm)
		m := a.screen.(*matchScreen)
		a.View() // a frame must be drawn before its click targets exist

		// Tap one of my units: it gets selected and the cursor goes there.
		u := m.myUnits()[1]
		x, y := cellOf(m, u.Pos)
		click(a, x, y)
		if m.sel != u.ID || m.cur != u.Pos {
			t.Fatalf("%v: tap selected %d at %v, want %d at %v", sz, m.sel, m.cur, u.ID, u.Pos)
		}
		// Ask to move, tap a reachable tile, tap it again to confirm.
		m.startMove(a)
		if m.mode != "move" {
			t.Fatalf("%v: move mode %q", sz, m.mode)
		}
		var dest engine.Pos
		for p := range m.reach {
			if p != u.Pos && (dest == engine.Pos{} || p.X > dest.X) {
				dest = p
			}
		}
		a.View()
		x, y = cellOf(m, dest)
		click(a, x, y)
		if m.cur != dest {
			t.Fatalf("%v: cursor %v after tapping %v", sz, m.cur, dest)
		}
		click(a, x, y)
		if len(m.orders) != 1 || m.orders[0].UnitID != u.ID {
			t.Fatalf("%v: orders after the second tap: %+v", sz, m.orders)
		}
		// A tap outside the board changes nothing.
		before := m.cur
		click(a, 0, 0)
		if m.cur != before {
			t.Fatalf("%v: tap off the board moved the cursor", sz)
		}
		// Tapping a unit in the order list selects it.
		a.View()
		other := m.myUnits()[3]
		found := false
		for _, r := range a.rows {
			if r.id == other.ID {
				click(a, r.x0+2, r.y)
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("%v: no order row for unit %d", sz, other.ID)
		}
		if m.sel != other.ID {
			t.Fatalf("%v: order row tap selected %d, want %d", sz, m.sel, other.ID)
		}
		checkFrame(t, a)
	}
}

// TestPointerScrollsAndMenus: the wheel scrolls a board bigger than the
// viewport, and menu rows answer to taps.
func TestPointerScrollsAndMenus(t *testing.T) {
	a := testApp(t)
	a.w, a.h = 60, 22 // smaller than the 20x12 board plus labels
	a.applyTier()
	lm, err := newLocalMatch(a.c, engine.Setup{ID: "t", Mode: "1v1", Seed: 4, TimeControl: "bots",
		Players: []engine.SetupPlayer{{Name: "me", Hero: "nul"}, {Name: "bot", Hero: "mott"}}}, [2]bool{false, true}, "normal")
	if err != nil {
		t.Fatal(err)
	}
	a.screen = newMatchScreen(a, lm)
	m := a.screen.(*matchScreen)
	a.View()
	if m.vp.W >= m.scene.Board.W {
		t.Skip("board fits; nothing to scroll")
	}
	before := m.vp.OX
	deliver(a, tea.MouseMsg{X: 10, Y: 5, Action: tea.MouseActionPress, Button: tea.MouseButtonWheelRight})
	if m.vp.OX == before {
		t.Fatal("wheel did not scroll")
	}
	checkFrame(t, a)

	// Menu: a tap highlights, a second tap on the same row opens it.
	a2 := testApp(t)
	a2.screen = newMenuScreen()
	a2.View()
	var rosterRow, rosterID int
	for _, r := range a2.rows {
		if strings.Contains(a2.screen.(*menuScreen).items[r.id].label, "roster") {
			rosterRow, rosterID = r.y, r.id
		}
	}
	click(a2, 4, rosterRow)
	if a2.screen.(*menuScreen).sel != rosterID {
		t.Fatalf("menu tap selected %d, want %d", a2.screen.(*menuScreen).sel, rosterID)
	}
	click(a2, 4, rosterRow)
	if _, ok := a2.screen.(*rosterScreen); !ok {
		t.Fatalf("second tap opened %T", a2.screen)
	}
	a2.View()
	// And the roster's own rows answer to taps.
	if len(a2.rows) < 2 {
		t.Fatal("roster has no click targets")
	}
	click(a2, 4, a2.rows[1].y)
	if a2.screen.(*rosterScreen).sel != a2.rows[1].id {
		t.Fatal("roster tap did not select")
	}
}

// TestPortraitCardOnSmallScreens: a phone-sized panel must still show who
// you are commanding — the portrait card with the unit's stats.
func TestPortraitCardOnSmallScreens(t *testing.T) {
	for _, sz := range [][2]int{{46, 24}, {46, 30}, {50, 26}} {
		a := testApp(t)
		a.w, a.h = sz[0], sz[1]
		a.set.Tier = "t1"
		a.applyTier()
		lm, err := newLocalMatch(a.c, engine.Setup{ID: "t", Mode: "1v1", Seed: 7, TimeControl: "bots",
			Players: []engine.SetupPlayer{{Name: "me", Hero: "hask"}, {Name: "bot", Hero: "wren"}}}, [2]bool{false, true}, "normal")
		if err != nil {
			t.Fatal(err)
		}
		a.screen = newMatchScreen(a, lm)
		m := a.screen.(*matchScreen)
		u := m.myUnits()[0]
		m.sel, m.cur = u.ID, u.Pos
		out := stripEscapes(a.View())
		if !strings.Contains(out, "HASK") || !strings.Contains(out, "HP ") {
			t.Fatalf("%v: no unit card:\n%s", sz, out)
		}
		if !strings.Contains(out, "▀") { // the portrait's half blocks
			t.Fatalf("%v: no portrait beside the stats:\n%s", sz, out)
		}
		checkFrame(t, a)
	}
}

// TestMouseHoverThenSingleClick: with a real mouse the row under the
// pointer highlights, so one click acts. A touch screen sends no motion,
// so it still takes two taps — both paths from the same code.
func TestMouseHoverThenSingleClick(t *testing.T) {
	a := testApp(t)
	a.w, a.h = 100, 30
	a.applyTier()
	a.screen = newMenuScreen()
	a.View()
	var row, id int
	for _, r := range a.rows {
		if a.screen.(*menuScreen).items[r.id].label == "replays" {
			row, id = r.y, r.id
		}
	}
	// Move over the row: it highlights, nothing opens.
	deliver(a, tea.MouseMsg{X: 50, Y: row, Action: tea.MouseActionMotion})
	m, ok := a.screen.(*menuScreen)
	if !ok || m.sel != id {
		t.Fatalf("hover selected %v (screen %T)", m.sel, a.screen)
	}
	// One click opens it.
	click(a, 50, row)
	if _, ok := a.screen.(*replayBrowser); !ok {
		t.Fatalf("single click after hover opened %T", a.screen)
	}
}

// TestWideTerminalCentresTheBoard: a board smaller than the terminal is
// drawn at its own size and centred, not stretched into empty columns.
func TestWideTerminalCentresTheBoard(t *testing.T) {
	a := testApp(t)
	a.w, a.h = 200, 60
	a.set.Tier = "t1" // the default test env probes T0, which draws ASCII frames
	a.applyTier()
	lm, err := newLocalMatch(a.c, engine.Setup{ID: "t", Mode: "1v1", Seed: 2, TimeControl: "bots",
		Players: []engine.SetupPlayer{{Name: "me", Hero: "hask"}, {Name: "bot", Hero: "wren"}}}, [2]bool{false, true}, "normal")
	if err != nil {
		t.Fatal(err)
	}
	a.screen = newMatchScreen(a, lm)
	m := a.screen.(*matchScreen)
	out := strings.Split(stripEscapes(a.View()), "\n")
	var frame string
	for _, l := range out {
		if strings.Contains(l, "╭") || strings.Contains(l, "+-") {
			frame = l
			break
		}
	}
	if frame == "" {
		t.Fatal("no board frame")
	}
	// Widths in printed columns: box drawing is multi-byte.
	left := lipglossWidth(frame) - lipglossWidth(strings.TrimLeft(frame, " "))
	right := a.w - lipglossWidth(strings.TrimRight(frame, " "))
	if left < 10 {
		t.Fatalf("board is not centred: %d columns of margin", left)
	}
	if d := left - right; d > 2 || d < -2 {
		t.Fatalf("margins differ: %d left, %d right", left, right)
	}
	// The drawn block is the board plus the sidebar, not the terminal.
	if w := lipglossWidth(strings.TrimSpace(frame)); w > m.scene.Board.W*m.scene.CellW+8+sidebarW {
		t.Fatalf("block is %d wide for a %d-tile board", w, m.scene.Board.W)
	}
	// Clicks still land after the shift.
	a.View()
	u := m.myUnits()[2]
	ux, uy := cellOf(m, u.Pos)
	click(a, ux, uy)
	if m.sel != u.ID {
		t.Fatalf("click after centring selected %d, want %d", m.sel, u.ID)
	}
	checkFrame(t, a)
}
