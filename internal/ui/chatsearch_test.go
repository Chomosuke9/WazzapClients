package ui

import (
	"slices"
	"testing"

	"github.com/chomosuke9/wazzapclients/internal/mock"
	"github.com/chomosuke9/wazzapclients/internal/model"
)

func TestMatchRanges(t *testing.T) {
	for _, c := range []struct {
		s, q string
		want [][2]int
	}{
		{"The theme", "the", [][2]int{{0, 3}, {4, 7}}},
		{"ÉTÉ été", "été", [][2]int{{0, 3}, {4, 7}}},
		{"aaa", "aa", [][2]int{{0, 2}}},
		{"abc", "", nil},
		{"ab", "abc", nil},
		{"ΟΔΟΣ", "οδος", [][2]int{{0, 4}}},
	} {
		if got := matchRanges([]rune(c.s), []rune(c.q)); !slices.Equal(got, c.want) {
			t.Errorf("matchRanges(%q, %q) = %v, want %v", c.s, c.q, got, c.want)
		}
	}
}

// TestChatSearch opens a chat's search, checks that a query finds its
// messages, and that the info panel then takes the panel's place.
func TestChatSearch(t *testing.T) {
	b := mock.New()
	u := New(b)
	u.Start(func() {})
	u.SelectID("rina")
	u.openChatSearch()
	if !u.search.open || u.search.chatID != "rina" {
		t.Fatal("search didn't open for the chat")
	}
	u.search.query.SetText("villa")
	u.runChatSearch()
	u.applyEvents()
	if len(u.search.results) == 0 {
		t.Fatal("no results for a word in the chat")
	}
	for i, m := range u.search.results {
		if m.ChatID != "rina" || len(matchRanges([]rune(m.Text), []rune("villa"))) == 0 {
			t.Errorf("result %q doesn't match", m.Text)
		}
		if i > 0 && m.Time.After(u.search.results[i-1].Time) {
			t.Error("results aren't newest first")
		}
	}
	// A new matching message shows up in the open search.
	n := len(u.search.results)
	b.Forward(b.Messages("rina", 1), []string{"rina"})
	u.applyEvents()
	u.runChatSearch()
	u.applyEvents()
	if len(u.search.results) != n+1 {
		t.Errorf("%d results after a matching message came, want %d", len(u.search.results), n+1)
	}
	u.openInfo("rina")
	if u.search.shown() || !u.info.open {
		t.Error("the info panel didn't replace the search")
	}
	u.openChatSearch()
	if u.info.shown() || u.search.query.Text() != "villa" {
		t.Error("search didn't replace the info panel, keeping its query")
	}
	u.SelectID("work")
	if u.search.shown() {
		t.Error("search stayed open in another chat")
	}
}

func TestFilterMembers(t *testing.T) {
	ms := []model.Member{{Name: "Andre"}, {Name: "Bima"}, {Name: "*Dani*"}}
	var got []string
	for _, m := range filterMembers(ms, " AN ") {
		got = append(got, m.Name)
	}
	if want := []string{"Andre", "*Dani*"}; !slices.Equal(got, want) {
		t.Errorf("filterMembers = %v, want %v", got, want)
	}
}
