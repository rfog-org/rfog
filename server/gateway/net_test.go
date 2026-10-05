package gateway

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"rfog/data"
	"rfog/proto"
	"rfog/proto/wsconn"
	"rfog/server"
	"rfog/server/store"
)

// The graphical client's transport: one protocol frame per WebSocket
// message, the same handshake as any client, answered with a Welcome.
func TestNetSpeaksTheProtocol(t *testing.T) {
	c0, err := data.Load()
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(context.Background(), "sqlite::memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	srv := server.New(c0, st, server.Config{Logger: log.New(io.Discard, "", 0)})
	sctx, stop := context.WithCancel(context.Background())
	defer stop()
	go srv.Run(sctx)
	hs := httptest.NewServer(NetHandler(sctx, srv, Options{}))
	defer hs.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(hs.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close(websocket.StatusNormalClosure, "")
	c.SetReadLimit(proto.MaxFrame) // the welcome carries the rules
	send := func(typ string, body any) {
		b, _ := json.Marshal(body)
		f, _ := json.Marshal(proto.Frame{V: proto.Version, T: typ, Body: b})
		if err := c.Write(ctx, websocket.MessageText, f); err != nil {
			t.Fatal(err)
		}
	}
	send(proto.THello, proto.Hello{Version: proto.Version, Client: "web"})
	send(proto.TAuth, proto.Auth{Name: "pagey"})
	_, msg, err := c.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var f proto.Frame
	if err := json.Unmarshal(msg, &f); err != nil || f.T != proto.TWelcome {
		t.Fatalf("first message %q: %v", msg, err)
	}
	var w proto.Welcome
	_ = f.As(&w)
	if w.Name != "pagey" || !w.Guest || w.Token == "" {
		t.Fatalf("welcome %+v", w)
	}
	send(proto.TPing, nil)
	for i := 0; i < 5; i++ {
		_, msg, err = c.Read(ctx)
		if err != nil {
			t.Fatal(err)
		}
		_ = json.Unmarshal(msg, &f)
		if f.T == proto.TPong {
			return
		}
	}
	t.Fatal("no pong")
}

// A terminal client can play through the same endpoint: wsconn turns the
// WebSocket back into the byte stream proto.Encode and proto.Decode use,
// so a server behind one HTTPS port serves browsers and terminals alike.
func TestTerminalOverWebSocket(t *testing.T) {
	c0, err := data.Load()
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(context.Background(), "sqlite::memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	srv := server.New(c0, st, server.Config{Logger: log.New(io.Discard, "", 0)})
	sctx, stop := context.WithCancel(context.Background())
	defer stop()
	go srv.Run(sctx)
	hs := httptest.NewServer(NetHandler(sctx, srv, Options{}))
	defer hs.Close()
	conn, err := wsconn.Dial("ws" + strings.TrimPrefix(hs.URL, "http"))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := proto.Encode(conn, proto.THello, proto.Hello{Version: proto.Version, Client: "rfog-tui", Rules: c0.Fingerprint()}); err != nil {
		t.Fatal(err)
	}
	if err := proto.Encode(conn, proto.TAuth, proto.Auth{Name: "termy"}); err != nil {
		t.Fatal(err)
	}
	f, err := proto.Decode(conn)
	if err != nil {
		t.Fatal(err)
	}
	var w proto.Welcome
	if f.T != proto.TWelcome || f.As(&w) != nil || w.Name != "termy" {
		t.Fatalf("got %s %+v", f.T, w)
	}
}
