package record

import (
	"bytes"
	"encoding/binary"
	"os"
	"os/exec"
	"testing"
	"time"
)

func TestDemoSession(t *testing.T) {
	Demo = true
	defer func() { Demo = false }()
	s, err := Start(nil)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(300 * time.Millisecond)
	if err := s.Pause(); err != nil {
		t.Fatal(err)
	}
	wav, err := os.ReadFile(s.Path())
	if err != nil {
		t.Fatal(err)
	}
	if len(wav) <= 44 || !bytes.HasPrefix(wav, []byte("RIFF")) || binary.LittleEndian.Uint32(wav[40:]) != uint32(len(wav)-44) {
		t.Fatalf("bad WAV: %d bytes, header %q", len(wav), wav[:min(44, len(wav))])
	}
	first := s.Elapsed()
	if err := s.Resume(); err != nil {
		t.Fatal(err)
	}
	time.Sleep(200 * time.Millisecond)
	if s.Elapsed() <= first {
		t.Fatalf("resumed recording didn't grow: %v", s.Elapsed())
	}
	lv := make([]float32, 100)
	if n := s.Levels(lv); n == 0 || lv[len(lv)-1] == 0 {
		t.Fatalf("no levels: %d %v", n, lv[len(lv)-5:])
	}
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		s.Discard()
		t.Skip("no ffmpeg")
	}
	path, secs, wave, err := s.Finish()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(path)
	ogg, err := os.ReadFile(path)
	if err != nil || !bytes.HasPrefix(ogg, []byte("OggS")) || !bytes.Contains(ogg, []byte("OpusHead")) {
		t.Fatalf("not OGG Opus: %v %q", err, ogg[:min(40, len(ogg))])
	}
	if secs != 1 || len(wave) != WaveformLen {
		t.Fatalf("secs %d, waveform %d", secs, len(wave))
	}
	if _, err := os.Stat(s.Path()); !os.IsNotExist(err) {
		t.Fatalf("WAV left behind: %v", err)
	}
}

func TestWaveform(t *testing.T) {
	w := waveform([]byte{10, 20, 50})
	if len(w) != WaveformLen || w[WaveformLen-1] != 100 || w[0] != 20 {
		t.Fatalf("%v", w)
	}
	if w := Sample(5*time.Second, true).Waveform(); len(w) != WaveformLen {
		t.Fatalf("%d", len(w))
	}
}
