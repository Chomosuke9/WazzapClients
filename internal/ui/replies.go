package ui

import (
	"image"

	"gioui.org/font"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

// Replies to a community's announcements, as in WhatsApp: only admins
// write in the announcements, but any member may reply to one. Its replies
// don't show in the chat: a button under its card opens them in the
// Message info panel's place, with a field to write one. They notify no
// one.

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

// repliesLabel names the button that opens an announcement's n replies.
func repliesLabel(n int) string {
	switch n {
	case 0:
		return "Reply"
	case 1:
		return "1 reply"
	}
	return itoa(n) + " replies"
}

// openReplies shows the replies to the announcement m, ready to write one.
func (u *UI) openReplies(m *model.Message) {
	u.openMsgInfo(m)
	s := &u.msgInfo
	s.replies, s.data = true, nil
	if s.replyFor != m.ID {
		s.replyFor = m.ID
		s.replyEd.SetText("")
	}
	s.replyEd.Submit = true
	s.replyList = u.backend.Replies(m)
	if u.canReply() {
		u.requestFocus(&s.replyEd)
	}
}

// repliesChanged reloads the panel when its announcement changed: a reply
// came in, went out or was deleted.
func (u *UI) repliesChanged(m *model.Message) {
	s := &u.msgInfo
	if s.open && s.replies && s.chatID == m.ChatID && s.msg.ID == m.ID {
		s.msg = m
		s.replyList = u.backend.Replies(m)
	}
}

// canReply reports whether the open panel's announcement takes replies of
// yours.
func (u *UI) canReply() bool {
	m := u.msgInfo.msg
	c := u.chatByID(u.msgInfo.chatID)
	return c != nil && !c.Left && m.Revoked.IsZero() && m.Kind != model.KindDeleted
}

// sendReply sends what the panel's field holds.
func (u *UI) sendReply() {
	s := &u.msgInfo
	txt := trimSpace(s.replyEd.Text())
	if txt == "" || !u.canReply() {
		return
	}
	u.backend.SendReply(s.msg, txt)
	s.replyEd.SetText("")
	s.list.Position = layout.Position{First: len(s.replyList) + 2} // the new one, at the end
}

// replyRows is the announcement on the chat's wallpaper, then its replies.
func (u *UI) replyRows() []layout.Widget {
	s := &u.msgInfo
	rows := []layout.Widget{u.msgInfoPreview}
	head := "No replies yet"
	if n := len(s.replyList); n > 0 {
		head = repliesLabel(n)
	}
	rows = append(rows, func(gtx C) D {
		return layout.Inset{Left: infoPadX, Right: 24, Top: 20, Bottom: 8}.Layout(gtx,
			u.label(15, head, u.pal.TextSecondary, labelOpts{maxLines: 1}).Layout)
	})
	for _, r := range s.replyList {
		rows = append(rows, func(gtx C) D { return u.replyRow(gtx, r) })
	}
	return append(rows, layout.Spacer{Height: 12}.Layout)
}

// replyRow is one reply: who wrote it and when, over its text. A right
// click opens its menu.
func (u *UI) replyRow(gtx C, r model.Reply) D {
	p := u.pal
	s := &u.msgInfo
	col := u.senderColor(s.chatID, r.SenderID, r.Sender)
	if r.Me {
		col = p.Green
	}
	if c := u.btn("replywho:" + r.ID); c.Clicked(gtx) && !r.Me {
		u.openContact(r.SenderID, r.Sender)
	}
	unhide := u.hiding(gtx, "reply:"+r.ID, u.hovered["reply:"+r.ID])
	defer unhide()
	dims := layout.Inset{Left: infoPadX, Right: 20, Top: 8, Bottom: 8}.Layout(gtx, func(gtx C) D {
		return layout.Flex{}.Layout(gtx,
			layout.Rigid(func(gtx C) D {
				return clickable(gtx, u.btn("replywho:"+r.ID), func(gtx C) D {
					id, name := r.SenderID, r.Sender
					if r.Me {
						id, name = u.meID, u.meName()
					}
					return u.avatar(gtx, id, name, false, 36)
				})
			}),
			layout.Rigid(layout.Spacer{Width: 12}.Layout),
			layout.Flexed(1, func(gtx C) D {
				return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
					layout.Rigid(func(gtx C) D {
						return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
							layout.Flexed(1, u.label(14, plainText(r.Sender), col, labelOpts{weight: font.SemiBold, maxLines: 1}).Layout),
							layout.Rigid(func(gtx C) D { return u.replyMeta(gtx, r) }),
						)
					}),
					layout.Rigid(layout.Spacer{Height: 2}.Layout),
					layout.Rigid(func(gtx C) D {
						o := richOpts{sel: "reply:" + r.ID, links: "reply:" + r.ID}
						gtx.Constraints.Min.X = 0
						return u.layoutRich(gtx, r.Text, 15, p.Text, p.TextSecondary, o)
					}),
				)
			}),
		)
	})
	u.hoverArea(gtx, "reply:"+r.ID, dims.Size)
	if u.rightClick(gtx, "reply:"+r.ID, dims.Size) {
		if len(u.replyItemsOf(r)) > 0 {
			u.ctx = ctxMenu{kind: ctxReply, chatID: s.chatID, at: u.mouse, job: r.ID}
		}
	}
	return dims
}

// replyMeta is when a reply was sent, or that it wasn't, or was deleted.
func (u *UI) replyMeta(gtx C, r model.Reply) D {
	p := u.pal
	txt, col := statusTime(r.Time, u.now()), p.TextSecondary
	switch {
	case !r.Revoked.IsZero():
		txt, col = "Deleted", p.Danger
	case r.Me && r.Receipt == model.Failed:
		txt, col = "Not sent", p.Danger
	case r.Me && r.Receipt == model.Pending:
		return iconW(icClock, 15, col)(gtx)
	}
	return u.label(12.5, txt, col, labelOpts{maxLines: 1}).Layout(gtx)
}

// replyItems is the menu of the reply it was opened on.
func (u *UI) replyItems() []menuItem {
	for _, r := range u.msgInfo.replyList {
		if r.ID == u.ctx.job {
			return u.replyItemsOf(r)
		}
	}
	return nil
}

func (u *UI) replyItemsOf(r model.Reply) []menuItem {
	m, b := u.msgInfo.msg, u.backend
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

// replyComposer is the field at the panel's bottom that writes a reply.
func (u *UI) replyComposer(gtx C) D {
	s := &u.msgInfo
	p := u.pal
	if !u.canReply() {
		return D{}
	}
	for {
		ev, ok := s.replyEd.Update(gtx)
		if !ok {
			break
		}
		if _, ok := ev.(widget.SubmitEvent); ok {
			u.sendReply()
		}
	}
	if s.replySend.Clicked(gtx) {
		u.sendReply()
		u.requestFocus(&s.replyEd)
	}
	empty := trimSpace(s.replyEd.Text()) == ""
	return layout.Inset{Left: 12, Right: 12, Top: 8, Bottom: 12}.Layout(gtx, func(gtx C) D {
		m := op.Record(gtx.Ops)
		dims := layout.Inset{Left: 18, Right: 6, Top: 4, Bottom: 4}.Layout(gtx, func(gtx C) D {
			return layout.Flex{Alignment: layout.End}.Layout(gtx,
				layout.Flexed(1, func(gtx C) D {
					return layout.Inset{Top: 9, Bottom: 9, Right: 8}.Layout(gtx, func(gtx C) D {
						gtx.Constraints.Min.X = gtx.Constraints.Max.X
						gtx.Constraints.Max.Y = gtx.Dp(110)
						e := material.Editor(u.th, &s.replyEd, "Reply to announcement")
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
					return clickable(gtx, &s.replySend, func(gtx C) D {
						fillCircle(gtx, image.Pt(sz/2, sz/2), sz/2, mix(p.Green, p.Text, 0.12*u.hover(gtx, &s.replySend)))
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
