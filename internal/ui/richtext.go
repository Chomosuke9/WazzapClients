package ui

import (
	"image"
	"image/color"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"gioui.org/font"
	"gioui.org/layout"
	"gioui.org/unit"

	"github.com/chomosuke9/wazzapclients/internal/ui/styledtext"
)

// Mentions arrive from the backend wrapped in Unicode isolate marks
// (U+2068 … U+2069), so the UI knows exactly which text to highlight even
// when a name contains spaces. The marks are invisible either way.
const (
	mentionStart = '⁨'
	mentionEnd   = '⁩'
)

type textStyle uint8

const (
	styleBold textStyle = 1 << iota
	styleItalic
	styleStrike
	styleMono // ```code block```
	styleCode // `inline code`, on a tinted background
)

var markerStyle = map[byte]textStyle{'*': styleBold, '_': styleItalic, '~': styleStrike, '`': styleCode}

type run struct {
	text  string
	style textStyle
}

// parseFormatting splits WhatsApp markup into styled runs: *bold*,
// _italic_, ~strike~, `code` and ```code blocks```. Markers only count at
// word boundaries and must hug their text, as in WhatsApp.
func parseFormatting(s string) []run {
	var out []run
	parts := strings.Split(s, "```")
	for i, p := range parts {
		if i%2 == 1 && i < len(parts)-1 {
			// The line breaks right inside the fences only frame the block.
			p = strings.TrimSuffix(strings.TrimPrefix(p, "\n"), "\n")
			out = append(out, run{p, styleMono})
			continue
		}
		if i%2 == 1 { // unterminated block: keep the backticks
			p = "```" + p
		}
		out = parseInline(out, p, 0)
	}
	return out
}

func isWordRune(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }

func parseInline(out []run, s string, style textStyle) []run {
	start := 0
	for i := 0; i < len(s); i++ {
		st, ok := markerStyle[s[i]]
		if !ok || style&st != 0 {
			continue
		}
		if i > 0 {
			if r, _ := utf8.DecodeLastRuneInString(s[:i]); isWordRune(r) {
				continue
			}
		}
		if i+1 >= len(s) || s[i+1] == ' ' || s[i+1] == '\n' {
			continue
		}
		j := closingMarker(s, i)
		if j < 0 {
			continue
		}
		if i > start {
			out = append(out, run{s[start:i], style})
		}
		if st == styleCode {
			out = append(out, run{s[i+1 : j], style | st})
		} else {
			out = parseInline(out, s[i+1:j], style|st)
		}
		start = j + 1
		i = j
	}
	if start < len(s) {
		out = append(out, run{s[start:], style})
	}
	return out
}

// closingMarker finds the marker closing the one at s[open] on the same line.
func closingMarker(s string, open int) int {
	m := s[open]
	for j := open + 2; j < len(s); j++ {
		if s[j] == '\n' {
			return -1
		}
		if s[j] != m || s[j-1] == ' ' {
			continue
		}
		// "**bold**" closes at the last marker of a run, so it shows as
		// "*bold*" in bold, like WhatsApp.
		for j+1 < len(s) && s[j+1] == m {
			j++
		}
		if j+1 < len(s) {
			if r, _ := utf8.DecodeRuneInString(s[j+1:]); isWordRune(r) {
				continue
			}
		}
		return j
	}
	return -1
}

// plainText strips formatting markers and mention marks, for previews.
func plainText(s string) string {
	if !strings.ContainsAny(s, "*_~`⁨⁩") {
		return s
	}
	var b strings.Builder
	for _, r := range parseFormatting(s) {
		b.WriteString(r.text)
	}
	return strings.NewReplacer("⁨", "", "⁩", "").Replace(b.String())
}

var linkRe = regexp.MustCompile(`https?://[^\s\x{2068}\x{2069}]+|www\.[^\s\x{2068}\x{2069}]+`)

// richSpans turns message text into styled spans: WhatsApp formatting,
// highlighted mentions and links.
//
// code reports which spans are inline code, strike which are struck through.
func (u *UI) richSpans(text string, size unit.Sp, col color.NRGBA, italic bool) (spans []styledtext.SpanStyle, code, strike []bool) {
	p := u.pal
	base := font.Font{Typeface: typeface}
	if italic {
		base.Style = font.Italic
	}
	var isCode, isStrike bool
	add := func(s string, f font.Font, c color.NRGBA) {
		if s != "" {
			spans = append(spans, styledtext.SpanStyle{Font: f, Size: size, Color: c, Content: displayText(s)})
			code, strike = append(code, isCode), append(strike, isStrike)
		}
	}
	for _, r := range parseFormatting(text) {
		f := base
		if r.style&styleBold != 0 {
			f.Weight = font.Bold
		}
		if r.style&styleItalic != 0 {
			f.Style = font.Italic
		}
		isCode, isStrike = r.style&styleCode != 0, r.style&styleStrike != 0
		rest := r.text
		if r.style&(styleMono|styleCode) != 0 {
			f.Typeface = monoTypeface
		}
		if isCode {
			// Hair spaces pad the text inside its background.
			rest = "\u200a" + rest + "\u200a"
		}
		// Split out mentions, then links.
		for rest != "" {
			i := strings.IndexRune(rest, mentionStart)
			seg := rest
			if i >= 0 {
				seg = rest[:i]
			}
			u.addLinks(seg, f, col, add)
			if i < 0 {
				break
			}
			rest = rest[i+len(string(mentionStart)):]
			j := strings.IndexRune(rest, mentionEnd)
			if j < 0 {
				j = len(rest)
			}
			mf := f
			mf.Weight = max(mf.Weight, font.Medium)
			add(rest[:j], mf, p.Green)
			if j < len(rest) {
				rest = rest[j+len(string(mentionEnd)):]
			} else {
				rest = ""
			}
		}
	}
	return spans, code, strike
}

const monoTypeface = "Consolas, Cascadia Mono, Courier New, monospace"

// Block-level WhatsApp formatting: "> " quotes and "- ", "* " or "1. "
// list items, at the start of a line.
type blockKind uint8

const (
	blockText blockKind = iota
	blockQuote
	blockList
)

type textBlock struct {
	kind   blockKind
	marker string // a list item's bullet or number
	text   string
}

var numberedRe = regexp.MustCompile(`^\d{1,3}\. `)

// parseBlocks splits text into paragraphs, quotes and list items.
// Consecutive quote lines form one quote; lines inside ``` code blocks
// are plain text.
func parseBlocks(s string) []textBlock {
	var out []textBlock
	inCode := false
	for _, line := range strings.Split(s, "\n") {
		b := textBlock{kind: blockText, text: line}
		if !inCode {
			switch {
			case strings.HasPrefix(line, "> "):
				b.kind, b.text = blockQuote, line[2:]
			case strings.HasPrefix(line, "- ") || strings.HasPrefix(line, "* "):
				b.kind, b.marker, b.text = blockList, "•", line[2:]
			default:
				if m := numberedRe.FindString(line); m != "" {
					b.kind, b.marker, b.text = blockList, strings.TrimSpace(m), line[len(m):]
				}
			}
		}
		if strings.Count(line, "```")%2 == 1 {
			inCode = !inCode
		}
		if n := len(out); n > 0 && b.kind != blockList && out[n-1].kind == b.kind {
			out[n-1].text += "\n" + b.text
			continue
		}
		out = append(out, b)
	}
	return out
}

// layoutRich lays out message text with WhatsApp formatting. prefix and
// suffix are spans before the first and after the last block: the indent
// for a leading icon and the room for the time.
func (u *UI) layoutRich(gtx C, text string, size unit.Sp, col, secondary color.NRGBA, italic bool, prefix, suffix string) D {
	plain := font.Font{Typeface: typeface}
	blocks := u.parsedRich(text, size, col, italic)
	maxW := gtx.Constraints.Max.X
	y, w := 0, 0
	for i, b := range blocks {
		// The parsed spans are cached: cap them so appends copy.
		spans, code, strike := b.spans[:len(b.spans):len(b.spans)], b.code[:len(b.code):len(b.code)], b.strike[:len(b.strike):len(b.strike)]
		if i == 0 && prefix != "" {
			spans = append([]styledtext.SpanStyle{{Font: plain, Size: size, Color: col, Content: prefix}}, spans...)
			code, strike = append([]bool{false}, code...), append([]bool{false}, strike...)
		}
		if i == len(blocks)-1 && suffix != "" {
			spans = append(spans, styledtext.SpanStyle{Font: plain, Size: size, Color: col, Content: suffix})
			code, strike = append(code, false), append(strike, false)
		}
		indent := 0
		var marker part
		switch b.kind {
		case blockQuote:
			indent = gtx.Dp(13)
		case blockList:
			marker = record(gtx, func(gtx C) D {
				return u.layoutSpans(gtx, []styledtext.SpanStyle{{Font: plain, Size: size, Color: col, Content: b.marker}}, nil, nil)
			})
			indent = max(gtx.Dp(18), marker.size.X+gtx.Dp(6))
		}
		bgtx := gtx
		bgtx.Constraints = layout.Constraints{Max: image.Pt(max(0, maxW-indent), 1<<20)}
		var body part
		if len(spans) == 0 {
			body.size.Y = gtx.Sp(22) // an empty line
		} else {
			body = record(bgtx, func(gtx C) D { return u.layoutSpans(gtx, spans, code, strike) })
		}
		switch b.kind {
		case blockQuote:
			bar := gtx.Dp(3)
			fillRRect(gtx, image.Rect(0, y+gtx.Dp(2), bar, y+body.size.Y-gtx.Dp(2)), bar/2, secondary)
		case blockList:
			marker.at(gtx, 0, y)
		}
		body.at(gtx, indent, y)
		y += body.size.Y
		w = max(w, indent+body.size.X)
	}
	return D{Size: image.Pt(w, y)}
}

// layoutSpans draws styled text with 22sp lines, plus the background of
// inline code and strike-through lines.
func (u *UI) layoutSpans(gtx C, spans []styledtext.SpanStyle, code, strike []bool) D {
	st := styledtext.Text(u.th.Shaper, spans...)
	st.LineHeight, st.LineHeightScale = 22, 1
	has := func(flags []bool) bool {
		for _, f := range flags {
			if f {
				return true
			}
		}
		return false
	}
	if has(code) {
		// Lay the text out once, invisibly, to paint the code backgrounds
		// under the real text.
		hidden := make([]styledtext.SpanStyle, len(spans))
		copy(hidden, spans)
		for i := range hidden {
			hidden[i].Color = color.NRGBA{}
		}
		ht := st
		ht.Styles = hidden
		ht.Layout(gtx, func(gtx C, idx int, d D) {
			if code[idx] {
				r := image.Rectangle{Max: d.Size}
				r.Min.Y, r.Max.Y = gtx.Dp(1), d.Size.Y-gtx.Dp(1)
				fillRRect(gtx, r, gtx.Dp(4), u.pal.CodeBg)
			}
		})
	}
	var fn func(gtx C, idx int, d D)
	if has(strike) {
		fn = func(gtx C, idx int, d D) {
			if strike[idx] {
				y := d.Baseline - gtx.Sp(spans[idx].Size)*3/10
				fillRect(gtx, image.Rect(0, y, d.Size.X, y+max(1, gtx.Dp(1))), spans[idx].Color)
			}
		}
	}
	return st.Layout(gtx, fn)
}

func (u *UI) addLinks(s string, f font.Font, col color.NRGBA, add func(string, font.Font, color.NRGBA)) {
	for s != "" {
		loc := linkRe.FindStringIndex(s)
		if loc == nil {
			add(s, f, col)
			return
		}
		add(s[:loc[0]], f, col)
		add(s[loc[0]:loc[1]], f, u.pal.TickRead)
		s = s[loc[1]:]
	}
}
