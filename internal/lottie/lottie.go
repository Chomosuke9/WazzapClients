// Package lottie plays Lottie animations: WhatsApp's animated stickers
// (.was, a zip holding animation/animation.json), Telegram's .tgs (gzipped
// JSON) and plain Lottie JSON.
//
// It draws one frame at a time into an RGBA picture of any size, as rlottie
// and lottie-web do: the frame's shapes are worked out from the keyframes,
// then filled and stroked by a scanline rasterizer of its own. Shape, solid,
// image, null and precomposition layers, masks, track mattes, trim paths,
// repeaters, round corners, dashes and gradients are drawn. Expressions,
// text layers, effects and blend modes aren't (WhatsApp Web plays its
// stickers with lottie-web's light build, which has no expressions either).
package lottie

import (
	"archive/zip"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"io"
	"math"
	"strings"
	"time"
)

// maxJSON caps the animation's JSON, unpacked.
const maxJSON = 32 << 20

// maxSide caps the composition's size, which sizes nothing but the frame's
// fit, but a broken file shouldn't make it absurd.
const maxSide = 1 << 14

// IsWAS reports whether data is a zip file, as WhatsApp's Lottie stickers
// (application/was) are.
func IsWAS(data []byte) bool {
	return len(data) >= 4 && string(data[:4]) == "PK\x03\x04"
}

// Animation is a parsed Lottie animation. It isn't safe for concurrent use:
// Render keeps its scratch buffers.
type Animation struct {
	// Width and Height are the composition's size.
	Width, Height int
	// FrameRate is how many frames the animation plays a second.
	FrameRate float64

	first, last float64 // the composition's in and out points, in frames
	root        *comp
	r           renderer
}

// Load parses an animation: a .was zip, a gzipped .tgs or Lottie JSON.
func Load(data []byte) (*Animation, error) {
	js, err := extract(data, "animation/animation.json")
	if err != nil {
		return nil, err
	}
	return parse(js)
}

// LoadSecondary parses a premium sticker's effect: the second animation of
// a .was zip (animation/animation_secondary.json), which WhatsApp plays
// once around the sticker, four times its size.
func LoadSecondary(data []byte) (*Animation, error) {
	if !IsWAS(data) {
		return nil, errors.New("lottie: not a .was sticker")
	}
	js, err := extract(data, "animation/animation_secondary.json")
	if err != nil {
		return nil, err
	}
	return parse(js)
}

func parse(js []byte) (*Animation, error) {
	var ja jsonAnim
	if err := json.Unmarshal(js, &ja); err != nil {
		return nil, fmt.Errorf("lottie: %w", err)
	}
	if ja.FR <= 0 || ja.OP <= ja.IP || ja.W <= 0 || ja.H <= 0 || ja.W > maxSide || ja.H > maxSide {
		return nil, errors.New("lottie: not an animation")
	}
	b := builder{assets: map[string]*jsonAsset{}, comps: map[string]*comp{}, images: map[string]*imageAsset{}}
	for i := range ja.Assets {
		b.assets[idString(ja.Assets[i].ID)] = &ja.Assets[i]
	}
	a := &Animation{
		Width:     int(math.Round(ja.W)),
		Height:    int(math.Round(ja.H)),
		FrameRate: ja.FR,
		first:     ja.IP,
		last:      ja.OP,
	}
	a.root = b.comp(ja.Layers, ja.W, ja.H, 0)
	a.r.frameRate = ja.FR
	return a, nil
}

// extract returns the animation's JSON from a .was zip (the file whose
// name ends in name) or a .tgs, or data itself.
func extract(data []byte, name string) ([]byte, error) {
	switch {
	case IsWAS(data):
		zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
		if err != nil {
			return nil, fmt.Errorf("lottie: %w", err)
		}
		for _, f := range zr.File {
			// WhatsApp Web looks for /animation\/animation.json$/.
			if !strings.HasSuffix(f.Name, name) {
				continue
			}
			if f.UncompressedSize64 > maxJSON {
				return nil, errors.New("lottie: animation too big")
			}
			rc, err := f.Open()
			if err != nil {
				return nil, fmt.Errorf("lottie: %w", err)
			}
			defer rc.Close()
			return readLimited(rc)
		}
		return nil, errors.New("lottie: no " + name)
	case len(data) >= 2 && data[0] == 0x1f && data[1] == 0x8b:
		zr, err := gzip.NewReader(bytes.NewReader(data))
		if err != nil {
			return nil, fmt.Errorf("lottie: %w", err)
		}
		return readLimited(zr)
	}
	return data, nil
}

func readLimited(r io.Reader) ([]byte, error) {
	b, err := io.ReadAll(io.LimitReader(r, maxJSON+1))
	if err != nil {
		return nil, fmt.Errorf("lottie: %w", err)
	}
	if len(b) > maxJSON {
		return nil, errors.New("lottie: animation too big")
	}
	return b, nil
}

// Frames returns how many frames the animation has.
func (a *Animation) Frames() int {
	return max(1, int(math.Ceil(a.last-a.first)))
}

// Duration returns how long the animation plays once.
func (a *Animation) Duration() time.Duration {
	return time.Duration((a.last - a.first) / a.FrameRate * float64(time.Second))
}

// Fit returns the size to draw the animation at so that its longer side is
// at most maxSide (and no bigger than the composition).
func (a *Animation) Fit(maxSide int) (w, h int) {
	w, h = a.Width, a.Height
	if s := max(w, h); s > maxSide && maxSide > 0 {
		w, h = max(1, w*maxSide/s), max(1, h*maxSide/s)
	}
	return w, h
}

// Scratch returns the memory, in bytes, that Render keeps between frames:
// its coverage rows, layers drawn apart (masks, mattes) and paths.
func (a *Animation) Scratch() int64 {
	r := &a.r
	n := 4 * (len(r.ras.acc) + len(r.ras.row) + len(r.cov) + len(r.mask))
	for _, b := range r.free {
		n += len(b.pix)
	}
	n += 16 * (cap(r.pts) + cap(r.poly) + cap(r.stroke.pts) + cap(r.dash.out))
	return int64(n)
}

// Render draws frame n (0 is the first; it may be fractional) over all of
// dst, which it clears first. The composition keeps its aspect ratio,
// centered.
func (a *Animation) Render(dst *image.RGBA, n float64) {
	f := a.first + max(0, n)
	if f >= a.last {
		f = a.last - 1e-3
	}
	a.r.render(dst, a.root, f, float64(a.Width), float64(a.Height))
}
