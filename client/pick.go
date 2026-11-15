package client

import (
	"fmt"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"rfog/bots"
	"rfog/engine"
	"rfog/render"
)

type matchKind int

const (
	matchBots matchKind = iota
	matchHotseat
)

// pickScreen is the pick before a local match, as the web app's: a slot
// for every seat along the top (your team left, theirs right), all ten
// commanders in a grid below, the chosen one's detail and Lock in. Once
// the humans have locked in, the bots' seats fill one by one and the
// match starts. The bot level, mode and length are set here too.
type pickScreen struct {
	kind    matchKind
	heroes  []string
	sel     int
	picks   []string // chosen so far
	level   string
	levels  []string
	lvlSel  int
	modes   []string // 1v1 first; team modes against bots only
	modeSel int
	lengths []string // standard first
	lenSel  int
	// lineup is every seat's hero once the humans have locked in, and
	// shown how many of them are on the board so far; seed starts the
	// match they fill.
	lineup []string
	shown  int
	seed   uint64
}

// pickFillMs is the beat at which the bots' seats fill.
const pickFillMs = 350

func newPickScreen(a *App, kind matchKind) *pickScreen {
	p := &pickScreen{kind: kind, heroes: a.c.HeroIDs(), levels: []string{"easy", "normal", "hard"}, level: a.set.BotLevel,
		modes: []string{"1v1"}, lengths: a.c.LengthIDs()}
	if kind == matchBots {
		p.modes = nil
		for m := range a.c.Rules.Modes {
			p.modes = append(p.modes, m)
		}
		sort.Strings(p.modes) // 1v1 2v2 ... 5v5
	}
	for i, h := range p.heroes {
		if h == a.set.Hero {
			p.sel = i
		}
	}
	for i, l := range p.levels {
		if l == p.level {
			p.lvlSel = i
		}
	}
	return p
}

// choose locks in the highlighted hero for whoever is picking. Once every
// human seat is locked the bots' seats start to fill. Shared by the
// keyboard and a tap.
func (p *pickScreen) choose(a *App) (screen, tea.Cmd) {
	p.picks = append(p.picks, p.heroes[p.sel])
	if len(p.picks) == 1 {
		a.set.Hero = p.heroes[p.sel]
		a.set.BotLevel = p.level
		a.saveSettings()
	}
	if p.kind == matchHotseat {
		if len(p.picks) < 2 {
			return p, nil
		}
		return p.start(a)
	}
	p.seed = uint64(time.Now().UnixNano())
	heroes, err := bots.Lineup(a.c, p.modes[p.modeSel], p.picks[:1], p.seed)
	if err != nil {
		return newMenuScreen(), nil
	}
	p.lineup, p.shown = heroes, 1
	return p, tick(pickFillMs)
}

// start begins the match the pick made.
func (p *pickScreen) start(a *App) (screen, tea.Cmd) {
	var lm *localMatch
	var err error
	length := p.lengths[p.lenSel]
	if p.kind == matchHotseat {
		lm, err = newLocalMatch(a.c, engine.Setup{ID: "hotseat", Mode: "1v1", Length: length, Seed: uint64(time.Now().UnixNano()), TimeControl: "bots",
			Players: []engine.SetupPlayer{{Name: "player 1", Hero: p.picks[0]}, {Name: "player 2", Hero: p.picks[1]}}},
			[2]bool{false, false}, p.level)
	} else {
		// Bots take every other seat, heroes from the seed: no repeat
		// within a team, mirror across teams allowed.
		st := engine.Setup{ID: "bots", Mode: p.modes[p.modeSel], Length: length, Seed: p.seed, TimeControl: "bots"}
		for i, h := range p.lineup {
			name := "bot " + h
			if i == 0 {
				name = a.set.Name
			}
			st.Players = append(st.Players, engine.SetupPlayer{Name: name, Hero: h})
		}
		lm, err = newSeatedMatch(a.c, st, []bool{false}, p.level)
	}
	if err != nil {
		return newMenuScreen(), nil
	}
	return newMatchScreen(a, lm), tick(50)
}

func (p *pickScreen) update(a *App, msg tea.Msg) (screen, tea.Cmd) {
	if p.lineup != nil {
		// The bots are taking their seats: one a beat, a beat to look at
		// the full row, then the match. Any key goes straight to it.
		switch msg.(type) {
		case tickMsg:
			if p.shown++; p.shown <= len(p.lineup) {
				return p, tick(pickFillMs * map[bool]int{false: 1, true: 2}[p.shown == len(p.lineup)])
			}
			return p.start(a)
		case tea.KeyMsg:
			return p.start(a)
		}
		return p, nil
	}
	if mm, ok := msg.(tea.MouseMsg); ok {
		if id, hit, act := a.pointer(mm, p.sel); hit {
			if id == pickLock {
				if act || mm.Action == tea.MouseActionPress {
					return p.choose(a)
				}
				return p, nil
			}
			p.sel = id
			if act {
				return p.choose(a)
			}
		}
		return p, nil
	}
	k, ok := msg.(tea.KeyMsg)
	if !ok {
		return p, nil
	}
	if gridMove(k, &p.sel, len(p.heroes)) {
		return p, nil
	}
	switch {
	case isKey(k, "b") && p.kind == matchBots:
		p.lvlSel = (p.lvlSel + 1) % len(p.levels)
		p.level = p.levels[p.lvlSel]
	case isKey(k, "m"):
		p.modeSel = (p.modeSel + 1) % len(p.modes)
	case isKey(k, "t"):
		p.lenSel = (p.lenSel + 1) % len(p.lengths)
	case isKey(k, "enter", " "):
		return p.choose(a)
	case isKey(k, "q", "esc"):
		return newMenuScreen(), nil
	}
	return p, nil
}

// gridCols is how many commanders the pick grid holds across.
const gridCols = 5

// gridMove moves a pick grid's cursor with the arrows or hjkl, reporting
// whether the key was one of them.
func gridMove(k tea.KeyMsg, sel *int, n int) bool {
	switch {
	case isKey(k, "l", "right"):
		*sel = (*sel + 1) % n
	case isKey(k, "h", "left"):
		*sel = (*sel + n - 1) % n
	case isKey(k, "j", "down"):
		*sel = (*sel + gridCols) % (((n + gridCols - 1) / gridCols) * gridCols)
		if *sel >= n {
			*sel = n - 1
		}
	case isKey(k, "k", "up"):
		rows := (n + gridCols - 1) / gridCols
		*sel = (*sel - gridCols + rows*gridCols) % (rows * gridCols)
		if *sel >= n {
			*sel -= gridCols
		}
	default:
		return false
	}
	return true
}

func (p *pickScreen) view(a *App) string {
	pm := pickModel{heroes: p.heroes, sel: p.sel, button: "Lock in"}
	mode, length := p.modes[p.modeSel], p.lengths[p.lenSel]
	per := a.c.Rules.Modes[mode].PlayersPerTeam
	choice := func(label, v, key string) string {
		return fmt.Sprintf("%s %s%s%s %s", label, a.st.Dim.Render("‹"), a.st.Accent.Render(v), a.st.Dim.Render("›"), a.st.Key.Render(key))
	}
	if p.kind == matchHotseat {
		pm.title = "pass and play · " + length
		for i, name := range []string{"player 1", "player 2"} {
			s := pickSeat{name: name, team: i, active: len(p.picks) == i}
			if i < len(p.picks) {
				s.hero = p.picks[i]
			}
			pm.seats = append(pm.seats, s)
		}
		pm.status = fmt.Sprintf("player %d: pick a commander", minInt(len(p.picks), 1)+1)
		pm.options = []string{choice("length", length, "t")}
		pm.keys = "arrows choose  enter lock in  t length  esc back"
	} else {
		pm.title = "vs bots · " + mode + " · " + length
		for i := 0; i < 2*per; i++ {
			s := pickSeat{name: "bot", team: i / per, you: i == 0}
			if i == 0 {
				s.name = "you"
			}
			if p.lineup != nil && i < p.shown {
				s.hero = p.lineup[i]
			}
			s.active = p.lineup == nil && i == 0 || p.lineup != nil && i == p.shown
			pm.seats = append(pm.seats, s)
		}
		pm.status = "you: pick a commander"
		pm.options = []string{choice("bot level", p.level, "b") + "   " + choice("mode", mode, "m") + "   " + choice("length", length, "t")}
		pm.keys = "arrows choose  enter lock in  b bot level  m mode  t length  esc back"
	}
	if p.lineup != nil {
		pm.status, pm.button = "the bots are taking their seats", ""
		pm.keys = "any key: start now"
	}
	// What the mode and length mean, when they are not the plain 1v1.
	var notes []string
	if per > 1 {
		allies := "a bot ally"
		if per > 2 {
			allies = fmt.Sprintf("%d bot allies", per-1)
		}
		notes = append(notes, fmt.Sprintf("experimental: you and %s against %d bots", allies, per))
	}
	if length != engine.LengthStandard {
		turns, win, _ := a.c.MatchLength(mode, length)
		notes = append(notes, fmt.Sprintf("%d turns, first to %d", turns, win))
	}
	if len(notes) > 0 {
		pm.options = append(pm.options, a.st.Dim.Render(strings.Join(notes, "; ")))
	}
	return a.pickView(pm)
}

// pickSeat is one seat on the pick screen's top row.
type pickSeat struct {
	name, hero  string // hero is "" until picked
	team        int    // 0 the viewer's side (left), 1 the other (right)
	you, active bool   // the viewer's seat; the seat picking now
}

// pickModel is what the pick screen shows, from a local pick or an
// online draft.
type pickModel struct {
	title, status string
	seats         []pickSeat
	heroes        []string
	notes         map[string]string // a hero's standing: banned, your pick, ...
	sel           int
	button        string // the action enter takes ("" none)
	options       []string
	keys, err     string
}

// centred is s cut to w columns and centred in exactly w.
func centred(s string, w int) string {
	s = truncate(s, w)
	return fit(strings.Repeat(" ", maxInt(0, (w-render.Width(s))/2))+s, w)
}

// pickLock is the Lock in button's click id (heroes are 0 to 9).
const pickLock = 1000

// pickView draws the pick screen: the seats along the top, the grid of
// commanders, the chosen one, Lock in. With room the seats and the grid
// show the figures; on a small screen they are names.
func (a *App) pickView(pm pickModel) string {
	st := &a.st
	figures := a.artTier() != "t0" && a.w >= 70 && a.h >= 30
	var out []string
	row := func(l string) { out = append(out, l) }
	row(st.Title.Render(pm.title))
	row("")
	// Seats: yours on the left in your colour, theirs on the right.
	var left, right []pickSeat
	for _, s := range pm.seats {
		if s.team == 0 {
			left = append(left, s)
		} else {
			right = append(right, s)
		}
	}
	// seatCard is a seat in a box in its side's colour (brighter while it
	// picks): the commander's figure once picked, the player, the hero.
	seatCard := func(s pickSeat, sw int, fig bool) []string {
		style := st.TeamA
		if s.team != 0 {
			style = st.TeamB
		}
		border := st.Border
		if s.active {
			border = style
		}
		inner := sw - 2
		hero := "?"
		if s.hero != "" {
			hero = a.c.Heroes[s.hero].Name
		}
		side := func(l string) string { return border.Render(a.g.V) + l + border.Render(a.g.V) }
		card := []string{border.Render(a.g.TL + strings.Repeat(a.g.H, inner) + a.g.TR)}
		if fig {
			fc := minInt(inner-2, 8)
			var f []string
			if s.hero != "" {
				f = a.figureLines("hero_"+s.hero, s.team, "", "", fc, 3, false, 0, 0)
			}
			for k := 0; k < 3; k++ {
				l := strings.Repeat(" ", inner)
				if k < len(f) {
					l = strings.Repeat(" ", (inner-fc)/2) + f[k] + strings.Repeat(" ", inner-fc-(inner-fc)/2)
				} else if k == 1 && s.hero == "" {
					l = st.Dim.Render(centred("?", inner))
				}
				card = append(card, side(l))
			}
		}
		card = append(card, side(style.Render(centred(s.name, inner))), side(st.Plain.Bold(true).Render(centred(hero, inner))))
		return append(card, border.Render(a.g.BL+strings.Repeat(a.g.H, inner)+a.g.BR))
	}
	if figures {
		n := maxInt(len(left), len(right))
		sw := minInt(16, (a.w-8)/(2*n))
		fig := a.h >= 36
		var rows []string
		add := func(ss []pickSeat) {
			for _, s := range ss {
				for k, l := range seatCard(s, sw, fig) {
					if k >= len(rows) {
						rows = append(rows, "")
					}
					rows[k] += l
				}
			}
		}
		add(left)
		for k := range rows {
			rows[k] += map[bool]string{true: st.Dim.Render(" vs "), false: "    "}[k == len(rows)/2]
		}
		add(right)
		for _, l := range rows {
			row(l)
		}
	} else {
		var parts []string
		for _, ss := range [][]pickSeat{left, right} {
			var names []string
			for _, s := range ss {
				hero := "?"
				if s.hero != "" {
					hero = a.c.Heroes[s.hero].Name
				}
				name := s.name
				if s.active {
					name = "▸" + name
				}
				names = append(names, name+" "+hero)
			}
			style := st.TeamA
			if len(parts) > 0 {
				style = st.TeamB
			}
			parts = append(parts, style.Render(strings.Join(names, ", ")))
		}
		row(strings.Join(parts, st.Dim.Render("  vs  ")))
	}
	row("")
	// The grid of commanders.
	cw := minInt(18, a.w/gridCols)
	gridTop := len(out)
	fr := 3
	if a.h >= 36 {
		fr = 4
	}
	for r0 := 0; r0 < len(pm.heroes); r0 += gridCols {
		var rows []string
		for i := r0; i < minInt(r0+gridCols, len(pm.heroes)); i++ {
			id := pm.heroes[i]
			h := a.c.Heroes[id]
			note := pm.notes[id]
			name, role := h.Name, h.Role
			if note != "" {
				role = note
			}
			nameStyle, border := st.Plain.Bold(true), st.Border
			if i == pm.sel {
				nameStyle, border = st.Accent.Bold(true), st.Accent
			} else if note != "" {
				nameStyle = st.Dim
			}
			var card []string
			if figures {
				inner := cw - 2
				fig := a.figureLines("hero_"+id, 0, "", "", minInt(inner-2, 10), fr, note != "", 0, 0)
				card = append(card, border.Render(a.g.TL+strings.Repeat(a.g.H, inner)+a.g.TR))
				for k := 0; k < fr; k++ {
					l := strings.Repeat(" ", inner)
					if k < len(fig) {
						fw := minInt(inner-2, 10)
						l = strings.Repeat(" ", (inner-fw)/2) + fig[k] + strings.Repeat(" ", inner-fw-(inner-fw)/2)
					}
					card = append(card, border.Render(a.g.V)+l+border.Render(a.g.V))
				}
				card = append(card, border.Render(a.g.V)+nameStyle.Render(centred(name, inner))+border.Render(a.g.V))
				card = append(card, border.Render(a.g.V)+st.Dim.Render(centred(role, inner))+border.Render(a.g.V))
				card = append(card, border.Render(a.g.BL+strings.Repeat(a.g.H, inner)+a.g.BR))
			} else {
				mark := " "
				if i == pm.sel {
					mark = st.Accent.Render("▸")
				}
				card = append(card, mark+nameStyle.Render(fit(name, cw-1)))
				if a.h >= 22 {
					card = append(card, " "+st.Dim.Render(fit(truncate(role, cw-2), cw-1)))
				}
			}
			for k, l := range card {
				if k >= len(rows) {
					rows = append(rows, "")
				}
				rows[k] += l
			}
		}
		for _, l := range rows {
			row(l)
		}
	}
	gridRows := len(out) - gridTop
	// The chosen one.
	h := a.c.Heroes[pm.heroes[pm.sel%len(pm.heroes)]]
	row("")
	row(st.Accent.Bold(true).Render(h.Name) + "  " + h.Class + " · " + st.Dim.Render(h.Role))
	row(st.Dim.Render(fmt.Sprintf("HP %d  MV %d  JMP %d  RNG %d  ATK %d  DEF %d  INI %d  VIS %d", h.HP, h.MV, h.JMP, h.RNG, h.ATK, h.DEF, h.INI, h.VIS)))
	abilities := len(out)
	for _, key := range []string{"q", "w", "e", "r"} {
		ab := a.c.Abilities[h.Abilities[key]]
		row(fmt.Sprintf("%s  %-12s %s", st.Key.Render(strings.ToUpper(key)), ab.Name, st.Dim.Render(describeAbility(a.c, ab))))
	}
	row("")
	buttonRow := len(out)
	status := st.Dim.Render(pm.status)
	if pm.button != "" {
		status = st.Key.Reverse(true).Render(" "+pm.button+" ") + "  " + status
	}
	row(status)
	for _, o := range pm.options {
		row(o)
	}
	if pm.err != "" {
		row(st.Danger.Render(pm.err))
	}
	row(st.Dim.Render(pm.keys))
	// Lay it out: one block, centred; on a screen too short for all of
	// it the abilities go first, the last first, then the blank rows.
	for n := 4; len(out) > a.h && n > 0; n-- {
		out = append(out[:abilities+n-1], out[abilities+n:]...)
		buttonRow--
	}
	for _, y := range []int{buttonRow - 1, gridTop + gridRows, gridTop - 1, 1} {
		if len(out) > a.h && y >= 0 && y < len(out) && out[y] == "" {
			out = append(out[:y], out[y+1:]...)
			if y < buttonRow {
				buttonRow--
			}
			if y < gridTop {
				gridTop--
			}
		}
	}
	w := 0
	for _, l := range out {
		w = maxInt(w, render.Width(l))
	}
	w = minInt(w, a.w)
	x0 := (a.w - w) / 2
	y0 := maxInt(0, (a.h-len(out))/2)
	lines := make([]string, 0, a.h)
	for i := 0; i < y0; i++ {
		lines = append(lines, "")
	}
	for _, l := range out {
		lines = append(lines, strings.Repeat(" ", x0)+clip(l, a.w-x0))
	}
	if len(lines) > a.h {
		lines = lines[:a.h]
	}
	// Taps: a card picks its commander, the button locks in.
	cardH := gridRows / maxInt(1, (len(pm.heroes)+gridCols-1)/gridCols)
	for i := range pm.heroes {
		y := y0 + gridTop + (i/gridCols)*cardH
		for k := 0; k < cardH; k++ {
			a.clickBox(y+k, x0+(i%gridCols)*cw, x0+(i%gridCols+1)*cw, i)
		}
	}
	if pm.button != "" {
		a.clickBox(y0+buttonRow, x0, x0+render.Width(pm.button)+2, pickLock)
	}
	return a.onBackdrop(strings.Join(lines, "\n"), true)
}

// describeAbility gives a terse, data-derived summary of an ability.
func describeAbility(c *engine.Content, ab engine.AbilityDef) string {
	var parts []string
	for _, e := range ab.Effects {
		switch e.Kind {
		case "damage":
			parts = append(parts, fmt.Sprintf("dmg %d", e.N))
		case "heal":
			parts = append(parts, fmt.Sprintf("heal %d", e.N))
		case "push":
			parts = append(parts, fmt.Sprintf("push %d", e.Dist))
		case "pull":
			parts = append(parts, fmt.Sprintf("pull %d", e.Dist))
		case "shield":
			parts = append(parts, fmt.Sprintf("shield %d/%dt", e.N, e.Turns))
		case "reveal":
			parts = append(parts, fmt.Sprintf("reveal r%d/%dt", radius(e, ab), e.Turns))
		case "smoke":
			parts = append(parts, fmt.Sprintf("smoke r%d/%dt", radius(e, ab), e.Turns))
		case "slow":
			parts = append(parts, fmt.Sprintf("slow %d/%dt", e.MV, e.Turns))
		case "root", "silence", "cloak", "taunt":
			parts = append(parts, fmt.Sprintf("%s %dt", e.Kind, e.Turns))
		case "stat":
			parts = append(parts, fmt.Sprintf("%s%+d/%dt", strings.ToUpper(e.Stat), e.Delta, e.Turns))
		case "mark":
			parts = append(parts, fmt.Sprintf("mark +%d/%dt", e.Dice, e.Turns))
		case "spawn":
			parts = append(parts, fmt.Sprintf("spawn %dx%s", maxInt(e.Count, 1), e.Unit))
		case "overwatch":
			parts = append(parts, fmt.Sprintf("overwatch rng%d x%d", e.RNG, e.Shots))
		case "teleport":
			if e.Who == "target" {
				parts = append(parts, "teleport ally")
			} else {
				parts = append(parts, "teleport")
			}
		case "approach":
			parts = append(parts, fmt.Sprintf("dash %d", e.Dist))
		case "strike":
			parts = append(parts, "attack")
		case "sacrifice":
			parts = append(parts, "sacrifice")
		}
	}
	tgt := ab.Target
	if ab.Range > 0 {
		tgt += fmt.Sprintf(" r%d", ab.Range)
	}
	if ab.Area > 0 {
		tgt += fmt.Sprintf(" %dx%d", ab.Area*2+1, ab.Area*2+1)
	}
	d := ""
	if ab.Delay > 0 {
		d = fmt.Sprintf(" delay %d", ab.Delay)
	}
	return fmt.Sprintf("%s;%s cd%d: %s", tgt, d, ab.Cooldown, strings.Join(parts, ", "))
}

func radius(e engine.EffectDef, ab engine.AbilityDef) int {
	if e.Radius != nil {
		return *e.Radius
	}
	return ab.Area
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
