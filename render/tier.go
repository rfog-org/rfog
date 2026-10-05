// Package render turns engine state into text for any terminal tier.
//
// Every tier renders the same scene; only glyphs and colours differ. Nothing
// is size-gated: a small terminal scrolls, it never hides information.
package render

import (
	"github.com/charmbracelet/lipgloss"
	"strings"
)

// Tier is a terminal capability level.
type Tier int

const (
	T0 Tier = iota // 80x24, 16 colours or mono, ASCII
	T1             // 256 colours, Unicode box drawing and shades
	T2             // truecolor, >= 120x40, richer glyphs
)

func (t Tier) String() string {
	switch t {
	case T0:
		return "T0 ascii"
	case T1:
		return "T1 unicode/256"
	}
	return "T2 truecolor"
}

// Env is what Probe looks at. Fill it from os.Getenv and the terminal size.
type Env struct {
	Term      string // TERM
	ColorTerm string // COLORTERM
	Lang      string // LANG / LC_ALL / LC_CTYPE
	Width     int
	Height    int
	NoColor   bool // NO_COLOR set
}

// Probe picks the richest tier the environment supports. Conservative: when
// in doubt, drop a tier. The user can override in settings.
func Probe(e Env) Tier {
	term := strings.ToLower(e.Term)
	utf8 := strings.Contains(strings.ToLower(e.Lang), "utf-8") || strings.Contains(strings.ToLower(e.Lang), "utf8")
	if e.NoColor || term == "" || term == "dumb" || strings.HasPrefix(term, "vt100") || term == "linux" || !utf8 {
		return T0
	}
	colors256 := strings.Contains(term, "256color") || strings.Contains(term, "direct") ||
		strings.HasPrefix(term, "xterm") || strings.HasPrefix(term, "screen") ||
		strings.HasPrefix(term, "tmux") || strings.HasPrefix(term, "alacritty") ||
		strings.HasPrefix(term, "kitty") || strings.HasPrefix(term, "wezterm") ||
		strings.HasPrefix(term, "foot") || strings.HasPrefix(term, "rxvt-unicode") ||
		strings.HasPrefix(term, "ghostty")
	if !colors256 {
		return T0
	}
	ct := strings.ToLower(e.ColorTerm)
	truecolor := ct == "truecolor" || ct == "24bit" || strings.Contains(term, "direct")
	if truecolor && e.Width >= 120 && e.Height >= 40 {
		return T2
	}
	return T1
}

// TrueColor reports whether the terminal takes 24-bit colour. Colour
// depth is the terminal's, not the screen's size: a phone that cannot fit
// a T2 layout still shows the theme's real colours, where rounding them to
// 256 turns slate into purple.
func TrueColor(e Env) bool {
	if e.NoColor {
		return false
	}
	ct := strings.ToLower(e.ColorTerm)
	return ct == "truecolor" || ct == "24bit" || strings.Contains(strings.ToLower(e.Term), "direct")
}

// Fits reports whether a board of w×h tiles (2 columns per tile) fits in a
// viewport of the given size; used to decide between scrolling and not.
func Fits(boardW, boardH, viewW, viewH int) bool {
	return boardW*2 <= viewW && boardH <= viewH
}

// Width is the printed width of a styled string. Plain ASCII (most of
// what the client measures) is counted directly: the ANSI-aware path
// parses the whole string and shows up in every frame.
func Width(s string) int {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 || s[i] == 0x1b {
			return lipgloss.Width(s)
		}
	}
	return len(s)
}
