package ui

import (
	"image"
	"image/color"
	"math"
	"strconv"
	"strings"
	"time"

	"gioui.org/f32"
	"gioui.org/font"
	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/text"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"github.com/chomosuke9/wazzapclients/internal/model"
	"github.com/chomosuke9/wazzapclients/internal/ui/icon"
)

type statusState struct {
	groupID   string
	menu, add widget.Clickable
	list      widget.List
	viewer    statusViewer
	text      statusTextState    // writing a text status
	privacy   statusPrivacyPanel // Status privacy and your audiences (statusprivacy.go)
	// showHidden opens the Hidden section: the status updates of the
	// contacts you hid (model.StatusThread.Muted).
	showHidden bool
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
// (unseen) and viewed updates from contacts, and last those of the
// contacts you hid, under Hidden, which opens and closes.
func (u *UI) layoutStatusList(gtx C) D {
	p := u.pal
	if u.status.groupID != "" && u.btn("st:all").Clicked(gtx) {
		u.setPage(pageStatus)
	}
	if u.btn("st:hidden").Clicked(gtx) {
		u.status.showHidden = !u.status.showHidden
	}
	var mine *model.StatusThread
	var recent, viewed, hidden []*model.StatusThread
	for _, t := range u.statuses {
		if u.status.groupID != "" && (!t.Group || t.ID != u.status.groupID) {
			continue
		}
		switch {
		case t.Mine:
			mine = t
		case t.Muted:
			hidden = append(hidden, t)
		case t.Viewed():
			viewed = append(viewed, t)
		default:
			recent = append(recent, t)
		}
	}
	type entry struct {
		label  string
		thread *model.StatusThread
		hidden bool // the Hidden heading, with its Show or Hide
	}
	var entries []entry
	section := func(label string, threads []*model.StatusThread) {
		if len(threads) > 0 {
			entries = append(entries, entry{label: label})
			for _, t := range threads {
				entries = append(entries, entry{thread: t})
			}
		}
	}
	section("Recent", recent)
	section("Viewed", viewed)
	if len(hidden) > 0 {
		entries = append(entries, entry{label: "Hidden", hidden: true})
		if u.status.showHidden {
			for _, t := range hidden {
				entries = append(entries, entry{thread: t})
			}
		}
	}
	now := u.now()
	if u.status.add.Clicked(gtx) {
		u.openStatusAdd()
	}
	if u.status.menu.Clicked(gtx) && u.status.groupID == "" {
		u.ctx = ctxMenu{kind: ctxStatusMenu, at: u.mouse}
	}

	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx C) D {
			title := "Status"
			if u.status.groupID != "" {
				title = "Group status"
				if c := u.chatByID(u.status.groupID); c != nil {
					title = c.Name
				}
			}
			return u.pageHeader(gtx, title,
				layout.Rigid(func(gtx C) D {
					if u.status.groupID == "" {
						return D{}
					}
					return u.iconButton(gtx, u.btn("st:all"), icBack, 40, 25, p.IconStrong)
				}),
				layout.Rigid(func(gtx C) D {
					if u.status.groupID != "" {
						return D{}
					}
					return u.iconButton(gtx, &u.status.menu, icMenu, 40, 25, p.IconStrong)
				}),
				layout.Rigid(layout.Spacer{Width: 8}.Layout),
				u.headerButton(&u.status.add, icAddCircle, 27),
			)
		}),
		layout.Flexed(1, func(gtx C) D {
			return u.scrollList(gtx, &u.status.list, len(entries)+1, func(gtx C, i int) D {
				if i == 0 {
					sub := "Click to add status update"
					if mine != nil {
						sub = statusTime(mine.Latest().Time, now)
					}
					return layout.Inset{Bottom: 9.5}.Layout(gtx, func(gtx C) D {
						title := "My status"
						if u.status.groupID != "" {
							title, sub = "Add group status", "Visible to members for 24 hours"
						}
						return u.statusRow(gtx, "status:me", mine, title, sub, 72, true)
					})
				}
				e := entries[i-1]
				if e.hidden {
					return u.hiddenHeading(gtx)
				}
				if e.label != "" {
					return u.sectionLabel(gtx, e.label, layout.Inset{Left: 27, Top: 31.5, Bottom: 22}, labelOpts{maxLines: 1})
				}
				t := e.thread
				return u.statusRow(gtx, "status:"+t.ID, t, t.Name, statusTime(t.Latest().Time, now), 76, false)
			})
		}),
	)
}

// hiddenHeading is the Hidden section's heading, with a green Show or
// Hide at its end that opens or closes the section, like WhatsApp's.
func (u *UI) hiddenHeading(gtx C) D {
	p := u.pal
	return layout.Inset{Left: 27, Right: 26, Top: 31.5, Bottom: 22}.Layout(gtx, func(gtx C) D {
		gtx.Constraints.Min.X = gtx.Constraints.Max.X
		return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
			layout.Flexed(1, u.label(15.2, "Hidden", p.TextSecondary, labelOpts{maxLines: 1}).Layout),
			layout.Rigid(func(gtx C) D {
				txt := "Show"
				if u.status.showHidden {
					txt = "Hide"
				}
				c := u.btn("st:hidden")
				return clickable(gtx, c, func(gtx C) D {
					col := mix(p.Green, p.Text, 0.25*u.hover(gtx, c))
					return u.label(15.2, txt, col, labelOpts{weight: font.Medium, maxLines: 1}).Layout(gtx)
				})
			}),
		)
	})
}

// statusRow is one poster: the ringed preview, name and time. A right
// click on someone else's opens its menu (Hide or Unhide).
func (u *UI) statusRow(gtx C, key string, t *model.StatusThread, title, sub string, height unit.Dp, mine bool) D {
	dims := u.statusRowBody(gtx, key, t, title, sub, height, mine)
	if t != nil && !t.Mine && !t.Group && u.rightClick(gtx, key, dims.Size) {
		u.ctx = ctxMenu{kind: ctxStatusRow, chatID: t.ID, at: u.mouse}
	}
	return dims
}

func (u *UI) statusRowBody(gtx C, key string, t *model.StatusThread, title, sub string, height unit.Dp, mine bool) D {
	p := u.pal
	c := u.btn(key)
	if c.Clicked(gtx) {
		switch {
		case t != nil:
			u.status.viewer.show(t)
			gtx.Execute(op.InvalidateCmd{})
		case mine:
			// Nothing posted yet: "Click to add status update".
			u.openStatusAdd()
		}
	}
	return layout.Inset{Left: 8, Right: 18}.Layout(gtx, func(gtx C) D {
		return clickable(gtx, c, func(gtx C) D {
			gtx.Constraints.Min.X = gtx.Constraints.Max.X
			if !mine {
				// Privacy mode hides who posted, not when.
				defer u.hiding(gtx, key, c.Hovered())()
			}
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
									layout.Rigid(func(gtx C) D {
										defer u.unhidden()()
										return u.label(15.2, sub, p.TextSecondary).Layout(gtx)
									}),
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
	inner := px - 2*gtx.Dp(4.5)
	off := (px - inner) / 2
	if t == nil {
		// No status yet: just your profile picture.
		t := op.Offset(image.Pt(off, off)).Push(gtx.Ops)
		u.avatar(gtx, u.meID, u.meName(), false, dp(gtx, inner))
		t.Pop()
	} else {
		u.statusRing(gtx, t, px, 1.7)
		tr := op.Offset(image.Pt(off, off)).Push(gtx.Ops)
		u.statusPreview(gtx, t, t.Latest(), inner)
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

// statusRing strokes a thread's ring in a square of px: one arc per
// update, oldest first, counter-clockwise from the top, gray once seen,
// like WhatsApp. An unseen update shared with close friends or a custom
// audience is purple. Recorded once per look, as stroked paths are costly.
func (u *UI) statusRing(gtx C, t *model.StatusThread, px int, width unit.Dp) {
	p := u.pal
	sw := gtx.Dp(width)
	// The key's name is the stroke's width and each update's look.
	name := strconv.AppendInt(make([]byte, 0, 24), int64(sw), 10)
	name = append(name, ':')
	from := len(name)
	for _, up := range t.Updates {
		switch {
		case up.Viewed:
			name = append(name, 'v')
		case up.Audience != "":
			name = append(name, 'a')
		default:
			name = append(name, 'n')
		}
	}
	k := glyphKey{name: "ring" + string(name), px: px, col: p.Green, bg: p.RingViewed, alt: p.RingAudience}
	cachedGlyph(gtx, k, func(gtx C) D {
		seen := name[from:]
		n := len(seen)
		stroke := float32(sw)
		gap := float32(0)
		if n > 1 {
			gap = 0.16
		}
		sweep := (2*math.Pi - float32(n)*gap) / float32(n)
		c := f32.Pt(float32(px)/2, float32(px)/2)
		r := float32(px)/2 - stroke/2
		for i, s := range seen {
			col := p.Green
			switch s {
			case 'v':
				col = p.RingViewed
			case 'a':
				col = p.RingAudience
			}
			start := -math.Pi/2 - gap/2 - float32(i)*(sweep+gap) - sweep
			strokeArc(gtx, c, r, start, sweep, stroke, col)
		}
		return D{Size: image.Pt(px, px)}
	})
}

// listStatus is the thread whose ring a chat list row shows: yours on
// your own chat, and a contact's or group's while it has updates you
// haven't seen (never a hidden one), as WhatsApp's list does.
func (u *UI) listStatus(c *model.Chat) *model.StatusThread {
	for _, t := range u.statuses {
		if t.Mine {
			if c.Self {
				return t
			}
			continue
		}
		if t.ID == c.ID && t.Group == c.IsGroup {
			if t.Muted || t.Viewed() {
				return nil
			}
			return t
		}
	}
	return nil
}

// statusPreview fills a circle of px with an update's picture, or a text
// status's words in small on its background color.
func (u *UI) statusPreview(gtx C, t *model.StatusThread, up *model.StatusUpdate, px int) {
	r := image.Rect(0, 0, px, px)
	if len(up.Thumb) > 0 {
		th := up.Thumb
		load := func() []byte { return th }
		var e *imgEntry
		if u.blurred() {
			e = u.images.getBlurred("st:"+up.ID, load) // privacy mode
		} else {
			e = u.images.get("st:"+up.ID, px, load)
		}
		if e.state == imgReady {
			defer clip.Ellipse(r).Push(gtx.Ops).Pop()
			paintCover(gtx, e.op, e.size, r)
			return
		}
	}
	if up.Media == model.MediaNone && up.Background != 0 {
		fillCircle(gtx, image.Pt(px/2, px/2), px/2, argbColor(up.Background))
		u.statusTextThumb(gtx, up.Text, px)
		return
	}
	id, name := t.ID, t.Name
	if t.Mine {
		id, name = u.meID, u.meName()
	}
	u.avatar(gtx, id, name, t.Group, dp(gtx, px))
}

// statusTextThumb draws a text status's words in small in the middle of
// its circle of px, as WhatsApp's previews show them.
func (u *UI) statusTextThumb(gtx C, txt string, px int) {
	txt = trimSpace(txt)
	if txt == "" {
		return
	}
	size := unit.Sp(float32(px) / gtx.Metric.PxPerSp / 6.5)
	l := record(gtx, func(gtx C) D {
		gtx.Constraints = layout.Constraints{Max: image.Pt(px*3/4, px*3/4)}
		return u.label(size, txt, rgb(0xffffff), labelOpts{align: text.Middle, maxLines: 3}).Layout(gtx)
	})
	defer clip.Ellipse(image.Rect(0, 0, px, px)).Push(gtx.Ops).Pop()
	l.at(gtx, (px-l.size.X)/2, (px-l.size.Y)/2)
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
	backBtn  widget.Clickable
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
	// The reply bar's emoji button opens the quick reactions (reacts), and
	// its heart sends ❤️.
	emojiBtn, heartBtn widget.Clickable
	reacts             bool
	reactAnim          tween
	mention            widget.Clickable // "Mentioned privately"
	backdrop           tween            // the blurred update behind it fading in
	views              statusViews      // who saw your own update (statusviews.go)
}

// statusDuration is how long a picture or text update stays on screen. A
// video stays until it ends.
const statusDuration = 6 * time.Second

func (v *statusViewer) show(t *model.StatusThread) {
	// Start at the first unseen update, like WhatsApp.
	at := 0
	for i, up := range t.Updates {
		if !up.Viewed {
			at = i
			break
		}
	}
	v.showAt(t, at)
}

// showAt opens the viewer on t's i-th update.
func (v *statusViewer) showAt(t *model.StatusThread, i int) {
	v.video.stop()
	v.thread, v.closing = t, false
	v.index = i
	v.shownAt, v.zp, v.frac, v.held, v.paused = time.Time{}, zoomPan{}, 0, 0, false
	v.reply.SingleLine, v.reply.Submit = true, true
	v.views.open, v.views.loaded = false, false
	v.reacts = false
	v.reply.SetText("")
}

// close fades the viewer out. A video stops at once.
func (v *statusViewer) close() {
	if v.thread != nil {
		v.closing = true
	}
	v.views.open = false
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
		Text: up.Text, Thumb: up.Thumb, Time: up.Time, FromMe: t.Mine || up.FromMe, Sender: firstStatusAuthor(t, up), SenderID: firstStatusAuthorID(t, up), Duration: up.Duration, FileType: up.FileType}
}

// syncStatusVideo starts the update's video, and stops the last one when
// the viewer moves on.
func (u *UI) syncStatusVideo(t *model.StatusThread, up *model.StatusUpdate) {
	v := &u.status.viewer
	vv := &v.video
	m := statusMsg(t, up)
	if !isVideo(m) && m.Media != model.MediaVoice && m.Media != model.MediaAudio {
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
	if u.sendStatusText(trimSpace(v.reply.Text())) {
		v.reply.SetText("")
		u.requestFocus(nil)
	}
}

// sendStatusText sends txt to the poster of the update shown, quoting it,
// and reports whether it went.
func (u *UI) sendStatusText(txt string) bool {
	v := &u.status.viewer
	t := v.thread
	if !v.isOpen() || t.Mine || t.Group || txt == "" {
		return false
	}
	m := u.backend.Send(t.ID, model.Draft{Text: txt, Reply: statusMsg(t, t.Updates[v.index])})
	if m != nil {
		u.upsertMessage(m)
		u.toast("Reply sent")
	}
	return true
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
		v.shownAt, v.zp, v.frac, v.held = time.Time{}, zoomPan{}, 0, 0
		if v.index < 0 {
			v.index = 0
		}
		if v.index >= len(t.Updates) {
			v.index = len(t.Updates) - 1
			v.close()
		}
	}
	if v.isOpen() {
		if v.closeBtn.Clicked(gtx) || v.backBtn.Clicked(gtx) {
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
			u.toggleMute(&v.video)
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
		// Your own updates in a group's stories are seen once watched
		// too; the backend tells only others' posters.
		if up := t.Updates[v.index]; !up.Viewed && !t.Mine {
			up.Viewed = true
			u.backend.ViewStatus(t.ID, up.ID)
		}
	}
	up := t.Updates[v.index]
	if v.isOpen() {
		u.syncStatusVideo(t, up)
		// Its menu, and the dialog Hide asks in, hold it too.
		hold := v.paused || v.zp.zoomed() || v.replying(gtx) || v.reacts || u.ctx.isOpen() || u.dialog.isOpen() || v.views.open
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
	u.statusBackdrop(gtx, up, a == 1 && v.isOpen())
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

	// The update fills a portrait frame as tall as the window, like
	// WhatsApp's, with its controls over it and the reply bar across its
	// bottom.
	frameW := min(sz.X-gtx.Dp(40), sz.Y*9/16)
	frame := image.Rect((sz.X-frameW)/2, 0, (sz.X+frameW)/2, sz.Y)
	zoom := pushFx(gtx, 1, scaleAt(frame.Min.Add(frame.Size().Div(2)), lerp(0.3, 1, e)))
	u.layoutStatusContent(gtx, t, up, frame, &v.zp)
	zoom.Pop()
	defer pushFx(gtx, e, f32.Affine2D{}).Pop()
	zoomed := v.zp.zoomed()
	white := rgb(0xffffff)
	reply := !t.Mine && !t.Group
	barH := gtx.Dp(44)
	barTop := sz.Y - gtx.Dp(32) - barH
	if !zoomed {
		// Shades that keep the controls and the caption readable.
		shade(gtx, image.Rect(frame.Min.X, frame.Min.Y, frame.Max.X, frame.Min.Y+gtx.Dp(130)), 0x90, 0)
		if up.Text != "" && up.Media != model.MediaNone || reply || t.Mine && !t.Group {
			shade(gtx, image.Rect(frame.Min.X, barTop-gtx.Dp(110), frame.Max.X, frame.Max.Y), 0, 0xa0)
		}
	}

	// Progress segments across the top of the frame.
	n := len(t.Updates)
	gap := gtx.Dp(4)
	inset := gtx.Dp(12)
	segW := (frame.Dx() - 2*inset - (n-1)*gap) / n
	y := gtx.Dp(22)
	h := max(2, gtx.Dp(3))
	for i := 0; i < n; i++ {
		x := frame.Min.X + inset + i*(segW+gap)
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
			fillRRect(gtx, image.Rect(x, y, x+int(float32(segW)*done), y+h), h/2, white)
		}
	}

	// The buttons on the header's right, right to left: the menu, the
	// volume (with its slider under it while hovered) and pause.
	cy := gtx.Dp(71) // the header's middle
	bx := frame.Max.X - gtx.Dp(6)
	headBtn := func(c *widget.Clickable, ic *icon.Icon, size unit.Dp) {
		bx -= gtx.Dp(48)
		defer op.Offset(image.Pt(bx, cy-gtx.Dp(24))).Push(gtx.Ops).Pop()
		bg := gtx
		bg.Constraints = layout.Constraints{Max: image.Pt(gtx.Dp(48), gtx.Dp(48))}
		u.iconButton(bg, c, ic, 48, size, white)
	}
	if reply {
		c := u.btn("st:viewermenu")
		if c.Clicked(gtx) {
			u.ctx = ctxMenu{kind: ctxStatusRow, chatID: t.ID, at: u.mouse}
		}
		headBtn(c, icMenu, 24)
	}
	if v.video.player != nil {
		ic := icVolumeFill
		if !u.soundOn(v.video.muted) {
			ic = icVolumeOffFill
		}
		headBtn(&v.muteBtn, ic, 24)
		u.statusVolume(gtx, image.Rect(bx, cy-gtx.Dp(24), bx+gtx.Dp(48), cy+gtx.Dp(24)))
	}
	ic := icPauseFill
	if v.paused {
		ic = icPlayFill
	}
	headBtn(&v.pauseBtn, ic, 24)

	// Poster and time on the left.
	name := t.Name
	id := t.ID
	if t.Mine {
		name, id = "My status", u.meID
	}
	if t.Group {
		name = t.Name + " · " + up.Sender
	}
	ax := frame.Min.X + inset
	av := op.Offset(image.Pt(ax, cy-gtx.Dp(20))).Push(gtx.Ops)
	u.avatar(gtx, id, name, t.Group, 40)
	av.Pop()
	x0 := ax + gtx.Dp(52)
	tg := gtx
	tg.Constraints = layout.Constraints{Max: image.Pt(max(0, bx-gtx.Dp(4)-x0), gtx.Dp(60))}
	nm := record(tg, u.label(16, name, white, labelOpts{maxLines: 1}).Layout)
	sub := record(tg, func(gtx C) D { return u.statusSubtitle(gtx, up) })
	top := cy - (nm.size.Y+sub.size.Y)/2
	nm.at(gtx, x0, top)
	sub.at(gtx, x0, top+nm.size.Y)

	if up.Audience != "" && !zoomed {
		// The audience its poster shared it with, under the header.
		chip := record(gtx, func(gtx C) D {
			gtx.Constraints = layout.Constraints{Max: image.Pt(max(0, frame.Dx()-2*inset), frame.Dy())}
			return background(gtx, argb(0x000000, 0x90), 14, func(gtx C) D {
				return layout.Inset{Left: 10, Right: 12, Top: 5, Bottom: 5}.Layout(gtx,
					u.label(13.5, up.AudienceEmoji+" "+up.Audience, white, labelOpts{weight: font.Medium, maxLines: 1}).Layout)
			})
		})
		chip.at(gtx, frame.Min.X+inset, cy+gtx.Dp(32))
	}

	// The caption, over the bottom of the picture.
	if up.Text != "" && up.Media != model.MediaNone && !zoomed {
		bottom := frame.Max.Y - gtx.Dp(24)
		switch {
		case reply:
			bottom = barTop - gtx.Dp(10)
		case t.Mine && !t.Group:
			bottom = sz.Y - gtx.Dp(32) - gtx.Dp(38) - gtx.Dp(12) // over the eye
		}
		capW := max(0, frame.Dx()-gtx.Dp(32))
		cap := record(gtx, func(gtx C) D {
			gtx.Constraints = layout.Constraints{Min: image.Pt(capW, 0), Max: image.Pt(capW, frame.Dy()/2)}
			l := u.label(16, up.Text, white, labelOpts{weight: font.SemiBold, align: text.Middle})
			l.MaxLines = 4
			return l.Layout(gtx)
		})
		cap.at(gtx, frame.Min.X+gtx.Dp(16), bottom-cap.size.Y)
	}

	// Back and close, in the window's corners.
	for _, b := range []struct {
		c  *widget.Clickable
		ic *icon.Icon
		x  int
	}{{&v.backBtn, icBack, gtx.Dp(18)}, {&v.closeBtn, icClose, sz.X - gtx.Dp(54)}} {
		t := op.Offset(image.Pt(b.x, gtx.Dp(7))).Push(gtx.Ops)
		bg := gtx
		bg.Constraints = layout.Constraints{Max: image.Pt(gtx.Dp(48), gtx.Dp(48))}
		u.iconButton(bg, b.c, b.ic, 48, 26, white)
		t.Pop()
	}

	if reply {
		u.layoutStatusReply(gtx, frame.Min.X+frame.Dx()/2, barTop, barH)
	}
	if t.Mine && !t.Group {
		u.layoutStatusViews(gtx, up, frame)
	}
}

// shade fills r with black fading from alpha a0 at its top to a1 at its
// bottom.
func shade(gtx C, r image.Rectangle, a0, a1 uint8) {
	defer clip.Rect(r).Push(gtx.Ops).Pop()
	paint.LinearGradientOp{
		Stop1: f32.Pt(0, float32(r.Min.Y)), Color1: argb(0x000000, a0),
		Stop2: f32.Pt(0, float32(r.Max.Y)), Color2: argb(0x000000, a1),
	}.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
}

// statusBackdrop draws the update blurred across the window once the
// viewer is open (on), under a veil that lifts as it fades in.
func (u *UI) statusBackdrop(gtx C, up *model.StatusUpdate, on bool) {
	v := &u.status.viewer
	if !on {
		v.backdrop = tween{}
		return
	}
	k := easeOut(v.backdrop.step(gtx, true, 2*durDialog))
	r := image.Rectangle{Max: gtx.Constraints.Max}
	switch {
	case len(up.Thumb) > 0:
		th := up.Thumb
		e := u.images.getBlurred("st:"+up.ID, func() []byte { return th })
		if e.state != imgReady {
			return
		}
		paintCover(gtx, e.op, e.size, r)
	case up.Media == model.MediaNone && up.Background != 0:
		fillRect(gtx, r, argbColor(up.Background))
	default:
		return
	}
	fillRect(gtx, r, faded(u.pal.StatusBg, lerp(1, 0.72, k)))
}

// statusSubtitle is the line under the poster's name: when the update was
// posted, whether it was deleted (kept with Keep deleted messages) and
// whether it mentioned you or a group of yours, which leads to the
// message that says so.
func (u *UI) statusSubtitle(gtx C, up *model.StatusUpdate) D {
	v := &u.status.viewer
	dim := argb(0xffffff, 0xd8)
	const size = 13.5
	line := []layout.FlexChild{layout.Rigid(u.label(size, statusTime(up.Time, u.now()), dim, labelOpts{maxLines: 1}).Layout)}
	dot := layout.Rigid(u.label(size, " • ", dim, labelOpts{maxLines: 1}).Layout)
	if !up.Revoked.IsZero() {
		// Kept with Keep deleted messages (extras.go).
		line = append(line, dot,
			layout.Rigid(iconW(icBlock, 14, u.pal.Danger)),
			layout.Rigid(layout.Spacer{Width: 2}.Layout),
			layout.Rigid(u.label(size, "Deleted", u.pal.Danger, labelOpts{maxLines: 1}).Layout))
	}
	if up.MentionMsg != "" {
		if v.mention.Clicked(gtx) && v.isOpen() {
			u.openStatusMention(up)
		}
		line = append(line, dot, layout.Rigid(func(gtx C) D {
			return clickable(gtx, &v.mention, func(gtx C) D {
				chev := gtx.Dp(18)
				lg := gtx
				lg.Constraints.Min.X = 0
				lg.Constraints.Max.X = max(0, gtx.Constraints.Max.X-chev)
				l := record(lg, u.label(size, "@ Mentioned privately", dim, labelOpts{maxLines: 1}).Layout)
				l.at(gtx, 0, 0)
				t := op.Offset(image.Pt(l.size.X+gtx.Dp(2), (l.size.Y-gtx.Dp(16))/2)).Push(gtx.Ops)
				drawIcon(gtx, icChevronRight, 16, dim)
				t.Pop()
				return D{Size: image.Pt(l.size.X+chev, l.size.Y)}
			})
		}))
	}
	return layout.Flex{Alignment: layout.Middle}.Layout(gtx, line...)
}

// statusVolume shows the video's volume slider under its button (btn)
// while the pointer is over either, or drags it.
func (u *UI) statusVolume(gtx C, btn image.Rectangle) {
	vv := &u.status.viewer.video
	cx := btn.Min.X + btn.Dx()/2
	pill := image.Rect(cx-gtx.Dp(56), btn.Max.Y+gtx.Dp(4), cx+gtx.Dp(56), btn.Max.Y+gtx.Dp(36))
	show := u.hovered["st:vol"] || vv.vol.dragging
	area := btn
	if show {
		area = area.Union(pill)
	}
	func() {
		defer op.Offset(area.Min).Push(gtx.Ops).Pop()
		u.hoverArea(gtx, "st:vol", area.Size())
	}()
	if !show {
		return
	}
	fillRRect(gtx, pill, pill.Dy()/2, argb(0x1f1f1f, 0xe8))
	defer op.Offset(image.Pt(pill.Min.X+gtx.Dp(16), pill.Min.Y+pill.Dy()/2)).Push(gtx.Ops).Pop()
	u.layoutVolume(gtx, vv, pill.Dx()-gtx.Dp(32))
}

// likeRed is the heart of a liked status.
var likeRed = rgb(0xff3b5c)

// isLike reports whether a reaction to a status is a like: a heart.
func isLike(emoji string) bool { return strings.HasPrefix(emoji, "❤") }

// toggleStatusLike likes the update shown, or takes the like back.
func (u *UI) toggleStatusLike() {
	v := &u.status.viewer
	t := v.thread
	if !v.isOpen() || t.Mine || t.Group {
		return
	}
	up := t.Updates[v.index]
	up.Liked = !up.Liked
	u.backend.LikeStatus(up.ID, up.Liked)
}

// statusQuickReactions are the emojis the reply bar's emoji button
// offers, as WhatsApp's phones do: one sends it as a reply.
var statusQuickReactions = []string{"😂", "😮", "😍", "😢", "👏", "🔥", "🎉", "💯"}

// layoutStatusReply draws the reply bar centered on cx, h px tall from
// top: the quick reactions' button, the "Type a reply…" box, and the
// heart that likes the update, or the send button once there is text.
func (u *UI) layoutStatusReply(gtx C, cx, top, h int) {
	v := &u.status.viewer
	p := u.pal
	white := rgb(0xffffff)
	sz := gtx.Constraints.Max
	if v.isOpen() {
		if v.emojiBtn.Clicked(gtx) {
			v.reacts = !v.reacts
		}
		if v.heartBtn.Clicked(gtx) {
			u.toggleStatusLike()
		}
		for i, em := range statusQuickReactions {
			if u.btn("st:qr:" + itoa(i)).Clicked(gtx) {
				v.reacts = false
				u.sendStatusText(em)
			}
		}
	}
	btn := gtx.Dp(44)
	inputW := max(gtx.Dp(120), min(gtx.Dp(760), sz.X-gtx.Dp(32)-2*btn-gtx.Dp(24)))
	rowW := btn + gtx.Dp(16) + inputW + gtx.Dp(8) + btn
	x0 := max(gtx.Dp(16), cx-rowW/2)
	place := func(x int, w func(gtx C) D) {
		defer op.Offset(image.Pt(x, top+(h-btn)/2)).Push(gtx.Ops).Pop()
		bg := gtx
		bg.Constraints = layout.Constraints{Max: image.Pt(btn, btn)}
		w(bg)
	}
	place(x0, func(gtx C) D { return u.iconButton(gtx, &v.emojiBtn, icEmoji, 44, 26, white) })

	r := image.Rect(x0+btn+gtx.Dp(16), top, x0+btn+gtx.Dp(16)+inputW, top+h)
	fillRRect(gtx, r, gtx.Dp(8), argb(0x262626, 0xd8))
	func() {
		defer op.Offset(r.Min).Push(gtx.Ops).Pop()
		gtx := gtx
		gtx.Constraints = layout.Exact(r.Size())
		// A click on the box types, rather than going to the next update.
		box := u.btn("st:replybox")
		if noFocus(box).Clicked(gtx) {
			gtx.Execute(key.FocusCmd{Tag: &v.reply})
		}
		noFocus(box).Layout(gtx, func(gtx C) D { return D{Size: gtx.Constraints.Max} })
		layout.Inset{Left: 16, Right: 12}.Layout(gtx, func(gtx C) D {
			return vcenter(gtx, gtx.Constraints.Max.Y, func(gtx C) D {
				gtx.Constraints.Min.X = gtx.Constraints.Max.X
				e := material.Editor(u.th, &v.reply, "Type a reply…")
				e.TextSize = 16
				e.Color = white
				e.HintColor = argb(0xffffff, 0xb0)
				e.SelectionColor = argb(0x53bdeb, 0x60)
				return e.Layout(gtx)
			})
		})
	}()

	hx := r.Max.X + gtx.Dp(8)
	if trimSpace(v.reply.Text()) != "" {
		place(hx, func(gtx C) D {
			return clickable(gtx, &v.send, func(gtx C) D {
				fillCircle(gtx, image.Pt(btn/2, btn/2), btn/2, p.Green)
				return centerIn(gtx, btn, iconW(icSend, 21, p.OnGreen))
			})
		})
	} else {
		up := v.thread.Updates[v.index]
		ic, col := icHeart, white
		if up.Liked {
			ic, col = icHeartFill, likeRed
		}
		place(hx, func(gtx C) D { return u.iconButton(gtx, &v.heartBtn, ic, 44, 26, col) })
	}

	// The quick reactions, in a pill over the emoji button.
	a := v.reactAnim.step(gtx, v.reacts && v.isOpen(), durPopIn)
	if a == 0 {
		return
	}
	cell := gtx.Dp(44)
	pad := gtx.Dp(6)
	pill := image.Rect(x0, top-gtx.Dp(10)-cell-2*pad, x0+len(statusQuickReactions)*cell+2*pad, top-gtx.Dp(10))
	pill = pill.Add(image.Pt(0, int(float32(gtx.Dp(8))*(1-easeOut(a)))))
	withOpacity(gtx, easeOut(a), func() {
		fillRRect(gtx, pill, pill.Dy()/2, argb(0x262626, 0xf0))
		for i, em := range statusQuickReactions {
			c := u.btn("st:qr:" + itoa(i))
			func() {
				defer op.Offset(image.Pt(pill.Min.X+pad+i*cell, pill.Min.Y+pad)).Push(gtx.Ops).Pop()
				cg := gtx
				cg.Constraints = layout.Exact(image.Pt(cell, cell))
				clickable(cg, c, func(gtx C) D {
					if hv := u.hover(gtx, c); hv > 0 {
						fillCircle(gtx, image.Pt(cell/2, cell/2), cell/2, faded(argb(0xffffff, 0x30), hv))
					}
					return centerIn(gtx, cell, u.label(24, em, white, labelOpts{maxLines: 1}).Layout)
				})
			}()
		}
	})
}

// layoutStatusContent draws an update inside r: its picture (full size once
// downloaded, zoomed and panned by zp), its video, or its text on the
// chosen background.
func (u *UI) layoutStatusContent(gtx C, t *model.StatusThread, up *model.StatusUpdate, r image.Rectangle, zp *zoomPan) {
	defer clip.UniformRRect(r, gtx.Dp(8)).Push(gtx.Ops).Pop()
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
	audio := up.Media == model.MediaVoice || up.Media == model.MediaAudio
	vid := isVideo(statusMsg(t, up)) || audio
	vv := &u.status.viewer.video
	switch {
	case up.Media == model.MediaImage:
		b := u.backend
		id := up.ID
		if e := u.images.get("sm:"+id, max(r.Dx(), r.Dy()), func() []byte { return b.MediaData(statusChatID, id) }); e.state == imgReady {
			img = e
		}
	case vid && !audio && vv.msgID == up.ID:
		img = u.videoFrame(vv, r.Size())
	}
	if audio {
		tr := op.Offset(r.Min.Add(image.Pt((r.Dx()-gtx.Dp(64))/2, r.Dy()/3))).Push(gtx.Ops)
		icMic.Layout(gtx, 64, u.pal.OnGreen)
		tr.Pop()
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
}

// layoutStatusVideoCenter draws a spinner while a video status downloads
// or starts. A video this system can't play, or that failed to download,
// gets a play button that opens it in the system's player or tries again.
func (u *UI) layoutStatusVideoCenter(gtx C, t *model.StatusThread, up *model.StatusUpdate, c image.Point) {
	vv := &u.status.viewer.video
	rad := gtx.Dp(34)
	switch {
	case vv.loading || vv.player != nil && vv.size == (image.Point{}) && up.Media != model.MediaAudio && up.Media != model.MediaVoice:
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

func firstStatusAuthor(t *model.StatusThread, up *model.StatusUpdate) string {
	if up.Sender != "" {
		return up.Sender
	}
	return t.Name
}
func firstStatusAuthorID(t *model.StatusThread, up *model.StatusUpdate) string {
	if up.SenderID != "" {
		return up.SenderID
	}
	return t.ID
}
