package client

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"rfog/proto"
	"rfog/render"
)

// The home screen on a big terminal, laid out as the web app's: a top bar,
// the quick pairing grid of clocks (rated or casual), a side column with
// playing bots, challenging a friend and learning, your games and the
// leaderboard, and the rest of the menu along the bottom.

// homeClocks are the clocks before the server has said which it offers
// (the server's list replaces them once connected).
var homeClocks = []proto.ClockInfo{
	{ID: "10s", Turn: 10, Category: "bullet"}, {ID: "15s", Turn: 15, Category: "bullet"},
	{ID: "20s", Turn: 20, Category: "blitz"}, {ID: "30s", Turn: 30, Category: "blitz"},
	{ID: "45s", Turn: 45, Category: "rapid"}, {ID: "60s", Turn: 60, Category: "rapid"},
	{ID: "90s+", Turn: 90, Bank: 300, Category: "long-haul"}, {ID: "120s+", Turn: 120, Bank: 480, Category: "long-haul"},
	{ID: "24h", Turn: 86400, Category: "daily", Async: true},
}

// gridFits is whether the screen has room for the home layout.
func gridFits(a *App) bool { return a.w >= 96 && a.h >= 36 }

// gridItems is the home screen's items: tiles, side column, bottom row.
func (m *menuScreen) gridItems(a *App) []menuItem {
	clocks := homeClocks
	if a.net != nil && len(a.net.welcome.Clocks) > 0 {
		clocks = a.net.welcome.Clocks
	}
	var out []menuItem
	for _, c := range clocks {
		c := c
		out = append(out, menuItem{label: c.ID + " " + c.Category, enabled: true, kind: "tile", clock: c.ID,
			act: func(a *App) screen { return newOnlineQueue(a, c.ID, m.rated) }})
	}
	side := []menuItem{
		{label: "play against bots", enabled: true, kind: "side", act: func(a *App) screen { return newPickScreen(a, matchBots) }},
		{label: "pass and play", enabled: true, kind: "side", act: func(a *App) screen { return newPickScreen(a, matchHotseat) }},
		{label: "challenge a friend", enabled: true, kind: "side", act: func(a *App) screen {
			o := newOnlineScreen(a)
			o.thenChallenge, o.fromHome = true, true
			return o
		}},
		{label: "how to play", enabled: true, kind: "side", act: func(a *App) screen {
			s, err := newTutorial(a)
			if err != nil {
				return newMenuScreen()
			}
			return s
		}},
	}
	more := []menuItem{
		{label: "online", enabled: true, kind: "more", act: func(a *App) screen { return newOnlineScreen(a) }},
		{label: "replays", enabled: true, kind: "more", act: func(a *App) screen {
			if a.ephemeral {
				return newMenuScreen()
			}
			return newReplayBrowser(a)
		}},
		{label: "settings", enabled: true, kind: "more", act: func(a *App) screen { return newSettingsScreen(a) }},
		{label: "quit", enabled: true, kind: "more", act: func(a *App) screen { return nil }},
	}
	// The top bar, as the web app's: this page, then watch, learn (the
	// commanders) and the leaderboard.
	nav := []menuItem{
		{label: "play", enabled: true, kind: "nav", act: func(a *App) screen { return m }},
		{label: "watch", enabled: true, kind: "nav", act: homeWatch},
		{label: "learn", enabled: true, kind: "nav", act: func(a *App) screen { return newRosterScreen(a) }},
		{label: "leaderboard", enabled: true, kind: "nav", act: homeLadder},
	}
	return append(append(append(out, side...), more...), nav...)
}

// homeWatch is the live matches, signing in first if need be.
func homeWatch(a *App) screen {
	if a.net != nil {
		return newLobbyScreen(a)
	}
	o := newOnlineScreen(a)
	o.thenWatch = true
	return o
}

// homeLadder is the leaderboard, signing in first if need be.
func homeLadder(a *App) screen {
	if a.net != nil {
		return newLadderScreen(a, "blitz")
	}
	o := newOnlineScreen(a)
	o.thenLadder = true
	return o
}

// layout picks the grid or the list for this screen, keeping the
// selection on the same item where it can.
func (m *menuScreen) layout(a *App) {
	grid := gridFits(a)
	cur := ""
	if m.sel < len(m.items) {
		cur = m.items[m.sel].label
	}
	if grid {
		m.items = m.gridItems(a)
	} else {
		m.items = listItems()
	}
	if grid != m.grid {
		m.sel = 0
	}
	m.grid = grid
	for i, it := range m.items {
		if it.label == cur {
			m.sel = i
		}
	}
	if m.sel >= len(m.items) {
		m.sel = 0
	}
}

// gridKey moves through the home screen: arrows across the tiles (three
// to a row) and into the side column and the bottom row.
func (m *menuScreen) gridKey(a *App, k tea.KeyMsg) (screen, tea.Cmd, bool) {
	var tiles, side, more, nav []int
	for i, it := range m.items {
		switch it.kind {
		case "tile":
			tiles = append(tiles, i)
		case "side":
			side = append(side, i)
		case "more":
			more = append(more, i)
		case "nav":
			nav = append(nav, i)
		}
	}
	idx := func(list []int, i int) int {
		for j, v := range list {
			if v == i {
				return j
			}
		}
		return -1
	}
	cur := m.items[m.sel].kind
	switch {
	case isKey(k, "w"):
		return homeWatch(a), nil, true
	case isKey(k, "L"):
		return homeLadder(a), nil, true
	case isKey(k, "c"):
		if a.net != nil && a.net.welcome.Guest {
			return m, nil, true // guests play casual
		}
		m.rated = !m.rated
		return m, nil, true
	case isKey(k, "right", "l"):
		switch cur {
		case "tile":
			j := idx(tiles, m.sel)
			if j%3 == 2 || j == len(tiles)-1 {
				m.sel = side[minInt(j/3, len(side)-1)]
			} else {
				m.sel = tiles[j+1]
			}
		case "more":
			m.sel = more[(idx(more, m.sel)+1)%len(more)]
		case "nav":
			m.sel = nav[(idx(nav, m.sel)+1)%len(nav)]
		}
		return m, nil, true
	case isKey(k, "left", "h"):
		switch cur {
		case "tile":
			if j := idx(tiles, m.sel); j%3 > 0 {
				m.sel = tiles[j-1]
			}
		case "side":
			j := idx(side, m.sel)
			m.sel = tiles[minInt(j*3+2, len(tiles)-1)]
		case "more":
			m.sel = more[(idx(more, m.sel)+len(more)-1)%len(more)]
		case "nav":
			m.sel = nav[(idx(nav, m.sel)+len(nav)-1)%len(nav)]
		}
		return m, nil, true
	case isKey(k, "down", "j"):
		switch cur {
		case "nav":
			if idx(nav, m.sel) == len(nav)-1 {
				m.sel = side[0] // the leaderboard sits over the side column
			} else {
				m.sel = tiles[minInt(idx(nav, m.sel), len(tiles)-1)]
			}
		case "tile":
			if j := idx(tiles, m.sel); j+3 < len(tiles) {
				m.sel = tiles[j+3]
			} else {
				m.sel = more[0]
			}
		case "side":
			if j := idx(side, m.sel); j+1 < len(side) {
				m.sel = side[j+1]
			} else {
				m.sel = more[len(more)-1]
			}
		}
		return m, nil, true
	case isKey(k, "up", "k"):
		switch cur {
		case "tile":
			if j := idx(tiles, m.sel); j >= 3 {
				m.sel = tiles[j-3]
			} else {
				m.sel = nav[minInt(j, len(nav)-1)]
			}
		case "side":
			if j := idx(side, m.sel); j > 0 {
				m.sel = side[j-1]
			} else {
				m.sel = nav[len(nav)-1]
			}
		case "more":
			m.sel = tiles[len(tiles)-1]
		}
		return m, nil, true
	}
	return m, nil, false
}

// gridView draws the home screen.
func (m *menuScreen) gridView(a *App) string {
	st := &a.st
	const tileW, tileH, gap = 15, 5, 1
	leftW := 3*tileW + 2*gap
	rightW := 44
	bw := leftW + 4 + rightW
	x0 := (a.w - bw) / 2
	if x0 < 1 {
		x0 = 1
	}
	pad := strings.Repeat(" ", x0)
	var out []string
	row := func(s string) { out = append(out, pad+s) }

	// The top bar.
	who := st.Dim.Render("offline")
	if a.net != nil {
		w := a.net.welcome
		who = st.TeamA.Render("● ") + w.Name
		if w.Guest {
			who += st.Dim.Render(" · guest")
		}
		who += st.Dim.Render(fmt.Sprintf("   %d online", w.Online))
	}
	bar := st.Title.Render(GameName) + "  "
	for i, it := range m.items {
		if it.kind != "nav" {
			continue
		}
		label := " " + capital(it.label) + " "
		switch {
		case i == m.sel:
			label = st.Key.Render(label)
		case it.label == "play":
			label = st.Accent.Render(label)
		default:
			label = st.Plain.Render(label)
		}
		x := x0 + render.Width(bar)
		a.clickBox(len(out), x, x+render.Width(label), i)
		bar += label + " "
	}
	row(fitPad(bar, bw-render.Width(who)) + who)
	row(st.Border.Render(strings.Repeat("─", bw)))
	if b := m.banner(a); len(b) > 0 {
		for _, l := range b {
			row(l)
		}
	}
	row("")

	// The quick pairing heading with the rated/casual toggle.
	guest := a.net != nil && a.net.welcome.Guest
	rated, casual := st.Key.Render(" rated "), st.Dim.Render(" casual ")
	if !m.rated || guest {
		rated, casual = st.Dim.Render(" rated "), st.Key.Render(" casual ")
	}
	head := st.Accent.Render("QUICK PAIRING") + st.Dim.Render("  online · 1v1")
	toggle := rated + casual + st.Dim.Render(" (c)")
	headLine := fitPad(head, leftW-render.Width(toggle)) + toggle

	// The left column: heading and tiles.
	var left []string
	left = append(left, headLine, "")
	var tileIdx []int
	for i, it := range m.items {
		if it.kind == "tile" {
			tileIdx = append(tileIdx, i)
		}
	}
	topY := len(out)
	for r := 0; r*3 < len(tileIdx); r++ {
		lines := make([]string, tileH)
		for c := 0; c < 3; c++ {
			j := r*3 + c
			if j >= len(tileIdx) {
				break
			}
			i := tileIdx[j]
			box := m.tile(a, m.items[i], i == m.sel, tileW, tileH, guest)
			for k := range lines {
				if c > 0 {
					lines[k] += strings.Repeat(" ", gap)
				}
				lines[k] += box[k]
			}
			// click target: the whole tile
			ty := topY + len(left)
			for k := 0; k < tileH; k++ {
				a.clickBox(ty+k, x0+c*(tileW+gap), x0+c*(tileW+gap)+tileW, i)
			}
		}
		left = append(left, lines...)
		left = append(left, "")
	}

	// The right column: actions, your games, leaderboard.
	var right []string
	type sideRow struct{ y, id int }
	var sideRows []sideRow
	right = append(right, "", "")
	subs := map[string]string{"play against bots": "easy, normal or hard", "pass and play": "two players, one device",
		"challenge a friend": "a private match by code", "how to play": "a guided first match"}
	for i, it := range m.items {
		if it.kind != "side" {
			continue
		}
		name := capital(it.label)
		mark := "  "
		style := st.Plain
		if i == m.sel {
			mark, style = st.Accent.Render("> "), st.Accent
		}
		sideRows = append(sideRows, sideRow{len(right), i})
		right = append(right, mark+style.Render(name))
		sideRows = append(sideRows, sideRow{len(right), i})
		right = append(right, "  "+st.Dim.Render(subs[it.label]))
	}
	right = append(right, "", st.Title.Render("GAMES"))
	right = append(right, m.yourGames(a, rightW)...)
	right = append(right, "", st.Title.Render("LEADERBOARD"))
	right = append(right, m.leaderboard(a, rightW)...)
	for _, sr := range sideRows {
		a.clickBox(topY+sr.y, x0+leftW+4, x0+bw, sr.id)
	}

	for i := 0; i < len(left) || i < len(right); i++ {
		l, r := "", ""
		if i < len(left) {
			l = left[i]
		}
		if i < len(right) {
			r = right[i]
		}
		row(fitPad(l, leftW) + "    " + r)
	}

	// The bottom row: the rest of the menu.
	row(st.Border.Render(strings.Repeat("─", bw)))
	line := st.Dim.Render("more  ")
	x := x0 + render.Width(line)
	y := len(out)
	for i, it := range m.items {
		if it.kind != "more" {
			continue
		}
		label := " " + it.label + " "
		w := render.Width(label)
		if i == m.sel {
			line += st.Key.Render(label)
		} else {
			line += label
		}
		a.clickBox(y, x, x+w, i)
		line += " "
		x += w + 1
	}
	row(line)
	row("")
	row(st.Dim.Render("arrows move  enter select  c rated or casual  w watch  L leaderboard  r rejoin  q quit"))
	for len(out) > a.h {
		out = out[:a.h]
	}
	return strings.Join(out, "\n")
}

// tile is one clock as a boxed tile: the clock, its category, and your
// rating there (or rated / casual).
func (m *menuScreen) tile(a *App, it menuItem, sel bool, w, h int, guest bool) []string {
	st := &a.st
	border := st.Border
	if sel {
		border = st.Accent
	}
	cat := strings.TrimPrefix(it.label, it.clock+" ")
	foot := "casual"
	if m.rated && !guest {
		foot = "rated"
		if a.net != nil {
			if r, ok := a.net.welcome.Ratings[cat]; ok {
				foot = fmt.Sprintf("%d · %d-%d", int(r.Rating+0.5), r.Wins, r.Games-r.Wins)
			}
		}
	}
	center := func(s string, style func(...string) string) string {
		n := render.Width(s)
		l := (w - 2 - n) / 2
		if l < 0 {
			l = 0
		}
		return border.Render("│") + strings.Repeat(" ", l) + style(s) + strings.Repeat(" ", maxInt(0, w-2-n-l)) + border.Render("│")
	}
	title := st.Plain.Bold(true).Render
	if sel {
		title = st.Accent.Bold(true).Render
	}
	lines := []string{border.Render("┌" + strings.Repeat("─", w-2) + "┐")}
	body := []string{it.clock, capital(cat), foot}
	styles := []func(...string) string{title, st.Plain.Render, st.Dim.Render}
	for k := 0; k < h-2; k++ {
		if k < len(body) {
			lines = append(lines, center(body[k], styles[k]))
		} else {
			lines = append(lines, center("", st.Plain.Render))
		}
	}
	return append(lines, border.Render("└"+strings.Repeat("─", w-2)+"┘"))
}

// yourGames is the side column's account panel: a match to rejoin, your
// last games (who you played and as whom), the ratings.
func (m *menuScreen) yourGames(a *App, w int) []string {
	st := &a.st
	if a.net == nil {
		return []string{st.Dim.Render("Offline.")}
	}
	var out []string
	welcome := a.net.welcome
	if welcome.InMatch != "" {
		out = append(out, st.Warn.Render("match in progress")+st.Dim.Render("  r rejoin"))
	}
	for _, lm := range welcome.Live {
		state := st.Dim.Render("waiting on them")
		if lm.YourTurn {
			state = st.Warn.Render("your move") + st.Dim.Render("  d open")
		}
		out = append(out, "daily vs "+truncate(lm.Versus, 14)+"  "+state)
	}
	for i, h := range m.history {
		if i >= 3 {
			break
		}
		res := outcome(h)
		as := ""
		for _, p := range h.Players {
			if p.Slot == h.You {
				as = heroName(a, p.Hero)
			}
		}
		line := resStyle(st, res).Render(fmt.Sprintf("%-4s", res)) + " vs " + truncate(opponents(h), 16)
		out = append(out, line, "     "+st.Dim.Render("as "+as+" · "+timeLabel(a, h.Time)))
	}
	if len(welcome.Ratings) > 0 {
		var rs []string
		for _, cat := range ladderModes {
			if r, ok := welcome.Ratings[cat]; ok {
				rs = append(rs, fmt.Sprintf("%s %d", cat, int(r.Rating+0.5)))
			}
		}
		out = append(out, st.Dim.Render(strings.Join(rs, " · ")))
	}
	if len(out) == 0 {
		out = append(out, st.Dim.Render("No games yet."))
	}
	return out
}

// leaderboard is the top three of each category, one line each (the
// web lists them as columns); a dash where no one is.
func (m *menuScreen) leaderboard(a *App, w int) []string {
	st := &a.st
	out := make([]string, 0, len(ladderModes))
	for _, mode := range ladderModes {
		rows := m.ladders[mode]
		var cells []string
		for i := 0; i < 3; i++ {
			if i >= len(rows) {
				cells = append(cells, st.Dim.Render("—"))
				continue
			}
			cells = append(cells, truncate(rows[i].Name, 5)+" "+st.Plain.Bold(true).Render(fmt.Sprint(int(rows[i].Rating.Rating+0.5))))
		}
		out = append(out, st.Accent.Render(catGlyph(mode))+" "+st.Dim.Render(fmt.Sprintf("%-10s", capital(mode)))+strings.Join(cells, " "))
	}
	return out
}

// catGlyph is a rating category's mark, the web's icon in a terminal: a
// clock face that fills as the clock lengthens.
func catGlyph(cat string) string {
	switch cat {
	case "bullet":
		return "◔"
	case "blitz":
		return "◑"
	case "rapid":
		return "◕"
	case "long-haul":
		return "●"
	case "daily":
		return "◎"
	}
	return "·"
}

// ladderModes are the rating categories, in the order every list shows them.
var ladderModes = []string{"bullet", "blitz", "rapid", "long-haul", "daily"}

// outcome is a finished match from your seat: won, lost or draw.
func outcome(h proto.HistoryMatch) string {
	if h.Winner < 0 || h.You < 0 {
		return "draw"
	}
	for _, p := range h.Players {
		if p.Slot == h.You && p.Team == h.Winner {
			return "won"
		}
	}
	return "lost"
}

// opponents names the other side (everyone but you, if you did not play).
func opponents(h proto.HistoryMatch) string {
	team := -1
	for _, p := range h.Players {
		if p.Slot == h.You {
			team = p.Team
		}
	}
	var out []string
	for _, p := range h.Players {
		if p.Slot != h.You && (team < 0 || p.Team != team) {
			out = append(out, p.Name)
		}
	}
	if len(out) == 0 {
		return "opponent"
	}
	return strings.Join(out, ", ")
}

// resStyle colours a result.
func resStyle(st *render.Styles, res string) lipgloss.Style {
	switch res {
	case "won":
		return st.Good
	case "lost":
		return st.Danger
	}
	return st.Plain
}

// heroName is a commander's display name.
func heroName(a *App, id string) string {
	if h, ok := a.c.Heroes[id]; ok {
		return h.Name
	}
	return id
}

// timeLabel names a match's time control as the web does:
// "20s · blitz · casual".
func timeLabel(a *App, id string) string {
	base := strings.TrimSuffix(id, "-casual")
	out := base
	if c := category(a, id); c != "other" {
		out += " · " + c
	}
	if base != id {
		return out + " · casual"
	}
	return out
}

func capital(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// fitPad pads (or clips) a styled string to exactly w columns.
func fitPad(s string, w int) string {
	n := render.Width(s)
	if n >= w {
		return clip(s, w)
	}
	return s + strings.Repeat(" ", w-n)
}
