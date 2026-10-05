package model

import (
	"strings"
	"time"
)

// Snippet is a saved message on this account. List results omit Body.
type Snippet struct {
	ID      int64
	Name    string
	Payload bool
	Body    string
	Preview string
}

const MaxSnippetBytes = 1 << 20

// SnippetBackend keeps protocol types out of the UI. Storage and export
// methods may do disk I/O and should run through Host.Do. SendSnippet only
// queues a send; uploads and delivery happen in the backend. vars fills a
// snippet's {variables} (ExpandSnippet): its text, or a payload's text and
// captions.
type SnippetBackend interface {
	Snippets() ([]Snippet, error)
	Snippet(id int64) (Snippet, error)
	SaveSnippet(s Snippet) (Snippet, error)
	// SaveMessageSnippet saves a stored message as a snippet called name
	// (one is made up when name is "").
	SaveMessageSnippet(chatID, messageID, name string) (Snippet, error)
	DeleteSnippet(id int64) error
	SendSnippet(chatID string, id int64, reply *Message, vars map[string]string) (*Message, error)
	MessagePayload(chatID, messageID string) (string, error)
	ExportMessagePayload(chatID, messageID string) (string, error)
}

// SnippetVars lists the variables a snippet may use, for the settings page.
var SnippetVars = []struct{ Name, Description string }{
	{"name", "who you're writing to: the contact, or in a group the author of the message you reply to (else the group)"},
	{"first", "the first word of {name}"},
	{"mention", "@mentions the author of the message you reply to; without a reply, the chat by its name, notifying no one"},
	{"chat", "the chat's name"},
	{"me", "your name"},
	{"quote", "the text of the message you reply to"},
	{"greeting", "Good morning, Good afternoon or Good evening"},
	{"date", "today's date, like 5 October 2026"},
	{"time", "the time, like 14:05"},
	{"day", "the weekday, like Monday"},
}

// SnippetValues builds the variables for a snippet sent at now. name is
// who it's for, chat the chat's name, me your name and quote the replied
// message's text.
func SnippetValues(name, chat, me, quote string, now time.Time) map[string]string {
	first := name
	if f := strings.Fields(name); len(f) > 0 {
		first = f[0]
	}
	greeting := "Good evening"
	switch h := now.Hour(); {
	case h >= 4 && h < 12:
		greeting = "Good morning"
	case h >= 12 && h < 18:
		greeting = "Good afternoon"
	}
	return map[string]string{
		"name": name, "first": first, "chat": chat, "me": me, "quote": quote,
		"greeting": greeting,
		"date":     now.Format("2 January 2006"),
		"time":     now.Format("15:04"),
		"day":      now.Format("Monday"),
	}
}

// ExpandSnippet replaces each {variable} in text that vars knows. Unknown
// ones, like the braces of pasted JSON, stay as they are, and \{ writes a
// literal brace, so \{name} sends "{name}".
func ExpandSnippet(text string, vars map[string]string) string {
	out, _ := expandSnippet(text, vars, "")
	return out
}

// SnippetUses reports whether text uses variable name (not escaped).
func SnippetUses(text, name string) bool {
	_, used := expandSnippet(text, map[string]string{name: ""}, name)
	return used
}

// expandSnippet is ExpandSnippet, and reports whether it filled watch.
func expandSnippet(text string, vars map[string]string, watch string) (string, bool) {
	if len(vars) == 0 || !strings.Contains(text, "{") {
		return text, false
	}
	used := false
	var sb strings.Builder
	for i := 0; i < len(text); i++ {
		c := text[i]
		if c == '\\' && i+1 < len(text) && text[i+1] == '{' {
			sb.WriteByte('{')
			i++
			continue
		}
		if c == '{' {
			if end := strings.IndexByte(text[i+1:], '}'); end > 0 {
				key := strings.ToLower(text[i+1 : i+1+end])
				if v, ok := vars[key]; ok {
					used = used || key == watch
					sb.WriteString(v)
					i += end + 1
					continue
				}
			}
		}
		sb.WriteByte(c)
	}
	return sb.String(), used
}
