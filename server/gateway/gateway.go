// Package gateway exposes a server over the network: a TCP listener for
// native clients, an SSH listener that runs the TUI in-process for
// zero-install play, and an HTTP listener serving the browser client and
// its WebSocket terminal sessions.
package gateway

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log"
	"net"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/ssh"
	"github.com/charmbracelet/wish"
	"github.com/charmbracelet/wish/activeterm"
	"github.com/charmbracelet/wish/bubbletea"

	"rfog/client"
	"rfog/engine"
	"rfog/render"
	"rfog/server"
)

// Options for the listeners. Empty addresses disable that listener.
type Options struct {
	TCP     string // e.g. ":7777"
	SSH     string // e.g. ":2222"
	WS      string // e.g. ":8080"; serves the graphical client at / and the protocol at /net
	TLSCert string // optional: enables TLS on the TCP and web listeners
	TLSKey  string
	HostKey string // SSH host key path; generated if missing
	// WSOrigins lists extra Origin host patterns allowed to connect to
	// /net (same-origin is always allowed): a copy of the graphical client
	// hosted elsewhere.
	WSOrigins []string
	Logger    *log.Logger
}

// Run serves until ctx ends.
func Run(ctx context.Context, srv *server.Server, c *engine.Content, o Options) error {
	if o.Logger == nil {
		o.Logger = log.Default()
	}
	if o.TCP == "" && o.SSH == "" && o.WS == "" {
		return errors.New("nothing to listen on")
	}
	errc := make(chan error, 3)
	if o.TCP != "" {
		go func() { errc <- serveTCP(ctx, srv, o) }()
	}
	if o.SSH != "" {
		go func() { errc <- serveSSH(ctx, srv, c, o) }()
	}
	if o.WS != "" {
		go func() { errc <- serveWeb(ctx, srv, c, o) }()
	}
	select {
	case <-ctx.Done():
		return nil
	case err := <-errc:
		return err
	}
}

func serveTCP(ctx context.Context, srv *server.Server, o Options) error {
	var ln net.Listener
	var err error
	if o.TLSCert != "" {
		cert, err := tls.LoadX509KeyPair(o.TLSCert, o.TLSKey)
		if err != nil {
			return fmt.Errorf("tls: %w", err)
		}
		ln, err = tls.Listen("tcp", o.TCP, &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12})
		if err != nil {
			return err
		}
	} else {
		ln, err = net.Listen("tcp", o.TCP)
		if err != nil {
			return err
		}
	}
	o.Logger.Printf("tcp listening on %s (tls=%v)", o.TCP, o.TLSCert != "")
	go func() {
		<-ctx.Done()
		ln.Close()
	}()
	for {
		conn, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		go srv.Serve(conn)
	}
}

func serveSSH(ctx context.Context, srv *server.Server, c *engine.Content, o Options) error {
	handler := func(s ssh.Session) (tea.Model, []tea.ProgramOption) {
		pty, _, _ := s.Pty()
		env := render.Env{Term: pty.Term, Width: pty.Window.Width, Height: pty.Window.Height}
		for _, kv := range s.Environ() {
			if len(kv) > 10 && kv[:10] == "COLORTERM=" {
				env.ColorTerm = kv[10:]
			}
			if len(kv) > 5 && kv[:5] == "LANG=" {
				env.Lang = kv[5:]
			}
		}
		set := client.DefaultSettings()
		set.Name = s.User()
		app := client.New(client.Options{
			Content:   c,
			Settings:  set,
			Env:       env,
			Start:     "online",
			Server:    "internal",
			Ephemeral: true,
			Output:    s,
			Dial:      func(string) (net.Conn, error) { return srv.DialInternal(), nil },
		})
		return app, []tea.ProgramOption{tea.WithAltScreen(), tea.WithMouseCellMotion()}
	}
	hostKey := o.HostKey
	if hostKey == "" {
		hostKey = ".ssh/rfog_host_ed25519"
	}
	s, err := wish.NewServer(
		wish.WithAddress(o.SSH),
		wish.WithHostKeyPath(hostKey),
		wish.WithIdleTimeout(30*time.Minute),
		wish.WithMiddleware(
			bubbletea.Middleware(handler),
			activeterm.Middleware(),
		),
	)
	if err != nil {
		return err
	}
	o.Logger.Printf("ssh listening on %s", o.SSH)
	go func() {
		<-ctx.Done()
		sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.Shutdown(sctx)
	}()
	if err := s.ListenAndServe(); err != nil && !errors.Is(err, ssh.ErrServerClosed) {
		return err
	}
	return nil
}
