package ui

import (
	"image"
	"image/color"
	"math"
	"time"

	"gioui.org/f32"
	"gioui.org/font"
	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/text"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

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
	pauseBtn widget.Clickable
	muteBtn  widget.Clickable
	closing  bool // fading out
	anim     tween
	zp       zoomPan       // pictures zoom like in the media viewer
	held     time.Duration // time shown when the timer paused
	frac     float32       // how much of the current update has played
	paused   bool          // paused with the pause button
	video    videoView     // the video update playing
	vidHeld  bool          // the video is paused while the timer waits
	reply    widget.Editor
	send     widget.Clickable
}

// statusDuration is how long a picture or text update stays on screen. A
// video stays until it ends.
const statusDuration = 6 * time.Second

func (v *statusViewer) show(t *model.StatusThread) {
	v.video.stop()
	v.thread, v.closing = t, false
	v.index = 0
	// Start at the first unseen update, like WhatsApp.
	for i, up := range t.Updates {
		if !up.Viewed {
			v.index = i
			break
		}
	}
	v.shownAt, v.zp, v.frac, v.paused = time.Time{}, zoomPan{}, 0, false
	v.reply.SingleLine, v.reply.Submit = true, true
	v.reply.SetText("")
}

// close fades the viewer out. A video stops at once.
func (v *statusViewer) close() {
	if v.thread != nil {
		v.closing = true
	}
	v.video.stop()
}

// isOpen reports whether the viewer is open and not fading out.
func (v *statusViewer) isOpen() bool { return v.thread != nil && !v.closing }

// replying reports whether a reply is being typed, which pauses the update.
func (v *statusViewer) replying(gtx C) bool {
	return gtx.Focused(&v.reply) || v.reply.Len() > 0
}

// statusMsg is a status update as a message: how its video downloads, and
// what a reply to it quotes.
func statusMsg(t *model.StatusThread, up *model.StatusUpdate) *model.Message {
	return &model.Message{ID: up.ID, ChatID: statusChatID, Kind: model.KindImage, Media: up.Media,
		Text: up.Text, Thumb: up.Thumb, Time: up.Time, FromMe: t.Mine, Sender: t.Name, SenderID: t.ID}
}

// syncStatusVideo starts the update's video, and stops the last one when
// the viewer moves on.
func (u *UI) syncStatusVideo(t *model.StatusThread, up *model.StatusUpdate) {
	v := &u.status.viewer
	vv := &v.video
	m := statusMsg(t, up)
	if !isVideo(m) {
		if vv.msgID != "" {
			vv.stop()
		}
		return
	}
	if vv.msgID != m.ID {
		vv.stop()
		vv.msgID, v.vidHeld = m.ID, false
		u.loadVideo(vv, m)
	}
}

// statusVideoDownloaded starts the status viewer's video once it has
// downloaded. If it couldn't, the update shows its preview for the usual
// time instead.
func (u *UI) statusVideoDownloaded(e model.MediaEvent) {
	v := &u.status.viewer
	vv := &v.video
	if e.ChatID != statusChatID || !vv.loading || vv.msgID != e.MsgID || !v.isOpen() {
		return
	}
	vv.loading = false
	if e.Failed {
		v.shownAt = time.Time{}
		return
	}
	if up := v.thread.Updates[v.index]; up.ID == e.MsgID {
		u.loadVideo(vv, statusMsg(v.thread, up))
	}
}

// sendStatusReply sends the typed reply to the poster, quoting the update.
func (u *UI) sendStatusReply() {
	v := &u.status.viewer
	t := v.thread
	txt := trimSpace(v.reply.Text())
	if !v.isOpen() || t.Mine || txt == "" {
		return
	}
	m := u.backend.Send(t.ID, model.Draft{Text: txt, Reply: statusMsg(t, t.Updates[v.index])})
	v.reply.SetText("")
	u.requestFocus(nil)
	if m != nil {
		u.upsertMessage(m)
		u.toast("Reply sent")
	}
}

// statusTick runs the update's timer: a video's own position, or the time
// shown otherwise. While hold is set it waits, and so does the video. It
// reports whether the update is over.
func (u *UI) statusTick(gtx C, now time.Time, hold bool) bool {
	v := &u.status.viewer
	vv := &v.video
	if vv.player != nil && vv.player.Status().Err != nil {
		vv.player.Close()
		vv.player, vv.external = nil, true
		v.shownAt = now
		u.toast("This video can't play here. Press play to open it in your video player.")
	}
	if vv.player != nil || vv.loading {
		// The video keeps the time; the timer starts over if it can't play.
		v.shownAt, v.held = now, 0
		if vv.loading {
			v.frac = 0
			return false
		}
		if hold != v.vidHeld {
			if hold {
				vv.player.Pause()
			} else {
				vv.player.Play()
			}
			v.vidHeld = hold
		}
		st := vv.player.Status()
		if st.Ended && !hold {
			return true
		}
		if st.Dur > 0 {
			v.frac = min(1, max(0, float32(st.Pos)/float32(st.Dur)))
		}
		if !hold {
			gtx.Execute(op.InvalidateCmd{At: now.Add(50 * time.Millisecond)})
		}
		return false
	}
	elapsed := now.Sub(v.shownAt)
	if hold {
		elapsed, v.shownAt = v.held, now.Add(-v.held)
	} else {
		v.held = elapsed
	}
	if elapsed >= statusDuration {
		return true
	}
	v.frac = float32(elapsed) / float32(statusDuration)
	if !hold {
		gtx.Execute(op.InvalidateCmd{At: now.Add(50 * time.Millisecond)})
	}
	return false
}

func (u *UI) layoutStatusViewer(gtx C) {
	v := &u.status.viewer
	t := v.thread
	if t == nil {
		return
	}
	p := u.pal
	advance := func(d int) {
		v.index += d
		v.shownAt, v.zp, v.frac = time.Time{}, zoomPan{}, 0
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
		if v.pauseBtn.Clicked(gtx) {
			v.paused = !v.paused
		}
		if v.muteBtn.Clicked(gtx) {
			v.video.muted = !v.video.muted
			if v.video.player != nil {
				v.video.player.SetMuted(v.video.muted)
			}
		}
		for {
			ev, ok := v.reply.Update(gtx)
			if !ok {
				break
			}
			if _, ok := ev.(widget.SubmitEvent); ok {
				u.sendStatusReply()
			}
		}
		if v.send.Clicked(gtx) {
			u.sendStatusReply()
		}
		// A click on the picture goes back or forward like the rest of
		// the window, unless it's zoomed in.
		if clicked, _ := v.zp.update(gtx); clicked && !v.zp.zoomed() {
			if v.zp.drag.start.X < float32(gtx.Constraints.Max.X/3) {
				advance(-1)
			} else {
				advance(1)
			}
		}
	}
	a := v.anim.step(gtx, v.isOpen(), durDialog)
	if a == 0 && v.closing {
		v.thread, v.closing = nil, false
		v.reply.SetText("")
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
	up := t.Updates[v.index]
	if v.isOpen() {
		u.syncStatusVideo(t, up)
		hold := v.paused || v.zp.zoomed() || v.replying(gtx)
		if u.statusTick(gtx, now, hold) {
			advance(1)
			gtx.Execute(op.InvalidateCmd{})
			return
		}
	} else {
		var done func()
		gtx, done = fadeOut(gtx) // no timer either
		defer done()
	}

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

	// The update itself, centered in a portrait frame, with the reply box
	// under it for someone else's status.
	replyH := 0
	if !t.Mine {
		replyH = gtx.Dp(64)
	}
	frameH := sz.Y - gtx.Dp(120) - replyH
	frameW := min(sz.X-gtx.Dp(40), frameH*9/16)
	frame := image.Rect((sz.X-frameW)/2, gtx.Dp(92), (sz.X+frameW)/2, gtx.Dp(92)+frameH)
	zoom := pushFx(gtx, 1, scaleAt(frame.Min.Add(frame.Size().Div(2)), lerp(0.3, 1, e)))
	u.layoutStatusContent(gtx, t, up, frame, &v.zp)
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
		done := float32(0)
		switch {
		case i < v.index:
			done = 1
		case i == v.index:
			done = v.frac
		}
		if done > 0 {
			fillRRect(gtx, image.Rect(x, y, x+int(float32(segW)*done), y+h), h/2, rgb(0xffffff))
		}
	}

	// Poster and time, pause and mute, and the close button.
	name := t.Name
	id := t.ID
	if t.Mine {
		name, id = "My status", u.meID
	}
	white := rgb(0xffffff)
	hdr := op.Offset(image.Pt(frame.Min.X, y+h+gtx.Dp(14))).Push(gtx.Ops)
	hg := gtx
	hg.Constraints = layout.Constraints{Max: image.Pt(frame.Dx(), gtx.Dp(48))}
	layout.Flex{Alignment: layout.Middle}.Layout(hg,
		layout.Rigid(func(gtx C) D { return u.avatar(gtx, id, name, false, 40) }),
		layout.Rigid(layout.Spacer{Width: 12}.Layout),
		layout.Flexed(1, func(gtx C) D {
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
				layout.Rigid(u.label(16, name, white, labelOpts{weight: font.Medium, maxLines: 1}).Layout),
				layout.Rigid(u.label(13.5, statusTime(up.Time, u.now()), argb(0xffffff, 0xb0)).Layout),
			)
		}),
		layout.Rigid(func(gtx C) D {
			ic := icPauseFill
			if v.paused {
				ic = icPlayFill
			}
			return u.iconButton(gtx, &v.pauseBtn, ic, 40, 24, white)
		}),
		layout.Rigid(func(gtx C) D {
			if v.video.player == nil {
				return D{}
			}
			ic := icVolumeFill
			if v.video.muted {
				ic = icVolumeOffFill
			}
			return u.iconButton(gtx, &v.muteBtn, ic, 40, 22, white)
		}),
	)
	hdr.Pop()
	cb := op.Offset(image.Pt(sz.X-gtx.Dp(64), gtx.Dp(16))).Push(gtx.Ops)
	u.iconButton(gtx, &v.closeBtn, icClose, 48, 30, white)
	cb.Pop()

	if !t.Mine {
		u.layoutStatusReply(gtx, image.Rect(frame.Min.X, frame.Max.Y+gtx.Dp(14), frame.Max.X, frame.Max.Y+gtx.Dp(14)+gtx.Dp(48)))
	}
}

// layoutStatusReply draws the "Type a reply…" box in r, with a send
// button once there is text.
func (u *UI) layoutStatusReply(gtx C, r image.Rectangle) {
	v := &u.status.viewer
	p := u.pal
	fillRRect(gtx, r, r.Dy()/2, mix(p.StatusBg, rgb(0xffffff), 0.14))
	defer op.Offset(r.Min).Push(gtx.Ops).Pop()
	gtx.Constraints = layout.Exact(r.Size())
	// A click on the box types, rather than going to the next update.
	box := u.btn("st:replybox")
	if box.Clicked(gtx) {
		gtx.Execute(key.FocusCmd{Tag: &v.reply})
	}
	box.Layout(gtx, func(gtx C) D { return D{Size: gtx.Constraints.Max} })
	typed := trimSpace(v.reply.Text()) != ""
	layout.Inset{Left: 20, Right: 4}.Layout(gtx, func(gtx C) D {
		return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
			layout.Flexed(1, func(gtx C) D {
				return vcenter(gtx, gtx.Constraints.Max.Y, func(gtx C) D {
					gtx.Constraints.Min.X = gtx.Constraints.Max.X
					e := material.Editor(u.th, &v.reply, "Type a reply…")
					e.TextSize = 16
					e.Color = rgb(0xffffff)
					e.HintColor = argb(0xffffff, 0x99)
					e.SelectionColor = argb(0x53bdeb, 0x60)
					return e.Layout(gtx)
				})
			}),
			layout.Rigid(func(gtx C) D {
				sz := gtx.Dp(40)
				if !typed {
					return D{Size: image.Pt(sz, sz)}
				}
				return clickable(gtx, &v.send, func(gtx C) D {
					fillCircle(gtx, image.Pt(sz/2, sz/2), sz/2, p.Green)
					return centerIn(gtx, sz, iconW(icSend, 21, p.OnGreen))
				})
			}),
		)
	})
}

// layoutStatusContent draws an update inside r: its picture (full size once
// downloaded, zoomed and panned by zp), its video, or its text on the
// chosen background.
func (u *UI) layoutStatusContent(gtx C, t *model.StatusThread, up *model.StatusUpdate, r image.Rectangle, zp *zoomPan) {
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
	vid := isVideo(statusMsg(t, up))
	vv := &u.status.viewer.video
	switch {
	case up.Media == model.MediaImage:
		b := u.backend
		id := up.ID
		if e := u.images.get("sm:"+id, max(r.Dx(), r.Dy()), func() []byte { return b.MediaData(statusChatID, id) }); e.state == imgReady {
			img = e
		}
	case vid && vv.msgID == up.ID:
		img = u.videoFrame(vv, r.Size())
	}
	if img == nil && len(up.Thumb) > 0 {
		th := up.Thumb
		if e := u.images.get("st:"+up.ID, 160, func() []byte { return th }); e.state == imgReady {
			img = e
		}
	}
	if img != nil {
		// The whole picture fits inside the frame at zoom 1.
		paintCover(gtx, img.op, img.size, zp.layout(gtx, r, img.size))
	}
	if vid && u.status.viewer.isOpen() {
		u.layoutStatusVideoCenter(gtx, t, up, r.Min.Add(r.Size().Div(2)))
	}
	if up.Text != "" && !zp.zoomed() {
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

// layoutStatusVideoCenter draws a spinner while a video status downloads
// or starts. A video this system can't play, or that failed to download,
// gets a play button that opens it in the system's player or tries again.
func (u *UI) layoutStatusVideoCenter(gtx C, t *model.StatusThread, up *model.StatusUpdate, c image.Point) {
	vv := &u.status.viewer.video
	rad := gtx.Dp(34)
	switch {
	case vv.loading || vv.player != nil && vv.size == (image.Point{}):
		s := gtx.Dp(44)
		fillCircle(gtx, c, rad, argb(0x000000, 0x90))
		tr := op.Offset(c.Sub(image.Pt(s/2, s/2))).Push(gtx.Ops)
		lg := gtx
		lg.Constraints = layout.Exact(image.Pt(s, s))
		l := material.Loader(u.th)
		l.Color = rgb(0xffffff)
		l.Layout(lg)
		tr.Pop()
	case vv.player == nil:
		cl := u.btn("st:play")
		if cl.Clicked(gtx) {
			m := statusMsg(t, up)
			if vv.external {
				u.status.viewer.paused = true
				u.backend.OpenMedia(m)
				u.toast("Opening video…")
			} else {
				u.loadVideo(vv, m) // after a failed download
			}
		}
		tr := op.Offset(c.Sub(image.Pt(rad, rad))).Push(gtx.Ops)
		cg := gtx
		cg.Constraints = layout.Exact(image.Pt(2*rad, 2*rad))
		clickable(cg, cl, func(gtx C) D {
			bg := mix(argb(0x000000, 0x90), argb(0x000000, 0xc0), u.hover(gtx, cl))
			playButton(gtx, image.Pt(rad, rad), rad, bg)
			return D{Size: gtx.Constraints.Min}
		})
		tr.Pop()
	}
}

// statusChatID is the chat ID under which status media is downloaded.
const statusChatID = "status@broadcast"
