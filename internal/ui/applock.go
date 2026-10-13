package ui

import (
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"image"
	"image/color"
	"strconv"
	"strings"
	"time"

	"gioui.org/font"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/text"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

// App lock, like WhatsApp Desktop's: a password that locks the app. The
// window then shows only the lock screen, which asks for it. The ⋮ menu
// sets it, changes it or turns it off (App lock) and locks at once (Lock
// app); the app also locks when it starts and after a chosen time without
// input. Only a salted PBKDF2 hash of the password is kept, in the prefs.

const (
	// prefAppLock is the password's hash (see hashPassword), "" when
	// App lock is off.
	prefAppLock = "app_lock"
	// prefAppLockAfter is the minutes without input before the app locks
	// itself, 0 for never; "" is lockAfterDefault.
	prefAppLockAfter = "app_lock_after"
)

// lockIter is PBKDF2-HMAC-SHA256's iteration count for new hashes
// (OWASP's 2023 advice). Stored hashes carry their own.
const lockIter = 600_000

// lockMinLen is the shortest password App lock takes, as in WhatsApp.
const lockMinLen = 6

// lockAfterChoices are the auto-lock times offered, in minutes.
var lockAfterChoices = []struct {
	label string
	min   int
}{
	{"After 1 minute", 1}, {"After 15 minutes", 15}, {"After 1 hour", 60}, {"Never", 0},
}

const lockAfterDefault = 15

// hashPassword returns a salted PBKDF2-HMAC-SHA256 hash of pw as
// "pbkdf2-sha256$<iterations>$<salt>$<key>", in hex.
func hashPassword(pw string) string {
	salt := make([]byte, 16)
	rand.Read(salt)
	return hashWith(pw, salt, lockIter)
}

func hashWith(pw string, salt []byte, iter int) string {
	k, err := pbkdf2.Key(sha256.New, pw, salt, iter, 32)
	if err != nil {
		return ""
	}
	return "pbkdf2-sha256$" + strconv.Itoa(iter) + "$" + hex.EncodeToString(salt) + "$" + hex.EncodeToString(k)
}

// checkPassword reports whether pw is the password hashed in stored.
func checkPassword(stored, pw string) bool {
	f := strings.Split(stored, "$")
	if len(f) != 4 || f[0] != "pbkdf2-sha256" {
		return false
	}
	iter, err := strconv.Atoi(f[1])
	salt, err2 := hex.DecodeString(f[2])
	if err != nil || err2 != nil || iter < 1 {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(hashWith(pw, salt, iter)), []byte(stored)) == 1
}

// appLock is whether the app is locked and when it last saw input. The
// host keeps one for all the windows' UIs, so closing the window or
// switching accounts doesn't unlock it.
type appLock struct {
	locked bool
	last   time.Time // the last input, for the auto-lock
	pw     widget.Editor
	wrong  bool // the password just tried was wrong
	anim   tween
}

// newAppLock starts locked when App lock is on.
func newAppLock(b model.Backend) *appLock {
	l := &appLock{}
	l.pw.SingleLine, l.pw.Submit, l.pw.Mask = true, true, '•'
	l.locked = b != nil && b.Pref(prefAppLock) != ""
	return l
}

// lockSet reports whether App lock is on.
func (u *UI) lockSet() bool { return u.backend != nil && u.backend.Pref(prefAppLock) != "" }

// lockAfter is the auto-lock time, 0 for never.
func (u *UI) lockAfter() time.Duration {
	n := lockAfterDefault
	if v := u.backend.Pref(prefAppLockAfter); v != "" {
		n, _ = strconv.Atoi(v)
	}
	return time.Duration(n) * time.Minute
}

// lockNow locks the app: whatever was open over the chats goes.
func (u *UI) lockNow() {
	l := u.lock
	l.locked, l.wrong = true, false
	l.pw.SetText("")
	u.menu.open, u.acctMenu.open = false, false
	u.ctx = ctxMenu{}
	u.dialog = dialogState{}
	u.requestFocus(&l.pw)
}

// touchLock notes input, which puts the auto-lock off.
func (u *UI) touchLock() {
	if u.lock != nil {
		u.lock.last = u.now()
	}
}

// autoLock locks the app once it has gone without input for the chosen
// time, and otherwise wakes the window when that time comes.
func (u *UI) autoLock(gtx C) {
	l := u.lock
	if l.locked || !u.lockSet() {
		return
	}
	after := u.lockAfter()
	if after <= 0 {
		return
	}
	now := u.now()
	if l.last.IsZero() {
		l.last = now
	}
	at := l.last.Add(after)
	if !now.Before(at) {
		u.lockNow()
		return
	}
	gtx.Execute(op.InvalidateCmd{At: gtx.Now.Add(at.Sub(now))})
}

// layoutLockScreen draws the lock screen over the whole window below the
// title bar.
func (u *UI) layoutLockScreen(gtx C) {
	p := u.pal
	l := u.lock
	unlock := false
	for {
		ev, ok := l.pw.Update(gtx)
		if !ok {
			break
		}
		switch ev.(type) {
		case widget.SubmitEvent:
			unlock = true
		case widget.ChangeEvent:
			l.wrong = false
		}
	}
	if u.btn("lock:unlock").Clicked(gtx) {
		unlock = true
	}
	if unlock && l.pw.Text() != "" {
		if checkPassword(u.backend.Pref(prefAppLock), l.pw.Text()) {
			l.locked, l.wrong = false, false
			l.pw.SetText("")
			u.touchLock()
			return
		}
		l.wrong = true
		l.pw.SetText("")
		u.requestFocus(&l.pw)
	}
	if u.btn("lock:logout").Clicked(gtx) {
		u.confirm("Log out?", "You'll need to link this device again with your phone to use WhatsApp here. App lock turns off.",
			dialogButton{label: "Log out", primary: true, danger: true, run: func() {
				u.backend.SetPref(prefAppLock, "")
				u.logout()
			}})
	}
	if !gtx.Focused(&l.pw) && !u.dialog.isOpen() && !u.focusReq {
		u.requestFocus(&l.pw)
	}

	sz := gtx.Constraints.Max
	fillRect(gtx, image.Rectangle{Max: sz}, p.Panel)
	v := easeOut(l.anim.step(gtx, true, durDialog))
	w := min(gtx.Dp(360), sz.X-gtx.Dp(32))
	cgtx := gtx
	cgtx.Constraints = layout.Constraints{Min: image.Pt(w, 0), Max: image.Pt(w, sz.Y)}
	col := record(cgtx, func(gtx C) D {
		center := func(w layout.Widget) layout.FlexChild {
			return layout.Rigid(func(gtx C) D { return layout.N.Layout(gtx, w) })
		}
		para := func(size unit.Sp, s string, c color.NRGBA, weight font.Weight) layout.FlexChild {
			return layout.Rigid(func(gtx C) D {
				gtx.Constraints.Min.X = gtx.Constraints.Max.X
				return u.label(size, s, c, labelOpts{weight: weight, align: text.Middle}).Layout(gtx)
			})
		}
		msg, msgCol := "", p.TextSecondary
		if l.wrong {
			msg, msgCol = "Incorrect password. Try again.", p.Danger
		}
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			center(func(gtx C) D {
				s := gtx.Dp(96)
				fillCircle(gtx, image.Pt(s/2, s/2), s/2, p.Hover)
				return centerIn(gtx, s, iconW(icLock, 44, p.Green))
			}),
			layout.Rigid(layout.Spacer{Height: 28}.Layout),
			para(22, "The app is locked", p.Text, font.Medium),
			layout.Rigid(layout.Spacer{Height: 8}.Layout),
			para(15, "Enter your password to unlock it.", p.TextSecondary, font.Normal),
			layout.Rigid(layout.Spacer{Height: 24}.Layout),
			layout.Rigid(func(gtx C) D { return u.pollLine(gtx, &l.pw, "Password", l.wrong) }),
			layout.Rigid(layout.Spacer{Height: 8}.Layout),
			layout.Rigid(func(gtx C) D {
				// Always laid out, so the button doesn't jump.
				gtx.Constraints.Min.X = gtx.Constraints.Max.X
				return u.label(13.5, msg+" ", msgCol, labelOpts{maxLines: 1}).Layout(gtx)
			}),
			layout.Rigid(layout.Spacer{Height: 18}.Layout),
			center(func(gtx C) D {
				return u.dialogButton(gtx, "lock:unlock", dialogButton{label: "Unlock", primary: true, disabled: l.pw.Text() == ""})
			}),
			layout.Rigid(layout.Spacer{Height: 14}.Layout),
			center(func(gtx C) D {
				// A link: green text alone, on a pill while hovered.
				c := u.btn("lock:logout")
				return clickable(gtx, c, func(gtx C) D {
					gtx.Constraints.Min = image.Point{}
					lbl := record(gtx, u.label(14.5, "Forgot password? Log out", p.Green, labelOpts{weight: font.Medium, maxLines: 1}).Layout)
					h, w := gtx.Dp(40), lbl.size.X+gtx.Dp(32)
					if a := u.hover(gtx, c); a > 0 {
						fillRRect(gtx, image.Rect(0, 0, w, h), h/2, faded(p.Hover, a))
					}
					lbl.at(gtx, (w-lbl.size.X)/2, (h-lbl.size.Y)/2)
					return D{Size: image.Pt(w, h)}
				})
			}),
		)
	})
	x, y := (sz.X-col.size.X)/2, max(0, (sz.Y-col.size.Y)/2-gtx.Dp(24))
	defer pushFx(gtx, v, moveBy(0, float32(gtx.Dp(12))*(1-v))).Pop()
	col.at(gtx, x, y)
}

// The App lock dialog's pages.
const (
	lockSetup  = iota // no password yet: pick one
	lockManage        // on: the auto-lock time, change, turn off
	lockChange        // the current password, then a new one
	lockOff           // the current password, to turn it off
)

// lockDialog is the App lock dialog's state.
type lockDialog struct {
	page       int
	cur, a, b  widget.Editor // current, new and repeated password
	wrong      bool          // the current password given was wrong
	after      int           // the auto-lock minutes picked while setting up
	focusFirst bool
}

// openAppLock opens the App lock dialog (the ⋮ menu's App lock).
func (u *UI) openAppLock() {
	page := lockSetup
	if u.lockSet() {
		page = lockManage
	}
	u.lockPage(page)
	u.lockDlg.after = lockAfterDefault
	u.dialog = dialogState{kind: dialogAppLock}
}

// lockPage shows a page of the App lock dialog, its fields empty.
func (u *UI) lockPage(page int) {
	d := &u.lockDlg
	d.page, d.wrong = page, false
	for _, ed := range []*widget.Editor{&d.cur, &d.a, &d.b} {
		ed.SingleLine, ed.Submit, ed.Mask = true, true, '•'
		ed.SetText("")
	}
	switch page {
	case lockSetup:
		u.requestFocus(&d.a)
	case lockChange, lockOff:
		u.requestFocus(&d.cur)
	}
}

// appLockPanel is the App lock dialog.
func (u *UI) appLockPanel(gtx C) D {
	d := &u.lockDlg
	p := u.pal
	b := u.backend
	open := u.dialog.isOpen()

	var fields []*widget.Editor
	var hints []string
	switch d.page {
	case lockSetup:
		fields, hints = []*widget.Editor{&d.a, &d.b}, []string{"Password", "Confirm password"}
	case lockChange:
		fields, hints = []*widget.Editor{&d.cur, &d.a, &d.b}, []string{"Current password", "New password", "Confirm new password"}
	case lockOff:
		fields, hints = []*widget.Editor{&d.cur}, []string{"Current password"}
	}
	submit := false
	for i, ed := range fields {
		for {
			ev, ok := ed.Update(gtx)
			if !ok {
				break
			}
			switch ev.(type) {
			case widget.SubmitEvent:
				if i+1 < len(fields) {
					u.requestFocus(fields[i+1])
				} else {
					submit = true
				}
			case widget.ChangeEvent:
				if ed == &d.cur {
					d.wrong = false
				}
			}
		}
	}
	pw, again := d.a.Text(), d.b.Text()
	short := len([]rune(pw)) < lockMinLen
	mismatch := again != "" && pw != again
	newOK := !short && pw == again

	// The buttons, right to left as they're drawn.
	var buttons []dialogButton
	problem := ""
	switch d.page {
	case lockSetup:
		if pw != "" && short {
			problem = "Use at least " + itoa(lockMinLen) + " characters."
		} else if mismatch {
			problem = "Passwords don't match."
		}
		buttons = []dialogButton{{label: "Turn on", primary: true, disabled: !newOK, run: func() {
			b.SetPref(prefAppLock, hashPassword(pw))
			b.SetPref(prefAppLockAfter, strconv.Itoa(d.after))
			u.touchLock()
			u.closeDialog()
			u.toast("App lock is on")
		}}, {label: "Cancel", run: u.closeDialog}}
	case lockManage:
		buttons = []dialogButton{
			{label: "Done", primary: true, run: u.closeDialog},
			{label: "Change password", run: func() { u.lockPage(lockChange) }},
			{label: "Turn off", run: func() { u.lockPage(lockOff) }},
		}
	case lockChange:
		if d.wrong {
			problem = "Incorrect password."
		} else if pw != "" && short {
			problem = "Use at least " + itoa(lockMinLen) + " characters."
		} else if mismatch {
			problem = "Passwords don't match."
		}
		buttons = []dialogButton{{label: "Change", primary: true, disabled: !newOK || d.cur.Text() == "", run: func() {
			if !checkPassword(b.Pref(prefAppLock), d.cur.Text()) {
				d.wrong = true
				d.cur.SetText("")
				u.requestFocus(&d.cur)
				return
			}
			b.SetPref(prefAppLock, hashPassword(pw))
			u.closeDialog()
			u.toast("Password changed")
		}}, {label: "Cancel", run: func() { u.lockPage(lockManage) }}}
	case lockOff:
		if d.wrong {
			problem = "Incorrect password."
		}
		buttons = []dialogButton{{label: "Turn off", danger: true, disabled: d.cur.Text() == "", run: func() {
			if !checkPassword(b.Pref(prefAppLock), d.cur.Text()) {
				d.wrong = true
				d.cur.SetText("")
				u.requestFocus(&d.cur)
				return
			}
			b.SetPref(prefAppLock, "")
			u.closeDialog()
			u.toast("App lock is off")
		}}, {label: "Cancel", run: func() { u.lockPage(lockManage) }}}
	}
	for i, bt := range buttons {
		if u.btn("lockdlg:"+itoa(i)).Clicked(gtx) && open && !bt.disabled {
			bt.run()
			break
		}
	}
	if submit && open && !buttons[0].disabled {
		buttons[0].run()
	}
	// The auto-lock times: picked for later while setting up, kept at
	// once once on.
	after := d.after
	if d.page == lockManage {
		after = int(u.lockAfter() / time.Minute)
	}
	for _, ch := range lockAfterChoices {
		if u.btn("lockafter:"+itoa(ch.min)).Clicked(gtx) && open {
			if d.page == lockManage {
				b.SetPref(prefAppLockAfter, strconv.Itoa(ch.min))
				u.touchLock()
			}
			d.after, after = ch.min, ch.min
		}
	}

	body := "Set a password to lock the app. You'll need it when the app starts and after it locks itself. It can't be recovered: if you forget it, you'll have to log out."
	switch d.page {
	case lockManage:
		body = "The app is locked with a password when it starts, when you lock it from the menu, and after the time below without use."
	case lockChange:
		body = "Enter your current password, then the new one."
	case lockOff:
		body = "Enter your password to turn App lock off."
	}
	w := min(gtx.Dp(440), gtx.Constraints.Max.X-gtx.Dp(32))
	gtx.Constraints = layout.Constraints{Min: image.Pt(w, 0), Max: image.Pt(w, gtx.Constraints.Max.Y)}
	return layout.UniformInset(24).Layout(gtx, func(gtx C) D {
		children := []layout.FlexChild{
			layout.Rigid(u.label(19.5, "App lock", p.Text, labelOpts{weight: font.Medium}).Layout),
			layout.Rigid(layout.Spacer{Height: 12}.Layout),
			layout.Rigid(func(gtx C) D {
				lb := u.label(14.5, body, p.TextSecondary)
				lb.MaxLines = 0
				return lb.Layout(gtx)
			}),
		}
		for i, ed := range fields {
			ed, hint := ed, hints[i]
			children = append(children, layout.Rigid(layout.Spacer{Height: 10}.Layout),
				layout.Rigid(func(gtx C) D {
					bad := ed == &d.cur && d.wrong || ed == &d.b && mismatch
					return u.pollLine(gtx, ed, hint, bad)
				}))
		}
		if len(fields) > 0 {
			children = append(children, layout.Rigid(layout.Spacer{Height: 8}.Layout),
				layout.Rigid(func(gtx C) D {
					gtx.Constraints.Min.X = gtx.Constraints.Max.X
					return u.label(13.5, problem+" ", p.Danger, labelOpts{maxLines: 2}).Layout(gtx)
				}))
		}
		if d.page == lockSetup || d.page == lockManage {
			children = append(children, layout.Rigid(layout.Spacer{Height: 14}.Layout),
				layout.Rigid(u.label(14, "Lock automatically", p.TextSecondary, labelOpts{weight: font.Medium, maxLines: 1}).Layout),
				layout.Rigid(layout.Spacer{Height: 4}.Layout))
			for _, ch := range lockAfterChoices {
				ch := ch
				children = append(children, layout.Rigid(func(gtx C) D {
					c := u.btn("lockafter:" + itoa(ch.min))
					return clickable(gtx, c, func(gtx C) D {
						gtx.Constraints.Min.X = gtx.Constraints.Max.X
						return vcenter(gtx, gtx.Dp(38), func(gtx C) D {
							return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
								layout.Rigid(func(gtx C) D { return u.lockRadio(gtx, after == ch.min) }),
								layout.Rigid(layout.Spacer{Width: 14}.Layout),
								layout.Flexed(1, u.label(15, ch.label, p.Text, labelOpts{maxLines: 1}).Layout),
							)
						})
					})
				}))
			}
		}
		children = append(children, layout.Rigid(layout.Spacer{Height: 22}.Layout))
		var btns []layout.FlexChild
		for i := len(buttons) - 1; i >= 0; i-- {
			i, bt := i, buttons[i]
			if len(btns) > 0 {
				btns = append(btns, layout.Rigid(layout.Spacer{Width: 12}.Layout))
			}
			btns = append(btns, layout.Rigid(func(gtx C) D { return u.dialogButton(gtx, "lockdlg:"+itoa(i), bt) }))
		}
		children = append(children, layout.Rigid(func(gtx C) D {
			return layout.E.Layout(gtx, func(gtx C) D {
				return layout.Flex{Alignment: layout.Middle}.Layout(gtx, btns...)
			})
		}))
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
	})
}

// lockRadio is a radio button on the dialog's background.
func (u *UI) lockRadio(gtx C, on bool) D {
	p := u.pal
	sz := gtx.Dp(20)
	c := image.Pt(sz/2, sz/2)
	ring := p.TextSecondary
	if on {
		ring = p.Green
	}
	fillCircle(gtx, c, sz/2, ring)
	fillCircle(gtx, c, sz/2-max(1, gtx.Dp(2)), p.Dialog)
	if on {
		fillCircle(gtx, c, sz/2-gtx.Dp(5), p.Green)
	}
	return D{Size: image.Pt(sz, sz)}
}
