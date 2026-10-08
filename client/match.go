package client

import (
	"fmt"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"rfog/engine"
	"rfog/proto"
	"rfog/render"
	"rfog/render/art"
)

// matchScreen is the board: orders, resolution playback and the end card.
// It only ever sees fog-filtered views from the localMatch (or, later, the
// server), so the same screen serves offline, hotseat, online and replays.
//
// Phases: pass (hotseat hand-over) -> orders -> anim -> orders ... -> end.
type matchScreen struct {
	src    matchSource
	player int // viewing player; -1 = spectator (replays)
	phase  string

	vs    engine.State
	scene render.Scene
	vp    render.Viewport
	cur   engine.Pos
	sel   int // selected unit id, 0 = none

	mode    string // "" | move | attack | ability | cmd
	ability string
	reach   map[engine.Pos][]engine.Pos
	targets map[engine.Pos]int // legal targets: pos -> unit id (0 for tile targets)
	orders  []engine.Order
	msg     string
	cmdline string
	help    bool
	info    bool
	// logOpen shows the match log (and chat) on its own screen; logBack
	// is how many lines it is scrolled back from the newest.
	logOpen bool
	logBack int
	// compactKeys is set by the view: the key bar is shorter when stacked.
	compactKeys bool
	// boardX0/boardY0 are where the board's top-left tile was drawn in the
	// last frame, and cellW/cellH how big a tile is; a pointer event maps
	// back through them.
	boardX0, boardY0 int
	cellW, cellH     int
	panelX0, panelY0 int // where the stacked stats begin in their panel (tap targets)
	// orient is how the board is turned on screen (see render.Orient):
	// chosen with the layout, per viewer, so each player's side is
	// nearest them. m.vp is in screen tiles.
	orient render.Orient
	// quitArmed is set by a first Q; a second Q leaves, any other key
	// stays. quitLeave is whether leaving also tells the source (forfeit).
	quitArmed, quitLeave bool
	// popup is which level of the unit's commands the action area shows:
	// "" (Move, Act, Hold, Status) or "act" (attack and abilities).
	popup   string
	actions []MenuItem // the action area's buttons as last drawn, by click id
	// drag is a unit being dragged (press on it, move, release): dropped
	// on a tile it moves there, on an enemy it attacks. dragged is set
	// once the pointer has left the unit's tile.
	drag      *engine.Unit
	dragged   bool
	dragWasUp bool // the pressed unit's moves were already showing
	// more swaps the turn row of the action area for the rarer commands.
	more bool
	// trail and scars linger from the last resolution into the next
	// planning phase (see render.Scene).
	trail, scars map[engine.Pos]bool
	// sideRows maps a line inside the detail panel to the unit it names,
	// so the order list can be tapped.
	sideRows map[int]int // compact layout: the sidebar shown on its own screen

	an        *anim
	pending   []int  // humans still to be shown the last resolution
	afterPass string // what follows the hand-over screen: "orders" | "anim"
	res       *turnResolvedMsg
	ended     bool
	final     *engine.State
	reason    string
	ratings   map[int]proto.RatingDelta
	nextStart *turnStartMsg // a TurnStart that arrived mid-animation
	deadline  time.Time
	ambient   int // ambient-effect tick while waiting for orders
	log       []string
	saved     string
	err       string
	coach     *coach              // guided first match, nil otherwise
	back      func(a *App) screen // where a replay opened from a list returns to
}

const sidebarW = 34

func newMatchScreen(a *App, src matchSource) *matchScreen {
	m := &matchScreen{src: src, player: -1}
	if src.Watching() {
		m.phase = "orders" // spectator: "orders" means "waiting for the next turn key"
		m.refresh(a)
		return m
	}
	m.player = src.Humans()[0]
	m.deadline = src.Deadline()
	if src.Hotseat() {
		m.phase, m.afterPass = "pass", "orders"
	} else {
		m.phase = "orders"
	}
	m.refresh(a)
	return m
}

// refresh reloads the viewer's fog view and rebuilds the scene. The cell
// size is chosen when drawing, so it is carried across rebuilds.
func (m *matchScreen) refresh(a *App) {
	m.vs = m.src.View(m.player)
	team := 0
	if m.player >= 0 {
		team = m.vs.Player(m.player).Team
	}
	m.scene = render.SceneFromView(&m.vs, team)
	m.scene.CellW, m.scene.CellH = m.cellW, m.cellH
	m.scene.Trail, m.scene.Scars = m.trail, m.scars
	m.scene.Orient = m.orient
	m.scene.TelegraphArea(a.c, &m.vs)
	if m.sel == 0 || m.vs.Unit(m.sel) == nil || !m.vs.Unit(m.sel).Alive() {
		m.sel = 0
		if m.player >= 0 {
			if u := m.vs.Unit(m.vs.Player(m.player).Commander); u != nil && u.Alive() {
				m.sel = u.ID
				m.cur = u.Pos
			}
		}
	}
	m.mode = ""
	m.reach, m.targets = nil, nil
	m.decorate(a)
}

// decorate applies selection state to the scene.
func (m *matchScreen) decorate(a *App) {
	m.scene.Cursor = &m.cur
	m.scene.Selected = m.sel
	m.scene.Highlight = map[engine.Pos]bool{}
	m.scene.Targets = map[engine.Pos]bool{}
	m.scene.Path = map[engine.Pos]bool{}
	m.scene.Ordered = map[int]bool{}
	m.scene.Planned = map[engine.Pos]render.SceneUnit{}
	// Every planned move shows where the unit will end up: a faded copy of
	// it on that square. Only the selected unit's path is drawn, so a
	// turn's worth of orders does not become a tangle of paths.
	for _, o := range m.orders {
		m.scene.Ordered[o.UnitID] = true
		if o.Action != engine.ActMove || len(o.Path) == 0 {
			continue
		}
		if u := m.vs.Unit(o.UnitID); u != nil {
			m.scene.Planned[o.Path[len(o.Path)-1]] = render.SceneUnit{ID: u.ID, Team: u.Team, Kind: u.Kind,
				Commander: u.IsCommander, Pos: o.Path[len(o.Path)-1], HP: u.HP, MaxHP: u.MaxHP}
		}
		if o.UnitID == m.sel {
			for _, p := range o.Path {
				m.scene.Path[p] = true
			}
		}
	}
	switch m.mode {
	case "move", "go":
		for p := range m.reach {
			m.scene.Highlight[p] = true
		}
		for p := range m.targets { // go: the enemies it can hit from here
			m.scene.Targets[p] = true
		}
		if path, ok := m.reach[m.cur]; ok {
			for _, p := range path {
				m.scene.Path[p] = true
			}
		}
	case "attack", "ability":
		for p := range m.targets {
			m.scene.Targets[p] = true
		}
		if m.mode == "ability" {
			if ab, ok := a.c.Abilities[m.ability]; ok && ab.Target == engine.TargetTile {
				if _, legal := m.targets[m.cur]; legal {
					for _, p := range m.scene.Board.Area(m.cur, ab.Area) {
						m.scene.Path[p] = true
					}
				}
			}
		}
	}
}

func (m *matchScreen) myUnits() []*engine.Unit {
	var out []*engine.Unit
	if m.player < 0 {
		return out
	}
	for i := range m.vs.Units {
		u := &m.vs.Units[i]
		if u.Owner == m.player && u.Alive() {
			out = append(out, u)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].IsCommander != out[j].IsCommander {
			return out[i].IsCommander
		}
		return out[i].ID < out[j].ID
	})
	return out
}

func (m *matchScreen) selected() *engine.Unit {
	if m.sel == 0 {
		return nil
	}
	u := m.vs.Unit(m.sel)
	if u == nil || !u.Alive() || u.Owner != m.player {
		return nil
	}
	return u
}

// ---- update -------------------------------------------------------------

func (m *matchScreen) update(a *App, msg tea.Msg) (screen, tea.Cmd) {
	switch t := msg.(type) {
	case tickMsg:
		// Spectators: play each frame as it arrives from the server.
		if sm, ok := m.src.(*spectateMatch); ok && m.phase != "anim" {
			if sm.pending != nil {
				return m.advanceReplay(a)
			}
			if m.phase != "end" {
				return m, tick(500)
			}
		}
		if m.phase == "anim" && m.an != nil {
			if m.an.done {
				return m.afterAnim(a)
			}
			if m.an.beat() {
				// Hold the final frame briefly, then continue on the next tick.
				return m, tick(maxInt(a.set.AnimMs*2, 250))
			}
			return m, tick(m.an.pace(a.set.AnimMs))
		}
		if m.phase == "orders" || m.phase == "wait" {
			if a.set.EffectsOn() {
				// Ambient effects are decoration: once a second is plenty,
				// and every tick is a full frame over the wire.
				m.ambient++
				return m, tick(1000)
			}
			if !m.deadline.IsZero() {
				return m, tick(1000)
			}
		}
		return m, nil
	case tea.KeyMsg:
		return m.key(a, t)
	case tea.MouseMsg:
		return m.mouse(a, t)
	case turnResolvedMsg:
		return m.onResolved(a, t)
	case turnStartMsg:
		if m.phase == "anim" || m.phase == "pass" {
			m.nextStart = &t
			return m, nil
		}
		return m.beginOrders(a, &t)
	case matchEndMsg:
		f := t.Final
		m.final, m.reason, m.ended, m.ratings = &f, t.Reason, true, t.Ratings
		if m.phase == "anim" || m.phase == "pass" {
			return m, nil
		}
		m.phase = "end"
		return m, nil
	case chatMsg:
		m.log = append(m.log, t.From+": "+t.Text)
		if !m.logOpen {
			m.msg = t.From + ": " + t.Text // the log may be off screen
		}
		return m, nil
	case errMsg:
		m.msg = t.Text
		return m, nil
	case netMsg:
		if t.Err != nil {
			m.msg = "disconnected: " + t.Err.Error()
			if a.displaced {
				m.msg = "continued on another device; open online to take it back here"
			}
			return m, nil
		}
		if t.T == proto.TSpecState {
			var ss proto.SpecState
			if err := t.F.As(&ss); err != nil {
				return m, nil
			}
			if _, ok := m.src.(*spectateMatch); ok && ss.Match == m.src.Setup().ID {
				return m, m.apply(a, ss)
			}
		}
		if t.T == proto.TError {
			var e proto.Error
			_ = t.F.As(&e)
			m.msg = e.Msg
		}
		return m, nil
	}
	return m, nil
}

// beginOrders enters the orders phase for the current player with a fresh view.
func (m *matchScreen) beginOrders(a *App, ts *turnStartMsg) (screen, tea.Cmd) {
	m.nextStart = nil
	if ts != nil {
		m.deadline = ts.Deadline
	} else {
		m.deadline = m.src.Deadline()
	}
	m.phase = "orders"
	m.sel = 0
	m.lingerFrom(m.res)
	m.refresh(a)
	if !m.deadline.IsZero() || a.set.EffectsOn() {
		return m, tick(1000)
	}
	return m, nil
}

// lingerFrom keeps what the last turn left on the board: the paths units
// walked and where units fell, as this player saw them.
func (m *matchScreen) lingerFrom(r *turnResolvedMsg) {
	m.trail, m.scars = nil, nil
	if r == nil {
		return
	}
	tv, ok := r.Views[m.player]
	if !ok {
		if tv, ok = r.Views[-1]; !ok {
			return
		}
	}
	m.trail, m.scars = map[engine.Pos]bool{}, map[engine.Pos]bool{}
	for _, e := range tv.Events {
		switch e.Kind {
		case engine.EvMoved:
			for _, p := range e.Path {
				m.trail[p] = true
			}
		case engine.EvDied:
			m.scars[e.From] = true
		}
	}
}

// onResolved plays the turn's resolution to every human in turn.
func (m *matchScreen) onResolved(a *App, r turnResolvedMsg) (screen, tea.Cmd) {
	m.res = &r
	m.ended = r.Ended
	m.deadline = time.Time{}
	if m.src.Watching() {
		m.pending = []int{-1}
	} else if _, whole := r.Views[-1]; whole && m.src.Hotseat() {
		m.pending = []int{-1} // one playback for both; the hand-over comes after
	} else {
		m.pending = append([]int{}, m.src.Humans()...)
		for i, p := range m.pending {
			if p == m.player && i != 0 {
				m.pending[0], m.pending[i] = m.pending[i], m.pending[0]
			}
		}
	}
	return m.nextAnim(a)
}

func (m *matchScreen) key(a *App, k tea.KeyMsg) (screen, tea.Cmd) {
	if m.quitArmed {
		m.quitArmed, m.msg = false, ""
		if isKey(k, "Q") {
			return m.quit(a, m.quitLeave)
		}
		return m, nil // any other key: stay
	}
	if m.help {
		m.help = false
		return m, nil
	}
	if m.logOpen {
		return m.logKey(a, k)
	}
	if m.info {
		if isKey(k, "i", "esc", "q", "enter", " ") {
			m.info = false
			return m, nil
		}
	}
	switch m.phase {
	case "pass":
		if isKey(k, "enter", " ") {
			if m.afterPass == "anim" {
				return m.startAnim(a)
			}
			m.phase = "orders"
			m.refresh(a)
		}
		if isKey(k, "Q") {
			return m.armQuit(a, false)
		}
		return m, nil
	case "anim":
		if m.an != nil && !m.an.done {
			if isKey(k, ".", " ", "enter") {
				m.an.skip()
			}
			return m, nil
		}
		return m.afterAnim(a)
	case "end":
		return m.endKey(a, k)
	case "wait":
		if isKey(k, "Q") {
			return m.armQuit(a, true)
		}
		if isKey(k, "?") {
			m.help = true
		}
		return m, nil
	}
	// orders phase
	if m.src.Watching() {
		switch {
		case isKey(k, "i"):
			if m.stacked(a) {
				m.info = !m.info
			}
		case isKey(k, "t"):
			if isRemote(m.src) || m.src.Watching() {
				m.mode, m.cmdline = "chat", ""
			}
		case isKey(k, "enter", " ", "n", "."):
			if _, live := m.src.(*spectateMatch); live {
				return m, nil // live spectating advances by itself
			}
			return m.advanceReplay(a)
		case isKey(k, "q", "esc", "Q"):
			return m.leaveReplay(a), nil
		case isKey(k, "?"):
			m.help = true
		default:
			m.moveCursor(a, k)
		}
		return m, nil
	}
	if m.mode == "cmd" || m.mode == "chat" {
		return m.cmdKey(a, k)
	}
	// The action area's open level: esc goes back from it; any other key
	// acts, and the area returns to the unit's first commands.
	if (m.popup != "" || m.more) && isKey(k, "esc") && m.mode == "" {
		m.popup, m.more = "", false
		return m, nil
	}
	m.popup, m.more = "", false
	switch {
	case isKey(k, "?"):
		m.help = true
	case isKey(k, "i"):
		m.info = !m.info
	case isKey(k, "esc"):
		if m.mode != "" {
			m.mode, m.reach, m.targets = "", nil, nil
			m.msg = ""
		}
	case isKey(k, "Q"):
		return m.armQuit(a, true)
	case isKey(k, ":"):
		m.mode, m.cmdline = "cmd", ""
	case isKey(k, "g"):
		m.logOpen, m.logBack = true, 0
	case isKey(k, "t"):
		if isRemote(m.src) || m.src.Watching() {
			m.mode, m.cmdline = "chat", ""
		}
	case isKey(k, "tab"):
		m.cycle(1)
	case isKey(k, "shift+tab"):
		m.cycle(-1)
	case isKey(k, "enter"):
		m.confirm(a)
	case isKey(k, "m"):
		m.startMove(a)
	case isKey(k, "a"):
		m.startAttack(a)
	case isKey(k, "q", "w", "e", "r", "R"):
		m.startAbility(a, strings.ToLower(k.String()))
	case isKey(k, "1", "2", "3", "4"):
		m.startUnitAbility(a, int(k.String()[0]-'1'))
	case isKey(k, "o"):
		m.simpleOrder(a, engine.ActOverwatch)
	case isKey(k, "x"):
		m.simpleOrder(a, engine.ActHold)
	case isKey(k, "d", "backspace"):
		m.clearOrders(a)
	case isKey(k, " ", "c"):
		return m.commit(a)
	default:
		m.moveCursor(a, k)
	}
	m.decorate(a)
	if m.coach != nil {
		m.coach.advance(m)
	}
	return m, nil
}

func (m *matchScreen) moveCursor(a *App, k tea.KeyMsg) {
	dx, dy := 0, 0
	switch k.String() {
	case "h", "left":
		dx = -1
	case "l", "right":
		dx = 1
	case "k", "up":
		dy = -1
	case "j", "down":
		dy = 1
	case "y":
		dx, dy = -1, -1
	case "u":
		dx, dy = 1, -1
	case "b":
		dx, dy = -1, 1
	case "n":
		dx, dy = 1, 1
	case "H":
		dx = -5
	case "L":
		dx = 5
	case "K":
		dy = -5
	case "J":
		dy = 5
	default:
		return
	}
	// The keys move the way the screen shows the board.
	dx, dy = m.orient.Step(dx, dy)
	p := engine.Pos{X: m.cur.X + dx, Y: m.cur.Y + dy}
	if p.X < 0 {
		p.X = 0
	}
	if p.Y < 0 {
		p.Y = 0
	}
	if p.X >= m.vs.Board.W {
		p.X = m.vs.Board.W - 1
	}
	if p.Y >= m.vs.Board.H {
		p.Y = m.vs.Board.H - 1
	}
	m.cur = p
	m.decorate(a)
}

func (m *matchScreen) cycle(dir int) {
	us := m.myUnits()
	if len(us) == 0 {
		return
	}
	idx := -1
	for i, u := range us {
		if u.ID == m.sel {
			idx = i
		}
	}
	idx = (idx + dir + len(us)) % len(us)
	if idx < 0 {
		idx = 0
	}
	m.sel = us[idx].ID
	m.cur = us[idx].Pos
	m.mode, m.reach, m.targets = "", nil, nil
}

// confirm handles enter: select a unit, or confirm the current mode's target.
func (m *matchScreen) confirm(a *App) {
	switch m.mode {
	case "":
		if u := m.vs.UnitAt(m.cur); u != nil && u.Owner == m.player {
			m.sel = u.ID
			m.msg = ""
		}
	case "go":
		// Touch: the unit stays where the player's attention is. A
		// commander with an order left stays picked up (move, then hit
		// something from there); anything else is put down, like a chess
		// piece after its move. No jumping to the next unit.
		sel, cur := m.sel, m.cur
		n := len(m.orders)
		if id, ok := m.targets[m.cur]; ok && id != 0 {
			m.addOrder(a, engine.Order{UnitID: m.sel, Action: engine.ActAttack, TargetU: id, Target: m.cur})
		} else if path, ok := m.reach[m.cur]; ok && len(path) > 0 {
			m.addOrder(a, engine.Order{UnitID: m.sel, Action: engine.ActMove, Path: path})
		} else {
			m.msg = "not reachable"
			return
		}
		if len(m.orders) < n+1 && !m.hasOrder(sel) {
			return // refused: the message says why
		}
		m.sel, m.cur = sel, cur
		if u := m.vs.Unit(sel); u != nil && u.IsCommander && m.ordersFor(sel) < a.c.Rules.CommanderOrders {
			m.startGo(a)
		}
	case "move":
		path, ok := m.reach[m.cur]
		if !ok || len(path) == 0 {
			m.msg = "not reachable"
			return
		}
		m.addOrder(a, engine.Order{UnitID: m.sel, Action: engine.ActMove, Path: path})
	case "attack":
		id, ok := m.targets[m.cur]
		if !ok || id == 0 {
			m.msg = "no target there"
			return
		}
		m.addOrder(a, engine.Order{UnitID: m.sel, Action: engine.ActAttack, TargetU: id, Target: m.cur})
	case "ability":
		id, ok := m.targets[m.cur]
		if !ok {
			m.msg = "not a legal target"
			return
		}
		m.addOrder(a, engine.Order{UnitID: m.sel, Action: engine.ActAbility, Ability: m.ability, TargetU: id, Target: m.cur})
	}
}

// startGo shows everything the selected unit can do on the board at once,
// the way a chess board shows a picked-up piece's squares: the tiles it
// can reach and the enemies it can hit from where it stands. A tap on
// either gives the order.
func (m *matchScreen) startGo(a *App) {
	u := m.selected()
	if u == nil || u.Owner != m.player || !u.Alive() {
		return
	}
	// A unit that already has a move planned is shown from where it will
	// be: it cannot move again, and it attacks from there.
	from, moved := u.Pos, false
	for _, o := range m.orders {
		if o.UnitID == u.ID && o.Action == engine.ActMove && len(o.Path) > 0 {
			from, moved = o.Path[len(o.Path)-1], true
		}
	}
	m.reach = map[engine.Pos][]engine.Pos{}
	if !moved {
		m.reach = engine.Reachable(a.c, &m.vs, u)
		delete(m.reach, u.Pos)
	}
	m.targets = map[engine.Pos]int{}
	for i := range m.vs.Units {
		t := &m.vs.Units[i]
		if t.Team != u.Team && t.Alive() && engine.CanAttack(a.c, &m.vs, u, from, t) {
			m.targets[t.Pos] = t.ID
		}
	}
	if len(m.reach) == 0 && len(m.targets) == 0 {
		m.mode = ""
		if !moved {
			m.msg = u.Name + " cannot move or attack"
		}
		return
	}
	m.mode, m.msg = "go", ""
}

// ordersFor counts the unit's planned orders.
func (m *matchScreen) ordersFor(id int) int {
	n := 0
	for _, o := range m.orders {
		if o.UnitID == id {
			n++
		}
	}
	return n
}

func (m *matchScreen) startMove(a *App) {
	u := m.selected()
	if u == nil {
		m.msg = "select one of your units first (tab)"
		return
	}
	m.reach = engine.Reachable(a.c, &m.vs, u)
	delete(m.reach, u.Pos)
	if len(m.reach) == 0 {
		m.msg = "cannot move"
		return
	}
	m.mode, m.targets = "move", nil
	m.msg = "move: cursor to a tile, enter to confirm"
}

func (m *matchScreen) startAttack(a *App) {
	u := m.selected()
	if u == nil {
		m.msg = "select one of your units first (tab)"
		return
	}
	m.targets = map[engine.Pos]int{}
	for i := range m.vs.Units {
		t := &m.vs.Units[i]
		if t.Team == u.Team || !t.Alive() {
			continue
		}
		if engine.CanAttack(a.c, &m.vs, u, u.Pos, t) {
			m.targets[t.Pos] = t.ID
		}
	}
	if len(m.targets) == 0 {
		m.msg = "no target in range"
		return
	}
	m.mode, m.reach = "attack", nil
	m.msg = "attack: cursor to a target, enter to confirm"
	m.jumpToTarget()
}

func (m *matchScreen) startAbility(a *App, key string) {
	u := m.selected()
	if u == nil {
		m.msg = "select one of your units first (tab)"
		return
	}
	if !u.IsCommander {
		m.msg = "q/w/e/r are commander abilities; unit abilities are 1-4"
		return
	}
	id := a.c.Heroes[u.Kind].Abilities[key]
	m.beginAbility(a, u, id)
}

func (m *matchScreen) startUnitAbility(a *App, idx int) {
	u := m.selected()
	if u == nil {
		return
	}
	abs := engine.UnitAbilities(a.c, u)
	if u.IsCommander || idx >= len(abs) {
		m.msg = "no such ability"
		return
	}
	m.beginAbility(a, u, abs[idx])
}

func (m *matchScreen) beginAbility(a *App, u *engine.Unit, id string) {
	ab, ok := a.c.Abilities[id]
	if !ok {
		m.msg = "no such ability"
		return
	}
	if !contains(engine.UnitAbilities(a.c, u), id) {
		m.msg = ab.Name + ": locked (level " + fmt.Sprint(a.c.Rules.Unlock[ab.Key]) + ")"
		return
	}
	if cd := u.Cooldowns[id]; cd > 0 {
		m.msg = fmt.Sprintf("%s: cooldown %d", ab.Name, cd)
		return
	}
	switch ab.Target {
	case engine.TargetSelf, engine.TargetAll:
		m.addOrder(a, engine.Order{UnitID: u.ID, Action: engine.ActAbility, Ability: id, TargetU: u.ID, Target: u.Pos})
		return
	}
	vis := map[engine.Pos]bool{}
	for _, p := range m.vs.Visible {
		vis[p] = true
	}
	m.targets = map[engine.Pos]int{}
	switch ab.Target {
	case engine.TargetUnit:
		for i := range m.vs.Units {
			t := &m.vs.Units[i]
			if !t.Alive() || engine.Dist(u.Pos, t.Pos) > ab.Range {
				continue
			}
			if ab.Kind != "" && t.Kind != ab.Kind {
				continue
			}
			ok := true
			switch ab.Filter {
			case engine.FilterEnemies:
				ok = t.Team != u.Team
			case engine.FilterAllies:
				ok = t.Team == u.Team
			case engine.FilterSelf:
				ok = t.ID == u.ID
			}
			if ok {
				m.targets[t.Pos] = t.ID
			}
		}
	case engine.TargetTile:
		for _, p := range m.vs.Board.Area(u.Pos, ab.Range) {
			if ab.NeedsVisible() && !vis[p] {
				continue
			}
			o := engine.Order{UnitID: u.ID, Action: engine.ActAbility, Ability: id, Target: p}
			if errs := engine.Validate(a.c, &m.vs, m.player, []engine.Order{o}); len(errs) == 0 {
				m.targets[p] = 0
			}
		}
	}
	if len(m.targets) == 0 {
		m.msg = ab.Name + ": no legal target"
		return
	}
	m.mode, m.ability, m.reach = "ability", id, nil
	m.msg = ab.Name + ": cursor to a target, enter to confirm"
	if ab.Target == engine.TargetUnit {
		m.jumpToTarget()
	}
}

// jumpToTarget moves the cursor onto the nearest legal target.
func (m *matchScreen) jumpToTarget() {
	if _, ok := m.targets[m.cur]; ok {
		return
	}
	best, bd := m.cur, 1<<30
	for p := range m.targets {
		if d := engine.Dist(m.cur, p); d < bd {
			best, bd = p, d
		}
	}
	m.cur = best
}

func (m *matchScreen) simpleOrder(a *App, action string) {
	u := m.selected()
	if u == nil {
		m.msg = "select one of your units first (tab)"
		return
	}
	m.addOrder(a, engine.Order{UnitID: u.ID, Action: action})
}

// addOrder appends an order if the whole set stays legal.
func (m *matchScreen) addOrder(a *App, o engine.Order) {
	u := m.vs.Unit(o.UnitID)
	limit := 1
	if u != nil && u.IsCommander {
		limit = a.c.Rules.CommanderOrders
	}
	var kept []engine.Order
	n := 0
	for _, x := range m.orders {
		if x.UnitID == o.UnitID {
			n++
		}
		kept = append(kept, x)
	}
	if n >= limit {
		// Replace the oldest order for this unit.
		for i, x := range kept {
			if x.UnitID == o.UnitID {
				kept = append(kept[:i], kept[i+1:]...)
				break
			}
		}
	}
	kept = append(kept, o)
	if errs := engine.Validate(a.c, &m.vs, m.player, kept); len(errs) > 0 {
		m.msg = strings.TrimPrefix(errs[len(errs)-1].Error(), "unit ")
		return
	}
	m.orders = kept
	m.mode, m.reach, m.targets = "", nil, nil
	m.msg = ""
	// Advance to the next unit without orders, if any.
	for _, nu := range m.myUnits() {
		if !m.hasOrder(nu.ID) {
			m.sel, m.cur = nu.ID, nu.Pos
			break
		}
	}
}

func (m *matchScreen) hasOrder(id int) bool {
	for _, o := range m.orders {
		if o.UnitID == id {
			return true
		}
	}
	return false
}

func (m *matchScreen) clearOrders(a *App) {
	if m.sel == 0 {
		m.orders = nil
		return
	}
	var kept []engine.Order
	for _, o := range m.orders {
		if o.UnitID != m.sel {
			kept = append(kept, o)
		}
	}
	m.orders = kept
	m.mode, m.reach, m.targets = "", nil, nil
}

// ---- command line -------------------------------------------------------

// logKey drives the log screen: scroll, chat, back.
func (m *matchScreen) logKey(a *App, k tea.KeyMsg) (screen, tea.Cmd) {
	if m.mode == "chat" {
		return m.cmdKey(a, k)
	}
	switch {
	case isKey(k, "g", "esc", "q"):
		m.logOpen = false
	case isKey(k, "t"):
		if isRemote(m.src) || m.src.Watching() {
			m.mode, m.cmdline = "chat", ""
		}
	case isKey(k, "k", "up"):
		m.logBack++
	case isKey(k, "j", "down"):
		if m.logBack > 0 {
			m.logBack--
		}
	case isKey(k, "?"):
		m.help = true
	}
	return m, nil
}

// logView is the match log on a screen of its own: every event and chat
// line, newest at the bottom, scrolled back logBack lines.
func (m *matchScreen) logView(a *App) string {
	st := &a.st
	lines := m.log
	if m.phase == "anim" && m.an != nil {
		lines = append(append([]string{}, m.log...), m.an.log...)
	}
	room := a.h - 3
	if room < 1 {
		room = 1
	}
	end := len(lines) - m.logBack
	if end < room && len(lines) >= room {
		end = room
		m.logBack = len(lines) - end
	}
	if end > len(lines) {
		end = len(lines)
	}
	start := end - room
	if start < 0 {
		start = 0
	}
	out := []string{st.Title.Render(fit(" log", a.w))}
	for _, l := range lines[start:end] {
		out = append(out, st.Dim.Render(fit(" > "+l, a.w)))
	}
	if items := m.actionItems(a); len(items) > 0 && len(out)+len(items)*3 <= a.h-2 {
		// Room for the buttons: they replace the oldest lines shown.
		for len(out) > a.h-2-len(items)*3 {
			out = append(out[:1], out[2:]...)
		}
		out = append(out, m.actionArea(a, a.w, len(out), 3)...)
	}
	for len(out) < a.h-2 {
		out = append(out, "")
	}
	bar := " g back  j/k scroll"
	if isRemote(m.src) || m.src.Watching() {
		bar += "  t chat"
	}
	if m.mode == "chat" {
		bar = " say: " + m.cmdline + "_"
	}
	out = append(out, st.Warn.Render(fit(" "+m.msg, a.w)), st.Dim.Render(fit(bar, a.w)))
	return strings.Join(out[:minInt(len(out), a.h)], "\n")
}

func (m *matchScreen) cmdKey(a *App, k tea.KeyMsg) (screen, tea.Cmd) {
	switch {
	case isKey(k, "esc"):
		m.mode, m.cmdline = "", ""
	case isKey(k, "enter"):
		line := m.cmdline
		chat := m.mode == "chat"
		m.mode, m.cmdline = "", ""
		if chat {
			m.say(a, line)
			return m, nil
		}
		if strings.TrimSpace(line) == "commit" {
			return m.commit(a)
		}
		m.runCommand(a, line)
	case isKey(k, "backspace"):
		if len(m.cmdline) > 0 {
			m.cmdline = m.cmdline[:len(m.cmdline)-1]
		}
	case isKey(k, " "):
		m.cmdline += " "
	default:
		if len(k.Runes) > 0 {
			m.cmdline += string(k.Runes)
		}
	}
	m.decorate(a)
	return m, nil
}

// say sends a chat line to the match (online and spectating only).
func (m *matchScreen) say(a *App, text string) {
	text = strings.TrimSpace(text)
	if text == "" || a.net == nil {
		return
	}
	if err := a.net.send(proto.TChat, proto.Chat{Match: m.src.Setup().ID, Text: text}); err != nil {
		m.msg = err.Error()
	}
}

// runCommand parses terse orders:
//
//	m <unit> <tile>       move           (:m l d7)
//	a <unit> <tile>       attack unit at tile
//	c <unit> <q|w|e|r|1..4> [tile]  cast
//	o <unit>  x <unit>    overwatch / hold
//	d [unit]              drop orders
//	resign                forfeit (the only way out of a daily match)
//
// <unit> is a unit letter (L R U M, or the commander's first letter), a
// unit id, or "c" for the commander.
func (m *matchScreen) runCommand(a *App, line string) {
	f := strings.Fields(strings.ToLower(line))
	if len(f) == 0 {
		return
	}
	if f[0] == "resign" {
		m.src.Leave()
		m.msg = "resigned"
		return
	}
	if f[0] == "d" {
		if len(f) > 1 {
			if u := m.findUnit(f[1]); u != nil {
				m.sel = u.ID
			}
		} else {
			m.sel = 0
		}
		m.clearOrders(a)
		return
	}
	if len(f) < 2 {
		m.msg = "usage: m|a|c|o|x <unit> [tile]"
		return
	}
	u := m.findUnit(f[1])
	if u == nil {
		m.msg = "no unit " + f[1]
		return
	}
	m.sel, m.cur = u.ID, u.Pos
	tile := func(i int) (engine.Pos, bool) {
		if len(f) <= i {
			return engine.Pos{}, false
		}
		return render.ParsePos(f[i])
	}
	switch f[0] {
	case "m":
		p, ok := tile(2)
		if !ok {
			m.msg = "move needs a tile"
			return
		}
		m.startMove(a)
		if m.mode == "move" {
			m.cur = p
			m.confirm(a)
		}
	case "a":
		p, ok := tile(2)
		if !ok {
			m.msg = "attack needs a tile"
			return
		}
		m.startAttack(a)
		if m.mode == "attack" {
			m.cur = p
			m.confirm(a)
		}
	case "c":
		if len(f) < 3 {
			m.msg = "cast needs an ability key"
			return
		}
		if f[2] >= "1" && f[2] <= "4" {
			m.startUnitAbility(a, int(f[2][0]-'1'))
		} else {
			m.startAbility(a, f[2])
		}
		if m.mode == "ability" {
			p, ok := tile(3)
			if !ok {
				m.msg = "ability needs a tile"
				m.mode = ""
				return
			}
			m.cur = p
			m.confirm(a)
		}
	case "o":
		m.simpleOrder(a, engine.ActOverwatch)
	case "x":
		m.simpleOrder(a, engine.ActHold)
	default:
		m.msg = "unknown command " + f[0]
	}
}

func (m *matchScreen) findUnit(tok string) *engine.Unit {
	us := m.myUnits()
	if tok == "c" {
		for _, u := range us {
			if u.IsCommander {
				return u
			}
		}
	}
	var n int
	if _, err := fmt.Sscanf(tok, "%d", &n); err == nil {
		for _, u := range us {
			if u.ID == n {
				return u
			}
		}
	}
	for _, u := range us {
		if strings.ToLower(render.UnitGlyph(u.Kind, u.IsCommander)) == tok && !m.hasOrder(u.ID) {
			return u
		}
	}
	for _, u := range us {
		if strings.ToLower(render.UnitGlyph(u.Kind, u.IsCommander)) == tok {
			return u
		}
	}
	return nil
}

// ---- commit / resolve / animation --------------------------------------

func (m *matchScreen) commit(a *App) (screen, tea.Cmd) {
	if m.player < 0 {
		return m.advanceReplay(a)
	}
	cmd := m.src.Commit(m.player, m.orders)
	m.orders = nil
	if cmd != nil {
		return m, cmd
	}
	if m.src.Hotseat() {
		for _, p := range m.src.Humans() {
			if !m.src.Committed(p) {
				m.player = p
				m.sel = 0
				m.phase, m.afterPass = "pass", "orders"
				m.refresh(a)
				return m, nil
			}
		}
	}
	// Remote: the server resolves when everyone is in or the clock runs out.
	m.phase = "wait"
	m.msg = ""
	if !m.deadline.IsZero() {
		return m, tick(1000)
	}
	return m, nil
}

func (m *matchScreen) advanceReplay(a *App) (screen, tea.Cmd) {
	r := m.src.Advance()
	if r == nil {
		m.final = m.src.Final()
		m.ended = true
		m.phase = "end"
		m.refresh(a)
		return m, nil
	}
	return m.onResolved(a, *r)
}

func (m *matchScreen) nextAnim(a *App) (screen, tea.Cmd) {
	if len(m.pending) == 0 {
		if m.ended {
			if m.final == nil {
				m.final = m.src.Final()
			}
			if m.final == nil {
				m.phase = "wait" // remote: MatchEnd is on its way
				return m, nil
			}
			m.phase = "end"
			m.refresh(a)
			return m, nil
		}
		m.sel = 0
		if m.src.Hotseat() {
			if first := m.src.Humans()[0]; first != m.player {
				m.player = first
				m.phase, m.afterPass = "pass", "orders"
				m.refresh(a)
				return m, nil
			}
		}
		if m.nextStart != nil {
			return m.beginOrders(a, m.nextStart)
		}
		if !isRemote(m.src) {
			return m.beginOrders(a, nil)
		}
		m.phase = "wait" // the server's TurnStart is on its way
		return m, nil
	}
	p := m.pending[0]
	m.pending = m.pending[1:]
	if p != m.player && p >= 0 {
		// Hotseat: hand over before revealing this player's view of the turn.
		m.player = p
		m.sel = 0
		m.phase, m.afterPass = "pass", "anim"
		m.an = nil
		m.refresh(a)
		return m, nil
	}
	m.player = p
	return m.startAnim(a)
}

// leaveReplay returns to wherever the replay (or live watch) was opened
// from, telling the server to stop sending frames.
func (m *matchScreen) leaveReplay(a *App) screen {
	if sm, ok := m.src.(*spectateMatch); ok {
		sm.Leave()
		a.spec = nil
	}
	if m.back != nil {
		return m.back(a)
	}
	if a.ephemeral {
		return newMenuScreen()
	}
	return newReplayBrowser(a)
}

// bottomPanels is the stacked layout's lower band: the portrait of the
// unit under the cursor on the left, its stats and the orders on the
// right, both framed. The portrait takes the largest size the band can
// hold while leaving the stats a usable width.
func (m *matchScreen) bottomPanels(a *App, h, width int) []string {
	st, g := &a.st, a.g
	inner := h - 2
	if inner < 1 {
		inner = 1
	}
	var sz portraitSize
	for _, c := range []portraitSize{sizeFull, sizeMedium, sizeSmall} {
		if inner >= c.rows && width-(c.cols+2) >= 30 {
			sz = c
			break
		}
	}
	if sz.cols == 0 && width-(sizeSmall.cols+2) >= 18 && inner >= sizeSmall.rows {
		sz = sizeSmall
	}
	pw := 0
	if sz.cols > 0 {
		pw = sz.cols + 2
	}
	m.panelX0 = pw + 1
	sw := width - pw

	var leftPanel []string
	if pw > 0 {
		f, name := m.portraitFrame(a, sz)
		lines := f.Render(a.artStyler())
		// Name under the portrait when there is a row for it.
		if len(lines) < inner {
			lines = append(lines, fit(st.Dim.Render(centreIn(strings.ToLower(name), pw-2)), pw-2))
		}
		// Centre the portrait in whatever height the band has.
		if pad := (inner - len(lines)) / 2; pad > 0 {
			lines = append(make([]string, pad), lines...)
		}
		for len(lines) < inner {
			lines = append(lines, "")
		}
		leftPanel = render.Frame(lines[:inner], pw-2, "", st, g)
	}

	var right []string
	m.panelY0 = 0
	if m.coach != nil {
		// The tutorial's coaching comes first: on a phone it is the
		// only place the lesson can go.
		right = append(m.coach.panel(a, sw-2), "")
		m.panelY0 = len(right)
	}
	right = append(right, m.statsLines(a, sw-2, inner-len(right))...)
	for len(right) < inner {
		right = append(right, "")
	}
	rightPanel := render.Frame(right[:inner], sw-2, "", st, g)

	out := make([]string, 0, h)
	for i := 0; i < h; i++ {
		l, r := "", ""
		if i < len(leftPanel) {
			l = leftPanel[i]
		}
		if i < len(rightPanel) {
			r = rightPanel[i]
		}
		out = append(out, fit(fit(l, pw)+r, width))
	}
	return out
}

// portraitUnit is who the portrait shows: the unit under the cursor, or
// nobody (static) when the cursor is on an empty or fogged tile. The
// stats beside it keep showing the selected unit, the one taking orders.
func (m *matchScreen) portraitUnit() *engine.Unit {
	if u := m.vs.UnitAt(m.cur); u != nil && u.Alive() {
		return u
	}
	return nil
}

// portraitFrame is the portrait at a size, and the name to print under it.
func (m *matchScreen) portraitFrame(a *App, sz portraitSize) (art.Frame, string) {
	u := m.portraitUnit()
	if u == nil {
		return a.staticFrame(sz, uint64(m.ambient)), "no signal"
	}
	return a.unitPortrait(u, sz), u.Name
}

// layoutFor chooses how the board is laid out for the biggest tiles: over
// the panels (a phone in portrait, a tall window) or beside the sidebar
// (a wide one), and turned a quarter or not, so a long board runs the
// long way of the screen. Detail lives in the size of a square, not the
// number of squares. On a tie the sidebar wins (a wide screen), then a
// turned board over the panels (a tall one: your side at the bottom, as
// a chess board sits in front of you).
func (m *matchScreen) layoutFor(a *App) (turned, stacked bool) {
	if m.scene.Board == nil {
		return false, true
	}
	bw, bh := m.scene.Board.W, m.scene.Board.H
	best := -1
	for _, t := range []bool{false, true} {
		for _, st := range []bool{false, true} {
			if !st && a.w < wideEnough {
				continue // no room for a sidebar
			}
			dw, dh := bw, bh
			if t {
				dw, dh = bh, bw
			}
			aw, ah := m.boardRoom(a, st, dh)
			cw, ch := render.FitCell(dw, dh, aw*2, ah)
			if cw*dw > aw*2 || ch*dh > ah {
				cw, ch = 0, 0 // scrolls: only better than nothing
			}
			pref := 0 // among equal tiles: sidebar, then turned and stacked
			switch {
			case !st && !t:
				pref = 3
			case st && t:
				pref = 2
			case st:
				pref = 1
			}
			if score := cw*ch*10 + pref; score > best {
				best, turned, stacked = score, t, st
			}
		}
	}
	return turned, stacked
}

// boardRoom is the space the board may take, in two-column tiles and
// rows: beside the sidebar, or over the panels and the action area. dh is
// the board's height on screen in tiles: the buttons' rows are kept only
// if the whole board still fits without them, since a board that scrolls
// is worse than buttons the keys can stand in for.
func (m *matchScreen) boardRoom(a *App, stacked bool, dh int) (int, int) {
	w, h := (a.w-sidebarW-6)/2, a.h-8
	if stacked {
		// Reserve the panel's and the buttons' rows before the board
		// takes the rest: seeing who you command, and something to press,
		// matter more than the last row of a map you can scroll.
		h = a.h - 9 - art.SmallRows - 6
		if h < dh {
			h = a.h - 9 - art.SmallRows // a small screen: keys instead of buttons
		}
		if h < 8 {
			h = a.h - 9
		}
		w = (a.w - 5) / 2
	}
	return maxInt(w, 4), maxInt(h, 4)
}

// stacked reports whether the board sits over the panels.
func (m *matchScreen) stacked(a *App) bool {
	_, st := m.layoutFor(a)
	return st
}

// screenBoard is the board's size on screen, as a board for viewports.
func (m *matchScreen) screenBoard() *engine.Board {
	w, h := m.orient.Dims(m.vs.Board.W, m.vs.Board.H)
	return &engine.Board{W: w, H: h}
}

// statsLines is the left panel: the unit, its numbers, its abilities and
// the orders, in that order of importance, cut to the rows available.
func (m *matchScreen) statsLines(a *App, w, h int) []string {
	st, g := &a.st, a.g
	m.sideRows = map[int]int{}
	var l []string
	add := func(s string) { l = append(l, fit(s, w)) }
	u := m.vs.Unit(m.sel)
	if u == nil {
		u = m.vs.UnitAt(m.cur)
	}
	if u == nil || !u.Alive() {
		add(st.Dim.Render("no unit here"))
		add(st.Dim.Render(render.PosLabel(m.cur) + "  " + m.vs.Board.At(m.cur).Terrain))
		return l
	}
	team := st.TeamA
	if u.Team != m.scene.ViewTeam {
		team = st.TeamB
	}
	name := u.Name
	if u.IsCommander {
		name = g.Commander + " " + u.Name + fmt.Sprintf(" L%d", u.Level)
	}
	// A short panel compresses the header to two lines so the order list
	// still fits; a taller one spreads out.
	tight := h < 11
	if tight {
		add(team.Render(name) + st.Dim.Render(" "+render.PosLabel(u.Pos)) +
			fmt.Sprintf("  HP %s %d/%d", hpBar(a, u.HP, u.MaxHP, minInt(6, w-24)), u.HP, u.MaxHP))
		add(st.Dim.Render(fmt.Sprintf("MV%d JMP%d RNG%d ATK%d DEF%d INI%d",
			u.Eff("mv"), u.JMP, u.Eff("rng"), u.Eff("atk"), u.Eff("def"), u.Eff("ini"))))
	} else {
		add(team.Render(name) + st.Dim.Render("  "+render.PosLabel(u.Pos)))
		add(fmt.Sprintf("HP %s %d/%d", hpBar(a, u.HP, u.MaxHP, minInt(10, w-12)), u.HP, u.MaxHP))
		add(st.Dim.Render(fmt.Sprintf("MV %d JMP %d RNG %d", u.Eff("mv"), u.JMP, u.Eff("rng"))))
		add(st.Dim.Render(fmt.Sprintf("ATK %d DEF %d INI %d", u.Eff("atk"), u.Eff("def"), u.Eff("ini"))))
	}
	// Orders first: they are what the player acts on, and tapping one
	// selects that unit. The abilities are on the button bar anyway.
	if len(l)+2 <= h && m.player >= 0 && m.phase == "orders" {
		if !tight {
			add("")
		}
		add(st.Title.Render("ORDERS"))
		for _, mu := range m.myUnits() {
			if len(l) >= h {
				break
			}
			m.sideRows[len(l)] = mu.ID
			gl := render.UnitGlyph(mu.Kind, mu.IsCommander)
			what := st.Dim.Render("—")
			for _, o := range m.orders {
				if o.UnitID == mu.ID {
					what = o.Action
					if o.Action == engine.ActMove && len(o.Path) > 0 {
						what = "move " + render.PosLabel(o.Path[len(o.Path)-1])
					}
					break
				}
			}
			row := fmt.Sprintf("%s %s %s", st.Key.Render(gl), fit(truncate(mu.Name, 7), 7), what)
			if mu.ID == m.sel {
				row = st.Accent.Render("›") + row
			} else {
				row = " " + row
			}
			add(row)
		}
	}
	if len(l) < h && u.IsCommander {
		for _, key := range []string{"q", "w", "e", "r"} {
			if len(l) >= h {
				break
			}
			id := a.c.Heroes[u.Kind].Abilities[key]
			ab := a.c.Abilities[id]
			state := m.abilityState(a, u, key, id)
			add(st.Key.Render(strings.ToUpper(key)) + " " + fit(ab.Name, 9) + " " + state)
		}
	}
	return l
}

// abilityState is "rdy", "cdN" or the level it unlocks at, styled.
func (m *matchScreen) abilityState(a *App, u *engine.Unit, key, id string) string {
	st := &a.st
	if !contains(engine.UnitAbilities(a.c, u), id) {
		return st.Dim.Render(fmt.Sprintf("L%d", a.c.Rules.Unlock[key]))
	}
	if cd := u.Cooldowns[id]; cd > 0 {
		return st.Warn.Render(fmt.Sprintf("cd%d", cd))
	}
	return st.Good.Render("rdy")
}

// centreIn pads s to width w with the text centred.
func centreIn(s string, w int) string {
	if len(s) >= w {
		return s
	}
	left := (w - len(s)) / 2
	return strings.Repeat(" ", left) + s
}

// blockWidth is how wide the drawn layout actually is.
func (m *matchScreen) blockWidth(compact bool, innerW int) int {
	if compact {
		return innerW + 2
	}
	return innerW + 2 + 1 + sidebarW
}

// portraitCard draws the selected unit's commander beside its stats, for
// panels too short for the full portrait. It returns nothing when there
// is no room for even the card, or when the big portrait will be drawn.
func (m *matchScreen) portraitCard(a *App, w, h int) []string {
	st := &a.st
	u := m.vs.Unit(m.sel)
	if u == nil {
		u = m.vs.UnitAt(m.cur)
	}
	if u == nil || !u.Alive() {
		return nil
	}
	// Estimate the panel's content rather than measuring the previous
	// frame: stats, abilities, the order list and its header.
	content := 10 + len(m.myUnits())
	tall := content+art.PortraitRows+2 <= h && w >= art.PortraitCols
	if tall {
		return nil // the full portrait goes at the bottom instead
	}
	if w < art.SmallCols+17 || h < art.SmallRows {
		return nil
	}
	card := a.unitPortrait(u, sizeSmall).Render(a.artStyler())
	team := st.TeamA
	if u.Team != m.scene.ViewTeam {
		team = st.TeamB
	}
	name := u.Name
	if u.IsCommander {
		name += fmt.Sprintf(" L%d", u.Level)
	}
	right := []string{
		team.Render(name) + st.Dim.Render(" "+render.PosLabel(u.Pos)),
		fmt.Sprintf("HP %s %d/%d", hpBar(a, u.HP, u.MaxHP, 6), u.HP, u.MaxHP),
		st.Dim.Render(fmt.Sprintf("MV %d JMP %d RNG %d", u.Eff("mv"), u.JMP, u.Eff("rng"))),
		st.Dim.Render(fmt.Sprintf("ATK %d DEF %d INI %d", u.Eff("atk"), u.Eff("def"), u.Eff("ini"))),
	}
	var out []string
	for i := 0; i < len(card) || i < len(right); i++ {
		left, r := strings.Repeat(" ", art.SmallCols), ""
		if i < len(card) {
			left = card[i]
		}
		if i < len(right) {
			r = right[i]
		}
		out = append(out, fit(left+" "+r, w))
	}
	return out
}

// registerSideRows turns the panel's order lines into click targets.
// base is the screen row the panel starts on; the frame adds one line.
func (m *matchScreen) registerSideRows(a *App, base, x0 int) {
	for idx, id := range m.sideRows {
		a.clickBox(base+1+idx, x0, 1<<30, id)
	}
}

// inView reports whether a tile is inside the board's viewport.
func (m *matchScreen) inView(p engine.Pos) bool {
	x, y := m.orient.Screen(p, m.vs.Board.W, m.vs.Board.H)
	return x >= m.vp.OX && x < m.vp.OX+m.vp.W && y >= m.vp.OY && y < m.vp.OY+m.vp.H
}

// cellAt is where a tile is drawn: the inverse of tileAt.
func (m *matchScreen) cellAt(p engine.Pos) (int, int) {
	cw, ch := m.cellW, m.cellH
	if cw < 2 {
		cw = 2
	}
	if ch < 1 {
		ch = 1
	}
	x, y := m.orient.Screen(p, m.vs.Board.W, m.vs.Board.H)
	return m.boardX0 + (x-m.vp.OX)*cw, m.boardY0 + (y-m.vp.OY)*ch
}

// tileAt maps a screen cell to a board tile, if the pointer is on the
// board. Each tile is two columns wide.
func (m *matchScreen) tileAt(x, y int) (engine.Pos, bool) {
	cw, ch := m.cellW, m.cellH
	if cw < 2 {
		cw = 2
	}
	if ch < 1 {
		ch = 1
	}
	col := (x - m.boardX0) / cw
	row := (y - m.boardY0) / ch
	if x < m.boardX0 || col < 0 || col >= m.vp.W || row < 0 || row >= m.vp.H {
		return engine.Pos{}, false
	}
	p := m.orient.Board(m.vp.OX+col, m.vp.OY+row, m.vs.Board.W, m.vs.Board.H)
	if !m.vs.Board.In(p) {
		return engine.Pos{}, false
	}
	return p, true
}

// mouse handles a pointer event: the board takes taps (first tap moves
// the cursor, a second on the same tile confirms, exactly like the
// cursor keys and enter), the order list takes taps on a unit, and the
// wheel scrolls the viewport. Nothing here is required to play.
func (m *matchScreen) mouse(a *App, e tea.MouseMsg) (screen, tea.Cmd) {
	if tea.MouseEvent(e).IsWheel() {
		switch e.Button {
		case tea.MouseButtonWheelUp:
			m.vp.OY--
		case tea.MouseButtonWheelDown:
			m.vp.OY++
		case tea.MouseButtonWheelLeft:
			m.vp.OX--
		case tea.MouseButtonWheelRight:
			m.vp.OX++
		}
		m.vp = render.ClampViewport(m.vp, m.screenBoard(), nil)
		return m, nil
	}
	if m.drag != nil && (e.Action == tea.MouseActionMotion || e.Action == tea.MouseActionRelease) {
		return m.dragTo(a, e)
	}
	if e.Action != tea.MouseActionPress || e.Button != tea.MouseButtonLeft {
		return m, nil
	}
	// A button in the action area.
	if id, hit := a.clicked(e); hit && id < 0 {
		return m.actionPick(a, -id-1)
	}
	// Tapping a unit in the order list selects it.
	if id, hit := a.clicked(e); hit && id > 0 {
		if u := m.vs.Unit(id); u != nil {
			m.sel, m.cur = u.ID, u.Pos
			m.decorate(a)
		}
		return m, nil
	}
	p, ok := m.tileAt(e.X, e.Y)
	if !ok {
		return m, nil
	}
	switch m.phase {
	case "end", "pass":
		return m.key(a, tea.KeyMsg{Type: tea.KeyEnter})
	case "anim":
		return m, nil
	}
	if m.src.Watching() {
		m.cur = p
		m.decorate(a)
		return m, nil
	}
	m.popup, m.more = "", false
	// Pressing on one of your own units picks it up: drag it to a tile to
	// move, onto an enemy to attack, or let go where it is to see its
	// moves (a plain tap is exactly that last case; tapping it again puts
	// it down).
	if u := m.vs.UnitAt(p); u != nil && u.Owner == m.player && u.Alive() && m.phase == "orders" && (m.mode == "" || m.mode == "move" || m.mode == "go") {
		m.dragWasUp = m.mode == "go" && m.sel == u.ID
		m.mode, m.reach, m.targets = "", nil, nil
		m.cur, m.sel = p, u.ID
		m.drag, m.dragged = u, false
		m.decorate(a)
		return m, nil
	}
	if m.mode == "go" {
		// A lit tile or a marked enemy: that order. Anywhere else: put the
		// unit down.
		m.cur = p
		if _, ok := m.reach[p]; ok || m.targets[p] != 0 {
			m.confirm(a)
		} else {
			m.mode, m.reach, m.targets = "", nil, nil
		}
		m.decorate(a)
		if m.coach != nil {
			m.coach.advance(m)
		}
		return m, nil
	}
	if m.cur == p {
		m.confirm(a) // second tap on the same tile: confirm, like enter
	} else {
		m.cur = p
		if m.mode == "" {
			if u := m.vs.UnitAt(p); u != nil && u.Owner == m.player && u.Alive() {
				m.sel = u.ID
				m.startGo(a)
			}
		}
	}
	m.decorate(a)
	if m.coach != nil {
		m.coach.advance(m)
	}
	return m, nil
}

// dragTo follows a dragged unit and drops it.
func (m *matchScreen) dragTo(a *App, e tea.MouseMsg) (screen, tea.Cmd) {
	u := m.drag
	p, onBoard := m.tileAt(e.X, e.Y)
	if e.Action == tea.MouseActionMotion {
		if onBoard && p != u.Pos {
			if !m.dragged {
				m.dragged = true
				m.startMove(a) // the reachable tiles light up while it is held
			}
			m.cur = p
			m.decorate(a)
		}
		return m, nil
	}
	// Released.
	m.drag = nil
	if !m.dragged || !onBoard || p == u.Pos {
		// Let go where it was picked up: a tap. Show its moves, or put it
		// down if they were already showing.
		m.mode, m.reach, m.targets = "", nil, nil
		m.cur = u.Pos
		if !m.dragWasUp {
			m.startGo(a)
		}
		m.decorate(a)
		return m, nil
	}
	m.cur = p
	if t := m.vs.UnitAt(p); t != nil && t.Alive() && t.Team != u.Team {
		// Dropped on an enemy: attack it, if it can be hit from here.
		m.mode, m.reach = "", nil
		m.startAttack(a)
		if m.mode == "attack" && m.targets[p] != 0 {
			m.cur = p
			m.confirm(a)
		} else {
			m.mode, m.targets = "", nil
			m.msg = "out of range: move first, then attack"
		}
		m.decorate(a)
		return m, nil
	}
	if _, ok := m.reach[p]; ok && m.mode == "move" {
		m.confirm(a)
	} else {
		m.mode, m.reach = "", nil
		m.msg = "not reachable"
	}
	m.decorate(a)
	return m, nil
}

// quit leaves the match screen. Online, leaving forfeits — except async
// (daily) matches, which simply wait for the player to come back.
// armQuit asks before leaving: Q is one keypress (and one button on the
// phone keypad) away from a forfeit. The warning says what leaving costs.
func (m *matchScreen) armQuit(a *App, leave bool) (screen, tea.Cmd) {
	m.quitArmed, m.quitLeave = true, leave
	switch rm, remote := m.src.(*remoteMatch); {
	case remote && rm.async:
		m.msg = "Q again to go back to the menu (the daily match keeps going) · any other key stays"
	case remote && leave:
		m.msg = "Q again to FORFEIT and leave · any other key stays"
	default:
		m.msg = "Q again to abandon this match · any other key stays"
	}
	return m, nil
}

func (m *matchScreen) quit(a *App, leave bool) (screen, tea.Cmd) {
	if rm, ok := m.src.(*remoteMatch); ok {
		if !rm.async && leave {
			rm.Leave()
		}
		a.rm = nil
		return onlineBack(a), nil
	}
	if leave {
		m.src.Leave()
	}
	return newMenuScreen(), nil
}

func isRemote(src matchSource) bool {
	_, ok := src.(*remoteMatch)
	return ok
}

func (m *matchScreen) startAnim(a *App) (screen, tea.Cmd) {
	tv, ok := m.res.Views[m.player]
	if !ok {
		return m.nextAnim(a)
	}
	team := 0
	if m.player >= 0 {
		team = tv.Post.Player(m.player).Team
	}
	pre, post := tv.Pre, tv.Post
	m.an = newAnim(a.c, a.g, &pre, &post, team, tv.Events)
	m.phase = "anim"
	m.scene = m.an.scene
	m.scene.Cursor = nil
	return m, tick(m.an.pace(a.set.AnimMs))
}

func (m *matchScreen) afterAnim(a *App) (screen, tea.Cmd) {
	if m.an != nil {
		m.log = append(m.log, m.an.log...)
		if len(m.log) > 200 {
			m.log = m.log[len(m.log)-200:]
		}
	}
	m.an = nil
	return m.nextAnim(a)
}

// ---- end screen ---------------------------------------------------------

func (m *matchScreen) endKey(a *App, k tea.KeyMsg) (screen, tea.Cmd) {
	switch {
	case isKey(k, "s"):
		if m.src.Watching() {
			return m, nil
		}
		path, err := m.src.SaveReplay()
		if err != nil {
			m.err = err.Error()
		} else {
			m.saved = path
		}
	case isKey(k, "r"):
		if m.src.Watching() {
			return m.leaveReplay(a), nil
		}
		src, err := m.src.Rematch(a.set.BotLevel)
		if err != nil {
			m.err = err.Error()
			return m, nil
		}
		return newMatchScreen(a, src), tick(50)
	case isKey(k, "enter", "q", "esc"):
		if m.src.Watching() && m.back != nil {
			return m.back(a), nil
		}
		if isRemote(m.src) {
			return onlineBack(a), nil
		}
		return newMenuScreen(), nil
	}
	return m, nil
}

// ---- view ---------------------------------------------------------------

func (m *matchScreen) view(a *App) string {
	if m.help {
		return m.helpView(a)
	}
	if m.logOpen {
		return m.logView(a)
	}
	if m.info {
		return m.infoView(a)
	}
	switch m.phase {
	case "pass":
		return m.withButtons(a, m.passView(a))
	case "end":
		return m.withButtons(a, m.endView(a))
	}
	st, g := &a.st, a.g
	// Two layouts, same information: side by side when there is room for
	// the sidebar, stacked (board, then a two-line status) when there is
	// not. On a small screen the sidebar's detail moves behind `i`.
	turned, compact := m.layoutFor(a)
	m.compactKeys = compact
	team := m.scene.ViewTeam
	m.orient = render.ForTeam(team, turned)
	m.scene.Orient = m.orient
	m.scene.Piece = a.piece
	m.scene.Ghost = a.ghost
	m.scene.Block = a.block
	dw, dh := m.orient.Dims(m.vs.Board.W, m.vs.Board.H)
	boardW, boardH := m.boardRoom(a, compact, dh)
	// Tiles get as much room as the terminal can give them: a chessboard
	// on a desktop, big squares on a phone turned to fit.
	cw, chh := render.FitCell(dw, dh, boardW*2, boardH)
	switch a.set.CellsOrAuto() {
	case "small":
		// Every cell drawn is work for the terminal; on a slow one the
		// packed board is the difference between smooth and sluggish.
		cw, chh = 2, 1
	case "large":
		if boardW*2 >= dw*4 && boardH >= dh*2 {
			cw, chh = 4, 2
		}
	}
	m.cellW, m.cellH = cw, chh
	m.scene.CellW, m.scene.CellH = cw, chh
	var focus *engine.Pos
	if f := m.focus(); f != nil {
		x, y := m.orient.Screen(*f, m.vs.Board.W, m.vs.Board.H)
		focus = &engine.Pos{X: x, Y: y}
	}
	m.vp = render.ClampViewport(render.Viewport{OX: m.vp.OX, OY: m.vp.OY, W: boardW * 2 / cw, H: boardH / chh}, m.screenBoard(), focus)
	if m.phase == "anim" && m.an != nil {
		// The animation's scene is its own; give it the tile size, or the
		// board drops to the packed 2x1 for the length of the turn.
		m.scene = m.an.scene
		m.scene.Cursor = nil
		m.scene.CellW, m.scene.CellH = cw, chh
		m.scene.Orient = m.orient
		m.scene.Piece = a.piece
		m.scene.Ghost = a.ghost
		m.scene.Block = a.block
	}
	m.scene.Ambient = 0
	if a.set.EffectsOn() && (m.phase == "orders" || m.phase == "wait") {
		m.scene.Ambient = m.ambient + 1
	}

	// Header.
	s := &m.vs
	you, them := 0, 1
	if m.player >= 0 {
		you = s.Player(m.player).Team
		them = 1 - you
	}
	scoreA, scoreB := s.Teams[you].Score, s.Teams[them].Score
	if m.phase == "anim" && m.an != nil {
		scoreA, scoreB = m.an.scores[you], m.an.scores[them]
	}
	phase := strings.ToUpper(m.phase)
	if m.phase == "anim" {
		phase = "RESOLVE"
	}
	if m.src.Watching() {
		phase = "REPLAY"
	}
	clock := ""
	if !m.deadline.IsZero() && (m.phase == "orders" || m.phase == "wait") {
		left := int(time.Until(m.deadline).Seconds())
		if left < 0 {
			left = 0
		}
		clock = fmt.Sprintf(" %d:%02d", left/60, left%60)
		if left <= 10 {
			clock = st.Danger.Render(clock)
		}
	}
	// The opponent's strip, as the web app has it above the board: who,
	// the turn and the clock, and their score as signal bars.
	head := m.strip(a, them, scoreB, fmt.Sprintf("TURN %d/%d  %s%s", s.Match.Turn, s.Match.MaxTurns, phase, clock), compact)
	lines := []string{fit(head, a.w)}

	// Board with labels, framed.
	var bl []string
	bl = append(bl, "   "+m.scene.ColumnHeader(m.vp, st))
	for i, row := range m.scene.RenderBoard(st, g, m.vp) {
		label := "  "
		if i%chh == (chh-1)/2 { // beside the tile's middle row, where its glyph is
			label = m.scene.RowLabel(m.vp.OY+i/chh, m.vp, st)
		}
		bl = append(bl, label+" "+row)
	}
	// The frame follows the board, not the space available: a 20-wide map
	// on a 200-column terminal is a 20-wide map, centred, not a box with
	// 60 empty columns in it.
	innerW := m.vp.W*cw + 3
	for i := range bl {
		bl[i] = fit(bl[i], innerW)
	}
	title := ""
	if m.vp.W < dw || m.vp.H < dh {
		title = "H/J/K/L scroll"
	}
	left := render.Frame(bl, innerW, title, st, g)

	// Your strip, under the board as the web app has it: who you are, the
	// target and the forecast, and your score as signal bars.
	goal := fmt.Sprintf("first to %d", s.Match.WinScore) + m.forecast(a, you)
	render.FrameFooter(left, innerW, strings.TrimSpace(m.strip(a, you, scoreA, goal, true)), st, g)
	var out []string
	out = append(out, lines...)
	// The board's first tile sits after the frame edge and the row label
	// ("│NN "), on the line after the frame top and the column header.
	m.boardX0, m.boardY0 = 4, len(out)+2
	if compact {
		out = append(out, left...)
		// Tall and narrow (a phone in portrait): the detail panel goes
		// under the board, where there is room for it. Short and wide: two
		// lines, and `i` opens the rest.
		room := a.h - len(out) - 2
		// The band needs the full portrait and the stats beside it; more
		// is empty frame. Rows left over show the latest events below.
		need := maxInt(art.PortraitRows, 10+len(m.myUnits())) + 2
		if spare := room - m.actionRows(a); spare >= art.SmallRows+2 && spare < need {
			need = spare // leave the action area its rows
		}
		if m.coach != nil {
			need += 8 // the tutorial's coaching sits above the stats
		}
		if room > need {
			room = need
		}
		switch {
		case room >= art.SmallRows+2:
			base := len(out)
			width := innerW + 2 // the board frame's, so the panels line up with it
			if width < 40 {
				width = minInt(a.w, 40)
			}
			out = append(out, m.bottomPanels(a, room, width)...)
			m.registerSideRows(a, base+m.panelY0, m.panelX0)
		default:
			out = append(out, m.compactStatus(a)...)
		}
	} else {
		// The panel may run past the board on a tall screen: that is where
		// the full portrait goes, instead of leaving the space empty.
		sideH := m.vp.H + 3
		if room := a.h - len(out) - 3; room > sideH {
			sideH = minInt(room, sideH+art.PortraitRows+6)
		}
		side := m.sidebar(a, sidebarW, sideH, true)
		m.registerSideRows(a, len(out), innerW+3)
		for i := 0; i < len(left) || i < len(side); i++ {
			l, r := "", ""
			if i < len(left) {
				l = left[i]
			}
			if i < len(side) {
				r = side[i]
			}
			out = append(out, fit(l, innerW+2)+" "+r)
		}
	}
	// The action area, under everything above, when there are rows for it
	// (keys do everything it does on a screen too small).
	if len(m.actionItems(a)) > 0 {
		areaW := innerW + 2
		if !compact {
			areaW = innerW + 3 + sidebarW
		}
		if compact && areaW < 40 {
			areaW = minInt(a.w, 40)
		}
		if room, need := a.h-len(out)-2, m.actionRows(a); room >= need {
			out = append(out, m.actionArea(a, areaW, len(out), 3)...)
		}
	}

	// Log fills the space above the message line and key bar: beside a
	// sidebar, or under the stacked panels when the screen is tall. All
	// of it is on its own screen (g).
	logLines := m.log
	if m.phase == "anim" && m.an != nil {
		logLines = append(append([]string{}, m.log...), m.an.log...)
	}
	room := a.h - len(out) - 2
	if room < 0 {
		room = 0
	}
	start := len(logLines) - room
	if start < 0 {
		start = 0
	}
	shown := logLines[start:]
	for _, l := range shown {
		out = append(out, st.Dim.Render(fit("> "+l, a.w)))
	}
	// Centre the block horizontally as well, so a wide terminal frames
	// the game instead of pinning it to the left. The key bar and the
	// message line stay full width: they are bars, not part of the block.
	if blockW := m.blockWidth(compact, innerW); blockW < a.w {
		margin := strings.Repeat(" ", (a.w-blockW)/2)
		for i := range out {
			out[i] = clip(margin+out[i], a.w)
		}
		m.boardX0 += len(margin)
		for i := range a.rows {
			a.rows[i].x0 += len(margin)
			if a.rows[i].x1 < 1<<29 {
				a.rows[i].x1 += len(margin)
			}
		}
	}
	// (Pointer targets were registered against the unpadded rows; the
	// shift below moves them, so it is applied to them too.)
	// A terminal much taller than the layout needs (a phone in portrait at
	// a small font) would leave a hole between the board and the key bar.
	// Centre the block instead of pinning it to the top.
	// Stacked, the board stays at the top: the space under the panels is
	// where the latest events go as the match fills it.
	if slack := a.h - 2 - len(out); slack > 2 && !compact {
		top := slack / 2
		out = append(make([]string, top), out...)
		m.boardY0 += top
		for i := range a.rows {
			a.rows[i].y += top
		}
	}
	for len(out) < a.h-2 {
		out = append(out, "")
	}
	if m.msg != "" {
		out = append(out, st.Warn.Render(fit(" "+m.msg, a.w)))
	} else {
		out = append(out, "")
	}
	out = append(out, st.Dim.Render(fit(m.keybar(), a.w)))
	return strings.Join(out[:minInt(len(out), a.h)], "\n")
}

// compactStatus is the two-line stand-in for the sidebar on a small
// screen: who is selected, what they can do, and what is planned.
func (m *matchScreen) compactStatus(a *App) []string {
	st, g := &a.st, a.g
	u := m.vs.Unit(m.sel)
	if u == nil {
		u = m.vs.UnitAt(m.cur)
	}
	first := st.Dim.Render("no unit selected — move the cursor onto one")
	if u != nil && u.Alive() {
		name := u.Name
		if u.IsCommander {
			name = g.Commander + " " + u.Name + fmt.Sprintf(" L%d", u.Level)
		}
		team := st.TeamA
		if u.Team != m.scene.ViewTeam {
			team = st.TeamB
		}
		first = fmt.Sprintf("%s %s  HP %d/%d MV %d RNG %d DEF %d",
			team.Render(name), st.Dim.Render(render.PosLabel(u.Pos)), u.HP, u.MaxHP, u.Eff("mv"), u.Eff("rng"), u.Eff("def"))
	}
	ordered := 0
	for _, o := range m.orders {
		_ = o
		ordered++
	}
	second := st.Dim.Render(fmt.Sprintf("%d orders  ·  i details  ? keys", ordered))
	if m.src.Watching() {
		second = st.Dim.Render("watching  ·  i details  ? keys")
	}
	return []string{fit(first, a.w), fit(second, a.w)}
}

// infoView is the sidebar on its own screen, for terminals too narrow to
// show it beside the board. Same lines, nothing hidden.
func (m *matchScreen) infoView(a *App) string {
	lines := []string{a.st.Title.Render("unit and tile"), ""}
	for _, l := range m.sidebar(a, minInt(a.w, sidebarW+8), a.h-4, true) {
		lines = append(lines, l)
	}
	lines = append(lines, "", a.st.Dim.Render("i or esc back"))
	if len(lines) > a.h {
		lines = lines[:a.h]
	}
	return strings.Join(lines, "\n")
}

func (m *matchScreen) focus() *engine.Pos {
	if m.phase == "anim" {
		return nil
	}
	p := m.cur
	return &p
}

func (m *matchScreen) teamName(team int) string {
	for _, p := range m.vs.Players {
		if p.Team == team {
			return p.Name
		}
	}
	return fmt.Sprintf("team %d", team)
}

func (m *matchScreen) keybar() string {
	switch {
	case m.src.Watching():
		return " enter next turn  . skip  hjkl cursor  q back"
	case m.phase == "wait":
		return " committed — waiting for the other side   Q leave"
	case m.mode == "cmd":
		return " :" + m.cmdline + "_"
	case m.mode == "chat":
		return " say: " + m.cmdline + "_"
	case m.mode == "go":
		return " tap a lit tile to move, a marked enemy to attack  esc put down"
	case m.mode == "move":
		return " move: hjkl cursor  enter confirm  esc cancel"
	case m.mode == "attack":
		return " attack: hjkl cursor  enter confirm  esc cancel"
	case m.mode == "ability":
		return " cast: hjkl cursor  enter confirm  esc cancel"
	case m.phase == "anim":
		return " . skip"
	}
	if m.src.Watching() {
		return " watching   t chat   Q leave"
	}
	if m.compactKeys {
		return " tab unit  m move  a attack  q/w/e/r cast  space commit  g log  ? keys"
	}
	base := " tab unit  m move  a attack  q/w/e/r cast  1-4 unit ability  o overwatch  x hold  d drop  space commit  : cmd  ? help"
	if isRemote(m.src) {
		base = " tab unit  m move  a attack  q/w/e/r cast  1-4 ability  o overwatch  x hold  space commit  t chat  ? help"
	}
	return base
}

// sidebar renders the detail panel at the given width and height. It is
// drawn beside the board on a wide screen and under it on a tall narrow
// one; the content is the same either way.
func (m *matchScreen) sidebar(a *App, w, h int, framed bool) []string {
	st, g := &a.st, a.g
	if framed {
		w -= 2 // the frame
	}
	m.sideRows = map[int]int{}
	var l []string
	add := func(s string) { l = append(l, fit(s, w)) }

	if m.coach != nil {
		l = append(l, m.coach.panel(a, w)...)
		add("")
	}
	// On a small panel the portrait becomes a card beside the unit's
	// stats, so the player can still see who they are commanding.
	card := m.portraitCard(a, w, h)
	l = append(l, card...)

	u := m.vs.Unit(m.sel)
	if m.phase == "anim" || u == nil {
		u = m.vs.UnitAt(m.cur)
	}
	if u != nil && u.Alive() && len(card) > 0 {
		// The card already carries the name, HP and stats.
	} else if u != nil && u.Alive() {
		name := u.Name
		if u.IsCommander {
			name = fmt.Sprintf("%s %s L%d", g.Commander, u.Name, u.Level)
		}
		team := st.TeamA
		if u.Team != m.scene.ViewTeam {
			team = st.TeamB
		}
		add(team.Render(name) + st.Dim.Render("  "+u.Kind+"  "+render.PosLabel(u.Pos)))
		add(fmt.Sprintf("HP %s %d/%d   INI %d", hpBar(a, u.HP, u.MaxHP, 10), u.HP, u.MaxHP, u.Eff("ini")))
		add(st.Dim.Render(fmt.Sprintf("MV %d  JMP %d  RNG %d  ATK %d  DEF %d", u.Eff("mv"), u.JMP, u.Eff("rng"), u.Eff("atk"), u.Eff("def"))))
		var status []string
		for _, s := range u.Status {
			if s.Kind == "stat" {
				status = append(status, fmt.Sprintf("%s%+d", s.Stat, s.Magnitude))
			} else if s.Turns > 0 {
				status = append(status, fmt.Sprintf("%s(%d)", s.Kind, s.Turns))
			} else {
				status = append(status, s.Kind)
			}
		}
		if u.Held {
			status = append(status, "held")
		}
		if u.Overwatch {
			status = append(status, "overwatch")
		}
		if len(status) > 0 {
			add(st.Warn.Render(strings.Join(status, " ")))
		}
		if u.Owner == m.player || m.player < 0 {
			abs := engine.UnitAbilities(a.c, u)
			if u.IsCommander {
				h := a.c.Heroes[u.Kind]
				for _, k := range []string{"q", "w", "e", "r"} {
					id := h.Abilities[k]
					ab := a.c.Abilities[id]
					state := "rdy"
					style := st.Good
					if !contains(abs, id) {
						state = fmt.Sprintf("L%d", a.c.Rules.Unlock[k])
						style = st.Dim
					} else if cd := u.Cooldowns[id]; cd > 0 {
						state = fmt.Sprintf("cd%d", cd)
						style = st.Warn
					}
					add(fmt.Sprintf("%s %-11s %s %s", st.Key.Render(strings.ToUpper(k)), ab.Name, style.Render(state), st.Dim.Render(abilityShort(ab))))
				}
			} else {
				for i, id := range abs {
					ab := a.c.Abilities[id]
					state := "rdy"
					style := st.Good
					if cd := u.Cooldowns[id]; cd > 0 {
						state = fmt.Sprintf("cd%d", cd)
						style = st.Warn
					}
					add(fmt.Sprintf("%s %-11s %s %s", st.Key.Render(fmt.Sprint(i+1)), ab.Name, style.Render(state), st.Dim.Render(abilityShort(ab))))
				}
			}
		}
	} else {
		add(st.Dim.Render("no unit selected"))
	}
	add("")

	// Attack preview.
	if m.mode == "attack" || (m.mode == "ability" && m.targets[m.cur] != 0) {
		if id, ok := m.targets[m.cur]; ok && id != 0 && m.mode == "attack" {
			if att := m.selected(); att != nil {
				t := m.vs.Unit(id)
				dice, tn := engine.AttackPreview(a.c, &m.vs, att, t, t.Pos)
				p := float64(7-tn) / 6
				if p < 0 {
					p = 0
				}
				add(st.Accent.Render(fmt.Sprintf("%dd6 vs %d+  %.0f%%/die  ~%.1f dmg", dice, tn, p*100, float64(dice)*p)))
				add("")
			}
		}
	}

	// The squad, as the web app's column lists it: each unit's health and
	// its orders (pips for how many it has given of how many it may).
	if m.player >= 0 && m.phase == "orders" {
		add(st.Title.Render("SQUAD"))
		for _, mu := range m.myUnits() {
			given, limit := 0, 1
			if mu.IsCommander {
				limit = a.c.Rules.CommanderOrders
			}
			for _, o := range m.orders {
				if o.UnitID == mu.ID {
					given++
				}
			}
			pips := ""
			for i := 0; i < limit; i++ {
				if i < given {
					pips += st.Accent.Render("●")
				} else {
					pips += st.Dim.Render("○")
				}
			}
			gl := hpBar(a, mu.HP, mu.MaxHP, 5) + " " + pips
			var parts []string
			for _, o := range m.orders {
				if o.UnitID != mu.ID {
					continue
				}
				switch o.Action {
				case engine.ActMove:
					parts = append(parts, "move "+render.PosLabel(o.Path[len(o.Path)-1]))
				case engine.ActAttack:
					parts = append(parts, "atk "+render.PosLabel(o.Target))
				case engine.ActAbility:
					parts = append(parts, a.c.Abilities[o.Ability].Name+" "+render.PosLabel(o.Target))
				default:
					parts = append(parts, o.Action)
				}
			}
			m.sideRows[len(l)] = mu.ID
			line := fmt.Sprintf("%-7s %s %s", truncate(mu.Name, 7), gl, st.Dim.Render(strings.Join(parts, ", ")))
			if len(parts) == 0 {
				line = fmt.Sprintf("%-7s %s", truncate(mu.Name, 7), gl)
			}
			if mu.ID == m.sel {
				line = st.Accent.Render("›") + line
			} else {
				line = " " + line
			}
			add(line)
		}
		add("")
	}

	// Tile under cursor.
	if m.phase != "anim" {
		t := m.vs.Board.At(m.cur)
		desc := fmt.Sprintf("%s  %s z%d", render.PosLabel(m.cur), t.Terrain, t.Z)
		if m.scene.Smoke[m.cur] {
			desc += " smoke"
		}
		if m.scene.Telegraph[m.cur] {
			desc += " " + st.Danger.Render("incoming")
		}
		if gh, ok := m.scene.Ghosts[m.cur]; ok {
			desc += st.Dim.Render(fmt.Sprintf("  last seen %s T%02d", gh.Kind, gh.Turn))
		}
		add(st.Dim.Render(desc))
	}

	// Minimap when scrolling.
	if sw, sh := m.orient.Dims(m.vs.Board.W, m.vs.Board.H); m.vp.W < sw || m.vp.H < sh {
		add("")
		for _, ml := range m.scene.RenderMinimap(st, g, m.vp, w, 6) {
			add(ml)
		}
	}
	// Portrait of the unit's commander when the terminal is tall enough:
	// never information, so it goes last and yields to everything else.
	if len(l)+art.PortraitRows+1 <= h-2 && w >= art.PortraitCols {
		for len(l) < h-2-art.PortraitRows {
			add("")
		}
		f, _ := m.portraitFrame(a, sizeFull)
		for _, pl := range f.Render(a.artStyler()) {
			l = append(l, fit(pl, w))
		}
	}
	if !framed {
		// Stacked under the board on a small screen: no frame, so the
		// card and the orders fit in the rows there are.
		return l[:minInt(len(l), h)]
	}
	for len(l) < h-2 {
		add("")
	}
	return render.Frame(l[:minInt(len(l), h-2)], w, "", st, g)
}

func abilityShort(ab engine.AbilityDef) string {
	s := ab.Target
	if ab.Range > 0 {
		s += fmt.Sprintf(" r%d", ab.Range)
	}
	if ab.Delay > 0 {
		s += fmt.Sprintf(" d%d", ab.Delay)
	}
	return s
}

// withButtons puts the action area along the bottom of a centred screen
// (hand-over, end card), so a phone always has something to press.
func (m *matchScreen) withButtons(a *App, v string) string {
	need := m.actionRows(a)
	if need == 0 || a.h < need+6 {
		return v
	}
	lines := strings.Split(v, "\n")
	for len(lines) < a.h {
		lines = append(lines, "")
	}
	lines = lines[:a.h-need-1]
	lines = append(lines, m.actionArea(a, minInt(a.w, 80), len(lines), 3)...)
	return strings.Join(lines, "\n")
}

func (m *matchScreen) passView(a *App) string {
	name := m.vs.Player(m.player).Name
	lines := []string{
		a.st.Title.Render("hand over"),
		"",
		fmt.Sprintf("%s: press enter when you have the keyboard", a.st.Accent.Render(name)),
		"",
		a.st.Dim.Render("the other player should look away"),
	}
	if m.quitArmed {
		lines = append(lines, "", a.st.Warn.Render(m.msg))
	}
	return a.centered(lines)
}

func (m *matchScreen) endView(a *App) string {
	s := m.final
	if s == nil {
		return a.centered([]string{a.st.Dim.Render("waiting for the result...")})
	}
	you := 0
	if m.player >= 0 {
		you = s.Player(m.player).Team
	}
	title := "DRAW"
	style := a.st.Warn
	if s.Match.Winner >= 0 {
		if m.player < 0 {
			title = strings.ToUpper(m.teamName(s.Match.Winner)) + " WINS"
			style = a.st.Accent
		} else if s.Match.Winner == you {
			title = "SIGNAL HELD"
			style = a.st.Good
		} else {
			title = "SIGNAL LOST"
			style = a.st.Danger
		}
	}
	lines := []string{
		style.Render(title),
		"",
		fmt.Sprintf("%d – %d   turn %d   by %s", s.Teams[0].Score, s.Teams[1].Score, s.Match.Turn, s.Match.Result),
		"",
	}
	for _, p := range s.Players {
		cmd := s.Unit(p.Commander)
		kills := 0
		for _, u := range s.Units {
			if u.Owner == p.ID {
				kills += u.Kills
			}
		}
		lvl := 0
		if cmd != nil {
			lvl = cmd.Level
		}
		st := a.st.TeamA
		if p.Team != you {
			st = a.st.TeamB
		}
		row := fmt.Sprintf("%s  %s L%d  kills %d", st.Render(fmt.Sprintf("%-12s", p.Name)), cmdName(a, cmd), lvl, kills)
		if d, ok := m.ratings[p.ID]; ok {
			delta := fmt.Sprintf("%+d", int(d.After-d.Before))
			if d.After < d.Before {
				delta = a.st.Danger.Render(delta)
			} else {
				delta = a.st.Good.Render(delta)
			}
			row += fmt.Sprintf("  %d %s", int(d.After+0.5), delta)
		}
		lines = append(lines, row)
	}
	lines = append(lines, "")
	if m.reason != "" {
		lines = append(lines, a.st.Dim.Render(m.reason))
	}
	if m.saved != "" {
		lines = append(lines, a.st.Dim.Render("saved "+m.saved))
	}
	if m.err != "" {
		lines = append(lines, a.st.Danger.Render(m.err))
	}
	if m.src.Watching() {
		lines = append(lines, a.st.Dim.Render("r back to the list  enter menu"))
	} else if isRemote(m.src) {
		lines = append(lines, a.st.Dim.Render("s save replay  enter back to online"))
	} else {
		lines = append(lines, a.st.Dim.Render("r rematch  s save replay  enter menu"))
	}
	return a.centered(lines)
}

func cmdName(a *App, u *engine.Unit) string {
	if u == nil {
		return "?"
	}
	if h, ok := a.c.Heroes[u.Kind]; ok {
		return h.Name
	}
	return u.Kind
}

func (m *matchScreen) helpView(a *App) string {
	k := a.st.Key.Render
	if a.w < 80 { // stacked layout: one key per line, no columns
		var lines []string
		lines = append(lines, a.st.Title.Render("keys"), "")
		for _, row := range [][2]string{
			{"hjkl yubn", "cursor (arrows too)"}, {"HJKL", "scroll the board"},
			{"tab", "next unit"}, {"enter", "select / confirm"},
			{"m", "move"}, {"a", "attack"}, {"q w e r", "commander abilities"},
			{"1-4", "unit abilities"}, {"o", "overwatch"}, {"x", "hold"},
			{"d", "drop orders"}, {"space", "commit the turn"}, {"esc", "cancel"},
			{"i", "unit and tile detail"}, {"g", "log and chat"}, {"t", "chat (online)"}, {":", "command line"},
			{".", "skip animation"}, {"Q Q", "quit (asks; forfeits online)"},
		} {
			lines = append(lines, fit(k(fmt.Sprintf("%-10s", row[0]))+" "+row[1], a.w-2))
		}
		lines = append(lines, "", a.st.Dim.Render("any key to close"))
		if len(lines) > a.h {
			lines = lines[:a.h]
		}
		return strings.Join(lines, "\n")
	}
	lines := []string{
		a.st.Title.Render("keys"), "",
		k("hjkl yubn") + "  cursor (arrows too)      " + k("HJKL") + "  scroll by 5",
		k("tab") + "        next unit                " + k("enter") + " select unit / confirm target",
		k("m") + "  move    " + k("a") + "  attack    " + k("q w e r") + "  commander abilities    " + k("1-4") + "  unit abilities",
		k("o") + "  overwatch (no attack)    " + k("x") + "  hold (+DEF until moved)",
		k("d") + "  drop orders for unit     " + k("space") + "  commit turn      " + k("esc") + "  cancel",
		k(":") + "  command line:  m l d7 · a r k9 · c q g5 · o u · x l · commit",
		k(".") + "  skip animation           " + k("t") + "  chat (online)",
		k("i") + "  unit and tile detail (small terminals)    " + k("g") + "  log and chat",
		k("Q Q") + " quit to menu (asks first; forfeits online, daily keeps going, :resign)",
		"",
		a.st.Dim.Render("resolution: instant abilities → delayed abilities → moves (INI wins tiles) →"),
		a.st.Dim.Render("overwatch → attacks by INI → end of turn → objectives score"),
		a.st.Dim.Render("telegraphed tiles (" + a.g.Telegraph + ") are hit next turn: move off them"),
		"", a.st.Dim.Render("any key to close"),
	}
	return a.centered(lines)
}

func contains(xs []string, x string) bool {
	for _, s := range xs {
		if s == x {
			return true
		}
	}
	return false
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// forecast is who scores at the end of the turn if no objective changes
// hands: under net scoring only the side holding more scores, by the
// difference ("  +1/turn you"), as the web app's bottom strip says.
func (m *matchScreen) forecast(a *App, you int) string {
	if m.phase == "anim" || m.vs.Ended() {
		return ""
	}
	var held [2]int
	for _, o := range m.vs.Objectives {
		if o.Holder == 0 || o.Holder == 1 {
			held[o.Holder]++
		}
	}
	lead := held[you] - held[1-you]
	st := &a.st
	switch {
	case lead > 0:
		return st.TeamA.Render(fmt.Sprintf("  +%d/turn you", lead))
	case lead < 0:
		return st.TeamB.Render(fmt.Sprintf("  +%d/turn them", -lead))
	}
	return st.Dim.Render("  even")
}

// strip is a player's line, as the web app's strips above and below the
// board: a team dot and name, a middle note, and the score as signal bars.
func (m *matchScreen) strip(a *App, team, score int, note string, compact bool) string {
	st := &a.st
	ts := st.TeamA
	if team != m.youTeam() {
		ts = st.TeamB
	}
	name := strings.ToUpper(m.teamName(team))
	if team == m.youTeam() {
		name = "YOU"
		if u := m.vs.Unit(m.myCommander()); u != nil {
			name += " · " + u.Name
		}
	}
	bars := ""
	win := m.vs.Match.WinScore
	if win <= 0 {
		win = 5
	}
	levels := []rune("▁▂▃▅▇")
	for i := 0; i < win && i < 8; i++ {
		ch := string(levels[minInt(i*len(levels)/win, len(levels)-1)])
		if i < score {
			bars += ts.Render(ch)
		} else {
			bars += st.Dim.Render(ch)
		}
	}
	if compact {
		return fmt.Sprintf(" %s %s  %s  %s %d", ts.Render("●"), name, note, bars, score)
	}
	return fmt.Sprintf(" %s %-22s %s   %s %d", ts.Render("●"), name, note, bars, score)
}

// youTeam is the viewer's team (team 0 when watching).
func (m *matchScreen) youTeam() int {
	if m.player >= 0 {
		if p := m.vs.Player(m.player); p != nil {
			return p.Team
		}
	}
	return 0
}

// myCommander is the viewer's commander's unit id (0 if none).
func (m *matchScreen) myCommander() int {
	if m.player >= 0 {
		if p := m.vs.Player(m.player); p != nil {
			return p.Commander
		}
	}
	return 0
}

// hpBar is a health bar coloured by how hurt, as the web app colours it:
// green, then amber below two thirds, red below one third.
func hpBar(a *App, hp, max, width int) string {
	st := &a.st
	full := st.Good
	switch {
	case hp*3 <= max:
		full = st.Danger
	case hp*3 <= max*2:
		full = st.Warn
	}
	return render.Bar(hp, max, width, a.g, full, st.Dim)
}
