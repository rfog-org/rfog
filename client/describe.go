package client

import (
	"fmt"
	"strings"

	"rfog/engine"
)

// Ability text in sentences, written from the rules data, as the web
// app's Commanders page writes it: what the ability does (abilityText) and
// how it is aimed (abilityMeta). Both follow the data when numbers change.

// abilityText says what an ability does.
func abilityText(c *engine.Content, ab engine.AbilityDef) string {
	who := map[string]string{"enemies": "enemies", "allies": "allies", "all": "everyone", "self": "itself"}[ab.Filter]
	turns := func(n int) string {
		if n == 0 {
			return ""
		}
		if n == 1 {
			return " for 1 turn"
		}
		return fmt.Sprintf(" for %d turns", n)
	}
	var parts []string
	for _, e := range ab.Effects {
		var p string
		switch e.Kind {
		case "damage":
			p = fmt.Sprintf("%d damage", e.N)
			if e.Radius != nil && *e.Radius > 0 {
				f := e.Filter
				if f == "" {
					f = who
				}
				if f == "" {
					f = "units"
				}
				p += " to " + f + " around it"
			}
		case "heal":
			p = fmt.Sprintf("heals %d", e.N)
		case "shield":
			p = fmt.Sprintf("a %d-point shield%s", e.N, turns(e.Turns))
		case "push":
			p = fmt.Sprintf("pushes back %d", e.Dist)
		case "pull":
			p = fmt.Sprintf("pulls %d squares closer", e.Dist)
		case "slow":
			p = fmt.Sprintf("−%d movement%s", e.MV, turns(e.Turns))
		case "root":
			p = "rooted" + turns(e.Turns)
		case "silence":
			p = "can't cast" + turns(e.Turns)
		case "taunt":
			p = "must attack the caster" + turns(e.Turns)
		case "cloak":
			p = "cloaked" + turns(e.Turns) + " (unseen unless adjacent)"
		case "mark":
			p = fmt.Sprintf("marked: attacks on it roll +%d dice%s", e.Dice, turns(e.Turns))
		case "reveal":
			r := radius(e, ab)
			p = fmt.Sprintf("reveals a %dx%d area%s", 2*r+1, 2*r+1, turns(e.Turns))
			if ab.Visible != nil && !*ab.Visible {
				p += ", even through fog"
			}
		case "smoke":
			p = "smoke that blocks sight" + turns(e.Turns)
		case "stat":
			p = fmt.Sprintf("%+d %s%s", e.Delta, strings.ToUpper(e.Stat), turns(e.Turns))
		case "teleport":
			p = "teleports there"
			if e.Who == "target" {
				p = "brings the ally to its side"
			}
		case "spawn":
			name := e.Unit
			if u, ok := c.Units[e.Unit]; ok {
				name = u.Name
			}
			n := maxInt(e.Count, 1)
			if n > 1 {
				p = fmt.Sprintf("%d %ss appear", n, name)
			} else {
				p = "a " + name + " appears"
			}
			if u, ok := c.Units[e.Unit]; ok && u.Expires > 0 {
				if n > 1 {
					p += fmt.Sprintf(" (they last %d turns)", u.Expires)
				} else {
					p += fmt.Sprintf(" (it lasts %d turns)", u.Expires)
				}
			}
		case "overwatch":
			shots := maxInt(e.Shots, 1)
			p = fmt.Sprintf("%d overwatch shot", shots)
			if shots > 1 {
				p += "s"
			}
			if e.RNG > 0 {
				p += fmt.Sprintf(" at range %d", e.RNG)
			}
		case "approach":
			p = fmt.Sprintf("jumps up to %d squares to the target", e.Dist)
		case "strike":
			p = "and attacks it"
		case "sacrifice":
			p = "the unit is used up"
		default:
			p = e.Kind
		}
		parts = append(parts, p)
	}
	s := strings.Join(parts, ", ")
	if s == "" {
		return ""
	}
	return capital(s) + "."
}

// abilityMeta is how an ability is aimed: target, range, area, timing,
// cooldown.
func abilityMeta(ab engine.AbilityDef) string {
	var parts []string
	switch {
	case ab.Target == engine.TargetSelf:
		parts = append(parts, "on itself")
	case ab.Target == engine.TargetAll:
		t := "all allies"
		if ab.Kind != "" {
			t += " (" + ab.Kind + "s)"
		}
		parts = append(parts, t)
	default:
		what := "a square"
		if ab.Target == engine.TargetUnit {
			what = "a unit"
		}
		switch {
		case ab.Range == 1:
			parts = append(parts, "an adjacent "+strings.TrimPrefix(what, "a "))
		case ab.Range > 0:
			parts = append(parts, fmt.Sprintf("%s up to %d away", what, ab.Range))
		default:
			parts = append(parts, what)
		}
	}
	if ab.Area > 0 {
		parts = append(parts, fmt.Sprintf("%dx%d area", 2*ab.Area+1, 2*ab.Area+1))
	}
	switch {
	case ab.Delay == 1:
		parts = append(parts, "lands next turn")
	case ab.Delay > 1:
		parts = append(parts, fmt.Sprintf("lands in %d turns", ab.Delay))
	}
	if ab.Cooldown > 0 {
		parts = append(parts, fmt.Sprintf("cooldown %d", ab.Cooldown))
	}
	return strings.Join(parts, " · ")
}

// wrapWords breaks text into lines of at most width runes, at spaces.
func wrapWords(s string, width int) []string {
	if width < 10 {
		width = 10
	}
	var out []string
	line := ""
	for _, w := range strings.Fields(s) {
		switch {
		case line == "":
			line = w
		case len([]rune(line))+1+len([]rune(w)) <= width:
			line += " " + w
		default:
			out = append(out, line)
			line = w
		}
	}
	if line != "" {
		out = append(out, line)
	}
	return out
}
