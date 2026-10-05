package client

import (
	"strings"

	"rfog/assets"
	"rfog/engine"
	"rfog/render"
	"rfog/render/art"
)

// artTier is the frame tier for this app. T0 frames (a density ramp, no
// colour) serve mono themes at any tier. Everything with colour uses the
// half-block frames: braille packs more dots per cell but draws them as
// separated points, which reads as a scattering of pixels on a portrait
// rather than a face. Braille stays in the pipeline for line art.
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

// portrait returns a hero's portrait frame: the embedded converted art if
// present, else a placeholder drawn on the spot. Cached per tier.
func (a *App) portrait(heroID string) art.Frame {
	return a.portraitOf(heroID, art.PortraitCols, art.PortraitRows, "hero_"+heroID)
}

// mediumPortrait is the phone-panel variant.
func (a *App) mediumPortrait(heroID string) art.Frame {
	return a.portraitOf(heroID, art.MediumCols, art.MediumRows, art.MediumName("hero_"+heroID))
}

// smallPortrait is the card-sized variant, for panels that cannot spare
// ten rows (a phone in portrait).
func (a *App) smallPortrait(heroID string) art.Frame {
	return a.portraitOf(heroID, art.SmallCols, art.SmallRows, art.SmallName("hero_"+heroID))
}

func (a *App) portraitOf(heroID string, cols, rows int, name string) art.Frame {
	if f, ok := a.figurePortrait("hero_"+heroID, cols, rows); ok {
		return f
	}
	tier := a.artTier()
	key := name + "/" + tier
	if f, ok := a.portraits[key]; ok {
		return f
	}
	f, err := art.Load(assets.Art(), name, tier)
	if err != nil {
		class := ""
		if h, ok := a.c.Heroes[heroID]; ok {
			class = h.Class
		}
		f = art.Convert(art.Placeholder(heroID, class, cols*3, rows*6), tier, cols, rows)
	}
	if a.portraits == nil {
		a.portraits = map[string]art.Frame{}
	}
	a.portraits[key] = f
	return f
}

// Portrait sizes, largest first.
type portraitSize struct{ cols, rows int }

var (
	sizeFull   = portraitSize{art.PortraitCols, art.PortraitRows}
	sizeMedium = portraitSize{art.MediumCols, art.MediumRows}
	sizeSmall  = portraitSize{art.SmallCols, art.SmallRows}
)

// unitSilhouette gives the placeholder art for each unit kind the build
// of the nearest hero class; real art replaces it by name.
var unitSilhouette = map[string]string{
	"lineman": "breaker", "ranged": "marksman", "runner": "signaler", "medic": "summoner",
}

// unitPortrait is a unit's own portrait: the hero's for a commander,
// the unit kind's for anything else ("unit_<kind>" in assets/art).
func (a *App) unitPortrait(u *engine.Unit, sz portraitSize) art.Frame {
	if u.IsCommander {
		switch sz {
		case sizeSmall:
			return a.smallPortrait(u.Kind)
		case sizeMedium:
			return a.mediumPortrait(u.Kind)
		}
		return a.portrait(u.Kind)
	}
	if f, ok := a.figurePortrait("unit_"+u.Kind, sz.cols, sz.rows); ok {
		return f
	}
	name := "unit_" + u.Kind
	switch sz {
	case sizeSmall:
		name = art.SmallName(name)
	case sizeMedium:
		name = art.MediumName(name)
	}
	tier := a.artTier()
	key := name + "/" + tier
	if f, ok := a.portraits[key]; ok {
		return f
	}
	f, err := art.Load(assets.Art(), name, tier)
	if err != nil {
		f = art.Convert(art.Placeholder("unit_"+u.Kind, unitSilhouette[u.Kind], sz.cols*3, sz.rows*6), tier, sz.cols, sz.rows)
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

// piece is a unit's picture on the board (mini_hero_<kind> or
// mini_unit_<kind> in assets/art), its dark outline drawn in the unit's
// team colour so a side reads at a glance, like the two colours of chess
// pieces. Nil means letters: no art, a mono theme, or no colour at all.
func (a *App) piece(u *render.SceneUnit, shade string, cols, rows int) []string {
	return a.figureLines(figureID(u), u.Team, u.Pose, shade, cols, rows, false, u.HP, u.MaxHP)
}

// ghost is a unit's piece faded toward the board: where it is going to be.
func (a *App) ghost(u *render.SceneUnit, shade string, cols, rows int) []string {
	return a.figureLines(figureID(u), u.Team, "", shade, cols, rows, true, 0, 0)
}

// pieceArt draws a piece, faded or not, on a square's background (shade,
// "" for none): the empty half of every cell takes the square's colour.
func (a *App) pieceArt(u *render.SceneUnit, faded bool, shade string) []string {
	t := a.st.Theme
	if a.artTier() == "t0" || t.Mono {
		return nil
	}
	team := t.TeamA
	if u.Team != 0 {
		team = t.TeamB
	}
	name := "mini_unit_" + u.Kind
	if u.Commander {
		name = "mini_hero_" + u.Kind
	}
	key := name + "/" + team + "/" + shade
	if faded {
		key += "/ghost"
	}
	if lines, ok := a.pieces[key]; ok {
		return lines
	}
	f, err := art.Load(assets.Art(), name, "t1")
	if err != nil {
		return nil
	}
	tint := hexColor(team)
	f = f.Clone()
	for i := range f.Cells {
		c := &f.Cells[i]
		if c.Flags&art.HasFg != 0 && dark(c.Fg) {
			c.Fg = tint
		}
		if c.Flags&art.HasBg != 0 && dark(c.Bg) {
			c.Bg = tint
		}
		if faded {
			c.Fg, c.Bg = fade(c.Fg), fade(c.Bg)
		}
		if shade != "" && c.Flags&art.HasBg == 0 {
			c.Bg, c.Flags = hexColor(shade), c.Flags|art.HasBg
		}
	}
	lines := f.Render(a.artStyler())
	if a.pieces == nil {
		a.pieces = map[string][]string{}
	}
	a.pieces[key] = lines
	return lines
}

// fade takes a colour most of the way to the board's dark background.
func fade(c uint32) uint32 {
	r, g, b := c>>16&0xff, c>>8&0xff, c&0xff
	return (r*35/100)<<16 | (g*35/100)<<8 | b*35/100
}

// dark reports whether a colour is outline-dark.
func dark(c uint32) bool {
	r, g, b := float64(c>>16&0xff), float64(c>>8&0xff), float64(c&0xff)
	return 0.3*r+0.59*g+0.11*b < 70
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
