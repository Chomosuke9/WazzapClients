package ui

import (
	"image"
	"image/color"
	"image/draw"
	"math/rand"

	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"golang.org/x/exp/shiny/iconvg"
)

// wallpaper is the doodle pattern behind conversations. The tile is
// rasterized once per color/scale and then repeated, so a frame costs a
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

func (wp *wallpaper) layout(gtx C, bg, doodle color.NRGBA) D {
	sz := gtx.Constraints.Max
	fillRect(gtx, image.Rectangle{Max: sz}, bg)

	cell := gtx.Dp(56)
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

// renderTile scatters doodle icons on a jittered grid. The doodle color is
// opaque and pre-mixed with the background: Gio blends in linear space, so a
// translucent overlay would look much stronger than it does in WhatsApp.
func renderTile(col color.NRGBA, cell int) (paint.ImageOp, int) {
	const n = 6
	size := cell * n
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	rng := rand.New(rand.NewSource(7))
	var pal iconvg.Palette
	pal[0] = color.RGBA{R: col.R, G: col.G, B: col.B, A: 0xff}
	var z iconvg.Rasterizer
	i := 0
	for gy := 0; gy < n; gy++ {
		for gx := 0; gx < n; gx++ {
			data := doodleIcons[(i*7+gy)%len(doodleIcons)]
			i++
			isz := cell*5/10 + rng.Intn(cell/5)
			slack := cell - isz
			x := gx*cell + rng.Intn(slack+1)
			y := gy*cell + rng.Intn(slack+1)
			z.SetDstImage(img, image.Rect(x, y, x+isz, y+isz), draw.Over)
			_ = iconvg.Decode(&z, data, &iconvg.DecodeOptions{Palette: &pal})
		}
	}
	return paint.NewImageOp(img), size
}
