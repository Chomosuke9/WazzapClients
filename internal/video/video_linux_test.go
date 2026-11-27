//go:build linux && cgo

package video

import (
	"encoding/json"
	"errors"
	"image"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

// media makes a test file with the system's ffmpeg, or skips the test.
func media(t *testing.T, name string, args ...string) string {
	t.Helper()
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("no ffmpeg to make test media")
	}
	path := filepath.Join(t.TempDir(), name)
	cmd := exec.Command("ffmpeg", append(append([]string{"-v", "error", "-y"}, args...), path)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("ffmpeg can't make %s: %v\n%s", name, err, out)
	}
	return path
}

// open opens path, skipping the test where GStreamer or its plugins are
// missing (CI has neither).
func open(t *testing.T, path string, audio bool) *Player {
	t.Helper()
	opener := Open
	if audio {
		opener = OpenAudio
	}
	p, err := opener(path, nil)
	if errors.Is(err, ErrUnsupported) {
		t.Skip("no GStreamer here")
	}
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(p.Close)
	return p
}

// wait polls until ok holds for the player's status, or fails.
func wait(t *testing.T, p *Player, what string, ok func(Status) bool) Status {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		st := p.Status()
		if st.Err != nil {
			t.Skipf("can't play here (missing GStreamer plugins?): %v", st.Err)
		}
		if ok(st) {
			return st
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s; status %+v", what, st)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestVideo(t *testing.T) {
	// A phone video: stored 320x240, shown turned a quarter.
	plain := media(t, "plain.mp4", "-f", "lavfi", "-i", "testsrc=size=320x240:rate=30:duration=1",
		"-f", "lavfi", "-i", "sine=frequency=440:duration=1",
		"-c:v", "libx264", "-pix_fmt", "yuv420p", "-c:a", "aac", "-shortest")
	path := media(t, "clip.mp4", "-display_rotation", "90", "-i", plain, "-c", "copy")
	p := open(t, path, false)
	p.SetMuted(true)
	p.SetFrameSize(image.Pt(120, 120))

	st := wait(t, p, "ready", func(s Status) bool { return s.Ready })
	if st.Size != image.Pt(240, 320) {
		t.Errorf("size %v, want 240x320 (upright)", st.Size)
	}
	if st.Dur < 900*time.Millisecond || st.Dur > 1100*time.Millisecond {
		t.Errorf("length %v, want about 1s", st.Dur)
	}
	deadline := time.Now().Add(5 * time.Second)
	var f *image.RGBA
	for f == nil || f.Rect.Size() != image.Pt(90, 120) {
		if time.Now().After(deadline) {
			t.Fatalf("no 90x120 frame; last %v", f)
		}
		f, _ = p.Frame()
		time.Sleep(10 * time.Millisecond)
	}
	if f.Pix[3] != 0xff {
		t.Errorf("frame isn't opaque: alpha %d", f.Pix[3])
	}

	wait(t, p, "the end", func(s Status) bool { return s.Ended && s.Paused })

	// Seeking at the end shows that frame, paused; play starts over.
	p.Seek(500*time.Millisecond, true)
	wait(t, p, "the seek", func(s Status) bool { return !s.Ended && s.Paused && s.Pos >= 450*time.Millisecond })
	p.Play()
	wait(t, p, "playing again", func(s Status) bool { return !s.Paused && !s.Ended })
}

func TestLoop(t *testing.T) {
	path := media(t, "gif.mp4", "-f", "lavfi", "-i", "testsrc=size=64x64:rate=30:duration=0.4",
		"-c:v", "libx264", "-pix_fmt", "yuv420p")
	p := open(t, path, false)
	p.SetMuted(true)
	p.SetLoop(true)
	// Three laps without ending.
	time.Sleep(1300 * time.Millisecond)
	st := wait(t, p, "ready", func(s Status) bool { return s.Ready })
	if st.Ended || st.Paused {
		t.Fatalf("a looping video stopped: %+v", st)
	}
	p.SetLoop(false)
	wait(t, p, "the end after looping", func(s Status) bool { return s.Ended })
}

func TestVoice(t *testing.T) {
	// WhatsApp's voice messages are Opus in OGG.
	path := media(t, "voice.ogg", "-f", "lavfi", "-i", "sine=frequency=440:duration=1.5",
		"-c:a", "libopus", "-b:a", "16k")
	p := open(t, path, true)
	p.SetVolume(0)
	p.SetRate(2)
	start := time.Now()
	wait(t, p, "ready", func(s Status) bool { return s.Ready })
	if f, _ := p.Frame(); f != nil {
		t.Error("an audio player gave a frame")
	}
	wait(t, p, "the end", func(s Status) bool { return s.Ended })
	// 1.5 s at twice the speed, plus setting up.
	if d := time.Since(start); d > 1400*time.Millisecond {
		t.Errorf("took %v at 2x, want well under 1.5s", d)
	}
}

// The sound server remembers a stream's mute and volume for the app's next
// streams, so a player's must not reach it: a muted GIF silenced every voice
// message after it.
func TestMuteStaysHere(t *testing.T) {
	path := media(t, "clip.mp4", "-f", "lavfi", "-i", "testsrc=size=64x64:rate=30:duration=5",
		"-f", "lavfi", "-i", "anullsrc=duration=5",
		"-c:v", "libx264", "-pix_fmt", "yuv420p", "-c:a", "aac", "-shortest")
	p := open(t, path, false)
	p.SetMuted(true)
	p.SetVolume(0.1)
	wait(t, p, "playing", func(s Status) bool { return s.Ready && s.Pos > 300*time.Millisecond })
	out, err := exec.Command("pactl", "-f", "json", "list", "sink-inputs").Output()
	if err != nil {
		t.Skip("no sound server to ask")
	}
	var streams []struct {
		Mute   bool `json:"mute"`
		Volume map[string]struct {
			Value int `json:"value"`
		} `json:"volume"`
		Properties map[string]string `json:"properties"`
	}
	if err := json.Unmarshal(out, &streams); err != nil {
		t.Skipf("can't read pactl's answer: %v", err)
	}
	found := false
	for _, s := range streams {
		if s.Properties["application.process.id"] != strconv.Itoa(os.Getpid()) {
			continue
		}
		found = true
		if s.Mute {
			t.Error("the sound server's stream is muted")
		}
		for ch, v := range s.Volume {
			if v.Value < 1<<15 { // 0.1 is about 30000 of 65536
				t.Errorf("the sound server's stream has volume %d on %s", v.Value, ch)
			}
		}
	}
	if !found {
		t.Skip("the sound went to no PulseAudio or PipeWire stream")
	}
}

func TestStill(t *testing.T) {
	// Stored 320x240, shown turned a quarter: the still and size are as shown.
	plain := media(t, "plain.mp4", "-f", "lavfi", "-i", "testsrc=size=320x240:rate=30:duration=1",
		"-f", "lavfi", "-i", "sine=frequency=440:duration=1",
		"-c:v", "libx264", "-pix_fmt", "yuv420p", "-c:a", "aac", "-shortest")
	path := media(t, "clip.mp4", "-display_rotation", "90", "-i", plain, "-c", "copy")
	start := time.Now()
	img, size, err := Still(path, image.Pt(160, 160), 10*time.Second)
	if errors.Is(err, ErrUnsupported) {
		t.Skip("no GStreamer here")
	}
	if err != nil {
		t.Skipf("can't play here (missing GStreamer plugins?): %v", err)
	}
	t.Logf("still in %v", time.Since(start))
	if size != image.Pt(240, 320) {
		t.Errorf("size %v, want 240x320", size)
	}
	if b := img.Rect.Size(); b.X > 160 || b.Y > 160 || b.X >= b.Y {
		t.Errorf("still is %v, want at most 160x160 and taller than wide", b)
	}
}
