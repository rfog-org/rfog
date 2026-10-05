// Package client is the terminal client: screens, input and local play.
// The same App runs natively, offline against bots, and (later) in-process
// on the server for SSH and web sessions.
package client

import (
	"fmt"
	"io"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"rfog/engine"
	"rfog/proto"
	"rfog/render"
	"rfog/render/art"
)

// Options configure an App.
type Options struct {
	Content  *engine.Content
	Settings Settings
	Env      render.Env
	// Start picks the first screen: "" (title), "bots" (straight to a bot match setup),
	// "online" (connect and show the online menu).
	Start string
	// Dial connects to a server; nil means TCP. SSH sessions pass an in-process dialer.
	Dial Dialer
	// Server overrides the settings' server address (SSH sessions use "internal").
	Server string
	// Ephemeral apps (SSH sessions running on the server) never read or
	// write the host's config or data directories.
	Ephemeral bool
	// NoSave keeps command-line overrides (theme, tier, name) out of the
	// settings file for this run.
	NoSave bool
	// Output is the terminal the app renders to; nil means stdout. Colour
	// profiles are chosen per app, never from the process's own terminal.
	Output io.Writer
}

// The smallest terminal the client will draw on, and the width below
// which it switches to the stacked layout. Nothing is hidden at small
// sizes: the board scrolls with a minimap and the sidebar moves behind
// `i` (SPEC §2: no information is size-gated).
const (
	minWidth   = 40
	minHeight  = 18
	wideEnough = 80
)

// App is the root Bubble Tea model.
type App struct {
	pieces map[string][]string // board pieces, tinted and rendered, by name/team colour
	c      *engine.Content     // the rules in play: local, or the server's while online
	local  *engine.Content     // the rules this build embeds
	set    Settings
	env    render.Env
	tier   render.Tier
	st     render.Styles
	g      render.Glyphs
	w, h   int
	screen screen
	quit   bool

	// sized is set once the terminal has told us how big it is. The first
	// frame happens before that, so anything that would be laid out wrong
	// at the default 80x24 waits for it.
	sized     bool
	renderer  *lipgloss.Renderer // one per session: see render.NewRenderer
	dial      Dialer
	server    string
	ephemeral bool
	// displaced is set when the server says this player continued on
	// another device: screens say so instead of "connection lost", and
	// nothing reconnects on its own.
	displaced bool
	// resumeTried: the menu's quiet check for a match in progress has run.
	resumeTried bool
	nosave      bool
	out         io.Writer
	// Pointer input. Screens register clickable rows as they render; the
	// map is rebuilt every frame and read when a mouse event arrives.
	// Mouse is always optional: everything is reachable from the keyboard.
	pendingRows []clickRow
	rows        []clickRow
	net         *remote        // live server connection, nil offline
	rm          *remoteMatch   // live online match, nil otherwise
	spec        *spectateMatch // live match being watched, nil otherwise
	portraits   map[string]art.Frame
}

// clickRow is a clickable line: id is whatever the screen wants back.
// x0/x1 bound it horizontally, because the board and the detail panel
// share rows when they sit side by side.
type clickRow struct {
	y      int
	x0, x1 int
	id     int
}

// clickRow marks the content line at index i (before centring) as a
// click target for item id.
func (a *App) clickRow(i, id int) {
	a.pendingRows = append(a.pendingRows, clickRow{y: i, x1: 1 << 30, id: id})
}

// block pads lines to the width of the widest, so a centred list reads
// as a column instead of a ragged pile (and every row is the same
// target).
func block(lines []string) []string {
	w := 0
	for _, l := range lines {
		if n := render.Width(l); n > w {
			w = n
		}
	}
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = fit(l, w)
	}
	return out
}

// roomy reports whether list rows should be spaced out: a tap needs a
// bigger target than a keyboard does, and a screen with rows to spare
// should use them.
func (a *App) roomy(items int) bool { return a.h >= items*2+8 }

// clickRows marks a content line and, when the list is spaced out, the
// blank line under it, so a tap between rows still lands.
func (a *App) clickRows(i, id int, roomy bool) {
	a.clickRow(i, id)
	if roomy {
		a.clickRow(i+1, id)
	}
}

// clickRowAt marks an absolute screen row (screens that do not centre).
func (a *App) clickRowAt(y, id int) {
	a.rows = append(a.rows, clickRow{y: y, x1: 1 << 30, id: id})
}

// clickBox marks part of an absolute row: x0 up to (not including) x1.
func (a *App) clickBox(y, x0, x1, id int) {
	a.rows = append(a.rows, clickRow{y: y, x0: x0, x1: x1, id: id})
}

// pointer resolves a mouse event against the rows the last frame
// registered. With a real mouse the motion event highlights the row
// under the cursor, so a single click acts; a touch screen sends no
// motion, so the first tap highlights and the second acts.
func (a *App) pointer(m tea.MouseMsg, sel int) (id int, hit, act bool) {
	id, hit = a.pointed(m)
	if !hit {
		return 0, false, false
	}
	if m.Action == tea.MouseActionMotion {
		return id, true, false
	}
	if m.Action != tea.MouseActionPress || m.Button != tea.MouseButtonLeft {
		return 0, false, false
	}
	return id, true, id == sel
}

// pointed reports the item under any pointer event.
func (a *App) pointed(m tea.MouseMsg) (int, bool) {
	for _, r := range a.rows {
		if r.y == m.Y && m.X >= r.x0 && m.X < r.x1 {
			return r.id, true
		}
	}
	return 0, false
}

// clicked reports the item under a left press, if any.
func (a *App) clicked(m tea.MouseMsg) (int, bool) {
	if m.Action != tea.MouseActionPress || m.Button != tea.MouseButtonLeft {
		return 0, false
	}
	for _, r := range a.rows {
		if r.y == m.Y && m.X >= r.x0 && m.X < r.x1 {
			return r.id, true
		}
	}
	return 0, false
}

// screen is one UI screen. Screens return the next screen (themselves to stay).
type screen interface {
	update(a *App, msg tea.Msg) (screen, tea.Cmd)
	view(a *App) string
}

// initer is a screen that needs a command started when it is entered.
type initer interface {
	init(a *App) tea.Cmd
}

// enter switches screens, starting the new screen's init command if any.
func (a *App) enter(next screen, cmd tea.Cmd) tea.Cmd {
	if next == a.screen {
		return cmd
	}
	a.screen = next
	if in, ok := next.(initer); ok {
		return tea.Batch(cmd, in.init(a))
	}
	return cmd
}

// EnvFromOS fills a render.Env from the process environment.
func EnvFromOS() render.Env {
	lang := os.Getenv("LC_ALL")
	if lang == "" {
		lang = os.Getenv("LC_CTYPE")
	}
	if lang == "" {
		lang = os.Getenv("LANG")
	}
	return render.Env{Term: os.Getenv("TERM"), ColorTerm: os.Getenv("COLORTERM"), Lang: lang, NoColor: os.Getenv("NO_COLOR") != ""}
}

// New creates the app.
func New(o Options) *App {
	a := &App{c: o.Content, local: o.Content, set: o.Settings, env: o.Env, w: 80, h: 24, dial: o.Dial, server: o.Server, ephemeral: o.Ephemeral, nosave: o.NoSave, out: o.Output}
	if a.dial == nil {
		a.dial = DialTCP
	}
	if a.out == nil {
		a.out = os.Stdout
	}
	if o.Env.Width > 0 && o.Env.Height > 0 {
		a.w, a.h, a.sized = o.Env.Width, o.Env.Height, true
	}
	if a.server == "" {
		a.server = a.set.Server
	}
	a.applyTier()
	switch o.Start {
	case "bots":
		a.screen = newPickScreen(a, matchBots)
	case "online":
		a.screen = newOnlineScreen(a)
	default:
		a.screen = newTitleScreen()
	}
	return a
}

// compact reports whether the stacked layout is in use: the sidebar only
// fits beside the board on a wide screen. A short wide terminal keeps the
// sidebar (it saves rows); a narrow one stacks whatever the height.
func (a *App) compact() bool { return a.w < wideEnough }

// saveSettings persists settings unless this app is ephemeral.
func (a *App) saveSettings() {
	if a.ephemeral || a.nosave {
		return
	}
	_ = a.set.Save()
}

// applyTier recomputes tier/styles/glyphs from settings and env.
func (a *App) applyTier() {
	a.pieces = nil // tinted with the theme's team colours
	a.env.Width, a.env.Height = a.w, a.h
	switch a.set.Tier {
	case "t0":
		a.tier = render.T0
	case "t1":
		a.tier = render.T1
	case "t2":
		a.tier = render.T2
	default:
		a.tier = render.Probe(a.env)
	}
	if a.renderer == nil {
		a.renderer = render.NewRenderer(a.out, a.tier, a.env.NoColor)
	}
	theme := render.GetTheme(a.set.Theme)
	if a.tier != render.T0 && render.TrueColor(a.env) {
		a.renderer.SetColorProfile(termenv.TrueColor) // T1 layout, the terminal's real colours
	} else if a.renderer.ColorProfile() == termenv.ANSI256 {
		theme = render.Theme256(theme) // no 24-bit colour: exact palette entries, not rounded ones
	}
	a.st = render.StylesFor(theme, a.tier, a.renderer, a.env.NoColor)
	a.g = render.GlyphsFor(a.tier)
}

// Init implements tea.Model.
func (a *App) Init() tea.Cmd {
	if in, ok := a.screen.(initer); ok {
		return tea.Batch(tea.EnterAltScreen, in.init(a))
	}
	return tea.EnterAltScreen
}

// Update implements tea.Model.
func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	return a.update(msg)
}

func (a *App) update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m := msg.(type) {
	case tea.WindowSizeMsg:
		a.w, a.h = m.Width, m.Height
		a.sized = true
		a.applyTier()
	case tea.KeyMsg:
		if m.Type == tea.KeyCtrlC {
			a.quit = true
			return a, tea.Quit
		}
	case netMsg:
		if m.T == proto.TError {
			var e proto.Error
			if m.F.As(&e) == nil && e.Code == proto.ErrDisplaced {
				a.displaced = true
			}
		}
		// Server traffic: translate match frames, then keep listening.
		var again tea.Cmd
		if a.net != nil && m.Err == nil {
			again = a.net.recv()
		}
		if m.Err != nil && a.net != nil {
			a.net = nil
		}
		inner := tea.Msg(m)
		if a.rm != nil && m.Err == nil {
			if t, err := a.rm.translate(a, m); err == nil && t != nil {
				inner = t
			}
		}
		next, cmd := a.screen.update(a, inner)
		if next == nil {
			a.quit = true
			return a, tea.Quit
		}
		return a, tea.Batch(a.enter(next, cmd), again)
	}
	next, cmd := a.screen.update(a, msg)
	if next == nil {
		a.quit = true
		return a, tea.Quit
	}
	return a, a.enter(next, cmd)
}

// View implements tea.Model.
func (a *App) View() string {
	if a.quit {
		return ""
	}
	a.rows, a.pendingRows = a.rows[:0], a.pendingRows[:0]
	if a.w < minWidth || a.h < minHeight {
		return a.st.Warn.Render(fmt.Sprintf("terminal is %dx%d: need at least %dx%d", a.w, a.h, minWidth, minHeight)) + "\n" +
			a.st.Dim.Render("(in a browser: rotate, or shrink the font)") + "\n"
	}
	return a.screen.view(a)
}

// Run starts the client on the current terminal.
func Run(o Options) error {
	if name := LoadUserTheme(); name != "" && o.Settings.Theme == "" {
		o.Settings.Theme = name
	}
	// Mouse is optional everywhere (SPEC §2.8) but makes the game playable
	// on a touchscreen, where the tap arrives as a click.
	p := tea.NewProgram(New(o), tea.WithAltScreen(), tea.WithOutput(os.Stdout), tea.WithMouseCellMotion())
	_, err := p.Run()
	return err
}

// ---- small helpers shared by screens -----------------------------------

// centered pads lines to the app size, vertically and horizontally
// centred, and clips anything that would not fit. Every screen goes
// through here, so no screen can overflow a small terminal.
func (a *App) centered(lines []string) string {
	clipped := make([]string, 0, len(lines))
	for _, l := range lines {
		clipped = append(clipped, clip(l, a.w))
	}
	if len(clipped) > a.h {
		clipped = clipped[:a.h]
	}
	pad := (a.h - len(clipped)) / 2
	if pad < 0 {
		pad = 0
	}
	var out []string
	for i := 0; i < pad; i++ {
		out = append(out, "")
	}
	for _, l := range clipped {
		w := render.Width(l)
		left := (a.w - w) / 2
		if left < 0 {
			left = 0
		}
		out = append(out, strings.Repeat(" ", left)+l)
	}
	// Click targets the screen registered are content-relative; the
	// vertical padding moves them.
	for _, r := range a.pendingRows {
		if r.y < len(clipped) {
			a.rows = append(a.rows, clickRow{y: pad + r.y, x0: r.x0, x1: r.x1, id: r.id})
		}
	}
	a.pendingRows = a.pendingRows[:0]
	return strings.Join(out, "\n")
}

// clip cuts a styled line to w printed columns, keeping escape sequences
// intact and closing any style it cuts through.
func clip(s string, w int) string {
	if render.Width(s) <= w {
		return s
	}
	var b strings.Builder
	width, styled := 0, false
	rs := []rune(s)
	for i := 0; i < len(rs); i++ {
		if rs[i] == 0x1b { // escape: copy it whole, it costs no columns
			start := i
			for i < len(rs) && rs[i] != 'm' {
				i++
			}
			seq := string(rs[start:minInt(i+1, len(rs))])
			b.WriteString(seq)
			styled = seq != "\x1b[0m"
			continue
		}
		cw := render.Width(string(rs[i]))
		if width+cw > w {
			break
		}
		b.WriteRune(rs[i])
		width += cw
	}
	if styled {
		b.WriteString("\x1b[0m")
	}
	return b.String()
}

// lipglossWidthOf is the printed width of a styled line.
func lipglossWidthOf(s string) int { return render.Width(s) }

// fit pads or truncates a line to exactly w columns.
func fit(s string, w int) string {
	cur := render.Width(s)
	if cur == w {
		return s
	}
	if cur < w {
		return s + strings.Repeat(" ", w-cur)
	}
	return truncate(s, w)
}

// truncate cuts a string to w columns (see clip for styled text).
func truncate(s string, w int) string {
	if render.Width(s) <= w {
		return s
	}
	r := []rune(s)
	for len(r) > 0 && lipgloss.Width(string(r)) > w {
		r = r[:len(r)-1]
	}
	return string(r)
}

// key helpers
func isKey(m tea.KeyMsg, keys ...string) bool {
	s := m.String()
	for _, k := range keys {
		if s == k {
			return true
		}
	}
	return false
}

// NewBench builds an app sitting in a match, for `rfog bench`. It never
// touches the network or the settings file.
func NewBench(o Options, st engine.Setup) *App {
	o.NoSave = true
	a := New(o)
	lm, err := newLocalMatch(a.c, st, [2]bool{false, true}, "normal")
	if err != nil {
		return nil
	}
	a.sized = true
	a.screen = newMatchScreen(a, lm)
	return a
}

// Frame renders one frame, as the program would.
func (a *App) Frame() string { return a.View() }
