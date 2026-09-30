package ui

import (
	"fmt"
	"image"
	"math"
	"strings"

	"gioui.org/font"
	"gioui.org/layout"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

type channelState struct {
	add, discover widget.Clickable
	search        widget.Editor
	list          widget.List
}

// followers formats a follower count the way WhatsApp does: 17k, 736k,
// 1.4m. Counts are truncated, with one decimal below 10.
func followers(n int) string {
	short := func(v float64, unit string) string {
		if v < 10 {
			return strings.TrimSuffix(fmt.Sprintf("%.1f", math.Floor(v*10)/10), ".0") + unit
		}
		return fmt.Sprintf("%d", int(v)) + unit
	}
	switch {
	case n <= 0:
		return "Channel"
	case n >= 1_000_000:
		return short(float64(n)/1e6, "m") + " followers"
	case n >= 1_000:
		return short(float64(n)/1e3, "k") + " followers"
	case n == 1:
		return "1 follower"
	}
	return fmt.Sprintf("%d followers", n)
}

func isChannelID(id string) bool { return strings.HasSuffix(id, "@newsletter") }

func (u *UI) channelByID(id string) *model.Channel {
	if !isChannelID(id) {
		return nil
	}
	for _, ch := range u.channels {
		if ch.ID == id {
			return ch
		}
	}
	return nil
}

// channelChat adapts a channel to the chat row and conversation views.
func channelChat(ch *model.Channel) *model.Chat {
	// WhatsApp doesn't mark muted channels in the list.
	c := &model.Chat{ID: ch.ID, Name: ch.Name, Unread: ch.Unread, Time: ch.Time}
	if ch.Last != nil {
		// Channel posts have no delivery ticks.
		last := *ch.Last
		last.FromMe = false
		c.Last = &last
	}
	return c
}

// layoutChannelList draws the Channels page: followed channels, then
// suggestions with Follow buttons.
func (u *UI) layoutChannelList(gtx C) D {
	q := strings.ToLower(trimSpace(u.channel.search.Text()))
	var followed []*model.Channel
	for _, ch := range u.channels {
		if q == "" || strings.Contains(strings.ToLower(ch.Name), q) {
			followed = append(followed, ch)
		}
	}
	suggested := u.suggested
	if q != "" {
		suggested = nil
	}
	n := len(followed)
	if len(suggested) > 0 {
		n += 1 + len(suggested) + 1 // heading, suggestions, "Discover more"
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx C) D {
			return u.pageHeader(gtx, "Channels", u.headerButton(&u.channel.add, icAddCircle, 27))
		}),
		layout.Rigid(func(gtx C) D { return u.searchBox(gtx, &u.channel.search, "Search") }),
		layout.Flexed(1, func(gtx C) D {
			return u.scrollList(gtx, &u.channel.list, n, func(gtx C, i int) D {
				if i < len(followed) {
					if i == 0 {
						return layout.Inset{Top: 2.5}.Layout(gtx, func(gtx C) D { return u.channelRow(gtx, followed[i]) })
					}
					return u.channelRow(gtx, followed[i])
				}
				i -= len(followed)
				switch {
				case i == 0:
					return u.sectionLabel(gtx, "Find channels to follow", layout.Inset{Left: 20, Top: 24, Bottom: 10},
						labelOpts{weight: font.SemiBold, maxLines: 1})
				case i <= len(suggested):
					return u.suggestedRow(gtx, suggested[i-1])
				}
				return layout.Inset{Left: 20, Right: 30, Top: 14, Bottom: 20}.Layout(gtx, func(gtx C) D {
					return u.outlineButton(gtx, &u.channel.discover, "Discover more")
				})
			})
		}),
	)
}

func (u *UI) channelRow(gtx C, ch *model.Channel) D {
	c := channelChat(ch)
	click := u.btn("channel:" + ch.ID)
	if click.Clicked(gtx) {
		u.open(c)
		ch.Unread = 0
	}
	return u.chatRow(gtx, c, rowOpts{
		click:   click,
		sel:     u.sidebar.openSel.of(ch.ID),
		avatar:  func(gtx C) D { return u.avatarOf(gtx, ch.ID, avatarChannel, 51) },
		avatarW: 11,
		textGap: 15,
	})
}

// suggestedRow is a channel to follow: picture, name, follower count and a
// Follow button.
func (u *UI) suggestedRow(gtx C, ch *model.Channel) D {
	p := u.pal
	return layout.Inset{Left: 22, Right: 33.5}.Layout(gtx, func(gtx C) D {
		return vcenter(gtx, gtx.Dp(76), func(gtx C) D {
			return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(func(gtx C) D { return u.avatarOf(gtx, ch.ID, avatarChannel, 51) }),
				layout.Rigid(layout.Spacer{Width: 19}.Layout),
				layout.Flexed(1, func(gtx C) D {
					return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
						layout.Rigid(func(gtx C) D { return u.nameWithBadge(gtx, ch.Name, 17.5, p.Text, ch.Verified) }),
						layout.Rigid(layout.Spacer{Height: 2}.Layout),
						layout.Rigid(u.label(15.3, followers(ch.Followers), p.TextSecondary).Layout),
					)
				}),
				layout.Rigid(layout.Spacer{Width: 12}.Layout),
				layout.Rigid(func(gtx C) D {
					c := u.btn("follow:" + ch.ID)
					if c.Clicked(gtx) {
						u.backend.FollowChannel(ch.ID)
					}
					return clickable(gtx, c, func(gtx C) D {
						bg := mix(p.ChipActive, p.Green, 0.15*u.hover(gtx, c))
						sz := image.Pt(gtx.Dp(76), gtx.Dp(34))
						fillRRect(gtx, image.Rectangle{Max: sz}, sz.Y/2, bg)
						gtx.Constraints = layout.Exact(sz)
						return layout.Center.Layout(gtx,
							u.label(15, "Follow", p.ChipActiveText, labelOpts{weight: font.SemiBold, maxLines: 1}).Layout)
					})
				}),
			)
		})
	})
}

// outlineButton is a full-width pill with a thin border.
func (u *UI) outlineButton(gtx C, c *widget.Clickable, txt string) D {
	p := u.pal
	return clickable(gtx, c, func(gtx C) D {
		sz := image.Pt(gtx.Constraints.Max.X, gtx.Dp(44))
		bg := mix(p.Panel, p.Hover, u.hover(gtx, c))
		borderRRect(gtx, image.Rectangle{Max: sz}, sz.Y/2, bg, p.ChipBorder)
		gtx.Constraints = layout.Exact(sz)
		return layout.Center.Layout(gtx, u.label(15.5, txt, p.Green, labelOpts{weight: font.SemiBold, maxLines: 1}).Layout)
	})
}

// searchBox is the rounded search field under a page header.
func (u *UI) searchBox(gtx C, e *widget.Editor, hint string) D {
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
							ed := material.Editor(u.th, e, hint)
							ed.TextSize = 15.5
							ed.Color = p.Text
							ed.HintColor = p.TextSecondary
							return ed.Layout(gtx)
						}),
					)
				})
			})
		})
	})
}
