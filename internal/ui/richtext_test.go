package ui

import (
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"
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
	if !more || !strings.HasSuffix(got, "… ") {
		t.Fatalf("long text: more %v, %q", more, got[len(got)-10:])
	}
	if n := utf8.RuneCountInString(got); n > readMoreRunes+2 || n < readMoreRunes-40 {
		t.Errorf("cut to %d runes", n)
	}
	if strings.HasSuffix(strings.TrimSuffix(got, "… "), "wor") {
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
	code := "```\n" + strings.Repeat("x = 1\n", 40) + "```"
	if got, _ := readMoreCut(code, 0); strings.Count(got, "```") != 2 {
		t.Errorf("code block left open: %q", got)
	}
}
