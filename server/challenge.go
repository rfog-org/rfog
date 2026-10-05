package server

import (
	"crypto/rand"
	"errors"
	"strings"
	"time"

	"rfog/proto"
)

// Challenges are private matches between friends: one player opens a
// challenge and gets a code (sent as a link or read out), the other takes
// it, and the match starts for both, with no queue in between. A player
// has at most one open challenge; it ends when taken, cancelled, when its
// owner leaves, or after ChallengeTTL.

// ChallengeTTL is how long an untaken challenge stays open.
const ChallengeTTL = 30 * time.Minute

type challenge struct {
	code    string
	control string // resolved: the clock's casual twin
	from    *Session
	created time.Time
}

// challengeCode is six characters with no look-alikes, easy to read out.
func challengeCode() string {
	var b [6]byte
	_, _ = rand.Read(b[:])
	out := make([]byte, 6)
	for i, v := range b {
		out[i] = recoveryAlphabet[int(v)%len(recoveryAlphabet)]
	}
	return string(out)
}

// resolveClock turns a requested clock and rated flag into a control id
// (the clock, or its casual twin).
func (s *Server) resolveClock(mode string, rated bool) (string, error) {
	if _, ok := s.cfg.TimeControls[mode]; !ok {
		return "", errors.New("unknown clock " + mode)
	}
	if !rated && !strings.HasSuffix(mode, CasualSuffix) {
		if _, ok := s.cfg.TimeControls[mode+CasualSuffix]; ok {
			return mode + CasualSuffix, nil
		}
	}
	return mode, nil
}

func (ch *challenge) info(status string) proto.ChallengeInfo {
	return proto.ChallengeInfo{Code: ch.code, Mode: strings.TrimSuffix(ch.control, CasualSuffix), From: ch.from.player.Name, Status: status}
}

// challenge carries out a Challenge action for the session's player.
func (se *Session) challenge(c proto.Challenge) error {
	s := se.srv
	switch c.Action {
	case "create":
		if se.currentMatch() != nil {
			return errors.New("already in a match")
		}
		// Challenges between friends are always casual: a rating counts
		// only games against whoever the pairing finds. (Challenge.Rated
		// from older clients is ignored.)
		control, err := s.resolveClock(c.Mode, false)
		if err != nil {
			return err
		}
		if s.cfg.TimeControls[control].Async {
			return errors.New("daily challenges are not supported yet")
		}
		ch := &challenge{code: challengeCode(), control: control, from: se, created: time.Now()}
		s.mu.Lock()
		for code, old := range s.challenges {
			if old.from == se {
				delete(s.challenges, code) // one open challenge each
			}
		}
		s.challenges[ch.code] = ch
		s.mu.Unlock()
		se.send(proto.TChallengeInfo, ch.info("waiting"))
		return nil
	case "peek", "accept":
		code := strings.ToUpper(strings.TrimSpace(c.Code))
		s.mu.Lock()
		ch := s.challenges[code]
		if ch != nil && (time.Since(ch.created) > ChallengeTTL || !ch.from.connected()) {
			delete(s.challenges, code)
			ch = nil
		}
		s.mu.Unlock()
		if ch == nil {
			se.send(proto.TChallengeInfo, proto.ChallengeInfo{Code: code, Status: "gone"})
			return nil
		}
		if c.Action == "peek" {
			se.send(proto.TChallengeInfo, ch.info("waiting"))
			return nil
		}
		switch {
		case ch.from == se:
			return errors.New("that is your own challenge; send the link to a friend")
		case se.currentMatch() != nil:
			return errors.New("already in a match")
		case ch.from.currentMatch() != nil:
			return errors.New("they are in a match now; try again soon")
		}
		s.mu.Lock()
		if s.challenges[code] != ch { // taken a moment ago
			s.mu.Unlock()
			se.send(proto.TChallengeInfo, proto.ChallengeInfo{Code: code, Status: "gone"})
			return nil
		}
		delete(s.challenges, code)
		s.mu.Unlock()
		s.lobby.leave(se)
		s.lobby.leave(ch.from)
		s.startMatch(ch.control, "1v1", []*Session{ch.from, se}, 0)
		return nil
	case "cancel":
		s.mu.Lock()
		for code, ch := range s.challenges {
			if ch.from == se {
				delete(s.challenges, code)
				se.send(proto.TChallengeInfo, ch.info("cancelled"))
			}
		}
		s.mu.Unlock()
		return nil
	}
	return errors.New("unknown challenge action")
}

// expireChallenges drops challenges past their time or whose owner left.
func (s *Server) expireChallenges() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for code, ch := range s.challenges {
		if time.Since(ch.created) > ChallengeTTL || !ch.from.connected() {
			delete(s.challenges, code)
		}
	}
}
