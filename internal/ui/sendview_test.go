package ui

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/input"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/chomosuke9/wazzapclients/internal/mock"
	"github.com/chomosuke9/wazzapclients/internal/model"
)

// sendHarness drives a UI with the send view open on a photo.
type sendHarness struct {
	t   *testing.T
	u   *UI
	r   input.Router
	ops op.Ops
	now time.Time
	off f32.Point // window position of the canvas's origin
}

func newSendHarness(t *testing.T) *sendHarness {
	h := &sendHarness{t: t, u: New(mock.New()), now: time.Now()}
	h.u.Start(func() {})
	h.u.SelectID("rina")
	dir := t.TempDir()
	img := image.NewNRGBA(image.Rect(0, 0, 400, 300))
	for y := range 300 {
		for x := range 400 {
			img.SetNRGBA(x, y, color.NRGBA{R: uint8(x), G: uint8(y), B: 0x80, A: 0xff})
		}
	}
	photo := filepath.Join(dir, "photo.png")
	f, err := os.Create(photo)
	if err != nil {
		t.Fatal(err)
	}
	png.Encode(f, img)
	f.Close()
	doc := filepath.Join(dir, "notes.txt")
	os.WriteFile(doc, []byte("hello"), 0o600)
	h.u.attachPaths([]string{photo, doc})
	if len(h.u.attach.files) != 2 {
		t.Fatalf("attachPaths added %d files, want 2", len(h.u.attach.files))
	}
	if m := h.u.attach.files[1].Media; m != model.MediaDocument {
		t.Fatalf("a .txt file goes as %v, want a document", m)
	}
	for i := 0; i < 200 && !h.ready(); i++ {
		h.frame()
		time.Sleep(5 * time.Millisecond)
	}
	if !h.ready() {
		t.Fatal("the photo never decoded")
	}
	for range 40 { // let the view slide all the way in
		h.frame()
	}
	// Find the canvas: press somewhere on it and see where it says.
	h.u.setTool(toolNone)
	a := h.u.attach.ed.area
	probe := f32.Pt(700, 300)
	h.r.Queue(pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Position: probe, Buttons: pointer.ButtonPrimary},
		pointer.Event{Kind: pointer.Release, Source: pointer.Mouse, Position: probe})
	h.frame()
	h.off = probe.Sub(h.u.attach.ed.dragFrom)
	if a.Empty() || h.off == (f32.Point{}) {
		t.Fatalf("no canvas found (area %v)", a)
	}
	return h
}

func (h *sendHarness) ready() bool {
	v := h.u.attach.ed.view
	return v != nil && v.state == imgReady && !h.u.attach.ed.area.Empty()
}

func (h *sendHarness) frame() {
	h.ops.Reset()
	h.u.Layout(layout.Context{Ops: &h.ops, Now: h.now, Source: h.r.Source(), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1},
		Constraints: layout.Exact(image.Pt(1100, 700))})
	h.r.Frame(&h.ops)
	h.now = h.now.Add(10 * time.Millisecond)
}

// at returns the window position of a point of the photo (in its pixels).
func (h *sendHarness) at(p f32.Point) f32.Point {
	f := h.u.attach.current()
	ed := &h.u.attach.ed
	tr, _ := fitPhoto(f.edit.region(ed.view.orig, ed.tool == toolCrop), f.edit.rot, ed.area, false)
	return tr.Transform(p).Add(h.off)
}

// drag presses at a, moves through b and lets go at c (window positions).
func (h *sendHarness) drag(pts ...f32.Point) {
	h.r.Queue(pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Position: pts[0], Buttons: pointer.ButtonPrimary})
	h.frame()
	for _, p := range pts[1:] {
		h.r.Queue(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: p, Buttons: pointer.ButtonPrimary})
		h.frame()
	}
	h.r.Queue(pointer.Event{Kind: pointer.Release, Source: pointer.Mouse, Position: pts[len(pts)-1]})
	h.frame()
}

func TestSendViewEditing(t *testing.T) {
	h := newSendHarness(t)
	u := h.u
	f := u.attach.current()
	e := &f.edit

	// Draw a line, then undo it.
	u.setTool(toolDraw)
	h.frame()
	h.drag(h.at(f32.Pt(50, 50)), h.at(f32.Pt(150, 80)), h.at(f32.Pt(250, 120)))
	if len(e.marks) != 1 || e.marks[0].kind != markLine || len(e.marks[0].pts) < 3 {
		t.Fatalf("drawing made %+v, want one line of 3 points", e.marks)
	}
	if p := e.marks[0].pts[2]; abs32(p.X-250) > 2 || abs32(p.Y-120) > 2 {
		t.Errorf("the line ends at %v, want (250,120) in photo pixels", p)
	}
	e.undo()
	if len(e.marks) != 0 {
		t.Fatalf("undo left %d marks", len(e.marks))
	}

	// A rectangle; a click without a drag draws none.
	u.setTool(toolShape)
	h.drag(h.at(f32.Pt(100, 100)))
	if len(e.marks) != 0 {
		t.Fatalf("a click drew %d shapes", len(e.marks))
	}
	h.drag(h.at(f32.Pt(100, 100)), h.at(f32.Pt(200, 180)))
	if len(e.marks) != 1 || e.marks[0].kind != markRect {
		t.Fatalf("dragging drew %+v, want a rectangle", e.marks)
	}

	// Text: a click starts typing, Escape finishes.
	u.setTool(toolText)
	h.drag(h.at(f32.Pt(200, 150)))
	if u.attach.ed.typing != 1 {
		t.Fatalf("a click with the text tool isn't typing (typing=%d)", u.attach.ed.typing)
	}
	u.attach.ed.textEd.SetText("Hi")
	h.frame()
	u.Escape()
	h.frame()
	if len(e.marks) != 2 || e.marks[1].text != "Hi" || u.attach.ed.typing >= 0 {
		t.Fatalf("typing made %+v (typing=%d)", e.marks, u.attach.ed.typing)
	}

	// Drag the text along.
	u.setTool(toolNone)
	h.frame()
	from := e.marks[1].at
	h.drag(h.at(from), h.at(from.Add(f32.Pt(20, 10))), h.at(from.Add(f32.Pt(40, 20))))
	if d := e.marks[1].at.Sub(from); abs32(d.X-40) > 2 || abs32(d.Y-20) > 2 {
		t.Errorf("the text moved by %v, want (40,20)", d)
	}

	// Crop: drag the top left corner in.
	u.setTool(toolCrop)
	h.frame()
	h.drag(h.at(f32.Pt(0, 0)), h.at(f32.Pt(50, 40)), h.at(f32.Pt(100, 80)))
	u.setTool(toolNone)
	if c := e.crop; abs(c.Min.X-100) > 2 || abs(c.Min.Y-80) > 2 || c.Max != image.Pt(400, 300) {
		t.Errorf("the crop is %v, want about (100,80)-(400,300)", c)
	}

	// Rotate in crop mode: a quarter turn back.
	u.setTool(toolCrop)
	h.frame()
	e.push()
	e.rot = (e.rot + 3) % 4
	u.setTool(toolNone)
	h.frame()
	if !e.edited() {
		t.Fatal("the edit doesn't count as edited")
	}
}

func TestSendViewCaptionsAndSend(t *testing.T) {
	h := newSendHarness(t)
	u := h.u
	u.conv.composer.SetText("the photo")
	u.showFile(1)
	if got := u.conv.composer.Text(); got != "" {
		t.Fatalf("the document's caption starts as %q", got)
	}
	u.conv.composer.SetText("the notes")
	u.showFile(0)
	if got := u.conv.composer.Text(); got != "the photo" {
		t.Fatalf("back on the photo, its caption is %q", got)
	}
	// Edit the photo, so it goes through the renderer.
	f := u.attach.current()
	f.edit.marks = append(f.edit.marks, mark{kind: markLine, pts: []f32.Point{{X: 10, Y: 10}, {X: 300, Y: 200}}, width: 8, col: markColors[2]})
	before := len(u.msgs)
	u.sendComposer()
	if len(u.attach.files) != 0 {
		t.Fatal("the send view stayed open")
	}
	for i := 0; i < 300 && len(u.attach.outbox) > 0; i++ {
		h.frame()
		time.Sleep(5 * time.Millisecond)
	}
	if n := len(u.attach.outbox); n > 0 {
		t.Fatalf("%d files never went out", n)
	}
	msgs := u.msgs[before:]
	if len(msgs) != 2 {
		t.Fatalf("sent %d messages, want 2", len(msgs))
	}
	if msgs[0].Media != model.MediaImage || msgs[0].Text != "the photo" || msgs[1].Text != "the notes" {
		t.Errorf("sent %q (%v) then %q (%v)", msgs[0].Text, msgs[0].Media, msgs[1].Text, msgs[1].Media)
	}
	if msgs[0].FileName == "photo.png" {
		t.Error("the edited photo went out unedited")
	}
	if got := u.conv.composer.Text(); got != "" {
		t.Errorf("after sending, the composer has %q", got)
	}
}

func TestSendViewCloseRestoresDraft(t *testing.T) {
	u := New(mock.New())
	u.Start(func() {})
	u.SelectID("rina")
	u.conv.composer.SetText("half a message")
	dir := t.TempDir()
	p := filepath.Join(dir, "a.pdf")
	os.WriteFile(p, []byte("%PDF"), 0o600)
	u.attachPaths([]string{p})
	if got := u.conv.composer.Text(); got != "" {
		t.Fatalf("the caption starts as %q", got)
	}
	u.conv.composer.SetText("a caption")
	u.Escape()
	if len(u.attach.files) != 0 {
		t.Fatal("Escape didn't close the send view")
	}
	if got := u.conv.composer.Text(); got != "half a message" {
		t.Errorf("the composer came back with %q", got)
	}
}

func TestFitPhoto(t *testing.T) {
	region := image.Rect(100, 50, 500, 350) // 400 x 300
	area := image.Rect(0, 0, 600, 800)
	for rot := range 4 {
		tr, s := fitPhoto(region, rot, area, false)
		r := screenRect(tr, region).Canon()
		w, h := 400*s, 300*s
		if rot%2 != 0 {
			w, h = h, w
		}
		if abs32(float32(r.Dx())-w) > 1 || abs32(float32(r.Dy())-h) > 1 || !r.In(area.Inset(-1)) {
			t.Errorf("rot %d: the region lands on %v (scale %v)", rot, r, s)
		}
		back := tr.Invert().Transform(tr.Transform(f32.Pt(123, 222)))
		if abs32(back.X-123) > 0.01 || abs32(back.Y-222) > 0.01 {
			t.Errorf("rot %d: the inverse maps back to %v", rot, back)
		}
	}
	// A quarter turn is clockwise on screen: the region's top left corner
	// goes to the top right.
	tr, _ := fitPhoto(region, 1, area, false)
	r := screenRect(tr, region).Canon()
	if p := tr.Transform(pointF(region.Min)).Round(); abs(p.X-r.Max.X) > 1 || abs(p.Y-r.Min.Y) > 1 {
		t.Errorf("after a quarter turn the top left corner is at %v of %v", p, r)
	}
}

func TestCropDrag(t *testing.T) {
	bounds := image.Rect(0, 0, 400, 300)
	inv := f32.AffineId()
	if got := cropDrag(bounds, cropAll, image.Pt(50, 50), bounds, 40, inv); got != bounds {
		t.Errorf("moving a full crop gave %v", got)
	}
	from := image.Rect(100, 100, 200, 200)
	if got := cropDrag(from, cropLeft|cropTop, image.Pt(90, 90), bounds, 40, inv); got != image.Rect(160, 160, 200, 200) {
		t.Errorf("the corner went past the minimum size: %v", got)
	}
	if got := cropDrag(from, cropAll, image.Pt(500, -500), bounds, 40, inv); got != image.Rect(300, 0, 400, 100) {
		t.Errorf("moving out of bounds gave %v", got)
	}
	if m := cropHit(from, image.Pt(101, 150), 10); m != cropLeft {
		t.Errorf("the left edge hits %b", m)
	}
	if m := cropHit(from, image.Pt(150, 150), 10); m != cropAll {
		t.Errorf("the middle hits %b", m)
	}
	if m := cropHit(from, image.Pt(10, 10), 10); m != 0 {
		t.Errorf("far outside hits %b", m)
	}
}

func TestFilters(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 2, 1))
	copy(img.Pix, []byte{200, 100, 50, 255, 0, 0, 0, 0})
	if got := filtered(img, 0); string(got.Pix) != string(img.Pix) {
		t.Errorf("no filter changed the pixels to %v", got.Pix)
	}
	bw := filtered(img, 2)
	if p := bw.Pix; p[0] != p[1] || p[1] != p[2] {
		t.Errorf("B&W left color: %v", p[:4])
	}
	if p := bw.Pix[4:]; p[0] != 0 || p[1] != 0 || p[2] != 0 {
		t.Errorf("a clear pixel came out %v", p)
	}
}

// TestCenteredPicker checks that the picker for reactions and for the
// photo editor shows, and that a click on it doesn't close it.
func TestCenteredPicker(t *testing.T) {
	for _, mode := range []pickMode{pickReaction, pickMedia} {
		u := New(mock.New())
		u.Start(func() {})
		u.SelectID("rina")
		var ops op.Ops
		var r input.Router
		now := time.Now()
		frame := func() {
			ops.Reset()
			u.Layout(layout.Context{Ops: &ops, Now: now, Source: r.Source(), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1},
				Constraints: layout.Exact(image.Pt(1100, 700))})
			r.Frame(&ops)
			now = now.Add(10 * time.Millisecond)
		}
		frame()
		u.openPicker(mode, u.msgs[len(u.msgs)-1])
		for range 30 {
			frame()
		}
		p := f32.Pt(550, 120) // the panel's header, in the middle of the window
		r.Queue(pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Position: p, Buttons: pointer.ButtonPrimary},
			pointer.Event{Kind: pointer.Release, Source: pointer.Mouse, Position: p})
		frame()
		frame()
		if !u.picker.open {
			t.Errorf("mode %d: a click on the picker closed it", mode)
		}
	}
}
