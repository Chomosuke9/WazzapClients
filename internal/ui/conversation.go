package ui

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"slices"
	"strings"
	"time"

	"gioui.org/f32"
	"gioui.org/font"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/text"
	"gioui.org/unit"

	"github.com/chomosuke9/wazzapclients/internal/model"
	"github.com/chomosuke9/wazzapclients/internal/ui/styledtext"
)

type rowKind int

const (
	rowDate rowKind = iota
	rowEncryption
	rowMessage
	rowTyping // someone typing, after the newest message
	rowNote   // a slash command's note, which only you see (slash.go)
	rowUnread // "N unread messages", above the first of them (unread.go)
)

// convRow is one entry of the message list: a day separator, the
// encryption notice, or a message.
type convRow struct {
	kind rowKind
	date string
	msg  *model.Message
	note *localNote
	// first marks the first message of a run from the same sender. It gets
	// the bubble tail (and, in groups, the sender's name and avatar).
	first bool
	// group holds a run of stickers from one sender (msg is the first), laid
	// out side by side (stickerrow.go), or with album set the pictures of
	// an album, in one grid (album.go). Nil for a single message.
	group []*model.Message
	album bool
}

// has reports whether the row shows message id.
func (r convRow) has(id string) bool {
	if r.msg == nil {
		return false
	}
	if r.group == nil {
		return r.msg.ID == id
	}
	return slices.ContainsFunc(r.group, func(m *model.Message) bool { return m.ID == id })
}

// rows rebuilds the flattened message list when the loaded messages change.
func (u *UI) rows(c *model.Chat) []convRow {
	if u.conv.rowsFor == c && u.conv.rowsVer == u.msgsVer {
		return u.conv.rows
	}
	now := u.now()
	var rows []convRow
	if !u.conv.olderMore {
		rows = append(rows, convRow{kind: rowEncryption}) // the start of the chat
	}
	var prev *model.Message
	ann := u.announcementsOf(c) != nil
	// Notes go between the messages by time, after any that came earlier.
	// Ones older than the loaded messages wait for their page, unless the
	// chat's start is loaded (messages can be dated ahead of this clock).
	notes := u.slash.notes[c.ID]
	for _, m := range u.msgs {
		for len(notes) > 0 && notes[0].at.Before(m.Time) {
			if prev != nil || !u.conv.olderMore {
				rows = append(rows, convRow{kind: rowNote, note: notes[0], first: true})
			}
			notes = notes[1:]
		}
		newDay := prev == nil || !sameDay(prev.Time, m.Time)
		if newDay {
			rows = append(rows, convRow{kind: rowDate, date: dateChip(m.Time, now)})
		}
		// Announcements each have a card of their own.
		first := ann || newDay || prev.FromMe != m.FromMe || prev.SenderID != m.SenderID ||
			m.Time.Sub(prev.Time) > 10*time.Minute
		if u.unreadRow(c, m) {
			rows = append(rows, convRow{kind: rowUnread})
			first = true
		}
		// A picture of an album joins the one before it in the album.
		if last := &rows[len(rows)-1]; last.kind == rowMessage && !ann && (last.group == nil || last.album) && joinsAlbum(last.msg, m) {
			if last.group == nil {
				last.group, last.album = []*model.Message{last.msg}, true
			}
			last.group = append(last.group, m)
			prev = m
			continue
		}
		// A sticker joins the stickers its sender sent just before.
		if last := &rows[len(rows)-1]; !first && last.kind == rowMessage && groupsSticker(m) && groupsSticker(last.msg) && !ann {
			if last.group == nil {
				last.group = []*model.Message{last.msg}
			}
			last.group = append(last.group, m)
			prev = m
			continue
		}
		rows = append(rows, convRow{kind: rowMessage, msg: m, first: first})
		prev = m
	}
	if !u.conv.newerMore {
		for _, n := range notes {
			rows = append(rows, convRow{kind: rowNote, note: n, first: true})
		}
	}
	u.conv.rows, u.conv.rowsFor, u.conv.rowsVer = rows, c, u.msgsVer
	return rows
}

func (u *UI) layoutConversation(gtx C) D {
	c := u.selected
	// The chat's theme colors its wallpaper and your bubbles.
	if tp := u.chatPalette(c); tp != u.pal {
		old := u.pal
		u.pal = tp
		defer func() { u.pal = old }()
	}
	u.conv.selV = u.conv.selAnim.step(gtx, u.conv.selecting, durGrow)
	pinned := u.conv.pinned
	// The send view covers the conversation; once it's all in, the chat
	// underneath isn't drawn at all.
	sv := u.sendViewStep(gtx)
	u.conv.editorElsewhere = sv > 0
	if sv >= 1 {
		u.layoutSendView(gtx, sv)
		u.layoutDropHint(gtx)
		return D{Size: gtx.Constraints.Max}
	}
	d := layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx C) D { return u.layoutConvHeader(gtx, c) }),
		layout.Rigid(func(gtx C) D {
			if pinned == nil {
				return D{}
			}
			return u.layoutPinnedBanner(gtx, pinned)
		}),
		layout.Flexed(1, func(gtx C) D {
			sz := gtx.Constraints.Max
			u.conv.wallpaper.layout(gtx, u.pal.ChatBg, u.pal.Doodle, u.doodles)

			// The composer floats over the wallpaper; the list ends above it.
			m := op.Record(gtx.Ops)
			cgtx := gtx
			cgtx.Constraints = layout.Constraints{Min: image.Pt(sz.X, 0), Max: sz}
			var cd D
			switch who := u.sendBlocked(c); {
			case isChannelID(c.ID):
				// Channels are read-only.
			case who != "" && !u.conv.selecting && u.conv.selV == 0:
				cd = u.layoutSendBlocked(cgtx, who)
			default:
				cd = u.layoutComposer(cgtx)
			}
			composer := m.Stop()

			lgtx := gtx
			lgtx.Constraints = layout.Exact(image.Pt(sz.X, max(0, sz.Y-cd.Size.Y)))
			u.layoutMessages(lgtx, c)

			t := op.Offset(image.Pt(0, sz.Y-cd.Size.Y)).Push(gtx.Ops)
			composer.Add(gtx.Ops)
			t.Pop()
			if u.picker.shown() && u.picker.mode == pickComposer && sv == 0 {
				// Deferred so it draws (and takes clicks) above everything.
				m := op.Record(gtx.Ops)
				u.layoutPicker(gtx, image.Pt(gtx.Dp(12), sz.Y-cd.Size.Y+gtx.Dp(4)), sz.X-gtx.Dp(24))
				op.Defer(gtx.Ops, m.Stop())
			}
			return D{Size: sz}
		}),
	)
	if sv > 0 {
		u.layoutSendView(gtx, sv)
	}
	u.layoutDropHint(gtx)
	return d
}

func (u *UI) layoutConvHeader(gtx C, c *model.Chat) D {
	p := u.pal
	gtx.Constraints.Min.X = gtx.Constraints.Max.X
	return background(gtx, p.Panel, 0, func(gtx C) D {
		return vcenter(gtx, gtx.Dp(64), func(gtx C) D {
			return layout.Inset{Left: 17, Right: 16}.Layout(gtx, func(gtx C) D {
				sub := c.Presence
				if ch := u.channelByID(c.ID); ch != nil {
					sub = followers(ch.Followers)
				}
				// Announcements go by their community's name and picture.
				cm := u.announcementsOf(c)
				if cm != nil {
					sub = "Announcements"
				}
				if c.Typing != "" {
					sub = "typing…"
					if c.IsGroup {
						sub = shortName(c.Typing) + " is typing…"
					}
				}
				if sub == "" {
					sub = "click here for contact info"
					if c.IsGroup {
						sub = "click here for group info"
					}
				}
				name := c.Name
				if c.Self {
					name += " (You)"
				}
				if cm != nil {
					name = cm.Name
				}
				calls := func(gtx C) D {
					// Video call with a drop-down arrow, like WhatsApp's call picker.
					return clickable(gtx, &u.conv.video, func(gtx C) D {
						h := gtx.Dp(40)
						if a := u.hover(gtx, &u.conv.video); a > 0 {
							fillRRect(gtx, image.Rect(0, 0, gtx.Dp(60), h), h/2, faded(p.Hover, a))
						}
						gtx.Constraints = layout.Exact(image.Pt(gtx.Dp(60), h))
						return layout.Center.Layout(gtx, func(gtx C) D {
							return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
								layout.Rigid(iconW(icVideo, 27, p.IconStrong)),
								layout.Rigid(iconW(icDropDown, 22, p.IconStrong)),
							)
						})
					})
				}
				divider := func(gtx C) D {
					h := gtx.Dp(24)
					fillRect(gtx, image.Rect(0, 0, max(1, gtx.Dp(1)), h), p.Divider)
					return D{Size: image.Pt(max(1, gtx.Dp(1)), h)}
				}
				if cm != nil {
					// No calls in announcements; their community's groups instead.
					calls = func(gtx C) D { return u.layoutCommunityButton(gtx, cm) }
					divider = func(C) D { return D{} }
				}
				return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
					layout.Flexed(1, func(gtx C) D {
						return clickable(gtx, &u.conv.header, func(gtx C) D {
							gtx.Constraints.Min.X = gtx.Constraints.Max.X
							return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
								layout.Rigid(func(gtx C) D {
									if isChannelID(c.ID) {
										return u.avatarOf(gtx, c.ID, avatarChannel, 41)
									}
									if cm != nil {
										return u.avatarOf(gtx, cm.ID, avatarCommunity, 41)
									}
									return u.avatar(gtx, c.ID, c.Name, c.IsGroup, 41)
								}),
								layout.Rigid(layout.Spacer{Width: 16}.Layout),
								layout.Flexed(1, func(gtx C) D {
									return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
										layout.Rigid(u.label(17, name, p.Text, labelOpts{weight: font.SemiBold, maxLines: 1}).Layout),
										layout.Rigid(layout.Spacer{Height: 1}.Layout),
										layout.Rigid(u.label(14, sub, p.TextSecondary).Layout),
									)
								}),
							)
						})
					}),
					layout.Rigid(calls),
					layout.Rigid(layout.Spacer{Width: 12}.Layout),
					layout.Rigid(divider),
					layout.Rigid(layout.Spacer{Width: 12}.Layout),
					layout.Rigid(func(gtx C) D { return u.iconButton(gtx, &u.conv.search, icSearch, 40, 26, p.IconStrong) }),
					layout.Rigid(layout.Spacer{Width: 8}.Layout),
					layout.Rigid(func(gtx C) D { return u.iconButton(gtx, &u.conv.menu, icMenu, 40, 26, p.IconStrong) }),
				)
			})
		})
	})
}

func dp(gtx C, px int) unit.Dp { return unit.Dp(float32(px) / gtx.Metric.PxPerDp) }

// flashTime is how long a message stays highlighted after a jump to it.
const flashTime = 1500 * time.Millisecond

// glide scrolls the message list smoothly to a row: the list is put where
// the row is now (or a screen away, when it isn't on screen) and the
// offset eases to zero.
type glide struct {
	first   int // the row that ends at the top
	pending bool
	active  bool
	from    float32 // starting offset in px
	start   time.Time
}

// glideFrom returns the list offset that shows row i where it is now, from
// the rows laid out last frame, or one screen away in its direction.
func (u *UI) glideFrom(i, screen int) int {
	pos := u.conv.list.Position
	y := -pos.Offset // of row i, from the top of the list
	known := true
	for j := pos.First; j < i && known; j++ {
		h, ok := u.conv.heights[j]
		y, known = y+h, ok
	}
	for j := i; j < pos.First && known; j++ {
		h, ok := u.conv.heights[j]
		y, known = y-h, ok
	}
	switch {
	case !known && i < pos.First, known && y < -screen:
		y = -screen
	case !known, y > screen:
		y = screen
	}
	return -y
}

// appearing lays out a new message's row growing from nothing at the
// bottom. The bubble is revealed from its top down as the rows above make
// room, like WhatsApp. A message that replaces the typing bubble grows from
// the room the bubble took instead, so the chat moves only once, while
// ghost draws the bubble leaving: its growing in played backward.
func (u *UI) appearing(gtx C, id string, w layout.Widget, ghost func(gtx C, v float32)) D {
	k := animKey{id: id, tag: tagAppear}
	v := u.anims.fade(gtx, k, true, durAppear, durAppear)
	takeover := u.conv.takeover == id
	from := 0
	if takeover {
		from = u.conv.takeoverH
	}
	if v >= 1 {
		u.anims.stop(k)
		if takeover {
			u.conv.takeover = ""
		}
	}
	dims := growRow(gtx, from, easeOut(v), w)
	if g := 1 - 2*v; takeover && g > 0 {
		// Twice as fast, so it's gone before the message shows clearly.
		ghost(gtx, g)
	}
	return dims
}

// growRow lays out a row growing from height from (in px) to its full
// height as e goes from 0 to 1, revealed from its top down and faded in.
func growRow(gtx C, from int, e float32, w layout.Widget) D {
	full := record(gtx, w)
	// At least 1px: the list drops a trailing child of no height when it
	// trims to the viewport, and would then stop following the end.
	h := max(1, lerpInt(from, full.size.Y, e))
	defer clip.Rect{Max: image.Pt(full.size.X, h)}.Push(gtx.Ops).Pop()
	withOpacity(gtx, e, func() { full.at(gtx, 0, 0) })
	return D{Size: image.Pt(full.size.X, h)}
}

func (u *UI) layoutMessages(gtx C, c *model.Chat) D {
	u.pageMessages(c)
	rows := u.rows(c)
	// The typing bubble grows in and shrinks away like a message, instead
	// of popping in and out. Opening a chat shows it as it is.
	typing := c.Typing != "" && !u.conv.newerMore
	if u.conv.typingFor != c.ID {
		u.conv.typingFor = c.ID
		u.conv.typingAnim.snap(typing)
	}
	if typing {
		u.conv.typingWho = [2]string{c.Typing, c.TypingID}
	}
	// WhatsApp often says they stopped typing just before their message
	// arrives. Keep the bubble a moment, so the message can take its place
	// (see insertMessage) instead of the chat moving down and up again.
	shown := typing
	if typing || gtx.Now.IsZero() {
		u.conv.typingSeen = gtx.Now
	} else if left := typingGrace - gtx.Now.Sub(u.conv.typingSeen); left > 0 && u.conv.typingAnim.on {
		shown = true
		gtx.Execute(op.InvalidateCmd{At: gtx.Now.Add(left)})
	}
	typingV := u.conv.typingAnim.step(gtx, shown, durAppear)
	u.conv.typingH = 0
	msgRows := len(rows)
	if typingV > 0 {
		rows = append(rows[:len(rows):len(rows)], convRow{kind: rowTyping, first: true})
	}
	width := gtx.Constraints.Max.X
	margin := max(gtx.Dp(12), min(gtx.Dp(63), width*13/100))
	maxBubble := min(width*69/100, width-2*margin)

	if p := u.conv.scrollTo; p != nil {
		u.conv.list.Position = *p
		u.conv.list.Position.Offset -= gtx.Dp(u.conv.scrollAbove)
		u.conv.scrollTo, u.conv.scrollAbove = nil, 0
	}
	if g := &u.conv.glide; g.pending {
		g.pending, g.active = false, true
		g.from, g.start = float32(u.glideFrom(g.first, gtx.Constraints.Max.Y)), gtx.Now
	}
	if g := &u.conv.glide; g.active {
		t := float32(1)
		if !gtx.Now.IsZero() {
			t = min(1, float32(gtx.Now.Sub(g.start))/float32(durScroll))
		}
		// BeforeEnd keeps the list from snapping back to the newest message.
		u.conv.list.Position = layout.Position{First: g.first, Offset: int(g.from * (1 - easeInOut(t))), BeforeEnd: true}
		g.active = t < 1
		if g.active {
			gtx.Execute(op.InvalidateCmd{})
		}
	}
	if u.conv.heights == nil {
		u.conv.heights = make(map[int]int)
	}
	clear(u.conv.heights)
	gtx.Constraints.Min = gtx.Constraints.Max
	defer func() {
		if u.conv.scrollTo != nil {
			gtx.Execute(op.InvalidateCmd{}) // requested while laying out
		}
	}()
	return u.scrollList(gtx, &u.conv.list, len(rows), func(gtx C, i int) D {
		r := rows[i]
		in := layout.Inset{Left: dp(gtx, margin), Right: dp(gtx, margin)}
		switch {
		case r.kind == rowDate, r.kind == rowEncryption, r.kind == rowUnread:
			in.Top, in.Bottom = 10, 6
		case r.kind == rowTyping:
			// The newest message keeps its bottom space, so the gap above
			// the bubble doesn't jump as it grows in.
			in.Top, in.Bottom = 2, 8
		case r.first:
			in.Top = 10
		default:
			in.Top = 2
		}
		if i == 0 {
			in.Top += 10
		}
		if i == msgRows-1 {
			in.Bottom += 8
		}
		row := func(gtx C) D {
			return in.Layout(gtx, func(gtx C) D {
				gtx.Constraints.Min.X = gtx.Constraints.Max.X
				switch {
				case r.kind == rowDate:
					return layout.N.Layout(gtx, func(gtx C) D { return u.systemChip(gtx, r.date) })
				case r.kind == rowEncryption:
					return layout.N.Layout(gtx, func(gtx C) D { return u.encryptionNotice(gtx, maxBubble) })
				case r.kind == rowUnread:
					return layout.N.Layout(gtx, func(gtx C) D { return u.unreadChip(gtx, u.conv.unread.n) })
				case r.kind == rowTyping:
					return u.layoutTyping(gtx, c.IsGroup, margin, typingV)
				case r.kind == rowNote:
					return u.layoutNote(gtx, r.note, maxBubble)
				case r.album:
					return u.layoutAlbumRow(gtx, c, r, maxBubble, margin)
				case r.group != nil:
					return u.layoutStickerRow(gtx, c, r, maxBubble, margin)
				default:
					return u.layoutMessageRow(gtx, c, r, maxBubble, margin)
				}
			})
		}
		var dims D
		switch {
		case r.kind == rowNote && u.anims.running(animKey{id: r.note.id, tag: tagAppear}):
			dims = u.appearing(gtx, r.note.id, row, func(C, float32) {})
		case r.kind == rowMessage && u.anims.running(animKey{id: r.msg.ID, tag: tagAppear}):
			dims = u.appearing(gtx, r.msg.ID, row, func(gtx C, v float32) {
				// Where the typing row drew it: below the space the newest
				// message kept for it (see the insets above).
				defer op.Offset(image.Pt(margin, gtx.Dp(10))).Push(gtx.Ops).Pop()
				withOpacity(gtx, easeOut(v), func() { u.layoutTyping(gtx, c.IsGroup, margin, v) })
			})
		case r.kind == rowTyping && typingV < 1:
			dims = growRow(gtx, 0, easeOut(typingV), row)
		default:
			dims = row(gtx)
		}
		if r.kind == rowTyping {
			// With the newest message's bottom space, which goes to the
			// message that takes the bubble's place.
			u.conv.typingH = dims.Size.Y + gtx.Dp(8)
		}
		u.conv.heights[i] = dims.Size.Y
		return dims
	})
}

// layoutTyping draws the bubble with three bouncing dots that shows
// someone is typing, with their avatar in groups. v is how far it has
// grown in: the bubble pops up from its bottom corner as it does.
func (u *UI) layoutTyping(gtx C, group bool, margin int, v float32) D {
	p := u.pal
	w, h := gtx.Dp(58), gtx.Dp(34)
	defer pushFx(gtx, 1, scaleAt(image.Pt(0, h), lerp(0.6, 1, easeOutBack(v)))).Pop()
	u.paintBubble(gtx, w, h, p.BubbleIn, false, true)
	// Each dot rises and falls in turn, then all rest: a wave.
	const period, step, rise = 1300 * time.Millisecond, 160 * time.Millisecond, 520 * time.Millisecond
	var t time.Duration
	if !gtx.Now.IsZero() {
		t = time.Duration(gtx.Now.UnixNano())
		gtx.Execute(op.InvalidateCmd{At: gtx.Now.Add(time.Second / 30)})
	}
	r, gap, amp := gtx.Dp(3.5), gtx.Dp(5), float32(gtx.Dp(4))
	x0 := (w - 6*r - 2*gap) / 2
	for i := range 3 {
		ph := (t - time.Duration(i)*step) % period
		if ph < 0 {
			ph += period
		}
		y := 0
		if ph < rise {
			y = int(amp * float32(math.Sin(math.Pi*float64(ph)/float64(rise))))
		}
		fillCircle(gtx, image.Pt(x0+r+i*(2*r+gap), h/2-y), r, p.MetaIn)
	}
	if group {
		t := op.Offset(image.Pt(-min(gtx.Dp(40), margin), 0)).Push(gtx.Ops)
		u.avatar(gtx, u.conv.typingWho[1], u.conv.typingWho[0], false, 29)
		t.Pop()
	}
	return D{Size: image.Pt(gtx.Constraints.Max.X, h)}
}

// layoutMessageRow draws one message with its interactions: the hover
// chevron and right-click menu, selection, and the flash after jumping to
// it from a reply.
func (u *UI) layoutMessageRow(gtx C, c *model.Chat, r convRow, maxW, margin int) D {
	p := u.pal
	m := r.msg
	w := gtx.Constraints.Max.X
	sel := u.conv.selecting
	ann := u.announcementsOf(c) != nil
	if ann {
		maxW = annWidth(gtx, w)
	}
	rowKey := "row:" + m.ID
	if sel && u.btn(rowKey).Clicked(gtx) {
		if u.conv.picked[m.ID] {
			delete(u.conv.picked, m.ID)
		} else {
			u.conv.picked[m.ID] = true
		}
	}
	// Select mode moves incoming bubbles over for the checkboxes.
	selV := easeOut(u.conv.selV)
	shift := 0
	if !m.FromMe && !ann {
		shift = int(float32(max(0, gtx.Dp(44)-margin)) * selV)
	}
	cgtx := gtx
	cgtx.Constraints = layout.Constraints{Max: image.Pt(w-shift, gtx.Constraints.Max.Y)}
	bubble := record(cgtx, func(gtx C) D { return u.layoutMessage(gtx, c, r, maxW) })
	cardH := u.conv.cardH
	x := shift
	switch {
	case ann:
		x = (w - bubble.size.X) / 2 // down the middle, whoever sent it
	case m.FromMe:
		x = w - bubble.size.X
	}
	h := bubble.size.Y
	band := image.Rect(-margin, -gtx.Dp(2), w+margin, h+gtx.Dp(2))
	if u.conv.flash == m.ID {
		u.drawFlash(gtx, band)
	}
	if sel && u.conv.picked[m.ID] {
		fillRect(gtx, band, argb(0x5dbf6e, 0x26))
	}
	bubble.at(gtx, x, 0)
	if c.IsGroup && r.first && !m.FromMe && !ann {
		// The sender's avatar sits in the left margin, level with the bubble.
		sz := gtx.Dp(29)
		t := op.Offset(image.Pt(x-min(gtx.Dp(40), margin), 0)).Push(gtx.Ops)
		u.avatar(gtx, m.SenderID, m.Sender, false, dp(gtx, sz))
		u.senderButton(gtx, m, image.Point{}, image.Pt(sz, sz))
		t.Pop()
	}
	if !sel {
		t := op.Offset(image.Pt(x, 0)).Push(gtx.Ops)
		hovered := u.hoverArea(gtx, m.ID, bubble.size)
		// A double click beside the bubble replies too, as in WhatsApp; a
		// right click only on it opens its menu.
		right, at, double := u.pressArea(gtx, m.ID, image.Rect(-margin-x, 0, w+margin-x, h))
		if right && !at.In(image.Rectangle{Max: bubble.size}) {
			right = false
		}
		if right {
			u.openMessageMenu(m)
		}
		// A double click on the text selects a word instead.
		if double && m.Kind != model.KindDeleted && (u.textSel.id != m.ID || u.textSel.clicks < 2) && u.sendBlocked(c) == "" {
			u.startReply(m)
		}
		chev := u.btn("chev:" + m.ID)
		if chev.Clicked(gtx) {
			u.openMessageMenu(m)
		}
		// The chevron covers part of the bubble's hover area, so it keeps
		// itself visible while it is hovered. It fades and slides in.
		show := (hovered || chev.Hovered() || (u.ctx.isOpen() && u.ctx.msg == m)) && m.Kind != model.KindSticker
		if cv := smooth(u.anims.fade(gtx, animKey{p: chev, tag: tagShow}, show, durHoverIn, durHoverOut)); cv > 0 {
			bg := p.BubbleIn
			if m.FromMe {
				bg = p.BubbleOut
			}
			fg := p.MetaIn
			if m.Kind == model.KindImage && !ann {
				bg, fg = argb(0x000000, 0x60), rgb(0xffffff)
			}
			ct := op.Offset(image.Pt(bubble.size.X-gtx.Dp(26+5), gtx.Dp(4))).Push(gtx.Ops)
			fx := pushFx(gtx, cv, moveBy(float32(gtx.Dp(6))*(1-cv), 0))
			u.chevronButton(gtx, chev, bg, fg)
			fx.Pop()
			ct.Pop()
		}
		if ann && m.Kind != model.KindDeleted && m.Kind != model.KindUnsupported && m.Kind != model.KindSticker {
			// Announcements offer a forward button beside them, when there's room.
			if bx := bubble.size.X + gtx.Dp(6); x+bx+gtx.Dp(34) <= w+margin {
				ft := op.Offset(image.Pt(bx, 0)).Push(gtx.Ops)
				u.annForwardButton(gtx, m, cardH)
				ft.Pop()
			}
		}
		t.Pop()
	}
	if selV > 0 {
		// The whole row toggles; a checkbox sits in the left margin.
		t := op.Offset(image.Pt(-margin, 0)).Push(gtx.Ops)
		if sel {
			rg := gtx
			rg.Constraints = layout.Exact(image.Pt(w+2*margin, h))
			clickable(rg, u.btn(rowKey), func(gtx C) D { return D{Size: gtx.Constraints.Max} })
		}
		box, col := icCheckBoxEmpty, p.TextSecondary
		if u.conv.picked[m.ID] {
			box, col = icCheckBox, p.Green
		}
		bt := op.Offset(image.Pt(gtx.Dp(12), gtx.Dp(6))).Push(gtx.Ops)
		withOpacity(gtx, selV, func() { drawIcon(gtx, box, 24, col) })
		bt.Pop()
		t.Pop()
	}
	return D{Size: image.Pt(w, h)}
}

// drawFlash highlights band, the row of the message just jumped to. The
// highlight fades in quickly and out slowly.
func (u *UI) drawFlash(gtx C, band image.Rectangle) {
	const in, out = 200 * time.Millisecond, 600 * time.Millisecond
	left := u.conv.flashUntil.Sub(u.now())
	if left <= 0 {
		u.conv.flash = ""
		return
	}
	a := min(1, float32(flashTime-left)/float32(in), float32(left)/float32(out))
	fillRect(gtx, band, faded(argb(0x5dbf6e, 0x30), smooth(a)))
	if left > out && flashTime-left > in {
		gtx.Execute(op.InvalidateCmd{At: u.conv.flashUntil.Add(-out)})
	} else {
		gtx.Execute(op.InvalidateCmd{})
	}
}

func (u *UI) systemChip(gtx C, txt string) D {
	p := u.pal
	gtx.Constraints.Min = image.Point{}
	return u.card(gtx, 8, p.DateChip, func(gtx C) D {
		return layout.Inset{Left: 11, Right: 11, Top: 4, Bottom: 5}.Layout(gtx,
			u.label(13, txt, p.DateChipText, labelOpts{weight: font.Medium, maxLines: 1}).Layout)
	})
}

func (u *UI) encryptionNotice(gtx C, maxW int) D {
	p := u.pal
	gtx.Constraints.Max.X = min(gtx.Constraints.Max.X, max(maxW, gtx.Dp(300)))
	gtx.Constraints.Min.X = 0
	return u.card(gtx, 8, p.Encryption, func(gtx C) D {
		return layout.Inset{Left: 12, Right: 12, Top: 6, Bottom: 7}.Layout(gtx, func(gtx C) D {
			l := u.label(12.5, "🔒 Messages and calls are end-to-end encrypted. Only people in this chat can read, listen to, or share them.",
				p.EncryptionText, labelOpts{align: text.Middle})
			l.MaxLines = 0
			return l.Layout(gtx)
		})
	})
}

// card draws w on a rounded rectangle. Light mode adds WhatsApp's 1px shadow.
func (u *UI) card(gtx C, radius unit.Dp, bg color.NRGBA, w layout.Widget) D {
	m := op.Record(gtx.Ops)
	dims := w(gtx)
	call := m.Stop()
	r := gtx.Dp(radius)
	rect := image.Rectangle{Max: dims.Size}
	if !u.dark {
		fillRRect(gtx, rect.Add(image.Pt(0, 1)), r, argb(0x0b141a, 0x21))
	}
	fillRRect(gtx, rect, r, bg)
	call.Add(gtx.Ops)
	return dims
}

// nbspWidth returns the advance of a non-breaking space at the given size.
// Bubbles pad their text with NBSPs so the timestamp can sit on the last line
// when it fits (the same trick WhatsApp Web uses with an inline spacer).
func (u *UI) nbspWidth(gtx C, size unit.Sp) float32 {
	px := gtx.Sp(size)
	if w, ok := u.conv.nbsp[px]; ok {
		return w
	}
	const n = 20
	measure := func(s string) int {
		m := op.Record(gtx.Ops)
		gtx := gtx
		gtx.Constraints = layout.Constraints{Max: image.Pt(1<<20, 1<<20)}
		d := u.label(size, s, color.NRGBA{}).Layout(gtx)
		m.Stop()
		return d.Size.X
	}
	spaces := make([]rune, n)
	for i := range spaces {
		spaces[i] = '\u00a0'
	}
	w := float32(measure("x"+string(spaces)+"x")-measure("xx")) / n
	if w <= 0 {
		w = float32(gtx.Sp(size)) * 0.27
	}
	if u.conv.nbsp == nil {
		u.conv.nbsp = make(map[int]float32)
	}
	u.conv.nbsp[px] = w
	return w
}

func (u *UI) layoutMeta(gtx C, m *model.Message, col color.NRGBA, tickCol *color.NRGBA) D {
	gtx.Constraints.Min = image.Point{}
	var children []layout.FlexChild
	if m.Starred {
		children = append(children,
			layout.Rigid(iconW(icStarFill, 14, col)),
			layout.Rigid(layout.Spacer{Width: 3}.Layout))
	}
	if !m.Edited.IsZero() && m.Kind != model.KindDeleted {
		children = append(children,
			layout.Rigid(u.label(12.5, "Edited", col).Layout),
			layout.Rigid(layout.Spacer{Width: 4}.Layout))
	}
	children = append(children, layout.Rigid(u.label(12.5, m.Time.Format("15:04"), col).Layout))
	if m.FromMe && m.Kind != model.KindDeleted {
		ic, tc := receiptIcon(m.Receipt, u.pal, true)
		if m.Receipt != model.Read && m.Receipt != model.Failed {
			tc = col
			if tickCol != nil {
				tc = *tickCol
			}
		}
		children = append(children,
			layout.Rigid(layout.Spacer{Width: 3}.Layout),
			layout.Rigid(iconW(ic, 17, tc)),
		)
	}
	return layout.Flex{Alignment: layout.Middle}.Layout(gtx, children...)
}

type part struct {
	call op.CallOp
	size image.Point
}

func record(gtx C, w layout.Widget) part {
	m := op.Record(gtx.Ops)
	d := w(gtx)
	return part{m.Stop(), d.Size}
}

func (p part) at(gtx C, x, y int) {
	t := op.Offset(image.Pt(x, y)).Push(gtx.Ops)
	p.call.Add(gtx.Ops)
	t.Pop()
}

// layoutMessage draws a bubble (or a sticker) plus its reaction pill.
func (u *UI) layoutMessage(gtx C, c *model.Chat, r convRow, maxW int) D {
	m := r.msg
	// A reaction that changes while the chat is open pops in.
	pop := animKey{id: m.ID, tag: tagPop}
	if old, ok := u.conv.reactions[m.ID]; !ok || old != m.Reaction {
		if ok && m.Reaction != "" {
			u.anims.start(pop)
		}
		if u.conv.reactions == nil {
			u.conv.reactions = make(map[string]string)
		}
		u.conv.reactions[m.ID] = m.Reaction
	}
	var dims D
	switch {
	case m.Kind == model.KindSticker && u.announcementsOf(c) != nil:
		dims = u.layoutAnnSticker(gtx, m, maxW)
	case m.Kind == model.KindSticker:
		dims = u.layoutStickerMessage(gtx, c, m, r.first, maxW)
	default:
		dims = u.layoutBubble(gtx, c, m, r.first, maxW)
	}
	u.conv.cardH = dims.Size.Y
	if m.Reaction == "" {
		return dims
	}
	p := u.pal
	bg := p.BubbleIn
	pill := record(gtx, func(gtx C) D {
		gtx.Constraints.Min = image.Point{}
		return u.card(gtx, 13, bg, func(gtx C) D {
			return layout.Inset{Left: 6, Right: 6, Top: 2, Bottom: 3}.Layout(gtx, u.label(14, m.Reaction, p.Text).Layout)
		})
	})
	x := gtx.Dp(8)
	if m.FromMe {
		x = dims.Size.X - pill.size.X - gtx.Dp(8)
	}
	ring := gtx.Dp(2)
	py := dims.Size.Y - gtx.Dp(5)
	if u.announcementsOf(c) != nil {
		py = dims.Size.Y + gtx.Dp(3) // under the card, not on it
	}
	fx := fxStack{}
	if u.anims.running(pop) {
		v := u.anims.fade(gtx, pop, true, 380*time.Millisecond, 0)
		if v >= 1 {
			u.anims.stop(pop)
		}
		mid := image.Pt(x+pill.size.X/2, py+pill.size.Y/2)
		fx = pushFx(gtx, min(1, 3*v), scaleAt(mid, lerp(0.3, 1, easeOutBack(v))))
	}
	fillRRect(gtx, image.Rect(x-ring, py-ring, x+pill.size.X+ring, py+pill.size.Y+ring), pill.size.Y/2+ring, p.ChatBg)
	pill.at(gtx, x, py)
	fx.Pop()
	dims.Size.Y = py + pill.size.Y + ring
	return dims
}

// layoutBubble draws one message bubble, sized to its content.
func (u *UI) layoutBubble(gtx C, c *model.Chat, m *model.Message, tail bool, maxW int) D {
	p := u.pal
	out := m.FromMe
	bg, quoteBg, textCol, metaCol, secondary := p.BubbleIn, p.QuoteIn, p.Text, p.MetaIn, p.TextSecondary
	if out {
		bg, quoteBg, textCol, metaCol, secondary = p.BubbleOut, p.QuoteOut, p.TextOut, p.MetaOut, p.SecondaryOut
	}
	isImg := m.Kind == model.KindImage
	footerText, buttons := m.Footer, m.Buttons
	if m.Kind == model.KindDeleted || m.Kind == model.KindUnsupported {
		footerText, buttons = "", nil
	}
	for i := range buttons {
		if u.btn("mbtn:" + m.ID + ":" + itoa(i)).Clicked(gtx) {
			u.pressButton(m, i)
		}
	}

	// An announcement's card is as wide as it may be, headed by its sender.
	ann := u.announcementsOf(c) != nil
	if ann {
		tail = false
	}
	padL, padR, padT, padB := gtx.Dp(9), gtx.Dp(8), gtx.Dp(6), gtx.Dp(8)
	if isImg {
		padL, padR, padT, padB = gtx.Dp(3), gtx.Dp(3), gtx.Dp(3), gtx.Dp(3)
	}
	inner := maxW - padL - padR
	cgtx := gtx
	cgtx.Constraints = layout.Constraints{Max: image.Pt(inner, 1<<20)}

	// Timestamp and receipt ticks.
	metaOnImage := isImg && m.Text == "" && footerText == ""
	var tickCol *color.NRGBA
	if metaOnImage {
		white := rgb(0xffffff)
		metaCol, tickCol = white, &white
	}
	meta := record(cgtx, func(gtx C) D { return u.layoutMeta(gtx, m, metaCol, tickCol) })

	// Measure natural widths first; the quote and image stretch to the widest part.
	contentW := 0
	var sender, body part
	hasSender := c.IsGroup && !out && tail && m.Sender != ""
	if hasSender {
		col := p.Senders[hashIndex(m.SenderID+m.Sender, len(p.Senders))]
		sender = record(cgtx, u.label(13, m.Sender, col, labelOpts{weight: font.SemiBold, maxLines: 1}).Layout)
		contentW = max(contentW, sender.size.X)
	}
	var head part
	headX := gtx.Dp(9) - padL // the header lines up with text
	if ann && (out || m.Sender != "") {
		hgtx := cgtx
		hgtx.Constraints.Max.X = inner - 2*headX
		hgtx.Constraints.Min.X = hgtx.Constraints.Max.X
		head = record(hgtx, func(gtx C) D { return u.layoutAnnHeader(gtx, m) })
	}
	if ann {
		contentW = inner
	}

	imgW, imgH := 0, 0
	textInset := 0 // horizontal inset of text inside image bubbles
	var img *imgEntry
	if isImg {
		imgW = min(inner, gtx.Dp(330))
		maxPx := gtx.Dp(330)
		if ann {
			imgW, maxPx = inner, inner
		}
		imgH = imgW * 3 / 4
		img = u.messageImage(m, maxPx)
		if img != nil && img.state == imgReady {
			ratio := float32(img.size.Y) / float32(img.size.X)
			imgH = int(float32(imgW) * min(max(ratio, 0.4), 1.4))
		}
		contentW = max(contentW, imgW)
		textInset = gtx.Dp(6)
	}

	const textSize = unit.Sp(15.7)
	text := m.Text
	italic := false
	var lead layout.Widget // icon before the text (media types, deleted)
	switch {
	case m.Kind == model.KindDeleted:
		text, textCol, italic = "This message was deleted", secondary, true
		if out {
			text = "You deleted this message"
		}
		lead = iconW(icBlock, 19, secondary)
	case m.Kind == model.KindUnsupported:
		text, textCol, italic = "This message couldn't load. Open the message on your phone to view it.", secondary, true
		lead = iconW(icUnsupported, 19, secondary)
	case hasAttachment(m):
		text = attachmentCaption(m) // under the document card or player
	case !isImg && m.Media != model.MediaNone:
		lead = iconW(mediaIcon(m.Media), 20, secondary)
		text = mediaLabel(m)
		if m.Media == model.MediaVoice {
			text = "Voice message · " + mediaLabel(m)
		}
	}
	var att part // document card or audio player
	if hasAttachment(m) {
		cols := bubbleColors{bg: bg, card: quoteBg, text: textCol, secondary: secondary}
		att = record(cgtx, func(gtx C) D { return u.layoutAttachment(gtx, c, m, inner, meta.size.X, meta.size.Y, cols) })
		contentW = max(contentW, att.size.X)
	}
	leadW := 0
	if lead != nil {
		leadW = gtx.Dp(25)
	}
	if text != "" {
		nbsp := u.nbspWidth(gtx, textSize)
		spacer := make([]rune, int(float32(meta.size.X+gtx.Dp(8))/nbsp)+1)
		for i := range spacer {
			spacer[i] = '\u00a0'
		}
		prefix := ""
		if leadW > 0 {
			indent := make([]rune, int(float32(leadW)/nbsp)+1)
			for i := range indent {
				indent[i] = '\u00a0'
			}
			prefix = string(indent)
		}
		tgtx := cgtx
		if isImg {
			tgtx.Constraints.Max.X = imgW - 2*textInset
		}
		suffix := ""
		if footerText == "" {
			// The meta sits at the end of the last line, or of the footer.
			suffix = " " + string(spacer)
		}
		o := richOpts{italic: italic, prefix: prefix, suffix: suffix}
		if lead == nil {
			if !u.conv.selecting {
				o.sel, o.links = m.ID, m.ID
			}
			if !out {
				o.pills = pillMe
				if c.IsGroup && strings.ContainsRune(text, model.MentionAdmins) && u.amAdmin(c.ID) {
					o.pills |= pillAdmin
				}
			}
			more := u.btn("more:" + m.ID)
			if more.Clicked(gtx) {
				if u.conv.expanded == nil {
					u.conv.expanded = make(map[string]int)
				}
				u.conv.expanded[m.ID]++
			}
			if cut, ok := readMore(text, u.conv.expanded[m.ID]); ok {
				text, o.more = cut, more
			}
		}
		body = record(tgtx, func(gtx C) D {
			return u.layoutRich(gtx, text, textSize, textCol, secondary, o)
		})
		contentW = max(contentW, body.size.X+2*textInset)
	} else if !isImg {
		contentW = max(contentW, meta.size.X)
	}

	// A business message's footer, with the meta at its end.
	var footer part
	if footerText != "" {
		const size = unit.Sp(13)
		nbsp := u.nbspWidth(gtx, size)
		spacer := strings.Repeat(" ", int(float32(meta.size.X+gtx.Dp(8))/nbsp)+1)
		plain := font.Font{Typeface: typeface}
		st := styledtext.Text(u.th.Shaper,
			styledtext.SpanStyle{Font: plain, Size: size, Color: secondary, Content: footerText},
			styledtext.SpanStyle{Font: plain, Size: size, Color: secondary, Content: " " + spacer})
		st.LineHeight, st.LineHeightScale = 18, 1
		fgtx := cgtx
		if isImg {
			fgtx.Constraints.Max.X = imgW - 2*textInset
		}
		footer = record(fgtx, func(gtx C) D { return st.Layout(gtx, nil) })
		contentW = max(contentW, footer.size.X+2*textInset)
	}

	// Buttons span the bubble below its content, one per row.
	btnH := gtx.Dp(44)
	var btnLabels []part
	for _, b := range buttons {
		lgtx := cgtx
		lgtx.Constraints.Max.X = max(0, inner-gtx.Dp(26))
		lb := record(lgtx, u.label(15, b.Label, p.BubbleButton, labelOpts{weight: font.Medium, maxLines: 1}).Layout)
		btnLabels = append(btnLabels, lb)
		contentW = max(contentW, min(inner, lb.size.X+gtx.Dp(26+32)))
	}
	if len(buttons) > 0 {
		contentW = max(contentW, min(inner, gtx.Dp(240)))
	}

	var fwd part
	if m.Forwarded && m.Kind != model.KindDeleted {
		fwd = record(cgtx, func(gtx C) D {
			return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(iconW(icForward, 16, secondary)),
				layout.Rigid(layout.Spacer{Width: 4}.Layout),
				layout.Rigid(u.label(13, "Forwarded", secondary, labelOpts{italic: true, maxLines: 1}).Layout),
			)
		})
		contentW = max(contentW, fwd.size.X+2*textInset)
	}

	var quote part
	if m.Quote != nil {
		qm := u.quotedMessage(m.Quote)
		qw := min(inner, max(contentW, gtx.Dp(180)))
		quote = record(cgtx, func(gtx C) D { return u.layoutQuote(gtx, m.Quote, quoteBg, secondary, qw, qm) })
		contentW = max(contentW, quote.size.X)
		if quote.size.X < contentW {
			quote = record(cgtx, func(gtx C) D { return u.layoutQuote(gtx, m.Quote, quoteBg, secondary, contentW, qm) })
		}
	}
	if u.btn("quote:"+m.ID).Clicked(gtx) && m.Quote != nil && m.Quote.ID != "" {
		u.jumpTo(m.Quote.ID)
	}
	if u.btn("img:" + m.ID).Clicked(gtx) {
		u.openViewer(m)
	}

	// Place everything, then paint the bubble behind it.
	macro := op.Record(gtx.Ops)
	y := 0
	if head.size.Y > 0 {
		top := gtx.Dp(12) - padT
		head.at(gtx, headX, top)
		y = top + head.size.Y + gtx.Dp(6)
		if isImg {
			y += gtx.Dp(5)
		}
	}
	if hasSender {
		sx := 0
		if isImg {
			sx = textInset
			y += gtx.Dp(3)
		}
		sender.at(gtx, sx, y)
		u.senderButton(gtx, m, image.Pt(sx, y), sender.size)
		y += sender.size.Y + gtx.Dp(2)
		if isImg {
			y += gtx.Dp(3)
		}
	}
	if m.Forwarded && m.Kind != model.KindDeleted {
		fx := 0
		if isImg {
			fx = textInset
			y += gtx.Dp(2)
		}
		fwd.at(gtx, fx, y)
		y += fwd.size.Y + gtx.Dp(3)
	}
	if m.Quote != nil {
		quote.at(gtx, 0, y)
		func() {
			t := op.Offset(image.Pt(0, y)).Push(gtx.Ops)
			defer t.Pop()
			qg := gtx
			qg.Constraints = layout.Exact(quote.size)
			clickable(qg, u.btn("quote:"+m.ID), func(gtx C) D { return D{Size: quote.size} })
		}()
		y += quote.size.Y + gtx.Dp(5)
	}
	if att.size.Y > 0 {
		att.at(gtx, 0, y)
		y += att.size.Y
		if text == "" && footerText == "" {
			meta.at(gtx, contentW-meta.size.X, y-meta.size.Y)
		} else {
			y += gtx.Dp(4)
		}
	}
	if isImg {
		u.layoutImage(gtx, image.Rect(0, y, imgW, y+imgH), m, img)
		func() {
			t := op.Offset(image.Pt(0, y)).Push(gtx.Ops)
			defer t.Pop()
			ig := gtx
			ig.Constraints = layout.Exact(image.Pt(imgW, imgH))
			clickable(ig, u.btn("img:"+m.ID), func(gtx C) D { return D{Size: gtx.Constraints.Max} })
		}()
		y += imgH
		if metaOnImage {
			meta.at(gtx, imgW-meta.size.X-gtx.Dp(7), y-meta.size.Y-gtx.Dp(5))
		} else {
			y += gtx.Dp(5)
		}
	}
	if text != "" {
		body.at(gtx, textInset, y)
		if lead != nil {
			t := op.Offset(image.Pt(textInset, y)).Push(gtx.Ops)
			lead(gtx)
			t.Pop()
		}
		y += body.size.Y
		if footerText == "" {
			meta.at(gtx, contentW-meta.size.X-textInset, y-meta.size.Y+gtx.Dp(4))
			if isImg {
				y += gtx.Dp(5)
			}
		}
	}
	switch {
	case footerText != "":
		y += gtx.Dp(2)
		footer.at(gtx, textInset, y)
		y += footer.size.Y
		meta.at(gtx, contentW-meta.size.X-textInset, y-meta.size.Y+gtx.Dp(3))
		if isImg {
			y += gtx.Dp(5)
		}
	case text == "" && !isImg && att.size.Y == 0:
		meta.at(gtx, contentW-meta.size.X, y)
		y += meta.size.Y
	}
	w := contentW + padL + padR
	if len(buttons) > 0 {
		y += padB
		line := max(1, gtx.Dp(1))
		for i, lb := range btnLabels {
			top := y + i*btnH
			fillRect(gtx, image.Rect(-padL, top, w-padL, top+line), p.BubbleLine)
			ic := icReply
			switch buttons[i].Kind {
			case model.ButtonURL:
				ic = icOpenInNew
			case model.ButtonCopy:
				ic = icCopy
			}
			if h := u.hover(gtx, u.btn("mbtn:"+m.ID+":"+itoa(i))); h > 0 {
				rr := clip.RRect{Rect: image.Rect(-padL, top+line, w-padL, top+btnH)}
				if i == len(btnLabels)-1 {
					rr.SE, rr.SW = gtx.Dp(8), gtx.Dp(8) // the bubble's corners
				}
				paintRRect(gtx, rr, faded(p.BubbleLine, h))
			}
			rowW := lb.size.X + gtx.Dp(26)
			x := -padL + (w-rowW)/2
			it := op.Offset(image.Pt(x, top+(btnH-gtx.Dp(20))/2)).Push(gtx.Ops)
			drawIcon(gtx, ic, 20, p.BubbleButton)
			it.Pop()
			lb.at(gtx, x+gtx.Dp(26), top+(btnH-lb.size.Y)/2)
			func() {
				t := op.Offset(image.Pt(-padL, top)).Push(gtx.Ops)
				defer t.Pop()
				bg := gtx
				bg.Constraints = layout.Exact(image.Pt(w, btnH))
				clickable(bg, u.btn("mbtn:"+m.ID+":"+itoa(i)), func(gtx C) D { return D{Size: gtx.Constraints.Max} })
			}()
		}
		y += len(buttons)*btnH - padB
	}
	content := macro.Stop()

	h := y + padT + padB
	u.paintBubble(gtx, w, h, bg, out, tail)
	t := op.Offset(image.Pt(padL, padT)).Push(gtx.Ops)
	content.Add(gtx.Ops)
	t.Pop()
	return D{Size: image.Pt(w, h)}
}

// paintBubble paints the bubble body and its tail (plus a 1px shadow in light mode).
// senderButton makes a group message's sender name (or picture) open
// their contact info.
func (u *UI) senderButton(gtx C, m *model.Message, at, size image.Point) {
	if m.SenderID == "" || u.conv.selecting {
		return
	}
	c := u.btn("sender:" + m.ID)
	if c.Clicked(gtx) {
		u.openContact(m.SenderID, m.Sender)
	}
	t := op.Offset(at).Push(gtx.Ops)
	sg := gtx
	sg.Constraints = layout.Exact(size)
	clickable(sg, c, func(gtx C) D { return D{Size: size} })
	t.Pop()
}

func (u *UI) paintBubble(gtx C, w, h int, bg color.NRGBA, out, tail bool) {
	r := gtx.Dp(8)
	rr := clip.RRect{Rect: image.Rect(0, 0, w, h), NW: r, NE: r, SW: r, SE: r}
	if tail {
		if out {
			rr.NE = 0
		} else {
			rr.NW = 0
		}
	}
	if !u.dark {
		shadow := rr
		shadow.Rect = shadow.Rect.Add(image.Pt(0, 1))
		paintRRect(gtx, shadow, argb(0x0b141a, 0x21))
	}
	paintRRect(gtx, rr, bg)
	if !tail {
		return
	}
	// The tail is a small curved triangle hanging off the top corner.
	tw, th := float32(gtx.Dp(8)), float32(gtx.Dp(13))
	var path clip.Path
	path.Begin(gtx.Ops)
	if out {
		x := float32(w)
		path.MoveTo(f32.Pt(x-1, 0))
		path.LineTo(f32.Pt(x+tw-2, 0))
		path.QuadTo(f32.Pt(x+tw, 0), f32.Pt(x+tw-1.2, 1.6))
		path.LineTo(f32.Pt(x, th))
		path.LineTo(f32.Pt(x-1, th))
	} else {
		path.MoveTo(f32.Pt(1, 0))
		path.LineTo(f32.Pt(-tw+2, 0))
		path.QuadTo(f32.Pt(-tw, 0), f32.Pt(-tw+1.2, 1.6))
		path.LineTo(f32.Pt(0, th))
		path.LineTo(f32.Pt(1, th))
	}
	path.Close()
	paint.FillShape(gtx.Ops, bg, clip.Outline{Path: path.End()}.Op())
}

// layoutQuote draws a quoted message: a colored bar, the author and a
// snippet, plus the quoted picture's thumbnail when it's loaded (qm).
func (u *UI) layoutQuote(gtx C, q *model.Quote, bg, secondary color.NRGBA, width int, qm *model.Message) D {
	p := u.pal
	name := q.Sender
	if name == "" {
		name = "You"
	}
	name = plainText(name)
	col := p.Senders[hashIndex(name, len(p.Senders))]
	if name == "You" {
		col = p.Green
	}
	txt := q.Text
	if q.Media != model.MediaNone {
		txt = mediaLabel(&model.Message{Media: q.Media, Text: q.Text})
	}
	gtx.Constraints.Min.X = width
	gtx.Constraints.Max.X = width
	m := op.Record(gtx.Ops)
	right := unit.Dp(10)
	if qm != nil && qm.Kind == model.KindImage {
		right = 66 // room for the thumbnail
	}
	dims := layout.Inset{Left: 12, Right: right, Top: 6, Bottom: 7}.Layout(gtx, func(gtx C) D {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(u.label(13, name, col, labelOpts{weight: font.SemiBold, maxLines: 1}).Layout),
			layout.Rigid(layout.Spacer{Height: 1}.Layout),
			layout.Rigid(func(gtx C) D {
				children := []layout.FlexChild{}
				if ic := mediaIcon(q.Media); ic != nil {
					children = append(children, layout.Rigid(func(gtx C) D {
						return layout.Inset{Right: 4}.Layout(gtx, iconW(ic, 16, secondary))
					}))
				}
				children = append(children, layout.Flexed(1, u.label(13.5, plainText(txt), secondary, labelOpts{maxLines: 2}).Layout))
				return layout.Flex{Alignment: layout.Middle}.Layout(gtx, children...)
			}),
		)
	})
	call := m.Stop()
	dims.Size.X = width
	r := gtx.Dp(7)
	defer clip.UniformRRect(image.Rectangle{Max: dims.Size}, r).Push(gtx.Ops).Pop()
	fillRect(gtx, image.Rectangle{Max: dims.Size}, bg)
	fillRect(gtx, image.Rect(0, 0, gtx.Dp(4), dims.Size.Y), col)
	call.Add(gtx.Ops)
	if qm != nil && qm.Kind == model.KindImage {
		s := dims.Size.Y
		tr := image.Rect(width-s, 0, width, s)
		if img := u.messageImage(qm, s*2); img != nil && img.state == imgReady {
			paintCover(gtx, img.op, img.size, tr)
		} else if qm.ImageA != 0 || qm.ImageB != 0 {
			u.gradientImage(gtx, tr, qm.ImageA, qm.ImageB)
		}
	}
	return dims
}

// messageImage returns the best available picture for an image message:
// the downloaded media, or the embedded thumbnail while that loads.
func (u *UI) messageImage(m *model.Message, maxPx int) *imgEntry {
	b := u.backend
	if m.Media == model.MediaImage || m.Media == model.MediaSticker {
		full := u.images.get("m:"+m.ChatID+"/"+m.ID, maxPx, func() []byte { return b.MediaData(m.ChatID, m.ID) })
		if full.state == imgReady {
			return full
		}
	}
	if len(m.Thumb) > 0 {
		thumb := m.Thumb
		return u.images.get("t:"+m.ChatID+"/"+m.ID, maxPx, func() []byte { return thumb })
	}
	return nil
}

// layoutImage draws a picture preview in r: the image, the demo gradient,
// or a neutral placeholder. Videos get a play button and duration.
func (u *UI) layoutImage(gtx C, r image.Rectangle, m *model.Message, img *imgEntry) {
	func() {
		defer clip.UniformRRect(r, gtx.Dp(6)).Push(gtx.Ops).Pop()
		switch {
		case img != nil && img.state == imgReady:
			paintCover(gtx, img.op, img.size, r)
		case m.ImageA != 0 || m.ImageB != 0:
			u.gradientImage(gtx, r, m.ImageA, m.ImageB)
		default:
			fillRect(gtx, r, u.pal.Hover)
			isz := gtx.Dp(48)
			t := op.Offset(r.Min.Add(r.Size().Div(2)).Sub(image.Pt(isz/2, isz/2))).Push(gtx.Ops)
			drawIcon(gtx, icImage, 48, u.pal.TextSecondary)
			t.Pop()
		}
	}()
	if m.Media != model.MediaVideo && m.Media != model.MediaGIF {
		return
	}
	playButton(gtx, r.Min.Add(r.Size().Div(2)), gtx.Dp(26), argb(0x000000, 0x80))
	if m.Duration > 0 {
		d := record(gtx, u.label(11.5, fmt.Sprintf("%d:%02d", m.Duration/60, m.Duration%60), rgb(0xffffff)).Layout)
		d.at(gtx, r.Min.X+gtx.Dp(8), r.Max.Y-d.size.Y-gtx.Dp(6))
	}
}

// playButton draws a round play button centered on c.
func playButton(gtx C, c image.Point, rad int, bg color.NRGBA) {
	fillCircle(gtx, c, rad, bg)
	var tri clip.Path
	tri.Begin(gtx.Ops)
	s := float32(rad) * 0.45
	cf := f32.Pt(float32(c.X), float32(c.Y))
	tri.MoveTo(cf.Add(f32.Pt(-s*0.6, -s)))
	tri.LineTo(cf.Add(f32.Pt(s, 0)))
	tri.LineTo(cf.Add(f32.Pt(-s*0.6, s)))
	tri.Close()
	paint.FillShape(gtx.Ops, rgb(0xffffff), clip.Outline{Path: tri.End()}.Op())
}

// quotedMessage returns the loaded message a reply quotes, or nil.
func (u *UI) quotedMessage(q *model.Quote) *model.Message {
	for _, x := range u.msgs {
		if x.ID == q.ID {
			return x
		}
	}
	return nil
}

// layoutStickerMessage draws a sticker. A sticker that replies to a
// message sits in a bubble, under the quote and above its time, like
// WhatsApp.
func (u *UI) layoutStickerMessage(gtx C, c *model.Chat, m *model.Message, tail bool, maxW int) D {
	if m.Quote == nil {
		return u.layoutSticker(gtx, m)
	}
	p := u.pal
	out := m.FromMe
	bg, quoteBg, metaCol, secondary := p.BubbleIn, p.QuoteIn, p.MetaIn, p.TextSecondary
	if out {
		bg, quoteBg, metaCol, secondary = p.BubbleOut, p.QuoteOut, p.MetaOut, p.SecondaryOut
	}
	if u.btn("quote:"+m.ID).Clicked(gtx) && m.Quote.ID != "" {
		u.jumpTo(m.Quote.ID)
	}
	pad, inset := gtx.Dp(3), gtx.Dp(8)
	sz := min(gtx.Dp(200), maxW-2*pad-2*inset)
	w := sz + 2*inset // the inner width
	cgtx := gtx
	cgtx.Constraints = layout.Constraints{Max: image.Pt(w, 1<<20)}
	var sender part
	hasSender := c.IsGroup && !out && tail && m.Sender != ""
	if hasSender {
		col := p.Senders[hashIndex(m.SenderID+m.Sender, len(p.Senders))]
		sender = record(cgtx, u.label(13, m.Sender, col, labelOpts{weight: font.SemiBold, maxLines: 1}).Layout)
	}
	quote := record(cgtx, func(gtx C) D { return u.layoutQuote(gtx, m.Quote, quoteBg, secondary, w, u.quotedMessage(m.Quote)) })
	meta := record(cgtx, func(gtx C) D { return u.layoutMeta(gtx, m, metaCol, nil) })

	content := op.Record(gtx.Ops)
	y := 0
	if hasSender {
		sender.at(gtx, gtx.Dp(6), gtx.Dp(3))
		u.senderButton(gtx, m, image.Pt(gtx.Dp(6), gtx.Dp(3)), sender.size)
		y += sender.size.Y + gtx.Dp(6)
	}
	quote.at(gtx, 0, y)
	func() {
		t := op.Offset(image.Pt(0, y)).Push(gtx.Ops)
		defer t.Pop()
		qg := gtx
		qg.Constraints = layout.Exact(quote.size)
		clickable(qg, u.btn("quote:"+m.ID), func(gtx C) D { return D{Size: quote.size} })
	}()
	y += quote.size.Y + gtx.Dp(6)
	t := op.Offset(image.Pt(inset, y)).Push(gtx.Ops)
	u.stickerPicture(gtx, m, sz)
	t.Pop()
	y += sz + gtx.Dp(4)
	meta.at(gtx, w-meta.size.X-gtx.Dp(5), y)
	y += meta.size.Y + gtx.Dp(2)
	call := content.Stop()

	bw, bh := w+2*pad, y+2*pad
	u.paintBubble(gtx, bw, bh, bg, out, tail)
	t = op.Offset(image.Pt(pad, pad)).Push(gtx.Ops)
	call.Add(gtx.Ops)
	t.Pop()
	return D{Size: image.Pt(bw, bh)}
}

// layoutSticker draws a sticker without a bubble, with the time on a chip
// under its corner, colored like the sender's bubbles.
func (u *UI) layoutSticker(gtx C, m *model.Message) D {
	p := u.pal
	sz := gtx.Dp(150)
	u.stickerPicture(gtx, m, sz)
	bg, metaCol := p.BubbleIn, p.MetaIn
	if m.FromMe {
		bg, metaCol = p.BubbleOut, p.MetaOut
	}
	meta := record(gtx, func(gtx C) D {
		return u.card(gtx, 8, bg, func(gtx C) D {
			return layout.Inset{Left: 6, Right: 6, Top: 2, Bottom: 3}.Layout(gtx, func(gtx C) D {
				return u.layoutMeta(gtx, m, metaCol, nil)
			})
		})
	})
	y := sz + gtx.Dp(2)
	meta.at(gtx, sz-meta.size.X, y)
	return D{Size: image.Pt(sz, y+meta.size.Y)}
}

// stickerPicture draws a sticker (or its placeholder) in an sz square.
func (u *UI) stickerPicture(gtx C, m *model.Message, sz int) {
	img := u.messageImage(m, sz*2)
	if img == nil || img.state != imgReady {
		fillRRect(gtx, image.Rect(0, 0, sz, sz), gtx.Dp(12), argb(0x808080, 0x30))
		return
	}
	pic, size := img.op, img.size
	if img.animated {
		b, chat, id := u.backend, m.ChatID, m.ID
		if f, ok := u.stickerFrame("m:"+chat+"/"+id, sz*2, func() []byte { return b.MediaData(chat, id) }); ok {
			pic, size = f, f.Size()
		}
	}
	// Stickers keep their aspect ratio inside the square.
	s := min(float32(sz)/float32(size.X), float32(sz)/float32(size.Y))
	w, h := int(float32(size.X)*s), int(float32(size.Y)*s)
	dst := image.Rect((sz-w)/2, (sz-h)/2, (sz-w)/2+w, (sz-h)/2+h)
	paintCover(gtx, pic, size, dst)
}

// gradientImage stands in for photos in demo data.
func (u *UI) gradientImage(gtx C, r image.Rectangle, a, b uint32) {
	paint.LinearGradientOp{
		Stop1: f32.Pt(float32(r.Min.X), float32(r.Min.Y)), Color1: rgb(a),
		Stop2: f32.Pt(float32(r.Max.X), float32(r.Max.Y)), Color2: rgb(b),
	}.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
	w, h := r.Dx(), r.Dy()
	fillCircle(gtx, image.Pt(r.Min.X+w*3/4, r.Min.Y+h/4), h/8, argb(0xffffff, 0xb0))
	var path clip.Path
	path.Begin(gtx.Ops)
	path.MoveTo(f32.Pt(float32(r.Min.X), float32(r.Max.Y)))
	path.LineTo(f32.Pt(float32(r.Min.X), float32(r.Min.Y+h*3/4)))
	path.QuadTo(f32.Pt(float32(r.Min.X+w/4), float32(r.Min.Y+h/2)), f32.Pt(float32(r.Min.X+w/2), float32(r.Min.Y+h*3/4)))
	path.QuadTo(f32.Pt(float32(r.Min.X+w*3/4), float32(r.Min.Y+h)), f32.Pt(float32(r.Max.X), float32(r.Min.Y+h*5/8)))
	path.LineTo(f32.Pt(float32(r.Max.X), float32(r.Max.Y)))
	path.Close()
	paint.FillShape(gtx.Ops, argb(0x0b3d2e, 0x90), clip.Outline{Path: path.End()}.Op())
	paint.LinearGradientOp{
		Stop1: f32.Pt(0, float32(r.Max.Y-gtx.Dp(40))), Color1: color.NRGBA{},
		Stop2: f32.Pt(0, float32(r.Max.Y)), Color2: color.NRGBA{A: 0x70},
	}.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
}

// layoutEmpty is the welcome pane shown when no chat is open.
func (u *UI) layoutEmpty(gtx C) D {
	p := u.pal
	dims := fill(gtx, p.Panel)
	gtx.Constraints.Min = gtx.Constraints.Max
	layout.Center.Layout(gtx, func(gtx C) D {
		gtx.Constraints.Max.X = min(gtx.Constraints.Max.X, gtx.Dp(460))
		return layout.Flex{Axis: layout.Vertical, Alignment: layout.Middle}.Layout(gtx,
			layout.Rigid(func(gtx C) D {
				sz := gtx.Dp(150)
				fillCircle(gtx, image.Pt(sz/2, sz/2), sz/2, p.Hover)
				return centerIn(gtx, sz, iconW(icChats, 72, p.Green))
			}),
			layout.Rigid(layout.Spacer{Height: 28}.Layout),
			layout.Rigid(u.label(30, "WazzapClients for Windows", p.Text, labelOpts{weight: font.Light, maxLines: 1, align: text.Middle}).Layout),
			layout.Rigid(layout.Spacer{Height: 14}.Layout),
			layout.Rigid(u.label(14, "Send and receive messages without keeping your phone online. Native, lightweight, and no browser inside.",
				p.TextSecondary, labelOpts{maxLines: 0, align: text.Middle}).Layout),
		)
	})
	layout.S.Layout(gtx, func(gtx C) D {
		gtx.Constraints.Min.X = 0 // S only clears Min.Y
		return layout.Inset{Bottom: 36}.Layout(gtx, func(gtx C) D {
			return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(iconW(icLock, 14, p.TextSecondary)),
				layout.Rigid(layout.Spacer{Width: 5}.Layout),
				layout.Rigid(u.label(13, "Your personal messages are end-to-end encrypted", p.TextSecondary).Layout),
			)
		})
	})
	return dims
}
