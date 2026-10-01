package ui

import (
	"bytes"
	"image"
	"image/color"
	"math"
	"os"
	"strings"
	"time"

	"gioui.org/f32"
	"gioui.org/font"
	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/text"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"github.com/chomosuke9/wazzapclients/internal/model"
	"github.com/chomosuke9/wazzapclients/internal/photo"
)

// The send view's photo editor, like WhatsApp's: crop and rotate, filters,
// drawing, text, shapes, blur, emoji and stickers. Edits are kept as data
// in the original photo's pixels (photoEdit) and drawn over the photo
// every frame; sending renders them into a new picture (editrender.go).

type editTool int

const (
	toolNone editTool = iota
	toolCrop
	toolFilter
	toolDraw
	toolText
	toolShape
	toolBlur
)

type markKind int

const (
	markLine markKind = iota // a freehand line
	markRect                 // a rectangle's outline
	markBlur                 // a line that blurs the photo under it
	markText
	markEmoji
	markSticker
)

// mark is something drawn on a photo. Positions and sizes are in the
// original photo's pixels, so they stay put through crops and rotations.
type mark struct {
	kind  markKind
	pts   []f32.Point // lines: the points; rectangles: two corners
	width float32     // lines and rectangles: the stroke width
	col   color.NRGBA

	// Text, emoji and stickers sit centered on at, size tall (text: the
	// font size). turns is the photo's rotation when the mark was placed:
	// it stays upright on screen then, and turns with the photo after.
	at      f32.Point
	size    float32
	turns   int
	text    string
	img     paint.ImageOp // sticker
	imgSize image.Point
	box     f32.Point // text: its size when last drawn
}

// movable reports whether a mark is moved and resized as a whole.
func (m *mark) movable() bool { return m.kind >= markText }

// photoEdit is how a picked photo is edited.
type photoEdit struct {
	rot    int             // quarter turns clockwise
	crop   image.Rectangle // in original pixels; empty for all of it
	filter int             // index in photoFilters
	marks  []mark
	hist   []photoSnap // for undo
}

type photoSnap struct {
	rot    int
	crop   image.Rectangle
	filter int
	marks  []mark
}

// edited reports whether the photo needs rendering to send.
func (e *photoEdit) edited() bool {
	return e.rot != 0 || !e.crop.Empty() || e.filter != 0 || len(e.marks) > 0
}

// push saves the edit before a change, for undo. Marks are values and a
// line's points only grow while it's drawn, so a shallow copy will do.
func (e *photoEdit) push() {
	e.hist = append(e.hist, photoSnap{e.rot, e.crop, e.filter, append([]mark(nil), e.marks...)})
	if len(e.hist) > 100 {
		e.hist = e.hist[1:]
	}
}

func (e *photoEdit) undo() {
	if n := len(e.hist); n > 0 {
		s := e.hist[n-1]
		e.hist = e.hist[:n-1]
		e.rot, e.crop, e.filter, e.marks = s.rot, s.crop, s.filter, s.marks
	}
}

// copyEdit returns e without its history, for rendering.
func (e *photoEdit) copyEdit() photoEdit {
	c := *e
	c.hist = nil
	c.marks = append([]mark(nil), e.marks...)
	return c
}

// region is the part of the photo shown: the crop, or all of it while
// cropping.
func (e *photoEdit) region(orig image.Point, cropping bool) image.Rectangle {
	full := image.Rectangle{Max: orig}
	if cropping || e.crop.Empty() {
		return full
	}
	return e.crop.Intersect(full)
}

// quarter turns by r quarter turns clockwise (on screen, where y points
// down), exactly.
func quarter(r int) f32.Affine2D {
	switch (r%4 + 4) % 4 {
	case 1:
		return f32.NewAffine2D(0, -1, 0, 1, 0, 0)
	case 2:
		return f32.NewAffine2D(-1, 0, 0, 0, -1, 0)
	case 3:
		return f32.NewAffine2D(0, 1, 0, -1, 0, 0)
	}
	return f32.AffineId()
}

// fitPhoto returns the transform from photo pixels that shows region,
// turned rot quarter turns, fitted in area (or covering it), and its
// scale.
func fitPhoto(region image.Rectangle, rot int, area image.Rectangle, cover bool) (f32.Affine2D, float32) {
	w, h := float32(region.Dx()), float32(region.Dy())
	if rot%2 != 0 {
		w, h = h, w
	}
	sx, sy := float32(area.Dx())/max(w, 1), float32(area.Dy())/max(h, 1)
	s := min(sx, sy)
	if cover {
		s = max(sx, sy)
	}
	c := f32.Pt(float32(region.Min.X+region.Max.X)/2, float32(region.Min.Y+region.Max.Y)/2)
	ac := f32.Pt(float32(area.Min.X+area.Max.X)/2, float32(area.Min.Y+area.Max.Y)/2)
	tr := quarter(rot).Mul(f32.AffineId().Offset(c.Mul(-1)))
	return tr.Scale(f32.Point{}, f32.Pt(s, s)).Offset(ac), s
}

// screenRect maps a rectangle of photo pixels through tr, which only
// turns by quarter turns.
func screenRect(tr f32.Affine2D, r image.Rectangle) image.Rectangle {
	a, b := tr.Transform(pointF(r.Min)), tr.Transform(pointF(r.Max))
	return image.Rect(int(math.Round(float64(a.X))), int(math.Round(float64(a.Y))),
		int(math.Round(float64(b.X))), int(math.Round(float64(b.Y))))
}

// photoPics is a photo's pictures to draw an edit with: the photo, and a
// tiny copy for the blur. Each covers a rectangle of the photo's pixels:
// all of it on screen, the crop when rendering.
type photoPics struct {
	img         paint.ImageOp
	size        image.Point // of img
	cover       image.Rectangle
	mosaic      paint.ImageOp
	mosaicSize  image.Point // zero when there is none (blur isn't drawn)
	mosaicCover image.Rectangle
}

// paintCovering paints img, of size sz, stretched over r.
func paintCovering(gtx C, img paint.ImageOp, sz image.Point, r image.Rectangle) {
	defer op.Affine(f32.AffineId().Scale(f32.Point{}, f32.Pt(
		float32(r.Dx())/float32(sz.X), float32(r.Dy())/float32(sz.Y))).Offset(pointF(r.Min))).Push(gtx.Ops).Pop()
	img.Filter = paint.FilterLinear
	img.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
}

// blurBlocks is how many blocks the blur's tiny copy of a photo has on
// its long side; drawn smoothly scaled up, it blurs.
const blurBlocks = 28

// drawPhoto draws a photo and its edit's marks under tr (photo pixels to
// gtx pixels), clipped to region. skip is a mark not to draw (the text
// being typed), or -1.
func (u *UI) drawPhoto(gtx C, pics photoPics, e *photoEdit, region image.Rectangle, tr f32.Affine2D, skip int) {
	defer op.Affine(tr).Push(gtx.Ops).Pop()
	defer clip.Rect(region).Push(gtx.Ops).Pop()
	if pics.size.X > 0 {
		paintCovering(gtx, pics.img, pics.size, pics.cover)
	}
	// Text is laid out in photo pixels.
	mg := gtx
	mg.Metric = unit.Metric{PxPerDp: 1, PxPerSp: 1}
	for i := range e.marks {
		if i != skip {
			u.drawMark(mg, &e.marks[i], pics)
		}
	}
}

// linePath is a path through pts; one point makes a dot.
func linePath(ops *op.Ops, pts []f32.Point) clip.PathSpec {
	var p clip.Path
	p.Begin(ops)
	p.MoveTo(pts[0])
	if len(pts) == 1 {
		p.LineTo(pts[0].Add(f32.Pt(0.01, 0)))
	}
	for _, q := range pts[1:] {
		p.LineTo(q)
	}
	return p.End()
}

func (u *UI) drawMark(gtx C, m *mark, pics photoPics) {
	switch m.kind {
	case markLine:
		if len(m.pts) > 0 {
			paint.FillShape(gtx.Ops, m.col, clip.Stroke{Path: linePath(gtx.Ops, m.pts), Width: m.width}.Op())
		}
	case markBlur:
		if len(m.pts) == 0 || pics.mosaicSize.X == 0 {
			return
		}
		c := clip.Stroke{Path: linePath(gtx.Ops, m.pts), Width: m.width}.Op().Push(gtx.Ops)
		paintCovering(gtx, pics.mosaic, pics.mosaicSize, pics.mosaicCover)
		c.Pop()
	case markRect:
		if len(m.pts) == 2 {
			a, b := m.pts[0], m.pts[1]
			strokeRect(gtx, a.X, a.Y, b.X, b.Y, m.width, m.col)
		}
	default:
		// Upright around its center.
		t := op.Affine(quarter(-m.turns).Offset(m.at)).Push(gtx.Ops)
		defer t.Pop()
		if m.kind == markSticker {
			if m.imgSize.X > 0 {
				sc := m.size / float32(max(m.imgSize.X, m.imgSize.Y))
				w, h := float32(m.imgSize.X)*sc, float32(m.imgSize.Y)*sc
				defer op.Affine(f32.AffineId().Scale(f32.Point{}, f32.Pt(sc, sc)).Offset(f32.Pt(-w/2, -h/2))).Push(gtx.Ops).Pop()
				img := m.img
				img.Filter = paint.FilterLinear
				img.Add(gtx.Ops)
				paint.PaintOp{}.Add(gtx.Ops)
			}
			m.box = f32.Pt(m.size, m.size)
			return
		}
		col := m.col
		if m.kind == markEmoji {
			col = color.NRGBA{A: 0xff}
		}
		// Laid out at most markTextPx tall and scaled up: Gio clips a
		// color emoji to its bitmap's size before scaling it, so a bigger
		// font shows only the bitmap's corner.
		base := min(m.size, markTextPx)
		k := m.size / base
		defer op.Affine(f32.AffineId().Scale(f32.Point{}, f32.Pt(k, k))).Push(gtx.Ops).Pop()
		lab := func(col color.NRGBA) part {
			return record(gtx, func(gtx C) D {
				gtx.Constraints = layout.Constraints{Max: image.Pt(1<<20, 1<<20)}
				l := material.Label(u.th, unit.Sp(base), m.text)
				l.Color, l.Font.Weight, l.Alignment, l.LineHeightScale = col, font.Bold, text.Middle, 1.1
				return l.Layout(gtx)
			})
		}
		body := lab(col)
		m.box = f32.Pt(float32(body.size.X)*k, float32(body.size.Y)*k)
		x, y := -body.size.X/2, -body.size.Y/2
		if m.kind == markText {
			// A soft shadow keeps light text readable on light photos.
			d := max(1, int(base*0.05))
			lab(color.NRGBA{A: 0x70}).at(gtx, x+d, y+d)
		}
		body.at(gtx, x, y)
	}
}

// markTextPx is the largest font size marks are laid out at.
const markTextPx = 96

// markHit returns the movable mark under p (photo pixels), topmost
// first, or -1. slack widens their boxes.
func markHit(marks []mark, p f32.Point, slack float32) int {
	for i := len(marks) - 1; i >= 0; i-- {
		m := &marks[i]
		if !m.movable() {
			continue
		}
		d := quarter(m.turns).Transform(p.Sub(m.at)) // into the mark's upright frame
		if abs32(d.X) <= m.box.X/2+slack && abs32(d.Y) <= m.box.Y/2+slack {
			return i
		}
	}
	return -1
}

func abs32(f float32) float32 { return max(f, -f) }

// markBounds is a movable mark's box on screen.
func markBounds(m *mark, tr f32.Affine2D) image.Rectangle {
	hw, hh := m.box.X/2, m.box.Y/2
	if m.turns%2 != 0 {
		hw, hh = hh, hw
	}
	a := image.Pt(int(m.at.X-hw), int(m.at.Y-hh))
	b := image.Pt(int(m.at.X+hw), int(m.at.Y+hh))
	return screenRect(tr, image.Rectangle{Min: a, Max: b})
}

// Mark colors and stroke widths (in dp on screen), like WhatsApp's.
var (
	drawWidths  = [...]unit.Dp{4, 8, 14}
	shapeWidths = [...]unit.Dp{3, 5, 8}
	blurWidths  = [...]unit.Dp{24, 40, 64}
)

// editState is the editor's state for the photo on show.
type editState struct {
	tool   editTool
	color  int // index in markColors
	width  int // index in the tool's widths
	sel    int // the selected movable mark, or -1
	typing int // the text mark being typed, or -1
	textEd widget.Editor

	canvas struct{} // input tag of the photo

	// The press being dragged.
	drag      editDrag
	dragMark  int
	dragFrom  f32.Point // where it started, on screen
	dragAt    f32.Point // the mark's position then
	dragMoved bool
	cropMask  int             // the crop edges being dragged (cropLeft, ...)
	cropFrom  image.Rectangle // the crop on screen then
	lastPress time.Duration   // for double clicks on text
	lastMark  int

	view *photoView      // the photo on show, decoded for the screen
	area image.Rectangle // where it was drawn last
}

type editDrag int

const (
	dragNone editDrag = iota
	dragLine
	dragShape
	dragMove
	dragCrop
)

const (
	cropLeft = 1 << iota
	cropRight
	cropTop
	cropBottom
	cropAll = cropLeft | cropRight | cropTop | cropBottom
)

// photoView is a photo decoded for the screen, with what the editor
// draws it with.
type photoView struct {
	path   string
	state  imgState
	base   *image.RGBA // the photo fitted in previewSide
	orig   image.Point
	filter int // the filter pics.img shows
	pics   photoPics
	thumbs []paint.ImageOp // the photo per filter, for the filter bar
	thumbN image.Point
	done   chan *photoView
}

// previewSide is the long side the editor decodes photos to.
const previewSide = 1600

// photoFor returns the decoded photo of f, loading it in the background
// first.
func (u *UI) photoFor(f *attachFile) *photoView {
	ed := &u.attach.ed
	v := ed.view
	if v == nil || v.path != f.Path {
		v = &photoView{path: f.Path, done: make(chan *photoView, 1), filter: -1}
		ed.view = v
		path, notify, done := f.Path, u.images.invalidate, v.done
		go func() {
			r := &photoView{state: imgMissing}
			if data, err := os.ReadFile(path); err == nil {
				release := acquireDecode(data)
				img, _ := decodeScaled(data, previewSide)
				orig := image.Point{}
				if c, _, err := image.DecodeConfig(bytes.NewReader(data)); err == nil {
					orig = image.Pt(c.Width, c.Height)
				}
				release()
				if rgba, ok := img.(*image.RGBA); ok && orig.X > 0 {
					r.state, r.base, r.orig = imgReady, rgba, orig
				}
			}
			done <- r
			if notify != nil {
				notify()
			}
		}()
	}
	select {
	case r := <-v.done:
		v.state, v.base, v.orig = r.state, r.base, r.orig
		f.orig = r.orig
	default:
	}
	if v.state == imgReady && v.filter != f.edit.filter {
		u.applyViewFilter(v, f.edit.filter)
	}
	return v
}

// applyViewFilter makes the screen picture and the blur's copy show
// filter k.
func (u *UI) applyViewFilter(v *photoView, k int) {
	img := v.base
	if k != 0 {
		img = filtered(v.base, k)
	}
	block := float64(max(v.orig.X, v.orig.Y)) / blurBlocks
	mw := max(1, int(math.Ceil(float64(v.orig.X)/block)))
	mh := max(1, int(math.Ceil(float64(v.orig.Y)/block)))
	v.filter = k
	full := image.Rectangle{Max: v.orig}
	v.pics = photoPics{
		img: paint.NewImageOp(img), size: img.Bounds().Size(), cover: full,
		mosaic: paint.NewImageOp(photo.Shrink(img, mw, mh)), mosaicSize: image.Pt(mw, mh),
		mosaicCover: image.Rectangle{Max: image.Pt(int(float64(mw)*block), int(float64(mh)*block))},
	}
}

// filterThumbs returns the photo's thumbnail with each filter.
func (u *UI) filterThumbs(v *photoView) []paint.ImageOp {
	if v.thumbs == nil && v.state == imgReady {
		s := max(v.orig.X, v.orig.Y)
		w, h := max(1, 96*v.orig.X/s), max(1, 96*v.orig.Y/s)
		small := photo.Shrink(v.base, w, h)
		for k := range photoFilters {
			img := small
			if k != 0 {
				img = filtered(small, k)
			}
			v.thumbs = append(v.thumbs, paint.NewImageOp(img))
		}
		v.thumbN = image.Pt(w, h)
	}
	return v.thumbs
}

// setTool switches the editor's tool, finishing what the last one did.
func (u *UI) setTool(t editTool) {
	ed := &u.attach.ed
	u.finishTyping()
	if f := u.attach.current(); f != nil && ed.tool == toolCrop && t != toolCrop && ed.view != nil {
		// A crop of the whole photo is no crop.
		if f.edit.crop == (image.Rectangle{Max: ed.view.orig}) {
			f.edit.crop = image.Rectangle{}
		}
	}
	if ed.tool == t {
		t = toolNone
	}
	if t == toolCrop {
		if f := u.attach.current(); f != nil && ed.view != nil && f.edit.crop.Empty() {
			f.edit.crop = image.Rectangle{Max: ed.view.orig}
		}
	}
	ed.tool, ed.drag, ed.sel = t, dragNone, -1
	ed.width = 1
}

// resetEditor forgets the editor's state for another photo.
func (u *UI) resetEditor() {
	ed := &u.attach.ed
	ed.tool, ed.drag, ed.sel, ed.typing = toolNone, dragNone, -1, -1
	ed.view = nil
}

// finishTyping ends typing a text mark; an empty one goes.
func (u *UI) finishTyping() {
	ed := &u.attach.ed
	f := u.attach.current()
	if ed.typing < 0 || f == nil || ed.typing >= len(f.edit.marks) {
		ed.typing = -1
		return
	}
	m := &f.edit.marks[ed.typing]
	m.text = strings.TrimSpace(ed.textEd.Text())
	if m.text == "" {
		f.edit.marks = append(f.edit.marks[:ed.typing:ed.typing], f.edit.marks[ed.typing+1:]...)
		ed.sel = -1
	} else {
		ed.sel = ed.typing
	}
	ed.typing = -1
	u.requestFocus(&ed.canvas)
}

// startTyping opens the editor on text mark i.
func (u *UI) startTyping(f *attachFile, i int) {
	ed := &u.attach.ed
	ed.typing, ed.sel = i, -1
	ed.textEd.Submit = true
	ed.textEd.SetText(f.edit.marks[i].text)
	ed.textEd.SetCaret(ed.textEd.Len(), ed.textEd.Len())
	u.requestFocus(&ed.textEd)
}

// placeMark adds an emoji or sticker in the middle of what's shown.
func (u *UI) placeMark(m mark) {
	f := u.attach.current()
	ed := &u.attach.ed
	if f == nil || ed.view == nil || ed.view.state != imgReady {
		return
	}
	u.finishTyping()
	r := f.edit.region(ed.view.orig, false)
	m.at = f32.Pt(float32(r.Min.X+r.Max.X)/2, float32(r.Min.Y+r.Max.Y)/2)
	m.size = float32(min(r.Dx(), r.Dy())) * 0.22
	m.turns = f.edit.rot
	m.box = f32.Pt(m.size, m.size)
	f.edit.push()
	f.edit.marks = append(f.edit.marks, m)
	ed.sel = len(f.edit.marks) - 1
	if ed.tool != toolText {
		ed.tool = toolNone
	}
}

func (u *UI) placeEmoji(ch string) { u.placeMark(mark{kind: markEmoji, text: ch}) }

func (u *UI) placeSticker(gtx C, s *model.Message) {
	img := u.messageImage(s, gtx.Dp(220))
	if img == nil || img.state != imgReady {
		u.toast("That sticker is still loading.")
		return
	}
	u.placeMark(mark{kind: markSticker, img: img.op, imgSize: img.size})
}

// deleteSelected removes the selected mark.
func (u *UI) deleteSelected() {
	f, ed := u.attach.current(), &u.attach.ed
	if f == nil || ed.sel < 0 || ed.sel >= len(f.edit.marks) {
		return
	}
	f.edit.push()
	f.edit.marks = append(f.edit.marks[:ed.sel:ed.sel], f.edit.marks[ed.sel+1:]...)
	ed.sel = -1
}

// pickColor sets the color of what's drawn next, and of the selected
// (or typed) text.
func (u *UI) pickColor(i int) {
	f, ed := u.attach.current(), &u.attach.ed
	ed.color = i
	if f == nil {
		return
	}
	for _, j := range []int{ed.sel, ed.typing} {
		if j >= 0 && j < len(f.edit.marks) && f.edit.marks[j].kind == markText {
			if j == ed.sel {
				f.edit.push()
			}
			f.edit.marks[j].col = markColors[i]
		}
	}
}

// layoutCanvas draws the photo f in area and handles editing it.
func (u *UI) layoutCanvas(gtx C, f *attachFile, area image.Rectangle) {
	ed := &u.attach.ed
	v := u.photoFor(f)
	if v.state != imgReady {
		if v.state == imgMissing {
			u.layoutFileCard(gtx, f, area, "Couldn't open this photo")
		}
		return
	}
	e := &f.edit
	ed.area = area
	if demo := u.attach.demoEdit; demo != "" {
		u.attach.demoEdit = ""
		u.demoEdit(f, v, demo)
	}
	cropping := ed.tool == toolCrop
	region := e.region(v.orig, cropping)
	tr, scale := fitPhoto(region, e.rot, area, false)
	u.canvasInput(gtx, f, v, tr, scale)
	// The input may have turned the photo.
	region = e.region(v.orig, cropping)
	tr, scale = fitPhoto(region, e.rot, area, false)

	u.drawPhoto(gtx, v.pics, e, region, tr, ed.typing)
	if cropping {
		u.drawCropFrame(gtx, screenRect(tr, region), screenRect(tr, e.crop.Intersect(region)))
	}
	if ed.sel >= 0 && ed.sel < len(e.marks) && ed.typing < 0 {
		r := markBounds(&e.marks[ed.sel], tr).Inset(-gtx.Dp(8))
		w := float32(max(1, gtx.Dp(1.5)))
		strokeRect(gtx, float32(r.Min.X)+1, float32(r.Min.Y)+1, float32(r.Max.X)+1, float32(r.Max.Y)+1, w, argb(0, 0x80))
		strokeRect(gtx, float32(r.Min.X), float32(r.Min.Y), float32(r.Max.X), float32(r.Max.Y), w, rgb(0xffffff))
	}

	// The photo's input area, under the text being typed.
	func() {
		defer clip.Rect(area).Push(gtx.Ops).Pop()
		switch {
		case ed.drag == dragMove:
			pointer.CursorGrabbing.Add(gtx.Ops)
		case ed.tool == toolDraw || ed.tool == toolShape || ed.tool == toolBlur:
			pointer.CursorCrosshair.Add(gtx.Ops)
		case ed.tool == toolText:
			pointer.CursorText.Add(gtx.Ops)
		}
		event.Op(gtx.Ops, &ed.canvas)
	}()

	if ed.typing >= 0 && ed.typing < len(e.marks) {
		m := &e.marks[ed.typing]
		for {
			ev, ok := ed.textEd.Update(gtx)
			if !ok {
				break
			}
			if _, ok := ev.(widget.SubmitEvent); ok {
				u.finishTyping()
				return
			}
		}
		m.text = ed.textEd.Text()
		at := tr.Transform(m.at).Round()
		box := record(gtx, func(gtx C) D {
			gtx.Constraints = layout.Constraints{Max: image.Pt(area.Dx(), area.Dy())}
			es := material.Editor(u.th, &ed.textEd, "Type text")
			es.TextSize = unit.Sp(m.size * scale / gtx.Metric.PxPerSp)
			es.Color, es.HintColor, es.Font.Weight = m.col, faded(m.col, 0.6), font.Bold
			es.SelectionColor = argb(0x53bdeb, 0x60)
			ed.textEd.Alignment = text.Middle
			return es.Layout(gtx)
		})
		r := image.Rectangle{Min: at.Sub(box.size.Div(2)), Max: at.Add(box.size.Div(2))}.Inset(-gtx.Dp(6))
		fillRRect(gtx, r, gtx.Dp(6), argb(0, 0x40))
		box.at(gtx, at.X-box.size.X/2, at.Y-box.size.Y/2)
	}
}

// drawCropFrame darkens the photo outside the crop and draws the crop's
// frame and corner handles.
func (u *UI) drawCropFrame(gtx C, all, crop image.Rectangle) {
	dim := argb(0, 0x99)
	fillRect(gtx, image.Rect(all.Min.X, all.Min.Y, all.Max.X, crop.Min.Y), dim)
	fillRect(gtx, image.Rect(all.Min.X, crop.Max.Y, all.Max.X, all.Max.Y), dim)
	fillRect(gtx, image.Rect(all.Min.X, crop.Min.Y, crop.Min.X, crop.Max.Y), dim)
	fillRect(gtx, image.Rect(crop.Max.X, crop.Min.Y, all.Max.X, crop.Max.Y), dim)
	white := rgb(0xffffff)
	// Thirds, then the frame.
	thin := max(1, gtx.Dp(1))
	for i := 1; i < 3; i++ {
		x := crop.Min.X + crop.Dx()*i/3
		y := crop.Min.Y + crop.Dy()*i/3
		fillRect(gtx, image.Rect(x, crop.Min.Y, x+thin, crop.Max.Y), argb(0xffffff, 0x60))
		fillRect(gtx, image.Rect(crop.Min.X, y, crop.Max.X, y+thin), argb(0xffffff, 0x60))
	}
	w := float32(max(1, gtx.Dp(1.5)))
	strokeRect(gtx, float32(crop.Min.X), float32(crop.Min.Y), float32(crop.Max.X), float32(crop.Max.Y), w, white)
	t, l := gtx.Dp(4), gtx.Dp(22)
	for _, c := range []image.Point{crop.Min, {crop.Max.X, crop.Min.Y}, {crop.Min.X, crop.Max.Y}, crop.Max} {
		dx, dy := 1, 1
		if c.X == crop.Max.X {
			dx = -1
		}
		if c.Y == crop.Max.Y {
			dy = -1
		}
		h := image.Rectangle{Min: c, Max: c.Add(image.Pt(dx*l, dy*t))}.Canon().Add(image.Pt(-dx*t/2, -dy*t/2))
		v := image.Rectangle{Min: c, Max: c.Add(image.Pt(dx*t, dy*l))}.Canon().Add(image.Pt(-dx*t/2, -dy*t/2))
		fillRect(gtx, h, white)
		fillRect(gtx, v, white)
	}
}

// canvasInput handles presses, drags and the wheel on the photo, and
// keys while it has the focus.
func (u *UI) canvasInput(gtx C, f *attachFile, v *photoView, tr f32.Affine2D, scale float32) {
	ed := &u.attach.ed
	e := &f.edit
	inv := tr.Invert()
	full := image.Rectangle{Max: v.orig}
	toPhoto := func(p f32.Point) f32.Point { return inv.Transform(p) }
	clampPt := func(p f32.Point) f32.Point {
		return f32.Pt(min(max(p.X, 0), float32(v.orig.X)), min(max(p.Y, 0), float32(v.orig.Y)))
	}
	widths := drawWidths[:]
	switch ed.tool {
	case toolShape:
		widths = shapeWidths[:]
	case toolBlur:
		widths = blurWidths[:]
	}
	for {
		ev, ok := gtx.Event(
			key.FocusFilter{Target: &ed.canvas},
			key.Filter{Focus: &ed.canvas, Name: key.NameDeleteForward},
			key.Filter{Focus: &ed.canvas, Name: key.NameDeleteBackward},
			key.Filter{Focus: &ed.canvas, Name: "Z", Required: key.ModShortcut},
			pointer.Filter{Target: &ed.canvas, Kinds: pointer.Press | pointer.Drag | pointer.Release | pointer.Cancel | pointer.Scroll,
				ScrollY: pointer.ScrollRange{Min: -1e6, Max: 1e6}},
		)
		if !ok {
			break
		}
		switch e2 := ev.(type) {
		case key.Event:
			if e2.State != key.Press || ed.typing >= 0 {
				continue
			}
			if e2.Name == "Z" {
				e.undo()
				ed.sel = -1
			} else {
				u.deleteSelected()
			}
		case pointer.Event:
			pos := e2.Position
			switch e2.Kind {
			case pointer.Scroll:
				// The wheel resizes the selected mark.
				if ed.sel >= 0 && ed.sel < len(e.marks) && ed.typing < 0 {
					m := &e.marks[ed.sel]
					k := float32(math.Pow(1.0015, float64(-e2.Scroll.Y)))
					lim := float32(max(v.orig.X, v.orig.Y))
					m.size = min(max(m.size*k, lim*0.02), lim*1.5)
				}
			case pointer.Press:
				if !e2.Buttons.Contain(pointer.ButtonPrimary) {
					continue
				}
				if ed.typing >= 0 {
					u.finishTyping()
				} else {
					gtx.Execute(key.FocusCmd{Tag: &ed.canvas})
				}
				o := toPhoto(pos)
				ed.drag, ed.dragFrom, ed.dragMoved = dragNone, pos, false
				switch ed.tool {
				case toolCrop:
					cr := screenRect(tr, e.crop)
					ed.cropMask = cropHit(cr, pos.Round(), gtx.Dp(18))
					if ed.cropMask != 0 {
						ed.drag, ed.cropFrom = dragCrop, cr
					}
				case toolDraw, toolBlur:
					kind, col := markLine, markColors[ed.color]
					if ed.tool == toolBlur {
						kind = markBlur
					}
					e.push()
					e.marks = append(e.marks, mark{kind: kind, pts: []f32.Point{o}, col: col,
						width: float32(gtx.Dp(widths[ed.width])) / scale})
					ed.drag, ed.dragMark, ed.sel = dragLine, len(e.marks)-1, -1
				case toolShape:
					e.push()
					e.marks = append(e.marks, mark{kind: markRect, pts: []f32.Point{o, o}, col: markColors[ed.color],
						width: float32(gtx.Dp(widths[ed.width])) / scale})
					ed.drag, ed.dragMark, ed.sel = dragShape, len(e.marks)-1, -1
				default:
					i := markHit(e.marks, o, float32(gtx.Dp(8))/scale)
					switch {
					case i >= 0 && e.marks[i].kind == markText && i == ed.lastMark && e2.Time-ed.lastPress < 400*time.Millisecond:
						// A double click types into the text.
						e.push()
						u.startTyping(f, i)
					case i >= 0:
						ed.sel, ed.drag, ed.dragMark, ed.dragAt = i, dragMove, i, e.marks[i].at
					case ed.tool == toolText:
						if !o.Round().In(e.region(v.orig, false)) {
							break
						}
						e.push()
						e.marks = append(e.marks, mark{kind: markText, at: o, col: markColors[ed.color], turns: e.rot,
							size: float32(gtx.Dp(30)) / scale})
						u.startTyping(f, len(e.marks)-1)
					default:
						ed.sel = -1
					}
					ed.lastPress, ed.lastMark = e2.Time, i
				}
			case pointer.Drag:
				if ed.drag == dragNone {
					continue
				}
				o := toPhoto(pos)
				moved := pos.Sub(ed.dragFrom)
				if !ed.dragMoved && abs32(moved.X)+abs32(moved.Y) < 2 {
					continue
				}
				first := !ed.dragMoved
				ed.dragMoved = true
				switch ed.drag {
				case dragLine:
					m := &e.marks[ed.dragMark]
					last := tr.Transform(m.pts[len(m.pts)-1])
					if d := pos.Sub(last); d.X*d.X+d.Y*d.Y >= 4 {
						m.pts = append(m.pts, o)
					}
				case dragShape:
					e.marks[ed.dragMark].pts[1] = clampPt(o)
				case dragMove:
					if first {
						e.push()
					}
					from := toPhoto(ed.dragFrom)
					e.marks[ed.dragMark].at = clampPt(ed.dragAt.Add(o.Sub(from)))
				case dragCrop:
					if first {
						e.push()
					}
					e.crop = cropDrag(ed.cropFrom, ed.cropMask, moved.Round(), screenRect(tr, full), gtx.Dp(48), inv).Intersect(full)
				}
			case pointer.Release, pointer.Cancel:
				if ed.drag == dragShape && ed.dragMark < len(e.marks) {
					// A click without a drag draws nothing.
					if r := e.marks[ed.dragMark].pts; r[0] == r[1] {
						e.marks = e.marks[:ed.dragMark]
						e.undo()
					}
				}
				ed.drag = dragNone
			}
		}
	}
}

// cropHit tells which edges of the crop r (on screen) p grabs: a corner,
// an edge, or inside it (all of them, to move it).
func cropHit(r image.Rectangle, p image.Point, slack int) int {
	if !p.In(r.Inset(-slack)) {
		return 0
	}
	m := 0
	if iabs(p.X-r.Min.X) <= slack {
		m |= cropLeft
	} else if iabs(p.X-r.Max.X) <= slack {
		m |= cropRight
	}
	if iabs(p.Y-r.Min.Y) <= slack {
		m |= cropTop
	} else if iabs(p.Y-r.Max.Y) <= slack {
		m |= cropBottom
	}
	if m == 0 {
		m = cropAll
	}
	return m
}

func iabs(n int) int { return max(n, -n) }

// cropDrag moves the edges mask of the crop from (on screen) by d, keeps
// it in bounds and at least minSize, and returns it in photo pixels.
func cropDrag(from image.Rectangle, mask int, d image.Point, bounds image.Rectangle, minSize int, inv f32.Affine2D) image.Rectangle {
	r := from
	if mask == cropAll {
		d.X = min(max(d.X, bounds.Min.X-r.Min.X), bounds.Max.X-r.Max.X)
		d.Y = min(max(d.Y, bounds.Min.Y-r.Min.Y), bounds.Max.Y-r.Max.Y)
		r = r.Add(d)
	} else {
		if mask&cropLeft != 0 {
			r.Min.X = min(max(r.Min.X+d.X, bounds.Min.X), r.Max.X-minSize)
		}
		if mask&cropRight != 0 {
			r.Max.X = max(min(r.Max.X+d.X, bounds.Max.X), r.Min.X+minSize)
		}
		if mask&cropTop != 0 {
			r.Min.Y = min(max(r.Min.Y+d.Y, bounds.Min.Y), r.Max.Y-minSize)
		}
		if mask&cropBottom != 0 {
			r.Max.Y = max(min(r.Max.Y+d.Y, bounds.Max.Y), r.Min.Y+minSize)
		}
	}
	return screenRect(inv, r)
}

// demoEdit makes a sample edit for cmd/screenshot.
func (u *UI) demoEdit(f *attachFile, v *photoView, kind string) {
	ed, e := &u.attach.ed, &f.edit
	w, h := float32(v.orig.X), float32(v.orig.Y)
	switch kind {
	case "crop":
		u.setTool(toolCrop)
		e.crop = image.Rect(int(w*0.1), int(h*0.15), int(w*0.8), int(h*0.9))
		return
	case "filter":
		e.filter = 1
		u.setTool(toolFilter)
		return
	}
	var line, blur []f32.Point
	for i := range 40 {
		t := float32(i) / 39
		line = append(line, f32.Pt(w*(0.12+0.3*t), h*(0.7-0.25*float32(math.Sin(float64(t)*math.Pi)))))
		blur = append(blur, f32.Pt(w*(0.45+0.15*t), h*(0.45+0.1*t)))
	}
	e.marks = append(e.marks,
		mark{kind: markBlur, pts: blur, width: h / 8},
		mark{kind: markLine, pts: line, width: w / 120, col: markColors[4]},
		mark{kind: markRect, pts: []f32.Point{{X: w * 0.55, Y: h * 0.2}, {X: w * 0.85, Y: h * 0.5}}, width: w / 250, col: markColors[2]},
		mark{kind: markText, text: "Beach day!", at: f32.Pt(w*0.5, h*0.12), size: h / 9, col: markColors[0]},
		mark{kind: markEmoji, text: "😎", at: f32.Pt(w*0.7, h*0.75), size: h / 6})
	ed.tool, ed.sel = toolDraw, len(e.marks)-1
}
