package ui

import (
	"context"
	"errors"
	"image"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/chomosuke9/wazzapclients/internal/model"
	"github.com/chomosuke9/wazzapclients/internal/transcribe"
)

// fakeSpeech answers what it's told to.
type fakeSpeech struct {
	mu        sync.Mutex
	said      string
	err       error
	check     error
	need      int64
	downloads int
	canceled  int
	asked     []string // "model/lang"
	hold      chan struct{}
}

func (f *fakeSpeech) Check() error { return f.check }
func (f *fakeSpeech) Status(string) transcribe.Status {
	f.mu.Lock()
	defer f.mu.Unlock()
	return transcribe.Status{Ready: f.need == 0}
}
func (f *fakeSpeech) Need(string) int64 { return f.need }
func (f *fakeSpeech) Download(string)   { f.downloads++ }
func (f *fakeSpeech) Cancel()           { f.canceled++ }
func (f *fakeSpeech) Size() int64       { return 1 }
func (f *fakeSpeech) Clear() error      { return nil }
func (f *fakeSpeech) SetNotify(func())  {}
func (f *fakeSpeech) Transcribe(_ context.Context, m, lang, _ string) (string, error) {
	if f.hold != nil {
		<-f.hold
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.asked = append(f.asked, m+"/"+lang)
	return f.said, f.err
}

// newTranscriptTest opens a demo chat with transcripts on, and returns its
// voice message received.
func newTranscriptTest(t *testing.T, f *fakeSpeech) (*slashTest, *model.Message) {
	t.Helper()
	t.Setenv("WAZZAP_DEMO_AUDIO", "voice.ogg") // its file, never read
	speech = f
	t.Cleanup(func() { speech = nil })
	st := newSlashTest(t, "work")
	for _, m := range st.u.msgs {
		if st.u.takesTranscript(m) {
			return st, m
		}
	}
	t.Fatal("no voice message to transcribe")
	return nil, nil
}

// settle waits for the running transcriptions to end.
func (st *slashTest) settleTranscripts() {
	st.t.Helper()
	for range 200 {
		st.frame()
		if len(st.u.transcripts.jobs) == 0 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	st.t.Fatal("transcribing never ended")
}

func labelsOf(items []menuItem) []string {
	var out []string
	for _, it := range items {
		out = append(out, it.label)
	}
	return out
}

func TestTranscript(t *testing.T) {
	f := &fakeSpeech{said: "Halo semua."}
	st, m := newTranscriptTest(t, f)
	u := st.u
	if got := labelsOf(u.transcriptItems(m)); !slices.Equal(got, []string{"Transcribe"}) {
		t.Errorf("menu before: %v", got)
	}
	f.hold = make(chan struct{})
	u.btn("transcribe:" + m.ID).Click()
	st.frame()
	if u.transcripts.jobs[fileKey(m)] == nil {
		t.Fatal("the Transcribe link does nothing")
	}
	if got := u.transcriptItems(m); len(got) != 0 {
		t.Errorf("menu while transcribing: %v", labelsOf(got))
	}
	close(f.hold)
	st.settleTranscripts()
	if got := u.transcriptOf(m); got == nil || got.Text != "Halo semua." || got.Lang != transcribe.Auto || got.Model != transcribe.DefaultModel {
		t.Fatalf("transcript %+v", got)
	}
	if got := labelsOf(u.transcriptItems(m)); !slices.Equal(got, []string{"Copy transcript"}) {
		t.Errorf("menu after: %v", got)
	}
	st.frame()

	// It's kept: the next window reads it from the backend.
	u2 := New(st.b)
	if got := u2.transcriptOf(m); got == nil || got.Text != "Halo semua." {
		t.Errorf("after a restart: %+v", got)
	}

	// In another language it stays, and can be made again.
	u.setChatTranscriptLang(m.ChatID, "id")
	if got := labelsOf(u.transcriptItems(m)); !slices.Equal(got, []string{"Copy transcript", "Transcribe again"}) {
		t.Errorf("menu in another language: %v", got)
	}
	u.transcribe(m)
	st.settleTranscripts()
	if !slices.Equal(f.asked, []string{"small/auto", "small/id"}) {
		t.Errorf("asked %v", f.asked)
	}
}

// A long transcript shows its first lines, cut at a word, until its arrow
// is clicked.
func TestTranscriptLong(t *testing.T) {
	words := strings.Repeat("sepatu kelinci berwarna ", 30)
	f := &fakeSpeech{said: strings.TrimSpace(words)}
	st, m := newTranscriptTest(t, f)
	u := st.u
	u.transcribe(m)
	st.settleTranscripts()
	st.frame()
	gtx := layout.Context{Ops: new(op.Ops), Constraints: layout.Exact(image.Pt(400, 400)), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}}
	for _, w := range []int{180, 330} {
		cut, long := u.cutTranscript(gtx, quoted(f.said), 15.7, w, 32)
		got := strings.TrimSuffix(cut, "…")
		if !long || got == cut || !strings.HasPrefix(quoted(f.said), got) {
			t.Fatalf("%d px: cut to %q, %v", w, cut, long)
		}
		if last := got[strings.LastIndexByte(got, ' ')+1:]; !slices.Contains([]string{"sepatu", "kelinci", "berwarna"}, last) {
			t.Errorf("%d px: cut inside a word: %q", w, last)
		}
		// It's drawn in as many lines as it was cut to.
		g := gtx
		g.Constraints = layout.Constraints{Max: image.Pt(w, 1<<20)}
		if h := u.layoutRich(g, cut, 15.7, u.pal.Text, u.pal.Text, richOpts{}).Size.Y; h != transcriptLines*22 {
			t.Errorf("%d px: %d px tall, want %d lines", w, h, transcriptLines)
		}
	}
	if cut, long := u.cutTranscript(gtx, quoted("Halo"), 15.7, 330, 32); long || cut != quoted("Halo") {
		t.Errorf("short one cut to %q", cut)
	}
	for _, want := range []int{1, 0} {
		u.btn("transcript:more:" + m.ID).Click()
		st.frame()
		st.frame()
		if got := u.conv.expanded[m.ID]; got != want {
			t.Errorf("expanded %d, want %d", got, want)
		}
	}
}

func TestTranscriptUnavailable(t *testing.T) {
	f := &fakeSpeech{}
	st, m := newTranscriptTest(t, f)
	u := st.u
	u.transcribe(m)
	st.settleTranscripts()
	if got := u.transcriptOf(m); got == nil || got.Text != "" {
		t.Fatalf("transcript %+v", got)
	}
	st.frame()
	u.btn("transcript:why:" + m.ID).Click()
	st.frame()
	if u.dialog.kind != dialogTranscript {
		t.Fatal("Learn more opens nothing")
	}
	st.frame()
	u.btn("transcript:lang").Click()
	st.frame()
	if !u.info.open || u.info.sub != infoTranscripts || u.dialog.isOpen() {
		t.Errorf("Change language: info %v %q, dialog open %v", u.info.open, u.info.sub, u.dialog.isOpen())
	}
	st.frame()
	u.btn("chatlang:id").Click()
	st.frame()
	if u.transcriptLang(m.ChatID) != "id" || u.transcriptLang("rina") != transcribe.Auto {
		t.Errorf("languages: %q and %q", u.transcriptLang(m.ChatID), u.transcriptLang("rina"))
	}
	// Another language may hear something.
	if u.transcriptOf(m) != nil {
		t.Error("still unavailable in another language")
	}
}

func TestTranscriptFails(t *testing.T) {
	f := &fakeSpeech{err: errors.New("no engine")}
	st, m := newTranscriptTest(t, f)
	st.u.transcribe(m)
	st.settleTranscripts()
	if st.u.transcriptOf(m) != nil || st.b.Transcript(m.ChatID, m.ID) != nil {
		t.Error("a failure was kept as a transcript")
	}
	f.check = transcribe.ErrNoFFmpeg
	st.u.transcribe(m)
	if len(st.u.transcripts.jobs) != 0 || !strings.Contains(st.u.dialog.title, "ffmpeg") {
		t.Errorf("without ffmpeg: dialog %q", st.u.dialog.title)
	}
}

// The first one asks before it downloads the engine and its model.
func TestTranscriptAsksToDownload(t *testing.T) {
	f := &fakeSpeech{said: "Halo", need: 190 << 20}
	st, m := newTranscriptTest(t, f)
	u := st.u
	u.transcribe(m)
	if len(u.transcripts.jobs) != 0 || u.dialog.kind != dialogConfirm || !strings.Contains(u.dialog.body, "190.0 MB") {
		t.Fatalf("no question: %d jobs, dialog %q", len(u.transcripts.jobs), u.dialog.title)
	}
	u.dialog.buttons[0].run()
	u.dialog.buttons[0].run() // twice makes one
	st.settleTranscripts()
	if len(f.asked) != 1 || u.transcriptOf(m) == nil {
		t.Errorf("asked %v", f.asked)
	}
}

// The voice message's own download failing ends it.
func TestTranscriptWaitsForFile(t *testing.T) {
	f := &fakeSpeech{said: "Oke"}
	st, m := newTranscriptTest(t, f)
	t.Setenv("WAZZAP_DEMO_AUDIO", "")
	st.u.transcribe(m)
	st.settleTranscripts()
	if len(f.asked) != 0 {
		t.Errorf("transcribed without a file: %v", f.asked)
	}
}

func TestTranscriptSettings(t *testing.T) {
	f := &fakeSpeech{need: 190 << 20}
	speech = f
	t.Cleanup(func() { speech = nil })
	st := newSlashTest(t, "work")
	u := st.u
	if !u.transcriptsOn() {
		t.Fatal("off by default")
	}
	titles := func(secs []settingsSection) (out []string) {
		for _, s := range secs {
			for _, r := range s.rows {
				out = append(out, r.title)
			}
		}
		return out
	}
	if slices.Contains(titles(u.extrasSettings()), "Transcripts") || !slices.Contains(titles(u.categoryPage(settingChats)), "Transcripts") {
		t.Error("not on the Chats page alone")
	}
	u.ShowPage("transcripts")
	st.frame()
	// Choosing a model downloads nothing by itself.
	u.btn("settings:transcriptmodel:turbo").Click()
	st.frame()
	if u.transcripts.model != "turbo" || f.downloads != 0 {
		t.Errorf("model %q, %d downloads", u.transcripts.model, f.downloads)
	}
	u.ShowPage("transcriptlang")
	st.frame()
	u.btn("settings:transcriptlang:id").Click()
	st.frame()
	if u.transcripts.lang != "id" || New(st.b).transcripts.lang != "id" {
		t.Errorf("language %q", u.transcripts.lang)
	}
	u.settingsBack()
	if u.settings.sub != "transcripts" {
		t.Errorf("back goes to %q", u.settings.sub)
	}
	// Off hides them and stops the download.
	u.transcriptSettings()[0].rows[0].run()
	if u.transcriptsOn() || f.canceled != 1 || New(st.b).transcripts.on || len(u.transcriptSettings()) != 1 {
		t.Errorf("off: on %v, canceled %d, settings %v", u.transcriptsOn(), f.canceled, titles(u.transcriptSettings()))
	}
	if u.settingsBack(); u.settings.sub != "" || u.settings.detail != settingChats+1 {
		t.Errorf("back goes to %d %q", u.settings.detail, u.settings.sub)
	}
	// There's no engine for macOS.
	speech = nil
	if u.transcriptRows() != nil || u.transcriptsOn() {
		t.Error("offered without an engine")
	}
}
