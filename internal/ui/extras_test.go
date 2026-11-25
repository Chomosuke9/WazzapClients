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
	st.u.btn("settings:" + prefExtendedMentions).Click()
	st.frame()
	if !st.u.extendedMentions || !extraOn(st.b, prefExtendedMentions) || st.u.dialog.isOpen() {
		t.Fatal("a regular extra feature did not enable immediately")
	}
}

// TestKeepDeletedByYou checks that with Keep deleted messages on, what you
// delete for everyone stays, marked as deleted by you, and that it can't
// be deleted for everyone or edited again.
func TestKeepDeletedByYou(t *testing.T) {
	st := newSlashTest(t, "work")
	st.b.SetPref(model.PrefKeepDeleted, "on")
	all := st.b.Messages("work", 1000)
	st.typeText("/purge 3")
	st.press(key.NameReturn)
	st.runConfirm("Delete 3 messages for everyone?")
	now := st.b.Messages("work", 1000)
	for i, m := range now {
		kept := i >= len(all)-3
		if m.Kind != all[i].Kind || m.Text != all[i].Text || m.Revoked.IsZero() == kept {
			t.Errorf("message %d (%q): kind %v text %q revoked %v", i, all[i].Text, m.Kind, m.Text, m.Revoked)
		}
		if tag, name := deletedTag(m); kept && (tag != "Deleted by you" || name != "") {
			t.Errorf("message %d is marked %q %q", i, tag, name)
		}
	}
	last := now[len(now)-1]
	last.FromMe, last.Receipt, last.Time = true, model.Read, st.u.now()
	if last.CanEdit(st.u.now()) {
		t.Error("a deleted message can be edited")
	}
	st.u.confirmDelete([]*model.Message{last})
	if bs := st.u.dialog.buttons; len(bs) == 0 || bs[0].label != "Delete for me" {
		t.Errorf("a deleted message offers %+v", bs)
	}
	st.u.dialog = dialogState{}
	// They don't count for the next /purge.
	st.typeText("/purge 1")
	st.press(key.NameReturn)
	st.runConfirm("Delete 1 message for everyone?")
	if m := st.b.Messages("work", 1000)[len(all)-4]; m.Revoked.IsZero() {
		t.Errorf("%q wasn't deleted", m.Text)
	}
}

// TestDeletedTag checks who a kept message says deleted it.
func TestDeletedTag(t *testing.T) {
	for _, c := range []struct {
		m    model.Message
		want string
	}{
		{model.Message{}, "Deleted"},
		{model.Message{FromMe: true}, "Deleted by you"},
		{model.Message{DeletedByMe: true}, "Deleted by you"},
		{model.Message{DeletedBy: "Alice Smith"}, "Deleted by Alice Smith"},
		{model.Message{FromMe: true, DeletedBy: "~Al"}, "Deleted by ~Al"},
	} {
		tag, name := deletedTag(&c.m)
		if got := strings.TrimSpace(tag + " " + name); got != c.want {
			t.Errorf("%+v is marked %q, want %q", c.m, got, c.want)
		}
	}
}
