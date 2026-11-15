package wa

import (
	"encoding/base64"
	"encoding/json"
	"testing"

	"github.com/polymorfa/hypermeow/proto/waE2E"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

const (
	richInlineURL  = "https://mmg.whatsapp.net/v/t62.7118-24/1_n.enc?ccb=11-4"
	richDisplayURL = "https://mmg.whatsapp.net/v/t62.7118-24/2_n.enc?ccb=11-4"
)

// forwardedAnswer is a Meta AI answer as someone forwards it: the keys of
// its formulas' pictures in the outer messageContextInfo, the answer in a
// botForwardedMessage. Only the inline formula's picture has keys.
func forwardedAnswer(t *testing.T, unified any) *waE2E.Message {
	t.Helper()
	data, err := json.Marshal(unified)
	if err != nil {
		t.Fatal(err)
	}
	key := base64.StdEncoding.EncodeToString(make([]byte, 32))
	js, _ := json.Marshal(map[string]any{
		"messageContextInfo": map[string]any{"botMetadata": map[string]any{"unifiedResponseMutation": map[string]any{
			"mediaDetailsMetadataList": []any{map[string]any{"ID": richPictureName(richInlineURL), "previewMedia": map[string]any{
				"fileSHA256": key, "mediaKey": key, "fileEncSHA256": key, "directPath": "/o1/v/t24/f2/m232/x?oe=6B1FDB04",
				"mediaKeyTimestamp": "1794663310233", "mimetype": "image/jpeg"}}}}}},
		"botForwardedMessage": map[string]any{"message": map[string]any{"richResponseMessage": map[string]any{
			"messageType":     "AI_RICH_RESPONSE_TYPE_STANDARD",
			"submessages":     []any{map[string]any{"messageType": "AI_RICH_RESPONSE_TEXT", "messageText": "**Old** form"}},
			"unifiedResponse": map[string]any{"data": base64.StdEncoding.EncodeToString(data)},
			"contextInfo":     map[string]any{"forwardingScore": 1, "isForwarded": true, "forwardOrigin": "META_AI"}}}},
	})
	var m waE2E.Message
	if err := protojson.Unmarshal(js, &m); err != nil {
		t.Fatal(err)
	}
	return &m
}

func section(typename string, fields map[string]any) map[string]any {
	fields["__typename"] = typename
	return map[string]any{"view_model": map[string]any{"__typename": "GenAISingleLayoutViewModel", "primitive": fields}}
}

func latexImage(url string) map[string]any {
	return map[string]any{"url": url, "mime_type": "image/png", "width": 458, "height": 120}
}

// TestRichAnswer reads a forwarded answer's sections, its formulas and
// the keys of their pictures, and its text.
func TestRichAnswer(t *testing.T) {
	m := forwardedAnswer(t, map[string]any{"sections": []any{
		section("GenAIMarkdownTextUXPrimitive", map[string]any{
			"text": "*Math*: {{IE_0}}$x^2${{/IE_0}}",
			"inline_entities": []any{map[string]any{"key": "IE_0", "metadata": map[string]any{
				"__typename": "GenAILatexItem", "latex_image": latexImage(richInlineURL), "latex_expression": "x^2",
				"font_height": 83.3, "padding": 15.0, "baseline_from_bottom": 32.0}}}}),
		section("GenAICodeUXPrimitive", map[string]any{"language": "python", "code_blocks": []any{
			map[string]any{"content": "def", "type": "KEYWORD"}, map[string]any{"content": " f():\n", "type": "DEFAULT"}}}),
		section("GenATableUXPrimitive", map[string]any{"rows": []any{
			map[string]any{"is_header": true, "cells": []string{"A", "B"},
				"markdown_cells": []any{map[string]any{"text": "A"}, map[string]any{"text": "B"}}},
			map[string]any{"cells": []string{"**x**", "y"}}}}),
		section("GenAILatexUXPrimitive", map[string]any{"latex_image": latexImage(richDisplayURL),
			"latex_expression": "E = mc^2", "font_height": 83.3}),
		section("GenAISomethingNewUXPrimitive", map[string]any{}),
	}})
	c := describe(m)
	if !c.ctx.GetIsForwarded() || c.rich == nil {
		t.Fatalf("forwarded %v, rich %v", c.ctx.GetIsForwarded(), c.rich)
	}
	kinds := []model.RichKind{model.RichMarkdown, model.RichCode, model.RichTable, model.RichMath}
	if len(c.rich.Sections) != len(kinds) {
		t.Fatalf("%d sections, want %d: %+v", len(c.rich.Sections), len(kinds), c.rich.Sections)
	}
	for i, k := range kinds {
		if c.rich.Sections[i].Kind != k {
			t.Errorf("section %d is %d, want %d", i, c.rich.Sections[i].Kind, k)
		}
	}
	md := c.rich.Sections[0]
	if want := "*Math*: ￼0￼"; md.Text != want || len(md.Inline) != 1 {
		t.Fatalf("markdown %q with %d formulas, want %q", md.Text, len(md.Inline), want)
	}
	if l := md.Inline[0]; l.Expr != "x^2" || l.Media != richPictureName(richInlineURL) || l.W != 458 || l.Baseline != 32 {
		t.Errorf("inline formula %+v", l)
	}
	if l := c.rich.Sections[3].Latex; l.Expr != "E = mc^2" || l.Media != "" {
		t.Errorf("a formula without keys: %+v, want no picture", l)
	}
	if got := c.rich.Sections[2].Rows[1].Cells[0]; got != "*x*" {
		t.Errorf("a plain markdown cell reads %q, want WhatsApp's *x*", got)
	}
	want := "*Math*: x^2\n\n```\ndef f():\n```\n\nA | B\n*x* | y\n\nE = mc^2"
	if c.text != want {
		t.Errorf("text %q, want %q", c.text, want)
	}

	// The picture downloads as a photo, with the keys from the context.
	blob := richMediaBlob(marshal(m), md.Inline[0].Media)
	var img waE2E.ImageMessage
	if err := proto.Unmarshal(blob, &img); err != nil || len(img.GetMediaKey()) != 32 || img.GetDirectPath() == "" {
		t.Fatalf("picture's blob %v (%v)", &img, err)
	}
	if richMediaBlob(marshal(m), "nope") != nil {
		t.Error("a picture the message has no keys for has a blob")
	}
}

// TestRichAnswerFallback reads the submessages of an answer whose unified
// response is unreadable.
func TestRichAnswerFallback(t *testing.T) {
	c := describe(forwardedAnswer(t, "not an object"))
	if c.rich == nil || len(c.rich.Sections) != 1 || c.rich.Sections[0].Text != "*Old* form" {
		t.Fatalf("fallback %+v", c.rich)
	}
	if c.text != "*Old* form" {
		t.Errorf("text %q", c.text)
	}
}

func TestMarkdownToWA(t *testing.T) {
	for in, want := range map[string]string{
		"**bold** and *italic*":      "*bold* and _italic_",
		"***both*** ~~gone~~":        "*_both_* ~gone~",
		"* a list item":              "* a list item",
		"`**code**` and __bold__":    "`**code**` and *bold*",
		"2 * 3 * 4":                  "2 * 3 * 4",
		"plain":                      "plain",
		"- **Bold**: **This is it**": "- *Bold*: *This is it*",
	} {
		if got := markdownToWA(in); got != want {
			t.Errorf("markdownToWA(%q) = %q, want %q", in, got, want)
		}
	}
}
