package ui

import (
	"reflect"
	"testing"
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
