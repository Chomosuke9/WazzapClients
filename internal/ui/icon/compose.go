package icon

import "gioui.org/f32"

// Icons made of two Material Symbols.
var (
	// ExtensionQuestion is the puzzle piece with a question mark in its
	// hollow middle: the Extra features page's ethically gray ones.
	ExtensionQuestion = Extension.With(QuestionMark, f32.Pt(0.5, 0.48), 0.46)
)

// With returns ic with inner drawn inside it: inner scaled by scale around
// its center and moved to at, all in the unit square. inner should sit in
// a hollow of ic, since overlapping outlines don't cut each other.
func (ic *Icon) With(inner *Icon, at f32.Point, scale float32) *Icon {
	move := func(p f32.Point) f32.Point {
		return f32.Pt((p.X-0.5)*scale+at.X, (p.Y-0.5)*scale+at.Y)
	}
	out := &Icon{segs: make([]seg, 0, len(ic.segs)+len(inner.segs))}
	out.segs = append(out.segs, ic.segs...)
	for _, s := range inner.segs {
		out.segs = append(out.segs, seg{op: s.op, a: move(s.a), b: move(s.b)})
	}
	return out
}
