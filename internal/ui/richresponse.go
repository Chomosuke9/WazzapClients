package ui

import (
	"bytes"
	"image"
	"image/color"
	_ "image/png" // formulas' pictures
	"math"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"gioui.org/font"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/unit"

	"github.com/chomosuke9/wazzapclients/internal/model"
	"github.com/chomosuke9/wazzapclients/internal/ui/styledtext"
)

// An AI's answer (model.RichResponse: Meta AI's richResponseMessage, and
// the same forwarded) fills its bubble as WhatsApp draws it: markdown with
// headings, lists, checkboxes, quotes and lines, code blocks under their
// language with a copy button, tables, and formulas. WhatsApp sends each
// formula as a picture, black on transparent, tinted here to the text's
// color; until it has downloaded (or when it can't) its LaTeX shows as
// text, with what Unicode has of super- and subscripts.

const (
	richSize   = unit.Sp(15.7) // the bubble's text
	richLineH  = unit.Sp(22)
	codeSize   = unit.Sp(13.5)
	codeLineH  = unit.Sp(20)
	tableSize  = unit.Sp(14.5)
	tableLineH = unit.Sp(20)
)

// headingSizes are the text sizes of headings # to ######.
var headingSizes = [...]unit.Sp{27, 23, 20, 18, 16.5, 15.7}

// mdKind is what a block of an answer is.
type mdKind uint8

const (
	mdPara mdKind = iota
	mdHeading
	mdBullet
	mdNumber
	mdCheck
	mdQuote
	mdRule
	mdCode
	mdTable
	mdMath
)

// mdBlock is a block of an answer: one of its markdown's, or a section
// that is code, a table or a formula.
type mdBlock struct {
	kind    mdKind
	level   int    // a heading's, 1 to 6; a list item's depth, from 0
	marker  string // a numbered item's number
	checked bool
	gap     bool   // a blank line comes before it
	text    string // its markdown; a code block's code; a formula's LaTeX
	lang    string
	code    []model.CodeToken // a code section's, by token
	rows    []model.RichRow
	latex   *model.RichLatex // a formula section's, with its picture
}

func (b *mdBlock) item() bool {
	return b.kind == mdBullet || b.kind == mdNumber || b.kind == mdCheck
}

var (
	mdHeadingRe = regexp.MustCompile(`^(#{1,6})\s+(.*?)(?:\s+#+)?$`)
	mdRuleRe    = regexp.MustCompile(`^(?:(?:-[ \t]*){3,}|(?:\*[ \t]*){3,}|(?:_[ \t]*){3,})$`)
	mdCheckRe   = regexp.MustCompile(`^[-*+]\s+\[([ xX])\]\s+(.*)$`)
	mdBulletRe  = regexp.MustCompile(`^[-*+•]\s+(.*)$`)
	mdNumberRe  = regexp.MustCompile(`^(\d{1,3})[.)]\s+(.*)$`)
	mdTableSep  = regexp.MustCompile(`^\|?\s*:?-{2,}:?\s*(?:\|\s*:?-{2,}:?\s*)*\|?$`)
)

var mdParsed = memo[string, []mdBlock]{limit: 200}

// parseMarkdown splits an answer's markdown into blocks, parsed once.
func parseMarkdown(s string) []mdBlock {
	return mdParsed.get(s, func() []mdBlock { return parseMarkdownNow(s) })
}

func parseMarkdownNow(s string) []mdBlock {
	var out []mdBlock
	gap := false
	para := false // the last block is a paragraph that the next line goes on
	add := func(b mdBlock) {
		b.gap = gap && len(out) > 0
		gap, para = false, false
		out = append(out, b)
	}
	lines := strings.Split(s, "\n")
	for i := 0; i < len(lines); i++ {
		line := strings.TrimRight(lines[i], " \t\r")
		trim := strings.TrimLeft(line, " \t")
		depth := indentWidth(line[:len(line)-len(trim)]) / 2
		var m []string
		switch {
		case trim == "":
			gap, para = true, false
		case strings.HasPrefix(trim, "```") && !strings.Contains(trim[3:], "```"):
			// A fence; ```code``` on one line is text in WhatsApp's markup.
			lang := strings.TrimSpace(trim[3:])
			var code []string
			for i++; i < len(lines) && !strings.HasPrefix(strings.TrimSpace(lines[i]), "```"); i++ {
				code = append(code, lines[i])
			}
			add(mdBlock{kind: mdCode, lang: lang, text: strings.Join(code, "\n")})
		case strings.HasPrefix(trim, "$$"):
			expr := trim[2:]
			if j := strings.Index(expr, "$$"); j >= 0 {
				expr = expr[:j]
			} else {
				parts := []string{expr}
				for i++; i < len(lines); i++ {
					if j := strings.Index(lines[i], "$$"); j >= 0 {
						parts = append(parts, lines[i][:j])
						break
					}
					parts = append(parts, lines[i])
				}
				expr = strings.Join(parts, " ")
			}
			add(mdBlock{kind: mdMath, text: strings.TrimSpace(expr)})
		case mdRuleRe.MatchString(trim):
			add(mdBlock{kind: mdRule})
		case strings.HasPrefix(trim, "|") && i+1 < len(lines) && mdTableSep.MatchString(strings.TrimSpace(lines[i+1])):
			rows := []model.RichRow{{Header: true, Cells: tableCells(trim)}}
			for i += 2; i < len(lines) && strings.HasPrefix(strings.TrimSpace(lines[i]), "|"); i++ {
				rows = append(rows, model.RichRow{Cells: tableCells(strings.TrimSpace(lines[i]))})
			}
			i--
			add(mdBlock{kind: mdTable, rows: rows})
		case strings.HasPrefix(trim, ">"):
			t := strings.TrimPrefix(strings.TrimPrefix(trim, ">"), " ")
			if n := len(out); n > 0 && out[n-1].kind == mdQuote && !gap {
				out[n-1].text += "\n" + t
				continue
			}
			add(mdBlock{kind: mdQuote, text: t})
		default:
			if m = mdHeadingRe.FindStringSubmatch(trim); m != nil {
				add(mdBlock{kind: mdHeading, level: len(m[1]), text: m[2]})
			} else if m = mdCheckRe.FindStringSubmatch(trim); m != nil {
				add(mdBlock{kind: mdCheck, level: depth, checked: m[1] != " ", text: m[2]})
			} else if m = mdBulletRe.FindStringSubmatch(trim); m != nil {
				add(mdBlock{kind: mdBullet, level: depth, text: m[1]})
			} else if m = mdNumberRe.FindStringSubmatch(trim); m != nil {
				add(mdBlock{kind: mdNumber, level: depth, marker: m[1] + ".", text: m[2]})
			} else if n := len(out); n > 0 && !gap && (para || out[n-1].item() && depth > out[n-1].level) {
				// The paragraph's next line, or an item's.
				out[n-1].text += "\n" + trim
			} else {
				add(mdBlock{kind: mdPara, text: trim})
				para = true
			}
		}
	}
	return out
}

// indentWidth counts leading white space, a tab as four spaces.
func indentWidth(ws string) int {
	n := 0
	for _, r := range ws {
		if r == '\t' {
			n += 4
		} else {
			n++
		}
	}
	return n
}

// tableCells splits a markdown table's row into its cells.
func tableCells(line string) []string {
	line = strings.TrimSuffix(strings.TrimPrefix(line, "|"), "|")
	cells := strings.Split(line, "|")
	for i, c := range cells {
		cells[i] = strings.TrimSpace(c)
	}
	return cells
}

// richBlocks flattens an answer's sections into blocks. Each block keeps
// the formulas of the section it came from.
func richItems(rr *model.RichResponse) (blocks []mdBlock, inline [][]model.RichLatex) {
	for i := range rr.Sections {
		s := &rr.Sections[i]
		first := len(blocks)
		switch s.Kind {
		case model.RichMarkdown:
			blocks = append(blocks, parseMarkdown(s.Text)...)
		case model.RichCode:
			blocks = append(blocks, mdBlock{kind: mdCode, lang: s.Lang, code: s.Code})
		case model.RichTable:
			blocks = append(blocks, mdBlock{kind: mdTable, rows: s.Rows})
		case model.RichMath:
			blocks = append(blocks, mdBlock{kind: mdMath, text: s.Latex.Expr, latex: &s.Latex})
		}
		for j := first; j < len(blocks); j++ {
			inline = append(inline, s.Inline)
		}
		if first > 0 && first < len(blocks) {
			blocks[first].gap = true // sections are paragraphs apart
		}
	}
	return blocks, inline
}

// mdGap is the room between two blocks.
func mdGap(gtx C, prev, b *mdBlock) int {
	switch {
	case b.kind == mdHeading:
		if b.level <= 2 {
			return gtx.Dp(18)
		}
		return gtx.Dp(14)
	case prev.kind == mdHeading:
		return gtx.Dp(10)
	case prev.item() && b.item() && !b.gap:
		return gtx.Dp(5)
	case b.gap || b.kind == mdRule || prev.kind == mdRule:
		return gtx.Dp(12)
	}
	return gtx.Dp(6)
}

// layoutRichResponse draws m's answer, at most maxW wide. Its last line
// leaves metaW free at its right end, metaH high, for the timestamp.
func (u *UI) layoutRichResponse(gtx C, m *model.Message, maxW, metaW, metaH int, cols bubbleColors) D {
	blocks, inline := richItems(m.Rich)
	type piece struct {
		p       part
		gap     int
		stretch bool // drawn once the answer's width is known
	}
	pieces := make([]piece, len(blocks))
	natW, stretch := metaW, false
	lastText := -1
	if n := len(blocks); n > 0 && blocks[n-1].kind != mdCode && blocks[n-1].kind != mdTable &&
		blocks[n-1].kind != mdRule && blocks[n-1].kind != mdMath {
		lastText = n - 1
	}
	for i := range blocks {
		b := &blocks[i]
		if i > 0 {
			pieces[i].gap = mdGap(gtx, &blocks[i-1], b)
		}
		switch b.kind {
		case mdCode, mdTable, mdRule, mdMath:
			pieces[i].stretch, stretch = true, stretch || b.kind != mdMath
			if b.kind == mdMath {
				natW = max(natW, u.mathWidth(gtx, m, b, cols.text))
			}
			continue
		}
		suffix := ""
		if i == lastText {
			nbsp := u.nbspWidth(gtx, richSize)
			suffix = " " + strings.Repeat(" ", int(float32(metaW+gtx.Dp(8))/nbsp)+1)
		}
		key := "ai:" + m.ChatID + "/" + m.ID + ":" + itoa(i)
		pieces[i].p = record(gtx, func(gtx C) D { return u.layoutMdBlock(gtx, m, b, inline[i], key, maxW, suffix, cols) })
		natW = max(natW, pieces[i].p.size.X)
	}
	w := min(maxW, natW)
	if stretch {
		w = maxW
	}
	y := 0
	for i := range blocks {
		b := &blocks[i]
		pc := &pieces[i]
		if pc.stretch {
			key := "ai:" + m.ChatID + "/" + m.ID + ":" + itoa(i)
			pc.p = record(gtx, func(gtx C) D {
				switch b.kind {
				case mdCode:
					return u.layoutCodeBlock(gtx, b, key, w)
				case mdTable:
					return u.layoutTable(gtx, m, b.rows, key, w, cols)
				case mdMath:
					return u.layoutMath(gtx, m, b, w, cols.text)
				}
				return D{Size: image.Pt(w, max(1, gtx.Dp(1)))}
			})
			if b.kind == mdRule {
				fillRect(gtx, image.Rect(0, y+pc.gap, w, y+pc.gap+pc.p.size.Y), u.pal.TableLine)
			}
		}
		y += pc.gap
		pc.p.at(gtx, 0, y)
		y += pc.p.size.Y
	}
	if lastText < 0 {
		y += gtx.Dp(6) + metaH // the timestamp gets a line of its own
	}
	return D{Size: image.Pt(w, y)}
}

// layoutMdBlock draws a block of text: a paragraph, heading, list item or
// quote, at most maxW wide, ending with suffix.
func (u *UI) layoutMdBlock(gtx C, m *model.Message, b *mdBlock, inline []model.RichLatex, key string, maxW int, suffix string, cols bubbleColors) D {
	size, lineH := richSize, richLineH
	if b.kind == mdHeading {
		size = headingSizes[b.level-1]
		lineH = size * 1.3
	}
	indent := 0
	if b.item() || b.kind == mdQuote {
		indent = gtx.Dp(18) * b.level // nested
	}
	var marker part
	plain := font.Font{Typeface: typeface}
	switch b.kind {
	case mdQuote:
		indent += gtx.Dp(13)
	case mdBullet, mdNumber:
		label := "•"
		if b.kind == mdNumber {
			label = b.marker
		}
		marker = record(gtx, func(gtx C) D {
			return u.layoutSpans(gtx, []styledtext.SpanStyle{{Font: plain, Size: size, Color: cols.text, Content: label}}, nil, nil, nil)
		})
		marker.size.X = max(gtx.Dp(18), marker.size.X+gtx.Dp(6))
	case mdCheck:
		ic, col := icCheckBoxEmpty, cols.secondary
		if b.checked {
			ic, col = icCheckBox, u.pal.BubbleButton
		}
		marker = record(gtx, func(gtx C) D {
			// Level with the text's first line.
			defer op.Offset(image.Pt(0, (gtx.Sp(lineH)-gtx.Dp(18))/2)).Push(gtx.Ops).Pop()
			return drawIcon(gtx, ic, 18, col)
		})
		marker.size.X = gtx.Dp(24)
	}
	mx := indent
	indent += marker.size.X
	t := u.mdSpans(gtx, m, b.text, inline, size, cols.text)
	tg := gtx
	tg.Constraints = layout.Constraints{Max: image.Pt(max(0, maxW-indent), 1<<20)}
	body := record(tg, func(gtx C) D { return u.layoutMdText(gtx, m, t, inline, lineH, key, suffix) })
	if b.kind == mdQuote {
		bar := gtx.Dp(3)
		x := indent - gtx.Dp(13)
		fillRRect(gtx, image.Rect(x, gtx.Dp(2), x+bar, body.size.Y-gtx.Dp(2)), bar/2, cols.secondary)
	}
	if marker.size.Y > 0 {
		marker.at(gtx, mx, 0)
	}
	body.at(gtx, indent, 0)
	return D{Size: image.Pt(indent+body.size.X, max(body.size.Y, marker.size.Y))}
}

// mdText is a block's text as styled spans.
type mdText struct {
	spans []styledtext.SpanStyle
	deco  []spanDeco
	links map[int]string // span → the URL its click opens, when not its text
	pics  map[int]int    // span → the formula whose picture it makes room for
}

type mdTextKey struct {
	text      string
	size      unit.Sp
	col, link color.NRGBA
	room      string // NBSPs each formula's picture takes, 0 for text
}

var mdTexts = memo[mdTextKey, *mdText]{limit: 800}

// mdTokenRe finds a markdown link, a formula's mark, or $inline math$.
var mdTokenRe = regexp.MustCompile(`\[([^\]\n]+)\]\(((?:https?://|www\.)[^)\s]+)\)|` +
	string(model.RichInline) + `(\d+)` + string(model.RichInline) + `|\$([^\s$](?:[^$\n]*[^\s$])?)\$`)

// mdSpans styles a block's markdown text, parsed once. A formula whose
// picture has loaded gets room for it, in NBSPs.
func (u *UI) mdSpans(gtx C, m *model.Message, text string, inline []model.RichLatex, size unit.Sp, col color.NRGBA) *mdText {
	link := linkFor(u.pal, col)
	var room []int
	if len(inline) > 0 && strings.ContainsRune(text, model.RichInline) {
		room = make([]int, len(inline))
		nbsp := u.nbspWidth(gtx, size)
		em := float32(gtx.Sp(size))
		for i := range inline {
			if e := u.formulaImage(m, &inline[i], col); e != nil && e.state == imgReady {
				w := float32(inline[i].W) * em / inline[i].FontHeight
				room[i] = int(math.Ceil(float64(w / nbsp)))
			}
		}
	}
	var rk strings.Builder
	for _, n := range room {
		rk.WriteString(itoa(n))
		rk.WriteByte(',')
	}
	return mdTexts.get(mdTextKey{text, size, col, link, rk.String()}, func() *mdText {
		t := &mdText{}
		plain := font.Font{Typeface: typeface}
		add := func(s, url string) {
			sp, dc := u.richSpans(s, size, col, false, 0)
			if url != "" {
				if t.links == nil {
					t.links = map[int]string{}
				}
				for i := range sp {
					sp[i].Color = link
					dc[i] |= decoLink
					t.links[len(t.spans)+i] = url
				}
			}
			t.spans = append(t.spans, sp...)
			t.deco = append(t.deco, dc...)
		}
		addMath := func(expr string) {
			f := plain
			f.Style = font.Italic
			t.spans = append(t.spans, styledtext.SpanStyle{Font: f, Size: size, Color: col, Content: displayText(latexText(expr))})
			t.deco = append(t.deco, 0)
		}
		pos := 0
		for _, mt := range mdTokenRe.FindAllStringSubmatchIndex(text, -1) {
			if mt[0] > pos {
				add(text[pos:mt[0]], "")
			}
			switch {
			case mt[2] >= 0:
				add(text[mt[2]:mt[3]], text[mt[4]:mt[5]])
			case mt[6] >= 0:
				n, _ := strconv.Atoi(text[mt[6]:mt[7]])
				switch {
				case n >= len(inline):
				case n < len(room) && room[n] > 0:
					if t.pics == nil {
						t.pics = map[int]int{}
					}
					t.pics[len(t.spans)] = n
					t.spans = append(t.spans, styledtext.SpanStyle{Font: plain, Size: size, Color: col, Content: strings.Repeat(" ", room[n])})
					t.deco = append(t.deco, 0)
				default:
					addMath(inline[n].Expr)
				}
			default:
				// Dollars around something that isn't math stay as they are.
				if expr := text[mt[8]:mt[9]]; strings.ContainsAny(expr, `^_\{}=`) {
					addMath(expr)
				} else {
					add(text[mt[0]:mt[1]], "")
				}
			}
			pos = mt[1]
		}
		if pos < len(text) {
			add(text[pos:], "")
		}
		return t
	})
}

// layoutMdText draws a block's text, its links clickable and its
// formulas' pictures in the room it keeps for them. key tells its links'
// buttons apart from others'.
func (u *UI) layoutMdText(gtx C, m *model.Message, t *mdText, inline []model.RichLatex, lineH unit.Sp, key, suffix string) D {
	spans, deco := t.spans[:len(t.spans):len(t.spans)], t.deco[:len(t.deco):len(t.deco)]
	if len(spans) == 0 && suffix == "" {
		return D{Size: image.Pt(0, gtx.Sp(lineH))}
	}
	if suffix != "" {
		spans = append(spans, styledtext.SpanStyle{Font: font.Font{Typeface: typeface}, Size: richSize, Content: suffix})
		deco = append(deco, 0)
	}
	var keys map[int]string
	if !u.conv.selecting {
		for j, dc := range deco {
			if dc&decoLink == 0 {
				continue
			}
			if keys == nil {
				keys = map[int]string{}
				deco = slices.Clone(deco)
			}
			k := key + ":" + itoa(j)
			keys[j] = k
			c := u.btn(k)
			if c.Clicked(gtx) {
				url, ok := t.links[j]
				if !ok {
					url = spans[j].Content
				}
				u.openLink(url)
			}
			if c.Hovered() {
				deco[j] ^= decoLink
			}
		}
	}
	var onSpan func(gtx C, idx int, d D)
	if keys != nil || t.pics != nil {
		onSpan = func(gtx C, idx int, d D) {
			if k, ok := keys[idx]; ok {
				clickable(gtx, u.btn(k), func(gtx C) D { return D{Size: d.Size} })
			}
			if n, ok := t.pics[idx]; ok {
				l := &inline[n]
				em := float32(gtx.Sp(spans[idx].Size))
				s := em / l.FontHeight
				w, h := int(float32(l.W)*s+0.5), int(float32(l.H)*s+0.5)
				x, bottom := (d.Size.X-w)/2, d.Baseline+int(l.Baseline*s+0.5)
				u.drawFormula(gtx, m, l, spans[idx].Color, image.Rect(x, bottom-h, x+w, bottom))
			}
		}
	}
	return u.layoutSpansLH(gtx, spans, deco, lineH, onSpan, nil)
}

// formulaImage is a formula's picture, tinted col, or nil when it has
// none. It loads in the background.
func (u *UI) formulaImage(m *model.Message, l *model.RichLatex, col color.NRGBA) *imgEntry {
	if l.Media == "" || l.FontHeight <= 0 {
		return nil
	}
	b, chat, id := u.backend, m.ChatID, model.RichMedia(m.ID, l.Media)
	return u.images.getWith(formulaKey(chat, id)+colorHex(col), func() []byte { return b.MediaData(chat, id) },
		func(data []byte) (image.Image, bool) { return tinted(data, col), false })
}

// formulaKey starts the keys of a formula's pictures in the image cache,
// which drops them once the picture has downloaded (see MediaEvent).
func formulaKey(chat, id string) string { return "ai:" + chat + "/" + id + "/" }

func colorHex(c color.NRGBA) string {
	return strconv.FormatUint(uint64(c.R)<<24|uint64(c.G)<<16|uint64(c.B)<<8|uint64(c.A), 16)
}

// tinted decodes a picture and paints it col, keeping its alpha.
func tinted(data []byte, col color.NRGBA) image.Image {
	if len(data) == 0 {
		return nil
	}
	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil
	}
	r := src.Bounds()
	dst := image.NewNRGBA(image.Rect(0, 0, r.Dx(), r.Dy()))
	for y := 0; y < r.Dy(); y++ {
		for x := 0; x < r.Dx(); x++ {
			_, _, _, a := src.At(r.Min.X+x, r.Min.Y+y).RGBA()
			i := dst.PixOffset(x, y)
			dst.Pix[i], dst.Pix[i+1], dst.Pix[i+2] = col.R, col.G, col.B
			dst.Pix[i+3] = uint8(a >> 8 * uint32(col.A) / 255)
		}
	}
	return dst
}

// drawFormula draws a formula's picture into r, unless privacy mode hides
// it.
func (u *UI) drawFormula(gtx C, m *model.Message, l *model.RichLatex, col color.NRGBA, r image.Rectangle) {
	if u.secret > 0 {
		return
	}
	if e := u.formulaImage(m, l, col); e != nil && e.state == imgReady && !r.Empty() {
		paintCovering(gtx, e.op, e.size, r)
	}
}

// formulaSize is the size a formula's picture is drawn at, beside text
// of size richSize; ok is false while it has none to draw.
func (u *UI) formulaSize(gtx C, m *model.Message, l *model.RichLatex, col color.NRGBA) (sz image.Point, ok bool) {
	if l == nil {
		return sz, false
	}
	e := u.formulaImage(m, l, col)
	if e == nil || e.state != imgReady {
		return sz, false
	}
	s := float32(gtx.Sp(richSize)) / l.FontHeight
	return image.Pt(int(float32(l.W)*s+0.5), int(float32(l.H)*s+0.5)), true
}

// mathText is a formula without its picture: its LaTeX as text.
func (u *UI) mathText(gtx C, expr string, col color.NRGBA) part {
	f := font.Font{Typeface: typeface, Style: font.Italic}
	return record(gtx, func(gtx C) D {
		return u.layoutSpansLH(gtx, []styledtext.SpanStyle{{Font: f, Size: 17, Color: col, Content: displayText(latexText(expr))}}, nil, 26, nil, nil)
	})
}

// mathWidth is how wide a formula on a line of its own is.
func (u *UI) mathWidth(gtx C, m *model.Message, b *mdBlock, col color.NRGBA) int {
	if sz, ok := u.formulaSize(gtx, m, b.latex, col); ok {
		return sz.X
	}
	return u.mathText(gtx, b.text, col).size.X
}

// layoutMath draws a formula on a line of its own, centered in w.
func (u *UI) layoutMath(gtx C, m *model.Message, b *mdBlock, w int, col color.NRGBA) D {
	pad := gtx.Dp(4)
	if sz, ok := u.formulaSize(gtx, m, b.latex, col); ok {
		x := max(0, (w-sz.X)/2)
		u.drawFormula(gtx, m, b.latex, col, image.Rectangle{Min: image.Pt(x, pad), Max: image.Pt(x+sz.X, pad+sz.Y)})
		return D{Size: image.Pt(w, sz.Y+2*pad)}
	}
	t := u.mathText(gtx, b.text, col)
	t.at(gtx, max(0, (w-t.size.X)/2), pad)
	return D{Size: image.Pt(w, t.size.Y+2*pad)}
}

// codeColor is the color of a code token of kind k.
func (p *Palette) codeColor(k model.CodeKind) color.NRGBA {
	switch k {
	case model.CodeKeyword:
		return p.CodeKeyword
	case model.CodeMethod:
		return p.CodeMethod
	case model.CodeString:
		return p.CodeString
	case model.CodeNumber:
		return p.CodeNumber
	case model.CodeComment:
		return p.CodeComment
	}
	return p.CodeText
}

type codeKey struct {
	key       string
	text, kw  color.NRGBA // the palette's, which colors the tokens
	tokens, n int
}

var codeSpans = memo[codeKey, []styledtext.SpanStyle]{limit: 100}

// layoutCodeBlock draws a block of code w wide: its language and a copy
// button over it, its tokens in their colors.
func (u *UI) layoutCodeBlock(gtx C, b *mdBlock, key string, w int) D {
	p := u.pal
	code := b.code
	if code == nil {
		code = []model.CodeToken{{Text: b.text}}
	}
	copyBtn := u.btn(key + ":copy")
	if copyBtn.Clicked(gtx) {
		var sb strings.Builder
		for _, t := range code {
			sb.WriteString(t.Text)
		}
		u.pendingCopy = strings.TrimRight(sb.String(), "\n")
		u.toast("Code copied")
	}
	spans := codeSpans.get(codeKey{key, p.CodeText, p.CodeKeyword, len(code), len(code[len(code)-1].Text)}, func() []styledtext.SpanStyle {
		mono := font.Font{Typeface: monoTypeface}
		out := make([]styledtext.SpanStyle, 0, len(code))
		for i, t := range code {
			s := t.Text
			if i == len(code)-1 {
				s = strings.TrimRight(s, "\n")
			}
			if s != "" {
				out = append(out, styledtext.SpanStyle{Font: mono, Size: codeSize, Color: p.codeColor(t.Kind), Content: displayText(s)})
			}
		}
		return out
	})
	pad := gtx.Dp(12)
	lang := strings.ToUpper(b.lang)
	if lang == "" {
		lang = "CODE"
	}
	head := record(gtx, u.label(12.5, lang, p.TextSecondary, labelOpts{weight: font.Medium}).Layout)
	btn := gtx.Dp(28)
	headH := max(btn, head.size.Y)
	bg := gtx
	bg.Constraints = layout.Constraints{Max: image.Pt(max(0, w-2*pad), 1<<20)}
	body := record(bg, func(gtx C) D { return u.layoutSpansLH(gtx, spans, nil, codeLineH, nil, nil) })
	h := pad + headH + gtx.Dp(8) + body.size.Y + pad
	fillRRect(gtx, image.Rect(0, 0, w, h), gtx.Dp(8), p.CodeBlock)
	head.at(gtx, pad, pad+(headH-head.size.Y)/2)
	br := image.Rect(w-pad+gtx.Dp(4)-btn, pad, w-pad+gtx.Dp(4), pad+btn)
	fillRRect(gtx, br, gtx.Dp(6), mix(p.BubbleLine, p.TableLine, u.hover(gtx, copyBtn)))
	func() {
		defer op.Offset(br.Min).Push(gtx.Ops).Pop()
		cg := gtx
		cg.Constraints = layout.Exact(br.Size())
		clickable(cg, copyBtn, func(gtx C) D {
			defer op.Offset(image.Pt((btn-gtx.Dp(16))/2, (btn-gtx.Dp(16))/2)).Push(gtx.Ops).Pop()
			drawIcon(gtx, icCopy, 16, p.CodeText)
			return D{Size: br.Size()}
		})
	}()
	body.at(gtx, pad, pad+headH+gtx.Dp(8))
	return D{Size: image.Pt(w, h)}
}

// layoutTable draws a table w wide, its columns as wide as their text
// wants, shrunk or stretched in proportion to fit.
func (u *UI) layoutTable(gtx C, m *model.Message, rows []model.RichRow, key string, w int, cols bubbleColors) D {
	p := u.pal
	n := 0
	for _, r := range rows {
		n = max(n, len(r.Cells))
	}
	if n == 0 {
		return D{}
	}
	padX, padY, headY := gtx.Dp(12), gtx.Dp(7), gtx.Dp(10)
	line := max(1, gtx.Dp(1))
	text := func(c string) *mdText { return u.mdSpans(gtx, m, c, nil, tableSize, cols.text) }
	// The width each column's text wants, on one line.
	want := make([]int, n)
	wide := gtx
	wide.Constraints = layout.Constraints{Max: image.Pt(1<<20, 1<<20)}
	for _, r := range rows {
		for c, cell := range r.Cells {
			t := text(cell)
			d := record(wide, func(gtx C) D { return u.layoutSpansLH(gtx, t.spans, t.deco, tableLineH, nil, nil) })
			want[c] = max(want[c], d.size.X+2*padX)
		}
	}
	sum := 0
	for _, x := range want {
		sum += x
	}
	colW := make([]int, n)
	used := 0
	for c := range colW {
		colW[c] = max(gtx.Dp(56), want[c]*w/max(1, sum))
		used += colW[c]
	}
	colW[n-1] = max(gtx.Dp(56), colW[n-1]+w-used)
	tw := 0
	for _, x := range colW {
		tw += x
	}
	// Each row's cells, laid out in their columns.
	cells := make([][]part, len(rows))
	rowH := make([]int, len(rows))
	for i, r := range rows {
		cells[i] = make([]part, len(r.Cells))
		for c, cl := range r.Cells {
			t := text(cl)
			cg := gtx
			cg.Constraints = layout.Constraints{Max: image.Pt(max(0, colW[c]-2*padX), 1<<20)}
			k := key + ":" + itoa(i) + ":" + itoa(c)
			cells[i][c] = record(cg, func(gtx C) D { return u.layoutMdText(gtx, m, t, nil, tableLineH, k, "") })
			rowH[i] = max(rowH[i], cells[i][c].size.Y)
		}
		py := padY
		if r.Header {
			py = headY
		}
		rowH[i] += 2 * py
	}
	h := line
	for _, x := range rowH {
		h += x + line
	}
	rad := gtx.Dp(8)
	full := image.Rect(0, 0, tw, h)
	fillRRect(gtx, full, rad, p.TableLine)
	fillRRect(gtx, full.Inset(line), rad-line, cols.bg)
	y := line
	for i, r := range rows {
		if r.Header {
			rr := clip.RRect{Rect: image.Rect(line, y, tw-line, y+rowH[i])}
			if i == 0 {
				rr.NW, rr.NE = rad-line, rad-line
			}
			if i == len(rows)-1 {
				rr.SW, rr.SE = rad-line, rad-line
			}
			paintRRect(gtx, rr, p.TableHead)
		}
		x := line
		for c := 0; c < n; c++ {
			if c > 0 {
				fillRect(gtx, image.Rect(x-line, y, x, y+rowH[i]), p.TableLine)
			}
			if c < len(cells[i]) {
				cp := cells[i][c]
				cp.at(gtx, x+padX-line, y+(rowH[i]-cp.size.Y)/2)
			}
			x += colW[c]
		}
		y += rowH[i]
		if i < len(rows)-1 {
			fillRect(gtx, image.Rect(line, y, tw-line, y+line), p.TableLine)
		}
		y += line
	}
	return D{Size: image.Pt(tw, h)}
}

// latexText writes a formula as plain text: its commands as the symbols
// they stand for, super- and subscripts in Unicode's where it has them.
func latexText(expr string) string {
	s := latexWords.Replace(expr)
	s = latexFrac.ReplaceAllString(s, "($1)/($2)")
	s = latexSqrt.ReplaceAllString(s, "√($1)")
	s = latexStyle.ReplaceAllString(s, "$1")
	s = latexScript.ReplaceAllStringFunc(s, func(m string) string {
		sup := m[0] == '^'
		body := strings.TrimSuffix(strings.TrimPrefix(m[1:], "{"), "}")
		table := subscripts
		if sup {
			table = superscripts
		}
		var b strings.Builder
		for _, r := range body {
			c, ok := table[r]
			if !ok {
				if sup {
					return "^(" + body + ")"
				}
				return "_(" + body + ")"
			}
			b.WriteRune(c)
		}
		return b.String()
	})
	s = strings.NewReplacer("{", "", "}", "", `\`, "").Replace(s)
	return strings.Join(strings.Fields(s), " ")
}

var (
	latexFrac   = regexp.MustCompile(`\\[dt]?frac\{([^{}]*)\}\{([^{}]*)\}`)
	latexSqrt   = regexp.MustCompile(`\\sqrt\{([^{}]*)\}`)
	latexStyle  = regexp.MustCompile(`\\(?:text|mathrm|mathbf|mathit|operatorname|textbf|mathcal|mathbb)\{([^{}]*)\}`)
	latexScript = regexp.MustCompile(`[\^_](?:\{[^{}]*\}|[^\s{}\\])`)
	latexWords  = strings.NewReplacer(
		`\leftarrow`, "←", `\rightarrow`, "→", `\Rightarrow`, "⇒", `\cdots`, "⋯",
		`\left`, "", `\right`, "", `\,`, " ", `\;`, " ", `\quad`, "  ", `\!`, "",
		`\times`, "×", `\cdot`, "·", `\div`, "÷", `\pm`, "±", `\mp`, "∓",
		`\leq`, "≤", `\le`, "≤", `\geq`, "≥", `\ge`, "≥", `\neq`, "≠", `\ne`, "≠", `\approx`, "≈", `\equiv`, "≡",
		`\infty`, "∞", `\sum`, "∑", `\prod`, "∏", `\int`, "∫", `\partial`, "∂", `\nabla`, "∇",
		`\to`, "→", `\in`, "∈", `\notin`, "∉",
		`\subseteq`, "⊆", `\subset`, "⊂", `\cup`, "∪", `\cap`, "∩", `\forall`, "∀", `\exists`, "∃",
		`\ldots`, "…", `\degree`, "°", `\circ`, "∘",
		`\alpha`, "α", `\beta`, "β", `\gamma`, "γ", `\delta`, "δ", `\epsilon`, "ε", `\varepsilon`, "ε",
		`\zeta`, "ζ", `\eta`, "η", `\theta`, "θ", `\iota`, "ι", `\kappa`, "κ", `\lambda`, "λ", `\mu`, "μ",
		`\nu`, "ν", `\xi`, "ξ", `\pi`, "π", `\rho`, "ρ", `\sigma`, "σ", `\tau`, "τ", `\upsilon`, "υ",
		`\phi`, "φ", `\varphi`, "φ", `\chi`, "χ", `\psi`, "ψ", `\omega`, "ω",
		`\Gamma`, "Γ", `\Delta`, "Δ", `\Theta`, "Θ", `\Lambda`, "Λ", `\Xi`, "Ξ", `\Pi`, "Π", `\Sigma`, "Σ",
		`\Phi`, "Φ", `\Psi`, "Ψ", `\Omega`, "Ω",
	)
	superscripts = runeMap("0123456789+-=()niabcdefghjklmoprstuvwxyz", "⁰¹²³⁴⁵⁶⁷⁸⁹⁺⁻⁼⁽⁾ⁿⁱᵃᵇᶜᵈᵉᶠᵍʰʲᵏˡᵐᵒᵖʳˢᵗᵘᵛʷˣʸᶻ")
	subscripts   = runeMap("0123456789+-=()aehijklmnoprstuvx", "₀₁₂₃₄₅₆₇₈₉₊₋₌₍₎ₐₑₕᵢⱼₖₗₘₙₒₚᵣₛₜᵤᵥₓ")
)

func runeMap(from, to string) map[rune]rune {
	m := map[rune]rune{}
	t := []rune(to)
	for i, r := range []rune(from) {
		m[r] = t[i]
	}
	return m
}
