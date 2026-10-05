package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/charmbracelet/x/term"

	"rfog/client"
	"rfog/data"
	"rfog/engine"
	"rfog/render"
)

// runBench measures where the time goes: building a frame, and the
// terminal drawing it. If the first number is small and the second is
// large, the terminal is the slow part — try a smaller window, a
// different terminal, or `-cells small`.
func runBench(args []string) error {
	fs := flag.NewFlagSet("rfog bench", flag.ContinueOnError)
	frames := fs.Int("n", 100, "frames to render")
	cells := fs.String("cells", "", "cell size to force: auto small large")
	if err := fs.Parse(args); err != nil {
		return err
	}
	c, err := data.Load()
	if err != nil {
		return err
	}
	set := client.LoadSettings()
	if *cells != "" {
		set.Cells = *cells
	}
	env := client.EnvFromOS()
	w, h := terminalSize()
	env.Width, env.Height = w, h
	a := client.NewBench(client.Options{Content: c, Settings: set, Env: env, NoSave: true}, engine.Setup{
		ID: "bench", Mode: "1v1", Seed: 7, TimeControl: "bots",
		Players: []engine.SetupPlayer{{Name: "you", Hero: "hask"}, {Name: "bot", Hero: "wren"}},
	})
	if a == nil {
		return fmt.Errorf("could not start a match to measure")
	}
	fmt.Printf("terminal %dx%d  TERM=%s COLORTERM=%s  tier %s  cells %s\n",
		w, h, os.Getenv("TERM"), os.Getenv("COLORTERM"), render.Probe(env), set.CellsOrAuto())

	// 1. Building frames, with nothing drawn.
	start := time.Now()
	var last string
	for i := 0; i < *frames; i++ {
		last = a.Frame()
	}
	build := time.Since(start) / time.Duration(*frames)

	// 2. The same frames, written to the terminal.
	var buf bytes.Buffer
	buf.WriteString("\x1b[?1049h") // alt screen: do not scroll the session away
	start = time.Now()
	for i := 0; i < *frames; i++ {
		buf.Reset()
		buf.WriteString("\x1b[H")
		buf.WriteString(last)
		if _, err := os.Stdout.Write(buf.Bytes()); err != nil {
			return err
		}
	}
	draw := time.Since(start) / time.Duration(*frames)
	os.Stdout.WriteString("\x1b[?1049l")

	fmt.Printf("build a frame: %6.2f ms   (%d bytes)\n", float64(build.Microseconds())/1000, len(last))
	fmt.Printf("draw a frame:  %6.2f ms   %s\n", float64(draw.Microseconds())/1000, verdict(build, draw))
	return nil
}

func verdict(build, draw time.Duration) string {
	switch {
	case draw > 3*build && draw > 8*time.Millisecond:
		return "← the terminal is the slow part: try a smaller window, another terminal, or -cells small"
	case build > 8*time.Millisecond:
		return "← building frames is slow; please report this with the numbers above"
	}
	return "← both fine"
}

// terminalSize asks the terminal how big it is, falling back to 100x30.
func terminalSize() (int, int) {
	w, h, err := term.GetSize(os.Stdout.Fd())
	if err != nil || w <= 0 || h <= 0 {
		return 100, 30
	}
	return w, h
}
