package client

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"rfog/data"
	"rfog/engine"
	"rfog/render"
)

// pinMap makes 1v1 play on a given map: tests of layout and pointing are
// about the rules for placing a board, and were written against relay.
func pinMap(a *App, id string) {
	m := a.c.Rules.Modes["1v1"]
	m.Map = id
	a.c.Rules.Modes["1v1"] = m
}

func testApp(t *testing.T) *App {
	t.Helper()
	c, err := data.Load()
	if err != nil {
		t.Fatal(err)
	}
	set := DefaultSettings()
	set.Tier, set.AnimMs = "t0", 1
	a := New(Options{Content: c, Settings: set, Env: render.Env{Term: "xterm"}})
	a.w, a.h = 80, 24
	a.applyTier()
	return a
}

func press(a *App, keys ...string) {
	for _, k := range keys {
		var msg tea.KeyMsg
		switch k {
		case "enter":
			msg = tea.KeyMsg{Type: tea.KeyEnter}
		case "esc":
			msg = tea.KeyMsg{Type: tea.KeyEsc}
		case "tab":
			msg = tea.KeyMsg{Type: tea.KeyTab}
		case " ":
			msg = tea.KeyMsg{Type: tea.KeySpace}
		default:
			msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
		}
		deliver(a, msg)
	}
}

// deliver feeds a message and synchronously runs any commands it produces,
// except timer ticks (the tests drive those with ticks()).
func deliver(a *App, msg tea.Msg) {
	_, cmd := a.Update(msg)
	runCmd(a, cmd)
}

func runCmd(a *App, cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	msg := cmd()
	switch m := msg.(type) {
	case nil:
		return
	case tea.BatchMsg:
		for _, c := range m {
			runCmd(a, c)
		}
	case tickMsg:
		return
	default:
		deliver(a, msg)
	}
}

func ticks(a *App, n int) {
	for i := 0; i < n; i++ {
		a.Update(tickMsg(time.Now()))
	}
}

// TestBotMatchThroughScreen plays a whole match against a bot using only
// key presses, issuing a move for every unit each turn and skipping the
// animation. The screen must reach the end card and every frame must fit.
func TestBotMatchThroughScreen(t *testing.T) {
	a := testApp(t)
	lm, err := newLocalMatch(a.c, engine.Setup{ID: "t", Mode: "1v1", Seed: 7, TimeControl: "bots",
		Players: []engine.SetupPlayer{{Name: "me", Hero: "hask"}, {Name: "bot", Hero: "wren"}}}, [2]bool{false, true}, "normal")
	if err != nil {
		t.Fatal(err)
	}
	a.screen = newMatchScreen(a, lm)
	m := a.screen.(*matchScreen)
	for turn := 0; turn < 40 && m.phase != "end"; turn++ {
		if m.phase != "orders" {
			t.Fatalf("turn %d: phase %q", turn, m.phase)
		}
		// Every unit: try to move one tile toward the centre, else hold.
		for _, u := range m.myUnits() {
			m.sel, m.cur = u.ID, u.Pos
			m.startMove(a)
			if m.mode == "move" {
				// pick any reachable tile with the largest x (toward the enemy)
				var best *engine.Pos
				for p := range m.reach {
					p := p
					if best == nil || p.X > best.X {
						best = &p
					}
				}
				m.cur = *best
				m.confirm(a)
			} else {
				m.simpleOrder(a, engine.ActHold)
			}
		}
		if m.selected() != nil && m.selected().IsCommander {
			// commander gets a second order: overwatch
			m.simpleOrder(a, engine.ActOverwatch)
		}
		checkFrame(t, a)
		press(a, " ") // commit
		if m.phase != "anim" {
			t.Fatalf("turn %d: expected anim, got %q", turn, m.phase)
		}
		ticks(a, 3)
		checkFrame(t, a)
		press(a, ".")     // skip
		press(a, "enter") // leave finished anim
		if m.phase == "anim" {
			t.Fatalf("turn %d: still animating", turn)
		}
	}
	if m.phase != "end" {
		t.Fatalf("match did not end: phase %q turn %d", m.phase, m.vs.Match.Turn)
	}
	checkFrame(t, a)
	press(a, "s")
	if m.saved == "" {
		t.Fatalf("replay not saved: %s", m.err)
	}
}

// TestHotseatHandover checks the pass screens appear between players.
func TestHotseatHandover(t *testing.T) {
	a := testApp(t)
	lm, err := newLocalMatch(a.c, engine.Setup{ID: "h", Mode: "1v1", Seed: 3, TimeControl: "bots",
		Players: []engine.SetupPlayer{{Name: "p1", Hero: "nul"}, {Name: "p2", Hero: "tally"}}}, [2]bool{false, false}, "")
	if err != nil {
		t.Fatal(err)
	}
	a.screen = newMatchScreen(a, lm)
	m := a.screen.(*matchScreen)
	if m.phase != "pass" || m.player != 0 {
		t.Fatalf("start: phase %q player %d", m.phase, m.player)
	}
	press(a, "enter")
	if m.phase != "orders" {
		t.Fatalf("after pass: %q", m.phase)
	}
	press(a, " ") // p1 commits nothing
	if m.phase != "pass" || m.player != 1 {
		t.Fatalf("handover: phase %q player %d", m.phase, m.player)
	}
	press(a, "enter", " ") // p2 in, commits
	// One playback of the whole turn for both players, not one each.
	if m.phase != "anim" || m.player != -1 {
		t.Fatalf("shared anim: phase %q player %d", m.phase, m.player)
	}
	press(a, ".", "enter")
	if m.phase != "pass" || m.player != 0 || m.afterPass != "orders" {
		t.Fatalf("after the anim p1 is handed turn 2: phase %q player %d after %q", m.phase, m.player, m.afterPass)
	}
	press(a, "enter")
	if m.phase != "orders" || m.player != 0 || m.vs.Match.Turn != 2 {
		t.Fatalf("turn 2 orders for p1: phase %q player %d turn %d", m.phase, m.player, m.vs.Match.Turn)
	}
	checkFrame(t, a)
}

// TestCommandLine issues orders through the : line.
func TestCommandLine(t *testing.T) {
	a := testApp(t)
	lm, _ := newLocalMatch(a.c, engine.Setup{ID: "c", Mode: "1v1", Seed: 5, TimeControl: "bots",
		Players: []engine.SetupPlayer{{Name: "me", Hero: "mott"}, {Name: "bot", Hero: "hask"}}}, [2]bool{false, true}, "easy")
	a.screen = newMatchScreen(a, lm)
	m := a.screen.(*matchScreen)
	press(a, ":")
	for _, r := range "x l" {
		press(a, string(r))
	}
	press(a, "enter")
	if len(m.orders) != 1 || m.orders[0].Action != engine.ActHold {
		t.Fatalf("orders: %+v msg %q", m.orders, m.msg)
	}
	press(a, ":")
	for _, r := range "c c w" {
		press(a, string(r))
	}
	press(a, "enter")
	if m.mode != "" && m.msg == "" {
		t.Fatalf("ping needs a tile: mode %q msg %q", m.mode, m.msg)
	}
}

func checkFrame(t *testing.T, a *App) {
	t.Helper()
	out := a.View()
	lines := strings.Split(out, "\n")
	if len(lines) > a.h {
		t.Fatalf("frame has %d lines > %d", len(lines), a.h)
	}
	for i, l := range lines {
		if w := lipglossWidth(l); w > a.w {
			t.Fatalf("line %d is %d cols > %d: %q", i, w, a.w, l)
		}
	}
}

func lipglossWidth(s string) int { return render.Width(s) }

// TestAnimKeepsTileSize: the resolution playback draws the board at the
// same tile size as planning. It used to drop to the packed 2x1, so the
// board shrank for the length of every turn.
func TestAnimKeepsTileSize(t *testing.T) {
	a := testApp(t)
	deliver(a, tea.WindowSizeMsg{Width: 140, Height: 44})
	lm, err := newLocalMatch(a.c, engine.Setup{ID: "z", Mode: "1v1", Seed: 7, TimeControl: "bots",
		Players: []engine.SetupPlayer{{Name: "me", Hero: "hask"}, {Name: "bot", Hero: "wren"}}}, [2]bool{false, true}, "normal")
	if err != nil {
		t.Fatal(err)
	}
	a.screen = newMatchScreen(a, lm)
	m := a.screen.(*matchScreen)
	a.View()
	planW, planH := m.scene.CellW, m.scene.CellH
	if planW < 3 {
		t.Fatalf("a 140x44 terminal should get large tiles, got %dx%d", planW, planH)
	}
	for _, u := range m.myUnits() {
		m.sel, m.cur = u.ID, u.Pos
		m.startMove(a)
		if m.mode == "move" {
			for p := range m.reach {
				m.cur = p
				break
			}
			m.confirm(a)
		}
	}
	press(a, " ")
	if m.phase != "anim" {
		t.Fatalf("phase %q after commit", m.phase)
	}
	a.View()
	if m.scene.CellW != planW || m.scene.CellH != planH {
		t.Fatalf("playback tiles %dx%d, planning %dx%d", m.scene.CellW, m.scene.CellH, planW, planH)
	}
}
