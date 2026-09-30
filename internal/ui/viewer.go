package ui

import (
	"image"

	"gioui.org/f32"
	"gioui.org/font"
	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/text"
	"gioui.org/widget"

	"github.com/chomosuke9/wazzapclients/internal/model"
	"github.com/chomosuke9/wazzapclients/internal/ui/icon"
)

// mediaViewer is the full-window photo and video viewer.
type mediaViewer struct {
	open  bool
	msgID string
	zp    zoomPan
	strip widget.List

	anim   tween       // opening and closing
	origin image.Point // where it was opened, which the picture zooms out of
	// What is shown glides to zoom, pan and the current thumbnail.
	stripX follower // the thumbnail strip sliding to the current one

	video videoView // the video being shown, if it is one
}

// viewerItems lists the chat's pictures and videos (KindImage), oldest first.
func (u *UI) viewerItems() []*model.Message {
	var out []*model.Message
	for _, m := range u.msgs {
		if m.Kind == model.KindImage {
			out = append(out, m)
		}
	}
	return out
}

func (u *UI) openViewer(m *model.Message) {
	if u.viewer.anim.v > 0 {
		u.images.forget("v:" + u.viewer.msgID) // still fading out
	}
	u.stopVideo()
	u.viewer = mediaViewer{open: true, msgID: m.ID, origin: u.mouse, video: videoView{muted: u.viewer.video.muted}}
	u.viewer.strip.Axis = layout.Horizontal
	u.closePicker()
	u.requestFocus(nil) // so arrow keys reach the viewer, not the composer
}

// closeViewer fades the viewer out. Its picture is released after; a
// video stops at once.
func (u *UI) closeViewer() {
	u.viewer.open = false
	u.stopVideo()
}

// hideViewer closes the viewer at once.
func (u *UI) hideViewer() {
	if u.viewer.open || u.viewer.anim.v > 0 {
		u.images.forget("v:" + u.viewer.msgID)
	}
	u.viewer.open = false
	u.viewer.anim.snap(false)
	u.stopVideo()
}

// viewerMsg returns the shown message and its index in items.
func (u *UI) viewerMsg(items []*model.Message) (*model.Message, int) {
	for i, m := range items {
		if m.ID == u.viewer.msgID {
			return m, i
		}
	}
	return nil, -1
}

func (u *UI) viewerMenuItems() []menuItem {
	m, _ := u.viewerMsg(u.viewerItems())
	if m == nil {
		return nil
	}
	return []menuItem{
		{key: "save", ic: icDownload, label: "Save as…", run: func() { u.backend.SaveMedia(m) }},
		{key: "goto", ic: icChats, label: "Go to message", run: func() { u.closeViewer(); u.jumpTo(m.ID) }},
		{key: "delete", ic: icDelete, label: "Delete", run: func() { u.closeViewer(); u.confirmDelete([]*model.Message{m}) }},
	}
}

func (u *UI) showViewerAt(items []*model.Message, i int) {
	if i < 0 || i >= len(items) {
		return
	}
	u.images.forget("v:" + u.viewer.msgID)
	u.stopVideo()
	u.viewer.msgID = items[i].ID
	u.viewer.zp.reset()
}

func (u *UI) layoutViewer(gtx C) {
	v := &u.viewer
	if !v.open && v.anim.v == 0 {
		return
	}
	p := u.pal
	items := u.viewerItems()
	m, idx := u.viewerMsg(items)
	if m == nil || u.selected == nil {
		u.hideViewer()
		return
	}
	// Keyboard: arrows switch, Escape closes.
	for v.open {
		ev, ok := gtx.Event(key.Filter{Name: key.NameLeftArrow}, key.Filter{Name: key.NameRightArrow},
			key.Filter{Name: key.NameEscape}, key.Filter{Name: key.NameSpace})
		if !ok {
			break
		}
		if ke, ok := ev.(key.Event); ok && ke.State == key.Press {
			switch ke.Name {
			case key.NameLeftArrow:
				u.showViewerAt(items, idx-1)
			case key.NameRightArrow:
				u.showViewerAt(items, idx+1)
			case key.NameEscape:
				if !u.ctx.isOpen() {
					u.closeViewer()
				}
			case key.NameSpace:
				if isVideo(m) {
					u.toggleVideo(gtx, m)
				}
			}
		}
	}
	type tool struct {
		key string
		ic  *icon.Icon
		off bool
		run func()
	}
	b := u.backend
	tools := []tool{
		{"zoomout", icZoomOut, !v.zp.zoomed(), func() { v.zp.zoomCenter(v.zp.zoom / 1.5) }},
		{"zoomin", icZoomIn, v.zp.zoom >= maxZoom, func() { v.zp.zoomCenter(max(1, v.zp.zoom) * 1.5) }},
		{"goto", icChats, false, func() { u.closeViewer(); u.jumpTo(m.ID) }},
		{"reply", icReply, isChannelID(m.ChatID), func() { u.closeViewer(); u.startReply(m) }},
		{"star", starIcon(m.Starred), false, func() { b.Star(m, !m.Starred) }},
		{"pin", pinIcon(m.Pinned), isChannelID(m.ChatID), func() { b.PinMessage(m, !m.Pinned) }},
		{"react", icEmoji, isChannelID(m.ChatID), func() { u.openPicker(pickReaction, m) }},
		{"forward", icForward, false, func() { u.openForward([]*model.Message{m}) }},
		{"save", icDownload, false, func() { b.SaveMedia(m) }},
		{"menu", icMenu, false, func() { u.ctx = ctxMenu{kind: ctxViewer, at: u.mouse} }},
		{"close", icClose, false, func() { u.closeViewer() }},
	}
	for _, t := range tools {
		if u.btn("vw:"+t.key).Clicked(gtx) && !t.off && v.open {
			t.run()
		}
	}
	if u.btn("vw:play").Clicked(gtx) && v.open && isVideo(m) {
		u.toggleVideo(gtx, m)
	}
	if u.btn("vw:prev").Clicked(gtx) {
		u.showViewerAt(items, idx-1)
	}
	if u.btn("vw:next").Clicked(gtx) {
		u.showViewerAt(items, idx+1)
	}
	for i, it := range items {
		if u.btn("vw:t:" + it.ID).Clicked(gtx) {
			u.showViewerAt(items, i)
		}
	}
	m, idx = u.viewerMsg(items)
	a := v.anim.step(gtx, v.open, durDialog)
	if a == 0 && !v.open {
		u.images.forget("v:" + v.msgID)
		return
	}
	if !v.open {
		var done func()
		gtx, done = fadeOut(gtx)
		defer done()
	} else {
		u.syncVideo(m)
	}

	sz := gtx.Constraints.Max
	// The backdrop and the controls fade and the picture zooms (see
	// layoutViewerImage). Fading everything at once would take an opacity
	// layer the size of the window, and Gio keeps its texture for good.
	e := easeOut(a)
	fillRect(gtx, image.Rectangle{Max: sz}, faded(p.Viewer, e))
	if v.open {
		// Block input to the chat underneath.
		u.btn("vw:bg").Layout(gtx, func(gtx C) D { return D{Size: sz} })
	}

	// Header: who sent it and when.
	name, avatarID, group := u.senderLabel(m)
	top := pushFx(gtx, e, moveBy(0, -float32(gtx.Dp(12))*(1-e)))
	func() {
		t := op.Offset(image.Pt(gtx.Dp(29), 0)).Push(gtx.Ops)
		defer t.Pop()
		hg := gtx
		hg.Constraints = layout.Constraints{Max: image.Pt(sz.X/2, gtx.Dp(62))}
		vcenter(hg, gtx.Dp(62), func(gtx C) D {
			return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(func(gtx C) D { return u.avatar(gtx, avatarID, name, group, 42) }),
				layout.Rigid(layout.Spacer{Width: 17}.Layout),
				layout.Flexed(1, func(gtx C) D {
					return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
						layout.Rigid(u.label(15.5, name, p.Text, labelOpts{weight: font.SemiBold, maxLines: 1}).Layout),
						layout.Rigid(layout.Spacer{Height: 2}.Layout),
						layout.Rigid(u.label(13, statusTime(m.Time, u.now()), p.TextSecondary, labelOpts{maxLines: 1}).Layout),
					)
				}),
			)
		})
	}()
	// Toolbar, right-aligned.
	pitch := gtx.Dp(51)
	x := sz.X - gtx.Dp(40) - pitch/2 - (len(tools)-1)*pitch
	for _, t := range tools {
		t := t
		col := p.IconStrong
		if t.off {
			col = p.EmptyIcon
		}
		off := op.Offset(image.Pt(x, gtx.Dp(31)-gtx.Dp(21))).Push(gtx.Ops)
		cl := u.btn("vw:" + t.key)
		clickable(gtx, cl, func(gtx C) D {
			s := gtx.Dp(42)
			if h := u.hover(gtx, cl); h > 0 && !t.off {
				fillCircle(gtx, image.Pt(s/2, s/2), s/2, faded(p.Hover, h))
			}
			return centerIn(gtx, s, iconW(t.ic, 24, col))
		})
		off.Pop()
		x += pitch
	}
	top.Pop()

	// Thumbnail strip and the divider above it.
	stripTop := sz.Y - gtx.Dp(105)
	fillRect(gtx, image.Rect(0, stripTop, sz.X, stripTop+max(1, gtx.Dp(1))), faded(p.ViewerDivider, e))
	lower := pushFx(gtx, e, moveBy(0, float32(gtx.Dp(12))*(1-e)))
	u.layoutViewerStrip(gtx, items, idx, stripTop+gtx.Dp(10))

	// Caption, then the picture filling what's left.
	bottom := stripTop - gtx.Dp(16)
	if m.Text != "" {
		cap := record(gtx, func(gtx C) D {
			gtx.Constraints = layout.Constraints{Max: image.Pt(sz.X*2/3, gtx.Dp(60))}
			return u.label(15, plainText(stripIsolates(m.Text)), p.Text, labelOpts{maxLines: 2, align: text.Middle}).Layout(gtx)
		})
		cap.at(gtx, (sz.X-cap.size.X)/2, stripTop-gtx.Dp(22)-cap.size.Y)
		bottom = stripTop - gtx.Dp(44) - cap.size.Y
	}
	lower.Pop()
	area := image.Rect(gtx.Dp(140), gtx.Dp(74), sz.X-gtx.Dp(140), bottom)
	if shown := u.layoutViewerImage(gtx, m, area); isVideo(m) && v.open {
		u.layoutVideoControls(gtx, m, shown)
	}

	// Previous and next.
	arrow := func(key string, ic *icon.Icon, cx int, enabled bool) {
		s := gtx.Dp(43)
		t := op.Offset(image.Pt(cx-s/2, (area.Min.Y+area.Max.Y)/2-s/2)).Push(gtx.Ops)
		defer t.Pop()
		defer pushFx(gtx, e, f32.Affine2D{}).Pop()
		cl := u.btn(key)
		col, bg := p.IconStrong, p.ViewerArrow
		if !enabled {
			col, bg = p.EmptyIcon, mix(p.Viewer, p.ViewerArrow, 0.5)
		}
		clickable(gtx, cl, func(gtx C) D {
			if enabled {
				bg = mix(bg, p.Hover, u.hover(gtx, cl))
			}
			fillCircle(gtx, image.Pt(s/2, s/2), s/2, bg)
			return centerIn(gtx, s, iconW(ic, 28, col))
		})
	}
	arrow("vw:prev", icChevronLeft, gtx.Dp(79), idx > 0)
	arrow("vw:next", icChevronRight, sz.X-gtx.Dp(79), idx < len(items)-1)
}

func starIcon(on bool) *icon.Icon {
	if on {
		return icStarFill
	}
	return icStar
}

func pinIcon(on bool) *icon.Icon {
	if on {
		return icPinFill
	}
	return icPin
}

// senderLabel names a message's author for headers: "You", the group
// member, or the contact.
func (u *UI) senderLabel(m *model.Message) (name, id string, group bool) {
	c := u.selected
	switch {
	case m.FromMe:
		return "You", u.meID, false
	case c != nil && c.IsGroup && m.Sender != "":
		return plainText(m.Sender), m.SenderID, false
	case c != nil:
		return c.Name, c.ID, c.IsGroup
	}
	return "", "", false
}

// layoutViewerImage draws the full picture (or the video) fitted into area,
// zoomed and panned (see zoomPan), and returns where it shows. Clicking a
// video plays or pauses it.
func (u *UI) layoutViewerImage(gtx C, m *model.Message, area image.Rectangle) image.Rectangle {
	v := &u.viewer
	if area.Dx() <= 0 || area.Dy() <= 0 {
		return image.Rectangle{}
	}
	clicked, moved := v.zp.update(gtx)
	if moved {
		v.video.lastMove = gtx.Now
	}
	if clicked && isVideo(m) {
		u.toggleVideo(gtx, m)
	}

	maxSide := max(area.Dx(), area.Dy()) * 2
	var img *imgEntry
	if isVideo(m) {
		img = u.videoFrame(area.Size())
	} else if m.Media == model.MediaImage {
		b := u.backend
		chat, id := m.ChatID, m.ID
		if e := u.images.get("v:"+id, maxSide, func() []byte { return b.MediaData(chat, id) }); e.state == imgReady {
			img = e
		}
	}
	if img == nil {
		img = u.messageImage(m, gtx.Dp(330))
	}
	var dst image.Rectangle
	ready := img != nil && img.state == imgReady
	if ready {
		dst = v.zp.layout(gtx, area, img.size)
	} else {
		v.zp.layout(gtx, area, image.Point{})
		w := min(area.Dx(), area.Dy()*3/4)
		dst = image.Rect(0, 0, w, w*4/3).Add(area.Min.Add(image.Pt((area.Dx()-w)/2, (area.Dy()-w*4/3)/2)))
	}
	// Opening grows the picture out of where it was clicked, from about the
	// size of a bubble's picture; closing shrinks it back.
	if e := easeOut(v.anim.v); e < 1 && dst.Dx() > 0 {
		c1 := pointF(dst.Min.Add(dst.Size().Div(2)))
		c := pointF(v.origin).Add(c1.Sub(pointF(v.origin)).Mul(e))
		s := lerp(min(1, float32(gtx.Dp(330))/float32(dst.Dx())), 1, e)
		defer pushFx(gtx, 1, f32.AffineId().Scale(c1, f32.Pt(s, s)).Offset(c.Sub(c1))).Pop()
	}
	defer clip.Rect(area).Push(gtx.Ops).Pop()
	if ready {
		func() {
			defer clip.Rect(dst.Intersect(area)).Push(gtx.Ops).Pop()
			s := f32.Pt(float32(dst.Dx())/float32(img.size.X), float32(dst.Dy())/float32(img.size.Y))
			defer op.Affine(f32.AffineId().Scale(f32.Point{}, s).Offset(pointF(dst.Min))).Push(gtx.Ops).Pop()
			io := img.op
			io.Filter = paint.FilterLinear
			io.Add(gtx.Ops)
			paint.PaintOp{}.Add(gtx.Ops)
		}()
	} else {
		pic := *m
		pic.Media = model.MediaImage // without the bubble's play button; the viewer draws its own
		u.layoutImage(gtx, dst, &pic, nil)
	}
	if isVideo(m) {
		u.layoutVideoCenter(gtx, m, dst.Min.Add(dst.Size().Div(2)))
	}
	return dst.Intersect(area)
}

// layoutViewerStrip draws the row of thumbnails, centered on the current one.
func (u *UI) layoutViewerStrip(gtx C, items []*model.Message, cur, y int) {
	p := u.pal
	sz := gtx.Constraints.Max
	cell := gtx.Dp(74)
	gap := gtx.Dp(12)
	pitch := cell + gap
	// The strip slides to keep the current picture in the middle.
	x0 := int(u.viewer.stripX.step(gtx, float32(sz.X/2-cell/2-cur*pitch), durSlide))
	first := max(0, (-x0)/pitch-1)
	for i := first; i < len(items); i++ {
		x := x0 + i*pitch
		if x > sz.X {
			break
		}
		m := items[i]
		r := image.Rect(x, y, x+cell, y+cell)
		t := op.Offset(r.Min).Push(gtx.Ops)
		cg := gtx
		cg.Constraints = layout.Exact(r.Size())
		clickable(cg, u.btn("vw:t:"+m.ID), func(gtx C) D {
			rad := gtx.Dp(6)
			fillRRect(gtx, image.Rectangle{Max: r.Size()}, rad, p.ViewerThumb)
			inner := image.Rectangle{Max: r.Size()}.Inset(gtx.Dp(5))
			if i == cur {
				fillRRect(gtx, image.Rectangle{Max: r.Size()}, rad, p.Green)
				fillRRect(gtx, image.Rectangle{Max: r.Size()}.Inset(gtx.Dp(3)), rad, p.ViewerThumb)
			}
			func() {
				defer clip.UniformRRect(inner, gtx.Dp(3)).Push(gtx.Ops).Pop()
				img := u.messageImage(m, cell)
				switch {
				case img != nil && img.state == imgReady:
					paintCover(gtx, img.op, img.size, inner)
				case m.ImageA != 0 || m.ImageB != 0:
					u.gradientImage(gtx, inner, m.ImageA, m.ImageB)
				default:
					fillRect(gtx, inner, p.Hover)
				}
			}()
			return D{Size: r.Size()}
		})
		t.Pop()
	}
}

// pointF converts to float coordinates.
func pointF(p image.Point) f32.Point { return f32.Pt(float32(p.X), float32(p.Y)) }
