package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"rfog/client"
	"rfog/data"
	"rfog/tools/balance"
)

// runBalance plays bot-vs-bot matches and prints win rates per hero.
//
//	rfog balance -n 20              # 20 matches per hero pairing, 1v1
//	rfog balance -mode 2v2 -json
func runBalance(args []string) error {
	fs := flag.NewFlagSet("rfog balance", flag.ContinueOnError)
	n := fs.Int("n", 10, "matches per hero pairing")
	mode := fs.String("mode", "1v1", "mode: 1v1 2v2 3v3 4v4 5v5")
	mapID := fs.String("map", "", "map id (default: the mode's own)")
	level := fs.String("level", "normal", "bot level: easy normal hard")
	seed := fs.Uint64("seed", 1, "base seed")
	asJSON := fs.Bool("json", false, "print JSON")
	quiet := fs.Bool("quiet", false, "no progress lines")
	if err := fs.Parse(args); err != nil {
		return err
	}
	c, err := data.Load()
	if err != nil {
		return err
	}
	var progress *os.File
	if !*quiet && !*asJSON {
		progress = os.Stderr
	}
	start := time.Now()
	rep, err := balance.Run(c, balance.Options{Mode: *mode, Map: *mapID, Matches: *n, Level: *level, Seed: *seed}, progress)
	if err != nil {
		return err
	}
	if *asJSON {
		return json.NewEncoder(os.Stdout).Encode(rep)
	}
	fmt.Print(rep.Text())
	fmt.Fprintf(os.Stderr, "\n%.1fs\n", time.Since(start).Seconds())
	return nil
}

// runStatus prints what a status bar wants: ratings and whose turn it is.
//
//	rfog status                 # one line per live match
//	rfog status -json           # machine readable, for tmux/polybar
func runStatus(args []string) error {
	fs := flag.NewFlagSet("rfog status", flag.ContinueOnError)
	server := fs.String("server", "", "server address (default: the saved one)")
	asJSON := fs.Bool("json", false, "print JSON")
	short := fs.Bool("short", false, "one line: matches waiting on you")
	timeout := fs.Duration("timeout", 10*time.Second, "give up after this long")
	if err := fs.Parse(args); err != nil {
		return err
	}
	set := client.LoadSettings()
	addr := set.Server
	if *server != "" {
		addr = *server
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	st, err := client.Status(ctx, addr, set)
	if err != nil {
		if *asJSON {
			return json.NewEncoder(os.Stdout).Encode(map[string]string{"error": err.Error()})
		}
		if *short {
			fmt.Println("rfog: offline")
			return nil
		}
		return err
	}
	switch {
	case *asJSON:
		return json.NewEncoder(os.Stdout).Encode(st)
	case *short:
		fmt.Println(shortStatus(st))
	default:
		fmt.Printf("%s%s  %d online  %s\n", st.Name, guestTag(st.Guest), st.Online, addr)
		for mode, r := range st.Ratings {
			fmt.Printf("  %-7s %4d ±%-3d %d-%d\n", mode, int(r.Rating+0.5), int(r.RD+0.5), r.Wins, r.Games-r.Wins)
		}
		if len(st.Live) == 0 {
			fmt.Println("  no matches in flight")
		}
		for _, m := range st.Live {
			turn := "waiting on them"
			if m.YourTurn {
				turn = "YOUR MOVE"
			}
			left := ""
			if !m.Deadline.IsZero() {
				left = fmt.Sprintf(" (%s left)", time.Until(m.Deadline).Round(time.Minute))
			}
			fmt.Printf("  %-4s %-6s turn %-3d vs %-16s %s%s\n", m.Mode, m.Time, m.Turn, m.Versus, turn, left)
		}
	}
	return nil
}

func guestTag(guest bool) string {
	if guest {
		return " (guest)"
	}
	return ""
}

// shortStatus is the one-line form for a status bar.
func shortStatus(st client.StatusReport) string {
	yours := 0
	for _, m := range st.Live {
		if m.YourTurn {
			yours++
		}
	}
	switch {
	case yours > 0:
		return fmt.Sprintf("rfog: %d to play", yours)
	case len(st.Live) > 0:
		return fmt.Sprintf("rfog: %d in flight", len(st.Live))
	}
	return "rfog: idle"
}
