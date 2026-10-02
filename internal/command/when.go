package command

import (
	"errors"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// ParseWhen reads when to do something, for /schedule:
//
//   - a while from now: "30m", "2h", "1h30m", "3d"
//   - a time today, or tomorrow once it's passed: "21:00", "9.30", "9pm",
//     "9:30am"
//   - a day and a time: "tomorrow 08:00", "fri 18:00", "25/12 09:00" (day
//     first), "25/12/2026 09:00", "2026-12-25 09:00"
//
// A weekday is the next one (today, if the time hasn't passed), and a
// date without a year the next one too.
func ParseWhen(s string, now time.Time) (time.Time, error) {
	f := strings.Fields(strings.ToLower(s))
	var t time.Time
	switch {
	case len(f) == 1 && relRe.MatchString(f[0]):
		var d time.Duration
		for _, m := range relPartRe.FindAllStringSubmatch(f[0], -1) {
			n, _ := strconv.Atoi(m[1])
			d += time.Duration(n) * map[string]time.Duration{"d": 24 * time.Hour, "h": time.Hour, "m": time.Minute}[m[2][:1]]
		}
		if d <= 0 {
			return time.Time{}, outOfRange("pick a time after now")
		}
		t = now.Add(d)
	case len(f) == 1 && dayWord(f[0]):
		return time.Time{}, unfinished("add a time: " + f[0] + " 08:00")
	case len(f) == 1:
		h, m, ok := clock(f[0])
		if !ok {
			return time.Time{}, errWhen
		}
		t = at(now, h, m)
		if !t.After(now) {
			t = t.AddDate(0, 0, 1)
		}
	case len(f) == 2:
		h, m, ok := clock(f[1])
		if !ok || !dayWord(f[0]) {
			return time.Time{}, errWhen
		}
		var err error
		if t, err = onDay(f[0], h, m, now); err != nil {
			return time.Time{}, err
		}
	default:
		return time.Time{}, errWhen
	}
	if !t.After(now) {
		return time.Time{}, outOfRange("that time has passed")
	}
	if t.After(now.AddDate(1, 0, 0)) {
		return time.Time{}, outOfRange("pick a time within a year")
	}
	return t, nil
}

var errWhen = errors.New("type a time like 21:00, 2h or tomorrow 08:00")

// outOfRange is the error of a time that reads well but is too soon or
// too far off.
type outOfRange string

func (e outOfRange) Error() string { return string(e) }

var (
	relRe     = regexp.MustCompile(`^(\d+(d|h|m|min|mins))+$`)
	relPartRe = regexp.MustCompile(`(\d+)(d|h|mins|min|m)`)
	clockRe   = regexp.MustCompile(`^(\d{1,2})(?:[:.](\d{2}))?(am|pm)?$`)
	dmyRe     = regexp.MustCompile(`^(\d{1,2})/(\d{1,2})(?:/(\d{2}|\d{4}))?$`)
	isoRe     = regexp.MustCompile(`^(\d{4})-(\d{1,2})-(\d{1,2})$`)
)

var weekdays = map[string]time.Weekday{
	"sun": time.Sunday, "sunday": time.Sunday, "mon": time.Monday, "monday": time.Monday,
	"tue": time.Tuesday, "tuesday": time.Tuesday, "wed": time.Wednesday, "wednesday": time.Wednesday,
	"thu": time.Thursday, "thursday": time.Thursday, "fri": time.Friday, "friday": time.Friday,
	"sat": time.Saturday, "saturday": time.Saturday,
}

// dayWord reports whether s names a day: "today", "tomorrow", a weekday
// or a date.
func dayWord(s string) bool {
	switch s {
	case "today", "tomorrow", "tmr", "tmrw":
		return true
	}
	_, ok := weekdays[s]
	return ok || dmyRe.MatchString(s) || isoRe.MatchString(s)
}

// clock reads a time of day: "21:00", "9.30", "9pm". A bare hour needs
// am or pm, so that "tomorrow 8" isn't read as one.
func clock(s string) (h, m int, ok bool) {
	g := clockRe.FindStringSubmatch(s)
	if g == nil || g[2] == "" && g[3] == "" {
		return 0, 0, false
	}
	h, _ = strconv.Atoi(g[1])
	if g[2] != "" {
		m, _ = strconv.Atoi(g[2])
	}
	switch g[3] {
	case "am", "pm":
		if h < 1 || h > 12 {
			return 0, 0, false
		}
		if h == 12 {
			h = 0
		}
		if g[3] == "pm" {
			h += 12
		}
	}
	return h, m, h < 24 && m < 60
}

func at(day time.Time, h, m int) time.Time {
	y, mo, d := day.Date()
	return time.Date(y, mo, d, h, m, 0, 0, day.Location())
}

// onDay is h:m on the day word names.
func onDay(word string, h, m int, now time.Time) (time.Time, error) {
	switch word {
	case "today":
		return at(now, h, m), nil
	case "tomorrow", "tmr", "tmrw":
		return at(now.AddDate(0, 0, 1), h, m), nil
	}
	if wd, ok := weekdays[word]; ok {
		t := at(now.AddDate(0, 0, (int(wd)-int(now.Weekday())+7)%7), h, m)
		if !t.After(now) {
			t = t.AddDate(0, 0, 7)
		}
		return t, nil
	}
	var y, mo, d int
	year := false
	if g := isoRe.FindStringSubmatch(word); g != nil {
		y, _ = strconv.Atoi(g[1])
		mo, _ = strconv.Atoi(g[2])
		d, _ = strconv.Atoi(g[3])
		year = true
	} else {
		g := dmyRe.FindStringSubmatch(word)
		d, _ = strconv.Atoi(g[1])
		mo, _ = strconv.Atoi(g[2])
		y = now.Year()
		if g[3] != "" {
			y, _ = strconv.Atoi(g[3])
			if y < 100 {
				y += 2000
			}
			year = true
		}
	}
	t := time.Date(y, time.Month(mo), d, h, m, 0, 0, now.Location())
	if mo < 1 || mo > 12 || t.Day() != d {
		return time.Time{}, errors.New(word + " isn't a date")
	}
	if !year && !t.After(now) {
		t = t.AddDate(1, 0, 0)
	}
	return t, nil
}

// DescribeWhen says when t is, from now: "today at 21:00, in 2h 5m",
// "tomorrow at 08:00", "Fri 9 Oct at 08:00".
func DescribeWhen(t, now time.Time) string {
	s := Day(t, now) + " at " + t.In(now.Location()).Format("15:04")
	if d := t.Sub(now); d < 24*time.Hour {
		s += ", in " + Duration(d)
	}
	return s
}

// Day names t's day from now: "today", "tomorrow", "yesterday" or "Fri 9
// Oct" (with the year when it isn't this one).
func Day(t, now time.Time) string {
	t = t.In(now.Location())
	same := func(d time.Time) bool {
		y, m, dd := d.Date()
		ty, tm, td := t.Date()
		return y == ty && m == tm && dd == td
	}
	switch {
	case same(now):
		return "today"
	case same(now.AddDate(0, 0, 1)):
		return "tomorrow"
	case same(now.AddDate(0, 0, -1)):
		return "yesterday"
	}
	if t.Year() != now.Year() {
		return t.Format("Mon 2 Jan 2006")
	}
	return t.Format("Mon 2 Jan")
}

// Duration writes d roughly: "under a minute", "45m", "2h 5m", "3d 2h".
func Duration(d time.Duration) string {
	m := int((d + 30*time.Second) / time.Minute)
	switch {
	case m < 1:
		return "under a minute"
	case m < 60:
		return strconv.Itoa(m) + "m"
	case m < 24*60:
		s := strconv.Itoa(m/60) + "h"
		if m%60 != 0 {
			s += " " + strconv.Itoa(m%60) + "m"
		}
		return s
	}
	h := m / 60
	s := strconv.Itoa(h/24) + "d"
	if h%24 != 0 {
		s += " " + strconv.Itoa(h%24) + "h"
	}
	return s
}
