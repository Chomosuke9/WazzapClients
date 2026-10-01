package ui

import (
	"image"
	"image/color"
	"runtime"
	"strconv"
	"time"

	"gioui.org/font"
	"gioui.org/layout"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"github.com/chomosuke9/wazzapclients/internal/desktop"
	"github.com/chomosuke9/wazzapclients/internal/filepick"
	"github.com/chomosuke9/wazzapclients/internal/model"
	"github.com/chomosuke9/wazzapclients/internal/ui/icon"
)

// Preferences of the Chats and Account settings (Backend.Pref keys).
const (
	prefTheme       = "theme"      // "light" or "dark" (the default)
	prefDoodles     = "doodles"    // wallpaper doodles; on unless "off"
	prefEnterSend   = "enter_send" // Enter sends; on unless "off"
	prefSecurityMsg = "security_notifications"
)

// The profile fields that can be edited (settingsState.editing).
const (
	editName = iota + 1
	editAbout
)

// WhatsApp's limits for your name and about.
const (
	maxNameLen  = 25
	maxAboutLen = 139
)

// settingKind is how a settings row is drawn and what clicking it does.
type settingKind int

const (
	setLink    settingKind = iota // runs run; sub is its current value
	setToggle                     // a checkbox
	setRadio                      // one of a choice's options
	setInfo                       // read-only; clicking copies sub
	setContact                    // a contact's picture and name
	setKeys                       // a keyboard shortcut: title and key caps
	setCustom                     // w draws it
)

// settingRow is one row of a settings page.
type settingRow struct {
	key        string // names its button (see btn)
	kind       settingKind
	ic         *icon.Icon
	title, sub string
	on         bool     // setToggle and setRadio
	id         string   // setContact
	keys       []string // setKeys
	danger     bool
	trailing   *icon.Icon // e.g. a chevron for rows that open a page
	run        func()
	w          layout.Widget // setCustom
	// show hides the row while it returns false; nil shows it.
	show func() bool
}

func (r *settingRow) shown() bool { return r.show == nil || r.show() }

// settingsSection is a heading and the settings under it.
type settingsSection struct {
	title string
	rows  []settingRow
	note  string // shown after the rows
}

// settingsViews names settings pages for ShowPage (screenshots).
var settingsViews = map[string]struct {
	category int
	sub      string
}{
	"general":       {settingGeneral, ""},
	"profile":       {settingProfile, ""},
	"account":       {settingAccount, ""},
	"privacy":       {settingPrivacy, ""},
	"lastseen":      {settingPrivacy, "lastseen"},
	"blocked":       {settingPrivacy, "blocked"},
	"chatsettings":  {settingChats, ""},
	"notifications": {settingNotifications, ""},
	"shortcuts":     {settingShortcuts, ""},
	"help":          {settingHelp, ""},
}

// openSettings opens a settings category (an index of settingsItems).
func (u *UI) openSettings(k int) {
	s := &u.settings
	s.detail = k + 1
	s.sub = ""
	s.editing = 0
	s.account = u.backend.Account()
	s.detailList.Position = layout.Position{}
	s.stale = true
}

// openSettingsSub opens a page inside the open category.
func (u *UI) openSettingsSub(sub string) {
	s := &u.settings
	s.sub = sub
	s.detailList.Position = layout.Position{}
	s.stale = true
}

// settingsRows returns the open page's rows. They are listed again only
// when something changed (stale), since prefs are read from the database.
func (u *UI) settingsRows() []settingsSection {
	s := &u.settings
	if s.stale || s.page == nil {
		s.page, s.stale = u.settingsPage(), false
	}
	return s.page
}

// settingsBack goes up a level: from a page to its category, from a
// category to the list. Editing a profile field is cancelled first.
func (u *UI) settingsBack() {
	s := &u.settings
	s.stale = true
	switch {
	case s.editing != 0:
		s.editing = 0
	case s.sub != "":
		u.openSettingsSub("")
	default:
		s.detail = 0
	}
}

// settingsTitle is the header of the open settings page.
func (u *UI) settingsTitle() string {
	s := &u.settings
	switch s.sub {
	case "lastseen":
		return "Last seen and online"
	case "photo":
		return "Profile photo"
	case "about":
		return "About"
	case "groups":
		return "Groups"
	case "timer":
		return "Default message timer"
	case "blocked":
		return "Blocked contacts"
	case "theme":
		return "Theme"
	}
	return settingsItems[s.detail-1].title
}

// settingsPage lists the settings of the open page.
func (u *UI) settingsPage() []settingsSection {
	s := &u.settings
	switch s.sub {
	case "lastseen", "photo", "about", "groups":
		return u.privacyChoices(s.sub)
	case "timer":
		return u.timerChoices()
	case "blocked":
		return u.blockedContacts()
	case "theme":
		return []settingsSection{{title: "Choose a theme", rows: []settingRow{
			{key: "light", kind: setRadio, title: "Light", on: !u.dark, run: func() { u.setTheme(false) }},
			{key: "dark", kind: setRadio, title: "Dark", on: u.dark, run: func() { u.setTheme(true) }},
		}}}
	}
	b := u.backend
	pref := func(key, title, sub string) settingRow {
		on := prefOn(b, key)
		return settingRow{key: key, kind: setToggle, title: title, sub: sub, on: on, run: func() { setPref(b, key, !on) }}
	}
	switch s.detail - 1 {
	case settingNotifications:
		return []settingsSection{
			{title: "Messages", rows: []settingRow{
				pref(prefNotifyMessages, "Message notifications", "Show notifications for new messages"),
				pref(prefNotifyPreviews, "Show previews", "Show message text in notifications"),
				pref(prefNotifySound, "Sounds", "Play a sound for new messages"),
			}},
			{title: "Groups", rows: []settingRow{
				pref(prefNotifyGroups, "Group notifications", "Show notifications for group messages"),
			}},
		}
	case settingGeneral:
		return u.generalSettings(pref)
	case settingProfile:
		return u.profileSettings()
	case settingAccount:
		return u.accountSettings()
	case settingPrivacy:
		return u.privacySettings()
	case settingChats:
		return []settingsSection{
			{title: "Display", rows: []settingRow{
				{key: "theme", ic: icPalette, title: "Theme", sub: map[bool]string{false: "Light", true: "Dark"}[u.dark],
					trailing: icChevronRight, run: func() { u.openSettingsSub("theme") }},
				{key: prefDoodles, kind: setToggle, title: "Wallpaper doodles", sub: "Draw doodles on the chat background",
					on: u.doodles, run: func() { u.doodles = !u.doodles; setPref(b, prefDoodles, u.doodles) }},
			}},
			{title: "Chat settings", rows: []settingRow{
				{key: prefEnterSend, kind: setToggle, title: "Enter is send", sub: "Enter key will send your message",
					on: prefOn(b, prefEnterSend), run: func() { u.setEnterSend(!prefOn(b, prefEnterSend)) }},
			}},
		}
	case settingShortcuts:
		return shortcutSettings(prefOn(b, prefEnterSend))
	case settingHelp:
		return helpSettings()
	}
	return nil
}

func (u *UI) generalSettings(pref func(key, title, sub string) settingRow) []settingsSection {
	sec := settingsSection{title: "Startup and close"}
	h := u.host
	if h != nil && h.tray && h.o.Relaunch != nil {
		on := desktop.StartAtLogin()
		sec.rows = append(sec.rows, settingRow{
			key: "login", kind: setToggle, on: on,
			title: "Start " + appName + " at login",
			sub:   "Open in the background when you sign in, so messages notify you",
			run: func() {
				args := append(append([]string(nil), h.o.Relaunch...), "-background")
				if err := desktop.SetStartAtLogin(!on, args); err != nil {
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

// profileSettings is the Profile page: your picture, name, about and
// phone number.
func (u *UI) profileSettings() []settingsSection {
	s := &u.settings
	a := s.account
	photo := settingsSection{rows: []settingRow{
		{key: "bigphoto", kind: setCustom, w: func(gtx C) D {
			return layout.Inset{Top: 12, Bottom: 16}.Layout(gtx, func(gtx C) D {
				gtx.Constraints.Min.X = gtx.Constraints.Max.X
				return layout.N.Layout(gtx, func(gtx C) D {
					return clickable(gtx, u.btn("settings:bigphoto"), func(gtx C) D {
						return u.avatar(gtx, u.meID, u.meName(), false, 200)
					})
				})
			})
		}, run: u.pickProfilePhoto},
		{key: "setphoto", ic: icAddPhoto, title: "Change profile photo", run: u.pickProfilePhoto},
	}}
	photo.rows = append(photo.rows, settingRow{key: "rmphoto", ic: icDelete, title: "Remove profile photo", danger: true,
		show: func() bool { return u.avatarImage(u.meID) != nil },
		run: func() {
			u.confirm("Remove your profile photo?", "",
				dialogButton{label: "Cancel"},
				dialogButton{label: "Remove", danger: true, run: func() { u.backend.SetProfilePhoto("") }})
		}})
	name := a.Name
	if name == "" {
		name = u.me
	}
	field := func(which int, key, value, hint string) settingRow {
		if s.editing == which {
			return settingRow{key: key, kind: setCustom, w: u.profileEditor}
		}
		title := value
		if title == "" {
			title = hint
		}
		return settingRow{key: key, title: title, trailing: icEdit, run: func() {
			s.editing = which
			s.editor.SingleLine, s.editor.Submit = true, true
			s.editor.MaxLen = maxNameLen
			if which == editAbout {
				s.editor.MaxLen = maxAboutLen
			}
			s.editor.SetText(value)
			n := s.editor.Len()
			s.editor.SetCaret(n, n)
			u.requestFocus(&s.editor)
		}}
	}
	secs := []settingsSection{
		photo,
		{title: "Name", rows: []settingRow{field(editName, "name", name, "Add your name")},
			note: "This is not your username or PIN. This name will be visible to your WhatsApp contacts."},
		{title: "About", rows: []settingRow{field(editAbout, "about", a.About, "Add a few words about you")}},
	}
	if a.Phone != "" {
		secs = append(secs, settingsSection{title: "Phone", rows: []settingRow{u.infoRow("phone", a.Phone, "")}})
	}
	if a.Username != "" {
		secs = append(secs, settingsSection{title: "Username", rows: []settingRow{u.infoRow("username", "@"+a.Username, "")}})
	}
	return secs
}

// profileEditor is the name or about being edited, with a tick to save.
func (u *UI) profileEditor(gtx C) D {
	p := u.pal
	s := &u.settings
	return layout.Inset{Left: 29.5, Right: 22, Top: 4, Bottom: 8}.Layout(gtx, func(gtx C) D {
		d := layout.Flex{Alignment: layout.Middle}.Layout(gtx,
			layout.Flexed(1, func(gtx C) D {
				ed := material.Editor(u.th, &s.editor, "")
				ed.TextSize = 17
				ed.Color = p.Text
				return layout.Inset{Top: 8, Bottom: 8}.Layout(gtx, ed.Layout)
			}),
			layout.Rigid(func(gtx C) D {
				left := s.editor.MaxLen - s.editor.Len()
				return layout.Inset{Left: 8, Right: 4}.Layout(gtx, u.label(14, strconv.Itoa(left), p.TextSecondary).Layout)
			}),
			layout.Rigid(func(gtx C) D { return u.iconButton(gtx, &s.save, icTick, 40, 24, p.Icon) }),
		)
		h := max(1, gtx.Dp(2))
		fillRect(gtx, image.Rect(0, d.Size.Y-h, d.Size.X, d.Size.Y), p.Green)
		return d
	})
}

// saveProfileField saves the field being edited.
func (u *UI) saveProfileField() {
	s := &u.settings
	v := trimSpace(s.editor.Text())
	switch s.editing {
	case editName:
		if v == "" {
			u.toast("Your name can't be empty.")
			return
		}
		if v != s.account.Name {
			u.backend.SetProfileName(v)
			s.account.Name, u.me = v, v
		}
	case editAbout:
		if v != s.account.About {
			u.backend.SetAbout(v)
			s.account.About = v
		}
	}
	s.editing = 0
	s.stale = true
}

// pickProfilePhoto asks for a picture to use as your profile photo.
func (u *UI) pickProfilePhoto() {
	s := &u.settings
	if s.picking {
		return
	}
	if s.picked == nil {
		s.picked = make(chan string, 1)
	}
	s.picking = true
	notify, picked := u.images.invalidate, s.picked
	go func() {
		paths, err := filepick.Open("Choose a profile photo", false,
			filepick.Filter{Name: "Pictures", Exts: []string{"jpg", "jpeg", "png", "webp"}})
		path := ""
		if err == nil && len(paths) > 0 {
			path = paths[0]
		}
		picked <- path
		if notify != nil {
			notify()
		}
	}()
}

// updateProfilePhoto takes the picture the Open dialog returned.
func (u *UI) updateProfilePhoto() {
	s := &u.settings
	select {
	case path := <-s.picked:
		s.picking = false
		if path != "" {
			u.backend.SetProfilePhoto(path)
		}
	default:
	}
}

// infoRow shows a value that can't be changed here; clicking copies it.
func (u *UI) infoRow(key, title, sub string) settingRow {
	v := sub
	if v == "" {
		v = title
	}
	return settingRow{key: key, kind: setInfo, title: title, sub: sub, run: func() {
		u.pendingCopy = v
		u.toast("Copied")
	}}
}

// accountSettings is the Account page.
func (u *UI) accountSettings() []settingsSection {
	b := u.backend
	a := u.settings.account
	var info []settingRow
	if a.Phone != "" {
		info = append(info, u.infoRow("phone", "Phone number", a.Phone))
	}
	if a.LID != "" {
		info = append(info, u.infoRow("lid", "Linked ID (LID)", a.LID))
	}
	if !a.Linked.IsZero() {
		info = append(info, u.infoRow("linked", "Linked on", a.Linked.Format("2 January 2006, 15:04")))
	}
	info = append(info, u.infoRow("device", "This device", appName+" on "+osName()))
	secure := b.Pref(prefSecurityMsg) == "on"
	return []settingsSection{
		{title: "Account info", rows: info},
		{title: "Security notifications", rows: []settingRow{{
			key: "security", kind: setToggle, on: secure,
			title: "Show security notifications",
			sub:   "On this computer, when a contact's security code changes",
			run: func() {
				v := "on"
				if secure {
					v = "off"
				}
				b.SetPref(prefSecurityMsg, v)
			},
		}}, note: "Messages and calls in end-to-end encrypted chats stay between you and the people you choose. " +
			"A contact's security code changes when they reinstall WhatsApp or change phones."},
	}
}

func osName() string {
	switch runtime.GOOS {
	case "windows":
		return "Windows"
	case "darwin":
		return "macOS"
	case "linux":
		return "Linux"
	}
	return runtime.GOOS
}

// whoLabel names a privacy value.
func whoLabel(v string) string {
	switch v {
	case model.WhoEveryone:
		return "Everyone"
	case model.WhoContacts:
		return "My contacts"
	case model.WhoContactsExcept:
		return "My contacts except…"
	case model.WhoNobody:
		return "Nobody"
	case model.WhoSameAsLastSeen:
		return "Same as last seen"
	}
	return "Not loaded yet"
}

// timerLabel names a disappearing messages timer.
func timerLabel(d time.Duration) string {
	switch {
	case d <= 0:
		return "Off"
	case d%(24*time.Hour) == 0:
		if n := int(d / (24 * time.Hour)); n != 1 {
			return strconv.Itoa(n) + " days"
		}
		return "24 hours"
	case d%time.Hour == 0:
		return strconv.Itoa(int(d/time.Hour)) + " hours"
	}
	return d.String()
}

// privacySettings is the Privacy page.
func (u *UI) privacySettings() []settingsSection {
	a := u.settings.account
	link := func(sub, title, value string) settingRow {
		return settingRow{key: sub, title: title, sub: value, trailing: icChevronRight, run: func() { u.openSettingsSub(sub) }}
	}
	rr := a.Privacy[model.PrivacyReadReceipts]
	receipts := settingRow{key: "receipts", kind: setToggle, on: rr != model.WhoNobody && rr != "",
		title: "Read receipts",
		sub:   "If turned off, you won't send or receive read receipts. Read receipts are always sent for group chats.",
		run: func() {
			if rr == "" {
				u.toast("Privacy settings haven't loaded yet.")
				return
			}
			v := model.WhoNobody
			if rr == model.WhoNobody {
				v = model.WhoEveryone
			}
			u.setPrivacy(model.PrivacyReadReceipts, v)
		}}
	timer := "Unknown" // WhatsApp sends it only when a device is linked
	if a.TimerKnown {
		timer = timerLabel(a.DefaultTimer)
	}
	blocked := "Not loaded yet"
	if a.BlockedKnown {
		blocked = "None"
		if n := len(a.Blocked); n > 0 {
			blocked = strconv.Itoa(n)
		}
	}
	return []settingsSection{
		{title: "Who can see my personal info", rows: []settingRow{
			link("lastseen", "Last seen and online", whoLabel(a.Privacy[model.PrivacyLastSeen])),
			link("photo", "Profile photo", whoLabel(a.Privacy[model.PrivacyPhoto])),
			link("about", "About", whoLabel(a.Privacy[model.PrivacyAbout])),
			receipts,
		}},
		{title: "Disappearing messages", rows: []settingRow{link("timer", "Default message timer", timer)},
			note: "Start new chats with disappearing messages set to your timer."},
		{title: "Groups", rows: []settingRow{link("groups", "Groups", whoLabel(a.Privacy[model.PrivacyGroups]))}},
		{title: "Blocked contacts", rows: []settingRow{link("blocked", "Blocked contacts", blocked)}},
	}
}

// setPrivacy changes a privacy setting, showing the new value right away.
func (u *UI) setPrivacy(key, v string) {
	a := u.settings.account
	p := make(map[string]string, len(a.Privacy)+1)
	for k, v := range a.Privacy {
		p[k] = v
	}
	p[key] = v
	a.Privacy = p
	u.backend.SetPrivacy(key, v)
}

// privacyChoices is the page of one privacy setting's options.
func (u *UI) privacyChoices(sub string) []settingsSection {
	a := u.settings.account
	choices := func(title, key string, values ...string) settingsSection {
		cur := a.Privacy[key]
		sec := settingsSection{title: title}
		var opts []string
		for _, v := range values {
			opts = append(opts, v)
			if v == model.WhoContacts && cur == model.WhoContactsExcept {
				// Set on the phone, which is where its exceptions are edited.
				opts = append(opts, cur)
			}
		}
		for _, v := range opts {
			r := settingRow{key: key + ":" + v, kind: setRadio, title: whoLabel(v), on: cur == v}
			if v == model.WhoContactsExcept {
				r.sub = "Choose the exceptions on your phone"
			} else {
				r.run = func() { u.setPrivacy(key, v) }
			}
			sec.rows = append(sec.rows, r)
		}
		return sec
	}
	switch sub {
	case "lastseen":
		return []settingsSection{
			choices("Who can see my last seen", model.PrivacyLastSeen, model.WhoEveryone, model.WhoContacts, model.WhoNobody),
			choices("Who can see when I'm online", model.PrivacyOnline, model.WhoEveryone, model.WhoSameAsLastSeen),
		}
	case "photo":
		return []settingsSection{choices("Who can see my profile photo", model.PrivacyPhoto,
			model.WhoEveryone, model.WhoContacts, model.WhoNobody)}
	case "about":
		return []settingsSection{choices("Who can see my about", model.PrivacyAbout,
			model.WhoEveryone, model.WhoContacts, model.WhoNobody)}
	case "groups":
		sec := choices("Who can add me to groups", model.PrivacyGroups, model.WhoEveryone, model.WhoContacts)
		sec.note = "Admins who can't add you to a group will be able to invite you privately instead."
		return []settingsSection{sec}
	}
	return nil
}

// timerChoices is the Default message timer page.
func (u *UI) timerChoices() []settingsSection {
	a := u.settings.account
	sec := settingsSection{title: "Start new chats with a timer",
		note: "When turned on, all new one-to-one chats you start will begin with disappearing messages set to this " +
			"timer. Your existing chats aren't affected."}
	for _, d := range []time.Duration{0, 24 * time.Hour, 7 * 24 * time.Hour, 90 * 24 * time.Hour} {
		sec.rows = append(sec.rows, settingRow{key: "timer:" + d.String(), kind: setRadio, title: timerLabel(d),
			on: a.TimerKnown && a.DefaultTimer == d, run: func() {
				a.DefaultTimer, a.TimerKnown = d, true
				u.backend.SetDefaultTimer(d)
			}})
	}
	return []settingsSection{sec}
}

// blockedContacts lists blocked contacts; clicking one offers to unblock.
func (u *UI) blockedContacts() []settingsSection {
	a := u.settings.account
	sec := settingsSection{note: "Blocked contacts can no longer call you or send you messages. " +
		"Block someone from their contact info."}
	switch {
	case !a.BlockedKnown:
		sec.note = "Blocked contacts haven't loaded yet. They load once you're connected."
	case len(a.Blocked) == 0:
		sec.note = "No blocked contacts yet. " + sec.note
	}
	for _, c := range a.Blocked {
		sec.rows = append(sec.rows, settingRow{key: "blocked:" + c.ID, kind: setContact, id: c.ID, title: c.Name,
			run: func() {
				u.confirm("Unblock "+c.Name+"?", "",
					dialogButton{label: "Cancel"},
					dialogButton{label: "Unblock", primary: true, run: func() { u.backend.SetBlocked(c.ID, false) }})
			}})
	}
	return []settingsSection{sec}
}

// setTheme switches the palette and remembers the choice.
func (u *UI) setTheme(dark bool) {
	u.SetDark(dark)
	v := "light"
	if dark {
		v = "dark"
	}
	u.backend.SetPref(prefTheme, v)
}

// setEnterSend makes Enter send (and Shift+Enter add a line), or Enter
// add a line (and Ctrl+Enter send).
func (u *UI) setEnterSend(on bool) {
	setPref(u.backend, prefEnterSend, on)
	u.conv.composer.Submit = on
}

// shortcutMod names the shortcut key: Cmd on macOS, Ctrl elsewhere.
func shortcutMod() string {
	if runtime.GOOS == "darwin" {
		return "Cmd"
	}
	return "Ctrl"
}

// shortcutSettings lists the keyboard shortcuts the app handles.
func shortcutSettings(enterSend bool) []settingsSection {
	mod := shortcutMod()
	k := func(title string, keys ...string) settingRow {
		return settingRow{key: "key:" + title, kind: setKeys, title: title, keys: keys}
	}
	send, line := k("Send message", "Enter"), k("New line", "Shift", "Enter")
	if !enterSend {
		send, line = k("Send message", mod, "Enter"), k("New line", "Enter")
	}
	return []settingsSection{
		{title: "Composer", rows: []settingRow{
			send, line,
			k("Bold", mod, "B"),
			k("Italic", mod, "I"),
			k("Strikethrough", mod, "Shift", "X"),
			k("Monospace", mod, "Shift", "M"),
			k("Paste files or a picture", mod, "V"),
		}},
		{title: "Messages", rows: []settingRow{
			k("Copy selected text", mod, "C"),
			k("Select all of a message's text", mod, "A"),
		}},
		{title: "Photos and videos", rows: []settingRow{
			k("Previous", "←"),
			k("Next", "→"),
			k("Play or pause a video", "Space"),
		}},
		{title: "Photo editor", rows: []settingRow{
			k("Undo", mod, "Z"),
			k("Delete the selected drawing or text", "Delete"),
		}},
		{title: "Everywhere", rows: []settingRow{
			k("Close a menu, dialog, panel or reply", "Esc"),
		}},
	}
}

// Links of the Help and feedback page.
const (
	helpCentreURL = "https://faq.whatsapp.com/"
	issuesURL     = "https://github.com/Chomosuke9/WazzapClients/issues"
	sourceURL     = "https://github.com/Chomosuke9/WazzapClients"
	legalURL      = "https://www.whatsapp.com/legal/"
)

func helpSettings() []settingsSection {
	link := func(key string, ic *icon.Icon, title, sub, url string) settingRow {
		return settingRow{key: key, ic: ic, title: title, sub: sub, trailing: icOpenInNew, run: func() { openURL(url) }}
	}
	return []settingsSection{
		{title: "Help", rows: []settingRow{
			link("faq", icHelp, "Help centre", "Get help with WhatsApp", helpCentreURL),
			link("issues", icBubble, "Report a problem", "Tell us about a bug in "+appName, issuesURL),
		}},
		{title: "About", rows: []settingRow{
			link("source", icLink, "Source code", "github.com/Chomosuke9/WazzapClients", sourceURL),
			link("legal", icDocument, "Terms and Privacy Policy", "WhatsApp's terms apply to your account", legalURL),
		}, note: appName + " is an unofficial WhatsApp client. It isn't made by or affiliated with WhatsApp or Meta."},
	}
}

// layoutSettingsDetail draws an open settings page: a header with a back
// arrow, then its rows under their headings.
func (u *UI) layoutSettingsDetail(gtx C) D {
	p := u.pal
	s := &u.settings
	if s.back.Clicked(gtx) {
		u.settingsBack()
		if s.detail == 0 {
			return u.layoutSettingsList(gtx)
		}
	}
	if s.editing != 0 {
		for {
			ev, ok := s.editor.Update(gtx)
			if !ok {
				break
			}
			if _, ok := ev.(widget.SubmitEvent); ok {
				u.saveProfileField()
			}
		}
		if s.save.Clicked(gtx) {
			u.saveProfileField()
		}
	}
	// Clicks run first, then the page is listed again with their effect.
	for _, sec := range u.settingsRows() {
		for _, r := range sec.rows {
			if r.run != nil && r.shown() && u.btn("settings:"+r.key).Clicked(gtx) {
				r.run()
				s.stale = true
			}
		}
	}
	if s.detail == 0 {
		return u.layoutSettingsList(gtx)
	}
	type row struct {
		heading string
		note    string
		r       *settingRow
	}
	var rows []row
	for i, sec := range u.settingsRows() {
		if sec.title != "" {
			rows = append(rows, row{heading: sec.title})
		} else if i > 0 {
			rows = append(rows, row{heading: " "})
		}
		for j := range sec.rows {
			if sec.rows[j].shown() {
				rows = append(rows, row{r: &sec.rows[j]})
			}
		}
		if sec.note != "" {
			rows = append(rows, row{note: sec.note})
		}
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx C) D {
			return vcenter(gtx, gtx.Dp(68), func(gtx C) D {
				return layout.Inset{Left: 10, Right: 16}.Layout(gtx, func(gtx C) D {
					return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
						layout.Rigid(func(gtx C) D { return u.iconButton(gtx, &s.back, icBack, 40, 24, p.Icon) }),
						layout.Rigid(layout.Spacer{Width: 10}.Layout),
						layout.Flexed(1, u.label(19, u.settingsTitle(), p.Text, labelOpts{weight: font.SemiBold, maxLines: 1}).Layout),
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
						l := u.label(14, r.note, p.TextSecondary)
						l.MaxLines = 0
						return l.Layout(gtx)
					})
				}
				return u.layoutSettingRow(gtx, r.r)
			})
		}),
	)
}

func (u *UI) layoutSettingRow(gtx C, r *settingRow) D {
	p := u.pal
	if r.kind == setCustom {
		return r.w(gtx)
	}
	it := listItem{ic: r.ic, title: r.title, sub: r.sub, danger: r.danger}
	if r.trailing != nil {
		ic := r.trailing
		it.trailing = func(gtx C) D {
			return layout.Inset{Left: 8, Right: 14}.Layout(gtx, iconW(ic, 22, p.TextSecondary))
		}
	}
	switch r.kind {
	case setToggle:
		on := r.on
		it.glyph = func(gtx C, col color.NRGBA) D {
			box, col := icCheckBoxEmpty, p.TextSecondary
			if on {
				box, col = icCheckBox, p.Green
			}
			return drawIcon(gtx, box, 24, col)
		}
	case setRadio:
		on := r.on
		it.glyph = func(gtx C, _ color.NRGBA) D { return u.radio(gtx, on) }
	case setContact:
		id, name := r.id, r.title
		it.glyph = func(gtx C, _ color.NRGBA) D { return u.avatar(gtx, id, name, false, 40) }
	case setKeys:
		return u.layoutShortcut(gtx, r.title, r.keys)
	}
	g := settingsGeom
	if it.ic == nil && it.glyph == nil {
		// No icon: the text lines up with the headings.
		it.glyph = func(gtx C, _ color.NRGBA) D { return D{} }
		g = settingsGeomPlain
	}
	return u.layoutListItem(gtx, u.btn("settings:"+r.key), it, g)
}

// settingsGeomPlain is settingsGeom for rows without an icon.
var settingsGeomPlain = func() listGeom {
	g := settingsGeom
	g.iconCenter, g.textLeft = 0, 11
	g.height, g.subHeight = 56, 68
	return g
}()

// layoutShortcut is a row of the Keyboard shortcuts page: what it does,
// then its keys.
func (u *UI) layoutShortcut(gtx C, title string, keys []string) D {
	return layout.Inset{Left: 29.5, Right: 29}.Layout(gtx, func(gtx C) D {
		return vcenter(gtx, gtx.Dp(48), func(gtx C) D {
			gtx.Constraints.Min.X = gtx.Constraints.Max.X
			return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
				layout.Flexed(1, func(gtx C) D {
					d := u.label(16, title, u.pal.Text, labelOpts{maxLines: 2}).Layout(gtx)
					d.Size.X = gtx.Constraints.Max.X
					return d
				}),
				layout.Rigid(func(gtx C) D { return u.keyCaps(gtx, keys) }),
			)
		})
	})
}

// radio draws a radio button.
func (u *UI) radio(gtx C, on bool) D {
	p := u.pal
	sz := gtx.Dp(20)
	c := image.Pt(sz/2, sz/2)
	ring := p.TextSecondary
	if on {
		ring = p.Green
	}
	fillCircle(gtx, c, sz/2, ring)
	fillCircle(gtx, c, sz/2-max(1, gtx.Dp(2)), p.Panel)
	if on {
		fillCircle(gtx, c, sz/2-gtx.Dp(5), p.Green)
	}
	return D{Size: image.Pt(sz, sz)}
}

// keyCaps draws the keys of a shortcut as small rounded boxes.
func (u *UI) keyCaps(gtx C, keys []string) D {
	p := u.pal
	children := make([]layout.FlexChild, 0, 2*len(keys))
	for i, k := range keys {
		if i > 0 {
			children = append(children, layout.Rigid(layout.Spacer{Width: 6}.Layout))
		}
		children = append(children, layout.Rigid(func(gtx C) D {
			m := record(gtx, func(gtx C) D {
				return layout.Inset{Left: 9, Right: 9, Top: 3, Bottom: 3}.Layout(gtx,
					u.label(13.5, k, p.Text, labelOpts{weight: font.Medium, maxLines: 1}).Layout)
			})
			r := image.Rectangle{Max: m.size}
			borderRRect(gtx, r, gtx.Dp(6), p.Panel, p.Divider)
			m.at(gtx, 0, 0)
			return D{Size: m.size}
		}))
	}
	return layout.Inset{Left: 12}.Layout(gtx, func(gtx C) D {
		return layout.Flex{Alignment: layout.Middle}.Layout(gtx, children...)
	})
}
