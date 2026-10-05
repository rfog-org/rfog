package data

import (
	"strings"
	"testing"
	"testing/fstest"
)

func TestLoadEmbedded(t *testing.T) {
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"hask", "wren", "nul", "tally", "mott"} {
		if _, ok := c.Heroes[id]; !ok {
			t.Errorf("hero %s missing", id)
		}
	}
	for _, id := range []string{"lineman", "ranged", "runner", "medic", "junkbot"} {
		if _, ok := c.Units[id]; !ok {
			t.Errorf("unit %s missing", id)
		}
	}
	if c.Rules.Modes["1v1"].Units != 4 {
		t.Errorf("1v1 units = %d", c.Rules.Modes["1v1"].Units)
	}
	if c.Abilities["lunge"].Key != "q" {
		t.Errorf("lunge key = %q", c.Abilities["lunge"].Key)
	}
	// Factions: operators are in play, the scaffolded second faction is not,
	// and every shipped hero belongs to an enabled faction.
	if f, ok := c.Factions["operators"]; !ok || !f.Enabled {
		t.Errorf("operators faction: %+v", f)
	}
	if f, ok := c.Factions["maintainers"]; !ok || f.Enabled {
		t.Errorf("maintainers faction should exist and be disabled: %+v", f)
	}
	if len(c.HeroIDs()) != len(c.Heroes) {
		t.Errorf("%d heroes, %d in play", len(c.Heroes), len(c.HeroIDs()))
	}
	for _, h := range c.Heroes {
		if h.Faction != "operators" {
			t.Errorf("hero %s: faction %q", h.ID, h.Faction)
		}
	}
}

// A hero in a disabled faction loads but never reaches the draft pool.
func TestDisabledFactionHeroesAreOutOfPlay(t *testing.T) {
	m := embeddedFS(t)
	m["factions.toml"] = &fstest.MapFile{Data: []byte(`
[[factions]]
id = "operators"
name = "Operators"
enabled = true

[[factions]]
id = "maintainers"
name = "Maintainers"
enabled = false
`)}
	hero := string(m["heroes/marksman_wren.toml"].Data)
	hero = strings.Replace(hero, `id = "wren"`, "id = \"ghost\"\nfaction = \"maintainers\"", 1)
	m["heroes/zz_ghost.toml"] = &fstest.MapFile{Data: []byte(hero)}
	c, err := LoadFS(m)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := c.Heroes["ghost"]; !ok {
		t.Fatal("hero not loaded")
	}
	for _, id := range c.HeroIDs() {
		if id == "ghost" {
			t.Fatal("disabled faction hero is draftable")
		}
	}
	// An unknown faction is a data error.
	m["heroes/zz_ghost.toml"] = &fstest.MapFile{Data: []byte(strings.Replace(hero, "faction = \"maintainers\"", "faction = \"nobody\"", 1))}
	if _, err := LoadFS(m); err == nil || !strings.Contains(err.Error(), "unknown faction") {
		t.Fatalf("expected unknown faction error, got %v", err)
	}
}

// embeddedFS copies the embedded data files into a mutable FS.
func embeddedFS(t *testing.T) fstest.MapFS {
	t.Helper()
	m := fstest.MapFS{}
	for _, name := range []string{"rules.toml", "factions.toml"} {
		b, _ := files.ReadFile(name)
		m[name] = &fstest.MapFile{Data: b}
	}
	for _, dir := range []string{"units", "heroes", "abilities", "maps"} {
		for _, name := range list(files, dir) {
			b, _ := files.ReadFile(name)
			m[name] = &fstest.MapFile{Data: b}
		}
	}
	return m
}

func TestLoadRejectsBadData(t *testing.T) {
	// Copy the embedded files into a mutable FS and break the map.
	m := fstest.MapFS{}
	for _, name := range []string{"rules.toml", "factions.toml"} {
		b, _ := files.ReadFile(name)
		m[name] = &fstest.MapFile{Data: b}
	}
	for _, dir := range []string{"units", "heroes", "abilities", "maps"} {
		for _, name := range list(files, dir) {
			b, _ := files.ReadFile(name)
			m[name] = &fstest.MapFile{Data: b}
		}
	}
	good := string(m["maps/relay.toml"].Data)
	m["maps/relay.toml"] = &fstest.MapFile{Data: []byte(strings.Replace(good, "tiles = [[7, 2]]", "tiles = [[5, 3]]", 1))}
	if _, err := LoadFS(m); err == nil || !strings.Contains(err.Error(), "objective") {
		t.Fatalf("expected objective mismatch error, got %v", err)
	}
	m["maps/relay.toml"] = &fstest.MapFile{Data: []byte(good)}
	m["abilities/breaker.toml"] = &fstest.MapFile{Data: []byte(strings.Replace(string(m["abilities/breaker.toml"].Data), `kind = "strike"`, `kind = "explode"`, 1))}
	if _, err := LoadFS(m); err == nil || !strings.Contains(err.Error(), "unknown effect") {
		t.Fatalf("expected unknown effect error, got %v", err)
	}
}
