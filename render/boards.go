package render

import (
	"fmt"
	"math"
	"strconv"
)

// Board is a board theme: the light squares, the dark squares and the
// walls. Both clients offer the same boards in the same order (the web
// app's copy is BOARDS in web/play.js; web/boards_test.go keeps the two
// equal), and every other colour a terminal theme needs is derived from
// these three, so a board looks the same wherever it is played.
//
// The figures are dark silhouettes, so a board's squares must be light
// enough to show them; TestBoardsReadable sets the bar.
type Board struct {
	Name, Light, Dark, Wall string
}

// Boards are the board themes, fibre (the default) first.
var Boards = []Board{
	{"fibre", "#52778a", "#3d5a6b", "#0d161c"},
	{"graphite", "#6e6e6c", "#555553", "#1a1a19"},
	{"daylight", "#9aa3ab", "#6c757d", "#22262a"},
	{"abyss", "#5d6976", "#4a5561", "#0b0d10"},
	{"nord", "#81a1c1", "#5e81ac", "#2e3440"},
	{"gruvbox", "#a89984", "#7c6f64", "#282828"},
	{"catppuccin", "#9399b2", "#6c7086", "#11111b"},
	{"tokyo-night", "#737aa2", "#565f89", "#16161e"},
	{"amber", "#b08a3a", "#7d5f22", "#1f1606"},
	{"green", "#3f8a46", "#2f6b35", "#0a1f0c"},
}

// chrome is everything that is not the board: text, the two sides, the
// signal colour. It is fibre's on every board, as in the web app, so the
// sides and the interface read the same whichever board is chosen.
var chrome = Theme{
	Fg: "#cdd5dc", Dim: "#76808a", Accent: "#4fd3e8", TeamA: "#4fd3e8", TeamB: "#ff7043",
	Danger: "#ff5a4a", Warn: "#e3c05c", Good: "#7ed98a", Objective: "#e6f0f5", Smoke: "#aab4bc",
	Cover: "#c9a13a", Cursor: "#1b8ea3", Border: "#3a4552",
}

// BoardTheme is the terminal theme for a board: fibre's chrome, and the
// board's colours worked out as the web app draws them (raised ground
// lighter, unseen squares at 45% brightness, held objectives tinted by
// side).
func BoardTheme(b Board) Theme {
	t := chrome
	t.Name = b.Name
	t.Light, t.Board, t.Wall = b.Light, b.Dark, b.Wall
	for z := range t.Ground {
		t.Ground[z] = mix(b.Light, "#ffffff", 0.18+0.12*float64(z))
	}
	t.Fog = scale(mix(b.Light, b.Dark, 0.5), 0.45)
	t.WallTop = scale(mix(grey(b.Dark), b.Dark, 0.35), 1.2)
	t.ObjBg = mix(b.Light, "#ffffff", 0.3)
	t.ObjA = mix(b.Dark, chrome.TeamA, 0.35)
	t.ObjB = mix(b.Dark, chrome.TeamB, 0.45)
	t.ObjC = mix(b.Dark, chrome.Warn, 0.45)
	t.LastBg = mix(b.Light, chrome.Accent, 0.2)
	t.LedgeBg = scale(b.Dark, 0.65)
	t.CastBg = scale(b.Dark, 0.85)
	t.Highlight = mix(scale(b.Dark, 0.7), chrome.Accent, 0.2)
	return t
}

// mix is a + (b-a)*f per channel, as hex.
func mix(a, b string, f float64) string {
	x, y := hexRGB(a), hexRGB(b)
	var o [3]float64
	for i := range o {
		o[i] = x[i] + (y[i]-x[i])*f
	}
	return rgbHex(o)
}

// grey is a colour's luma as a grey.
func grey(a string) string {
	x := hexRGB(a)
	y := 0.3*x[0] + 0.59*x[1] + 0.11*x[2]
	return rgbHex([3]float64{y, y, y})
}

// scale multiplies every channel (CSS brightness()).
func scale(a string, f float64) string {
	x := hexRGB(a)
	return rgbHex([3]float64{x[0] * f, x[1] * f, x[2] * f})
}

func hexRGB(h string) [3]float64 {
	var o [3]float64
	if len(h) != 7 || h[0] != '#' {
		return o
	}
	for i := range o {
		v, _ := strconv.ParseUint(h[1+2*i:3+2*i], 16, 8)
		o[i] = float64(v)
	}
	return o
}

func rgbHex(c [3]float64) string {
	ch := func(v float64) int { return int(math.Max(0, math.Min(255, math.Round(v)))) }
	return fmt.Sprintf("#%02x%02x%02x", ch(c[0]), ch(c[1]), ch(c[2]))
}

// Contrast is the WCAG contrast ratio between two hex colours (1 to 21).
func Contrast(a, b string) float64 {
	la, lb := luminance(a), luminance(b)
	if la < lb {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05)
}

func luminance(h string) float64 {
	c := hexRGB(h)
	lin := func(v float64) float64 {
		v /= 255
		if v <= 0.04045 {
			return v / 12.92
		}
		return math.Pow((v+0.055)/1.055, 2.4)
	}
	return 0.2126*lin(c[0]) + 0.7152*lin(c[1]) + 0.0722*lin(c[2])
}
