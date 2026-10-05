package client

import (
	"fmt"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// rosterScreen browses heroes and units straight from the data files.
type rosterScreen struct {
	entries []string // hero ids then unit ids
	heroes  int
	sel     int
}

func newRosterScreen(a *App) *rosterScreen {
	r := &rosterScreen{}
	r.entries = append(r.entries, a.c.HeroIDs()...)
	r.heroes = len(r.entries)
	var units []string
	for id := range a.c.Units {
		units = append(units, id)
	}
	sort.Strings(units)
	r.entries = append(r.entries, units...)
	return r
}

func (r *rosterScreen) update(a *App, msg tea.Msg) (screen, tea.Cmd) {
	if mm, ok := msg.(tea.MouseMsg); ok {
		if id, hit, _ := a.pointer(mm, r.sel); hit {
			r.sel = id
		}
		return r, nil
	}
	k, ok := msg.(tea.KeyMsg)
	if !ok {
		return r, nil
	}
	switch {
	case isKey(k, "j", "down", "tab"):
		r.sel = (r.sel + 1) % len(r.entries)
	case isKey(k, "k", "up", "shift+tab"):
		r.sel = (r.sel + len(r.entries) - 1) % len(r.entries)
	case isKey(k, "q", "esc", "enter"):
		return newMenuScreen(), nil
	}
	return r, nil
}

func (r *rosterScreen) view(a *App) string {
	var left []string
	left = append(left, a.st.Title.Render("roster"), "")
	// On a small screen the list scrolls: a window around the selection.
	first, last := 0, len(r.entries)
	if room := a.h - 6; room > 4 && len(r.entries)+1 > room {
		first = r.sel - room/2
		if first < 0 {
			first = 0
		}
		last = first + room - 1
		if last > len(r.entries) {
			last, first = len(r.entries), len(r.entries)-room+1
		}
	}
	for i, id := range r.entries {
		if i < first || i >= last {
			continue
		}
		name := id
		if i < r.heroes {
			name = a.c.Heroes[id].Name + " (" + a.c.Heroes[id].Class + ")"
		} else {
			name = a.c.Units[id].Name
		}
		if i == r.heroes && i != first {
			left = append(left, "")
		}
		a.clickRow(len(left), i)
		if i == r.sel {
			left = append(left, a.st.Accent.Render("> "+name))
		} else {
			left = append(left, "  "+name)
		}
	}
	var right []string
	id := r.entries[r.sel]
	if r.sel < r.heroes {
		h := a.c.Heroes[id]
		if a.h >= 24 {
			right = append(right, a.portrait(id).Render(a.artStyler())...)
		}
		right = append(right, a.st.Title.Render(h.Name)+"  "+a.st.Dim.Render(h.Class+" · "+h.Role))
		right = append(right, fmt.Sprintf("HP %d  MV %d  JMP %d  RNG %d", h.HP, h.MV, h.JMP, h.RNG),
			fmt.Sprintf("ATK %d  DEF %d  INI %d  VIS %d", h.ATK, h.DEF, h.INI, h.VIS), "")
		// As the web app's Commanders page: what each ability does, in
		// words, then how it is aimed; wrapped to the room there is.
		width := a.w - 30
		if width < 30 {
			width = 30
		}
		var long, short []string
		for _, key := range []string{"q", "w", "e", "r"} {
			ab := a.c.Abilities[h.Abilities[key]]
			head := a.st.Key.Render(strings.ToUpper(key)) + "  " + a.st.Accent.Render(ab.Name)
			if lv := a.c.Rules.Unlock[key]; lv > 1 {
				head += a.st.Dim.Render(fmt.Sprintf("  level %d", lv))
			}
			long = append(long, head)
			for _, l := range wrapWords(abilityText(a.c, ab), width-3) {
				long = append(long, "   "+l)
			}
			for _, l := range wrapWords(abilityMeta(ab), width-3) {
				long = append(long, "   "+a.st.Dim.Render(l))
			}
			short = append(short, a.st.Key.Render(strings.ToUpper(key))+"  "+ab.Name+"  "+a.st.Dim.Render(fmt.Sprintf("(L%d)", a.c.Rules.Unlock[key])), "   "+describeAbility(a.c, ab))
		}
		// The sentences when the screen has the rows; the short form when not.
		if len(right)+len(long)+2 <= a.h {
			right = append(right, long...)
		} else {
			right = append(right, short...)
		}
	} else {
		u := a.c.Units[id]
		right = append(right, a.st.Title.Render(u.Name))
		right = append(right, fmt.Sprintf("HP %d  MV %d  JMP %d  RNG %d", u.HP, u.MV, u.JMP, u.RNG),
			fmt.Sprintf("ATK %d  DEF %d  INI %d  VIS %d", u.ATK, u.DEF, u.INI, u.VIS), "")
		if u.HoldDefBonus > 0 {
			right = append(right, fmt.Sprintf("trait: hold gives +%d DEF", u.HoldDefBonus))
		}
		if u.AdjacentDicePenalty > 0 {
			right = append(right, fmt.Sprintf("trait: -%d die vs adjacent targets", u.AdjacentDicePenalty))
		}
		if u.IgnoreHeightCost {
			right = append(right, "trait: ignores height move cost")
		}
		if u.Expires > 0 {
			right = append(right, fmt.Sprintf("summon: expires after %d turns", u.Expires))
		}
		for _, abid := range u.Abilities {
			ab := a.c.Abilities[abid]
			right = append(right, a.st.Key.Render(ab.Name)+"  "+abilityText(a.c, ab), "   "+a.st.Dim.Render(abilityMeta(ab)))
		}
	}
	right = append(right, "", a.st.Dim.Render("j/k browse  esc back"))
	lw := 22
	var lines []string
	n := len(left)
	if len(right) > n {
		n = len(right)
	}
	for i := 0; i < n; i++ {
		l, rr := "", ""
		if i < len(left) {
			l = left[i]
		}
		if i < len(right) {
			rr = right[i]
		}
		lines = append(lines, fit("  "+fit(l, lw)+"  "+rr, a.w))
	}
	return strings.Join(lines, "\n")
}
