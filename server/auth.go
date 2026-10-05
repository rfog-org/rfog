package server

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"golang.org/x/crypto/argon2"
)

// Password hashing: argon2id with modest parameters so a small self-hosted
// box handles logins comfortably. The encoded form carries its parameters,
// so they can be raised later without invalidating old hashes.
const (
	argonTime    = 1
	argonMemory  = 32 * 1024 // KiB
	argonThreads = 2
	argonKeyLen  = 32
)

// MinPassword is the shortest password accepted at registration.
const MinPassword = 6

// HashPassword returns an encoded argon2id hash.
func HashPassword(pw string) (string, error) {
	if len(pw) < MinPassword {
		return "", fmt.Errorf("password must be at least %d characters", MinPassword)
	}
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key := argon2.IDKey([]byte(pw), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s", argon2.Version, argonMemory, argonTime, argonThreads,
		base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(key)), nil
}

// VerifyPassword checks pw against an encoded hash.
func VerifyPassword(encoded, pw string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false
	}
	var mem, t uint32
	var p uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &mem, &t, &p); err != nil {
		return false
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return false
	}
	got := argon2.IDKey([]byte(pw), salt, t, mem, p, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1
}

var errBadLogin = errors.New("unknown account or wrong password")

// Throttling: a name that fails to log in (wrong password or recovery
// code) MaxFails times within FailWindow is refused for LockFor. Every
// failure also costs FailDelay, so guessing is slow even below the limit.
// Kept in memory: a restart forgives, which is fine for a game.
var (
	MaxFails   = 5
	FailWindow = 10 * time.Minute
	LockFor    = 5 * time.Minute
	FailDelay  = 400 * time.Millisecond
)

var errTooMany = errors.New("too many attempts; try again in a few minutes")

type guard struct {
	fails []time.Time
	until time.Time
}

// guardCheck refuses a name that is locked out.
func (s *Server) guardCheck(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	g := s.guards[strings.ToLower(name)]
	if g != nil && time.Now().Before(g.until) {
		return errTooMany
	}
	return nil
}

// guardFail records a failure (and waits FailDelay); guardOK forgets them.
func (s *Server) guardFail(name string) {
	now := time.Now()
	s.mu.Lock()
	key := strings.ToLower(name)
	g := s.guards[key]
	if g == nil {
		g = &guard{}
		s.guards[key] = g
	}
	keep := g.fails[:0]
	for _, t := range g.fails {
		if now.Sub(t) < FailWindow {
			keep = append(keep, t)
		}
	}
	g.fails = append(keep, now)
	if len(g.fails) >= MaxFails {
		g.until, g.fails = now.Add(LockFor), nil
	}
	s.mu.Unlock()
	time.Sleep(FailDelay)
}

func (s *Server) guardOK(name string) {
	s.mu.Lock()
	delete(s.guards, strings.ToLower(name))
	s.mu.Unlock()
}
