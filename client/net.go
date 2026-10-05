package client

import (
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"rfog/engine"
	"rfog/proto"
	"rfog/proto/wsconn"
)

// Dialer opens a connection to a server. The native client dials TCP;
// SSH sessions get an in-process pipe.
type Dialer func(addr string) (net.Conn, error)

// DialTCP is the default dialer: plain TCP, TLS when the address is
// prefixed with "tls://", or the game over a site's WebSocket for
// "wss://" and "ws://" addresses (wss://rfog.org/net). A bare host with
// no port ("rfog.org") means its WebSocket endpoint over HTTPS.
func DialTCP(addr string) (net.Conn, error) {
	if !strings.Contains(addr, "://") && !strings.Contains(addr, ":") && strings.Contains(addr, ".") {
		addr = "wss://" + addr + "/net"
	}
	if strings.HasPrefix(addr, "wss://") || strings.HasPrefix(addr, "ws://") {
		if !strings.Contains(strings.SplitN(addr, "://", 2)[1], "/") {
			addr += "/net"
		}
		return wsconn.Dial(addr)
	}
	if strings.HasPrefix(addr, "tls://") {
		return tls.Dial("tcp", strings.TrimPrefix(addr, "tls://"), &tls.Config{MinVersion: tls.VersionTLS12})
	}
	return net.DialTimeout("tcp", addr, 10*time.Second)
}

// netMsg wraps a decoded server message for the Bubble Tea loop.
type netMsg struct {
	T   string
	F   proto.Frame
	Err error // set when the connection died
}

// remote is a live server connection. Reads run in a goroutine and land in
// the program as netMsg values; writes are synchronous and cheap.
type remote struct {
	conn    net.Conn
	msgs    chan tea.Msg
	wmu     sync.Mutex
	welcome proto.Welcome
	token   string
	closed  bool
}

// connect dials, handshakes and starts the reader.
// rules is the client's content fingerprint (see proto.Hello).
func connect(dial Dialer, addr, rules string, auth proto.Auth) (*remote, error) {
	c, err := dial(addr)
	if err != nil {
		return nil, err
	}
	r := &remote{conn: c, msgs: make(chan tea.Msg, 64)}
	if err := proto.Encode(c, proto.THello, proto.Hello{Version: proto.Version, Client: "rfog-tui", Rules: rules}); err != nil {
		c.Close()
		return nil, err
	}
	if err := proto.Encode(c, proto.TAuth, auth); err != nil {
		c.Close()
		return nil, err
	}
	_ = c.SetReadDeadline(time.Now().Add(10 * time.Second))
	f, err := proto.Decode(c)
	if err != nil {
		c.Close()
		return nil, err
	}
	_ = c.SetReadDeadline(time.Time{})
	if f.T == proto.TError {
		var e proto.Error
		_ = f.As(&e)
		c.Close()
		return nil, errors.New(e.Msg)
	}
	if f.T != proto.TWelcome {
		c.Close()
		return nil, fmt.Errorf("expected welcome, got %s", f.T)
	}
	if err := f.As(&r.welcome); err != nil {
		c.Close()
		return nil, err
	}
	if c := r.welcome.Content; c != nil {
		if err := c.Validate(); err != nil {
			r.conn.Close()
			return nil, fmt.Errorf("server rules: %w", err)
		}
	}
	r.token = r.welcome.Token
	go r.reader()
	return r, nil
}

func (r *remote) reader() {
	for {
		f, err := proto.Decode(r.conn)
		if err != nil {
			r.msgs <- netMsg{Err: err}
			close(r.msgs)
			return
		}
		r.msgs <- netMsg{T: f.T, F: f}
	}
}

// recv is the Bubble Tea command that waits for the next server message.
func (r *remote) recv() tea.Cmd {
	return func() tea.Msg {
		m, ok := <-r.msgs
		if !ok {
			return nil
		}
		return m
	}
}

func (r *remote) send(t string, body any) error {
	r.wmu.Lock()
	defer r.wmu.Unlock()
	if r.closed {
		return errors.New("disconnected")
	}
	return proto.Encode(r.conn, t, body)
}

func (r *remote) close() {
	r.wmu.Lock()
	r.closed = true
	r.wmu.Unlock()
	r.conn.Close()
}

// ---- remote match source -------------------------------------------------

// remoteMatch adapts a server match to the match screen. The server owns
// the state; this holds the latest view and forwards orders.
type remoteMatch struct {
	r         *remote
	id        string
	you       int
	mode      string
	timeCtl   string
	async     bool // daily: the match outlives the screen
	players   []proto.MatchPlayer
	view      engine.State
	deadline  time.Time
	bank      int // seconds banked beyond the base turn time (rapid)
	final     *engine.State
	reason    string
	ratings   map[int]proto.RatingDelta
	replay    json.RawMessage // sent with MatchEnd; saved on request
	ephemeral bool            // SSH session: nowhere to save
}

// team is the viewer's team.
func (rm *remoteMatch) team() int {
	for _, p := range rm.players {
		if p.Slot == rm.you {
			return p.Team
		}
	}
	return 0
}

func (rm *remoteMatch) View(player int) engine.State { return rm.view }
func (rm *remoteMatch) Humans() []int                { return []int{rm.you} }
func (rm *remoteMatch) Hotseat() bool                { return false }
func (rm *remoteMatch) Watching() bool               { return false }
func (rm *remoteMatch) Deadline() time.Time          { return rm.deadline }
func (rm *remoteMatch) Final() *engine.State         { return rm.final }
func (rm *remoteMatch) Setup() engine.Setup {
	return engine.Setup{ID: rm.id, Mode: rm.mode, TimeControl: rm.timeCtl}
}
func (rm *remoteMatch) SaveReplay() (string, error) {
	if len(rm.replay) == 0 {
		return "", errors.New("no replay yet")
	}
	if rm.ephemeral {
		return "", errors.New("replays are saved by the native client (rfog online)")
	}
	name := fmt.Sprintf("%s-%s.json", time.Now().Format("20060102-150405"), rm.id)
	path := filepath.Join(DataDir(), name)
	return path, os.WriteFile(path, rm.replay, 0o644)
}
func (rm *remoteMatch) Leave() { _ = rm.r.send(proto.TLeave, proto.Join{Match: rm.id}) }
func (rm *remoteMatch) Rematch(level string) (matchSource, error) {
	return nil, errors.New("queue again from the online menu")
}

// Commit sends the orders; the resolution arrives later as a netMsg.
func (rm *remoteMatch) Commit(player int, orders []engine.Order) tea.Cmd {
	if err := rm.r.send(proto.TOrders, proto.Orders{Match: rm.id, Turn: rm.view.Match.Turn, Orders: orders}); err != nil {
		return func() tea.Msg { return netMsg{Err: err} }
	}
	return nil
}

func (rm *remoteMatch) Advance() *turnResolvedMsg { return nil }
func (rm *remoteMatch) Committed(player int) bool { return false }

// translate turns server frames into the match screen's messages. Frames
// about other matches (async games in flight) are left alone.
func (rm *remoteMatch) translate(a *App, n netMsg) (tea.Msg, error) {
	var about struct {
		Match string `json:"match"`
	}
	if len(n.F.Body) > 0 {
		_ = n.F.As(&about)
	}
	if about.Match != "" && about.Match != rm.id {
		return nil, nil
	}
	switch n.T {
	case proto.TTurnStart:
		var ts proto.TurnStart
		if err := n.F.As(&ts); err != nil {
			return nil, err
		}
		rm.view = ts.View
		rm.deadline = ts.Deadline
		rm.bank = ts.Bank
		return turnStartMsg{View: ts.View, Deadline: ts.Deadline, Turn: ts.Turn}, nil
	case proto.TResolved:
		var rs proto.Resolved
		if err := n.F.As(&rs); err != nil {
			return nil, err
		}
		pre := rm.view
		rm.view = rs.View
		rm.deadline = time.Time{}
		return turnResolvedMsg{Views: map[int]turnView{rm.you: {Pre: pre, Post: rs.View, Events: rs.Events}}, Ended: rs.Ended}, nil
	case proto.TMatchEnd:
		var me proto.MatchEnd
		if err := n.F.As(&me); err != nil {
			return nil, err
		}
		f := me.Final
		rm.final = &f
		rm.reason = me.Reason
		rm.ratings = me.Ratings
		rm.replay = me.Replay
		return matchEndMsg{Final: f, Reason: me.Reason, Ratings: me.Ratings}, nil
	case proto.TChatMsg:
		var c proto.Chat
		_ = n.F.As(&c)
		return chatMsg{From: c.From, Text: c.Text}, nil
	case proto.TError:
		var e proto.Error
		_ = n.F.As(&e)
		return errMsg{Text: e.Msg}, nil
	}
	return nil, nil
}
