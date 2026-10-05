package render

import (
	"strings"

	"rfog/engine"
)

// Orient is how the board is turned on screen. Like a chess board, each
// player sees their own side nearest them: on a tall screen the board is
// turned a quarter so its long side runs up the screen and the player's
// deploy edge is at the bottom; on a wide one the second team sees it
// turned half way, so both play from the left. The engine never knows:
// only drawing and pointing go through this.
type Orient int

const (
	OrientNone Orient = iota // as stored: x right, y down
	OrientCCW                // a quarter anticlockwise: x=0 at the bottom
	OrientCW                 // a quarter clockwise: x=W-1 at the bottom
	OrientHalf               // half a turn
)

// Turned reports whether the board's width runs up the screen.
func (o Orient) Turned() bool { return o == OrientCCW || o == OrientCW }

// Dims is the board's size on screen, in tiles.
func (o Orient) Dims(w, h int) (int, int) {
	if o.Turned() {
		return h, w
	}
	return w, h
}

// Board is the board tile drawn at screen tile (dx, dy) of a w x h board.
func (o Orient) Board(dx, dy, w, h int) engine.Pos {
	switch o {
	case OrientCCW:
		return engine.Pos{X: w - 1 - dy, Y: dx}
	case OrientCW:
		return engine.Pos{X: dy, Y: h - 1 - dx}
	case OrientHalf:
		return engine.Pos{X: w - 1 - dx, Y: h - 1 - dy}
	}
	return engine.Pos{X: dx, Y: dy}
}

// Screen is where board tile p is drawn, in screen tiles.
func (o Orient) Screen(p engine.Pos, w, h int) (int, int) {
	switch o {
	case OrientCCW:
		return p.Y, w - 1 - p.X
	case OrientCW:
		return h - 1 - p.Y, p.X
	case OrientHalf:
		return w - 1 - p.X, h - 1 - p.Y
	}
	return p.X, p.Y
}

// Step turns a step on screen (right, down) into a step on the board, so
// the cursor keys move the way the screen shows.
func (o Orient) Step(sx, sy int) (int, int) {
	switch o {
	case OrientCCW:
		return -sy, sx
	case OrientCW:
		return sy, -sx
	case OrientHalf:
		return -sx, -sy
	}
	return sx, sy
}

// ForTeam is the orientation that puts team's side nearest the player:
// turned a quarter when turned, else upright for team 0 and half a turn
// for team 1 (team 0 deploys on the left, team 1 on the right).
func ForTeam(team int, turned bool) Orient {
	switch {
	case turned && team == 1:
		return OrientCW
	case turned:
		return OrientCCW
	case team == 1:
		return OrientHalf
	}
	return OrientNone
}

// Dims is the scene's board size on screen.
func (sc *Scene) Dims() (int, int) { return sc.Orient.Dims(sc.Board.W, sc.Board.H) }

// ToBoard is the board tile at screen tile (dx, dy).
func (sc *Scene) ToBoard(dx, dy int) engine.Pos {
	return sc.Orient.Board(dx, dy, sc.Board.W, sc.Board.H)
}

// ToScreen is where board tile p is drawn.
func (sc *Scene) ToScreen(p engine.Pos) (int, int) {
	return sc.Orient.Screen(p, sc.Board.W, sc.Board.H)
}

// label names a screen column or row by the board coordinate it shows:
// a letter for a board column, a number for a board row.
func (sc *Scene) label(dx, dy int, column bool) string {
	p := sc.ToBoard(dx, dy)
	// A screen column shows one board column when upright or half turned,
	// one board row when turned a quarter; a screen row the other.
	if column != sc.Orient.Turned() {
		return string(rune('a' + p.X))
	}
	return itoa(p.Y + 1)
}

// ColumnHeader labels the viewport's screen columns.
func (sc *Scene) ColumnHeader(vp Viewport, st *Styles) string {
	cw, _ := sc.cellSize()
	var sb strings.Builder
	for x := vp.OX; x < vp.OX+vp.W; x++ {
		l := sc.label(x, vp.OY, true)
		pad := cw - len(l)
		if pad < 0 {
			pad = 0
		}
		left := (cw - 1) / 2
		if left > pad {
			left = pad
		}
		sb.WriteString(strings.Repeat(" ", left) + l + strings.Repeat(" ", pad-left))
	}
	return st.Dim.Render(sb.String())
}

// RowLabel labels screen row dy, padded to 2 columns.
func (sc *Scene) RowLabel(dy int, vp Viewport, st *Styles) string {
	l := sc.label(vp.OX, dy, false)
	if len(l) < 2 {
		l = " " + l
	}
	return st.Dim.Render(l)
}
