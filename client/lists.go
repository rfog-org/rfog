package client

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"rfog/engine"
	"rfog/proto"
)

// ---- live (async) matches -------------------------------------------------

// liveScreen lists the player's matches in flight (daily) and joins one.
type liveScreen struct {
	rows []proto.LiveMatch
	sel  int
	err  string
}

func newLiveScreen(a *App) *liveScreen {
	l := &liveScreen{rows: a.net.welcome.Live}
	return l
}

func (l *liveScreen) init(a *App) tea.Cmd {
	_ = a.net.send(proto.TLive, nil)
	return tick(1000)
}

func (l *liveScreen) update(a *App, msg tea.Msg) (screen, tea.Cmd) {
	switch t := msg.(type) {
	case tickMsg:
		return l, tick(1000)
	case netMsg:
		if t.Err != nil {
			return newOnlineScreen(a), nil
		}
		switch t.T {
		case proto.TLiveList:
			var ll proto.LiveList
			_ = t.F.As(&ll)
			l.rows = ll.Matches
			a.net.welcome.Live = ll.Matches
			if l.sel >= len(l.rows) {
				l.sel = 0
			}
		case proto.TMatchFound:
			// Join answered: same hand-off as a fresh match.
			o := newOnlineScreen(a)
			return o.onNet(a, t)
		case proto.TError:
			var e proto.Error
			_ = t.F.As(&e)
			l.err = e.Msg
		}
		return l, nil
	case tea.MouseMsg:
		if id, hit, act := a.pointer(t, l.sel); hit {
			l.sel = id
			if act && len(l.rows) > 0 {
				_ = a.net.send(proto.TJoin, proto.Join{Match: l.rows[l.sel].Match})
			}
		}
		return l, nil
	case tea.KeyMsg:
		switch {
		case isKey(t, "j", "down"):
			if len(l.rows) > 0 {
				l.sel = (l.sel + 1) % len(l.rows)
			}
		case isKey(t, "k", "up"):
			if len(l.rows) > 0 {
				l.sel = (l.sel + len(l.rows) - 1) % len(l.rows)
			}
		case isKey(t, "enter"):
			if len(l.rows) > 0 {
				_ = a.net.send(proto.TJoin, proto.Join{Match: l.rows[l.sel].Match})
			}
		case isKey(t, "r"):
			_ = a.net.send(proto.TLive, nil)
		case isKey(t, "q", "esc"):
			return onlineBack(a), nil
		}
	}
	return l, nil
}

func (l *liveScreen) view(a *App) string {
	st := &a.st
	lines := []string{st.Title.Render("matches in flight"), ""}
	if len(l.rows) == 0 {
		lines = append(lines, st.Dim.Render("none — queue daily to start one"))
	}
	for i, lm := range l.rows {
		a.clickRow(len(lines), i)
		state := st.Dim.Render("waiting on them")
		if lm.YourTurn {
			state = st.Warn.Render("your move")
		}
		when := ""
		if !lm.Deadline.IsZero() {
			when = st.Dim.Render(" " + shortDuration(time.Until(lm.Deadline)) + " left")
		}
		phase := fmt.Sprintf("turn %d", lm.Turn)
		if lm.Phase == "draft" {
			phase = "draft"
		}
		row := fmt.Sprintf("%-4s %-6s %-8s vs %-20s %s%s", lm.Mode, lm.Time, phase, truncate(lm.Versus, 20), state, when)
		if i == l.sel {
			lines = append(lines, st.Accent.Render("> ")+row)
		} else {
			lines = append(lines, "  "+row)
		}
	}
	if l.err != "" {
		lines = append(lines, "", st.Danger.Render(l.err))
	}
	lines = append(lines, "", st.Dim.Render("enter open  r refresh  esc back"))
	return a.centered(lines)
}

// ---- history ----------------------------------------------------------------

// historyScreen lists finished matches; enter downloads the replay and
// plays it back, s saves it to the replay folder.
type historyScreen struct {
	rows    []proto.HistoryMatch
	sel     int
	err     string
	loaded  bool
	saving  bool
	saved   string
	waiting string // match id whose replay was requested
}

func newHistoryScreen(a *App) *historyScreen { return &historyScreen{} }

func (h *historyScreen) init(a *App) tea.Cmd {
	_ = a.net.send(proto.THistory, proto.History{Limit: 30})
	return nil
}

func (h *historyScreen) update(a *App, msg tea.Msg) (screen, tea.Cmd) {
	switch t := msg.(type) {
	case netMsg:
		if t.Err != nil {
			return newOnlineScreen(a), nil
		}
		switch t.T {
		case proto.THistoryList:
			var hl proto.HistoryList
			_ = t.F.As(&hl)
			h.rows, h.loaded = hl.Matches, true
		case proto.TReplayData:
			var rd proto.ReplayData
			_ = t.F.As(&rd)
			if rd.Match != h.waiting {
				return h, nil
			}
			h.waiting = ""
			if h.saving {
				h.saving = false
				name := fmt.Sprintf("%s-%s.json", time.Now().Format("20060102-150405"), rd.Match)
				path := filepath.Join(DataDir(), name)
				if err := os.WriteFile(path, rd.Replay, 0o644); err != nil {
					h.err = err.Error()
				} else {
					h.saved = path
				}
				return h, nil
			}
			rp, err := engine.UnmarshalReplay(rd.Replay)
			if err != nil {
				h.err = err.Error()
				return h, nil
			}
			ms := newMatchScreen(a, newReplayMatch(a.c, rp))
			ms.back = func(a *App) screen { return newHistoryScreen(a) }
			return ms, tick(50)
		case proto.TError:
			var e proto.Error
			_ = t.F.As(&e)
			h.err, h.waiting, h.saving = e.Msg, "", false
		}
		return h, nil
	case tea.MouseMsg:
		if id, hit, act := a.pointer(t, h.sel); hit {
			h.sel = id
			if act && len(h.rows) > 0 && h.waiting == "" {
				h.saving, h.err, h.saved = false, "", ""
				h.waiting = h.rows[h.sel].Match
				_ = a.net.send(proto.TReplayGet, proto.ReplayGet{Match: h.waiting})
			}
		}
		return h, nil
	case tea.KeyMsg:
		switch {
		case isKey(t, "j", "down"):
			if len(h.rows) > 0 {
				h.sel = (h.sel + 1) % len(h.rows)
			}
		case isKey(t, "k", "up"):
			if len(h.rows) > 0 {
				h.sel = (h.sel + len(h.rows) - 1) % len(h.rows)
			}
		case isKey(t, "enter"), isKey(t, "s"):
			if len(h.rows) == 0 || h.waiting != "" {
				return h, nil
			}
			if isKey(t, "s") && a.ephemeral {
				h.err = "replays are saved by the native client (rfog online)"
				return h, nil
			}
			h.saving, h.err, h.saved = isKey(t, "s"), "", ""
			h.waiting = h.rows[h.sel].Match
			_ = a.net.send(proto.TReplayGet, proto.ReplayGet{Match: h.waiting})
		case isKey(t, "q", "esc"):
			return onlineBack(a), nil
		}
	}
	return h, nil
}

func (h *historyScreen) view(a *App) string {
	st := &a.st
	lines := []string{st.Title.Render("history"), ""}
	switch {
	case !h.loaded:
		lines = append(lines, st.Dim.Render("loading..."))
	case len(h.rows) == 0:
		lines = append(lines, st.Dim.Render("no finished matches yet"))
	}
	for i, m := range h.rows {
		a.clickRow(len(lines), i)
		lines = append(lines, historyRow(a, m, i == h.sel))
	}
	if h.waiting != "" {
		lines = append(lines, "", st.Dim.Render("fetching replay..."))
	}
	if h.saved != "" {
		lines = append(lines, "", st.Dim.Render("saved "+h.saved))
	}
	if h.err != "" {
		lines = append(lines, "", st.Danger.Render(h.err))
	}
	lines = append(lines, "", st.Dim.Render("enter watch  s save replay  esc back"))
	return a.centered(lines)
}

// historyRow renders one finished match from the viewer's side.
func historyRow(a *App, m proto.HistoryMatch, selected bool) string {
	st := &a.st
	var you proto.HistoryPlayer
	for _, p := range m.Players {
		if p.Slot == m.You {
			you = p
		}
	}
	res := outcome(m)
	result := resStyle(st, res).Render(fmt.Sprintf("%-4s", res))
	delta := ""
	if you.Before != nil && you.After != nil {
		delta = fmt.Sprintf("%+d", int(*you.After-*you.Before))
		if d := *you.After - *you.Before; d < 0 {
			delta = st.Danger.Render(delta)
		} else {
			delta = st.Good.Render(delta)
		}
	}
	row := fmt.Sprintf("%s %-4s %-6s %s vs %-18s %-7s %s", m.Ended.Local().Format("01-02 15:04"), m.Mode, m.Time, result,
		truncate(opponents(m), 18), m.Result, delta)
	if selected {
		return st.Accent.Render("> ") + row
	}
	return "  " + row
}

// ---- ladder ---------------------------------------------------------------

type ladderScreen struct {
	mode string
	rows []proto.LadderRow
	err  string
}

func newLadderScreen(a *App, mode string) *ladderScreen {
	if mode == "casual" || strings.HasSuffix(mode, "-casual") {
		mode = "blitz"
	}
	return &ladderScreen{mode: mode}
}

func (l *ladderScreen) init(a *App) tea.Cmd {
	_ = a.net.send(proto.TLadder, proto.Ladder{Mode: l.mode})
	return nil
}

func (l *ladderScreen) update(a *App, msg tea.Msg) (screen, tea.Cmd) {
	switch t := msg.(type) {
	case netMsg:
		if t.Err != nil {
			return newOnlineScreen(a), nil
		}
		switch t.T {
		case proto.TLadderList:
			var ll proto.LadderList
			_ = t.F.As(&ll)
			l.mode, l.rows = ll.Mode, ll.Rows
		case proto.TError:
			var e proto.Error
			_ = t.F.As(&e)
			l.err = e.Msg
		}
		return l, nil
	case tea.KeyMsg:
		switch {
		case isKey(t, "l", "right", "h", "left", "tab"):
			ranked := []string{"blitz", "bullet", "rapid", "daily"}
			i := 0
			for j, m := range ranked {
				if m == l.mode {
					i = j
				}
			}
			if isKey(t, "h", "left") {
				i = (i + len(ranked) - 1) % len(ranked)
			} else {
				i = (i + 1) % len(ranked)
			}
			l.mode, l.rows = ranked[i], nil
			_ = a.net.send(proto.TLadder, proto.Ladder{Mode: l.mode})
		case isKey(t, "q", "esc"):
			return onlineBack(a), nil
		}
	}
	return l, nil
}

func (l *ladderScreen) view(a *App) string {
	st := &a.st
	lines := []string{st.Title.Render("leaderboard  " + catGlyph(l.mode) + " " + capital(l.mode)), ""}
	if len(l.rows) == 0 {
		lines = append(lines, st.Dim.Render("—"))
	}
	for i, r := range l.rows {
		name := r.Name
		if a.net != nil && r.Name == a.net.welcome.Name {
			name = st.Accent.Render(name)
		}
		lines = append(lines, fmt.Sprintf("%2d. %-16s %s", i+1, name, ratingStr(r.Rating)))
	}
	if l.err != "" {
		lines = append(lines, "", st.Danger.Render(l.err))
	}
	lines = append(lines, "", st.Dim.Render("h/l other control  esc back"))
	return a.centered(lines)
}
