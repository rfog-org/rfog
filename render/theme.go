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

var themes = map[string]Theme{
	"mono": {Name: "mono", Mono: true},
	// fibre is the graphical client's palette: cold slate squares, cyan
	// for one side and hazard orange for the other, walls as dark blocks.
	"fibre": {
		Name: "fibre", Fg: "#cdd5dc", Dim: "#76808a", Accent: "#4fd3e8", TeamA: "#4fd3e8", TeamB: "#ff7043",
		Danger: "#ff5a4a", Warn: "#e3c05c", Good: "#7ed98a", Objective: "#e6f0f5", Smoke: "#aab4bc", Wall: "#141c23",
		Cover: "#c9a13a", Ground: [4]string{"#7d93a3", "#8ea5b5", "#a0b8c8", "#b4ccdc"}, Fog: "#28333d",
		Cursor: "#1b8ea3", Highlight: "#2d6676", Border: "#3a4552", Board: "#44596a", Light: "#5a7282", WallTop: "#4a5966",
		ObjBg: "#7a92a3", ObjA: "#3d8291", ObjB: "#93614d",
		ObjC: "#7d6c3c", LastBg: "#4f8597", LedgeBg: "#2d3c47", CastBg: "#3a4c59",
	},
	"amber": {
		Name: "amber", Fg: "#ffb000", Dim: "#7a5200", Accent: "#ffd75f", TeamA: "#ffd75f", TeamB: "#ff8700",
		Danger: "#ff5f00", Warn: "#ffaf00", Good: "#d7af00", Objective: "#ffff5f", Smoke: "#875f00", Wall: "#5f3f00",
		Cover: "#af8700", Ground: [4]string{"#5f4a1a", "#7a5f22", "#9a782c", "#b8913a"}, Fog: "#3a2a08",
		Cursor: "#875f00", Highlight: "#3a2a00", Border: "#875f00", Board: "#2a1e06",
	},
	"green": {
		Name: "green", Fg: "#33ff33", Dim: "#1a7a1a", Accent: "#aaffaa", TeamA: "#aaffaa", TeamB: "#33cc33",
		Danger: "#ffff55", Warn: "#88ff44", Good: "#33ff33", Objective: "#ffffaa", Smoke: "#227722", Wall: "#0f3f0f",
		Cover: "#22aa22", Ground: [4]string{"#0e3a0e", "#155215", "#1d6a1d", "#268226"}, Fog: "#071f07",
		Cursor: "#1f7f1f", Highlight: "#0f3f0f", Border: "#1f7f1f", Board: "#0c240c",
	},
	"gruvbox": {
		Name: "gruvbox", Fg: "#ebdbb2", Dim: "#928374", Accent: "#fabd2f", TeamA: "#83a598", TeamB: "#fb4934",
		Danger: "#fb4934", Warn: "#fe8019", Good: "#b8bb26", Objective: "#fabd2f", Smoke: "#a89984", Wall: "#3c3836",
		Cover: "#98971a", Ground: [4]string{"#3c3836", "#504945", "#665c54", "#7c6f64"}, Fog: "#282828",
		Cursor: "#d79921", Highlight: "#3c3836", Border: "#665c54", Board: "#32302f",
	},
	"nord": {
		Name: "nord", Fg: "#d8dee9", Dim: "#4c566a", Accent: "#88c0d0", TeamA: "#81a1c1", TeamB: "#bf616a",
		Danger: "#bf616a", Warn: "#d08770", Good: "#a3be8c", Objective: "#ebcb8b", Smoke: "#7b88a1", Wall: "#3b4252",
		Cover: "#8fbcbb", Ground: [4]string{"#2e3440", "#3b4252", "#434c5e", "#4c566a"}, Fog: "#242933",
		Cursor: "#5e81ac", Highlight: "#3b4252", Border: "#4c566a", Board: "#272e3c",
	},
	"catppuccin": {
		Name: "catppuccin", Fg: "#cdd6f4", Dim: "#6c7086", Accent: "#cba6f7", TeamA: "#89b4fa", TeamB: "#f38ba8",
		Danger: "#f38ba8", Warn: "#fab387", Good: "#a6e3a1", Objective: "#f9e2af", Smoke: "#9399b2", Wall: "#313244",
		Cover: "#94e2d5", Ground: [4]string{"#1e1e2e", "#313244", "#45475a", "#585b70"}, Fog: "#181825",
		Cursor: "#b4befe", Highlight: "#313244", Border: "#585b70", Board: "#1c1c2b",
	},
}

// ThemeNames lists the built-in themes.
func ThemeNames() []string {
	var out []string
	for k := range themes {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// GetTheme returns a built-in theme, falling back to gruvbox.
func GetTheme(name string) Theme {
	if t, ok := themes[name]; ok {
		return t
	}
	return themes["gruvbox"]
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
