package termqr

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestText(t *testing.T) {
	s, err := Text("2@abc,def,ghi", false)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSuffix(s, "\n"), "\n")
	// Version 1 is 21 modules, and the margin 2 a side: 25 wide, 13 lines.
	w := utf8.RuneCountInString(lines[0])
	if w < 25 || len(lines) != (w+1)/2 {
		t.Fatalf("%d lines of %d", len(lines), w)
	}
	for _, l := range lines {
		if utf8.RuneCountInString(l) != w {
			t.Fatalf("lines of different widths:\n%s", s)
		}
	}
	if strings.Trim(lines[0], " ") != "" {
		t.Fatalf("no margin at the top: %q", lines[0])
	}
	if a, _ := Text("x", true); !strings.HasPrefix(a, "\x1b[30;107m") {
		t.Fatal("no colors with ansi")
	}
}

func TestSVG(t *testing.T) {
	b, err := SVG("hello")
	if err != nil {
		t.Fatal(err)
	}
	if s := string(b); !strings.HasPrefix(s, "<svg") || !strings.Contains(s, "h") || !strings.HasSuffix(s, "</svg>") {
		t.Fatalf("not an SVG: %.80s", s)
	}
}
