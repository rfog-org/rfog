// Package proto is the wire protocol between clients and servers: length
// prefixed JSON frames carrying one typed message each. The same codec runs
// over TCP, WebSocket (M4) and the in-process pipe used by SSH sessions.
package proto

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"rfog/engine"
)

// Version is bumped on any incompatible change.
const Version = 1

// MaxFrame bounds a single frame; a full state view is well under this.
const MaxFrame = 4 << 20

// Frame is the envelope. T names the message; Body is that message's JSON.
type Frame struct {
	V    int             `json:"v"`
	T    string          `json:"t"`
	Body json.RawMessage `json:"b,omitempty"`
}

// Message types.
const (
	// client -> server
	THello     = "hello"
	TAuth      = "auth"
	TQueue     = "queue"
	TCancel    = "cancel"
	TDraft     = "draft"
	TOrders    = "orders"
	TChat      = "chat"
	TPing      = "ping"
	TLeave     = "leave"
	THistory   = "history"    // list my finished matches
	TReplayGet = "replay_get" // download one replay
	TLive      = "live"       // list my live (async) matches
	TJoin      = "join"       // attach to one of my live matches
	TLadder    = "ladder"     // leaderboard for a mode
	TSpectate  = "spectate"   // watch a live match
	TUnspec    = "unspectate" // stop watching
	TLobby     = "lobby"      // list live matches
	TAccount   = "account"    // manage my account (see Account)
	TChallenge = "challenge"  // challenge a friend (see Challenge)

	// server -> client
	TWelcome       = "welcome"
	TQueueStatus   = "queue_status"
	TMatchFound    = "match_found"
	TDraftState    = "draft_state"
	TTurnStart     = "turn_start"
	TResolved      = "resolved"
	TMatchEnd      = "match_end"
	TChatMsg       = "chat_msg"
	TError         = "error"
	TPong          = "pong"
	THistoryList   = "history_list"
	TReplayData    = "replay_data"
	TLiveList      = "live_list"
	TLadderList    = "ladder_list"
	TLobbyList     = "lobby_list"
	TSpecState     = "spec_state" // a spectator's view of a match
	TAccountDone   = "account_done"
	TChallengeInfo = "challenge_info"
)

type Hello struct {
	Version int    `json:"v"`
	Client  string `json:"client"`
	// Rules is the client's engine.Content fingerprint; the server answers
	// a mismatch (or an empty one) with its own content in Welcome.
	Rules string `json:"rules,omitempty"`
}

// Auth identifies the player. Precedence: a valid Token reattaches that
// player; Recovery (with Name) sets Password as the account's new password
// and signs every other device out; Register creates an account with
// Name+Password; Password alone logs in; a bare Name plays as a guest
// (bots/casual only). Every sign-in is a token for this device; other
// devices stay signed in.
type Auth struct {
	Name     string `json:"name,omitempty"`
	Token    string `json:"token,omitempty"`
	Password string `json:"password,omitempty"`
	Register bool   `json:"register,omitempty"`
	Recovery string `json:"recovery,omitempty"`
}

// Account manages the signed-in player's account. Actions:
//
//	save        a guest keeps its progress as an account: Name, Password
//	password    change password: Password (current), NewPassword
//	recovery    a new recovery code (the old one stops working): Password
//	logout      sign this device out: Token
//	logout_all  sign every device out
//	delete      delete the account and everything tied to it: Password
//	            (guests: none)
//
// The answer is AccountDone, or an Error.
type Account struct {
	Action      string `json:"action"`
	Name        string `json:"name,omitempty"`
	Password    string `json:"password,omitempty"`
	NewPassword string `json:"new_password,omitempty"`
	Token       string `json:"token,omitempty"`
}

// AccountDone confirms an Account action. Recovery is a new recovery code,
// shown once and never stored in the clear: the player writes it down.
type AccountDone struct {
	Action   string `json:"action"`
	Name     string `json:"name"`
	Guest    bool   `json:"guest"`
	Recovery string `json:"recovery,omitempty"`
}

// RatingInfo is one mode's rating as shown to players.
type RatingInfo struct {
	Rating float64 `json:"rating"`
	RD     float64 `json:"rd"`
	Games  int     `json:"games"`
	Wins   int     `json:"wins"`
}

type Welcome struct {
	Player  string                `json:"player"` // account id
	Name    string                `json:"name"`
	Guest   bool                  `json:"guest"`
	Token   string                `json:"token"` // session token for reconnects
	MOTD    string                `json:"motd,omitempty"`
	Online  int                   `json:"online"`
	InMatch string                `json:"in_match,omitempty"` // live timed match id to resume
	Ratings map[string]RatingInfo `json:"ratings,omitempty"`  // by time control
	Live    []LiveMatch           `json:"live,omitempty"`     // async matches in flight
	// Rules is the server's content fingerprint. Content is the server's
	// rules, sent only when the client's fingerprint differs: online, the
	// server's rules are the ones a client previews and validates with.
	Rules   string          `json:"rules"`
	Content *engine.Content `json:"content,omitempty"`
	// Recovery is a new account's (or a just-recovered one's) recovery
	// code, shown once.
	Recovery string `json:"recovery,omitempty"`
	// Last is the player's most recent match when it ended recently, so a
	// client can show how it went (it may have ended while they were away).
	Last *HistoryMatch `json:"last,omitempty"`
	// Clocks are the time controls this server offers, in order; each can
	// be queued rated or casual.
	Clocks []ClockInfo `json:"clocks,omitempty"`
}

// Queue asks for a match: Mode is the time control (casual | blitz |
// bullet | rapid | daily), Size the team size (1v1 .. 5v5, default 1v1).
// Queue asks for a match. Mode is a clock ("30s", see server.Clocks);
// Rated picks its rated pool, else the casual one (guests, bots fill
// seats). Older clients name a control ("blitz", "casual") and leave
// Rated unset: they get what that name always meant (Clock set false).
type Queue struct {
	Mode  string `json:"mode"`
	Size  string `json:"size,omitempty"`
	Hero  string `json:"hero,omitempty"`
	Rated bool   `json:"rated,omitempty"`
	// Clock marks a client that knows rated/casual clocks; without it the
	// server treats Mode as a control name, as before.
	Clock bool `json:"clock,omitempty"`
}

// Legacy reports a queue from a client that predates rated/casual clocks.
func (q Queue) Legacy() bool { return !q.Clock }

type QueueStatus struct {
	Mode    string `json:"mode"`
	Size    string `json:"size,omitempty"`
	Waiting int    `json:"waiting"`
	Seconds int    `json:"seconds"`
}

// LiveMatch summarises one of the player's in-progress matches.
type LiveMatch struct {
	Match    string    `json:"match"`
	Mode     string    `json:"mode"` // 1v1 ..
	Time     string    `json:"time"` // time control
	Phase    string    `json:"phase"`
	Turn     int       `json:"turn"`
	YourTurn bool      `json:"your_turn"` // orders still wanted from you
	Deadline time.Time `json:"deadline"`
	Versus   string    `json:"versus"` // opponent names
}

type Join struct {
	Match string `json:"match"`
}

type History struct {
	Limit int `json:"limit,omitempty"`
}

// HistoryMatch is one finished match as listed to a player.
type HistoryMatch struct {
	Match   string          `json:"match"`
	Mode    string          `json:"mode"`
	Time    string          `json:"time"`
	Started time.Time       `json:"started"`
	Ended   time.Time       `json:"ended"`
	Winner  int             `json:"winner"`
	Result  string          `json:"result"`
	You     int             `json:"you"` // your slot, -1 if you did not play
	Players []HistoryPlayer `json:"players"`
}

type HistoryPlayer struct {
	Name   string   `json:"name"`
	Slot   int      `json:"slot"`
	Team   int      `json:"team"`
	Hero   string   `json:"hero"`
	Bot    bool     `json:"bot,omitempty"`
	Before *float64 `json:"before,omitempty"` // rating before, ranked only
	After  *float64 `json:"after,omitempty"`
}

type HistoryList struct {
	Matches []HistoryMatch `json:"matches"`
}

type ReplayGet struct {
	Match string `json:"match"`
}

type ReplayData struct {
	Match  string          `json:"match"`
	Replay json.RawMessage `json:"replay"`
}

type LiveList struct {
	Matches []LiveMatch `json:"matches"`
}

type Ladder struct {
	Mode string `json:"mode"`
}

// Spectate asks to watch a live match (Match empty = stop watching).
type Spectate struct {
	Match string `json:"match"`
}

// LobbyList is what is playable to watch right now.
type LobbyList struct {
	Matches []LobbyMatch `json:"matches"`
}

type LobbyMatch struct {
	Match   string   `json:"match"`
	Mode    string   `json:"mode"`
	Time    string   `json:"time"`
	Phase   string   `json:"phase"`
	Turn    int      `json:"turn"`
	Score   [2]int   `json:"score"`
	Players []string `json:"players"`        // "name (hero)" per slot
	Delayed bool     `json:"delayed"`        // ranked: spectators lag one turn
	Bots    bool     `json:"bots,omitempty"` // a seat is a bot's (watch lists leave these out)
}

// SpecState is one spectator frame: the board as spectators may see it.
// Ranked matches lag a turn (see .claude/DECISIONS.md); Turn is the turn the
// state shows, Live the turn the players are on.
type SpecState struct {
	Match   string         `json:"match"`
	Mode    string         `json:"mode"`
	Time    string         `json:"time"`
	Turn    int            `json:"turn"`
	Live    int            `json:"live"`
	Delayed bool           `json:"delayed,omitempty"`
	View    engine.State   `json:"view"`
	Events  []engine.Event `json:"events,omitempty"`
	Players []MatchPlayer  `json:"players"`
	Ended   bool           `json:"ended,omitempty"`
	Result  string         `json:"result,omitempty"`
}

type LadderRow struct {
	Name   string     `json:"name"`
	Rating RatingInfo `json:"rating"`
}

type LadderList struct {
	Mode string      `json:"mode"`
	Rows []LadderRow `json:"rows"`
}

type MatchPlayer struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Team int    `json:"team"`
	Slot int    `json:"slot"` // engine player index
	Bot  bool   `json:"bot,omitempty"`
}

type MatchFound struct {
	Match   string        `json:"match"`
	Mode    string        `json:"mode"`
	Time    string        `json:"time"`            // time control id
	Async   bool          `json:"async,omitempty"` // daily: leaving the screen does not forfeit
	You     int           `json:"you"`             // your engine player index
	Players []MatchPlayer `json:"players"`
}

// DraftAction is a ban or a pick.
type DraftAction struct {
	Match string `json:"match"`
	Kind  string `json:"kind"` // ban | pick
	Hero  string `json:"hero"`
}

type DraftState struct {
	Match    string         `json:"match"`
	Heroes   []string       `json:"heroes"`
	Banned   []string       `json:"banned"`
	Picks    map[int]string `json:"picks"` // slot -> hero
	Turn     int            `json:"turn"`  // slot whose action it is
	Kind     string         `json:"kind"`  // ban | pick | done
	Deadline time.Time      `json:"deadline"`
	Order    []DraftStep    `json:"order"`
}

type DraftStep struct {
	Slot int    `json:"slot"`
	Kind string `json:"kind"`
}

type TurnStart struct {
	Match    string       `json:"match"`
	Turn     int          `json:"turn"`
	Deadline time.Time    `json:"deadline"`       // your deadline (includes banked time)
	Bank     int          `json:"bank,omitempty"` // seconds banked beyond the base turn time
	View     engine.State `json:"view"`
	Resumed  bool         `json:"resumed,omitempty"` // reconnect: orders may already be committed
}

type Orders struct {
	Match  string         `json:"match"`
	Turn   int            `json:"turn"`
	Orders []engine.Order `json:"orders"`
}

// Resolved carries one turn's fog-filtered events and the post-turn view.
type Resolved struct {
	Match  string         `json:"match"`
	Turn   int            `json:"turn"`
	Events []engine.Event `json:"events"`
	View   engine.State   `json:"view"`
	Ended  bool           `json:"ended"`
}

// RatingDelta is a ranked player's rating before and after a match.
type RatingDelta struct {
	Before float64 `json:"before"`
	After  float64 `json:"after"`
}

type MatchEnd struct {
	Match   string              `json:"match"`
	Winner  int                 `json:"winner"` // team, -1 draw
	Result  string              `json:"result"`
	Final   engine.State        `json:"final"` // unfogged
	Reason  string              `json:"reason,omitempty"`
	Ratings map[int]RatingDelta `json:"ratings,omitempty"` // by slot, ranked only
	Replay  json.RawMessage     `json:"replay,omitempty"`  // full replay for saving
}

type Chat struct {
	Match string `json:"match,omitempty"`
	From  string `json:"from,omitempty"`
	Text  string `json:"text"`
}

// Challenge is a private match between friends, by code (and a link that
// carries it). Actions:
//
//	create  open a challenge: Mode (a clock), Rated; answered with its code
//	peek    what a code is (who, which clock) before accepting: Code
//	accept  take it: Code; the match starts for both
//	cancel  withdraw your open challenge
type Challenge struct {
	Action string `json:"action"`
	Code   string `json:"code,omitempty"`
	Mode   string `json:"mode,omitempty"`
	Rated  bool   `json:"rated,omitempty"` // ignored: challenges are always casual
}

// ChallengeInfo describes a challenge: Status is "waiting" (open),
// "gone" (expired, cancelled or taken) or "cancelled".
type ChallengeInfo struct {
	Code   string `json:"code"`
	Mode   string `json:"mode"`  // the clock, as Queue names it
	Rated  bool   `json:"rated"` // always false (challenges are casual)
	From   string `json:"from"`
	Status string `json:"status"`
}

// ClockInfo is one time control as a client lists it.
type ClockInfo struct {
	ID       string `json:"id"`       // what Queue.Mode names
	Turn     int    `json:"turn"`     // seconds a turn
	Bank     int    `json:"bank"`     // seconds of bank cap (0 = none)
	Category string `json:"category"` // the rating it counts toward
	Async    bool   `json:"async,omitempty"`
}

// ErrDisplaced is the Error code a connection gets when the same player
// connects from somewhere else: the game continues there. A client seeing
// it should not reconnect on its own.
const ErrDisplaced = "displaced"

type Error struct {
	Code string `json:"code"`
	Msg  string `json:"msg"`
}

// Encode marshals a typed message into a frame and writes it.
func Encode(w io.Writer, t string, body any) error {
	var raw json.RawMessage
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		raw = b
	}
	f, err := json.Marshal(Frame{V: Version, T: t, Body: raw})
	if err != nil {
		return err
	}
	if len(f) > MaxFrame {
		return fmt.Errorf("frame too large: %d", len(f))
	}
	var hdr [4]byte
	binary.BigEndian.PutUint32(hdr[:], uint32(len(f)))
	if _, err := w.Write(hdr[:]); err != nil {
		return err
	}
	_, err = w.Write(f)
	return err
}

// Decode reads one frame.
func Decode(r io.Reader) (Frame, error) {
	var hdr [4]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return Frame{}, err
	}
	n := binary.BigEndian.Uint32(hdr[:])
	if n == 0 || n > MaxFrame {
		return Frame{}, fmt.Errorf("bad frame length %d", n)
	}
	buf := make([]byte, n)
	if _, err := io.ReadFull(r, buf); err != nil {
		return Frame{}, err
	}
	var f Frame
	if err := json.Unmarshal(buf, &f); err != nil {
		return Frame{}, err
	}
	if f.V != Version {
		return f, fmt.Errorf("protocol version %d, want %d", f.V, Version)
	}
	return f, nil
}

// As unmarshals the frame body into v.
func (f Frame) As(v any) error {
	if len(f.Body) == 0 {
		return errors.New("empty body")
	}
	return json.Unmarshal(f.Body, v)
}
