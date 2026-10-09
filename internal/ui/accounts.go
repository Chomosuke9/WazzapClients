package ui

import (
	"image"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/chomosuke9/wazzapclients/internal/accounts"
	"github.com/chomosuke9/wazzapclients/internal/auto"
	"github.com/chomosuke9/wazzapclients/internal/memtrim"
	"github.com/chomosuke9/wazzapclients/internal/model"
	"github.com/chomosuke9/wazzapclients/internal/ui/icon"
)

// Several WhatsApp accounts can be linked; one is open at a time, and the
// others are closed or run in the background, as their modes say
// (background.go). On a switch the host gives the window a new UI for the
// next account (switchAccount), whose backend runs already if it was in
// the background.

// appPrefs are the preferences that belong to the app rather than to an
// account: they carry over when another account opens.
var appPrefs = append([]string{prefTheme, prefDoodles, prefEnterSend, prefBackground, prefListWidth, prefListHidden, prefZoom,
	prefPrivacy, prefPrivacyToggle, prefNoCapture, prefVolume, prefVoiceRate}, perfPrefs...)

// accountRow is an account in the switcher.
type accountRow struct {
	accounts.Account
	active bool
	pic    string // the saved picture of an account that isn't open
	bg     bgView // what it does in the background
}

// accountRows lists the accounts for the switcher: the linked ones and
// the open one. It's nil without accounts (demo data, screenshots).
func (h *host) accountRows() []accountRow {
	l := h.o.Accounts
	if l == nil || h.o.Open == nil {
		return nil
	}
	rows := make([]accountRow, 0, len(l.Accounts))
	for _, a := range l.Accounts {
		if a.Dir == l.Active || a.Linked() {
			rows = append(rows, accountRow{Account: a, active: a.Dir == l.Active, pic: l.PicturePath(a.Dir),
				bg: h.bgView(a.Dir)})
		}
	}
	return rows
}

func (h *host) refreshAccounts() {
	if h.u != nil {
		h.u.accounts = h.accountRows()
		h.u.settings.stale = true
		h.win.Invalidate()
	}
}

func (h *host) saveAccounts() {
	if err := h.o.Accounts.Save(); err != nil {
		log.Printf("save accounts: %v", err)
	}
}

// accountState follows the open account's connection state.
func (h *host) accountState(e model.ConnEvent) {
	if h.o.Accounts == nil {
		return
	}
	switch {
	case e.State.LoggedIn():
		h.noteAccount(&e)
	case h.leaving:
		// Logged out: open the next account. The host does that between
		// frames; leaving keeps the login screen hidden meanwhile.
		h.request(request{kind: reqSwitch, dir: h.leaveTo})
	}
}

// noteAccount keeps the open account's name and number in the list. e is
// the connection state that brought news, or nil for an AccountEvent.
func (h *host) noteAccount(e *model.ConnEvent) {
	l := h.o.Accounts
	if l == nil || h.leaving {
		return
	}
	cur := l.Current()
	if cur == nil {
		return
	}
	if e != nil && cur.Linked() && (e.MeID == "" || e.MeID == cur.ID) && (e.Me == "" || e.Me == cur.Name) {
		return // nothing new; Account would fetch the profile again
	}
	a := h.b.Account()
	if a.ID == "" {
		return
	}
	name := a.Name
	if name == "" {
		name = h.conn.Me
	}
	if cur.ID == a.ID && cur.Name == name && cur.Phone == a.Phone {
		return
	}
	if !cur.Linked() {
		for _, o := range l.Accounts {
			if o.Dir != cur.Dir && o.ID == a.ID {
				// Linked an account that was here already: unlink this
				// second device and go back to the first.
				h.notice = "This account was already added"
				h.leave(o.Dir)
				return
			}
		}
	}
	cur.ID, cur.Name, cur.Phone = a.ID, name, a.Phone
	h.saveAccounts()
	h.refreshAccounts()
}

// logout logs the open account out. With other accounts linked, the next
// one opens and the one logged out leaves the list.
func (h *host) logout() {
	if h.o.Accounts != nil {
		if others := h.o.Accounts.Others(); len(others) > 0 {
			h.leave(others[0].Dir)
			return
		}
	}
	h.b.Logout()
}

// leave logs the open account out and then opens the account with dir.
func (h *host) leave(dir string) {
	h.leaving, h.leaveTo = true, dir
	h.b.Logout()
}

// addAccount adds an account and opens it, which shows the QR code to
// link it.
func (h *host) addAccount() {
	l := h.o.Accounts
	if l == nil || h.o.Open == nil {
		return
	}
	dir, err := l.Add()
	if err != nil {
		h.toast("Couldn't add an account: " + err.Error())
		return
	}
	h.switchAccount(dir)
}

// switchAccount closes the open account and opens the one with dir. One
// that runs in the background opens as it is: its backend doesn't open a
// second time.
func (h *host) switchAccount(dir string) {
	l := h.o.Accounts
	if l == nil || h.o.Open == nil || l.Find(dir) == nil {
		h.leaving = false
		return
	}
	if dir == l.Active {
		h.leaving = false
		h.refreshAccounts()
		return
	}
	bg := h.takeBackground(dir)
	var b model.Backend
	if bg != nil && bg.b != nil {
		b = bg.b
	} else {
		h.awaitClosed(dir)
		var err error
		b, err = h.o.Open(l.Path(dir))
		if err != nil {
			h.leaving = false
			if !l.Find(dir).Linked() {
				_ = l.Remove(dir) // added just now
			}
			if bg != nil {
				h.bg[dir] = bg // it goes on as it was
				h.armBg()
			}
			h.toast("Couldn't open the account: " + err.Error())
			return
		}
	}
	h.closeAccount(b)
	l.Active = dir
	h.saveAccounts()
	h.useBackend(b, bg)
}

// takeBackground takes the account with dir out of the background, to
// open it: a backend being opened is waited for.
func (h *host) takeBackground(dir string) *bgAccount {
	bg := h.bg[dir]
	if bg == nil {
		return nil
	}
	for bg.opening {
		h.bgOpened(<-h.bgOpen)
	}
	if h.bg[dir] != bg {
		return nil // it was logged out meanwhile
	}
	delete(h.bg, dir)
	if bg.loggingOut {
		// Opening it cancels the logout from Settings; it stays linked.
		bg.loggingOut = false
	}
	return bg
}

// closeAccount closes the open account before next opens: the app's
// preferences carry over to next, the account's picture is kept for the
// switcher, and an account that isn't linked (any more), or was logged
// out to leave it, leaves the list. One that runs in the background goes
// on there, connected (toBackground) or checked from time to time; the
// others' notifications go.
func (h *host) closeAccount(next model.Backend) {
	l := h.o.Accounts
	cur := *l.Current()
	for _, k := range appPrefs {
		next.SetPref(k, h.b.Pref(k))
	}
	a := h.b.Account()
	if a.ID != "" {
		for _, id := range []string{a.ID, a.LID, h.conn.MeID} {
			if id == "" {
				continue
			}
			if pic := h.b.Avatar(id); len(pic) > 0 {
				path := l.PicturePath(cur.Dir)
				err := os.MkdirAll(filepath.Dir(path), 0o700)
				if err == nil {
					err = os.WriteFile(path, pic, 0o600)
				}
				if err != nil {
					log.Printf("save account picture: %v", err)
				}
				break
			}
		}
	}
	if h.u != nil {
		h.u.stopOutgoingTyping()
		h.u.shutdown()
	}
	linked := a.ID != "" && !h.leaving
	if linked && cur.Background == accounts.Always {
		h.toBackground(cur.Dir)
		return
	}
	h.notes.clear()
	var nextJob time.Time
	if x, ok := h.b.(*auto.Backend); ok {
		nextJob = x.Next()
	}
	h.b.Close()
	switch {
	case !linked:
		if err := l.Remove(cur.Dir); err != nil {
			log.Printf("remove account: %v", err)
		}
	case cur.Background != accounts.Off:
		// Checked every few minutes from now on. It was open, so it's
		// caught up.
		now := timeNow()
		l.Find(cur.Dir).Checked = now
		bg := &bgAccount{dir: cur.Dir, nextJob: nextJob}
		h.bg[cur.Dir] = bg
		h.scheduleCheck(bg, now, cur.Background)
	}
}

// toBackground keeps the open account's backend running in the
// background as the next one opens, with its notifier.
func (h *host) toBackground(dir string) {
	bg := &bgAccount{dir: dir, b: h.b, notes: h.notes, conn: h.conn, sending: map[string]bool{}}
	h.bg[dir] = bg
	if x, ok := bg.b.(model.Backgrounder); ok {
		x.SetBackground(true)
	}
	h.backgroundNotes(bg.notes, dir)
	h.armBg()
}

// useBackend makes b the open account's backend and gives the window a
// new UI for it. b was just opened, or ran in the background (bg) and
// goes on as it is.
func (h *host) useBackend(b model.Backend, bg *bgAccount) {
	b, _ = withAuto(b)
	h.b = b
	loadPerf(b)
	running := bg != nil && bg.b != nil
	h.conn, h.syncPct, h.queue, h.later = model.ConnEvent{}, -1, nil, nil
	if cur := h.o.Accounts.Current(); running {
		h.conn = bg.conn
	} else if cur != nil && cur.Linked() {
		// Show the chats right away; the backend reports the real state
		// once it starts.
		h.conn = model.ConnEvent{State: model.StateConnecting, Me: cur.Name, MeID: cur.ID}
	}
	h.leaving, h.leaveTo = false, ""
	// Drafts belong to the account that was open.
	if h.u != nil {
		h.u.dropAttachments()
	}
	clearDrafts(h.drafts)
	// A background account's notifier goes on, with what it shows.
	if bg != nil && bg.notes != nil {
		h.notes = bg.notes
		h.notes.b = b
	} else {
		h.notes = h.newNotifier(b, h.o.Accounts.Active)
	}
	h.openNotes(h.notes)
	if !running {
		h.notes.setChats(b.Chats())
	}
	if x, ok := b.(model.Backgrounder); ok {
		x.SetBackground(false)
	}
	if h.win != nil {
		dropCaches()
		h.newUI()
		h.win.Invalidate()
	}
	if !running {
		b.Start(h.poke)
	}
	h.updateTooltip()
	memtrim.Trim()
}

// toast shows text in the window, if there is one.
func (h *host) toast(text string) {
	if h.u != nil {
		h.u.toast(text)
		h.win.Invalidate()
	}
}

// clear takes all the notifications away, as when another account opens.
func (n *notifier) clear() {
	if n.enabled {
		for id := range n.shown {
			n.remove(n.noteID(id))
		}
	}
	clear(n.shown)
	n.pending, n.flushAt = nil, time.Time{}
}

// logout logs the open account out (the ⋮ menu and Settings).
func (u *UI) logout() {
	if u.host != nil {
		u.host.logout()
		return
	}
	u.backend.Logout()
}

// confirmLogout asks before logging out: the ⋮ menu's "Log out" sits right
// under "Switch account", and a misclick would unlink the account.
func (u *UI) confirmLogout() {
	u.confirm("Log out?", "You'll need to link this device again with your phone to use WhatsApp here.",
		dialogButton{label: "Log out", primary: true, danger: true, run: u.logout})
}

// leaving reports whether the open account is logging out for another
// one to open.
func (u *UI) leaving() bool { return u.host != nil && u.host.leaving }

// canAddAccount reports whether the switcher offers to add an account.
func (u *UI) canAddAccount() bool {
	return len(u.accounts) > 0 && u.conn.State.LoggedIn()
}

// otherAccounts reports whether accounts besides the open one are linked.
func (u *UI) otherAccounts() bool {
	for _, a := range u.accounts {
		if !a.active {
			return true
		}
	}
	return false
}

// acctMenuState is the account switcher: a pop-up list of the accounts,
// opened from the ⋮ menu's "Switch account" or the login screen.
type acctMenuState struct {
	open   bool
	anim   tween
	anchor image.Point // top-left corner, in content coordinates
	scrim  widget.Clickable
	rows   []widget.Clickable
	modes  []widget.Clickable // each row's background mode button
	add    widget.Clickable
}

// acctMenuWidth is the switcher's width, and acctMenuWide its width with
// the background mode buttons, which leave room for a status line.
const (
	acctMenuWidth = 300
	acctMenuWide  = 340
)

func (u *UI) updateAccountMenu(gtx C) {
	m := &u.acctMenu
	if m.scrim.Clicked(gtx) {
		m.open, u.menu.open = false, false
	}
	if len(m.rows) != len(u.accounts) {
		m.rows = make([]widget.Clickable, len(u.accounts))
		m.modes = make([]widget.Clickable, len(u.accounts))
	}
	for i := range m.rows {
		if m.rows[i].Clicked(gtx) {
			m.open, u.menu.open = false, false
			if a := u.accounts[i]; !a.active && u.host != nil {
				u.host.request(request{kind: reqSwitch, dir: a.Dir})
			}
		}
		if m.modes[i].Clicked(gtx) {
			// Over the switcher, which stays open to show the change.
			u.ctx = ctxMenu{kind: ctxAcctMode, chatID: u.accounts[i].Dir, at: u.mouse}
		}
	}
	if m.add.Clicked(gtx) {
		m.open, u.menu.open = false, false
		if u.host != nil {
			u.host.request(request{kind: reqAddAccount})
		}
	}
}

// layoutAccountMenu draws the open account switcher over everything else.
func (u *UI) layoutAccountMenu(gtx C) {
	m := &u.acctMenu
	u.updateAccountMenu(gtx)
	open := m.open && len(u.accounts) > 0
	v := m.anim.step(gtx, open, popDur(open))
	if v == 0 {
		return
	}
	p := u.pal
	if open {
		sgtx := gtx
		sgtx.Constraints = layout.Exact(gtx.Constraints.Max)
		m.scrim.Layout(sgtx, func(gtx C) D { return D{Size: gtx.Constraints.Max} })
	} else {
		var done func()
		gtx, done = fadeOut(gtx)
		defer done()
	}

	w := gtx.Dp(acctMenuWidth)
	if u.showAccountModes() {
		w = gtx.Dp(acctMenuWide)
	}
	rec := op.Record(gtx.Ops)
	mgtx := gtx
	mgtx.Constraints = layout.Constraints{Min: image.Pt(w, 0), Max: image.Pt(w, gtx.Constraints.Max.Y)}
	dims := layout.UniformInset(8).Layout(mgtx, func(gtx C) D {
		var children []layout.FlexChild
		modes := u.showAccountModes()
		for i := range u.accounts {
			if i >= len(m.rows) {
				break
			}
			i := i
			children = append(children, layout.Rigid(func(gtx C) D {
				// The open account's mode is on its settings page: here
				// it would read as its notifications.
				var mode *widget.Clickable
				if modes && !u.accounts[i].active {
					mode = &m.modes[i]
				}
				return u.accountItem(gtx, &m.rows[i], mode, &u.accounts[i])
			}))
		}
		if u.canAddAccount() {
			children = append(children,
				layout.Rigid(func(gtx C) D {
					return layout.Inset{Top: 4, Bottom: 4, Left: 12, Right: 12}.Layout(gtx, func(gtx C) D {
						gtx.Constraints = layout.Exact(image.Pt(gtx.Constraints.Max.X, max(1, gtx.Dp(1))))
						return fill(gtx, p.Divider)
					})
				}),
				layout.Rigid(func(gtx C) D {
					return u.acctMenuItem(gtx, &m.add, func(gtx C) D {
						px := gtx.Dp(40)
						fillCircle(gtx, image.Pt(px/2, px/2), px/2, p.Divider)
						return centerIn(gtx, px, iconW(icPersonAdd, 22, p.Icon))
					}, func(gtx C) D {
						return u.label(15, "Add account", p.Text).Layout(gtx)
					}, nil)
				}),
			)
		}
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
	})
	call := rec.Stop()

	// Keep it in the window.
	lim := gtx.Constraints.Max
	margin := gtx.Dp(8)
	pos := m.anchor
	pos.X = max(min(pos.X, lim.X-dims.Size.X-margin), margin)
	pos.Y = max(min(pos.Y, lim.Y-dims.Size.Y-margin), 0)
	origin := image.Point{}
	if pos.X < m.anchor.X {
		origin.X = dims.Size.X // pushed left: it grows from its right edge
	}
	defer op.Offset(pos).Push(gtx.Ops).Pop()
	defer pushPopup(gtx, v, origin).Pop()
	r := gtx.Dp(12)
	rect := image.Rectangle{Max: dims.Size}
	fillRRect(gtx, rect.Add(image.Pt(0, gtx.Dp(2))).Inset(-gtx.Dp(1)), r, p.Shadow)
	fillRRect(gtx, rect, r, p.Menu)
	defer clip.UniformRRect(rect, r).Push(gtx.Ops).Pop()
	call.Add(gtx.Ops)
}

// accountItem is an account's row in the switcher: its picture, name and
// number (or what it does in the background), and a tick on the open one.
// With mode, a button at the end chooses how it runs in the background.
func (u *UI) accountItem(gtx C, c, mode *widget.Clickable, a *accountRow) D {
	p := u.pal
	name, sub := a.Name, a.Phone
	if a.active && u.me != "" {
		name = u.me
	}
	if s := a.status(u.now()); s != "" {
		sub = s
	}
	switch {
	case name == "" && sub != "":
		name, sub = sub, ""
	case name == "":
		name, sub = "New account", "Not linked yet"
	}
	var tick layout.Widget
	if a.active {
		tick = iconW(icTick, 20, p.Green)
	}
	const btn, gap = 36, 4
	mark := tick
	if mode != nil {
		// Room for the mode button, drawn over the row once it's laid out
		// (the row's Clickable would hide one inside it).
		mark = func(gtx C) D {
			w, h := gtx.Dp(btn), gtx.Dp(btn)
			if tick != nil {
				off := op.Offset(image.Pt(w+gtx.Dp(gap), (h-gtx.Dp(20))/2)).Push(gtx.Ops)
				tick(gtx)
				off.Pop()
				w += gtx.Dp(gap + 20)
			}
			return D{Size: image.Pt(w, h)}
		}
	}
	dims := u.acctMenuItem(gtx, c, func(gtx C) D {
		return u.accountAvatar(gtx, a, 40)
	}, func(gtx C) D {
		if sub == "" {
			return u.label(15, name, p.Text).Layout(gtx)
		}
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(u.label(15, name, p.Text).Layout),
			layout.Rigid(layout.Spacer{Height: 2}.Layout),
			layout.Rigid(u.label(13, sub, p.TextSecondary).Layout),
		)
	}, mark)
	if mode != nil {
		x := dims.Size.X - gtx.Dp(12) - gtx.Dp(btn)
		if tick != nil {
			x -= gtx.Dp(gap + 20)
		}
		off := op.Offset(image.Pt(x, (dims.Size.Y-gtx.Dp(btn))/2)).Push(gtx.Ops)
		u.iconButton(gtx, mode, modeIcon(a.Background), btn, 20, p.Icon)
		off.Pop()
	}
	return dims
}

// modeIcon shows how an account runs in the background.
func modeIcon(m accounts.Mode) *icon.Icon {
	switch {
	case m == accounts.Always:
		return icBell
	case m > 0:
		return icClock
	}
	return icMuted
}

// showAccountModes reports whether the switcher offers the accounts'
// background modes: with another account to switch to, once logged in.
func (u *UI) showAccountModes() bool {
	return u.conn.State.LoggedIn() && u.otherAccounts()
}

// accountByDir returns the switcher's row of the account with dir, or
// nil.
func (u *UI) accountByDir(dir string) *accountRow {
	for i := range u.accounts {
		if u.accounts[i].Dir == dir {
			return &u.accounts[i]
		}
	}
	return nil
}

// acctModeItems are the background modes of the account with dir, its
// own ticked.
func (u *UI) acctModeItems(dir string) []menuItem {
	a := u.accountByDir(dir)
	if a == nil {
		return nil
	}
	item := func(key string, m accounts.Mode) menuItem {
		return menuItem{key: "acctmode:" + key, label: m.String(), tick: a.Background == m,
			run: func() { u.setAccountMode(dir, m) }}
	}
	items := []menuItem{item("off", accounts.Off)}
	for _, m := range accounts.Checks {
		items = append(items, item(strconv.Itoa(int(m)), m))
	}
	return append(items, item("always", accounts.Always),
		menuItem{divider: true},
		menuItem{note: true, label: modeNote(u.accountName(a))},
	)
}

// modeNote explains the background modes of the account named name.
func modeNote(name string) string {
	return "How " + name + " runs while another account is open. Always connected notifies at once; " +
		"checking connects for a few seconds now and then, and uses less memory."
}

// accountName is what the switcher calls an account.
func (u *UI) accountName(a *accountRow) string {
	switch {
	case a.active && u.me != "":
		return u.me
	case a.Label() != "":
		return a.Label()
	}
	return "this account"
}

// setAccountMode sets how the account with dir runs in the background.
func (u *UI) setAccountMode(dir string, m accounts.Mode) {
	if u.host != nil {
		u.host.setBackgroundMode(dir, m)
		return
	}
	if a := u.accountByDir(dir); a != nil {
		a.Background = m // demo data
		u.settings.stale = true
	}
}

// demoAccounts are the accounts screenshots show: the open one and a
// second, connected in the background, and a third checked every 15
// minutes.
func (u *UI) demoAccounts() []accountRow {
	at := u.now().Add(-12 * time.Minute)
	return []accountRow{
		{Account: accounts.Account{ID: u.meID, Phone: "+1 555 0100"}, active: true},
		{Account: accounts.Account{Dir: "accounts/2", ID: "15550142@s.whatsapp.net", Name: "Work", Phone: "+1 555 0142",
			Background: accounts.Always}, bg: bgView{running: true, state: model.StateOnline}},
		{Account: accounts.Account{Dir: "accounts/3", ID: "15550177@s.whatsapp.net", Name: "Shop", Phone: "+1 555 0177",
			Background: 15, Checked: at}},
	}
}

// acctMenuItem draws a switcher row: a 40dp picture, text and, at the
// end, an optional mark.
func (u *UI) acctMenuItem(gtx C, c *widget.Clickable, pic, txt, mark layout.Widget) D {
	p := u.pal
	return clickable(gtx, c, func(gtx C) D {
		gtx.Constraints.Min.X = gtx.Constraints.Max.X
		bg := mix(p.Menu, p.MenuHover, u.hover(gtx, c))
		return background(gtx, bg, 8, func(gtx C) D {
			return vcenter(gtx, gtx.Dp(60), func(gtx C) D {
				return layout.Inset{Left: 12, Right: 12}.Layout(gtx, func(gtx C) D {
					children := []layout.FlexChild{
						layout.Rigid(pic),
						layout.Rigid(layout.Spacer{Width: 14}.Layout),
						layout.Flexed(1, txt),
					}
					if mark != nil {
						children = append(children,
							layout.Rigid(layout.Spacer{Width: 8}.Layout),
							layout.Rigid(mark))
					}
					return layout.Flex{Alignment: layout.Middle}.Layout(gtx, children...)
				})
			})
		})
	})
}

// accountAvatar draws an account's picture: the open account's like
// anywhere else, the others' from the copy kept when they were last open.
func (u *UI) accountAvatar(gtx C, a *accountRow, size unit.Dp) D {
	if a.active {
		return u.avatar(gtx, u.meID, u.meName(), false, size)
	}
	px := gtx.Dp(size)
	r := image.Rect(0, 0, px, px)
	path := a.pic
	e := u.images.get("acct:"+path, avatarPx, func() []byte {
		data, _ := os.ReadFile(path)
		return data
	})
	if e.state == imgReady {
		defer roundShape(px, px, px/2).Push(gtx.Ops).Pop()
		paintCover(gtx, e.op, e.size, r)
		return D{Size: r.Size()}
	}
	return u.avatarOf(gtx, "", avatarPerson, size)
}

// layoutLoginSwitch draws the login screen's "Switch account" button in
// its top right corner, when other accounts are linked.
func (u *UI) layoutLoginSwitch(gtx C) {
	if !u.otherAccounts() {
		return
	}
	p := u.pal
	c := &u.login.switchAcct
	if c.Clicked(gtx) {
		u.acctMenu.open = !u.acctMenu.open
	}
	lim := gtx.Constraints.Max
	gtx.Constraints.Min = image.Point{}
	rec := op.Record(gtx.Ops)
	dims := clickable(gtx, c, func(gtx C) D {
		bg := mix(p.Panel, p.MenuHover, u.hover(gtx, c))
		return background(gtx, bg, 18, func(gtx C) D {
			return vcenter(gtx, gtx.Dp(36), func(gtx C) D {
				return layout.Inset{Left: 14, Right: 16}.Layout(gtx, func(gtx C) D {
					return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
						layout.Rigid(iconW(icSwitchAccount, 20, p.Icon)),
						layout.Rigid(layout.Spacer{Width: 10}.Layout),
						layout.Rigid(u.label(14, "Switch account", p.Text).Layout),
					)
				})
			})
		})
	})
	call := rec.Stop()
	m := gtx.Dp(16)
	pos := image.Pt(lim.X-m-dims.Size.X, m)
	t := op.Offset(pos).Push(gtx.Ops)
	call.Add(gtx.Ops)
	t.Pop()
	u.acctMenu.anchor = image.Pt(lim.X-m-gtx.Dp(acctMenuWidth), pos.Y+dims.Size.Y+gtx.Dp(6))
}

// accountTitle names an account on its settings pages.
func (u *UI) accountTitle(a *accountRow) string {
	switch {
	case a.active && u.me != "":
		return u.me
	case a.Label() != "":
		return a.Label()
	}
	return "Account"
}

// accountsSection is the Account page's list of the accounts linked on
// this computer, each opening its own page (accountPage), when there is
// more than one.
func (u *UI) accountsSection() []settingsSection {
	if len(u.accounts) < 2 {
		return nil
	}
	sec := settingsSection{title: "Accounts on this computer",
		note: "Choose how each account notifies you while another one is open."}
	now := u.now()
	for i := range u.accounts {
		a := &u.accounts[i]
		sub := a.status(now)
		switch {
		case a.active:
			sub = "Open now"
		case sub == "":
			sub = a.Background.String()
		}
		dir := a.Dir
		sec.rows = append(sec.rows, settingRow{key: "acct:" + dir, kind: setAccount, acct: a, title: u.accountTitle(a),
			sub: sub, trailing: icChevronRight, run: func() { u.openSettingsSub("acct:" + dir) }})
	}
	return []settingsSection{sec}
}

// accountPage is an account's settings page: what it's doing in the
// background, how it runs while another account is open, and logging it
// out.
func (u *UI) accountPage(dir string) []settingsSection {
	a := u.accountByDir(dir)
	name := u.accountName(a)
	var secs []settingsSection
	if s := a.status(u.now()); s != "" {
		secs = append(secs, settingsSection{title: "Status", rows: []settingRow{
			{key: "acctstatus", ic: modeIcon(a.Background), title: s}}})
	}
	modes := settingsSection{title: "While another account is open", note: modeNote(name)}
	add := func(m accounts.Mode, sub string) {
		modes.rows = append(modes.rows, settingRow{key: "acctmode:" + strconv.Itoa(int(m)), kind: setRadio,
			title: m.String(), sub: sub, on: a.Background == m, run: func() { u.setAccountMode(dir, m) }})
	}
	add(accounts.Off, "No notifications from it")
	for _, m := range accounts.Checks {
		add(m, "")
	}
	add(accounts.Always, "Notifies at once")
	secs = append(secs, modes)
	if a.active {
		return secs // logged out from the Settings list
	}
	return append(secs, settingsSection{rows: []settingRow{{key: "acctlogout", ic: icLogout, title: "Log out",
		danger: true, run: func() { u.confirmLogoutAccount(dir, name) }}}})
}

// confirmLogoutAccount asks before logging out an account that isn't
// open.
func (u *UI) confirmLogoutAccount(dir, name string) {
	u.confirm("Log out of "+name+"?", "You'll need to link this device again with your phone to use "+name+" here.",
		dialogButton{label: "Log out", primary: true, danger: true, run: func() {
			if u.host != nil {
				u.host.logoutAccount(dir)
			}
		}})
}
