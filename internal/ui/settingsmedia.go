package ui

import (
	"slices"

	"github.com/chomosuke9/wazzapclients/internal/desktop"
	"github.com/chomosuke9/wazzapclients/internal/model"
)

// Preferences of the Video & voice page (Backend.Pref keys): a device's
// name, or "" for the system's default.
const (
	prefCamera     = "device_camera"
	prefMicrophone = "device_microphone"
	prefSpeaker    = "device_speaker"
)

// mediaDevice is a device setting of the Video & voice page.
type mediaDevice struct {
	sub, title, noun, pref string
	list                   func() []string // the system's devices; nil lists none
}

var mediaDevices = []mediaDevice{
	// Cameras would take starting Media Foundation to list, for calls this
	// app doesn't make.
	{"camera", "Camera", "camera", prefCamera, nil},
	{"mic", "Microphone", "microphone", prefMicrophone, desktop.Microphones},
	{"speaker", "Speakers", "speaker", prefSpeaker, desktop.Speakers},
}

func deviceLabel(name string) string {
	if name == "" {
		return "Default"
	}
	return name
}

// mediaSettings is the Video & voice page: the camera, microphone and
// speakers calls would use.
func (u *UI) mediaSettings() []settingsSection {
	b := u.backend
	link := func(d mediaDevice) settingRow {
		return settingRow{key: d.sub, title: d.title, sub: deviceLabel(b.Pref(d.pref)),
			trailing: icChevronRight, run: func() { u.openSettingsSub(d.sub) }}
	}
	return []settingsSection{
		{title: "Video", rows: []settingRow{link(mediaDevices[0])}},
		{title: "Voice", rows: []settingRow{link(mediaDevices[1]), link(mediaDevices[2])},
			note: "Calls can't be made or answered in " + appName + " yet. These are the devices they'll use."},
	}
}

// deviceChoices is the page of one device setting: the system's default,
// then each device the system lists.
func (u *UI) deviceChoices(sub string) []settingsSection {
	b := u.backend
	i := slices.IndexFunc(mediaDevices, func(d mediaDevice) bool { return d.sub == sub })
	if i < 0 {
		return nil
	}
	d := mediaDevices[i]
	cur := b.Pref(d.pref)
	names := []string{""}
	if d.list != nil {
		names = append(names, d.list()...)
	}
	if !slices.Contains(names, cur) {
		names = append(names, cur) // chosen, but not plugged in now
	}
	sec := settingsSection{title: "Choose a " + d.noun}
	for _, n := range names {
		sec.rows = append(sec.rows, settingRow{key: sub + ":" + n, kind: setRadio, title: deviceLabel(n), on: n == cur,
			run: func() { b.SetPref(d.pref, n) }})
	}
	if len(names) == 1 {
		sec.note = "Calls use the system's default " + d.noun + "."
	}
	return []settingsSection{sec}
}

// storageSettings is the Storage page: which media downloads by itself.
func (u *UI) storageSettings() []settingsSection {
	b := u.backend
	toggle := func(key, title string, m model.Media) settingRow {
		on := model.AutoDownload(b.Pref, m)
		return settingRow{key: key, kind: setToggle, title: title, on: on, run: func() {
			v := "on"
			if on {
				v = "off"
			}
			b.SetPref(key, v)
			if m == model.MediaImage {
				u.autoPhotos = !on
			}
		}}
	}
	return []settingsSection{{title: "Automatic downloads", rows: []settingRow{
		toggle(model.PrefAutoPhotos, "Photos", model.MediaImage),
		toggle(model.PrefAutoAudio, "Audio", model.MediaAudio),
		toggle(model.PrefAutoVideos, "Videos", model.MediaVideo),
		toggle(model.PrefAutoDocuments, "Documents", model.MediaDocument),
	}, note: "Choose which media downloads by itself from the messages you receive. Voice messages always do."}}
}
