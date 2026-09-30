package ui

import (
	"fmt"
	"image"
	"image/color"
	"strings"

	"gioui.org/font"
	"gioui.org/layout"
	"gioui.org/op"
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
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(u.layoutSidebarHeader),
		layout.Rigid(u.layoutSearch),
		layout.Rigid(u.layoutChips),
		layout.Rigid(u.layoutBanner),
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
						col := p.Green
						if u.sidebar.newChat.Hovered() {
							col = mix(col, rgb(0xffffff), 0.1)
						}
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

// chip draws one filter pill; active chips are green.
func (u *UI) chip(gtx C, c *widget.Clickable, active bool, w layout.Widget) D {
	p := u.pal
	bg, border := p.Chip, p.ChipBorder
	switch {
	case active:
		bg, border = p.ChipActive, p.ChipActiveBorder
	case c.Hovered():
		bg = p.Hover
	}
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
		parts := make([]part, len(filterNames))
		for i, name := range filterNames {
			i, name := i, name
			active := u.sidebar.filter == i
			fg := p.ChipText
			if active {
				fg = p.ChipActiveText
			}
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
			hiddenActive := u.sidebar.filter >= shown
			t := op.Offset(image.Pt(x, 0)).Push(gtx.Ops)
			d := u.chip(cgtx, &u.sidebar.more, hiddenActive, func(gtx C) D {
				gtx.Constraints.Min.X = more
				col := p.ChipText
				if hiddenActive {
					col = p.ChipActiveText
				}
				return layout.Center.Layout(gtx, iconW(icDropDown, 22, col))
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
	var out []*model.Chat
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
		if q != "" && !strings.Contains(strings.ToLower(c.Name), q) {
			continue
		}
		out = append(out, c)
	}
	return out
}

func (u *UI) layoutChatList(gtx C) D {
	chats := u.sidebar.visible
	l := material.List(u.th, &u.sidebar.list)
	l.AnchorStrategy = material.Overlay
	l.Indicator.Color = u.pal.TextSecondary
	l.Indicator.Color.A = 0x50
	l.Indicator.MinorWidth = 5
	l.Indicator.CornerRadius = 3
	return l.Layout(gtx, len(chats), func(gtx C, i int) D {
		return u.layoutChatRow(gtx, chats[i])
	})
}

func (u *UI) rowClick(c *model.Chat) *widget.Clickable {
	cl, ok := u.sidebar.rows[c.ID]
	if !ok {
		cl = new(widget.Clickable)
		u.sidebar.rows[c.ID] = cl
	}
	return cl
}

func (u *UI) layoutChatRow(gtx C, c *model.Chat) D {
	p := u.pal
	click := u.rowClick(c)
	last := c.Last

	return layout.Inset{Left: 13, Right: 18, Top: 2, Bottom: 2}.Layout(gtx, func(gtx C) D {
		return clickable(gtx, click, func(gtx C) D {
			gtx.Constraints.Min.X = gtx.Constraints.Max.X
			bg := p.Panel
			hovered := click.Hovered()
			switch {
			case u.selected != nil && c.ID == u.selected.ID:
				bg = p.Selected
			case hovered:
				bg = p.Hover
			}
			return background(gtx, bg, 10, func(gtx C) D {
				return vcenter(gtx, gtx.Dp(76), func(gtx C) D {
					return layout.Inset{Left: 12, Right: 14}.Layout(gtx, func(gtx C) D {
						return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
							layout.Rigid(func(gtx C) D { return u.avatar(gtx, c.ID, c.Name, c.IsGroup, 52) }),
							layout.Rigid(layout.Spacer{Width: 16}.Layout),
							layout.Flexed(1, func(gtx C) D {
								return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
									layout.Rigid(func(gtx C) D { return u.layoutRowTitle(gtx, c) }),
									layout.Rigid(layout.Spacer{Height: 3}.Layout),
									layout.Rigid(func(gtx C) D { return u.layoutRowPreview(gtx, c, last, hovered) }),
								)
							}),
						)
					})
				})
			})
		})
	})
}

func (u *UI) layoutRowTitle(gtx C, c *model.Chat) D {
	p := u.pal
	timeCol := p.TextSecondary
	if c.Unread > 0 {
		timeCol = p.Green
	}
	var ts string
	if c.Last != nil {
		ts = listTime(c.Last.Time, u.now())
	}
	children := []layout.FlexChild{
		layout.Flexed(1, func(gtx C) D {
			if !c.Self {
				return u.label(17.5, c.Name, p.Text).Layout(gtx)
			}
			// "Name (You)": the name truncates, the suffix never does.
			return layout.Flex{Alignment: layout.Baseline}.Layout(gtx,
				layout.Flexed(1, func(gtx C) D {
					gtx.Constraints.Min.X = 0
					return u.label(17.5, c.Name, p.Text).Layout(gtx)
				}),
				layout.Rigid(u.label(17.5, "  (You)", p.Text).Layout),
			)
		}),
		layout.Rigid(layout.Spacer{Width: 6}.Layout),
		layout.Rigid(u.label(13.2, ts, timeCol).Layout),
	}
	return layout.Flex{Alignment: layout.Baseline}.Layout(gtx, children...)
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
// followed by muted / pinned / unread indicators.
func (u *UI) layoutRowPreview(gtx C, c *model.Chat, last *model.Message, hovered bool) D {
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
		case last.Media != model.MediaNone:
			col := p.TextSecondary
			if last.Media == model.MediaVoice && !last.FromMe && c.Unread > 0 {
				col = p.Green
			}
			children = append(children, small(mediaIcon(last.Media), col, 18, 4))
			txt = mediaLabel(last)
		}
		children = append(children, layout.Flexed(1, u.label(size, firstLine(plainText(txt)), p.TextSecondary, labelOpts{maxLines: 1, italic: italic}).Layout))
	}

	// Indicators are rigid children of the outer row, so Flex sizes them
	// first and the preview gets whatever width is left.
	row := []layout.FlexChild{layout.Flexed(1, func(gtx C) D {
		return layout.Flex{Alignment: layout.Middle}.Layout(gtx, children...)
	})}
	indicator := func(w layout.Widget) layout.FlexChild {
		return layout.Rigid(func(gtx C) D { return layout.Inset{Left: 8}.Layout(gtx, w) })
	}
	if c.Muted {
		row = append(row, indicator(iconW(icMuted, 20, p.TextSecondary)))
	}
	if c.Unread > 0 {
		row = append(row, indicator(func(gtx C) D { return u.badge(gtx, c.Unread) }))
	}
	if c.Pinned {
		row = append(row, indicator(iconW(icPin, 20, p.TextSecondary)))
	}
	if hovered {
		row = append(row, indicator(iconW(icChevron, 22, p.TextSecondary)))
	}
	return vcenter(gtx, gtx.Dp(22), func(gtx C) D {
		return layout.Flex{Alignment: layout.Middle}.Layout(gtx, row...)
	})
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
