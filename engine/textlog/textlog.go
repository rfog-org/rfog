// Package textlog renders engine events and boards as plain text. Used by
// `rfog replay`, tests and the client's log pane.
package textlog

import (
	"fmt"
	"strings"

	"rfog/engine"
)

func pos(p engine.Pos) string { return fmt.Sprintf("%c%d", 'a'+p.X, p.Y+1) }

func unitName(s *engine.State, id int) string {
	u := s.Unit(id)
	if u == nil {
		return fmt.Sprintf("#%d", id)
	}
	return fmt.Sprintf("%s#%d", u.Name, u.ID)
}

func abilityName(c *engine.Content, id string) string {
	if a, ok := c.Abilities[id]; ok {
		return a.Name
	}
	return id
}

// Event renders one event as a single log line, or "" if it is silent.
func Event(c *engine.Content, s *engine.State, e engine.Event) string {
	t := fmt.Sprintf("T%02d ", e.Turn)
	switch e.Kind {
	case engine.EvTurnStart:
		return t + "--- turn start ---"
	case engine.EvTurnEnd:
		return t + "--- turn end ---"
	case engine.EvMoved:
		return t + fmt.Sprintf("%s moves %s -> %s (%d)", unitName(s, e.Unit), pos(e.From), pos(e.To), len(e.Path))
	case engine.EvAttacked:
		dice := "ev"
		if len(e.Dice) > 0 {
			var parts []string
			for _, d := range e.Dice {
				parts = append(parts, fmt.Sprint(d))
			}
			dice = strings.Join(parts, " ")
		}
		return t + fmt.Sprintf("%s %s %s at %s: [%s] vs %d+ = %d hit", unitName(s, e.Unit), e.Reason, unitName(s, e.Target), pos(e.To), dice, e.Amount, e.Hits)
	case engine.EvDamaged:
		sh := ""
		if e.Hits > 0 {
			sh = fmt.Sprintf(", shield absorbed %d", e.Hits)
		}
		u := s.Unit(e.Target)
		hp := ""
		if u != nil {
			hp = fmt.Sprintf(" (%d/%d)", u.HP, u.MaxHP)
		}
		return t + fmt.Sprintf("%s takes %d %s%s%s", unitName(s, e.Target), e.Amount, e.Reason, sh, hp)
	case engine.EvHealed:
		return t + fmt.Sprintf("%s heals %s for %d", unitName(s, e.Unit), unitName(s, e.Target), e.Amount)
	case engine.EvDied:
		return t + fmt.Sprintf("%s dies at %s (%s)", unitName(s, e.Unit), pos(e.From), e.Reason)
	case engine.EvExpired:
		return t + fmt.Sprintf("%s expires at %s", unitName(s, e.Unit), pos(e.From))
	case engine.EvSpawned:
		return t + fmt.Sprintf("%s spawns at %s", unitName(s, e.Unit), pos(e.To))
	case engine.EvAbilityCast:
		return t + fmt.Sprintf("%s casts %s at %s", unitName(s, e.Unit), abilityName(c, e.Ability), pos(e.To))
	case engine.EvAbilityResolved:
		return t + fmt.Sprintf("%s resolves at %s", abilityName(c, e.Ability), pos(e.To))
	case engine.EvTelegraph:
		return t + fmt.Sprintf("telegraph: %s lands at %s in %d turn(s)", abilityName(c, e.Ability), pos(e.To), e.Amount)
	case engine.EvRevealed:
		return t + fmt.Sprintf("team %d reveals %d tiles around %s", e.Team, len(e.Tiles), pos(center(e.Tiles)))
	case engine.EvSmoke:
		return t + fmt.Sprintf("smoke on %d tiles around %s for %d turns", len(e.Tiles), pos(center(e.Tiles)), e.Amount)
	case engine.EvStatus:
		if e.Reason == "expired" {
			return t + fmt.Sprintf("%s: %s wears off", unitName(s, e.Unit), e.Status)
		}
		st := e.Status
		if st == "stat" {
			st = fmt.Sprintf("%s %+d", strings.ToUpper(e.Reason), e.Amount)
		} else if e.Amount != 1 {
			st = fmt.Sprintf("%s %d", st, e.Amount)
		}
		return t + fmt.Sprintf("%s gets %s for %d turn(s)", unitName(s, e.Target), st, e.Hits)
	case engine.EvPushed:
		return t + fmt.Sprintf("%s is shoved %s -> %s", unitName(s, e.Unit), pos(e.From), pos(e.To))
	case engine.EvTeleported:
		return t + fmt.Sprintf("%s jumps %s -> %s", unitName(s, e.Unit), pos(e.From), pos(e.To))
	case engine.EvObjectiveScored:
		return t + fmt.Sprintf("team %d +%d (%s)", e.Team, e.Amount, e.Reason)
	case engine.EvLevelUp:
		return t + fmt.Sprintf("%s reaches level %d", unitName(s, e.Unit), e.Amount)
	case engine.EvRespawned:
		return t + fmt.Sprintf("%s respawns at %s", unitName(s, e.Unit), pos(e.To))
	case engine.EvOrderRejected:
		return t + fmt.Sprintf("order rejected for %s: %s", unitName(s, e.Unit), e.Reason)
	case engine.EvOverwatch:
		return t + fmt.Sprintf("%s on overwatch", unitName(s, e.Unit))
	case engine.EvHold:
		return t + fmt.Sprintf("%s holds", unitName(s, e.Unit))
	case engine.EvMatchEnd:
		return t + "match over: " + Result(s)
	}
	return t + e.Kind
}

func center(ps []engine.Pos) engine.Pos {
	if len(ps) == 0 {
		return engine.Pos{}
	}
	return ps[len(ps)/2]
}

// Result describes the match outcome.
func Result(s *engine.State) string {
	if !s.Ended() {
		return fmt.Sprintf("in progress, turn %d, score %d-%d", s.Match.Turn, s.Teams[0].Score, s.Teams[1].Score)
	}
	if s.Match.Winner < 0 {
		return fmt.Sprintf("draw %d-%d", s.Teams[0].Score, s.Teams[1].Score)
	}
	return fmt.Sprintf("team %d wins by %s, %d-%d", s.Match.Winner, s.Match.Result, s.Teams[0].Score, s.Teams[1].Score)
}

// Board renders the board in ASCII: terrain, height and units (team 0 upper
// case, team 1 lower case, first letter of the kind; commanders use '@'/'&').
func Board(c *engine.Content, s *engine.State) string {
	var b strings.Builder
	b.WriteString("    ")
	for x := 0; x < s.Board.W; x++ {
		b.WriteByte(byte('a' + x))
	}
	b.WriteString("\n")
	for y := 0; y < s.Board.H; y++ {
		fmt.Fprintf(&b, "%3d ", y+1)
		for x := 0; x < s.Board.W; x++ {
			p := engine.Pos{X: x, Y: y}
			if u := s.UnitAt(p); u != nil {
				ch := u.Kind[0]
				if u.IsCommander {
					ch = '@'
				}
				if u.Team == 0 {
					ch = byte(strings.ToUpper(string(ch))[0])
				} else if u.IsCommander {
					ch = '&'
				}
				b.WriteByte(ch)
				continue
			}
			if s.Smoked(p) {
				b.WriteByte('~')
				continue
			}
			t := s.Board.At(p)
			switch t.Terrain {
			case engine.TerrainWall:
				b.WriteByte('#')
			case engine.TerrainCover:
				b.WriteByte('%')
			case engine.TerrainObjective:
				b.WriteByte('*')
			default:
				if t.Z > 0 {
					b.WriteByte(byte('0' + t.Z))
				} else {
					b.WriteByte('.')
				}
			}
		}
		b.WriteString("\n")
	}
	fmt.Fprintf(&b, "turn %d  score %d-%d\n", s.Match.Turn, s.Teams[0].Score, s.Teams[1].Score)
	return b.String()
}
