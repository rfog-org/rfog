package client

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"rfog/proto"
)

// draftScreen runs the ban/pick phase of an online match. In team modes
// the two teams are listed, bans alternate per team and picks snake; a
// hero a teammate already took is greyed out.
type draftScreen struct {
	rm    *remoteMatch
	st    proto.DraftState
	sel   int
	err   string
	ready bool // play has started; waiting for the first TurnStart
}

func newDraftScreen(a *App, rm *remoteMatch) *draftScreen {
	d := &draftScreen{rm: rm}
	for i, h := range a.c.HeroIDs() {
		if h == a.set.Hero {
			d.sel = i
		}
	}
	return d
}

func (d *draftScreen) update(a *App, msg tea.Msg) (screen, tea.Cmd) {
	switch t := msg.(type) {
	case tickMsg:
		return d, tick(1000)
	case netMsg:
		if t.Err != nil {
			return newOnlineScreen(a), nil
		}
		switch t.T {
		case proto.TDraftState:
			var st proto.DraftState
			_ = t.F.As(&st)
			if st.Match != d.rm.id {
				return d, nil
			}
			d.st = st
			if d.st.Kind == "done" {
				d.ready = true
			}
			return d, tick(1000)
		case proto.TMatchFound:
			// Sent again when play begins (names now include bot heroes).
			var mf proto.MatchFound
			_ = t.F.As(&mf)
			if mf.Match == d.rm.id {
				d.rm.players = mf.Players
				d.ready = true
			}
		case proto.TError:
			var e proto.Error
			_ = t.F.As(&e)
			d.err = e.Msg
		}
		return d, nil
	case turnStartMsg:
		m := newMatchScreen(a, d.rm)
		return m.beginOrders(a, &t)
	case matchEndMsg:
		return onlineBack(a), nil
	case tea.KeyMsg:
		heroes := d.st.Heroes
		if len(heroes) == 0 {
			heroes = a.c.HeroIDs()
		}
		switch {
		case isKey(t, "j", "down"):
			d.sel = (d.sel + 1) % len(heroes)
		case isKey(t, "k", "up"):
			d.sel = (d.sel + len(heroes) - 1) % len(heroes)
		case isKey(t, "enter", " "):
			if len(d.st.Heroes) > 0 && d.st.Turn == d.rm.you && d.st.Kind != "done" {
				h := heroes[d.sel]
				if err := d.rm.r.send(proto.TDraft, proto.DraftAction{Match: d.rm.id, Kind: d.st.Kind, Hero: h}); err != nil {
					d.err = err.Error()
				}
			}
		case isKey(t, "Q"):
			d.rm.Leave()
			a.rm = nil
			return onlineBack(a), nil
		}
	}
	return d, nil
}

// teamOf returns the team of a slot from the seating.
func (d *draftScreen) teamOf(slot int) int {
	for _, p := range d.rm.players {
		if p.Slot == slot {
			return p.Team
		}
	}
	return -1
}

func (d *draftScreen) view(a *App) string {
	st := &a.st
	var lines []string
	lines = append(lines, st.Title.Render("draft  "+d.rm.mode+" "+d.rm.timeCtl), "")
	you := d.teamOf(d.rm.you)
	var mine, theirs []string
	for _, p := range d.rm.players {
		n := p.Name
		if h, ok := d.st.Picks[p.Slot]; ok {
			n += " (" + a.c.Heroes[h].Name + ")"
		}
		if p.Slot == d.rm.you {
			n = st.TeamA.Render(n + " *")
		} else if p.Team == you {
			n = st.TeamA.Render(n)
		} else {
			n = st.TeamB.Render(n)
		}
		if p.Team == you {
			mine = append(mine, n)
		} else {
			theirs = append(theirs, n)
		}
	}
	lines = append(lines, strings.Join(mine, ", ")+"  vs  "+strings.Join(theirs, ", "), "")
	if d.ready {
		lines = append(lines, st.Dim.Render("deploying..."))
		return a.centered(lines)
	}
	if len(d.st.Heroes) == 0 {
		lines = append(lines, st.Dim.Render("waiting for the draft..."))
		return a.centered(lines)
	}
	left := time.Until(d.st.Deadline)
	if left < 0 {
		left = 0
	}
	who := "them"
	switch {
	case d.st.Turn == d.rm.you:
		who = "you"
	case d.teamOf(d.st.Turn) == you:
		who = "ally"
	}
	lines = append(lines, fmt.Sprintf("%s: %s   %s", st.Accent.Render(who), d.st.Kind, st.Dim.Render(shortDuration(left))), "")
	banned := map[string]bool{}
	for _, b := range d.st.Banned {
		banned[b] = true
	}
	for i, id := range d.st.Heroes {
		h := a.c.Heroes[id]
		row := fmt.Sprintf("%-6s %-9s %s", h.Name, h.Class, h.Role)
		switch {
		case banned[id]:
			row = st.Dim.Render(row + "   banned")
		case d.st.Picks[d.rm.you] == id:
			row = st.TeamA.Render(row + "   your pick")
		case d.pickedBy(id, you, true):
			row = st.Dim.Render(row + "   ally's pick")
		case d.pickedBy(id, you, false):
			row = st.TeamB.Render(row + "   their pick")
		}
		if i == d.sel {
			lines = append(lines, st.Accent.Render("> ")+row)
		} else {
			lines = append(lines, "  "+row)
		}
	}
	hid := d.st.Heroes[d.sel%len(d.st.Heroes)]
	h := a.c.Heroes[hid]
	lines = append(lines, "")
	for _, key := range []string{"q", "w", "e", "r"} {
		ab := a.c.Abilities[h.Abilities[key]]
		lines = append(lines, fmt.Sprintf("  %s  %-14s %s", st.Key.Render(strings.ToUpper(key)), ab.Name, describeAbility(a.c, ab)))
	}
	if d.err != "" {
		lines = append(lines, "", st.Danger.Render(d.err))
	}
	lines = append(lines, "", st.Dim.Render("j/k choose  enter "+d.st.Kind+"  Q leave"))
	// Portrait beside the list when there is room (the list block starts
	// at the hero rows; the portrait lines up with it).
	if a.w >= 100 {
		pf := a.portrait(hid).Render(a.artStyler())
		start := 6 // title, blank, names, blank, whose turn, blank
		width := 0
		for _, l := range lines {
			if w := lipglossWidthOf(l); w > width {
				width = w
			}
		}
		for i := range lines {
			l := fit(lines[i], width)
			if j := i - start; j >= 0 && j < len(pf) {
				l += "   " + pf[j]
			}
			lines[i] = l
		}
	}
	return a.centered(lines)
}

// pickedBy reports whether hero was picked by someone on (ally=true) or
// not on (ally=false) the viewer's team, other than the viewer.
func (d *draftScreen) pickedBy(hero string, you int, ally bool) bool {
	for slot, h := range d.st.Picks {
		if h != hero || slot == d.rm.you {
			continue
		}
		if (d.teamOf(slot) == you) == ally {
			return true
		}
	}
	return false
}

// shortDuration renders a countdown: 47s, 12m, 23h.
func shortDuration(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	default:
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
}
