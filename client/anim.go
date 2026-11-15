package client

import (
	"fmt"
	"strconv"

	"rfog/engine"
	"rfog/engine/textlog"
	"rfog/render"
)

// anim replays a turn's events onto a scene, one beat per tick, starting
// from the viewer's pre-turn view and ending on their post-turn view.
type anim struct {
	c      *engine.Content
	g      render.Glyphs
	post   *engine.State // post-turn view, for unit details and names
	scene  render.Scene
	events []engine.Event
	i      int
	sub    int
	log    []string
	done   bool
	scores [2]int
	// estimate is roughly how many beats this turn will take, used to
	// keep a busy turn from taking half a minute to watch.
	estimate int
	// team and player are the viewer's (player -1 watching); names are how
	// a callout names the viewer's side and the other ("you", "the bot").
	team, player int
	names        [2]string
	// call is the callout over the board, if any; hold is how long the
	// beat just applied should stay up at least, in milliseconds.
	call *callout
	hold int
	held bool // a callout has held a beat this turn
}

// callout is a big moment said across the board, in the colour of the side
// it favours (0 the viewer's), for a few beats: a commander down, an
// ultimate, the lead changing hands, match point. Worded as the web app's.
type callout struct {
	text, sub string
	side      int
	left      int // beats it has still to show
}

// callBeats is how many beats a callout stays up, and callHold how long
// the beat that raises the turn's first callout holds (the later ones in
// a busy turn ride its beats, or a turn would take seconds to watch).
const callBeats, callHold = 4, 600

// say raises a callout, replacing any other.
func (an *anim) say(text, sub string, side int) {
	an.call = &callout{text: text, sub: sub, side: side, left: callBeats}
	if !an.held {
		an.hold, an.held = callHold, true
	}
}

// side is 0 for the viewer's team, 1 for the other.
func (an *anim) side(team int) int {
	if team == an.team {
		return 0
	}
	return 1
}

// heroName is a commander's hero as players know it.
func (an *anim) heroName(kind string) string {
	if h, ok := an.c.Heroes[kind]; ok {
		return h.Name
	}
	return kind
}

// scored says what a side's points did to the match: match point, or the
// lead changing hands, as the web app says them.
func (an *anim) scored(team, n int) {
	if n <= 0 || team < 0 || team > 1 {
		return
	}
	you := an.team
	was := an.scores[you] - an.scores[1-you]
	before := an.scores[team]
	an.scores[team] += n
	lead := an.scores[you] - an.scores[1-you]
	took := ""
	if lead > 0 && was <= 0 || lead < 0 && was >= 0 {
		who := an.names[0]
		if lead < 0 {
			who = an.names[1]
		}
		took = who + " takes the lead"
		if who == "you" || who == "they" || who == "the bots" {
			took = who + " take the lead"
		}
	}
	win := an.post.Match.WinScore
	switch {
	case an.scores[team] >= win-1 && before < win-1 && an.scores[team] < win:
		sub := took
		if sub == "" {
			sub = fmt.Sprintf("%s · first to %d", an.names[an.side(team)], win)
		}
		an.say("match point", sub, an.side(team))
	case took != "":
		an.say(took, "", map[bool]int{true: 0, false: 1}[lead > 0])
	}
}

// turnBudget is how long a whole turn's resolution should take. More
// fighting means more events, and at a fixed pace per event a busy turn
// crawled; the pace now scales so the turn lands near this.
const turnBudget = 1300

// minBeat keeps the fastest pace readable.
const minBeat = 35

// pace returns the milliseconds a beat should take, given the player's
// preferred pace as the ceiling.
func (an *anim) pace(prefMs int) int {
	if prefMs <= 0 || an.estimate <= 0 {
		return prefMs
	}
	ms := turnBudget / an.estimate
	if ms > prefMs {
		ms = prefMs
	}
	if ms < minBeat {
		ms = minBeat
	}
	return ms
}

func newAnim(c *engine.Content, g render.Glyphs, pre, post *engine.State, team int, events []engine.Event) *anim {
	an := &anim{c: c, g: g, post: post, events: events, team: team, player: -1, names: [2]string{"you", "they"}}
	an.scene = render.SceneFromView(pre, team)
	// Visibility and ghosts from the post-turn view: the events were filtered
	// against it, so anything they mention is fair to show.
	postScene := render.SceneFromView(post, team)
	an.scene.Visible = postScene.Visible
	an.scene.Ghosts = postScene.Ghosts
	an.scene.Telegraph = map[engine.Pos]bool{}
	for p := range postScene.Telegraph {
		_ = p
	}
	// Keep telegraphs that existed before the turn; new ones arrive as events.
	preScene := render.SceneFromView(pre, team)
	preScene.TelegraphArea(c, pre)
	an.scene.Telegraph = preScene.Telegraph
	an.scene.Flash = map[engine.Pos]render.Flash{}
	for i, t := range pre.Teams {
		if i < 2 {
			an.scores[i] = t.Score
		}
	}
	if len(events) == 0 {
		an.done = true
	}
	// A move is one beat per tile, everything else about one.
	for _, e := range events {
		switch e.Kind {
		case engine.EvMoved:
			an.estimate += len(e.Path)
		case engine.EvTurnStart, engine.EvTurnEnd, engine.EvDamaged, engine.EvStatus, engine.EvOrderRejected:
			// Folded into another beat or silent.
		default:
			an.estimate++
		}
	}
	return an
}

// perTeam is how many players a side has.
func (an *anim) perTeam() int {
	n := 0
	for _, p := range an.post.Players {
		if p.Team == an.team {
			n++
		}
	}
	return n
}

func (an *anim) unit(id int) *render.SceneUnit {
	for i := range an.scene.Units {
		if an.scene.Units[i].ID == id {
			return &an.scene.Units[i]
		}
	}
	return nil
}

// ensure adds a unit from the post view if the scene does not have it yet.
func (an *anim) ensure(id int) *render.SceneUnit {
	if u := an.unit(id); u != nil {
		return u
	}
	pu := an.post.Unit(id)
	if pu == nil {
		return nil
	}
	an.scene.Units = append(an.scene.Units, render.SceneUnit{ID: pu.ID, Team: pu.Team, Kind: pu.Kind, Commander: pu.IsCommander, Pos: pu.Pos, HP: pu.HP, MaxHP: pu.MaxHP})
	return &an.scene.Units[len(an.scene.Units)-1]
}

func (an *anim) remove(id int) {
	for i := range an.scene.Units {
		if an.scene.Units[i].ID == id {
			an.scene.Units = append(an.scene.Units[:i], an.scene.Units[i+1:]...)
			return
		}
	}
}

// beat applies the next beat. Returns true when the animation has finished.
// Silent events (turn markers, statuses) are folded into the same beat.
func (an *anim) beat() bool {
	an.decay()
	an.hold = 0
	if an.call != nil {
		if an.call.left--; an.call.left <= 0 {
			an.call = nil
		}
	}
	for i := range an.scene.Units { // poses last one beat
		an.scene.Units[i].Pose = ""
	}
	for !an.done {
		if an.i >= len(an.events) {
			an.done = true
			break
		}
		e := an.events[an.i]
		visible := an.apply(e)
		if visible {
			// The damage a blow does lands in the blow's beat, its number
			// over the target.
			for an.i < len(an.events) && an.events[an.i].Kind == engine.EvDamaged {
				an.apply(an.events[an.i])
			}
			return an.done
		}
	}
	return an.done
}

// decay starts a beat: flashes go, except light falling off a square,
// which steps down its dither (▓▒░) one beat at a time.
func (an *anim) decay() {
	old := an.scene.Flash
	an.scene.Flash = map[engine.Pos]render.Flash{}
	for p, f := range old {
		if f.Fade > 1 && an.unit0(p) == nil {
			an.scene.Flash[p] = render.Flash{Role: f.Role, Fade: f.Fade - 1}
		}
	}
}

// fresh is the flash this beat put on p, if any (not light fading off).
func (an *anim) fresh(p engine.Pos) (render.Flash, bool) {
	f, ok := an.scene.Flash[p]
	return f, ok && f.Fade == 0
}

// skip applies everything remaining at once.
func (an *anim) skip() {
	for !an.done {
		an.beat()
	}
	an.scene.Flash = map[engine.Pos]render.Flash{}
}

// apply applies one event (or one sub-step of a move). It returns true if
// the beat should be shown before continuing.
func (an *anim) apply(e engine.Event) bool {
	logLine := func() {
		if l := textlog.Event(an.c, an.post, e); l != "" {
			an.log = append(an.log, l)
		}
	}
	next := func() { an.i++; an.sub = 0 }
	switch e.Kind {
	case engine.EvTurnStart, engine.EvTurnEnd:
		next()
		return false
	case engine.EvMoved:
		u := an.ensure(e.Unit)
		if u == nil || len(e.Path) == 0 {
			next()
			return false
		}
		if an.sub == 0 {
			logLine()
		}
		u.Pos = e.Path[an.sub]
		an.sub++
		if an.sub >= len(e.Path) {
			next()
		}
		return true
	case engine.EvTeleported, engine.EvPushed:
		if u := an.ensure(e.Unit); u != nil {
			u.Pos = e.To
			an.scene.Flash[e.To] = render.Flash{Mark: "*", Role: "hit"}
		}
		logLine()
		next()
		return true
	case engine.EvAttacked:
		f := render.Flash{Mark: "x", Role: "miss"}
		if e.Hits > 0 {
			f = render.Flash{Mark: "*", Role: "hit"}
		}
		an.scene.Flash[e.To] = f
		// The attacker strikes its pose; a shot from range crosses the
		// squares between in the attacker's colour, as on the web board.
		if u := an.ensure(e.Unit); u != nil {
			u.Pose = "attack"
			role := "beamA"
			if u.Team != 0 {
				role = "beamB"
			}
			for _, p := range between(u.Pos, e.To) {
				if _, taken := an.fresh(p); !taken && an.unit0(p) == nil {
					an.scene.Flash[p] = render.Flash{Mark: an.g.Beam, Role: role}
				}
			}
		}
		logLine()
		next()
		return true
	case engine.EvDamaged:
		// Damage belongs to the blow that caused it: it shares that beat
		// rather than costing one of its own.
		if u := an.ensure(e.Target); u != nil {
			u.HP -= e.Amount
			if u.HP < 0 {
				u.HP = 0
			}
			f, ok := an.fresh(u.Pos)
			if !ok {
				f = render.Flash{Mark: "-", Role: "hit"}
			}
			f.Text = addAmount(f.Text, -e.Amount)
			if u.HP == 0 {
				f.Role = "kill" // the killing blow: the number red on white
			}
			an.scene.Flash[u.Pos] = f
		}
		logLine()
		next()
		return false
	case engine.EvHealed:
		if u := an.ensure(e.Target); u != nil {
			u.HP += e.Amount
			if u.HP > u.MaxHP {
				u.HP = u.MaxHP
			}
			an.scene.Flash[u.Pos] = render.Flash{Mark: "+", Role: "heal", Text: fmt.Sprintf("+%d", e.Amount)}
		}
		logLine()
		next()
		return true
	case engine.EvDied, engine.EvExpired:
		// A kill is three beats, as on the web board: the blow lands and the
		// figure holds on a white square, it is struck out and the squares
		// around take the shock (two deep for a commander), then only dust
		// remains. Something that runs out of time skips the first.
		switch u := an.unit(e.Unit); {
		case an.sub == 0 && e.Kind == engine.EvDied && u != nil:
			an.scene.Flash[e.From] = render.Flash{Mark: an.g.Death, Role: "kill"}
			logLine()
			an.sub = 1
			if u.Commander {
				pu := an.post.Unit(u.ID)
				switch {
				case u.Team != an.team:
					an.say("enemy commander down", an.heroName(u.Kind), 0)
				case pu == nil || an.player < 0 || pu.Owner == an.player:
					an.say("commander down", an.heroName(u.Kind), 1)
				default:
					an.say("ally commander down", an.heroName(u.Kind), 1)
				}
			}
		case an.sub < 2:
			if an.sub == 0 {
				logLine()
			} else {
				an.ring(e.From, 1, an.g.Dust, "dust")
				if u != nil && u.Commander {
					an.ring(e.From, 2, an.g.Dust, "dust")
				}
			}
			an.remove(e.Unit)
			an.scene.Flash[e.From] = render.Flash{Mark: an.g.Death, Role: "death"}
			an.sub = 2
		default:
			an.scene.Flash[e.From] = render.Flash{Mark: an.g.Dust, Role: "dust", Fade: 2}
			next()
		}
		return true
	case engine.EvAbilityCast:
		// An ultimate is felt across the board: after the cast's own beat
		// its ring widens from the caster for two more.
		if an.sub > 0 {
			an.ring(e.From, an.sub, an.g.Cast, "cast")
			if an.sub++; an.sub > 2 {
				next()
			}
			return true
		}
		// Instant casts: the caster's pose, a ring on it and the target tile.
		u := an.unit(e.Unit)
		if u != nil {
			u.Pose = "attack"
		}
		an.scene.Flash[e.From] = render.Flash{Mark: an.g.Cast, Role: "cast"}
		if e.To != e.From {
			an.scene.Flash[e.To] = render.Flash{Mark: an.g.Cast, Role: "cast"}
		}
		logLine()
		if u != nil && u.Commander && an.c.Heroes[u.Kind].Abilities["r"] == e.Ability {
			an.sub = 1
			name := an.c.Abilities[e.Ability].Name
			if name == "" {
				name = e.Ability
			}
			an.say(name, "ultimate · "+an.heroName(u.Kind), an.side(u.Team))
			return true
		}
		next()
		return true
	case engine.EvSpawned, engine.EvRespawned:
		an.remove(e.Unit)
		an.ensure(e.Unit)
		if u := an.unit(e.Unit); u != nil {
			u.Pos = e.To
			if e.Kind == engine.EvRespawned {
				u.HP = u.MaxHP
			}
		}
		logLine()
		next()
		return true
	case engine.EvTelegraph:
		for _, p := range e.Tiles {
			an.scene.Telegraph[p] = true
		}
		logLine()
		next()
		return true
	case engine.EvAbilityResolved:
		delete(an.scene.Telegraph, e.To)
		ab := an.c.Abilities[e.Ability]
		for _, p := range an.scene.Board.Area(e.To, ab.Area) {
			delete(an.scene.Telegraph, p)
			an.scene.Flash[p] = render.Flash{Mark: "*", Role: "hit"}
		}
		logLine()
		next()
		return true
	case engine.EvSmoke:
		for _, p := range e.Tiles {
			an.scene.Smoke[p] = true
		}
		logLine()
		next()
		return true
	case engine.EvRevealed:
		for _, p := range e.Tiles {
			an.scene.Reveal[p] = true
		}
		logLine()
		next()
		return true
	case engine.EvObjectiveScored:
		an.scored(e.Team, e.Amount)
		for _, p := range e.Tiles {
			an.scene.Flash[p] = render.Flash{Mark: "+", Role: "score"}
		}
		logLine()
		next()
		return len(e.Tiles) > 0
	case engine.EvLevelUp:
		// A level shows over the commander, as the web board floats it.
		if u := an.ensure(e.Unit); u != nil {
			an.scene.Flash[u.Pos] = render.Flash{Mark: "+", Role: "score", Text: fmt.Sprintf("L%d", e.Amount)}
			// In a team mode only your own commander's is called out: ten
			// commanders would call it ten times a match.
			pu := an.post.Unit(u.ID)
			mine := pu != nil && pu.Owner == an.player
			if e.Amount == an.c.Rules.Unlock["r"] && (mine || an.perTeam() == 1) {
				an.say("ultimate ready", fmt.Sprintf("%s · level %d", an.heroName(u.Kind), e.Amount), an.side(u.Team))
			}
		}
		logLine()
		next()
		return true
	case engine.EvMatchEnd:
		logLine()
		next()
		return true
	default:
		logLine()
		next()
		return false
	}
}

// ring flashes the squares at distance d from p that are empty and not
// flashing already this beat: a shock or a signal spreading out from p,
// its light falling off over the next beats.
func (an *anim) ring(p engine.Pos, d int, mark, role string) {
	for _, q := range an.scene.Board.Area(p, d) {
		if _, taken := an.fresh(q); !taken && engine.Dist(p, q) == d && an.unit0(q) == nil {
			an.scene.Flash[q] = render.Flash{Mark: mark, Role: role, Fade: 3}
		}
	}
}

// unit0 is the unit standing at p in the scene, if any.
func (an *anim) unit0(p engine.Pos) *render.SceneUnit {
	for i := range an.scene.Units {
		if an.scene.Units[i].Pos == p {
			return &an.scene.Units[i]
		}
	}
	return nil
}

// between is the squares on a straight line from a to b, ends excluded.
func between(a, b engine.Pos) []engine.Pos {
	dx, dy := b.X-a.X, b.Y-a.Y
	n := dx
	if n < 0 {
		n = -n
	}
	if m := dy; m > n || -m > n {
		n = m
		if n < 0 {
			n = -n
		}
	}
	var out []engine.Pos
	for i := 1; i < n; i++ {
		p := engine.Pos{X: a.X + (dx*i*2+sign(dx)*n)/(2*n), Y: a.Y + (dy*i*2+sign(dy)*n)/(2*n)}
		if p != a && p != b {
			out = append(out, p)
		}
	}
	return out
}

func sign(v int) int {
	switch {
	case v > 0:
		return 1
	case v < 0:
		return -1
	}
	return 0
}

// addAmount adds n to a floating number ("-2" and -1 make "-3"), so two
// blows in one beat show their sum.
func addAmount(text string, n int) string {
	cur, _ := strconv.Atoi(text)
	if cur += n; cur > 0 {
		return fmt.Sprintf("+%d", cur)
	}
	return fmt.Sprintf("%d", cur)
}
