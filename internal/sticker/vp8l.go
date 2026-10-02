package sticker

import "encoding/binary"

// A lossless WebP (VP8L) encoder: subtract-green and predictor transforms,
// LZ77 backward references and one group of prefix codes. The bitstream is
// described in https://developers.google.com/speed/webp/docs/webp_lossless_bitstream_specification.
//
// quant drops that many low bits of each color (not alpha) first, which
// makes a photo smaller at the cost of some precision. It then predicts
// only with modes whose predictions are colors of the image, so the
// residuals keep the dropped bits zero.

// Transform types.
const (
	transformPredictor     = 0
	transformSubtractGreen = 2
)

// The alphabets of the five prefix codes.
const (
	numLiterals = 256
	numLengths  = 24
	numDists    = 40
)

// predictorBits is the log2 size of a predictor block.
const predictorBits = 4

// encodeWebP encodes non-premultiplied ARGB pixels, w by h, as a lossless
// WebP file.
func encodeWebP(argb []uint32, w, h int, quant uint) []byte {
	pix := append([]uint32(nil), argb...)
	alpha := false
	mask := 0xff000000 | uint32(0xff>>quant<<quant)*0x010101
	for i, c := range pix {
		if c>>24 != 0xff {
			alpha = true
		}
		if c>>24 == 0 {
			c = 0 // its color can't be seen: make it compress
		}
		pix[i] = c & mask
	}
	var bw bitWriter
	bw.write(0x2f, 8)
	bw.write(uint32(w-1), 14)
	bw.write(uint32(h-1), 14)
	if alpha {
		bw.write(1, 1)
	} else {
		bw.write(0, 1)
	}
	bw.write(0, 3) // version

	// Subtract green, then predict. The decoder undoes them backwards.
	bw.write(1, 1)
	bw.write(transformSubtractGreen, 2)
	for i, c := range pix {
		g := c >> 8 & 0xff
		r := (c>>16 - g) & 0xff
		b := (c - g) & 0xff
		pix[i] = c&0xff00ff00 | r<<16 | b
	}
	bw.write(1, 1)
	bw.write(transformPredictor, 2)
	bw.write(predictorBits-2, 3)
	modes, res := predict(pix, w, h, quant > 0)
	tw := tiles(w)
	sub := make([]uint32, len(modes))
	for i, m := range modes {
		sub[i] = 0xff000000 | uint32(m)<<8
	}
	writeImage(&bw, sub, tw, false)
	bw.write(0, 1) // no more transforms

	writeImage(&bw, res, w, true)
	data := bw.bytes()

	n := len(data)
	out := make([]byte, 0, 20+n+1)
	out = append(out, "RIFF"...)
	out = binary.LittleEndian.AppendUint32(out, uint32(4+8+n+n&1))
	out = append(out, "WEBPVP8L"...)
	out = binary.LittleEndian.AppendUint32(out, uint32(n))
	out = append(out, data...)
	if n&1 != 0 {
		out = append(out, 0)
	}
	return out
}

func tiles(n int) int { return (n + 1<<predictorBits - 1) >> predictorBits }

// predict chooses a predictor mode for each block and returns the modes
// and the residuals.
func predict(pix []uint32, w, h int, exact bool) ([]uint8, []uint32) {
	tw, th := tiles(w), tiles(h)
	modes := make([]uint8, tw*th)
	candidates := []uint8{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13}
	if exact {
		candidates = []uint8{1, 2, 3, 4, 11}
	}
	for ty := range th {
		for tx := range tw {
			best, bestCost := uint8(11), -1
			for _, m := range candidates {
				cost := 0
				for y := ty << predictorBits; y < min(h, (ty+1)<<predictorBits); y++ {
					if y == 0 {
						continue // the first row always predicts from the left
					}
					for x := max(1, tx<<predictorBits); x < min(w, (tx+1)<<predictorBits); x++ {
						i := y*w + x
						cost += residualCost(sub(pix[i], prediction(pix, i, w, m)))
					}
				}
				if bestCost < 0 || cost < bestCost {
					best, bestCost = m, cost
				}
			}
			modes[ty*tw+tx] = best
		}
	}
	res := make([]uint32, len(pix))
	for y := range h {
		for x := range w {
			i := y*w + x
			var p uint32
			switch {
			case x == 0 && y == 0:
				p = 0xff000000
			case y == 0:
				p = pix[i-1]
			case x == 0:
				p = pix[i-w]
			default:
				p = prediction(pix, i, w, modes[(y>>predictorBits)*tw+x>>predictorBits])
			}
			res[i] = sub(pix[i], p)
		}
	}
	return modes, res
}

// residualCost estimates how many bits a residual costs: small ones, up or
// down, are cheap.
func residualCost(r uint32) int {
	c := 0
	for range 4 {
		v := int(int8(r))
		if v < 0 {
			v = -v
		}
		c += v
		r >>= 8
	}
	return c
}

// sub subtracts b from a per channel, modulo 256.
func sub(a, b uint32) uint32 {
	ag := ((a | 0x00ff00ff) - (b & 0xff00ff00)) & 0xff00ff00
	rb := ((a | 0xff00ff00) - (b & 0x00ff00ff)) & 0x00ff00ff
	return ag | rb
}

// prediction is predictor mode m's prediction for pixel i, which is past
// the first row and column.
func prediction(pix []uint32, i, w int, m uint8) uint32 {
	l, t := pix[i-1], pix[i-w]
	tl, tr := pix[i-w-1], pix[i-w+1]
	switch m {
	case 0:
		return 0xff000000
	case 1:
		return l
	case 2:
		return t
	case 3:
		return tr
	case 4:
		return tl
	case 5:
		return avg(avg(l, tr), t)
	case 6:
		return avg(l, tl)
	case 7:
		return avg(l, t)
	case 8:
		return avg(tl, t)
	case 9:
		return avg(t, tr)
	case 10:
		return avg(avg(l, tl), avg(t, tr))
	case 11:
		// The decoder's Select: the side whose neighbor changes least.
		if dist(tl, t) < dist(tl, l) {
			return l
		}
		return t
	case 12:
		return perChannel(l, t, tl, func(a, b, c int) int { return clamp(a + b - c) })
	case 13:
		return perChannel(avg(l, t), tl, 0, func(a, b, _ int) int { return clamp(a + (a-b)/2) })
	}
	return 0
}

func avg(a, b uint32) uint32 {
	return perChannel(a, b, 0, func(x, y, _ int) int { return (x + y) / 2 })
}

func dist(a, b uint32) int {
	d := 0
	for range 4 {
		v := int(a&0xff) - int(b&0xff)
		if v < 0 {
			v = -v
		}
		d += v
		a, b = a>>8, b>>8
	}
	return d
}

func perChannel(a, b, c uint32, f func(a, b, c int) int) uint32 {
	var out uint32
	for s := 0; s < 32; s += 8 {
		out |= uint32(f(int(a>>s&0xff), int(b>>s&0xff), int(c>>s&0xff))&0xff) << s
	}
	return out
}

func clamp(v int) int { return min(255, max(0, v)) }

// A token is a literal pixel or a backward reference.
type token struct {
	argb   uint32 // a literal, when length is 0
	length int    // a copy of length pixels
	dcode  int    // its distance code
}

// writeImage writes an entropy-coded image: no color cache, one group of
// prefix codes (top marks the main image, which says so) and its pixels.
func writeImage(bw *bitWriter, pix []uint32, w int, top bool) {
	bw.write(0, 1) // no color cache
	if top {
		bw.write(0, 1) // no meta prefix codes
	}
	toks := backwardRefs(pix, w)
	var hist [5][]uint32
	for k, n := range [5]int{numLiterals + numLengths, numLiterals, numLiterals, numLiterals, numDists} {
		hist[k] = make([]uint32, n)
	}
	for _, t := range toks {
		if t.length == 0 {
			hist[0][t.argb>>8&0xff]++
			hist[1][t.argb>>16&0xff]++
			hist[2][t.argb&0xff]++
			hist[3][t.argb>>24]++
			continue
		}
		lc, _, _ := prefixCode(t.length)
		dc, _, _ := prefixCode(t.dcode)
		hist[0][numLiterals+lc]++
		hist[4][dc]++
	}
	var codes [5]prefixCodes
	for k := range hist {
		codes[k] = writePrefixCode(bw, hist[k])
	}
	for _, t := range toks {
		if t.length == 0 {
			codes[0].put(bw, int(t.argb>>8&0xff))
			codes[1].put(bw, int(t.argb>>16&0xff))
			codes[2].put(bw, int(t.argb&0xff))
			codes[3].put(bw, int(t.argb>>24))
			continue
		}
		lc, lbits, lextra := prefixCode(t.length)
		codes[0].put(bw, numLiterals+lc)
		bw.write(lextra, lbits)
		dc, dbits, dextra := prefixCode(t.dcode)
		codes[4].put(bw, dc)
		bw.write(dextra, dbits)
	}
}

// prefixCode splits a length or distance code v (1 and up) into its prefix
// symbol and extra bits.
func prefixCode(v int) (sym int, nbits uint, extra uint32) {
	d := v - 1
	if d < 4 {
		return d, 0, 0
	}
	hb := 0
	for d>>(hb+1) != 0 {
		hb++
	}
	second := d >> (hb - 1) & 1
	nbits = uint(hb - 1)
	return 2*hb + second, nbits, uint32(d & (1<<nbits - 1))
}

// Backward reference limits.
const (
	minMatch   = 3
	maxMatch   = 4096
	maxDist    = 1<<20 - 120 // fits the 40 distance prefix codes
	hashBits   = 16
	chainLimit = 24
)

// backwardRefs finds repeats in pix with a hash chain, preferring the
// pixel to the left and the one above, whose distance codes are short.
func backwardRefs(pix []uint32, w int) []token {
	n := len(pix)
	toks := make([]token, 0, n/2)
	head := make([]int32, 1<<hashBits)
	for i := range head {
		head[i] = -1
	}
	prev := make([]int32, n)
	hash := func(i int) uint32 {
		return (pix[i]*0x1e35a7bd ^ pix[i+1]*0x9e3779b1) >> (32 - hashBits)
	}
	insert := func(i int) {
		if i+1 < n {
			h := hash(i)
			prev[i] = head[h]
			head[h] = int32(i)
		}
	}
	matchLen := func(i, j int) int {
		l := 0
		for i+l < n && l < maxMatch && pix[i+l] == pix[j+l] {
			l++
		}
		return l
	}
	for i := 0; i < n; {
		bestLen, bestDist := 0, 0
		for _, d := range [2]int{1, w} {
			if i >= d {
				if l := matchLen(i, i-d); l > bestLen {
					bestLen, bestDist = l, d
				}
			}
		}
		if i+1 < n && bestLen < maxMatch {
			j := head[hash(i)]
			for tries := 0; j >= 0 && tries < chainLimit; tries++ {
				d := i - int(j)
				if d > maxDist {
					break
				}
				// A longer match must beat the short codes of left and up.
				if l := matchLen(i, int(j)); l > bestLen+1 {
					bestLen, bestDist = l, d
				}
				j = prev[j]
			}
		}
		if bestLen < minMatch {
			toks = append(toks, token{argb: pix[i]})
			insert(i)
			i++
			continue
		}
		toks = append(toks, token{length: bestLen, dcode: distCode(bestDist, w)})
		for k := range bestLen {
			insert(i + k)
		}
		i += bestLen
	}
	return toks
}

// distCode is the distance code of a backward reference: 1 for the pixel
// above, 2 for the one to the left, else the distance plus 120.
func distCode(d, w int) int {
	switch d {
	case w:
		return 1
	case 1:
		return 2
	}
	return d + 120
}

type bitWriter struct {
	buf  []byte
	acc  uint64
	nacc uint
}

// write writes the low n bits of v, least significant first.
func (b *bitWriter) write(v uint32, n uint) {
	b.acc |= uint64(v&(1<<n-1)) << b.nacc
	b.nacc += n
	for b.nacc >= 8 {
		b.buf = append(b.buf, byte(b.acc))
		b.acc >>= 8
		b.nacc -= 8
	}
}

func (b *bitWriter) bytes() []byte {
	if b.nacc > 0 {
		b.buf = append(b.buf, byte(b.acc))
		b.acc, b.nacc = 0, 0
	}
	return b.buf
}
