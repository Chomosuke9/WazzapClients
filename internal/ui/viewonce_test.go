package ui

import (
	"image"
	"testing"
	"time"

	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

func TestViewOnceAnimationOrigin(t *testing.T) {
	for _, media := range []model.Media{model.MediaImage, model.MediaVideo} {
		for _, replay := range []bool{false, true} {
			st := newSlashTest(t, "rina")
			m := st.viewOnceMsg()
			m.Media, m.Opened = media, replay
			st.u.viewOnceReplay = replay
			origin := image.Pt(400, 170)
			var size image.Point
			frame := func() {
				st.ops.Reset()
				gtx := layout.Context{Ops: &st.ops, Now: st.now, Source: st.r.Source(),
					Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Constraints: layout.Exact(image.Pt(1100, 700))}
				off := op.Offset(origin).Push(&st.ops)
				bg := gtx
				bg.Constraints = layout.Constraints{Max: image.Pt(400, 700)}
				size = st.u.layoutBubble(bg, st.u.selected, m, false, 400).Size
				off.Pop()
				st.u.mediaChat.track(gtx, st.u)
				st.u.layoutViewer(gtx)
				st.u.trackMouse(gtx)
				st.r.Frame(&st.ops)
				st.now = st.now.Add(16 * time.Millisecond)
			}
			frame()
			padMin, padMax := image.Pt(3, 3), image.Pt(3, 3)
			if replay {
				padMin, padMax = image.Pt(9, 6), image.Pt(8, 8)
			}
			want := image.Rectangle{Min: origin.Add(padMin), Max: origin.Add(size).Sub(padMax)}
			pos := pointF(want.Min.Add(image.Pt(8, 8)))
			st.r.Queue(pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Position: pos, Buttons: pointer.ButtonPrimary},
				pointer.Event{Kind: pointer.Release, Source: pointer.Mouse, Position: pos})
			frame()
			v := &st.u.viewer
			if !v.open || !v.viewOnce || v.origin != want {
				t.Fatalf("media %v replay %v: open=%v viewOnce=%v origin=%v, want %v", media, replay, v.open, v.viewOnce, v.origin, want)
			}
			frame()
			if v.anim.v <= 0 || v.anim.v >= 1 || st.u.captureBlocked == replay {
				t.Fatalf("media %v replay %v: progress=%v capture blocked=%v", media, replay, v.anim.v, st.u.captureBlocked)
			}
			for range 20 {
				frame()
			}
			if v.anim.v != 1 {
				t.Fatal("view once animation did not settle")
			}
			st.u.closeViewer()
			frame()
			if st.u.captureBlocked == replay {
				t.Fatal("capture protection changed before the closing animation finished")
			}
			for range 20 {
				frame()
			}
			if v.anim.v != 0 || st.u.captureBlocked {
				t.Fatal("view once did not finish closing")
			}
		}
	}
}

// viewOnceMsg returns the open chat's view once message.
func (st *slashTest) viewOnceMsg() *model.Message {
	st.t.Helper()
	for _, m := range st.u.msgs {
		if m.Kind == model.KindViewOnce {
			return m
		}
	}
	st.t.Fatal("no view once message")
	return nil
}

// TestViewOnceOnce checks that a view once photo opens once, with
// screenshots blocked, and as often as you like with Replay view once on.
func TestViewOnceOnce(t *testing.T) {
	st := newSlashTest(t, "rina")
	m := st.viewOnceMsg()
	if !st.u.canOpenViewOnce(m) {
		t.Fatal("an unopened view once photo doesn't open")
	}
	st.u.openViewOnce(m)
	st.frame()
	if !st.u.viewer.open || !st.u.viewer.viewOnce || !st.u.captureBlocked {
		t.Fatalf("viewer open %v view once %v capture blocked %v", st.u.viewer.open, st.u.viewer.viewOnce, st.u.captureBlocked)
	}
	st.u.applyEvents()
	if m = st.viewOnceMsg(); !m.Opened || st.u.canOpenViewOnce(m) {
		t.Errorf("after opening: opened %v, opens again %v", m.Opened, st.u.canOpenViewOnce(m))
	}
	st.u.hideViewer()
	st.frame()
	if st.u.captureBlocked {
		t.Error("screenshots still blocked with the viewer closed")
	}
	st.u.openViewOnce(m)
	if st.u.viewer.open {
		t.Error("an opened view once photo opened again")
	}

	st.b.SetPref(model.PrefViewOnceReplay, "on")
	st.u.loadExtras()
	st.u.openViewOnce(m)
	st.frame()
	if !st.u.viewer.open {
		t.Fatal("Replay view once didn't open it again")
	}
	if st.u.captureBlocked {
		t.Error("Replay view once still blocks screenshots")
	}
}

// TestViewOnceOnPhone checks that a view once message whose media only
// the phone got doesn't open.
func TestViewOnceOnPhone(t *testing.T) {
	st := newSlashTest(t, "family")
	st.b.SetPref(model.PrefViewOnceReplay, "on")
	st.u.loadExtras()
	m := st.viewOnceMsg()
	st.u.openViewOnce(m)
	if st.u.canOpenViewOnce(m) || st.u.viewer.open {
		t.Error("a view once message on the phone opened")
	}
}
