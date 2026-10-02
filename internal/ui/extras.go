package ui

import (
	"image"

	"gioui.org/font"
	"gioui.org/layout"

	"github.com/chomosuke9/wazzapclients/internal/command"
	"github.com/chomosuke9/wazzapclients/internal/model"
	"github.com/chomosuke9/wazzapclients/internal/ui/icon"
)

// Extra features are this app's own, which WhatsApp doesn't have. Each is
// off until it's turned on, on the Extra features settings page.

// Preferences of the extra features (Backend.Pref keys), on when "on".
const (
	prefSlash        = "slash_commands" // slash.go
	prefAdminMention = "admin_mention"  // "@admin" in the mention picker
	prefRawPhotos    = "raw_photos"     // Raw quality for photos
	prefEditHistory  = "edit_history"   // the earlier texts of edited messages (edit.go)
)

// extraOn reports whether an extra feature is turned on.
func extraOn(b model.Backend, key string) bool { return b != nil && b.Pref(key) == "on" }

func setExtra(b model.Backend, key string, on bool) {
	v := "off"
	if on {
		v = "on"
	}
	b.SetPref(key, v)
}

// loadExtras reads which extra features are on.
func (u *UI) loadExtras() {
	b := u.backend
	u.slash.on = extraOn(b, prefSlash)
	u.adminMention = extraOn(b, prefAdminMention)
	u.rawPhotos = extraOn(b, prefRawPhotos)
	u.editHistory = extraOn(b, prefEditHistory)
}

// commandIcons are the commands' icons in the picker and in settings.
var commandIcons = map[string]*icon.Icon{
	"add":         icPersonAdd,
	"kick":        icPersonRemove,
	"promote":     icAddModerator,
	"demote":      icRemoveModerator,
	"link":        icLink,
	"lockdown":    icLockOutline,
	"description": icEditNote,
	"sticker":     icSticker,
	"purge":       icDeleteSweep,
	"raffle":      icCasino,
	"calc":        icCalculate,
	"schedule":    icScheduleSend,
	"scheduled":   icClock,
	"afk":         icBedtime,
}

func commandIcon(name string) *icon.Icon {
	if ic := commandIcons[name]; ic != nil {
		return ic
	}
	return icTerminal
}

// extrasSettings is the Extra features page.
func (u *UI) extrasSettings() []settingsSection {
	b := u.backend
	toggle := func(key, title, sub string, flag *bool, changed func()) settingRow {
		on := *flag
		return settingRow{key: key, kind: setToggle, on: on, title: title, sub: sub, run: func() {
			*flag = !on
			setExtra(b, key, !on)
			if changed != nil {
				changed()
			}
		}}
	}
	on := u.slash.on
	secs := []settingsSection{
		{title: "Slash commands", rows: []settingRow{
			toggle(prefSlash, "Slash commands", "Type / at the start of a message to run a command, like in Discord",
				&u.slash.on, func() { u.conv.richFor = "" }), // the composer starts or stops showing commands
		}, note: "Commands run on this computer, from your account. Group commands work in groups you administer."},
		{title: "Mentions", rows: []settingRow{
			toggle(prefAdminMention, "@admin", "Type @admin in a group to mention all of its admins at once",
				&u.adminMention, nil),
		}},
		{title: "Photos", rows: []settingRow{
			toggle(prefRawPhotos, "Raw quality", "Offer Raw when sending photos: JPEG and PNG files go as they are, not scaled or compressed",
				&u.rawPhotos, func() {
					if !u.rawPhotos && u.attach.quality == model.QualityRaw {
						u.attach.quality = model.QualityHD
					}
				}),
		}},
		{title: "Messages", rows: []settingRow{
			toggle(prefEditHistory, "Edit history", "See what an edited message said before: right-click it and pick Edit history",
				&u.editHistory, nil),
		}},
	}
	if on {
		sec := settingsSection{title: "Commands"}
		for _, c := range command.All {
			sec.rows = append(sec.rows, settingRow{key: "cmd:" + c.Name, kind: setCustom, w: func(gtx C) D {
				return layout.Inset{Left: 18.5, Right: 29}.Layout(gtx, func(gtx C) D {
					return u.layoutCommandRow(gtx, c, -1, nil, gtx.Dp(64))
				})
			}})
		}
		// Under the switch that turns them on.
		secs = append(secs[:1], append([]settingsSection{sec}, secs[1:]...)...)
	}
	secs[len(secs)-1].note = appendNote(secs[len(secs)-1].note,
		"Extra features aren't made by WhatsApp. They only use what WhatsApp lets every linked device do.")
	return secs
}

func appendNote(a, b string) string {
	if a == "" {
		return b
	}
	return a + " " + b
}

// layoutCommandRow draws a command: its icon, its name with a chip per
// option and its description, h px tall. cur is the option being typed
// (-1 for none), and values the options' values so far, which mark the
// filled ones.
func (u *UI) layoutCommandRow(gtx C, c *command.Command, cur int, values [][]command.Value, h int) D {
	p := u.pal
	gtx.Constraints.Min.X = gtx.Constraints.Max.X
	return vcenter(gtx, h, func(gtx C) D {
		return layout.Inset{Left: 12, Right: 12}.Layout(gtx, func(gtx C) D {
			return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(func(gtx C) D {
					d := gtx.Dp(36)
					fillCircle(gtx, image.Pt(d/2, d/2), d/2, p.PopupBorder)
					return centerIn(gtx, d, iconW(commandIcon(c.Name), 21, p.Green))
				}),
				layout.Rigid(layout.Spacer{Width: 14}.Layout),
				layout.Flexed(1, func(gtx C) D {
					return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
						layout.Rigid(func(gtx C) D { return u.commandUsage(gtx, c, cur, values) }),
						layout.Rigid(layout.Spacer{Height: 2}.Layout),
						layout.Rigid(u.label(13.5, c.Description, p.PopupSub, labelOpts{maxLines: 1}).Layout),
					)
				}),
			)
		})
	})
}

// commandUsage is "/name" and a chip per option. The option being typed
// is outlined in green; filled ones are dimmed.
func (u *UI) commandUsage(gtx C, c *command.Command, cur int, values [][]command.Value) D {
	p := u.pal
	children := []layout.FlexChild{
		layout.Rigid(u.label(15.5, "/"+c.Name, p.Text, labelOpts{weight: font.SemiBold, maxLines: 1}).Layout),
	}
	for i, o := range c.Options {
		filled := i < len(values) && len(values[i]) > 0
		children = append(children, layout.Rigid(layout.Spacer{Width: 6}.Layout), layout.Rigid(func(gtx C) D {
			txt, col := o.Name, p.Text
			if !o.Required {
				txt, col = o.Name+" (optional)", p.TextSecondary
			}
			if filled && i != cur {
				col = p.TextSecondary
			}
			m := record(gtx, func(gtx C) D {
				return layout.Inset{Left: 7, Right: 7, Top: 1, Bottom: 2}.Layout(gtx,
					u.label(13, txt, col, labelOpts{weight: font.Medium, maxLines: 1}).Layout)
			})
			r := image.Rectangle{Max: m.size}
			border := p.CodeBg
			if i == cur {
				border = p.Green
			}
			borderRRect(gtx, r, gtx.Dp(5), p.CodeBg, border)
			m.at(gtx, 0, 0)
			return D{Size: m.size}
		}))
	}
	return layout.Flex{Alignment: layout.Middle}.Layout(gtx, children...)
}
