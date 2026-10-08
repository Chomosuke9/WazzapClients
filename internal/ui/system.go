package ui

import (
	"image"

	"gioui.org/layout"
	"gioui.org/text"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

// System messages (model.KindSystem: "Alice added Bob", "You turned on
// disappearing messages", a missed call) are grey chips down the middle
// of the chat, like the day separators, wrapping to more lines when long.

// layoutSystem draws system message m of chat c, at most maxW wide.
func (u *UI) layoutSystem(gtx C, c *model.Chat, m *model.Message, maxW int) D {
	p := u.pal
	key := "sys:" + m.ID
	btn := u.btn(key)
	security := m.Notice == model.NoticeSecurity && !c.IsGroup
	if security && btn.Clicked(gtx) {
		u.openEncryption(c, c.Name)
	}
	// Privacy mode shows it under the pointer, as it does a message.
	defer u.hiding(gtx, key, u.hovered[key] || btn.Hovered())()
	gtx.Constraints.Min = image.Point{}
	gtx.Constraints.Max.X = min(gtx.Constraints.Max.X, maxW)
	txt := m.Text
	var mark layout.Widget
	switch m.Notice {
	case model.NoticeMissedCall:
		txt += " at " + m.Time.Format("15:04")
		mark = iconW(icCall, 16, p.Danger)
	case model.NoticeSecurity:
		mark = iconW(icLock, 14, p.DateChipText)
	case model.NoticeTimer:
		mark = func(gtx C) D {
			// The info panel's timer, drawn 16dp instead of 26.
			gtx.Metric.PxPerDp *= 16.0 / 26
			return disappearingIcon(gtx, p.DateChipText)
		}
	}
	chip := func(gtx C) D {
		return u.card(gtx, 7.5, p.DateChip, func(gtx C) D {
			return layout.Inset{Left: 12, Right: 12, Top: 5, Bottom: 6}.Layout(gtx, func(gtx C) D {
				l := u.label(12.5, txt, p.DateChipText, labelOpts{align: text.Middle})
				l.MaxLines = 0
				if mark == nil {
					return l.Layout(gtx)
				}
				return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(func(gtx C) D {
						defer u.unhidden()()
						return mark(gtx)
					}),
					layout.Rigid(layout.Spacer{Width: 6}.Layout),
					layout.Rigid(l.Layout),
				)
			})
		})
	}
	var dims D
	if security {
		dims = clickable(gtx, btn, chip)
	} else {
		dims = chip(gtx)
	}
	u.hoverArea(gtx, key, dims.Size)
	return dims
}
