package model

import (
	"testing"
	"time"
)

func TestExpandSnippet(t *testing.T) {
	vars := SnippetValues("Rina Putri", "Work", "Bagus", "When?", time.Date(2026, 10, 5, 14, 5, 0, 0, time.UTC))
	for in, want := range map[string]string{
		"Hi {first}, {greeting}!":        "Hi Rina, Good afternoon!",
		"{NAME} in {chat} from {me}":     "Rina Putri in Work from Bagus",
		"> {quote}\n{day} {date} {time}": "> When?\nMonday 5 October 2026 14:05",
		`{"a": 1} {unknown} {} {name`:    `{"a": 1} {unknown} {} {name`,
		`\{name} stays, \x and {name}`:   `{name} stays, \x and Rina Putri`,
		"no variables":                   "no variables",
	} {
		if got := ExpandSnippet(in, vars); got != want {
			t.Errorf("ExpandSnippet(%q) = %q, want %q", in, got, want)
		}
	}
	if !SnippetUses("Hi {Mention}", "mention") || SnippetUses(`Hi \{mention}`, "mention") || SnippetUses("Hi {name}", "mention") {
		t.Error("SnippetUses")
	}
	if got := ExpandSnippet("{name}", nil); got != "{name}" {
		t.Errorf("without vars: %q", got)
	}
}
