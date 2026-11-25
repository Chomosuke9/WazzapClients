package ui

import (
	"testing"

	"gioui.org/io/key"

	"github.com/chomosuke9/wazzapclients/internal/mock"
	"github.com/chomosuke9/wazzapclients/internal/model"
)

// The doodles' switches (the Performance page's and the Default chat
// theme's) are one preference, and a chat's own theme can say otherwise.
func TestDoodlesAgree(t *testing.T) {
	resetPerf(t)
	st := newSlashTest(t, "rina")
	u := st.u
	perfOn := func() bool {
		u.ShowPage("performance")
		u.settings.stale = true
		return settingRowByKey(t, u, prefDoodles).on
	}
	check := func(what string, all, rina bool) {
		t.Helper()
		if perfOn() != all || u.bareWall(u.lookOf(lookAll)) == all || u.bareWall(u.lookOf("rina")) == rina {
			t.Fatalf("%s: performance %v, every chat %v, rina %v, want %v, %v, %v", what, perfOn(),
				!u.bareWall(u.lookOf(lookAll)), !u.bareWall(u.lookOf("rina")), all, all, rina)
		}
		if u.chatPalette(u.chatByID("rina")).Plain == rina {
			t.Fatalf("%s: the conversation doesn't follow", what)
		}
	}
	toggle := func(id string) { // the Chat theme panel's switch
		t.Helper()
		if id == lookAll {
			u.openChatTheme(nil)
		} else {
			u.ShowPage("chats")
			u.SelectID(id)
			u.openChatTheme(u.selected)
		}
		st.frame()
		u.btn("theme:doodles").Click()
		st.frame()
		u.hideChatTheme()
		u.dialog = dialogState{}
	}
	check("at first", true, true)
	u.ShowPage("performance")
	settingRowByKey(t, u, prefDoodles).run()
	check("Performance off", false, false)
	toggle(lookAll)
	check("Default chat theme on", true, true)
	toggle(lookAll)
	check("Default chat theme off", false, false)
	toggle("rina")
	check("rina's on", false, true)
	u.themes = nil
	check("read back", false, true)
	toggle("rina")
	check("rina's as every chat's", false, false)

	// Resets.
	u.resetLook(lookAll)
	check("theme reset", true, true)
	u.setDoodles(false)
	u.ShowPage("advanced")
	settingRowByKey(t, u, "perfreset").run()
	u.dialog.buttons[0].run()
	u.dialog = dialogState{}
	check("Performance reset", true, true)

	// What older versions kept.
	st.b.SetPref(prefChatTheme+lookAll, "||bare")
	st.b.SetPref(prefChatTheme+"rina", "Pink")
	u.themes = nil
	check("every chat's kept bare", false, true)
	u.ShowPage("performance")
	u.settings.stale = true
	settingRowByKey(t, u, prefDoodles).run()
	check("on again", true, true)
}

// Each kind of notifications has a sound of its own; a group's was the
// messages' until it's set.
func TestNotifySounds(t *testing.T) {
	b := mock.New()
	if !groupSound(b) {
		t.Fatal("groups have a sound by default")
	}
	b.SetPref(prefNotifySound, "off")
	if groupSound(b) {
		t.Fatal("the one switch of older versions silenced groups too")
	}
	u := New(b)
	u.Start(func() {})
	u.ShowPage("notifygroups")
	settingRowByKey(t, u, "notifygroups:sound").run()
	if !groupSound(b) || prefOn(b, prefNotifySound) {
		t.Fatal("turning the groups' sound on should leave the messages' off")
	}
	b.SetPref(prefNotifyGroupSound, "")
	b.SetPref(prefNotifySound, "")
	setSound(b, false, false)
	if !groupSound(b) || prefOn(b, prefNotifySound) {
		t.Fatal("turning the messages' sound off should leave the groups' on")
	}
	n := newNotifier(b, &host{})
	for id, silent := range map[string]bool{"rina": true, "x@g.us": false} {
		if got := n.notification(id, chatInfo{name: id}, &model.Message{Text: "hi"}, 1).Silent; got != silent {
			t.Errorf("%s's notification is silent: %v", id, got)
		}
	}
}

// Search finds the settings of pages inside a category, and a hit opens
// the page it's on.
func TestSettingsSearchSubPages(t *testing.T) {
	st := newSlashTest(t, "rina")
	u := st.u
	u.ShowPage("settings")
	for q, want := range map[string]settingsHit{
		"stickers":                {category: settingPerformance, sub: "advanced", title: "Stickers"},
		"your stickers":           {category: settingPerformance, sub: "stickers", title: "Your stickers"},
		"lottie":                  {category: settingPerformance, sub: "stickers", title: "Lottie sticker frame rate"},
		"theme":                   {category: settingChats, title: "Theme"},
		"default chat theme":      {category: settingChats, title: "Default chat theme"},
		"app theme color":         {category: settingAppearance, title: "App theme color"},
		"media auto-download":     {category: settingChats, title: "Media auto-download"},
		"videos":                  {category: settingChats, sub: "autodownload", title: "Videos"},
		"upload quality":          {category: settingChats, title: "Media upload quality"},
		"app lock":                {category: settingPrivacy, title: "App lock"},
		"status":                  {category: settingPrivacy, title: "Status"},
		"profile picture":         {category: settingPrivacy, title: "Profile picture"},
		"turn off link previews":  {category: settingPrivacy, title: "Turn off link previews"},
		"disappearing messages":   {category: settingPrivacy, sub: "timer", title: "Start new chats with disappearing messages"},
		"play sound":              {category: settingNotifications, sub: "notifymessages", title: "Play sound"},
		"sound":                   {category: settingNotifications, sub: "notifygroups", title: "Play sound"},
		"groups":                  {category: settingNotifications, title: "Groups"},
		"on this computer":        {category: settingAccount, title: "Show security notifications on this computer"},
		"help centre":             {category: settingHelp, title: "Help centre"},
		"search the open chat":    {category: settingShortcuts, title: "Search the open chat"},
		"keep the picker open":    {category: settingPerformance, sub: "stickers", title: "Keep the picker open"},
		"play gifs and animated ": {category: settingPerformance, title: "Play GIFs and animated stickers"},
	} {
		u.settings.search.SetText(q)
		st.frame()
		var hit *settingsHit
		for i, h := range u.settings.hits {
			if h.key != "" && h.category == want.category && h.sub == want.sub && h.title == want.title {
				hit = &u.settings.hits[i]
			}
		}
		if hit == nil {
			t.Errorf("%q found %+v, want %+v", q, u.settings.hits, want)
		}
	}
	u.settings.search.SetText("keep the picker open")
	st.frame()
	u.btn("settings:hit:" + itoa(settingPerformance) + ":stickers:" + prefKeepPicker).Click()
	st.frame()
	if u.settings.detail != settingPerformance+1 || u.settings.sub != "stickers" {
		t.Fatalf("the hit opened %d %q", u.settings.detail, u.settings.sub)
	}
	u.settingsBack()
	if u.settings.sub != "advanced" {
		t.Fatalf("back went to %q", u.settings.sub)
	}
}

// Keyboard shortcuts is a dialog over the settings list, from its row and
// from a search hit.
func TestShortcutsDialog(t *testing.T) {
	st := newSlashTest(t, "rina")
	u := st.u
	u.ShowPage("settings")
	st.frame()
	open := func(click func()) {
		t.Helper()
		click()
		st.frame()
		if u.dialog.kind != dialogShortcuts || u.settings.detail != 0 {
			t.Fatalf("dialog %v, detail %d", u.dialog.kind, u.settings.detail)
		}
		for range 30 {
			st.frame()
		}
	}
	open(func() { u.settings.items[settingShortcuts].Click() })
	u.btn("shortcuts:ok").Click()
	for range 30 {
		st.frame()
	}
	if u.dialog.kind != dialogNone {
		t.Fatal("OK didn't close it")
	}
	u.settings.search.SetText("search the open chat")
	st.frame()
	open(func() { u.btn("settings:hit:" + itoa(settingShortcuts) + "::key:Search the open chat").Click() })
	st.press(key.NameEscape)
	for range 30 {
		st.frame()
	}
	if u.dialog.kind != dialogNone || u.page != pageSettings {
		t.Fatal("Esc should close the dialog alone")
	}
}

// Media upload quality is the send view's own choice, Raw offered only
// while that extra feature is on.
func TestUploadQuality(t *testing.T) {
	b := mock.New()
	u := New(b)
	u.Start(func() {})
	u.ShowPage("uploadquality")
	settingRowByKey(t, u, "quality:HD").run()
	if u.attach.quality != model.QualityHD || b.Pref(prefPhotoQuality) != "hd" {
		t.Fatalf("quality %v, pref %q", u.attach.quality, b.Pref(prefPhotoQuality))
	}
	if n := len(u.uploadQuality()[0].rows); n != 2 {
		t.Fatalf("%d choices without Raw", n)
	}
	u.rawPhotos = true
	if n := len(u.uploadQuality()[0].rows); n != 3 {
		t.Fatalf("%d choices with Raw", n)
	}
}

// Status privacy opens over the settings from Privacy, and Esc goes back
// there.
func TestStatusPrivacyFromSettings(t *testing.T) {
	st := newSlashTest(t, "rina")
	u := st.u
	u.ShowPage("privacy")
	st.frame()
	u.btn("settings:statusprivacy").Click()
	for range 40 {
		st.frame()
	}
	if u.page != pageSettings || u.status.privacy.top() != spMain || !u.statusPrivacyCovers() {
		t.Fatalf("page %v, stack %v", u.page, u.status.privacy.stack)
	}
	u.chooseAudience(model.AudienceExcept, -1)
	st.press(key.NameEscape)
	for range 40 {
		st.frame()
	}
	for u.status.privacy.open() {
		st.press(key.NameEscape)
	}
	if u.page != pageSettings || u.settings.detail != settingPrivacy+1 {
		t.Fatalf("Esc went to page %v, detail %d", u.page, u.settings.detail)
	}
	if got, want := settingRowByKey(t, u, "statusprivacy").sub, chosenAudienceTitle(st.b.StatusPrivacy()); got != want {
		t.Fatalf("the row says %q, want %q", got, want)
	}
}
