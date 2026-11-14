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
