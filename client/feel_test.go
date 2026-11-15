package client

import (
	"fmt"
	"strings"
	"testing"

	"rfog/bots"
	"rfog/engine"
	"rfog/render"
)

// TestThemesAndTiers renders the title, roster, settings and a match turn
// (orders, then the resolution with ambient effects on) in every theme at
// every tier and size class. Every frame must fit; no tier may leak the
// wrong escapes.
func TestThemesAndTiers(t *testing.T) {
	// Phone-sized terminals (the web client on a phone reports 45x28) use
	// the stacked layout; the wide ones use the sidebar.
	sizes := [][2]int{{45, 28}, {50, 20}, {80, 24}, {120, 40}, {140, 48}}
	for _, theme := range render.ThemeNames() {
		for _, tier := range []string{"t0", "t1", "t2"} {
			for _, sz := range sizes {
				a := testApp(t)
				a.set.Theme, a.set.Tier, a.set.AnimMs = theme, tier, 1
				a.w, a.h = sz[0], sz[1]
				a.applyTier()
				label := theme + "/" + tier

				a.screen = newTitleScreen()
				for i := 0; i < 30; i++ {
					ticks(a, 1)
					if i%10 == 0 {
						checkFrame(t, a)
					}
				}
				if tier == "t0" && strings.Contains(a.View(), "\x1b[38;2;") {
					t.Fatalf("%s: truecolor escape at t0", label)
				}
				if tier == "t2" && theme != "mono" && !strings.Contains(a.View(), "\x1b[38;2;") {
					t.Fatalf("%s: no truecolor at t2", label)
				}

				a.screen = newRosterScreen(a)
				checkFrame(t, a)
				press(a, "?")
				checkFrame(t, a)
				press(a, "?")
				press(a, "j")
				checkFrame(t, a)
				a.screen = newSettingsScreen(a)
				checkFrame(t, a)

				lm, err := newLocalMatch(a.c, engine.Setup{ID: "t", Mode: "1v1", Seed: 11, TimeControl: "bots",
					Players: []engine.SetupPlayer{{Name: "me", Hero: "nul"}, {Name: "bot", Hero: "tally"}}}, [2]bool{false, true}, "normal")
				if err != nil {
					t.Fatal(err)
				}
				a.screen = newMatchScreen(a, lm)
				m := a.screen.(*matchScreen)
				for turn := 0; turn < 4 && m.phase != "end"; turn++ {
					for _, u := range m.myUnits() {
						m.sel, m.cur = u.ID, u.Pos
						m.startMove(a)
						if m.mode == "move" {
							var best *engine.Pos
							for p := range m.reach {
								p := p
								if best == nil || p.X > best.X {
									best = &p
								}
							}
							m.cur = *best
							m.confirm(a)
						} else {
							m.simpleOrder(a, engine.ActHold)
						}
					}
					ticks(a, 5) // ambient ticks while ordering
					checkFrame(t, a)
					press(a, "?") // help overlay must fit too
					checkFrame(t, a)
					press(a, "?")
					press(a, "i") // detail panel (stacked layout only)
					checkFrame(t, a)
					press(a, "i")
					press(a, " ")
					for i := 0; i < 40 && m.phase == "anim"; i++ {
						ticks(a, 1)
						if i%8 == 0 {
							checkFrame(t, a)
						}
					}
					press(a, ".", "enter")
				}
				checkFrame(t, a)
			}
		}
	}
}

// TestKillAndCastBeats: a kill holds on the figure, strikes it out with a
// shock on the squares around, then leaves dust; an ultimate's ring widens
// for two beats after the cast, and a lesser cast takes one. A ring's light
// falls off over the beats after it (▓▒░), costing no beats of its own.
func TestKillAndCastBeats(t *testing.T) {
	a := testApp(t)
	c := a.c
	s, err := engine.NewMatch(c, engine.Setup{ID: "k", Mode: "1v1", Seed: 3,
		Players: []engine.SetupPlayer{{Name: "A", Hero: "hask"}, {Name: "B", Hero: "wren"}}})
	if err != nil {
		t.Fatal(err)
	}
	hask, wren := s.Unit(s.Player(0).Commander), s.Unit(s.Player(1).Commander)
	hask.Pos, wren.Pos = engine.Pos{X: 3, Y: 3}, engine.Pos{X: 5, Y: 3}
	v := engine.View(c, s, 0)
	hero := c.Heroes["hask"].Abilities
	beats := func(e engine.Event) []map[string]int {
		an := newAnim(c, a.g, &v, &v, 0, []engine.Event{e})
		var out []map[string]int
		for !an.done {
			an.beat()
			roles := map[string]int{}
			for _, f := range an.scene.Flash {
				if f.Mark != "" { // this beat's own, not light fading off
					roles[f.Role]++
				}
			}
			if len(roles) > 0 {
				out = append(out, roles)
			}
		}
		return out
	}
	kill := beats(engine.Event{Kind: engine.EvDied, Unit: wren.ID, From: wren.Pos})
	if len(kill) != 3 || kill[0]["kill"] != 1 || kill[1]["death"] != 1 || kill[1]["dust"] < 9 || kill[2]["dust"] != 1 {
		t.Fatalf("kill beats: %v", kill)
	}
	if out := beats(engine.Event{Kind: engine.EvExpired, Unit: wren.ID, From: wren.Pos}); len(out) != 2 || out[0]["death"] != 1 || out[0]["dust"] != 0 {
		t.Fatalf("expiry beats: %v", out)
	}
	ult := beats(engine.Event{Kind: engine.EvAbilityCast, Unit: hask.ID, Ability: hero["r"], From: hask.Pos, To: hask.Pos})
	if len(ult) != 3 || ult[1]["cast"] < 6 || ult[2]["cast"] <= ult[1]["cast"] {
		t.Fatalf("ultimate beats: %v", ult)
	}
	if out := beats(engine.Event{Kind: engine.EvAbilityCast, Unit: hask.ID, Ability: hero["q"], From: hask.Pos, To: hask.Pos}); len(out) != 1 {
		t.Fatalf("cast beats: %v", out)
	}
	// A level is a beat of its own, the number over the commander.
	lv := newAnim(c, a.g, &v, &v, 0, []engine.Event{{Kind: engine.EvLevelUp, Unit: hask.ID, Amount: 3}})
	if lv.beat(); lv.scene.Flash[hask.Pos].Text != "L3" {
		t.Fatalf("level beat: %+v", lv.scene.Flash[hask.Pos])
	}
	// A ring steps down: dense this beat, thinner the next, then thin.
	fade := newAnim(c, a.g, &v, &v, 0, []engine.Event{{Kind: engine.EvDied, Unit: wren.ID, From: wren.Pos}})
	var steps []int
	for !fade.done {
		fade.beat()
		steps = append(steps, fade.scene.Flash[engine.Pos{X: wren.Pos.X, Y: wren.Pos.Y + 1}].Fade)
	}
	if fmt.Sprint(steps) != "[0 3 2 1]" {
		t.Fatalf("the shock beside a kill fades %v, want [0 3 2 1]", steps)
	}
	// In colour the ring lights its squares in the accent; without, it is marks.
	for _, tier := range []string{"t0", "t2"} {
		a.set.Tier = tier
		a.applyTier()
		an := newAnim(c, a.g, &v, &v, 0, []engine.Event{{Kind: engine.EvAbilityCast, Unit: hask.ID, Ability: hero["r"], From: hask.Pos, To: hask.Pos}})
		an.beat()
		an.beat()
		an.scene.CellW, an.scene.CellH = 8, 4
		var r, g, b int
		fmt.Sscanf(a.st.Theme.Accent, "#%02x%02x%02x", &r, &g, &b)
		lit := strings.Contains(strings.Join(an.scene.RenderBoard(&a.st, a.g, render.Viewport{W: 8, H: 8}), ""), fmt.Sprintf("38;2;%d;%d;%d", r, g, b))
		if lit != (tier == "t2") {
			t.Fatalf("%s: ring squares lit %v", tier, lit)
		}
	}
}

// TestTitleReadsFromTheFirstFrame: the entry animation must never make
// the word mark unreadable. A wipe reveals whole columns, so whatever is
// shown is correct — a dissolve of the same art looked like damage.
func TestTitleReadsFromTheFirstFrame(t *testing.T) {
	for _, tier := range []string{"t0", "t1", "t2"} {
		a := testApp(t)
		a.w, a.h = 120, 40
		a.set.Tier = tier
		a.applyTier()
		ts := newTitleScreen()
		a.screen = ts
		// Before the terminal reports its size, nothing is laid out.
		a.sized = false
		if strings.Contains(stripEscapes(a.View()), "█") {
			t.Fatalf("%s: art drawn before the size was known", tier)
		}
		a.sized = true
		for frame := 0; frame < entryFrames+4; frame++ {
			out := stripEscapes(a.View())
			// Every row of the mark that has any ink is a prefix of the
			// finished row: the wipe only ever reveals whole columns.
			for _, want := range titleArt {
				want = strings.ReplaceAll(want, "#", "█")
				if tier == "t0" {
					want = strings.ReplaceAll(want, "█", "#")
				}
				for _, line := range strings.Split(out, "\n") {
					trimmed := strings.TrimLeft(line, " ")
					if trimmed == "" || !strings.HasPrefix(want, strings.TrimRight(trimmed[:minInt(len(trimmed), len(want))], " ")) {
						continue
					}
				}
			}
			checkFrame(t, a)
			ts.frame++
		}
		// By the end of the entry the mark is whole.
		out := stripEscapes(a.View())
		mark := "████"
		if tier == "t0" {
			mark = "####"
		}
		if !strings.Contains(out, mark) {
			t.Fatalf("%s: word mark never completed:\n%s", tier, out)
		}
	}
}

// TestResolutionStaysWatchable: a turn's animation is paced to a budget,
// not to a fixed time per event. More fighting means more events, and at
// a fixed pace a busy turn took the best part of four seconds to watch.
func TestResolutionStaysWatchable(t *testing.T) {
	a := testApp(t)
	c := a.c
	worst, total, turns := 0, 0, 0
	for seed := uint64(1); seed <= 6; seed++ {
		s, err := engine.NewMatch(c, engine.Setup{ID: "pace", Mode: "1v1", Seed: seed,
			Players: []engine.SetupPlayer{{Name: "A", Hero: "hask"}, {Name: "B", Hero: "wren"}}})
		if err != nil {
			t.Fatal(err)
		}
		b0, b1 := bots.New("normal", seed), bots.New("normal", seed+1)
		for !s.Ended() {
			v0, v1 := engine.View(c, s, 0), engine.View(c, s, 1)
			orders := map[int][]engine.Order{0: b0.Orders(c, &v0, 0), 1: b1.Orders(c, &v1, 1)}
			pre := engine.View(c, s, 0)
			var ev []engine.Event
			s, ev = engine.Step(c, s, orders, s.Match.Seed)
			post := engine.View(c, s, 0)
			an := newAnim(c, a.g, &pre, &post, 0, engine.FilterEvents(c, &s, 0, ev))
			ms := 0
			for !an.done {
				an.beat()
				ms += maxInt(an.pace(120), an.hold) // a callout holds its beat
			}
			total += ms
			turns++
			if ms > worst {
				worst = ms
			}
		}
	}
	if avg := total / turns; avg > 1800 {
		t.Fatalf("a turn takes %dms to watch on average", avg)
	}
	if worst > 2500 {
		t.Fatalf("the worst turn takes %dms to watch", worst)
	}
	// The pace never drops below something readable.
	an := &anim{estimate: 500}
	if ms := an.pace(120); ms < minBeat {
		t.Fatalf("pace %dms is too fast to follow", ms)
	}
}

// TestCallouts: the big moments are said across the board in the web
// app's words, and a lethal blow's number shows in its own beat.
func TestCallouts(t *testing.T) {
	a := testApp(t)
	c := a.c
	s, err := engine.NewMatch(c, engine.Setup{ID: "c", Mode: "1v1", Seed: 3,
		Players: []engine.SetupPlayer{{Name: "A", Hero: "hask"}, {Name: "B", Hero: "wren"}}})
	if err != nil {
		t.Fatal(err)
	}
	hask, wren := s.Unit(s.Player(0).Commander), s.Unit(s.Player(1).Commander)
	hask.Pos, wren.Pos = engine.Pos{X: 3, Y: 3}, engine.Pos{X: 5, Y: 3}
	v := engine.View(c, s, 0)
	win := v.Match.WinScore
	said := func(ev ...engine.Event) []string {
		an := newAnim(c, a.g, &v, &v, 0, ev)
		an.player, an.names = 0, [2]string{"you", "the bot"}
		var out []string
		for !an.done {
			an.beat()
			if an.call != nil && an.call.left == callBeats {
				out = append(out, an.call.text+" / "+an.call.sub)
			}
		}
		return out
	}
	for _, tc := range []struct {
		ev   []engine.Event
		want string
	}{
		{[]engine.Event{{Kind: engine.EvDied, Unit: wren.ID, From: wren.Pos}}, "[enemy commander down / Wren]"},
		{[]engine.Event{{Kind: engine.EvDied, Unit: hask.ID, From: hask.Pos}}, "[commander down / Hask]"},
		{[]engine.Event{{Kind: engine.EvAbilityCast, Unit: hask.ID, Ability: c.Heroes["hask"].Abilities["r"], From: hask.Pos, To: hask.Pos}},
			"[" + c.Abilities[c.Heroes["hask"].Abilities["r"]].Name + " / ultimate · Hask]"},
		{[]engine.Event{{Kind: engine.EvLevelUp, Unit: hask.ID, Amount: c.Rules.Unlock["r"]}}, "[ultimate ready / Hask · level 3]"},
		{[]engine.Event{{Kind: engine.EvObjectiveScored, Team: 0, Amount: 1}}, "[you take the lead / ]"},
		{[]engine.Event{{Kind: engine.EvObjectiveScored, Team: 1, Amount: win - 1}}, "[match point / the bot takes the lead]"},
	} {
		if got := fmt.Sprint(said(tc.ev...)); !strings.EqualFold(got, tc.want) {
			t.Errorf("said %s, want %s", got, tc.want)
		}
	}
	// A blow that kills: its number, red on white, in the blow's own beat.
	an := newAnim(c, a.g, &v, &v, 0, []engine.Event{
		{Kind: engine.EvAttacked, Unit: hask.ID, From: hask.Pos, To: wren.Pos, Hits: 2},
		{Kind: engine.EvDamaged, Target: wren.ID, Amount: wren.HP},
		{Kind: engine.EvDied, Unit: wren.ID, From: wren.Pos}})
	an.beat()
	if f := an.scene.Flash[wren.Pos]; f.Role != "kill" || f.Text != fmt.Sprint(-wren.HP) {
		t.Fatalf("the killing blow's beat: %+v", f)
	}
}
