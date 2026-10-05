package server

import (
	"context"
	"crypto/rand"
	"errors"
	"strings"

	"rfog/engine"
	"rfog/proto"
	"rfog/server/store"
)

// Accounts ask for as little as possible: a name and a password, nothing
// else. There is no email, so a lost password is recovered with a code
// shown once when the account is made (or when a new one is asked for),
// stored only as a hash, like the password.

// recoveryAlphabet leaves out letters and digits that read alike (0/O, 1/I).
const recoveryAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"

// NewRecoveryCode returns a fresh code: four groups of four, e.g.
// K7QM-2XPA-9RTD-HV4C.
func NewRecoveryCode() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	var out strings.Builder
	for i, v := range b {
		if i > 0 && i%4 == 0 {
			out.WriteByte('-')
		}
		out.WriteByte(recoveryAlphabet[int(v)%len(recoveryAlphabet)])
	}
	return out.String()
}

// normRecovery is a typed code as it was hashed: upper case, no dashes or
// spaces, so "k7qm 2xpa…" works as well as "K7QM-2XPA-…".
func normRecovery(code string) string {
	var b strings.Builder
	for _, r := range strings.ToUpper(code) {
		if r != '-' && r != ' ' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// recoveryPair makes a code and the hash that is stored for it.
func recoveryPair() (code, hash string, err error) {
	code = NewRecoveryCode()
	hash, err = HashPassword(normRecovery(code))
	return code, hash, err
}

// authenticate resolves an Auth message to a stored player (see
// proto.Auth). recovery is a new recovery code to show the player once,
// when one was made (a new account, a recovered one).
func (s *Server) authenticate(ctx context.Context, a proto.Auth) (p store.Player, recovery string, err error) {
	if a.Token != "" {
		p, err := s.st.PlayerByToken(ctx, a.Token)
		if err == nil {
			return p, "", nil
		}
		if !errors.Is(err, store.ErrNotFound) {
			return store.Player{}, "", err
		}
	}
	name := cleanName(a.Name)
	if name == "" {
		return store.Player{}, "", errors.New("name required")
	}
	switch {
	case a.Recovery != "":
		// A forgotten password: the code sets a new one, then is used up;
		// every other device is signed out, since someone else may have
		// had the password.
		if err := s.guardCheck(name); err != nil {
			return store.Player{}, "", err
		}
		p, err := s.st.PlayerByName(ctx, name)
		if errors.Is(err, store.ErrNotFound) || (err == nil && (p.Recovery == "" || !VerifyPassword(p.Recovery, normRecovery(a.Recovery)))) {
			s.guardFail(name)
			return store.Player{}, "", errors.New("unknown account or wrong recovery code")
		}
		s.guardOK(name)
		if err != nil {
			return store.Player{}, "", err
		}
		hash, err := HashPassword(a.Password)
		if err != nil {
			return store.Player{}, "", err
		}
		code, rhash, err := recoveryPair()
		if err != nil {
			return store.Player{}, "", err
		}
		if err := s.st.SetPassword(ctx, p.ID, hash); err != nil {
			return store.Player{}, "", err
		}
		if err := s.st.SetRecovery(ctx, p.ID, rhash); err != nil {
			return store.Player{}, "", err
		}
		if err := s.st.DropTokens(ctx, p.ID); err != nil {
			return store.Player{}, "", err
		}
		p.Token = store.NewToken()
		return p, code, s.st.AddToken(ctx, p.ID, p.Token)
	case a.Register:
		hash, err := HashPassword(a.Password)
		if err != nil {
			return store.Player{}, "", err
		}
		p, err := s.st.CreateAccount(ctx, name, hash)
		if errors.Is(err, store.ErrExists) {
			return store.Player{}, "", errors.New("that name is taken")
		}
		if err != nil {
			return store.Player{}, "", err
		}
		code, rhash, err := recoveryPair()
		if err != nil {
			return store.Player{}, "", err
		}
		return p, code, s.st.SetRecovery(ctx, p.ID, rhash)
	case a.Password != "":
		if err := s.guardCheck(name); err != nil {
			return store.Player{}, "", err
		}
		p, err := s.st.PlayerByName(ctx, name)
		if errors.Is(err, store.ErrNotFound) || (err == nil && !VerifyPassword(p.PassHash, a.Password)) {
			s.guardFail(name)
			return store.Player{}, "", errBadLogin
		}
		s.guardOK(name)
		if err != nil {
			return store.Player{}, "", err
		}
		// A token for this device; the others stay signed in.
		p.Token = store.NewToken()
		return p, "", s.st.AddToken(ctx, p.ID, p.Token)
	}
	if _, err := s.st.PlayerByName(ctx, name); err == nil {
		return store.Player{}, "", errors.New("that name is registered; log in with a password")
	}
	p, err = s.st.CreateGuest(ctx, name)
	return p, "", err
}

// account carries out an Account action for the session's player.
func (se *Session) account(ctx context.Context, a proto.Account) error {
	s := se.srv
	se.mu.Lock()
	p := se.player
	se.mu.Unlock()
	done := proto.AccountDone{Action: a.Action, Name: p.Name, Guest: p.Guest}
	needPassword := func() error {
		if p.Guest {
			return errors.New("guests have no password")
		}
		cur, err := s.st.GetPlayer(ctx, p.ID)
		if err != nil {
			return err
		}
		if err := s.guardCheck(p.Name); err != nil {
			return err
		}
		if !VerifyPassword(cur.PassHash, a.Password) {
			s.guardFail(p.Name)
			return errors.New("wrong password")
		}
		s.guardOK(p.Name)
		return nil
	}
	switch a.Action {
	case "save":
		// A guest keeps everything it has played as an account.
		if !p.Guest {
			return errors.New("already an account")
		}
		name := cleanName(a.Name)
		if name == "" {
			return errors.New("name required")
		}
		hash, err := HashPassword(a.Password)
		if err != nil {
			return err
		}
		code, rhash, err := recoveryPair()
		if err != nil {
			return err
		}
		switch err := s.st.UpgradeGuest(ctx, p.ID, name, hash, rhash); {
		case errors.Is(err, store.ErrExists):
			return errors.New("that name is taken")
		case err != nil:
			return err
		}
		se.mu.Lock()
		se.player.Name, se.player.Guest, se.player.PassHash, se.player.Recovery = name, false, hash, rhash
		se.mu.Unlock()
		done.Name, done.Guest, done.Recovery = name, false, code
	case "password":
		if err := needPassword(); err != nil {
			return err
		}
		hash, err := HashPassword(a.NewPassword)
		if err != nil {
			return err
		}
		if err := s.st.SetPassword(ctx, p.ID, hash); err != nil {
			return err
		}
	case "recovery":
		if err := needPassword(); err != nil {
			return err
		}
		code, rhash, err := recoveryPair()
		if err != nil {
			return err
		}
		if err := s.st.SetRecovery(ctx, p.ID, rhash); err != nil {
			return err
		}
		done.Recovery = code
	case "logout":
		tok := a.Token
		if tok == "" {
			tok = p.Token
		}
		if err := s.st.DropToken(ctx, p.ID, tok); err != nil {
			return err
		}
	case "logout_all":
		if err := s.st.DropTokens(ctx, p.ID); err != nil {
			return err
		}
	case "delete":
		if !p.Guest {
			if err := needPassword(); err != nil {
				return err
			}
		}
		if se.currentMatch() != nil {
			return errors.New("finish or leave your match first")
		}
		if err := s.deletePlayer(ctx, p); err != nil {
			return err
		}
	default:
		return errors.New("unknown account action")
	}
	se.send(proto.TAccountDone, done)
	return nil
}

// deletePlayer removes a player and everything tied to them. Past matches
// stay (the other players' history), with this player's seat renamed in
// the record and in the replay.
func (s *Server) deletePlayer(ctx context.Context, p store.Player) error {
	ms, err := s.st.History(ctx, p.ID, 1<<30)
	if err != nil {
		return err
	}
	for _, m := range ms {
		b, err := s.st.Replay(ctx, m.ID)
		if err != nil || len(b) == 0 {
			continue
		}
		r, err := engine.UnmarshalReplay(b)
		if err != nil {
			continue
		}
		for _, mp := range m.Players {
			if mp.PlayerID != p.ID {
				continue
			}
			if mp.Slot < len(r.Setup.Players) {
				r.Setup.Players[mp.Slot].Name = store.NameDeleted
			}
			for i := range r.Initial.Players {
				if r.Initial.Players[i].ID == mp.Slot {
					r.Initial.Players[i].Name = store.NameDeleted
				}
			}
		}
		// The final state's hash covers names; recompute it, or drop it
		// when the replay no longer runs under these rules.
		if r.FinalHash != "" {
			r.FinalHash = ""
			if st, _, err := r.Run(s.c); err == nil {
				r.FinalHash = engine.Hash(st)
			}
		}
		if nb, err := r.Marshal(); err == nil {
			_ = s.st.SetReplay(ctx, m.ID, nb)
		}
	}
	if err := s.st.DeletePlayer(ctx, p.ID); err != nil {
		return err
	}
	s.mu.Lock()
	delete(s.sessions, p.ID)
	s.mu.Unlock()
	return nil
}
