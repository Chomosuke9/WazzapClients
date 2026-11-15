package ui

import (
	"image"
	"testing"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/chomosuke9/wazzapclients/internal/mock"
	"github.com/chomosuke9/wazzapclients/internal/model"
)

func TestParseMarkdown(t *testing.T) {
	src := "Intro\nsecond line\n\n# Title\n- one\n  - nested\n- [x] done\n- [ ] todo\n2. two\n" +
		"> quoted\n> more\n\n---\n\n```go\nfunc f() {}\n```\n\n| A | B |\n| --- | --- |\n| 1 | 2 |\n\n$$E = mc^2$$\n*1. bold, no item*"
	type want struct {
		kind  mdKind
		level int
		text  string
	}
	wants := []want{
		{mdPara, 0, "Intro\nsecond line"}, {mdHeading, 1, "Title"}, {mdBullet, 0, "one"}, {mdBullet, 1, "nested"},
		{mdCheck, 0, "done"}, {mdCheck, 0, "todo"}, {mdNumber, 0, "two"}, {mdQuote, 0, "quoted\nmore"},
		{mdRule, 0, ""}, {mdCode, 0, "func f() {}"}, {mdTable, 0, ""}, {mdMath, 0, "E = mc^2"}, {mdPara, 0, "*1. bold, no item*"},
	}
	got := parseMarkdownNow(src)
	if len(got) != len(wants) {
		t.Fatalf("%d blocks, want %d: %+v", len(got), len(wants), got)
	}
	for i, w := range wants {
		if g := got[i]; g.kind != w.kind || g.level != w.level || g.text != w.text {
			t.Errorf("block %d: %+v, want %+v", i, g, w)
		}
	}
	if !got[4].checked || got[5].checked {
		t.Error("checkboxes' ticks are wrong")
	}
	if got[9].lang != "go" || len(got[10].rows) != 2 || !got[10].rows[0].Header || got[10].rows[1].Cells[1] != "2" {
		t.Errorf("code %+v, table %+v", got[9], got[10])
	}
	if !got[1].gap || got[2].gap {
		t.Error("blank lines aren't where they were")
	}
}

func TestLatexText(t *testing.T) {
	for in, want := range map[string]string{
		"x^2 + y^2 = z^2":            "x² + y² = z²",
		"x^{2}":                      "x²",
		"H_2O":                       "H₂O",
		`\frac{a}{b} \leq \pi`:       "(a)/(b) ≤ π",
		`\alpha^{q}`:                 "α^(q)",
		`\sqrt{x} \cdots \leftarrow`: "√(x) ⋯ ←",
		`\text{speed} = \Delta x`:    "speed = Δ x",
	} {
		if got := latexText(in); got != want {
			t.Errorf("latexText(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestRichAnswerBubble lays out the demo's forwarded answer: it draws as
// an attachment, its labeled link opens its URL, and a formula without a
// picture shows as text.
func TestRichAnswerBubble(t *testing.T) {
	u := New(mock.New())
	u.Start(func() {})
	u.SelectID("bank")
	u.applyEvents()
	var m *model.Message
	for _, x := range u.msgs {
		if x.Rich != nil {
			m = x
		}
	}
	if m == nil || !hasAttachment(m) || u.jumbo(u.selected, m) != nil {
		t.Fatal("no answer in the chat, or it doesn't draw as one")
	}
	var ops op.Ops
	gtx := layout.Context{Ops: &ops, Now: testNow(), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1},
		Constraints: layout.Constraints{Max: image.Pt(600, 1<<20)}}
	cols := bubbleColors{bg: u.pal.BubbleIn, card: u.pal.QuoteIn, text: u.pal.Text, secondary: u.pal.TextSecondary}
	d := u.layoutRichResponse(gtx, m, 600, 40, 16, cols)
	if d.Size.X != 600 || d.Size.Y < 600 {
		t.Errorf("the answer is %v, want the full width and tall", d.Size)
	}
	u.Layout(layout.Context{Ops: new(op.Ops), Now: testNow(), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1},
		Constraints: layout.Exact(image.Pt(1400, 900))})

	link := u.mdSpans(gtx, m, "- [Meta AI help](https://www.meta.ai) and www.example.com", nil, richSize, u.pal.Text)
	var urls []string
	for i, s := range link.spans {
		if link.deco[i]&decoLink != 0 {
			url, ok := link.links[i]
			if !ok {
				url = s.Content
			}
			urls = append(urls, s.Content+"→"+url)
		}
	}
	if len(urls) != 2 || urls[0] != "Meta AI help→https://www.meta.ai" || urls[1] != "www.example.com→www.example.com" {
		t.Errorf("links %q", urls)
	}

	math := u.mdSpans(gtx, m, "is ￼0￼", []model.RichLatex{{Expr: "H_2O"}}, richSize, u.pal.Text)
	if n := len(math.spans); n != 2 || math.spans[1].Content != "H₂O" || len(math.pics) != 0 {
		t.Errorf("a formula without a picture: %+v", math.spans)
	}
}
