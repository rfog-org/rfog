package client

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"rfog/engine"
	"rfog/proto"
)

// matchSource feeds the match screen. localMatch (offline, hotseat,
// replays) and remoteMatch (server) both implement it, so the screen never
// knows where the truth lives. Resolutions arrive as messages either way.
type matchSource interface {
	View(player int) engine.State
	Humans() []int
	Hotseat() bool
	Watching() bool
	Deadline() time.Time // zero when untimed
	// Commit records a player's orders. The returned command (may be nil)
	// yields a turnResolvedMsg when the turn resolves locally; remote
	// resolutions arrive through the network instead.
	Commit(player int, orders []engine.Order) tea.Cmd
	Committed(player int) bool
	// Advance steps a replay one turn (watching only); nil when finished.
	Advance() *turnResolvedMsg
	Final() *engine.State // set once the match has ended
	Setup() engine.Setup
	SaveReplay() (string, error)
	Rematch(level string) (matchSource, error)
	Leave()
}

// turnView is what one viewer sees of a resolved turn.
type turnView struct {
	Pre, Post engine.State
	Events    []engine.Event
}

type turnResolvedMsg struct {
	Views map[int]turnView // by player; -1 for the spectator
	Ended bool
}

type turnStartMsg struct {
	View     engine.State
	Deadline time.Time
	Turn     int
}

type matchEndMsg struct {
	Final   engine.State
	Reason  string
	Ratings map[int]proto.RatingDelta // by slot, ranked online matches only
}

type chatMsg struct{ From, Text string }
type errMsg struct{ Text string }
