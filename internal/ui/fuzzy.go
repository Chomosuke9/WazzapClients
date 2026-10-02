package ui

import (
	"slices"
	"strings"
	"unicode"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

// Finding people as you type, in the @mention picker and the slash
// commands' member and contact pickers. A name matches from its start, from
// a word's start, anywhere, by the starts of its words in order ("bsan"
// finds Budi Santoso), or with one typo. A phone number matches in any
// format, from its country code or from its national number with or
// without the trunk 0 ("0812" finds +62 812-…), or by three or more of its
// digits anywhere.

// How well a query matches, best first. 0 is no match.
const (
	matchTypo = iota + 1
	matchLetters
	matchInside
	matchWords
	matchWord
	matchStart
	matchExact
)

// memberScore is how well q matches a group member.
func memberScore(m model.Member, q string) int {
	return personScore(q, m.Phone, m.Contact, m.Push, m.Name)
}

// personScore is how well q matches the best of a person's names, or their
// phone number.
func personScore(q, phone string, names ...string) int {
	qr := foldRunes(strings.TrimPrefix(trimSpace(q), "~"))
	if len(qr) == 0 {
		return 0
	}
	best := phoneScore(phone, q)
	for _, n := range names {
		best = max(best, nameScore(n, qr))
	}
	return best
}

// memberTitle is how a picker names a member: the name you saved them
// under, else the one they gave themselves, else as the group lists them.
func memberTitle(m model.Member) string {
	switch {
	case m.Contact != "":
		return m.Contact
	case m.Push != "":
		return "~" + m.Push
	}
	return m.Name
}

// memberSub is the phone number under a member you haven't saved.
func memberSub(m model.Member) string {
	if m.Contact == "" && m.Phone != "" && memberTitle(m) != m.Phone {
		return m.Phone
	}
	return ""
}

func foldRunes(s string) []rune {
	rs := []rune(s)
	for i, r := range rs {
		rs[i] = model.FoldRune(r)
	}
	return rs
}

// nameScore is how well q, folded, matches a name.
func nameScore(name string, q []rune) int {
	n := foldRunes(strings.TrimPrefix(trimSpace(name), "~"))
	if len(n) == 0 {
		return 0
	}
	start := make([]bool, len(n))
	for i, r := range n {
		start[i] = isWordRune(r) && (i == 0 || !isWordRune(n[i-1]))
	}
	at := func(i int) bool { return i+len(q) <= len(n) && slices.Equal(n[i:i+len(q)], q) }
	switch {
	case slices.Equal(n, q):
		return matchExact
	case at(0):
		return matchStart
	}
	inside := false
	for i := range n {
		if at(i) {
			if start[i] {
				return matchWord
			}
			inside = true
		}
	}
	if wordsMatch(n, q) {
		return matchWords
	}
	if inside {
		return matchInside
	}
	letters := slices.DeleteFunc(slices.Clone(q), unicode.IsSpace)
	if len(letters) >= 2 && byWordStarts(n, letters, start) {
		return matchLetters
	}
	if len(q) >= 4 && oneTypo(n, q, start) {
		return matchTypo
	}
	return 0
}

// wordsMatch reports whether q has several words, and each one starts a
// word of n, in any order: "santoso bu" in "budi santoso".
func wordsMatch(n, q []rune) bool {
	qw := strings.Fields(string(q))
	if len(qw) < 2 {
		return false
	}
	nw := strings.FieldsFunc(string(n), func(r rune) bool { return !isWordRune(r) })
	for _, w := range qw {
		if !slices.ContainsFunc(nw, func(x string) bool { return strings.HasPrefix(x, w) }) {
			return false
		}
	}
	return true
}

// byWordStarts reports whether q's letters appear in n in order, the first
// starting a word and each other one either starting a word or right after
// the one before: "bsan" in "budi santoso", "msal" in "muhammad salim".
func byWordStarts(n, q []rune, start []bool) bool {
	// end[j]: q so far can be matched with its last letter at n[j].
	end := make([]bool, len(n))
	next := make([]bool, len(n))
	for k, r := range q {
		before := false // q[:k] matched, ending before j
		for j := range n {
			next[j] = n[j] == r && (k == 0 && start[j] ||
				k > 0 && (j > 0 && end[j-1] || start[j] && before))
			before = before || end[j]
		}
		end, next = next, end
	}
	return slices.Contains(end, true)
}

// oneTypo reports whether q is the start of a word of n with one letter
// wrong, missing, extra or swapped with the next.
func oneTypo(n, q []rune, start []bool) bool {
	for i, s := range start {
		if !s {
			continue
		}
		for l := len(q) - 1; l <= len(q)+1; l++ {
			if i+l <= len(n) && editDistance(q, n[i:i+l]) <= 1 {
				return true
			}
		}
	}
	return false
}

// editDistance counts the letters to change, add, remove or swap with a
// neighbour to turn a into b (optimal string alignment).
func editDistance(a, b []rune) int {
	rows := make([][]int, len(a)+1)
	for i := range rows {
		rows[i] = make([]int, len(b)+1)
		rows[i][0] = i
	}
	for j := range rows[0] {
		rows[0][j] = j
	}
	for i := 1; i <= len(a); i++ {
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			d := min(rows[i-1][j]+1, rows[i][j-1]+1, rows[i-1][j-1]+cost)
			if i > 1 && j > 1 && a[i-1] == b[j-2] && a[i-2] == b[j-1] {
				d = min(d, rows[i-2][j-2]+1)
			}
			rows[i][j] = d
		}
	}
	return rows[len(a)][len(b)]
}

// phoneScore is how well q matches a phone number, when q looks like one:
// digits, maybe with "+", spaces, dashes, dots or brackets.
func phoneScore(phone, q string) int {
	qd, ok := typedDigits(q)
	pd, pok := typedDigits(phone) // not a redacted one, "+62 8•• ••90"
	if !ok || !pok {
		return 0
	}
	switch {
	case pd == qd:
		return matchExact
	case strings.HasPrefix(pd, qd):
		return matchStart
	}
	// The national number, typed with its trunk 0 or without it, after a
	// country code of one to three digits.
	nat := strings.TrimLeft(qd, "0")
	if nat == "" {
		return 0
	}
	for cc := 1; cc <= 3 && cc < len(pd); cc++ {
		if strings.HasPrefix(pd[cc:], nat) {
			return matchWord
		}
	}
	if len(nat) >= 3 && strings.Contains(pd, nat) {
		return matchInside
	}
	return 0
}

// typedDigits returns the digits of s, and whether s is (part of) a phone
// number as people write them, with at least one digit.
func typedDigits(s string) (string, bool) {
	var b strings.Builder
	ok := true
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case !strings.ContainsRune("+-.() ", r):
			ok = false
		}
	}
	return b.String(), ok && b.Len() > 0
}
