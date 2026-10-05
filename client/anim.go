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
	an := &anim{c: c, g: g, post: post, events: events}
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
	an.scene.Flash = map[engine.Pos]render.Flash{}
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
			return an.done
		}
	}
	return an.done
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
				if _, taken := an.scene.Flash[p]; !taken && an.unit0(p) == nil {
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
			f, ok := an.scene.Flash[u.Pos]
			if !ok {
				f = render.Flash{Mark: "-", Role: "hit"}
			}
			f.Text = addAmount(f.Text, -e.Amount)
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
		// Two beats: the unit is struck out, then only dust remains.
		if an.sub == 0 {
			an.remove(e.Unit)
			an.scene.Flash[e.From] = render.Flash{Mark: an.g.Death, Role: "death"}
			logLine()
			an.sub++
			return true
		}
		an.scene.Flash[e.From] = render.Flash{Mark: an.g.Dust, Role: "dust"}
		next()
		return true
	case engine.EvAbilityCast:
		// Instant casts: the caster's pose, a ring on it and the target tile.
		if u := an.unit(e.Unit); u != nil {
			u.Pose = "attack"
		}
		an.scene.Flash[e.From] = render.Flash{Mark: an.g.Cast, Role: "cast"}
		if e.To != e.From {
			an.scene.Flash[e.To] = render.Flash{Mark: an.g.Cast, Role: "cast"}
		}
		logLine()
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
		if e.Team >= 0 && e.Team < 2 {
			an.scores[e.Team] += e.Amount
		}
		for _, p := range e.Tiles {
			an.scene.Flash[p] = render.Flash{Mark: "+", Role: "score"}
		}
		logLine()
		next()
		return len(e.Tiles) > 0
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
