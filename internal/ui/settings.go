package ui

import (
	"image"
	"image/color"
	"strconv"
	"strings"

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
	mem     memStats // Performance > Advanced's Memory now
}

// settingsItems mirrors WhatsApp Web's settings menu. General, Appearance,
// Video & voice, Performance and Extra features are this app's own.
var settingsItems = [...]listItem{
	{ic: icLaptop, title: "General", sub: "Startup and close"},
	{ic: icAccount, title: "Profile", sub: "Name, profile picture, about"},
	{ic: icKey, title: "Account", sub: "Security notifications, account info, accounts"},
	{ic: icLockOutline, title: "Privacy", sub: "Blocked contacts, disappearing messages"},
	{title: "Chats", sub: "Theme, wallpaper, chat settings"},
	{ic: icPalette, title: "Appearance", sub: "Palette, app theme color"},
	{ic: icVideoLine, title: "Video & voice", sub: "Camera, microphone and speakers"},
	{ic: icBell, title: "Notifications", sub: "Messages, groups, sounds"},
	{ic: icKeyboard, title: "Keyboard shortcuts", sub: "Quick actions"},
	{ic: icSpeed, title: "Performance", sub: "Animations, media, memory and CPU"},
	{ic: icExtension, title: "Extra features", sub: "Slash commands and more, not in WhatsApp"},
	{ic: icHelp, title: "Help and feedback", sub: "Help centre, contact us, privacy policy"},
	{ic: icLogout, title: "Log out", danger: true},
}

const (
	settingGeneral = iota
	settingProfile // your picture opens it too
	settingAccount
	settingPrivacy
	settingChats      // drawn with the rail's Chats glyph
	settingAppearance // palette and App theme color (the Android app's Appearance)
	settingMedia      // Video & voice
	settingNotifications
	settingShortcuts   // a dialog (openShortcuts)
	settingPerformance // perf.go
	settingExtras      // this app's own features (extras.go)
	settingHelp
	settingLogout
)

var settingsGeom = listGeom{hoverLeft: 18.5, hoverRight: 29, iconCenter: 31.7, textLeft: 68.4, height: 72, subHeight: 72, padY: 10}

// settingsHit is a search result: a category, or a setting on its page or
// on a page inside it (sub).
type settingsHit struct {
	category int
	sub      string
	key      string // the setting's row key; "" for the category
	title    string
}

// settingsSearched are the pages inside a category whose settings search
// finds too: those with settings of their own, rather than one setting's
// choices.
var settingsSearched = map[int][]string{
	settingPrivacy:       {"lastseen", "photo", "about", "timer"},
	settingChats:         {"autodownload", "emojianim"},
	settingNotifications: {"notifymessages", "notifygroups"},
	settingPerformance:   {"advanced", "stickers"},
}

// layoutSettingsList draws the Settings page: your name, search, your
// picture and about (which open Profile) and the categories.
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
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx C) D { return u.pageHeader(gtx, u.meName()) }),
		layout.Rigid(func(gtx C) D { return u.settingsSearch(gtx) }),
		layout.Flexed(1, func(gtx C) D {
			// You, the items, then a closing divider.
			return u.scrollList(gtx, &s.list, len(settingsItems)+2, func(gtx C, i int) D {
				switch {
				case i == 0:
					return layout.Inset{Top: 10, Bottom: 18}.Layout(gtx, u.settingsMe)
				case i == len(settingsItems)+1:
					return layout.Inset{Left: 29.5, Right: 40, Top: 14, Bottom: 24}.Layout(gtx, func(gtx C) D {
						h := max(1, gtx.Dp(1))
						fillRect(gtx, image.Rect(0, 0, gtx.Constraints.Max.X, h), p.Divider)
						return D{Size: image.Pt(gtx.Constraints.Max.X, h)}
					})
				}
				k := i - 1
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

// settingsMe is your picture, big and centered, with your about in a
// bubble over it; it opens Profile.
func (u *UI) settingsMe(gtx C) D {
	p := u.pal
	s := &u.settings
	about := ""
	if s.account != nil {
		about = s.account.About
	}
	gtx.Constraints.Min = image.Pt(gtx.Constraints.Max.X, 0)
	return layout.N.Layout(gtx, func(gtx C) D {
		gtx.Constraints.Min = image.Point{}
		return clickable(gtx, &s.photo, func(gtx C) D {
			gtx.Constraints.Min = image.Point{}
			pic := record(gtx, func(gtx C) D { return u.avatar(gtx, u.meID, u.meName(), false, 118) })
			if about == "" {
				pic.at(gtx, 0, 0)
				return D{Size: pic.size}
			}
			gtx.Constraints.Max.X = min(gtx.Constraints.Max.X-gtx.Dp(60), gtx.Dp(280))
			txt := record(gtx, u.label(14.5, about, p.Text, labelOpts{maxLines: 2, align: text.Middle}).Layout)
			bubble := txt.size.Add(image.Pt(gtx.Dp(28), gtx.Dp(18)))
			over := gtx.Dp(8) // how far the bubble hangs over the picture
			sz := image.Pt(max(bubble.X, pic.size.X), bubble.Y-over+pic.size.Y)
			pic.at(gtx, (sz.X-pic.size.X)/2, bubble.Y-over)
			r := image.Rectangle{Max: bubble}.Add(image.Pt((sz.X-bubble.X)/2, 0))
			borderRRect(gtx, r, gtx.Dp(16), p.Dialog, p.PopupBorder)
			txt.at(gtx, r.Min.X+gtx.Dp(14), gtx.Dp(9))
			return D{Size: sz}
		})
	})
}

// layoutSettingsHits lists what search finds: the categories, then the
// settings on their pages, each over the page's name.
func (u *UI) layoutSettingsHits(gtx C, q string) D {
	p := u.pal
	s := &u.settings
	if s.hitsFor != q {
		s.hits, s.hitsFor = u.settingsSearchHits(q), q
		s.list.Position = layout.Position{}
	}
	hitKey := func(h settingsHit) string {
		return "settings:hit:" + strconv.Itoa(h.category) + ":" + h.sub + ":" + h.key
	}
	for _, h := range s.hits {
		if h.key != "" && u.btn(hitKey(h)).Clicked(gtx) {
			u.openSettings(h.category)
			if s.detail == 0 {
				break // a dialog
			}
			if h.sub != "" {
				u.openSettingsSub(h.sub)
			}
			return u.layoutSettingsDetail(gtx)
		}
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx C) D { return u.pageHeader(gtx, u.meName()) }),
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
				if h.sub != "" {
					it.sub += " › " + settingsSubTitle(h.sub)
				}
				return u.layoutListItem(gtx, u.btn(hitKey(h)), it, settingsGeom)
			})
		}),
	)
}

// settingsSearchHits finds q in the categories' names and in the titles
// of the settings on their pages.
func (u *UI) settingsSearchHits(q string) []settingsHit {
	var cats, rows []settingsHit
	for k, it := range settingsItems {
		if strings.Contains(strings.ToLower(it.title+" "+it.sub), q) {
			cats = append(cats, settingsHit{category: k, title: it.title})
		}
		if k == settingLogout {
			continue
		}
		for _, sub := range append([]string{""}, settingsSearched[k]...) {
			seen := map[string]bool{}
			page := u.categoryPage(k)
			if sub != "" {
				page = u.settingsSubPage(sub)
			}
			add := func(key, title string) {
				if title != "" && !seen[title] && strings.Contains(strings.ToLower(title), q) {
					seen[title] = true
					rows = append(rows, settingsHit{category: k, sub: sub, key: key, title: title})
				}
			}
			for _, sec := range page {
				if len(sec.rows) > 0 && (sec.rows[0].kind == setRadio || sec.rows[0].kind == setCustom) {
					add("heading:"+sec.title, sec.title) // the setting its rows are the choices of
				}
				for _, r := range sec.rows {
					if r.shown() && !(sub != "" && r.kind == setRadio) {
						add(r.key, r.title)
					}
				}
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
							ed := material.Editor(u.th, e, "Search")
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
