package client

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"

	"rfog/render"
)

type settingsScreen struct {
	sel     int
	editing bool
	buf     string
}

func newSettingsScreen(a *App) *settingsScreen { return &settingsScreen{} }

var tierOptions = []string{"auto", "t0", "t1", "t2"}
var animOptions = []int{0, 60, 120, 250, 500}

func (s *settingsScreen) rows(a *App) []string {
	return []string{
		"theme:      " + a.set.Theme,
		"tier:       " + a.set.Tier + "  (active " + a.tier.String() + ")",
		fmt.Sprintf("animation:  %d ms/event", a.set.AnimMs),
		"effects:    " + a.set.Effects,
		"cells:      " + a.set.CellsOrAuto() + "  (board tile size)",
		"bot level:  " + a.set.BotLevel,
		"name:       " + a.set.Name,
		"server:     " + a.set.Server,
		"back",
	}
}

func cycle(list []string, cur string, dir int) string {
	for i, v := range list {
		if v == cur {
			return list[(i+dir+len(list))%len(list)]
		}
	}
	return list[0]
}

func (s *settingsScreen) update(a *App, msg tea.Msg) (screen, tea.Cmd) {
	// A tap selects a row; a tap on the selected row changes it (or opens
	// it for typing, or goes back): the same as enter.
	if mm, ok := msg.(tea.MouseMsg); ok && !s.editing {
		if id, hit, act := a.pointer(mm, s.sel); hit {
			s.sel = id
			if act {
				return s.update(a, tea.KeyMsg{Type: tea.KeyEnter})
			}
		}
		return s, nil
	}
	k, ok := msg.(tea.KeyMsg)
	if !ok {
		return s, nil
	}
	if s.editing {
		switch {
		case isKey(k, "enter"):
			if s.sel == 6 && s.buf != "" {
				a.set.Name = s.buf
			}
			if s.sel == 7 && s.buf != "" {
				a.set.Server = s.buf
			}
			s.editing = false
			a.saveSettings()
		case isKey(k, "esc"):
			s.editing = false
		case isKey(k, "backspace"):
			if len(s.buf) > 0 {
				s.buf = s.buf[:len(s.buf)-1]
			}
		default:
			if k.Type == tea.KeyRunes && len(s.buf) < 24 {
				s.buf += string(k.Runes)
			}
		}
		return s, nil
	}
	n := len(s.rows(a))
	dir := 0
	switch {
	case isKey(k, "j", "down", "tab"):
		s.sel = (s.sel + 1) % n
	case isKey(k, "k", "up", "shift+tab"):
		s.sel = (s.sel + n - 1) % n
	case isKey(k, "l", "right", " "):
		dir = 1
	case isKey(k, "h", "left"):
		dir = -1
	case isKey(k, "enter"):
		switch s.sel {
		case 6, 7:
			s.editing = true
			s.buf = ""
		case 8:
			return newMenuScreen(), nil
		default:
			dir = 1
		}
	case isKey(k, "q", "esc"):
		return newMenuScreen(), nil
	}
	if dir != 0 {
		switch s.sel {
		case 0:
			a.set.Theme = cycle(render.ThemeNames(), a.set.Theme, dir)
		case 1:
			a.set.Tier = cycle(tierOptions, a.set.Tier, dir)
		case 2:
			for i, v := range animOptions {
				if v == a.set.AnimMs {
					a.set.AnimMs = animOptions[(i+dir+len(animOptions))%len(animOptions)]
					break
				}
			}
		case 3:
			a.set.Effects = cycle([]string{"on", "off"}, a.set.Effects, dir)
		case 4:
			a.set.Cells = cycle([]string{"auto", "small", "large"}, a.set.CellsOrAuto(), dir)
		case 5:
			a.set.BotLevel = cycle([]string{"easy", "normal", "hard"}, a.set.BotLevel, dir)
		}
		a.applyTier()
		a.saveSettings()
	}
	return s, nil
}

func (s *settingsScreen) view(a *App) string {
	var lines []string
	lines = append(lines, a.st.Title.Render("settings"), "")
	for i, r := range s.rows(a) {
		a.clickRow(len(lines), i)
		cur := "  "
		if i == s.sel {
			cur = a.st.Accent.Render("> ")
			if s.editing {
				r = r[:12] + s.buf + "_"
			}
			r = a.st.Accent.Render(r)
		}
		lines = append(lines, cur+r)
	}
	lines = append(lines, "")
	lines = append(lines, a.st.Dim.Render("h/l or tap twice: change  enter edit  esc back"))
	if a.w >= 80 {
		lines = append(lines, a.st.Dim.Render(truncate("custom theme: "+ConfigDir()+"/theme.toml", a.w-2)))
	}
	lines = append(lines, "")
	// Palette preview.
	lines = append(lines, a.st.TeamA.Render("L R U M H")+"  "+a.st.TeamB.Render("l r u m w")+"  "+
		a.st.Objective.Render(a.g.Objective)+" "+a.st.Cover.Render(a.g.Cover)+" "+a.st.Wall.Render(a.g.Wall)+" "+
		a.st.Smoke.Render(a.g.Smoke)+" "+a.st.Danger.Render(a.g.Telegraph)+"  "+
		a.st.Ground[0].Render(a.g.Open[0])+a.st.Ground[1].Render(a.g.Open[1])+a.st.Ground[2].Render(a.g.Open[2])+a.st.Ground[3].Render(a.g.Open[3]))
	return a.centered(lines)
}
