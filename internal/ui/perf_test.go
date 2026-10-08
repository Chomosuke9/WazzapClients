package ui

import (
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/pointer"

	"github.com/chomosuke9/wazzapclients/internal/mock"
)

// resetPerf puts the package's performance settings back after a test.
func resetPerf(t *testing.T) {
	t.Cleanup(func() {
		perf = defaultPerf
		applyProcess()
	})
}

func TestReadPerf(t *testing.T) {
	b := mock.New()
	if p := readPerf(b); p != defaultPerf {
		t.Fatalf("no prefs: %+v, want the defaults %+v", p, defaultPerf)
	}
	for k, v := range map[string]string{
		prefAnimations: "off", prefSmoothScroll: "off", prefAutoplay: "hover",
		prefImageCache: "64", prefLoadedMsgs: "800", prefGCPercent: "200",
		prefTrimAfter: "never", prefPlaying: "1", prefCPUs: "1",
	} {
		b.SetPref(k, v)
	}
	p := readPerf(b)
	want := perfSettings{autoplay: "hover", imageCache: 64 << 20, loadedMsgs: 800, gcPercent: 200, gifs: 1, cpus: 1}
	if p != want {
		t.Fatalf("got %+v, want %+v", p, want)
	}
	// Values that don't parse, or are out of range, are the defaults.
	for k, v := range map[string]string{
		prefAutoplay: "sometimes", prefImageCache: "lots", prefLoadedMsgs: "50",
		prefGCPercent: "-1", prefTrimAfter: "0", prefPlaying: "99", prefCPUs: "100000",
	} {
		b.SetPref(k, v)
	}
	p = readPerf(b)
	p.animations, p.smoothScroll = true, true
	if p != defaultPerf {
		t.Fatalf("bad values: %+v, want the defaults %+v", p, defaultPerf)
	}
}

// TestPerfSettings goes through the Performance pages: a switch, a knob of
// the Advanced page and Reset to defaults.
func TestPerfSettings(t *testing.T) {
	resetPerf(t)
	st := newSlashTest(t, "rina")
	u := st.u
	click := func(btn string) {
		t.Helper()
		u.btn(btn).Click()
		st.frame()
		st.frame()
	}
	u.ShowPage("performance")
	st.frame()
	click("settings:" + prefAnimations)
	if perf.animations || st.b.Pref(prefAnimations) != "off" || settingRowByKey(t, u, prefAnimations).on {
		t.Fatal("the Animations switch didn't turn them off")
	}
	var tw tween
	if v := tw.step(C{Now: st.now}, true, time.Second); v != 1 {
		t.Fatalf("with animations off a tween went to %v, want a jump to 1", v)
	}
	click("settings:autoplay:never")
	if perf.autoplay != "never" || st.b.Pref(prefAutoplay) != "never" {
		t.Fatalf("autoplay %q, pref %q", perf.autoplay, st.b.Pref(prefAutoplay))
	}

	click("settings:advanced")
	if u.settings.sub != "advanced" {
		t.Fatalf("Advanced opened %q", u.settings.sub)
	}
	settingRowByKey(t, u, "mem") // Memory now
	click("settings:knob:" + prefImageCache)
	if u.settings.sub != "knob:"+prefImageCache || u.settingsTitle() != "Picture cache" {
		t.Fatalf("the knob opened %q, titled %q", u.settings.sub, u.settingsTitle())
	}
	click("settings:opt:" + prefImageCache + ":128")
	if _, budget := u.images.usage(); budget != 128<<20 || perf.imageCache != 128<<20 {
		t.Fatalf("picture cache budget %d after choosing 128 MB", budget)
	}
	if !settingRowByKey(t, u, "opt:"+prefImageCache+":128").on {
		t.Fatal("the choice isn't marked")
	}
	u.settingsBack()
	st.frame()
	if u.settings.sub != "advanced" {
		t.Fatalf("back from a knob went to %q, want the Advanced page", u.settings.sub)
	}
	if r := settingRowByKey(t, u, "knob:"+prefImageCache); r.sub != "128 MB" {
		t.Fatalf("the knob's row says %q", r.sub)
	}

	click("settings:perfreset")
	if !u.dialog.isOpen() {
		t.Fatal("Reset to defaults didn't ask first")
	}
	click("dialog:1")
	for _, k := range perfPrefs {
		if v := st.b.Pref(k); v != "" {
			t.Errorf("after the reset %s is %q", k, v)
		}
	}
	if perf != defaultPerf {
		t.Fatalf("after the reset: %+v", perf)
	}
	if _, budget := u.images.usage(); budget != defaultPerf.imageCache {
		t.Fatalf("picture cache budget %d after the reset", budget)
	}
}

// TestNoAnimationsAtRest checks that with animations off, opening a menu
// draws it at once and asks for no more frames.
func TestNoAnimationsAtRest(t *testing.T) {
	resetPerf(t)
	st := newSlashTest(t, "rina")
	st.b.SetPref(prefAnimations, "off")
	st.u.applyPerf()
	st.frame()
	st.r.WakeupTime()
	for _, o := range []string{"msgmenu", "emoji", "viewer"} {
		st.u.ShowOverlay(o, 600, 300)
		st.frame()
		if w, ok := st.r.WakeupTime(); ok && w.IsZero() {
			t.Errorf("%s: still redrawing right after it opened", o)
		}
		st.u.Escape()
		st.frame()
		st.r.WakeupTime()
	}
}

// TestWheelWithoutSmoothScroll checks that with Smooth scrolling off a
// wheel notch moves the list in one frame.
func TestWheelWithoutSmoothScroll(t *testing.T) {
	resetPerf(t)
	st := newSlashTest(t, "rina")
	st.b.SetPref(prefSmoothScroll, "off")
	st.u.applyPerf()
	st.frame()
	sb := &st.u.sidebar.list.List
	start := sb.Position
	st.r.Queue(pointer.Event{Kind: pointer.Scroll, Source: pointer.Mouse, Position: f32.Pt(200, 330), Scroll: f32.Pt(0, 120)})
	st.frame()
	if moved := sb.Position.Offset - start.Offset; sb.Position.First == start.First && moved != 120 || st.u.wheels[sb] != nil {
		t.Fatalf("a notch moved the list from %+v to %+v, left to ease: %+v", start, sb.Position, st.u.wheels[sb])
	}
}
