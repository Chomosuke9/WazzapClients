package ui

import (
	"image"
	"strings"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/input"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/chomosuke9/wazzapclients/internal/mock"
)

// In privacy mode a notification says neither who wrote nor what.
func TestPrivacyNotification(t *testing.T) {
	nt := newNotifyTest(t)
	setExtra(nt.b, prefPrivacyToggle, true)
	setExtra(nt.b, prefPrivacy, true)
	nt.receive("ana@lid", "", "the villa is at Jl. Melati 4")
	got := nt.flush(t)
	if len(got) != 1 {
		t.Fatalf("got %d notifications", len(got))
	}
	n := got[0]
	if n.Title != appName || n.Body != "New message" {
		t.Errorf("notification %q: %q, want %q: \"New message\"", n.Title, n.Body, appName)
	}
	if strings.Contains(n.Title+n.Body+n.Footer, "Ana") || strings.Contains(n.Body, "villa") {
		t.Errorf("notification gives the chat away: %+v", n)
	}
}

// Text drawn while hidden takes the room the text would.
func TestPrivacyHidesText(t *testing.T) {
	u := New(mock.New())
	u.loadPrivacy()
	draw := func() D {
		var ops op.Ops
		gtx := layout.Context{Ops: &ops, Now: testNow(), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1},
			Constraints: layout.Constraints{Max: image.Pt(400, 400)}}
		return u.label(15, "meet me at the station at seven", u.pal.Text, labelOpts{maxLines: 3}).Layout(gtx)
	}
	shown := draw()
	u.secret = 1
	if l := u.label(15, "x", u.pal.Text); l.hide != 1 {
		t.Errorf("label hides %v, want 1", l.hide)
	}
	hidden := draw()
	u.secret = 0
	if shown.Size != hidden.Size || shown.Baseline != hidden.Baseline {
		t.Errorf("hidden label is %v, shown %v", hidden, shown)
	}

	// Outside privacy mode nothing hides, whatever is hovered.
	var ops op.Ops
	gtx := layout.Context{Ops: &ops, Now: testNow()}
	restore := u.hiding(gtx, "row:x", false)
	if u.secret != 0 {
		t.Errorf("privacy mode off hides %v", u.secret)
	}
	restore()
	u.privacy.on, u.privacy.v = true, 1
	restore = u.hiding(gtx, "row:x", false)
	if u.secret != 1 {
		t.Errorf("privacy mode on hides %v, want 1", u.secret)
	}
	restore()
	if u.secret != 0 {
		t.Errorf("secret %v after restore", u.secret)
	}
}

// The title bar's eye turns privacy mode on and off. Its Clickable drops
// clicks no one read before it was laid out, so the click is read there.
func TestPrivacyButton(t *testing.T) {
	u := New(mock.New())
	u.Start(func() {})
	u.privacy.toggle = true // the extra feature
	now := testNow()
	var ops op.Ops
	var r input.Router
	const w = 1100
	frame := func() {
		ops.Reset()
		u.Layout(layout.Context{Ops: &ops, Now: now, Source: r.Source(), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1},
			Constraints: layout.Exact(image.Pt(w, 700))})
		r.Frame(&ops)
		now = now.Add(10 * time.Millisecond)
	}
	frame()
	p := f32.Pt(w-3*46-23, 20) // left of minimize
	for _, want := range []bool{true, false} {
		r.Queue(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: p},
			pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Position: p, Buttons: pointer.ButtonPrimary},
			pointer.Event{Kind: pointer.Release, Source: pointer.Mouse, Position: p})
		frame()
		frame()
		if u.privacy.on != want {
			t.Fatalf("after a click privacy mode is %v, want %v", u.privacy.on, want)
		}
	}
}

// Privacy mode is an extra feature: until its toggle is on there's no
// eye and no shortcut, and turning the toggle off turns the mode off.
func TestPrivacyToggle(t *testing.T) {
	b := mock.New()
	setExtra(b, prefPrivacy, true) // left on by an earlier version
	u := New(b)
	if u.privacy.on {
		t.Fatal("privacy mode on without its toggle")
	}
	if privacyNotice(b) {
		t.Error("notifications hide their text without the toggle")
	}
	u.privacy.toggle = true
	u.setPrivacyMode(true)
	for _, sec := range u.extrasSettings() {
		for _, r := range sec.rows {
			if r.key == prefPrivacyToggle {
				r.run()
			}
		}
	}
	if u.privacy.toggle || u.privacy.on || extraOn(b, prefPrivacy) {
		t.Errorf("toggle off: toggle %v, privacy mode %v, pref %q", u.privacy.toggle, u.privacy.on, b.Pref(prefPrivacy))
	}
}
