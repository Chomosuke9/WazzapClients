package ui

import (
	"image"
	"image/color"
	"math"
	"math/rand"

	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"golang.org/x/image/vector"

	"github.com/chomosuke9/wazzapclients/internal/ui/icon"
)

// wallpaper is the doodle pattern behind conversations. The tile is
// rasterized once per color and scale, then repeated, so a frame costs a
// handful of image draws.
type wallpaper struct {
	key  wallKey
	tile paint.ImageOp
	size int
}

type wallKey struct {
	col  color.NRGBA
	cell int
}

// layout fills the area with bg and, with doodles, the doodle pattern.
func (wp *wallpaper) layout(gtx C, bg, doodle color.NRGBA, doodles bool) D {
	sz := gtx.Constraints.Max
	fillRect(gtx, image.Rectangle{Max: sz}, bg)
	if !doodles {
		*wp = wallpaper{} // lets the tile go
		return D{Size: sz}
	}

	cell := gtx.Dp(62)
	k := wallKey{doodle, cell}
	if wp.key != k {
		wp.key = k
		wp.tile, wp.size = renderTile(doodle, cell)
	}
	defer clip.Rect{Max: sz}.Push(gtx.Ops).Pop()
	for y := 0; y < sz.Y; y += wp.size {
		for x := 0; x < sz.X; x += wp.size {
			t := op.Offset(image.Pt(x, y)).Push(gtx.Ops)
			wp.tile.Add(gtx.Ops)
			c := clip.Rect{Max: image.Pt(wp.size, wp.size)}.Push(gtx.Ops)
			paint.PaintOp{}.Add(gtx.Ops)
			c.Pop()
			t.Pop()
		}
	}
	return D{Size: sz}
}

// renderTile scatters thin-line doodles on a jittered grid, with small rings
// and dots in the gaps, like WhatsApp's wallpaper. Glyphs that cross the tile
// edge are drawn again on the opposite side so the tile repeats seamlessly.
func renderTile(col color.NRGBA, cell int) (paint.ImageOp, int) {
	const n = 7
	size := cell * n
	z := vector.NewRasterizer(size, size)
	rng := rand.New(rand.NewSource(11))
	fs := float32(size)
	wrap := func(draw func(dx, dy float32)) {
		for _, dx := range []float32{-fs, 0, fs} {
			for _, dy := range []float32{-fs, 0, fs} {
				draw(dx, dy)
			}
		}
	}
	for gy := 0; gy < n; gy++ {
		for gx := 0; gx < n; gx++ {
			ic := icon.Doodles[rng.Intn(len(icon.Doodles))]
			isz := float32(cell) * (0.75 + 0.45*rng.Float32())
			x := float32(gx*cell) + rng.Float32()*(float32(cell)-isz*0.6)
			y := float32(gy*cell) + rng.Float32()*(float32(cell)-isz*0.6)
			angle := (rng.Float32() - 0.5) * 0.9
			wrap(func(dx, dy float32) { ic.Rasterize(z, x+dx, y+dy, isz, angle) })

			// Filler: a small ring or dot somewhere in the cell.
			cx := float32(gx*cell) + rng.Float32()*float32(cell)
			cy := float32(gy*cell) + rng.Float32()*float32(cell)
			r := float32(cell) * (0.03 + 0.03*rng.Float32())
			ring := rng.Intn(2) == 0
			wrap(func(dx, dy float32) {
				circle(z, cx+dx, cy+dy, r, false)
				if ring {
					circle(z, cx+dx, cy+dy, r-float32(cell)*0.018, true)
				}
			})
		}
	}
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	z.Draw(img, img.Bounds(), image.NewUniform(col), image.Point{})
	return paint.NewImageOp(img), size
}

// circle adds a circle contour to z; reverse winding cuts a hole.
func circle(z *vector.Rasterizer, cx, cy, r float32, reverse bool) {
	const steps = 16
	dir := float32(1)
	if reverse {
		dir = -1
	}
	z.MoveTo(cx+r, cy)
	for i := 1; i <= steps; i++ {
		a := dir * float32(i) * 2 * math.Pi / steps
		z.LineTo(cx+r*float32(math.Cos(float64(a))), cy+r*float32(math.Sin(float64(a))))
	}
	z.ClosePath()
}
