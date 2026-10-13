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
	"regexp"
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

// MaxAnimBytes is the largest animated sticker this makes.
const MaxAnimBytes = 1 << 20

// maxAnim is the longest animation a sticker keeps; the rest is cut.
const maxAnim = 6 * time.Second

// An animation gets frameBudget frames: maxFPS up to 3 seconds, fewer a
// second the longer it is (15 at 6 seconds). If it doesn't fit even at
// minQuality, the frame rate drops by a third, down to minFPS.
const (
	frameBudget = 90
	maxFPS      = 30
	minFPS      = 8
)

// The qualities searched for the best that fits: maxQuality first, and
// at least minQuality while the frame rate can still drop.
const (
	maxQuality = 90
	minQuality = 40
)

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

// Make makes a sticker of the file at path, or of data when path is "":
// a still one of a still picture (as FromImage), an animated one of a
// video, a GIF or an animated WebP, cut to 6 seconds, at the best frame
// rate and quality that fit in MaxAnimBytes. Animations need
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
	var (
		src    frameSource
		length time.Duration // 0 when unknown
	)
	if kind == kindAnimWebP {
		a, err := webpanim.Parse(data)
		if err != nil {
			return nil, err
		}
		if int64(a.Width)*int64(a.Height) > maxPixels {
			return nil, ErrTooLarge
		}
		src = webpFrames(a)
		for _, f := range a.Frames {
			length += delayOf(f.Delay)
		}
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
		length = probeLength(ff, in)
	}
	return animate(ff, src, fpsFor(length), text)
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

// fpsFor is the frame rate an animation of length starts at.
func fpsFor(length time.Duration) int {
	if length <= 0 || length > maxAnim {
		length = maxAnim
	}
	return max(minFPS, min(maxFPS, int(frameBudget*time.Second/length)))
}

// durationRE finds the length ffmpeg prints for its input.
var durationRE = regexp.MustCompile(`Duration: (\d+):(\d\d):(\d\d(?:\.\d+)?)`)

// probeLength asks ffmpeg how long a video or GIF is, or returns 0.
func probeLength(ff, path string) time.Duration {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	// With no output, ffmpeg prints what it found and fails.
	cmd := exec.CommandContext(ctx, ff, "-hide_banner", "-nostdin", "-i", path)
	hideWindow(cmd)
	out, _ := cmd.CombinedOutput()
	m := durationRE.FindSubmatch(out)
	if m == nil {
		return 0
	}
	h, _ := strconv.Atoi(string(m[1]))
	mins, _ := strconv.Atoi(string(m[2]))
	sec, _ := strconv.ParseFloat(string(m[3]), 64)
	return time.Duration(h)*time.Hour + time.Duration(mins)*time.Minute + time.Duration(sec*float64(time.Second))
}

// A frameSource calls yield with the frames of an animation at fps
// frames a second, at most maxAnim of them, and stops at yield's first
// error. The frames are reused after yield returns.
type frameSource func(ctx context.Context, fps int, yield func(*image.RGBA) error) error

// animate encodes the frames of src, at fps frames a second, as a
// sticker: animated if they change, otherwise still (a one-frame GIF,
// which ffmpeg repeats).
//
// The frames are composed once, with the text, into a temporary file of
// raw pictures (at most frameBudget of them, 1 MB each), which ffmpeg
// encodes again and again at falling qualities: decoding the source
// again each time would cost more, and holding the frames in memory up
// to 90 MB.
func animate(ff string, src frameSource, fps int, text Text) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	f, err := os.CreateTemp("", "wazzap-sticker-*.rgba")
	if err != nil {
		return nil, err
	}
	defer os.Remove(f.Name())
	defer f.Close()
	var (
		layer   *image.RGBA // the text, drawn once
		frame   = image.NewRGBA(image.Rect(0, 0, Size, Size))
		raw     = make([]byte, 4*Size*Size)
		w       = bufio.NewWriterSize(f, 256<<10)
		still   *image.RGBA // the first frame
		moved   bool        // whether a frame differs from it
		repeats int         // frames like it before the first that differs
	)
	write := func(img *image.RGBA) error {
		straight(raw, img.Pix)
		_, err := w.Write(raw)
		return err
	}
	err = src(ctx, fps, func(img *image.RGBA) error {
		area := fit(frame, img)
		if layer == nil {
			layer = image.NewRGBA(frame.Rect)
			if err := drawText(layer, text, area); err != nil {
				return err
			}
		}
		draw.Draw(frame, frame.Bounds(), layer, image.Point{}, draw.Over)
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
				if err := write(still); err != nil {
					return err
				}
			}
		}
		return write(frame)
	})
	if err == nil {
		err = w.Flush()
	}
	if err == nil {
		err = f.Close()
	}
	if err != nil {
		return nil, err
	}
	if !moved {
		if still == nil {
			return nil, errors.New("sticker: no frames")
		}
		return encodeStill(still)
	}
	return search(ctx, ff, f.Name(), fps)
}

// search encodes the raw frames at path, made at fps frames a second, at
// the highest frame rate and then quality that fit in MaxAnimBytes.
func search(ctx context.Context, ff, path string, fps int) ([]byte, error) {
	for rate := fps; ; rate = rate * 2 / 3 {
		last := rate*2/3 < minFPS
		floor := minQuality
		if last {
			floor = 0
		}
		enc := func(q int) ([]byte, error) { return encode(ctx, ff, path, fps, rate, q) }
		hiOut, err := enc(maxQuality)
		if err != nil || len(hiOut) <= MaxAnimBytes {
			return hiOut, err
		}
		best, err := enc(floor)
		if err != nil {
			return nil, err
		}
		if len(best) > MaxAnimBytes {
			if last {
				return nil, errors.New("sticker: the animation doesn't fit in a sticker")
			}
			continue
		}
		// The size grows about evenly with the quality: guess where it
		// reaches the cap from the two ends, a little under it, until
		// one comes close to the cap or the qualities that fit and
		// don't are close.
		lo, hi := floor, maxQuality
		loSize, hiSize := len(best), len(hiOut)
		for range 4 {
			if hi-lo <= 4 || loSize >= MaxAnimBytes*92/100 {
				break
			}
			q := lo + (hi-lo)*(MaxAnimBytes*97/100-loSize)/(hiSize-loSize)
			q = max(lo+1, min(hi-1, q))
			out, err := enc(q)
			if err != nil {
				return nil, err
			}
			if len(out) <= MaxAnimBytes {
				lo, loSize, best = q, len(out), out
			} else {
				hi, hiSize = q, len(out)
			}
		}
		return best, nil
	}
}

// encode runs ffmpeg on the raw frames at path, made at srcFPS frames a
// second, to make an animated WebP of fps frames a second at quality.
func encode(ctx context.Context, ff, path string, srcFPS, fps, quality int) ([]byte, error) {
	f, err := os.CreateTemp("", "wazzap-sticker-*.webp")
	if err != nil {
		return nil, err
	}
	out := f.Name()
	f.Close()
	defer os.Remove(out)
	args := []string{"-hide_banner", "-loglevel", "error", "-nostdin",
		"-f", "rawvideo", "-pix_fmt", "rgba", "-s", fmt.Sprintf("%dx%d", Size, Size),
		"-framerate", strconv.Itoa(srcFPS), "-i", path}
	if fps != srcFPS {
		args = append(args, "-vf", fmt.Sprintf("fps=%d", fps))
	}
	args = append(args, "-c:v", "libwebp_anim", "-pix_fmt", "bgra", "-lossless", "0",
		"-quality", strconv.Itoa(quality), "-compression_level", "4", "-loop", "0",
		"-an", "-f", "webp", "-y", out)
	cmd := exec.CommandContext(ctx, ff, args...)
	hideWindow(cmd)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return nil, fmt.Errorf("sticker: ffmpeg: %v: %s", err, msg)
		}
		return nil, fmt.Errorf("sticker: ffmpeg: %w", err)
	}
	return os.ReadFile(out)
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

// straight copies premultiplied RGBA pixels into dst with straight alpha,
// as ffmpeg reads them.
func straight(dst, pix []byte) {
	for i := 0; i < len(dst); i += 4 {
		p := pix[i : i+4 : i+4]
		r, g, b, a := uint32(p[0]), uint32(p[1]), uint32(p[2]), uint32(p[3])
		if a != 0 && a != 0xff {
			r, g, b = min(255, (r*255+a/2)/a), min(255, (g*255+a/2)/a), min(255, (b*255+a/2)/a)
		}
		dst[i], dst[i+1], dst[i+2], dst[i+3] = byte(r), byte(g), byte(b), byte(a)
	}
}

// ffmpegFrames decodes a video or GIF with ffmpeg, scaled to fit a
// sticker. Frames come as PAM pictures, which say their size: ffmpeg
// turns phone videos upright, so the file's own size may be the wrong
// way round.
func ffmpegFrames(ff, path string) frameSource {
	return func(ctx context.Context, fps int, yield func(*image.RGBA) error) error {
		ctx, cancel := context.WithCancel(ctx)
		defer cancel()
		vf := fmt.Sprintf("fps=%d,format=rgba,scale=%d:%d:force_original_aspect_ratio=decrease:flags=lanczos", fps, Size, Size)
		cmd := exec.CommandContext(ctx, ff, "-hide_banner", "-loglevel", "error", "-nostdin",
			"-t", strconv.FormatFloat(maxAnim.Seconds(), 'f', -1, 64), "-i", path,
			"-an", "-vf", vf, "-frames:v", strconv.Itoa(int(maxAnim.Seconds()*float64(fps))),
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

// delayOf is how long a frame with delay d shows: 100 ms without one, as
// players do.
func delayOf(d time.Duration) time.Duration {
	if d <= 0 {
		return 100 * time.Millisecond
	}
	return d
}

// webpFrames plays an animated WebP once at fps frames a second, the
// frame showing at each tick.
func webpFrames(a *webpanim.Anim) frameSource {
	return func(ctx context.Context, fps int, yield func(*image.RGBA) error) error {
		a.Rewind()
		tick := time.Second / time.Duration(fps)
		var (
			cur  *image.RGBA
			end  time.Duration // when cur stops showing
			next int           // frames drawn
		)
		for t := time.Duration(0); t < maxAnim; t += tick {
			for cur == nil || t >= end {
				if next == len(a.Frames) {
					return nil // played once
				}
				img, delay, err := a.Next()
				if err != nil {
					return err
				}
				cur, end, next = img, end+delayOf(delay), next+1
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
