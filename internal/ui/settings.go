package ui

import (
	"image"
	"image/color"
	"strings"

	"gioui.org/layout"
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

// settingsItems mirrors WhatsApp Desktop's settings menu, without Video &
// voice: this app has no calls to configure.
var settingsItems = [...]listItem{
	{ic: icLaptop, title: "General", sub: "Startup and close"},
	{ic: icAccount, title: "Profile", sub: "Name, profile picture, about"},
	{ic: icKey, title: "Account", sub: "Security notifications, account info"},
	{ic: icLockOutline, title: "Privacy", sub: "Blocked contacts, disappearing messages"},
	{title: "Chats", sub: "Theme, wallpaper, chat settings"},
	{ic: icBell, title: "Notifications", sub: "Messages, groups, sounds"},
	{ic: icKeyboard, title: "Keyboard shortcuts", sub: "Quick actions"},
	{ic: icExtension, title: "Extra features", sub: "Slash commands and more, not in WhatsApp"},
	{ic: icDocument, title: "Snippets", sub: "Saved messages and payloads"},
	{ic: icHelp, title: "Help and feedback", sub: "Help centre, contact us, privacy policy"},
	{ic: icLogout, title: "Log out", danger: true},
}

const (
	settingGeneral = iota
	settingProfile
	settingAccount
	settingPrivacy
	settingChats // drawn with the rail's Chats glyph
	settingNotifications
	settingShortcuts
	settingExtras // this app's own features (extras.go)
	settingSnippets
	settingHelp
	settingLogout
)

var settingsGeom = listGeom{hoverLeft: 18.5, hoverRight: 29, iconCenter: 31.7, textLeft: 68.4, height: 72, subHeight: 72, padY: 10}

// layoutSettingsList draws the Settings page: your name, search, your
// picture and the settings categories.
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
	q := strings.ToLower(trimSpace(s.search.Text()))
	var shown []int
	for i, it := range settingsItems {
		if q == "" || strings.Contains(strings.ToLower(it.title+" "+it.sub), q) {
			shown = append(shown, i)
		}
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx C) D { return u.pageHeader(gtx, u.meName()) }),
		layout.Rigid(func(gtx C) D { return u.settingsSearch(gtx) }),
		layout.Flexed(1, func(gtx C) D {
			// Picture, the items, then a closing divider.
			return u.scrollList(gtx, &s.list, len(shown)+2, func(gtx C, i int) D {
				switch {
				case i == 0:
					return layout.Inset{Top: 22, Bottom: 68}.Layout(gtx, func(gtx C) D {
						gtx.Constraints.Min.X = gtx.Constraints.Max.X - gtx.Dp(8)
						return layout.N.Layout(gtx, func(gtx C) D {
							return clickable(gtx, &s.photo, func(gtx C) D {
								return u.avatar(gtx, u.meID, u.meName(), false, 127)
							})
						})
					})
				case i == len(shown)+1:
					return layout.Inset{Left: 29.5, Right: 40, Top: 14, Bottom: 24}.Layout(gtx, func(gtx C) D {
						h := max(1, gtx.Dp(1))
						fillRect(gtx, image.Rect(0, 0, gtx.Constraints.Max.X, h), p.Divider)
						return D{Size: image.Pt(gtx.Constraints.Max.X, h)}
					})
				}
				k := shown[i-1]
				it := settingsItems[k]
				if k == settingChats {
					it.glyph = func(gtx C, col color.NRGBA) D { return chatsOutline(gtx, 26, col) }
				}
				return u.layoutListItem(gtx, &s.items[k], it, settingsGeom)
			})
		}),
	)
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
