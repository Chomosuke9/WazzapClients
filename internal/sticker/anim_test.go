package sticker

import (
	"bufio"
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/color/palette"
	"image/gif"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/chomosuke9/wazzapclients/internal/webpanim"
)

func needFFmpeg(t *testing.T) string {
	t.Helper()
	ff := FFmpeg()
	if ff == "" {
		t.Skip("no ffmpeg")
	}
	return ff
}

// testGIF is a GIF of n frames, each a square moving across a w x h picture.
func testGIF(t *testing.T, n, w, h int) []byte {
	t.Helper()
	g := &gif.GIF{}
	for i := range n {
		p := image.NewPaletted(image.Rect(0, 0, w, h), palette.Plan9)
		for y := range h {
			for x := range w {
				p.Set(x, y, color.RGBA{uint8(x), uint8(y), 128, 255})
			}
		}
		for y := 10; y < 40; y++ {
			for x := 10 + 20*i; x < 40+20*i && x < w; x++ {
				p.Set(x, y, color.White)
			}
		}
		g.Image = append(g.Image, p)
		g.Delay = append(g.Delay, 10)
	}
	var buf bytes.Buffer
	if err := gif.EncodeAll(&buf, g); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// checkAnimated checks that out is an animated sticker of at least
// frames frames.
func checkAnimated(t *testing.T, out []byte, frames int) *webpanim.Anim {
	t.Helper()
	if len(out) > MaxAnimBytes {
		t.Errorf("sticker is %d bytes, over %d", len(out), MaxAnimBytes)
	}
	a, err := webpanim.Parse(out)
	if err != nil {
		t.Fatalf("not an animated WebP: %v", err)
	}
	if a.Width != Size || a.Height != Size {
		t.Errorf("sticker is %dx%d", a.Width, a.Height)
	}
	if len(a.Frames) < frames {
		t.Errorf("sticker has %d frames, want at least %d", len(a.Frames), frames)
	}
	for range a.Frames {
		if _, _, err := a.Next(); err != nil {
			t.Fatalf("frame: %v", err)
		}
	}
	return a
}

func TestMakeGIF(t *testing.T) {
	needFFmpeg(t)
	out, err := Make("", testGIF(t, 4, 200, 100), Text{Top: "hello", Bottom: "world"})
	if err != nil {
		t.Fatal(err)
	}
	a := checkAnimated(t, out, 2)
	// The picture is wide: the top and bottom of the canvas stay clear.
	a.Rewind()
	c, _, _ := a.Next()
	if _, _, _, alpha := c.At(Size/2, 10).RGBA(); alpha != 0 {
		t.Errorf("above the picture isn't transparent (alpha %d)", alpha)
	}
	if _, _, _, alpha := c.At(Size/2, Size/2).RGBA(); alpha == 0 {
		t.Error("the picture's middle is transparent")
	}
}

func TestMakeGIFStill(t *testing.T) {
	needFFmpeg(t)
	out, err := Make("", testGIF(t, 1, 100, 100), Text{})
	if err != nil {
		t.Fatal(err)
	}
	if webpanim.IsAnimated(out) {
		t.Error("a one-frame GIF made an animated sticker")
	}
	if _, w, h := decodeARGB(t, out); w != Size || h != Size {
		t.Errorf("sticker is %dx%d", w, h)
	}
}

func TestMakeVideo(t *testing.T) {
	ff := needFFmpeg(t)
	path := filepath.Join(t.TempDir(), "clip.mp4")
	// Longer than a sticker keeps, and portrait.
	cmd := exec.Command(ff, "-hide_banner", "-loglevel", "error", "-f", "lavfi",
		"-i", "testsrc=size=240x320:rate=25:duration=8", "-c:v", "mpeg4", path)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("can't make a test video: %v: %s", err, out)
	}
	out, err := Make(path, nil, Text{Bottom: "video"})
	if err != nil {
		t.Fatal(err)
	}
	a := checkAnimated(t, out, 2)
	var total int64
	for _, f := range a.Frames {
		total += int64(f.Delay)
	}
	if total > int64(maxAnim)+int64(maxAnim)/50 {
		t.Errorf("sticker lasts %v, longer than %v", total, maxAnim)
	}
}

func TestMakeAnimatedWebP(t *testing.T) {
	needFFmpeg(t)
	first, err := Make("", testGIF(t, 4, 100, 100), Text{})
	if err != nil {
		t.Fatal(err)
	}
	out, err := Make("", first, Text{Top: "again"})
	if err != nil {
		t.Fatal(err)
	}
	checkAnimated(t, out, 2)
}

func TestMakeWithoutFFmpeg(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	// A GIF makes a still sticker of its first frame.
	out, err := Make("", testGIF(t, 3, 100, 100), Text{})
	if err != nil {
		t.Fatal(err)
	}
	if webpanim.IsAnimated(out) {
		t.Error("made an animated sticker without ffmpeg")
	}
	path := filepath.Join(t.TempDir(), "clip.mp4")
	if err := os.WriteFile(path, []byte("\x00\x00\x00\x18ftypmp42"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Make(path, nil, Text{}); !errors.Is(err, ErrNoFFmpeg) {
		t.Errorf("a video without ffmpeg: %v, want ErrNoFFmpeg", err)
	}
}

func TestReadPAM(t *testing.T) {
	frame := "P7\nWIDTH 2\nHEIGHT 1\nDEPTH 4\nMAXVAL 255\nTUPLTYPE RGB_ALPHA\nENDHDR\n" +
		"\xff\x00\x00\xff" + "\xff\xff\xff\x80"
	r := bufio.NewReader(bytes.NewReader([]byte(frame + frame)))
	var img *image.RGBA
	for range 2 {
		var err error
		if img, err = readPAM(r, img); err != nil {
			t.Fatal(err)
		}
		if img.Rect.Dx() != 2 || img.Rect.Dy() != 1 {
			t.Fatalf("frame is %v", img.Rect)
		}
		if got := img.RGBAAt(1, 0); got != (color.RGBA{0x80, 0x80, 0x80, 0x80}) {
			t.Errorf("half-transparent white is %v, want premultiplied", got)
		}
	}
	if _, err := readPAM(r, img); err != io.EOF {
		t.Errorf("after the last frame: %v, want io.EOF", err)
	}
}

func TestSniff(t *testing.T) {
	for _, c := range []struct {
		head string
		want kind
	}{
		{"GIF89a", kindGIF},
		{"\xff\xd8\xff\xe0", kindStill},
		{"\x89PNG\r\n\x1a\n", kindStill},
		{"RIFF\x00\x00\x00\x00WEBPVP8 ", kindStill},
		{"RIFF\x00\x00\x00\x00WEBPVP8X\x0a\x00\x00\x00\x02", kindAnimWebP},
		{"\x00\x00\x00\x18ftypmp42", kindVideo},
	} {
		if got := sniff([]byte(c.head)); got != c.want {
			t.Errorf("sniff(%q) = %v, want %v", c.head, got, c.want)
		}
	}
}

func TestFPSFor(t *testing.T) {
	for _, c := range []struct {
		length time.Duration
		want   int
	}{
		{0, 15}, {time.Second, 30}, {2 * time.Second, 30}, {3 * time.Second, 30},
		{4 * time.Second, 22}, {6 * time.Second, 15}, {time.Minute, 15},
	} {
		if got := fpsFor(c.length); got != c.want {
			t.Errorf("fpsFor(%v) = %d, want %d", c.length, got, c.want)
		}
	}
}

func TestProbeLength(t *testing.T) {
	ff := needFFmpeg(t)
	path := filepath.Join(t.TempDir(), "clip.gif")
	cmd := exec.Command(ff, "-hide_banner", "-loglevel", "error", "-f", "lavfi",
		"-i", "testsrc=size=100x100:rate=10:duration=2.3", path)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	if got := probeLength(ff, path); got != 2300*time.Millisecond {
		t.Errorf("probeLength = %v, want 2.3s", got)
	}
}
