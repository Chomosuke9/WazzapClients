package ui

import (
	"image"
	"image/color"
	"regexp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"gioui.org/font"
	"gioui.org/io/event"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/chomosuke9/wazzapclients/internal/model"
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
	return mentionMarks.Replace(b.String())
}

var linkRe = regexp.MustCompile(`https?://[^\s\x{2068}\x{2069}]+|www\.[^\s\x{2068}\x{2069}]+`)

// spanDeco is what layoutSpans draws around a span besides its text.
type spanDeco uint8

const (
	decoCode   spanDeco = 1 << iota // inline code, on a tinted background
	decoStrike                      // struck through
	decoPill                        // a mention of you, on a rounded tint
	decoLink                        // a link, underlined until hovered
)

// pillFor says which mentions get a pill: the ones that notify you.
type pillFor uint8

const (
	pillMe    pillFor = 1 << iota // "@You" and "@all"
	pillAdmin                     // "@admin", when you are an admin
)

// pilled reports whether a mention, which starts with its kind mark if it
// has one, is drawn as a pill.
func (pf pillFor) pilled(mention string) bool {
	r, _ := utf8.DecodeRuneInString(mention)
	switch r {
	case model.MentionNotifies:
		return pf&pillMe != 0
	case model.MentionAdmins:
		return pf&pillAdmin != 0
	}
	return false
}

// mentionMarks are the invisible marks around and inside a mention.
var mentionMarks = strings.NewReplacer("⁨", "", "⁩", "", string(model.MentionNotifies), "", string(model.MentionAdmins), "")

// richSpans turns message text into styled spans: WhatsApp formatting,
// highlighted mentions and links. deco has each span's decorations.
func (u *UI) richSpans(text string, size unit.Sp, col color.NRGBA, italic bool, pills pillFor) (spans []styledtext.SpanStyle, deco []spanDeco) {
	p := u.pal
	base := font.Font{Typeface: typeface}
	if italic {
		base.Style = font.Italic
	}
	var cur spanDeco
	add := func(s string, f font.Font, c color.NRGBA) {
		if s != "" {
			spans = append(spans, styledtext.SpanStyle{Font: f, Size: size, Color: c, Content: displayText(s)})
			deco = append(deco, cur)
		}
	}
	addLink := func(s string, f font.Font) {
		prev := cur
		cur |= decoLink
		add(s, f, p.Link)
		cur = prev
	}
	for _, r := range parseFormatting(text) {
		f := base
		if r.style&styleBold != 0 {
			f.Weight = font.Bold
		}
		if r.style&styleItalic != 0 {
			f.Style = font.Italic
		}
		cur = 0
		if r.style&styleCode != 0 {
			cur |= decoCode
		}
		if r.style&styleStrike != 0 {
			cur |= decoStrike
		}
		isCode := cur&decoCode != 0
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
			addLinks(seg, f, col, add, addLink)
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
			name := rest[:j]
			if pills.pilled(name) {
				// NBSPs pad the name inside its pill, and keep it on one line.
				prev := cur
				cur |= decoPill
				add("\u00a0"+strings.ReplaceAll(mentionMarks.Replace(name), " ", "\u00a0")+"\u00a0", mf, p.Green)
				cur = prev
			} else {
				add(mentionMarks.Replace(name), mf, p.Green)
			}
			if j < len(rest) {
				rest = rest[j+len(string(mentionEnd)):]
			} else {
				rest = ""
			}
		}
	}
	return spans, deco
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

// richOpts are layoutRich's options besides the text and its colors.
type richOpts struct {
	italic bool
	pills  pillFor
	// prefix and suffix are spans before the first and after the last
	// block: the indent for a leading icon and the room for the time.
	prefix, suffix string
	// more, if set, ends the text with a "Read more" link that clicks it.
	more *widget.Clickable
	// links, if set, makes the text's links clickable: it tells their
	// buttons apart from other texts'.
	links string
	// sel, if set, is the message whose text can be selected.
	sel string
}

// layoutRich lays out message text with WhatsApp formatting.
func (u *UI) layoutRich(gtx C, text string, size unit.Sp, col, secondary color.NRGBA, o richOpts) D {
	plain := font.Font{Typeface: typeface}
	blocks := u.parsedRich(text, size, col, o.italic, o.pills)
	maxW := gtx.Constraints.Max.X
	ts := &u.textSel
	var tag event.Tag
	var evs []pointer.Event
	if o.sel != "" {
		tag = u.btn("txt:" + o.sel) // only its address is used, as an event tag
		evs = ts.pointerEvents(gtx, tag)
	}
	// Carets are only needed to select, or to show a selection.
	track := o.sel != "" && (len(evs) > 0 || ts.id == o.sel)
	if track {
		ts.setPlain(text, blocks)
		ts.carets = ts.carets[:0]
	}
	base := 0 // the selection position of the block's first rune
	macro := op.Record(gtx.Ops)
	y, w := 0, 0
	for i, b := range blocks {
		// The parsed spans are cached: cap them so appends copy.
		spans, deco := b.spans[:len(b.spans):len(b.spans)], b.deco[:len(b.deco):len(b.deco)]
		if i == 0 && o.prefix != "" {
			spans = append([]styledtext.SpanStyle{{Font: plain, Size: size, Color: col, Content: o.prefix}}, spans...)
			deco = append([]spanDeco{0}, deco...)
		}
		// Links take clicks unless the text can't be used (select mode),
		// and lose their underline while hovered.
		var linkKeys map[int]string
		if o.links != "" {
			for j, dc := range deco {
				if dc&decoLink == 0 {
					continue
				}
				if linkKeys == nil {
					linkKeys = map[int]string{}
					deco = slices.Clone(deco)
				}
				key := "lk:" + o.links + ":" + itoa(i) + ":" + itoa(j)
				linkKeys[j] = key
				c := u.btn(key)
				if c.Clicked(gtx) {
					u.openLink(spans[j].Content)
				}
				if c.Hovered() {
					deco[j] &^= decoLink
				}
			}
		}
		link := -1
		if i == len(blocks)-1 {
			if o.more != nil {
				link = len(spans)
				medium := plain
				medium.Weight = font.Medium
				spans = append(spans, styledtext.SpanStyle{Font: medium, Size: size, Color: u.pal.Link, Content: readMoreLabel})
				deco = append(deco, 0)
			}
			if o.suffix != "" {
				spans = append(spans, styledtext.SpanStyle{Font: plain, Size: size, Color: col, Content: o.suffix})
				deco = append(deco, 0)
			}
		}
		indent := 0
		var marker part
		switch b.kind {
		case blockQuote:
			indent = gtx.Dp(13)
		case blockList:
			marker = record(gtx, func(gtx C) D {
				return u.layoutSpans(gtx, []styledtext.SpanStyle{{Font: plain, Size: size, Color: col, Content: b.marker}}, nil, nil, nil)
			})
			indent = max(gtx.Dp(18), marker.size.X+gtx.Dp(6))
		}
		bgtx := gtx
		bgtx.Constraints = layout.Constraints{Max: image.Pt(max(0, maxW-indent), 1<<20)}
		var body part
		if len(spans) == 0 {
			body.size.Y = gtx.Sp(22) // an empty line
		} else {
			var onSpan func(gtx C, idx int, d D)
			if link >= 0 || linkKeys != nil {
				onSpan = func(gtx C, idx int, d D) {
					if idx == link {
						clickable(gtx, o.more, func(gtx C) D { return D{Size: d.Size} })
					} else if key, ok := linkKeys[idx]; ok {
						clickable(gtx, u.btn(key), func(gtx C) D { return D{Size: d.Size} })
					}
				}
			}
			var carets *[]styledtext.Caret
			if track {
				ts.tmp = ts.tmp[:0]
				carets = &ts.tmp
			}
			body = record(bgtx, func(gtx C) D { return u.layoutSpans(gtx, spans, deco, onSpan, carets) })
		}
		if track {
			// Keep the carets of the block's own text, not of the prefix,
			// "Read more" or the room for the time.
			pre, n := 0, 0
			if i == 0 && o.prefix != "" {
				pre = utf8.RuneCountInString(o.prefix)
			}
			for _, s := range b.spans {
				n += utf8.RuneCountInString(s.Content)
			}
			if len(spans) == 0 {
				ts.carets = append(ts.carets, styledtext.Caret{Rune: base, X: indent, Top: y, Bottom: y + body.size.Y})
			}
			for _, c := range ts.tmp {
				if r := c.Rune - pre; r >= 0 && r <= n {
					ts.carets = append(ts.carets, styledtext.Caret{Rune: base + r, X: indent + c.X, Top: y + c.Top, Bottom: y + c.Bottom})
				}
			}
			base += n + 1
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
	content := macro.Stop()
	if track {
		ts.handle(gtx, o.sel, evs)
		if a, b, ok := ts.span(o.sel); ok {
			u.paintSelection(gtx, ts.carets, a, b)
		}
	}
	if o.sel != "" {
		// Under the text, so the "Read more" link takes its clicks.
		u.selectableArea(gtx, tag, o.sel, image.Pt(w, y))
		if ts.id == o.sel {
			u.selectionKeys(gtx, o.sel)
		}
	}
	content.Add(gtx.Ops)
	return D{Size: image.Pt(w, y)}
}

// layoutSpans draws styled text with 22sp lines, plus the decorations of
// deco (code and pill backgrounds, strike-through lines). onSpan, if set,
// is called for each span (or each line of one) after it is drawn. carets,
// if set, receives the text's caret positions.
func (u *UI) layoutSpans(gtx C, spans []styledtext.SpanStyle, deco []spanDeco, onSpan func(gtx C, idx int, d D), carets *[]styledtext.Caret) D {
	st := styledtext.Text(u.th.Shaper, spans...)
	st.LineHeight, st.LineHeightScale = 22, 1
	var all spanDeco
	for _, d := range deco {
		all |= d
	}
	if all&(decoCode|decoPill) != 0 {
		// Lay the text out once, invisibly, to paint the backgrounds under
		// the real text.
		hidden := make([]styledtext.SpanStyle, len(spans))
		copy(hidden, spans)
		for i := range hidden {
			hidden[i].Color = color.NRGBA{}
		}
		ht := st
		ht.Styles = hidden
		ht.Layout(gtx, func(gtx C, idx int, d D) {
			// Center the box on the text, not on the font's ascent and
			// descent, which differ between fonts (Consolas, Segoe UI).
			em := float32(gtx.Sp(spans[idx].Size))
			r := image.Rect(0, d.Baseline-int(em*0.98+0.5), d.Size.X, d.Baseline+int(em*0.26+0.5))
			switch {
			case deco[idx]&decoCode != 0:
				fillRRect(gtx, r, gtx.Dp(4), u.pal.CodeBg)
			case deco[idx]&decoPill != 0:
				fillRRect(gtx, r, r.Dy()/2, u.pal.MentionPill)
			}
		})
	}
	fn := onSpan
	if all&(decoStrike|decoLink) != 0 {
		fn = func(gtx C, idx int, d D) {
			if deco[idx]&decoStrike != 0 {
				y := d.Baseline - gtx.Sp(spans[idx].Size)*3/10
				fillRect(gtx, image.Rect(0, y, d.Size.X, y+max(1, gtx.Dp(1))), spans[idx].Color)
			}
			if deco[idx]&decoLink != 0 {
				y := d.Baseline + max(1, gtx.Sp(spans[idx].Size)/8)
				fillRect(gtx, image.Rect(0, y, d.Size.X, y+max(1, gtx.Dp(1))), spans[idx].Color)
			}
			if onSpan != nil {
				onSpan(gtx, idx, d)
			}
		}
	}
	st.Carets = carets
	return st.Layout(gtx, fn)
}

// addLinks adds the text s, its links with addLink.
func addLinks(s string, f font.Font, col color.NRGBA, add func(string, font.Font, color.NRGBA), addLink func(string, font.Font)) {
	for s != "" {
		loc := linkRe.FindStringIndex(s)
		if loc == nil {
			add(s, f, col)
			return
		}
		end := loc[0] + len(trimLink(s[loc[0]:loc[1]]))
		add(s[:loc[0]], f, col)
		addLink(s[loc[0]:end], f)
		add(s[end:loc[1]], f, col)
		s = s[loc[1]:]
	}
}

// trimLink drops the punctuation that ends a sentence, not the link, from
// a link found in text ("see https://example.com." or "(www.example.com)").
func trimLink(s string) string {
	for len(s) > 0 {
		switch s[len(s)-1] {
		case '.', ',', ';', ':', '!', '?', '\'', '"':
		case ')':
			if strings.Count(s, "(") >= strings.Count(s, ")") {
				return s // part of the link, like Wikipedia's
			}
		default:
			return s
		}
		s = s[:len(s)-1]
	}
	return s
}

// Long messages show their start and a "Read more" link, like WhatsApp.
// Each click shows readMoreStep times as much again.
const (
	readMoreRunes = 700 // shown at first
	readMoreLines = 16
	readMoreStep  = 4
	readMoreLabel = "Read\u00a0more"
)

// readMoreCut returns the part of a message's text to show after clicks
// "Read more" clicks, and whether there is more. It cuts at a word
// boundary, never inside a mention, and closes an open ``` block.
func readMoreCut(s string, clicks int) (string, bool) {
	maxRunes, maxLines := readMoreRunes, readMoreLines
	for range clicks {
		if maxRunes > len(s) {
			return s, false
		}
		maxRunes, maxLines = maxRunes*readMoreStep, maxLines*readMoreStep
	}
	return cutText(s, maxRunes, maxLines)
}

// cutText returns the start of s, up to about maxRunes runes and maxLines
// lines, and whether there is more; see readMoreCut.
func cutText(s string, maxRunes, maxLines int) (string, bool) {
	// Leave a little slack, so "Read more" never reveals only a few words.
	if utf8.RuneCountInString(s) <= maxRunes*5/4 && strings.Count(s, "\n") < maxLines*5/4 {
		return s, false
	}
	cut, runes, lines := len(s), 0, 0
	for i, r := range s {
		if runes == maxRunes {
			cut = i
			break
		}
		if r == '\n' {
			if lines++; lines == maxLines {
				cut = i
				break
			}
		}
		runes++
	}
	if cut == len(s) {
		return s, false
	}
	t := s[:cut]
	if s[cut] != '\n' {
		// Back up to the last space, unless the word is very long.
		if i := strings.LastIndexAny(t, " \n\t"); i > 0 && utf8.RuneCountInString(t[i:]) < 40 {
			t = t[:i]
		}
	}
	if strings.Count(t, string(mentionStart)) > strings.Count(t, string(mentionEnd)) {
		t = t[:strings.LastIndex(t, string(mentionStart))]
	}
	t = strings.TrimRightFunc(t, unicode.IsSpace)
	if strings.Count(t, "```")%2 == 1 {
		t += "```"
	}
	return closeFormatting(t, s) + "…\u00a0", true
}

// closeFormatting closes the *bold*, _italic_, ~strike~ or `code` that a
// cut through full left open in its start t, so the shown start is styled
// as it is in the whole text. It adds up to two markers (nested styles).
func closeFormatting(t, full string) string {
	want := plainText(full)
	fits := func(c string) bool { return strings.HasPrefix(want, plainText(t+c)) }
	if fits("") {
		return t
	}
	markers := []string{"*", "_", "~", "`"}
	for _, a := range markers {
		if fits(a) {
			return t + a
		}
	}
	for _, a := range markers {
		for _, b := range markers {
			if a != b && fits(a+b) {
				return t + a + b
			}
		}
	}
	return t
}

type readMoreKey struct {
	text   string
	clicks int
}

type readMoreVal struct {
	text string
	more bool
}

// readMoreCuts caches readMoreCut, which parses the text.
var readMoreCuts = memo[readMoreKey, readMoreVal]{limit: 100}

// readMore is readMoreCut, cached.
func readMore(s string, clicks int) (string, bool) {
	v := readMoreCuts.get(readMoreKey{s, clicks}, func() readMoreVal {
		t, more := readMoreCut(s, clicks)
		return readMoreVal{t, more}
	})
	return v.text, v.more
}
