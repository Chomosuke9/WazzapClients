// Package offheap makes pictures whose pixels live outside the Go heap.
//
// The GC lets the heap grow by GOGC percent of what is live before it
// collects, so every MB of pixels kept in the heap also makes room for
// another MB or more of garbage (two at the GC target of 200 Settings >
// Performance offers). Pixels kept here cost their own size and no more,
// and go back to the system once their picture is unreachable.
package offheap

import (
	"image"
	"runtime"
	"sync/atomic"
)

var inUse atomic.Int64

// NewRGBA returns a transparent picture of r whose pixels are outside the
// Go heap. They are freed once the returned *image.RGBA is unreachable, so
// never keep its Pix, or a picture made from it (SubImage), without it.
// Where memory can't be had that way, the pixels are in the heap.
func NewRGBA(r image.Rectangle) *image.RGBA {
	w, h := r.Dx(), r.Dy()
	if w <= 0 || h <= 0 {
		return image.NewRGBA(r)
	}
	n := 4 * w * h
	pix := alloc(n)
	if pix == nil {
		return image.NewRGBA(r)
	}
	inUse.Add(int64(n))
	m := &image.RGBA{Pix: pix, Stride: 4 * w, Rect: r}
	runtime.AddCleanup(m, func(pix []byte) {
		free(pix)
		inUse.Add(-int64(len(pix)))
	}, pix)
	return m
}

// InUse returns how many bytes of pixels NewRGBA holds outside the heap.
func InUse() int64 { return inUse.Load() }
