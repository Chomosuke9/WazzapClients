package ui

import (
	"fmt"
	"hash/fnv"
	"image"
	"image/color"
	"mime"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"gioui.org/font"
	"gioui.org/io/event"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/widget/material"

	"github.com/chomosuke9/wazzapclients/internal/model"
	"github.com/chomosuke9/wazzapclients/internal/video"
)

// Documents and audio in bubbles: a document card that opens the file, and
// a player for voice messages and audio files.

// hasAttachment reports whether m's bubble starts with a document card or
// an audio player.
func hasAttachment(m *model.Message) bool {
	if m.Kind == model.KindDeleted || m.Kind == model.KindUnsupported || m.Kind == model.KindViewOnce {
		return false
	}
	switch m.Media {
	case model.MediaDocument, model.MediaVoice, model.MediaAudio:
		return true
	}
	return hasCard(m)
}

// attachmentCaption is the text under a document card. A document without
// a caption has its file name as its text, which the card shows already.
func attachmentCaption(m *model.Message) string {
	if m.Media == model.MediaDocument && m.Text != m.FileName && m.FileName != "" {
		return m.Text
	}
	return ""
}

// fileState tracks documents and audio files on disk.
type fileState struct {
	have map[string]bool // message key → downloaded, as Backend.HasMediaFile said
	busy map[string]bool // downloads started from a bubble
}

func fileKey(m *model.Message) string { return m.ChatID + "/" + m.ID }

// fileReady reports whether m's file is downloaded. It asks the backend
// once per message.
func (u *UI) fileReady(m *model.Message) bool {
	k := fileKey(m)
	have, ok := u.files.have[k]
	if !ok {
		if u.files.have == nil {
			u.files.have = make(map[string]bool)
		}
		have = u.backend.HasMediaFile(m)
		u.files.have[k] = have
	}
	return have
}

// fileDownloaded updates the bubbles after a download ended.
func (u *UI) fileDownloaded(e model.MediaEvent) {
	k := e.ChatID + "/" + e.MsgID
	delete(u.files.have, k)
	delete(u.files.busy, k)
	u.voiceDownloaded(e)
}

// openDocument opens a document (downloading it first if needed).
func (u *UI) openDocument(m *model.Message) {
	if !u.fileReady(m) {
		u.markBusy(m)
	}
	u.backend.OpenMedia(m)
}

// downloadDocument downloads a document without opening it.
func (u *UI) downloadDocument(m *model.Message) {
	u.markBusy(m)
	u.backend.MediaFile(m)
}

func (u *UI) markBusy(m *model.Message) {
	if u.files.busy == nil {
		u.files.busy = make(map[string]bool)
	}
	u.files.busy[fileKey(m)] = true
}

// fileExt is a document's type as WhatsApp names it ("PDF"), from its file
// name or MIME type.
func fileExt(m *model.Message) string {
	ext := strings.TrimPrefix(filepath.Ext(m.FileName), ".")
	if ext == "" || len(ext) > 5 {
		if t, _, err := mime.ParseMediaType(m.FileType); err == nil {
			if exts, _ := mime.ExtensionsByType(t); len(exts) > 0 {
				ext = strings.TrimPrefix(exts[0], ".")
			}
		}
	}
	if len(ext) > 5 {
		ext = ""
	}
	return strings.ToUpper(ext)
}

// fileColor is the color of a document's icon, by type, like WhatsApp's.
func fileColor(ext string) color.NRGBA {
	switch ext {
	case "PDF":
		return rgb(0xe5483b)
	case "DOC", "DOCX", "ODT", "RTF":
		return rgb(0x3f7ee8)
	case "XLS", "XLSX", "ODS", "CSV":
		return rgb(0x1f9d55)
	case "PPT", "PPTX", "ODP", "KEY":
		return rgb(0xf0702b)
	case "ZIP", "RAR", "7Z", "TAR", "GZ":
		return rgb(0x8a6dde)
	case "APK":
		return rgb(0x3ddc84)
	}
	return rgb(0x8696a0)
}

// formatSize renders a file size the way WhatsApp does ("1.2 MB").
func formatSize(n int64) string {
	switch {
	case n <= 0:
		return ""
	case n < 1000:
		return fmt.Sprintf("%d bytes", n)
	case n < 1000*1000:
		return fmt.Sprintf("%d kB", (n+500)/1000)
	case n < 1000*1000*1000:
		return trimZero(fmt.Sprintf("%.1f", float64(n)/1e6)) + " MB"
	}
	return trimZero(fmt.Sprintf("%.1f", float64(n)/1e9)) + " GB"
}

func trimZero(s string) string { return strings.TrimSuffix(s, ".0") }

// docInfo is the line under a document card: "3 pages • PDF • 1.2 MB".
func docInfo(m *model.Message) string {
	var parts []string
	switch {
	case m.Pages == 1:
		parts = append(parts, "1 page")
	case m.Pages > 1:
		parts = append(parts, itoa(m.Pages)+" pages")
	}
	if ext := fileExt(m); ext != "" {
		parts = append(parts, ext)
	}
	if s := formatSize(m.FileSize); s != "" {
		parts = append(parts, s)
	}
	return strings.Join(parts, " • ")
}

// clock formats seconds as m:ss.
func clock(secs int) string {
	return fmt.Sprintf("%d:%02d", secs/60, secs%60)
}

// bubbleColors are the colors of the bubble an attachment sits in.
type bubbleColors struct {
	bg, card, text, secondary color.NRGBA
}

// layoutAttachment draws a document card or audio player, at most maxW
// wide. Its last row is metaH high and leaves metaW free at its right end
// for the timestamp.
func (u *UI) layoutAttachment(gtx C, c *model.Chat, m *model.Message, maxW, metaW, metaH int, cols bubbleColors) D {
	if hasCard(m) {
		return u.layoutCard(gtx, m, maxW, metaW, metaH, cols)
	}
	if m.Media == model.MediaDocument {
		return u.layoutDocument(gtx, m, maxW, metaW, metaH, cols)
	}
	return u.layoutAudio(gtx, c, m, maxW, metaW, metaH, cols)
}

func (u *UI) layoutDocument(gtx C, m *model.Message, maxW, metaW, metaH int, cols bubbleColors) D {
	cardBtn, dlBtn := u.btn("doc:"+m.ID), u.btn("docdl:"+m.ID)
	if cardBtn.Clicked(gtx) {
		u.openDocument(m)
	}
	if dlBtn.Clicked(gtx) {
		u.downloadDocument(m)
	}
	ready, busy := u.fileReady(m), u.files.busy[fileKey(m)]
	w := min(maxW, gtx.Dp(330))
	cardH := gtx.Dp(62)
	pad := gtx.Dp(10)

	// The card, which opens the file.
	card := image.Rect(0, 0, w, cardH)
	func() {
		defer clip.Rect(card).Push(gtx.Ops).Pop()
		cg := gtx
		cg.Constraints = layout.Exact(card.Size())
		clickable(cg, cardBtn, func(gtx C) D {
			fillRRect(gtx, card, gtx.Dp(6), mix(cols.card, cols.text, 0.06*u.hover(gtx, cardBtn)))
			return D{Size: card.Size()}
		})
	}()

	// A page with a folded corner, in the color of the file's type.
	ext := fileExt(m)
	iw, ih := gtx.Dp(30), gtx.Dp(38)
	ix, iy := pad, (cardH-ih)/2
	fillRRect(gtx, image.Rect(ix, iy, ix+iw, iy+ih), gtx.Dp(4), fileColor(ext))
	fold := gtx.Dp(9)
	fillRect(gtx, image.Rect(ix+iw-fold, iy, ix+iw, iy+fold), cols.card)
	fillRRect(gtx, image.Rect(ix+iw-fold, iy, ix+iw, iy+fold), gtx.Dp(2), mix(fileColor(ext), rgb(0xffffff), 0.45))
	if ext != "" {
		lg := gtx
		lg.Constraints = layout.Constraints{Max: image.Pt(iw, ih)}
		lbl := record(lg, u.label(8.5, ext, rgb(0xffffff), labelOpts{weight: font.Bold, maxLines: 1}).Layout)
		lbl.at(gtx, ix+(iw-lbl.size.X)/2, iy+ih-lbl.size.Y-gtx.Dp(4))
	}

	// The download button, or a spinner while it downloads.
	right := w - pad
	if !ready {
		sz := gtx.Dp(34)
		x, y := w-pad-sz, (cardH-sz)/2
		right = x - gtx.Dp(8)
		t := op.Offset(image.Pt(x, y)).Push(gtx.Ops)
		if busy {
			lg := gtx
			lg.Constraints = layout.Exact(image.Pt(sz, sz))
			l := material.Loader(u.th)
			l.Color = cols.secondary
			l.Layout(lg)
		} else {
			bg := gtx
			bg.Constraints = layout.Exact(image.Pt(sz, sz))
			clickable(bg, dlBtn, func(gtx C) D {
				mid := image.Pt(sz/2, sz/2)
				fillCircle(gtx, mid, sz/2, cols.secondary)
				fillCircle(gtx, mid, sz/2-max(1, gtx.Dp(1.5)), mix(cols.card, cols.text, 0.08*u.hover(gtx, dlBtn)))
				return centerIn(gtx, sz, iconW(icDownload, 20, cols.secondary))
			})
		}
		t.Pop()
	}

	// The file name, on up to two lines.
	nx := ix + iw + gtx.Dp(12)
	ng := gtx
	ng.Constraints = layout.Constraints{Max: image.Pt(max(0, right-nx), cardH)}
	name := m.FileName
	if name == "" {
		name = m.Text
	}
	if name == "" {
		name = "Document"
	}
	nm := record(ng, u.label(14.5, name, cols.text, labelOpts{maxLines: 2}).Layout)
	nm.at(gtx, nx, (cardH-nm.size.Y)/2)

	// Pages, type and size, beside the timestamp.
	y := cardH + gtx.Dp(4)
	ig := gtx
	ig.Constraints = layout.Constraints{Max: image.Pt(max(0, w-metaW-gtx.Dp(8)), metaH)}
	info := record(ig, u.label(12, docInfo(m), cols.secondary, labelOpts{maxLines: 1}).Layout)
	info.at(gtx, gtx.Dp(2), y+(metaH-info.size.Y)/2)
	return D{Size: image.Pt(w, y+metaH)}
}

// voiceState is the voice message or audio file playing. Only one plays at
// a time.
type voiceState struct {
	key     string // fileKey of the message
	msg     *model.Message
	player  *video.Player
	loading bool    // waiting for the download
	seekTo  float32 // where to go (0 to 1) once the length is known; -1 none
	drag    bool    // the knob follows the pointer
}

// toggleVoice plays or pauses an audio message.
func (u *UI) toggleVoice(m *model.Message) {
	v := &u.voice
	if v.key == fileKey(m) {
		switch {
		case v.player != nil:
			st := v.player.Status()
			switch {
			case st.Paused || st.Ended:
				v.player.Play()
			default:
				v.player.Pause()
			}
			return
		case v.loading:
			return
		}
	}
	u.stopVoice()
	*v = voiceState{key: fileKey(m), msg: m, seekTo: -1}
	u.loadVoice()
}

// loadVoice opens the player, or waits for the download.
func (u *UI) loadVoice() {
	v := &u.voice
	path := u.backend.MediaFile(v.msg)
	if path == "" {
		v.loading = true // voiceDownloaded continues
		return
	}
	v.loading = false
	p, err := video.OpenAudio(path, u.images.invalidate)
	if err != nil {
		// No player here: the system's app plays it.
		m := v.msg
		u.stopVoice()
		u.backend.OpenMedia(m)
		return
	}
	p.SetVolume(float64(u.volume))
	if v.msg.Media == model.MediaVoice && u.voiceRate != 1 {
		p.SetRate(u.voiceRate)
	}
	v.player = p
}

// voiceRates are the speeds the voice message pill goes through.
var voiceRates = []float64{1, 1.5, 2}

// prefVoiceRate is the voice message speed; 1 if "".
const prefVoiceRate = "voice_rate"

// rateName labels a speed: "1×", "1.5×".
func rateName(r float64) string {
	return strconv.FormatFloat(r, 'f', -1, 64) + "×"
}

// loadVoiceRate reads the saved voice message speed.
func (u *UI) loadVoiceRate() {
	u.voiceRate = 1
	if r, err := strconv.ParseFloat(u.backend.Pref(prefVoiceRate), 64); err == nil && slices.Contains(voiceRates, r) {
		u.voiceRate = r
	}
}

// nextVoiceRate moves the speed of voice messages, the playing one and
// the next ones, on to the next of voiceRates.
func (u *UI) nextVoiceRate() {
	i := slices.Index(voiceRates, u.voiceRate)
	u.voiceRate = voiceRates[(i+1)%len(voiceRates)]
	if u.voice.player != nil {
		u.voice.player.SetRate(u.voiceRate)
	}
	u.backend.SetPref(prefVoiceRate, strconv.FormatFloat(u.voiceRate, 'f', -1, 64))
}

// stopVoice closes the player.
func (u *UI) stopVoice() {
	if u.voice.player != nil {
		u.voice.player.Close()
	}
	u.voice = voiceState{seekTo: -1}
}

// voiceDownloaded starts the audio message that waited for its download.
func (u *UI) voiceDownloaded(e model.MediaEvent) {
	v := &u.voice
	if !v.loading || v.key != e.ChatID+"/"+e.MsgID {
		return
	}
	if e.Failed {
		u.stopVoice()
		return
	}
	u.loadVoice()
}

// voiceProgress returns how far m has played (0 to 1), its position and
// length to show, and whether it is playing.
func (u *UI) voiceProgress(m *model.Message) (t float32, pos int, playing, loading bool) {
	v := &u.voice
	if v.key != fileKey(m) {
		return 0, m.Duration, false, false
	}
	if v.player == nil {
		return max(0, v.seekTo), m.Duration, false, v.loading
	}
	st := v.player.Status()
	if st.Err != nil {
		u.stopVoice()
		u.toast("This audio can't play here. Opening it in your audio player…")
		u.backend.OpenMedia(m)
		return 0, m.Duration, false, false
	}
	if st.Ready && st.Dur > 0 && v.seekTo >= 0 && !v.drag {
		v.player.Seek(time.Duration(float64(st.Dur)*float64(v.seekTo)), true)
		v.seekTo = -1
	}
	if st.Ended && !v.drag {
		// Back to the start, and the player's memory back to the system.
		u.stopVoice()
		return 0, m.Duration, false, false
	}
	if st.Dur <= 0 {
		return 0, m.Duration, false, !st.Ready
	}
	t = float32(st.Pos) / float32(st.Dur)
	if v.drag && v.seekTo >= 0 {
		t = v.seekTo
	}
	return min(1, t), int(st.Pos.Seconds()), !st.Paused, false
}

// seekVoice moves the playing audio to t (0 to 1), starting it first if
// it isn't the one playing.
func (u *UI) seekVoice(m *model.Message, t float32, done bool) {
	v := &u.voice
	if v.key != fileKey(m) {
		u.toggleVoice(m)
	}
	if v.player == nil {
		v.seekTo = t
		return
	}
	st := v.player.Status()
	if st.Dur <= 0 {
		v.seekTo = t
		return
	}
	v.player.Seek(time.Duration(float64(st.Dur)*float64(t)), done)
	v.seekTo = -1
	if v.drag {
		v.seekTo = t // shown until the player gets there
	}
	if st.Ended {
		v.player.Play()
	}
}

// waveform returns n bar heights from 0 to 1. Messages without a waveform
// get a made-up one, the same every time.
func waveform(m *model.Message, n int) []float32 {
	out := make([]float32, n)
	if len(m.Waveform) > 0 {
		for i := range out {
			j := i * len(m.Waveform) / n
			out[i] = float32(min(100, m.Waveform[j])) / 100
		}
		return out
	}
	h := fnv.New32a()
	h.Write([]byte(m.ID))
	x := h.Sum32() | 1
	for i := range out {
		x ^= x << 13
		x ^= x >> 17
		x ^= x << 5
		out[i] = 0.15 + 0.5*float32(x%1000)/1000
	}
	return out
}

func (u *UI) layoutAudio(gtx C, c *model.Chat, m *model.Message, maxW, metaW, metaH int, cols bubbleColors) D {
	p := u.pal
	voice := m.Media == model.MediaVoice
	playBtn := u.btn("vplay:" + m.ID)
	if playBtn.Clicked(gtx) {
		u.toggleVoice(m)
	}
	t, pos, playing, loading := u.voiceProgress(m)
	w := min(maxW, gtx.Dp(330))
	rowH := gtx.Dp(52)
	picSz := gtx.Dp(52)

	// The sender's picture (a headphones badge for audio files) sits at
	// the outer side of the bubble.
	picX := 0
	x0 := picSz + gtx.Dp(8)
	x1 := w - gtx.Dp(6)
	if m.FromMe {
		picX = w - picSz
		x0, x1 = 0, w-picSz-gtx.Dp(14)
	}
	rateBtn := u.btn("vrate:" + m.ID)
	if rateBtn.Clicked(gtx) {
		u.nextVoiceRate()
	}
	func() {
		t := op.Offset(image.Pt(picX, (rowH-picSz)/2)).Push(gtx.Ops)
		defer t.Pop()
		if voice && u.voice.key == fileKey(m) && u.voice.player != nil {
			// While it plays (or pauses midway) the speed takes the
			// picture's place: 1×, 1.5×, 2×, and round again.
			lg := gtx
			lg.Constraints.Min = image.Point{}
			lbl := record(lg, u.label(14, rateName(u.voiceRate), cols.text, labelOpts{weight: font.SemiBold, maxLines: 1}).Layout)
			pw, ph := max(gtx.Dp(44), lbl.size.X+gtx.Dp(16)), gtx.Dp(26)
			pt := op.Offset(image.Pt((picSz-pw)/2, (picSz-ph)/2)).Push(gtx.Ops)
			pg := gtx
			pg.Constraints = layout.Exact(image.Pt(pw, ph))
			clickable(pg, rateBtn, func(gtx C) D {
				bg := cols.card
				if h := u.hover(gtx, rateBtn); h > 0 {
					bg = mix(bg, cols.text, 0.08*h)
				}
				fillRRect(gtx, image.Rect(0, 0, pw, ph), ph/2, bg)
				lbl.at(gtx, (pw-lbl.size.X)/2, (ph-lbl.size.Y)/2)
				return D{Size: image.Pt(pw, ph)}
			})
			pt.Pop()
			return
		}
		if !voice {
			fillCircle(gtx, image.Pt(picSz/2, picSz/2), picSz/2, rgb(0xfa6533))
			centerIn(gtx, picSz, iconW(icHeadphonesFill, 26, rgb(0xffffff)))
			return
		}
		id, name := m.SenderID, m.Sender
		switch {
		case m.FromMe:
			id, name = u.meID, u.me
		case id == "" && c != nil:
			id, name = c.ID, c.Name
		}
		u.avatar(gtx, id, name, false, dp(gtx, picSz))
		// A mic in the corner, on a ring of the bubble's color.
		bs := gtx.Dp(20)
		bx, by := picSz-bs+gtx.Dp(3), picSz-bs+gtx.Dp(2)
		if !m.FromMe {
			bx = -gtx.Dp(3)
		}
		fillCircle(gtx, image.Pt(bx+bs/2, by+bs/2), bs/2, cols.bg)
		bt := op.Offset(image.Pt(bx+gtx.Dp(1), by+gtx.Dp(1))).Push(gtx.Ops)
		drawIcon(gtx, icMic, 18, p.Green)
		bt.Pop()
	}()

	// Play or pause, or a spinner while it downloads.
	btnSz := gtx.Dp(36)
	func() {
		t := op.Offset(image.Pt(x0, (rowH-btnSz)/2)).Push(gtx.Ops)
		defer t.Pop()
		if loading {
			lg := gtx
			lg.Constraints = layout.Exact(image.Pt(btnSz, btnSz))
			l := material.Loader(u.th)
			l.Color = cols.secondary
			layout.UniformInset(5).Layout(lg, l.Layout)
			return
		}
		ic := icPlayFill
		if playing {
			ic = icPauseFill
		}
		bg := gtx
		bg.Constraints = layout.Exact(image.Pt(btnSz, btnSz))
		clickable(bg, playBtn, func(gtx C) D {
			if h := u.hover(gtx, playBtn); h > 0 {
				fillCircle(gtx, image.Pt(btnSz/2, btnSz/2), btnSz/2, faded(cols.text, 0.08*h))
			}
			return centerIn(gtx, btnSz, iconW(ic, 34, cols.secondary))
		})
	}()

	// The waveform (a plain track for audio files), with a knob.
	tx0 := x0 + btnSz + gtx.Dp(8)
	tw := max(gtx.Dp(40), x1-tx0)
	mid := rowH / 2
	knob := gtx.Dp(13)
	played, rest := cols.text, faded(cols.secondary, 0.55)
	knobCol := rgb(0x53bdeb)
	if m.FromMe || playing || t > 0 {
		knobCol = cols.text
	}
	if voice {
		barW, gap := gtx.Dp(3), gtx.Dp(2)
		n := max(1, (tw+gap)/(barW+gap))
		maxH, minH := gtx.Dp(26), gtx.Dp(3)
		for i, v := range waveform(m, n) {
			x := tx0 + i*(barW+gap)
			h := max(minH, int(v*float32(maxH)))
			col := rest
			if float32(i)+0.5 < t*float32(n) {
				col = played
			}
			fillRRect(gtx, image.Rect(x, mid-h/2, x+barW, mid-h/2+h), barW/2, col)
		}
	} else {
		th := gtx.Dp(4)
		split := tx0 + int(t*float32(tw))
		fillRRect(gtx, image.Rect(tx0, mid-th/2, tx0+tw, mid-th/2+th), th/2, rest)
		fillRRect(gtx, image.Rect(tx0, mid-th/2, split, mid-th/2+th), th/2, played)
	}
	kx := tx0 + int(t*float32(tw))
	fillCircle(gtx, image.Pt(kx, mid), knob/2, knobCol)

	// Pressing the track seeks; dragging follows the pointer.
	tag := u.btn("vseek:" + m.ID)
	for {
		ev, ok := gtx.Event(pointer.Filter{Target: tag, Kinds: pointer.Press | pointer.Drag | pointer.Release | pointer.Cancel})
		if !ok {
			break
		}
		e, ok := ev.(pointer.Event)
		if !ok {
			continue
		}
		at := min(1, max(0, (e.Position.X-float32(tx0))/float32(tw)))
		switch e.Kind {
		case pointer.Press:
			if !e.Buttons.Contain(pointer.ButtonPrimary) {
				continue
			}
			u.voice.drag = true
			u.seekVoice(m, at, false)
			u.voice.drag = u.voice.key == fileKey(m)
		case pointer.Drag:
			if u.voice.drag && u.voice.key == fileKey(m) {
				u.seekVoice(m, at, false)
			}
		case pointer.Release, pointer.Cancel:
			if u.voice.drag && u.voice.key == fileKey(m) {
				u.voice.drag = false
				u.seekVoice(m, at, true)
				u.voice.seekTo = -1
			}
		}
	}
	func() {
		area := image.Rect(tx0-knob/2, mid-gtx.Dp(14), tx0+tw+knob/2, mid+gtx.Dp(14))
		defer clip.Rect(area).Push(gtx.Ops).Pop()
		pointer.CursorPointer.Add(gtx.Ops)
		event.Op(gtx.Ops, tag)
	}()

	// The length, or where it is while playing, beside the timestamp.
	y := rowH
	lg := gtx
	lg.Constraints = layout.Constraints{Max: image.Pt(max(0, w-tx0-metaW-gtx.Dp(8)), metaH)}
	lbl := record(lg, u.label(12, clock(pos), cols.secondary, labelOpts{maxLines: 1}).Layout)
	lbl.at(gtx, tx0, y+(metaH-lbl.size.Y)/2-gtx.Dp(2))
	return D{Size: image.Pt(w, y+metaH)}
}
