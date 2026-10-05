package engine

// Dist is Chebyshev distance: diagonal steps cost 1.
func Dist(a, b Pos) int {
	dx := a.X - b.X
	if dx < 0 {
		dx = -dx
	}
	dy := a.Y - b.Y
	if dy < 0 {
		dy = -dy
	}
	if dx > dy {
		return dx
	}
	return dy
}

// Adjacent reports whether two positions are within one step (and distinct).
func Adjacent(a, b Pos) bool { return a != b && Dist(a, b) == 1 }

// In reports whether p is on the board.
func (b *Board) In(p Pos) bool {
	return p.X >= 0 && p.Y >= 0 && p.X < b.W && p.Y < b.H
}

// At returns the tile at p. Callers must check In first.
func (b *Board) At(p Pos) Tile { return b.Tiles[p.Y*b.W+p.X] }

// Passable reports whether a unit may stand on p.
func (b *Board) Passable(p Pos) bool {
	return b.In(p) && b.At(p).Terrain != TerrainWall
}

// Line returns the tiles strictly between a and b along a Bresenham line.
// It is symmetric: Line(a,b) visits the same tiles as Line(b,a) reversed,
// because we always walk from the lexicographically smaller endpoint.
func Line(a, b Pos) []Pos {
	if b.X < a.X || (b.X == a.X && b.Y < a.Y) {
		out := Trace(b, a)
		reversePos(out)
		return out
	}
	return Trace(a, b)
}

// Trace returns the tiles strictly between a and b along a Bresenham line
// walked from a. Where the line passes exactly between two tiles it
// rounds the same way relative to its direction, so the trace of rotated
// endpoints is the rotated trace; Line's fixed starting end is not, and
// made sight on a 180-degree symmetric map differ between the two sides.
func Trace(a, b Pos) []Pos {
	dx := b.X - a.X
	dy := b.Y - a.Y
	sx, sy := 1, 1
	if dx < 0 {
		dx = -dx
		sx = -1
	}
	if dy < 0 {
		dy = -dy
		sy = -1
	}
	var out []Pos
	x, y := a.X, a.Y
	if dx >= dy {
		err := dx / 2
		for i := 0; i < dx-1; i++ {
			x += sx
			err -= dy
			if err < 0 {
				y += sy
				err += dx
			}
			out = append(out, Pos{x, y})
		}
	} else {
		err := dy / 2
		for i := 0; i < dy-1; i++ {
			y += sy
			err -= dx
			if err < 0 {
				x += sx
				err += dy
			}
			out = append(out, Pos{x, y})
		}
	}
	return out
}

// Area returns all on-board tiles within Chebyshev radius r of c, in
// deterministic row-major order.
func (b *Board) Area(c Pos, r int) []Pos {
	var out []Pos
	for y := c.Y - r; y <= c.Y+r; y++ {
		for x := c.X - r; x <= c.X+r; x++ {
			p := Pos{x, y}
			if b.In(p) {
				out = append(out, p)
			}
		}
	}
	return out
}

// Neighbors returns the up-to-8 on-board neighbours of p in a fixed order.
func (b *Board) Neighbors(p Pos) []Pos {
	out := make([]Pos, 0, 8)
	for _, d := range dirs8 {
		q := Pos{p.X + d.X, p.Y + d.Y}
		if b.In(q) {
			out = append(out, q)
		}
	}
	return out
}

// Maps are symmetric under 180-degree rotation, and so must be every tie
// the rules break by position: "first in row-major order" is north-west
// for both teams, which is forward for one and backward for the other.
// The *For variants below order tiles in the team's own rotated frame,
// so team 1 reads the board from the opposite corner.

// AreaFor is Area in team's reading order.
func (b *Board) AreaFor(c Pos, r, team int) []Pos {
	out := b.Area(c, r)
	if team == 1 {
		reversePos(out)
	}
	return out
}

// NeighborsFor is Neighbors in team's reading order.
func (b *Board) NeighborsFor(p Pos, team int) []Pos {
	out := make([]Pos, 0, 8)
	start := 0
	if team == 1 {
		start = 4 // the same compass order, rotated half a turn
	}
	for i := range dirs8 {
		d := dirs8[(start+i)%len(dirs8)]
		q := Pos{p.X + d.X, p.Y + d.Y}
		if b.In(q) {
			out = append(out, q)
		}
	}
	return out
}

func reversePos(ps []Pos) {
	for i, j := 0, len(ps)-1; i < j; i, j = i+1, j-1 {
		ps[i], ps[j] = ps[j], ps[i]
	}
}

var dirs8 = []Pos{{0, -1}, {1, -1}, {1, 0}, {1, 1}, {0, 1}, {-1, 1}, {-1, 0}, {-1, -1}}

// Sign returns -1, 0 or 1.
func Sign(n int) int {
	switch {
	case n < 0:
		return -1
	case n > 0:
		return 1
	}
	return 0
}

// StepToward returns the unit step from a toward b (each axis clamped to ±1).
func StepToward(a, b Pos) Pos {
	return Pos{Sign(b.X - a.X), Sign(b.Y - a.Y)}
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
