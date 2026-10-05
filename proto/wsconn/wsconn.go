// Package wsconn carries the game protocol over a WebSocket. Each message
// is one protocol frame's JSON; the length prefix of the byte-stream
// transports (TCP, SSH) is the message boundary here. Conn adapts a
// WebSocket to the byte stream proto.Encode and proto.Decode speak, so the
// server and the terminal client use it like any other connection: the
// server for browsers at /net, the terminal client to play through a
// site's HTTPS port (wss://rfog.org/net).
package wsconn

import (
	"bytes"
	"context"
	"encoding/binary"
	"net"
	"sync"
	"time"

	"github.com/coder/websocket"

	"rfog/proto"
)

// Conn is a WebSocket as a protocol byte stream. Writes are collected
// until a whole frame (length and body) is in, then sent as one message;
// each message read is handed back with its length in front.
type Conn struct {
	ctx    context.Context
	cancel context.CancelFunc
	c      *websocket.Conn

	rmu  sync.Mutex
	rbuf bytes.Buffer

	wmu  sync.Mutex
	wbuf bytes.Buffer
}

// New wraps an open WebSocket. One long-lived context serves every read
// and write: a read whose own context expires closes the connection.
func New(ctx context.Context, c *websocket.Conn) *Conn {
	c.SetReadLimit(proto.MaxFrame)
	cctx, cancel := context.WithCancel(ctx)
	return &Conn{ctx: cctx, cancel: cancel, c: c}
}

// Dial opens a game connection to a server's WebSocket endpoint, e.g.
// "wss://rfog.org/net".
func Dial(url string) (*Conn, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, url, &websocket.DialOptions{CompressionMode: websocket.CompressionContextTakeover})
	if err != nil {
		return nil, err
	}
	return New(context.Background(), c), nil
}

func (f *Conn) Read(p []byte) (int, error) {
	f.rmu.Lock()
	defer f.rmu.Unlock()
	for f.rbuf.Len() == 0 {
		_, msg, err := f.c.Read(f.ctx)
		if err != nil {
			return 0, err
		}
		if len(msg) == 0 || len(msg) > proto.MaxFrame {
			continue
		}
		var hdr [4]byte
		binary.BigEndian.PutUint32(hdr[:], uint32(len(msg)))
		f.rbuf.Write(hdr[:])
		f.rbuf.Write(msg)
	}
	return f.rbuf.Read(p)
}

func (f *Conn) Write(p []byte) (int, error) {
	f.wmu.Lock()
	defer f.wmu.Unlock()
	f.wbuf.Write(p)
	for f.wbuf.Len() >= 4 {
		n := int(binary.BigEndian.Uint32(f.wbuf.Bytes()[:4]))
		if f.wbuf.Len() < 4+n {
			break
		}
		f.wbuf.Next(4)
		body := append([]byte(nil), f.wbuf.Next(n)...)
		if err := f.c.Write(f.ctx, websocket.MessageText, body); err != nil {
			return 0, err
		}
	}
	return len(p), nil
}

// Close ends the connection.
func (f *Conn) Close() error {
	f.cancel()
	return f.c.Close(websocket.StatusNormalClosure, "bye")
}

// The rest of net.Conn: a WebSocket has no addresses to give and its
// deadlines come from the context, so these are no-ops.

func (f *Conn) LocalAddr() net.Addr                { return addr{} }
func (f *Conn) RemoteAddr() net.Addr               { return addr{} }
func (f *Conn) SetDeadline(t time.Time) error      { return nil }
func (f *Conn) SetReadDeadline(t time.Time) error  { return nil }
func (f *Conn) SetWriteDeadline(t time.Time) error { return nil }

type addr struct{}

func (addr) Network() string { return "websocket" }
func (addr) String() string  { return "websocket" }
