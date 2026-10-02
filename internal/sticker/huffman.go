package sticker

import "sort"

// prefixCodes is a canonical prefix code: each symbol's length and its
// bits, reversed so that writing them least significant first puts the
// code's first bit first, as VP8L reads them.
type prefixCodes struct {
	lengths []uint8
	codes   []uint32
}

func (p *prefixCodes) put(bw *bitWriter, sym int) {
	bw.write(p.codes[sym], uint(p.lengths[sym]))
}

// codeLengthOrder is the order code length code lengths are written in.
var codeLengthOrder = [19]int{17, 18, 0, 1, 2, 3, 4, 5, 16, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15}

// writePrefixCode writes a prefix code for the symbol counts in hist and
// returns it.
func writePrefixCode(bw *bitWriter, hist []uint32) prefixCodes {
	var used []int
	for s, c := range hist {
		if c > 0 {
			used = append(used, s)
		}
	}
	if len(used) == 0 {
		used = []int{0} // never read: any code will do
	}
	if len(used) <= 2 && used[len(used)-1] < 256 {
		// A simple code: one symbol (of no bits) or two (of one bit).
		bw.write(1, 1)
		bw.write(uint32(len(used)-1), 1)
		if used[0] < 2 {
			bw.write(0, 1)
			bw.write(uint32(used[0]), 1)
		} else {
			bw.write(1, 1)
			bw.write(uint32(used[0]), 8)
		}
		p := prefixCodes{lengths: make([]uint8, len(hist)), codes: make([]uint32, len(hist))}
		if len(used) == 2 {
			bw.write(uint32(used[1]), 8)
			p.lengths[used[0]], p.lengths[used[1]] = 1, 1
			p.codes[used[1]] = 1
		}
		return p
	}
	if len(used) == 1 {
		// A one-symbol code takes no bits; give it a second symbol so
		// that every decoder agrees it takes one.
		hist = append([]uint32(nil), hist...)
		hist[(used[0]+1)%len(hist)] = 1
	}
	lengths := huffmanLengths(hist, 15)
	writeCodeLengths(bw, lengths)
	return canonical(lengths)
}

// clToken is a symbol of the code length code, with its extra bits.
type clToken struct {
	sym   int
	nbits uint
	extra uint32
}

// writeCodeLengths writes the lengths of a normal prefix code, run-length
// coded with the code length code.
func writeCodeLengths(bw *bitWriter, lengths []uint8) {
	var toks []clToken
	for i := 0; i < len(lengths); {
		v := lengths[i]
		run := 1
		for i+run < len(lengths) && lengths[i+run] == v {
			run++
		}
		i += run
		if v == 0 {
			for run >= 3 {
				switch {
				case run >= 11:
					n := min(run, 138)
					toks = append(toks, clToken{18, 7, uint32(n - 11)})
					run -= n
				default:
					n := min(run, 10)
					toks = append(toks, clToken{17, 3, uint32(n - 3)})
					run -= n
				}
			}
			for ; run > 0; run-- {
				toks = append(toks, clToken{sym: 0})
			}
			continue
		}
		// The literal, then repeats of it (16 repeats the last non-zero).
		toks = append(toks, clToken{sym: int(v)})
		run--
		for run >= 3 {
			n := min(run, 6)
			toks = append(toks, clToken{16, 2, uint32(n - 3)})
			run -= n
		}
		for ; run > 0; run-- {
			toks = append(toks, clToken{sym: int(v)})
		}
	}
	hist := make([]uint32, 19)
	used := 0
	for _, t := range toks {
		if hist[t.sym] == 0 {
			used++
		}
		hist[t.sym]++
	}
	if used == 1 {
		hist[(toks[0].sym+1)%19] = 1 // see writePrefixCode
	}
	clLengths := huffmanLengths(hist, 7)
	cl := canonical(clLengths)
	num := 19
	for num > 4 && clLengths[codeLengthOrder[num-1]] == 0 {
		num--
	}
	bw.write(0, 1) // a normal code
	bw.write(uint32(num-4), 4)
	for _, s := range codeLengthOrder[:num] {
		bw.write(uint32(clLengths[s]), 3)
	}
	bw.write(0, 1) // as many lengths as symbols
	for _, t := range toks {
		cl.put(bw, t.sym)
		bw.write(t.extra, t.nbits)
	}
}

// huffmanLengths returns Huffman code lengths of at most limit bits for
// the symbol counts in hist, which counts at least two symbols. Rare
// symbols are counted as more common until the code fits.
func huffmanLengths(hist []uint32, limit int) []uint8 {
	type node struct {
		count       uint64
		left, right int // children, or -1 for a leaf
		sym         int
	}
	var leaves []node
	for s, c := range hist {
		if c > 0 {
			leaves = append(leaves, node{count: uint64(c), left: -1, right: -1, sym: s})
		}
	}
	lengths := make([]uint8, len(hist))
	for floor := uint64(1); ; floor *= 2 {
		nodes := make([]node, 0, 2*len(leaves))
		for _, l := range leaves {
			l.count = max(l.count, floor)
			nodes = append(nodes, l)
		}
		sort.SliceStable(nodes, func(a, b int) bool { return nodes[a].count < nodes[b].count })
		// Two queues: the sorted leaves and the merged nodes, which are
		// made in order of their counts.
		nl := len(nodes)
		li, mi := 0, nl
		pick := func() int {
			if li < nl && (mi >= len(nodes) || nodes[li].count <= nodes[mi].count) {
				li++
				return li - 1
			}
			mi++
			return mi - 1
		}
		for len(nodes)-nl < nl-1 {
			a := pick()
			b := pick()
			nodes = append(nodes, node{count: nodes[a].count + nodes[b].count, left: a, right: b})
		}
		clear(lengths)
		deepest := 0
		var walk func(i, depth int)
		walk = func(i, depth int) {
			n := nodes[i]
			if n.left < 0 {
				lengths[n.sym] = uint8(depth)
				deepest = max(deepest, depth)
				return
			}
			walk(n.left, depth+1)
			walk(n.right, depth+1)
		}
		walk(len(nodes)-1, 0)
		if deepest <= limit {
			return lengths
		}
	}
}

// canonical assigns the canonical codes of the given lengths.
func canonical(lengths []uint8) prefixCodes {
	var count [16]uint32
	for _, l := range lengths {
		count[l]++
	}
	count[0] = 0
	var next [16]uint32
	code := uint32(0)
	for l := 1; l < 16; l++ {
		code = (code + count[l-1]) << 1
		next[l] = code
	}
	p := prefixCodes{lengths: lengths, codes: make([]uint32, len(lengths))}
	for s, l := range lengths {
		if l == 0 {
			continue
		}
		c := next[l]
		next[l]++
		// Reverse the code's l bits.
		r := uint32(0)
		for range l {
			r = r<<1 | c&1
			c >>= 1
		}
		p.codes[s] = r
	}
	return p
}
