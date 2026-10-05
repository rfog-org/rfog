package palette

import "testing"

// Exact palette entries map to themselves, and fibre's slate floor lands
// on a grey, not the purple channel rounding gives it.
func TestIndex256(t *testing.T) {
	for hex, want := range map[string]int{"#808080": 244, "#5fd7ff": 81, "#000000": 16, "#ff875f": 209} {
		if got := Index256(hex); got != want {
			t.Errorf("%s: %d, want %d", hex, got, want)
		}
	}
	if got := Snap256("#7d93a3"); got == "#8787af" {
		t.Errorf("slate snapped to purple %s", got)
	}
}
