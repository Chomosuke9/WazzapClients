package ui

import (
	"image"

	"gioui.org/op/paint"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

// Settings > Performance > Advanced > Stickers: Your stickers, whether the sticker picker's
// stickers move, how many animated stickers play at once and how often
// Lottie stickers are drawn. They are app preferences (appPrefs), read
// into perf with the Performance ones (readPerf).

const (
	prefPickerAnim      = "picker_anim"      // "" (still), "hover" or "all"
	prefStickersPlaying = "stickers_playing" // animated stickers playing at once
	prefLottieFPS       = "lottie_fps"       // frames a second of Lottie stickers, at most
	prefKeepPicker      = "picker_keep_open" // "on": sending a sticker or GIF leaves the picker open
)

var stickerPrefs = []string{prefPickerAnim, prefStickersPlaying, prefLottieFPS, prefKeepPicker}

// stickerSettings is the Stickers page.
func (u *UI) stickerSettings() []settingsSection {
	b := u.backend
	radios := func(title, key, note string, opts []perfOpt) settingsSection {
		cur := b.Pref(key)
		sec := settingsSection{title: title, note: note}
		found := false
		for _, o := range opts {
			found = found || o.v == cur
		}
		for _, o := range opts {
			on := o.v == cur || (!found && o.v == "")
			sec.rows = append(sec.rows, settingRow{key: key + ":" + o.v, kind: setRadio, title: o.label,
				on: on, run: func() { u.setPerfPref(key, o.v) }})
		}
		return sec
	}
	sub := "No sticker packs added"
	switch n := len(u.pickerPacks()); {
	case n == 1:
		sub = "1 sticker pack"
	case n > 1:
		sub = itoa(n) + " sticker packs"
	}
	picker := radios("Sticker picker", prefPickerAnim,
		"Animated stickers in the picker play as Play GIFs and animated stickers (Performance) allows.",
		[]perfOpt{{"", "Still"}, {"hover", "Play the one under the pointer"}, {"all", "Play them all"}})
	keep := perf.keepPicker
	after := settingsSection{rows: []settingRow{{key: prefKeepPicker, kind: setToggle, title: "Keep the picker open",
		sub: "Sending a sticker or GIF leaves the picker open, to send another", on: keep, run: func() {
			v := "on"
			if keep {
				v = ""
			}
			u.setPerfPref(prefKeepPicker, v)
		}}}}
	return []settingsSection{
		{rows: []settingRow{{key: "mypacks", ic: icSticker, title: "Your stickers", sub: sub,
			trailing: icChevronRight, run: u.openMyPacks}}},
		picker,
		after,
		radios("Playing at once", prefStickersPlaying,
			"How many animated stickers play at the same time in chats; the rest show a still picture until "+
				"clicked. Each may hold about 4 MB while it plays (a normal one about 2.5 MB). Unlimited plays "+
				"every one on screen, with no cap on their memory.",
			[]perfOpt{{"2", "2"}, {"4", "4"}, {"", "6 (default)"}, {"10", "10"}, {"16", "16"}, {"unlimited", "Unlimited"}}),
		radios("Lottie sticker frame rate", prefLottieFPS,
			"How often WhatsApp's own animated stickers (Flump, Chomp and the like, and premium stickers' "+
				"effects) are drawn. Each frame is drawn from shapes, so 60 takes about twice the CPU of 30. "+
				"A sticker never plays faster than it was made; most are made at 60. Stickers already playing "+
				"change when they next start.",
			[]perfOpt{{"15", "15 a second"}, {"24", "24 a second"}, {"", "30 a second (default)"}, {"60", "60 a second"}}),
	}
}

// pickerPlays reports whether a sticker in the picker plays now: hovered
// is whether the pointer is on it.
func (u *UI) pickerPlays(hovered bool) bool {
	if u.blurred() || perf.autoplay == "never" {
		return false
	}
	switch perf.pickerAnim {
	case "all":
		return perf.autoplay == "" || hovered
	case "hover":
		return hovered
	}
	return false
}

// pickerFrame is what the picker shows of sticker s, whose first frame
// is img: that, or its current frame while it plays.
func (u *UI) pickerFrame(s *model.Message, img *imgEntry, maxPx int, hovered bool) (paint.ImageOp, image.Point) {
	if img.animated && u.pickerPlays(hovered) {
		b, chat, id := u.backend, s.ChatID, s.ID
		if f, ok := u.previewFrame("pick:"+stickerKey(s), maxPx, func() []byte { return b.MediaData(chat, id) }); ok {
			return f, f.Size()
		}
	}
	return img.op, img.size
}
