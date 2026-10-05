package fx

import (
	"testing"

	"rfog/render/art"
)

func mark() art.Frame {
	return art.FromLines("t1", []string{"#### ####", "#    #   ", "#### ####", "   # #   ", "#### ####"}, art.Bold)
}

func count(f art.Frame) int {
	n := 0
	for _, c := range f.Cells {
		if c.R != ' ' {
			n++
		}
	}
	return n
}

func TestDissolveMonotonic(t *testing.T) {
	f := mark()
	prev := -1
	for i := 0; i <= 10; i++ {
		n := count(Dissolve(f, 7, float64(i)/10))
		if n < prev {
			t.Fatalf("step %d: %d < %d", i, n, prev)
		}
		prev = n
	}
	if count(Dissolve(f, 7, 0)) != 0 || count(Dissolve(f, 7, 1)) != count(f) {
		t.Fatal("ends")
	}
	// Same seed, same reveal.
	a, b := Dissolve(f, 3, 0.5), Dissolve(f, 3, 0.5)
	for i := range a.Cells {
		if a.Cells[i] != b.Cells[i] {
			t.Fatal("dissolve not deterministic")
		}
	}
}

func TestEffectsKeepShape(t *testing.T) {
	f := mark()
	for tick := 0; tick < 50; tick++ {
		for _, g := range []art.Frame{GlitchBands(f, 1, tick, 1), Corrupt(f, 1, tick, 0.5), Scanline(f, tick, 7), Typewriter(f, tick)} {
			if g.W != f.W || g.H != f.H || len(g.Cells) != len(f.Cells) {
				t.Fatalf("tick %d: shape changed", tick)
			}
		}
	}
	// Corrupt only touches non-blank cells and never the original frame.
	g := Corrupt(f, 1, 3, 1)
	for i, c := range g.Cells {
		if (f.Cells[i].R == ' ') != (c.R == ' ') {
			t.Fatal("corrupt changed blankness")
		}
	}
	if count(f) != count(mark()) {
		t.Fatal("original mutated")
	}
	// Glitch bands happen on some ticks at full intensity.
	moved := false
	for tick := 0; tick < 20 && !moved; tick++ {
		g := GlitchBands(f, 9, tick, 1)
		for i := range g.Cells {
			if g.Cells[i] != f.Cells[i] {
				moved = true
			}
		}
	}
	if !moved {
		t.Fatal("no glitch in 20 ticks")
	}
}

func TestFlickerRare(t *testing.T) {
	n := 0
	for tick := 0; tick < 1000; tick++ {
		if Flicker(5, tick) {
			n++
		}
	}
	if n < 50 || n > 250 {
		t.Fatalf("flicker rate %d/1000", n)
	}
}
