package client

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"rfog/proto"
)

type menuItem struct {
	label   string
	enabled bool
	note    string
	act     func(a *App) screen
	// kind places it on the home screen: "tile" (quick pairing, clock),
	// "side" (the right column), "more" (the bottom row); "" in the list.
	kind  string
	clock string
}

type menuScreen struct {
	items []menuItem
	sel   int
	// grid is the web app's home layout (big screens); else the list.
	grid    bool
	rated   bool
	ladders map[string][]proto.LadderRow // the top of each rating category
	history []proto.HistoryMatch         // your last matches
}

// resumeMsg is the menu's quiet sign-in: whether there is a match to go
// back to, or a result to show.
type resumeMsg struct {
	r   *remote
	err error
}

// init signs this device in quietly when it has signed in before, so the
// menu can say "match in progress" (or how the last one went), as a chess
// app does when it opens. Nothing changes if the server is not there.
func (m *menuScreen) init(a *App) tea.Cmd {
	if a.net != nil {
		m.fetchLadder(a)
		return nil
	}
	if a.resumeTried || a.set.Token == "" || a.displaced {
		return nil
	}
	a.resumeTried = true
	dial, addr, rules := a.dial, a.server, a.local.Fingerprint()
	auth := proto.Auth{Name: a.set.Name, Token: a.set.Token}
	return func() tea.Msg {
		r, err := connect(dial, addr, rules, auth)
		return resumeMsg{r: r, err: err}
	}
}

// banner is the line at the top of the menu about online play: a match in
// progress to rejoin, matches waiting on you, or the last result.
func (m *menuScreen) banner(a *App) []string {
	if a.net == nil {
		return nil
	}
	st := &a.st
	w := a.net.welcome
	var out []string
	if w.InMatch != "" {
		out = append(out, st.Warn.Render("match in progress")+st.Dim.Render("  r rejoin"))
	}
	waiting := 0
	for _, lm := range w.Live {
		if lm.YourTurn {
			waiting++
		}
	}
	if waiting > 0 {
		out = append(out, st.Warn.Render(fmt.Sprintf("%d daily match(es) waiting on you", waiting))+st.Dim.Render("  d open"))
	}
	if l := w.Last; l != nil && w.InMatch == "" && l.Match != a.set.Seen {
		res := outcome(*l)
		line := fmt.Sprintf("last game: %s vs %s (%s)", res, opponents(*l), l.Result)
		out = append(out, resStyle(st, res).Render(line)+st.Dim.Render("  x dismiss"))
	}
	return out
}

func newMenuScreen() *menuScreen {
	m := &menuScreen{rated: true}
	m.items = listItems()
	return m
}

// listItems is the menu as a list, for screens too small for the grid.
func listItems() []menuItem {
	return []menuItem{
		{label: "tutorial (guided first match)", enabled: true, act: func(a *App) screen {
			s, err := newTutorial(a)
			if err != nil {
				return newMenuScreen()
			}
			return s
		}},
		{label: "play vs bots", enabled: true, act: func(a *App) screen { return newPickScreen(a, matchBots) }},
		{label: "pass and play (two players, one device)", enabled: true, act: func(a *App) screen { return newPickScreen(a, matchHotseat) }},
		{label: "online", enabled: true, act: func(a *App) screen { return newOnlineScreen(a) }},
		{label: "roster", enabled: true, act: func(a *App) screen { return newRosterScreen(a) }},
		{label: "replays", enabled: true, act: func(a *App) screen {
			if a.ephemeral {
				return newMenuScreen()
			}
			return newReplayBrowser(a)
		}},
		{label: "settings", enabled: true, act: func(a *App) screen { return newSettingsScreen(a) }},
		{label: "quit", enabled: true, act: func(a *App) screen { return nil }},
	}
}

func (m *menuScreen) update(a *App, msg tea.Msg) (screen, tea.Cmd) {
	if rm, ok := msg.(resumeMsg); ok {
		if rm.err != nil || a.net != nil {
			if rm.r != nil && a.net != nil {
				rm.r.close()
			}
			return m, nil // offline, or the server is away: the menu as it was
		}
		a.adopt(rm.r)
		m.fetchLadder(a)
		return m, rm.r.recv()
	}
	if n, ok := msg.(netMsg); ok {
		switch {
		case n.Err != nil:
		case n.T == proto.TLadderList:
			var l proto.LadderList
			if n.F.As(&l) == nil && m.ladders != nil {
				m.ladders[l.Mode] = l.Rows
			}
		case n.T == proto.THistoryList:
			var h proto.HistoryList
			if n.F.As(&h) == nil {
				m.history = h.Matches
			}
		}
		return m, nil
	}
	m.layout(a)
	if k, ok := msg.(tea.KeyMsg); ok && m.grid {
		if next, cmd, done := m.gridKey(a, k); done {
			return next, cmd
		}
	}
	if mm, ok := msg.(tea.MouseMsg); ok {
		if id, hit, act := a.pointer(mm, m.sel); hit {
			m.sel = id
			if act {
				it := m.items[id]
				if it.enabled && it.act != nil {
					return it.act(a), nil
				}
			}
		}
		return m, nil
	}
	k, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	switch {
	case isKey(k, "j", "down", "tab"):
		m.sel = (m.sel + 1) % len(m.items)
	case isKey(k, "k", "up", "shift+tab"):
		m.sel = (m.sel + len(m.items) - 1) % len(m.items)
	case isKey(k, "enter", " ", "l", "right"):
		it := m.items[m.sel]
		if it.enabled && it.act != nil {
			return it.act(a), nil
		}
	case isKey(k, "r") && a.net != nil && a.net.welcome.InMatch != "":
		return a.rejoin(a.net.welcome.InMatch), nil
	case isKey(k, "d") && a.net != nil && len(a.net.welcome.Live) > 0:
		return newLiveScreen(a), nil
	case isKey(k, "x") && a.net != nil && a.net.welcome.Last != nil:
		a.set.Seen = a.net.welcome.Last.Match
		a.saveSettings()
	case isKey(k, "q", "esc"):
		return nil, nil
	}
	return m, nil
}

// fetchLadder asks for what the home screen shows from the server: the
// top of each ladder and your last matches.
func (m *menuScreen) fetchLadder(a *App) {
	if m.ladders != nil || a.net == nil {
		return
	}
	m.ladders = map[string][]proto.LadderRow{}
	for _, mode := range ladderModes {
		_ = a.net.send(proto.TLadder, proto.Ladder{Mode: mode})
	}
	_ = a.net.send(proto.THistory, proto.History{Limit: 5})
}

func (m *menuScreen) view(a *App) string {
	m.layout(a)
	if m.grid {
		return m.gridView(a)
	}
	var items []string
	roomy := a.roomy(len(m.items))
	head := 2 // title and the blank under it
	var top []string
	if b := m.banner(a); len(b) > 0 {
		top = append(b, "")
		head += len(top) // the banner above the title
	}
	for i, it := range m.items {
		a.clickRows(head+len(items), i, roomy)
		cursor := "  "
		if i == m.sel {
			cursor = a.st.Accent.Render("> ")
		}
		label := it.label
		if !it.enabled {
			label = a.st.Dim.Render(label + "  (" + it.note + ")")
		} else if i == m.sel {
			label = a.st.Accent.Render(label)
		}
		items = append(items, cursor+label)
		if roomy {
			items = append(items, "")
		}
	}
	lines := append(top, append([]string{a.st.Title.Render(GameName), ""}, block(items)...)...)
	hint := "j/k move  enter select  q quit"
	if a.w < 60 {
		hint = "tap or j/k · enter select"
	}
	lines = append(lines, "", a.st.Dim.Render(hint))
	lines = append(lines, "", a.st.Dim.Render(strings.TrimSpace("operator: "+a.set.Name)))
	return a.centered(lines)
}
