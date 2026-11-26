package ui

import (
	"image"

	"gioui.org/font"
	"gioui.org/layout"
	"gioui.org/widget"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

// Chat lock, as WhatsApp's linked devices have it: chats locked on the
// phone leave the chat list, its search, the Media panel and the
// notifications' text, and show in Locked chats, a page like Archived
// that opens with the secret code set on the phone (checked by the
// backend: it's synced as a hash). Its row heads the list unless the
// phone hides it; then typing the secret code in the search opens them.
// Without a secret code they don't open here, as on WhatsApp Web. Leaving
// the page locks them again.

// chatLockState is Chat lock's state in the UI.
type chatLockState struct {
	set  model.ChatLock
	read bool // set was read from the backend
	// open is set once the secret code was given, until Locked chats is
	// left: their chats open.
	open bool
	row  bool // the chat list begins with the Locked chats row
	news int  // the locked chats the row counts as unread
	btn  widget.Clickable

	code  widget.Editor // the dialog's field
	wrong bool          // the code tried last was wrong
	// A check of a code runs on a goroutine (it's a slow key derivation);
	// its answer comes through checks. tried is the search query checked
	// last, so each is checked once.
	checking bool
	checks   chan lockCheck
	tried    string
	// then runs once the code is given (a locked chat opened from
	// elsewhere opens after it).
	then func()
}

// lockCheck is the answer to a check of a secret code.
type lockCheck struct {
	code   string
	ok     bool
	search bool // typed in the chat list's search
}

// lockSettings returns the Chat lock's settings, read once and again on
// each ChatLockEvent.
func (u *UI) lockSettings() model.ChatLock {
	l := &u.chatLock
	if !l.read && u.backend != nil {
		l.set, l.read = u.backend.ChatLock(), true
	}
	return l.set
}

// checkLockCode checks code on a goroutine.
func (u *UI) checkLockCode(code string, search bool) {
	l := &u.chatLock
	if l.checks == nil {
		l.checks = make(chan lockCheck, 1)
	}
	if l.checking {
		return
	}
	l.checking = true
	b, out, notify := u.backend, l.checks, u.images.invalidate
	go func() {
		out <- lockCheck{code: code, ok: b.CheckLockCode(code), search: search}
		if notify != nil {
			notify()
		}
	}()
}

// updateChatLock takes the answers of checks, tries the search's query as
// the secret code while the row is hidden, and closes a chat that got
// locked.
func (u *UI) updateChatLock(gtx C) {
	l := &u.chatLock
	select {
	case r := <-l.checks:
		l.checking = false
		switch {
		case r.search && r.ok && u.sidebar.search.Text() == r.code:
			u.sidebar.search.SetText("")
			u.unlockChats()
		case r.search:
		case r.ok:
			u.closeDialog()
			u.unlockChats()
		default:
			l.wrong = true
			l.code.SetText("")
			u.requestFocus(&l.code)
		}
	default:
	}
	if set := u.lockSettings(); set.Hide && set.Code && !u.sidebar.showLocked && !l.checking {
		if q := u.sidebar.search.Text(); q != l.tried && q != "" && u.anyLocked() {
			l.tried = q
			u.checkLockCode(q, true)
		}
	}
	if l.btn.Clicked(gtx) {
		u.askLockCode(nil)
	}
	if c := u.selected; c != nil && c.Locked && !l.open {
		u.closeChat()
	}
}

// anyLocked reports whether a chat is locked.
func (u *UI) anyLocked() bool {
	for _, c := range u.chats {
		if c.Locked {
			return true
		}
	}
	return false
}

// unlockChats opens Locked chats once the secret code was given.
func (u *UI) unlockChats() {
	l := &u.chatLock
	l.open = true
	u.setPage(pageChats)
	u.sidebar.showArchived, u.sidebar.showLocked = false, true
	u.sidebar.list.Position = layout.Position{}
	if then := l.then; then != nil {
		l.then = nil
		then()
	}
}

// leaveLocked locks Locked chats again: their open chat closes.
func (u *UI) leaveLocked() {
	l := &u.chatLock
	if !l.open && !u.sidebar.showLocked {
		return
	}
	l.open, u.sidebar.showLocked = false, false
	if c := u.selected; c != nil && c.Locked {
		u.closeChat()
	}
}

// askLockCode asks for the secret code, then opens Locked chats and runs
// then. Without a secret code it says where to set one.
func (u *UI) askLockCode(then func()) {
	l := &u.chatLock
	if l.open {
		u.unlockChats()
		if then != nil {
			then()
		}
		return
	}
	if !u.lockSettings().Code {
		u.confirm("Set a secret code", "To open locked chats on linked devices, set a secret code on your phone: open a locked chat's Chat lock settings, then Secret code.",
			dialogButton{label: "OK", primary: true})
		u.dialog.buttons = u.dialog.buttons[:1] // no Cancel
		return
	}
	l.then, l.wrong = then, false
	l.code.SingleLine, l.code.Submit, l.code.Mask = true, true, '•'
	l.code.SetText("")
	u.dialog = dialogState{kind: dialogLockCode}
	u.requestFocus(&l.code)
}

// lockCodePanel is the dialog that asks for the secret code.
func (u *UI) lockCodePanel(gtx C) D {
	l := &u.chatLock
	p := u.pal
	open := u.dialog.isOpen()
	submit := false
	for {
		ev, ok := l.code.Update(gtx)
		if !ok {
			break
		}
		switch ev.(type) {
		case widget.SubmitEvent:
			submit = true
		case widget.ChangeEvent:
			l.wrong = false
		}
	}
	code := l.code.Text()
	unlock := func() {
		if code != "" && !l.checking {
			u.checkLockCode(code, false)
		}
	}
	buttons := []dialogButton{
		{label: "Unlock", primary: true, disabled: code == "" || l.checking, run: unlock},
		{label: "Cancel", run: func() { l.then = nil; u.closeDialog() }},
	}
	for i, bt := range buttons {
		if u.btn("lockcode:"+itoa(i)).Clicked(gtx) && open && !bt.disabled {
			bt.run()
			break
		}
	}
	if submit && open {
		unlock()
	}
	problem := ""
	switch {
	case l.checking:
		problem = "Checking…"
	case l.wrong:
		problem = "Wrong secret code. Try again."
	}
	col := p.Danger
	if l.checking {
		col = p.TextSecondary
	}
	w := min(gtx.Dp(440), gtx.Constraints.Max.X-gtx.Dp(32))
	gtx.Constraints = layout.Constraints{Min: image.Pt(w, 0), Max: image.Pt(w, gtx.Constraints.Max.Y)}
	return layout.UniformInset(24).Layout(gtx, func(gtx C) D {
		var btns []layout.FlexChild
		for i := len(buttons) - 1; i >= 0; i-- {
			i, bt := i, buttons[i]
			if len(btns) > 0 {
				btns = append(btns, layout.Rigid(layout.Spacer{Width: 12}.Layout))
			}
			btns = append(btns, layout.Rigid(func(gtx C) D { return u.dialogButton(gtx, "lockcode:"+itoa(i), bt) }))
		}
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(u.label(19.5, "Enter secret code", p.Text, labelOpts{weight: font.Medium}).Layout),
			layout.Rigid(layout.Spacer{Height: 12}.Layout),
			layout.Rigid(func(gtx C) D {
				lb := u.label(14.5, "Enter the secret code you set on your phone to see your locked chats.", p.TextSecondary)
				lb.MaxLines = 0
				return lb.Layout(gtx)
			}),
			layout.Rigid(layout.Spacer{Height: 10}.Layout),
			layout.Rigid(func(gtx C) D { return u.pollLine(gtx, &l.code, "Secret code", l.wrong) }),
			layout.Rigid(layout.Spacer{Height: 8}.Layout),
			layout.Rigid(func(gtx C) D {
				gtx.Constraints.Min.X = gtx.Constraints.Max.X
				return u.label(13.5, problem+" ", col, labelOpts{maxLines: 2}).Layout(gtx)
			}),
			layout.Rigid(layout.Spacer{Height: 22}.Layout),
			layout.Rigid(func(gtx C) D {
				return layout.E.Layout(gtx, func(gtx C) D {
					return layout.Flex{Alignment: layout.Middle}.Layout(gtx, btns...)
				})
			}),
		)
	})
}

// stepLockedRow works out whether the list has the Locked chats row:
// under All, outside select mode, while chats are locked and the phone
// doesn't hide them. What the list is scrolled to stays where it is.
func (u *UI) stepLockedRow() {
	s, l := &u.sidebar, &u.chatLock
	if u.listQuery() != "" {
		return
	}
	any, news := false, 0
	for _, c := range u.chats {
		if !c.Locked {
			continue
		}
		any = true
		if c.Unread != 0 && !c.Muted {
			news++
		}
	}
	l.news = news
	on := any && !u.lockSettings().Hide && !s.showArchived && !s.showLocked && !s.pick.on && s.filter == filterAll
	if on == l.row {
		return
	}
	l.row = on
	pos := &s.list.Position
	switch {
	case on && (pos.First > 0 || pos.Offset > 0):
		pos.First++
	case !on && pos.First > 0:
		pos.First--
	case !on:
		pos.Offset = 0
	}
}

// layoutLockedRow is the row that opens Locked chats, with the number of
// locked chats that are unread.
func (u *UI) layoutLockedRow(gtx C) D {
	p := u.pal
	c := &u.chatLock.btn
	return layout.Inset{Left: 13, Right: 18, Top: 2, Bottom: 2}.Layout(gtx, func(gtx C) D {
		return clickable(gtx, c, func(gtx C) D {
			gtx.Constraints.Min.X = gtx.Constraints.Max.X
			bg := mix(p.Panel, p.Hover, u.hover(gtx, c))
			return background(gtx, u.rowBg(bg), 10, func(gtx C) D {
				return vcenter(gtx, gtx.Dp(54), func(gtx C) D {
					return layout.Inset{Left: 12, Right: 14}.Layout(gtx, func(gtx C) D {
						return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
							layout.Rigid(func(gtx C) D { return centerIn(gtx, gtx.Dp(52), iconW(icLockOutline, 22, p.Icon)) }),
							layout.Rigid(layout.Spacer{Width: 16}.Layout),
							layout.Flexed(1, u.label(17, "Locked chats", p.TextSecondary, labelOpts{maxLines: 1}).Layout),
							layout.Rigid(func(gtx C) D {
								if u.chatLock.news == 0 {
									return D{}
								}
								return u.label(13.2, itoa(u.chatLock.news), p.TextSecondary, labelOpts{maxLines: 1}).Layout(gtx)
							}),
						)
					})
				})
			})
		})
	})
}

// layoutLockedNote is the Locked chats page's explanation.
func (u *UI) layoutLockedNote(gtx C) D {
	return layout.Inset{Left: 23, Right: 23, Bottom: 8}.Layout(gtx, func(gtx C) D {
		gtx.Constraints.Min.X = gtx.Constraints.Max.X
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(u.label(14.5, "These chats are locked on your phone. They lock again when you leave this page.", u.pal.TextSecondary, labelOpts{maxLines: 3}).Layout),
			layout.Rigid(layout.Spacer{Height: 16}.Layout),
			layout.Rigid(func(gtx C) D {
				d := D{Size: image.Pt(gtx.Constraints.Max.X, max(1, gtx.Dp(1)))}
				fillRect(gtx, image.Rectangle{Max: d.Size}, u.pal.Divider)
				return d
			}),
		)
	})
}

// lockChat locks or unlocks a chat from its menu. Locking needs a secret
// code, as on WhatsApp Web, so the chat can be opened here again.
func (u *UI) lockChat(c *model.Chat, locked bool) {
	if locked && !u.lockSettings().Code {
		u.askLockCode(nil) // says where to set one
		return
	}
	u.backend.SetLocked(c.ID, locked)
	if locked {
		u.toast("Chat locked")
		if u.selected != nil && u.selected.ID == c.ID && !u.chatLock.open {
			u.closeChat()
		}
	} else {
		u.toast("Chat unlocked")
	}
}
