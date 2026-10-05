package render

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"rfog/render/palette"

	"rfog/engine"
)

// SceneUnit is a unit as drawn.
type SceneUnit struct {
	ID        int
	Team      int
	Kind      string
	Commander bool
	Pos       engine.Pos
	HP, MaxHP int
	// Pose is a figure's pose this beat ("" standing, "attack").
	Pose string
}

// Ghost is a last-seen enemy marker.
type Ghost struct {
	Kind string
	Turn int
}

// Scene is everything the board renderer needs. It is built from a fog
// filtered engine.State plus client-side selection state. All tiers render
// the same scene.
type Scene struct {
	Board      *engine.Board
	ViewTeam   int
	Visible    map[engine.Pos]bool // nil = everything visible
	Units      []SceneUnit
	Smoke      map[engine.Pos]bool
	Reveal     map[engine.Pos]bool
	Telegraph  map[engine.Pos]bool
	Ghosts     map[engine.Pos]Ghost
	Objectives []engine.Objective
	Cursor     *engine.Pos
	Highlight  map[engine.Pos]bool  // reachable tiles
	Targets    map[engine.Pos]bool  // legal targets
	Path       map[engine.Pos]bool  // planned path preview
	Selected   int                  // selected unit id, 0 = none
	Ordered    map[int]bool         // units that already have orders
	Flash      map[engine.Pos]Flash // one-frame overlays from animation
	Deploy     map[engine.Pos]bool
	// Ambient is the ambient-effect tick (0 = off): smoke drifts, objectives
	// pulse, telegraphs blink. Purely cosmetic; never changes what is shown.
	Ambient int
	// Planned is where this turn's ordered moves end, and whose: the
	// square shows a faded copy of the unit (Ghost) or its letter, dim.
	Planned map[engine.Pos]SceneUnit
	// Ghost is Piece, faded: a unit where it is going to be.
	Ghost func(u *SceneUnit, shade string, cols, rows int) []string
	// Trail and Scars are last turn, lingering through the next planning
	// phase: the tiles units walked through (faint steps under the floor)
	// and where units died (a dust mark). Cosmetic, like Ambient.
	Trail map[engine.Pos]bool
	Scars map[engine.Pos]bool
	// styles and wraps cache what a frame needs per style: deriving a
	// lipgloss style, and rendering through it, are both expensive.
	styles map[styleKey]lipgloss.Style
	wraps  map[styleKey]wrap
	fibre  map[engine.Pos]fibreSeg // cables under the floor, laid per frame
	// CellW and CellH are the size of one tile in terminal cells. The
	// board reads as a grid of squares when there is room for it (4x2 is
	// about square in a terminal); 2x1 is the packed form for a phone.
	CellW, CellH int
	// Orient is how the board is turned on screen; viewports are in
	// screen tiles (see Orient).
	Orient Orient
	// Piece, when set, gives a visible unit's picture for tiles big enough
	// to hold one (6x3 and up): the whole tile, cols x rows cells, already
	// coloured, its health drawn in the bottom pixel row. It returns nil
	// for a unit without one, which keeps its letter. shade is the
	// square's background ("" = none), or the cursor's or a target's
	// colour when one is on the square.
	Piece func(u *SceneUnit, shade string, cols, rows int) []string
	// Block, when set, draws a visible wall as a block standing on its
	// square (cols x rows cells, coloured); nil keeps the wall glyphs.
	Block func(p engine.Pos, shade string, cols, rows int) []string
	// lit is set while drawing a board whose every square has a colour
	// (Styles.Light): unseen squares then get the fog's background too.
	lit bool
}

// PieceCols and PieceRows are the smallest tile a piece is drawn on.
const PieceCols, PieceRows = 6, 3

// cellSize returns the tile size, defaulting to the packed 2x1.
func (sc *Scene) cellSize() (int, int) {
	w, h := sc.CellW, sc.CellH
	if w < 2 {
		w = 2
	}
	if h < 1 {
		h = 1
	}
	return w, h
}

// FitCell picks the largest tile size that shows the whole board in the
// space available, preferring square-ish tiles. Returns 2x1 when the
// board has to scroll anyway.
func FitCell(boardW, boardH, availW, availH int) (int, int) {
	for _, c := range [][2]int{{12, 6}, {10, 5}, {8, 4}, {6, 3}, {5, 2}, {4, 2}, {4, 1}, {3, 1}} {
		if boardW*c[0] <= availW && boardH*c[1] <= availH {
			return c[0], c[1]
		}
	}
	return 2, 1
}

// Flash is a one-beat overlay on a tile: the mark replaces the height
// digit and the role picks the colour.
type Flash struct {
	Mark string
	Role string // hit | miss | heal | score | cast | death | dust
	// Text floats over a piece this beat: damage ("-2") or healing ("+3"),
	// as the web app's numbers do. Drawn on tiles big enough for pieces.
	Text string
}

// flashKey picks the style for a flash role.
func flashKey(role string) styleKey {
	switch role {
	case "miss", "dust":
		return sFog
	case "heal":
		return sGround3
	case "score":
		return sObjective
	case "cast":
		return sSelected
	case "beamA":
		return sTeamA
	case "beamB":
		return sTeamB
	}
	return sDanger
}

// SceneFromView builds a scene from a player's view. Client selection state
// is added by the caller.
func SceneFromView(v *engine.State, team int) Scene {
	sc := Scene{Board: &v.Board, ViewTeam: team, Smoke: map[engine.Pos]bool{}, Reveal: map[engine.Pos]bool{},
		Telegraph: map[engine.Pos]bool{}, Ghosts: map[engine.Pos]Ghost{}, Objectives: v.Objectives}
	if v.Visible != nil {
		sc.Visible = map[engine.Pos]bool{}
		for _, p := range v.Visible {
			sc.Visible[p] = true
		}
	}
	for _, u := range v.Units {
		if !u.Alive() {
			continue
		}
		sc.Units = append(sc.Units, SceneUnit{ID: u.ID, Team: u.Team, Kind: u.Kind, Commander: u.IsCommander, Pos: u.Pos, HP: u.HP, MaxHP: u.MaxHP})
	}
	for _, e := range v.Effects {
		for _, p := range e.Tiles {
			switch e.Kind {
			case "smoke":
				sc.Smoke[p] = true
			case "reveal":
				sc.Reveal[p] = true
			}
		}
	}
	for _, d := range v.Delayed {
		sc.Telegraph[d.Target] = true
	}
	alive := map[int]bool{}
	for _, u := range sc.Units {
		alive[u.ID] = true
	}
	for _, s := range v.Sightings {
		if s.Team == team && !alive[s.UnitID] {
			sc.Ghosts[s.Pos] = Ghost{Kind: s.Kind, Turn: s.Turn}
		}
	}
	return sc
}

// TelegraphArea expands telegraph centres to their areas using content.
func (sc *Scene) TelegraphArea(c *engine.Content, v *engine.State) {
	sc.Telegraph = map[engine.Pos]bool{}
	for _, d := range v.Delayed {
		ab := c.Abilities[d.Ability]
		for _, p := range sc.Board.Area(d.Target, ab.Area) {
			sc.Telegraph[p] = true
		}
	}
}

func (sc *Scene) unitAt(p engine.Pos) *SceneUnit {
	for i := range sc.Units {
		if sc.Units[i].Pos == p {
			return &sc.Units[i]
		}
	}
	return nil
}

func (sc *Scene) visible(p engine.Pos) bool {
	return sc.Visible == nil || sc.Visible[p]
}

// Cell renders one tile as a two-column string.
// cellParts is what one tile draws: the glyph, the mark that follows it
// (height, a flash, a telegraph) and the style for both.
func (sc *Scene) cellParts(p engine.Pos, st *Styles, g Glyphs) (ch, zmark string, key styleKey) {
	b := sc.Board
	t := b.At(p)
	key = sPlain
	zmark = " "
	if t.Z > 0 {
		zmark = string('0' + rune(t.Z))
	}
	switch {
	case !sc.visible(p):
		if gh, ok := sc.Ghosts[p]; ok {
			ch = strings.ToLower(UnitGlyph(gh.Kind, false))
			if len(gh.Kind) > 0 && gh.Kind != "lineman" && gh.Kind != "ranged" && gh.Kind != "runner" && gh.Kind != "medic" && gh.Kind != "junkbot" {
				ch = strings.ToLower(UnitGlyph(gh.Kind, true))
			}
			key = sFog
			zmark = g.Ghost
		} else if t.Terrain == engine.TerrainWall {
			ch, key, zmark = g.Wall, sFog, " "
		} else {
			ch, key, zmark = g.Fog, sFog, " "
			if t.Terrain == engine.TerrainObjective {
				ch = g.Objective
			}
		}
	case sc.unitAt(p) != nil:
		u := sc.unitAt(p)
		ch = UnitGlyph(u.Kind, u.Commander)
		if u.Team == 0 {
			key = sTeamA
		} else {
			ch = strings.ToLower(ch)
			key = sTeamB
		}
		if sc.Selected == u.ID {
			key = sSelected
		}
		if u.Commander {
			key |= fUnderline
		}
		if u.HP*3 <= u.MaxHP {
			key |= fItalic
		}
	case sc.plannedAt(p) != nil:
		// Where one of this turn's ordered moves ends: the unit's letter,
		// dim and underlined, so it reads as "going here", not "here".
		u := sc.plannedAt(p)
		ch = UnitGlyph(u.Kind, u.Commander)
		if u.Team != 0 {
			ch = strings.ToLower(ch)
		}
		key = sFog | fUnderline
	case sc.Smoke[p]:
		ch, key = g.Smoke, sSmoke
		if sc.Ambient > 0 && (sc.Ambient+p.X+p.Y)%3 == 0 {
			ch = g.SmokeAlt
		}
	case sc.Telegraph[p]:
		ch, key = g.Telegraph, sDanger
		if sc.Ambient%2 == 1 {
			key |= fReverse
		}
	default:
		switch t.Terrain {
		case engine.TerrainWall:
			ch, key, zmark = g.Wall, sWall, " "
		case engine.TerrainCover:
			ch, key = g.Cover, sCover
		case engine.TerrainObjective:
			ch, key = g.Objective, sObjective
			if sc.Ambient > 0 && sc.Ambient%4 == 0 {
				key |= fFlat
			}
		default:
			ch, key, zmark = g.Open[t.Z], sGround0+styleKey(t.Z), " "
			if sc.Deploy[p] {
				ch = g.Deploy
			}
			if sc.Scars[p] {
				ch, key = g.Dust, sFog // someone fell here last turn
			}
		}
	}
	if f, ok := sc.Flash[p]; ok && f.Mark != "" {
		zmark = f.Mark
		key = flashKey(f.Role)
		if f.Role == "death" || f.Role == "dust" {
			ch, zmark = f.Mark, " "
		}
	}
	if sc.Telegraph[p] && sc.unitAt(p) != nil {
		zmark = g.Telegraph
	}
	return ch, zmark, key
}

// Cell renders one tile at the packed size (used by tests and tools).
func (sc *Scene) Cell(p engine.Pos, st *Styles, g Glyphs) string {
	ch, zmark, key := sc.cellParts(p, st, g)
	style := sc.resolve(key, st)
	cell := ch + zmark
	switch {
	case sc.Cursor != nil && *sc.Cursor == p:
		return st.Cursor.Render(cell)
	case sc.Targets[p]:
		return st.Target.Render(cell)
	case sc.Path[p]:
		return st.Target.Render(cell)
	case sc.Highlight[p]:
		return st.Highlight.Render(style.Render(ch) + zmark)
	}
	return style.Render(cell)
}

// Viewport is the visible window of the board in tiles.
type Viewport struct {
	OX, OY int // origin tile
	W, H   int // size in tiles
}

// ClampViewport moves the viewport so it stays on the board and, if given,
// keeps the focus tile inside it.
func ClampViewport(vp Viewport, b *engine.Board, focus *engine.Pos) Viewport {
	if vp.W > b.W {
		vp.W = b.W
	}
	if vp.H > b.H {
		vp.H = b.H
	}
	if focus != nil {
		if focus.X < vp.OX {
			vp.OX = focus.X
		}
		if focus.X >= vp.OX+vp.W {
			vp.OX = focus.X - vp.W + 1
		}
		if focus.Y < vp.OY {
			vp.OY = focus.Y
		}
		if focus.Y >= vp.OY+vp.H {
			vp.OY = focus.Y - vp.H + 1
		}
	}
	if vp.OX+vp.W > b.W {
		vp.OX = b.W - vp.W
	}
	if vp.OY+vp.H > b.H {
		vp.OY = b.H - vp.H
	}
	if vp.OX < 0 {
		vp.OX = 0
	}
	if vp.OY < 0 {
		vp.OY = 0
	}
	return vp
}

// RenderBoard draws the viewport. Each tile is CellW x CellH terminal
// cells; empty ground alternates two shades so the grid reads as squares
// (a chessboard), and runs of identical styling share one escape
// sequence, which is most of what a frame costs.
func (sc *Scene) RenderBoard(st *Styles, g Glyphs, vp Viewport) []string {
	cw, ch := sc.cellSize()
	// The glyph sits on the tile's middle row, its health on the row
	// under it; any other rows are floor.
	glyphRow := (ch - 1) / 2
	// Big tiles in colour are a real chessboard: the dark squares get a
	// background instead of a dotted floor.
	shaded := cw >= 6 && st.Shade != ""
	sc.lit = shaded && st.Light != ""
	sc.fibre = nil
	if sc.lit && ch >= 3 {
		sc.fibre = sc.fibreRuns()
	}
	lines := make([]string, 0, vp.H*ch)
	for y := vp.OY; y < vp.OY+vp.H; y++ {
		rows := make([]*runBuilder, ch)
		for i := range rows {
			rows[i] = &runBuilder{}
		}
		for x := vp.OX; x < vp.OX+vp.W; x++ {
			p := sc.ToBoard(x, y)
			if sc.drawBlock(p, st, cw, ch, rows, shaded) || sc.drawPiece(p, st, g, cw, ch, rows, shaded) || sc.drawPlanned(p, st, cw, ch, rows, shaded) {
				continue
			}
			glyph, mark, key := sc.cellParts(p, st, g)
			key = sc.shadeKey(p, sc.tileKey(p, key), shaded)
			// The glyph sits in the middle of the tile, its mark after it.
			body := glyph + mark
			pad := cw - 2
			left := pad / 2
			text := strings.Repeat(" ", left) + body + strings.Repeat(" ", pad-left)
			// The selected unit breathes: pointers close in on it and
			// open again with the ambient tick.
			if u := sc.unitAt(p); u != nil && u.ID == sc.Selected && cw >= 4 && sc.Ambient%2 == 1 && sc.visible(p) {
				side := (cw - 4) / 2
				text = strings.Repeat(" ", side) + g.PulseL + body + g.PulseR + strings.Repeat(" ", cw-4-side)
			}
			if f, ok := sc.Flash[p]; ok && (f.Role == "beamA" || f.Role == "beamB") && cw >= 4 {
				text = strings.Repeat(" ", 1) + strings.Repeat(f.Mark, cw-2) + " " // a shot crossing the square
			}
			if f, _, ok := sc.fibreAt(p, -1, glyphRow); ok && f.v && []rune(text)[cw/2] == ' ' {
				sc.addFibre(rows[glyphRow], key, st, text, f, false) // a cable running down passes the middle too
			} else {
				rows[glyphRow].add(key, sc.wrapper(key, st), text)
			}
			// The other rows are the tile's floor. A single faint dot in
			// the middle of the shaded squares makes the grid read as a
			// chessboard; filling them would be noise.
			for i := 0; i < ch; i++ {
				if i == glyphRow {
					continue
				}
				if i == glyphRow+1 && sc.healthRow(p, st, g, cw, rows[i], shaded) {
					continue
				}
				fk := sc.shadeKey(p, sc.tileKey(p, sGround0), shaded)
				if sc.lit && sc.visible(p) && fk&(fShade|fLight) != 0 {
					switch {
					case i == ch-1 && sc.ledge(x, y):
						fk = (fk &^ (fShade | fLight)) | fLedge
					case i == 0 && sc.shadowed(x, y):
						fk = (fk &^ (fShade | fLight)) | fCast
					}
				}
				text := sc.floorWear(p, i, ch, cw, sc.floorRow(p, g, cw, shaded))
				if run, across, ok := sc.fibreAt(p, i, glyphRow); ok {
					sc.addFibre(rows[i], fk, st, text, run, across)
					continue
				}
				rows[i].add(fk, sc.wrapper(fk, st), text)
			}
		}
		for _, r := range rows {
			lines = append(lines, r.String())
		}
	}
	return lines
}

// drawPiece draws a visible unit as its picture when the tile has room:
// the piece on the top rows, its health on the row under it (which also
// carries the cursor and target highlights), floor below that. It reports
// false to leave the tile to the letter.
func (sc *Scene) drawPiece(p engine.Pos, st *Styles, g Glyphs, cw, ch int, rows []*runBuilder, shaded bool) bool {
	if sc.Piece == nil || cw < PieceCols || ch < PieceRows || !sc.visible(p) {
		return false
	}
	u := sc.unitAt(p)
	if u == nil {
		return false
	}
	if f, ok := sc.Flash[p]; ok && f.Mark != "" && !(sc.lit && flashBg(f.Role) != "") {
		return false // a hit or a death this beat: the mark says it
	}
	piece := sc.Piece(u, sc.pieceBg(p, st, shaded), cw, ch)
	if len(piece) < ch {
		return false
	}
	if f, ok := sc.Flash[p]; ok && f.Text != "" && sc.lit && st.Renderer != nil {
		// The number takes the piece's top row, on the flash's colour.
		n := Width(f.Text)
		l := (cw - n) / 2
		piece[0] = st.Renderer.NewStyle().Background(palette.Color(st.Renderer, flashBg(f.Role))).Foreground(lipgloss.Color("#ffffff")).Bold(true).
			Render(strings.Repeat(" ", l) + f.Text + strings.Repeat(" ", cw-n-l))
	}
	sc.placePiece(p, st, cw, rows, piece, shaded)
	return true
}

// pieceBg is what a piece on p sits on: the cursor's colour, a target's,
// a reachable square's, else the square's own.
func (sc *Scene) pieceBg(p engine.Pos, st *Styles, shaded bool) string {
	t := st.Theme
	if f, ok := sc.Flash[p]; ok && sc.lit && flashBg(f.Role) != "" {
		return flashBg(f.Role) // the square flashes behind the figure
	}
	switch {
	case sc.Cursor != nil && *sc.Cursor == p:
		return t.Cursor
	case sc.Targets[p], sc.Path[p], sc.Highlight[p]:
		return t.Highlight
	}
	return sc.shadeFor(p, st, shaded)
}

// drawBlock draws a wall as a block when the client gives one and the
// tile has room — in the fog too, dimmer: walls are terrain both sides
// know, and sight lines stop at them, so a wall is rarely "visible". It
// reports false to leave the wall to glyphs.
func (sc *Scene) drawBlock(p engine.Pos, st *Styles, cw, ch int, rows []*runBuilder, shaded bool) bool {
	if sc.Block == nil || !shaded || cw < 4 || ch < 2 || sc.Board.At(p).Terrain != engine.TerrainWall {
		return false
	}
	if sc.Cursor != nil && *sc.Cursor == p {
		return false
	}
	lines := sc.Block(p, sc.shadeFor(p, st, shaded), cw, ch)
	if len(lines) < ch {
		return false
	}
	for i := 0; i < ch; i++ {
		rows[i].raw(lines[i])
	}
	return true
}

// placePiece writes a piece's rows (each a full tile wide) into the tile.
func (sc *Scene) placePiece(p engine.Pos, st *Styles, cw int, rows []*runBuilder, piece []string, shaded bool) {
	for i := range piece {
		if i < len(rows) {
			rows[i].raw(piece[i])
		}
	}
}

// plannedAt is the unit whose planned move ends at p, if any.
func (sc *Scene) plannedAt(p engine.Pos) *SceneUnit {
	if u, ok := sc.Planned[p]; ok {
		return &u
	}
	return nil
}

// drawPlanned draws a planned move's destination as the faded unit when
// the tile holds a picture; smaller tiles get the letter from cellParts.
func (sc *Scene) drawPlanned(p engine.Pos, st *Styles, cw, ch int, rows []*runBuilder, shaded bool) bool {
	u, ok := sc.Planned[p]
	if !ok || sc.Ghost == nil || cw < PieceCols || ch < PieceRows || sc.unitAt(p) != nil || !sc.visible(p) {
		return false
	}
	ghost := sc.Ghost(&u, sc.pieceBg(p, st, shaded), cw, ch)
	if len(ghost) < ch {
		return false
	}
	sc.placePiece(p, st, cw, rows, ghost, shaded)
	return true
}

// shadeKey puts a dark square's background under key on a board of big
// tiles, unless a highlight already paints the square.
func (sc *Scene) shadeKey(p engine.Pos, key styleKey, shaded bool) styleKey {
	if !shaded {
		return key
	}
	switch key & baseMask {
	case sCursor, sTarget, sHighlight:
		return key
	}
	switch {
	case !sc.visible(p):
		if sc.lit {
			return key | fFogBg
		}
		return key
	case sc.lit && sc.objFlag(p) != 0:
		return key | sc.objFlag(p)
	case sc.lit && sc.Trail[p]:
		return key | fLast
	case sc.dark(p):
		return key | fShade
	case sc.lit:
		return key | fLight
	}
	return key
}

// objFlag is an objective square's background on a lit board, by holder
// (0 when p is not part of an objective).
func (sc *Scene) objFlag(p engine.Pos) styleKey {
	for _, o := range sc.Objectives {
		for _, t := range o.Tiles {
			if t != p {
				continue
			}
			switch {
			case o.Holder < 0 && sc.occupied(o):
				return fObjC
			case o.Holder < 0:
				return fObjBg
			case o.Holder == 0:
				return fObjA
			}
			return fObjB
		}
	}
	return 0
}

// shadeFor is the background a piece on p sits on: the shade on a dark
// square of a shaded board, else none.
func (sc *Scene) shadeFor(p engine.Pos, st *Styles, shaded bool) string {
	switch {
	case !shaded:
		return ""
	case !sc.visible(p):
		if sc.lit {
			return st.FogBg
		}
		return ""
	case sc.lit && sc.objFlag(p) != 0:
		switch sc.objFlag(p) {
		case fObjA:
			return st.ObjA
		case fObjB:
			return st.ObjB
		case fObjC:
			return st.ObjC
		}
		return st.ObjBg
	case sc.lit && sc.Trail[p]:
		return st.LastBg
	case sc.dark(p):
		return st.Shade
	case sc.lit:
		return st.Light
	}
	return ""
}

// tileKey applies the cursor, targets, path and reachable highlights,
// and the checkerboard shade under everything else.
func (sc *Scene) tileKey(p engine.Pos, key styleKey) styleKey {
	switch {
	case sc.Cursor != nil && *sc.Cursor == p:
		return sCursor
	case sc.Targets[p], sc.Path[p]:
		return sTarget
	case sc.Highlight[p]:
		return sHighlight | (key & (fUnderline | fItalic))
	}
	return key
}

// healthRow draws a visible unit's health in the row under it on a tall
// tile: a bar as wide as the tile less a one-cell gutter (so neighbours'
// bars stay apart), filled in the unit's colour, red once it is down to a
// third. It reports false when there is no unit to draw, leaving the
// floor to floorRow.
func (sc *Scene) healthRow(p engine.Pos, st *Styles, g Glyphs, cw int, r *runBuilder, shaded bool) bool {
	u := sc.unitAt(p)
	if u == nil || !sc.visible(p) || u.MaxHP <= 0 || cw < 3 {
		return false
	}
	w := cw - 1
	full := (u.HP*w + u.MaxHP - 1) / u.MaxHP // round up: alive shows at least one
	if full > w {
		full = w
	}
	// Coloured by how hurt, not by team: the piece's outline says whose
	// it is, the bar how much is left.
	fill := sGood
	switch {
	case u.HP*3 <= u.MaxHP:
		fill = sDanger
	case u.HP*3 <= u.MaxHP*2:
		fill = sWarn
	}
	fk, ek := sc.shadeKey(p, sc.tileKey(p, fill), shaded), sc.shadeKey(p, sc.tileKey(p, sFog), shaded)
	r.add(fk, sc.wrapper(fk, st), strings.Repeat(g.HP, full))
	r.add(ek, sc.wrapper(ek, st), strings.Repeat(g.HPEmpty, w-full)+" ")
	return true
}

// floorRow is a tall tile's lower row: solid for a wall, a faint dot in
// the middle for the shaded half of the chessboard, blank otherwise.
// Shading with a background colour would cost an escape sequence per
// tile, which is most of a frame's bytes, and a dot grid reads as
// squares just as well.
func (sc *Scene) floorRow(p engine.Pos, g Glyphs, cw int, shaded bool) string {
	t := sc.Board.At(p)
	if t.Terrain == engine.TerrainWall {
		return strings.Repeat(g.Wall, cw)
	}
	if sc.Trail[p] && sc.visible(p) && sc.unitAt(p) == nil && cw >= 3 {
		mid := (cw - 2) / 2
		return strings.Repeat(" ", mid) + g.Step + g.Step + strings.Repeat(" ", cw-mid-2)
	}
	if shaded || !sc.dark(p) || !sc.visible(p) || sc.unitAt(p) != nil {
		return strings.Repeat(" ", cw)
	}
	mid := (cw - 1) / 2
	return strings.Repeat(" ", mid) + g.Checker + strings.Repeat(" ", cw-mid-1)
}

// dark reports whether a tile is on the shaded half of the chessboard.
func (sc *Scene) dark(p engine.Pos) bool { return (p.X+p.Y)%2 == 1 }

// runBuilder collects styled text, emitting one escape sequence per run
// of identical styling. Styles are compared by key, not by rendering
// them: building a lipgloss style string per cell is most of a frame.
type runBuilder struct {
	sb      strings.Builder
	cur     styleKey
	w       wrap
	run     strings.Builder
	begun   bool
	painted bool // the current style has a background
}

// raw writes text that carries its own colours (a piece), closing the
// current run first so its style does not leak into it.
func (r *runBuilder) raw(text string) {
	r.flush()
	r.begun = false
	r.sb.WriteString("\x1b[0m")
	r.sb.WriteString(text)
	r.sb.WriteString("\x1b[0m")
}

func (r *runBuilder) add(key styleKey, w wrap, text string) {
	if r.begun && key != r.cur {
		r.flush()
	}
	r.cur, r.w, r.begun = key, w, true
	base := key & baseMask
	r.painted = base == sCursor || base == sTarget || base == sHighlight || key&(fShade|fLight|fFogBg|fObjBg|fObjA|fObjB|fObjC|fLast|fLedge|fCast) != 0
	r.run.WriteString(text)
}

func (r *runBuilder) flush() {
	if r.run.Len() == 0 {
		return
	}
	text := r.run.String()
	r.run.Reset()
	// Blank text only needs styling when the style paints a background.
	if strings.TrimLeft(text, " ") == "" && !r.painted {
		r.sb.WriteString(text)
		return
	}
	r.sb.WriteString(r.w.pre)
	r.sb.WriteString(text)
	r.sb.WriteString(r.w.post)
}

func (r *runBuilder) String() string {
	r.flush()
	return r.sb.String()
}

// styleKey names a style without building it: which base style, plus the
// modifiers a tile can add.
type styleKey uint32

// baseMask picks the base style out of a key; modifiers sit above it.
const baseMask styleKey = 0xff

const (
	sPlain styleKey = iota
	sFog
	sTeamA
	sTeamB
	sSmoke
	sDanger
	sWall
	sCover
	sObjective
	sGround0
	sGround1
	sGround2
	sGround3
	sSelected
	sCursor
	sTarget
	sHighlight
	sGood // health above two thirds
	sWarn // health above a third
	sStyleCount
)

const (
	// Modifiers are bits above the base style's byte (baseMask), so any
	// set of them combines without colliding.
	fUnderline styleKey = 1 << (8 + iota)
	fItalic
	fReverse
	fFlat  // objective pulse: the bold turned off
	fShade // a dark square's background (big tiles, colour themes)
	fLight // a light square's background (themes with Light)
	fFogBg // an unseen square's background (themes with Light)
	fObjBg // an objective square nobody holds (themes with Light)
	fObjA  // an objective square team A holds
	fObjB  // an objective square team B holds
	fObjC  // an objective square that is contested
	fLast  // last turn's moves (themes with Light)
	fLedge // the front face of raised ground, where it drops
	fCast  // shadow from a wall or higher ground behind
)

// base returns the style a key starts from.
func (st *Styles) base(k styleKey) lipgloss.Style {
	switch k & baseMask {
	case sFog:
		return st.Fog
	case sTeamA:
		return st.TeamA
	case sTeamB:
		return st.TeamB
	case sSmoke:
		return st.Smoke
	case sDanger:
		return st.Danger
	case sWall:
		return st.Wall
	case sCover:
		return st.Cover
	case sObjective:
		return st.Objective
	case sGround0:
		return st.Ground[0]
	case sGround1:
		return st.Ground[1]
	case sGround2:
		return st.Ground[2]
	case sGround3:
		return st.Ground[3]
	case sSelected:
		return st.Selected
	case sCursor:
		return st.Cursor
	case sTarget:
		return st.Target
	case sHighlight:
		return st.Highlight
	case sGood:
		return st.Good
	case sWarn:
		return st.Warn
	}
	return st.Plain
}

// wrap is a style reduced to the escape sequences around its text.
// lipgloss.Style.Render measures widths and checks borders on every
// call, which is most of a frame when the board calls it per run; the
// sequences never change for a given key, so they are built once.
type wrap struct{ pre, post string }

// wrapper returns the escapes for a key, building them once per frame.
func (sc *Scene) wrapper(k styleKey, st *Styles) wrap {
	if sc.wraps == nil {
		sc.wraps = map[styleKey]wrap{}
	}
	if w, ok := sc.wraps[k]; ok {
		return w
	}
	const sentinel = "\x00"
	out := sc.resolve(k, st).Render(sentinel)
	w := wrap{}
	if i := strings.Index(out, sentinel); i >= 0 {
		w = wrap{pre: out[:i], post: out[i+len(sentinel):]}
	}
	sc.wraps[k] = w
	return w
}

// resolve builds the style for a key once per frame.
func (sc *Scene) resolve(k styleKey, st *Styles) lipgloss.Style {
	if sc.styles == nil {
		sc.styles = map[styleKey]lipgloss.Style{}
	}
	if s, ok := sc.styles[k]; ok {
		return s
	}
	s := st.base(k)
	if k&fUnderline != 0 {
		s = s.Underline(true)
	}
	if k&fItalic != 0 {
		s = s.Italic(true)
	}
	if k&fReverse != 0 {
		s = s.Reverse(true)
	}
	if k&fFlat != 0 {
		s = s.Bold(false)
	}
	if k&fShade != 0 && st.Shade != "" {
		s = s.Background(palette.Color(st.Renderer, st.Shade))
	}
	if k&fLight != 0 && st.Light != "" {
		s = s.Background(palette.Color(st.Renderer, st.Light))
	}
	if k&fFogBg != 0 && st.FogBg != "" {
		s = s.Background(palette.Color(st.Renderer, st.FogBg))
	}
	for _, o := range []struct {
		f  styleKey
		bg string
	}{{fObjBg, st.ObjBg}, {fObjA, st.ObjA}, {fObjB, st.ObjB}, {fObjC, st.ObjC}, {fLast, st.LastBg}, {fLedge, st.LedgeBg}, {fCast, st.CastBg}} {
		if k&o.f != 0 && o.bg != "" {
			s = s.Background(palette.Color(st.Renderer, o.bg))
		}
	}
	sc.styles[k] = s
	return s
}

// ColumnHeader renders the a..z column labels for the viewport.
func ColumnHeader(vp Viewport, st *Styles, cellW int) string {
	if cellW < 2 {
		cellW = 2
	}
	var sb strings.Builder
	for x := vp.OX; x < vp.OX+vp.W; x++ {
		pad := cellW - 1
		left := pad / 2
		sb.WriteString(strings.Repeat(" ", left) + string(rune('a'+x)) + strings.Repeat(" ", pad-left))
	}
	return st.Dim.Render(sb.String())
}

// RowLabel renders a 1-based row label padded to 2 columns.
func RowLabel(y int, st *Styles) string {
	s := strings.Repeat(" ", 0)
	n := y + 1
	if n < 10 {
		s = " "
	}
	return st.Dim.Render(s + itoa(n))
}

// RenderMinimap draws one character per tile (or per block, if the board is
// larger than maxW×maxH) and marks the viewport corners.
func (sc *Scene) RenderMinimap(st *Styles, g Glyphs, vp Viewport, maxW, maxH int) []string {
	b := sc.Board
	bw, bh := sc.Dims() // drawn the way the board is turned
	scale := 1
	for (bw+scale-1)/scale > maxW || (bh+scale-1)/scale > maxH {
		scale++
	}
	w := (bw + scale - 1) / scale
	h := (bh + scale - 1) / scale
	var lines []string
	for my := 0; my < h; my++ {
		var sb strings.Builder
		for mx := 0; mx < w; mx++ {
			ch := " "
			style := st.Fog
			prio := 0 // 0 fog, 1 ground, 2 terrain, 3 unit
			inView := false
			for dy := 0; dy < scale; dy++ {
				for dx := 0; dx < scale; dx++ {
					sx, sy := mx*scale+dx, my*scale+dy
					if sx >= bw || sy >= bh {
						continue
					}
					p := sc.ToBoard(sx, sy)
					if sx >= vp.OX && sx < vp.OX+vp.W && sy >= vp.OY && sy < vp.OY+vp.H {
						inView = true
					}
					if u := sc.unitAt(p); u != nil && sc.visible(p) {
						ch = UnitGlyph(u.Kind, u.Commander)
						if u.Team == 0 {
							style = st.TeamA
						} else {
							ch = strings.ToLower(ch)
							style = st.TeamB
						}
						prio = 3
						continue
					}
					if prio >= 2 {
						continue
					}
					t := b.At(p)
					switch {
					case !sc.visible(p):
						if t.Terrain == engine.TerrainObjective {
							ch, style, prio = g.Objective, st.Fog, 2
						}
					case t.Terrain == engine.TerrainObjective:
						ch, style, prio = g.Objective, st.Objective, 2
					case t.Terrain == engine.TerrainWall:
						ch, style, prio = g.Wall, st.Wall, 2
					case sc.Telegraph[p]:
						ch, style, prio = g.Telegraph, st.Danger, 2
					default:
						if prio < 1 {
							ch, style, prio = g.Open[t.Z], st.Ground[t.Z], 1
						}
					}
				}
			}
			if inView && (vp.W < bw || vp.H < bh) {
				style = style.Underline(true)
			}
			sb.WriteString(style.Render(ch))
		}
		lines = append(lines, sb.String())
	}
	return lines
}

// Bar renders a fixed-width meter.
func Bar(cur, max, width int, g Glyphs, full, empty lipgloss.Style) string {
	if width <= 0 {
		return ""
	}
	if max <= 0 {
		max = 1
	}
	n := cur * width / max
	if cur > 0 && n == 0 {
		n = 1
	}
	if n > width {
		n = width
	}
	return full.Render(strings.Repeat(g.Bar, n)) + empty.Render(strings.Repeat(g.BarEmpty, width-n))
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	if neg {
		b = append([]byte{'-'}, b...)
	}
	return string(b)
}

// PosLabel formats a tile as chess-like "d7".
func PosLabel(p engine.Pos) string {
	return string(rune('a'+p.X)) + itoa(p.Y+1)
}

// ParsePos parses "d7" (case-insensitive). ok=false if malformed.
func ParsePos(s string) (engine.Pos, bool) {
	s = strings.TrimSpace(strings.ToLower(s))
	if len(s) < 2 || s[0] < 'a' || s[0] > 'z' {
		return engine.Pos{}, false
	}
	n := 0
	for _, r := range s[1:] {
		if r < '0' || r > '9' {
			return engine.Pos{}, false
		}
		n = n*10 + int(r-'0')
	}
	if n < 1 {
		return engine.Pos{}, false
	}
	return engine.Pos{X: int(s[0] - 'a'), Y: n - 1}, true
}

// Frame draws a box around lines (each line must already be `w` columns).
func Frame(lines []string, w int, title string, st *Styles, g Glyphs) []string {
	top := g.TL + g.H
	if title != "" {
		t := " " + title + " "
		top += t
		w2 := w - lipgloss.Width(t) - 1
		if w2 < 0 {
			w2 = 0
		}
		top += strings.Repeat(g.H, w2)
	} else {
		top += strings.Repeat(g.H, w-1)
	}
	top += g.TR
	out := []string{st.Border.Render(top)}
	for _, l := range lines {
		pad := w - lipgloss.Width(l)
		if pad < 0 {
			pad = 0
		}
		out = append(out, st.Border.Render(g.V)+l+strings.Repeat(" ", pad)+st.Border.Render(g.V))
	}
	out = append(out, st.Border.Render(g.BL+strings.Repeat(g.H, w)+g.BR))
	return out
}

// FrameFooter writes text into a frame's bottom edge ("╰─ text ──╯"),
// for a line that belongs to the frame without costing a row.
func FrameFooter(frame []string, w int, text string, st *Styles, g Glyphs) {
	if len(frame) == 0 || text == "" {
		return
	}
	t := " " + text + " "
	rest := w - 1 - lipgloss.Width(t)
	if rest < 0 {
		return // no room: the plain edge stays
	}
	frame[len(frame)-1] = st.Border.Render(g.BL+g.H) + t + st.Border.Render(strings.Repeat(g.H, rest)+g.BR)
}

// flashBg is the colour a square flashes behind a figure this beat ("" for
// roles that replace the figure with a mark).
func flashBg(role string) string {
	switch role {
	case "hit":
		return "#7a2e2a"
	case "heal":
		return "#2e6a3e"
	case "cast":
		return "#245a66"
	case "score":
		return "#3d8291"
	}
	return ""
}

// occupied reports whether any visible unit stands on an objective.
func (sc *Scene) occupied(o engine.Objective) bool {
	for _, t := range o.Tiles {
		if sc.unitAt(t) != nil {
			return true
		}
	}
	return false
}

// tileAtScreen is the terrain at a screen square, ok false off the board.
func (sc *Scene) tileAtScreen(x, y int) (engine.Tile, bool) {
	w, h := sc.Orient.Dims(sc.Board.W, sc.Board.H)
	if x < 0 || y < 0 || x >= w || y >= h {
		return engine.Tile{}, false
	}
	return sc.Board.At(sc.ToBoard(x, y)), true
}

// ledge reports whether the square at screen (x, y) is higher than the one
// in front of it (below on screen): its bottom row is the face of the drop.
func (sc *Scene) ledge(x, y int) bool {
	t, _ := sc.tileAtScreen(x, y)
	f, ok := sc.tileAtScreen(x, y+1)
	return ok && t.Terrain != engine.TerrainWall && f.Terrain != engine.TerrainWall && t.Z > f.Z
}

// shadowed reports whether a wall or higher ground stands behind the square
// (above on screen): its top row lies in that shadow.
func (sc *Scene) shadowed(x, y int) bool {
	t, _ := sc.tileAtScreen(x, y)
	b, ok := sc.tileAtScreen(x, y-1)
	return ok && t.Terrain != engine.TerrainWall && (b.Terrain == engine.TerrainWall || b.Z > t.Z)
}

// floorWear marks some plain floor squares on a lit board, fixed per square
// like the web app's plates: corner bolts, a seam, a crack, a grate.
func (sc *Scene) floorWear(p engine.Pos, row, ch, cw int, text string) string {
	if !sc.lit || cw < 6 || ch < 3 || !sc.visible(p) || sc.unitAt(p) != nil || sc.Board.At(p).Terrain != engine.TerrainOpen {
		return text
	}
	rs := []rune(text)
	if len(rs) != cw {
		return text
	}
	wear := uint32(p.X*73856093) ^ uint32(p.Y*19349663)
	switch wear % 9 {
	case 3: // corner bolts
		if row == 0 || row == ch-1 {
			rs[0], rs[cw-1] = '·', '·'
		}
	case 4: // a seam across
		if row == ch-1 {
			for i := 1; i < cw-1; i++ {
				rs[i] = '┄'
			}
		}
	case 5: // a crack
		if row == ch-1 && cw > 3 {
			rs[cw/2-1], rs[cw/2] = '╱', '╲'
		}
	case 6: // a grate
		if row == ch-1 {
			for i := cw/2 - 1; i <= cw/2+1 && i < cw; i++ {
				rs[i] = '≡'
			}
		}
	}
	return string(rs)
}

// fibreSeg is the cable under one floor tile: running across, down, or
// both (a corner), and whose signal it carries (-1 nobody's).
type fibreSeg struct {
	h, v   bool
	holder int
}

// fibreRuns lays the cables the web board draws under the floor: from each
// objective to the nearest side of the board along its row, and from the
// biggest objective (the core) out to each other one, around a corner.
func (sc *Scene) fibreRuns() map[engine.Pos]fibreSeg {
	if len(sc.Objectives) == 0 {
		return nil
	}
	w, h := sc.Dims()
	out := map[engine.Pos]fibreSeg{}
	mark := func(x, y int, across bool, holder int) {
		if x < 0 || y < 0 || x >= w || y >= h {
			return
		}
		p := sc.Orient.Board(x, y, sc.Board.W, sc.Board.H)
		f, seen := out[p]
		if !seen || holder >= 0 {
			f.holder = holder // a held run shows over an idle one
		}
		if across {
			f.h = true
		} else {
			f.v = true
		}
		out[p] = f
	}
	mid := func(o engine.Objective) (int, int) {
		var x, y int
		for _, t := range o.Tiles {
			sx, sy := sc.Orient.Screen(t, sc.Board.W, sc.Board.H)
			x, y = x+sx, y+sy
		}
		return x / len(o.Tiles), y / len(o.Tiles)
	}
	line := func(a, b int, at func(int)) {
		if a > b {
			a, b = b, a
		}
		for i := a; i <= b; i++ {
			at(i)
		}
	}
	core := sc.Objectives[0]
	for _, o := range sc.Objectives {
		if len(o.Tiles) > len(core.Tiles) {
			core = o
		}
	}
	cx, cy := mid(core)
	for _, o := range sc.Objectives {
		mx, my := mid(o)
		edge := 0
		if mx >= w/2 {
			edge = w - 1
		}
		line(edge, mx, func(x int) { mark(x, my, true, o.Holder) })
		if o.ID == core.ID {
			continue
		}
		line(cy, my, func(y int) { mark(cx, y, false, o.Holder) })
		line(cx, mx, func(x int) { mark(x, my, true, o.Holder) })
	}
	return out
}

// fibreAt is the cable on row i of p's floor, if it shows there (open
// ground in sight with nothing on it), and whether it runs across that row.
func (sc *Scene) fibreAt(p engine.Pos, i, glyphRow int) (f fibreSeg, across, ok bool) {
	f, ok = sc.fibre[p]
	if !ok || !sc.visible(p) || sc.unitAt(p) != nil || sc.Board.At(p).Terrain != engine.TerrainOpen || sc.objectiveAt(p) {
		return f, false, false
	}
	across = f.h && i == glyphRow+1
	return f, across, across || f.v
}

// objectiveAt reports whether p is an objective square (lit on its own).
func (sc *Scene) objectiveAt(p engine.Pos) bool {
	for _, o := range sc.Objectives {
		for _, t := range o.Tiles {
			if t == p {
				return true
			}
		}
	}
	return false
}

// addFibre writes a floor row with the cable through it, across the tile
// or down its middle, in the holder's colour (pale when nobody holds it)
// on the floor's own background.
func (sc *Scene) addFibre(r *runBuilder, fk styleKey, st *Styles, text string, f fibreSeg, across bool) {
	rs := []rune(text)
	cw, mid := len(rs), len(rs)/2
	key := (fk &^ baseMask) | sFog
	switch f.holder {
	case 0:
		key = (fk &^ baseMask) | sTeamA
	case 1:
		key = (fk &^ baseMask) | sTeamB
	}
	if across {
		cable := strings.Repeat("─", cw)
		if f.v {
			cable = strings.Repeat("─", mid) + "┼" + strings.Repeat("─", cw-mid-1)
		}
		r.add(key, sc.wrapper(key, st), cable)
		return
	}
	r.add(fk, sc.wrapper(fk, st), string(rs[:mid]))
	r.add(key, sc.wrapper(key, st), "│")
	r.add(fk, sc.wrapper(fk, st), string(rs[mid+1:]))
}
