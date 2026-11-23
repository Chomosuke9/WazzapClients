package command

import (
	"errors"
	"testing"
	"time"
)

func TestParseWhen(t *testing.T) {
	now := time.Date(2026, time.October, 2, 15, 4, 30, 0, time.UTC) // a Friday
	for s, want := range map[string]string{
		"30m":            "2026-10-02 15:34:30",
		"2h":             "2026-10-02 17:04:30",
		"1h30m":          "2026-10-02 16:34:30",
		"3d":             "2026-10-05 15:04:30",
		"45min":          "2026-10-02 15:49:30",
		"21:00":          "2026-10-02 21:00:00",
		"9.30":           "2026-10-03 09:30:00", // passed today: tomorrow
		"9pm":            "2026-10-02 21:00:00",
		"12am":           "2026-10-03 00:00:00",
		"9:15AM":         "2026-10-03 09:15:00",
		"today 18:00":    "2026-10-02 18:00:00",
		"tomorrow 08:00": "2026-10-03 08:00:00",
		"Tmr 8am":        "2026-10-03 08:00:00",
		"fri 18:00":      "2026-10-02 18:00:00", // today
		"fri 09:00":      "2026-10-09 09:00:00", // next week
		"monday 07:45":   "2026-10-05 07:45:00",
		"25/12 09:00":    "2026-12-25 09:00:00",
		"1/1 00:00":      "2027-01-01 00:00:00", // the next one
		"1/2/27 10:00":   "2027-02-01 10:00:00",
		"2026-10-03 8pm": "2026-10-03 20:00:00",
	} {
		got, err := ParseWhen(s, now)
		if err != nil {
			t.Errorf("ParseWhen(%q): %v", s, err)
			continue
		}
		if g := got.Format(time.DateTime); g != want {
			t.Errorf("ParseWhen(%q) = %s, want %s", s, g, want)
		}
	}
	for s, unfinished := range map[string]bool{
		"tomorrow": true, "fri": true,
		"": false, "soon": false, "25:00": false, "8": false, "tomorrow 8": false, "13pm": false,
		"0m": false, "1/1/2026 10:00": false, "31/2 10:00": false, "today 09:00": false, "400d": false,
		"tomorrow 08:00 extra": false,
	} {
		if got, err := ParseWhen(s, now); err == nil {
			t.Errorf("ParseWhen(%q) = %v, want an error", s, got)
		} else if errors.Is(err, errUnfinished) != unfinished {
			t.Errorf("ParseWhen(%q): %v, unfinished %v", s, err, !unfinished)
		}
	}
}

func TestDescribeWhen(t *testing.T) {
	now := time.Date(2026, time.October, 2, 15, 4, 0, 0, time.UTC)
	for _, c := range []struct {
		t    time.Time
		want string
	}{
		{now.Add(2*time.Hour + 5*time.Minute), "today at 17:09, in 2h 5m"},
		{now.Add(20 * time.Second), "today at 15:04, in under a minute"},
		{time.Date(2026, 10, 3, 8, 0, 0, 0, time.UTC), "tomorrow at 08:00, in 16h 56m"},
		{time.Date(2026, 10, 9, 8, 0, 0, 0, time.UTC), "Fri 9 Oct at 08:00"},
		{time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC), "Fri 1 Jan 2027 at 00:00"},
	} {
		if got := DescribeWhen(c.t, now); got != c.want {
			t.Errorf("DescribeWhen(%v) = %q, want %q", c.t, got, c.want)
		}
	}
}
