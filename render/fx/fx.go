// Package fx is the procedural effects library: glitch bands, character
// corruption, dissolve-in, scanline, typewriter, flicker. Every effect is
// a pure function of (frame, seed, tick) applied at runtime to a still,
// so nothing is stored per frame of animation. Effects never carry
// information: they are applied to art and word marks, never to the board.
package fx

import (
	"rfog/render/art"
)

// rng is a tiny deterministic generator (xorshift) so effects look the
// same on every platform for the same seed and tick.
type rng uint64

func newRNG(seed uint64, tick int) rng {
	r := rng(seed*0x9E3779B97F4A7C15 + uint64(tick)*0xBF58476D1CE4E5B9 + 1)
	return r
}

func (r *rng) next() uint64 {
	x := uint64(*r)
	x ^= x << 13
	x ^= x >> 7
	x ^= x << 17
	*r = rng(x)
	return x
}

func (r *rng) intn(n int) int {
	if n <= 0 {
		return 0
	}
	return int(r.next() % uint64(n))
}

func (r *rng) chance(p float64) bool { return float64(r.next()%10000) < p*10000 }

// GlitchBands shifts a few horizontal bands sideways. intensity 0..1 sets
// how many bands and how far; most ticks have none (the loop is calm).
func GlitchBands(f art.Frame, seed uint64, tick int, intensity float64) art.Frame {
	r := newRNG(seed, tick)
	if !r.chance(0.25 + intensity*0.5) {
		return f
	}
	out := f.Clone()
	bands := 1 + r.intn(1+int(intensity*3))
	for b := 0; b < bands; b++ {
		y0 := r.intn(f.H)
		h := 1 + r.intn(2)
		shift := r.intn(1+int(intensity*6)+2) - int(intensity*3) - 1
		if shift == 0 {
			shift = 1
		}
		for y := y0; y < y0+h && y < f.H; y++ {
			for x := 0; x < f.W; x++ {
				out.Set(x, y, f.At(x-shift, y))
			}
		}
	}
	return out
}

// corruptGlyphs are what corrupted cells turn into, per tier.
var corruptGlyphs = map[string][]rune{
	"t0": []rune("#%*+=:-/\\|"),
	"t1": []rune("░▒▓█▚▞▌▐"),
	"t2": []rune("░▒▓█▚▞⣿⣷⣯⡿"),
}

// Corrupt replaces a sprinkling of non-blank cells with noise glyphs.
// rate is the fraction of cells touched (0.02 is subtle).
func Corrupt(f art.Frame, seed uint64, tick int, rate float64) art.Frame {
	r := newRNG(seed^0xC0FFEE, tick)
	glyphs := corruptGlyphs[f.Tier]
	if glyphs == nil {
		glyphs = corruptGlyphs["t1"]
	}
	out := f.Clone()
	for i, c := range out.Cells {
		if c.R == ' ' || !r.chance(rate) {
			continue
		}
		out.Cells[i].R = glyphs[r.intn(len(glyphs))]
	}
	return out
}

// Dissolve reveals the frame progressively: progress 0 shows nothing, 1
// everything. Cells appear in a fixed pseudo-random order for the seed,
// so the reveal is stable across ticks.
func Dissolve(f art.Frame, seed uint64, progress float64) art.Frame {
	if progress >= 1 {
		return f
	}
	if progress <= 0 {
		return art.New(f.Tier, f.W, f.H)
	}
	r := newRNG(seed^0xD15501, 0)
	out := f.Clone()
	for i := range out.Cells {
		if float64(r.next()%10000)/10000 > progress {
			out.Cells[i] = art.Cell{R: ' '}
		}
	}
	return out
}

// Wipe reveals a frame progressively in one direction: columns
// left-to-right, or rows top-to-bottom. A wipe reads as something being
// drawn; a dissolve of the same art reads as damage, which is the wrong
// note for a portrait.
func Wipe(f art.Frame, progress float64, rows bool) art.Frame {
	if progress >= 1 {
		return f
	}
	out := art.New(f.Tier, f.W, f.H)
	if progress <= 0 {
		return out
	}
	limit := int(progress * float64(f.W))
	if rows {
		limit = int(progress * float64(f.H))
	}
	for y := 0; y < f.H; y++ {
		for x := 0; x < f.W; x++ {
			if (rows && y < limit) || (!rows && x < limit) {
				out.Set(x, y, f.At(x, y))
			}
		}
	}
	return out
}

// Scanline dims one row that sweeps down the frame over period ticks.
func Scanline(f art.Frame, tick, period int) art.Frame {
	if f.H == 0 || period <= 0 {
		return f
	}
	y := (tick % period) * f.H / period
	out := f.Clone()
	for x := 0; x < f.W; x++ {
		c := out.At(x, y)
		if c.R != ' ' {
			c.Flags = (c.Flags &^ art.Bold) | art.Dim
			out.Set(x, y, c)
		}
	}
	return out
}

// Typewriter reveals the first n cells in reading order.
func Typewriter(f art.Frame, n int) art.Frame {
	out := f.Clone()
	for i := range out.Cells {
		if i >= n {
			out.Cells[i] = art.Cell{R: ' '}
		}
	}
	return out
}

// Flicker returns whether something should be shown dim this tick: a
// mostly-on signal with brief drops, deterministic per seed.
func Flicker(seed uint64, tick int) bool {
	r := newRNG(seed^0xF11C, tick)
	return r.chance(0.12)
}
