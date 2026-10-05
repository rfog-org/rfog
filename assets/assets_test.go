package assets

import (
	"testing"

	"rfog/data"
	"rfog/render/art"
)

// Every hero and every unit kind has a stored portrait at each size and
// tier (made by `go run ./tools/portraits`), of the right dimensions and
// not blank. Anything missing would fall back to a drawn placeholder.
func TestPortraitFrames(t *testing.T) {
	c, err := data.Load()
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, id := range c.HeroIDs() {
		names = append(names, "hero_"+id)
	}
	for id := range c.Units {
		names = append(names, "unit_"+id)
	}
	sizes := []struct {
		name       func(string) string
		cols, rows int
	}{
		{func(n string) string { return n }, art.PortraitCols, art.PortraitRows},
		{art.MediumName, art.MediumCols, art.MediumRows},
		{art.SmallName, art.SmallCols, art.SmallRows},
	}
	for _, n := range names {
		for _, sz := range sizes {
			for _, tier := range []string{"t0", "t1", "t2"} {
				f, err := art.Load(Art(), sz.name(n), tier)
				if err != nil {
					t.Fatalf("%s %s: %v", sz.name(n), tier, err)
				}
				if f.W != sz.cols || f.H != sz.rows {
					t.Fatalf("%s %s: %dx%d, want %dx%d", sz.name(n), tier, f.W, f.H, sz.cols, sz.rows)
				}
				blank := true
				for _, cell := range f.Cells {
					if cell.R != ' ' {
						blank = false
						break
					}
				}
				if blank {
					t.Fatalf("%s %s: blank frame", sz.name(n), tier)
				}
			}
		}
	}
}

// Every hero and unit in Cast has a figure, square, drawn only in palette
// colours.
func TestUnitFigures(t *testing.T) {
	for id := range Cast {
		rows, ok := Units[id]
		if !ok {
			t.Errorf("%s: no figure", id)
			continue
		}
		if len(rows) != UnitSize {
			t.Errorf("%s: %d rows, want %d", id, len(rows), UnitSize)
		}
		for i, r := range rows {
			if len(r) != UnitSize {
				t.Errorf("%s row %d: %d pixels, want %d", id, i, len(r), UnitSize)
			}
			for j := 0; j < len(r); j++ {
				if _, ok := Palette[r[j]]; !ok {
					t.Errorf("%s row %d: %q is not in the palette", id, i, r[j])
				}
			}
		}
	}
}

// Poses are the same size and palette as the figures, for known figures.
func TestUnitPoses(t *testing.T) {
	for id, poses := range Poses {
		if _, ok := Units[id]; !ok {
			t.Errorf("pose for unknown figure %s", id)
		}
		for name, rows := range poses {
			if len(rows) != UnitSize {
				t.Errorf("%s %s: %d rows", id, name, len(rows))
			}
			for i, r := range rows {
				if len(r) != UnitSize {
					t.Errorf("%s %s row %d: %d pixels", id, name, i, len(r))
				}
				for j := 0; j < len(r); j++ {
					if _, ok := Palette[r[j]]; !ok {
						t.Errorf("%s %s row %d: %q not in palette", id, name, i, r[j])
					}
				}
			}
		}
	}
}
