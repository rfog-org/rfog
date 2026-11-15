package art

import (
	"bytes"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// A coloured frame renders its colours; the same frame without colour
// renders plain.
func TestRender(t *testing.T) {
	g := New("t1", 12, 4)
	for i := range g.Cells {
		g.Cells[i] = Cell{R: '▀', Fg: 0x4fd6be, Bg: 0x33444f, Flags: HasFg | HasBg}
	}
	r := lipgloss.NewRenderer(&bytes.Buffer{})
	r.SetColorProfile(termenv.TrueColor)
	st := Styler{Renderer: r, Plain: r.NewStyle(), Accent: r.NewStyle().Bold(true), Dim: r.NewStyle().Faint(true), Color: true}
	lines := g.Render(st)
	if len(lines) != 4 || !strings.Contains(lines[0], "38;2;") || lipgloss.Width(lines[0]) != 12 {
		t.Fatalf("render: %d lines, width %d", len(lines), lipgloss.Width(lines[0]))
	}
	st.Color = false
	if strings.Contains(g.Render(st)[0], "38;2;") {
		t.Fatal("colour leaked at t0")
	}
}

func TestFromLines(t *testing.T) {
	f := FromLines("t0", []string{"ab", "c"}, Bold)
	if f.W != 2 || f.H != 2 || f.At(1, 1).R != ' ' || f.At(0, 0).Flags != Bold || f.At(1, 1).Flags != 0 {
		t.Fatalf("%+v", f)
	}
}
