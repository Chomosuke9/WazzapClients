package ui

import (
	"image"
	"image/color"

	"gioui.org/font"
	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/text"
	"gioui.org/unit"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

// A status mention is a message saying its sender mentioned you, or the
// group, in a status update (model.StatusMention): WhatsApp shows a card,
// "Status <poster>" over "@ This group was mentioned.", with the status's
// picture on its right, which opens the status while it's kept. The status
// viewer says "Mentioned privately" over such a status, which leads back
// to the message.

// statusMentionNote is what a status mention says, in its card and as a
// chat's last message.
func statusMentionNote(sm *model.StatusMention) string {
	if sm.Group {
		return "This group was mentioned."
	}
	return "You were mentioned."
}

// statusMentionPoster names who posted the status a mention in chat c
// names: its sender.
func statusMentionPoster(c *model.Chat, m *model.Message) string {
	switch {
	case m.FromMe:
		return "You"
	case m.Sender != "":
		return m.Sender
	case c != nil:
		return c.Name
	}
	return ""
}

// statusMentionLines are a status mention card's two lines.
func (u *UI) statusMentionLines(c *model.Chat, m *model.Message, textCol, secondary color.NRGBA) (title, note layout.Widget) {
	return u.label(14.5, "Status "+plainText(statusMentionPoster(c, m)), textCol, labelOpts{weight: font.SemiBold, maxLines: 1}).Layout,
		u.label(13, "@ "+statusMentionNote(m.StatusMention), secondary, labelOpts{maxLines: 1}).Layout
}

// statusMentionPic reports whether a status mention card shows its
// status's picture (or a text status's words).
func statusMentionPic(sm *model.StatusMention) bool {
	return sm.Found && (len(sm.Thumb) > 0 || sm.Media == model.MediaNone)
}

// statusMentionWidth is how wide m's status mention card would be to show
// its lines whole.
func (u *UI) statusMentionWidth(gtx C, c *model.Chat, m *model.Message) int {
	title, note := u.statusMentionLines(c, m, u.pal.Text, u.pal.TextSecondary)
	gtx.Constraints = layout.Constraints{Max: image.Pt(1<<20, 1<<20)}
	w := max(record(gtx, title).size.X, record(gtx, note).size.X) + gtx.Dp(20)
	if statusMentionPic(m.StatusMention) {
		w += gtx.Dp(70)
	}
	return max(w, gtx.Dp(262))
}

// layoutStatusMentionCard draws m's status mention card width px wide.
func (u *UI) layoutStatusMentionCard(gtx C, c *model.Chat, m *model.Message, width int, bg, textCol, secondary color.NRGBA) D {
	defer u.emojiIn(emojiQuotes)()
	sm := m.StatusMention
	side := gtx.Dp(70)
	pic := statusMentionPic(sm)
	right := gtx.Dp(10)
	if pic {
		right += side
	}
	title, note := u.statusMentionLines(c, m, textCol, secondary)
	gtx.Constraints = layout.Exact(image.Pt(width, side))
	r := image.Rectangle{Max: gtx.Constraints.Max}
	defer clip.UniformRRect(r, gtx.Dp(7)).Push(gtx.Ops).Pop()
	fillRect(gtx, r, bg)
	if h := u.hover(gtx, u.btn("smention:"+m.ID)); h > 0 {
		fillRect(gtx, r, faded(argb(0x000000, 0x18), h))
	}
	txt := record(gtx, func(gtx C) D {
		gtx.Constraints = layout.Constraints{Max: image.Pt(max(0, width-gtx.Dp(10)-right), side)}
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(title),
			layout.Rigid(layout.Spacer{Height: 2}.Layout),
			layout.Rigid(note),
		)
	})
	txt.at(gtx, gtx.Dp(10), (side-txt.size.Y)/2)
	if pic {
		pr := image.Rect(width-side, 0, width, side)
		switch {
		case u.blurred():
			fillRect(gtx, pr, u.pal.Hover)
			if th := sm.Thumb; len(th) > 0 {
				if e := u.images.getBlurred("smt:"+sm.StatusID, func() []byte { return th }); e.state == imgReady {
					paintCover(gtx, e.op, e.size, pr)
				}
			}
		case len(sm.Thumb) > 0:
			th := sm.Thumb
			if e := u.images.get("smt:"+sm.StatusID, side*2, func() []byte { return th }); e.state == imgReady {
				paintCover(gtx, e.op, e.size, pr)
			}
		default: // a text status: its words on its color
			col := argbColor(sm.Background)
			if sm.Background == 0 {
				col = rgb(0x4a5a62)
			}
			fillRect(gtx, pr, col)
			l := record(gtx, func(gtx C) D {
				gtx.Constraints = layout.Constraints{Max: image.Pt(side-gtx.Dp(8), side-gtx.Dp(8))}
				return u.label(unit.Sp(9), sm.Text, rgb(0xffffff), labelOpts{align: text.Middle, maxLines: 4}).Layout(gtx)
			})
			l.at(gtx, pr.Min.X+(side-l.size.X)/2, (side-l.size.Y)/2)
		}
	}
	return D{Size: r.Max}
}

// openMentionedStatus opens the status a status mention names, while it's
// kept.
func (u *UI) openMentionedStatus(m *model.Message) {
	for _, t := range u.statuses {
		for i, up := range t.Updates {
			if up.ID == m.StatusMention.StatusID {
				u.status.viewer.showAt(t, i)
				return
			}
		}
	}
	u.toast("This status update is no longer available")
}

// openStatusMention closes the viewer and shows the message that says
// the status in it mentioned you or a group of yours.
func (u *UI) openStatusMention(up *model.StatusUpdate) {
	chat, id := up.MentionChat, up.MentionMsg
	u.status.viewer.close()
	if u.chatByID(chat) == nil {
		return
	}
	u.setPage(pageChats)
	u.SelectID(chat)
	u.jumpTo(id)
}
