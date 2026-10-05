package client

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"rfog/engine"
)

// resize drives a terminal size change the way Bubble Tea does.
func resize(a *App, w, h int) {
	deliver(a, tea.WindowSizeMsg{Width: w, Height: h})
}

// TestResizeDuringEverything rotates a phone (and a desktop window) at
// every point in a match: the frame must stay inside the new size, the
// layout must follow the width, and the match must keep its state.
func TestResizeDuringEverything(t *testing.T) {
	shapes := [][2]int{{45, 28}, {100, 20}, {46, 50}, {140, 44}, {80, 24}, {40, 18}}
	a := testApp(t)
	a.w, a.h = 45, 28
	a.applyTier()
	lm, err := newLocalMatch(a.c, engine.Setup{ID: "t", Mode: "1v1", Seed: 9, TimeControl: "bots",
		Players: []engine.SetupPlayer{{Name: "me", Hero: "nul"}, {Name: "bot", Hero: "tally"}}}, [2]bool{false, true}, "normal")
	if err != nil {
		t.Fatal(err)
	}
	a.screen = newMatchScreen(a, lm)
	m := a.screen.(*matchScreen)

	for turn := 0; turn < 3 && m.phase != "end"; turn++ {
		for i, sz := range shapes {
			resize(a, sz[0], sz[1])
			checkFrame(t, a)
			if a.compact() != (sz[0] < 80) {
				t.Fatalf("%dx%d: compact %v", sz[0], sz[1], a.compact())
			}
			// The board viewport must stay on the board after every resize.
			// (The viewport is in screen tiles: the board may be turned.)
			bw, bh := m.orient.Dims(m.scene.Board.W, m.scene.Board.H)
			if m.vp.OX < 0 || m.vp.OY < 0 || m.vp.OX+m.vp.W > bw || m.vp.OY+m.vp.H > bh {
				t.Fatalf("%dx%d: viewport %+v off a %dx%d board", sz[0], sz[1], m.vp, bw, bh)
			}
			// Orders survive the rotation.
			if i == 0 {
				u := m.myUnits()[0]
				m.sel, m.cur = u.ID, u.Pos
				m.simpleOrder(a, engine.ActHold)
			}
			if len(m.orders) == 0 {
				t.Fatalf("%dx%d: orders lost", sz[0], sz[1])
			}
			// The detail panel opens at any size and always fits.
			press(a, "i")
			checkFrame(t, a)
			press(a, "i")
			press(a, "?")
			checkFrame(t, a)
			press(a, "?")
		}
		for _, u := range m.myUnits() {
			m.sel, m.cur = u.ID, u.Pos
			m.simpleOrder(a, engine.ActHold)
		}
		press(a, " ")
		for i := 0; i < 40 && m.phase == "anim"; i++ {
			ticks(a, 1)
			if i%7 == 0 { // rotating mid-animation must not break the frame
				resize(a, shapes[i%len(shapes)][0], shapes[i%len(shapes)][1])
				checkFrame(t, a)
			}
		}
		press(a, ".", "enter")
	}
}

// TestTooSmallMessage: below the floor the client says so, with the size.
func TestTooSmallMessage(t *testing.T) {
	a := testApp(t)
	resize(a, 30, 10)
	out := stripEscapes(a.View())
	if !strings.Contains(out, "30x10") || !strings.Contains(out, "40x18") {
		t.Fatalf("message: %q", out)
	}
	resize(a, 45, 20)
	if strings.Contains(stripEscapes(a.View()), "need at least") {
		t.Fatal("45x20 should draw")
	}
}

// TestMenusFitEverywhere renders every non-match screen at phone sizes.
func TestMenusFitEverywhere(t *testing.T) {
	for _, sz := range [][2]int{{40, 18}, {45, 28}, {46, 50}, {100, 20}} {
		a := testApp(t)
		a.w, a.h = sz[0], sz[1]
		a.applyTier()
		screens := map[string]screen{
			"title":    newTitleScreen(),
			"menu":     newMenuScreen(),
			"roster":   newRosterScreen(a),
			"settings": newSettingsScreen(a),
			"pick":     newPickScreen(a, matchBots),
			"replays":  newReplayBrowser(a),
		}
		for name, s := range screens {
			a.screen = s
			for i := 0; i < 3; i++ {
				ticks(a, 1)
			}
			out := a.View()
			for j, l := range strings.Split(out, "\n") {
				if w := lipglossWidth(l); w > sz[0] {
					t.Fatalf("%s at %dx%d: line %d is %d cols: %q", name, sz[0], sz[1], j, w, stripEscapes(l))
				}
			}
			if n := len(strings.Split(out, "\n")); n > sz[1] {
				t.Fatalf("%s at %dx%d: %d lines", name, sz[0], sz[1], n)
			}
		}
	}
}
