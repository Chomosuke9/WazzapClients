package wa

import (
	"runtime"
	"testing"
	"time"

	"github.com/chomosuke9/wazzapclients/internal/model"
	whatsmeow "github.com/polymorfa/hypermeow"
	"github.com/polymorfa/hypermeow/store"
	waLog "github.com/polymorfa/hypermeow/util/log"
)

func TestSnippetPersistenceAndAccountSeparation(t *testing.T) {
	dir := t.TempDir()
	b, err := Open(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	s, err := b.SaveSnippet(model.Snippet{Body: "Saved on this account"})
	b.Close()
	if err != nil {
		t.Fatal(err)
	}
	b, err = Open(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	got, err := b.Snippet(s.ID)
	if err != nil || got.Body != s.Body {
		t.Fatal("snippet lost on restart", got, err)
	}
	other, err := Open(t.TempDir(), false)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	items, err := other.Snippets()
	if err != nil || len(items) != 0 {
		t.Fatal("snippets leaked between accounts", items, err)
	}
}

func TestSnippetMigrationKeepsExistingMessages(t *testing.T) {
	b := testBackend(t)
	_, err := b.db.Exec(`ALTER TABLE wz_messages DROP COLUMN raw_payload; DROP TABLE wz_snippets; INSERT INTO wz_chats(jid) VALUES('old'); INSERT INTO wz_messages(chat,id,text,ts) VALUES('old','1','keep me',1700000000)`)
	if err != nil {
		t.Fatal(err)
	}
	if err = b.store.init(b.ctx); err != nil {
		t.Fatal(err)
	}
	var text string
	var raw []byte
	if err = b.db.QueryRow(`SELECT text,raw_payload FROM wz_messages WHERE chat='old' AND id='1'`).Scan(&text, &raw); err != nil || text != "keep me" || raw != nil {
		t.Fatal(text, raw, err)
	}
	if _, err = b.SaveSnippet(model.Snippet{Body: "new"}); err != nil {
		t.Fatal(err)
	}
}

func TestSnippetSendQueuesAndRecordsFailureWithoutNetwork(t *testing.T) {
	b := testBackend(t)
	// An unlinked client fails before attempting any network operation.
	b.cli = whatsmeow.NewClient(&store.Device{}, waLog.Noop)
	at := time.Unix(1700000000, 0)
	b.clock = func() time.Time { return at }
	s, err := b.SaveSnippet(model.Snippet{Payload: true, Body: `{"conversation":"Hello {name}"}`})
	if err != nil {
		t.Fatal(err)
	}
	m, err := b.SendSnippet("123@g.us", s.ID, nil, map[string]string{"name": "Ana"})
	if err != nil || m == nil || m.Receipt != model.Pending || !m.Time.Equal(at) || m.Text != "Hello Ana" {
		t.Fatal(m, err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		r, ok := b.store.message(b.ctx, m.ChatID, m.ID)
		if ok && r.Receipt == model.Failed {
			body, err := b.MessagePayload(m.ChatID, m.ID)
			if err != nil || body == "" {
				t.Fatal(body, err)
			}
			return
		}
		runtime.Gosched()
	}
	t.Fatal("failed send stayed pending")
}
