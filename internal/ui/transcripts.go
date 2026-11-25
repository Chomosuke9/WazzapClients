package ui

import (
	"context"
	"errors"
	"image"
	"image/color"
	"math"
	"strings"
	"sync"

	"gioui.org/font"
	"gioui.org/io/semantic"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/text"
	"gioui.org/unit"
	"gioui.org/widget/material"
	"golang.org/x/image/math/fixed"

	"github.com/chomosuke9/wazzapclients/internal/model"
	"github.com/chomosuke9/wazzapclients/internal/transcribe"
)

// Transcripts: what others' voice messages say, as text under the player.
// WhatsApp makes them on the phone, and syncs none. Here they're made on
// this computer (internal/transcribe) when asked for. What makes them
// downloads the first time, once the user agrees. Settings > Chats >
// Transcripts turns them off.

// Preferences: the first three belong to the app (appPrefs), and a chat's
// own language to its account.
const (
	prefTranscripts     = "transcripts"      // on unless "off"
	prefTranscriptLang  = "transcript_lang"  // "" detects it
	prefTranscriptModel = "transcript_model" // "" is transcribe.DefaultModel

	prefChatTranscriptLang = "transcript_lang:" // and the chat's ID; "" is the language of every chat
)

// speechEngine is what transcribes: *transcribe.Engine, but for tests and
// screenshots.
type speechEngine interface {
	Check() error
	Status(model string) transcribe.Status
	Need(model string) int64
	Download(model string)
	Cancel()
	Transcribe(ctx context.Context, model, lang, audio string) (string, error)
	Size() int64
	Clear() error
	SetNotify(func())
}

// speech is nil where there's no engine (macOS), and in tests and
// screenshots. Run sets it (Options.TranscriptDir).
var speech speechEngine

// transcripts is the feature's state.
type transcripts struct {
	on          bool
	lang, model string
	langs       map[string]string            // chats' own languages, as read from the prefs
	got         map[string]*model.Transcript // by fileKey, as the backend said; nil for none
	jobs        map[string]*transcriptJob    // by fileKey
	inbox       *transcriptInbox
	status      transcribe.Status // as the settings page showed it
}

// transcriptJob is a voice message being transcribed.
type transcriptJob struct {
	msg         *model.Message
	lang, model string
	waiting     bool // for the voice message to download
}

// transcriptInbox takes what the goroutines that transcribe found.
type transcriptInbox struct {
	mu   sync.Mutex
	done []transcriptDone
}

type transcriptDone struct {
	key string
	job *transcriptJob
	t   model.Transcript
	err error
}

// loadTranscripts reads the preferences.
func (u *UI) loadTranscripts() {
	t, b := &u.transcripts, u.backend
	*t = transcripts{inbox: &transcriptInbox{}, langs: map[string]string{},
		got: map[string]*model.Transcript{}, jobs: map[string]*transcriptJob{}}
	if b == nil {
		return
	}
	t.on = prefOn(b, prefTranscripts)
	t.lang, t.model = b.Pref(prefTranscriptLang), b.Pref(prefTranscriptModel)
	if t.lang == "" {
		t.lang = transcribe.Auto
	}
	if t.model == "" {
		t.model = transcribe.DefaultModel
	}
}

// chatTranscriptLang is a chat's own language, or "" when it has none.
func (u *UI) chatTranscriptLang(chatID string) string {
	l, ok := u.transcripts.langs[chatID]
	if !ok {
		l = u.backend.Pref(prefChatTranscriptLang + chatID)
		u.transcripts.langs[chatID] = l
	}
	return l
}

// transcriptLang is the language a chat's voice messages are transcribed
// in.
func (u *UI) transcriptLang(chatID string) string {
	if l := u.chatTranscriptLang(chatID); l != "" {
		return l
	}
	return u.transcripts.lang
}

func (u *UI) setChatTranscriptLang(chatID, lang string) {
	u.transcripts.langs[chatID] = lang
	u.backend.SetPref(prefChatTranscriptLang+chatID, lang)
}

// transcriptLangName names a language in itself.
func transcriptLangName(code string) string {
	if l := transcribe.LanguageByCode(code); l != nil {
		return l.Name
	}
	return "Detect language"
}

// transcriptsOn reports whether transcripts can be made and show.
func (u *UI) transcriptsOn() bool { return u.transcripts.on && speech != nil }

// takesTranscript reports whether m is a voice message to transcribe. As
// on WhatsApp, your own aren't.
func (u *UI) takesTranscript(m *model.Message) bool {
	return u.transcriptsOn() && m.Media == model.MediaVoice && !m.FromMe &&
		m.Kind != model.KindDeleted && m.Kind != model.KindViewOnce
}

// transcriptOf is m's transcript, or nil. One without words counts until
// the language or the model changes: then it can be tried again.
func (u *UI) transcriptOf(m *model.Message) *model.Transcript {
	k := fileKey(m)
	t, ok := u.transcripts.got[k]
	if !ok {
		t = u.backend.Transcript(m.ChatID, m.ID)
		u.transcripts.got[k] = t
	}
	if t != nil && t.Text == "" && u.transcriptOutdated(m, t) {
		return nil
	}
	return t
}

// transcriptOutdated reports whether transcribing m again could say
// something else than t.
func (u *UI) transcriptOutdated(m *model.Message, t *model.Transcript) bool {
	return t.Lang != u.transcriptLang(m.ChatID) || t.Model != u.transcripts.model
}

// transcribe makes m's transcript, asking first when that takes a
// download.
func (u *UI) transcribe(m *model.Message) {
	k := fileKey(m)
	if !u.takesTranscript(m) || u.transcripts.jobs[k] != nil {
		return
	}
	if err := speech.Check(); err != nil {
		u.transcriptFailed(err)
		return
	}
	model := u.transcripts.model
	start := func() {
		if u.transcripts.jobs[k] != nil {
			return
		}
		j := &transcriptJob{msg: m, lang: u.transcriptLang(m.ChatID), model: model}
		u.transcripts.jobs[k] = j
		u.startTranscript(k, j)
	}
	need := speech.Need(model)
	if need == 0 || speech.Status(model).Downloading {
		start()
		return
	}
	u.confirm("Download speech recognition?", "Transcripts are made on this computer, by whisper.cpp with OpenAI's Whisper model, and nothing you hear is sent anywhere. "+
		"It downloads once, from GitHub and Hugging Face: "+sizeLabel(need)+". "+
		"You can choose a smaller or a more accurate model in Settings > Chats > Transcripts.",
		dialogButton{label: "Download", primary: true, run: start})
}

// startTranscript transcribes the voice message, or waits for its
// download (transcriptDownloaded goes on).
func (u *UI) startTranscript(k string, j *transcriptJob) {
	path := u.backend.MediaFile(j.msg)
	if j.waiting = path == ""; j.waiting {
		return
	}
	eng, b, in, notify := speech, u.backend, u.transcripts.inbox, u.images.invalidate
	chatID, id := j.msg.ChatID, j.msg.ID
	go func() {
		said, err := eng.Transcribe(context.Background(), j.model, j.lang, path)
		t := model.Transcript{Lang: j.lang, Model: j.model, Text: said}
		if err == nil {
			b.SetTranscript(chatID, id, t) // kept even if the window closed meanwhile
		}
		in.mu.Lock()
		in.done = append(in.done, transcriptDone{k, j, t, err})
		in.mu.Unlock()
		if notify != nil {
			notify()
		}
	}()
}

// transcriptDownloaded goes on with the voice message that waited for its
// download.
func (u *UI) transcriptDownloaded(e model.MediaEvent) {
	k := e.ChatID + "/" + e.MsgID
	j := u.transcripts.jobs[k]
	if j == nil || !j.waiting {
		return
	}
	if e.Failed {
		delete(u.transcripts.jobs, k)
		return
	}
	u.startTranscript(k, j)
}

// takeTranscripts shows the transcripts that were made.
func (u *UI) takeTranscripts() {
	t := &u.transcripts
	if t.inbox == nil {
		return
	}
	t.inbox.mu.Lock()
	done := t.inbox.done
	t.inbox.done = nil
	t.inbox.mu.Unlock()
	for _, d := range done {
		if t.jobs[d.key] != d.job {
			continue
		}
		delete(t.jobs, d.key)
		if d.err != nil {
			u.transcriptFailed(d.err)
			continue
		}
		t.got[d.key] = &d.t
	}
}

func (u *UI) transcriptFailed(err error) {
	switch {
	case errors.Is(err, transcribe.ErrCanceled):
	case errors.Is(err, transcribe.ErrNoFFmpeg):
		u.inform("ffmpeg isn't installed", "Transcribing voice messages needs ffmpeg, as recording them does. "+
			"Install it, make sure it's on PATH, and try again.")
	default:
		u.toast("Couldn't transcribe the voice message: " + err.Error())
	}
}

// layoutTranscript draws what goes under a voice message's player, at most
// w wide: the Transcribe link, the transcript, or why there's none. The
// timestamp (metaW by metaH) goes at the end of its last row, or under it;
// the height returned includes that.
func (u *UI) layoutTranscript(gtx C, m *model.Message, w, metaW, metaH int, cols bubbleColors) D {
	p := u.pal
	// row is a line beside the timestamp, or above it in a narrow bubble.
	row := func(w0 layout.Widget) D {
		rg := gtx
		rg.Constraints = layout.Constraints{Max: image.Pt(w, 1<<20)}
		r := record(rg, w0)
		if r.size.X > w-metaW-gtx.Dp(10) {
			r.at(gtx, 0, 0)
			return D{Size: image.Pt(w, r.size.Y+metaH)}
		}
		h := max(metaH, r.size.Y+gtx.Dp(2))
		r.at(gtx, 0, (h-r.size.Y)/2)
		return D{Size: image.Pt(w, h)}
	}
	size := unit.Sp(14.5)
	if w < gtx.Dp(290) {
		size = 13 // "Transcript unavailable. Learn more" in a narrow chat
	}
	link := func(key, s string) layout.Widget {
		return func(gtx C) D {
			return clickable(gtx, u.btn(key), u.label(size, s, p.Green, labelOpts{weight: font.Medium, maxLines: 1}).Layout)
		}
	}
	if j := u.transcripts.jobs[fileKey(m)]; j != nil {
		defer u.unhidden()()
		s := "Transcribing…"
		if st := speech.Status(j.model); st.Downloading {
			s = "Downloading the speech model… " + percent(st)
		}
		return row(func(gtx C) D {
			return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(func(gtx C) D {
					gtx.Constraints = layout.Exact(image.Pt(gtx.Dp(14), gtx.Dp(14)))
					l := material.Loader(u.th)
					l.Color = cols.secondary
					return l.Layout(gtx)
				}),
				layout.Rigid(layout.Spacer{Width: 8}.Layout),
				layout.Rigid(u.label(size, s, cols.secondary, labelOpts{maxLines: 1}).Layout),
			)
		})
	}
	if u.btn("transcribe:" + m.ID).Clicked(gtx) {
		u.transcribe(m)
	}
	t := u.transcriptOf(m)
	switch {
	case t == nil:
		defer u.unhidden()()
		return row(link("transcribe:"+m.ID, "Transcribe"))
	case t.Text == "":
		defer u.unhidden()()
		if u.btn("transcript:why:" + m.ID).Clicked(gtx) {
			u.openTranscriptWhy(m.ChatID)
		}
		return row(func(gtx C) D {
			return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(iconW(icInfo, 17, cols.secondary)),
				layout.Rigid(layout.Spacer{Width: 6}.Layout),
				layout.Rigid(u.label(size, "Transcript unavailable.", cols.secondary, labelOpts{maxLines: 1}).Layout),
				layout.Rigid(layout.Spacer{Width: 5}.Layout),
				layout.Rigid(link("transcript:why:"+m.ID, "Learn more")),
			)
		})
	}
	const textSize = unit.Sp(15.7)
	tg := gtx
	tg.Constraints = layout.Constraints{Max: image.Pt(w, 1<<20)}
	var o richOpts
	if !u.conv.selecting {
		o.sel, o.links = m.ID, m.ID
	}
	// A long one shows its first lines and an arrow at their end, which
	// shows the rest and, pointing up, hides it again.
	arrow := u.btn("transcript:more:" + m.ID)
	if arrow.Clicked(gtx) {
		if u.conv.expanded == nil {
			u.conv.expanded = make(map[string]int)
		}
		u.conv.expanded[m.ID] ^= 1
	}
	said := quoted(t.Text)
	arrowW, open := gtx.Dp(32), u.conv.expanded[m.ID] != 0
	cut, long := u.cutTranscript(gtx, said, textSize, w, arrowW)
	switch {
	case long && open:
		o.suffix = u.metaSpacer(gtx, textSize, arrowW)
	case long:
		said = cut
	}
	body := record(tg, func(gtx C) D { return u.layoutRich(gtx, said, textSize, cols.secondary, cols.secondary, o) })
	top := gtx.Dp(2)
	body.at(gtx, 0, top)
	if long {
		defer u.unhidden()()
		// Level with the last line, above the timestamp.
		h := gtx.Dp(26)
		t := op.Offset(image.Pt(w-arrowW, top+body.size.Y-gtx.Sp(22)/2-h/2)).Push(gtx.Ops)
		ag := gtx
		ag.Constraints = layout.Exact(image.Pt(arrowW, h))
		clickable(ag, arrow, func(gtx C) D {
			label := "Show the whole transcript"
			if open {
				label = "Show less"
			}
			semantic.LabelOp(label).Add(gtx.Ops)
			sz := gtx.Dp(24)
			defer op.Offset(image.Pt(arrowW-sz-gtx.Dp(2), (h-sz)/2)).Push(gtx.Ops).Pop()
			angle := float32(0)
			if open {
				angle = math.Pi
			}
			col := cols.secondary
			if u.hover(gtx, arrow) >= 0.5 { // icons are cached per color: no fade
				col = cols.text
			}
			rotated(gtx, sz, angle, iconW(icChevron, 24, col))
			return D{Size: image.Pt(arrowW, h)}
		})
		t.Pop()
	}
	return D{Size: image.Pt(w, top+body.size.Y+metaH)}
}

// transcriptLines is how much of a long transcript shows at first, as on
// phones.
const transcriptLines = 3

type transcriptCutKey struct {
	text         string
	px, w, arrow int
}

type transcriptCutVal struct {
	text string
	long bool
}

// transcriptCuts caches cutTranscript, which shapes the text.
var transcriptCuts = memo[transcriptCutKey, transcriptCutVal]{limit: 100}

// cutTranscript returns what fits of s in transcriptLines lines w wide,
// with room for an arrow at the end of the last, and whether there's more.
func (u *UI) cutTranscript(gtx C, s string, size unit.Sp, w, arrow int) (string, bool) {
	v := transcriptCuts.get(transcriptCutKey{s, gtx.Sp(size), w, arrow}, func() transcriptCutVal {
		// The room is spaces after the ellipsis.
		room := strings.Repeat("\u00a0", int(float32(arrow)/u.nbspWidth(gtx, size))+1)
		u.th.Shaper.LayoutString(text.Parameters{
			Font:       font.Font{Typeface: typeface},
			PxPerEm:    fixed.I(gtx.Sp(size)),
			MaxLines:   transcriptLines,
			Truncator:  "…" + room,
			MaxWidth:   w,
			Locale:     gtx.Locale,
			WrapPolicy: text.WrapWords, // never half a word
		}, displayText(s))
		hidden := 0
		for g, ok := u.th.Shaper.NextGlyph(); ok; g, ok = u.th.Shaper.NextGlyph() {
			if g.Flags&text.FlagTruncator != 0 && g.Flags&text.FlagClusterBreak != 0 {
				hidden = int(g.Runes)
			}
		}
		rs := []rune(displayText(s))
		if hidden <= 0 || hidden >= len(rs) {
			return transcriptCutVal{text: s}
		}
		return transcriptCutVal{strings.TrimRight(string(rs[:len(rs)-hidden]), " ,;:-") + "…", true}
	})
	return v.text, v.long
}

// quoted puts a transcript in quotes, as WhatsApp shows it.
func quoted(s string) string { return "\"" + s + "\"" }

func percent(st transcribe.Status) string {
	if st.Total <= 0 {
		return ""
	}
	return itoa(int(100*st.Done/st.Total)) + "%"
}

// transcriptItems are what a voice message's menu does with its
// transcript.
func (u *UI) transcriptItems(m *model.Message) []menuItem {
	if !u.takesTranscript(m) || u.transcripts.jobs[fileKey(m)] != nil {
		return nil
	}
	t := u.transcriptOf(m)
	if t == nil {
		return []menuItem{{key: "transcribe", ic: icDocument, label: "Transcribe", run: func() { u.transcribe(m) }}}
	}
	var items []menuItem
	if t.Text != "" {
		said := t.Text
		if sel := u.textSel.selected(m.ID); sel != "" {
			said = sel
		}
		items = append(items, menuItem{key: "copytranscript", ic: icCopy, label: "Copy transcript", run: func() { u.copyText(said) }})
		if u.transcriptOutdated(m, t) {
			items = append(items, menuItem{key: "transcribe", ic: icDocument, label: "Transcribe again", run: func() { u.transcribe(m) }})
		}
	}
	return items
}

// openTranscriptWhy says why a voice message of a chat has no transcript.
func (u *UI) openTranscriptWhy(chatID string) {
	u.dialog = dialogState{kind: dialogTranscript, title: "Why transcripts may be unavailable", transcriptOf: chatID}
}

func (u *UI) transcriptWhyPanel(gtx C) D {
	d, p := &u.dialog, u.pal
	if u.btn("dialog:1").Clicked(gtx) && d.isOpen() {
		u.closeDialog()
	}
	if u.btn("transcript:lang").Clicked(gtx) && d.isOpen() {
		u.closeDialog()
		u.openInfo(d.transcriptOf)
		u.openInfoSub(infoTranscripts)
	}
	lang := "The language is detected from the voice message, which can go wrong in a short one."
	if l := transcribe.LanguageByCode(u.transcriptLang(d.transcriptOf)); l != nil {
		lang = "Your transcript language is set to " + l.English + "."
	}
	w := min(gtx.Dp(440), gtx.Constraints.Max.X-gtx.Dp(32))
	gtx.Constraints = layout.Constraints{Min: image.Pt(w, 0), Max: image.Pt(w, gtx.Constraints.Max.Y)}
	para := func(s string) layout.Widget {
		return func(gtx C) D {
			l := u.label(15, s, p.Text)
			l.MaxLines = 0
			return l.Layout(gtx)
		}
	}
	reason := func(ic layout.Widget, w0 layout.Widget) layout.FlexChild {
		return layout.Rigid(func(gtx C) D {
			return layout.Inset{Top: 22}.Layout(gtx, func(gtx C) D {
				return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(ic),
					layout.Rigid(layout.Spacer{Width: 22}.Layout),
					layout.Flexed(1, w0))
			})
		})
	}
	return layout.UniformInset(28).Layout(gtx, func(gtx C) D {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx C) D {
				gtx.Constraints.Min.X = gtx.Constraints.Max.X
				return layout.N.Layout(gtx, func(gtx C) D {
					sz := gtx.Dp(64)
					d := drawIcon(gtx, icDocumentFill, 64, p.Green)
					// The badge in its corner, on a ring of the dialog's color.
					r := gtx.Dp(13)
					at := image.Pt(r/2, sz-r)
					fillCircle(gtx, at, r+gtx.Dp(2), p.Dialog)
					defer op.Offset(at.Sub(image.Pt(r, r))).Push(gtx.Ops).Pop()
					drawIcon(gtx, icInfo, 26, p.Text)
					return d
				})
			}),
			layout.Rigid(layout.Spacer{Height: 14}.Layout),
			layout.Rigid(func(gtx C) D {
				gtx.Constraints.Min.X = gtx.Constraints.Max.X
				l := u.label(20, d.title, p.Text, labelOpts{weight: font.Medium, align: text.Middle})
				l.MaxLines = 0
				return l.Layout(gtx)
			}),
			reason(iconW(icPublic, 24, p.TextSecondary), func(gtx C) D {
				return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
					layout.Rigid(para(lang)),
					layout.Rigid(layout.Spacer{Height: 4}.Layout),
					layout.Rigid(func(gtx C) D {
						gtx.Constraints.Min.X = 0
						return clickable(gtx, u.btn("transcript:lang"),
							u.label(15, "Change language for this chat", p.Green, labelOpts{weight: font.Medium, maxLines: 1}).Layout)
					}))
			}),
			reason(iconW(icLiveHelp, 24, p.TextSecondary), para("No words were detected in this voice message.")),
			layout.Rigid(layout.Spacer{Height: 22}.Layout),
			layout.Rigid(func(gtx C) D {
				gtx.Constraints.Min.X = gtx.Constraints.Max.X
				l := u.label(14, "You can always turn off transcripts in Settings > Chats.", p.TextSecondary, labelOpts{align: text.Middle})
				l.MaxLines = 0
				return l.Layout(gtx)
			}),
			layout.Rigid(layout.Spacer{Height: 22}.Layout),
			layout.Rigid(func(gtx C) D {
				return layout.E.Layout(gtx, func(gtx C) D {
					return u.dialogButton(gtx, "dialog:1", dialogButton{label: "OK", primary: true})
				})
			}),
		)
	})
}

// infoTranscriptRow is the info panel's row of a chat's transcript
// language; nothing while transcripts are off.
func (u *UI) infoTranscriptRow(chatID string) layout.Widget {
	if !u.transcriptsOn() {
		return func(gtx C) D { return D{} }
	}
	return u.infoItem("transcripts", listItem{ic: icDocument, title: "Transcripts", sub: transcriptLangName(u.transcriptLang(chatID))})
}

// transcriptLangRows is the info panel's Chat transcript language page.
func (u *UI) transcriptLangRows(gtx C, c *model.Chat) []layout.Widget {
	id := c.ID
	row := func(code, title, sub string) layout.Widget {
		b := u.btn("chatlang:" + code)
		if b.Clicked(gtx) {
			u.setChatTranscriptLang(id, code)
		}
		return func(gtx C) D {
			return u.layoutListItem(gtx, b, listItem{title: title, sub: sub,
				glyph: func(gtx C, _ color.NRGBA) D { return u.radio(gtx, code == u.chatTranscriptLang(id)) }}, infoGeom)
		}
	}
	rows := []layout.Widget{
		u.infoText("This language is for transcripts in this chat only. To change the language for all transcripts, "+
			"go to Transcript language in Settings > Chats > Transcripts.", u.pal.TextSecondary, 14, 10, 14, infoPadX),
		row("", "Same as all chats", transcriptLangName(u.transcripts.lang)),
	}
	if u.transcripts.lang != transcribe.Auto {
		rows = append(rows, row(transcribe.Auto, transcriptLangName(transcribe.Auto), "From each voice message"))
	}
	for _, l := range transcribe.Languages {
		rows = append(rows, row(l.Code, l.Name, l.English))
	}
	return rows
}

// Settings.

// transcriptRows is the Chats page's row of transcripts; none where there's
// no engine.
func (u *UI) transcriptRows() []settingRow {
	if speech == nil {
		return nil
	}
	sub := "Off"
	if u.transcripts.on {
		sub = transcriptLangName(u.transcripts.lang)
	}
	return []settingRow{{key: "transcriptsettings", ic: icDocument, title: "Transcripts", sub: sub,
		trailing: icChevronRight, run: func() { u.openSettingsSub("transcripts") }}}
}

// transcriptModels describes transcribe.Models.
var transcriptModels = map[string]struct{ title, sub string }{
	"base":  {"Fast", "The smallest, and the one that makes the most mistakes"},
	"small": {"Balanced", "Fewer mistakes, and slower"},
	"turbo": {"Accurate", "The fewest mistakes, and the slowest"},
}

// transcriptSettings is the Transcripts page of the Chats settings.
func (u *UI) transcriptSettings() []settingsSection {
	if speech == nil {
		return nil
	}
	t, b := &u.transcripts, u.backend
	on := t.on
	toggle := settingsSection{rows: []settingRow{{key: prefTranscripts, kind: setToggle, on: on, title: "Voice message transcripts",
		sub: "Read what a voice message you received says: click Transcribe under it", run: func() {
			t.on = !on
			setPref(b, prefTranscripts, !on)
			if on {
				speech.Cancel()
			}
			u.settings.stale = true
		}}}, note: "Transcripts are made on this computer, and nothing is sent anywhere."}
	if !on {
		return []settingsSection{toggle}
	}
	t.status = speech.Status(t.model)
	models := settingsSection{title: "Accuracy", note: "It uses hundreds of MB of memory while it transcribes (Accurate the most), " +
		"and none otherwise. Changing it keeps the transcripts already made."}
	for _, m := range transcribe.Models {
		desc := transcriptModels[m.ID]
		sub := desc.sub
		if n := speech.Need(m.ID); n > 0 {
			sub += " · " + sizeLabel(n) + " to download"
		}
		models.rows = append(models.rows, settingRow{key: "transcriptmodel:" + m.ID, kind: setRadio, title: desc.title, sub: sub,
			on: m.ID == t.model, run: func() {
				t.model = m.ID
				b.SetPref(prefTranscriptModel, m.ID)
				u.settings.stale = true
			}})
	}
	out := []settingsSection{
		toggle,
		{title: "Language", rows: []settingRow{{key: "transcriptlang", ic: icPublic, title: "Transcript language", sub: transcriptLangName(t.lang),
			trailing: icChevronRight, run: func() { u.openSettingsSub("transcriptlang") }}},
			note: "A chat can have a language of its own, in its info."},
		models,
	}
	files := settingsSection{title: "Download"}
	row := settingRow{key: "transcriptdownload"}
	switch st := t.status; {
	case st.Downloading:
		row.title, row.sub = "Downloading… "+percent(st), "Click to stop"
		row.run = func() { speech.Cancel(); u.settings.stale = true }
	case st.Ready:
		row.title, row.sub, row.danger = "Delete downloaded files", sizeLabel(speech.Size())+" on this computer. Transcripts stay", true
		row.run = func() {
			eng, notify := speech, u.images.invalidate
			go func() { // it waits for a transcription to end
				_ = eng.Clear()
				if notify != nil {
					notify()
				}
			}()
		}
	default:
		row.title, row.sub = "Download", sizeLabel(speech.Need(t.model))
		if st.Err != nil {
			row.sub = "Couldn't download: " + st.Err.Error() + ". Click to try again"
		}
		m := t.model
		row.run = func() { speech.Download(m); u.settings.stale = true }
	}
	files.rows = []settingRow{row}
	return append(out, files)
}

// transcriptLangSettings is the Transcript language page.
func (u *UI) transcriptLangSettings() []settingsSection {
	t, b := &u.transcripts, u.backend
	sec := settingsSection{note: "Choosing the language makes short voice messages come out right more often."}
	add := func(code, title, sub string) {
		sec.rows = append(sec.rows, settingRow{key: "transcriptlang:" + code, kind: setRadio, title: title, sub: sub,
			on: t.lang == code, run: func() {
				t.lang = code
				b.SetPref(prefTranscriptLang, code)
			}})
	}
	add(transcribe.Auto, transcriptLangName(transcribe.Auto), "From each voice message")
	for _, l := range transcribe.Languages {
		add(l.Code, l.Name, l.English)
	}
	return []settingsSection{sec}
}

// transcriptPageStale reports whether the open settings page shows a
// download that has gone on since.
func (u *UI) transcriptPageStale() bool {
	if u.settings.sub != "transcripts" || speech == nil {
		return false
	}
	st, was := speech.Status(u.transcripts.model), u.transcripts.status
	return st.Ready != was.Ready || st.Downloading != was.Downloading || st.Done != was.Done || (st.Err == nil) != (was.Err == nil)
}

// demoSpeech is the engine of screenshots, which has downloaded and
// transcribes nothing.
type demoSpeech struct{ st transcribe.Status }

func (demoSpeech) Check() error                      { return nil }
func (d demoSpeech) Status(string) transcribe.Status { return d.st }
func (demoSpeech) Need(model string) int64 {
	if model == transcribe.DefaultModel {
		return 0
	}
	return map[string]int64{"base": 59707625}[model]
}
func (demoSpeech) Download(string) {}
func (demoSpeech) Cancel()         {}
func (demoSpeech) Transcribe(context.Context, string, string, string) (string, error) {
	return "", transcribe.ErrCanceled
}
func (demoSpeech) Size() int64      { return 190 << 20 }
func (demoSpeech) Clear() error     { return nil }
func (demoSpeech) SetNotify(func()) {}

// demoTranscripts turns transcripts on for screenshots, and shows the open
// chat's last voice message received in each of their states.
func (u *UI) demoTranscripts() {
	speech = demoSpeech{st: transcribe.Status{Ready: true}}
	u.transcripts.on = true
	var voice *model.Message
	for _, m := range u.msgs {
		if u.takesTranscript(m) {
			voice = m
		}
	}
	if voice == nil {
		return
	}
	for i, t := range []*model.Transcript{nil, {}, {}, {Text: "This is a test."},
		{Text: "Hi all, I've looked at the failed upload. The payload is fine, it's the signature that's computed before the body is compressed, so I'll move that and send a fix before the demo."}} {
		m := *voice
		m.ID += "-" + itoa(i)
		k := fileKey(&m)
		u.transcripts.got[k] = nil
		switch {
		case t != nil && i == 1:
			u.transcripts.jobs[k] = &transcriptJob{msg: &m, lang: transcribe.Auto, model: u.transcripts.model}
		case t != nil:
			t.Lang, t.Model = u.transcriptLang(m.ChatID), u.transcripts.model
			u.transcripts.got[k] = t
		}
		u.msgs = append(u.msgs, &m)
	}
	u.msgsVer++
	u.scrollMessages(layout.Position{})
}
