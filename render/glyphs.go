package render

import "strings"

// Glyphs is the glyph set for one tier. Chosen for legibility first: an
// outsider must read the board in five seconds. Team identity is carried by
// case (A upper, B lower) so it survives monochrome; colour is additive.
type Glyphs struct {
	Open      [4]string // ground by height
	Cover     string
	Wall      string
	Objective string
	Smoke     string
	SmokeAlt  string // ambient drift frame
	Fog       string
	Telegraph string
	Ghost     string // last-seen enemy marker
	Cursor    string // cell marker when the cursor sits on empty ground (unused; cursor is a style)
	Deploy    string
	// Box drawing.
	H, V, TL, TR, BL, BR string
	Bar                  string // progress fill
	BarEmpty             string
	Commander            string // prefix marker for commanders in lists
	// Animation marks.
	Death   string // where a unit died this beat
	Dust    string // the beat after
	Cast    string // an ability going off
	Beam    string // a shot crossing a square
	Checker string // fills the shaded half of a tall tile
	// A unit's health, drawn under it on a tall tile.
	HP, HPEmpty string
	// The selected unit's breathing pointers, and a footstep of last
	// turn's moves.
	PulseL, PulseR, Step string
	// Reach marks a square the selected unit can reach, PathDot one on its
	// path (the web board's move dots).
	Reach, PathDot string
}

var glyphSets = map[Tier]Glyphs{
	T0: {
		Open: [4]string{".", "-", "=", "^"}, Cover: "%", Wall: "#", Objective: "*", Smoke: "~", SmokeAlt: "-", Fog: " ",
		Telegraph: "!", Ghost: "?", Cursor: "+", Deploy: ",",
		H: "-", V: "|", TL: "+", TR: "+", BL: "+", BR: "+", Bar: "#", BarEmpty: ".", Commander: "@",
		Death: "X", Dust: ":", Cast: "!", Beam: "-", Checker: ".", HP: "=", HPEmpty: "-", PulseL: ">", PulseR: "<", Step: ".",
		Reach: "+", PathDot: "o",
	},
	T1: {
		Open: [4]string{"·", "░", "▒", "▓"}, Cover: "▞", Wall: "█", Objective: "◆", Smoke: "≋", SmokeAlt: "≈", Fog: " ",
		Telegraph: "✕", Ghost: "?", Cursor: "+", Deploy: "·",
		H: "─", V: "│", TL: "╭", TR: "╮", BL: "╰", BR: "╯", Bar: "█", BarEmpty: "░", Commander: "◉",
		Death: "✖", Dust: "∴", Cast: "◌", Beam: "•", Checker: "·", HP: "━", HPEmpty: "╌", PulseL: "▸", PulseR: "◂", Step: "∙",
		Reach: "●", PathDot: "■",
	},
	T2: {
		Open: [4]string{"·", "░", "▒", "▓"}, Cover: "▞", Wall: "█", Objective: "◆", Smoke: "≋", SmokeAlt: "≈", Fog: " ",
		Telegraph: "✕", Ghost: "?", Cursor: "+", Deploy: "·",
		H: "─", V: "│", TL: "╭", TR: "╮", BL: "╰", BR: "╯", Bar: "█", BarEmpty: "░", Commander: "◉",
		// Braille dust and a ring for casts: the T2 extras are marks only,
		// never a different reading of the board.
		Death: "⣿", Dust: "⠛", Cast: "◎", Beam: "•", Checker: "·", HP: "━", HPEmpty: "╌", PulseL: "▸", PulseR: "◂", Step: "∙",
		Reach: "●", PathDot: "■",
	},
}

// GlyphsFor returns the glyph set for a tier.
func GlyphsFor(t Tier) Glyphs {
	if g, ok := glyphSets[t]; ok {
		return g
	}
	return glyphSets[T1]
}

// UnitGlyph returns the board letter for a unit kind. Team B is lowercased
// by the renderer. Commanders show as their hero's first letter, marked by
// the commander style.
func UnitGlyph(kind string, commander bool) string {
	if commander {
		if kind == "" {
			return "@"
		}
		return strings.ToUpper(string(kind[0]))
	}
	switch kind {
	case "lineman":
		return "L"
	case "ranged":
		return "R"
	case "runner":
		return "U"
	case "medic":
		return "M"
	case "junkbot":
		return "J"
	}
	if kind == "" {
		return "?"
	}
	return strings.ToUpper(string(kind[0]))
}

// Dither is n cells, starting at column x, shaded at level 1 to 3 (a
// quarter, half, three quarters): an ordered 2x2 dither on the half-block
// pixels the figures are drawn in, the foreground the shade and the
// background what shows through. Shade characters (░▒▓) draw as fine
// stipple in some terminals and coarse in others; half blocks look the
// same in all.
func Dither(level, x, n int) string {
	pair := [4][2]string{{}, {"▀", " "}, {"▀", "▄"}, {"▀", "█"}}[min(max(level, 1), 3)]
	var b strings.Builder
	for i := 0; i < n; i++ {
		b.WriteString(pair[(x+i)%2])
	}
	return b.String()
}
