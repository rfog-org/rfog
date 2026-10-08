package server

// Live counts for the operator: one aggregate line in the log every
// Config.StatsEvery. Nothing about any player is kept or written; the
// history side is `rfog stats`, which reads the store (store.Activity).

// Live is a snapshot of who is connected and how.
type Live struct {
	Online   int // connected sessions
	Peak     int // most online since the last stats line
	Web      int // the web app
	Terminal int // the terminal client, dialling in (TCP or WebSocket)
	SSH      int // the terminal client run over ssh
	Matches  int // matches in progress on the server
}

// clientKind sorts a Hello.Client name into the stats line's buckets.
func clientKind(client string) string {
	switch client {
	case "web":
		return "web"
	case "rfog-tui":
		return "terminal"
	}
	return "other"
}

// Live counts the connected sessions by client and the matches running.
func (s *Server) Live() Live {
	s.mu.Lock()
	sessions := make([]*Session, 0, len(s.sessions))
	for _, se := range s.sessions {
		sessions = append(sessions, se)
	}
	l := Live{Matches: len(s.matches), Peak: s.peak}
	s.mu.Unlock()
	for _, se := range sessions {
		se.mu.Lock()
		c := se.conn
		se.mu.Unlock()
		if c == nil {
			continue
		}
		l.Online++
		switch c.kind {
		case "web":
			l.Web++
		case "terminal":
			l.Terminal++
		case "ssh":
			l.SSH++
		}
	}
	if l.Online > l.Peak {
		l.Peak = l.Online
	}
	return l
}

// samplePeak runs every second so the stats line's peak sees short visits.
func (s *Server) samplePeak() {
	n := s.Online()
	s.mu.Lock()
	if n > s.peak {
		s.peak = n
	}
	s.mu.Unlock()
}

// logStats writes the stats line and starts a new peak window.
func (s *Server) logStats() {
	l := s.Live()
	s.mu.Lock()
	s.peak = l.Online
	s.mu.Unlock()
	s.log.Printf("stats online=%d peak=%d web=%d terminal=%d ssh=%d matches=%d",
		l.Online, l.Peak, l.Web, l.Terminal, l.SSH, l.Matches)
}
