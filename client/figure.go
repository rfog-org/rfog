package client

import (
	"strconv"

	"rfog/assets"
	"rfog/engine"
	"rfog/render"
	"rfog/render/art"
)

// The figures (assets.Units) drawn in a terminal: the same pixel grids the
// graphical client draws, two pixels to a cell with half blocks (the top
// pixel in the foreground of ▀, the bottom one in its background). A
// figure is shrunk to fit by sampling blocks of pixels; the team's glowing
// pixels always win a block, so eyes and lights survive any size.

// pixels is a small RGB image; ok marks the opaque pixels.
type pixels struct {
	w, h int
	c    []uint32
	ok   []bool
}

// figure returns a figure's pixels in a team's colours, cropped to what
// it covers. pose "" is the standing frame. ok is false when there is no
// figure by that name.
func (a *App) figure(id string, team int, pose string) (pixels, []bool, bool) {
	rows, ok := assets.Units[id]
	if pose != "" {
		if p, has := assets.Poses[id][pose]; has {
			rows = p
		}
	}
	if !ok {
		return pixels{}, nil, false
	}
	t := a.st.Theme
	tc := hexColor(t.TeamA)
	if team != 0 {
		tc = hexColor(t.TeamB)
	}
	x0, y0, x1, y1 := assets.UnitSize, assets.UnitSize, -1, -1
	for y, r := range rows {
		for x := 0; x < len(r); x++ {
			if r[x] != '.' {
				x0, y0, x1, y1 = minInt(x0, x), minInt(y0, y), maxInt(x1, x), maxInt(y1, y)
			}
		}
	}
	if x1 < 0 {
		return pixels{}, nil, false
	}
	p := pixels{w: x1 - x0 + 1, h: y1 - y0 + 1}
	p.c = make([]uint32, p.w*p.h)
	p.ok = make([]bool, p.w*p.h)
	glow := make([]bool, p.w*p.h)
	for y := y0; y <= y1; y++ {
		for x := x0; x <= x1; x++ {
			ch := rows[y][x]
			col := assets.Palette[ch]
			if col == "" {
				continue
			}
			i := (y-y0)*p.w + (x - x0)
			switch col {
			case "team":
				p.c[i], glow[i] = tc, true
			case "team-shade":
				p.c[i], glow[i] = darken(tc, 60), true
			default:
				p.c[i] = hexColor(col)
			}
			p.ok[i] = true
		}
	}
	return p, glow, true
}

// shrink shrinks a figure into w x h pixels (never enlarging it), standing on
// the bottom edge and centred across. In each sampled block a glowing
// pixel wins, then the commonest colour if the block is at least half
// covered; otherwise the block is see-through.
func shrink(p pixels, glow []bool, w, h int) pixels {
	out := pixels{w: w, h: h, c: make([]uint32, w*h), ok: make([]bool, w*h)}
	s := 1.0
	if f := float64(p.w) / float64(w); f > s {
		s = f
	}
	if f := float64(p.h) / float64(h); f > s {
		s = f
	}
	dw, dh := int(float64(p.w)/s+0.999), int(float64(p.h)/s+0.999)
	ox, oy := (w-dw)/2, h-dh
	for ty := 0; ty < dh; ty++ {
		for tx := 0; tx < dw; tx++ {
			sx0, sy0 := int(float64(tx)*s), int(float64(ty)*s)
			sx1, sy1 := maxInt(sx0+1, int(float64(tx+1)*s)), maxInt(sy0+1, int(float64(ty+1)*s))
			count, total := map[uint32]int{}, 0
			var lit uint32
			hasLit := false
			for y := sy0; y < sy1 && y < p.h; y++ {
				for x := sx0; x < sx1 && x < p.w; x++ {
					total++
					i := y*p.w + x
					if !p.ok[i] {
						continue
					}
					if glow[i] && !hasLit {
						lit, hasLit = p.c[i], true
					}
					count[p.c[i]]++
				}
			}
			covered := 0
			var best uint32
			for c, n := range count {
				covered += n
				if n > count[best] || (n == count[best] && c < best) {
					best = c
				}
			}
			i := (oy+ty)*w + ox + tx
			switch {
			case hasLit:
				out.c[i], out.ok[i] = lit, true
			case covered*2 >= total && covered > 0:
				out.c[i], out.ok[i] = best, true
			}
		}
	}
	return out
}

// halfBlocks turns pixels (h even) into a frame of w x h/2 cells on bg.
func halfBlocks(p pixels, bg uint32, hasBg bool) art.Frame {
	f := art.New("t1", p.w, p.h/2)
	for y := 0; y < p.h/2; y++ {
		for x := 0; x < p.w; x++ {
			top, tok := p.c[(2*y)*p.w+x], p.ok[(2*y)*p.w+x]
			bot, bok := p.c[(2*y+1)*p.w+x], p.ok[(2*y+1)*p.w+x]
			c := art.Cell{R: ' '}
			switch {
			case tok && bok:
				c = art.Cell{R: '▀', Fg: top, Bg: bot, Flags: art.HasFg | art.HasBg}
			case tok:
				c = art.Cell{R: '▀', Fg: top, Bg: bg, Flags: art.HasFg}
			case bok:
				c = art.Cell{R: '▄', Fg: bot, Bg: bg, Flags: art.HasFg}
			}
			if hasBg {
				if c.Flags&art.HasBg == 0 {
					c.Bg = bg
				}
				c.Flags |= art.HasBg
			}
			f.Set(x, y, c)
		}
	}
	return f
}

// figureLines draws a figure in cols x rows cells on a background ("" =
// the terminal's), faded toward the board for a ghost. With maxHP > 0 the
// bottom pixel row is its health: a bar coloured by how hurt, a gutter at
// the end so neighbours' bars stay apart. Nil when there is no figure or
// no colour to draw it in.
func (a *App) figureLines(id string, team int, pose, shade string, cols, rows int, faded bool, hp, maxHP int) []string {
	if a.artTier() == "t0" || a.st.Theme.Mono || cols <= 0 || rows <= 0 {
		return nil
	}
	bar := 0
	if maxHP > 0 {
		w := cols - 1
		bar = (hp*w + maxHP - 1) / maxHP
		if bar > w {
			bar = w
		}
		if bar < 0 {
			bar = 0
		}
	}
	key := id + "/" + strconv.Itoa(team) + "/" + pose + "/" + shade + "/" + strconv.Itoa(cols) + "x" + strconv.Itoa(rows)
	if faded {
		key += "/ghost"
	}
	if maxHP > 0 {
		key += "/hp" + strconv.Itoa(bar) + "." + strconv.Itoa(hp*3/maxHP)
	}
	if lines, ok := a.pieces[key]; ok {
		return lines
	}
	p, glow, ok := a.figure(id, team, pose)
	if !ok {
		return nil
	}
	ph := rows * 2
	if maxHP > 0 {
		ph-- // the last pixel row is the health bar
	}
	q := shrink(p, glow, cols, ph)
	if maxHP > 0 {
		t := a.st.Theme
		fill := hexColor(t.Good)
		switch {
		case hp*3 <= maxHP:
			fill = hexColor(t.Danger)
		case hp*3 <= maxHP*2:
			fill = hexColor(t.Warn)
		}
		row := pixels{w: cols, h: 1, c: make([]uint32, cols), ok: make([]bool, cols)}
		for x := 0; x < cols-1; x++ {
			row.ok[x] = true
			row.c[x] = 0x0b0e12
			if x < bar {
				row.c[x] = fill
			}
		}
		q = pixels{w: cols, h: ph + 1, c: append(q.c, row.c...), ok: append(q.ok, row.ok...)}
	}
	bg := hexColor(shade)
	if faded {
		for i := range q.c {
			if q.ok[i] {
				q.c[i] = mix(q.c[i], bg, 60)
			}
		}
	}
	lines := halfBlocks(q, bg, shade != "").Render(a.artStyler())
	if a.pieces == nil {
		a.pieces = map[string][]string{}
	}
	a.pieces[key] = lines
	return lines
}

// figureID is the figure a scene unit is drawn with.
func figureID(u *render.SceneUnit) string {
	if u.Commander {
		return "hero_" + u.Kind
	}
	return "unit_" + u.Kind
}

// block draws a wall as a block standing on its square: a lit top face
// over a dark front, with one of three details on the front (a rack's
// vents and status light, a conduit's fibre strand, a pillar's crack),
// fixed per square.
func (a *App) block(p engine.Pos, shade string, cols, rows int) []string {
	t := a.st.Theme
	if t.WallTop == "" || a.artTier() == "t0" {
		return nil
	}
	variant := (p.X*7 + p.Y*3) % 3
	key := "block/" + strconv.Itoa(variant) + "/" + shade + "/" + strconv.Itoa(cols) + "x" + strconv.Itoa(rows)
	if lines, ok := a.pieces[key]; ok {
		return lines
	}
	sq, top, front := hexColor(shade), hexColor(t.WallTop), hexColor(t.Wall)
	if shade != "" && shade == a.st.FogBg { // out of sight: the same block, dimmer
		top, front = darken(top, 55), darken(front, 70)
	}
	f := art.New("t1", cols, rows)
	for y := 0; y < rows; y++ {
		for x := 0; x < cols; x++ {
			c := art.Cell{R: ' ', Bg: front, Flags: art.HasBg}
			switch {
			case y == 0:
				c = art.Cell{R: '▄', Fg: top, Bg: sq, Flags: art.HasFg | art.HasBg}
			case y == 1 && rows > 2:
				c = art.Cell{R: '▀', Fg: top, Bg: front, Flags: art.HasFg | art.HasBg}
			}
			f.Set(x, y, c)
		}
	}
	mid := rows / 2
	if rows > 2 {
		mid = (rows + 1) / 2
	}
	detail := func(x, y int, r rune, fg string) {
		f.Set(x, y, art.Cell{R: r, Fg: hexColor(fg), Bg: front, Flags: art.HasFg | art.HasBg})
	}
	switch variant {
	case 0: // rack: vents and a status light
		for x := 1; x < cols-2; x++ {
			detail(x, mid, '≡', "#6f8494")
		}
		detail(cols-2, minInt(mid, rows-1), '•', t.Accent)
	case 1: // conduit: a fibre strand down the front
		for y := 1; y < rows; y++ {
			if y == 1 && rows > 2 {
				continue
			}
			detail(cols/2, y, '┃', t.Accent)
		}
	default: // pillar: a crack
		for i, y := 0, rows-1; y >= 1 && i < cols; i, y = i+1, y-1 {
			if y == 1 && rows > 2 {
				break
			}
			detail(cols/2-1+i, y, '╱', "#2a3540")
		}
	}
	lines := f.Render(a.artStyler())
	if a.pieces == nil {
		a.pieces = map[string][]string{}
	}
	a.pieces[key] = lines
	return lines
}

// figurePortrait is a figure as a portrait frame, cols x rows cells on a
// slate backdrop; false when there is no figure by that name.
func (a *App) figurePortrait(id string, cols, rows int) (art.Frame, bool) {
	if a.artTier() == "t0" {
		return art.Frame{}, false
	}
	p, glow, ok := a.figure(id, 0, "")
	if !ok {
		return art.Frame{}, false
	}
	q := shrink(p, glow, cols, rows*2)
	// a little room above the head on a tall frame
	return halfBlocks(q, hexColor("#33444f"), true), true
}

// darken takes a colour to pct percent of itself.
func darken(c uint32, pct uint32) uint32 {
	r, g, b := c>>16&0xff, c>>8&0xff, c&0xff
	return (r*pct/100)<<16 | (g*pct/100)<<8 | b*pct/100
}

// mix blends a colour toward bg by pct percent.
func mix(c, bg uint32, pct uint32) uint32 {
	ch := func(a, b uint32) uint32 { return (a*(100-pct) + b*pct) / 100 }
	return ch(c>>16&0xff, bg>>16&0xff)<<16 | ch(c>>8&0xff, bg>>8&0xff)<<8 | ch(c&0xff, bg&0xff)
}
