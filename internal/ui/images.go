package ui

import (
	"bytes"
	"image"
	"image/draw"
	_ "image/jpeg"
	_ "image/png"
	"sync"

	"gioui.org/f32"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	xdraw "golang.org/x/image/draw"
	_ "golang.org/x/image/webp" // stickers
)

type imgState int

const (
	imgLoading imgState = iota
	imgReady
	imgMissing
)

type imgEntry struct {
	state imgState
	op    paint.ImageOp
	size  image.Point
	used  int64 // frame counter of last use, for eviction
}

// imageCache decodes and downscales images off the UI goroutine. Decoded
// bitmaps are the biggest memory cost of a chat app, so entries are capped
// and evicted least-recently-used first.
type imageCache struct {
	mu         sync.Mutex
	m          map[string]*imgEntry
	frame      int64
	limit      int
	invalidate func()
}

func newImageCache(limit int) *imageCache {
	return &imageCache{m: make(map[string]*imgEntry), limit: limit}
}

// get returns the entry for key, loading it in the background (load may
// block; it runs on its own goroutine) when it isn't cached yet.
func (c *imageCache) get(key string, maxSide int, load func() []byte) *imgEntry {
	c.mu.Lock()
	defer c.mu.Unlock()
	if e, ok := c.m[key]; ok {
		e.used = c.frame
		return e
	}
	e := &imgEntry{state: imgLoading, used: c.frame}
	c.m[key] = e
	go func() {
		img := decodeScaled(load(), maxSide)
		c.mu.Lock()
		if img == nil {
			e.state = imgMissing
		} else {
			e.state, e.op, e.size = imgReady, paint.NewImageOp(img), img.Bounds().Size()
		}
		c.mu.Unlock()
		if c.invalidate != nil {
			c.invalidate()
		}
	}()
	return e
}

// forget drops key so the next get reloads it.
func (c *imageCache) forget(key string) {
	c.mu.Lock()
	delete(c.m, key)
	c.mu.Unlock()
}

// endFrame evicts least recently used entries beyond the limit.
func (c *imageCache) endFrame() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.frame++
	for len(c.m) > c.limit {
		oldest, key := c.frame, ""
		for k, e := range c.m {
			if e.state != imgLoading && e.used < oldest {
				oldest, key = e.used, k
			}
		}
		if key == "" {
			return
		}
		delete(c.m, key)
	}
}

func decodeScaled(data []byte, maxSide int) image.Image {
	if len(data) == 0 {
		return nil
	}
	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil
	}
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	if w == 0 || h == 0 {
		return nil
	}
	if s := max(w, h); s > maxSide {
		w, h = max(1, w*maxSide/s), max(1, h*maxSide/s)
	}
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	if w == b.Dx() && h == b.Dy() {
		draw.Draw(dst, dst.Bounds(), src, b.Min, draw.Src)
	} else {
		xdraw.CatmullRom.Scale(dst, dst.Bounds(), src, b, draw.Src, nil)
	}
	return dst
}

// paintCover paints img scaled to cover r (like CSS object-fit: cover),
// clipped to the current clip.
func paintCover(gtx C, img paint.ImageOp, size image.Point, r image.Rectangle) {
	defer clip.Rect(r).Push(gtx.Ops).Pop()
	s := max(float32(r.Dx())/float32(size.X), float32(r.Dy())/float32(size.Y))
	off := f32.Pt(
		float32(r.Min.X)+(float32(r.Dx())-float32(size.X)*s)/2,
		float32(r.Min.Y)+(float32(r.Dy())-float32(size.Y)*s)/2,
	)
	defer op.Affine(f32.AffineId().Scale(f32.Point{}, f32.Pt(s, s)).Offset(off)).Push(gtx.Ops).Pop()
	img.Filter = paint.FilterLinear
	img.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
}
