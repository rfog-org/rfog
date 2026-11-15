package client

import (
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"rfog/proto"
)

// draftScreen runs the ban/pick phase of an online match on the pick
// screen (pickView), driven by the server's draft state: the seats fill as
// the picks arrive. In team modes bans alternate per team and picks snake;
// a hero someone already took or banned is greyed out.
type draftScreen struct {
	ticking bool
	rm      *remoteMatch
	st      proto.DraftState
	sel     int
	err     string
	ready   bool // play has started; waiting for the first TurnStart
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
			if d.ticking { // one clock, however many states arrive
				return d, nil
			}
			d.ticking = true
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
	case tea.MouseMsg:
		if id, hit, act := a.pointer(t, d.sel); hit {
			if id == pickLock {
				if t.Action == tea.MouseActionPress {
					return d, d.act(a)
				}
				return d, nil
			}
			d.sel = id
			if act {
				return d, d.act(a)
			}
		}
		return d, nil
	case turnStartMsg:
		m := newMatchScreen(a, d.rm)
		return m.beginOrders(a, &t)
	case matchEndMsg:
		return onlineBack(a), nil
	case tea.KeyMsg:
		if gridMove(t, &d.sel, len(d.heroes(a))) {
			return d, nil
		}
		switch {
		case isKey(t, "enter", " "):
			return d, d.act(a)
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

// heroes are the draft's commanders (every one until the first state).
func (d *draftScreen) heroes(a *App) []string {
	if len(d.st.Heroes) > 0 {
		return d.st.Heroes
	}
	return a.c.HeroIDs()
}

// act bans or picks the highlighted hero when it is this seat's turn.
func (d *draftScreen) act(a *App) tea.Cmd {
	if len(d.st.Heroes) == 0 || d.st.Turn != d.rm.you || d.st.Kind == "done" {
		return nil
	}
	h := d.st.Heroes[d.sel%len(d.st.Heroes)]
	if err := d.rm.r.send(proto.TDraft, proto.DraftAction{Match: d.rm.id, Kind: d.st.Kind, Hero: h}); err != nil {
		d.err = err.Error()
	}
	return nil
}

func (d *draftScreen) view(a *App) string {
	you := d.teamOf(d.rm.you)
	pm := pickModel{title: "draft · " + d.rm.mode + " · " + d.rm.timeCtl, heroes: d.heroes(a), sel: d.sel,
		notes: map[string]string{}, keys: "arrows choose  enter " + d.st.Kind + "  Q leave", err: d.err}
	for _, p := range d.rm.players {
		s := pickSeat{name: p.Name, hero: d.st.Picks[p.Slot], you: p.Slot == d.rm.you,
			active: p.Slot == d.st.Turn && d.st.Kind != "done" && len(d.st.Heroes) > 0}
		if p.Team != you {
			s.team = 1
		}
		if s.you {
			s.name = "you"
		}
		pm.seats = append(pm.seats, s)
	}
	for _, b := range d.st.Banned {
		pm.notes[b] = "banned"
	}
	for slot, h := range d.st.Picks {
		switch {
		case slot == d.rm.you:
			pm.notes[h] = "your pick"
		case d.teamOf(slot) == you:
			pm.notes[h] = "ally's pick"
		default:
			pm.notes[h] = "their pick"
		}
	}
	left := time.Until(d.st.Deadline)
	if left < 0 {
		left = 0
	}
	switch {
	case d.ready:
		pm.status, pm.keys = "deploying...", "Q leave"
	case len(d.st.Heroes) == 0:
		pm.status, pm.keys = "waiting for the draft...", "Q leave"
	case d.st.Turn == d.rm.you:
		pm.status = fmt.Sprintf("you: %s  %s", d.st.Kind, shortDuration(left))
		pm.button = map[string]string{"pick": "Lock in", "ban": "Ban"}[d.st.Kind]
	case d.teamOf(d.st.Turn) == you:
		pm.status = fmt.Sprintf("ally: %s  %s", d.st.Kind, shortDuration(left))
	default:
		pm.status = fmt.Sprintf("them: %s  %s", d.st.Kind, shortDuration(left))
	}
	return a.pickView(pm)
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
