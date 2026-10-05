package server

import (
	"context"
	"strings"
	"testing"
	"time"

	"rfog/engine"
	"rfog/proto"
	"rfog/server/store"
)

// accountDo sends an Account action and returns the answer or the error.
func accountDo(t *testing.T, c *tclient, a proto.Account) (proto.AccountDone, string) {
	t.Helper()
	c.send(proto.TAccount, a)
	for f := range c.in {
		switch f.T {
		case proto.TAccountDone:
			var d proto.AccountDone
			_ = f.As(&d)
			return d, ""
		case proto.TError:
			var e proto.Error
			_ = f.As(&e)
			return proto.AccountDone{}, e.Msg
		}
	}
	t.Fatal("connection closed")
	return proto.AccountDone{}, ""
}

// A guest keeps its progress as an account; the account works from
// several devices at once; a forgotten password comes back with the
// recovery code (which is then used up); signing out works per device and
// everywhere; and deleting the account removes it, leaving past matches
// to the other players under "(deleted)".
func TestAccountLifecycle(t *testing.T) {
	s, stop := newTestServer(t, Config{})
	defer stop()
	ctx := context.Background()

	// A guest plays, then saves its progress.
	g, msg := dialAuth(t, s, proto.Auth{Name: "drifter"})
	if msg != "" || !g.welcome.Guest {
		t.Fatalf("guest: %q %+v", msg, g.welcome)
	}
	id := g.welcome.Player
	if _, msg := accountDo(t, g, proto.Account{Action: "save", Name: "drifter", Password: "abc"}); !strings.Contains(msg, "at least") {
		t.Fatalf("short password accepted: %q", msg)
	}
	d, msg := accountDo(t, g, proto.Account{Action: "save", Name: "Drifter", Password: "glass42"})
	if msg != "" || d.Guest || d.Name != "Drifter" || len(d.Recovery) != 19 {
		t.Fatalf("save: %q %+v", msg, d)
	}
	code := d.Recovery
	if _, msg := accountDo(t, g, proto.Account{Action: "save", Name: "x", Password: "glass42"}); msg == "" {
		t.Fatal("saved twice")
	}
	// Same id: the guest's history and ratings are the account's now.
	p, err := s.st.PlayerByName(ctx, "drifter")
	if err != nil || p.ID != id || p.Guest {
		t.Fatalf("upgraded row: %+v %v", p, err)
	}
	// The guest's token still signs that device in, now as the account.
	// (An account has one live connection at a time: each dial below
	// replaces the previous one, so actions go through the newest.)
	desk, msg := dialAuth(t, s, proto.Auth{Token: g.tok})
	if msg != "" || desk.welcome.Guest || desk.welcome.Player != id {
		t.Fatalf("guest token after save: %q %+v", msg, desk.welcome)
	}

	// Two devices: a phone signs in, and the desktop's token still works.
	phone, msg := dialAuth(t, s, proto.Auth{Name: "drifter", Password: "glass42"})
	if msg != "" || phone.welcome.Player != id || phone.tok == desk.tok {
		t.Fatalf("phone: %q", msg)
	}
	desk, msg = dialAuth(t, s, proto.Auth{Token: desk.tok})
	if msg != "" {
		t.Fatalf("desktop signed out by the phone: %q", msg)
	}

	// Change the password: the old one stops working.
	if _, msg := accountDo(t, desk, proto.Account{Action: "password", Password: "nope", NewPassword: "fibre99"}); msg != "wrong password" {
		t.Fatalf("password with a wrong current: %q", msg)
	}
	if _, msg := accountDo(t, desk, proto.Account{Action: "password", Password: "glass42", NewPassword: "fibre99"}); msg != "" {
		t.Fatalf("password: %q", msg)
	}
	if _, msg := dialAuth(t, s, proto.Auth{Name: "drifter", Password: "glass42"}); msg != errBadLogin.Error() {
		t.Fatalf("old password still works: %q", msg)
	}

	// Forgot it: the recovery code sets a new one, in any case and spacing,
	// signs the other devices out, and gives a fresh code.
	low := strings.ToLower(strings.ReplaceAll(code, "-", " "))
	rec, msg := dialAuth(t, s, proto.Auth{Name: "drifter", Recovery: low, Password: "newline7"})
	if msg != "" || rec.welcome.Player != id || rec.welcome.Recovery == "" || rec.welcome.Recovery == code {
		t.Fatalf("recover: %q %+v", msg, rec.welcome)
	}
	if _, msg := dialAuth(t, s, proto.Auth{Token: phone.tok}); msg == "" {
		t.Fatal("a recovery left other devices signed in")
	}
	if _, msg := dialAuth(t, s, proto.Auth{Token: desk.tok}); msg == "" {
		t.Fatal("a recovery left other devices signed in")
	}
	if _, msg := dialAuth(t, s, proto.Auth{Name: "drifter", Recovery: code, Password: "again99"}); msg == "" {
		t.Fatal("a used recovery code worked twice")
	}

	// Sign one device out (from another), then every device.
	two, _ := dialAuth(t, s, proto.Auth{Name: "drifter", Password: "newline7"})
	if _, msg := accountDo(t, two, proto.Account{Action: "logout", Token: rec.tok}); msg != "" {
		t.Fatalf("logout: %q", msg)
	}
	if _, msg := dialAuth(t, s, proto.Auth{Token: rec.tok}); msg == "" {
		t.Fatal("signed-out token works")
	}
	three, msg := dialAuth(t, s, proto.Auth{Token: two.tok})
	if msg != "" {
		t.Fatalf("logout signed out another device: %q", msg)
	}
	if _, msg := accountDo(t, three, proto.Account{Action: "logout_all"}); msg != "" {
		t.Fatalf("logout_all: %q", msg)
	}
	if _, msg := dialAuth(t, s, proto.Auth{Token: two.tok}); msg == "" {
		t.Fatal("logout_all left a device signed in")
	}

	// Delete: the password is asked for; then the account is gone and the
	// name is free again.
	last, _ := dialAuth(t, s, proto.Auth{Name: "drifter", Password: "newline7"})
	if _, msg := accountDo(t, last, proto.Account{Action: "delete", Password: "wrong1"}); msg != "wrong password" {
		t.Fatalf("delete with a wrong password: %q", msg)
	}
	if _, msg := accountDo(t, last, proto.Account{Action: "delete", Password: "newline7"}); msg != "" {
		t.Fatalf("delete: %q", msg)
	}
	if _, err := s.st.GetPlayer(ctx, id); err == nil {
		t.Fatal("deleted player still stored")
	}
	if _, msg := dialAuth(t, s, proto.Auth{Name: "drifter", Password: "newline7"}); msg == "" {
		t.Fatal("deleted account still logs in")
	}
	if fresh, msg := dialAuth(t, s, proto.Auth{Name: "drifter", Password: "other77", Register: true}); msg != "" || fresh.welcome.Player == id {
		t.Fatalf("name not freed: %q", msg)
	}
}

// Recovery codes are four groups of four from an alphabet without
// look-alikes, and are not repeated.
func TestRecoveryCodes(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 200; i++ {
		c := NewRecoveryCode()
		if len(c) != 19 || strings.Count(c, "-") != 3 || strings.ContainsAny(c, "01OI") {
			t.Fatalf("code %q", c)
		}
		if seen[c] {
			t.Fatalf("repeated code %q", c)
		}
		seen[c] = true
	}
}

// Deleting a player renames their seat in past matches, in the record and
// in the replay (whose final hash is recomputed), and leaves the other
// player's history alone.
func TestDeleteRenamesPastMatches(t *testing.T) {
	s, stop := newTestServer(t, Config{})
	defer stop()
	ctx := context.Background()
	gone, _ := dialAuth(t, s, proto.Auth{Name: "gone", Password: "secret1", Register: true})
	stays, _ := dialAuth(t, s, proto.Auth{Name: "stays", Password: "secret2", Register: true})
	setup := engine.Setup{ID: "m1", Mode: "1v1", Seed: 3, TimeControl: "casual",
		Players: []engine.SetupPlayer{{Name: "gone", Hero: "hask"}, {Name: "stays", Hero: "wren"}}}
	st0, err := engine.NewMatch(s.c, setup)
	if err != nil {
		t.Fatal(err)
	}
	r := engine.NewReplay(setup, st0)
	r.Record(map[int][]engine.Order{})
	final, _, err := r.Run(s.c)
	if err != nil {
		t.Fatal(err)
	}
	r.FinalHash = engine.Hash(final)
	b, _ := r.Marshal()
	if err := s.st.CreateMatch(ctx, store.Match{ID: "m1", Mode: "1v1", Map: "relay8", TimeCtl: "casual", Seed: 3, Started: time.Now(),
		Players: []store.MatchPlayer{{PlayerID: gone.welcome.Player, Name: "gone", Slot: 0, Team: 0, Hero: "hask"},
			{PlayerID: stays.welcome.Player, Name: "stays", Slot: 1, Team: 1, Hero: "wren"}}}); err != nil {
		t.Fatal(err)
	}
	if err := s.st.EndMatch(ctx, "m1", 1, "score", b); err != nil {
		t.Fatal(err)
	}
	if _, msg := accountDo(t, gone, proto.Account{Action: "delete", Password: "secret1"}); msg != "" {
		t.Fatalf("delete: %q", msg)
	}
	hist, err := s.st.History(ctx, stays.welcome.Player, 10)
	if err != nil || len(hist) != 1 {
		t.Fatalf("other player's history: %v %v", hist, err)
	}
	for _, p := range hist[0].Players {
		if p.Slot == 0 && p.Name != store.NameDeleted {
			t.Fatalf("seat not renamed: %+v", p)
		}
	}
	rb, err := s.st.Replay(ctx, "m1")
	if err != nil {
		t.Fatal(err)
	}
	r2, err := engine.UnmarshalReplay(rb)
	if err != nil {
		t.Fatal(err)
	}
	if r2.Setup.Players[0].Name != store.NameDeleted || r2.Initial.Players[0].Name != store.NameDeleted || r2.Setup.Players[1].Name != "stays" {
		t.Fatalf("replay names: %+v", r2.Setup.Players)
	}
	if strings.Contains(string(rb), `"gone"`) {
		t.Fatal("the deleted name is still in the replay")
	}
	if f2, _, err := r2.Run(s.c); err != nil || engine.Hash(f2) != r2.FinalHash {
		t.Fatalf("replay no longer verifies: %v", err)
	}
}

// One place at a time: signing in from a second device takes the game
// over, and the first device is told so (it must not reconnect and take
// it back); a recently finished match is in the next Welcome.
func TestTakeoverAndLastMatch(t *testing.T) {
	s, stop := newTestServer(t, Config{})
	defer stop()
	ctx := context.Background()
	desk, _ := dialAuth(t, s, proto.Auth{Name: "roam", Password: "secret1", Register: true})
	phone, msg := dialAuth(t, s, proto.Auth{Name: "roam", Password: "secret1"})
	if msg != "" {
		t.Fatal(msg)
	}
	got := ""
	for f := range desk.in {
		if f.T == proto.TError {
			var e proto.Error
			_ = f.As(&e)
			got = e.Code
		}
	}
	if got != proto.ErrDisplaced {
		t.Fatalf("first device was not told it was displaced (got %q)", got)
	}
	// A match that ended a moment ago shows up for the returning player.
	if err := s.st.CreateMatch(ctx, store.Match{ID: "m9", Mode: "1v1", Map: "relay8", TimeCtl: "casual", Seed: 1, Started: time.Now(),
		Players: []store.MatchPlayer{{PlayerID: phone.welcome.Player, Name: "roam", Slot: 0, Team: 0, Hero: "hask"}, {PlayerID: "bot", Name: "bot", Slot: 1, Team: 1, Hero: "wren", Bot: true}}}); err != nil {
		t.Fatal(err)
	}
	if err := s.st.EndMatch(ctx, "m9", 0, "score", nil); err != nil {
		t.Fatal(err)
	}
	back, _ := dialAuth(t, s, proto.Auth{Token: phone.tok})
	if back.welcome.Last == nil || back.welcome.Last.Match != "m9" || back.welcome.Last.You != 0 || back.welcome.Last.Winner != 0 {
		t.Fatalf("last match: %+v", back.welcome.Last)
	}
}

// Guessing is throttled: after MaxFails wrong passwords a name is locked
// for a while, even for the right password; a success clears the count.
func TestLoginThrottle(t *testing.T) {
	old := FailDelay
	FailDelay = 0
	defer func() { FailDelay = old }()
	s, stop := newTestServer(t, Config{})
	defer stop()
	dialAuth(t, s, proto.Auth{Name: "vault", Password: "secret1", Register: true})
	for i := 0; i < MaxFails-1; i++ {
		if _, msg := dialAuth(t, s, proto.Auth{Name: "vault", Password: "nope" + strings.Repeat("x", i)}); msg != errBadLogin.Error() {
			t.Fatalf("attempt %d: %q", i, msg)
		}
	}
	if _, msg := dialAuth(t, s, proto.Auth{Name: "vault", Password: "secret1"}); msg != "" {
		t.Fatalf("right password below the limit: %q", msg)
	}
	for i := 0; i < MaxFails; i++ {
		dialAuth(t, s, proto.Auth{Name: "vault", Password: "wrong1"})
	}
	if _, msg := dialAuth(t, s, proto.Auth{Name: "VAULT", Password: "secret1"}); msg != errTooMany.Error() {
		t.Fatalf("locked name let in: %q", msg)
	}
}

// Every clock can be played rated or casual: rated goes to the clock's
// pool, casual to its casual twin; guests can play any clock casual but
// none rated; older clients' control names still mean what they did; and
// clocks of one speed share a rating category.
func TestRatedOrCasualClocks(t *testing.T) {
	s, stop := newTestServer(t, Config{})
	defer stop()
	acct, _ := dialAuth(t, s, proto.Auth{Name: "clocky", Password: "secret1", Register: true})
	status := func(c *tclient, q proto.Queue) string {
		c.send(proto.TQueue, q)
		for f := range c.in {
			switch f.T {
			case proto.TQueueStatus:
				var st proto.QueueStatus
				_ = f.As(&st)
				c.send(proto.TCancel, nil)
				for g := range c.in { // the cancel's answer: a status with no mode
					var cs proto.QueueStatus
					if g.T == proto.TQueueStatus && g.As(&cs) == nil && cs.Mode == "" {
						break
					}
				}
				return st.Mode
			case proto.TError:
				var e proto.Error
				_ = f.As(&e)
				return "error: " + e.Msg
			}
		}
		return "closed"
	}
	for _, c := range []struct {
		q    proto.Queue
		want string
	}{
		{proto.Queue{Mode: "30s", Rated: true, Clock: true}, "30s"},
		{proto.Queue{Mode: "30s", Clock: true}, "30s" + CasualSuffix},
		{proto.Queue{Mode: "120s+", Rated: true, Clock: true}, "120s+"},
		{proto.Queue{Mode: "blitz"}, "30s"},
		{proto.Queue{Mode: "casual"}, "60s" + CasualSuffix},
	} {
		if got := status(acct, c.q); got != c.want {
			t.Errorf("queue %+v: %q, want %q", c.q, got, c.want)
		}
	}
	guest, _ := dialAuth(t, s, proto.Auth{Name: "wanderer"})
	if got := status(guest, proto.Queue{Mode: "10s", Clock: true}); got != "10s"+CasualSuffix {
		t.Errorf("guest casual: %q", got)
	}
	if got := status(guest, proto.Queue{Mode: "10s", Rated: true, Clock: true}); got != "error: log in to play rated" {
		t.Errorf("guest rated: %q", got)
	}
	if s.ratingKey("20s") != "blitz" || s.ratingKey("30s"+CasualSuffix) != "blitz" || s.ratingKey("90s+") != "long-haul" {
		t.Error("clocks do not share their category's rating")
	}
}

// Challenge a friend: a code is made, the friend sees who and which clock,
// accepts, and the match starts for both; a code works once; challenges
// are always casual (a request for rated is ignored); your own code cannot
// be taken by you.
func TestChallengeAFriend(t *testing.T) {
	s, stop := newTestServer(t, Config{})
	defer stop()
	host, _ := dialAuth(t, s, proto.Auth{Name: "host", Password: "secret1", Register: true})
	info := func(c *tclient) proto.ChallengeInfo {
		var ci proto.ChallengeInfo
		_ = c.expect(proto.TChallengeInfo).As(&ci)
		return ci
	}
	host.send(proto.TChallenge, proto.Challenge{Action: "create", Mode: "30s", Rated: true})
	ci := info(host)
	if ci.Status != "waiting" || len(ci.Code) != 6 || ci.Mode != "30s" || ci.Rated {
		t.Fatalf("create: %+v", ci)
	}
	host.send(proto.TChallenge, proto.Challenge{Action: "accept", Code: ci.Code})
	if msg := host.expectError(); !strings.Contains(msg, "your own") {
		t.Fatalf("own challenge: %q", msg)
	}
	guest, _ := dialAuth(t, s, proto.Auth{Name: "pal"})
	guest.send(proto.TChallenge, proto.Challenge{Action: "peek", Code: strings.ToLower(ci.Code)})
	if p := info(guest); p.From != "host" || p.Status != "waiting" {
		t.Fatalf("peek: %+v", p)
	}
	// A casual challenge the guest can take.
	host.send(proto.TChallenge, proto.Challenge{Action: "create", Mode: "20s"})
	ci = info(host)
	guest.send(proto.TChallenge, proto.Challenge{Action: "accept", Code: ci.Code})
	var a, b proto.MatchFound
	_ = host.expect(proto.TMatchFound).As(&a)
	_ = guest.expect(proto.TMatchFound).As(&b)
	if a.Match == "" || a.Match != b.Match || a.Time != "20s"+CasualSuffix {
		t.Fatalf("match: %+v / %+v", a, b)
	}
	// A code works once.
	other, _ := dialAuth(t, s, proto.Auth{Name: "late"})
	other.send(proto.TChallenge, proto.Challenge{Action: "accept", Code: ci.Code})
	if g := info(other); g.Status != "gone" {
		t.Fatalf("taken code: %+v", g)
	}
}
