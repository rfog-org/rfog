package client

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"rfog/proto"
)

// Challenge a friend, in the terminal: open a challenge on the selected
// clock (always casual) and give your friend the code, or type a code a
// friend gave you. The web app makes the same challenges as links; the
// codes work in both.

type chState struct {
	sel   int    // 0 open a challenge, 1 join with a code
	code  string // typed code
	open  *proto.ChallengeInfo
	offer *proto.ChallengeInfo // someone's challenge, peeked
	note  string
}

func (o *onlineScreen) openChallenge() {
	o.state, o.err = "challenge", ""
	o.ch = chState{}
}

func (o *onlineScreen) challengeKey(a *App, k tea.KeyMsg) (screen, tea.Cmd) {
	c := &o.ch
	switch {
	case c.open != nil: // our challenge is open: esc withdraws it
		if isKey(k, "esc") {
			_ = a.net.send(proto.TChallenge, proto.Challenge{Action: "cancel"})
			c.open = nil
		}
		return o, nil
	case c.offer != nil:
		switch {
		case isKey(k, "enter"):
			_ = a.net.send(proto.TChallenge, proto.Challenge{Action: "accept", Code: c.offer.Code})
			c.note = "starting…"
		case isKey(k, "esc"):
			c.offer = nil
		}
		return o, nil
	}
	switch {
	case isKey(k, "esc"):
		o.state = "menu"
		return o.homeOr(a, o), nil
	case isKey(k, "up", "down", "tab", "shift+tab"):
		c.sel = 1 - c.sel
	case isKey(k, "enter"):
		if c.sel == 0 {
			if o.clocks == nil {
				o.err = "this server has no challenges"
				return o, nil
			}
			mode := o.modes[o.sel]
			if cl := o.clock(o.sel); cl != nil && cl.Async {
				o.err = "pick a clock that is not daily (esc, j/k)"
				return o, nil
			}
			_ = a.net.send(proto.TChallenge, proto.Challenge{Action: "create", Mode: mode})
			return o, nil
		}
		if strings.TrimSpace(c.code) == "" {
			o.err = "type the code your friend gave you"
			return o, nil
		}
		_ = a.net.send(proto.TChallenge, proto.Challenge{Action: "peek", Code: c.code})
	case isKey(k, "backspace"):
		if c.sel == 1 && len(c.code) > 0 {
			c.code = c.code[:len(c.code)-1]
		}
	default:
		if c.sel == 1 && len(k.Runes) > 0 && k.Type == tea.KeyRunes && len(c.code) < 8 {
			c.code += strings.ToUpper(string(k.Runes))
		}
	}
	return o, nil
}

// onChallenge applies a ChallengeInfo from the server.
func (o *onlineScreen) onChallenge(a *App, ci proto.ChallengeInfo) {
	c := &o.ch
	o.err = ""
	switch {
	case ci.Status == "gone":
		c.offer, c.note = nil, ""
		o.err = "that challenge is gone (taken, cancelled or expired)"
	case ci.Status == "cancelled":
		c.open = nil
	case ci.From == a.net.welcome.Name && c.sel == 0:
		c.open = &ci
	default:
		c.offer = &ci
	}
}

// clockLabel names a challenge's clock: "30s · blitz · rated".
func clockLabel(a *App, mode string, rated bool) string {
	if rated {
		return timeLabel(a, mode) + " · rated"
	}
	return timeLabel(a, mode+"-casual")
}

func (o *onlineScreen) challengeView(a *App) []string {
	st := &a.st
	c := &o.ch
	lines := []string{st.Title.Render("challenge a friend"), ""}
	switch {
	case c.open != nil:
		lines = append(lines, "your challenge: "+st.Accent.Render(clockLabel(a, c.open.Mode, c.open.Rated)), "",
			"code  "+st.Title.Render(c.open.Code), "",
			"Give your friend the code: in the terminal, online, f, then type it;",
			"in the web app, it is the link the challenge card shares.", "",
			st.Dim.Render("waiting for your friend…  esc withdraws it"))
	case c.offer != nil:
		lines = append(lines, st.Accent.Render(c.offer.From)+" challenges you", clockLabel(a, c.offer.Mode, c.offer.Rated)+" · 1v1", "")
		if c.note != "" {
			lines = append(lines, st.Dim.Render(c.note), "")
		}
		lines = append(lines, st.Dim.Render("enter accept  esc decline"))
	default:
		mode := o.modes[o.sel]
		opt := func(i int, s string) string {
			if c.sel == i {
				return st.Accent.Render("> " + s)
			}
			return "  " + s
		}
		code := c.code
		if c.sel == 1 {
			code += "_"
		}
		lines = append(lines,
			opt(0, fmt.Sprintf("open a challenge: %s", clockLabel(a, mode, false))),
			opt(1, "join with a code: ")+st.Accent.Render(code), "",
			st.Dim.Render("the clock is the one picked in the online menu; challenges are casual"),
			st.Dim.Render("up/down move  enter select  esc back"))
	}
	if o.err != "" {
		lines = append(lines, "", st.Danger.Render(o.err))
	}
	return lines
}
