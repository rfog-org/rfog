// Package data embeds the rules, units, heroes, abilities and maps and loads
// them into engine.Content. This is the only place TOML is parsed.
package data

import (
	"embed"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"

	"rfog/engine"
)

//go:embed factions.toml rules.toml units/*.toml heroes/*.toml abilities/*.toml maps/*.toml
var files embed.FS

// FS exposes the embedded data files (for tools).
func FS() fs.FS { return files }

type rulesFile struct {
	MaxTurns               int            `toml:"max_turns"`
	WinScore               int            `toml:"win_score"`
	CommanderOrders        int            `toml:"commander_orders"`
	CommanderDeathPoints   int            `toml:"commander_death_points"`
	RespawnBase            int            `toml:"respawn_base"`
	RespawnCap             int            `toml:"respawn_cap"`
	CoverDefBonus          int            `toml:"cover_def_bonus"`
	HoldDefBonus           int            `toml:"hold_def_bonus"`
	MaxDef                 int            `toml:"max_def"`
	HeightRangeBonus       int            `toml:"height_range_bonus"`
	VisionPerZ             int            `toml:"vision_per_z"`
	FallThreshold          int            `toml:"fall_threshold"`
	FallDamagePerZ         int            `toml:"fall_damage_per_z"`
	PushBlockDamage        int            `toml:"push_block_damage"`
	PushMaxClimb           int            `toml:"push_max_climb"`
	XPKillUnit             int            `toml:"xp_kill_unit"`
	XPKillCommander        int            `toml:"xp_kill_commander"`
	XPObjectiveAdjacent    int            `toml:"xp_objective_adjacent"`
	SummonKillCredit       bool           `toml:"summon_kill_credit"`
	LevelThresholds        []int          `toml:"level_thresholds"`
	CooldownReductionLevel int            `toml:"cooldown_reduction_level"`
	MinCooldown            int            `toml:"min_cooldown"`
	Unlock                 map[string]int `toml:"unlock"`
	Scoring                string         `toml:"scoring"`
	Modes                  map[string]struct {
		PlayersPerTeam int    `toml:"players_per_team"`
		Units          int    `toml:"units"`
		Map            string `toml:"map"`
		WinScore       int    `toml:"win_score"`
	} `toml:"modes"`
	UnitSet []string `toml:"unit_set"`
}

type unitFile struct {
	ID                  string   `toml:"id"`
	Name                string   `toml:"name"`
	HP                  int      `toml:"hp"`
	MV                  int      `toml:"mv"`
	JMP                 int      `toml:"jmp"`
	RNG                 int      `toml:"rng"`
	ATK                 int      `toml:"atk"`
	DEF                 int      `toml:"def"`
	INI                 int      `toml:"ini"`
	VIS                 int      `toml:"vis"`
	Abilities           []string `toml:"abilities"`
	HoldDefBonus        int      `toml:"hold_def_bonus"`
	AdjacentDicePenalty int      `toml:"adjacent_dice_penalty"`
	IgnoreHeightCost    bool     `toml:"ignore_height_cost"`
	Expires             int      `toml:"expires"`
}

// defaultFaction is what a hero or unit file that names no faction gets.
const defaultFaction = "operators"

type factionsFile struct {
	Factions []struct {
		ID      string `toml:"id"`
		Name    string `toml:"name"`
		Blurb   string `toml:"blurb"`
		Enabled bool   `toml:"enabled"`
	} `toml:"factions"`
}

type heroFile struct {
	ID        string            `toml:"id"`
	Name      string            `toml:"name"`
	Faction   string            `toml:"faction"`
	Class     string            `toml:"class"`
	Role      string            `toml:"role"`
	HP        int               `toml:"hp"`
	MV        int               `toml:"mv"`
	JMP       int               `toml:"jmp"`
	RNG       int               `toml:"rng"`
	ATK       int               `toml:"atk"`
	DEF       int               `toml:"def"`
	INI       int               `toml:"ini"`
	VIS       int               `toml:"vis"`
	Abilities map[string]string `toml:"abilities"`
}

type effectFile struct {
	Kind   string `toml:"kind"`
	N      int    `toml:"n"`
	Dist   int    `toml:"dist"`
	Radius *int   `toml:"radius"`
	Turns  int    `toml:"turns"`
	Stat   string `toml:"stat"`
	Delta  int    `toml:"delta"`
	Unit   string `toml:"unit"`
	Count  int    `toml:"count"`
	Dice   int    `toml:"dice"`
	MV     int    `toml:"mv"`
	RNG    int    `toml:"rng"`
	Shots  int    `toml:"shots"`
	Who    string `toml:"who"`
	To     string `toml:"to"`
	Filter string `toml:"filter"`
}

type abilityFile struct {
	Abilities []struct {
		ID       string       `toml:"id"`
		Name     string       `toml:"name"`
		Target   string       `toml:"target"`
		Range    int          `toml:"range"`
		Area     int          `toml:"area"`
		Filter   string       `toml:"filter"`
		Kind     string       `toml:"kind"`
		Delay    int          `toml:"delay"`
		Cooldown int          `toml:"cooldown"`
		Visible  *bool        `toml:"visible"`
		Effects  []effectFile `toml:"effects"`
	} `toml:"abilities"`
}

type mapFile struct {
	ID         string   `toml:"id"`
	Name       string   `toml:"name"`
	Width      int      `toml:"width"`
	Height     int      `toml:"height"`
	DeployCols int      `toml:"deploy_cols"`
	Terrain    []string `toml:"terrain"`
	HeightRows []string `toml:"elevation"`
	Objectives []struct {
		ID    string   `toml:"id"`
		Tiles [][2]int `toml:"tiles"`
	} `toml:"objectives"`
}

// Load parses the embedded data into engine content.
func Load() (*engine.Content, error) {
	return LoadFS(files)
}

// LoadFS parses data from any filesystem with the same layout (for tests and
// balance tools that patch numbers).
func LoadFS(fsys fs.FS) (*engine.Content, error) {
	c := &engine.Content{
		Units:     map[string]engine.UnitDef{},
		Heroes:    map[string]engine.HeroDef{},
		Abilities: map[string]engine.AbilityDef{},
		Maps:      map[string]engine.MapDef{},
		Factions:  map[string]engine.Faction{},
	}
	var ff factionsFile
	if err := decode(fsys, "factions.toml", &ff); err != nil {
		return nil, err
	}
	for _, f := range ff.Factions {
		c.Factions[f.ID] = engine.Faction{ID: f.ID, Name: f.Name, Blurb: f.Blurb, Enabled: f.Enabled}
	}
	var rf rulesFile
	if err := decode(fsys, "rules.toml", &rf); err != nil {
		return nil, err
	}
	c.Rules = engine.Rules{
		MaxTurns: rf.MaxTurns, WinScore: rf.WinScore, CommanderOrders: rf.CommanderOrders,
		CommanderDeathPoints: rf.CommanderDeathPoints, RespawnBase: rf.RespawnBase, RespawnCap: rf.RespawnCap,
		CoverDefBonus: rf.CoverDefBonus, HoldDefBonus: rf.HoldDefBonus, MaxDef: rf.MaxDef,
		HeightRangeBonus: rf.HeightRangeBonus, VisionPerZ: rf.VisionPerZ, FallThreshold: rf.FallThreshold,
		FallDamagePerZ: rf.FallDamagePerZ, PushBlockDamage: rf.PushBlockDamage, PushMaxClimb: rf.PushMaxClimb,
		XPKillUnit: rf.XPKillUnit, XPKillCommander: rf.XPKillCommander, XPObjectiveAdjacent: rf.XPObjectiveAdjacent,
		SummonKillCredit: rf.SummonKillCredit,
		LevelThresholds:  rf.LevelThresholds, CooldownReductionLevel: rf.CooldownReductionLevel,
		MinCooldown: rf.MinCooldown, Unlock: rf.Unlock, Scoring: rf.Scoring, Modes: map[string]engine.ModeDef{}, UnitSet: rf.UnitSet,
	}
	for k, m := range rf.Modes {
		c.Rules.Modes[k] = engine.ModeDef{PlayersPerTeam: m.PlayersPerTeam, Units: m.Units, Map: m.Map, WinScore: m.WinScore}
	}
	for _, name := range list(fsys, "units") {
		var uf unitFile
		if err := decode(fsys, name, &uf); err != nil {
			return nil, err
		}
		c.Units[uf.ID] = engine.UnitDef{
			ID: uf.ID, Name: uf.Name,
			Stats:     engine.Stats{HP: uf.HP, MV: uf.MV, JMP: uf.JMP, RNG: uf.RNG, ATK: uf.ATK, DEF: uf.DEF, INI: uf.INI, VIS: uf.VIS},
			Abilities: uf.Abilities, HoldDefBonus: uf.HoldDefBonus, AdjacentDicePenalty: uf.AdjacentDicePenalty,
			IgnoreHeightCost: uf.IgnoreHeightCost, Expires: uf.Expires,
		}
	}
	for _, name := range list(fsys, "heroes") {
		var hf heroFile
		if err := decode(fsys, name, &hf); err != nil {
			return nil, err
		}
		if hf.Faction == "" {
			hf.Faction = defaultFaction
		}
		c.Heroes[hf.ID] = engine.HeroDef{
			ID: hf.ID, Name: hf.Name, Faction: hf.Faction, Class: hf.Class, Role: hf.Role,
			Stats:     engine.Stats{HP: hf.HP, MV: hf.MV, JMP: hf.JMP, RNG: hf.RNG, ATK: hf.ATK, DEF: hf.DEF, INI: hf.INI, VIS: hf.VIS},
			Abilities: hf.Abilities,
		}
	}
	for _, name := range list(fsys, "abilities") {
		var af abilityFile
		if err := decode(fsys, name, &af); err != nil {
			return nil, err
		}
		for _, a := range af.Abilities {
			if _, dup := c.Abilities[a.ID]; dup {
				return nil, fmt.Errorf("%s: duplicate ability %q", name, a.ID)
			}
			ad := engine.AbilityDef{
				ID: a.ID, Name: a.Name, Target: a.Target, Range: a.Range, Area: a.Area, Filter: a.Filter,
				Kind: a.Kind, Delay: a.Delay, Cooldown: a.Cooldown, Visible: a.Visible,
			}
			for _, e := range a.Effects {
				ad.Effects = append(ad.Effects, engine.EffectDef{
					Kind: e.Kind, N: e.N, Dist: e.Dist, Radius: e.Radius, Turns: e.Turns, Stat: e.Stat,
					Delta: e.Delta, Unit: e.Unit, Count: e.Count, Dice: e.Dice, MV: e.MV, RNG: e.RNG,
					Shots: e.Shots, Who: e.Who, To: e.To, Filter: e.Filter,
				})
			}
			c.Abilities[a.ID] = ad
		}
	}
	for _, name := range list(fsys, "maps") {
		var mf mapFile
		if err := decode(fsys, name, &mf); err != nil {
			return nil, err
		}
		md, err := parseMap(mf)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		c.Maps[md.ID] = md
	}
	// Fill hero ability keys.
	for _, h := range c.Heroes {
		for k, id := range h.Abilities {
			a := c.Abilities[id]
			a.Key = k
			c.Abilities[id] = a
		}
	}
	if err := c.Validate(); err != nil {
		return nil, err
	}
	return c, nil
}

func parseMap(mf mapFile) (engine.MapDef, error) {
	md := engine.MapDef{ID: mf.ID, Name: mf.Name, Width: mf.Width, Height: mf.Height, DeployCols: mf.DeployCols}
	if len(mf.Terrain) != mf.Height || len(mf.HeightRows) != mf.Height {
		return md, fmt.Errorf("map %s: need %d terrain and height rows", mf.ID, mf.Height)
	}
	for y := 0; y < mf.Height; y++ {
		tr, hr := mf.Terrain[y], mf.HeightRows[y]
		if len(tr) != mf.Width || len(hr) != mf.Width {
			return md, fmt.Errorf("map %s: row %d has wrong width", mf.ID, y)
		}
		for x := 0; x < mf.Width; x++ {
			t := engine.Tile{}
			switch tr[x] {
			case '.':
				t.Terrain = engine.TerrainOpen
			case '#':
				t.Terrain = engine.TerrainWall
			case 'c':
				t.Terrain = engine.TerrainCover
			case 'o':
				t.Terrain = engine.TerrainObjective
			default:
				return md, fmt.Errorf("map %s: bad terrain %q at %d,%d", mf.ID, tr[x], x, y)
			}
			if hr[x] < '0' || hr[x] > '3' {
				return md, fmt.Errorf("map %s: bad height %q at %d,%d", mf.ID, hr[x], x, y)
			}
			t.Z = int(hr[x] - '0')
			md.Tiles = append(md.Tiles, t)
		}
	}
	for _, o := range mf.Objectives {
		ob := engine.Objective{ID: o.ID, Holder: -1}
		for _, t := range o.Tiles {
			ob.Tiles = append(ob.Tiles, engine.Pos{X: t[0], Y: t[1]})
		}
		md.Objectives = append(md.Objectives, ob)
	}
	return md, nil
}

func decode(fsys fs.FS, name string, v any) error {
	b, err := fs.ReadFile(fsys, name)
	if err != nil {
		return err
	}
	if _, err := toml.Decode(string(b), v); err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	return nil
}

func list(fsys fs.FS, dir string) []string {
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".toml") {
			out = append(out, path.Join(dir, e.Name()))
		}
	}
	sort.Strings(out)
	return out
}
