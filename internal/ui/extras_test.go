package ui

import (
	"strings"
	"testing"

	"gioui.org/io/key"

	"github.com/chomosuke9/wazzapclients/internal/command"
	"github.com/chomosuke9/wazzapclients/internal/model"
)

// Every gray switch must leave both its saved preference and UI state off
// until the acknowledgment and the activation button have been used.
func TestGrayFeatureConsent(t *testing.T) {
	keys := []string{prefEditHistory, model.PrefKeepDeleted, model.PrefViewOnceReplay}
	for _, c := range command.All {
		if c.Gray {
			keys = append(keys, grayCmdPref(c.Name))
		}
	}
	for _, pref := range keys {
		t.Run(pref, func(t *testing.T) {
			st := newSlashTest(t, "work")
			st.u.ShowPage("gray")
			st.frame()
			click := func(btn string) {
				st.u.btn(btn).Click()
				st.frame()
				st.frame()
			}
			assertOff := func() {
				t.Helper()
				if extraOn(st.b, pref) || settingRowByKey(t, st.u, pref).on {
					t.Fatal("feature enabled before consent")
				}
			}
			click("settings:" + pref)
			if !st.u.dialog.isOpen() || st.u.dialog.agreement == "" || st.u.dialog.agreed {
				t.Fatal("activation did not open an unchecked acknowledgment")
			}
			assertOff()
			click("dialog:1")
			assertOff()
			if !st.u.dialog.isOpen() {
				t.Fatal("unchecked activation closed the warning")
			}
			click("dialog:agreement")
			if !st.u.dialog.agreed {
				t.Fatal("acknowledgment did not check")
			}
			assertOff() // checking alone does not activate the feature
			click("dialog:agreement")
			click("dialog:1")
			assertOff() // unchecking must block activation again
			click("dialog:agreement")
			click("dialog:1")
			if pref == grayCmdPref("whisper") {
				assertOff()
				if !st.u.dialog.isOpen() || st.u.dialog.agreed || !strings.Contains(st.u.dialog.body, "easily get your account banned") {
					t.Fatal("whisper did not open a fresh ban-risk confirmation")
				}
				click("dialog:1")
				assertOff()
				click("dialog:agreement")
				assertOff()
				click("dialog:agreement")
				click("dialog:1")
				assertOff()
				click("dialog:agreement")
				click("dialog:1")
			}
			if !extraOn(st.b, pref) || !settingRowByKey(t, st.u, pref).on || st.u.dialog.isOpen() {
				t.Fatal("consent did not enable and save the feature or refresh its switch")
			}
			for _, other := range keys {
				if other != pref && extraOn(st.b, other) {
					t.Fatalf("consent also enabled %s", other)
				}
			}
			if pref == grayCmdPref("ghost") {
				st.u.setGhost(true)
			}
			for range 30 {
				st.frame()
			}
			click("settings:" + pref)
			assertOff()
			if st.u.dialog.isOpen() {
				t.Fatal("disabling asked for consent")
			}
			if pref == grayCmdPref("ghost") && st.u.ghostMode() {
				t.Fatal("disabling /ghost left ghost mode active")
			}
			click("settings:" + pref)
			assertOff()
			if !st.u.dialog.isOpen() || st.u.dialog.agreed {
				t.Fatal("reactivation reused the previous consent")
			}
		})
	}
}

func TestGrayFeatureCancel(t *testing.T) {
	for _, dismiss := range []string{"cancel", "escape", "outside"} {
		t.Run(dismiss, func(t *testing.T) {
			st := newSlashTest(t, "work")
			st.u.ShowPage("gray")
			st.frame()
			before := st.b.Pref(model.PrefKeepDeleted)
			st.u.btn("settings:" + model.PrefKeepDeleted).Click()
			st.frame()
			st.u.btn("dialog:agreement").Click()
			st.frame()
			switch dismiss {
			case "cancel":
				st.u.btn("dialog:2").Click()
			case "escape":
				st.press(key.NameEscape)
			case "outside":
				st.u.dialog.scrim.Click()
			}
			st.frame()
			if st.u.dialog.isOpen() {
				t.Fatal("warning did not close")
			}
			// Even an activation queued during the closing animation must
			// not run the pending callback.
			st.u.btn("dialog:1").Click()
			st.frame()
			if st.b.Pref(model.PrefKeepDeleted) != before || st.u.keepDeleted {
				t.Fatal("dismissing the warning changed the feature")
			}
			for range 30 {
				st.frame()
			}
			st.u.btn("settings:" + model.PrefKeepDeleted).Click()
			st.frame()
			if !st.u.dialog.isOpen() || st.u.dialog.agreed {
				t.Fatal("a new warning retained the canceled acknowledgment")
			}
		})
	}
}

func TestExtraFeatureWithoutConsent(t *testing.T) {
	st := newSlashTest(t, "work")
	st.u.ShowPage("extras")
	st.frame()
	st.u.btn("settings:" + prefAdminMention).Click()
	st.frame()
	if !st.u.adminMention || !extraOn(st.b, prefAdminMention) || st.u.dialog.isOpen() {
		t.Fatal("a regular extra feature did not enable immediately")
	}
}
