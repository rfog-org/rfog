package art

import (
	"bytes"
	"image"
	"image/color"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

func testImage() image.Image {
	img := image.NewRGBA(image.Rect(0, 0, 40, 20))
	for y := 0; y < 20; y++ {
		for x := 0; x < 40; x++ {
			v := uint8(x * 255 / 39)
			img.Set(x, y, color.RGBA{R: v, G: v / 2, B: 255 - v, A: 255})
		}
	}
	return img
}

func TestConvertTiers(t *testing.T) {
	img := testImage()
	for _, tier := range []string{"t0", "t1", "t2"} {
		f := Convert(img, tier, 20, 5)
		if f.W != 20 || f.H != 5 || len(f.Cells) != 100 || f.Tier != tier {
			t.Fatalf("%s: %dx%d %d cells", tier, f.W, f.H, len(f.Cells))
		}
		lines := f.Lines()
		if len(lines) != 5 {
			t.Fatal("lines")
		}
		switch tier {
		case "t0":
			// Left is dark, right is bright: the ramp must rise.
			if strings.IndexByte(ramp, lines[2][0]) >= strings.IndexByte(ramp, lines[2][19]) {
				t.Fatalf("t0 ramp: %q", lines[2])
			}
			for _, c := range f.Cells {
				if c.Flags&(HasFg|HasBg) != 0 {
					t.Fatal("t0 has colour")
				}
			}
		case "t1":
			if f.At(0, 0).R != '▀' || f.At(0, 0).Flags&(HasFg|HasBg) != HasFg|HasBg {
				t.Fatalf("t1 cell: %+v", f.At(0, 0))
			}
		case "t2":
			for _, c := range f.Cells {
				if c.R != ' ' && (c.R < 0x2800 || c.R > 0x28ff) {
					t.Fatalf("t2 non-braille %q", c.R)
				}
			}
		}
	}
}

func TestRoundTripAndRender(t *testing.T) {
	f := Convert(testImage(), "t1", 12, 4)
	var buf bytes.Buffer
	if err := Encode(&buf, f); err != nil {
		t.Fatal(err)
	}
	g, err := Decode(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if g.W != f.W || g.H != f.H || len(g.Cells) != len(f.Cells) || g.Cells[5] != f.Cells[5] {
		t.Fatalf("round trip: %+v", g)
	}
	r := lipgloss.NewRenderer(&bytes.Buffer{})
	r.SetColorProfile(termenv.TrueColor)
	st := Styler{Renderer: r, Plain: r.NewStyle(), Accent: r.NewStyle().Bold(true), Dim: r.NewStyle().Faint(true), Color: true}
	lines := g.Render(st)
	if len(lines) != 4 || !strings.Contains(lines[0], "38;2;") || lipgloss.Width(lines[0]) != 12 {
		t.Fatalf("render: %d lines, width %d", len(lines), lipgloss.Width(lines[0]))
	}
	// Without colour the same frame renders plain.
	st.Color = false
	plain := g.Render(st)
	if strings.Contains(plain[0], "38;2;") {
		t.Fatal("colour leaked at t0")
	}
}

func TestPlaceholderDeterministic(t *testing.T) {
	a := Placeholder("wren", "marksman", 60, 40)
	b := Placeholder("wren", "marksman", 60, 40)
	c := Placeholder("hask", "breaker", 60, 40)
	same, diff := 0, 0
	for y := 0; y < 40; y++ {
		for x := 0; x < 60; x++ {
			if a.At(x, y) != b.At(x, y) {
				t.Fatal("not deterministic")
			}
			if a.At(x, y) != c.At(x, y) {
				diff++
			} else {
				same++
			}
		}
	}
	if diff == 0 {
		t.Fatal("heroes look identical")
	}
}

func TestFromLines(t *testing.T) {
	f := FromLines("t0", []string{"ab", "c"}, Bold)
	if f.W != 2 || f.H != 2 || f.At(1, 1).R != ' ' || f.At(0, 0).Flags != Bold || f.At(1, 1).Flags != 0 {
		t.Fatalf("%+v", f)
	}
}
