package ui

import (
	"image"
	"image/color"
	"strings"

	"gioui.org/font"
	"gioui.org/op"
	"gioui.org/text"
	"gioui.org/unit"
	"gioui.org/widget"
	"golang.org/x/image/math/fixed"

	"github.com/chomosuke9/wazzapclients/internal/ui/styledtext"
)

// How many lines a group's description and a status's caption show before
// "Read more", as on phones.
const (
	descLines    = 4
	captionLines = 4
)

// lineCut is how cutLines cuts a text.
type lineCut struct {
	text    string
	px, w   int // of the text, and of its lines
	lines   int
	weight  font.Weight
	policy  text.WrapPolicy
	padding int // non-breaking spaces left after the ellipsis
}

type lineCutVal struct {
	text string
	long bool
}

// lineCuts caches cutLines, which shapes the text.
var lineCuts = memo[lineCut, lineCutVal]{limit: 100}

// cutLines returns what fits of a text in c.lines lines of its size, with
// an ellipsis and room px after it at the end of the last, and whether
// there's more.
func (u *UI) cutLines(gtx C, c lineCut, size unit.Sp, room int) (string, bool) {
	c.text, c.px = displayText(c.text), gtx.Sp(size)
	c.padding = int(float32(room)/u.nbspWidth(gtx, size)) + 1
	v := lineCuts.get(c, func() lineCutVal {
		// The room is spaces after the ellipsis.
		u.th.Shaper.LayoutString(text.Parameters{
			Font:       font.Font{Typeface: typeface, Weight: c.weight},
			PxPerEm:    fixed.I(c.px),
			MaxLines:   c.lines,
			Truncator:  "…" + strings.Repeat(" ", c.padding),
			MaxWidth:   c.w,
			Locale:     gtx.Locale,
			WrapPolicy: c.policy,
		}, c.text)
		hidden := 0
		for g, ok := u.th.Shaper.NextGlyph(); ok; g, ok = u.th.Shaper.NextGlyph() {
			if g.Flags&text.FlagTruncator != 0 && g.Flags&text.FlagClusterBreak != 0 {
				hidden = int(g.Runes)
			}
		}
		rs := []rune(c.text)
		if hidden <= 0 || hidden >= len(rs) {
			return lineCutVal{text: c.text}
		}
		return lineCutVal{strings.TrimRight(string(rs[:len(rs)-hidden]), " \n.,;:-") + "…", true}
	})
	return v.text, v.long
}

// moreText is a text that shows its first lines, ending with a "Read more"
// link, until it's all to be shown.
type moreText struct {
	text      string
	size      unit.Sp
	lineH     unit.Sp
	weight    font.Weight
	align     text.Alignment
	col, link color.NRGBA
	lines     int               // 0 shows it all
	more      *widget.Clickable // the link
}

// layoutMoreText draws t across the width given.
func (u *UI) layoutMoreText(gtx C, t moreText) D {
	gtx.Constraints.Min.X = gtx.Constraints.Max.X // it aligns in the whole width
	if t.lines > 0 {
		const gap = " "
		mg := gtx
		mg.Constraints.Min = image.Point{}
		mg.Constraints.Max.X = 1 << 20
		m := op.Record(gtx.Ops)
		label := u.label(t.size, gap+readMoreLabel, t.link, labelOpts{weight: font.Bold, maxLines: 1}).Layout(mg)
		m.Stop()
		cut := lineCut{text: t.text, w: gtx.Constraints.Max.X, lines: t.lines, weight: t.weight}
		if s, long := u.cutLines(gtx, cut, t.size, label.Size.X); long {
			spans := []styledtext.SpanStyle{
				{Font: font.Font{Typeface: typeface, Weight: t.weight}, Size: t.size, Color: t.col, Content: s + gap},
				{Font: font.Font{Typeface: typeface, Weight: font.Bold}, Size: t.size, Color: t.link, Content: readMoreLabel},
			}
			return u.layoutSpansAligned(gtx, spans, nil, t.lineH, t.align, func(gtx C, idx int, d D) {
				if idx == 1 {
					clickable(gtx, t.more, func(gtx C) D { return D{Size: d.Size} })
				}
			}, nil)
		}
	}
	l := u.label(t.size, t.text, t.col, labelOpts{weight: t.weight, align: t.align})
	l.LineHeight, l.LineHeightScale = t.lineH, 1
	return l.Layout(gtx)
}
