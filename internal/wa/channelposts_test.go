package wa

import (
	"slices"
	"testing"
	"time"

	"github.com/polymorfa/hypermeow/types"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

// TestChannelPostReactions checks that a channel's post shows the
// server's reaction counts, the most given first, with your own reaction
// counted once even before the server's counts include it, and that the
// post is found by its server ID.
func TestChannelPostReactions(t *testing.T) {
	b := testBackend(t)
	ctx := b.ctx
	const chat = "120363000000000001@newsletter"
	m := &model.Message{ID: "p1", ChatID: chat, Text: "news", Time: time.Unix(100, 0)}
	if err := b.store.ensureChat(ctx, b.db, chat, false, "Ch"); err != nil {
		t.Fatal(err)
	}
	if err := b.store.putMessage(ctx, b.db, storedMsg{Message: m}); err != nil {
		t.Fatal(err)
	}
	if err := b.store.putPost(ctx, chat, "p1", 77, map[string]int{"👍": 3, "❤️": 9}); err != nil {
		t.Fatal(err)
	}
	if id, ok := b.store.postBySID(ctx, chat, 77); !ok || id != "p1" {
		t.Errorf("postBySID = %q, %v", id, ok)
	}
	if sid := b.store.oldestPost(ctx, chat); sid != 77 {
		t.Errorf("oldestPost = %d", sid)
	}
	got := b.Messages(chat, 10)[0]
	want := []model.ReactionCount{{Emoji: "❤️", Count: 9}, {Emoji: "👍", Count: 3}}
	if !slices.Equal(got.Reactions, want) || got.MyReaction != "" {
		t.Errorf("reactions = %+v, mine %q; want %+v", got.Reactions, got.MyReaction, want)
	}

	// Yours, before the server counted it, and the counts updated without
	// the server ID (a live update).
	_, _ = b.db.ExecContext(ctx, `INSERT INTO wz_reactions (chat, id, who, ts, emoji) VALUES (?, ?, ?, 1, ?)`,
		chat, "p1", meVoter, "🔥")
	if err := b.store.putPost(ctx, chat, "p1", 0, map[string]int{"👍": 4}); err != nil {
		t.Fatal(err)
	}
	got = b.Messages(chat, 10)[0]
	want = []model.ReactionCount{{Emoji: "👍", Count: 4}, {Emoji: "🔥", Count: 1}}
	if !slices.Equal(got.Reactions, want) || got.MyReaction != "🔥" {
		t.Errorf("reactions = %+v, mine %q; want %+v, 🔥", got.Reactions, got.MyReaction, want)
	}
	if _, sid := b.store.postCounts(ctx, chat, "p1"); sid != types.MessageServerID(77) {
		t.Errorf("a live update lost the server ID: %d", sid)
	}
}

// TestChannelRank checks that refreshing a suggested channel's details
// keeps its place among the suggestions, and that the details a reply
// leaves out keep what was stored.
func TestChannelRank(t *testing.T) {
	b := testBackend(t)
	ctx := b.ctx
	const id = "120363000000000002@newsletter"
	if err := b.store.putChannel(ctx, channelMeta{jid: id, name: "A", rank: 3, description: "about", invite: "CODE",
		created: time.Unix(5000, 0)}); err != nil {
		t.Fatal(err)
	}
	if err := b.store.putChannel(ctx, channelMeta{jid: id, name: "B", rank: -1, followers: 10}); err != nil {
		t.Fatal(err)
	}
	m, ok := b.store.channelMeta(ctx, id)
	if !ok || m.rank != 3 || m.name != "B" || m.followers != 10 || m.description != "about" || m.invite != "CODE" ||
		m.created.Unix() != 5000 {
		t.Errorf("channel = %+v", m)
	}
	sug := b.SuggestedChannels()
	if len(sug) != 1 || sug[0].ID != id || sug[0].Invite != "CODE" {
		t.Errorf("suggested = %+v", sug)
	}
}
