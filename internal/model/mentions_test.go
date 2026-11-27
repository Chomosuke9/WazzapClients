package model

import (
	"slices"
	"testing"
)

// The mentioned JIDs say whose each person is: those the text names, then
// each group mention's after a separator, someone in two under both.
func TestJoinSplitMentions(t *testing.T) {
	direct := []string{"1@lid"}
	groups := [][]string{{"2@lid", "3@lid"}, {}, {"3@lid", "4@lid"}}
	jids := JoinMentions(direct, groups)
	want := []string{"1@lid", MentionSeparator, "2@lid", "3@lid", MentionSeparator, MentionSeparator, "3@lid", "4@lid"}
	if !slices.Equal(jids, want) {
		t.Fatalf("joined %v, want %v", jids, want)
	}
	d, g, ok := SplitMentions(jids, 3)
	if !ok || !slices.Equal(d, direct) || len(g) != 3 || !slices.Equal(g[0], groups[0]) || len(g[1]) != 0 || !slices.Equal(g[2], groups[2]) {
		t.Fatalf("split %v %v %v", d, g, ok)
	}
	// Another client's list, or one that doesn't match the group mentions,
	// says nothing of whose they are.
	for _, n := range []int{0, 2, 4} {
		d, g, ok := SplitMentions(jids, n)
		if ok || g != nil || slices.Contains(d, MentionSeparator) || len(d) != 5 {
			t.Errorf("n=%d: %v %v %v", n, d, g, ok)
		}
	}
	if d, _, ok := SplitMentions([]string{"1@lid", "2@lid"}, 2); ok || len(d) != 2 {
		t.Errorf("plain list: %v %v", d, ok)
	}
}
