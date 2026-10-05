package engine

// RNG is a small deterministic generator (xorshift64*), seeded via splitmix64.
// Every roll is consumed in the documented resolution order so replays match.
type RNG struct {
	s uint64
}

// NewRNG seeds a generator from seed ^ turn as the spec requires.
func NewRNG(seed uint64, turn int) *RNG {
	x := seed ^ uint64(turn)
	// splitmix64 to spread a weak seed.
	x += 0x9E3779B97F4A7C15
	z := x
	z = (z ^ (z >> 30)) * 0xBF58476D1CE4E5B9
	z = (z ^ (z >> 27)) * 0x94D049BB133111EB
	z ^= z >> 31
	if z == 0 {
		z = 0x2545F4914F6CDD1D
	}
	return &RNG{s: z}
}

// Next returns the next 64-bit value.
func (r *RNG) Next() uint64 {
	x := r.s
	x ^= x >> 12
	x ^= x << 25
	x ^= x >> 27
	r.s = x
	return x * 0x2545F4914F6CDD1D
}

// Intn returns a value in [0, n).
func (r *RNG) Intn(n int) int {
	if n <= 0 {
		return 0
	}
	return int(r.Next() % uint64(n))
}

// D6 rolls one six-sided die.
func (r *RNG) D6() int { return r.Intn(6) + 1 }
