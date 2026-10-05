// Package art converts images to text frames per render tier and draws
// procedural placeholder portraits. Frames are tier-specific: T0 uses a
// character density ramp (no colour), T1 half-block dithering (two pixels
// per cell, foreground and background colours), T2 braille (2×4 dots per
// cell, one colour). Frames are stored gzipped under assets/art/.
package art

import (
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"io"
	"io/fs"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"rfog/render/palette"
)

// Portrait frame sizes. The small one is a card for a phone-sized panel,
// where ten rows of portrait would cost the whole detail panel.
const (
	PortraitCols = 30
	PortraitRows = 10

	MediumCols = 20
	MediumRows = 9

	SmallCols = 12
	SmallRows = 5
)

// SmallName and MediumName are the stored names of a portrait's smaller
// variants. Each is converted at its own size: dithering a 12x5 frame is
// not the same as scaling a 30x10 one.
func SmallName(name string) string  { return name + "_sm" }
func MediumName(name string) string { return name + "_md" }

// Flags on a cell.
const (
	HasFg uint8 = 1 << iota
	HasBg
	Bold
	Dim
)

// Cell is one character cell: a rune plus optional colours (0xRRGGBB).
type Cell struct {
	R     rune   `json:"r"`
	Fg    uint32 `json:"f,omitempty"`
	Bg    uint32 `json:"b,omitempty"`
	Flags uint8  `json:"x,omitempty"`
}

// Frame is a rectangle of cells.
type Frame struct {
	Tier  string `json:"tier"` // t0 | t1 | t2
	W     int    `json:"w"`
	H     int    `json:"h"`
	Cells []Cell `json:"cells"` // row-major, len W*H
}

// At returns the cell at (x, y); out of range is a blank.
func (f *Frame) At(x, y int) Cell {
	if x < 0 || y < 0 || x >= f.W || y >= f.H {
		return Cell{R: ' '}
	}
	return f.Cells[y*f.W+x]
}

// Set writes a cell.
func (f *Frame) Set(x, y int, c Cell) {
	if x < 0 || y < 0 || x >= f.W || y >= f.H {
		return
	}
	f.Cells[y*f.W+x] = c
}

// Clone copies the frame.
func (f *Frame) Clone() Frame {
	g := *f
	g.Cells = append([]Cell(nil), f.Cells...)
	return g
}

// New makes a blank frame.
func New(tier string, w, h int) Frame {
	f := Frame{Tier: tier, W: w, H: h, Cells: make([]Cell, w*h)}
	for i := range f.Cells {
		f.Cells[i].R = ' '
	}
	return f
}

// FromLines makes a frame from plain text lines (padded to the longest),
// every non-space cell flagged with flags.
func FromLines(tier string, lines []string, flags uint8) Frame {
	w := 0
	rows := make([][]rune, len(lines))
	for i, l := range lines {
		rows[i] = []rune(l)
		if len(rows[i]) > w {
			w = len(rows[i])
		}
	}
	f := New(tier, w, len(lines))
	for y, r := range rows {
		for x, ch := range r {
			c := Cell{R: ch}
			if ch != ' ' {
				c.Flags = flags
			}
			f.Set(x, y, c)
		}
	}
	return f
}

// Lines returns the frame as plain text (colours dropped).
func (f Frame) Lines() []string {
	out := make([]string, f.H)
	for y := 0; y < f.H; y++ {
		var sb strings.Builder
		for x := 0; x < f.W; x++ {
			sb.WriteRune(f.At(x, y).R)
		}
		out[y] = sb.String()
	}
	return out
}

// Styler maps a cell's flags to styles when the cell carries no colour
// (word marks, T0 frames): Bold → Accent, Dim → Dim, else Plain.
type Styler struct {
	Renderer *lipgloss.Renderer
	Plain    lipgloss.Style
	Accent   lipgloss.Style
	Dim      lipgloss.Style
	// Color is false for T0 and mono themes: RGB is ignored, flags only.
	Color bool
}

// Render returns the frame as styled lines.
func (f Frame) Render(s Styler) []string {
	cache := map[Cell]lipgloss.Style{}
	styleFor := func(c Cell) lipgloss.Style {
		key := Cell{Fg: c.Fg, Bg: c.Bg, Flags: c.Flags}
		if st, ok := cache[key]; ok {
			return st
		}
		var st lipgloss.Style
		switch {
		case s.Color && c.Flags&(HasFg|HasBg) != 0:
			st = s.Renderer.NewStyle()
			if c.Flags&HasFg != 0 {
				st = st.Foreground(palette.Color(s.Renderer, hex(c.Fg)))
			}
			if c.Flags&HasBg != 0 {
				st = st.Background(palette.Color(s.Renderer, hex(c.Bg)))
			}
			if c.Flags&Bold != 0 {
				st = st.Bold(true)
			}
		case c.Flags&Bold != 0:
			st = s.Accent
		case c.Flags&Dim != 0:
			st = s.Dim
		default:
			st = s.Plain
		}
		cache[key] = st
		return st
	}
	out := make([]string, f.H)
	for y := 0; y < f.H; y++ {
		var sb strings.Builder
		// Run-length: consecutive cells with the same style share one escape.
		var run strings.Builder
		var cur Cell
		flush := func() {
			if run.Len() > 0 {
				sb.WriteString(styleFor(cur).Render(run.String()))
				run.Reset()
			}
		}
		for x := 0; x < f.W; x++ {
			c := f.At(x, y)
			key := Cell{Fg: c.Fg, Bg: c.Bg, Flags: c.Flags}
			if x == 0 || key != cur {
				flush()
				cur = key
			}
			run.WriteRune(c.R)
		}
		flush()
		out[y] = sb.String()
	}
	return out
}

func hex(c uint32) string { return fmt.Sprintf("#%06x", c&0xffffff) }

// ---- conversion -------------------------------------------------------------

// Convert renders an image into a frame of cols×rows cells for a tier.
// Cells are roughly twice as tall as wide, so the image is sampled with a
// 1:2 cell aspect.
func Convert(img image.Image, tier string, cols, rows int) Frame {
	switch tier {
	case "t0":
		return convertRamp(img, cols, rows)
	case "t2":
		return convertBraille(img, cols, rows)
	default:
		return convertHalfBlocks(img, cols, rows)
	}
}

// ramp is ordered dark to light for a dark terminal.
const ramp = " .:-=+*#%@"

// sample averages the image over a box in normalised coordinates.
func sample(img image.Image, x0, y0, x1, y1 float64) (r, g, b, a float64) {
	bd := img.Bounds()
	w, h := float64(bd.Dx()), float64(bd.Dy())
	px0, py0 := int(x0*w), int(y0*h)
	px1, py1 := int(x1*w), int(y1*h)
	if px1 <= px0 {
		px1 = px0 + 1
	}
	if py1 <= py0 {
		py1 = py0 + 1
	}
	n := 0.0
	for y := py0; y < py1 && y < bd.Dy(); y++ {
		for x := px0; x < px1 && x < bd.Dx(); x++ {
			cr, cg, cb, ca := img.At(bd.Min.X+x, bd.Min.Y+y).RGBA()
			r += float64(cr >> 8)
			g += float64(cg >> 8)
			b += float64(cb >> 8)
			a += float64(ca >> 8)
			n++
		}
	}
	if n == 0 {
		return 0, 0, 0, 0
	}
	return r / n, g / n, b / n, a / n
}

func luma(r, g, b, a float64) float64 { return (0.2126*r + 0.7152*g + 0.0722*b) * a / 255 / 255 }

func pack(r, g, b float64) uint32 {
	return uint32(clamp(r))<<16 | uint32(clamp(g))<<8 | uint32(clamp(b))
}

func clamp(v float64) int {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return int(v + 0.5)
}

func convertRamp(img image.Image, cols, rows int) Frame {
	f := New("t0", cols, rows)
	for y := 0; y < rows; y++ {
		for x := 0; x < cols; x++ {
			r, g, b, a := sample(img, float64(x)/float64(cols), float64(y)/float64(rows), float64(x+1)/float64(cols), float64(y+1)/float64(rows))
			l := luma(r, g, b, a)
			i := int(l * float64(len(ramp)-1))
			c := Cell{R: rune(ramp[i])}
			if i >= len(ramp)*2/3 {
				c.Flags = Bold
			} else if i > 0 && i < len(ramp)/3 {
				c.Flags = Dim
			}
			f.Set(x, y, c)
		}
	}
	return f
}

func convertHalfBlocks(img image.Image, cols, rows int) Frame {
	f := New("t1", cols, rows)
	for y := 0; y < rows; y++ {
		for x := 0; x < cols; x++ {
			x0, x1 := float64(x)/float64(cols), float64(x+1)/float64(cols)
			ym, y0, y1 := (float64(y)+0.5)/float64(rows), float64(y)/float64(rows), float64(y+1)/float64(rows)
			tr, tg, tb, ta := sample(img, x0, y0, x1, ym)
			br, bg, bb, ba := sample(img, x0, ym, x1, y1)
			c := Cell{R: '▀'}
			if ta > 8 {
				c.Fg, c.Flags = pack(tr, tg, tb), c.Flags|HasFg
			}
			if ba > 8 {
				c.Bg, c.Flags = pack(br, bg, bb), c.Flags|HasBg
			}
			if c.Flags == 0 {
				c.R = ' '
			} else if c.Flags&HasFg == 0 {
				// Only the bottom half: draw it as a lower block instead of a coloured background.
				c.R, c.Fg, c.Flags = '▄', c.Bg, HasFg
			}
			f.Set(x, y, c)
		}
	}
	return f
}

// braille dot bit positions: column-major, dots 1-3 left, 4-6 right, 7-8 bottom.
var brailleBits = [4][2]uint8{{0x01, 0x08}, {0x02, 0x10}, {0x04, 0x20}, {0x40, 0x80}}

func convertBraille(img image.Image, cols, rows int) Frame {
	f := New("t2", cols, rows)
	// Sample every dot's luma, then Floyd–Steinberg dither on the dot grid.
	dw, dh := cols*2, rows*4
	lum := make([]float64, dw*dh)
	alpha := make([]float64, dw*dh)
	cols3 := make([][3]float64, cols*rows)
	for dy := 0; dy < dh; dy++ {
		for dx := 0; dx < dw; dx++ {
			r, g, b, a := sample(img, float64(dx)/float64(dw), float64(dy)/float64(dh), float64(dx+1)/float64(dw), float64(dy+1)/float64(dh))
			lum[dy*dw+dx] = luma(r, g, b, a)
			alpha[dy*dw+dx] = a
			cell := (dy/4)*cols + dx/2
			cols3[cell][0] += r * a / 255
			cols3[cell][1] += g * a / 255
			cols3[cell][2] += b * a / 255
		}
	}
	for dy := 0; dy < dh; dy++ {
		for dx := 0; dx < dw; dx++ {
			i := dy*dw + dx
			old := lum[i]
			nu := 0.0
			if old >= 0.5 {
				nu = 1
			}
			lum[i] = nu
			err := old - nu
			if old < 0.08 {
				err = 0 // near-black stays black instead of sparse noise
			}
			spread := func(x, y int, k float64) {
				if x >= 0 && x < dw && y >= 0 && y < dh {
					lum[y*dw+x] += err * k
				}
			}
			spread(dx+1, dy, 7.0/16)
			spread(dx-1, dy+1, 3.0/16)
			spread(dx, dy+1, 5.0/16)
			spread(dx+1, dy+1, 1.0/16)
		}
	}
	for y := 0; y < rows; y++ {
		for x := 0; x < cols; x++ {
			var bits uint8
			visible := 0
			for r := 0; r < 4; r++ {
				for c := 0; c < 2; c++ {
					i := (y*4+r)*dw + x*2 + c
					if alpha[i] > 8 {
						visible++
					}
					if lum[i] >= 1 && alpha[i] > 8 {
						bits |= brailleBits[r][c]
					}
				}
			}
			cell := Cell{R: ' '}
			if visible > 0 {
				cell.R = rune(0x2800 + int(bits))
				k := cols3[y*cols+x]
				n := float64(visible)
				// Brighten: dots are thin, so use the lit colour rather than the average.
				cell.Fg = pack(k[0]/n*1.25, k[1]/n*1.25, k[2]/n*1.25)
				cell.Flags = HasFg
			}
			f.Set(x, y, cell)
		}
	}
	return f
}

// ---- storage ----------------------------------------------------------------

// Encode writes a frame as gzipped JSON.
func Encode(w io.Writer, f Frame) error {
	gz := gzip.NewWriter(w)
	if err := json.NewEncoder(gz).Encode(f); err != nil {
		return err
	}
	return gz.Close()
}

// Decode reads a gzipped frame.
func Decode(r io.Reader) (Frame, error) {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return Frame{}, err
	}
	defer gz.Close()
	var f Frame
	if err := json.NewDecoder(gz).Decode(&f); err != nil {
		return Frame{}, err
	}
	if len(f.Cells) != f.W*f.H {
		return Frame{}, errors.New("frame size mismatch")
	}
	return f, nil
}

// FileName is the stored name of a frame: <name>.<tier>.gz.
func FileName(name, tier string) string { return name + "." + tier + ".gz" }

// Load reads <name>.<tier>.gz from fsys (a directory or an embedded FS).
func Load(fsys fs.FS, name, tier string) (Frame, error) {
	r, err := fsys.Open(FileName(name, tier))
	if err != nil {
		return Frame{}, err
	}
	defer r.Close()
	return Decode(r)
}

// ---- placeholders -----------------------------------------------------------

// Placeholder draws a procedural portrait for an id: a badge whose hue,
// silhouette and markings derive from the id and class. It stands in
// until real source images exist; the same pipeline then replaces it.
func Placeholder(id, class string, w, h int) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	size := w
	if h < w {
		size = h
	}
	fw, fh := float64(w), float64(h)
	hash := fnv(id)
	hue := float64(hash%360) / 360
	base := hsl(hue, 0.55, 0.42)
	light := hsl(hue, 0.6, 0.62)
	dark := hsl(hue, 0.5, 0.18)
	glow := hsl(hue+0.5, 0.7, 0.7)
	fs := float64(size)
	cx, cy := fw/2, fh*0.5
	// Silhouette parameters per class (head radius, shoulder width).
	headR, shoulderW, shoulderY := fs*0.24, fw*0.62, fh*0.78
	switch class {
	case "breaker":
		headR, shoulderW = fs*0.26, fw*0.74
	case "marksman":
		headR, shoulderW = fs*0.21, fw*0.5
	case "summoner":
		headR, shoulderW = fs*0.22, fw*0.56
	case "signaler":
		headR, shoulderW = fs*0.22, fw*0.58
	}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			px, py := float64(x)+0.5, float64(y)+0.5
			// Background: vertical gradient with a diagonal band.
			t := py / fh
			c := lerp(dark, base, t*0.6)
			if int((px+py)/(fs/8))%2 == 0 {
				c = lerp(c, dark, 0.25)
			}
			// Shoulders: a rounded trapezoid.
			if py > shoulderY-fs*0.12 {
				half := shoulderW / 2 * (0.7 + 0.3*(py-(shoulderY-fs*0.12))/(fs*0.32))
				if px > cx-half && px < cx+half {
					c = lerp(light, base, (py-shoulderY+fs*0.12)/(fs*0.4))
				}
			}
			// Head.
			dx, dy := px-cx, py-(cy-fs*0.1)
			if dx*dx+dy*dy < headR*headR {
				c = light
				// Visor band.
				if dy > -headR*0.15 && dy < headR*0.25 {
					c = dark
					if dx > -headR*0.6 && dx < headR*0.6 && int(px/(fs/24))%3 != 0 {
						c = glow
					}
				}
			}
			// Class markings.
			switch class {
			case "marksman":
				// A scope line across the visor.
				if math1(dy) < fs*0.01 && dx > headR*0.4 && dx < headR*1.4 {
					c = glow
				}
			case "signaler":
				// Antenna.
				if math1(dx-headR*0.9) < fs*0.012 && dy < -headR*0.6 && dy > -headR*2.2 {
					c = glow
				}
			case "summoner":
				// Three small lights on the shoulders.
				for _, ox := range []float64{-0.3, 0, 0.3} {
					sx, sy := cx+ox*shoulderW, shoulderY+fs*0.06
					if (px-sx)*(px-sx)+(py-sy)*(py-sy) < (fs*0.02)*(fs*0.02) {
						c = glow
					}
				}
			case "breaker":
				// Shoulder plates.
				if py > shoulderY-fs*0.1 && py < shoulderY-fs*0.02 && math1(dx) > shoulderW*0.28 && math1(dx) < shoulderW*0.5 {
					c = dark
				}
			case "operator":
				// Diagonal stripe.
				if math1((px-cx)-(py-cy)*0.5) < fs*0.015 && py > shoulderY-fs*0.1 {
					c = glow
				}
			}
			// Border vignette.
			edge := min4(px, py, fw-px, fh-py) / fs
			if edge < 0.06 {
				c = lerp(dark, c, edge/0.06)
			}
			img.Set(x, y, c)
		}
	}
	return img
}

func math1(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

func min4(a, b, c, d float64) float64 {
	m := a
	for _, v := range []float64{b, c, d} {
		if v < m {
			m = v
		}
	}
	return m
}

func fnv(s string) uint32 {
	h := uint32(2166136261)
	for i := 0; i < len(s); i++ {
		h ^= uint32(s[i])
		h *= 16777619
	}
	return h
}

func lerp(a, b color.RGBA, t float64) color.RGBA {
	if t < 0 {
		t = 0
	}
	if t > 1 {
		t = 1
	}
	return color.RGBA{
		R: uint8(float64(a.R) + (float64(b.R)-float64(a.R))*t),
		G: uint8(float64(a.G) + (float64(b.G)-float64(a.G))*t),
		B: uint8(float64(a.B) + (float64(b.B)-float64(a.B))*t),
		A: 255,
	}
}

// hsl converts hue (0-1, wraps), saturation, lightness to RGB.
func hsl(h, s, l float64) color.RGBA {
	h -= float64(int(h))
	if h < 0 {
		h++
	}
	var r, g, b float64
	if s == 0 {
		r, g, b = l, l, l
	} else {
		var q float64
		if l < 0.5 {
			q = l * (1 + s)
		} else {
			q = l + s - l*s
		}
		p := 2*l - q
		r = hue2rgb(p, q, h+1.0/3)
		g = hue2rgb(p, q, h)
		b = hue2rgb(p, q, h-1.0/3)
	}
	return color.RGBA{R: uint8(r*255 + 0.5), G: uint8(g*255 + 0.5), B: uint8(b*255 + 0.5), A: 255}
}

func hue2rgb(p, q, t float64) float64 {
	if t < 0 {
		t++
	}
	if t > 1 {
		t--
	}
	switch {
	case t < 1.0/6:
		return p + (q-p)*6*t
	case t < 0.5:
		return q
	case t < 2.0/3:
		return p + (q-p)*(2.0/3-t)*6
	}
	return p
}
