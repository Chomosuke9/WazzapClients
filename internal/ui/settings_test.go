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
	now := testNow()
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

// TestZoom checks the zoom shortcuts and their bubble, that the zoom is
// kept, that it scales what the UI lays out, and the Font size menu.
func TestZoom(t *testing.T) {
	b := mock.New()
	u := New(b)
	u.Start(func() {})
	now := testNow()
	var ops op.Ops
	var r input.Router
	var metric unit.Metric
	frame := func() {
		ops.Reset()
		gtx := layout.Context{Ops: &ops, Now: now, Source: r.Source(), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1},
			Constraints: layout.Exact(image.Pt(1100, 700))}
		u.Layout(gtx)
		metric = u.applyZoom(gtx).Metric
		r.Frame(&ops)
		now = now.Add(10 * time.Millisecond)
	}
	frame()
	press := func(name key.Name) {
		r.Queue(key.Event{Name: name, Modifiers: key.ModShortcut, State: key.Press})
		frame()
	}
	press("+")
	press("+")
	if u.zoom.pct != 125 || b.Pref(prefZoom) != "125" || metric.PxPerDp != 1.25 {
		t.Fatalf("after Ctrl+ twice: zoom %d, pref %q, %v px/dp", u.zoom.pct, b.Pref(prefZoom), metric.PxPerDp)
	}
	if u.zoom.changed.IsZero() {
		t.Error("the bubble didn't show")
	}
	if New(b).zoom.pct != 125 {
		t.Error("a new UI doesn't keep the zoom")
	}
	press("-")
	if u.zoom.pct != 110 {
		t.Errorf("after Ctrl-: zoom %d", u.zoom.pct)
	}
	u.btn("zoom:in").Click()
	frame()
	if u.zoom.pct != 125 {
		t.Errorf("the bubble's + left the zoom at %d", u.zoom.pct)
	}
	press("0")
	if u.zoom.pct != 100 || b.Pref(prefZoom) != "" {
		t.Errorf("after Ctrl+0: zoom %d, pref %q", u.zoom.pct, b.Pref(prefZoom))
	}
	for range 20 {
		press("-")
	}
	if u.zoom.pct != zoomLevels[0] {
		t.Errorf("zoomed out to %d", u.zoom.pct)
	}
	// The bubble goes away by itself.
	now = now.Add(zoomBubbleFor + time.Second)
	for range 50 {
		frame()
	}
	if !u.zoom.changed.IsZero() {
		t.Error("the bubble stayed")
	}

	u.ShowPage("general")
	settingRowByKey(t, u, "zoom").run()
	if u.ctx.kind != ctxZoom {
		t.Fatal("the Font size field didn't open its menu")
	}
	for _, it := range u.zoomMenuItems() {
		if it.key == "zoom150" {
			it.run()
		}
	}
	if u.zoom.pct != 150 {
		t.Errorf("picking 150%% left the zoom at %d", u.zoom.pct)
	}
}
