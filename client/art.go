package client

import (
	"strconv"
	"strings"

	"rfog/engine"
	"rfog/render"
	"rfog/render/art"
	"rfog/render/palette"
)

// artTier is the frame tier for this app. T0 frames (characters, no
// colour) serve mono themes at any tier. Everything with colour uses
// half blocks: braille packs more dots per cell but draws them as
// separated points, which reads as a scattering of pixels on a portrait
// rather than a face.
func (a *App) artTier() string {
	if a.tier == render.T0 || a.st.Theme.Mono || a.env.NoColor {
		return "t0"
	}
	return "t1"
}

// artStyler renders frames through this session's renderer and theme.
func (a *App) artStyler() art.Styler {
	return art.Styler{Renderer: a.st.Renderer, Plain: a.st.Plain, Accent: a.st.Accent, Dim: a.st.Dim, Color: a.artTier() != "t0"}
}

// portrait is a hero's portrait at full size.
func (a *App) portrait(heroID string) art.Frame {
	return a.portraitOf("hero_"+heroID, sizeFull)
}

// Portrait sizes, largest first. The small one is a card for a
// phone-sized panel, where ten rows would cost the whole detail panel.
type portraitSize struct{ cols, rows int }

var (
	sizeFull   = portraitSize{art.PortraitCols, art.PortraitRows}
	sizeMedium = portraitSize{art.MediumCols, art.MediumRows}
	sizeSmall  = portraitSize{art.SmallCols, art.SmallRows}
)

// unitPortrait is a unit's own portrait: the hero's for a commander, the
// unit kind's for anything else.
func (a *App) unitPortrait(u *engine.Unit, sz portraitSize) art.Frame {
	if u.IsCommander {
		return a.portraitOf("hero_"+u.Kind, sz)
	}
	return a.portraitOf("unit_"+u.Kind, sz)
}

// portraitOf is a figure's portrait at a size, drawn once per tier; a
// name with no figure gets static.
func (a *App) portraitOf(id string, sz portraitSize) art.Frame {
	key := id + "/" + a.artTier() + "/" + strconv.Itoa(sz.cols)
	if f, ok := a.portraits[key]; ok {
		return f
	}
	f, ok := a.figurePortrait(id, sz.cols, sz.rows)
	if !ok {
		f = a.staticFrame(sz, 0)
	}
	if a.portraits == nil {
		a.portraits = map[string]art.Frame{}
	}
	a.portraits[key] = f
	return f
}

// staticFrame is a dead channel: shaded noise that shifts with seed, shown
// where there is no unit to look at.
func (a *App) staticFrame(sz portraitSize, seed uint64) art.Frame {
	shades := []rune{' ', '░', '▒', '▓'}
	if a.artTier() == "t0" {
		shades = []rune{' ', '.', ':', '%'}
	}
	f := art.New(a.artTier(), sz.cols, sz.rows)
	x := seed*0x9E3779B97F4A7C15 + 1
	for i := range f.Cells {
		x ^= x << 13
		x ^= x >> 7
		x ^= x << 17
		f.Cells[i] = art.Cell{R: shades[x%uint64(len(shades))], Flags: art.Dim}
	}
	return f
}

// piece is a unit's picture on the board: its figure, lit in the unit's
// team colour so a side reads at a glance, like the two colours of chess
// pieces. Nil means letters: no art, a mono theme, or no colour at all.
func (a *App) piece(u *render.SceneUnit, shade string, cols, rows int) []string {
	return a.figureLines(figureID(u), u.Team, u.Pose, shade, cols, rows, false, u.HP, u.MaxHP)
}

// ghost is a unit's piece faded toward the board: where it is going to be.
func (a *App) ghost(u *render.SceneUnit, shade string, cols, rows int) []string {
	return a.figureLines(figureID(u), u.Team, "", shade, cols, rows, true, 0, 0)
}

// hexColor parses "#rrggbb".
func hexColor(s string) uint32 {
	var v uint32
	for _, ch := range strings.TrimPrefix(s, "#") {
		v <<= 4
		switch {
		case ch >= '0' && ch <= '9':
			v |= uint32(ch - '0')
		case ch >= 'a' && ch <= 'f':
			v |= uint32(ch-'a') + 10
		case ch >= 'A' && ch <= 'F':
			v |= uint32(ch-'A') + 10
		}
	}
	return v
}

// bayer4 is the 4x4 ordered dither: a pixel is lit when its threshold is
// under the level (0 to 16).
var bayer4 = [4][4]int{{0, 8, 2, 10}, {12, 4, 14, 6}, {3, 11, 1, 9}, {15, 7, 13, 5}}

// backdrop is centered on the board's colours, as the web app's screens
// sit on theirs: the lines on a panel of the terminal's own background,
// and round it a vertical dithered gradient from the board's walls at the
// top to half its dark squares at the bottom, in half-block pixels (two to
// a cell, as the figures). Without colour it is centered.
func (a *App) backdrop(lines []string) string { return a.onBackdrop(a.centered(lines), false) }

// onBackdrop puts a laid-out screen (rows already in place) on the
// backdrop (see backdrop); toLight ramps toward the light squares
// instead, the pick screen's floor.
func (a *App) onBackdrop(out string, toLight bool) string {
	t := a.st.Theme
	if a.artTier() == "t0" || t.Light == "" || a.st.Renderer == nil {
		return out
	}
	rows := strings.Split(out, "\n")
	for len(rows) < a.h {
		rows = append(rows, "")
	}
	// The panel: the content's rows, its widest line and a cell of air
	// round it.
	top, bot, x0, x1 := a.h, -1, a.w, 0
	for y, l := range rows {
		body := strings.TrimLeft(l, " ")
		if body == "" {
			continue
		}
		left := len(l) - len(body)
		top, bot = minInt(top, y), maxInt(bot, y)
		x0, x1 = minInt(x0, left), maxInt(x1, left+render.Width(body))
	}
	if bot < 0 {
		top, bot, x0, x1 = a.h, -1, a.w, a.w
	}
	top, bot, x0, x1 = maxInt(top-1, 0), minInt(bot+1, a.h-1), maxInt(x0-2, 0), minInt(x1+2, a.w)
	r := a.st.Renderer
	to := t.Board
	if toLight {
		to = t.Light
	}
	wrap := r.NewStyle().Foreground(palette.Color(r, to)).Background(palette.Color(r, t.Wall)).Render("\x00")
	pre, post := wrap[:strings.Index(wrap, "\x00")], wrap[strings.Index(wrap, "\x00")+1:]
	cells := func(y, from, to int) string {
		if from >= to {
			return ""
		}
		lit := func(py, x int) bool { return bayer4[py%4][x%4] < py*9/(2*a.h) }
		var b strings.Builder
		b.WriteString(pre)
		for x := from; x < to; x++ {
			switch t, u := lit(2*y, x), lit(2*y+1, x); {
			case t && u:
				b.WriteString("█")
			case t:
				b.WriteString("▀")
			case u:
				b.WriteString("▄")
			default:
				b.WriteByte(' ')
			}
		}
		b.WriteString(post)
		return b.String()
	}
	for y := range rows {
		if y < top || y > bot {
			rows[y] = cells(y, 0, a.w)
			continue
		}
		l := rows[y]
		if n := len(l) - len(strings.TrimLeft(l, " ")); n >= x0 {
			l = l[x0:]
		}
		rows[y] = cells(y, 0, x0) + fit(l, x1-x0) + cells(y, x1, a.w)
	}
	return strings.Join(rows[:a.h], "\n")
}

// band is a line of big news w columns wide, in the colour of the side it
// favours (0 the viewer's) on the board's wall colour: the text spaced out,
// the reason under it, dithered edges above and below. A callout over the
// board and the end card's result are bands.
func (a *App) band(text, sub string, side, w int) []string {
	st := &a.st
	fg := st.TeamA
	col := st.Theme.TeamA
	if side != 0 {
		fg, col = st.TeamB, st.Theme.TeamB
	}
	big := strings.ToUpper(text)
	if spaced := strings.Join(strings.Split(big, ""), " "); len(spaced) <= w-2 {
		big = spaced
	}
	rows := []string{centred(big, w)}
	if sub != "" {
		rows = append(rows, centred(sub, w))
	}
	if a.artTier() == "t0" || st.Renderer == nil || st.Theme.Wall == "" {
		edge := st.Dim.Render(strings.Repeat(a.g.H, w))
		out := []string{edge, fg.Bold(true).Render(rows[0])}
		if len(rows) > 1 {
			out = append(out, st.Plain.Render(rows[1]))
		}
		return append(out, edge)
	}
	r := st.Renderer
	bg := palette.Color(r, st.Theme.Wall)
	edge := r.NewStyle().Foreground(palette.Color(r, col)).Background(bg)
	out := []string{edge.Render(render.Dither(1, 0, w)), r.NewStyle().Foreground(palette.Color(r, col)).Background(bg).Bold(true).Render(rows[0])}
	if len(rows) > 1 {
		out = append(out, r.NewStyle().Foreground(palette.Color(r, st.Theme.Fg)).Background(bg).Render(rows[1]))
	}
	return append(out, edge.Render(render.Dither(1, 1, w)))
}
