package ui

import (
	"slices"
	"testing"
	"time"

	"github.com/chomosuke9/wazzapclients/internal/mock"
	"github.com/chomosuke9/wazzapclients/internal/model"
)

// TestStickerRuns checks a sender's stickers in a row share one list row,
// which a reply, another sender or a text ends, and that the row wraps
// them to the width.
func TestStickerRuns(t *testing.T) {
	u := New(mock.New())
	u.Start(func() {})
	u.SelectID("rina")
	at := testNow()
	st := func(id string, me bool) *model.Message {
		return &model.Message{ID: id, ChatID: "rina", FromMe: me, Kind: model.KindSticker, Media: model.MediaSticker, Time: at}
	}
	reply := st("s4", true)
	reply.Quote = &model.Quote{Text: "hi"}
	u.msgs = []*model.Message{
		st("s1", true), st("s2", true), st("s3", true), reply, st("s5", true),
		st("r1", false), st("r2", false),
		{ID: "t1", ChatID: "rina", Kind: model.KindText, Text: "hi", Time: at.Add(time.Second)},
		st("r3", false),
	}
	u.msgsVer++
	var got [][]string
	for _, r := range u.rows(u.selected) {
		if r.kind != rowMessage {
			continue
		}
		ids := []string{r.msg.ID}
		if r.group != nil {
			ids = nil
			for _, m := range r.group {
				ids = append(ids, m.ID)
			}
		}
		got = append(got, ids)
	}
	want := [][]string{{"s1", "s2", "s3"}, {"s4"}, {"s5"}, {"r1", "r2"}, {"t1"}, {"r3"}}
	if len(got) != len(want) {
		t.Fatalf("rows = %v, want %v", got, want)
	}
	for i := range want {
		if len(got[i]) != len(want[i]) || got[i][0] != want[i][0] {
			t.Errorf("row %d = %v, want %v", i, got[i], want[i])
		}
	}
	if i := slices.IndexFunc(u.rows(u.selected), func(r convRow) bool { return r.has("s3") }); i < 0 {
		t.Error("s3 isn't found in its run")
	}

	for _, c := range []struct{ n, w, lines int }{{6, 340, 3}, {6, 500, 2}, {6, 1000, 2}, {6, 1100, 1}, {1, 100, 1}} {
		if got := len(stickerLines(c.n, c.w, 150, 24)); got != c.lines {
			t.Errorf("%d stickers in %d px: %d lines, want %d", c.n, c.w, got, c.lines)
		}
	}
}
