package client

import (
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"

	"rfog/render"
)

// Settings persist in ~/.config/rfog/settings.toml.
type Settings struct {
	Name     string `toml:"name"`
	Theme    string `toml:"theme"`
	Tier     string `toml:"tier"` // auto | t0 | t1 | t2
	AnimMs   int    `toml:"anim_ms"`
	Effects  string `toml:"effects"` // on | off: ambient board effects and title glitch
	Cells    string `toml:"cells"`   // auto | small | large: tile size on the board
	BotLevel string `toml:"bot_level"`
	Hero     string `toml:"hero"`
	Server   string `toml:"server"`
	Token    string `toml:"token"`   // session token from the last server login
	Account  string `toml:"account"` // registered name the token belongs to ("" = guest)
	Seen     string `toml:"seen"`    // last finished match whose result was shown on the menu
}

// DefaultSettings are used when no file exists.
func DefaultSettings() Settings {
	return Settings{Name: "operator", Theme: "fibre", Tier: "auto", AnimMs: 120, Effects: "on", Cells: "auto", BotLevel: "normal", Hero: "hask", Server: "rfog.org"}
}

// ConfigDir returns the config directory, creating it if needed.
func ConfigDir() string {
	base, err := os.UserConfigDir()
	if err != nil || base == "" {
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, ".config")
	}
	dir := filepath.Join(base, "rfog")
	moveOld(filepath.Join(base, "signal"), dir)
	_ = os.MkdirAll(dir, 0o755)
	return dir
}

// moveOld moves a directory from before the rename (the code name was
// "signal") to its new place, once, so settings and replays carry over.
func moveOld(old, dir string) {
	if _, err := os.Stat(dir); err == nil {
		return
	}
	if _, err := os.Stat(old); err == nil {
		_ = os.MkdirAll(filepath.Dir(dir), 0o755)
		_ = os.Rename(old, dir)
	}
}

// DataDir returns where replays are stored.
func DataDir() string {
	home, _ := os.UserHomeDir()
	dir := filepath.Join(home, ".local", "share", "rfog", "replays")
	moveOld(filepath.Join(home, ".local", "share", "signal", "replays"), dir)
	_ = os.MkdirAll(dir, 0o755)
	return dir
}

// LoadSettings reads settings, falling back to defaults for missing fields.
func LoadSettings() Settings {
	s := DefaultSettings()
	b, err := os.ReadFile(filepath.Join(ConfigDir(), "settings.toml"))
	if err != nil {
		return s
	}
	_, _ = toml.Decode(string(b), &s)
	if s.AnimMs <= 0 {
		s.AnimMs = 120
	}
	if s.Effects == "" {
		s.Effects = "on"
	}
	if s.Cells == "" {
		s.Cells = "auto"
	}
	return s
}

// CellsOrAuto is the tile size preference, defaulting to auto.
func (s Settings) CellsOrAuto() string {
	if s.Cells == "" {
		return "auto"
	}
	return s.Cells
}

// EffectsOn reports whether ambient effects are enabled.
func (s Settings) EffectsOn() bool { return s.Effects != "off" }

// Save writes settings.
func (s Settings) Save() error {
	f, err := os.Create(filepath.Join(ConfigDir(), "settings.toml"))
	if err != nil {
		return err
	}
	defer f.Close()
	return toml.NewEncoder(f).Encode(s)
}

// themeFile is the on-disk custom theme format (~/.config/rfog/theme.toml).
// light and dark (and optionally wall) make a board the way the built-in
// ones are made (render.BoardTheme); any other key overrides one colour.
type themeFile struct {
	Name      string    `toml:"name"`
	Mono      bool      `toml:"mono"`
	Light     string    `toml:"light"`
	Dark      string    `toml:"dark"`
	Fg        string    `toml:"fg"`
	Dim       string    `toml:"dim"`
	Accent    string    `toml:"accent"`
	TeamA     string    `toml:"team_a"`
	TeamB     string    `toml:"team_b"`
	Danger    string    `toml:"danger"`
	Warn      string    `toml:"warn"`
	Good      string    `toml:"good"`
	Objective string    `toml:"objective"`
	Smoke     string    `toml:"smoke"`
	Wall      string    `toml:"wall"`
	Cover     string    `toml:"cover"`
	Ground    [4]string `toml:"ground"`
	Fog       string    `toml:"fog"`
	Cursor    string    `toml:"cursor"`
	Highlight string    `toml:"highlight"`
	Border    string    `toml:"border"`
}

// LoadUserTheme registers ~/.config/rfog/theme.toml if present and returns
// its name.
func LoadUserTheme() string {
	b, err := os.ReadFile(filepath.Join(ConfigDir(), "theme.toml"))
	if err != nil {
		return ""
	}
	var tf themeFile
	if _, err := toml.Decode(string(b), &tf); err != nil || tf.Name == "" {
		return ""
	}
	t := render.GetTheme("fibre")
	if tf.Light != "" && tf.Dark != "" {
		t = render.BoardTheme(render.Board{Name: tf.Name, Light: tf.Light, Dark: tf.Dark, Wall: or(tf.Wall, t.Wall)})
	}
	t.Name, t.Mono = tf.Name, tf.Mono
	for _, f := range []struct {
		to *string
		v  string
	}{
		{&t.Fg, tf.Fg}, {&t.Dim, tf.Dim}, {&t.Accent, tf.Accent}, {&t.TeamA, tf.TeamA}, {&t.TeamB, tf.TeamB},
		{&t.Danger, tf.Danger}, {&t.Warn, tf.Warn}, {&t.Good, tf.Good}, {&t.Objective, tf.Objective},
		{&t.Smoke, tf.Smoke}, {&t.Wall, tf.Wall}, {&t.Cover, tf.Cover}, {&t.Fog, tf.Fog}, {&t.Cursor, tf.Cursor},
		{&t.Highlight, tf.Highlight}, {&t.Border, tf.Border},
		{&t.Ground[0], tf.Ground[0]}, {&t.Ground[1], tf.Ground[1]}, {&t.Ground[2], tf.Ground[2]}, {&t.Ground[3], tf.Ground[3]},
	} {
		*f.to = or(f.v, *f.to)
	}
	render.RegisterTheme(t)
	return t.Name
}

func or(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
