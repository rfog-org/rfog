package gateway

import (
	"context"
	"errors"
	"net/http"
	"time"

	"rfog/engine"
	"rfog/server"
	"rfog/web"
)

// serveWeb serves the graphical client at / and the game protocol it
// speaks at /net. The terminal is played natively or over SSH; there is no
// terminal-in-a-browser: the graphical client is the web's way to play.
func serveWeb(ctx context.Context, srv *server.Server, c *engine.Content, o Options) error {
	mux := http.NewServeMux()
	mux.Handle("/", newStatic(web.Files()))
	mux.Handle("/net", NetHandler(ctx, srv, o))
	hs := &http.Server{Addr: o.WS, Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	go func() {
		<-ctx.Done()
		sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = hs.Shutdown(sctx)
	}()
	o.Logger.Printf("web listening on %s (tls=%v)", o.WS, o.TLSCert != "")
	var err error
	if o.TLSCert != "" {
		err = hs.ListenAndServeTLS(o.TLSCert, o.TLSKey)
	} else {
		err = hs.ListenAndServe()
	}
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}
