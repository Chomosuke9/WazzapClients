package ui

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"strings"
	"time"

	"gioui.org/f32"
	"gioui.org/font"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/text"
	"gioui.org/unit"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

// contactRows builds the scrolling content of a person's or business's
// info panel, in WhatsApp Desktop's order.
func (u *UI) contactRows(gtx C, c *model.Chat, info *model.ChatInfo) []layout.Widget {
	p := u.pal
	rows := []layout.Widget{
		func(gtx C) D { return u.infoProfile(gtx, c, info) },
	}
	if biz := info.Business; biz != nil {
		rows = append(rows, u.infoRule, func(gtx C) D { return u.businessNotice(gtx) })
		if biz.Description != "" {
			rows = append(rows, u.infoText(biz.Description, p.Text, 15.5, 0, 22))
		}
		if len(biz.Hours) > 0 {
			rows = append(rows, func(gtx C) D { return u.businessHours(gtx, biz) })
		}
		if biz.Address != "" {
			rows = append(rows, u.infoText(biz.Address, p.Text, 16, 10, 16))
		}
		if biz.Email != "" {
			rows = append(rows, u.infoText(biz.Email, p.Green, 16, 10, 16))
		}
		for _, w := range biz.Websites {
			rows = append(rows, u.infoLink(w))
		}
		rows = append(rows, func(gtx C) D { return layout.Spacer{Height: 8}.Layout(gtx) })
	}
	rows = append(rows, u.infoDivider(0, 8),
		func(gtx C) D { return u.infoMedia(gtx, info) },
		u.infoDivider(12.2, 6),
	)
	item := func(key string, it listItem) layout.Widget {
		return func(gtx C) D { return u.layoutListItem(gtx, u.btn("info:"+key), it, infoGeom) }
	}
	notif := ""
	notifIc := icBell
	if c.Muted {
		notif, notifIc = "Muted always", icMuted
	}
	disappearing := "Off"
	if d := info.Disappearing; d > 0 {
		disappearing = durationLabel(d)
	}
	rows = append(rows,
		item("starred", listItem{ic: icStar, title: "Starred messages"}),
		item("notif", listItem{ic: notifIc, title: "Notification settings", sub: notif}),
		item("disappearing", listItem{glyph: disappearingIcon, title: "Disappearing messages", sub: disappearing}),
		item("privacy", listItem{ic: icShield, title: "Advanced chat privacy", sub: "Off"}),
		item("theme", listItem{ic: icPalette, title: "Chat theme"}),
		item("encryption", listItem{ic: icLockOutline, title: "Encryption", sub: "Messages are end-to-end encrypted. Click to verify."}),
		u.infoDivider(12, 0),
	)

	// About and phone number.
	rows = append(rows, u.infoSection("About and phone number", true))
	if info.Business == nil && info.About != "" {
		rows = append(rows, u.infoText(info.About, p.Text, 17, 6, 4, infoPadX))
	}
	if info.Phone != "" {
		rows = append(rows, u.infoText(info.Phone, p.Text, 17, 6, 10, infoPadX))
	}

	if n := len(info.Common); n > 0 {
		title := fmt.Sprintf("%d groups in common", n)
		if n == 1 {
			title = "1 group in common"
		}
		rows = append(rows, func(gtx C) D { return layout.Spacer{Height: 18}.Layout(gtx) }, u.infoSection(title, false))
		for _, g := range info.Common {
			rows = append(rows, func(gtx C) D { return u.infoCommonGroup(gtx, g) })
		}
	}

	// Actions. Those about the chat itself need one to exist.
	tall := infoGeom
	tall.height = 65.5
	action := func(key string, it listItem, run func()) layout.Widget {
		return func(gtx C) D {
			b := u.btn("info:" + key)
			if b.Clicked(gtx) && run != nil {
				run()
			}
			return u.layoutListItem(gtx, b, it, tall)
		}
	}
	b := u.backend
	id, name := c.ID, info.Name
	chat := u.chatByID(id)
	rows = append(rows, u.infoDivider(12, 8))
	if chat != nil {
		fav := "Add to favourites"
		if chat.Favorite {
			fav = "Remove from favourites"
		}
		rows = append(rows,
			action("fav", listItem{ic: icHeart, title: fav}, func() { b.SetFavorite(id, !chat.Favorite) }),
			action("list", listItem{ic: icAddToList, title: "Add to list"}, nil),
			action("export", listItem{ic: icDownload, title: "Export chat"}, func() { b.ExportChat(id) }),
			action("clear", listItem{ic: icClear, title: "Clear chat", danger: true}, func() {
				u.confirm("Clear this chat?", "Messages will only be removed from this device and your devices on the newer versions of WhatsApp.",
					dialogButton{label: "Clear chat", primary: true, run: func() { b.ClearChat(id) }})
			}),
		)
	}
	if info.Blocked {
		rows = append(rows, action("block", listItem{ic: icBlock, title: "Unblock " + name, danger: true}, func() { b.SetBlocked(id, false) }))
	} else {
		rows = append(rows, action("block", listItem{ic: icBlock, title: "Block " + name, danger: true}, func() {
			u.confirm("Block "+name+"?", "Blocked contacts will no longer be able to call you or send you messages.",
				dialogButton{label: "Block", primary: true, danger: true, run: func() { b.SetBlocked(id, true) }})
		}))
	}
	report := "Report " + name
	if info.Business != nil {
		report = "Report business"
	}
	rows = append(rows, action("report", listItem{ic: icThumbDown, title: report, danger: true}, func() {
		u.toast("Reporting isn't supported in this app yet. Report from your phone.")
	}))
	if chat != nil {
		rows = append(rows, action("delete", listItem{ic: icDelete, title: "Delete chat", danger: true}, func() {
			u.confirm("Delete this chat?", "Messages will be removed from this device and your other linked devices.",
				dialogButton{label: "Delete chat", primary: true, danger: true, run: func() {
					if u.selected != nil && u.selected.ID == id {
						u.closeChat()
					}
					b.DeleteChat(id)
				}})
		}))
	}
	return append(rows, func(gtx C) D { return layout.Spacer{Height: 24}.Layout(gtx) })
}

// contactSubtitle is what the profile shows under a contact's name: the
// phone number, or a business's name, category and opening hours.
func (u *UI) contactSubtitle(c *model.Chat, info *model.ChatInfo) layout.Widget {
	p := u.pal
	biz := info.Business
	if biz == nil {
		phone := info.Phone
		if phone == "" {
			phone = c.Presence
		}
		return u.label(16.8, phone, p.TextSecondary).Layout
	}
	var lines []layout.FlexChild
	add := func(gap unit.Dp, w layout.Widget) {
		if len(lines) > 0 {
			lines = append(lines, layout.Rigid(layout.Spacer{Height: gap}.Layout))
		}
		lines = append(lines, layout.Rigid(w))
	}
	if biz.Name != "" {
		add(0, u.label(15.5, biz.Name, p.TextSecondary).Layout)
	}
	if biz.Category != "" {
		add(22, u.label(15.5, biz.Category, p.TextSecondary).Layout)
	}
	if st := hoursNow(biz, u.now()); st.known {
		add(10, func(gtx C) D {
			col := p.Green
			if !st.open {
				col = p.Danger
			}
			return layout.Flex{Alignment: layout.Baseline}.Layout(gtx,
				layout.Rigid(u.label(15.5, st.word, col).Layout),
				layout.Rigid(u.label(15.5, " "+st.summary, p.TextSecondary).Layout),
			)
		})
	}
	return func(gtx C) D {
		return layout.Flex{Axis: layout.Vertical, Alignment: layout.Middle}.Layout(gtx, lines...)
	}
}

// infoTextX is the left edge of a business's details.
const infoTextX = infoPadX + 10

// infoRule is a divider across the whole panel.
func (u *UI) infoRule(gtx C) D {
	h := max(1, gtx.Dp(1))
	fillRect(gtx, image.Rect(0, 0, gtx.Constraints.Max.X, h), u.pal.Hover)
	return D{Size: image.Pt(gtx.Constraints.Max.X, h)}
}

// businessNotice is the "This is a business account." line.
func (u *UI) businessNotice(gtx C) D {
	p := u.pal
	return layout.Inset{Left: infoTextX, Right: 38, Top: 18, Bottom: 44}.Layout(gtx, func(gtx C) D {
		return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
			layout.Flexed(1, u.label(15, "This is a business account.", p.Text, labelOpts{maxLines: 2}).Layout),
			layout.Rigid(func(gtx C) D { return infoIcon(gtx, p.Green) }),
		)
	})
}

// infoText is a block of text in the panel, like a business description.
func (u *UI) infoText(txt string, col color.NRGBA, size unit.Sp, top, bottom unit.Dp, left ...unit.Dp) layout.Widget {
	x := unit.Dp(infoTextX)
	if len(left) > 0 {
		x = left[0]
	}
	return func(gtx C) D {
		return layout.Inset{Left: x, Right: 46, Top: top, Bottom: bottom}.Layout(gtx, func(gtx C) D {
			l := u.label(size, txt, col, labelOpts{})
			l.MaxLines = 0
			l.LineHeight, l.LineHeightScale = unit.Sp(float32(size)*1.6), 1
			if col == u.pal.Green {
				l.Font.Weight = font.SemiBold
			}
			return l.Layout(gtx)
		})
	}
}

// infoLink is a business's website, opened in the browser when clicked.
func (u *UI) infoLink(link string) layout.Widget {
	return func(gtx C) D {
		c := u.btn("info:link:" + link)
		if c.Clicked(gtx) {
			target := link
			if !strings.Contains(target, "://") {
				target = "https://" + target
			}
			if !openURL(target) {
				u.toast("Couldn't open the link.")
			}
		}
		return layout.Inset{Left: infoTextX, Right: 46, Top: 10, Bottom: 16}.Layout(gtx, func(gtx C) D {
			return clickable(gtx, c, u.label(16, link, u.pal.Green, labelOpts{weight: font.SemiBold, maxLines: 1}).Layout)
		})
	}
}

// infoSection is a section's small grey heading, optionally underlined.
func (u *UI) infoSection(title string, rule bool) layout.Widget {
	return func(gtx C) D {
		p := u.pal
		return layout.Inset{Left: infoPadX, Right: 30, Top: 14, Bottom: 6}.Layout(gtx, func(gtx C) D {
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
				layout.Rigid(u.label(15, title, p.TextSecondary, labelOpts{weight: font.SemiBold, maxLines: 1}).Layout),
				layout.Rigid(func(gtx C) D {
					if !rule {
						return D{}
					}
					top, h := gtx.Dp(8), max(1, gtx.Dp(1))
					fillRect(gtx, image.Rect(0, top, gtx.Constraints.Max.X, top+h), p.Hover)
					return D{Size: image.Pt(gtx.Constraints.Max.X, top+h)}
				}),
			)
		})
	}
}

// businessHours is the "Open now" line, which unfolds into the week.
func (u *UI) businessHours(gtx C, biz *model.Business) D {
	p := u.pal
	c := u.btn("info:hours")
	if c.Clicked(gtx) {
		u.info.allHours = !u.info.allHours
	}
	st := hoursNow(biz, u.now())
	line := func(gtx C) D {
		word, col := "Closed now", p.Danger
		if st.open {
			word, col = "Open now", p.Green
		}
		return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
			layout.Flexed(1, u.label(16, word, col, labelOpts{weight: font.SemiBold, maxLines: 1}).Layout),
			layout.Rigid(u.label(16, dayHours(st.today), p.Text, labelOpts{maxLines: 1}).Layout),
			layout.Rigid(layout.Spacer{Width: 14}.Layout),
			layout.Rigid(func(gtx C) D {
				if !u.info.allHours {
					return drawIcon(gtx, icChevron, 24, p.TextSecondary)
				}
				// Point the chevron up while the week is shown.
				sz := gtx.Dp(24)
				c := f32.Pt(float32(sz)/2, float32(sz)/2)
				defer op.Affine(f32.AffineId().Rotate(c, math.Pi)).Push(gtx.Ops).Pop()
				return drawIcon(gtx, icChevron, 24, p.TextSecondary)
			}),
		)
	}
	return layout.Inset{Left: infoTextX, Right: 38, Top: 12, Bottom: 16}.Layout(gtx, func(gtx C) D {
		children := []layout.FlexChild{layout.Rigid(func(gtx C) D { return clickable(gtx, c, line) })}
		if u.info.allHours {
			days := biz.Hours
			for i := range 7 {
				// The week starts on Monday, like WhatsApp lists it.
				day := time.Weekday((i + 1) % 7)
				var h *model.BusinessHours
				for j := range days {
					if days[j].Day == day {
						h = &days[j]
					}
				}
				children = append(children, layout.Rigid(func(gtx C) D {
					return layout.Inset{Top: 8, Right: 38}.Layout(gtx, func(gtx C) D {
						return layout.Flex{}.Layout(gtx,
							layout.Flexed(1, u.label(15.5, day.String(), p.TextSecondary, labelOpts{maxLines: 1}).Layout),
							layout.Rigid(u.label(15.5, dayHours(h), p.TextSecondary, labelOpts{maxLines: 1, align: text.End}).Layout),
						)
					})
				}))
			}
		}
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
	})
}

// openState is a business's opening state at a given moment.
type openState struct {
	known bool
	open  bool
	today *model.BusinessHours // nil when closed all day
	// word and summary make the profile's line: "Open" "24 hours".
	word, summary string
}

// tzCache holds the time zones businesses are in; LoadLocation reads a file.
var tzCache = map[string]*time.Location{}

func zone(name string) *time.Location {
	if loc, ok := tzCache[name]; ok {
		return loc
	}
	loc, err := time.LoadLocation(name)
	if err != nil || name == "" {
		loc = time.Local
	}
	tzCache[name] = loc
	return loc
}

// hoursNow works out whether a business is open at now, in its time zone.
func hoursNow(biz *model.Business, now time.Time) openState {
	if len(biz.Hours) == 0 {
		return openState{}
	}
	now = now.In(zone(biz.TimeZone))
	st := openState{known: true}
	for i := range biz.Hours {
		if biz.Hours[i].Day == now.Weekday() {
			st.today = &biz.Hours[i]
		}
	}
	mins := now.Hour()*60 + now.Minute()
	switch h := st.today; {
	case h == nil:
		st.word, st.summary = "Closed", "today"
	case h.Mode == "open_24h":
		st.open, st.word, st.summary = true, "Open", "24 hours"
	case h.Mode == "appointment_only":
		st.open, st.word, st.summary = true, "Open", "by appointment only"
	case h.Open <= mins && (mins < h.Close || h.Close <= h.Open):
		st.open, st.word, st.summary = true, "Open", "until "+hhmm(h.Close)
	case mins < h.Open:
		st.word, st.summary = "Closed", "· Opens "+hhmm(h.Open)
	default:
		st.word, st.summary = "Closed", "now"
	}
	return st
}

// dayHours describes one day's opening hours.
func dayHours(h *model.BusinessHours) string {
	switch {
	case h == nil:
		return "Closed"
	case h.Mode == "open_24h":
		return "Open 24 hours"
	case h.Mode == "appointment_only":
		return "By appointment only"
	}
	return hhmm(h.Open) + "–" + hhmm(h.Close)
}

func hhmm(mins int) string {
	mins %= 24 * 60
	return fmt.Sprintf("%02d:%02d", mins/60, mins%60)
}

// infoCommonGroup is a row of "groups in common": the picture, the
// community's name, the group's name and its members.
func (u *UI) infoCommonGroup(gtx C, g model.CommonGroup) D {
	p := u.pal
	c := u.btn("info:common:" + g.ID)
	if c.Clicked(gtx) {
		if chat := u.chatByID(g.ID); chat != nil {
			u.setPage(pageChats)
			u.open(chat)
		}
	}
	it := listItem{content: func(gtx C) D {
		var lines []layout.FlexChild
		line := func(w layout.Widget) {
			if len(lines) > 0 {
				lines = append(lines, layout.Rigid(layout.Spacer{Height: 2}.Layout))
			}
			lines = append(lines, layout.Rigid(w))
		}
		if g.Community != "" {
			line(u.label(15, g.Community, p.TextSecondary, labelOpts{maxLines: 1}).Layout)
		}
		line(u.label(17, g.Name, p.Text, labelOpts{maxLines: 1}).Layout)
		if g.Members != "" {
			line(u.label(15, g.Members, p.TextSecondary, labelOpts{maxLines: 1}).Layout)
		}
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx, lines...)
	}}
	it.glyph = func(gtx C, _ color.NRGBA) D { return u.commonGroupAvatar(gtx, g) }
	geom := infoGeom
	geom.iconCenter = infoPadX - geom.hoverLeft + 25.5
	geom.textLeft = 75.4
	geom.height, geom.subHeight = 76, 76
	if g.Community != "" {
		geom.subHeight = 96
	}
	return u.layoutListItem(gtx, c, it, geom)
}

// commonGroupAvatar draws a shared group's picture. A community's group
// shows the community's rounded square with the group's picture over its
// bottom-right corner, like the chat list.
func (u *UI) commonGroupAvatar(gtx C, g model.CommonGroup) D {
	if g.CommunityID == "" {
		return u.avatarOf(gtx, g.ID, avatarGroup, 51)
	}
	box := gtx.Dp(52)
	u.avatarOf(gtx, g.CommunityID, avatarCommunity, 33)
	const d = unit.Dp(34)
	dpx := gtx.Dp(d)
	fillCircle(gtx, image.Pt(box-dpx/2, box-dpx/2), dpx/2+gtx.Dp(2.5), u.pal.Panel)
	t := op.Offset(image.Pt(box-dpx, box-dpx)).Push(gtx.Ops)
	u.avatarOf(gtx, g.ID, avatarGroup, d)
	t.Pop()
	return D{Size: image.Pt(box, box)}
}

// infoIcon is an outlined "i" in a circle.
func infoIcon(gtx C, col color.NRGBA) D {
	return cachedGlyph(gtx, glyphKey{name: "info", px: gtx.Dp(24), col: col}, func(gtx C) D {
		px := float32(gtx.Dp(24))
		s := px / 24
		c := f32.Pt(12*s, 12*s)
		strokeArc(gtx, c, 9*s, 0, 2*math.Pi, 1.9*s, col)
		fillCircle(gtx, image.Pt(int(12*s+0.5), int(8*s+0.5)), int(1.25*s+0.5), col)
		w := 1.9 * s
		fillRRect(gtx, image.Rectangle{Min: image.Pt(int(12*s-w/2+0.5), int(10.6*s)), Max: image.Pt(int(12*s+w/2+0.5), int(16.6*s))},
			int(w/2), col)
		return D{Size: image.Pt(int(px), int(px))}
	})
}
