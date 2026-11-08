package ui

import (
	"bytes"
	"fmt"
	"image"
	"image/draw"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"gioui.org/f32"
	"gioui.org/io/event"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"

	"github.com/chomosuke9/wazzapclients/internal/photo"
)

// A photo wallpaper covers the conversation like CSS's object-fit: cover,
// cropped around a point of the photo the user picks (the Photo position
// page), which stays in view at any window size: in the middle, unless
// that would show past the photo's edge.
//
// Only the part that shows is decoded, at the size it shows, so the photo
// is sharp and costs no more than a pane of pixels. While the pane changes
// size or the point is dragged, the part decoded before is drawn where it
// still fits, over the whole photo decoded small; once the pane stays put
// for wallSettle, the new part decodes.

// wallSettle is how long the pane's size and the focus stay put before the
// part they show is decoded again.
const wallSettle = 200 * time.Millisecond

// wallThumbSide is the size the whole photo decodes at, for the tiles of
// the Wallpaper page and under a part that doesn't cover the pane yet.
const wallThumbSide = 512

// wallFocusCenter is a photo's focus until one is picked.
var wallFocusCenter = f32.Pt(0.5, 0.5)

// parsePhotoWall splits a photo wallpaper's argument, "<file>" or
// "<file>@<x>,<y>" with the focus in thousandths of the photo's width and
// height, into the file and the focus.
func parsePhotoWall(arg string) (file string, focus f32.Point) {
	file, at, ok := strings.Cut(arg, "@")
	focus = wallFocusCenter
	if !ok {
		return file, focus
	}
	xs, ys, _ := strings.Cut(at, ",")
	x, err1 := strconv.Atoi(xs)
	y, err2 := strconv.Atoi(ys)
	if err1 == nil && err2 == nil {
		focus = clampFocus(f32.Pt(float32(x)/1000, float32(y)/1000))
	}
	return file, focus
}

// photoWallValue is the wallpaper value of photo file with its focus.
func photoWallValue(file string, focus f32.Point) string {
	f := clampFocus(focus)
	return fmt.Sprintf("photo:%s@%d,%d", file, int(f.X*1000+0.5), int(f.Y*1000+0.5))
}

func clampFocus(f f32.Point) f32.Point {
	return f32.Pt(min(1, max(0, f.X)), min(1, max(0, f.Y)))
}

// viewRect is a part of a photo, in its pixels.
type viewRect struct{ x, y, w, h float32 }

// wallView returns the part of a photo of size src that covers a pane of
// size pane, its focus as near the middle as the photo allows, and the
// scale from the photo's pixels to the pane's.
func wallView(src, pane image.Point, focus f32.Point) (v viewRect, s float32) {
	sw, sh := float32(src.X), float32(src.Y)
	s = max(float32(pane.X)/sw, float32(pane.Y)/sh)
	v.w, v.h = float32(pane.X)/s, float32(pane.Y)/s
	v.x = min(max(focus.X*sw-v.w/2, 0), sw-v.w)
	v.y = min(max(focus.Y*sh-v.h/2, 0), sh-v.h)
	return v, s
}

// paintPart paints img, the part crop of a photo decoded at img's size,
// where the photo's part v shows in r at scale s.
func paintPart(gtx C, img paint.ImageOp, size image.Point, crop image.Rectangle, v viewRect, s float32, r image.Rectangle) {
	defer clip.Rect(r).Push(gtx.Ops).Pop()
	sc := f32.Pt(float32(crop.Dx())*s/float32(size.X), float32(crop.Dy())*s/float32(size.Y))
	off := f32.Pt(float32(r.Min.X)+(float32(crop.Min.X)-v.x)*s, float32(r.Min.Y)+(float32(crop.Min.Y)-v.y)*s)
	defer op.Affine(f32.AffineId().Scale(f32.Point{}, sc).Offset(off)).Push(gtx.Ops).Pop()
	img.Filter = paint.FilterLinear
	img.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
}

// paintFocused paints a whole photo, decoded at size, to cover r around
// focus.
func paintFocused(gtx C, img paint.ImageOp, size image.Point, r image.Rectangle, focus f32.Point) {
	v, s := wallView(size, r.Size(), focus)
	paintPart(gtx, img, size, image.Rectangle{Max: size}, v, s, r)
}

// photoWall is the photo wallpaper drawn last.
type photoWall struct {
	path string
	src  image.Point // the photo's size; zero when it couldn't be read
	// The part decoded, once one is: its cache key and where in the photo.
	key  string
	crop image.Rectangle
	size image.Point
	// pending is the key of the part loading.
	pending string
	// The part the pane showed last, its size there and since when.
	want     image.Rectangle
	wantSize image.Point
	since    time.Time
}

// photoSize reads a photo's size from its header.
func photoSize(path string) image.Point {
	f, err := os.Open(path)
	if err != nil {
		return image.Point{}
	}
	defer f.Close()
	c, _, err := image.DecodeConfig(f)
	if err != nil {
		return image.Point{}
	}
	return image.Pt(c.Width, c.Height)
}

// wallThumb returns the whole photo at path, decoded small.
func (u *UI) wallThumb(path string) *imgEntry {
	return u.images.get("wall:"+path, wallThumbSide, func() []byte {
		b, _ := os.ReadFile(path)
		return b
	})
}

// wallPart returns part crop of the photo at path, decoded at size.
func (u *UI) wallPart(path string, crop image.Rectangle, size image.Point) (string, *imgEntry) {
	key := fmt.Sprintf("wallpart:%s@%d,%d,%d,%d:%dx%d", path, crop.Min.X, crop.Min.Y, crop.Max.X, crop.Max.Y, size.X, size.Y)
	return key, u.images.getWith(key, func() []byte {
		b, _ := os.ReadFile(path)
		return b
	}, func(data []byte) (image.Image, bool) {
		return decodeWallPart(data, crop, size), false
	})
}

// decodeWallPart decodes part crop of a photo, scaled down to size.
func decodeWallPart(data []byte, crop image.Rectangle, size image.Point) image.Image {
	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil
	}
	b := src.Bounds()
	crop = crop.Add(b.Min).Intersect(b)
	sub, ok := src.(interface {
		SubImage(image.Rectangle) image.Image
	})
	if crop.Empty() || !ok {
		return nil
	}
	part := sub.SubImage(crop)
	w, h := min(size.X, crop.Dx()), min(size.Y, crop.Dy())
	if w == crop.Dx() && h == crop.Dy() {
		dst := image.NewRGBA(image.Rect(0, 0, w, h))
		draw.Draw(dst, dst.Bounds(), part, crop.Min, draw.Src)
		return dst
	}
	return photo.Shrink(part, max(1, w), max(1, h))
}

// layoutWallPhoto covers the pane with the photo at path around focus. It
// reports false when nothing of the photo is ready to draw.
func (u *UI) layoutWallPhoto(gtx C, path string, focus f32.Point) bool {
	sz := gtx.Constraints.Max
	w := &u.themeUI.wall
	if w.path != path {
		u.dropWallPhoto()
		*w = photoWall{path: path, src: photoSize(path)}
	}
	if w.src.X <= 0 || w.src.Y <= 0 || sz.X <= 0 || sz.Y <= 0 {
		return false
	}
	r := image.Rectangle{Max: sz}
	v, s := wallView(w.src, sz, focus)
	// The part of the photo that shows, in whole pixels, and its size on
	// screen (never more than the photo has).
	want := image.Rect(int(v.x), int(v.y), int(math.Ceil(float64(v.x+v.w))), int(math.Ceil(float64(v.y+v.h)))).
		Intersect(image.Rectangle{Max: w.src})
	wantSize := image.Pt(min(want.Dx(), int(math.Ceil(float64(float32(want.Dx())*s)))),
		min(want.Dy(), int(math.Ceil(float64(float32(want.Dy())*s)))))
	if want != w.want || wantSize != w.wantSize {
		w.want, w.wantSize, w.since = want, wantSize, gtx.Now
	}

	var part *imgEntry
	if w.key != "" {
		_, part = u.wallPart(path, w.crop, w.size)
	}
	fits := part != nil && part.state == imgReady && w.crop.Intersect(want) == want &&
		float32(part.size.X) >= 0.97*float32(w.crop.Dx())*min(1, s)
	// A part much bigger than the pane wants (it was bigger, or showed more)
	// is decoded again too, to give the memory back.
	wasteful := fits && (w.crop.Dx()*w.crop.Dy() > want.Dx()*want.Dy()*3/2 ||
		float32(part.size.X) > 1.3*float32(w.crop.Dx())*s)
	if !fits || wasteful {
		settled := gtx.Now.Sub(w.since) >= wallSettle && !u.themeUI.drag
		if settled || w.key == "" && !u.themeUI.drag {
			key, e := u.wallPart(path, want, wantSize)
			if w.pending != "" && w.pending != key {
				u.images.forget(w.pending)
			}
			w.pending = key
			if e.state == imgReady {
				if w.key != "" && w.key != key {
					u.images.forget(w.key)
				}
				w.key, w.crop, w.size, w.pending, part, fits = key, want, wantSize, "", e, true
			}
		} else {
			gtx.Execute(op.InvalidateCmd{At: w.since.Add(wallSettle)})
		}
	} else if w.pending != "" {
		u.images.forget(w.pending) // the pane went back to what's decoded
		w.pending = ""
	}
	covered := fits
	if !covered {
		// The whole photo, small, under what the part leaves out.
		if t := u.wallThumb(path); t.state == imgReady {
			paintFocused(gtx, t.op, t.size, r, focus)
			covered = true
		}
	}
	if part != nil && part.state == imgReady {
		paintPart(gtx, part.op, part.size, w.crop, v, s, r)
		covered = true
	}
	return covered
}

// dropWallPhoto lets go of the photo wallpaper's decoded part.
func (u *UI) dropWallPhoto() {
	w := &u.themeUI.wall
	for _, k := range []string{w.key, w.pending} {
		if k != "" {
			u.images.forget(k)
		}
	}
	*w = photoWall{}
}

// wallPosRows is the Photo position page: the photo whole, the part this
// window's chat shows framed in it, and the point that stays in view,
// which a click or a drag moves.
func (u *UI) wallPosRows(gtx C, id string, w int) []layout.Widget {
	t := &u.themeUI
	kind, arg, _ := strings.Cut(u.lookOf(id).wall, ":")
	if kind != "photo" || wallpaperDir == "" {
		t.page = "wall"
		return nil
	}
	file, focus := parsePhotoWall(arg)
	path := filepath.Join(wallpaperDir, filepath.Base(file))
	for {
		ev, ok := gtx.Event(pointer.Filter{Target: &t.drag, Kinds: pointer.Press | pointer.Drag | pointer.Release | pointer.Cancel})
		if !ok {
			break
		}
		e, ok := ev.(pointer.Event)
		if !ok || t.posSize.X <= 0 || t.posSize.Y <= 0 {
			continue
		}
		f := clampFocus(f32.Pt(e.Position.X/float32(t.posSize.X), e.Position.Y/float32(t.posSize.Y)))
		switch e.Kind {
		case pointer.Press:
			t.drag, t.dragPath, t.dragFocus = true, path, f
		case pointer.Drag:
			if t.drag {
				t.dragFocus = f
			}
		case pointer.Release:
			if t.drag {
				t.drag = false
				u.setWall(id, photoWallValue(file, t.dragFocus))
			}
		case pointer.Cancel:
			t.drag = false
		}
	}
	if t.drag {
		focus = t.dragFocus
	}
	pane := t.pane
	if pane.X <= 0 || pane.Y <= 0 {
		pane = image.Pt(1100, 800) // no chat open: a window's usual shape
	}
	return []layout.Widget{
		func(gtx C) D {
			boxH := gtx.Dp(420)
			e := u.images.get("wallpos:"+path, 1024, func() []byte {
				b, _ := os.ReadFile(path)
				return b
			})
			if e.state != imgReady {
				return D{Size: image.Pt(w, boxH)}
			}
			k := min(float32(w)/float32(e.size.X), float32(boxH)/float32(e.size.Y))
			dw, dh := int(float32(e.size.X)*k), int(float32(e.size.Y)*k)
			t.posSize = image.Pt(dw, dh)
			defer op.Offset(image.Pt((w-dw)/2, (boxH-dh)/2)).Push(gtx.Ops).Pop()
			box := image.Rect(0, 0, dw, dh)
			paintPart(gtx, e.op, e.size, image.Rectangle{Max: e.size}, viewRect{w: float32(e.size.X), h: float32(e.size.Y)}, k, box)
			// What this window's chat shows: the rest dimmed, the part framed.
			v, _ := wallView(t.posSize, pane, focus)
			fr := image.Rect(int(v.x+0.5), int(v.y+0.5), int(v.x+v.w+0.5), int(v.y+v.h+0.5))
			dim := argb(0x000000, 0xa0)
			fillRect(gtx, image.Rect(0, 0, dw, fr.Min.Y), dim)
			fillRect(gtx, image.Rect(0, fr.Max.Y, dw, dh), dim)
			fillRect(gtx, image.Rect(0, fr.Min.Y, fr.Min.X, fr.Max.Y), dim)
			fillRect(gtx, image.Rect(fr.Max.X, fr.Min.Y, dw, fr.Max.Y), dim)
			bw := float32(gtx.Dp(2))
			strokeRect(gtx, float32(fr.Min.X)+bw/2, float32(fr.Min.Y)+bw/2, float32(fr.Max.X)-bw/2, float32(fr.Max.Y)-bw/2, bw, rgb(0xffffff))
			c := image.Pt(int(focus.X*float32(dw)), int(focus.Y*float32(dh)))
			// A ring, the photo showing through it.
			rr := gtx.Dp(10)
			ring := clip.Ellipse{Min: c.Sub(image.Pt(rr, rr)), Max: c.Add(image.Pt(rr, rr))}
			paint.FillShape(gtx.Ops, argb(0x000000, 0x70), clip.Stroke{Path: ring.Path(gtx.Ops), Width: float32(gtx.Dp(4))}.Op())
			paint.FillShape(gtx.Ops, rgb(0xffffff), clip.Stroke{Path: ring.Path(gtx.Ops), Width: float32(gtx.Dp(2))}.Op())
			func() {
				defer clip.Rect(box).Push(gtx.Ops).Pop()
				pointer.CursorCrosshair.Add(gtx.Ops)
				event.Op(gtx.Ops, &t.drag)
			}()
			return D{Size: image.Pt(w, boxH)}
		},
		func(gtx C) D {
			return layout.Inset{Top: 12, Bottom: 8}.Layout(gtx,
				u.label(13.5, "Click or drag to choose what stays in view. The frame is what this window's chat shows; at any size, the point stays in view.",
					u.pal.TextSecondary, labelOpts{maxLines: 3}).Layout)
		},
	}
}
