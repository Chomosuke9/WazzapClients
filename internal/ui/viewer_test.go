package ui

import (
	"image"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/input"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/unit"

	"github.com/chomosuke9/wazzapclients/internal/mock"
	"github.com/chomosuke9/wazzapclients/internal/model"
)

// A tile's origin must be independent of the point clicked and of pointer
// movement between press and release, including both events in one frame.
func TestViewerThumbnailOrigin(t *testing.T) {
	for _, scale := range []float32{1, 1.5} {
		for _, at := range []image.Point{image.Pt(5, 7), image.Pt(130, 90)} {
			u := New(mock.New())
			m := &model.Message{ID: "image", ChatID: "chat", Kind: model.KindImage, Media: model.MediaImage, ImageA: 0x123456}
			u.gallery.tab = model.GalleryMedia
			u.gallery.list.msgs = []*model.Message{m}
			var ops op.Ops
			var router input.Router
			now := testNow()
			origin := image.Pt(400, 170)
			size := image.Pt(int(160*scale), int(120*scale))
			visible := image.Rectangle{Min: origin.Add(image.Pt(0, 4)), Max: origin.Add(size).Sub(image.Pt(0, 4))}
			frame := func() {
				ops.Reset()
				gtx := layout.Context{Ops: &ops, Now: now, Source: router.Source(),
					Metric: unit.Metric{PxPerDp: scale, PxPerSp: scale}, Constraints: layout.Exact(image.Pt(1100, 700))}
				crop := clip.Rect(visible).Push(&ops)
				off := op.Offset(origin).Push(&ops)
				tg := gtx
				tg.Constraints = layout.Exact(size)
				u.galleryTile(tg, m)
				off.Pop()
				crop.Pop()
				off = op.Offset(visible.Min).Push(&ops)
				vg := gtx
				vg.Constraints = layout.Exact(visible.Size())
				u.mediaGallery.track(vg, u)
				off.Pop()
				u.trackMouse(gtx)
				router.Frame(&ops)
				now = now.Add(16 * time.Millisecond)
			}
			frame()
			pos := pointF(origin.Add(at))
			router.Queue(pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Position: pos, Buttons: pointer.ButtonPrimary},
				pointer.Event{Kind: pointer.Release, Source: pointer.Mouse, Position: pos.Add(f32.Pt(2, 1))})
			frame()
			want := image.Rectangle{Min: origin, Max: origin.Add(size)}
			if !u.viewer.open || u.viewer.origin != want {
				t.Fatalf("scale %v click %v: open=%v origin=%v, want %v", scale, at, u.viewer.open, u.viewer.origin, want)
			}
			var drawOps op.Ops
			gtx := layout.Context{Ops: &drawOps, Metric: unit.Metric{PxPerDp: scale, PxPerSp: scale}}
			area := image.Rect(140, 74, 960, 550)
			if got := u.layoutViewerImage(gtx, m, area, 0); got != visible {
				t.Fatalf("opening frame = %v, want visible thumbnail %v", got, visible)
			}
			end := u.layoutViewerImage(gtx, m, area, 1)
			if end.Empty() || !end.In(area) {
				t.Fatalf("settled image outside viewer: %v", end)
			}
			mid := u.layoutViewerImage(gtx, m, area, 0.5)
			if mid == want || mid == end {
				t.Fatal("opening has no travel between thumbnail and viewer")
			}
			u.closeViewer()
			if got := u.layoutViewerImage(gtx, m, area, 0); got != visible {
				t.Fatalf("closing frame = %v, want visible thumbnail %v", got, visible)
			}
		}
	}
}
