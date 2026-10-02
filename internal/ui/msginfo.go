package ui

import (
	"fmt"
	"image"
	"image/color"
	"strings"
	"time"

	"gioui.org/layout"
	"gioui.org/widget"

	"github.com/chomosuke9/wazzapclients/internal/model"
	"github.com/chomosuke9/wazzapclients/internal/ui/icon"
)

// msgInfoState is the "Message info" panel of one of your messages: who
// got it and who read it, and when. Like the search panel, it takes the
// info panel's place beside the conversation.
type msgInfoState struct {
	open     bool
	chatID   string
	msg      *model.Message
	data     *model.MessageInfo
	list     widget.List
	closeBtn widget.Clickable
	anim     tween
}

// shown reports whether the panel is open or still sliding away.
func (s *msgInfoState) shown() bool { return s.open || s.anim.v > 0 }

// openMsgInfo shows the Message info of m, in place of the info or search
// panel if one is open.
func (u *UI) openMsgInfo(m *model.Message) {
	s := &u.msgInfo
	if u.info.open || u.search.open {
		u.hideInfo()
		u.hideChatSearch()
		s.anim.snap(true)
	}
	s.open, s.chatID, s.msg = true, m.ChatID, m
	s.data = u.backend.MessageInfo(m)
	s.list.Position = layout.Position{}
}

// hideMsgInfo closes the panel at once, for when the chat or page changes
// under it, or another panel takes its place.
func (u *UI) hideMsgInfo() {
	u.msgInfo.open = false
	u.msgInfo.anim.snap(false)
}

// msgInfoReceipt reloads the panel when a receipt for its message comes.
func (u *UI) msgInfoReceipt(e model.ReceiptEvent) {
	s := &u.msgInfo
	if !s.open || s.chatID != e.ChatID {
		return
	}
	for _, id := range e.IDs {
		if id == s.msg.ID {
			s.data = u.backend.MessageInfo(s.msg)
			return
		}
	}
}

func (u *UI) layoutMsgInfo(gtx C) D {
	p := u.pal
	s := &u.msgInfo
	if s.closeBtn.Clicked(gtx) {
		s.open = false
	}
	// The loaded copy of the message is the one receipts and edits update.
	for _, m := range u.msgs {
		if m.ID == s.msg.ID && m.ChatID == s.chatID {
			s.msg = m
			break
		}
	}
	dims := fill(gtx, p.Panel)
	rows := u.msgInfoRows()
	layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx C) D {
			return vcenter(gtx, gtx.Dp(64), func(gtx C) D {
				return layout.Inset{Left: 11.5}.Layout(gtx, func(gtx C) D {
					return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
						layout.Rigid(func(gtx C) D { return u.iconButton(gtx, &s.closeBtn, icClose, 40, 25, p.IconStrong) }),
						layout.Rigid(layout.Spacer{Width: 10}.Layout),
						layout.Flexed(1, u.label(16.5, "Message info", p.Text, labelOpts{maxLines: 1}).Layout),
					)
				})
			})
		}),
		layout.Flexed(1, func(gtx C) D {
			return u.scrollList(gtx, &s.list, len(rows), func(gtx C, i int) D { return rows[i](gtx) })
		}),
	)
	return dims
}

// msgInfoRows is the message on the chat's wallpaper, then who read it
// and who it was delivered to: in a group, each member, with how many are
// still to come; with one person, just when.
func (u *UI) msgInfoRows() []layout.Widget {
	s := &u.msgInfo
	m, info := s.msg, s.data
	if info == nil {
		info = &model.MessageInfo{Members: 1}
	}
	rows := []layout.Widget{u.msgInfoPreview}
	c := u.chatByID(s.chatID)
	if c == nil || !c.IsGroup {
		var r model.PersonReceipt
		if len(info.Receipts) > 0 {
			r = info.Receipts[0]
		}
		if m.Media == model.MediaVoice {
			rows = append(rows, u.msgInfoTime(icMic, true, "Played", r.Played))
		}
		rows = append(rows,
			u.msgInfoTime(icTicks, true, "Read", r.Read),
			u.msgInfoTime(icTicks, false, "Delivered", r.Delivered))
		return rows
	}
	var played, read, delivered []model.PersonReceipt
	for _, r := range info.Receipts {
		switch {
		case m.Media == model.MediaVoice && !r.Played.IsZero():
			played = append(played, r)
		case !r.Read.IsZero():
			read = append(read, r)
		case !r.Delivered.IsZero():
			delivered = append(delivered, r)
		}
	}
	section := func(ic *icon.Icon, blue bool, title string, people []model.PersonReceipt, done int, when func(model.PersonReceipt) time.Time) {
		rows = append(rows, u.msgInfoHeading(ic, blue, title))
		for _, r := range people {
			rows = append(rows, u.msgInfoPerson(r, when(r)))
		}
		if left := info.Members - done; left > 0 {
			rows = append(rows, u.msgInfoRemaining(left))
		}
	}
	// Each list leaves out those in the lists above it, but the counts of
	// those still to come take them in.
	if m.Media == model.MediaVoice {
		section(icMic, true, "Played by", played, len(played), func(r model.PersonReceipt) time.Time { return r.Played })
	}
	section(icTicks, true, "Read by", read, len(played)+len(read), func(r model.PersonReceipt) time.Time { return r.Read })
	section(icTicks, false, "Delivered to", delivered, len(played)+len(read)+len(delivered), func(r model.PersonReceipt) time.Time { return r.Delivered })
	return rows
}

// msgInfoPreview is the message as the chat shows it, on its wallpaper.
// It reads no input: the chat has the message's buttons.
func (u *UI) msgInfoPreview(gtx C) D {
	s := &u.msgInfo
	c := u.chatByID(s.chatID)
	if c == nil {
		c = &model.Chat{ID: s.chatID}
	}
	if tp := u.chatPalette(c); tp != u.pal {
		old := u.pal
		u.pal = tp
		defer func() { u.pal = old }()
	}
	w := gtx.Constraints.Max.X
	padX, padTop, padBottom := gtx.Dp(22), gtx.Dp(24), gtx.Dp(48)
	maxW := max(w-2*padX, gtx.Dp(120))
	bgtx := gtx.Disabled()
	bgtx.Constraints = layout.Constraints{Max: image.Pt(maxW, gtx.Dp(2000))}
	var bub part
	if s.msg.Kind == model.KindSticker {
		bub = record(bgtx, func(gtx C) D { return u.layoutStickerMessage(gtx, c, s.msg, true, maxW) })
	} else {
		bub = record(bgtx, func(gtx C) D { return u.layoutBubble(gtx, c, s.msg, true, maxW) })
	}
	h := padTop + bub.size.Y + padBottom
	wgtx := gtx
	wgtx.Constraints = layout.Exact(image.Pt(w, h))
	u.conv.wallpaper.layout(wgtx, u.pal.ChatBg, u.pal.Doodle, u.doodles)
	bub.at(gtx, w-padX-bub.size.X, padTop)
	return D{Size: image.Pt(w, h)}
}

// msgInfoHeading heads a list of people: ticks, blue when for reading.
func (u *UI) msgInfoHeading(ic *icon.Icon, blue bool, title string) layout.Widget {
	return func(gtx C) D {
		p := u.pal
		return layout.Inset{Left: infoPadX, Right: 24, Top: 26, Bottom: 14}.Layout(gtx, func(gtx C) D {
			return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(iconW(ic, 21, msgInfoTickColor(p, blue))),
				layout.Rigid(layout.Spacer{Width: 14}.Layout),
				layout.Flexed(1, u.label(16, title, p.TextSecondary, labelOpts{maxLines: 1}).Layout),
			)
		})
	}
}

func msgInfoTickColor(p *Palette, blue bool) color.NRGBA {
	if blue {
		return p.TickRead
	}
	return p.TextSecondary
}

// msgInfoPerson is someone who got the message, and when.
func (u *UI) msgInfoPerson(r model.PersonReceipt, at time.Time) layout.Widget {
	return func(gtx C) D {
		it := listItem{title: r.Name, sub: u.receiptTime(at)}
		g := infoGeom
		g.iconCenter = infoPadX - g.hoverLeft + 25.5
		g.textLeft = 75.4
		g.height, g.subHeight = 76, 76
		it.glyph = func(gtx C, col color.NRGBA) D { return u.avatar(gtx, r.ID, r.Name, false, 51) }
		c := u.btn("msginfo:" + r.ID)
		if c.Clicked(gtx) {
			u.openContact(r.ID, r.Name)
		}
		return u.layoutListItem(gtx, c, it, g)
	}
}

// msgInfoRemaining counts those the message hasn't reached (or who
// haven't read it) yet.
func (u *UI) msgInfoRemaining(n int) layout.Widget {
	return func(gtx C) D {
		return layout.Inset{Left: infoPadX + 12, Right: 24, Top: 6, Bottom: 8}.Layout(gtx,
			u.label(15.5, fmt.Sprintf("%d remaining", n), u.pal.TextSecondary, labelOpts{maxLines: 1}).Layout)
	}
}

// msgInfoTime is one step in a chat with one person: Read or Delivered,
// and when, or "—" while it hasn't happened.
func (u *UI) msgInfoTime(ic *icon.Icon, blue bool, title string, at time.Time) layout.Widget {
	return func(gtx C) D {
		p := u.pal
		when := "—"
		if !at.IsZero() {
			when = u.receiptTime(at)
		}
		return layout.Inset{Left: infoPadX, Right: 24, Top: 22, Bottom: 6}.Layout(gtx, func(gtx C) D {
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
				layout.Rigid(func(gtx C) D {
					return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
						layout.Rigid(iconW(ic, 21, msgInfoTickColor(p, blue))),
						layout.Rigid(layout.Spacer{Width: 14}.Layout),
						layout.Flexed(1, u.label(16, title, p.Text, labelOpts{maxLines: 1}).Layout),
					)
				}),
				layout.Rigid(layout.Spacer{Height: 6}.Layout),
				layout.Rigid(func(gtx C) D {
					return layout.Inset{Left: 35}.Layout(gtx, u.label(14.5, when, p.TextSecondary, labelOpts{maxLines: 1}).Layout)
				}),
			)
		})
	}
}

// receiptTime is "Today at 01:01", as Message info puts it.
func (u *UI) receiptTime(t time.Time) string {
	s := u.versionTime(t)
	return strings.ToUpper(s[:1]) + s[1:]
}
