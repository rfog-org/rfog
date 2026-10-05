// Package balance runs bot-vs-bot matches and reports win rates. It is a
// tuning tool, not part of the game: it calls the same engine and bots
// everything else does, so its numbers are the real ones.
package balance

import (
	"fmt"
	"io"
	"sort"

	"rfog/bots"
	"rfog/engine"
)

// Options configure a run.
type Options struct {
	Mode    string // 1v1 .. 5v5
	Map     string // "" = the mode's own map
	Matches int    // matches per hero pairing (both sides)
	Level   string // bot level
	Seed    uint64
	Heroes  []string // defaults to every hero
}

// HeroResult is one hero's record across the run.
type HeroResult struct {
	Hero              string  `json:"hero"`
	Games             int     `json:"games"`
	Wins              int     `json:"wins"`
	Draws             int     `json:"draws"`
	WinRate           float64 `json:"win_rate"`
	AvgScore          float64 `json:"avg_score"`
	AvgKills          float64 `json:"avg_kills"`
	CommanderDeaths   int     `json:"commander_deaths"`
	AvgCommanderLevel float64 `json:"avg_commander_level"`
}

// Report is the whole run.
type Report struct {
	Mode     string       `json:"mode"`
	Level    string       `json:"level"`
	Matches  int          `json:"matches"`
	Turns    float64      `json:"avg_turns"`
	Draws    int          `json:"draws"`
	FirstWin float64      `json:"first_side_win_rate"` // team 0's win rate: a symmetry check
	Heroes   []HeroResult `json:"heroes"`
}

// Run plays the matches and returns the report. progress, if non-nil, is
// written one line per pairing.
func Run(c *engine.Content, o Options, progress io.Writer) (Report, error) {
	if o.Mode == "" {
		o.Mode = "1v1"
	}
	if o.Matches <= 0 {
		o.Matches = 10
	}
	if o.Level == "" {
		o.Level = bots.Normal
	}
	if len(o.Heroes) == 0 {
		o.Heroes = c.HeroIDs()
	}
	mode, ok := c.Rules.Modes[o.Mode]
	if !ok {
		return Report{}, fmt.Errorf("unknown mode %s", o.Mode)
	}
	per := mode.PlayersPerTeam
	rep := Report{Mode: o.Mode, Level: o.Level}
	acc := map[string]*HeroResult{}
	for _, h := range o.Heroes {
		acc[h] = &HeroResult{Hero: h}
	}
	totalTurns, games, firstWins := 0, 0, 0
	seed := o.Seed
	if seed == 0 {
		seed = 1
	}
	for i, a := range o.Heroes {
		for j, b := range o.Heroes {
			if i == j {
				continue // mirror matches say nothing about balance
			}
			for n := 0; n < o.Matches; n++ {
				seed++
				st := engine.Setup{ID: "balance", Mode: o.Mode, Map: o.Map, Seed: seed, Deterministic: false}
				rng := engine.NewRNG(seed, 99)
				teams := [2][]string{squad(o.Heroes, i, per, rng), squad(o.Heroes, j, per, rng)}
				for _, team := range teams {
					for _, hero := range team {
						st.Players = append(st.Players, engine.SetupPlayer{Name: hero, Hero: hero})
					}
				}
				s, err := engine.NewMatch(c, st)
				if err != nil {
					return Report{}, err
				}
				players := make([]*bots.Bot, len(st.Players))
				for k := range players {
					players[k] = bots.New(o.Level, seed+uint64(k)*7919)
				}
				for !s.Ended() {
					orders := map[int][]engine.Order{}
					for k := range players {
						v := engine.View(c, s, k)
						orders[k] = players[k].Orders(c, &v, k)
					}
					s, _ = engine.Step(c, s, orders, s.Match.Seed)
				}
				games++
				totalTurns += s.Match.Turn
				if s.Match.Winner == 0 {
					firstWins++
				}
				if s.Match.Winner < 0 {
					rep.Draws++
				}
				for team, heroes := range teams {
					for _, hero := range heroes {
						r := acc[hero]
						r.Games++
						switch s.Match.Winner {
						case team:
							r.Wins++
						case -1:
							r.Draws++
						}
						r.AvgScore += float64(s.Teams[team].Score)
						r.AvgKills += float64(s.Teams[team].Kills)
						for _, p := range s.Teams[team].Players {
							if cmd := s.Unit(s.Player(p).Commander); cmd != nil {
								r.AvgCommanderLevel += float64(cmd.Level)
								if !cmd.Alive() {
									r.CommanderDeaths++
								}
							}
						}
					}
				}
			}
			if progress != nil {
				fmt.Fprintf(progress, "  %s vs %s: %d matches\n", a, b, o.Matches)
			}
		}
	}
	for _, h := range o.Heroes {
		r := acc[h]
		if r.Games > 0 {
			r.WinRate = float64(r.Wins) / float64(r.Games)
			r.AvgScore /= float64(r.Games)
			r.AvgKills /= float64(r.Games)
			r.AvgCommanderLevel /= float64(r.Games * per)
		}
		rep.Heroes = append(rep.Heroes, *r)
	}
	sort.Slice(rep.Heroes, func(i, j int) bool { return rep.Heroes[i].WinRate > rep.Heroes[j].WinRate })
	rep.Matches = games
	if games > 0 {
		rep.Turns = float64(totalTurns) / float64(games)
		rep.FirstWin = float64(firstWins) / float64(games)
	}
	return rep, nil
}

// Text renders the report as a table.
// squad is a legal team for a draft: the hero at index first, then
// per-1 others drawn at random, never the same hero twice (drafts forbid
// mirror picks within a team). Random partners keep a hero's numbers from
// measuring one fixed teammate. In 1v1 it is just that hero.
func squad(heroes []string, first, per int, rng *engine.RNG) []string {
	out := []string{heroes[first]}
	rest := make([]string, 0, len(heroes)-1)
	for k, h := range heroes {
		if k != first {
			rest = append(rest, h)
		}
	}
	for len(out) < per && len(rest) > 0 {
		k := rng.Intn(len(rest))
		out = append(out, rest[k])
		rest = append(rest[:k], rest[k+1:]...)
	}
	return out
}

func (r Report) Text() string {
	s := fmt.Sprintf("%s %s: %d matches, %.1f turns average, %d draws, side A wins %.0f%%\n\n",
		r.Mode, r.Level, r.Matches, r.Turns, r.Draws, r.FirstWin*100)
	s += fmt.Sprintf("%-8s %6s %6s %7s %7s %7s %7s\n", "hero", "games", "wins", "win%", "score", "kills", "cmd L")
	for _, h := range r.Heroes {
		s += fmt.Sprintf("%-8s %6d %6d %6.1f%% %7.1f %7.1f %7.2f\n", h.Hero, h.Games, h.Wins, h.WinRate*100, h.AvgScore, h.AvgKills, h.AvgCommanderLevel)
	}
	return s
}
