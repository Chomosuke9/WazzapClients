package ui

import (
	"image"

	"gioui.org/font"
	"gioui.org/io/event"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

// Replies to a community's announcements, as in WhatsApp: only admins
// write in the announcements, but any member may reply to one. Its replies
// don't show in the chat: a pill on its card's bottom edge, after the
// reactions, counts them and opens them in a small chat floating over the
// conversation, with a field to write one. They notify no one.

// repliesPopup is the floating chat of an announcement's replies.
type repliesPopup struct {
	msg      *model.Message
	list     []model.Reply
	msgs     []*model.Message // list's, as bubbles show them
	at       image.Point      // where the pill was clicked
	ed       widget.Editor
	edFor    string // the announcement ed's text is for
	send     widget.Clickable
	rows     widget.List
	wall     wallpaper
	scrim    widget.Clickable
	closeBtn widget.Clickable
	panel    int // an event tag that keeps clicks on the panel from the scrim

	closing bool
	anim    tween
}

func (r *repliesPopup) isOpen() bool { return r.msg != nil && !r.closing }

// takesReplies reports whether m, an announcement, can be replied to, or
// was.
func (u *UI) takesReplies(m *model.Message) bool {
	switch m.Kind {
	case model.KindDeleted, model.KindUnsupported, model.KindWaiting, model.KindSystem:
		return false
	}
	sent := !m.FromMe || m.Receipt != model.Pending && m.Receipt != model.Failed
	return m.Replies > 0 || sent && m.Revoked.IsZero()
}

// repliesLabel counts an announcement's n replies.
func repliesLabel(n int) string { return countOf(n, "reply", "replies") }

// repliesPill is the button that opens the replies to the announcement m,
// to draw after its reactions, or nothing while it has none.
func (u *UI) repliesPill(gtx C, m *model.Message) part {
	if m.Replies == 0 || !u.takesReplies(m) {
		return part{}
	}
	p := u.pal
	c := u.btn("replies:" + m.ChatID + "/" + m.ID)
	if c.Clicked(gtx) {
		u.openReplies(m)
	}
	gtx.Constraints.Min = image.Point{}
	return record(gtx, func(gtx C) D {
		return clickable(gtx, c, func(gtx C) D {
			return u.card(gtx, 13, mix(p.BubbleIn, p.Text, 0.06*u.hover(gtx, c)), func(gtx C) D {
				gtx.Constraints.Min.Y = gtx.Dp(pillH)
				return layout.W.Layout(gtx, func(gtx C) D {
					gtx.Constraints.Min = image.Point{}
					return layout.Inset{Left: 9, Right: 9}.Layout(gtx, func(gtx C) D {
						return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
							layout.Rigid(iconW(icReply, 17, p.TextSecondary)),
							layout.Rigid(layout.Spacer{Width: 4}.Layout),
							layout.Rigid(u.label(13.5, repliesLabel(m.Replies), p.TextSecondary, labelOpts{maxLines: 1}).Layout),
						)
					})
				})
			})
		})
	})
}

// openReplies shows the replies to the announcement m, ready to write one.
func (u *UI) openReplies(m *model.Message) {
	s := &u.replies
	s.msg, s.at, s.closing = m, u.mouse, false
	s.anim.snap(false)
	s.rows.Axis, s.rows.ScrollToEnd = layout.Vertical, true
	s.rows.Position = layout.Position{}
	if s.edFor != m.ID {
		s.edFor = m.ID
		s.ed.SetText("")
	}
	s.ed.Submit = true
	u.loadReplies()
	if u.canReply() {
		u.requestFocus(&s.ed)
	}
}

func (u *UI) closeReplies() { u.replies.closing = true }

func (u *UI) loadReplies() {
	s := &u.replies
	s.list = u.backend.Replies(s.msg)
	s.msgs = s.msgs[:0:0]
	for _, r := range s.list {
		s.msgs = append(s.msgs, &model.Message{ID: "reply:" + r.ID, ChatID: s.msg.ChatID, SenderID: r.SenderID, Sender: r.Sender,
			FromMe: r.Me, Text: r.Text, Time: r.Time, Receipt: r.Receipt, Revoked: r.Revoked,
			DeletedBy: r.DeletedBy, DeletedByMe: r.DeletedByMe})
	}
}

// repliesChanged reloads the open replies when their announcement changed:
// a reply came in, went out or was deleted.
func (u *UI) repliesChanged(m *model.Message) {
	s := &u.replies
	if s.isOpen() && s.msg.ChatID == m.ChatID && s.msg.ID == m.ID {
		s.msg = m
		u.loadReplies()
	}
}

// canReply reports whether the announcement whose replies are open takes
// yours.
func (u *UI) canReply() bool {
	m := u.replies.msg
	c := u.chatByID(m.ChatID)
	return c != nil && !c.Left && m.Revoked.IsZero() && m.Kind != model.KindDeleted
}

// sendReply sends what the field holds.
func (u *UI) sendReply() {
	s := &u.replies
	txt := trimSpace(s.ed.Text())
	if txt == "" || !u.canReply() {
		return
	}
	u.backend.SendReply(s.msg, txt)
	s.ed.SetText("")
	s.rows.Position = layout.Position{} // the new one, at the end
}

// layoutReplies draws the open replies over the window.
func (u *UI) layoutReplies(gtx C) {
	s := &u.replies
	if s.msg == nil {
		return
	}
	if s.isOpen() && (noFocus(&s.scrim).Clicked(gtx) || s.closeBtn.Clicked(gtx) ||
		u.selected == nil || u.selected.ID != s.msg.ChatID || u.selPage != u.page) {
		u.closeReplies()
	}
	v := s.anim.step(gtx, s.isOpen(), popDur(s.isOpen()))
	if v == 0 && s.closing {
		// What was being written stays, for the next time.
		s.msg, s.list, s.msgs, s.wall, s.closing = nil, nil, nil, wallpaper{}, false
		return
	}
	sz := gtx.Constraints.Max
	if s.closing {
		var done func()
		gtx, done = fadeOut(gtx)
		defer done()
	} else {
		sgtx := gtx
		sgtx.Constraints = layout.Exact(sz)
		focusTrap(gtx)
		noFocus(&s.scrim).Layout(sgtx, func(gtx C) D { return D{Size: gtx.Constraints.Max} })
	}

	edge, gap := gtx.Dp(8), gtx.Dp(16)
	size := image.Pt(min(gtx.Dp(400), sz.X-2*edge), min(gtx.Dp(460), sz.Y-2*edge))
	// Above the pill, or below it, or over it when there's no room.
	pos := image.Pt(s.at.X-size.X/2, s.at.Y-gap-size.Y)
	if below := s.at.Y + gap; pos.Y < edge && below+size.Y <= sz.Y-edge {
		pos.Y = below
	}
	pos.Y = max(edge, min(pos.Y, sz.Y-edge-size.Y))
	pos.X = max(edge, min(pos.X, sz.X-edge-size.X))
	rect := image.Rectangle{Max: size}.Add(pos)
	origin := image.Pt(min(max(s.at.X, rect.Min.X), rect.Max.X), min(max(s.at.Y, rect.Min.Y), rect.Max.Y))
	defer pushPopup(gtx, v, origin).Pop()
	rad := gtx.Dp(12)
	fillRRect(gtx, rect.Add(image.Pt(0, gtx.Dp(3))).Inset(-gtx.Dp(2)), rad+gtx.Dp(2), u.pal.Shadow)
	borderRRect(gtx, rect, rad, u.pal.Panel, u.pal.PopupBorder)
	area := clip.Rect(rect).Push(gtx.Ops)
	event.Op(gtx.Ops, &s.panel)
	area.Pop()
	// The rounded corners are the header's and the field's, which paint
	// nothing there, so the panel needs no rounded clip.
	defer op.Offset(pos.Add(image.Pt(1, 1))).Push(gtx.Ops).Pop()
	gtx.Constraints = layout.Exact(size.Sub(image.Pt(2, 2)))
	layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(u.repliesHeader),
		layout.Flexed(1, u.repliesChat),
		layout.Rigid(u.replyComposer),
	)
}

// repliesHeader counts the replies, over what they reply to.
func (u *UI) repliesHeader(gtx C) D {
	p := u.pal
	s := &u.replies
	title := "No replies yet"
	if n := len(s.list); n > 0 {
		title = repliesLabel(n)
	}
	m := s.msg
	who := plainText(m.Sender)
	if m.FromMe {
		who = "You"
	}
	sub := m.Text
	switch {
	case m.Kind == model.KindDeleted:
		sub = m.DeletedNote()
	case m.Media != model.MediaNone:
		sub = mediaLabel(m)
	}
	return vcenter(gtx, gtx.Dp(56), func(gtx C) D {
		return layout.Inset{Left: 18, Right: 8}.Layout(gtx, func(gtx C) D {
			return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
				layout.Flexed(1, func(gtx C) D {
					gtx.Constraints.Min.X = gtx.Constraints.Max.X
					return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
						layout.Rigid(u.label(15.5, title, p.Text, labelOpts{weight: font.SemiBold, maxLines: 1}).Layout),
						layout.Rigid(layout.Spacer{Height: 1}.Layout),
						layout.Rigid(func(gtx C) D {
							defer u.hiding(gtx, "replieshead", false)()
							return u.label(13, who+": "+previewText(sub), p.TextSecondary, labelOpts{maxLines: 1}).Layout(gtx)
						}),
					)
				}),
				layout.Rigid(layout.Spacer{Width: 8}.Layout),
				layout.Rigid(func(gtx C) D { return u.iconButton(gtx, &s.closeBtn, icClose, 40, 22, p.IconStrong) }),
			)
		})
	})
}

// repliesChat is the replies as bubbles on the chat's colors.
func (u *UI) repliesChat(gtx C) D {
	s := &u.replies
	if tp := u.chatPalette(u.chatByID(s.msg.ChatID)); tp != u.pal {
		old := u.pal
		u.pal = tp
		defer func() { u.pal = old }()
	}
	p := u.pal
	sz := gtx.Constraints.Max
	// Not a photo wallpaper: it's decoded for the conversation's size.
	s.wall.layout(gtx, p.ChatBg, p.Doodle, !p.Plain)
	defer clip.Rect{Max: sz}.Push(gtx.Ops).Pop()
	gtx.Constraints = layout.Exact(sz)
	u.scrollList(gtx, &s.rows, len(s.msgs), func(gtx C, i int) D {
		top, bottom := unit.Dp(2), unit.Dp(0)
		day := i == 0 || !sameDay(s.msgs[i-1].Time, s.msgs[i].Time)
		first := day || s.msgs[i-1].SenderID != s.msgs[i].SenderID || s.msgs[i-1].FromMe != s.msgs[i].FromMe
		if first {
			top = 10
		}
		if i == len(s.msgs)-1 {
			bottom = 12
		}
		return layout.Inset{Top: top, Bottom: bottom}.Layout(gtx, func(gtx C) D {
			row := func(gtx C) D { return u.replyRow(gtx, s.list[i], s.msgs[i], first) }
			if !day {
				return row(gtx)
			}
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
				layout.Rigid(func(gtx C) D {
					return layout.Inset{Bottom: 8}.Layout(gtx, func(gtx C) D {
						gtx.Constraints.Min.X = gtx.Constraints.Max.X
						return layout.N.Layout(gtx, func(gtx C) D {
							return u.systemChip(gtx, dateChip(s.msgs[i].Time, u.now()))
						})
					})
				}),
				layout.Rigid(row),
			)
		})
	})
	return D{Size: sz}
}

// repliesGroup is the chat replies are drawn as messages of: in the
// announcements themselves they would be cards.
var repliesGroup = model.Chat{ID: "replies", IsGroup: true}

// replyRow is one reply's bubble, yours on the right. A run of someone
// else's is headed by their name, with their picture beside it. A right
// click opens its menu.
func (u *UI) replyRow(gtx C, r model.Reply, m *model.Message, first bool) D {
	s := &u.replies
	c := &repliesGroup
	if u.btn("sender:"+m.ID).Clicked(gtx) && !r.Me {
		u.closeReplies()
		u.openContact(r.SenderID, r.Sender)
	}
	left, right := gtx.Dp(46), gtx.Dp(16)
	w := gtx.Constraints.Max.X
	unhide := u.hiding(gtx, m.ID, u.hovered[m.ID] || u.textSel.id == m.ID)
	bgtx := gtx
	bgtx.Constraints = layout.Constraints{Max: image.Pt(max(w-left-right, gtx.Dp(120)), 1<<20)}
	bub := record(bgtx, func(gtx C) D {
		if em := u.jumbo(c, m); em != nil {
			return u.layoutJumboMessage(gtx, c, m, em, first, gtx.Constraints.Max.X)
		}
		return u.layoutBubble(gtx, c, m, first, gtx.Constraints.Max.X)
	})
	x := left
	if r.Me {
		x = w - right - bub.size.X
	}
	bub.at(gtx, x, 0)
	if first && !r.Me {
		av := gtx.Dp(28)
		t := op.Offset(image.Pt(gtx.Dp(8), 0)).Push(gtx.Ops)
		u.avatar(gtx, r.SenderID, r.Sender, false, 28)
		u.senderButton(gtx, m, image.Point{}, image.Pt(av, av))
		t.Pop()
	}
	unhide()
	t := op.Offset(image.Pt(x, 0)).Push(gtx.Ops)
	u.hoverArea(gtx, m.ID, bub.size)
	if u.rightClick(gtx, m.ID, bub.size) {
		u.ctx = ctxMenu{kind: ctxReply, chatID: s.msg.ChatID, at: u.mouse, job: r.ID}
	}
	t.Pop()
	return D{Size: image.Pt(w, bub.size.Y)}
}

// replyItems is the menu of the reply it was opened on.
func (u *UI) replyItems() []menuItem {
	for _, r := range u.replies.list {
		if r.ID == u.ctx.job {
			return u.replyItemsOf(r)
		}
	}
	return nil
}

func (u *UI) replyItemsOf(r model.Reply) []menuItem {
	m, b := u.replies.msg, u.backend
	items := []menuItem{{key: "copy", ic: icCopy, label: "Copy", run: func() { u.copyText(stripIsolates(plainText(r.Text))) }}}
	failed := r.Me && r.Receipt == model.Failed
	if failed {
		items = append(items, menuItem{key: "retry", ic: icSend, label: "Send again", run: func() {
			b.DeleteReply(m, r)
			b.SendReply(m, r.Text)
		}})
	}
	switch {
	case failed:
		items = append(items, menuItem{key: "delete", ic: icDelete, label: "Delete", run: func() { b.DeleteReply(m, r) }})
	case (r.Me && r.Receipt != model.Pending || u.amAdmin(m.ChatID)) && r.Revoked.IsZero():
		items = append(items, menuItem{key: "delete", ic: icDelete, label: "Delete for everyone", run: func() {
			u.confirm("Delete this reply?", "It will be deleted for everyone in the community.",
				dialogButton{label: "Delete for everyone", danger: true, run: func() { b.DeleteReply(m, r) }})
		}})
	}
	return items
}

// replyComposer is the field at the bottom that writes a reply.
func (u *UI) replyComposer(gtx C) D {
	s := &u.replies
	p := u.pal
	if !u.canReply() {
		return layout.Spacer{Height: 12}.Layout(gtx) // the rounded corners
	}
	for {
		ev, ok := s.ed.Update(gtx)
		if !ok {
			break
		}
		if _, ok := ev.(widget.SubmitEvent); ok {
			u.sendReply()
		}
	}
	if s.send.Clicked(gtx) {
		u.sendReply()
		u.requestFocus(&s.ed)
	}
	empty := trimSpace(s.ed.Text()) == ""
	return layout.Inset{Left: 12, Right: 12, Top: 8, Bottom: 12}.Layout(gtx, func(gtx C) D {
		m := op.Record(gtx.Ops)
		dims := layout.Inset{Left: 18, Right: 6, Top: 4, Bottom: 4}.Layout(gtx, func(gtx C) D {
			return layout.Flex{Alignment: layout.End}.Layout(gtx,
				layout.Flexed(1, func(gtx C) D {
					return layout.Inset{Top: 9, Bottom: 9, Right: 8}.Layout(gtx, func(gtx C) D {
						gtx.Constraints.Min.X = gtx.Constraints.Max.X
						gtx.Constraints.Max.Y = gtx.Dp(110)
						e := material.Editor(u.th, &s.ed, "Reply to announcement")
						e.TextSize = unit.Sp(15.5)
						e.Color, e.HintColor = p.Text, p.ComposerHint
						e.SelectionColor = argb(0x53bdeb, 0x60)
						return e.Layout(gtx)
					})
				}),
				layout.Rigid(func(gtx C) D {
					sz := gtx.Dp(40)
					if empty {
						return D{Size: image.Pt(sz, sz)}
					}
					return clickable(gtx, &s.send, func(gtx C) D {
						fillCircle(gtx, image.Pt(sz/2, sz/2), sz/2, mix(p.Green, p.Text, 0.12*u.hover(gtx, &s.send)))
						return centerIn(gtx, sz, iconW(icSend, 20, p.OnGreen))
					})
				}),
			)
		})
		call := m.Stop()
		fillRRect(gtx, image.Rectangle{Max: dims.Size}, min(gtx.Dp(24), dims.Size.Y/2), p.Composer)
		call.Add(gtx.Ops)
		return dims
	})
}
