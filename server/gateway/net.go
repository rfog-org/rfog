package gateway

import (
	"context"
	"net/http"

	"github.com/coder/websocket"

	"rfog/proto/wsconn"
	"rfog/server"
)

// NetHandler serves the game protocol over a WebSocket at /net, for the
// graphical web client and terminal clients dialling wss:// (see wsconn).
// The server sees an ordinary connection, exactly as from a TCP client, so
// accounts, queues, drafts and matches are the same everywhere. A page on
// another site may connect when its origin is in Options.WSOrigins.
func NetHandler(ctx context.Context, srv *server.Server, o Options) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{OriginPatterns: o.WSOrigins,
			CompressionMode: websocket.CompressionContextTakeover})
		if err != nil {
			return
		}
		c := wsconn.New(ctx, conn)
		defer c.Close()
		srv.Serve(c)
	})
}
