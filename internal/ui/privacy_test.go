package ui

import (
	"bytes"
	"image"
	"image/jpeg"
	"strings"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/input"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/text"
	"gioui.org/unit"

	"github.com/chomosuke9/wazzapclients/internal/mock"
	"github.com/chomosuke9/wazzapclients/internal/model"
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

// Privacy mode hides albums, stickers side by side and the pinned banner
// too, and shows them under the pointer.
func TestPrivacyGroupedRows(t *testing.T) {
	ft := newFloatTest("family")
	ft.SetPrivacy(true)
	ft.frames(10)
	if !ft.privacy.on {
		t.Fatal("privacy mode is off")
	}
	var ms []*model.Message
	for _, r := range ft.rows(ft.selected) {
		if r.album {
			ms = r.group
		}
	}
	if ms == nil {
		t.Fatal("no album")
	}
	ft.jumpTo(ms[0].ID)
	ft.frames(10)
	// The album's own fade runs only where its row hides.
	shown := func() float32 {
		return ft.anims.fade(layout.Context{Ops: new(op.Ops), Now: ft.now}, animKey{id: "msg:" + ms[0].ID, tag: tagReveal}, ft.hovered["album:"+ms[0].ID], durHoverIn, durHoverOut)
	}
	if v := shown(); v != 0 {
		t.Errorf("the album shows %v", v)
	}
	// Point all over the chat until the pointer is on the album.
	on := false
	for y := 120; y < 640 && !on; y += 20 {
		ft.r.Queue(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: f32.Pt(560, float32(y))})
		ft.frames(2)
		on = ft.hovered["album:"+ms[0].ID]
	}
	ft.frames(10)
	if v := shown(); !on || v != 1 {
		t.Errorf("pointed at (%v), the album shows %v", on, v)
	}
	ft.r.Queue(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: f32.Pt(100, 690)})
	ft.frames(10)
	if v := shown(); v != 0 {
		t.Errorf("after the pointer left, the album shows %v", v)
	}
}

// A hidden message is as tall as it is shown.
func TestPrivacyKeepsSizes(t *testing.T) {
	ft := newFloatTest("rina")
	// A photo whose blurred picture is far from its shape.
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 297, 400)), nil); err != nil {
		t.Fatal(err)
	}
	ft.upsertMessage(&model.Message{ID: "tall", ChatID: "rina", Kind: model.KindImage, Media: model.MediaVideo,
		Thumb: buf.Bytes(), Text: "look at this", Time: ft.now})
	for _, c := range ft.chats {
		if c.ID != "rina" {
			ft.SelectID(c.ID)
		}
		heights := func() map[string]int {
			out := map[string]int{}
			for range 20 { // pictures decode in the background
				ft.frames(1)
				time.Sleep(time.Millisecond)
			}
			rows, pos := ft.rows(ft.selected), ft.conv.list.Position
			for i := pos.First; i < min(pos.First+pos.Count, len(rows)); i++ {
				if rows[i].kind == rowMessage {
					out[rows[i].msg.ID] = ft.conv.heights[i]
				}
			}
			return out
		}
		shown := heights()
		ft.SetPrivacy(true)
		hidden := heights()
		ft.SetPrivacy(false)
		for id, h := range hidden {
			if s, ok := shown[id]; ok && s != h {
				t.Errorf("%s in %s: %d px hidden, %d shown", id, c.ID, h, s)
			}
		}
		if _, ok := shown["tall"]; c.ID == "rina" && !ok {
			t.Error("the photo isn't on screen")
		}
	}
}

// A hidden label has a bar on each of its lines, as wide as its words.
func TestPrivacyLabelLines(t *testing.T) {
	u := New(mock.New())
	for _, o := range []labelOpts{{maxLines: 1}, {maxLines: 2}, {}, {maxLines: 2, align: text.Middle}} {
		var ops op.Ops
		gtx := layout.Context{Ops: &ops, Now: testNow(), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1},
			Constraints: layout.Constraints{Max: image.Pt(120, 400)}}
		var lines []labelLine
		l := u.label(15, "meet me at the station at seven, and bring the keys", u.pal.Text, o)
		d := layoutLabelLines(gtx, l.LabelStyle, nil, &lines)
		if want := o.maxLines; len(lines) < 1 || want > 0 && len(lines) != want || want == 0 && len(lines) < 3 {
			t.Fatalf("%+v: lines %v", o, lines)
		}
		if first := d.Size.Y - d.Baseline; lines[0].baseline != first {
			t.Errorf("%+v: the first line is at %d, want %d", o, lines[0].baseline, first)
		}
		for i, ln := range lines {
			if ln.min < 0 || ln.max > d.Size.X || ln.max-ln.min < 20 || ln.baseline >= d.Size.Y ||
				i > 0 && ln.baseline-lines[i-1].baseline < 15 {
				t.Errorf("%+v: line %d is %+v of %v", o, i, ln, d.Size)
			}
		}
	}
}
