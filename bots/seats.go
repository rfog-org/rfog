package bots

import (
	"fmt"

	"rfog/engine"
)

// Lineup is a hero for every seat of a mode: picks[i] for seat i where
// given, and for every other seat one drawn from the seed, as the server's
// draft does for a bot: never a hero already on that seat's team (seats
// [0,n) are team 0, [n,2n) team 1). In 1v1 the draw is the one the offline
// clients have always made for the bot, mirror allowed.
func Lineup(c *engine.Content, mode string, picks []string, seed uint64) ([]string, error) {
	m, ok := c.Rules.Modes[mode]
	if !ok {
		return nil, fmt.Errorf("unknown mode %q", mode)
	}
	per := m.PlayersPerTeam
	heroes := c.HeroIDs()
	out := make([]string, 2*per)
	copy(out, picks)
	rng := engine.NewRNG(seed, 1)
	for i := range out {
		if out[i] != "" {
			continue
		}
		var legal []string
		for _, h := range heroes {
			taken := false
			for j := (i / per) * per; j < (i/per+1)*per; j++ {
				taken = taken || out[j] == h
			}
			if !taken {
				legal = append(legal, h)
			}
		}
		if len(legal) == 0 {
			legal = heroes
		}
		out[i] = legal[rng.Intn(len(legal))]
	}
	return out, nil
}

// Table is the bots of one match, one per seat it fills; each plays on its
// own seat's fog-filtered view, as a bot seat on the server does.
type Table struct {
	seats []int
	bots  map[int]*Bot
}

// NewTable seats a bot at every seat of an n-seat match except the humans'.
// The bot at seat i is seeded seed+i*7919, as the offline clients and the
// server seed theirs.
func NewTable(level string, seed uint64, n int, humans ...int) *Table {
	t := &Table{bots: map[int]*Bot{}}
	for i := 0; i < n; i++ {
		human := false
		for _, h := range humans {
			human = human || h == i
		}
		if !human {
			t.seats = append(t.seats, i)
			t.bots[i] = New(level, seed+uint64(i)*7919)
		}
	}
	return t
}

// Seats are the seats the table plays, in order.
func (t *Table) Seats() []int { return t.seats }

// Orders adds every bot's orders for the turn to orders.
func (t *Table) Orders(c *engine.Content, s engine.State, orders map[int][]engine.Order) {
	for _, i := range t.seats {
		v := engine.View(c, s, i)
		orders[i] = t.bots[i].Orders(c, &v, i)
	}
}

// Dropped is the planned orders the engine would have refused, over every
// bot at the table (zero unless the planner has a bug).
func (t *Table) Dropped() int {
	n := 0
	for _, b := range t.bots {
		n += b.Dropped
	}
	return n
}
