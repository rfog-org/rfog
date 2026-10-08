package client

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"rfog/proto"
)

// onlineScreen connects to a server and runs the queue. Once a match is
// found it hands off to the draft screen, then to the match screen with a
// remoteMatch source. It also fronts the account form and the lists
// (daily matches, history, ladder).
type onlineScreen struct {
	state   string // connecting | login | menu | queued | error
	err     string
	modes   []string
	sel     int
	size    int // index into sizes
	status  proto.QueueStatus
	since   time.Time
	editing bool // editing the server address
	addr    string
	form    loginForm
	acct    acctState
	// clocks are the server's time controls (newer servers); modes then
	// names them, and rated picks the rated or the casual pool.
	clocks []proto.ClockInfo
	rated  bool
	ch     chState
	// autoClock seeks that clock as soon as the screen is signed in (a
	// quick pairing tile on the home screen); thenChallenge opens the
	// challenge form instead.
	autoClock     string
	autoRated     bool
	thenChallenge bool
	thenWatch     bool // the home screen's watch: the live matches once signed in
	thenLadder    bool // the home screen's leaderboard, once signed in
	// fromHome: opened from the home screen, so esc goes back there.
	fromHome bool
}

// onlineBack is where "back" from an online screen goes: the home screen
// when the terminal is big enough for it (quick pairing, your games and
// the leaderboard are all there), else the online menu.
func onlineBack(a *App) screen {
	if a.net != nil && gridFits(a) {
		return newMenuScreen()
	}
	return newOnlineScreen(a)
}

// homeOr is s, or the home screen when this screen came from there and
// the terminal still has room for it.
func (o *onlineScreen) homeOr(a *App, s screen) screen {
	if o.fromHome && gridFits(a) {
		return newMenuScreen()
	}
	return s
}

// newOnlineQueue signs in if needed and seeks a match on one clock.
func newOnlineQueue(a *App, clock string, rated bool) *onlineScreen {
	o := newOnlineScreen(a)
	o.autoClock, o.autoRated, o.fromHome = clock, rated, true
	return o
}

// auto does what the home screen asked for, once signed in.
func (o *onlineScreen) auto(a *App) tea.Cmd {
	switch {
	case o.thenChallenge:
		o.thenChallenge = false
		o.openChallenge()
	case o.autoClock != "":
		for i, m := range o.modes {
			if m == o.autoClock {
				o.sel = i
			}
		}
		o.autoClock = ""
		if o.clocks != nil {
			o.rated = o.autoRated
		}
		return o.queue(a)
	}
	return nil
}

// queue seeks a match on the selected clock and size.
func (o *onlineScreen) queue(a *App) tea.Cmd {
	mode := o.modes[o.sel]
	q := proto.Queue{Mode: mode, Size: sizes[o.size], Hero: a.set.Hero}
	if o.clocks != nil {
		q.Rated, q.Clock = o.rated && !a.net.welcome.Guest, true
	}
	if err := a.net.send(proto.TQueue, q); err != nil {
		o.err = err.Error()
		return nil
	}
	o.err = ""
	o.state, o.since = "queued", time.Now()
	o.status = proto.QueueStatus{Mode: mode, Size: sizes[o.size]}
	return tick(1000)
}

// syncModes takes the server's clocks when it sends them (else the
// control names older servers use).
func (o *onlineScreen) syncModes(a *App) {
	if a.net == nil || len(a.net.welcome.Clocks) == 0 {
		o.modes, o.clocks = onlineModes, nil
		o.pickDefault("blitz")
		return
	}
	cur := ""
	if o.sel < len(o.modes) {
		cur = o.modes[o.sel]
	}
	o.clocks = a.net.welcome.Clocks
	o.modes = nil
	for _, c := range o.clocks {
		o.modes = append(o.modes, c.ID)
	}
	o.rated = !a.net.welcome.Guest
	if !o.pickDefault(cur) {
		o.pickDefault("30s")
	}
}

func (o *onlineScreen) pickDefault(mode string) bool {
	for i, m := range o.modes {
		if m == mode {
			o.sel = i
			return true
		}
	}
	return false
}

// clock is the selected entry's clock info (nil for older servers).
func (o *onlineScreen) clock(i int) *proto.ClockInfo {
	if i < len(o.clocks) {
		return &o.clocks[i]
	}
	return nil
}

// clockInfo is a clock's line: its pace, and whether it is rated here.
func clockInfo(c proto.ClockInfo, rated bool) string {
	pace := fmt.Sprintf("%ds a turn", c.Turn)
	if c.Async {
		pace = "a day a turn, several at once"
	} else if c.Bank > 0 {
		pace += fmt.Sprintf(", %d min bank", c.Bank/60)
	}
	mode := "casual"
	if rated {
		mode = "rated"
	}
	return c.Category + " · " + pace + " · " + mode
}

type connectedMsg struct {
	r   *remote
	err error
}

var onlineModes = []string{"casual", "blitz", "bullet", "rapid", "daily"}
var sizes = []string{"1v1", "2v2", "3v3", "4v4", "5v5"}

// modeInfo is the per-control blurb; the server enforces the real rules.
var modeInfo = map[string]string{
	"casual": "60s/turn   unranked, bots fill empty seats",
	"blitz":  "30s/turn   ranked",
	"bullet": "10s/turn   ranked",
	"rapid":  "90s/turn   ranked, unused time banks (5 min cap)",
	"daily":  "24h/turn   ranked, async: play several at once",
}

func newOnlineScreen(a *App) *onlineScreen {
	if a.spec != nil {
		a.spec.Leave()
		a.spec = nil
	}
	a.rm = nil
	o := &onlineScreen{modes: onlineModes, addr: a.server}
	o.syncModes(a)
	if a.net != nil {
		o.state = "menu"
	} else {
		o.state = "connecting"
	}
	return o
}

func (o *onlineScreen) init(a *App) tea.Cmd {
	if o.state == "connecting" {
		return o.connect(a, proto.Auth{Name: a.set.Name, Token: a.set.Token})
	}
	if o.state == "menu" {
		return o.auto(a)
	}
	return nil
}

func (o *onlineScreen) connect(a *App, auth proto.Auth) tea.Cmd {
	dial, addr, rules := a.dial, a.server, a.local.Fingerprint()
	o.state, o.err = "connecting", ""
	return func() tea.Msg {
		r, err := connect(dial, addr, rules, auth)
		return connectedMsg{r: r, err: err}
	}
}

func (o *onlineScreen) update(a *App, msg tea.Msg) (screen, tea.Cmd) {
	switch t := msg.(type) {
	case connectedMsg:
		if t.err != nil {
			o.err = t.err.Error()
			// A registered name (or a stale token for one) needs the form.
			if strings.Contains(o.err, "log in") || strings.Contains(o.err, "password") || strings.Contains(o.err, "taken") || strings.Contains(o.err, "recovery") {
				// Back to the form, keeping what was typed (a wrong code
				// returns to the forgotten-password form).
				wasRecover := strings.Contains(o.err, "recovery")
				if o.form.name == "" {
					o.form = newLoginForm(a)
				}
				o.form.recover = wasRecover
				o.state = "login"
				return o, nil
			}
			o.state = "error"
			return o, nil
		}
		a.adopt(t.r)
		o.syncModes(a)
		w := t.r.welcome
		o.state = "menu"
		if w.Recovery != "" {
			// A new account (or a recovered one): its code, once.
			o.state, o.acct = "recovery", acctState{code: w.Recovery}
		}
		if w.InMatch != "" {
			o.state = "queued"
			o.status = proto.QueueStatus{Mode: "resuming"}
		}
		if o.state == "menu" && o.thenWatch {
			return newLobbyScreen(a), t.r.recv()
		}
		if o.state == "menu" && o.thenLadder {
			l := newLadderScreen(a, "blitz")
			return l, tea.Batch(t.r.recv(), l.init(a))
		}
		if o.state == "menu" {
			return o, tea.Batch(t.r.recv(), o.auto(a))
		}
		return o, t.r.recv()
	case tickMsg:
		if o.state == "queued" {
			return o, tick(1000)
		}
		return o, nil
	case netMsg:
		return o.onNet(a, t)
	case tea.KeyMsg:
		return o.key(a, t)
	case tea.MouseMsg:
		if o.state == "menu" {
			if id, hit, act := a.pointer(t, o.sel); hit {
				o.sel = id
				if act {
					return o.key(a, tea.KeyMsg{Type: tea.KeyEnter})
				}
			}
		}
		return o, nil
	}
	return o, nil
}

func (o *onlineScreen) onNet(a *App, n netMsg) (screen, tea.Cmd) {
	if n.Err != nil {
		o.state, o.err = "error", "connection lost: "+n.Err.Error()
		if a.displaced {
			o.err = "continued on another device (r takes it back here)"
		}
		return o, nil
	}
	switch n.T {
	case proto.TQueueStatus:
		_ = n.F.As(&o.status)
		if o.status.Mode == "" {
			o.state = "menu"
		}
	case proto.TMatchFound:
		var mf proto.MatchFound
		if err := n.F.As(&mf); err != nil {
			o.err = err.Error()
			return o, nil
		}
		rm := &remoteMatch{r: a.net, id: mf.Match, you: mf.You, mode: mf.Mode, timeCtl: mf.Time, async: mf.Async, players: mf.Players, ephemeral: a.ephemeral}
		a.rm = rm
		return newDraftScreen(a, rm), nil
	case proto.TLiveList:
		var ll proto.LiveList
		_ = n.F.As(&ll)
		a.net.welcome.Live = ll.Matches
	case proto.TChallengeInfo:
		var ci proto.ChallengeInfo
		_ = n.F.As(&ci)
		o.onChallenge(a, ci)
	case proto.TAccountDone:
		var d proto.AccountDone
		_ = n.F.As(&d)
		sc, cmd := o.accountDone(a, d)
		if a.net == nil {
			return sc, cmd // signed out: nothing more to read
		}
		return sc, tea.Batch(cmd, a.net.recv())
	case proto.TError:
		var e proto.Error
		_ = n.F.As(&e)
		o.err = e.Msg
	}
	return o, nil
}

func (o *onlineScreen) key(a *App, k tea.KeyMsg) (screen, tea.Cmd) {
	if o.editing {
		switch {
		case isKey(k, "enter"):
			o.editing = false
			a.server = strings.TrimSpace(o.addr)
			a.set.Server = a.server
			a.saveSettings()
			a.disconnect()
			return o, o.connect(a, proto.Auth{Name: a.set.Name, Token: a.set.Token})
		case isKey(k, "esc"):
			o.editing = false
		case isKey(k, "backspace"):
			if len(o.addr) > 0 {
				o.addr = o.addr[:len(o.addr)-1]
			}
		default:
			if len(k.Runes) > 0 {
				o.addr += string(k.Runes)
			}
		}
		return o, nil
	}
	switch o.state {
	case "connecting":
		if isKey(k, "esc", "q") {
			return newMenuScreen(), nil
		}
	case "login":
		return o.loginKey(a, k)
	case "account":
		return o.accountKey(a, k)
	case "challenge":
		return o.challengeKey(a, k)
	case "recovery":
		if isKey(k, "enter", "esc", " ") {
			o.state, o.acct.code = "menu", ""
		}
	case "error":
		switch {
		case isKey(k, "r", "enter"):
			return o, o.connect(a, proto.Auth{Name: a.set.Name, Token: a.set.Token})
		case isKey(k, "s"):
			o.editing = true
		case isKey(k, "a"):
			o.state, o.err = "login", ""
			o.form = newLoginForm(a)
		case isKey(k, "esc", "q"):
			return newMenuScreen(), nil
		}
	case "menu":
		switch {
		case isKey(k, "j", "down"):
			o.sel = (o.sel + 1) % len(o.modes)
		case isKey(k, "k", "up"):
			o.sel = (o.sel + len(o.modes) - 1) % len(o.modes)
		case isKey(k, "l", "right"):
			o.size = (o.size + 1) % len(sizes)
		case isKey(k, "h", "left"):
			o.size = (o.size + len(sizes) - 1) % len(sizes)
		case isKey(k, "enter", " "):
			return o, o.queue(a)
		case isKey(k, "d"):
			return newLiveScreen(a), nil
		case isKey(k, "H"):
			return newHistoryScreen(a), nil
		case isKey(k, "L"):
			if c := o.clock(o.sel); c != nil {
				return newLadderScreen(a, c.Category), nil
			}
			return newLadderScreen(a, o.modes[o.sel]), nil
		case isKey(k, "f"):
			o.openChallenge()
		case isKey(k, "c") && o.clocks != nil:
			if a.net.welcome.Guest {
				o.err = "make an account to play rated (a account)"
			} else {
				o.rated, o.err = !o.rated, ""
			}
		case isKey(k, "w"):
			return newLobbyScreen(a), nil
		case isKey(k, "a"):
			o.openAccount(a)
		case isKey(k, "s"):
			o.editing = true
		case isKey(k, "esc", "q"):
			return newMenuScreen(), nil
		}
	case "queued":
		if isKey(k, "esc", "q") {
			_ = a.net.send(proto.TCancel, nil)
			o.state = "menu"
			return o.homeOr(a, o), nil
		}
	}
	return o, nil
}

// adopt takes a fresh connection as this app's: the server's rules while
// online, and the token and name it gave, saved for next time.
func (a *App) adopt(r *remote) {
	a.net = r
	a.rm = nil
	a.displaced = false
	w := r.welcome
	a.c = a.local
	if w.Content != nil {
		a.c = w.Content
	}
	changed := false
	if w.Token != "" && w.Token != a.set.Token {
		a.set.Token, changed = w.Token, true
	}
	acct := ""
	if !w.Guest {
		acct = w.Name
	}
	if acct != a.set.Account || w.Name != a.set.Name {
		a.set.Account, a.set.Name, changed = acct, w.Name, true
	}
	if changed {
		a.saveSettings()
	}
}

// rejoin goes back into a match in progress: the server resends it.
func (a *App) rejoin(match string) screen {
	o := newOnlineScreen(a)
	o.state, o.since = "queued", time.Now()
	o.status = proto.QueueStatus{Mode: "resuming"}
	_ = a.net.send(proto.TJoin, proto.Join{Match: match})
	return o
}

// disconnect drops the live connection (before reconnecting as someone else).
func (a *App) disconnect() {
	if a.net != nil {
		a.net.close()
		a.net = nil
	}
	a.rm = nil
	a.c = a.local
}

// ---- account form ---------------------------------------------------------

// loginForm is name + password with three actions. Letters go to the
// focused field; up/down move focus; enter on an action performs it.
type loginForm struct {
	name, pass string
	focus      int // 0 name, 1 password, 2 log in, 3 register, 4 guest, 5 forgot password
	// recover is the forgotten-password form: 0 name, 1 recovery code,
	// 2 new password, 3 set it, 4 back.
	recover bool
	code    string
}

func newLoginForm(a *App) loginForm {
	name := a.set.Account
	if name == "" {
		name = a.set.Name
	}
	return loginForm{name: name, focus: 1}
}

func (o *onlineScreen) loginKey(a *App, k tea.KeyMsg) (screen, tea.Cmd) {
	f := &o.form
	if f.recover {
		return o.recoverKey(a, k)
	}
	switch {
	case isKey(k, "esc"):
		if a.net != nil {
			o.state = "menu"
		} else {
			return newMenuScreen(), nil
		}
	case isKey(k, "down", "tab"):
		f.focus = (f.focus + 1) % 6
	case isKey(k, "up", "shift+tab"):
		f.focus = (f.focus + 5) % 6
	case isKey(k, "enter"):
		switch f.focus {
		case 0, 1:
			f.focus++
		case 5:
			f.recover, f.focus, f.pass, o.err = true, 1, "", ""
			if f.name == "" {
				f.focus = 0
			}
		case 2, 3, 4:
			auth := proto.Auth{Name: strings.TrimSpace(f.name)}
			switch f.focus {
			case 2:
				auth.Password = f.pass
			case 3:
				auth.Password, auth.Register = f.pass, true
			}
			if auth.Name == "" {
				o.err = "name required"
				return o, nil
			}
			if f.focus != 4 && auth.Password == "" {
				o.err = "password required"
				return o, nil
			}
			a.disconnect()
			a.set.Token, a.set.Account = "", ""
			return o, o.connect(a, auth)
		}
	case isKey(k, "backspace"):
		switch f.focus {
		case 0:
			if len(f.name) > 0 {
				f.name = f.name[:len(f.name)-1]
			}
		case 1:
			if len(f.pass) > 0 {
				f.pass = f.pass[:len(f.pass)-1]
			}
		}
	default:
		if len(k.Runes) > 0 && k.Type == tea.KeyRunes {
			switch f.focus {
			case 0:
				if len(f.name) < 16 {
					f.name += string(k.Runes)
				}
			case 1:
				if len(f.pass) < 64 {
					f.pass += string(k.Runes)
				}
			}
		}
	}
	return o, nil
}

func (o *onlineScreen) loginView(a *App) []string {
	st := &a.st
	f := &o.form
	if f.recover {
		return o.recoverView(a)
	}
	field := func(i int, label, val string) string {
		cur := "  "
		if f.focus == i {
			cur = st.Accent.Render("> ")
			val += "_"
		}
		return cur + fmt.Sprintf("%-10s", label) + st.Accent.Render(val)
	}
	action := func(i int, label string) string {
		if f.focus == i {
			return st.Accent.Render("> " + label)
		}
		return "  " + label
	}
	lines := []string{st.Title.Render("account"), "",
		field(0, "name", f.name),
		field(1, "password", strings.Repeat("*", len(f.pass))),
		"",
		action(2, "log in"),
		action(3, "register (new account)"),
		action(4, "play as guest (bots and casual only)"),
		action(5, "forgot password (recovery code)"),
		""}
	if o.err != "" {
		lines = append(lines, st.Danger.Render(o.err), "")
	}
	lines = append(lines, st.Dim.Render("up/down move  enter select  esc back"))
	return lines
}

// ---- view ---------------------------------------------------------------

func (o *onlineScreen) view(a *App) string {
	st := &a.st
	var lines []string
	if o.editing {
		lines = append(lines, st.Title.Render("online"), "", "server: "+st.Accent.Render(o.addr+"_"), "", st.Dim.Render("enter connect  esc cancel"))
		return a.centered(lines)
	}
	switch o.state {
	case "login":
		return a.centered(o.loginView(a))
	case "account":
		return a.centered(o.accountView(a))
	case "challenge":
		return a.centered(o.challengeView(a))
	case "recovery":
		return a.centered(recoveryCard(a, o.acct.code))
	case "connecting":
		lines = append(lines, st.Title.Render("online"), "", st.Dim.Render("connecting to "+a.server+" ..."), "", st.Dim.Render("esc back"))
	case "error":
		lines = append(lines, st.Title.Render("online"), "", st.Danger.Render(o.err), "", st.Dim.Render("server: "+a.server), "",
			st.Dim.Render("r retry  a account  s change server  esc back"))
	case "menu":
		lines = append(lines, o.menuView(a)...)
	case "queued":
		wait := int(time.Since(o.since).Seconds())
		lines = append(lines, st.Title.Render("online"), "")
		if o.status.Mode == "resuming" {
			lines = append(lines, "rejoining your match...")
		} else {
			lines = append(lines, fmt.Sprintf("queued: %s %s   %d waiting   %d:%02d", st.Accent.Render(o.status.Mode), o.status.Size, o.status.Waiting, wait/60, wait%60))
		}
		lines = append(lines, "", st.Dim.Render("esc cancel"))
	}
	return a.centered(lines)
}

func (o *onlineScreen) menuView(a *App) []string {
	st := &a.st
	w := a.net.welcome
	who := st.Accent.Render(w.Name)
	if w.Guest {
		who = st.Accent.Render(w.Name) + st.Dim.Render(" (guest)")
	}
	lines := []string{st.Title.Render("online"), "",
		fmt.Sprintf("%s  %s", who, st.Dim.Render(fmt.Sprintf("%d online · %s", w.Online, a.server)))}
	if w.MOTD != "" {
		lines = append(lines, st.Dim.Render(w.MOTD))
	}
	head := fmt.Sprintf("queue   size %s", st.Key.Render("< "+sizes[o.size]+" >"))
	if o.clocks != nil {
		rc := st.Key.Render("casual")
		if o.rated && !w.Guest {
			rc = st.Key.Render("rated")
		}
		head += "   " + rc + st.Dim.Render(" (c)")
	}
	lines = append(lines, "", head)
	roomy := a.roomy(len(o.modes) + 8)
	for i, m := range o.modes {
		a.clickRows(len(lines), i, roomy)
		label := fmt.Sprintf("%-7s %s", m, modeInfo[m])
		key := m // the rating this entry counts toward
		if c := o.clock(i); c != nil {
			key = c.Category
			label = fmt.Sprintf("%-6s %s", m, c.Category)
		}
		if a.w < 70 { // a phone: the blurb does not fit beside the name
			label = m
			if c := o.clock(i); c != nil {
				label = fmt.Sprintf("%-6s %s", m, c.Category)
			}
			if r, ok := w.Ratings[key]; ok {
				label += "  " + st.Dim.Render(ratingStr(r))
			}
		} else if r, ok := w.Ratings[key]; ok {
			label += st.Dim.Render(fmt.Sprintf("   %s", ratingStr(r)))
		}
		if i == o.sel {
			lines = append(lines, st.Accent.Render("> "+label))
		} else {
			lines = append(lines, "  "+label)
		}
		if roomy {
			lines = append(lines, "")
		}
	}
	if c := o.clock(o.sel); c != nil {
		lines = append(lines, st.Dim.Render(clockInfo(*c, o.rated && !w.Guest)))
	} else if a.w < 70 {
		lines = append(lines, st.Dim.Render(modeInfo[o.modes[o.sel]]))
	}
	if n := len(w.Live); n > 0 {
		yours := 0
		for _, lm := range w.Live {
			if lm.YourTurn {
				yours++
			}
		}
		note := fmt.Sprintf("%d match(es) in flight", n)
		if yours > 0 {
			note = st.Warn.Render(fmt.Sprintf("%d match(es) in flight, %d waiting on you", n, yours))
		}
		lines = append(lines, "", note)
	}
	if o.err != "" {
		lines = append(lines, "", st.Danger.Render(o.err))
	}
	if a.w < 70 {
		lines = append(lines, "", st.Dim.Render("enter queue · h/l size · c rated · f friend"), st.Dim.Render("d matches · w watch · H history"),
			st.Dim.Render("L ladder · a account · s server"))
	} else {
		lines = append(lines, "", st.Dim.Render("enter queue  h/l size  c rated/casual  f challenge a friend  d matches  w watch  H history  L ladder"),
			st.Dim.Render("a account  s server  esc back"))
	}
	return lines
}

// ratingStr renders a rating as "1523 ±120 (7-3)".
func ratingStr(r proto.RatingInfo) string {
	return fmt.Sprintf("%d ±%d (%d-%d)", int(r.Rating+0.5), int(r.RD+0.5), r.Wins, r.Games-r.Wins)
}

// recoverKey is the forgotten-password form: name, recovery code, a new
// password. The server checks the code, sets the password, signs every
// other device out and answers with a fresh code (shown once).
func (o *onlineScreen) recoverKey(a *App, k tea.KeyMsg) (screen, tea.Cmd) {
	f := &o.form
	field := func() *string {
		switch f.focus {
		case 0:
			return &f.name
		case 1:
			return &f.code
		case 2:
			return &f.pass
		}
		return nil
	}
	switch {
	case isKey(k, "esc"):
		f.recover, f.focus, o.err = false, 1, ""
	case isKey(k, "down", "tab"):
		f.focus = (f.focus + 1) % 5
	case isKey(k, "up", "shift+tab"):
		f.focus = (f.focus + 4) % 5
	case isKey(k, "enter"):
		switch f.focus {
		case 0, 1, 2:
			f.focus++
		case 4:
			f.recover, f.focus, o.err = false, 1, ""
		case 3:
			auth := proto.Auth{Name: strings.TrimSpace(f.name), Recovery: strings.TrimSpace(f.code), Password: f.pass}
			switch {
			case auth.Name == "":
				o.err = "name required"
			case auth.Recovery == "":
				o.err = "recovery code required"
			case auth.Password == "":
				o.err = "new password required"
			default:
				a.disconnect()
				a.set.Token, a.set.Account = "", ""
				f.recover = false
				return o, o.connect(a, auth)
			}
		}
	case isKey(k, "backspace"):
		if p := field(); p != nil && len(*p) > 0 {
			*p = (*p)[:len(*p)-1]
		}
	default:
		if p := field(); p != nil && len(k.Runes) > 0 && k.Type == tea.KeyRunes && len(*p) < 64 {
			*p += string(k.Runes)
		}
	}
	return o, nil
}

func (o *onlineScreen) recoverView(a *App) []string {
	st := &a.st
	f := &o.form
	field := func(i int, label, val string) string {
		cur := "  "
		if f.focus == i {
			cur, val = st.Accent.Render("> "), val+"_"
		}
		return cur + fmt.Sprintf("%-14s", label) + st.Accent.Render(val)
	}
	action := func(i int, label string) string {
		if f.focus == i {
			return st.Accent.Render("> " + label)
		}
		return "  " + label
	}
	lines := []string{st.Title.Render("forgot password"), "",
		field(0, "name", f.name),
		field(1, "recovery code", f.code),
		field(2, "new password", strings.Repeat("*", len(f.pass))),
		"",
		action(3, "set new password"),
		action(4, "back"),
		""}
	if o.err != "" {
		lines = append(lines, st.Danger.Render(o.err), "")
	}
	return append(lines, st.Dim.Render("the code was shown when the account was made"), st.Dim.Render("up/down move  enter select  esc back"))
}
