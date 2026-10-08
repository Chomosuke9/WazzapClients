package ui

import (
	"fmt"
	"runtime"
	rdebug "runtime/debug"
	"strconv"
	"time"

	"gioui.org/layout"
	"gioui.org/op"

	"github.com/chomosuke9/wazzapclients/internal/memtrim"
	"github.com/chomosuke9/wazzapclients/internal/model"
	"github.com/chomosuke9/wazzapclients/internal/ui/icon"
)

// Settings > Performance: switches that trade looks for less work, and an
// Advanced page of knobs that trade memory against CPU. They belong to the
// app, not to an account (appPrefs).

// Preferences of the Performance page (Backend.Pref keys).
const (
	prefAnimations   = "animations"    // on unless "off"
	prefSmoothScroll = "smooth_scroll" // on unless "off"
	prefAutoplay     = "autoplay"      // "" (always), "hover" or "never"

	// The Advanced page's knobs. "" is the default of each.
	prefImageCache = "image_cache"     // MB of decoded pictures
	prefLoadedMsgs = "loaded_messages" // messages of the open chat kept loaded
	prefGCPercent  = "gc_percent"      // the GC's target, debug.SetGCPercent
	prefTrimAfter  = "trim_after"      // seconds without a frame, or "never"
	prefPlaying    = "playing"         // GIFs playing at once; stickers twice as many
	prefCPUs       = "cpus"            // GOMAXPROCS, or "" for all
)

var perfPrefs = []string{prefAnimations, prefSmoothScroll, prefAutoplay,
	prefImageCache, prefLoadedMsgs, prefGCPercent, prefTrimAfter, prefPlaying, prefCPUs}

// perfSettings is what the Performance prefs say, parsed.
type perfSettings struct {
	animations   bool
	smoothScroll bool
	autoplay     string
	imageCache   int           // bytes
	loadedMsgs   int           // at least two pages (paging.go)
	gcPercent    int           //
	trimAfter    time.Duration // 0: never
	gifs         int           // stickers play twice as many
	cpus         int           // 0: all
}

var defaultPerf = perfSettings{
	animations:   true,
	smoothScroll: true,
	imageCache:   32 << 20,
	loadedMsgs:   maxLoaded,
	gcPercent:    50, // as cmd/wazzap sets it
	trimAfter:    idleTrim,
	gifs:         maxGIFs,
}

// perf is the app's performance settings. Animation helpers and the host
// read it, so it's the package's rather than a UI's; only the UI goroutine
// touches it.
var perf = defaultPerf

// applied is what applyProcess set last: the process starts as cmd/wazzap
// leaves it, which is the default.
var applied = struct{ gc, cpus int }{defaultPerf.gcPercent, 0}

// readPerf parses b's Performance prefs. A value that doesn't parse is the
// default.
func readPerf(b model.Backend) perfSettings {
	p := defaultPerf
	if b == nil {
		return p
	}
	num := func(key string, lo, hi int) (int, bool) {
		n, err := strconv.Atoi(b.Pref(key))
		return n, err == nil && n >= lo && n <= hi
	}
	p.animations = prefOn(b, prefAnimations)
	p.smoothScroll = prefOn(b, prefSmoothScroll)
	if v := b.Pref(prefAutoplay); v == "hover" || v == "never" {
		p.autoplay = v
	}
	if n, ok := num(prefImageCache, 4, 1024); ok {
		p.imageCache = n << 20
	}
	if n, ok := num(prefLoadedMsgs, 2*messagePage, 10000); ok {
		p.loadedMsgs = n
	}
	if n, ok := num(prefGCPercent, 10, 1000); ok {
		p.gcPercent = n
	}
	if v := b.Pref(prefTrimAfter); v == "never" {
		p.trimAfter = 0
	} else if n, ok := num(prefTrimAfter, 1, 3600); ok {
		p.trimAfter = time.Duration(n) * time.Second
	}
	if n, ok := num(prefPlaying, 0, 20); ok {
		p.gifs = n
	}
	if n, ok := num(prefCPUs, 1, runtime.NumCPU()); ok {
		p.cpus = n
	}
	return p
}

// loadPerf reads b's Performance prefs and applies them.
func loadPerf(b model.Backend) {
	perf = readPerf(b)
	applyProcess()
}

// applyProcess sets what perf says about the whole process: the GC's
// target and how many cores Go uses. A Go memory limit isn't offered:
// once a long session's live heap nears one, the GC runs back to back.
func applyProcess() {
	if perf.gcPercent != applied.gc {
		rdebug.SetGCPercent(perf.gcPercent)
		applied.gc = perf.gcPercent
	}
	if perf.cpus != applied.cpus {
		if perf.cpus == 0 {
			runtime.SetDefaultGOMAXPROCS()
		} else {
			runtime.GOMAXPROCS(perf.cpus)
		}
		applied.cpus = perf.cpus
	}
}

// awayAfter is how long after the window loses focus memory is trimmed,
// or 0 for never.
func awayAfter() time.Duration {
	if perf.trimAfter == 0 {
		return 0
	}
	return min(awayTrim, perf.trimAfter)
}

// resetTimer starts t over to fire after d, or stops it when d is 0.
func resetTimer(t *time.Timer, d time.Duration) {
	if d == 0 {
		t.Stop()
		return
	}
	t.Reset(d)
}

// setPerfPref changes a Performance pref and applies it at once.
func (u *UI) setPerfPref(key, v string) {
	u.backend.SetPref(key, v)
	u.applyPerf()
}

// applyPerf applies the Performance prefs after one changed.
func (u *UI) applyPerf() {
	loadPerf(u.backend)
	u.images.resize(perf.imageCache)
	u.wheels = nil
	u.doodles = prefOn(u.backend, prefDoodles)
}

// autoplays reports whether m's GIF or animated sticker plays now.
func (u *UI) autoplays(m *model.Message) bool {
	switch perf.autoplay {
	case "never":
		return false
	case "hover":
		return u.hovered[m.ID]
	}
	return true
}

// perfOpt is one of a knob's choices; v "" is the default.
type perfOpt struct{ v, label string }

// perfKnob is a setting of the Advanced page, chosen on a page of its own.
type perfKnob struct {
	key, title string
	ic         *icon.Icon
	about      string // the note under its choices
	opts       []perfOpt
}

func (k *perfKnob) current(b model.Backend) perfOpt {
	v := b.Pref(k.key)
	for _, o := range k.opts {
		if o.v == v {
			return o
		}
	}
	return k.opts[0]
}

var perfKnobs = []*perfKnob{
	{key: prefImageCache, title: "Picture cache", ic: icMedia,
		about: "Pictures kept decoded in memory. A bigger cache shows pictures you scroll back to at once, " +
			"instead of decoding them again.",
		opts: []perfOpt{{"", "32 MB (default)"}, {"16", "16 MB"}, {"64", "64 MB"}, {"128", "128 MB"}}},
	{key: prefLoadedMsgs, title: "Messages kept loaded", ic: icChats,
		about: "How much of the open chat stays in memory. Scrolling further loads more and lets go of the far end.",
		opts:  []perfOpt{{"", "400 (default)"}, {"200", "200"}, {"800", "800"}, {"1600", "1600"}}},
	{key: prefGCPercent, title: "Memory or CPU", ic: icSpeed,
		about: "How much the memory in use may grow before Go collects garbage. Less memory means " +
			"collecting more often, which takes more CPU while a lot happens, such as a history sync. " +
			"There's no hard memory limit: near one, the collector runs without a break and the app crawls.",
		opts: []perfOpt{{"", "Balanced (default)"}, {"25", "Least memory"}, {"100", "Less CPU"}, {"200", "Least CPU"}}},
	{key: prefTrimAfter, title: "Give memory back", ic: icClock,
		about: "After the window draws nothing for this long, memory it no longer uses goes back to the system. " +
			"Coming back costs a few milliseconds while the pages still in use return.",
		opts: []perfOpt{{"", "After 10 seconds (default)"}, {"3", "After 3 seconds"}, {"30", "After 30 seconds"},
			{"60", "After a minute"}, {"never", "Never"}}},
	{key: prefPlaying, title: "Playing at once", ic: icPlayFill,
		about: "How many GIFs, and twice as many animated stickers, play at the same time. The rest show a still picture.",
		opts:  []perfOpt{{"", "3 GIFs (default)"}, {"1", "1 GIF"}, {"6", "6 GIFs"}}},
	{key: prefCPUs, title: "CPU cores", ic: icMemory,
		about: "How many cores the app's work may use at once. Fewer keeps a busy moment, " +
			"such as a history sync, from slowing down other apps, and makes it take longer.",
		opts: cpuOpts()},
}

func cpuOpts() []perfOpt {
	opts := []perfOpt{{"", fmt.Sprintf("All %d (default)", runtime.NumCPU())}}
	for n := 1; n < runtime.NumCPU(); n *= 2 {
		opts = append(opts, perfOpt{strconv.Itoa(n), strconv.Itoa(n)})
	}
	return opts
}

func knobNamed(key string) *perfKnob {
	for _, k := range perfKnobs {
		if k.key == key {
			return k
		}
	}
	return nil
}

// perfSettingsPage is the Performance page.
func (u *UI) perfSettingsPage() []settingsSection {
	b := u.backend
	toggle := func(key, title, sub string) settingRow {
		on := prefOn(b, key)
		return settingRow{key: key, kind: setToggle, title: title, sub: sub, on: on, run: func() {
			setPref(b, key, !on)
			u.applyPerf()
		}}
	}
	play := settingsSection{title: "Play GIFs and animated stickers"}
	for _, o := range []perfOpt{{"", "Automatically"}, {"hover", "When the pointer is on them"}, {"never", "Never"}} {
		play.rows = append(play.rows, settingRow{key: "autoplay:" + o.v, kind: setRadio, title: o.label,
			on: perf.autoplay == o.v, run: func() { u.setPerfPref(prefAutoplay, o.v) }})
	}
	play.note = "GIFs that don't play show their first picture: click one to play it in the viewer."
	return []settingsSection{
		{title: "Motion", rows: []settingRow{
			toggle(prefAnimations, "Animations", "Menus, panels and highlights slide and fade. Off, they change at once"),
			toggle(prefSmoothScroll, "Smooth scrolling", "The mouse wheel glides lists instead of moving them a notch at a time"),
			toggle(prefDoodles, "Wallpaper doodles", "Draw doodles on the chat background"),
		}},
		play,
		{rows: []settingRow{{key: "advanced", ic: icTune, title: "Advanced", sub: "Memory, CPU and caches",
			trailing: icChevronRight, run: func() { u.openSettingsSub("advanced") }}}},
	}
}

// advancedSettings is the Advanced page of Performance: what the app uses
// now, and the knobs.
func (u *UI) advancedSettings() []settingsSection {
	b := u.backend
	now := settingsSection{title: "Memory now", rows: []settingRow{
		{key: "mem", kind: setCustom, w: u.memoryStats},
		{key: "trim", ic: icDeleteSweep, title: "Free memory now", sub: "Give memory the app isn't using back to the system",
			run: func() { memtrim.Trim(); u.settings.mem.at = time.Time{} }},
	}}
	knobs := func(keys ...string) []settingRow {
		var rows []settingRow
		for _, key := range keys {
			k := knobNamed(key)
			rows = append(rows, settingRow{key: "knob:" + k.key, ic: k.ic, title: k.title, sub: k.current(b).label,
				trailing: icChevronRight, run: func() { u.openSettingsSub("knob:" + k.key) }})
		}
		return rows
	}
	reset := settingRow{key: "perfreset", ic: icRefresh, title: "Reset to defaults",
		sub: "Every Performance setting, the switches too", run: func() {
			u.confirm("Reset performance settings?", "Every setting on the Performance pages goes back to its default.",
				dialogButton{label: "Reset", primary: true, run: func() {
					for _, k := range perfPrefs {
						b.SetPref(k, "")
					}
					u.applyPerf()
					u.settings.stale = true
				}})
		}}
	return []settingsSection{
		now,
		{title: "Memory", rows: knobs(prefImageCache, prefLoadedMsgs, prefGCPercent, prefTrimAfter)},
		{title: "CPU", rows: knobs(prefPlaying, prefCPUs)},
		{rows: []settingRow{reset},
			note: "These change how much memory and CPU the app uses, not what it shows. Watch Memory now as you try them."},
	}
}

// knobSettings is the page of one knob's choices.
func (u *UI) knobSettings(key string) []settingsSection {
	k := knobNamed(key)
	if k == nil {
		return nil
	}
	cur := k.current(u.backend)
	sec := settingsSection{title: k.title, note: k.about}
	for _, o := range k.opts {
		sec.rows = append(sec.rows, settingRow{key: "opt:" + k.key + ":" + o.v, kind: setRadio, title: o.label,
			on: o == cur, run: func() { u.setPerfPref(k.key, o.v) }})
	}
	return []settingsSection{sec}
}

// memStats is what Memory now shows, read at most once a second.
type memStats struct {
	at           time.Time
	private      uint64 // the private working set; 0 where it isn't known
	heap, fromOS uint64
	images       int
}

// memoryStats draws Memory now: what Task Manager shows, the Go heap and
// the picture cache.
func (u *UI) memoryStats(gtx C) D {
	s := &u.settings.mem
	now := u.now()
	if now.Sub(s.at) >= time.Second {
		var ms runtime.MemStats
		runtime.ReadMemStats(&ms)
		s.at, s.private = now, memtrim.PrivateWorkingSet()
		s.heap, s.fromOS = ms.HeapInuse, ms.HeapSys-ms.HeapReleased
		s.images, _ = u.images.usage()
	}
	if !gtx.Now.IsZero() {
		gtx.Execute(op.InvalidateCmd{At: gtx.Now.Add(time.Second)})
	}
	mb := func(n uint64) string { return fmt.Sprintf("%.1f MB", float64(n)/(1<<20)) }
	_, budget := u.images.usage()
	rows := []layout.FlexChild{}
	stat := func(title, value string) {
		rows = append(rows, layout.Rigid(func(gtx C) D { return u.layoutStat(gtx, title, value) }))
	}
	if s.private > 0 {
		stat("In use (Task Manager)", mb(s.private))
	}
	stat("Go heap in use", mb(s.heap))
	stat("Go heap held from the system", mb(s.fromOS))
	stat("Pictures decoded", mb(uint64(s.images))+" of "+mb(uint64(budget)))
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx, rows...)
}

// layoutStat is a row of Memory now: what, then how much.
func (u *UI) layoutStat(gtx C, title, value string) D {
	p := u.pal
	return layout.Inset{Left: 29.5, Right: 29}.Layout(gtx, func(gtx C) D {
		return vcenter(gtx, gtx.Dp(40), func(gtx C) D {
			gtx.Constraints.Min.X = gtx.Constraints.Max.X
			return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
				layout.Flexed(1, func(gtx C) D {
					d := u.label(16, title, p.Text, labelOpts{maxLines: 1}).Layout(gtx)
					d.Size.X = gtx.Constraints.Max.X
					return d
				}),
				layout.Rigid(u.label(15, value, p.TextSecondary, labelOpts{maxLines: 1}).Layout),
			)
		})
	})
}
