package ui

import (
	"image/color"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"gioui.org/font"
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
	styleMono
)

var markerStyle = map[byte]textStyle{'*': styleBold, '_': styleItalic, '~': styleStrike, '`': styleMono}

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
		if i+1 >= len(s) || s[i+1] == ' ' || s[i+1] == '\n' || s[i+1] == s[i] {
			continue
		}
		j := closingMarker(s, i)
		if j < 0 {
			continue
		}
		if i > start {
			out = append(out, run{s[start:i], style})
		}
		if st == styleMono {
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
func (u *UI) richSpans(text string, size unit.Sp, col color.NRGBA, italic bool) []styledtext.SpanStyle {
	p := u.pal
	base := font.Font{Typeface: typeface}
	if italic {
		base.Style = font.Italic
	}
	var spans []styledtext.SpanStyle
	add := func(s string, f font.Font, c color.NRGBA) {
		if s != "" {
			spans = append(spans, styledtext.SpanStyle{Font: f, Size: size, Color: c, Content: s})
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
		if r.style&styleMono != 0 {
			f.Typeface = "Consolas, Cascadia Mono, Courier New, monospace"
		}
		// Split out mentions, then links.
		rest := r.text
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
	return spans
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
