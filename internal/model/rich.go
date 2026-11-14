package model

import "strings"

// RichResponse is an AI's answer laid out in sections (Meta AI's
// richResponseMessage, also when it's forwarded): markdown text, code,
// tables and math. Message.Text holds all of it as plain WhatsApp text,
// for previews, search and copying.
type RichResponse struct {
	Sections []RichSection
}

// RichKind is what a section of a RichResponse is.
type RichKind uint8

const (
	// RichMarkdown is text in WhatsApp's markup (*bold*, _italic_...) with
	// markdown's blocks: "#" headings, lists, "- [x]" checkboxes, "> "
	// quotes, "---" lines, ``` code, | tables |, [links](url) and $$math$$.
	RichMarkdown RichKind = iota
	RichCode              // Lang and Code
	RichTable             // Rows
	RichMath              // Latex, a formula on a line of its own
)

// RichSection is one section of a RichResponse.
type RichSection struct {
	Kind RichKind
	// Text is a markdown section's. RichInline marks each formula in it
	// that has a picture: RichInline, the formula's index in Inline in
	// decimal, RichInline.
	Text   string
	Inline []RichLatex
	Lang   string      // a code section's language, "" when unknown
	Code   []CodeToken // its text, colored by token
	Rows   []RichRow
	Latex  RichLatex
}

// RichInline brackets a formula's index in a markdown section's Text.
const RichInline = '￼'

// CodeToken is a piece of a code section, of one color.
type CodeToken struct {
	Text string
	Kind CodeKind
}

// CodeKind is what a code token is, which picks its color.
type CodeKind uint8

const (
	CodePlain CodeKind = iota
	CodeKeyword
	CodeMethod
	CodeString
	CodeNumber
	CodeComment
)

// RichRow is a table's row; each cell is markdown text.
type RichRow struct {
	Header bool
	Cells  []string
}

// RichLatex is a formula: its LaTeX source and the picture WhatsApp drew
// of it, black on transparent.
type RichLatex struct {
	Expr string
	// Media names the picture for RichMedia, "" when there's none.
	Media string
	// W and H are the picture's size in pixels, FontHeight the size of the
	// text in it (a font size in the same pixels) and Baseline how far
	// above its bottom edge the text's baseline is.
	W, H       int
	FontHeight float32
	Baseline   float32
}

// RichMedia returns the message ID that Backend.MediaData downloads a
// formula's picture under: the message's own ID, then a mark, then the
// picture's name (RichLatex.Media).
func RichMedia(msgID, media string) string {
	return msgID + richMediaMark + media
}

const richMediaMark = "~ai~"

// SplitRichMedia undoes RichMedia: ok is false for any other ID.
func SplitRichMedia(id string) (msgID, media string, ok bool) {
	return strings.Cut(id, richMediaMark)
}

// RichFromMarkdown makes a rich message of text in WhatsApp's markup with
// markdown's blocks: ``` code blocks and | tables | become sections of
// their own, and the rest markdown sections. Formulas stay text: a
// formula's picture is WhatsApp's to draw. Nil when text is blank.
func RichFromMarkdown(text string) *RichResponse {
	rr := &RichResponse{}
	var md []string
	flush := func() {
		if s := strings.Trim(strings.Join(md, "\n"), "\n"); strings.TrimSpace(s) != "" {
			rr.Sections = append(rr.Sections, RichSection{Kind: RichMarkdown, Text: s})
		}
		md = md[:0]
	}
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	for i := 0; i < len(lines); i++ {
		trim := strings.TrimSpace(lines[i])
		switch {
		case strings.HasPrefix(trim, "```") && !strings.Contains(trim[3:], "```"):
			// A fence; ```code``` on one line is text in WhatsApp's markup.
			lang := strings.TrimSpace(trim[3:])
			var code []string
			for i++; i < len(lines) && !strings.HasPrefix(strings.TrimSpace(lines[i]), "```"); i++ {
				code = append(code, lines[i])
			}
			flush()
			if c := strings.Join(code, "\n"); strings.TrimSpace(c) != "" {
				rr.Sections = append(rr.Sections, RichSection{Kind: RichCode, Lang: lang, Code: []CodeToken{{Text: c}}})
			}
		case strings.HasPrefix(trim, "|") && i+1 < len(lines) && tableRule(lines[i+1]):
			flush()
			s := RichSection{Kind: RichTable, Rows: []RichRow{{Header: true, Cells: tableRow(trim)}}}
			for i += 2; i < len(lines) && strings.HasPrefix(strings.TrimSpace(lines[i]), "|"); i++ {
				s.Rows = append(s.Rows, RichRow{Cells: tableRow(strings.TrimSpace(lines[i]))})
			}
			i--
			rr.Sections = append(rr.Sections, s)
		default:
			md = append(md, lines[i])
		}
	}
	flush()
	if len(rr.Sections) == 0 {
		return nil
	}
	return rr
}

// tableRule reports whether line is the rule under a markdown table's
// header: | --- | :---: |.
func tableRule(line string) bool {
	cells := tableRow(strings.TrimSpace(line))
	for _, c := range cells {
		c = strings.Trim(c, ":")
		if len(c) < 2 || strings.Trim(c, "-") != "" {
			return false
		}
	}
	return len(cells) > 0
}

// tableRow splits a markdown table's row into its cells.
func tableRow(line string) []string {
	line = strings.TrimSuffix(strings.TrimPrefix(line, "|"), "|")
	cells := strings.Split(line, "|")
	for i, c := range cells {
		cells[i] = strings.TrimSpace(c)
	}
	return cells
}
