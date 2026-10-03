package ui

import (
	"image"
	"log"
	"os"
	"path/filepath"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/chomosuke9/wazzapclients/internal/accounts"
	"github.com/chomosuke9/wazzapclients/internal/memtrim"
	"github.com/chomosuke9/wazzapclients/internal/model"
)

// Several WhatsApp accounts can be linked; one is open at a time. The
// host opens the open one's backend and, on a switch, closes it and
// gives the window a new UI for the next one (switchAccount).

// appPrefs are the preferences that belong to the app rather than to an
// account: they carry over when another account opens.
var appPrefs = []string{prefTheme, prefDoodles, prefEnterSend, prefBackground, prefListWidth, prefListHidden, prefZoom}

// accountRow is an account in the switcher.
type accountRow struct {
	accounts.Account
	active bool
	pic    string // the saved picture of an account that isn't open
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
			rows = append(rows, accountRow{Account: a, active: a.Dir == l.Active, pic: l.PicturePath(a.Dir)})
		}
	}
	return rows
}

func (h *host) refreshAccounts() {
	if h.u != nil {
		h.u.accounts = h.accountRows()
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

// switchAccount closes the open account and opens the one with dir.
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
	b, err := h.o.Open(l.Path(dir))
	if err != nil {
		h.leaving = false
		if !l.Find(dir).Linked() {
			_ = l.Remove(dir) // added just now
		}
		h.toast("Couldn't open the account: " + err.Error())
		return
	}
	h.closeAccount(b)
	l.Active = dir
	h.saveAccounts()
	h.useBackend(b)
}

// closeAccount closes the open account before next opens: the app's
// preferences carry over to next, the account's picture is kept for the
// switcher, its notifications go, and an account that isn't linked (any
// more), or was logged out to leave it, leaves the list.
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
	h.notes.clear()
	if h.u != nil {
		h.u.shutdown()
	}
	h.b.Close()
	if a.ID == "" || h.leaving {
		if err := l.Remove(cur.Dir); err != nil {
			log.Printf("remove account: %v", err)
		}
	}
}

// useBackend makes b, just opened, the open account's backend and gives
// the window a new UI for it.
func (h *host) useBackend(b model.Backend) {
	b, _ = withAuto(b)
	h.b = b
	h.conn, h.syncPct, h.queue = model.ConnEvent{}, -1, nil
	if cur := h.o.Accounts.Current(); cur != nil && cur.Linked() {
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
	h.notes = newNotifier(b, h)
	h.notes.enabled = h.notifyOK
	h.notes.setChats(b.Chats())
	if h.win != nil {
		dropCaches()
		h.newUI()
		h.win.Invalidate()
	}
	b.Start(h.poke)
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
			n.remove(id)
		}
	}
	clear(n.shown)
	n.pending, n.due = nil, nil
}

// logout logs the open account out (the ⋮ menu and Settings).
func (u *UI) logout() {
	if u.host != nil {
		u.host.logout()
		return
	}
	u.backend.Logout()
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
	add    widget.Clickable
}

const acctMenuWidth = 300

func (u *UI) updateAccountMenu(gtx C) {
	m := &u.acctMenu
	if m.scrim.Clicked(gtx) {
		m.open, u.menu.open = false, false
	}
	if len(m.rows) != len(u.accounts) {
		m.rows = make([]widget.Clickable, len(u.accounts))
	}
	for i := range m.rows {
		if m.rows[i].Clicked(gtx) {
			m.open, u.menu.open = false, false
			if a := u.accounts[i]; !a.active && u.host != nil {
				u.host.request(request{kind: reqSwitch, dir: a.Dir})
			}
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
	rec := op.Record(gtx.Ops)
	mgtx := gtx
	mgtx.Constraints = layout.Constraints{Min: image.Pt(w, 0), Max: image.Pt(w, gtx.Constraints.Max.Y)}
	dims := layout.UniformInset(8).Layout(mgtx, func(gtx C) D {
		var children []layout.FlexChild
		for i := range u.accounts {
			if i >= len(m.rows) {
				break
			}
			i := i
			children = append(children, layout.Rigid(func(gtx C) D {
				return u.accountItem(gtx, &m.rows[i], &u.accounts[i])
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
// number, and a tick on the open one.
func (u *UI) accountItem(gtx C, c *widget.Clickable, a *accountRow) D {
	p := u.pal
	name, sub := a.Name, a.Phone
	if a.active && u.me != "" {
		name = u.me
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
	return u.acctMenuItem(gtx, c, func(gtx C) D {
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
	}, tick)
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
