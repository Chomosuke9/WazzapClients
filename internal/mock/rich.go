package mock

import (
	"time"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

// aiAnswer is a forwarded Meta AI answer in each of its sections' kinds.
// Its formulas have no pictures, so they show as text.
func aiAnswer(t time.Time) *model.Message {
	rr := &model.RichResponse{Sections: []model.RichSection{
		{Kind: model.RichMarkdown, Text: "Here is every text format I can use in this chat:\n\n" +
			"*1. Text styles*\n\n- *Bold*: *This is bold*\n- *Italic*: _This is italic_\n" +
			"- *Strikethrough*: ~This is crossed out~\n- *Inline code*: `print(\"hello\")`\n" +
			"- *Inline math*: ￼0￼\n- *Subscript*: ￼1￼\n\n" +
			"*2. Headings*\n\n# Heading 1\n## Heading 2\n### Heading 3\n\n" +
			"*3. Lists*\n\n1. First item\n2. Second item\n\n- [x] Done item\n- [ ] Todo item\n\n" +
			"> This is a blockquote.\n\n*4. Code blocks*",
			Inline: []model.RichLatex{{Expr: "x^2 + y^2 = z^2"}, {Expr: "H_2O"}}},
		{Kind: model.RichCode, Lang: "python", Code: []model.CodeToken{
			{Text: "def", Kind: model.CodeKeyword}, {Text: " "}, {Text: "hello", Kind: model.CodeMethod},
			{Text: "():\n    print("}, {Text: "\"Hello, world!\"", Kind: model.CodeString}, {Text: ")"}}},
		{Kind: model.RichMarkdown, Text: "*5. Tables*"},
		{Kind: model.RichTable, Rows: []model.RichRow{
			{Header: true, Cells: []string{"Feature", "Example", "Use"}},
			{Cells: []string{"Bold", "*Bold text*", "Emphasis"}},
			{Cells: []string{"Code", "`Code text`", "Commands"}}}},
		{Kind: model.RichMarkdown, Text: "*6. Links*\n\n- [Meta AI help page](https://www.meta.ai)\n\n---\n\n*7. Display math*"},
		{Kind: model.RichMath, Latex: model.RichLatex{Expr: "E = mc^2"}},
		{Kind: model.RichMarkdown, Text: "Want me to redo your last list using all of these at once?"},
	}}
	return &model.Message{Text: "Here is every text format I can use in this chat", Time: t, Forwarded: true, Rich: rr}
}
