package client

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"rfog/render/art"
	"rfog/render/fx"
)

// titleScreen is the splash: the word mark wipes in, then loops at two
// frames a second with glitch bands, a sweeping scanline and the odd
// corrupted cell. Just the title: the heroes are met in the pick screen.
// Any key skips to the menu.
type titleScreen struct {
	frame int
	seed  uint64
}

type tickMsg time.Time

func tick(ms int) tea.Cmd {
	return tea.Tick(time.Duration(ms)*time.Millisecond, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func newTitleScreen() *titleScreen { return &titleScreen{seed: uint64(time.Now().UnixNano())} }

// GameName is the game's name as players see it: RFoG, "RF over Glass".
// The binary, module and data paths are "rfog".
const GameName = "RFoG"

// titleArt is GameName as a word mark (the o sits on the baseline).
var titleArt = []string{
	"####   #####          ####",
	"#   #  #             #    ",
	"####   ####    ###   #  ##",
	"#  #   #      #   #  #   #",
	"#   #  #       ###    ####",
}

// Entry dissolve: this many frames at entryMs, then the calm loop.
const (
	entryFrames = 6
	entryMs     = 70
	loopMs      = 500
)

func (t *titleScreen) init(a *App) tea.Cmd { return tick(entryMs) }

func (t *titleScreen) update(a *App, msg tea.Msg) (screen, tea.Cmd) {
	switch m := msg.(type) {
	case tickMsg:
		t.frame++
		if t.frame < entryFrames {
			return t, tick(entryMs)
		}
		return t, tick(loopMs)
	case tea.KeyMsg:
		if isKey(m, "q", "esc") {
			return nil, nil
		}
		return newMenuScreen(), nil
	case tea.MouseMsg:
		// A tap or a click is "any key" too: a phone has no keys.
		if m.Action == tea.MouseActionPress && m.Button == tea.MouseButtonLeft {
			return newMenuScreen(), nil
		}
	}
	return t, nil
}

func (t *titleScreen) view(a *App) string {
	if !a.sized {
		// The terminal has not said how big it is yet; drawing art now
		// would place it for 80x24 and then jump.
		return a.centered([]string{a.st.Dim.Render(GameName)})
	}
	tier := a.artTier()
	mark := art.FromLines(tier, titleArt, art.Bold)
	if tier != "t0" {
		for i := range mark.Cells {
			if mark.Cells[i].R == '#' {
				mark.Cells[i].R = '█'
			}
		}
	}
	loop := t.frame - entryFrames
	switch {
	case t.frame < entryFrames:
		// Wiped in, not dissolved: the word mark should read from the
		// first frame, not arrive as confetti.
		mark = fx.Wipe(mark, float64(t.frame+1)/float64(entryFrames), false)
	case a.set.EffectsOn():
		mark = fx.GlitchBands(mark, t.seed, loop, 0.3)
		mark = fx.Corrupt(mark, t.seed, loop, 0.015)
		mark = fx.Scanline(mark, loop, 12)
	}
	lines := mark.Render(a.artStyler())
	lines = append(lines, "")
	lines = append(lines, a.st.Dim.Render("terminal tactics skirmish"))
	lines = append(lines, "")
	status := "link established. operator terminal ready."
	if t.frame < entryFrames {
		n := len(status) * (t.frame + 1) / entryFrames
		status = status[:n]
	}
	lines = append(lines, a.st.Dim.Render(status))
	lines = append(lines, "")
	prompt := "press any key or tap"
	if fx.Flicker(t.seed, t.frame) || t.frame%2 == 1 {
		lines = append(lines, a.st.Dim.Render(prompt))
	} else {
		lines = append(lines, a.st.Accent.Render(prompt))
	}
	lines = append(lines, "")
	lines = append(lines, a.st.Dim.Render(a.tier.String()+"  "+a.st.Theme.Name))
	return a.centered(lines)
}
