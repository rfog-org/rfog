package client

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"rfog/engine"
)

// MenuItem is one command in the action area. Key is what a keyboard
// would press for it; Sub, when set, opens another level instead (Act
// lists the attack and the abilities, More the rarer commands).
type MenuItem struct {
	Label string `json:"label"`
	Key   string `json:"key,omitempty"`
	Note  string `json:"note,omitempty"`
	Off   bool   `json:"off,omitempty"`
	Sub   string `json:"sub,omitempty"`
}

// menuLevel is one level of the unit's commands: "unit" or "act".
func (m *matchScreen) menuLevel(a *App, u *engine.Unit, level string) []MenuItem {
	if level != "act" {
		return []MenuItem{
			{Label: "Move", Key: "m"},
			{Label: "Act", Note: "▸", Sub: "act"},
			{Label: "Hold", Key: "x"},
			{Label: "Status", Key: "i"},
		}
	}
	items := []MenuItem{{Label: "Attack", Key: "a"}}
	for _, ab := range m.abilityButtons(a, u) {
		note := strings.ToUpper(ab.Key)
		if !ab.Ready {
			note = ab.State
		}
		items = append(items, MenuItem{Label: ab.Name, Key: ab.Key, Note: note, Off: !ab.Ready})
	}
	items = append(items, MenuItem{Label: "Overwatch", Key: "o"}, MenuItem{Label: "Back", Note: "◂", Sub: "unit"})
	return items
}

// AbilityUI is one of a unit's abilities as the action menu shows it: its
// key, name, and whether it can be cast now (State says why not: "L2" for
// a level to reach, "cd1" for a cooldown).
type AbilityUI struct {
	Key   string
	Name  string
	Ready bool
	State string
}

// abilityButtons is the selected unit's abilities with whether each can
// be cast now.
func (m *matchScreen) abilityButtons(a *App, u *engine.Unit) []AbilityUI {
	var out []AbilityUI
	abs := engine.UnitAbilities(a.c, u)
	if u.IsCommander {
		h := a.c.Heroes[u.Kind]
		for _, k := range []string{"q", "w", "e", "r"} {
			id := h.Abilities[k]
			ab := AbilityUI{Key: k, Name: a.c.Abilities[id].Name, Ready: true, State: "rdy"}
			if !contains(abs, id) {
				ab.Ready, ab.State = false, fmt.Sprintf("L%d", a.c.Rules.Unlock[k])
			} else if cd := u.Cooldowns[id]; cd > 0 {
				ab.Ready, ab.State = false, fmt.Sprintf("cd%d", cd)
			}
			out = append(out, ab)
		}
		return out
	}
	for i, id := range abs {
		ab := AbilityUI{Key: fmt.Sprint(i + 1), Name: a.c.Abilities[id].Name, Ready: true, State: "rdy"}
		if cd := u.Cooldowns[id]; cd > 0 {
			ab.Ready, ab.State = false, fmt.Sprintf("cd%d", cd)
		}
		out = append(out, ab)
	}
	return out
}

// turnItems is the action area's second row: what applies to the whole
// turn, or with More open the rarer commands.
func (m *matchScreen) turnItems() []MenuItem {
	if m.more {
		return []MenuItem{
			{Label: "Drop", Key: "d", Note: "orders"}, {Label: "Keys", Key: "?"},
			{Label: "Command", Key: ":"}, {Label: "Quit", Key: "Q"}, {Label: "Back", Note: "◂", Sub: "less"},
		}
	}
	log := "Log"
	if isRemote(m.src) {
		log = "Log·Chat"
	}
	return []MenuItem{
		{Label: "End turn", Key: " ", Note: fmt.Sprint(len(m.orders))},
		{Label: "Next", Key: "tab"},
		{Label: log, Key: "g"},
		{Label: "More", Note: "▸", Sub: "more"},
	}
}

// actionArea draws the action area: boxed buttons, the selected unit's
// commands on top and the turn's below, width columns wide, each button
// tall rows high. Each button
// is registered as a click target (ids -1, -2, ... into m.actions) at
// rows starting from base. It sits in the game screen itself, the same in
// a terminal and in a browser, where the buttons are thumb targets.
func (m *matchScreen) actionArea(a *App, width, base, tall int) []string {
	st, g := &a.st, a.g
	rows := m.actionItems(a)
	m.actions = m.actions[:0]
	var out []string
	for _, row := range rows {
		bw := width / len(row)
		lines := make([]string, tall)
		x := 0
		for i, it := range row {
			w := bw
			if i == len(row)-1 {
				w = width - bw*(len(row)-1) // the last takes the remainder
			}
			inner := w - 2
			key := it.Key
			switch key {
			case " ":
				key = "␣"
			case "tab":
				key = "⇥"
			case "enter":
				key = "⏎"
			}
			label := strings.ToUpper(it.Label)
			if it.Note != "" {
				label += " " + it.Note
			}
			text := label
			if key != "" {
				text = key + " " + label
			}
			text = centreIn(text, inner)
			body := text
			if it.Off {
				body = st.Dim.Render(text)
			} else if key != "" {
				// The key in the key colour, the word in bold.
				if i := strings.Index(text, key); i >= 0 {
					body = text[:i] + st.Key.Render(key) + st.Plain.Bold(true).Render(text[i+len(key):])
				}
			} else {
				body = st.Plain.Bold(true).Render(text)
			}
			edge := st.Dim.Render
			for r := range lines {
				switch r {
				case 0:
					lines[r] += edge(g.TL + strings.Repeat(g.H, inner) + g.TR)
				case tall - 1:
					lines[r] += edge(g.BL + strings.Repeat(g.H, inner) + g.BR)
				case tall / 2:
					lines[r] += edge(g.V) + fit(body, inner) + edge(g.V)
				default:
					lines[r] += edge(g.V) + strings.Repeat(" ", inner) + edge(g.V)
				}
			}
			if !it.Off {
				m.actions = append(m.actions, it)
				id := -len(m.actions)
				for r := 0; r < tall; r++ {
					a.clickBox(base+len(out)+r, x, x+w, id)
				}
			}
			x += w
		}
		out = append(out, lines...)
	}
	return out
}

// actionItems is the action area's buttons for this moment, in rows of
// at most four. Every moment of a match has some: the page has no buttons
// of its own, so a phone can always go on.
func (m *matchScreen) actionItems(a *App) [][]MenuItem {
	rowsOf := func(items []MenuItem) [][]MenuItem {
		var rows [][]MenuItem
		for len(items) > 0 {
			n := minInt(4, len(items))
			rows = append(rows, items[:n])
			items = items[n:]
		}
		return rows
	}
	chat := isRemote(m.src) || m.src.Watching()
	switch {
	case m.mode == "chat" || m.mode == "cmd":
		return nil // the line being typed has the screen's attention
	case m.quitArmed:
		what := "Leave"
		if rm, ok := m.src.(*remoteMatch); ok && !rm.async {
			what = "Forfeit"
		}
		return [][]MenuItem{{{Label: what, Key: "Q"}, {Label: "Stay", Key: "esc"}}}
	case m.logOpen:
		row := []MenuItem{{Label: "Back", Key: "g"}, {Label: "Older", Key: "k"}, {Label: "Newer", Key: "j"}}
		if chat {
			row = append(row, MenuItem{Label: "Chat", Key: "t"})
		}
		return [][]MenuItem{row}
	case m.src.Watching():
		return [][]MenuItem{{{Label: "Next", Key: "enter"}, {Label: "Skip", Key: "."}, {Label: "Log", Key: "g"}, {Label: "Leave", Key: "q"}}}
	}
	switch m.phase {
	case "pass":
		return [][]MenuItem{{{Label: "Ready", Key: "enter"}, {Label: "Quit", Key: "Q"}}}
	case "anim":
		return [][]MenuItem{{{Label: "Skip", Key: "."}, {Label: "Log", Key: "g"}}}
	case "wait":
		return [][]MenuItem{{{Label: "Log", Key: "g"}, {Label: "Leave", Key: "Q"}}}
	case "end":
		return [][]MenuItem{{{Label: "Rematch", Key: "r"}, {Label: "Save", Key: "s", Note: "replay"}, {Label: "Menu", Key: "enter"}}}
	}
	var rows [][]MenuItem
	if u := m.selected(); u != nil && u.Owner == m.player && u.Alive() {
		rows = rowsOf(m.menuLevel(a, u, m.popup))
	} else {
		rows = [][]MenuItem{{{Label: "tap one of your units", Off: true}}}
	}
	return append(rows, m.turnItems())
}

// actionRows is how tall the action area is right now: three rows per
// row of buttons.
func (m *matchScreen) actionRows(a *App) int {
	return len(m.actionItems(a)) * 3
}

// actionPick acts on action i (a click or a tap on its button).
func (m *matchScreen) actionPick(a *App, i int) (screen, tea.Cmd) {
	if i < 0 || i >= len(m.actions) {
		return m, nil
	}
	it := m.actions[i]
	switch it.Sub {
	case "act":
		m.popup = "act"
		return m, nil
	case "unit":
		m.popup = ""
		return m, nil
	case "more":
		m.more = true
		return m, nil
	case "less":
		m.more = false
		return m, nil
	}
	var k tea.KeyMsg
	switch it.Key {
	case " ":
		k = tea.KeyMsg{Type: tea.KeySpace}
	case "tab":
		k = tea.KeyMsg{Type: tea.KeyTab}
	case "enter":
		k = tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		k = tea.KeyMsg{Type: tea.KeyEsc}
	default:
		k = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(it.Key)}
	}
	m.more = false
	return m.key(a, k)
}
