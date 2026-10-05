package client

import (
	"fmt"
	"runtime"
	"testing"
	"time"

	"rfog/engine"
)

func TestDiagPerf(t *testing.T) {
	a := testApp(t)
	a.w, a.h = 183, 45
	a.set.Tier = "t2"
	a.applyTier()
	lm, _ := newLocalMatch(a.c, engine.Setup{ID: "t", Mode: "1v1", Seed: 7, TimeControl: "bots",
		Players: []engine.SetupPlayer{{Name: "me", Hero: "nul"}, {Name: "bot", Hero: "tally"}}}, [2]bool{false, true}, "normal")
	a.screen = newMatchScreen(a, lm)
	m := a.screen.(*matchScreen)
	m.sel, m.cur = m.myUnits()[0].ID, m.myUnits()[0].Pos

	start := time.Now()
	n := 200
	total := 0
	for i := 0; i < n; i++ {
		total += len(a.View())
	}
	per := time.Since(start) / time.Duration(n)
	fmt.Printf("view: %v per frame, %d bytes per frame\n", per, total/n)

	// Bot turn cost.
	v := lm.view(1)
	start = time.Now()
	for i := 0; i < 50; i++ {
		lm.bots[1].Orders(a.c, &v, 1)
	}
	fmt.Printf("bot orders: %v per turn\n", time.Since(start)/50)

	// Memory across several matches, the way a session accumulates them.
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	for g := 0; g < 5; g++ {
		src, _ := m.src.Rematch("normal")
		a.screen = newMatchScreen(a, src)
		m = a.screen.(*matchScreen)
		for turn := 0; turn < 12 && m.phase != "end"; turn++ {
			for _, u := range m.myUnits() {
				m.sel, m.cur = u.ID, u.Pos
				m.simpleOrder(a, engine.ActHold)
			}
			press(a, " ")
			for i := 0; i < 60 && m.phase == "anim"; i++ {
				ticks(a, 1)
			}
			press(a, ".", "enter")
			a.View()
		}
	}
	runtime.GC()
	runtime.ReadMemStats(&after)
	fmt.Printf("heap after 5 matches: %+.1f MB (goroutines %d)\n",
		float64(after.HeapAlloc-before.HeapAlloc)/1e6, runtime.NumGoroutine())
	fmt.Printf("log lines kept: %d\n", len(m.log))
}
