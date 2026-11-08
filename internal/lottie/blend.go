package lottie

import "math"

// Blend modes, as Lottie numbers them.
const (
	blendNormal = iota
	blendMultiply
	blendScreen
	blendOverlay
	blendDarken
	blendLighten
	blendColorDodge
	blendColorBurn
	blendHardLight
	blendSoftLight
	blendDifference
	blendExclusion
	blendHue
	blendSaturation
	blendColor
	blendLuminosity
	blendAdd
)

// blendable reports whether a layer's blend mode is drawn (the separable
// modes and add; hue, saturation, color and luminosity draw normally).
func blendable(mode int) bool {
	return mode > blendNormal && (mode <= blendExclusion || mode == blendAdd)
}

// blend draws src over b at opacity op with a blend mode, as CSS's
// mix-blend-mode does (lottie-web's SVG renderer uses it).
func (b *layerBuf) blend(src *layerBuf, op float32, mode int) {
	d := src.dirty
	if d.Empty() || op <= 0 {
		return
	}
	for y := d.Min.Y; y < d.Max.Y; y++ {
		i := (y*b.w + d.Min.X) * 4
		s := src.pix[i : i+d.Dx()*4]
		t := b.pix[i : i+d.Dx()*4]
		for j := 0; j < len(s); j += 4 {
			if s[j+3] == 0 {
				continue
			}
			sa := float32(s[j+3]) / 255 * op
			da := float32(t[j+3]) / 255
			for c := 0; c < 3; c++ {
				// Premultiplied, 0..1.
				sp := float32(s[j+c]) / 255 * op
				dp := float32(t[j+c]) / 255
				var sc, dc float32
				if sa > 0 {
					sc = sp / sa
				}
				if da > 0 {
					dc = dp / da
				}
				v := (1-sa)*dp + (1-da)*sp + sa*da*blendChannel(mode, dc, sc)
				t[j+c] = u8(v * 255)
			}
			t[j+3] = u8((sa + da - sa*da) * 255)
		}
	}
	b.touch(d)
}

// blendChannel blends a straight color channel: d below, s above.
func blendChannel(mode int, d, s float32) float32 {
	switch mode {
	case blendMultiply:
		return d * s
	case blendScreen:
		return d + s - d*s
	case blendOverlay:
		return hardLight(s, d)
	case blendDarken:
		return min(d, s)
	case blendLighten:
		return max(d, s)
	case blendColorDodge:
		switch {
		case d == 0:
			return 0
		case s >= 1:
			return 1
		}
		return min(1, d/(1-s))
	case blendColorBurn:
		switch {
		case d >= 1:
			return 1
		case s <= 0:
			return 0
		}
		return 1 - min(1, (1-d)/s)
	case blendHardLight:
		return hardLight(d, s)
	case blendSoftLight:
		if s <= 0.5 {
			return d - (1-2*s)*d*(1-d)
		}
		var g float32
		if d <= 0.25 {
			g = ((16*d-12)*d + 4) * d
		} else {
			g = float32(math.Sqrt(float64(d)))
		}
		return d + (2*s-1)*(g-d)
	case blendDifference:
		if d > s {
			return d - s
		}
		return s - d
	case blendExclusion:
		return d + s - 2*d*s
	case blendAdd:
		return min(1, d+s)
	}
	return s
}

func hardLight(d, s float32) float32 {
	if s <= 0.5 {
		return d * 2 * s
	}
	return d + (2*s - 1) - d*(2*s-1)
}
