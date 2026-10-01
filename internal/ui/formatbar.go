package ui

import (
	"image"
	"strings"

	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/widget"

	"github.com/chomosuke9/wazzapclients/internal/ui/icon"
)

// The formatting toolbar floats over text selected in the composer, like
// WhatsApp's: each button wraps the selection in WhatsApp markup, or
// unwraps it when it already has that style.

type fmtKind uint8

const (
	fmtBold fmtKind = iota
	fmtItalic
	fmtStrike
	fmtCode  // `inline code`
	fmtMono  // ```code block```
	fmtQuote // "> " before each line
	fmtList  // "- " before each line
	numFmt
)

var fmtMarks = [numFmt]string{"*", "_", "~", "`", "```", "> ", "- "}

var fmtButtons = [numFmt]struct {
	ic    *icon.Icon
	label string
}{
	{icon.FormatBold, "Bold"},
	{icon.FormatItalic, "Italic"},
	{icon.StrikethroughS, "Strikethrough"},
	{icon.Code, "Inline code"},
	{icon.DataObject, "Monospace"},
	{icon.FormatQuote, "Quote"},
	{icon.FormatListBulleted, "Bulleted list"},
}

// fmtEdit is how a format changes the composer text: runes lo to hi
// become repl, and runes a to b of the new text end up selected.
type fmtEdit struct {
	lo, hi int
	repl   []rune
	a, b   int
	// remove reports that the selection had the style, which the edit
	// takes away.
	remove bool
}

func hasAt(s []rune, i int, mark string) bool {
	m := []rune(mark)
	if i < 0 || i+len(m) > len(s) {
		return false
	}
	for j, r := range m {
		if s[i+j] != r {
			return false
		}
	}
	return true
}

// formatText works out format k applied to runes a to b of txt.
func formatText(txt []rune, a, b int, k fmtKind) fmtEdit {
	a, b = min(a, b), max(a, b)
	a, b = max(0, min(a, len(txt))), max(0, min(b, len(txt)))
	if k == fmtQuote || k == fmtList {
		return formatLines(txt, a, b, fmtMarks[k])
	}
	mark := fmtMarks[k]
	m := len([]rune(mark))
	// Inline styles stop at line ends, so each line is wrapped on its own.
	// A code block wraps the whole selection.
	lo, hi := a, b
	if k != fmtMono {
		for lo > 0 && txt[lo-1] != '\n' {
			lo--
		}
		for hi < len(txt) && txt[hi] != '\n' {
			hi++
		}
	}
	// A line's selected text, without its spaces, from s to e, and its
	// marks just outside or inside it (-1 if none).
	type seg struct{ s, e, sm, em int }
	var segs []seg
	for start := lo; start <= hi; {
		end := hi
		if k != fmtMono {
			end = start
			for end < hi && txt[end] != '\n' {
				end++
			}
		}
		s, e := max(start, a), min(end, b)
		for s < e && isSpaceRune(txt[s]) {
			s++
		}
		for e > s && isSpaceRune(txt[e-1]) {
			e--
		}
		if s < e {
			sg := seg{s, e, -1, -1}
			if m == 1 {
				sg.sm, sg.em = findMarks(txt, start, s, e, end, mark[0])
			} else {
				switch {
				case s-m >= start && hasAt(txt, s-m, mark):
					sg.sm = s - m
				case hasAt(txt, s, mark):
					sg.sm = s
				}
				switch {
				case e+m <= end && hasAt(txt, e, mark):
					sg.em = e
				case hasAt(txt, e-m, mark):
					sg.em = e - m
				}
			}
			if sg.sm < 0 || sg.em < sg.sm+m+1 {
				sg.sm, sg.em = -1, -1
			}
			segs = append(segs, sg)
		}
		start = end + 1
	}
	if len(segs) == 0 {
		return fmtEdit{lo: a, hi: a, a: a, b: b}
	}
	remove := true
	for _, sg := range segs {
		remove = remove && sg.sm >= 0
	}
	if remove {
		// The marks may be just outside the edited range.
		lo, hi = min(lo, segs[0].sm), max(hi, segs[len(segs)-1].em+m)
	}
	var out []rune
	na, nb := 0, 0
	pos := lo
	for i, sg := range segs {
		switch {
		case remove:
			// Other styles' marks may sit between these and the text.
			out = append(out, txt[pos:sg.sm]...)
			mid := len(out) - (sg.sm + m) // maps txt positions after the mark
			if i == 0 {
				na = mid + max(sg.s, sg.sm+m)
			}
			out = append(out, txt[sg.sm+m:sg.em]...)
			nb = mid + min(sg.e, sg.em)
			pos = sg.em + m
		case sg.sm >= 0: // already styled
			out = append(out, txt[pos:sg.s]...)
			if i == 0 {
				na = len(out)
			}
			out = append(out, txt[sg.s:sg.e]...)
			nb = len(out)
			pos = sg.e
		default:
			out = append(out, txt[pos:sg.s]...)
			out = append(out, []rune(mark)...)
			if i == 0 {
				na = len(out)
			}
			out = append(out, txt[sg.s:sg.e]...)
			nb = len(out)
			out = append(out, []rune(mark)...)
			pos = sg.e
		}
	}
	out = append(out, txt[pos:hi]...)
	return fmtEdit{lo: lo, hi: hi, repl: out, a: lo + na, b: lo + nb, remove: remove}
}

// findMarks finds a one-character mark around txt[s:e], on the line from
// start to end: just outside or just inside it, past other styles' marks,
// as in "*_~text~_*". It returns -1s if either is missing.
func findMarks(txt []rune, start, s, e, end int, mark byte) (sm, em int) {
	isMark := func(r rune) bool { return r == '*' || r == '_' || r == '~' }
	want := rune(mark)
	sm, em = -1, -1
	for j := s - 1; j >= start && isMark(txt[j]); j-- {
		if txt[j] == want {
			sm = j
			break
		}
	}
	if sm < 0 {
		for j := s; j < e && isMark(txt[j]); j++ {
			if txt[j] == want {
				sm = j
				break
			}
		}
	}
	for j := e; j < end && isMark(txt[j]); j++ {
		if txt[j] == want {
			em = j
			break
		}
	}
	if em < 0 {
		for j := e - 1; j > sm && isMark(txt[j]); j-- {
			if txt[j] == want {
				em = j
				break
			}
		}
	}
	if sm < 0 || em < 0 {
		return -1, -1
	}
	return sm, em
}

func isSpaceRune(r rune) bool { return r == ' ' || r == '\t' || r == '\n' || r == ' ' }

// formatLines adds prefix to each line from rune a to b, or takes it away
// when they all have it.
func formatLines(txt []rune, a, b int, prefix string) fmtEdit {
	if b > a && txt[b-1] == '\n' {
		b-- // a selection to the start of a line ends on the line before
	}
	lo, hi := a, b
	for lo > 0 && txt[lo-1] != '\n' {
		lo--
	}
	for hi < len(txt) && txt[hi] != '\n' {
		hi++
	}
	lines := strings.Split(string(txt[lo:hi]), "\n")
	remove, some := true, false
	for _, l := range lines {
		if strings.TrimSpace(l) != "" {
			some = true
			remove = remove && strings.HasPrefix(l, prefix)
		}
	}
	if !some {
		remove = false
	}
	for i, l := range lines {
		switch {
		case remove:
			lines[i] = strings.TrimPrefix(l, prefix)
		case strings.TrimSpace(l) != "" && !strings.HasPrefix(l, prefix):
			lines[i] = prefix + l
		}
	}
	repl := []rune(strings.Join(lines, "\n"))
	return fmtEdit{lo: lo, hi: hi, repl: repl, a: lo, b: lo + len(repl), remove: remove}
}

// applyFormat formats the composer's selection.
func (u *UI) applyFormat(k fmtKind) {
	ed := &u.conv.composer
	a, b := ed.Selection()
	if a == b {
		return
	}
	e := formatText([]rune(ed.Text()), a, b, k)
	if e.lo == e.hi && len(e.repl) == 0 {
		return
	}
	ed.SetCaret(e.lo, e.hi)
	ed.Insert(string(e.repl))
	ed.SetCaret(e.a, e.b)
	u.requestFocus(ed)
}

// formatKeys handles WhatsApp Desktop's formatting shortcuts in the
// composer: Ctrl+B, Ctrl+I, Ctrl+Shift+X and Ctrl+Shift+M.
func (u *UI) formatKeys(gtx C) {
	ed := &u.conv.composer
	for {
		ev, ok := gtx.Event(
			key.Filter{Focus: ed, Name: "B", Required: key.ModShortcut},
			key.Filter{Focus: ed, Name: "I", Required: key.ModShortcut},
			key.Filter{Focus: ed, Name: "X", Required: key.ModShortcut | key.ModShift},
			key.Filter{Focus: ed, Name: "M", Required: key.ModShortcut | key.ModShift})
		if !ok {
			break
		}
		e, ok := ev.(key.Event)
		if !ok || e.State != key.Press {
			continue
		}
		switch e.Name {
		case "B":
			u.applyFormat(fmtBold)
		case "I":
			u.applyFormat(fmtItalic)
		case "X":
			u.applyFormat(fmtStrike)
		case "M":
			u.applyFormat(fmtMono)
		}
	}
}

// layoutFormatBar draws the toolbar over the composer's selection. gtx is
// the editor's context: positions are relative to its top left corner.
func (u *UI) layoutFormatBar(gtx C, ed *widget.Editor) {
	c := &u.conv
	for k := range numFmt {
		if u.btn("fmt:" + fmtButtons[k].label).Clicked(gtx) {
			u.applyFormat(k)
		}
	}
	a, b := ed.Selection()
	a, b = min(a, b), max(a, b)
	// It opens once the mouse lets go of a selection, then follows it.
	show := a != b && gtx.Focused(ed) && (c.fmtAnim.on || !u.mouseDown)
	if show {
		c.fmtRegions = ed.Regions(a, b, c.fmtRegions[:0])
		if len(c.fmtRegions) > 0 {
			r := c.fmtRegions[0].Bounds
			c.fmtAt = image.Pt((r.Min.X+r.Max.X)/2, max(r.Min.Y, 0))
		}
		txt := []rune(ed.Text())
		for k := range numFmt {
			c.fmtActive[k] = formatText(txt, a, b, k).remove
		}
	}
	v := c.fmtAnim.step(gtx, show, popDur(show))
	if v == 0 {
		return
	}
	p := u.pal
	m := op.Record(gtx.Ops)
	bgtx := gtx
	var done func()
	if !show {
		bgtx, done = fadeOut(gtx)
	}
	bar := record(bgtx, func(gtx C) D {
		gtx.Constraints.Min = image.Point{}
		return layout.Inset{Left: 4, Right: 4, Top: 4, Bottom: 4}.Layout(gtx, func(gtx C) D {
			var children []layout.FlexChild
			for k := range numFmt {
				if k == fmtCode {
					children = append(children, layout.Rigid(func(gtx C) D {
						h := gtx.Dp(20)
						fillRect(gtx, image.Rect(gtx.Dp(4), (gtx.Dp(32)-h)/2, gtx.Dp(5), (gtx.Dp(32)+h)/2), p.PopupDivider)
						return D{Size: image.Pt(gtx.Dp(9), gtx.Dp(32))}
					}))
				}
				children = append(children, layout.Rigid(func(gtx C) D {
					col := p.IconStrong
					if c.fmtActive[k] {
						col = p.Green
					}
					cl := u.btn("fmt:" + fmtButtons[k].label)
					return clickable(gtx, cl, func(gtx C) D {
						sz := gtx.Dp(32)
						if h := u.hover(gtx, cl); h > 0 {
							fillRRect(gtx, image.Rect(0, 0, sz, sz), gtx.Dp(6), faded(p.PopupHover, h))
						}
						return centerIn(gtx, sz, iconW(fmtButtons[k].ic, 20, col))
					})
				}))
			}
			return layout.Flex{Alignment: layout.Middle}.Layout(gtx, children...)
		})
	})
	gap := gtx.Dp(18) // clear of the composer's top edge
	x := min(max(c.fmtAt.X-bar.size.X/2, -gtx.Dp(90)), gtx.Constraints.Max.X-bar.size.X)
	y := c.fmtAt.Y - bar.size.Y - gap
	rect := image.Rectangle{Min: image.Pt(x, y), Max: image.Pt(x+bar.size.X, y+bar.size.Y)}
	rad := gtx.Dp(8)
	fx := pushPopup(gtx, v, image.Pt(c.fmtAt.X, y+bar.size.Y))
	fillRRect(gtx, rect.Add(image.Pt(0, gtx.Dp(3))).Inset(-gtx.Dp(1)), rad+gtx.Dp(1), p.Shadow)
	borderRRect(gtx, rect, rad, p.Popup, p.PopupBorder)
	bar.at(gtx, x, y)
	fx.Pop()
	if done != nil {
		done()
	}
	op.Defer(gtx.Ops, m.Stop())
}
