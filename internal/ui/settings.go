package ui

import (
	"image"
	"image/color"
	"strconv"
	"strings"

	"gioui.org/font"
	"gioui.org/layout"
	"gioui.org/text"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

type settingsState struct {
	snippets *snippetSettings
	search   widget.Editor
	list     widget.List
	items    [len(settingsItems)]widget.Clickable
	photo    widget.Clickable // your picture, which opens Profile
	// hits are what search found for hitsFor, the query.
	hits    []settingsHit
	hitsFor string

	// detail is the open category (an index of settingsItems) plus one,
	// or 0 while the list shows. sub is a page inside it ("" for the
	// category itself), such as a privacy setting's choices.
	detail     int
	sub        string
	back       widget.Clickable
	detailList widget.List

	// account is your profile and privacy settings, read when Settings
	// opens and on every AccountEvent.
	account *model.Account
	// editing is the profile field being edited (editName or editAbout),
	// or 0; editor holds its text.
	editing int
	editor  widget.Editor
	save    widget.Clickable
	page    []settingsSection // the open page's rows (see settingsRows)
	stale   bool
	// picking is set while the system's Open dialog chooses a profile
	// photo; the path (or "" when cancelled) comes back on picked.
	picking bool
	picked  chan string
}

// settingsItems mirrors WhatsApp Desktop's settings menu. Profile isn't
// listed: the row with your picture opens it. Extra features and Snippets
// are this app's own.
var settingsItems = [...]listItem{
	{ic: icLaptop, title: "General", sub: "Startup and close"},
	{ic: icAccount, title: "Profile", sub: "Name, profile picture, about"},
	{ic: icKey, title: "Account", sub: "Security notifications, account info"},
	{ic: icLockOutline, title: "Privacy", sub: "Blocked contacts, disappearing messages"},
	{title: "Chats", sub: "Theme, wallpaper, chat settings"},
	{ic: icVideoLine, title: "Video & voice", sub: "Camera, microphone and speakers"},
	{ic: icBell, title: "Notifications", sub: "Messages, groups, sounds"},
	{ic: icStorage, title: "Storage", sub: "Automatic downloads"},
	{ic: icKeyboard, title: "Keyboard shortcuts", sub: "Quick actions"},
	{ic: icExtension, title: "Extra features", sub: "Slash commands and more, not in WhatsApp"},
	{ic: icDocument, title: "Snippets", sub: "Saved messages and payloads"},
	{ic: icHelp, title: "Help", sub: "Help centre, contact us, privacy policy"},
	{ic: icLogout, title: "Log out", danger: true},
}

const (
	settingGeneral = iota
	settingProfile // opened from your picture's row
	settingAccount
	settingPrivacy
	settingChats // drawn with the rail's Chats glyph
	settingMedia // Video & voice
	settingNotifications
	settingStorage
	settingShortcuts
	settingExtras // this app's own features (extras.go)
	settingSnippets
	settingHelp
	settingLogout
)

var settingsGeom = listGeom{hoverLeft: 18.5, hoverRight: 29, iconCenter: 31.7, textLeft: 68.4, height: 72, subHeight: 72, padY: 10}

// settingsMeGeom is the row with your picture, name and about.
var settingsMeGeom = listGeom{hoverLeft: 18.5, hoverRight: 29, iconCenter: 41, textLeft: 84, height: 92, subHeight: 92, padY: 12}

// settingsHit is a search result: a category, or a setting on its page.
type settingsHit struct {
	category int
	key      string // the setting's row key; "" for the category
	title    string
}

// layoutSettingsList draws the Settings page: its title, search, your
// picture, name and about (which open Profile) and the categories.
func (u *UI) layoutSettingsList(gtx C) D {
	p := u.pal
	s := &u.settings
	if s.items[settingLogout].Clicked(gtx) {
		u.confirmLogout()
	}
	for k := range settingLogout {
		if s.items[k].Clicked(gtx) {
			u.openSettings(k)
		}
	}
	if s.photo.Clicked(gtx) {
		u.openSettings(settingProfile)
	}
	u.updateProfilePhoto()
	if s.detail != 0 {
		return u.layoutSettingsDetail(gtx)
	}
	if s.account == nil {
		s.account = u.backend.Account()
	}
	if q := strings.ToLower(trimSpace(s.search.Text())); q != "" {
		return u.layoutSettingsHits(gtx, q)
	}
	var shown []int
	for i := range settingsItems {
		if i != settingProfile {
			shown = append(shown, i)
		}
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx C) D { return u.pageHeader(gtx, "Settings") }),
		layout.Rigid(func(gtx C) D { return u.settingsSearch(gtx) }),
		layout.Flexed(1, func(gtx C) D {
			// You, the items, then a closing divider.
			return u.scrollList(gtx, &s.list, len(shown)+2, func(gtx C, i int) D {
				switch {
				case i == 0:
					return layout.Inset{Top: 6, Bottom: 6}.Layout(gtx, u.settingsMe)
				case i == len(shown)+1:
					return layout.Inset{Left: 29.5, Right: 40, Top: 14, Bottom: 24}.Layout(gtx, func(gtx C) D {
						h := max(1, gtx.Dp(1))
						fillRect(gtx, image.Rect(0, 0, gtx.Constraints.Max.X, h), p.Divider)
						return D{Size: image.Pt(gtx.Constraints.Max.X, h)}
					})
				}
				k := shown[i-1]
				return u.layoutListItem(gtx, &s.items[k], settingsItem(k), settingsGeom)
			})
		}),
	)
}

// settingsItem is the list row of category k.
func settingsItem(k int) listItem {
	it := settingsItems[k]
	if k == settingChats {
		it.glyph = func(gtx C, col color.NRGBA) D { return chatsOutline(gtx, 26, col) }
	}
	return it
}

// settingsMe is the row with your picture, name and about, which opens
// Profile.
func (u *UI) settingsMe(gtx C) D {
	p := u.pal
	s := &u.settings
	about := ""
	if s.account != nil {
		about = s.account.About
	}
	it := listItem{
		glyph: func(gtx C, _ color.NRGBA) D { return u.avatar(gtx, u.meID, u.meName(), false, 62) },
		content: func(gtx C) D {
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
				layout.Rigid(u.label(18, u.meName(), p.Text, labelOpts{weight: font.Medium, maxLines: 1}).Layout),
				layout.Rigid(func(gtx C) D {
					if about == "" {
						return D{}
					}
					return layout.Inset{Top: 2}.Layout(gtx, u.label(15.2, about, p.TextSecondary, labelOpts{maxLines: 1}).Layout)
				}),
			)
		},
	}
	return u.layoutListItem(gtx, &s.photo, it, settingsMeGeom)
}

// layoutSettingsHits lists what search finds: the categories, then the
// settings on their pages, each over its category's name.
func (u *UI) layoutSettingsHits(gtx C, q string) D {
	p := u.pal
	s := &u.settings
	if s.hitsFor != q {
		s.hits, s.hitsFor = u.settingsSearchHits(q), q
		s.list.Position = layout.Position{}
	}
	hitKey := func(h settingsHit) string { return "settings:hit:" + strconv.Itoa(h.category) + ":" + h.key }
	for _, h := range s.hits {
		if h.key != "" && u.btn(hitKey(h)).Clicked(gtx) {
			u.openSettings(h.category)
			return u.layoutSettingsDetail(gtx)
		}
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx C) D { return u.pageHeader(gtx, "Settings") }),
		layout.Rigid(func(gtx C) D { return u.settingsSearch(gtx) }),
		layout.Flexed(1, func(gtx C) D {
			if len(s.hits) == 0 {
				return layout.Inset{Top: 40, Left: 30, Right: 30}.Layout(gtx, func(gtx C) D {
					gtx.Constraints.Min.X = gtx.Constraints.Max.X
					return u.label(15, "No settings found", p.TextSecondary, labelOpts{align: text.Middle}).Layout(gtx)
				})
			}
			return u.scrollList(gtx, &s.list, len(s.hits), func(gtx C, i int) D {
				h := s.hits[i]
				if h.key == "" {
					return u.layoutListItem(gtx, &s.items[h.category], settingsItem(h.category), settingsGeom)
				}
				it := settingsItem(h.category)
				it.title, it.sub, it.danger = h.title, it.title, false
				return u.layoutListItem(gtx, u.btn(hitKey(h)), it, settingsGeom)
			})
		}),
	)
}

// settingsSearchHits finds q in the categories' names and in the titles
// of the settings on their pages. Snippets, which load from the database,
// are found only by name.
func (u *UI) settingsSearchHits(q string) []settingsHit {
	var cats, rows []settingsHit
	for k, it := range settingsItems {
		if strings.Contains(strings.ToLower(it.title+" "+it.sub), q) {
			cats = append(cats, settingsHit{category: k, title: it.title})
		}
		if k == settingSnippets || k == settingLogout {
			continue
		}
		seen := map[string]bool{}
		for _, sec := range u.categoryPage(k) {
			for _, r := range sec.rows {
				if r.title == "" || seen[r.title] || !r.shown() || !strings.Contains(strings.ToLower(r.title), q) {
					continue
				}
				seen[r.title] = true
				rows = append(rows, settingsHit{category: k, key: r.key, title: r.title})
			}
		}
	}
	return append(cats, rows...)
}

// settingsSearch is the settings filter field; it's outlined in green
// while focused, like WhatsApp's.
func (u *UI) settingsSearch(gtx C) D {
	p := u.pal
	e := &u.settings.search
	focused := gtx.Focused(e)
	return layout.Inset{Left: 19, Right: 20, Bottom: 8}.Layout(gtx, func(gtx C) D {
		gtx.Constraints.Min.X = gtx.Constraints.Max.X
		m := record(gtx, func(gtx C) D {
			return vcenter(gtx, gtx.Dp(46), func(gtx C) D {
				return layout.Inset{Left: 18, Right: 10}.Layout(gtx, func(gtx C) D {
					return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
						layout.Rigid(iconW(icSearch, 24, p.TextSecondary)),
						layout.Rigid(layout.Spacer{Width: 14}.Layout),
						layout.Flexed(1, func(gtx C) D {
							ed := material.Editor(u.th, e, "Search settings")
							ed.TextSize = 16.5
							ed.Color = p.Text
							ed.HintColor = p.TextSecondary
							return ed.Layout(gtx)
						}),
					)
				})
			})
		})
		r := image.Rectangle{Max: m.size}
		var bg, border color.NRGBA = p.Search, p.Search
		if focused {
			bg, border = p.Panel, p.Green
		}
		fillRRect(gtx, r, r.Dy()/2, border)
		inset := max(1, gtx.Dp(2))
		fillRRect(gtx, r.Inset(inset), r.Dy()/2-inset, bg)
		m.at(gtx, 0, 0)
		return D{Size: m.size}
	})
}
