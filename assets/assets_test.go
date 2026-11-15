package assets

import (
	"testing"

	"rfog/data"
)

// Every hero and every unit kind has a figure, square, drawn only in
// palette colours: it is their piece and their portrait in every client.
func TestUnitFigures(t *testing.T) {
	c, err := data.Load()
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, id := range c.HeroIDs() {
		ids = append(ids, "hero_"+id)
	}
	for id := range c.Units {
		ids = append(ids, "unit_"+id)
	}
	for _, id := range ids {
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
