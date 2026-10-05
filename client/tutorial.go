package client

import (
	"strings"

	"rfog/engine"
)

// coach is the guided first match: three turns of prompts over the normal
// match screen. It never takes control — it watches what the player has
// already done and says what to try next, so a player who ignores it is
// simply playing the game.
type coach struct {
	step  int
	done  bool
	turn  int
	notes []string // one line of chatter per completed step
}

// coachStep is one lesson: a title, a couple of lines, and a test that
// says whether the player has done it.
type coachStep struct {
	title string
	body  []string
	// ready reports whether the lesson is satisfied by the screen's state.
	ready func(m *matchScreen) bool
}

func coachSteps() []coachStep {
	return []coachStep{
		{
			title: "your squad",
			body: []string{
				"Nearest you on the board are your commander and four units.",
				"tab cycles them. The cursor moves with hjkl or the arrows.",
				"Move the cursor onto one of your units to select it.",
			},
			ready: func(m *matchScreen) bool { return m.selected() != nil },
		},
		{
			title: "move",
			body: []string{
				"m plans a move: reachable tiles light up, the path previews.",
				"enter confirms, esc cancels. Height costs extra move points.",
				"Plan a move for any unit.",
			},
			ready: func(m *matchScreen) bool { return m.hasOrderOf(engine.ActMove) },
		},
		{
			title: "objectives",
			body: []string{
				"The three objectives score for whoever holds more of them: the lead",
				"is the point. First to 5, or the best score after 12 turns.",
				"Order every unit, then press space to commit the turn.",
			},
			ready: func(m *matchScreen) bool { return m.vs.Match.Turn > 1 },
		},
		{
			title: "everyone plans at once",
			body: []string{
				"Both sides planned that turn in secret, then it resolved:",
				"instant abilities, telegraphed hits, movement (initiative wins",
				"contested tiles), overwatch, then attacks. No reflexes involved.",
				"a attacks a visible enemy in range; the odds show before you commit.",
			},
			ready: func(m *matchScreen) bool { return m.hasOrderOf(engine.ActAttack) || m.vs.Match.Turn > 2 },
		},
		{
			title: "commander abilities",
			body: []string{
				"q w e r cast your commander's abilities (r unlocks at level 4),",
				"1-4 cast unit abilities. Telegraphed tiles are marked: step off them.",
				"x holds (+DEF), o sets overwatch — a free shot at the first enemy",
				"that moves into range. Play on; press ? any time for the key list.",
			},
			ready: func(m *matchScreen) bool { return m.vs.Match.Turn > 3 },
		},
	}
}

// hasOrderOf reports whether any planned order uses this action.
func (m *matchScreen) hasOrderOf(action string) bool {
	for _, o := range m.orders {
		if o.Action == action {
			return true
		}
	}
	return false
}

// advance checks the current lesson against the screen and moves on.
func (c *coach) advance(m *matchScreen) {
	if c.done {
		return
	}
	steps := coachSteps()
	for c.step < len(steps) && steps[c.step].ready(m) {
		c.notes = append(c.notes, "✓ "+steps[c.step].title)
		c.step++
	}
	if c.step >= len(steps) {
		c.done = true
	}
}

// panel renders the coaching box for the sidebar width.
func (c *coach) panel(a *App, w int) []string {
	steps := coachSteps()
	if c.done || c.step >= len(steps) {
		return []string{a.st.Good.Render(fit("tutorial complete — play on", w))}
	}
	s := steps[c.step]
	out := []string{a.st.Accent.Render(fit(strings.ToUpper(s.title), w))}
	for _, l := range s.body {
		for _, wrapped := range wrapLine(l, w) {
			out = append(out, a.st.Dim.Render(fit(wrapped, w)))
		}
	}
	return out
}

// wrapLine breaks a line at spaces to fit w columns.
func wrapLine(s string, w int) []string {
	if w <= 0 || len(s) <= w {
		return []string{s}
	}
	var out []string
	for len(s) > w {
		cut := strings.LastIndex(s[:w], " ")
		if cut <= 0 {
			cut = w
		}
		out = append(out, s[:cut])
		s = strings.TrimLeft(s[cut:], " ")
	}
	if s != "" {
		out = append(out, s)
	}
	return out
}

// newTutorial starts the guided match: an easy bot, a fixed seed so the
// first turns are the same for everyone, and the coach attached.
func newTutorial(a *App) (screen, error) {
	lm, err := newLocalMatch(a.c, engine.Setup{ID: "tutorial", Mode: "1v1", Seed: 20260101, TimeControl: "bots",
		Players: []engine.SetupPlayer{{Name: a.set.Name, Hero: "hask"}, {Name: "bot", Hero: "wren"}}}, [2]bool{false, true}, "easy")
	if err != nil {
		return nil, err
	}
	m := newMatchScreen(a, lm)
	m.coach = &coach{}
	return m, nil
}
