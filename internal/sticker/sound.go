package sticker

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/chomosuke9/wazzapclients/internal/record"
)

// A Sound is what a sticker plays: Opus in OGG, its length in whole
// seconds and its waveform, as an audio message carries them.
type Sound struct {
	Ogg      []byte
	Seconds  int
	Waveform []byte
}

// ErrNoSound means a file has no sound.
var ErrNoSound = errors.New("sticker: there's no sound")

// soundBitrate is what WhatsApp's own stickers with a sound come at, about.
const soundBitrate = "64k"

// audioRE finds an audio stream in what ffmpeg prints about its input.
var audioRE = regexp.MustCompile(`(?m)^\s*Stream #\d+:\d+.*: Audio: `)

// MakeSound makes the sound of a sticker made from the file at path: its
// audio, cut like the animation. Pictures, and videos without audio, fail
// with ErrNoSound; anything else needs ffmpeg.
func MakeSound(path string) (*Sound, error) {
	head, err := readHead(path, 64)
	if err != nil {
		return nil, err
	}
	if sniff(head) != kindVideo {
		return nil, ErrNoSound
	}
	ff, err := exec.LookPath("ffmpeg")
	if err != nil {
		return nil, ErrNoFFmpeg
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	// With no output, ffmpeg prints what it found and fails.
	probe := exec.CommandContext(ctx, ff, "-hide_banner", "-nostdin", "-i", path)
	hideWindow(probe)
	info, _ := probe.CombinedOutput()
	if !audioRE.Match(info) {
		return nil, ErrNoSound
	}
	f, err := os.CreateTemp("", "wazzap-sticker-*.ogg")
	if err != nil {
		return nil, err
	}
	out := f.Name()
	f.Close()
	defer os.Remove(out)
	// ffmpeg's own Opus encoder stands in when it was built without libopus.
	var pcm []byte
	for _, codec := range [][]string{{"-c:a", "libopus"}, {"-c:a", "opus", "-strict", "-2"}} {
		args := []string{"-hide_banner", "-loglevel", "error", "-nostdin", "-y",
			"-t", strconv.FormatFloat(maxAnim.Seconds(), 'f', -1, 64), "-i", path,
			"-map", "0:a:0", "-map_metadata", "-1", "-ac", "1", "-ar", "48000"}
		args = append(args, codec...)
		args = append(args, "-b:a", soundBitrate, "-f", "ogg", out,
			// The samples, for the length and waveform.
			"-map", "0:a:0", "-ac", "1", "-ar", strconv.Itoa(record.Rate), "-f", "s16le", "pipe:1")
		cmd := exec.CommandContext(ctx, ff, args...)
		hideWindow(cmd)
		var stdout, stderr bytes.Buffer
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		if err = cmd.Run(); err == nil {
			pcm = stdout.Bytes()
			break
		}
		err = fmt.Errorf("sticker: ffmpeg: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	if err != nil {
		return nil, err
	}
	if len(pcm) < 2 {
		return nil, ErrNoSound // an empty stream
	}
	ogg, err := os.ReadFile(out)
	if err != nil {
		return nil, err
	}
	secs := float64(len(pcm)/2) / record.Rate
	return &Sound{Ogg: ogg, Seconds: max(1, int(math.Round(secs))), Waveform: record.PCMWaveform(pcm)}, nil
}
