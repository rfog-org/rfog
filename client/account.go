package client

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"rfog/proto"
)

// The account page, online: what a guest can do (keep its progress as an
// account, log in as someone else) and what an account can do (change its
// password, get a new recovery code, sign out here or everywhere, delete
// itself). Accounts are a name and a password, nothing more; a recovery
// code, shown once, stands in for an email.

// acctAction is one line of the account page.
type acctAction struct {
	id, label string
	fields    []acctField // asked for before the action is sent
}

type acctField struct {
	label  string
	secret bool
}

// acctActions lists what the signed-in player can do.
func acctActions(guest bool) []acctAction {
	if guest {
		return []acctAction{
			{id: "save", label: "save my progress (make an account)", fields: []acctField{{"name", false}, {"password", true}}},
			{id: "switch", label: "log in to another account"},
		}
	}
	return []acctAction{
		{id: "password", label: "change password", fields: []acctField{{"current", true}, {"new", true}}},
		{id: "recovery", label: "new recovery code", fields: []acctField{{"password", true}}},
		{id: "logout", label: "sign out of this device"},
		{id: "logout_all", label: "sign out everywhere"},
		{id: "switch", label: "log in to another account"},
		{id: "delete", label: "delete my account", fields: []acctField{{"password", true}}},
	}
}

// acctState is the account page's state inside the online screen.
type acctState struct {
	sel    int
	form   *acctAction // the action whose fields are being filled
	vals   []string
	focus  int
	sure   bool   // delete asks twice
	code   string // a recovery code to show once
	notice string
}

func (o *onlineScreen) openAccount(a *App) {
	o.state, o.err = "account", ""
	o.acct = acctState{}
}

// accountKey handles keys on the account page (and its forms and the
// recovery code card).
func (o *onlineScreen) accountKey(a *App, k tea.KeyMsg) (screen, tea.Cmd) {
	s := &o.acct
	if s.code != "" { // the recovery code card: any key once it is noted
		if isKey(k, "enter", "esc", " ") {
			s.code = ""
			if o.state == "account" && a.net != nil && !a.net.welcome.Guest && s.notice == "" {
				s.notice = "recovery code saved? it will not be shown again"
			}
		}
		return o, nil
	}
	if s.form != nil {
		return o.acctFormKey(a, k)
	}
	acts := acctActions(a.net.welcome.Guest)
	switch {
	case isKey(k, "esc", "q"):
		o.state = "menu"
	case isKey(k, "down", "j", "tab"):
		s.sel, s.sure = (s.sel+1)%len(acts), false
	case isKey(k, "up", "k", "shift+tab"):
		s.sel, s.sure = (s.sel+len(acts)-1)%len(acts), false
	case isKey(k, "enter", " "):
		act := acts[s.sel]
		switch act.id {
		case "switch":
			o.state, o.err = "login", ""
			o.form = newLoginForm(a)
			return o, nil
		case "delete":
			if !s.sure {
				s.sure = true
				o.err = "this deletes your account, ratings and history; enter again to go on"
				return o, nil
			}
		}
		o.err = ""
		if len(act.fields) == 0 {
			return o, o.sendAccount(a, proto.Account{Action: act.id, Token: a.set.Token})
		}
		s.form, s.vals, s.focus = &act, make([]string, len(act.fields)), 0
		if act.id == "save" {
			s.vals[0] = a.net.welcome.Name
			s.focus = 1
		}
	}
	return o, nil
}

func (o *onlineScreen) acctFormKey(a *App, k tea.KeyMsg) (screen, tea.Cmd) {
	s := &o.acct
	f := s.form
	switch {
	case isKey(k, "esc"):
		s.form, s.sure, o.err = nil, false, ""
	case isKey(k, "down", "tab"):
		s.focus = (s.focus + 1) % len(f.fields)
	case isKey(k, "up", "shift+tab"):
		s.focus = (s.focus + len(f.fields) - 1) % len(f.fields)
	case isKey(k, "enter"):
		if s.focus < len(f.fields)-1 {
			s.focus++
			return o, nil
		}
		for i, v := range s.vals {
			if strings.TrimSpace(v) == "" {
				o.err = f.fields[i].label + " required"
				s.focus = i
				return o, nil
			}
		}
		req := proto.Account{Action: f.id}
		switch f.id {
		case "save":
			req.Name, req.Password = strings.TrimSpace(s.vals[0]), s.vals[1]
		case "password":
			req.Password, req.NewPassword = s.vals[0], s.vals[1]
		default:
			req.Password = s.vals[0]
		}
		return o, o.sendAccount(a, req)
	case isKey(k, "backspace"):
		if v := s.vals[s.focus]; len(v) > 0 {
			s.vals[s.focus] = v[:len(v)-1]
		}
	default:
		if len(k.Runes) > 0 && k.Type == tea.KeyRunes && len(s.vals[s.focus]) < 64 {
			s.vals[s.focus] += string(k.Runes)
		}
	}
	return o, nil
}

func (o *onlineScreen) sendAccount(a *App, req proto.Account) tea.Cmd {
	if err := a.net.send(proto.TAccount, req); err != nil {
		o.err = err.Error()
	}
	return nil
}

// accountDone applies the server's answer to an account action.
func (o *onlineScreen) accountDone(a *App, d proto.AccountDone) (screen, tea.Cmd) {
	s := &o.acct
	s.form, s.sure, o.err = nil, false, ""
	switch d.Action {
	case "save":
		a.net.welcome.Name, a.net.welcome.Guest = d.Name, false
		a.set.Account, a.set.Name = d.Name, d.Name
		a.saveSettings()
		s.sel, s.code, s.notice = 0, d.Recovery, "progress saved: you are "+d.Name
	case "password":
		s.notice = "password changed"
	case "recovery":
		s.code, s.notice = d.Recovery, ""
	case "logout", "logout_all", "delete":
		// Signed out (or gone): forget this device's token and start over.
		a.disconnect()
		a.set.Token = ""
		if d.Action == "delete" {
			a.set.Account = ""
		}
		a.saveSettings()
		o.state = "login"
		o.form = newLoginForm(a)
		switch d.Action {
		case "delete":
			o.err = "account deleted"
		case "logout_all":
			o.err = "signed out everywhere"
		default:
			o.err = "signed out"
		}
	}
	return o, nil
}

// accountView draws the account page, a form, or the recovery code card.
func (o *onlineScreen) accountView(a *App) []string {
	st := &a.st
	s := &o.acct
	if s.code != "" {
		return recoveryCard(a, s.code)
	}
	w := a.net.welcome
	who := st.Accent.Render(w.Name)
	if w.Guest {
		who += st.Dim.Render(" (guest)")
	}
	lines := []string{st.Title.Render("account"), "", who, ""}
	if s.form != nil {
		f := s.form
		lines = append(lines, f.label, "")
		for i, fd := range f.fields {
			val := s.vals[i]
			if fd.secret {
				val = strings.Repeat("*", len(val))
			}
			cur := "  "
			if s.focus == i {
				cur, val = st.Accent.Render("> "), val+"_"
			}
			lines = append(lines, cur+fmt.Sprintf("%-10s", fd.label)+st.Accent.Render(val))
		}
		lines = append(lines, "")
		if o.err != "" {
			lines = append(lines, st.Danger.Render(o.err), "")
		}
		return append(lines, st.Dim.Render("enter next/confirm  esc back"))
	}
	for i, act := range acctActions(w.Guest) {
		label := act.label
		if act.id == "delete" {
			label = st.Danger.Render(label)
		}
		if i == s.sel {
			lines = append(lines, st.Accent.Render("> ")+label)
		} else {
			lines = append(lines, "  "+label)
		}
	}
	lines = append(lines, "")
	if s.notice != "" {
		lines = append(lines, st.Good.Render(s.notice), "")
	}
	if o.err != "" {
		lines = append(lines, st.Danger.Render(o.err), "")
	}
	lines = append(lines, st.Dim.Render("an account is a name and a password; nothing else is asked or kept"),
		st.Dim.Render("up/down move  enter select  esc back"))
	return lines
}

// recoveryCard shows a recovery code once, with what it is for.
func recoveryCard(a *App, code string) []string {
	st := &a.st
	return []string{st.Title.Render("your recovery code"), "",
		"  " + st.Accent.Render(code), "",
		"Write it down. If you forget your password, log in with",
		"\"forgot password\" and this code to set a new one.",
		"There is no email: this code is the only way back.",
		"It is shown once; you can get a new one from the account page.", "",
		st.Dim.Render("enter when it is noted")}
}
