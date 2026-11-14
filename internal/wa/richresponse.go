package wa

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"regexp"
	"strconv"
	"strings"

	"github.com/polymorfa/hypermeow/proto/waAICommon"
	"github.com/polymorfa/hypermeow/proto/waAICommonDeprecated"
	"github.com/polymorfa/hypermeow/proto/waE2E"
	"google.golang.org/protobuf/proto"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

// Meta AI answers with a richResponseMessage, which comes the same way
// when someone forwards one (in a botForwardedMessage, unwrapped like
// any envelope). It says everything twice:
//
//   - submessages, the older form: mostly one TEXT submessage holding the
//     whole answer in plain markdown (**bold**, ``` code, | tables |);
//   - unifiedResponse.data, JSON that WhatsApp draws: sections of markdown
//     in WhatsApp's own markup (*bold*, _italic_), code split into
//     highlighted tokens, tables, and formulas as pictures.
//
// The formulas' pictures (black on transparent PNGs) are on WhatsApp's
// CDN, encrypted like any picture: the message's messageContextInfo
// carries their keys in botMetadata.unifiedResponseMutation, each under
// the SHA-256 (hex) of the picture's URL in the JSON. They download as
// images (mediaBlob) under model.RichMedia IDs.

// richContent describes an AI's answer; outer is the message as it came,
// envelopes and all, whose messageContextInfo holds the pictures' keys.
func richContent(outer *waE2E.Message, r *waE2E.AIRichResponseMessage) content {
	pics := richPictures(outer)
	rr := parseUnified(r.GetUnifiedResponse().GetData(), pics)
	if rr == nil {
		rr = parseSubmessages(r.GetSubmessages())
	}
	c := content{ctx: r.GetContextInfo()}
	if rr != nil {
		c.text, c.rich = richText(rr), rr
	}
	return c
}

// richPictures returns the keys of an answer's pictures by name (the
// SHA-256 of their URL), from the messageContextInfo of m or of any
// message wrapped in it.
func richPictures(m *waE2E.Message) map[string]*waAICommon.BotMediaMetadata {
	var out map[string]*waAICommon.BotMediaMetadata
	for i := 0; i < 6 && m != nil; i++ {
		for _, d := range m.GetMessageContextInfo().GetBotMetadata().GetUnifiedResponseMutation().GetMediaDetailsMetadataList() {
			md := d.GetHighResMedia()
			if md.GetDirectPath() == "" {
				md = d.GetPreviewMedia()
			}
			if d.GetID() == "" || md.GetDirectPath() == "" || md.GetMediaKey() == "" {
				continue
			}
			if out == nil {
				out = map[string]*waAICommon.BotMediaMetadata{}
			}
			out[strings.ToLower(d.GetID())] = md
		}
		m = unwrapOnce(m)
	}
	return out
}

// richPictureName is the name a picture's keys are kept under.
func richPictureName(url string) string {
	h := sha256.Sum256([]byte(url))
	return hex.EncodeToString(h[:])
}

// richMediaBlob returns the media blob of the picture named name in the
// stored payload raw: an ImageMessage, which downloads like a photo.
func richMediaBlob(raw []byte, name string) []byte {
	var m waE2E.Message
	if len(raw) == 0 || proto.Unmarshal(raw, &m) != nil {
		return nil
	}
	md := richPictures(&m)[name]
	if md == nil {
		return nil
	}
	b64 := func(s string) []byte {
		b, _ := base64.StdEncoding.DecodeString(s)
		return b
	}
	img := &waE2E.ImageMessage{
		DirectPath:    proto.String(md.GetDirectPath()),
		MediaKey:      b64(md.GetMediaKey()),
		FileSHA256:    b64(md.GetFileSHA256()),
		FileEncSHA256: b64(md.GetFileEncSHA256()),
		Mimetype:      proto.String(md.GetMimetype()),
	}
	if ts := md.GetMediaKeyTimestamp(); ts > 0 {
		img.MediaKeyTimestamp = proto.Int64(ts / 1000) // sent in milliseconds
	}
	return marshal(img)
}

// The JSON of a unified response, as much of it as is drawn.
type unified struct {
	Sections []struct {
		ViewModel struct {
			Primitive  *unifiedPrimitive  `json:"primitive"`
			Primitives []unifiedPrimitive `json:"primitives"`
		} `json:"view_model"`
	} `json:"sections"`
}

type unifiedPrimitive struct {
	Type           string `json:"__typename"`
	Text           string `json:"text"`
	InlineEntities []struct {
		Key      string       `json:"key"`
		Metadata unifiedLatex `json:"metadata"`
	} `json:"inline_entities"`
	Language   string `json:"language"`
	CodeBlocks []struct {
		Content string `json:"content"`
		Type    string `json:"type"`
	} `json:"code_blocks"`
	Rows []struct {
		IsHeader      bool     `json:"is_header"`
		Cells         []string `json:"cells"`
		MarkdownCells []struct {
			Text string `json:"text"`
		} `json:"markdown_cells"`
	} `json:"rows"`
	LatexImage *unifiedImage `json:"latex_image"`
	Expr       string        `json:"latex_expression"`
	FontHeight float64       `json:"font_height"`
	Baseline   float64       `json:"baseline_from_bottom"`
}

type unifiedLatex struct {
	LatexImage *unifiedImage `json:"latex_image"`
	Expr       string        `json:"latex_expression"`
	FontHeight float64       `json:"font_height"`
	Baseline   float64       `json:"baseline_from_bottom"`
}

type unifiedImage struct {
	URL    string  `json:"url"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}

// latex makes a formula of a picture's description.
func latex(img *unifiedImage, expr string, fontHeight, baseline float64, pics map[string]*waAICommon.BotMediaMetadata) model.RichLatex {
	l := model.RichLatex{Expr: strings.TrimSpace(expr)}
	if img == nil || img.Width <= 0 || img.Height <= 0 || fontHeight <= 0 {
		return l
	}
	if name := richPictureName(img.URL); pics[name] != nil {
		l.Media = name
		l.W, l.H = int(img.Width), int(img.Height)
		l.FontHeight, l.Baseline = float32(fontHeight), float32(baseline)
		if l.Baseline <= 0 {
			// Without it, the formula's middle sits on the text's.
			l.Baseline = float32(img.Height-fontHeight) / 2
		}
	}
	return l
}

// inlineEntity is a formula in a markdown section's text:
// {{IE_0}}$x^2$${{/IE_0}}.
var inlineEntity = regexp.MustCompile(`(?s)\{\{(IE_\d+)\}\}(.*?)\{\{/IE_\d+\}\}`)

// parseUnified reads a unified response; nil when it has nothing to draw.
func parseUnified(data []byte, pics map[string]*waAICommon.BotMediaMetadata) *model.RichResponse {
	var u unified
	if len(data) == 0 || json.Unmarshal(data, &u) != nil {
		return nil
	}
	rr := &model.RichResponse{}
	add := func(p *unifiedPrimitive) {
		switch t := p.Type; {
		case strings.Contains(t, "Code"):
			s := model.RichSection{Kind: model.RichCode, Lang: p.Language}
			for _, b := range p.CodeBlocks {
				if b.Content != "" {
					s.Code = append(s.Code, model.CodeToken{Text: b.Content, Kind: codeKind(b.Type)})
				}
			}
			if len(s.Code) > 0 {
				rr.Sections = append(rr.Sections, s)
			}
		case strings.Contains(t, "Table"):
			s := model.RichSection{Kind: model.RichTable}
			for _, r := range p.Rows {
				row := model.RichRow{Header: r.IsHeader}
				if len(r.MarkdownCells) > 0 {
					for _, c := range r.MarkdownCells {
						row.Cells = append(row.Cells, c.Text)
					}
				} else {
					for _, c := range r.Cells {
						row.Cells = append(row.Cells, markdownToWA(c))
					}
				}
				s.Rows = append(s.Rows, row)
			}
			if len(s.Rows) > 0 {
				rr.Sections = append(rr.Sections, s)
			}
		case strings.Contains(t, "Latex"):
			l := latex(p.LatexImage, p.Expr, p.FontHeight, p.Baseline, pics)
			if l.Expr != "" || l.Media != "" {
				rr.Sections = append(rr.Sections, model.RichSection{Kind: model.RichMath, Latex: l})
			}
		case p.Text != "":
			s := model.RichSection{Kind: model.RichMarkdown}
			ents := map[string]unifiedLatex{}
			for _, e := range p.InlineEntities {
				ents[e.Key] = e.Metadata
			}
			s.Text = inlineEntity.ReplaceAllStringFunc(p.Text, func(m string) string {
				sub := inlineEntity.FindStringSubmatch(m)
				e, ok := ents[sub[1]]
				expr := strings.Trim(strings.TrimSpace(sub[2]), "$")
				if e.Expr != "" {
					expr = e.Expr
				}
				if !ok {
					return "$" + expr + "$"
				}
				s.Inline = append(s.Inline, latex(e.LatexImage, expr, e.FontHeight, e.Baseline, pics))
				return string(model.RichInline) + strconv.Itoa(len(s.Inline)-1) + string(model.RichInline)
			})
			rr.Sections = append(rr.Sections, s)
		}
	}
	for i := range u.Sections {
		vm := &u.Sections[i].ViewModel
		if vm.Primitive != nil {
			add(vm.Primitive)
		}
		for j := range vm.Primitives {
			add(&vm.Primitives[j])
		}
	}
	if len(rr.Sections) == 0 {
		return nil
	}
	return rr
}

func codeKind(t string) model.CodeKind {
	switch strings.ToUpper(t) {
	case "KEYWORD":
		return model.CodeKeyword
	case "METHOD", "FUNCTION":
		return model.CodeMethod
	case "STR", "STRING":
		return model.CodeString
	case "NUMBER", "NUM":
		return model.CodeNumber
	case "COMMENT":
		return model.CodeComment
	}
	return model.CodePlain
}

// parseSubmessages reads the older form of an answer; nil when it has
// nothing to draw.
func parseSubmessages(subs []*waAICommonDeprecated.AIRichResponseSubMessage) *model.RichResponse {
	rr := &model.RichResponse{}
	for _, s := range subs {
		switch s.GetMessageType() {
		case waAICommonDeprecated.AIRichResponseSubMessageType_AI_RICH_RESPONSE_TEXT:
			if t := s.GetMessageText(); t != "" {
				rr.Sections = append(rr.Sections, model.RichSection{Kind: model.RichMarkdown, Text: markdownToWA(t)})
			}
		case waAICommonDeprecated.AIRichResponseSubMessageType_AI_RICH_RESPONSE_CODE:
			cm := s.GetCodeMetadata()
			sec := model.RichSection{Kind: model.RichCode, Lang: cm.GetCodeLanguage()}
			for _, b := range cm.GetCodeBlocks() {
				if b.GetCodeContent() != "" {
					sec.Code = append(sec.Code, model.CodeToken{Text: b.GetCodeContent(), Kind: model.CodeKind(b.GetHighlightType())})
				}
			}
			if len(sec.Code) > 0 {
				rr.Sections = append(rr.Sections, sec)
			}
		case waAICommonDeprecated.AIRichResponseSubMessageType_AI_RICH_RESPONSE_TABLE:
			sec := model.RichSection{Kind: model.RichTable}
			for _, r := range s.GetTableMetadata().GetRows() {
				row := model.RichRow{Header: r.GetIsHeading()}
				for _, c := range r.GetItems() {
					row.Cells = append(row.Cells, markdownToWA(c))
				}
				sec.Rows = append(sec.Rows, row)
			}
			if len(sec.Rows) > 0 {
				rr.Sections = append(rr.Sections, sec)
			}
		case waAICommonDeprecated.AIRichResponseSubMessageType_AI_RICH_RESPONSE_LATEX:
			if t := s.GetLatexMetadata().GetText(); t != "" {
				rr.Sections = append(rr.Sections, model.RichSection{Kind: model.RichMarkdown, Text: markdownToWA(t)})
			}
		}
	}
	if len(rr.Sections) == 0 {
		return nil
	}
	return rr
}

var (
	mdBoldItalic = regexp.MustCompile(`\*\*\*(\S(?:.*?\S)?)\*\*\*`)
	mdBold       = regexp.MustCompile(`\*\*(\S(?:.*?\S)?)\*\*|__(\S(?:.*?\S)?)__`)
	mdItalic     = regexp.MustCompile(`(^|[^\w*])\*(\S(?:[^*\n]*?\S)?)\*`)
	mdStrike     = regexp.MustCompile(`~~(\S(?:.*?\S)?)~~`)
)

// markdownToWA turns markdown's inline styles into WhatsApp's: **bold**
// becomes *bold*, *italic* _italic_ and ~~struck~~ ~struck~. Code, in
// backticks, is left as it is.
func markdownToWA(s string) string {
	if !strings.ContainsAny(s, "*_~") {
		return s
	}
	parts := strings.Split(s, "`")
	for i := 0; i < len(parts); i += 2 {
		p := parts[i]
		// \x01 stands for a bold marker until the italics are done.
		p = mdBoldItalic.ReplaceAllString(p, "\x01_${1}_\x01")
		p = mdBold.ReplaceAllString(p, "\x01${1}${2}\x01")
		p = mdItalic.ReplaceAllString(p, "${1}_${2}_")
		p = mdStrike.ReplaceAllString(p, "~${1}~")
		parts[i] = strings.ReplaceAll(p, "\x01", "*")
	}
	return strings.Join(parts, "`")
}

// richText is an answer as plain WhatsApp text: what previews, search,
// copying and forwarding see.
func richText(rr *model.RichResponse) string {
	var parts []string
	for _, s := range rr.Sections {
		switch s.Kind {
		case model.RichMarkdown:
			parts = append(parts, richInlineText(s))
		case model.RichCode:
			var b strings.Builder
			for _, t := range s.Code {
				b.WriteString(t.Text)
			}
			parts = append(parts, "```\n"+strings.Trim(b.String(), "\n")+"\n```")
		case model.RichTable:
			var rows []string
			for _, r := range s.Rows {
				rows = append(rows, strings.Join(r.Cells, " | "))
			}
			parts = append(parts, strings.Join(rows, "\n"))
		case model.RichMath:
			parts = append(parts, s.Latex.Expr)
		}
	}
	return strings.Join(parts, "\n\n")
}

// richInlineText is a markdown section's text with its formulas' sources
// in place of their marks.
func richInlineText(s model.RichSection) string {
	if len(s.Inline) == 0 {
		return s.Text
	}
	mark := string(model.RichInline)
	var b strings.Builder
	rest := s.Text
	for {
		i := strings.Index(rest, mark)
		if i < 0 {
			break
		}
		j := strings.Index(rest[i+len(mark):], mark)
		if j < 0 {
			break
		}
		b.WriteString(rest[:i])
		if n, err := strconv.Atoi(rest[i+len(mark) : i+len(mark)+j]); err == nil && n < len(s.Inline) {
			b.WriteString(s.Inline[n].Expr)
		}
		rest = rest[i+2*len(mark)+j:]
	}
	b.WriteString(rest)
	return b.String()
}
