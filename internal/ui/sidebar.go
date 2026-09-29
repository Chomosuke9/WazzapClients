package ui

import (
	"image"
	"image/color"
	"strings"

	"gioui.org/font"
	"gioui.org/layout"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"github.com/chomosuke9/wazzapclients/internal/mock"
)

// layoutSidebar draws the chat list column: header, search, filter chips
// and the scrollable list of chats.
func (u *UI) layoutSidebar(gtx C) D {
	p := u.pal
	fill(gtx, p.Panel)
	u.sidebar.visible = u.filteredChats()

	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(u.layoutSidebarHeader),
		layout.Rigid(u.layoutSearch),
		layout.Rigid(u.layoutChips),
		layout.Flexed(1, u.layoutChatList),
	)
}

func (u *UI) layoutSidebarHeader(gtx C) D {
	return layout.Inset{Left: 20, Right: 10, Top: 10, Bottom: 6}.Layout(gtx, func(gtx C) D {
		return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
			layout.Flexed(1, u.label(22, "Chats", u.pal.Text, labelOpts{weight: font.Bold, maxLines: 1}).Layout),
			layout.Rigid(func(gtx C) D { return u.iconButton(gtx, &u.sidebar.newChat, icNewChat, u.pal.Icon, false) }),
			layout.Rigid(layout.Spacer{Width: 4}.Layout),
			layout.Rigid(func(gtx C) D { return u.iconButton(gtx, &u.sidebar.menu, icMenu, u.pal.Icon, false) }),
		)
	})
}

func (u *UI) layoutSearch(gtx C) D {
	p := u.pal
	return layout.Inset{Left: 12, Right: 12, Top: 4, Bottom: 8}.Layout(gtx, func(gtx C) D {
		gtx.Constraints.Min.X = gtx.Constraints.Max.X
		gtx.Constraints.Min.Y = gtx.Dp(40)
		return background(gtx, p.Search, 20, func(gtx C) D {
			return layout.Inset{Left: 14, Right: 14}.Layout(gtx, func(gtx C) D {
				gtx.Constraints.Min.Y = gtx.Dp(40)
				return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(func(gtx C) D { return u.icons.layout(gtx, icSearch, 20, p.SearchHint) }),
					layout.Rigid(layout.Spacer{Width: 14}.Layout),
					layout.Flexed(1, func(gtx C) D {
						e := material.Editor(u.th, &u.sidebar.search, "Search or start a new chat")
						e.TextSize = 15
						e.Color = p.Text
						e.HintColor = p.SearchHint
						return e.Layout(gtx)
					}),
				)
			})
		})
	})
}

func (u *UI) layoutChips(gtx C) D {
	p := u.pal
	return layout.Inset{Left: 12, Right: 12, Bottom: 8}.Layout(gtx, func(gtx C) D {
		var children []layout.FlexChild
		for i, name := range filterNames {
			if i > 0 {
				children = append(children, layout.Rigid(layout.Spacer{Width: 8}.Layout))
			}
			i, name := i, name
			children = append(children, layout.Rigid(func(gtx C) D {
				c := &u.sidebar.chips[i]
				active := u.sidebar.filter == i
				bg, fg := p.Chip, p.ChipText
				if active {
					bg, fg = p.ChipActive, p.ChipActiveFg
				} else if c.Hovered() {
					bg = p.RowSelected
				}
				return clickable(gtx, c, func(gtx C) D {
					return background(gtx, bg, 16, func(gtx C) D {
						return layout.Inset{Left: 12, Right: 12, Top: 6, Bottom: 6}.Layout(gtx,
							u.label(14, name, fg, labelOpts{weight: font.Medium, maxLines: 1}).Layout)
					})
				})
			}))
		}
		return layout.Flex{}.Layout(gtx, children...)
	})
}

func (u *UI) filteredChats() []*mock.Chat {
	q := strings.ToLower(trimSpace(u.sidebar.search.Text()))
	var out []*mock.Chat
	for _, c := range u.chats {
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
	showArchived := u.sidebar.filter == filterAll && trimSpace(u.sidebar.search.Text()) == ""
	n := len(chats)
	if showArchived {
		n++
	}
	l := material.List(u.th, &u.sidebar.list)
	l.AnchorStrategy = material.Overlay
	l.Indicator.Color = u.pal.TextSecondary
	l.Indicator.Color.A = 0x60
	l.Indicator.MinorWidth = 6
	return l.Layout(gtx, n, func(gtx C, i int) D {
		if showArchived {
			if i == 0 {
				return u.layoutArchivedRow(gtx)
			}
			i--
		}
		return u.layoutChatRow(gtx, chats[i])
	})
}

func (u *UI) layoutArchivedRow(gtx C) D {
	p := u.pal
	c := &u.sidebar.archived
	return layout.Inset{Left: 8, Right: 8}.Layout(gtx, func(gtx C) D {
		return clickable(gtx, c, func(gtx C) D {
			gtx.Constraints.Min.X = gtx.Constraints.Max.X
			bg := p.Panel
			if c.Hovered() {
				bg = p.RowHover
			}
			return background(gtx, bg, 10, func(gtx C) D {
				return layout.Inset{Left: 12, Right: 16, Top: 12, Bottom: 12}.Layout(gtx, func(gtx C) D {
					return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
						layout.Rigid(func(gtx C) D {
							// Centered in the avatar column so it lines up with rows below.
							gtx.Constraints = layout.Exact(image.Pt(gtx.Dp(49), gtx.Dp(24)))
							return layout.Center.Layout(gtx, func(gtx C) D { return u.icons.layout(gtx, icArchive, 20, p.Green) })
						}),
						layout.Rigid(layout.Spacer{Width: 15}.Layout),
						layout.Flexed(1, u.label(16, "Archived", p.Text).Layout),
						layout.Rigid(u.label(12, "3", p.Green, labelOpts{weight: font.SemiBold, maxLines: 1}).Layout),
					)
				})
			})
		})
	})
}

func (u *UI) rowClick(c *mock.Chat) *widget.Clickable {
	cl, ok := u.sidebar.rows[c]
	if !ok {
		cl = new(widget.Clickable)
		u.sidebar.rows[c] = cl
	}
	return cl
}

func (u *UI) layoutChatRow(gtx C, c *mock.Chat) D {
	p := u.pal
	click := u.rowClick(c)
	last := c.Last()
	now := u.now()

	return layout.Inset{Left: 8, Right: 8}.Layout(gtx, func(gtx C) D {
		return clickable(gtx, click, func(gtx C) D {
			gtx.Constraints.Min.X = gtx.Constraints.Max.X
			bg := p.Panel
			switch {
			case c == u.selected:
				bg = p.RowSelected
			case click.Hovered():
				bg = p.RowHover
			}
			return background(gtx, bg, 10, func(gtx C) D {
				gtx.Constraints.Min.Y = gtx.Dp(72)
				return layout.Inset{Left: 12, Right: 12}.Layout(gtx, func(gtx C) D {
					return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
						layout.Rigid(func(gtx C) D { return u.avatar(gtx, c.Name, c.IsGroup, 49) }),
						layout.Rigid(layout.Spacer{Width: 14}.Layout),
						layout.Flexed(1, func(gtx C) D {
							return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
								layout.Rigid(func(gtx C) D {
									timeCol := p.TextSecondary
									if c.Unread > 0 {
										timeCol = p.Green
									}
									var ts string
									if last != nil {
										ts = listTime(last.Time, now)
									}
									return layout.Flex{Alignment: layout.Baseline}.Layout(gtx,
										layout.Flexed(1, u.label(16.5, c.Name, p.Text).Layout),
										layout.Rigid(layout.Spacer{Width: 6}.Layout),
										layout.Rigid(u.label(12, ts, timeCol).Layout),
									)
								}),
								layout.Rigid(layout.Spacer{Height: 3}.Layout),
								layout.Rigid(func(gtx C) D { return u.layoutRowPreview(gtx, c, last) }),
							)
						}),
					)
				})
			})
		})
	})
}

// layoutRowPreview draws the second line of a chat row: last message preview
// followed by muted / pinned / unread indicators.
func (u *UI) layoutRowPreview(gtx C, c *mock.Chat, last *mock.Message) D {
	p := u.pal
	var children []layout.FlexChild
	icon := func(data []byte, col color.NRGBA, size unit.Dp) layout.FlexChild {
		return layout.Rigid(func(gtx C) D {
			return layout.Inset{Right: 3}.Layout(gtx, func(gtx C) D { return u.icons.layout(gtx, data, size, col) })
		})
	}

	switch {
	case c.Typing != "":
		who := "typing…"
		if c.IsGroup {
			who = c.Typing + " is typing…"
		}
		children = append(children, layout.Flexed(1, u.label(14, who, p.Green).Layout))
	case last == nil:
		children = append(children, layout.Flexed(1, layout.Spacer{}.Layout))
	default:
		if last.FromMe {
			data, col := receiptIcon(last.Receipt, p)
			children = append(children, icon(data, col, 18))
		}
		txt := last.Text
		if last.Kind == mock.KindImage {
			children = append(children, icon(icCamera, p.TextSecondary, 16))
			if txt == "" {
				txt = "Photo"
			}
		}
		if c.IsGroup && !last.FromMe && last.Sender != "" {
			txt = last.Sender + ": " + txt
		}
		children = append(children, layout.Flexed(1, u.label(14, txt, p.TextSecondary).Layout))
	}

	if c.Muted {
		children = append(children, layout.Rigid(func(gtx C) D {
			return layout.Inset{Left: 6}.Layout(gtx, func(gtx C) D { return u.icons.layout(gtx, icMuted, 18, p.TextSecondary) })
		}))
	}
	if c.Unread > 0 {
		children = append(children, layout.Rigid(func(gtx C) D {
			return layout.Inset{Left: 6}.Layout(gtx, func(gtx C) D { return u.badge(gtx, c.Unread, p.Badge) })
		}))
	}
	if c.Pinned {
		children = append(children, layout.Rigid(func(gtx C) D {
			return layout.Inset{Left: 6}.Layout(gtx, func(gtx C) D { return pinIcon(gtx, 18, p.TextSecondary) })
		}))
	}
	gtx.Constraints.Min.Y = gtx.Dp(20)
	return layout.Flex{Alignment: layout.Middle}.Layout(gtx, children...)
}

func receiptIcon(r mock.Receipt, p *Palette) ([]byte, color.NRGBA) {
	switch r {
	case mock.Pending:
		return icClock, p.Meta
	case mock.Sent:
		return icTick, p.Meta
	case mock.Delivered:
		return icTicks, p.Meta
	default:
		return icTicks, p.TickRead
	}
}
