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

	"github.com/chomosuke9/wazzapclients/internal/photo"
	"github.com/chomosuke9/wazzapclients/internal/webpanim"
)

type imgState int

const (
	imgLoading imgState = iota
	imgReady
	imgMissing
)

// imgEntry is an image of the cache. The UI reads one without the cache's
// lock for the rest of its frame, so an entry never changes once get
// returned it, but for used: a load puts a new entry in its place.
type imgEntry struct {
	state imgState
	op    paint.ImageOp
	size  image.Point
	used  int64 // frame counter of last use, for eviction; under imageCache.mu
	bytes int   // decoded size, counted in imageCache.bytes
	// animated is set for animated stickers; the image is their first frame.
	animated bool
	// live is set for a playing GIF's current frame (gifFrame), which
	// isn't cached: its bubble shows no play button.
	live bool
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

// resize sets the cache's budget, its entry limit growing with it. Any
// excess goes at the end of the frame.
func (c *imageCache) resize(budget int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.limit, c.budget = max(60, int(int64(240)*int64(budget)/(32<<20))), budget
}

// usage returns the decoded bytes held and the budget.
func (c *imageCache) usage() (held, budget int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.bytes, c.budget
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
	return c.getWith(key, load, func(data []byte) (image.Image, bool) { return decodeScaled(data, maxSide) })
}

// blurSide is how many pixels across a blurred picture keeps. Scaled up
// to its place, it shows colors and nothing more.
const blurSide = 12

// getBlurred is get for a picture blurred past recognition (privacy mode):
// decoded blurSide px across and softened, so scaling it up blurs it
// further. It costs a few hundred bytes.
func (c *imageCache) getBlurred(key string, load func() []byte) *imgEntry {
	return c.getWith("blur:"+key, load, func(data []byte) (image.Image, bool) {
		img, _ := decodeScaled(data, blurSide)
		if img == nil {
			return nil, false
		}
		return boxBlur(boxBlur(img)), false
	})
}

// boxBlur averages each pixel with its neighbors, the edges repeating.
func boxBlur(src image.Image) *image.RGBA {
	b := src.Bounds()
	dst := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	for y := 0; y < b.Dy(); y++ {
		for x := 0; x < b.Dx(); x++ {
			var r, g, bl, a uint32
			for dy := -1; dy <= 1; dy++ {
				for dx := -1; dx <= 1; dx++ {
					sx := min(max(x+dx, 0), b.Dx()-1)
					sy := min(max(y+dy, 0), b.Dy()-1)
					pr, pg, pb, pa := src.At(b.Min.X+sx, b.Min.Y+sy).RGBA()
					r, g, bl, a = r+pr, g+pg, bl+pb, a+pa
				}
			}
			i := dst.PixOffset(x, y)
			dst.Pix[i], dst.Pix[i+1], dst.Pix[i+2], dst.Pix[i+3] = uint8(r/9>>8), uint8(g/9>>8), uint8(bl/9>>8), uint8(a/9>>8)
		}
	}
	return dst
}

// getWith is get with its own way to decode the loaded data.
func (c *imageCache) getWith(key string, load func() []byte, decode func([]byte) (image.Image, bool)) *imgEntry {
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
		img, animated := decode(data)
		release()
		// Not into e, which the UI may be reading: into the entry that
		// replaces it.
		done := &imgEntry{state: imgMissing, empty: len(data) == 0, loadedAt: time.Now()}
		if img != nil {
			done = &imgEntry{state: imgReady, op: paint.NewImageOp(img), size: img.Bounds().Size(),
				animated: animated, loadedAt: done.loadedAt}
			done.bytes = 4 * done.size.X * done.size.Y
		}
		c.mu.Lock()
		if c.m[key] == e { // not forgotten meanwhile
			done.used = e.used
			c.m[key] = done
			c.bytes += done.bytes
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
		return photo.Shrink(src, w, h), animated
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
