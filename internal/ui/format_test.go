package ui

import (
	"testing"
	"time"
)

// Date chips name the last week's days and give older ones as numbers,
// like WhatsApp.
func TestDateChip(t *testing.T) {
	now := time.Date(2025, 10, 13, 15, 0, 0, 0, time.UTC) // a Monday
	for _, c := range []struct {
		days int
		want string
	}{
		{0, "Today"},
		{1, "Yesterday"},
		{3, "Friday"},
		{6, "Tuesday"},
		{7, "06/10/2025"},
		{300, "17/12/2024"},
	} {
		if got := dateChip(now.AddDate(0, 0, -c.days), now); got != c.want {
			t.Errorf("%d days ago: %q, want %q", c.days, got, c.want)
		}
	}
}
