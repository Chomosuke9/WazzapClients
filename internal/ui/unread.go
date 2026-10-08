package ui

import (
	"image"

	"gioui.org/font"
	"gioui.org/layout"
	"gioui.org/unit"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

// Opening a chat with unread messages shows "N unread messages" above the
// first of them and scrolls there, like WhatsApp, instead of to the newest
// message. The divider stays until another chat opens or you send one.

// unreadDivider is where the open chat's divider goes.
type unreadDivider struct {
	chat, id string // the chat and its first unread message
	n        int    // unread messages, as counted when the chat opened
}

const (
	// unreadPages is how many pages past the loaded messages opening a
	// chat looks for its first unread one.
	unreadPages = 20
	// unreadAbove is the room left above the divider when the chat opens
	// at it, to show what came before.
	unreadAbove unit.Dp = 48
)

// showUnread puts the divider above the oldest of the newest n incoming
// messages of the open chat, which shows its newest messages, and
// scrolls to it.
func (u *UI) showUnread(n int) {
	c := u.selected
	u.conv.unread = unreadDivider{}
	if n <= 0 || u.conv.newerMore {
		return
	}
	id, loaded := u.firstUnread(n)
	if id == "" {
		return
	}
	u.conv.unread = unreadDivider{chat: c.ID, id: id, n: n}
	u.msgsVer++
	if !loaded && u.loadAround(id) < 0 {
		u.conv.unread = unreadDivider{}
		u.loadLatest()
		return
	}
	for i, r := range u.rows(c) {
		if r.kind == rowUnread {
			u.conv.scrollTo = &layout.Position{First: i, BeforeEnd: true}
			u.conv.scrollAbove = unreadAbove
			break
		}
	}
}

// firstUnread finds the oldest of the newest n incoming messages of the
// open chat (system messages aren't any), looking up to unreadPages pages past the loaded ones, and
// reports whether it is loaded. With fewer stored, it finds the oldest
// incoming one.
func (u *UI) firstUnread(n int) (id string, loaded bool) {
	c := u.selected
	msgs, older, inMsgs := u.msgs, u.conv.olderMore, true
	for page := 0; ; page++ {
		for i := len(msgs) - 1; i >= 0; i-- {
			if msgs[i].FromMe || msgs[i].Kind == model.KindSystem {
				continue
			}
			id, loaded = msgs[i].ID, inMsgs
			if n--; n == 0 {
				return id, loaded
			}
		}
		if !older || page == unreadPages || len(msgs) == 0 {
			return id, loaded
		}
		// Only the oldest message of each page is kept to fetch the next.
		msgs, older = more(u.backend.MessagesBefore(c.ID, msgs[0].ID, messagePage+1), messagePage, true)
		inMsgs = false
	}
}

// clearUnread takes the divider away.
func (u *UI) clearUnread() {
	if u.conv.unread.id != "" {
		u.conv.unread = unreadDivider{}
		u.msgsVer++
	}
}

// unreadRow reports whether the divider goes above message m of chat c.
func (u *UI) unreadRow(c *model.Chat, m *model.Message) bool {
	d := &u.conv.unread
	return d.id != "" && d.id == m.ID && d.chat == c.ID
}

// unreadChip draws the divider's "N unread messages".
func (u *UI) unreadChip(gtx C, n int) D {
	p := u.pal
	txt := itoa(n) + " unread messages"
	if n == 1 {
		txt = "1 unread message"
	}
	gtx.Constraints.Min = image.Point{}
	return u.card(gtx, 18, p.DateChip, func(gtx C) D {
		return layout.Inset{Left: 18, Right: 18, Top: 8, Bottom: 9}.Layout(gtx,
			u.label(14.5, txt, p.Text, labelOpts{weight: font.SemiBold, maxLines: 1}).Layout)
	})
}
