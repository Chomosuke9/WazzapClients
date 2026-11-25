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

// autoDownloadPrefs are the kinds of media of the Media auto-download page.
var autoDownloadPrefs = []struct {
	key, title string
	media      model.Media
}{
	{model.PrefAutoPhotos, "Photos", model.MediaImage},
	{model.PrefAutoAudio, "Audio", model.MediaAudio},
	{model.PrefAutoVideos, "Videos", model.MediaVideo},
	{model.PrefAutoDocuments, "Documents", model.MediaDocument},
}

// uploadQuality is the Media upload quality page: what photos are sent as,
// the same choice as the send view's quality menu.
func (u *UI) uploadQuality() []settingsSection {
	sec := settingsSection{note: "Photos go out at this quality. You can change it for each send, beside the photos."}
	add := func(q model.Quality, title, sub string) {
		sec.rows = append(sec.rows, settingRow{key: "quality:" + qualityNames[q], kind: setRadio, title: title, sub: sub,
			on: u.attach.quality == q, run: func() {
				u.attach.quality = q
				u.backend.SetPref(prefPhotoQuality, qualityPrefs[q])
			}})
	}
	add(model.QualityStandard, "Standard quality", "Standard photos use less storage space and are faster to send")
	add(model.QualityHD, "HD quality", "HD photos are clearer, and slower to send")
	if u.rawPhotos { // an extra feature
		add(model.QualityRaw, "Raw quality", "Raw photos are sent as they are")
	}
	return []settingsSection{sec}
}

// autoDownloads is the Media auto-download page: which media downloads by
// itself.
func (u *UI) autoDownloads() []settingsSection {
	b := u.backend
	sec := settingsSection{note: "Voice messages are always automatically downloaded for the best communication experience."}
	for _, p := range autoDownloadPrefs {
		on := model.AutoDownload(b.Pref, p.media)
		sec.rows = append(sec.rows, settingRow{key: p.key, kind: setToggle, left: true, title: p.title, on: on, run: func() {
			v := "on"
			if on {
				v = "off"
			}
			b.SetPref(p.key, v)
			if p.media == model.MediaImage {
				u.autoPhotos = !on
			}
		}})
	}
	return []settingsSection{sec}
}
