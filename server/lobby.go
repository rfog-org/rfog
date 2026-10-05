package server

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"

	"rfog/proto"
	"rfog/server/rating"
	"rfog/server/store"
)

// lobby holds one queue per (time control, size). Ranked 1v1 pairs players
// within a rating window that widens with waiting time; team queues fill
// first-come and balance the two teams by rating. Controls marked Backfill
// (casual) seat bots after BackfillAfter so nobody waits on an empty server.
type lobby struct {
	srv *Server
	mu  sync.Mutex
	q   map[string][]*Session
}

func newLobby(s *Server) *lobby { return &lobby{srv: s, q: map[string][]*Session{}} }

func queueKey(control, size string) string { return control + "/" + size }

func splitKey(key string) (control, size string) {
	i := strings.IndexByte(key, '/')
	return key[:i], key[i+1:]
}

func (l *lobby) join(se *Session, control, size string) {
	l.leave(se)
	key := queueKey(control, size)
	r := rating.Default.R
	if got, err := l.srv.st.Rating(context.Background(), se.player.ID, control); err == nil {
		r = got.Rating
	}
	l.mu.Lock()
	se.mu.Lock()
	se.queue, se.since, se.rating = key, time.Now(), r
	se.mu.Unlock()
	l.q[key] = append(l.q[key], se)
	n := len(l.q[key])
	l.mu.Unlock()
	se.send(proto.TQueueStatus, proto.QueueStatus{Mode: control, Size: size, Waiting: n})
	l.tick()
}

func (l *lobby) leave(se *Session) {
	l.mu.Lock()
	defer l.mu.Unlock()
	se.mu.Lock()
	key := se.queue
	se.queue = ""
	se.mu.Unlock()
	if key == "" {
		return
	}
	q := l.q[key]
	for i, x := range q {
		if x == se {
			l.q[key] = append(q[:i], q[i+1:]...)
			return
		}
	}
}

type start struct {
	control, size string
	seats         []*Session
	bots          int
}

// tick forms matches and reports queue status.
func (l *lobby) tick() {
	l.mu.Lock()
	var starts []start
	for key, q := range l.q {
		control, size := splitKey(key)
		tc := l.srv.cfg.TimeControls[control]
		need := 2 * l.srv.c.Rules.Modes[size].PlayersPerTeam
		// Drop sessions that went away.
		kept := q[:0]
		for _, se := range q {
			if se.connected() {
				kept = append(kept, se)
			} else {
				se.mu.Lock()
				se.queue = ""
				se.mu.Unlock()
			}
		}
		q = kept
		for {
			seats := l.pick(q, need, tc)
			if seats == nil {
				break
			}
			starts = append(starts, start{control: control, size: size, seats: seats})
			q = without(q, seats)
		}
		if tc.Backfill && len(q) > 0 && time.Since(q[0].since) >= l.srv.cfg.BackfillAfter {
			n := len(q)
			if n > need {
				n = need
			}
			starts = append(starts, start{control: control, size: size, seats: q[:n], bots: need - n})
			q = q[n:]
		}
		l.q[key] = q
		for _, se := range q {
			se.send(proto.TQueueStatus, proto.QueueStatus{Mode: control, Size: size, Waiting: len(q), Seconds: int(time.Since(se.since).Seconds())})
		}
	}
	l.mu.Unlock()
	for _, st := range starts {
		for _, se := range st.seats {
			se.mu.Lock()
			se.queue = ""
			se.mu.Unlock()
		}
		l.srv.startMatch(st.control, st.size, st.seats, st.bots)
	}
}

// pick chooses need sessions from q for one match, or nil if none can be
// formed yet. Callers hold l.mu.
func (l *lobby) pick(q []*Session, need int, tc TimeControl) []*Session {
	if len(q) < need {
		return nil
	}
	if need == 2 && tc.Ranked {
		// Oldest waiter first; accept the closest rating within its window.
		a := q[0]
		window := l.srv.cfg.RatingWindow + l.srv.cfg.RatingWiden*time.Since(a.since).Seconds()
		best := -1
		for i := 1; i < len(q); i++ {
			d := a.rating - q[i].rating
			if d < 0 {
				d = -d
			}
			if d <= window && (best < 0 || d < absf(a.rating-q[best].rating)) {
				best = i
			}
		}
		if best < 0 {
			return nil
		}
		return []*Session{a, q[best]}
	}
	seats := append([]*Session(nil), q[:need]...)
	if need > 2 {
		// Snake the strongest players across the two teams: seats [0,n) are
		// team 0 and [n,2n) team 1 (engine block assignment).
		sort.SliceStable(seats, func(i, j int) bool { return seats[i].rating > seats[j].rating })
		n := need / 2
		var t0, t1 []*Session
		for i, se := range seats {
			if i%4 == 0 || i%4 == 3 {
				t0 = append(t0, se)
			} else {
				t1 = append(t1, se)
			}
		}
		for len(t0) > n {
			t1 = append(t1, t0[len(t0)-1])
			t0 = t0[:len(t0)-1]
		}
		for len(t1) > n {
			t0 = append(t0, t1[len(t1)-1])
			t1 = t1[:len(t1)-1]
		}
		seats = append(t0, t1...)
	}
	return seats
}

func absf(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}

func without(q, taken []*Session) []*Session {
	var out []*Session
outer:
	for _, se := range q {
		for _, t := range taken {
			if t == se {
				continue outer
			}
		}
		out = append(out, se)
	}
	return out
}

// ratingFor returns a player's stored rating for a control, or the default.
func (s *Server) ratingFor(ctx context.Context, playerID, control string) store.Rating {
	control = s.ratingKey(control)
	r, err := s.st.Rating(ctx, playerID, control)
	if err != nil {
		if !errors.Is(err, store.ErrNotFound) {
			s.log.Printf("rating %s/%s: %v", playerID, control, err)
		}
		return store.Rating{Rating: rating.Default.R, RD: rating.Default.RD, Vol: rating.Default.Vol}
	}
	return r
}
