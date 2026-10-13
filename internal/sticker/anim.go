package sticker

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/draw"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/chomosuke9/wazzapclients/internal/webpanim"
)

// Animated stickers are encoded by the system's ffmpeg, which isn't
// bundled: running it isn't linking it, and lossy VP8 (all that fits an
// animation in MaxAnimBytes) would need libwebp through cgo. Its libwebp
// encoder is all it needs; it decodes videos and GIFs too. Animated WebP
// is decoded here (internal/webpanim), since ffmpeg before 8.0 can't.

// MaxAnimBytes is the largest animated sticker this makes, as WhatsApp's
// sticker guidelines ask.
const MaxAnimBytes = 500 << 10

// maxAnim is the longest animation a sticker keeps; the rest is cut.
const maxAnim = 10 * time.Second

// ErrNoFFmpeg means making the sticker needs ffmpeg (with libwebp), and
// there's none.
var ErrNoFFmpeg = errors.New("sticker: ffmpeg isn't installed")

// FFmpeg returns the path of the system's ffmpeg, or "" without one or
// when it was built without libwebp.
func FFmpeg() string {
	path, err := exec.LookPath("ffmpeg")
	if err != nil {
		return ""
	}
	ffmpegMu.Lock()
	defer ffmpegMu.Unlock()
	ok, seen := ffmpegOK[path]
	if !seen {
		cmd := exec.Command(path, "-hide_banner", "-encoders")
		hideWindow(cmd)
		out, _ := cmd.Output()
		ok = bytes.Contains(out, []byte("libwebp_anim"))
		ffmpegOK[path] = ok
	}
	if !ok {
		return ""
	}
	return path
}

var (
	ffmpegMu sync.Mutex
	ffmpegOK = map[string]bool{} // whether each ffmpeg has libwebp
)

// tries are the frame rates, qualities and lengths tried, in turn, until
// an animation fits in MaxAnimBytes. Most GIFs fit the first; 10 s of a
// busy video only the last ones.
var tries = []struct {
	fps, quality int
	length       time.Duration
}{
	{15, 75, maxAnim}, {15, 50, maxAnim}, {12, 35, 8 * time.Second},
	{10, 25, 6 * time.Second}, {8, 20, 5 * time.Second}, {8, 10, 3 * time.Second},
}

// Make makes a sticker of the file at path, or of data when path is "":
// a still one of a still picture (as FromImage), an animated one of a
// video, a GIF or an animated WebP, cut to 10 seconds. Animations need
// ffmpeg; without it a GIF makes a still sticker of its first frame, and
// anything else fails with ErrNoFFmpeg.
func Make(path string, data []byte, text Text) ([]byte, error) {
	head := data
	if path != "" {
		var err error
		if head, err = readHead(path, 64); err != nil {
			return nil, err
		}
	}
	ff := FFmpeg()
	kind := sniff(head)
	if kind == kindStill || kind == kindGIF && ff == "" {
		if path != "" {
			var err error
			if data, err = os.ReadFile(path); err != nil {
				return nil, err
			}
		}
		return FromImage(data, text)
	}
	if kind == kindAnimWebP && data == nil {
		var err error
		if data, err = os.ReadFile(path); err != nil {
			return nil, err
		}
	}
	if ff == "" {
		// A sticker already: it can go as it is.
		if kind == kindAnimWebP && strings.TrimSpace(text.Top+text.Bottom) == "" && fitsAsIs(data) {
			return data, nil
		}
		return nil, ErrNoFFmpeg
	}
	var src frameSource
	if kind == kindAnimWebP {
		a, err := webpanim.Parse(data)
		if err != nil {
			return nil, err
		}
		if int64(a.Width)*int64(a.Height) > maxPixels {
			return nil, ErrTooLarge
		}
		src = webpFrames(a)
	} else {
		in := path
		if in == "" {
			f, err := os.CreateTemp("", "wazzap-sticker-*")
			if err != nil {
				return nil, err
			}
			defer os.Remove(f.Name())
			_, err = f.Write(data)
			if cerr := f.Close(); err == nil {
				err = cerr
			}
			if err != nil {
				return nil, err
			}
			in = f.Name()
		}
		src = ffmpegFrames(ff, in)
	}
	return animate(ff, src, text)
}

type kind int

const (
	kindVideo kind = iota // or anything else ffmpeg may read
	kindStill             // JPEG, PNG or still WebP
	kindGIF               // animated or not
	kindAnimWebP
)

// sniff tells what a file holds from its first bytes.
func sniff(b []byte) kind {
	switch {
	case bytes.HasPrefix(b, []byte("GIF8")):
		return kindGIF
	case len(b) >= 12 && string(b[:4]) == "RIFF" && string(b[8:12]) == "WEBP":
		if animatedWebP(b) {
			return kindAnimWebP
		}
		return kindStill
	case bytes.HasPrefix(b, []byte("\xff\xd8\xff")), bytes.HasPrefix(b, []byte("\x89PNG\r\n\x1a\n")):
		return kindStill
	}
	return kindVideo
}

// readHead reads the first n bytes of a file (fewer if it's shorter).
func readHead(path string, n int) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	b := make([]byte, n)
	n, err = io.ReadFull(f, b)
	if err == io.ErrUnexpectedEOF || err == io.EOF {
		err = nil
	}
	return b[:n], err
}

// fitsAsIs reports whether an animated WebP is a sticker already.
func fitsAsIs(data []byte) bool {
	if len(data) > MaxAnimBytes {
		return false
	}
	a, err := webpanim.Parse(data)
	return err == nil && a.Width == Size && a.Height == Size
}

// A frameSource calls yield with the frames of an animation at fps
// frames a second, its first length of them, and stops at yield's first
// error. The frames are reused after yield returns.
type frameSource func(ctx context.Context, fps int, length time.Duration, yield func(*image.RGBA) error) error

// animate encodes the frames of src as a sticker: animated if they
// change, otherwise still (a one-frame GIF, which ffmpeg repeats). It
// lowers the frame rate, quality and length until the animation fits.
func animate(ff string, src frameSource, text Text) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	var (
		layer *image.RGBA // the text, drawn once
		frame = image.NewRGBA(image.Rect(0, 0, Size, Size))
		raw   = make([]byte, 4*Size*Size)
	)
	compose := func(img *image.RGBA) error {
		area := fit(frame, img)
		if layer == nil {
			layer = image.NewRGBA(frame.Rect)
			if err := drawText(layer, text, area); err != nil {
				return err
			}
		}
		draw.Draw(frame, frame.Bounds(), layer, image.Point{}, draw.Over)
		return nil
	}
	for i := 0; i < len(tries); i++ {
		t := tries[i]
		still, moved, repeats := (*image.RGBA)(nil), false, 0
		enc, err := startEncoder(ctx, ff, t.fps, t.quality)
		if err != nil {
			return nil, err
		}
		err = src(ctx, t.fps, t.length, func(img *image.RGBA) error {
			if err := compose(img); err != nil {
				return err
			}
			if still == nil {
				still = image.NewRGBA(frame.Rect)
				copy(still.Pix, frame.Pix)
			}
			if !moved {
				if bytes.Equal(frame.Pix, still.Pix) {
					repeats++
					return nil
				}
				moved = true
				for range repeats {
					if err := enc.write(still, raw); err != nil {
						return err
					}
				}
			}
			return enc.write(frame, raw)
		})
		if err != nil {
			enc.abort()
			return nil, err
		}
		if !moved {
			enc.abort()
			if still == nil {
				return nil, errors.New("sticker: no frames")
			}
			return encodeStill(still)
		}
		out, err := enc.finish()
		if err != nil {
			return nil, err
		}
		if len(out) <= MaxAnimBytes {
			return out, nil
		}
		if len(out) > 2*MaxAnimBytes {
			i++ // the next try won't halve it
		}
	}
	return nil, errors.New("sticker: the animation doesn't fit in a sticker")
}

// encodeStill encodes a Size x Size picture as a lossless WebP, dropping
// low bits of its colors until it fits.
func encodeStill(c *image.RGBA) ([]byte, error) {
	argb := unpremultiply(c)
	for quant := range uint(4) {
		if out := encodeWebP(argb, Size, Size, quant); len(out) <= MaxBytes {
			return out, nil
		}
	}
	return nil, errors.New("sticker: the picture doesn't fit in a sticker")
}

// encoder is an ffmpeg encoding raw frames into an animated WebP file.
type encoder struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	out    string // the file it writes
	stderr bytes.Buffer
}

func startEncoder(ctx context.Context, ff string, fps, quality int) (*encoder, error) {
	f, err := os.CreateTemp("", "wazzap-sticker-*.webp")
	if err != nil {
		return nil, err
	}
	f.Close()
	e := &encoder{out: f.Name()}
	e.cmd = exec.CommandContext(ctx, ff, "-hide_banner", "-loglevel", "error", "-nostdin",
		"-f", "rawvideo", "-pix_fmt", "rgba", "-s", fmt.Sprintf("%dx%d", Size, Size),
		"-framerate", strconv.Itoa(fps), "-i", "pipe:0",
		"-c:v", "libwebp_anim", "-pix_fmt", "bgra", "-lossless", "0",
		"-quality", strconv.Itoa(quality), "-compression_level", "4", "-loop", "0",
		"-an", "-f", "webp", "-y", e.out)
	hideWindow(e.cmd)
	e.cmd.Stderr = &e.stderr
	if e.stdin, err = e.cmd.StdinPipe(); err != nil {
		os.Remove(e.out)
		return nil, err
	}
	if err := e.cmd.Start(); err != nil {
		os.Remove(e.out)
		return nil, err
	}
	return e, nil
}

// write sends a frame, with straight (not premultiplied) alpha.
func (e *encoder) write(img *image.RGBA, raw []byte) error {
	for i := 0; i < len(raw); i += 4 {
		p := img.Pix[i : i+4 : i+4]
		r, g, b, a := uint32(p[0]), uint32(p[1]), uint32(p[2]), uint32(p[3])
		if a != 0 && a != 0xff {
			r, g, b = min(255, (r*255+a/2)/a), min(255, (g*255+a/2)/a), min(255, (b*255+a/2)/a)
		}
		raw[i], raw[i+1], raw[i+2], raw[i+3] = byte(r), byte(g), byte(b), byte(a)
	}
	if _, err := e.stdin.Write(raw); err != nil {
		return e.fail(err)
	}
	return nil
}

func (e *encoder) finish() ([]byte, error) {
	defer os.Remove(e.out)
	e.stdin.Close()
	if err := e.cmd.Wait(); err != nil {
		return nil, e.fail(err)
	}
	return os.ReadFile(e.out)
}

func (e *encoder) abort() {
	e.stdin.Close()
	e.cmd.Process.Kill()
	e.cmd.Wait()
	os.Remove(e.out)
}

// fail adds what ffmpeg said to err.
func (e *encoder) fail(err error) error {
	if msg := strings.TrimSpace(e.stderr.String()); msg != "" {
		return fmt.Errorf("sticker: ffmpeg: %v: %s", err, msg)
	}
	return fmt.Errorf("sticker: ffmpeg: %w", err)
}

// ffmpegFrames decodes a video or GIF with ffmpeg, scaled to fit a
// sticker. Frames come as PAM pictures, which say their size: ffmpeg
// turns phone videos upright, so the file's own size may be the wrong
// way round.
func ffmpegFrames(ff, path string) frameSource {
	return func(ctx context.Context, fps int, length time.Duration, yield func(*image.RGBA) error) error {
		ctx, cancel := context.WithCancel(ctx)
		defer cancel()
		vf := fmt.Sprintf("fps=%d,format=rgba,scale=%d:%d:force_original_aspect_ratio=decrease:flags=lanczos", fps, Size, Size)
		cmd := exec.CommandContext(ctx, ff, "-hide_banner", "-loglevel", "error", "-nostdin",
			"-t", strconv.FormatFloat(length.Seconds(), 'f', -1, 64), "-i", path,
			"-an", "-vf", vf, "-frames:v", strconv.Itoa(int(length.Seconds()*float64(fps))),
			"-f", "image2pipe", "-c:v", "pam", "pipe:1")
		hideWindow(cmd)
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			return err
		}
		if err := cmd.Start(); err != nil {
			return err
		}
		r := bufio.NewReaderSize(stdout, 64<<10)
		var img *image.RGBA
		for {
			if img, err = readPAM(r, img); err != nil {
				break
			}
			if err = yield(img); err != nil {
				break
			}
		}
		if err != io.EOF {
			cancel()
			cmd.Wait()
			return err
		}
		if werr := cmd.Wait(); werr != nil {
			if msg := strings.TrimSpace(stderr.String()); msg != "" {
				return fmt.Errorf("sticker: ffmpeg: %v: %s", werr, msg)
			}
			return fmt.Errorf("sticker: ffmpeg: %w", werr)
		}
		return nil
	}
}

// readPAM reads one RGBA PAM picture (as ffmpeg writes them) into img,
// or a new picture if img is nil or another size. It returns io.EOF when
// there are no more.
func readPAM(r *bufio.Reader, img *image.RGBA) (*image.RGBA, error) {
	w, h := -1, -1
	for first := true; ; first = false {
		line, err := r.ReadString('\n')
		if err != nil {
			if first && line == "" && err == io.EOF {
				return nil, io.EOF
			}
			return nil, errors.New("sticker: bad frame from ffmpeg")
		}
		line = strings.TrimSpace(line)
		if first {
			if line != "P7" {
				return nil, errors.New("sticker: bad frame from ffmpeg")
			}
			continue
		}
		if line == "ENDHDR" {
			break
		}
		k, v, _ := strings.Cut(line, " ")
		switch k {
		case "WIDTH":
			w, _ = strconv.Atoi(v)
		case "HEIGHT":
			h, _ = strconv.Atoi(v)
		case "DEPTH":
			if v != "4" {
				return nil, errors.New("sticker: frames from ffmpeg aren't RGBA")
			}
		}
	}
	if w <= 0 || h <= 0 || w > 4*Size || h > 4*Size {
		return nil, errors.New("sticker: bad frame size from ffmpeg")
	}
	if img == nil || img.Rect.Dx() != w || img.Rect.Dy() != h {
		img = image.NewRGBA(image.Rect(0, 0, w, h))
	}
	if _, err := io.ReadFull(r, img.Pix); err != nil {
		return nil, errors.New("sticker: frame from ffmpeg cut short")
	}
	// PAM's alpha is straight; image.RGBA's is premultiplied.
	premultiply(img.Pix)
	return img, nil
}

func premultiply(pix []byte) {
	for i := 0; i < len(pix); i += 4 {
		a := uint32(pix[i+3])
		if a == 0xff {
			continue
		}
		pix[i] = byte((uint32(pix[i])*a + 127) / 255)
		pix[i+1] = byte((uint32(pix[i+1])*a + 127) / 255)
		pix[i+2] = byte((uint32(pix[i+2])*a + 127) / 255)
	}
}

// webpFrames plays an animated WebP once at fps frames a second, the
// frame showing at each tick.
func webpFrames(a *webpanim.Anim) frameSource {
	return func(ctx context.Context, fps int, length time.Duration, yield func(*image.RGBA) error) error {
		a.Rewind()
		tick := time.Second / time.Duration(fps)
		var (
			cur  *image.RGBA
			end  time.Duration // when cur stops showing
			next int           // frames drawn
		)
		for t := time.Duration(0); t < length; t += tick {
			for cur == nil || t >= end {
				if next == len(a.Frames) {
					return nil // played once
				}
				img, delay, err := a.Next()
				if err != nil {
					return err
				}
				if delay <= 0 {
					delay = 100 * time.Millisecond
				}
				cur, end, next = img, end+delay, next+1
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			if err := yield(cur); err != nil {
				return err
			}
		}
		return nil
	}
}
