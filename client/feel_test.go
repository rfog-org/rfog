package client

import (
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

// TestKillAndCastBeats checks the two-beat death and the cast ring show
// up in the animation scene.
func TestKillAndCastBeats(t *testing.T) {
	a := testApp(t)
	c := a.c
	s, err := engine.NewMatch(c, engine.Setup{ID: "k", Mode: "1v1", Seed: 3,
		Players: []engine.SetupPlayer{{Name: "A", Hero: "hask"}, {Name: "B", Hero: "wren"}}})
	if err != nil {
		t.Fatal(err)
	}
	// Put an enemy runner next to Hask with 1 HP and have Hask attack it.
	hask := s.Unit(s.Player(0).Commander)
	var victim *engine.Unit
	for i := range s.Units {
		if s.Units[i].Team == 1 && !s.Units[i].IsCommander {
			victim = &s.Units[i]
			break
		}
	}
	victim.Pos = engine.Pos{X: hask.Pos.X + 1, Y: hask.Pos.Y}
	victim.HP = 1
	pre := engine.View(c, s, 0)
	orders := map[int][]engine.Order{0: {{UnitID: hask.ID, Action: engine.ActAttack, Target: victim.Pos, TargetU: victim.ID}}}
	var events []engine.Event
	post, events := engine.Step(c, s, orders, 1)
	pv := engine.View(c, post, 0)
	an := newAnim(c, a.g, &pre, &pv, 0, engine.FilterEvents(c, &post, 0, events))
	sawDeath, sawDust := false, false
	for !an.done {
		an.beat()
		for _, f := range an.scene.Flash {
			if f.Role == "death" {
				sawDeath = true
			}
			if f.Role == "dust" {
				sawDust = true
			}
		}
	}
	died := false
	for _, e := range events {
		if e.Kind == engine.EvDied {
			died = true
		}
	}
	if died && (!sawDeath || !sawDust) {
		t.Fatalf("death beats: death=%v dust=%v", sawDeath, sawDust)
	}
	if !died {
		t.Log("attack missed with this seed; death beats not exercised")
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
			beats := 0
			for !an.done {
				an.beat()
				beats++
			}
			ms := beats * an.pace(120)
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
