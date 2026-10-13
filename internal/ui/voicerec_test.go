package ui

import (
	"testing"
	"time"

	"github.com/chomosuke9/wazzapclients/internal/model"
	recorder "github.com/chomosuke9/wazzapclients/internal/record"
)

// TestRecordVoice records a (made-up) voice message with the mic, pauses
// it and sends it.
func TestRecordVoice(t *testing.T) {
	recorder.Demo = true
	defer func() { recorder.Demo = false }()
	st := newSlashTest(t, "rina")
	u := st.u
	u.conv.composer.SetText("hi")
	if u.micClicked() {
		t.Fatal("the button is send while there is text")
	}
	u.conv.composer.SetText("")
	if !u.micClicked() || !u.recording() {
		t.Fatal("the mic didn't start recording")
	}
	st.frame()
	time.Sleep(250 * time.Millisecond)
	u.pauseRecording()
	if d := u.rec.draft; d == nil || d.Media != model.MediaVoice || len(d.Waveform) != recorder.WaveformLen || u.draftPath(d) == "" {
		t.Fatalf("no draft to play back: %+v", d)
	}
	st.frame()
	u.sendRecording()
	if u.recording() {
		t.Fatal("still recording after send")
	}
	select {
	case s := <-u.rec.done:
		u.sendRecorded(s)
	case <-time.After(5 * time.Second):
		t.Fatal("the recording wasn't encoded")
	}
	st.frame()
	msgs := st.b.Messages("rina", 1000)
	m := msgs[len(msgs)-1]
	if !m.FromMe || m.Media != model.MediaVoice || m.Duration != 1 || len(m.Waveform) != recorder.WaveformLen {
		t.Fatalf("sent %+v", m)
	}
}

// TestDiscardRecording throws a recording away with the trash, and when
// another chat opens.
func TestDiscardRecording(t *testing.T) {
	recorder.Demo = true
	defer func() { recorder.Demo = false }()
	st := newSlashTest(t, "rina")
	u := st.u
	if !u.micClicked() {
		t.Fatal("no recording")
	}
	u.discardRecording()
	if u.rec.sess != nil {
		t.Fatal("trash kept the recording")
	}
	u.micClicked()
	u.SelectID("work")
	st.frame()
	if u.rec.sess != nil {
		t.Fatal("opening another chat kept the recording")
	}
}
