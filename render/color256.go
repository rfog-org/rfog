package render

import "rfog/render/palette"

// Terminals without 24-bit colour (macOS Terminal.app among them) get the
// 256-colour palette. Rounding a theme's colours channel by channel there
// turns cold slate into purple, so a theme is snapped to the xterm colour
// nearest by eye (CIE Lab) instead, and fibre, the graphical client's
// look, has a hand-picked set.

// Theme256 is t as a 256-colour terminal should draw it: every colour an
// exact xterm palette entry.
func Theme256(t Theme) Theme {
	if t.Mono {
		return t
	}
	if t.Name == "fibre" {
		return fibre256
	}
	// The board's own colours must stay apart; a later one that would round
	// onto an earlier one takes the next nearest.
	used := map[string]bool{}
	for _, p := range []*string{&t.Light, &t.Board, &t.Fog, &t.Wall, &t.WallTop, &t.ObjBg, &t.ObjA, &t.ObjB, &t.ObjC} {
		if *p != "" {
			*p = palette.SnapDistinct(*p, used)
			used[*p] = true
		}
	}
	for _, p := range []*string{&t.Fg, &t.Dim, &t.Accent, &t.TeamA, &t.TeamB, &t.Danger, &t.Warn, &t.Good, &t.Objective,
		&t.Smoke, &t.Cover, &t.Cursor, &t.Highlight, &t.Border, &t.LastBg, &t.LedgeBg, &t.CastBg,
		&t.Ground[0], &t.Ground[1], &t.Ground[2], &t.Ground[3]} {
		*p = palette.Snap256(*p)
	}
	return t
}

// fibre256 is fibre in xterm colours: grey squares in place of slate (the
// palette has no cool greys), the cyan and orange sides, teal and rust
// for held objectives.
var fibre256 = Theme{
	Name: "fibre", Fg: "#d7d7d7", Dim: "#808080", Accent: "#5fd7ff", TeamA: "#5fd7ff", TeamB: "#ff875f",
	Danger: "#ff5f5f", Warn: "#d7af5f", Good: "#87d787", Objective: "#eeeeee", Smoke: "#a8a8a8", Wall: "#1c1c1c",
	Cover: "#af8700", Ground: [4]string{"#808080", "#8a8a8a", "#949494", "#a8a8a8"}, Fog: "#303030",
	Cursor: "#0087af", Highlight: "#005f5f", Border: "#444444", Board: "#4e4e4e", Light: "#626262", WallTop: "#585858",
	ObjBg: "#87afaf", ObjA: "#5f8787", ObjB: "#875f5f",
	ObjC: "#87875f", LastBg: "#5f87af", LedgeBg: "#3a3a3a", CastBg: "#444444",
}
