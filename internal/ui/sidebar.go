package ui

import (
	"fmt"
	"image"
	"image/color"
	"strings"

	"gioui.org/font"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"github.com/chomosuke9/wazzapclients/internal/model"
	"github.com/chomosuke9/wazzapclients/internal/ui/icon"
)

// layoutSidebar draws the chat list column: header, search, filter chips
// and the scrollable list of chats.
func (u *UI) layoutSidebar(gtx C) D {
	u.sidebar.visible = u.filteredChats()
	u.sidebar.order.update(gtx, u.sidebar.visible, u.rowHeight(gtx))
	menuID := ""
	if u.ctx.isOpen() && u.ctx.kind == ctxChat {
		menuID = u.ctx.chatID
	}
	u.sidebar.menuSel.step(gtx, menuID, durSwitch)
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(u.layoutSidebarHeader),
		layout.Rigid(u.layoutSearch),
		layout.Rigid(u.layoutChips),
		layout.Rigid(u.layoutBanner),
		layout.Rigid(u.layoutAwayBar),
		layout.Flexed(1, u.layoutChatList),
	)
}

func (u *UI) layoutSidebarHeader(gtx C) D {
	p := u.pal
	h := gtx.Dp(68)
	if u.sidebar.showArchived {
		return vcenter(gtx, h, func(gtx C) D {
			return layout.Inset{Left: 10, Right: 16}.Layout(gtx, func(gtx C) D {
				return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(func(gtx C) D { return u.iconButton(gtx, &u.sidebar.back, icBack, 40, 24, p.Icon) }),
					layout.Rigid(layout.Spacer{Width: 10}.Layout),
					layout.Flexed(1, u.label(19, "Archived", p.Text, labelOpts{weight: font.SemiBold, maxLines: 1}).Layout),
				)
			})
		})
	}
	return vcenter(gtx, h, func(gtx C) D {
		return layout.Inset{Left: 21, Right: 21}.Layout(gtx, func(gtx C) D {
			return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
				layout.Flexed(1, u.label(23.5, "Chats", p.Text, labelOpts{weight: font.Medium, maxLines: 1}).Layout),
				layout.Rigid(func(gtx C) D { return u.iconButton(gtx, &u.sidebar.menu, icMenu, 40, 25, p.IconStrong) }),
				layout.Rigid(layout.Spacer{Width: 8}.Layout),
				layout.Rigid(func(gtx C) D {
					return clickable(gtx, &u.sidebar.newChat, func(gtx C) D {
						sz := gtx.Dp(42)
						col := mix(p.Green, rgb(0xffffff), 0.1*u.hover(gtx, &u.sidebar.newChat))
						fillCircle(gtx, image.Pt(sz/2, sz/2), sz/2, col)
						return centerIn(gtx, sz, iconW(icNewChat, 22, p.OnGreen))
					})
				}),
			)
		})
	})
}

func (u *UI) layoutSearch(gtx C) D {
	p := u.pal
	return layout.Inset{Left: 23, Right: 23, Bottom: 11}.Layout(gtx, func(gtx C) D {
		gtx.Constraints.Min.X = gtx.Constraints.Max.X
		return background(gtx, p.Search, 22, func(gtx C) D {
			return vcenter(gtx, gtx.Dp(43), func(gtx C) D {
				return layout.Inset{Left: 14, Right: 8}.Layout(gtx, func(gtx C) D {
					return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
						layout.Rigid(iconW(icSearch, 22, p.TextSecondary)),
						layout.Rigid(layout.Spacer{Width: 12}.Layout),
						layout.Flexed(1, func(gtx C) D {
							e := material.Editor(u.th, &u.sidebar.search, "Search or start a new chat")
							e.TextSize = 15.5
							e.Color = p.Text
							e.HintColor = p.TextSecondary
							return e.Layout(gtx)
						}),
					)
				})
			})
		})
	})
}

// chip draws one filter pill; active (0 to 1) chips are green.
func (u *UI) chip(gtx C, c *widget.Clickable, active float32, w layout.Widget) D {
	p := u.pal
	bg := mix(p.Chip, p.Hover, u.hover(gtx, c)*(1-active))
	bg = mix(bg, p.ChipActive, active)
	border := mix(p.ChipBorder, p.ChipActiveBorder, active)
	return clickable(gtx, c, func(gtx C) D {
		m := op.Record(gtx.Ops)
		dims := vcenter(gtx, gtx.Dp(34), w)
		call := m.Stop()
		borderRRect(gtx, image.Rectangle{Max: dims.Size}, dims.Size.Y/2, bg, border)
		call.Add(gtx.Ops)
		return dims
	})
}

// layoutChips draws the filter chips. Chips that don't fit collapse into a
// round "more" chip with a drop-down, like WhatsApp does in narrow windows.
func (u *UI) layoutChips(gtx C) D {
	p := u.pal
	return layout.Inset{Left: 21, Right: 21, Bottom: 10}.Layout(gtx, func(gtx C) D {
		gap := gtx.Dp(8)
		more := gtx.Dp(38)
		cgtx := gtx
		cgtx.Constraints.Min = image.Point{}
		sel := &u.sidebar.chipSel
		sel.step(gtx, u.sidebar.filter, durSwitch)
		parts := make([]part, len(filterNames))
		for i, name := range filterNames {
			i, name := i, name
			active := sel.of(i)
			fg := mix(p.ChipText, p.ChipActiveText, active)
			parts[i] = record(cgtx, func(gtx C) D {
				return u.chip(gtx, &u.sidebar.chips[i], active, func(gtx C) D {
					return layout.Inset{Left: 12, Right: 12}.Layout(gtx,
						u.label(15, name, fg, labelOpts{weight: font.SemiBold, maxLines: 1}).Layout)
				})
			})
		}
		avail := gtx.Constraints.Max.X
		// Show as many chips as fit; keep room for the more-chip if any are left.
		shown, x := 0, 0
		for i, pt := range parts {
			need := pt.size.X
			if i < len(parts)-1 {
				need += gap + more
			}
			if x+need > avail && i >= 2 {
				break
			}
			x += pt.size.X + gap
			shown++
		}
		u.sidebar.hiddenFilters = u.sidebar.hiddenFilters[:0]
		for i := shown; i < len(parts); i++ {
			u.sidebar.hiddenFilters = append(u.sidebar.hiddenFilters, i)
		}
		x = 0
		h := 0
		for i := 0; i < shown; i++ {
			parts[i].at(gtx, x, 0)
			x += parts[i].size.X + gap
			h = max(h, parts[i].size.Y)
		}
		if shown < len(parts) {
			var hiddenActive float32
			for i := shown; i < len(parts); i++ {
				hiddenActive = max(hiddenActive, sel.of(i))
			}
			t := op.Offset(image.Pt(x, 0)).Push(gtx.Ops)
			d := u.chip(cgtx, &u.sidebar.more, hiddenActive, func(gtx C) D {
				gtx.Constraints.Min.X = more
				return layout.Center.Layout(gtx, func(gtx C) D {
					d := iconW(icDropDown, 22, p.ChipText)(gtx)
					withOpacity(gtx, hiddenActive, func() { iconW(icDropDown, 22, p.ChipActiveText)(gtx) })
					return d
				})
			})
			t.Pop()
			u.filterMenu.anchor = image.Pt(x, 0)
			x += d.Size.X
			h = max(h, d.Size.Y)
		}
		return D{Size: image.Pt(min(x, avail), h)}
	})
}

func (u *UI) filteredChats() []*model.Chat {
	q := strings.ToLower(trimSpace(u.sidebar.search.Text()))
	out := u.sidebar.visible[:0] // reused every frame
	for _, c := range u.chats {
		if c.Archived != u.sidebar.showArchived {
			continue
		}
		switch u.sidebar.filter {
		case filterUnread:
			if c.Unread == 0 && c != u.selected {
				continue
			}
		case filterFavorites:
			if !c.Favorite {
				continue
			}
		case filterGroups:
			if !c.IsGroup {
				continue
			}
		}
		if q != "" && !strings.Contains(strings.ToLower(u.listName(c)), q) {
			continue
		}
		out = append(out, c)
	}
	return out
}

func (u *UI) layoutChatList(gtx C) D {
	chats := u.sidebar.visible
	o := &u.sidebar.order
	return u.scrollList(gtx, &u.sidebar.list, len(chats), func(gtx C, i int) D {
		c := chats[i]
		dy, a, moving := o.at(c.ID)
		if !moving {
			d := u.layoutChatRow(gtx, c)
			o.measured(c.ID, d.Size.Y)
			return d
		}
		row := record(gtx, func(gtx C) D { return u.layoutChatRow(gtx, c) })
		o.measured(c.ID, row.size.Y)
		withOpacity(gtx, a, func() { row.at(gtx, 0, dy) })
		return D{Size: row.size}
	})
}

// chatOrder slides chat rows to their new places when a message reorders
// the list. Rows that moved by one place slide there; a chat that jumped
// up fades in at its new place while the rows above it make room.
type chatOrder struct {
	prev    []string       // chat IDs as drawn last frame
	shift   map[string]int // moving rows: their old place, in px from the new one
	pending bool           // the order may have changed since the last frame
	anim    tween
	// Rows differ in height (a community's groups are taller), so each
	// row's last drawn height is kept, in px at metric. Rows not drawn
	// yet are guessed.
	heights map[string]int
	metric  unit.Metric
}

func (o *chatOrder) measured(id string, h int) {
	if o.heights == nil {
		o.heights = make(map[string]int)
	}
	o.heights[id] = h
}

func (o *chatOrder) height(id string, guess func(id string) int) int {
	if h, ok := o.heights[id]; ok {
		return h
	}
	return guess(id)
}

// rowHeight returns the height of a chat row not drawn yet, from the
// sizes chatRow lays it out at.
func (u *UI) rowHeight(gtx C) func(id string) int {
	return func(id string) int {
		if cm := u.inCommunity[id]; cm != nil && cm.Announcements != id {
			return gtx.Dp(96.5) + gtx.Dp(2)*2
		}
		return gtx.Dp(76.3) + gtx.Dp(2)*2
	}
}

// fadeInRow is the shift of a row that fades in instead of sliding.
const fadeInRow = 1 << 20

// update notices a new order of the visible chats and advances the slide.
func (o *chatOrder) update(gtx C, visible []*model.Chat, guess func(id string) int) {
	if gtx.Metric != o.metric {
		// The window moved to a display with another scale.
		o.metric = gtx.Metric
		clear(o.heights)
	}
	if o.pending && len(o.prev) > 0 {
		was := make(map[string]int, len(o.prev))
		wasY := make(map[string]int, len(o.prev))
		y := 0
		for i, id := range o.prev {
			was[id], wasY[id] = i, y
			y += o.height(id, guess)
		}
		if o.shift == nil {
			o.shift = make(map[string]int)
		}
		clear(o.shift)
		y = 0
		for i, c := range visible {
			switch j, ok := was[c.ID]; {
			case !ok || j-i > 1:
				o.shift[c.ID] = fadeInRow
			case j != i:
				o.shift[c.ID] = wasY[c.ID] - y
			}
			y += o.height(c.ID, guess)
		}
		if len(o.shift) > 0 {
			o.anim.snap(false)
		}
	}
	o.pending = false
	o.prev = o.prev[:0]
	for _, c := range visible {
		o.prev = append(o.prev, c.ID)
	}
	if len(o.shift) > 0 && o.anim.step(gtx, true, durSlide) >= 1 {
		clear(o.shift)
	}
}

// at returns where a row is drawn while rows move: its offset from its
// place in px and its opacity.
func (o *chatOrder) at(id string) (dy int, alpha float32, moving bool) {
	s, ok := o.shift[id]
	if !ok {
		return 0, 1, false
	}
	e := easeOut(o.anim.v)
	if s == fadeInRow {
		return 0, e, true
	}
	return int(float32(s) * (1 - e)), 1, true
}

func (u *UI) rowClick(c *model.Chat) *widget.Clickable {
	cl, ok := u.sidebar.rows[c.ID]
	if !ok {
		cl = new(widget.Clickable)
		u.sidebar.rows[c.ID] = cl
	}
	return cl
}

// listName is the name the chat list shows for c: WhatsApp lists a
// community's announcements under the community's name and picture.
func (u *UI) listName(c *model.Chat) string {
	if cm := u.inCommunity[c.ID]; cm != nil && cm.Announcements == c.ID {
		return cm.Name
	}
	return c.Name
}

func (u *UI) layoutChatRow(gtx C, c *model.Chat) D {
	click := u.rowClick(c)
	chev := u.btn("rowmenu:" + c.ID)
	if chev.Clicked(gtx) {
		u.openChatMenu(c)
	}
	o := rowOpts{
		click: click,
		sel:   max(u.sidebar.openSel.of(c.ID), u.sidebar.menuSel.of(c.ID)),
		// The chevron's button covers part of the row, so the row counts
		// as hovered while the chevron is.
		hovered: chev.Hovered(),
	}
	shown := c
	if cm := u.inCommunity[c.ID]; cm != nil {
		if c.ID == cm.Announcements {
			ann := *c
			ann.Name = u.listName(c)
			shown = &ann
			o.avatar = func(gtx C) D { return u.avatarOf(gtx, cm.ID, avatarCommunity, 52) }
		} else {
			o.community = cm
		}
	}
	dims := u.chatRow(gtx, shown, o)
	if u.rightClick(gtx, "chat:"+c.ID, dims.Size) {
		u.openChatMenu(c)
	}
	if click.Hovered() || chev.Hovered() {
		// The row's chevron (drawn with the row's indicators) opens the
		// menu. A community group's row has them on its middle line.
		s := gtx.Dp(30)
		y := gtx.Dp(53)
		if o.community != nil {
			y = dims.Size.Y / 2
		}
		t := op.Offset(image.Pt(dims.Size.X-gtx.Dp(18+14)-s+gtx.Dp(4), y-s/2)).Push(gtx.Ops)
		cg := gtx
		cg.Constraints = layout.Exact(image.Pt(s, s))
		clickable(cg, chev, func(gtx C) D { return D{Size: gtx.Constraints.Max} })
		t.Pop()
	}
	return dims
}

// rowOpts adapts the chat row to the Channels and Communities pages.
type rowOpts struct {
	click    *widget.Clickable
	sel      float32       // selected highlight, 0 to 1
	avatar   layout.Widget // replaces the chat's round avatar
	avatarW  unit.Dp       // left edge of the avatar within the row (default 12)
	textGap  unit.Dp       // space between avatar and text (default 16)
	verified bool          // blue badge after the name
	hovered  bool          // hovered through a button drawn over the row
	// community is the community a group belongs to: the row shows its
	// picture and name above the group's.
	community *model.Community
}

// chatRow draws one row of the chat list: avatar, name and time, then the
// last message preview with indicators.
func (u *UI) chatRow(gtx C, c *model.Chat, o rowOpts) D {
	p := u.pal
	click := o.click
	last := c.Last
	avatar := o.avatar
	if avatar == nil {
		avatar = func(gtx C) D { return u.avatar(gtx, c.ID, c.Name, c.IsGroup, 52) }
	}
	left, gap := o.avatarW, o.textGap
	if left == 0 {
		left = 12
	}
	if gap == 0 {
		gap = 16
	}

	return layout.Inset{Left: 13, Right: 18, Top: 2, Bottom: 2}.Layout(gtx, func(gtx C) D {
		return clickable(gtx, click, func(gtx C) D {
			gtx.Constraints.Min.X = gtx.Constraints.Max.X
			hover := u.hoverOn(gtx, click, click.Hovered() || o.hovered)
			bg := mix(mix(p.Panel, p.Hover, hover), p.Selected, o.sel)
			h := gtx.Dp(76.3)
			text := func(gtx C) D {
				return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
					layout.Rigid(func(gtx C) D { return u.layoutRowTitle(gtx, c, o.verified) }),
					layout.Rigid(layout.Spacer{Height: 3}.Layout),
					layout.Rigid(func(gtx C) D { return u.layoutRowPreview(gtx, c, last, hover) }),
				)
			}
			if cm := o.community; cm != nil {
				h = gtx.Dp(96.5)
				avatar = func(gtx C) D { return u.communityGroupAvatar(gtx, c, cm, bg) }
				text = func(gtx C) D { return u.layoutCommunityRowText(gtx, c, cm, o.verified, hover) }
			}
			return background(gtx, u.rowBg(bg), 10, func(gtx C) D {
				return vcenter(gtx, h, func(gtx C) D {
					return layout.Inset{Left: left, Right: 14}.Layout(gtx, func(gtx C) D {
						return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
							layout.Rigid(avatar),
							layout.Rigid(layout.Spacer{Width: gap}.Layout),
							layout.Flexed(1, text),
						)
					})
				})
			})
		})
	})
}

// nameWithBadge draws a name that truncates before a trailing verified
// badge, which always stays visible.
func (u *UI) nameWithBadge(gtx C, name string, size unit.Sp, col color.NRGBA, verified bool) D {
	if !verified {
		return u.label(size, name, col).Layout(gtx)
	}
	badge := gtx.Dp(22)
	ngtx := gtx
	ngtx.Constraints.Min.X = 0
	ngtx.Constraints.Max.X = max(0, gtx.Constraints.Max.X-badge)
	return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
		layout.Rigid(func(gtx C) D { return u.label(size, name, col).Layout(ngtx) }),
		layout.Rigid(func(gtx C) D {
			return layout.Inset{Left: 4}.Layout(gtx, iconW(icVerified, 18, u.pal.Verified))
		}),
	)
}

func (u *UI) layoutRowTitle(gtx C, c *model.Chat, verified bool) D {
	return layout.Flex{Alignment: layout.Baseline}.Layout(gtx,
		layout.Flexed(1, func(gtx C) D { return u.layoutRowName(gtx, c, verified) }),
		layout.Rigid(layout.Spacer{Width: 6}.Layout),
		layout.Rigid(func(gtx C) D { return u.layoutRowTime(gtx, c) }),
	)
}

// layoutRowName draws a chat row's name.
func (u *UI) layoutRowName(gtx C, c *model.Chat, verified bool) D {
	p := u.pal
	if !c.Self {
		return u.nameWithBadge(gtx, c.Name, 17.5, p.Text, verified)
	}
	// "Name (You)": the name truncates, the suffix never does.
	return layout.Flex{Alignment: layout.Baseline}.Layout(gtx,
		layout.Flexed(1, func(gtx C) D {
			gtx.Constraints.Min.X = 0
			return u.label(17.5, c.Name, p.Text).Layout(gtx)
		}),
		layout.Rigid(u.label(17.5, "  (You)", p.Text).Layout),
	)
}

// layoutRowTime draws when a chat row's last message came, green while
// the chat is unread.
func (u *UI) layoutRowTime(gtx C, c *model.Chat) D {
	p := u.pal
	col := p.TextSecondary
	if c.Unread != 0 {
		col = p.Green
	}
	var ts string
	if c.Last != nil {
		ts = listTime(c.Last.Time, u.now())
	}
	return u.label(13.2, ts, col).Layout(gtx)
}

// communityGroupAvatar is the picture of a group in a community, as the
// chat list shows it: the community's rounded square with the group's
// own picture over its bottom-right corner, cut out of it by a ring of
// the row's background bg.
func (u *UI) communityGroupAvatar(gtx C, c *model.Chat, cm *model.Community, bg color.NRGBA) D {
	box := gtx.Dp(52)
	u.avatarOf(gtx, cm.ID, avatarCommunity, 33)
	const d = unit.Dp(34)
	dpx := gtx.Dp(d)
	fillCircle(gtx, image.Pt(box-dpx/2, box-dpx/2), dpx/2+gtx.Dp(2.5), bg)
	t := op.Offset(image.Pt(box-dpx, box-dpx)).Push(gtx.Ops)
	u.avatarOf(gtx, c.ID, avatarGroup, d)
	t.Pop()
	return D{Size: image.Pt(box, box)}
}

// layoutCommunityRowText is the text of a community group's row: the
// community's name and the time, the group's name and the indicators,
// then the last message.
func (u *UI) layoutCommunityRowText(gtx C, c *model.Chat, cm *model.Community, verified bool, chev float32) D {
	p := u.pal
	line := func(w layout.Widget) layout.FlexChild {
		return layout.Rigid(func(gtx C) D { return vcenter(gtx, gtx.Dp(24), w) })
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		line(func(gtx C) D {
			return layout.Flex{Alignment: layout.Baseline}.Layout(gtx,
				layout.Flexed(1, u.label(15.3, cm.Name, p.TextSecondary, labelOpts{maxLines: 1}).Layout),
				layout.Rigid(layout.Spacer{Width: 6}.Layout),
				layout.Rigid(func(gtx C) D { return u.layoutRowTime(gtx, c) }),
			)
		}),
		layout.Rigid(layout.Spacer{Height: 3}.Layout),
		line(func(gtx C) D {
			row := []layout.FlexChild{layout.Flexed(1, func(gtx C) D { return u.layoutRowName(gtx, c, verified) })}
			return layout.Flex{Alignment: layout.Middle}.Layout(gtx, append(row, u.rowIndicators(c, chev)...)...)
		}),
		layout.Rigid(layout.Spacer{Height: 3}.Layout),
		line(func(gtx C) D {
			return layout.Flex{Alignment: layout.Middle}.Layout(gtx, u.previewParts(c, c.Last)...)
		}),
	)
}

// mediaIcon is the small glyph shown before media previews.
func mediaIcon(m model.Media) *icon.Icon {
	switch m {
	case model.MediaImage:
		return icImage
	case model.MediaVideo, model.MediaGIF:
		return icVideo
	case model.MediaVoice:
		return icMic
	case model.MediaAudio:
		return icHeadphones
	case model.MediaDocument:
		return icDocument
	case model.MediaSticker:
		return icSticker
	case model.MediaLocation:
		return icLocation
	case model.MediaContact:
		return icContact
	case model.MediaPoll:
		return icDocument
	}
	return nil
}

// mediaLabel is the preview text for a message without caption.
func mediaLabel(m *model.Message) string {
	if m.Text != "" && m.Media != model.MediaVoice && m.Media != model.MediaAudio {
		return m.Text
	}
	switch m.Media {
	case model.MediaImage:
		return "Photo"
	case model.MediaVideo:
		return "Video"
	case model.MediaGIF:
		return "GIF"
	case model.MediaVoice, model.MediaAudio:
		if m.Duration > 0 {
			return fmt.Sprintf("%d:%02d", m.Duration/60, m.Duration%60)
		}
		if m.Media == model.MediaVoice {
			return "Voice message"
		}
		return "Audio"
	case model.MediaDocument:
		return "Document"
	case model.MediaSticker:
		return "Sticker"
	case model.MediaLocation:
		return "Location"
	case model.MediaContact:
		return "Contact"
	case model.MediaPoll:
		return "Poll"
	}
	return m.Text
}

// shortName is how the chat list prefixes group previews: first names for
// saved contacts, full "~push name" or phone number otherwise.
func shortName(name string) string {
	if strings.HasPrefix(name, "~") || strings.HasPrefix(name, "+") {
		return name
	}
	if i := strings.IndexByte(name, ' '); i > 0 {
		return name[:i]
	}
	return name
}

// layoutRowPreview draws the second line of a chat row: last message preview
// followed by muted / pinned / unread indicators, and the menu chevron while
// the row is hovered (chev, 0 to 1).
func (u *UI) layoutRowPreview(gtx C, c *model.Chat, last *model.Message, chev float32) D {
	parts := u.previewParts(c, last)
	// Indicators are rigid children of the outer row, so Flex sizes them
	// first and the preview gets whatever width is left.
	row := []layout.FlexChild{layout.Flexed(1, func(gtx C) D {
		return layout.Flex{Alignment: layout.Middle}.Layout(gtx, parts...)
	})}
	row = append(row, u.rowIndicators(c, chev)...)
	return vcenter(gtx, gtx.Dp(22), func(gtx C) D {
		return layout.Flex{Alignment: layout.Middle}.Layout(gtx, row...)
	})
}

// previewParts lays out the last message preview: sender, receipt or
// media glyph, then the text.
func (u *UI) previewParts(c *model.Chat, last *model.Message) []layout.FlexChild {
	p := u.pal
	var children []layout.FlexChild
	small := func(ic *icon.Icon, col color.NRGBA, size unit.Dp, right unit.Dp) layout.FlexChild {
		return layout.Rigid(func(gtx C) D {
			return layout.Inset{Right: right}.Layout(gtx, iconW(ic, size, col))
		})
	}
	const size = unit.Sp(15.3)

	switch {
	case c.Typing != "":
		who := "typing…"
		if c.IsGroup {
			who = shortName(c.Typing) + " is typing…"
		}
		children = append(children, layout.Flexed(1, u.label(size, who, p.Green).Layout))
	case last == nil:
		children = append(children, layout.Flexed(1, layout.Spacer{}.Layout))
	default:
		if c.IsGroup && !last.FromMe && last.Sender != "" {
			children = append(children, layout.Rigid(u.label(size, shortName(last.Sender)+": ", p.TextSecondary).Layout))
		}
		if last.FromMe && last.Kind != model.KindDeleted {
			ic, col := receiptIcon(last.Receipt, p, false)
			children = append(children, small(ic, col, 18, 3))
		}
		txt := last.Text
		italic := false
		switch {
		case last.Kind == model.KindDeleted:
			children = append(children, small(icBlock, p.TextSecondary, 17, 4))
			txt, italic = "This message was deleted", true
			if last.FromMe {
				txt = "You deleted this message"
			}
		case last.Kind == model.KindUnsupported:
			children = append(children, small(icUnsupported, p.TextSecondary, 17, 4))
			txt, italic = "This message couldn't load", true
		case last.Media != model.MediaNone:
			col := p.TextSecondary
			if last.Media == model.MediaVoice && !last.FromMe && c.Unread > 0 {
				col = p.Green
			}
			children = append(children, small(mediaIcon(last.Media), col, 18, 4))
			txt = mediaLabel(last)
		}
		children = append(children, layout.Flexed(1, u.label(size, previewText(txt), p.TextSecondary, labelOpts{maxLines: 1, italic: italic}).Layout))
	}
	return children
}

// rowIndicators are a chat row's muted, unread and pinned indicators, and
// the menu chevron while the row is hovered (chev, 0 to 1).
func (u *UI) rowIndicators(c *model.Chat, chev float32) []layout.FlexChild {
	p := u.pal
	var row []layout.FlexChild
	indicator := func(w layout.Widget) layout.FlexChild {
		return layout.Rigid(func(gtx C) D { return layout.Inset{Left: 8}.Layout(gtx, w) })
	}
	if c.Muted {
		row = append(row, indicator(iconW(icMuted, 20, p.TextSecondary)))
	}
	if c.Unread > 0 && c.Mentioned {
		row = append(row, indicator(u.mentionMark))
	}
	if c.Unread > 0 {
		row = append(row, indicator(func(gtx C) D { return u.badge(gtx, c.Unread) }))
	} else if c.Unread < 0 {
		// Marked as unread: an empty green badge.
		row = append(row, indicator(func(gtx C) D {
			s := gtx.Dp(21)
			fillCircle(gtx, image.Pt(s/2, s/2), s/2, p.Green)
			return D{Size: image.Pt(s, s)}
		}))
	}
	if c.Pinned {
		row = append(row, indicator(iconW(icPin, 20, p.TextSecondary)))
	}
	if chev > 0 {
		// The chevron slides in, pushing the indicators aside.
		row = append(row, layout.Rigid(func(gtx C) D {
			full := record(gtx, func(gtx C) D { return layout.Inset{Left: 8}.Layout(gtx, iconW(icChevron, 22, p.TextSecondary)) })
			w := lerpInt(0, full.size.X, chev)
			defer clip.Rect{Max: image.Pt(w, full.size.Y)}.Push(gtx.Ops).Pop()
			withOpacity(gtx, chev, func() { full.at(gtx, 0, 0) })
			return D{Size: image.Pt(w, full.size.Y)}
		}))
	}
	return row
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

func receiptIcon(r model.Receipt, p *Palette, out bool) (*icon.Icon, color.NRGBA) {
	col := p.TextSecondary
	if out {
		col = p.MetaOut
	}
	switch r {
	case model.Failed:
		return icFailed, p.Danger
	case model.Pending:
		return icClock, col
	case model.Sent:
		return icTick, col
	case model.Delivered:
		return icTicks, col
	default:
		return icTicks, p.TickRead
	}
}

// layoutBanner shows connection and sync status above the chat list, like
// WhatsApp's "Computer not connected" notice.
func (u *UI) layoutBanner(gtx C) D {
	var msg string
	switch {
	case u.conn.State == model.StateConnecting:
		msg = "Connecting…"
	case u.conn.State == model.StateOffline:
		msg = "Computer not connected. Reconnecting…"
	case u.syncPct >= 0:
		msg = "Syncing chats… " + itoa(u.syncPct) + "%"
	default:
		return D{}
	}
	p := u.pal
	return layout.Inset{Left: 21, Right: 21, Bottom: 10}.Layout(gtx, func(gtx C) D {
		gtx.Constraints.Min.X = gtx.Constraints.Max.X
		return background(gtx, p.Banner, 12, func(gtx C) D {
			return layout.Inset{Left: 14, Right: 14, Top: 10, Bottom: 10}.Layout(gtx,
				u.label(14, msg, p.BannerText, labelOpts{maxLines: 2}).Layout)
		})
	})
}

// mix blends a toward b by t (0–1).
func mix(a, b color.NRGBA, t float32) color.NRGBA {
	l := func(x, y uint8) uint8 { return uint8(float32(x) + (float32(y)-float32(x))*t) }
	return color.NRGBA{R: l(a.R, b.R), G: l(a.G, b.G), B: l(a.B, b.B), A: l(a.A, b.A)}
}
