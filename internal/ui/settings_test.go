package ui

import (
	"image"
	"testing"
	"time"

	"gioui.org/io/input"
	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/chomosuke9/wazzapclients/internal/mock"
	"github.com/chomosuke9/wazzapclients/internal/model"
)

// settingRowByKey finds a row of the open settings page.
func settingRowByKey(t *testing.T, u *UI, key string) settingRow {
	t.Helper()
	for _, sec := range u.settingsRows() {
		for _, r := range sec.rows {
			if r.key == key {
				return r
			}
		}
	}
	t.Fatalf("no settings row %q", key)
	return settingRow{}
}

func TestSettingsPrivacyAndProfile(t *testing.T) {
	b := mock.New()
	u := New(b)
	u.Start(func() {})
	u.applyEvents()

	u.ShowPage("lastseen")
	settingRowByKey(t, u, model.PrivacyLastSeen+":"+model.WhoNobody).run()
	if got := b.Account().Privacy[model.PrivacyLastSeen]; got != model.WhoNobody {
		t.Errorf("last seen is %q after picking Nobody", got)
	}
	u.settings.stale = true
	if r := settingRowByKey(t, u, model.PrivacyLastSeen+":"+model.WhoNobody); !r.on {
		t.Error("Nobody isn't checked after picking it")
	}

	u.ShowPage("profile")
	settingRowByKey(t, u, "name").run()
	if u.settings.editing != editName {
		t.Fatal("clicking the name didn't edit it")
	}
	u.settings.editor.SetText("  New name ")
	u.saveProfileField()
	u.applyEvents()
	if got := b.Account().Name; got != "New name" {
		t.Errorf("name is %q after saving", got)
	}
	if u.me != "New name" || u.settings.editing != 0 {
		t.Errorf("after saving: me %q, editing %d", u.me, u.settings.editing)
	}

	u.ShowPage("blocked")
	if r := settingRowByKey(t, u, "blocked:spam1"); r.kind != setContact {
		t.Errorf("blocked contact row kind %d", r.kind)
	}
}

// TestCtrlEnterSends checks that with Enter is send off, Enter adds a line
// and Ctrl+Enter sends.
func TestCtrlEnterSends(t *testing.T) {
	b := mock.New()
	u := New(b)
	u.Start(func() {})
	u.SelectID("rina")
	u.setEnterSend(false)
	if b.Pref(prefEnterSend) != "off" || u.conv.composer.Submit {
		t.Fatal("Enter is send didn't turn off")
	}
	now := time.Now()
	var ops op.Ops
	var r input.Router
	frame := func() {
		ops.Reset()
		u.Layout(layout.Context{Ops: &ops, Now: now, Source: r.Source(), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1},
			Constraints: layout.Exact(image.Pt(1100, 700))})
		r.Frame(&ops)
		now = now.Add(10 * time.Millisecond)
	}
	frame()
	u.conv.composer.SetText("hello")
	u.requestFocus(&u.conv.composer)
	frame()
	frame()
	before := len(u.msgs)
	r.Queue(key.Event{Name: key.NameReturn, State: key.Press})
	frame()
	if len(u.msgs) != before {
		t.Fatal("Enter sent the message")
	}
	r.Queue(key.Event{Name: key.NameReturn, Modifiers: key.ModShortcut, State: key.Press})
	frame()
	frame()
	if len(u.msgs) != before+1 {
		t.Errorf("Ctrl+Enter didn't send: %d messages, want %d", len(u.msgs), before+1)
	}
}
