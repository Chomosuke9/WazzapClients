package mock

import (
	"bytes"
	"hash/fnv"
	"image"
	"image/color"
	"image/jpeg"
	"math"
)

// noPicture lists the demo chats and people without a profile picture, so
// the placeholders show too.
var noPicture = map[string]bool{
	"rina": true, "courier": true, "gym": true, "landlord": true, "bola@newsletter": true,
	"uni": true, "jobs": true, "design": true, // groups in communities, to show the group placeholder there
}

// Avatar returns a generated profile picture for id: a soft gradient with
// a shape on it, the same every time. Some IDs have none (see noPicture),
// and the reference data has no pictures at all.
func (b *Backend) Avatar(id string) []byte {
	if !b.pics || id == "" || noPicture[id] {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if pic, ok := b.avatars[id]; ok {
		return pic
	}
	if b.avatars == nil {
		b.avatars = make(map[string][]byte)
	}
	pic := avatarJPEG(id)
	b.avatars[id] = pic
	return pic
}

// avatarJPEG draws id's picture: two hues from its hash, and one of three
// motifs (a landscape, rings, stripes or a flat logo). None is a person,
// which the placeholder already is.
func avatarJPEG(id string) []byte {
	const n = 160
	h := fnv.New32a()
	h.Write([]byte(id))
	sum := h.Sum32()
	hue := float64(sum%360) / 360
	ca := hsl(hue, 0.55, 0.62)
	cb := hsl(math.Mod(hue+0.12, 1), 0.60, 0.42)
	light := color.RGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}
	img := image.NewRGBA(image.Rect(0, 0, n, n))
	motif := (sum / 360) % 4
	for y := 0; y < n; y++ {
		for x := 0; x < n; x++ {
			t := float64(x+y) / (2 * n)
			c := mixRGBA(ca, cb, t)
			if motif == 3 { // a flat logo: a dark disc on a pale ground
				c = hsl(hue, 0.12, 0.93)
			}
			fx, fy := float64(x)/n, float64(y)/n
			var cover float64
			switch motif {
			case 0: // a sun over hills
				hills := clamp01((fy-(0.66+0.08*math.Sin(fx*7)))/0.01 + 0.5)
				cover = math.Max(disc(fx, fy, 0.68, 0.32, 0.13), hills*0.7)
			case 1: // rings
				d := math.Hypot(fx-0.5, fy-0.5)
				cover = math.Max(ring(d, 0.16, 0.04), ring(d, 0.34, 0.04))
			case 3:
				c = mixRGBA(c, cb, disc(fx, fy, 0.5, 0.5, 0.3))
			default: // stripes
				cover = math.Max(0, math.Sin((fx+fy)*18)) * 0.35
			}
			img.SetRGBA(x, y, mixRGBA(c, light, cover*0.78))
		}
	}
	var buf bytes.Buffer
	_ = jpeg.Encode(&buf, img, &jpeg.Options{Quality: 82})
	return buf.Bytes()
}

// disc is 1 inside the circle at (cx, cy) with radius r, fading out over
// its edge (all in picture fractions).
func disc(x, y, cx, cy, r float64) float64 {
	return clamp01((r-math.Hypot(x-cx, y-cy))/0.01 + 0.5)
}

// ring is 1 within w of radius r from the center.
func ring(d, r, w float64) float64 {
	return clamp01((w-math.Abs(d-r))/0.01 + 0.5)
}

func clamp01(v float64) float64 { return math.Max(0, math.Min(1, v)) }

func mixRGBA(a, b color.RGBA, t float64) color.RGBA {
	return color.RGBA{
		R: uint8(float64(a.R)*(1-t) + float64(b.R)*t),
		G: uint8(float64(a.G)*(1-t) + float64(b.G)*t),
		B: uint8(float64(a.B)*(1-t) + float64(b.B)*t),
		A: 0xff,
	}
}

// hsl converts a hue, saturation and lightness (each 0 to 1) to a color.
func hsl(h, s, l float64) color.RGBA {
	c := (1 - math.Abs(2*l-1)) * s
	x := c * (1 - math.Abs(math.Mod(h*6, 2)-1))
	m := l - c/2
	var r, g, bl float64
	switch int(h * 6) {
	case 0:
		r, g, bl = c, x, 0
	case 1:
		r, g, bl = x, c, 0
	case 2:
		r, g, bl = 0, c, x
	case 3:
		r, g, bl = 0, x, c
	case 4:
		r, g, bl = x, 0, c
	default:
		r, g, bl = c, 0, x
	}
	return color.RGBA{R: uint8((r + m) * 255), G: uint8((g + m) * 255), B: uint8((bl + m) * 255), A: 0xff}
}
