package ui

import (
	"fmt"
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

// grayCmdPref is the preference that turns on a gray command.
func grayCmdPref(name string) string { return "cmd_" + name }

// commandOn reports whether a command can be used: a gray one only once
// its own switch is on.
func (u *UI) commandOn(c *command.Command) bool { return !c.Gray || u.grayCmds[c.Name] }

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
	u.keepDeleted = extraOn(b, model.PrefKeepDeleted)
	u.viewOnceReplay = extraOn(b, model.PrefViewOnceReplay)
	u.grayCmds = map[string]bool{}
	for _, c := range command.All {
		if c.Gray && extraOn(b, grayCmdPref(c.Name)) {
			u.grayCmds[c.Name] = true
		}
	}
	// A window opening in ghost mode shows it at once.
	u.ghostFx.bar.snap(u.ghostMode())
	u.ghostFx.compose.snap(u.ghostMode())
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
	"ghost":       icVisibilityOff,
}

func commandIcon(name string) *icon.Icon {
	if ic := commandIcons[name]; ic != nil {
		return ic
	}
	return icTerminal
}

// extraToggle is the switch of an extra feature, kept in flag. changed,
// when set, runs after it flips.
func (u *UI) extraToggle(key, title, sub string, flag *bool, changed func()) settingRow {
	b, on := u.backend, *flag
	return settingRow{key: key, kind: setToggle, on: on, title: title, sub: sub, run: func() {
		*flag = !on
		setExtra(b, key, !on)
		if changed != nil {
			changed()
		}
	}}
}

// grayToggle asks for an acknowledgment before enabling a gray feature.
// Disabling it is immediate, and each new activation asks again.
func (u *UI) grayToggle(key, title, sub string, flag *bool, changed func()) settingRow {
	r := u.extraToggle(key, title, sub, flag, changed)
	toggle, on := r.run, r.on
	r.run = func() {
		if on {
			toggle()
			return
		}
		u.confirm("Enable "+title+"?", sub+"\n\n"+
			"This is an ethically gray feature. It may go against other people's privacy expectations, "+
			"and they may feel uncomfortable or offended when you use it. Please respect their choices and decide wisely.",
			dialogButton{label: "Enable feature", primary: true, run: func() {
				toggle()
				u.settings.stale = true // the switch changes after the dialog, not the row's click
			}})
		u.dialog.agreement = "I understand this feature is ethically gray and may affect other people's privacy. I agree to use it responsibly."
	}
	return r
}

// extrasSettings is the Extra features page. The ethically gray ones have
// a page of their own (graySettings).
func (u *UI) extrasSettings() []settingsSection {
	secs := []settingsSection{
		{title: "Slash commands", rows: []settingRow{
			u.extraToggle(prefSlash, "Slash commands", "Type / at the start of a message to run a command, like in Discord",
				&u.slash.on, func() { u.conv.richFor = "" }), // the composer starts or stops showing commands
		}, note: "Commands run on this computer, from your account. Group commands work in groups you administer."},
		{title: "Mentions", rows: []settingRow{
			u.extraToggle(prefAdminMention, "@admin", "Type @admin in a group to mention all of its admins at once",
				&u.adminMention, nil),
		}},
		u.privacyExtras(),
		{title: "Photos", rows: []settingRow{
			u.extraToggle(prefRawPhotos, "Raw quality", "Offer Raw when sending photos: JPEG and PNG files go as they are, not scaled or compressed",
				&u.rawPhotos, func() {
					if !u.rawPhotos && u.attach.quality == model.QualityRaw {
						u.attach.quality = model.QualityHD
					}
				}),
		}},
		{rows: []settingRow{{key: "gray", ic: icExtensionGray, title: "Ethically gray features",
			sub: "Edit history, deleted messages, view once and more", run: func() { u.openSettingsSub("gray") }}},
			note: "Extra features aren't made by WhatsApp. They only use what WhatsApp lets every linked device do."},
	}
	if u.slash.on {
		var cmds []*command.Command
		for _, c := range command.All {
			if !c.Gray { // gray ones have a switch of their own
				cmds = append(cmds, c)
			}
		}
		// A row that shows or hides the list, under the switch that turns
		// them on.
		title, ic := fmt.Sprintf("Show the %d commands", len(cmds)), icChevronRight
		if u.cmdsOpen {
			title, ic = "Hide the commands", icChevron
		}
		sec := settingsSection{title: "Commands", rows: []settingRow{{key: "cmds", title: title, trailing: ic,
			run: func() { u.cmdsOpen = !u.cmdsOpen }}}}
		for _, c := range cmds {
			if !u.cmdsOpen {
				break
			}
			sec.rows = append(sec.rows, settingRow{key: "cmd:" + c.Name, kind: setCustom, w: func(gtx C) D {
				return layout.Inset{Left: 18.5, Right: 29}.Layout(gtx, func(gtx C) D {
					return u.layoutCommandRow(gtx, c, -1, nil, gtx.Dp(64))
				})
			}})
		}
		secs = append(secs[:1], append([]settingsSection{sec}, secs[1:]...)...)
	}
	return secs
}

// graySettings is the Ethically gray features page: features that let you
// see or do what the people you talk to wouldn't expect.
func (u *UI) graySettings() []settingsSection {
	secs := []settingsSection{{title: "Messages", rows: []settingRow{
		u.grayToggle(prefEditHistory, "Edit history", "See what an edited message said before: right-click it and pick Edit history",
			&u.editHistory, nil),
		u.grayToggle(model.PrefKeepDeleted, "Keep deleted messages",
			"When someone deletes a message for everyone or a status, keep showing it, marked Deleted",
			&u.keepDeleted, nil),
		u.grayToggle(model.PrefViewOnceReplay, "Replay view once",
			"Open view once photos, videos and voice messages as often as you like, and take screenshots of them",
			&u.viewOnceReplay, nil),
	}, note: "These let you see or do what the people you talk to wouldn't expect, so use them with care. " +
		"Messages deleted while Keep deleted messages is off can't be brought back."}}
	cmds := settingsSection{title: "Commands"}
	for _, c := range command.All {
		if !c.Gray {
			continue
		}
		flag := u.grayCmds[c.Name]
		cmds.rows = append(cmds.rows, u.grayToggle(grayCmdPref(c.Name), "/"+c.Name, c.Description, &flag, func() {
			u.grayCmds[c.Name] = flag
			u.slash.cacheOK = false // the picker offers it, or stops
			u.conv.richFor = ""
			if c.Name == "ghost" && !flag && u.ghostMode() {
				u.setGhost(false) // no way left to turn it off
			}
		}))
	}
	if !u.slash.on {
		cmds.note = "These also need Slash commands, on the Extra features page."
	}
	return append(secs, cmds)
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
