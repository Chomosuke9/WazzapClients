package ui

import (
	"image/color"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/chomosuke9/wazzapclients/internal/mock"
)

func TestParseFormatting(t *testing.T) {
	tests := []struct {
		in   string
		want []run
	}{
		{"plain", []run{{"plain", 0}}},
		{"a *bold* b", []run{{"a ", 0}, {"bold", styleBold}, {" b", 0}}},
		{"*_both_*", []run{{"both", styleBold | styleItalic}}},
		{"~gone~ and `code`", []run{{"gone", styleStrike}, {" and ", 0}, {"code", styleCode}}},
		{"2*3*4", []run{{"2*3*4", 0}}},                 // markers inside words don't count
		{"* not bold *", []run{{"* not bold *", 0}}},   // must hug the text
		{"*open\nclose*", []run{{"*open\nclose*", 0}}}, // no line breaks inside
		{"snake_case_name", []run{{"snake_case_name", 0}}},
		{"x ```a *b*``` y", []run{{"x ", 0}, {"a *b*", styleMono}, {" y", 0}}},
		{"*SUCCESS*", []run{{"SUCCESS", styleBold}}},
		{"**Quiz**", []run{{"*Quiz*", styleBold}}},
		// Markers in a mentioned name format nothing, and don't close one outside.
		{"Hai ⁨@~Adiyat~⁩.", []run{{"Hai ⁨@~Adiyat~⁩.", 0}}},
		{"~a ⁨@b~⁩ c~", []run{{"a ⁨@b~⁩ c", styleStrike}}},
		{"*⁨@Vivy⁩*", []run{{"⁨@Vivy⁩", styleBold}}},
	}
	for _, tt := range tests {
		if got := parseFormatting(tt.in); !reflect.DeepEqual(got, tt.want) {
			t.Errorf("parseFormatting(%q) = %v, want %v", tt.in, got, tt.want)
		}
	}
}

func TestParseBlocks(t *testing.T) {
	got := parseBlocks("hi\n> a\n> b\n\n- x\n2. y\n```\n> no\n```")
	want := []textBlock{
		{blockText, "", "hi"},
		{blockQuote, "", "a\nb"},
		{blockText, "", ""},
		{blockList, "•", "x"},
		{blockList, "2.", "y"},
		{blockText, "", "```\n> no\n```"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("parseBlocks = %q, want %q", got, want)
	}
}

func TestPlainText(t *testing.T) {
	if got := plainText("hi ⁨@Vivy⁩, *done*"); got != "hi @Vivy, done" {
		t.Errorf("plainText = %q", got)
	}
}

func TestDisplayText(t *testing.T) {
	if got := displayText("a\tb\x02c\x7f\n⁨@Vi⁩"); got != "a bc\n@Vi" {
		t.Errorf("displayText = %q", got)
	}
	if s := "plain text ✓"; displayText(s) != s {
		t.Error("clean text changed")
	}
}

func TestReadMoreCut(t *testing.T) {
	short := strings.Repeat("word ", 100)
	if got, more := readMoreCut(short, 0); more || got != short {
		t.Errorf("short text was cut")
	}
	long := strings.Repeat("word ", 400) // 2000 runes
	got, more := readMoreCut(long, 0)
	if !more || !strings.HasSuffix(got, "…\u00a0") {
		t.Fatalf("long text: more %v, %q", more, got[len(got)-10:])
	}
	if n := utf8.RuneCountInString(got); n > readMoreRunes+2 || n < readMoreRunes-40 {
		t.Errorf("cut to %d runes", n)
	}
	if strings.HasSuffix(strings.TrimSuffix(got, "…\u00a0"), "wor") {
		t.Errorf("cut inside a word: %q", got[len(got)-12:])
	}
	if got, more := readMoreCut(long, 1); more || got != long {
		t.Errorf("one click didn't show the rest")
	}
	lines := strings.Repeat("line\n", 40)
	if got, _ := readMoreCut(lines, 0); strings.Count(got, "\n") != readMoreLines-1 {
		t.Errorf("cut to %d lines", strings.Count(got, "\n")+1)
	}
	// Never inside a mention, and an open code block is closed.
	m := strings.Repeat("a", readMoreRunes-3) + " ⁨@Somebody Long⁩ " + strings.Repeat("b ", 300)
	if got, _ := readMoreCut(m, 0); strings.Contains(got, "⁨") {
		t.Errorf("cut inside a mention: %q", got[len(got)-20:])
	}
	// Bold and italic cut open are closed again.
	bold := strings.Repeat("a ", readMoreRunes/2-5) + "*_" + strings.Repeat("bold ", 100) + "end_* tail " + strings.Repeat("c ", 300)
	if got, _ := readMoreCut(bold, 0); !strings.HasSuffix(got, "_*…\u00a0") {
		t.Errorf("bold left open: %q", got[len(got)-20:])
	}
	code := "```\n" + strings.Repeat("x = 1\n", 40) + "```"
	if got, _ := readMoreCut(code, 0); strings.Count(got, "```") != 2 {
		t.Errorf("code block left open: %q", got)
	}
}

func TestPilled(t *testing.T) {
	u := New(mock.New())
	spans, deco := u.richSpans("hi \u2068@You\u2069 \u2068\u2063@You\u2069 \u2068\u2063@all\u2069 \u2068\u2062@admin\u2069", 15, color.NRGBA{}, false, pillMe)
	var pills []string
	for i, d := range deco {
		if d&decoPill != 0 {
			pills = append(pills, strings.TrimSpace(strings.ReplaceAll(spans[i].Content, "\u00a0", " ")))
		}
		if strings.ContainsAny(spans[i].Content, "\u2062\u2063\u2068\u2069") {
			t.Errorf("span %q keeps a mark", spans[i].Content)
		}
	}
	// A contact saved as "You" isn't you; "@admin" needs pillAdmin.
	if want := []string{"@You", "@all"}; !reflect.DeepEqual(pills, want) {
		t.Errorf("pills %q, want %q", pills, want)
	}
}
