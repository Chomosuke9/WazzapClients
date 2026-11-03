package remote

import (
	"image"
	"image/color"
	"image/draw"
	"slices"
)

// quantize reduces img to at most 256 colors, picked by median cut and
// spread with Floyd-Steinberg dithering. A paletted picture is a quarter
// of the size on the watch, which matters at Bluetooth's few KB a second.
func quantize(img image.Image) *image.Paletted {
	b := img.Bounds()
	out := image.NewPaletted(b, palette(img, 256))
	draw.FloydSteinberg.Draw(out, b, img, b.Min)
	return out
}

// palette picks up to n colors for img by median cut: it splits the box
// of colors with the most pixels along its widest channel, at the median,
// until there are n boxes, and takes each box's average.
func palette(img image.Image, n int) color.Palette {
	b := img.Bounds()
	// Every pixel of a small picture, a sample of a big one.
	step := 1
	for (b.Dx()/step)*(b.Dy()/step) > 40000 {
		step++
	}
	var px [][3]uint8
	for y := b.Min.Y; y < b.Max.Y; y += step {
		for x := b.Min.X; x < b.Max.X; x += step {
			r, g, bl, _ := img.At(x, y).RGBA()
			px = append(px, [3]uint8{uint8(r >> 8), uint8(g >> 8), uint8(bl >> 8)})
		}
	}
	if len(px) == 0 {
		return color.Palette{color.Black}
	}
	boxes := [][][3]uint8{px}
	for len(boxes) < n {
		// The box with the most pixels that can still be split.
		bi, best := -1, 1
		for i, bx := range boxes {
			if len(bx) > best {
				if _, w := widest(bx); w > 0 {
					bi, best = i, len(bx)
				}
			}
		}
		if bi < 0 {
			break
		}
		bx := boxes[bi]
		ch, _ := widest(bx)
		slices.SortFunc(bx, func(p, q [3]uint8) int { return int(p[ch]) - int(q[ch]) })
		mid := len(bx) / 2
		boxes[bi] = bx[:mid]
		boxes = append(boxes, bx[mid:])
	}
	pal := make(color.Palette, 0, len(boxes))
	for _, bx := range boxes {
		var s [3]int
		for _, p := range bx {
			s[0] += int(p[0])
			s[1] += int(p[1])
			s[2] += int(p[2])
		}
		k := len(bx)
		pal = append(pal, color.RGBA{uint8(s[0] / k), uint8(s[1] / k), uint8(s[2] / k), 255})
	}
	return pal
}

// widest returns the channel whose values spread the most in px, and how much.
func widest(px [][3]uint8) (int, int) {
	lo := [3]uint8{255, 255, 255}
	var hi [3]uint8
	for _, p := range px {
		for c := range 3 {
			lo[c] = min(lo[c], p[c])
			hi[c] = max(hi[c], p[c])
		}
	}
	ch, w := 0, -1
	for c := range 3 {
		if d := int(hi[c]) - int(lo[c]); d > w {
			ch, w = c, d
		}
	}
	return ch, w
}
