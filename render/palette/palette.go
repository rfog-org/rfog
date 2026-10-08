// Package palette matches colours to what a terminal can show. Terminals
// without 24-bit colour (macOS Terminal.app among them) take the xterm
// 256-colour palette; rounding there channel by channel (what the colour
// libraries do) turns cold slate into purple and greys into each other, so
// colours are matched by eye (CIE Lab) and sent as palette indexes.
package palette

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// Color is v as r should send it: on a 256-colour renderer a "#rrggbb" is
// replaced by the nearest palette index; anything else passes through.
func Color(r *lipgloss.Renderer, v string) lipgloss.Color {
	if r != nil && strings.HasPrefix(v, "#") && r.ColorProfile() == termenv.ANSI256 {
		return lipgloss.Color(strconv.Itoa(Index256(v)))
	}
	return lipgloss.Color(v)
}

// Snap256 is the xterm-256 colour nearest hex, as "#rrggbb" ("" stays "").
func Snap256(hex string) string {
	if _, _, _, ok := parseHex(hex); !ok {
		return hex
	}
	return xtermHex[Index256(hex)-16]
}

// SnapDistinct is the xterm-256 colour nearest hex that is not in used, as
// "#rrggbb": for a set of colours that must stay apart (a board's squares,
// walls and objectives) when two of them would round to the same one.
func SnapDistinct(hex string, used map[string]bool) string {
	r, g, b, ok := parseHex(hex)
	if !ok {
		return hex
	}
	if s := Snap256(hex); !used[s] {
		return s
	}
	want := lab(r, g, b)
	snapMu.Lock()
	defer snapMu.Unlock()
	best, bestD := "", math.MaxFloat64
	for i, c := range xterm {
		if used[xtermHex[i]] {
			continue
		}
		d := (c[0]-want[0])*(c[0]-want[0]) + (c[1]-want[1])*(c[1]-want[1]) + (c[2]-want[2])*(c[2]-want[2])
		if d < bestD {
			best, bestD = xtermHex[i], d
		}
	}
	return best
}

var (
	snapMu    sync.Mutex
	snapCache = map[string]int{}
	xterm     [][3]float64 // the 6x6x6 cube and the 24 greys, in Lab
	xtermHex  []string
)

// Index256 is the xterm-256 palette index nearest hex by eye (16-255;
// 16 for anything unparsable).
func Index256(hex string) int {
	r, g, b, ok := parseHex(hex)
	if !ok {
		return 16
	}
	snapMu.Lock()
	defer snapMu.Unlock()
	if i, ok := snapCache[hex]; ok {
		return i
	}
	if xterm == nil {
		levels := []int{0, 95, 135, 175, 215, 255}
		add := func(r, g, b int) {
			xterm = append(xterm, lab(r, g, b))
			xtermHex = append(xtermHex, fmt.Sprintf("#%02x%02x%02x", r, g, b))
		}
		for _, r := range levels {
			for _, g := range levels {
				for _, b := range levels {
					add(r, g, b)
				}
			}
		}
		for i := 0; i < 24; i++ {
			v := 8 + 10*i
			add(v, v, v)
		}
	}
	want := lab(r, g, b)
	best, bestD := 0, math.MaxFloat64
	for i, c := range xterm {
		d := (c[0]-want[0])*(c[0]-want[0]) + (c[1]-want[1])*(c[1]-want[1]) + (c[2]-want[2])*(c[2]-want[2])
		if d < bestD {
			best, bestD = i, d
		}
	}
	snapCache[hex] = best + 16
	return best + 16
}

func parseHex(s string) (r, g, b int, ok bool) {
	s = strings.TrimPrefix(s, "#")
	if len(s) != 6 {
		return 0, 0, 0, false
	}
	_, err := fmt.Sscanf(s, "%02x%02x%02x", &r, &g, &b)
	return r, g, b, err == nil
}

// lab converts sRGB to CIE L*a*b* (D65).
func lab(r, g, b int) [3]float64 {
	lin := func(c int) float64 {
		v := float64(c) / 255
		if v <= 0.04045 {
			return v / 12.92
		}
		return math.Pow((v+0.055)/1.055, 2.4)
	}
	R, G, B := lin(r), lin(g), lin(b)
	x := (0.4124*R + 0.3576*G + 0.1805*B) / 0.95047
	y := 0.2126*R + 0.7152*G + 0.0722*B
	z := (0.0193*R + 0.1192*G + 0.9505*B) / 1.08883
	f := func(t float64) float64 {
		if t > 0.008856 {
			return math.Cbrt(t)
		}
		return 7.787*t + 16.0/116
	}
	return [3]float64{116*f(y) - 16, 500 * (f(x) - f(y)), 200 * (f(y) - f(z))}
}
