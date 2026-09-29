package ui

import (
	"strings"
	"time"
)

func trimSpace(s string) string { return strings.TrimSpace(s) }

func sameDay(a, b time.Time) bool {
	ay, am, ad := a.Date()
	by, bm, bd := b.Date()
	return ay == by && am == bm && ad == bd
}

// daysAgo counts calendar days between t and now.
func daysAgo(t, now time.Time) int {
	y, m, d := t.Date()
	t0 := time.Date(y, m, d, 0, 0, 0, 0, now.Location())
	y, m, d = now.Date()
	n0 := time.Date(y, m, d, 0, 0, 0, 0, now.Location())
	return int(n0.Sub(t0).Hours()/24 + 0.5)
}

// listTime formats a timestamp for the chat list, like WhatsApp does.
func listTime(t, now time.Time) string {
	switch d := daysAgo(t, now); {
	case d == 0:
		return t.Format("15:04")
	case d == 1:
		return "Yesterday"
	case d < 7:
		return t.Weekday().String()
	default:
		return t.Format("02/01/2006")
	}
}

// dateChip formats the day separator shown between messages.
func dateChip(t, now time.Time) string {
	switch d := daysAgo(t, now); {
	case d == 0:
		return "Today"
	case d == 1:
		return "Yesterday"
	case d < 7:
		return t.Weekday().String()
	default:
		return t.Format("2 January 2006")
	}
}
