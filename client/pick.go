package client

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"rfog/engine"
)

type matchKind int

const (
	matchBots matchKind = iota
	matchHotseat
)

// pickScreen chooses heroes (and bot level) before a local match.
type pickScreen struct {
	kind   matchKind
	heroes []string
	sel    int
	picks  []string // chosen so far
	level  string
	levels []string
	lvlSel int
}

func newPickScreen(a *App, kind matchKind) *pickScreen {
	p := &pickScreen{kind: kind, heroes: a.c.HeroIDs(), levels: []string{"easy", "normal", "hard"}, level: a.set.BotLevel}
	for i, h := range p.heroes {
		if h == a.set.Hero {
			p.sel = i
		}
	}
	for i, l := range p.levels {
		if l == p.level {
			p.lvlSel = i
		}
	}
	return p
}

// choose takes the highlighted hero and, once every seat is picked,
// starts the match. Shared by the keyboard and a tap.
func (p *pickScreen) choose(a *App) (screen, tea.Cmd) {
	p.picks = append(p.picks, p.heroes[p.sel])
	if len(p.picks) == 1 {
		a.set.Hero = p.heroes[p.sel]
		a.set.BotLevel = p.level
		a.saveSettings()
	}
	need := 1
	if p.kind == matchHotseat {
		need = 2
	}
	if len(p.picks) < need {
		return p, nil
	}
	seed := uint64(time.Now().UnixNano())
	var lm *localMatch
	var err error
	if p.kind == matchHotseat {
		lm, err = newLocalMatch(a.c, engine.Setup{ID: "hotseat", Mode: "1v1", Seed: seed, TimeControl: "bots",
			Players: []engine.SetupPlayer{{Name: "player 1", Hero: p.picks[0]}, {Name: "player 2", Hero: p.picks[1]}}},
			[2]bool{false, false}, p.level)
	} else {
		// The bot picks a hero from the seed; mirror is allowed in 1v1.
		rng := engine.NewRNG(seed, 1)
		bh := p.heroes[rng.Intn(len(p.heroes))]
		lm, err = newLocalMatch(a.c, engine.Setup{ID: "bots", Mode: "1v1", Seed: seed, TimeControl: "bots",
			Players: []engine.SetupPlayer{{Name: a.set.Name, Hero: p.picks[0]}, {Name: "bot " + bh, Hero: bh}}},
			[2]bool{false, true}, p.level)
	}
	if err != nil {
		return newMenuScreen(), nil
	}
	return newMatchScreen(a, lm), tick(50)
}

func (p *pickScreen) whoPicks() string {
	if p.kind == matchHotseat {
		return fmt.Sprintf("player %d", len(p.picks)+1)
	}
	return "you"
}

func (p *pickScreen) update(a *App, msg tea.Msg) (screen, tea.Cmd) {
	if mm, ok := msg.(tea.MouseMsg); ok {
		if id, hit, act := a.pointer(mm, p.sel); hit {
			p.sel = id
			if act {
				return p.choose(a)
			}
		}
		return p, nil
	}
	k, ok := msg.(tea.KeyMsg)
	if !ok {
		return p, nil
	}
	switch {
	case isKey(k, "j", "down"):
		p.sel = (p.sel + 1) % len(p.heroes)
	case isKey(k, "k", "up"):
		p.sel = (p.sel + len(p.heroes) - 1) % len(p.heroes)
	case isKey(k, "h", "left"):
		p.lvlSel = (p.lvlSel + len(p.levels) - 1) % len(p.levels)
		p.level = p.levels[p.lvlSel]
	case isKey(k, "l", "right"):
		p.lvlSel = (p.lvlSel + 1) % len(p.levels)
		p.level = p.levels[p.lvlSel]
	case isKey(k, "enter", " "):
		return p.choose(a)
	case isKey(k, "q", "esc"):
		return newMenuScreen(), nil
	}
	return p, nil
}

func (p *pickScreen) view(a *App) string {
	var lines []string
	title := "draft"
	if p.kind == matchHotseat {
		title = "hotseat draft"
	} else {
		title = "vs bots"
	}
	lines = append(lines, a.st.Title.Render(title), "")
	lines = append(lines, fmt.Sprintf("%s: pick a commander", p.whoPicks()), "")
	for i, id := range p.heroes {
		a.clickRow(len(lines), i)
		h := a.c.Heroes[id]
		cur := "  "
		name := fmt.Sprintf("%-6s %-9s %s", h.Name, h.Class, h.Role)
		if i == p.sel {
			cur = a.st.Accent.Render("> ")
			name = a.st.Accent.Render(name)
		}
		lines = append(lines, cur+name)
	}
	h := a.c.Heroes[p.heroes[p.sel]]
	lines = append(lines, "", a.st.Dim.Render(fmt.Sprintf("HP %d  MV %d  JMP %d  RNG %d  ATK %d  DEF %d  INI %d  VIS %d",
		h.HP, h.MV, h.JMP, h.RNG, h.ATK, h.DEF, h.INI, h.VIS)))
	for _, key := range []string{"q", "w", "e", "r"} {
		ab := a.c.Abilities[h.Abilities[key]]
		lines = append(lines, fmt.Sprintf("  %s  %-14s %s", a.st.Key.Render(strings.ToUpper(key)), ab.Name, describeAbility(a.c, ab)))
	}
	if p.kind == matchBots {
		lines = append(lines, "", fmt.Sprintf("bot level: %s %s %s", a.st.Dim.Render("<"), a.st.Accent.Render(p.level), a.st.Dim.Render(">")))
	}
	if len(p.picks) > 0 {
		lines = append(lines, "", a.st.Dim.Render("picked: "+strings.Join(p.picks, ", ")))
	}
	lines = append(lines, "", a.st.Dim.Render("j/k choose  h/l bot level  enter pick  esc back"))
	return a.centered(lines)
}

// describeAbility gives a terse, data-derived summary of an ability.
func describeAbility(c *engine.Content, ab engine.AbilityDef) string {
	var parts []string
	for _, e := range ab.Effects {
		switch e.Kind {
		case "damage":
			parts = append(parts, fmt.Sprintf("dmg %d", e.N))
		case "heal":
			parts = append(parts, fmt.Sprintf("heal %d", e.N))
		case "push":
			parts = append(parts, fmt.Sprintf("push %d", e.Dist))
		case "pull":
			parts = append(parts, fmt.Sprintf("pull %d", e.Dist))
		case "shield":
			parts = append(parts, fmt.Sprintf("shield %d/%dt", e.N, e.Turns))
		case "reveal":
			parts = append(parts, fmt.Sprintf("reveal r%d/%dt", radius(e, ab), e.Turns))
		case "smoke":
			parts = append(parts, fmt.Sprintf("smoke r%d/%dt", radius(e, ab), e.Turns))
		case "slow":
			parts = append(parts, fmt.Sprintf("slow %d/%dt", e.MV, e.Turns))
		case "root", "silence", "cloak", "taunt":
			parts = append(parts, fmt.Sprintf("%s %dt", e.Kind, e.Turns))
		case "stat":
			parts = append(parts, fmt.Sprintf("%s%+d/%dt", strings.ToUpper(e.Stat), e.Delta, e.Turns))
		case "mark":
			parts = append(parts, fmt.Sprintf("mark +%d/%dt", e.Dice, e.Turns))
		case "spawn":
			parts = append(parts, fmt.Sprintf("spawn %dx%s", maxInt(e.Count, 1), e.Unit))
		case "overwatch":
			parts = append(parts, fmt.Sprintf("overwatch rng%d x%d", e.RNG, e.Shots))
		case "teleport":
			if e.Who == "target" {
				parts = append(parts, "teleport ally")
			} else {
				parts = append(parts, "teleport")
			}
		case "approach":
			parts = append(parts, fmt.Sprintf("dash %d", e.Dist))
		case "strike":
			parts = append(parts, "attack")
		case "sacrifice":
			parts = append(parts, "sacrifice")
		}
	}
	tgt := ab.Target
	if ab.Range > 0 {
		tgt += fmt.Sprintf(" r%d", ab.Range)
	}
	if ab.Area > 0 {
		tgt += fmt.Sprintf(" %dx%d", ab.Area*2+1, ab.Area*2+1)
	}
	d := ""
	if ab.Delay > 0 {
		d = fmt.Sprintf(" delay %d", ab.Delay)
	}
	return fmt.Sprintf("%s;%s cd%d: %s", tgt, d, ab.Cooldown, strings.Join(parts, ", "))
}

func radius(e engine.EffectDef, ab engine.AbilityDef) int {
	if e.Radius != nil {
		return *e.Radius
	}
	return ab.Area
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
