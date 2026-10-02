package ui

import (
	"image"

	"gioui.org/font"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

// A community's announcements look different from other chats, as in
// WhatsApp: each message is a card of one width down the middle, headed
// by its sender's picture, name and "Community admin", with a forward
// button beside it. Only admins may post, so members get a note where
// the composer would be.

// annMaxW is how wide an announcement's card grows.
const annMaxW = unit.Dp(506)

// announcementsOf returns the community whose announcements c is, or nil.
func (u *UI) announcementsOf(c *model.Chat) *model.Community {
	if c == nil {
		return nil
	}
	if cm := u.inCommunity[c.ID]; cm != nil && cm.Announcements == c.ID {
		return cm
	}
	return nil
}

// sendBlocked returns who may post in c when you may not ("admins" or
// "community admins"), or "" when you can send. A community's
// announcements count as admins-only until their members load.
func (u *UI) sendBlocked(c *model.Chat) string {
	if !c.IsGroup {
		return ""
	}
	info := u.chatMembers(c.ID)
	known := info != nil && len(info.Members) > 0
	switch {
	case u.announcementsOf(c) != nil:
		if !known || info.Announce && !u.amAdmin(c.ID) {
			return "community admins"
		}
	case known && info.Announce && !u.amAdmin(c.ID):
		return "admins"
	}
	return ""
}

// layoutSendBlocked takes the composer's place in a chat where only
// admins can send messages.
func (u *UI) layoutSendBlocked(gtx C, who string) D {
	p := u.pal
	gtx.Constraints.Min.X = gtx.Constraints.Max.X
	return background(gtx, p.Panel, 0, func(gtx C) D {
		gtx.Constraints.Min.Y = gtx.Dp(64)
		return layout.Center.Layout(gtx, func(gtx C) D {
			return layout.Inset{Left: 16, Right: 16}.Layout(gtx, func(gtx C) D {
				return layout.Flex{Alignment: layout.Baseline}.Layout(gtx,
					layout.Rigid(u.label(15, "Only ", p.TextSecondary, labelOpts{maxLines: 1}).Layout),
					layout.Rigid(u.label(15, who, p.Green, labelOpts{weight: font.SemiBold, maxLines: 1}).Layout),
					layout.Rigid(u.label(15, " can send messages", p.TextSecondary, labelOpts{maxLines: 1}).Layout),
				)
			})
		})
	})
}

// annWidth is the width of an announcement's card in a row w wide, which
// leaves room for the forward button on either side.
func annWidth(gtx C, w int) int {
	return max(min(gtx.Dp(annMaxW), w-2*gtx.Dp(annFwdRoom)), min(w, gtx.Dp(200)))
}

// annFwdRoom is the room the forward button takes beside a card.
const annFwdRoom = unit.Dp(44)

// layoutAnnHeader draws an announcement's sender: their picture, their
// name and "Community admin".
func (u *UI) layoutAnnHeader(gtx C, m *model.Message) D {
	p := u.pal
	name, col := m.Sender, p.Senders[hashIndex(m.SenderID+m.Sender, len(p.Senders))]
	pic := func(gtx C) D { return u.avatar(gtx, m.SenderID, m.Sender, false, 38) }
	if m.FromMe {
		name, col = "You", p.Green
		pic = func(gtx C) D { return u.avatar(gtx, u.meID, u.meName(), false, 38) }
	}
	secondary := p.TextSecondary
	if m.FromMe {
		secondary = p.SecondaryOut
	}
	dims := layout.Flex{Alignment: layout.Middle}.Layout(gtx,
		layout.Rigid(pic),
		layout.Rigid(layout.Spacer{Width: 12}.Layout),
		layout.Flexed(1, func(gtx C) D {
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
				layout.Rigid(u.label(14.5, plainText(name), col, labelOpts{weight: font.SemiBold, maxLines: 1}).Layout),
				layout.Rigid(layout.Spacer{Height: 3}.Layout),
				layout.Rigid(u.label(14.5, "Community admin", secondary, labelOpts{maxLines: 1}).Layout),
			)
		}),
	)
	if !m.FromMe {
		u.senderButton(gtx, m, image.Point{}, image.Pt(gtx.Dp(38), gtx.Dp(38)))
	}
	return dims
}

// layoutAnnSticker draws a sticker announcement: the sender on a card of
// its own, then the sticker and its time under it, without a bubble.
func (u *UI) layoutAnnSticker(gtx C, m *model.Message, w int) D {
	p := u.pal
	bg := p.BubbleIn
	if m.FromMe {
		bg = p.BubbleOut
	}
	hgtx := gtx
	hgtx.Constraints = layout.Constraints{Min: image.Pt(w-gtx.Dp(18), 0), Max: image.Pt(w-gtx.Dp(18), 1<<20)}
	head := record(hgtx, func(gtx C) D { return u.layoutAnnHeader(gtx, m) })
	hh := head.size.Y + gtx.Dp(24)
	u.paintBubble(gtx, w, hh, bg, m.FromMe, false)
	head.at(gtx, gtx.Dp(9), gtx.Dp(12))

	y := hh + gtx.Dp(12)
	sz := gtx.Dp(150)
	t := op.Offset(image.Pt((w-sz)/2, y)).Push(gtx.Ops)
	u.stickerPicture(gtx, m, sz)
	t.Pop()
	y += sz + gtx.Dp(10)
	meta := record(gtx, func(gtx C) D {
		gtx.Constraints.Min = image.Point{}
		return u.card(gtx, 12, p.DateChip, func(gtx C) D {
			return layout.Inset{Left: 9, Right: 9, Top: 4, Bottom: 5}.Layout(gtx, func(gtx C) D {
				return u.layoutMeta(gtx, m, p.DateChipText, nil)
			})
		})
	})
	meta.at(gtx, (w-meta.size.X)/2, y)
	y += meta.size.Y
	return D{Size: image.Pt(w, y)}
}

// annForwardButton draws the forward button beside an announcement,
// centered on its card's height h.
func (u *UI) annForwardButton(gtx C, m *model.Message, h int) {
	c := u.btn("annfwd:" + m.ID)
	if c.Clicked(gtx) {
		u.openForward([]*model.Message{m})
	}
	sz := gtx.Dp(34)
	defer op.Offset(image.Pt(0, (h-sz)/2)).Push(gtx.Ops).Pop()
	u.iconButton(gtx, c, icForward, 34, 24, u.pal.IconStrong)
}

// communityMenuItems lists the community's announcements and groups, to
// switch between them from the announcements' header.
func (u *UI) communityMenuItems(cm *model.Community) []menuItem {
	open := func(id string) func() {
		return func() {
			if c := u.chatByID(id); c != nil {
				u.open(c)
			}
		}
	}
	cur := ""
	if u.selected != nil {
		cur = u.selected.ID
	}
	items := []menuItem{{key: "ann", ic: icCampaign, label: "Announcements", tick: cur == cm.Announcements, run: open(cm.Announcements)}}
	for _, id := range cm.Groups {
		c := u.chatByID(id)
		if c == nil {
			continue
		}
		items = append(items, menuItem{key: "g:" + id, ic: icGroupsFill, label: c.Name, tick: cur == id, run: open(id)})
	}
	return items
}

// layoutCommunityButton is the header's community picture with a
// drop-down arrow, which lists the community's groups.
func (u *UI) layoutCommunityButton(gtx C, cm *model.Community) D {
	p := u.pal
	c := &u.conv.community
	if c.Clicked(gtx) {
		u.ctx = ctxMenu{kind: ctxCommunity, chatID: cm.ID, at: u.mouse}
	}
	return clickable(gtx, c, func(gtx C) D {
		h := gtx.Dp(40)
		if a := u.hover(gtx, c); a > 0 {
			fillRRect(gtx, image.Rect(0, 0, gtx.Dp(64), h), h/2, faded(p.Hover, a))
		}
		gtx.Constraints = layout.Exact(image.Pt(gtx.Dp(64), h))
		return layout.Center.Layout(gtx, func(gtx C) D {
			return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(func(gtx C) D { return u.avatarOf(gtx, cm.ID, avatarCommunity, 26) }),
				layout.Rigid(layout.Spacer{Width: 4}.Layout),
				layout.Rigid(iconW(icDropDown, 22, p.IconStrong)),
			)
		})
	})
}
