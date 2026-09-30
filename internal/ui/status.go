package ui

import (
	"image"
	"image/color"
	"math"
	"time"

	"gioui.org/f32"
	"gioui.org/font"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/text"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

type statusState struct {
	menu, add widget.Clickable
	list      widget.List
	viewer    statusViewer
}

// statusTime formats a status timestamp: "Today at 06:45", "Yesterday at
// 16:55", or a date.
func statusTime(t, now time.Time) string {
	switch {
	case sameDay(t, now):
		return "Today at " + t.Format("15:04")
	case sameDay(t, now.AddDate(0, 0, -1)):
		return "Yesterday at " + t.Format("15:04")
	}
	return t.Format("02/01/2006") + " at " + t.Format("15:04")
}

// layoutStatusList draws the Status page: your own status, then recent
// (unseen) and viewed updates from contacts.
func (u *UI) layoutStatusList(gtx C) D {
	p := u.pal
	var mine *model.StatusThread
	var recent, viewed []*model.StatusThread
	for _, t := range u.statuses {
		switch {
		case t.Mine:
			mine = t
		case t.Viewed():
			viewed = append(viewed, t)
		default:
			recent = append(recent, t)
		}
	}
	type entry struct {
		label  string
		thread *model.StatusThread
		first  bool // first label follows "My status" and sits a bit lower
	}
	var entries []entry
	if len(recent) > 0 {
		entries = append(entries, entry{label: "Recent", first: true})
		for _, t := range recent {
			entries = append(entries, entry{thread: t})
		}
	}
	if len(viewed) > 0 {
		entries = append(entries, entry{label: "Viewed", first: len(recent) == 0})
		for _, t := range viewed {
			entries = append(entries, entry{thread: t})
		}
	}
	now := u.now()
	if u.status.add.Clicked(gtx) && mine != nil {
		u.status.viewer.show(mine)
	}

	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx C) D {
			return u.pageHeader(gtx, "Status",
				layout.Rigid(func(gtx C) D { return u.iconButton(gtx, &u.status.menu, icMenu, 40, 25, p.IconStrong) }),
				layout.Rigid(layout.Spacer{Width: 8}.Layout),
				u.headerButton(&u.status.add, icAddCircle, 27),
			)
		}),
		layout.Flexed(1, func(gtx C) D {
			return u.scrollList(gtx, &u.status.list, len(entries)+1, func(gtx C, i int) D {
				if i == 0 {
					sub := "Click to add status update"
					if mine != nil {
						sub = statusTime(mine.Last().Time, now)
					}
					return layout.Inset{Bottom: 9.5}.Layout(gtx, func(gtx C) D {
						return u.statusRow(gtx, "status:me", mine, "My status", sub, 72, true)
					})
				}
				e := entries[i-1]
				if e.label != "" {
					top := unit.Dp(31.5)
					return u.sectionLabel(gtx, e.label, layout.Inset{Left: 27, Top: top, Bottom: 22}, labelOpts{maxLines: 1})
				}
				t := e.thread
				return u.statusRow(gtx, "status:"+t.ID, t, t.Name, statusTime(t.Last().Time, now), 76, false)
			})
		}),
	)
}

// statusRow is one poster: the ringed preview, name and time.
func (u *UI) statusRow(gtx C, key string, t *model.StatusThread, title, sub string, height unit.Dp, mine bool) D {
	p := u.pal
	c := u.btn(key)
	if c.Clicked(gtx) && t != nil {
		u.status.viewer.show(t)
		gtx.Execute(op.InvalidateCmd{})
	}
	return layout.Inset{Left: 8, Right: 18}.Layout(gtx, func(gtx C) D {
		return clickable(gtx, c, func(gtx C) D {
			gtx.Constraints.Min.X = gtx.Constraints.Max.X
			bg := mix(p.Panel, p.Hover, u.hover(gtx, c))
			return background(gtx, bg, 10, func(gtx C) D {
				return vcenter(gtx, gtx.Dp(height), func(gtx C) D {
					return layout.Inset{Left: 11}.Layout(gtx, func(gtx C) D {
						return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
							layout.Rigid(func(gtx C) D { return u.statusAvatar(gtx, t, 51, mine) }),
							layout.Rigid(layout.Spacer{Width: 11}.Layout),
							layout.Flexed(1, func(gtx C) D {
								return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
									layout.Rigid(u.label(17, title, p.Text).Layout),
									layout.Rigid(layout.Spacer{Height: 1}.Layout),
									layout.Rigid(u.label(15.2, sub, p.TextSecondary).Layout),
								)
							}),
						)
					})
				})
			})
		})
	})
}

// statusAvatar draws the segmented ring (one arc per update, gray once
// seen) around the newest update's preview. Your own row gets a "+" badge.
func (u *UI) statusAvatar(gtx C, t *model.StatusThread, size unit.Dp, mine bool) D {
	p := u.pal
	px := gtx.Dp(size)
	stroke := float32(gtx.Dp(1.7))
	inner := px - 2*gtx.Dp(4.5)
	off := (px - inner) / 2
	if t == nil {
		// No status yet: just your profile picture.
		t := op.Offset(image.Pt(off, off)).Push(gtx.Ops)
		u.avatar(gtx, u.meID, u.meName(), false, dp(gtx, inner))
		t.Pop()
	} else {
		n := len(t.Updates)
		gap := float32(0)
		if n > 1 {
			gap = 0.16
		}
		sweep := (2*math.Pi - float32(n)*gap) / float32(n)
		c := f32.Pt(float32(px)/2, float32(px)/2)
		r := float32(px)/2 - stroke/2
		// Oldest first, counter-clockwise from the top, like WhatsApp.
		for i, up := range t.Updates {
			col := p.Green
			if up.Viewed {
				col = p.RingViewed
			}
			start := -math.Pi/2 - gap/2 - float32(i)*(sweep+gap) - sweep
			strokeArc(gtx, c, r, start, sweep, stroke, col)
		}
		tr := op.Offset(image.Pt(off, off)).Push(gtx.Ops)
		u.statusPreview(gtx, t, t.Last(), inner)
		tr.Pop()
	}
	if mine {
		// Green "+" badge with a ring of the background color.
		br := gtx.Dp(8.5)
		ring := gtx.Dp(1.5)
		cx, cy := px/2+gtx.Dp(16), px/2+gtx.Dp(15)
		fillCircle(gtx, image.Pt(cx, cy), br+ring, p.Panel)
		fillCircle(gtx, image.Pt(cx, cy), br, p.Green)
		is := gtx.Dp(15)
		t := op.Offset(image.Pt(cx-is/2, cy-is/2)).Push(gtx.Ops)
		drawIcon(gtx, icAdd, 15, p.Panel)
		t.Pop()
	}
	return D{Size: image.Pt(px, px)}
}

// statusPreview fills a circle of px with an update's picture, or its
// background color for text statuses.
func (u *UI) statusPreview(gtx C, t *model.StatusThread, up *model.StatusUpdate, px int) {
	r := image.Rect(0, 0, px, px)
	if len(up.Thumb) > 0 {
		th := up.Thumb
		if e := u.images.get("st:"+up.ID, px, func() []byte { return th }); e.state == imgReady {
			defer clip.Ellipse(r).Push(gtx.Ops).Pop()
			paintCover(gtx, e.op, e.size, r)
			return
		}
	}
	if up.Media == model.MediaNone && up.Background != 0 {
		fillCircle(gtx, image.Pt(px/2, px/2), px/2, argbColor(up.Background))
		return
	}
	id, name := t.ID, t.Name
	if t.Mine {
		id, name = u.meID, u.meName()
	}
	u.avatar(gtx, id, name, false, dp(gtx, px))
}

func argbColor(c uint32) color.NRGBA {
	return color.NRGBA{A: 0xff, R: uint8(c >> 16), G: uint8(c >> 8), B: uint8(c)}
}

// statusViewer shows one poster's updates full-window, one after another.
type statusViewer struct {
	thread   *model.StatusThread
	index    int
	shownAt  time.Time
	closeBtn widget.Clickable
	prev     widget.Clickable
	next     widget.Clickable
	closing  bool // fading out
	anim     tween
}

// statusDuration is how long each update stays on screen.
const statusDuration = 6 * time.Second

func (v *statusViewer) show(t *model.StatusThread) {
	v.thread, v.closing = t, false
	v.index = 0
	// Start at the first unseen update, like WhatsApp.
	for i, up := range t.Updates {
		if !up.Viewed {
			v.index = i
			break
		}
	}
	v.shownAt = time.Time{}
}

// close fades the viewer out.
func (v *statusViewer) close() {
	if v.thread != nil {
		v.closing = true
	}
}

// isOpen reports whether the viewer is open and not fading out.
func (v *statusViewer) isOpen() bool { return v.thread != nil && !v.closing }

func (u *UI) layoutStatusViewer(gtx C) {
	v := &u.status.viewer
	t := v.thread
	if t == nil {
		return
	}
	p := u.pal
	advance := func(d int) {
		v.index += d
		v.shownAt = time.Time{}
		if v.index < 0 {
			v.index = 0
		}
		if v.index >= len(t.Updates) {
			v.index = len(t.Updates) - 1
			v.close()
		}
	}
	if v.isOpen() {
		if v.closeBtn.Clicked(gtx) {
			v.close()
		}
		if v.next.Clicked(gtx) {
			advance(1)
		}
		if v.prev.Clicked(gtx) {
			advance(-1)
		}
	}
	a := v.anim.step(gtx, v.isOpen(), durDialog)
	if a == 0 && v.closing {
		v.thread, v.closing = nil, false
		return
	}
	now := gtx.Now
	if now.IsZero() {
		now = time.Now()
	}
	if v.isOpen() && v.shownAt.IsZero() {
		v.shownAt = now
		if up := t.Updates[v.index]; !up.Viewed && !t.Mine {
			up.Viewed = true
			u.backend.ViewStatus(t.ID, up.ID)
		}
	}
	elapsed := now.Sub(v.shownAt)
	if v.isOpen() {
		if elapsed >= statusDuration {
			advance(1)
			gtx.Execute(op.InvalidateCmd{})
			return
		}
		gtx.Execute(op.InvalidateCmd{At: now.Add(50 * time.Millisecond)})
	} else {
		gtx = gtx.Disabled() // fading out: no timer, clicks go through
		elapsed = min(max(elapsed, 0), statusDuration)
	}
	up := t.Updates[v.index]

	sz := gtx.Constraints.Max
	// The backdrop fades, the update zooms out of the middle and the
	// controls fade: a fade of the whole window would need an opacity layer
	// that size, and Gio keeps its texture for good.
	e := easeOut(a)
	fillRect(gtx, image.Rectangle{Max: sz}, faded(p.StatusBg, e))
	// Clicks on the left third go back, anywhere else forward.
	if v.isOpen() {
		third := sz.X / 3
		pg := gtx
		pg.Constraints = layout.Exact(image.Pt(third, sz.Y))
		v.prev.Layout(pg, func(gtx C) D { return D{Size: gtx.Constraints.Max} })
		t := op.Offset(image.Pt(third, 0)).Push(gtx.Ops)
		ng := gtx
		ng.Constraints = layout.Exact(image.Pt(sz.X-third, sz.Y))
		v.next.Layout(ng, func(gtx C) D { return D{Size: gtx.Constraints.Max} })
		t.Pop()
	}

	// The update itself, centered in a portrait frame.
	frameH := sz.Y - gtx.Dp(120)
	frameW := min(sz.X-gtx.Dp(40), frameH*9/16)
	frame := image.Rect((sz.X-frameW)/2, gtx.Dp(92), (sz.X+frameW)/2, gtx.Dp(92)+frameH)
	zoom := pushFx(gtx, 1, scaleAt(frame.Min.Add(frame.Size().Div(2)), lerp(0.3, 1, e)))
	u.layoutStatusContent(gtx, t, up, frame)
	zoom.Pop()
	defer pushFx(gtx, e, f32.Affine2D{}).Pop()

	// Progress segments across the top of the frame.
	n := len(t.Updates)
	gap := gtx.Dp(4)
	segW := (frame.Dx() - (n-1)*gap) / n
	y := gtx.Dp(20)
	h := max(2, gtx.Dp(3))
	for i := 0; i < n; i++ {
		x := frame.Min.X + i*(segW+gap)
		r := image.Rect(x, y, x+segW, y+h)
		fillRRect(gtx, r, h/2, argb(0xffffff, 0x60))
		done := 0.0
		switch {
		case i < v.index:
			done = 1
		case i == v.index:
			done = float64(elapsed) / float64(statusDuration)
		}
		if done > 0 {
			fillRRect(gtx, image.Rect(x, y, x+int(float64(segW)*done), y+h), h/2, rgb(0xffffff))
		}
	}

	// Poster and time, and the close button.
	name := t.Name
	id := t.ID
	if t.Mine {
		name, id = "My status", u.meID
	}
	hdr := op.Offset(image.Pt(frame.Min.X, y+h+gtx.Dp(14))).Push(gtx.Ops)
	hg := gtx
	hg.Constraints = layout.Constraints{Max: image.Pt(frame.Dx(), gtx.Dp(48))}
	layout.Flex{Alignment: layout.Middle}.Layout(hg,
		layout.Rigid(func(gtx C) D { return u.avatar(gtx, id, name, false, 40) }),
		layout.Rigid(layout.Spacer{Width: 12}.Layout),
		layout.Flexed(1, func(gtx C) D {
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
				layout.Rigid(u.label(16, name, rgb(0xffffff), labelOpts{weight: font.Medium, maxLines: 1}).Layout),
				layout.Rigid(u.label(13.5, statusTime(up.Time, u.now()), argb(0xffffff, 0xb0)).Layout),
			)
		}),
	)
	hdr.Pop()
	cb := op.Offset(image.Pt(sz.X-gtx.Dp(64), gtx.Dp(16))).Push(gtx.Ops)
	u.iconButton(gtx, &v.closeBtn, icClose, 48, 30, rgb(0xffffff))
	cb.Pop()
}

// layoutStatusContent draws an update inside r: its picture (full size once
// downloaded) or its text on the chosen background.
func (u *UI) layoutStatusContent(gtx C, t *model.StatusThread, up *model.StatusUpdate, r image.Rectangle) {
	defer clip.UniformRRect(r, gtx.Dp(12)).Push(gtx.Ops).Pop()
	if up.Media == model.MediaNone {
		bg := argbColor(up.Background)
		if up.Background == 0 {
			bg = rgb(0x4a5a62)
		}
		fillRect(gtx, r, bg)
		tg := gtx
		tg.Constraints = layout.Exact(r.Size())
		tr := op.Offset(r.Min).Push(gtx.Ops)
		layout.UniformInset(32).Layout(tg, func(gtx C) D {
			return layout.Center.Layout(gtx, func(gtx C) D {
				l := u.label(28, up.Text, rgb(0xffffff), labelOpts{align: text.Middle})
				l.MaxLines = 0
				return l.Layout(gtx)
			})
		})
		tr.Pop()
		return
	}
	fillRect(gtx, r, rgb(0x000000))
	var img *imgEntry
	if up.Media == model.MediaImage {
		b := u.backend
		id := up.ID
		if e := u.images.get("sm:"+id, max(r.Dx(), r.Dy()), func() []byte { return b.MediaData(statusChatID, id) }); e.state == imgReady {
			img = e
		}
	}
	if img == nil && len(up.Thumb) > 0 {
		th := up.Thumb
		if e := u.images.get("st:"+up.ID, 160, func() []byte { return th }); e.state == imgReady {
			img = e
		}
	}
	if img != nil {
		// Fit the whole picture inside the frame.
		s := min(float32(r.Dx())/float32(img.size.X), float32(r.Dy())/float32(img.size.Y))
		w, h := int(float32(img.size.X)*s), int(float32(img.size.Y)*s)
		min := r.Min.Add(image.Pt((r.Dx()-w)/2, (r.Dy()-h)/2))
		dst := image.Rectangle{Min: min, Max: min.Add(image.Pt(w, h))}
		paintCover(gtx, img.op, img.size, dst)
	}
	if up.Text != "" {
		cap := record(gtx, func(gtx C) D {
			gtx.Constraints = layout.Constraints{Max: image.Pt(r.Dx(), r.Dy())}
			return background(gtx, argb(0x000000, 0x80), 0, func(gtx C) D {
				gtx.Constraints.Min.X = r.Dx()
				return layout.UniformInset(14).Layout(gtx, func(gtx C) D {
					l := u.label(16, up.Text, rgb(0xffffff), labelOpts{align: text.Middle})
					l.MaxLines = 4
					return l.Layout(gtx)
				})
			})
		})
		cap.at(gtx, r.Min.X, r.Max.Y-cap.size.Y)
	}
}

// statusChatID is the chat ID under which status media is downloaded.
const statusChatID = "status@broadcast"
