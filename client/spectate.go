package client

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"rfog/engine"
	"rfog/proto"
)

// lobbyScreen lists live matches and opens one as a spectator.
type lobbyScreen struct {
	rows   []proto.LobbyMatch
	sel    int
	err    string
	loaded bool
}

func newLobbyScreen(a *App) *lobbyScreen { return &lobbyScreen{} }

func (l *lobbyScreen) init(a *App) tea.Cmd {
	_ = a.net.send(proto.TLobby, nil)
	return tick(2000)
}

func (l *lobbyScreen) update(a *App, msg tea.Msg) (screen, tea.Cmd) {
	switch t := msg.(type) {
	case tickMsg:
		_ = a.net.send(proto.TLobby, nil)
		return l, tick(2000)
	case netMsg:
		if t.Err != nil {
			return newOnlineScreen(a), nil
		}
		switch t.T {
		case proto.TLobbyList:
			var ll proto.LobbyList
			_ = t.F.As(&ll)
			l.rows, l.loaded = watchable(a, ll.Matches), true
			if l.sel >= len(l.rows) {
				l.sel = 0
			}
		case proto.TSpecState:
			var ss proto.SpecState
			if err := t.F.As(&ss); err != nil {
				l.err = err.Error()
				return l, nil
			}
			sm := newSpectateMatch(a, ss)
			a.spec = sm
			ms := newMatchScreen(a, sm)
			ms.back = func(a *App) screen { return newLobbyScreen(a) }
			return ms, ms.apply(a, ss)
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
				_ = a.net.send(proto.TSpectate, proto.Spectate{Match: l.rows[l.sel].Match})
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
				_ = a.net.send(proto.TSpectate, proto.Spectate{Match: l.rows[l.sel].Match})
			}
		case isKey(t, "r"):
			_ = a.net.send(proto.TLobby, nil)
		case isKey(t, "q", "esc"):
			return onlineBack(a), nil
		}
	}
	return l, nil
}

func (l *lobbyScreen) view(a *App) string {
	st := &a.st
	lines := []string{st.Title.Render("watch"), ""}
	switch {
	case !l.loaded:
		lines = append(lines, st.Dim.Render("loading..."))
	case len(l.rows) == 0:
		lines = append(lines, st.Dim.Render("No games are currently being played."))
	}
	cat := ""
	for i, m := range l.rows {
		if c := category(a, m.Time); c != cat {
			cat = c
			lines = append(lines, st.Title.Render(catGlyph(c)+" "+capital(c)))
		}
		a.clickRow(len(lines), i)
		where := fmt.Sprintf("turn %d  %d-%d", m.Turn, m.Score[0], m.Score[1])
		if m.Phase == "draft" {
			where = "drafting"
		}
		tag := ""
		if m.Delayed {
			tag = st.Dim.Render("  (one turn behind)")
		}
		row := fmt.Sprintf("%-4s %-22s %-16s %s%s", m.Mode, timeLabel(a, m.Time), where, truncate(joinNames(m.Players), 34), tag)
		if i == l.sel {
			lines = append(lines, st.Accent.Render("> ")+row)
		} else {
			lines = append(lines, "  "+row)
		}
	}
	if l.err != "" {
		lines = append(lines, "", st.Danger.Render(l.err))
	}
	lines = append(lines, "", st.Dim.Render("enter watch  r refresh  esc back"))
	return a.centered(lines)
}

func joinNames(names []string) string {
	out := ""
	for i, n := range names {
		if i > 0 {
			out += " vs "
		}
		out += n
	}
	return out
}

// ---- spectator match source -------------------------------------------------

// spectateMatch feeds the match screen from SpecState frames. Spectators
// issue no orders; the screen runs in its watching mode.
type spectateMatch struct {
	r       *remote
	id      string
	mode    string
	timeCtl string
	players []proto.MatchPlayer
	view    engine.State
	turn    int
	live    int
	delayed bool
	final   *engine.State
	pending *turnResolvedMsg // the next resolution to animate
}

func newSpectateMatch(a *App, ss proto.SpecState) *spectateMatch {
	return &spectateMatch{r: a.net, id: ss.Match, mode: ss.Mode, timeCtl: ss.Time, players: ss.Players, view: ss.View, turn: ss.Turn, live: ss.Live, delayed: ss.Delayed}
}

func (sm *spectateMatch) View(player int) engine.State { return sm.view }
func (sm *spectateMatch) Humans() []int                { return nil }
func (sm *spectateMatch) Hotseat() bool                { return false }
func (sm *spectateMatch) Watching() bool               { return true }
func (sm *spectateMatch) Deadline() time.Time          { return time.Time{} }
func (sm *spectateMatch) Final() *engine.State         { return sm.final }
func (sm *spectateMatch) Committed(player int) bool    { return false }
func (sm *spectateMatch) Setup() engine.Setup {
	return engine.Setup{ID: sm.id, Mode: sm.mode, TimeControl: sm.timeCtl}
}
func (sm *spectateMatch) Commit(player int, orders []engine.Order) tea.Cmd { return nil }
func (sm *spectateMatch) SaveReplay() (string, error) {
	return "", errors.New("the replay is downloadable from history once the match ends")
}
func (sm *spectateMatch) Rematch(level string) (matchSource, error) {
	return nil, errors.New("watching")
}
func (sm *spectateMatch) Leave() { _ = sm.r.send(proto.TUnspec, nil) }

// Advance hands the screen the next resolution when one has arrived.
func (sm *spectateMatch) Advance() *turnResolvedMsg {
	r := sm.pending
	sm.pending = nil
	return r
}

// apply folds a new frame in, returning the command that animates it.
func (m *matchScreen) apply(a *App, ss proto.SpecState) tea.Cmd {
	sm, ok := m.src.(*spectateMatch)
	if !ok {
		return nil
	}
	pre := sm.view
	sm.view, sm.turn, sm.live, sm.players = ss.View, ss.Turn, ss.Live, ss.Players
	if ss.Ended {
		f := ss.View
		sm.final = &f
	}
	if len(ss.Events) > 0 {
		sm.pending = &turnResolvedMsg{Views: map[int]turnView{-1: {Pre: pre, Post: ss.View, Events: ss.Events}}, Ended: ss.Ended}
	}
	m.refresh(a)
	return tick(50)
}

// watchable is the matches worth watching, grouped by rating category as
// the web lists them: games against bots are left out.
func watchable(a *App, ms []proto.LobbyMatch) []proto.LobbyMatch {
	var out []proto.LobbyMatch
	for _, m := range ms {
		if !m.Bots {
			out = append(out, m)
		}
	}
	rank := map[string]int{}
	for i, c := range ladderModes {
		rank[c] = i
	}
	sort.SliceStable(out, func(i, j int) bool { return rank[category(a, out[i].Time)] < rank[category(a, out[j].Time)] })
	return out
}

// category is the rating category of a time control ("30s-casual" → blitz).
func category(a *App, id string) string {
	base := strings.TrimSuffix(id, "-casual")
	clocks := homeClocks
	if a.net != nil && len(a.net.welcome.Clocks) > 0 {
		clocks = a.net.welcome.Clocks
	}
	for _, c := range clocks {
		if c.ID == base {
			return c.Category
		}
	}
	return "other"
}
