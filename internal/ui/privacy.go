package ui

import (
	"hash/fnv"
	"image"
	"image/color"
	"runtime"

	"gioui.org/io/key"
	"gioui.org/op"
	"gioui.org/text"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

// Privacy mode is for using the app where others can see the screen. It
// hides who you talk with and what was said: names and messages become
// bars, profile pictures and photos are blurred. Pointing at a chat row,
// a message or the chat's header shows it. Times, ticks and unread
// counts stay.
//
// Hiding happens where text and pictures are drawn: hiding sets u.secret
// for what is drawn next, which u.label, layoutSpans, drawAvatar and
// messageImage read.

// Preferences (Backend.Pref keys) of privacy mode, on when "on". The
// toggle and screen recording blocking are extra features (extras.go),
// off until turned on.
const (
	prefPrivacy       = "privacy_mode"
	prefPrivacyToggle = "privacy_toggle" // the title bar's eye and Ctrl+Shift+P
	prefNoCapture     = "hide_capture"   // keep the window out of screen recordings
)

type privacyState struct {
	on        bool
	toggle    bool // the extra feature that offers privacy mode
	noCapture bool
	fx        tween            // the mode turning on and off
	v         float32          // fx this frame, eased
	btn       widget.Clickable // in the title bar
}

// loadPrivacy reads privacy mode's preferences.
func (u *UI) loadPrivacy() {
	ps := &u.privacy
	ps.toggle = extraOn(u.backend, prefPrivacyToggle)
	ps.on = ps.toggle && extraOn(u.backend, prefPrivacy)
	ps.noCapture = extraOn(u.backend, prefNoCapture)
	ps.fx.snap(ps.on)
	ps.v = 0
	if ps.on {
		ps.v = 1
	}
}

// SetPrivacy turns privacy mode on or off, and its toggle on (used for
// screenshots).
func (u *UI) SetPrivacy(on bool) {
	u.privacy.toggle = true
	setExtra(u.backend, prefPrivacyToggle, true)
	u.setPrivacyMode(on)
}

// setPrivacyMode turns privacy mode on or off.
func (u *UI) setPrivacyMode(on bool) {
	u.privacy.on = on
	setExtra(u.backend, prefPrivacy, on)
	if on {
		u.toast("Privacy mode is on. Point at something to see it.")
	} else {
		u.toast("Privacy mode is off")
	}
}

// updatePrivacy toggles privacy mode with Ctrl+Shift+P and steps the
// mode's fade. The title bar's button is layoutPrivacyButton.
func (u *UI) updatePrivacy(gtx C) {
	ps := &u.privacy
	for {
		ev, ok := gtx.Event(key.Filter{Name: "P", Required: key.ModShortcut | key.ModShift})
		if !ok {
			break
		}
		if e, ok := ev.(key.Event); ok && e.State == key.Press && ps.toggle {
			u.setPrivacyMode(!ps.on)
		}
	}
	ps.v = smooth(ps.fx.step(gtx, ps.on, durHoverOut))
}

// hiding hides what is drawn until the returned function runs, unless
// revealed is set (the pointer is on it). Each key fades on its own.
//
//	defer u.hiding(gtx, "row:"+id, hovered)()
func (u *UI) hiding(gtx C, key string, revealed bool) (restore func()) {
	prev := u.secret
	if u.privacy.v == 0 {
		u.secret = 0
		return func() { u.secret = prev }
	}
	r := smooth(u.anims.fade(gtx, animKey{id: key, tag: tagReveal}, revealed, durHoverIn, durHoverOut))
	u.secret = u.privacy.v * (1 - r)
	return func() { u.secret = prev }
}

// unhidden shows what is drawn until the returned function runs, inside
// something hidden: times, ticks and counts.
func (u *UI) unhidden() (restore func()) {
	prev := u.secret
	u.secret = 0
	return func() { u.secret = prev }
}

// blurred reports whether pictures drawn now are blurred. They don't fade:
// they switch halfway.
func (u *UI) blurred() bool { return u.secret >= 0.5 }

// labelStyle is a label that privacy mode can hide: hide is how much, from
// 0 (shown) to 1 (bars where its lines are).
type labelStyle struct {
	material.LabelStyle
	hide float32
}

func (l labelStyle) Layout(gtx C) D {
	if l.hide <= 0 {
		return l.LabelStyle.Layout(gtx)
	}
	// The text's own size, not the minimum it was given: bars only go
	// where text is.
	mg := gtx
	mg.Constraints.Min = image.Point{}
	m := op.Record(gtx.Ops)
	td := l.LabelStyle.Layout(mg)
	m.Stop()
	d := D{Size: gtx.Constraints.Constrain(td.Size), Baseline: td.Baseline + gtx.Constraints.Constrain(td.Size).Y - td.Size.Y}
	x := 0
	switch l.Alignment {
	case text.Middle:
		x = (d.Size.X - td.Size.X) / 2
	case text.End:
		x = d.Size.X - td.Size.X
	}
	if l.hide < 1 {
		t := l.LabelStyle
		t.Color = faded(t.Color, 1-l.hide)
		t.Layout(gtx)
	}
	lh := float32(gtx.Sp(l.TextSize))
	if l.LineHeight != 0 {
		lh = float32(gtx.Sp(l.LineHeight))
	}
	scale := l.LineHeightScale
	if scale == 0 {
		scale = 1.2
	}
	lh *= scale
	n := max(1, int(float32(td.Size.Y)/lh+0.5))
	last := td.Size.Y - td.Baseline // the last line's baseline
	t := op.Offset(image.Pt(x, 0)).Push(gtx.Ops)
	for i := range n {
		w := td.Size.X
		if i == n-1 && n > 1 {
			w = int(float32(w) * lastLineShare(l.Text))
		}
		redactBar(gtx, w, last-int(float32(n-1-i)*lh), gtx.Sp(l.TextSize), l.Color, l.hide)
	}
	t.Pop()
	return d
}

// lastLineShare is how much of a hidden text's width its last line's bar
// takes, between 30% and 90%, the same for the same text.
func lastLineShare(s string) float32 {
	h := fnv.New32a()
	h.Write([]byte(s))
	return 0.3 + float32(h.Sum32()%61)/100
}

// redactBar draws the bar that stands for w px of text on a line whose
// baseline is at y, in text of em px, colored like the text.
func redactBar(gtx C, w, y, em int, col color.NRGBA, hide float32) {
	if w <= 0 {
		return
	}
	h := max(2, em*3/5)
	mid := y - em*7/20
	r := image.Rect(0, mid-h/2, w, mid-h/2+h)
	fillRRect(gtx, r, h/2, faded(col, 0.28*hide))
}

// privacyNotice is what a notification says in privacy mode: nothing of
// who wrote or what.
func privacyNotice(b model.Backend) bool {
	return extraOn(b, prefPrivacyToggle) && extraOn(b, prefPrivacy)
}

// layoutPrivacyButton is the title bar's privacy mode button, w×h px: an
// eye, crossed out and green while the mode is on.
func (u *UI) layoutPrivacyButton(gtx C, w, h int) {
	ps := &u.privacy
	// Before the button is laid out: Clickable.Layout drops clicks no one
	// has read.
	if ps.btn.Clicked(gtx) {
		u.setPrivacyMode(!ps.on)
	}
	u.captionButton(gtx, &ps.btn, w, h, false, func(gtx C, col color.NRGBA) {
		if ps.on {
			col = u.pal.Green
		}
		g := gtx.Dp(10)
		// captionButton centers a 10dp glyph; the eye is bigger.
		t := op.Offset(image.Pt(g/2-gtx.Dp(9), g/2-gtx.Dp(9))).Push(gtx.Ops)
		drawIcon(gtx, icVisibilityOff, 18, col)
		t.Pop()
	})
}

// privacyExtras is the Privacy section of the Extra features page.
func (u *UI) privacyExtras() settingsSection {
	ps := &u.privacy
	rows := []settingRow{u.extraToggle(prefPrivacyToggle, "Privacy mode toggle",
		"Add an eye to the title bar that hides names, messages and pictures until you point at them, "+
			"for using "+appName+" where others can see your screen. "+shortcutMod()+"+Shift+P works too.",
		&ps.toggle, func() {
			if !ps.toggle && ps.on {
				u.setPrivacyMode(false) // no way left to turn it off
			}
		})}
	if runtime.GOOS == "windows" {
		rows = append(rows, u.extraToggle(prefNoCapture, "Block screen recording",
			"The window shows up black in screen recordings, screen sharing and screenshots, but stays as it is on your screen.",
			&ps.noCapture, nil))
	}
	return settingsSection{title: "Privacy", rows: rows}
}
