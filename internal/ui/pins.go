package ui

import (
	"image"
	"slices"
	"time"

	"gioui.org/font"
	"gioui.org/layout"
	"gioui.org/op"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

// Pinned messages: the banner under the open chat's header, and how long
// a pin lasts.

// pinChoices are how long a message can be pinned, as on WhatsApp.
var pinChoices = []struct {
	label string
	d     time.Duration
}{
	{"24 hours", 24 * time.Hour},
	{"7 days", 7 * 24 * time.Hour},
	{"30 days", 30 * 24 * time.Hour},
}

// togglePin unpins m, or asks how long to pin it.
func (u *UI) togglePin(m *model.Message) {
	if m.Pinned {
		u.backend.PinMessage(m, 0)
		return
	}
	u.openPinMenu(m)
}

// openPinMenu asks how long to pin m, at the pointer.
func (u *UI) openPinMenu(m *model.Message) {
	u.ctx = ctxMenu{kind: ctxPin, chatID: m.ChatID, msg: m, at: u.mouse}
}

func (u *UI) pinItems(m *model.Message) []menuItem {
	note := "Choose how long your pin lasts. You can unpin at any time."
	if p := &u.conv.pins; p.chat == m.ChatID && len(p.list) >= model.PinsMax {
		note += " It replaces the oldest pin."
	}
	items := []menuItem{{note: true, label: note}}
	for _, c := range pinChoices {
		items = append(items, menuItem{key: "pin:" + c.label, label: c.label, run: func() { u.backend.PinMessage(m, c.d) }})
	}
	return items
}

// pinState is the open chat's pinned messages. The banner shows one at a
// time, the latest pinned first: a click jumps to it and moves on to the
// next, as in WhatsApp.
type pinState struct {
	chat string
	list []*model.Message // the latest pinned first
	at   int              // the one in the banner
}

// shown is the message in the banner, or nil.
func (s *pinState) shown() *model.Message {
	if s.at >= len(s.list) {
		return nil
	}
	return s.list[s.at]
}

func (s *pinState) has(id string) bool {
	return slices.ContainsFunc(s.list, func(m *model.Message) bool { return m.ID == id })
}

// loadPins reads chat's pinned messages. The banner goes on showing the
// same one, unless another was just pinned.
func (u *UI) loadPins(chat string) {
	s := &u.conv.pins
	old, cur := *s, s.shown()
	*s = pinState{chat: chat, list: u.backend.PinnedMessages(chat)}
	if cur == nil || old.chat != chat || len(s.list) == 0 || s.list[0].ID != old.list[0].ID && !old.has(s.list[0].ID) {
		return
	}
	s.at = max(0, slices.IndexFunc(s.list, func(m *model.Message) bool { return m.ID == cur.ID }))
}

// layoutPinnedBanner shows m, one of the chat's pinned messages, under the
// header, with a bar for each of them when there are several.
func (u *UI) layoutPinnedBanner(gtx C, m *model.Message) D {
	p := u.pal
	pins := &u.conv.pins
	cl := u.btn("pinned:" + pins.chat)
	if cl.Clicked(gtx) {
		u.jumpTo(m.ID)
		pins.at = (pins.at + 1) % len(pins.list)
		gtx.Execute(op.InvalidateCmd{})
	}
	gtx.Constraints.Min.X = gtx.Constraints.Max.X
	defer u.hiding(gtx, "pinned", cl.Hovered())()
	return clickable(gtx, cl, func(gtx C) D {
		bg := mix(p.Panel, p.Hover, u.hover(gtx, cl))
		d := background(gtx, bg, 0, func(gtx C) D {
			return vcenter(gtx, gtx.Dp(50), func(gtx C) D {
				return layout.Inset{Left: 22, Right: 16}.Layout(gtx, func(gtx C) D {
					txt := plainText(stripIsolates(m.Text))
					if m.Media != model.MediaNone && txt == "" {
						txt = mediaLabel(m)
					}
					who, _, _ := u.senderLabel(m)
					return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
						layout.Rigid(iconW(icPin, 20, p.TextSecondary)),
						layout.Rigid(layout.Spacer{Width: 16}.Layout),
						layout.Rigid(u.label(14.5, who+": ", p.TextSecondary, labelOpts{weight: font.Medium, maxLines: 1}).Layout),
						layout.Flexed(1, u.label(14.5, firstLine(txt), p.TextSecondary, labelOpts{maxLines: 1}).Layout),
					)
				})
			})
		})
		fillRect(gtx, image.Rect(0, d.Size.Y-max(1, gtx.Dp(1)), d.Size.X, d.Size.Y), p.Divider)
		if n := len(pins.list); n > 1 {
			// Down the left edge, the latest pinned at the bottom: the
			// older messages are further up the chat.
			x, w, gap := gtx.Dp(9), gtx.Dp(3), gtx.Dp(3)
			top, h := gtx.Dp(8), d.Size.Y-gtx.Dp(16)
			for i := range n {
				y0, y1 := top+(h+gap)*(n-1-i)/n, top+(h+gap)*(n-i)/n-gap
				col := p.Divider
				if i == pins.at {
					col = p.Green
				}
				fillRRect(gtx, image.Rect(x, y0, x+w, y1), w/2, col)
			}
		}
		return d
	})
}
