package server

import (
	"bytes"
	"log"
	"net"
	"strings"
	"testing"
	"time"

	"rfog/proto"
)

// The stats line counts sessions by client: the web app by its Hello, the
// terminal client over ssh by the listener, and keeps the window's peak.
func TestLiveStats(t *testing.T) {
	s, stop := newTestServer(t, Config{})
	defer stop()

	a, b := net.Pipe()
	go s.Serve(b)
	web := &tclient{t: t, c: a, in: make(chan proto.Frame, 64), name: "w"}
	go func() {
		for {
			f, err := proto.Decode(a)
			if err != nil {
				close(web.in)
				return
			}
			web.in <- f
		}
	}()
	web.send(proto.THello, proto.Hello{Version: proto.Version, Client: "web"})
	web.send(proto.TAuth, proto.Auth{Name: "w"})
	web.expect(proto.TWelcome)
	ssh := dial(t, s, "s", "")

	l := s.Live()
	if l.Online != 2 || l.Web != 1 || l.SSH != 1 || l.Terminal != 0 || l.Peak != 2 {
		t.Fatalf("live: %+v", l)
	}
	s.samplePeak()
	ssh.c.Close()
	deadline := time.Now().Add(2 * time.Second)
	for s.Online() != 1 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	var buf bytes.Buffer
	s.log = log.New(&buf, "", 0)
	s.logStats()
	if got := buf.String(); !strings.Contains(got, "stats online=1 peak=2 web=1 terminal=0 ssh=0") {
		t.Fatalf("line: %q", got)
	}
	if s.Live().Peak != 1 {
		t.Fatalf("peak not reset: %+v", s.Live())
	}
}
