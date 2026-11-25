package ui

import (
	"image"

	"gioui.org/font"
	"gioui.org/layout"
	"gioui.org/widget"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

type communityState struct {
	list     widget.List
	expanded map[string]bool // communities showing all their groups
}

// communityGroupsShown is how many linked groups a collapsed community
// lists under its announcements, before "View all".
const communityGroupsShown = 2

type communityEntry struct {
	kind      int // entryHeader, entryGroup, entryViewAll, entryEnd, entryDivider
	community *model.Community
	chat      *model.Chat
	announce  bool
}

const (
	entryHeader = iota
	entryGroup
	entryViewAll
	entryEnd // the margin of a community without "View all"
	entryDivider
)

func (u *UI) communityEntries() []communityEntry {
	var entries []communityEntry
	for i, c := range u.communities {
		if i > 0 {
			entries = append(entries, communityEntry{kind: entryDivider})
		}
		entries = append(entries, communityEntry{kind: entryHeader, community: c})
		if ch := u.chatByID(c.Announcements); ch != nil {
			entries = append(entries, communityEntry{kind: entryGroup, community: c, chat: ch, announce: true})
		}
		var groups []*model.Chat
		for _, id := range c.Groups {
			if ch := u.chatByID(id); ch != nil {
				groups = append(groups, ch)
			}
		}
		more := len(groups) > communityGroupsShown
		if more && !u.commun.expanded[c.ID] {
			groups = groups[:communityGroupsShown]
		}
		for _, ch := range groups {
			entries = append(entries, communityEntry{kind: entryGroup, community: c, chat: ch})
		}
		// WhatsApp always offers View all, which opens the community's home.
		// Ours unfolds the groups you're in, so it shows only when some are
		// folded.
		end := entryEnd
		if more {
			end = entryViewAll
		}
		entries = append(entries, communityEntry{kind: end, community: c})
	}
	return entries
}

// layoutCommunityList draws the Communities page: each community with its
// announcements and most recent groups.
func (u *UI) layoutCommunityList(gtx C) D {
	entries := u.communityEntries()
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx C) D {
			return u.pageHeader(gtx, "Communities")
		}),
		layout.Flexed(1, func(gtx C) D {
			if len(entries) == 0 {
				return layout.Inset{Left: 27, Right: 27, Top: 20}.Layout(gtx, func(gtx C) D {
					l := u.label(15.2, "No communities yet. The ones you create or join on your phone show here.", u.pal.TextSecondary)
					l.MaxLines = 0
					return l.Layout(gtx)
				})
			}
			return u.scrollList(gtx, &u.commun.list, len(entries), func(gtx C, i int) D {
				return u.layoutCommunityEntry(gtx, entries[i])
			})
		}),
	)
}

func (u *UI) layoutCommunityEntry(gtx C, e communityEntry) D {
	p := u.pal
	switch e.kind {
	case entryHeader:
		// The name opens the announcements, once there are some.
		click := u.btn("community:" + e.community.ID)
		ch := u.chatByID(e.community.Announcements)
		if ch != nil && click.Clicked(gtx) {
			u.open(ch)
		}
		defer u.hiding(gtx, "community:"+e.community.ID, click.Hovered())()
		if ch == nil {
			click = nil
		}
		return u.simpleRow(gtx, click, func(gtx C) D {
			return u.avatarOf(gtx, e.community.ID, avatarCommunity, 52)
		}, e.community.Name, font.SemiBold)
	case entryGroup:
		c := e.chat
		click := u.btn("cgroup:" + c.ID)
		if click.Clicked(gtx) {
			u.open(c)
		}
		o := rowOpts{
			click:   click,
			sel:     u.sidebar.openSel.of(c.ID),
			avatarW: 15,
			textGap: 21,
			avatar:  func(gtx C) D { return u.avatar(gtx, c.ID, c.Name, true, 43) },
		}
		if e.announce {
			// The announcement group is named after the community;
			// WhatsApp labels it generically.
			ann := *c
			ann.Name = "Announcements"
			c = &ann
			o.avatarW, o.textGap = 15, 20
			o.avatar = func(gtx C) D {
				return roundedSquare(gtx, 51, 12, p.AnnounceBg, icCampaign, 25, p.AnnounceIcon)
			}
		}
		return u.chatRow(gtx, c, o)
	case entryViewAll:
		click := u.btn("viewall:" + e.community.ID)
		if click.Clicked(gtx) {
			if u.commun.expanded == nil {
				u.commun.expanded = map[string]bool{}
			}
			u.commun.expanded[e.community.ID] = !u.commun.expanded[e.community.ID]
		}
		label := "View all"
		if u.commun.expanded[e.community.ID] {
			label = "Show less"
		}
		return layout.Inset{Left: 91.5, Top: 17, Bottom: 29}.Layout(gtx, func(gtx C) D {
			return clickable(gtx, click, u.label(16.7, label, p.Green).Layout)
		})
	case entryEnd:
		return layout.Spacer{Height: 17}.Layout(gtx)
	case entryDivider:
		return layout.Inset{Left: 21, Right: 34, Bottom: 7}.Layout(gtx, func(gtx C) D {
			h := max(1, gtx.Dp(1))
			fillRect(gtx, image.Rect(0, 0, gtx.Constraints.Max.X, h), p.Divider)
			return D{Size: image.Pt(gtx.Constraints.Max.X, h)}
		})
	}
	return D{}
}

// simpleRow is a hoverable row with a picture and a single line of text. A
// nil c makes it a plain one.
func (u *UI) simpleRow(gtx C, c *widget.Clickable, pic layout.Widget, title string, weight font.Weight) D {
	p := u.pal
	return layout.Inset{Left: 13, Right: 18, Top: 2, Bottom: 2}.Layout(gtx, func(gtx C) D {
		row := func(gtx C) D {
			gtx.Constraints.Min.X = gtx.Constraints.Max.X
			bg := p.Panel
			if c != nil {
				bg = mix(p.Panel, p.Hover, u.hover(gtx, c))
			}
			return background(gtx, bg, 10, func(gtx C) D {
				return vcenter(gtx, gtx.Dp(76.3), func(gtx C) D {
					return layout.Inset{Left: 11, Right: 14}.Layout(gtx, func(gtx C) D {
						return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
							layout.Rigid(pic),
							layout.Rigid(layout.Spacer{Width: 15}.Layout),
							layout.Flexed(1, u.label(17, title, p.Text, labelOpts{weight: weight, maxLines: 1}).Layout),
						)
					})
				})
			})
		}
		if c == nil {
			return row(gtx)
		}
		return clickable(gtx, c, row)
	})
}
