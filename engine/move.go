package engine

import "sort"

// unitDef returns the unit definition for a non-commander unit, or nil.
func unitDef(c *Content, u *Unit) *UnitDef {
	if u.IsCommander {
		return nil
	}
	if d, ok := c.Units[u.Kind]; ok {
		return &d
	}
	return nil
}

// stepCost returns the movement cost of stepping from a to b for u, or -1 if
// the step is illegal (not adjacent, wall, too high a climb).
func stepCost(c *Content, s *State, u *Unit, a, b Pos) int {
	if !Adjacent(a, b) || !s.Board.Passable(b) {
		return -1
	}
	dz := s.Board.At(b).Z - s.Board.At(a).Z
	if dz > u.Eff("jmp") {
		return -1
	}
	cost := 1
	if dz > 0 {
		if d := unitDef(c, u); d == nil || !d.IgnoreHeightCost {
			cost += dz
		}
	}
	return cost
}

// PathCost validates a path for u from its current position and returns the
// total cost, or -1 with a reason if illegal. Enemy-occupied tiles (at the
// time of the call) block; ending on any occupied tile is illegal.
func PathCost(c *Content, s *State, u *Unit, path []Pos) (int, string) {
	if len(path) == 0 {
		return 0, ""
	}
	cur := u.Pos
	total := 0
	for i, p := range path {
		cost := stepCost(c, s, u, cur, p)
		if cost < 0 {
			return -1, "illegal step"
		}
		if o := s.UnitAt(p); o != nil && o.ID != u.ID {
			if o.Team != u.Team {
				return -1, "enemy in path"
			}
			if i == len(path)-1 {
				return -1, "destination occupied"
			}
		}
		total += cost
		cur = p
	}
	if total > u.Eff("mv") {
		return -1, "not enough movement"
	}
	return total, ""
}

// Reachable returns every tile u can move to this turn with a cheapest path.
// Paths are deterministic: ties broken by the tile's order in the unit's
// team frame (LessFor), so equal paths bend the same way for both sides.
func Reachable(c *Content, s *State, u *Unit) map[Pos][]Pos {
	out := map[Pos][]Pos{}
	if !u.Alive() || u.Rooted() {
		return out
	}
	mv := u.Eff("mv")
	type node struct {
		p    Pos
		cost int
	}
	best := map[Pos]int{u.Pos: 0}
	prev := map[Pos]Pos{}
	frontier := []node{{u.Pos, 0}}
	for len(frontier) > 0 {
		// pop cheapest (small graph; linear scan is fine and deterministic)
		bi := 0
		for i := range frontier {
			if frontier[i].cost < frontier[bi].cost ||
				(frontier[i].cost == frontier[bi].cost && LessFor(u.Team, frontier[i].p, frontier[bi].p)) {
				bi = i
			}
		}
		n := frontier[bi]
		frontier = append(frontier[:bi], frontier[bi+1:]...)
		if n.cost > best[n.p] {
			continue
		}
		for _, q := range s.Board.NeighborsFor(n.p, u.Team) {
			sc := stepCost(c, s, u, n.p, q)
			if sc < 0 {
				continue
			}
			if o := s.UnitAt(q); o != nil && o.Team != u.Team {
				continue
			}
			nc := n.cost + sc
			if nc > mv {
				continue
			}
			if b, ok := best[q]; !ok || nc < b {
				best[q] = nc
				prev[q] = n.p
				frontier = append(frontier, node{q, nc})
			}
		}
	}
	for p := range best {
		if p == u.Pos {
			continue
		}
		if o := s.UnitAt(p); o != nil {
			continue // cannot end on an occupied tile
		}
		var path []Pos
		for q := p; q != u.Pos; q = prev[q] {
			path = append(path, q)
		}
		for i, j := 0, len(path)-1; i < j; i, j = i+1, j-1 {
			path[i], path[j] = path[j], path[i]
		}
		out[p] = path
	}
	return out
}

func less(a, b Pos) bool {
	if a.Y != b.Y {
		return a.Y < b.Y
	}
	return a.X < b.X
}

// LessFor is row-major order in team's rotated frame (see Board.AreaFor).
func LessFor(team int, a, b Pos) bool {
	if team == 1 {
		return less(b, a)
	}
	return less(a, b)
}

// SortedPos returns positions in row-major order.
func SortedPos(ps []Pos) []Pos {
	return SortedPosFor(ps, 0)
}

// SortedPosFor returns positions in team's reading order.
func SortedPosFor(ps []Pos, team int) []Pos {
	out := append([]Pos(nil), ps...)
	sort.Slice(out, func(i, j int) bool { return LessFor(team, out[i], out[j]) })
	return out
}

// freeTilesNear returns up to n free passable tiles nearest to c (Chebyshev
// rings, row-major within a ring), optionally including c itself.
func freeTilesNear(s *State, c Pos, n int, maxR int, team int) []Pos {
	var out []Pos
	seen := map[Pos]bool{}
	for r := 0; r <= maxR && len(out) < n; r++ {
		for _, p := range s.Board.AreaFor(c, r, team) {
			if seen[p] || Dist(p, c) != r {
				continue
			}
			seen[p] = true
			if s.Board.Passable(p) && s.UnitAt(p) == nil {
				out = append(out, p)
				if len(out) == n {
					break
				}
			}
		}
	}
	return out
}

// AttackRange returns the effective range of a from its tile against a
// target at tp, including the height bonus.
func AttackRange(c *Content, s *State, a *Unit, from Pos, tp Pos) int {
	r := a.Eff("rng")
	if s.Board.At(from).Z > s.Board.At(tp).Z {
		r += c.Rules.HeightRangeBonus
	}
	return r
}

// CanAttack reports whether a at from can make a basic attack on t.
func CanAttack(c *Content, s *State, a *Unit, from Pos, t *Unit) bool {
	if !t.Alive() || a.Eff("rng") <= 0 || a.Eff("atk") <= 0 {
		return false
	}
	if Dist(from, t.Pos) > AttackRange(c, s, a, from, t.Pos) {
		return false
	}
	return s.LOS(from, t.Pos)
}
