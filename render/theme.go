package render

import (
	"io"
	"sort"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"rfog/render/palette"
)

// Theme is a colour palette. Themes never change glyph meaning.
// Colours are given for truecolor; lipgloss degrades them for 256/16.
// The "mono" theme uses no colours at all: bold/reverse/dim only.
type Theme struct {
	Name      string
	Mono      bool
	Fg        string
	Dim       string
	Accent    string
	TeamA     string
	TeamB     string
	Danger    string
	Warn      string
	Good      string
	Objective string
	Smoke     string
	Wall      string
	Cover     string
	Ground    [4]string // by height z0..z3
	Fog       string
	Cursor    string // background
	Highlight string // background for reachable / targetable tiles
	Border    string
	Board     string // the shaded half of the chessboard
	// Light, when set, is the other half's background on a board of big
	// tiles, so every square is a colour (the graphical client's look);
	// unset keeps the terminal's own background under the light squares.
	Light   string
	WallTop string // a wall block's lit top face (with Light)
	// ObjBg, ObjA and ObjB are an objective square's background (with
	// Light): nobody's, team A's, team B's. ObjC is a contested one, LastBg
	// last turn's moves, LedgeBg and CastBg the depth of raised ground.
	ObjBg, ObjA, ObjB             string
	ObjC, LastBg, LedgeBg, CastBg string
}

// themes are mono (no colour at all, for any terminal) and one per board.
var themes = func() map[string]Theme {
	m := map[string]Theme{"mono": {Name: "mono", Mono: true}}
	for _, b := range Boards {
		m[b.Name] = BoardTheme(b)
	}
	return m
}()

// ThemeNames lists the themes: the boards in their order, then mono.
func ThemeNames() []string {
	var out []string
	for _, b := range Boards {
		out = append(out, b.Name)
	}
	var extra []string
	for k := range themes {
		if _, ok := boardIndex(k); !ok && k != "mono" {
			extra = append(extra, k)
		}
	}
	sort.Strings(extra)
	return append(append(out, extra...), "mono")
}

func boardIndex(name string) (int, bool) {
	for i, b := range Boards {
		if b.Name == name {
			return i, true
		}
	}
	return 0, false
}

// GetTheme returns a theme by name, falling back to fibre.
func GetTheme(name string) Theme {
	if t, ok := themes[name]; ok {
		return t
	}
	return themes["fibre"]
}

// RegisterTheme adds or replaces a theme (from the user's theme.toml).
func RegisterTheme(t Theme) { themes[t.Name] = t }

// Styles are the lipgloss styles derived from a theme for a tier.
type Styles struct {
	Theme     Theme
	Tier      Tier
	Renderer  *lipgloss.Renderer // the session's renderer; art frames render through it
	Plain     lipgloss.Style
	Dim       lipgloss.Style
	Accent    lipgloss.Style
	Title     lipgloss.Style
	TeamA     lipgloss.Style
	TeamB     lipgloss.Style
	Danger    lipgloss.Style
	Warn      lipgloss.Style
	Good      lipgloss.Style
	Objective lipgloss.Style
	Smoke     lipgloss.Style
	Wall      lipgloss.Style
	Cover     lipgloss.Style
	Ground    [4]lipgloss.Style
	Fog       lipgloss.Style
	Cursor    lipgloss.Style
	Highlight lipgloss.Style
	Target    lipgloss.Style
	Border    lipgloss.Style
	Key       lipgloss.Style
	Selected  lipgloss.Style
	// Shade is the dark squares' background on a board of big tiles
	// ("" at T0 and in mono themes, which keep the dotted floor).
	Shade string
	// Light is the light squares' background, and FogBg the background of
	// squares out of sight, on a board of big tiles ("" = the terminal's).
	Light, FogBg string
	// ObjBg, ObjA, ObjB, ObjC, LastBg, LedgeBg, CastBg: see Theme.
	ObjBg, ObjA, ObjB, ObjC, LastBg, LedgeBg, CastBg string
}

// Profile is the colour depth a tier may use: T0 the 16 ANSI colours (or
// none with NO_COLOR), T1 256, T2 truecolor. Every session gets its own
// renderer with an explicit profile, so a session's colours never depend
// on the terminal the *server* process happens to run in.
func Profile(tier Tier, noColor bool) termenv.Profile {
	switch {
	case noColor:
		return termenv.Ascii
	case tier == T0:
		return termenv.ANSI
	case tier == T1:
		return termenv.ANSI256
	}
	return termenv.TrueColor
}

// NewRenderer makes the renderer a session draws through. Everything is
// set explicitly: asking the terminal what it supports means writing
// OSC 11 and CPR queries and waiting for an answer, which costs a second
// on a terminal that does not reply (and blocks forever on one that
// cannot). The tier already says what the colours should be.
func NewRenderer(out io.Writer, tier Tier, noColor bool) *lipgloss.Renderer {
	if out == nil {
		out = io.Discard
	}
	r := lipgloss.NewRenderer(out)
	r.SetColorProfile(Profile(tier, noColor))
	r.SetHasDarkBackground(true)
	return r
}

// NewStyles builds styles. T0 and mono themes use attributes only. out is
// the terminal the styles are rendered to (only its colour profile matters).
func NewStyles(t Theme, tier Tier, out io.Writer, noColor bool) Styles {
	return newStyles(t, tier, NewRenderer(out, tier, noColor))
}

// StylesFor rebuilds styles on an existing renderer, so a resize does not
// create (and re-probe) a new one.
func StylesFor(t Theme, tier Tier, r *lipgloss.Renderer, noColor bool) Styles {
	r.SetColorProfile(Profile(tier, noColor))
	return newStyles(t, tier, r)
}

func newStyles(t Theme, tier Tier, r *lipgloss.Renderer) Styles {
	s := Styles{Theme: t, Tier: tier, Renderer: r}
	lipgloss := styler{r} // every NewStyle below binds to this session's renderer
	if t.Mono || tier == T0 {
		s.Plain = lipgloss.NewStyle()
		s.Dim = lipgloss.NewStyle().Faint(true)
		s.Accent = lipgloss.NewStyle().Bold(true)
		s.Title = lipgloss.NewStyle().Bold(true)
		s.TeamA = lipgloss.NewStyle().Bold(true)
		s.TeamB = lipgloss.NewStyle().Reverse(true)
		s.Danger = lipgloss.NewStyle().Bold(true).Reverse(true)
		s.Warn = lipgloss.NewStyle().Bold(true)
		s.Good = lipgloss.NewStyle().Bold(true)
		s.Objective = lipgloss.NewStyle().Bold(true)
		s.Smoke = lipgloss.NewStyle().Faint(true)
		s.Wall = lipgloss.NewStyle()
		s.Cover = lipgloss.NewStyle()
		for i := range s.Ground {
			s.Ground[i] = lipgloss.NewStyle().Faint(true)
		}
		s.Fog = lipgloss.NewStyle().Faint(true)
		s.Cursor = lipgloss.NewStyle().Reverse(true).Bold(true)
		s.Highlight = lipgloss.NewStyle().Underline(true)
		s.Target = lipgloss.NewStyle().Underline(true).Bold(true)
		s.Border = lipgloss.NewStyle().Faint(true)
		s.Key = lipgloss.NewStyle().Bold(true)
		s.Selected = lipgloss.NewStyle().Bold(true).Underline(true)
		if !t.Mono && tier == T0 {
			// 16 colours are allowed at T0; use the ANSI basics for team contrast.
			s.TeamA = s.TeamA.Foreground(lipgloss.Color("14"))
			s.TeamB = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("9"))
			s.Objective = s.Objective.Foreground(lipgloss.Color("11"))
			s.Danger = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("9"))
			s.Good = s.Good.Foreground(lipgloss.Color("10"))
			s.Warn = s.Warn.Foreground(lipgloss.Color("11"))
			s.Accent = s.Accent.Foreground(lipgloss.Color("13"))
		}
		return s
	}
	c := lipgloss.Color
	s.Shade = "#1f1f1f"
	if t.Light != "" {
		s.Shade, s.Light, s.FogBg = t.Board, t.Light, t.Fog
		s.ObjBg, s.ObjA, s.ObjB = t.ObjBg, t.ObjA, t.ObjB
		s.ObjC, s.LastBg, s.LedgeBg, s.CastBg = t.ObjC, t.LastBg, t.LedgeBg, t.CastBg
	}
	s.Plain = lipgloss.NewStyle().Foreground(c(t.Fg))
	s.Dim = lipgloss.NewStyle().Foreground(c(t.Dim))
	s.Accent = lipgloss.NewStyle().Foreground(c(t.Accent))
	s.Title = lipgloss.NewStyle().Foreground(c(t.Accent)).Bold(true)
	s.TeamA = lipgloss.NewStyle().Foreground(c(t.TeamA)).Bold(true)
	s.TeamB = lipgloss.NewStyle().Foreground(c(t.TeamB)).Bold(true)
	s.Danger = lipgloss.NewStyle().Foreground(c(t.Danger)).Bold(true)
	s.Warn = lipgloss.NewStyle().Foreground(c(t.Warn))
	s.Good = lipgloss.NewStyle().Foreground(c(t.Good))
	s.Objective = lipgloss.NewStyle().Foreground(c(t.Objective)).Bold(true)
	s.Smoke = lipgloss.NewStyle().Foreground(c(t.Smoke))
	s.Wall = lipgloss.NewStyle().Foreground(c(t.Wall))
	s.Cover = lipgloss.NewStyle().Foreground(c(t.Cover))
	for i := range s.Ground {
		s.Ground[i] = lipgloss.NewStyle().Foreground(c(t.Ground[i]))
	}
	s.Fog = lipgloss.NewStyle().Foreground(c(t.Fog))
	s.Cursor = lipgloss.NewStyle().Background(c(t.Cursor)).Foreground(c(t.Fg)).Bold(true)
	s.Highlight = lipgloss.NewStyle().Background(c(t.Highlight))
	s.Target = lipgloss.NewStyle().Background(c(t.Highlight)).Foreground(c(t.Danger)).Bold(true)
	s.Border = lipgloss.NewStyle().Foreground(c(t.Border))
	s.Key = lipgloss.NewStyle().Foreground(c(t.Accent)).Bold(true)
	s.Selected = lipgloss.NewStyle().Foreground(c(t.Accent)).Bold(true).Underline(true)
	return s
}

// styler binds lipgloss.NewStyle to a renderer while keeping the call sites
// above readable (they shadow the package name on purpose).
type styler struct{ r *lipgloss.Renderer }

func (s styler) NewStyle() lipgloss.Style      { return s.r.NewStyle() }
func (s styler) Color(v string) lipgloss.Color { return palette.Color(s.r, v) }
