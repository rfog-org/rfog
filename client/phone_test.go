package client

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"rfog/engine"
	"rfog/render"
)

func phoneMatch(t *testing.T, w, h int) (*App, *matchScreen) {
	t.Helper()
	a := testApp(t)
	pinMap(a, "relay")
	deliver(a, tea.WindowSizeMsg{Width: w, Height: h})
	lm, err := newLocalMatch(a.c, engine.Setup{ID: "p", Mode: "1v1", Seed: 7, TimeControl: "bots",
		Players: []engine.SetupPlayer{{Name: "me", Hero: "hask"}, {Name: "bot", Hero: "tally"}}}, [2]bool{false, true}, "normal")
	if err != nil {
		t.Fatal(err)
	}
	a.screen = newMatchScreen(a, lm)
	return a, a.screen.(*matchScreen)
}

// The layout is chosen for the biggest tiles: a phone-shaped terminal
// (tall, ~86 wide) stacks the board over the panels at 4x2; a wide one
// keeps the sidebar, also at 4x2.
func TestLayoutGoesForTheBiggestTiles(t *testing.T) {
	for _, c := range []struct {
		w, h    int
		stacked bool
		tile    int
	}{
		{86, 60, true, 10},   // phone in portrait at a small font: turned, 5x2
		{86, 90, true, 18},   // a taller phone: turned, big squares
		{160, 45, false, 18}, // desktop: 6x3 beside the sidebar
		{100, 30, true, 4},   // a short window: 4x1 stacked beats 3x1 beside
		{45, 28, true, 2},    // phone at a big font
		{79, 68, true, 10},   // what the page picks on a phone: turned, 5x2
	} {
		a, m := phoneMatch(t, c.w, c.h)
		a.View()
		if got := m.stacked(a); got != c.stacked {
			t.Errorf("%dx%d: stacked=%v, want %v", c.w, c.h, got, c.stacked)
		}
		if got := m.scene.CellW * m.scene.CellH; got != c.tile {
			t.Errorf("%dx%d: tile %d, want %d", c.w, c.h, got, c.tile)
		}
		checkFrame(t, a)
	}
}

// The portrait shows whatever unit the cursor is on, and static (no
// signal) on an empty tile.
func TestPortraitFollowsTheCursor(t *testing.T) {
	a, m := phoneMatch(t, 86, 60)
	cmd := m.vs.Unit(m.vs.Player(0).Commander)
	m.cur = cmd.Pos
	if !strings.Contains(stripEscapes(a.View()), "hask") {
		t.Fatal("cursor on the commander: its portrait should be labelled hask")
	}
	m.cur = engine.Pos{X: 10, Y: 1}
	if m.vs.UnitAt(m.cur) != nil {
		t.Fatal("test tile is not empty")
	}
	if !strings.Contains(stripEscapes(a.View()), "no signal") {
		t.Fatal("cursor on an empty tile: the portrait should be static")
	}
}

// g opens the log on its own screen; g closes it.
func TestLogScreen(t *testing.T) {
	a, m := phoneMatch(t, 86, 60)
	m.log = append(m.log, "T01 something happened")
	press(a, "g")
	if !m.logOpen || !strings.Contains(stripEscapes(a.View()), "something happened") {
		t.Fatal("g should open the log screen")
	}
	press(a, "g")
	if m.logOpen {
		t.Fatal("g should close the log screen")
	}
}

func mouseAt(a *App, x, y int, action tea.MouseAction) {
	deliver(a, tea.MouseMsg{X: x, Y: y, Action: action, Button: tea.MouseButtonLeft})
}

// Chess-style: press on a unit, drag, let go on a tile: a move order.
func TestDragAUnitToMove(t *testing.T) {
	a, m := phoneMatch(t, 90, 60)
	a.View()
	cmd := m.vs.Unit(m.vs.Player(0).Commander)
	ux, uy := m.cellAt(cmd.Pos)
	mouseAt(a, ux, uy, tea.MouseActionPress)
	if m.drag == nil {
		t.Fatal("pressing on your commander should pick it up")
	}
	dest := engine.Pos{X: cmd.Pos.X + 2, Y: cmd.Pos.Y}
	dx, dy := m.cellAt(dest)
	mouseAt(a, dx, dy, tea.MouseActionMotion)
	if m.mode != "move" {
		t.Fatalf("dragging should show the reachable tiles (mode %q)", m.mode)
	}
	mouseAt(a, dx, dy, tea.MouseActionRelease)
	if len(m.orders) != 1 || m.orders[0].Action != engine.ActMove || m.orders[0].Path[len(m.orders[0].Path)-1] != dest {
		t.Fatalf("drop should give a move to %v: %+v msg %q", dest, m.orders, m.msg)
	}
}

// Lichess-style: a tap on your unit shows its moves and targets on the
// board, nothing covers the board, and a tap on a lit tile moves it. The
// action area under the panels holds the commands as buttons.
func TestTapShowsMovesLikeAChessBoard(t *testing.T) {
	a, m := phoneMatch(t, 90, 70)
	a.View()
	cmd := m.vs.Unit(m.vs.Player(0).Commander)
	ux, uy := m.cellAt(cmd.Pos)
	mouseAt(a, ux, uy, tea.MouseActionPress)
	mouseAt(a, ux, uy, tea.MouseActionRelease)
	if m.mode != "go" || len(m.reach) == 0 {
		t.Fatalf("tap on your unit should show its moves (mode %q)", m.mode)
	}
	var dest engine.Pos
	for p := range m.reach {
		dest = p
		break
	}
	a.View()
	dx, dy := m.cellAt(dest)
	mouseAt(a, dx, dy, tea.MouseActionPress)
	if len(m.orders) != 1 || m.orders[0].Action != engine.ActMove {
		t.Fatalf("tap on a lit tile should move there: %+v msg %q", m.orders, m.msg)
	}
	// Tapping it again puts it down.
	a.View()
	mouseAt(a, ux, uy, tea.MouseActionPress)
	mouseAt(a, ux, uy, tea.MouseActionRelease)
	mouseAt(a, ux, uy, tea.MouseActionPress)
	mouseAt(a, ux, uy, tea.MouseActionRelease)
	if m.mode != "" {
		t.Fatalf("a second tap should put the unit down (mode %q)", m.mode)
	}
}

// The action area's buttons act: Act opens the abilities, End turn commits.
func TestActionAreaButtons(t *testing.T) {
	a, m := phoneMatch(t, 90, 70)
	press(a, "tab")
	a.View()
	labels := map[string]bool{}
	for _, it := range m.actions {
		labels[it.Label] = true
	}
	for _, want := range []string{"Move", "Act", "Hold", "End turn"} {
		if !labels[want] {
			t.Fatalf("action area should have %q: %+v", want, m.actions)
		}
	}
	click := func(label string) {
		t.Helper()
		for i, it := range m.actions {
			if it.Label == label {
				for _, r := range a.rows {
					if r.id == -(i + 1) {
						mouseAt(a, r.x0+1, r.y, tea.MouseActionPress)
						a.View()
						return
					}
				}
			}
		}
		t.Fatalf("no button %q in %+v", label, m.actions)
	}
	click("Act")
	hasAttack := false
	for _, it := range m.actions {
		hasAttack = hasAttack || it.Label == "Attack"
	}
	if m.popup != "act" || !hasAttack {
		t.Fatal("Act should open the attack and abilities")
	}
	click("Back")
	click("End turn")
	if m.phase == "orders" && m.vs.Match.Turn == 1 {
		t.Fatalf("End turn should commit (phase %q)", m.phase)
	}
}

// Like a chess board: on a tall screen the board is turned so its long
// side runs up, with the player's own side at the bottom, and the keys
// move the way the screen shows. The other team sees it the other way up.
func TestBoardTurnsToFaceThePlayer(t *testing.T) {
	a, m := phoneMatch(t, 86, 90)
	a.View()
	if m.orient != render.OrientCCW {
		t.Fatalf("team 0 on a tall screen: orient %v, want CCW", m.orient)
	}
	cmd := m.vs.Unit(m.vs.Player(0).Commander)
	_, y := m.cellAt(cmd.Pos)
	if y < a.h/3 {
		t.Fatalf("your units should be at the bottom of the board, commander drawn at row %d of %d", y, a.h)
	}
	m.cur = cmd.Pos
	press(a, "k") // up the screen: toward the enemy, board x+1
	if m.cur.X != cmd.Pos.X+1 || m.cur.Y != cmd.Pos.Y {
		t.Fatalf("up from %v went to %v", cmd.Pos, m.cur)
	}
	// A tap lands on the tile drawn there.
	x, y := m.cellAt(engine.Pos{X: 5, Y: 3})
	if p, ok := m.tileAt(x, y); !ok || p != (engine.Pos{X: 5, Y: 3}) {
		t.Fatalf("tap at the drawn tile maps to %v", p)
	}
	if render.ForTeam(1, true) != render.OrientCW || render.ForTeam(1, false) != render.OrientHalf {
		t.Fatal("team 1 should see the board the other way up")
	}
}

// The 1v1 map on a phone: fourteen by ten, turned, 8x4 squares with room
// for the pieces' pictures (about what the page picks at a small font).
func TestPhoneGetsBigSquaresOnThe1v1Map(t *testing.T) {
	a := testApp(t)
	deliver(a, tea.WindowSizeMsg{Width: 90, Height: 78})
	lm, err := newLocalMatch(a.c, engine.Setup{ID: "p", Mode: "1v1", Seed: 7, TimeControl: "bots",
		Players: []engine.SetupPlayer{{Name: "me", Hero: "hask"}, {Name: "bot", Hero: "tally"}}}, [2]bool{false, true}, "normal")
	if err != nil {
		t.Fatal(err)
	}
	a.screen = newMatchScreen(a, lm)
	m := a.screen.(*matchScreen)
	a.View()
	if !m.orient.Turned() || m.cellW < 8 || m.cellH < 4 {
		t.Fatalf("orient %v, tiles %dx%d: want turned at 8x4 or bigger", m.orient, m.cellW, m.cellH)
	}
	checkFrame(t, a)
}

// Several planned moves stay readable: each destination shows who is
// going there, and only the selected unit's path is drawn.
func TestPlannedMovesShowTheirDestinations(t *testing.T) {
	a, m := phoneMatch(t, 90, 70)
	a.View()
	var dests []engine.Pos
	for _, u := range m.myUnits()[:2] {
		m.sel, m.cur = u.ID, u.Pos
		m.startMove(a)
		for p := range m.reach {
			if p != u.Pos && !contains2(dests, p) {
				m.cur = p
				break
			}
		}
		dests = append(dests, m.cur)
		m.confirm(a)
	}
	if len(m.orders) != 2 {
		t.Fatalf("orders: %+v", m.orders)
	}
	m.sel = m.orders[1].UnitID
	m.mode = ""
	m.decorate(a)
	for i, d := range dests {
		if u, ok := m.scene.Planned[d]; !ok || u.ID != m.orders[i].UnitID {
			t.Fatalf("destination %v should show unit %d: %+v", d, m.orders[i].UnitID, m.scene.Planned)
		}
	}
	for _, p := range m.orders[0].Path {
		if m.scene.Path[p] && !contains2(m.orders[1].Path, p) {
			t.Fatalf("the unselected unit's path is drawn at %v", p)
		}
	}
	for _, p := range m.orders[1].Path {
		if !m.scene.Path[p] {
			t.Fatalf("the selected unit's path is missing at %v", p)
		}
	}
	checkFrame(t, a)
}

func contains2(ps []engine.Pos, p engine.Pos) bool {
	for _, q := range ps {
		if q == p {
			return true
		}
	}
	return false
}

// The settings screen answers taps: a tap selects a row, a second tap on
// it changes it (here the bot level, from normal to hard).
func TestSettingsTakeTaps(t *testing.T) {
	a := testApp(t)
	a.nosave = true
	deliver(a, tea.WindowSizeMsg{Width: 60, Height: 40})
	s := &settingsScreen{}
	a.screen = s
	a.View()
	var row clickRow
	for _, r := range a.rows {
		if r.id == 5 {
			row = r
		}
	}
	mouseAt(a, 10, row.y, tea.MouseActionPress)
	if s.sel != 5 {
		t.Fatalf("first tap should select the bot level row, sel %d", s.sel)
	}
	mouseAt(a, 10, row.y, tea.MouseActionPress)
	if a.set.BotLevel != "hard" {
		t.Fatalf("second tap should change the bot level: %q", a.set.BotLevel)
	}
}

// Chess-like for the commander's two orders: tap it, tap a square, and it
// stays picked up at where it is going, showing what it can hit from there.
func TestCommanderMovesThenStaysUp(t *testing.T) {
	a, m := phoneMatch(t, 90, 78)
	a.View()
	cmd := m.vs.Unit(m.vs.Player(0).Commander)
	ux, uy := m.cellAt(cmd.Pos)
	mouseAt(a, ux, uy, tea.MouseActionPress)
	mouseAt(a, ux, uy, tea.MouseActionRelease)
	if m.mode != "go" {
		t.Fatalf("tap: mode %q", m.mode)
	}
	var dest engine.Pos
	for p := range m.reach {
		dest = p
		break
	}
	a.View()
	dx, dy := m.cellAt(dest)
	mouseAt(a, dx, dy, tea.MouseActionPress)
	if m.ordersFor(cmd.ID) != 1 || m.sel != cmd.ID {
		t.Fatalf("after the move: %d orders, selected %d (want the commander)", m.ordersFor(cmd.ID), m.sel)
	}
	// Up with only targets if it can hit something from there, else down.
	if len(m.reach) != 0 || (m.mode == "go") != (len(m.targets) > 0) {
		t.Fatalf("after the move: mode %q, %d reachable, %d targets", m.mode, len(m.reach), len(m.targets))
	}
	if m.msg != "" {
		t.Fatalf("no complaint after a move: %q", m.msg)
	}
}
