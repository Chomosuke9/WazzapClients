package ui

import (
	"bytes"
	"image"
	"image/draw"
	_ "image/jpeg"
	_ "image/png"
	"sync"
	"time"

	"gioui.org/f32"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	_ "golang.org/x/image/webp" // stickers

	"github.com/chomosuke9/wazzapclients/internal/webpanim"
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
	bytes int   // decoded size, counted in imageCache.bytes
	// animated is set for animated stickers; the image is their first frame.
	animated bool
	// empty is set when load returned nothing: the media isn't downloaded
	// yet, or the download failed. It is asked for again after retryAfter.
	empty    bool
	loadedAt time.Time
}

// retryAfter is how long missing media waits before it's asked for again.
// A download can fail without an event (offline, a timeout), and the
// backend only queues one when asked.
const retryAfter = 15 * time.Second

// imageCache decodes and downscales images off the UI goroutine. Decoded
// bitmaps are the biggest memory cost of a chat app, so entries are capped
// and evicted least-recently-used first, by count and by decoded size.
type imageCache struct {
	mu         sync.Mutex
	m          map[string]*imgEntry
	frame      int64
	limit      int // entries
	budget     int // decoded bytes
	bytes      int
	invalidate func()
}

func newImageCache(limit, budget int) *imageCache {
	return &imageCache{m: make(map[string]*imgEntry), limit: limit, budget: budget}
}

// decodeSlots limits how many images decode at once. A full-size photo
// takes tens of MB while it decodes, and opening a chat full of them
// would otherwise decode them all in parallel and grow the heap for good.
// A big picture takes every slot and decodes alone: Go's JPEG decoder
// keeps all of a progressive JPEG's coefficients (most WhatsApp photos
// are progressive), about 7.5 bytes a pixel, 93 MB for 12 megapixels.
var (
	decodeSlots = make(chan struct{}, 2)
	decodeBig   sync.Mutex // held while a big decode gathers its slots
)

// bigDecode is the pixel count from which a picture decodes alone.
const bigDecode = 4 << 20

// acquireDecode waits for decode slots for data, and returns how to give
// them back.
func acquireDecode(data []byte) (release func()) {
	n := 1
	if c, _, err := image.DecodeConfig(bytes.NewReader(data)); err == nil && c.Width*c.Height >= bigDecode {
		n = cap(decodeSlots)
		decodeBig.Lock()
		defer decodeBig.Unlock()
	}
	for range n {
		decodeSlots <- struct{}{}
	}
	return func() {
		for range n {
			<-decodeSlots
		}
	}
}

// get returns the entry for key, loading it in the background (load may
// block; it runs on its own goroutine) when it isn't cached yet.
func (c *imageCache) get(key string, maxSide int, load func() []byte) *imgEntry {
	c.mu.Lock()
	defer c.mu.Unlock()
	if e, ok := c.m[key]; ok {
		if !e.empty || time.Since(e.loadedAt) < retryAfter {
			e.used = c.frame
			return e
		}
		c.remove(key)
	}
	e := &imgEntry{state: imgLoading, used: c.frame}
	c.m[key] = e
	go func() {
		data := load()
		release := acquireDecode(data)
		img, animated := decodeScaled(data, maxSide)
		release()
		c.mu.Lock()
		e.loadedAt = time.Now()
		if img == nil {
			e.state, e.empty = imgMissing, len(data) == 0
		} else {
			e.state, e.op, e.size = imgReady, paint.NewImageOp(img), img.Bounds().Size()
			e.animated = animated
			if c.m[key] == e { // not forgotten meanwhile
				e.bytes = 4 * e.size.X * e.size.Y
				c.bytes += e.bytes
			}
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
	c.remove(key)
	c.mu.Unlock()
}

func (c *imageCache) remove(key string) {
	if e, ok := c.m[key]; ok {
		c.bytes -= e.bytes
		delete(c.m, key)
	}
}

// retryMissing drops media that wasn't there, so the next get asks the
// backend again. Called when the connection comes back.
func (c *imageCache) retryMissing() {
	c.mu.Lock()
	for k, e := range c.m {
		if e.empty {
			c.remove(k)
		}
	}
	c.mu.Unlock()
}

// endFrame evicts least recently used entries beyond the limits. Images
// drawn in the frame that just ended stay.
func (c *imageCache) endFrame() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.frame++
	for len(c.m) > c.limit || c.bytes > c.budget {
		oldest, key := c.frame-1, ""
		for k, e := range c.m {
			if e.state != imgLoading && e.used < oldest {
				oldest, key = e.used, k
			}
		}
		if key == "" {
			return
		}
		c.remove(key)
	}
}

// decodeScaled decodes an image to fit in maxSide. Animated WebP stickers
// decode to their first frame, and animated is set.
func decodeScaled(data []byte, maxSide int) (img image.Image, animated bool) {
	if len(data) == 0 {
		return nil, false
	}
	var src image.Image
	if webpanim.IsAnimated(data) {
		a, err := webpanim.Parse(data)
		if err != nil {
			return nil, false
		}
		if src, _, err = a.Next(); err != nil {
			return nil, false
		}
		animated = true
	} else {
		var err error
		if src, _, err = image.Decode(bytes.NewReader(data)); err != nil {
			return nil, false
		}
	}
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	if w == 0 || h == 0 {
		return nil, false
	}
	if s := max(w, h); s > maxSide {
		w, h = max(1, w*maxSide/s), max(1, h*maxSide/s)
	}
	if w != b.Dx() || h != b.Dy() {
		return shrink(src, w, h), animated
	}
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(dst, dst.Bounds(), src, b.Min, draw.Src)
	return dst, animated
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
