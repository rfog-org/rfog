// Package art is the text frame the terminal draws pictures with: a
// rectangle of character cells, each with optional colours. A T0 frame is
// characters only; a coloured one is half blocks, two pixels to a cell.
package art

import (
	"fmt"
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

// Flags on a cell.
const (
	HasFg uint8 = 1 << iota
	HasBg
	Bold
	Dim
)

// Cell is one character cell: a rune plus optional colours (0xRRGGBB).
type Cell struct {
	R     rune
	Fg    uint32
	Bg    uint32
	Flags uint8
}

// Frame is a rectangle of cells.
type Frame struct {
	Tier  string // t0 | t1
	W     int
	H     int
	Cells []Cell // row-major, len W*H
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
