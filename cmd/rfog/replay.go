package main

import (
	"flag"
	"fmt"
	"os"

	"rfog/data"
	"rfog/engine"
	"rfog/engine/textlog"
)

func runReplay(args []string) error {
	fs := flag.NewFlagSet("replay", flag.ContinueOnError)
	verbose := fs.Bool("v", false, "print every event, including turn markers")
	boards := fs.Bool("board", false, "print the board after each turn")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: rfog replay [-v] [-board] <file>")
	}
	b, err := os.ReadFile(fs.Arg(0))
	if err != nil {
		return err
	}
	rp, err := engine.UnmarshalReplay(b)
	if err != nil {
		return err
	}
	c, err := data.Load()
	if err != nil {
		return err
	}
	s := rp.Initial.Clone()
	fmt.Printf("match %s  mode %s  map %s  seed %d  players:", s.Match.ID, s.Match.Mode, s.Match.Map, s.Match.Seed)
	for _, p := range s.Players {
		cmd := s.Unit(p.Commander)
		fmt.Printf(" %s(%s, team %d)", p.Name, cmd.Name, p.Team)
	}
	fmt.Println()
	if *boards {
		fmt.Print(textlog.Board(c, &s))
	}
	for i, orders := range rp.Turns {
		if s.Ended() {
			return fmt.Errorf("turn %d: orders after match end", i+1)
		}
		var ev []engine.Event
		s, ev = engine.Step(c, s, orders, s.Match.Seed)
		for _, e := range ev {
			line := textlog.Event(c, &s, e)
			if line == "" {
				continue
			}
			if !*verbose && (e.Kind == engine.EvTurnStart || e.Kind == engine.EvTurnEnd) {
				continue
			}
			fmt.Println(line)
		}
		if *boards {
			fmt.Print(textlog.Board(c, &s))
		}
	}
	fmt.Printf("final: %s  hash %s\n", textlog.Result(&s), engine.Hash(s))
	if rp.FinalHash != "" && rp.FinalHash != engine.Hash(s) {
		return fmt.Errorf("final hash mismatch: replay says %s", rp.FinalHash)
	}
	return nil
}
