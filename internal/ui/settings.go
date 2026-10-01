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

	"github.com/chomosuke9/wazzapclients/internal/desktop"
)

type settingsState struct {
	search widget.Editor
	list   widget.List
	items  [10]widget.Clickable

	// detail is the open category (an index of settingsItems) plus one,
	// or 0 while the list shows.
	detail     int
	sections   []settingsSection // of the open category, read when it opens
	back       widget.Clickable
	detailList widget.List
	toggles    [8]widget.Clickable
}

// settingsItems mirrors WhatsApp Desktop's settings menu. General,
// Notifications and Log out do something yet.
var settingsItems = []listItem{
	{ic: icLaptop, title: "General", sub: "Startup and close"},
	{ic: icAccount, title: "Profile", sub: "Name, profile picture, username"},
	{ic: icKey, title: "Account", sub: "Security notifications, account info"},
	{ic: icLockOutline, title: "Privacy", sub: "Blocked contacts, disappearing messages"},
	{title: "Chats", sub: "Theme, wallpaper, chat settings"},
	{ic: icVideoLine, title: "Video & voice", sub: "Camera, microphone & speakers"},
	{ic: icBell, title: "Notifications", sub: "Messages, groups, sounds"},
	{ic: icKeyboard, title: "Keyboard shortcuts", sub: "Quick actions"},
	{ic: icHelp, title: "Help and feedback", sub: "Help centre, contact us, privacy policy"},
	{ic: icLogout, title: "Log out", danger: true},
}

const (
	settingGeneral       = 0
	settingChats         = 4 // drawn with the rail's Chats glyph
	settingNotifications = 6
	settingLogout        = 9
)

var settingsGeom = listGeom{hoverLeft: 18.5, hoverRight: 29, iconCenter: 31.7, textLeft: 68.4, height: 72, subHeight: 72, padY: 10}

// layoutSettingsList draws the Settings page: your name, search, your
// picture and the settings categories.
func (u *UI) layoutSettingsList(gtx C) D {
	p := u.pal
	s := &u.settings
	if s.items[settingLogout].Clicked(gtx) {
		u.backend.Logout()
	}
	for _, k := range []int{settingGeneral, settingNotifications} {
		if s.items[k].Clicked(gtx) {
			u.openSettings(k)
		}
	}
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
							return u.avatar(gtx, u.meID, u.meName(), false, 127)
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

// openSettings opens a settings category (an index of settingsItems).
func (u *UI) openSettings(k int) {
	s := &u.settings
	s.detail = k + 1
	s.sections = u.settingsDetail(k)
	s.detailList.Position = layout.Position{}
}

// settingToggle is a setting with a checkbox.
type settingToggle struct {
	title, sub string
	on         bool
	set        func(on bool)
}

// settingsSection is a heading and the settings under it.
type settingsSection struct {
	title string
	rows  []settingToggle
	note  string // shown when there are no rows
}

// settingsDetail lists the settings of category k.
func (u *UI) settingsDetail(k int) []settingsSection {
	b := u.backend
	pref := func(key, title, sub string) settingToggle {
		return settingToggle{title: title, sub: sub, on: prefOn(b, key), set: func(on bool) { setPref(b, key, on) }}
	}
	switch k {
	case settingNotifications:
		return []settingsSection{
			{title: "Messages", rows: []settingToggle{
				pref(prefNotifyMessages, "Message notifications", "Show notifications for new messages"),
				pref(prefNotifyPreviews, "Show previews", "Show message text in notifications"),
				pref(prefNotifySound, "Sounds", "Play a sound for new messages"),
			}},
			{title: "Groups", rows: []settingToggle{
				pref(prefNotifyGroups, "Group notifications", "Show notifications for group messages"),
			}},
		}
	case settingGeneral:
		sec := settingsSection{title: "Startup and close"}
		h := u.host
		if h != nil && h.tray && h.o.Relaunch != nil {
			sec.rows = append(sec.rows, settingToggle{
				title: "Start " + appName + " at login",
				sub:   "Open in the background when you sign in, so messages notify you",
				on:    desktop.StartAtLogin(),
				set: func(on bool) {
					args := append(append([]string(nil), h.o.Relaunch...), "-background")
					if err := desktop.SetStartAtLogin(on, args); err != nil {
						u.toast("Couldn't change the startup setting.")
					}
				},
			})
		}
		if h != nil && h.tray {
			sec.rows = append(sec.rows, pref(prefBackground, "Keep running in the background",
				"Closing the window keeps "+appName+" in the notification area, so messages still notify you"))
		}
		if len(sec.rows) == 0 {
			sec.note = "Closing the window quits " + appName + " on this system."
		}
		return []settingsSection{sec}
	}
	return nil
}

// layoutSettingsDetail draws an open settings category: a header with a
// back arrow, then the settings with checkboxes, under their headings.
func (u *UI) layoutSettingsDetail(gtx C) D {
	p := u.pal
	s := &u.settings
	k := s.detail - 1
	if s.back.Clicked(gtx) {
		s.detail = 0
		return u.layoutSettingsList(gtx)
	}
	sections := s.sections
	type row struct {
		heading string
		note    string
		toggle  *settingToggle
		click   *widget.Clickable
	}
	var rows []row
	n := 0
	for i := range sections {
		sec := &sections[i]
		rows = append(rows, row{heading: sec.title})
		if sec.note != "" {
			rows = append(rows, row{note: sec.note})
		}
		for j := range sec.rows {
			if n == len(s.toggles) {
				break
			}
			rows = append(rows, row{toggle: &sec.rows[j], click: &s.toggles[n]})
			n++
		}
	}
	for _, r := range rows {
		if r.click != nil && r.click.Clicked(gtx) {
			r.toggle.on = !r.toggle.on
			r.toggle.set(r.toggle.on)
		}
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx C) D {
			return vcenter(gtx, gtx.Dp(68), func(gtx C) D {
				return layout.Inset{Left: 10, Right: 16}.Layout(gtx, func(gtx C) D {
					return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
						layout.Rigid(func(gtx C) D { return u.iconButton(gtx, &s.back, icBack, 40, 24, p.Icon) }),
						layout.Rigid(layout.Spacer{Width: 10}.Layout),
						layout.Flexed(1, u.label(19, settingsItems[k].title, p.Text, labelOpts{weight: font.SemiBold, maxLines: 1}).Layout),
					)
				})
			})
		}),
		layout.Flexed(1, func(gtx C) D {
			return u.scrollList(gtx, &s.detailList, len(rows), func(gtx C, i int) D {
				r := rows[i]
				switch {
				case r.heading != "":
					top := unit.Dp(18)
					if i == 0 {
						top = 6
					}
					return u.sectionLabel(gtx, r.heading, layout.Inset{Left: 29.5, Right: 29, Top: top, Bottom: 8}, labelOpts{})
				case r.note != "":
					return layout.Inset{Left: 29.5, Right: 29, Top: 4, Bottom: 8}.Layout(gtx, func(gtx C) D {
						l := u.label(15.2, r.note, p.TextSecondary)
						l.MaxLines = 0
						return l.Layout(gtx)
					})
				}
				on := r.toggle.on
				return u.layoutListItem(gtx, r.click, listItem{
					glyph: func(gtx C, col color.NRGBA) D {
						box, col := icCheckBoxEmpty, p.TextSecondary
						if on {
							box, col = icCheckBox, p.Green
						}
						return drawIcon(gtx, box, 24, col)
					},
					title: r.toggle.title,
					sub:   r.toggle.sub,
				}, settingsGeom)
			})
		}),
	)
}
