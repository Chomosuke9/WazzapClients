package wa

import (
	"testing"
	"time"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

// TestReactions checks that each person's newest reaction counts, whatever
// order they come in, that taking one back takes it off the count, and
// that the message reads back the emojis, the most given first.
func TestReactions(t *testing.T) {
	b := testBackend(t)
	ctx := b.ctx
	m := &model.Message{ID: "m", ChatID: "g@g.us", Text: "hi", Time: time.Unix(100, 0)}
	if err := b.store.ensureChat(ctx, b.db, m.ChatID, true, "G"); err != nil {
		t.Fatal(err)
	}
	if err := b.store.putMessage(ctx, b.db, storedMsg{Message: m}); err != nil {
		t.Fatal(err)
	}
	for _, r := range []reaction{
		{who: "a@lid", ts: 2000, emoji: "❤️"},
		{who: "a@lid", ts: 1000, emoji: "😂"}, // older: ignored
		{who: "b@lid", ts: 1500, emoji: "👍"},
		{who: "c@lid", ts: 1600, emoji: "👍"},
		{who: "d@lid", ts: 1650, emoji: "😮"},
		{who: "d@lid", ts: 1700, emoji: ""},  // taken back
		{who: "d@lid", ts: 1690, emoji: "😮"}, // older than taking it back
		{who: meVoter, ts: 1800, emoji: "🙏"},
	} {
		if err := b.store.putReaction(ctx, b.db, m.ChatID, m.ID, r); err != nil {
			t.Fatal(err)
		}
	}
	got := b.Messages(m.ChatID, 10)
	if len(got) != 1 {
		t.Fatalf("messages = %+v", got)
	}
	// Ties go by when each was given: 🙏 at 1800 before ❤️ at 2000.
	want := []model.ReactionCount{{Emoji: "👍", Count: 2}, {Emoji: "🙏", Count: 1}, {Emoji: "❤️", Count: 1}}
	if g := got[0]; g.MyReaction != "🙏" || len(g.Reactions) != len(want) || g.ReactionTotal() != 4 {
		t.Fatalf("reactions = %v, mine %q; want %v, mine 🙏", g.Reactions, g.MyReaction, want)
	}
	for i, w := range want {
		if got[0].Reactions[i] != w {
			t.Errorf("reaction %d = %v, want %v", i, got[0].Reactions[i], w)
		}
	}
	rs := b.Reactors(got[0])
	if len(rs) != 4 || !rs[0].Me || rs[0].Name != "You" || rs[1].ID != "a@lid" || rs[3].ID != "b@lid" {
		t.Errorf("reactors = %+v, want you, then the newest first", rs)
	}

	// A column from before wz_reactions: the one latest emoji.
	var old model.Message
	fillReactions(&old, "😹")
	if old.MyReaction != "" || len(old.Reactions) != 1 || old.Reactions[0] != (model.ReactionCount{Emoji: "😹", Count: 1}) {
		t.Errorf("old column read as %v, mine %q", old.Reactions, old.MyReaction)
	}
}
