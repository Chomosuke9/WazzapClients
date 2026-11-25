package wa

import (
	"testing"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

func TestTranscripts(t *testing.T) {
	b := testBackend(t)
	const chat = "628123@s.whatsapp.net"
	add := func(id string) {
		t.Helper()
		if _, err := b.db.Exec(`INSERT INTO wz_messages (chat, id, sender_jid, ts, media) VALUES (?, ?, ?, 1, ?)`,
			chat, id, chat, int(model.MediaVoice)); err != nil {
			t.Fatal(err)
		}
	}
	want := model.Transcript{Lang: "id", Model: "small", Text: "Halo"}
	for _, id := range []string{"A", "B", "C"} {
		add(id)
		b.SetTranscript(chat, id, want)
		if got := b.Transcript(chat, id); got == nil || *got != want {
			t.Fatalf("%s: %+v", id, got)
		}
	}
	// Again replaces it; one without words is kept too.
	b.SetTranscript(chat, "A", model.Transcript{Lang: "en"})
	if got := b.Transcript(chat, "A"); got == nil || got.Text != "" || got.Lang != "en" {
		t.Errorf("replaced: %+v", got)
	}
	// It goes with its message, and never comes without one.
	if err := b.store.deleteMessage(b.ctx, chat, "A"); err != nil {
		t.Fatal(err)
	}
	if err := b.store.markDeleted(b.ctx, chat, "B", ""); err != nil {
		t.Fatal(err)
	}
	if err := b.store.clearChat(b.ctx, chat, 0); err != nil {
		t.Fatal(err)
	}
	b.SetTranscript(chat, "A", want)
	for _, id := range []string{"A", "B", "C"} {
		if got := b.Transcript(chat, id); got != nil {
			t.Errorf("%s outlived its message: %+v", id, got)
		}
	}
}
