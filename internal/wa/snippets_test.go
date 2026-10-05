package wa

import (
	"bytes"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chomosuke9/wazzapclients/internal/model"
	whatsmeow "github.com/polymorfa/hypermeow"
	"github.com/polymorfa/hypermeow/proto/waE2E"
	"github.com/polymorfa/hypermeow/store"
	"github.com/polymorfa/hypermeow/types"
	"github.com/polymorfa/hypermeow/types/events"
	waLog "github.com/polymorfa/hypermeow/util/log"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
)

func snippetEvent(id string, msg *waE2E.Message) *events.Message {
	return &events.Message{Info: types.MessageInfo{MessageSource: types.MessageSource{Chat: types.NewJID("123", types.GroupServer), Sender: types.NewJID("456", types.HiddenUserServer), IsGroup: true}, ID: id, Timestamp: time.Unix(1700000000, 0)}, Message: msg, RawMessage: msg}
}

func TestRawPayloadStorageAndEdits(t *testing.T) {
	b := testBackend(t)
	inner := &waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{Text: proto.String("hello"), ContextInfo: &waE2E.ContextInfo{MentionedJID: []string{"456@lid"}}}}
	raw := &waE2E.Message{EphemeralMessage: &waE2E.FutureProofMessage{Message: inner}}
	unknown := protowire.AppendVarint(protowire.AppendTag(nil, 10000, protowire.VarintType), 99)
	raw.ProtoReflect().SetUnknown(unknown)
	e := snippetEvent("original", inner)
	e.RawMessage = raw
	b.onMessage(e)
	got, err := b.rawMessage("123@g.us", "original")
	if err != nil || !proto.Equal(got, raw) {
		t.Fatalf("raw wrapper/fields changed: %v", err)
	}
	body, err := b.MessagePayload("123@g.us", "original")
	if err != nil || !strings.Contains(body, "ephemeralMessage") || !strings.Contains(body, "456@lid") {
		t.Fatalf("catch: %s, %v", body, err)
	}
	if err = b.store.init(b.ctx); err != nil {
		t.Fatal("repeat migration:", err)
	}
	edited := snippetEvent("original", &waE2E.Message{Conversation: proto.String("edited")})
	edited.IsEdit = true
	edited.Info.Timestamp = e.Info.Timestamp.Add(time.Minute)
	b.onMessage(edited)
	b.onMessage(e)
	body, err = b.MessagePayload("123@g.us", "original")
	if err != nil || !strings.Contains(body, "edited") {
		t.Fatalf("original overwrote edited payload: %s %v", body, err)
	}
	older := snippetEvent("original", &waE2E.Message{Conversation: proto.String("older")})
	older.IsEdit = true
	older.Info.Timestamp = e.Info.Timestamp.Add(30 * time.Second)
	b.onMessage(older)
	body, _ = b.MessagePayload("123@g.us", "original")
	if !strings.Contains(body, "edited") {
		t.Fatal("late edit replaced latest raw")
	}
	if err = b.store.markDeleted(b.ctx, "123@g.us", "original"); err != nil {
		t.Fatal(err)
	}
	if _, err = b.MessagePayload("123@g.us", "original"); err == nil {
		t.Fatal("deleted payload exported")
	}
	b.onMessage(e)
	if _, err = b.MessagePayload("123@g.us", "original"); err == nil {
		t.Fatal("duplicate resurrected deleted payload")
	}
}

func TestSnippetsTextAndLegacyMessages(t *testing.T) {
	b := testBackend(t)
	if err := b.store.ensureChat(b.ctx, b.db, "123@g.us", true, ""); err != nil {
		t.Fatal(err)
	}
	m := &model.Message{ChatID: "123@g.us", ID: "legacy", Text: "  Hello {name}\nSecond line  ", Time: time.Unix(1700000000, 0)}
	if err := b.store.putMessage(b.ctx, b.db, storedMsg{Message: m}); err != nil {
		t.Fatal(err)
	}
	if _, err := b.MessagePayload(m.ChatID, m.ID); err == nil {
		t.Fatal("fabricated an old payload")
	}
	s, err := b.SaveMessageSnippet(m.ChatID, m.ID, "")
	if err != nil || s.Name != "Snippet 1" || s.Body != m.Text || s.Payload {
		t.Fatalf("saved text: %+v %v", s, err)
	}
	s.Name = "Greeting"
	if _, err = b.SaveSnippet(s); err != nil {
		t.Fatal(err)
	}
	if _, err = b.SaveSnippet(model.Snippet{Name: "greeting", Body: "different"}); err == nil {
		t.Fatal("duplicate name overwritten")
	}
	second, err := b.SaveSnippet(model.Snippet{Body: "second"})
	if err != nil || second.ID == s.ID {
		t.Fatal(second, err)
	}
	list, err := b.Snippets()
	if err != nil || len(list) != 2 || list[0].Body != "" {
		t.Fatal("list loads full bodies", list, err)
	}
	if err = b.store.init(b.ctx); err != nil {
		t.Fatal(err)
	}
	got, err := b.Snippet(s.ID)
	if err != nil || got.Body != m.Text || got.Name != "Greeting" {
		t.Fatal("migration lost snippet", got, err)
	}
	if err = b.DeleteSnippet(s.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = b.Snippet(s.ID); err == nil {
		t.Fatal("deleted snippet remains")
	}
}

func TestPayloadSnippetMediaAndUnknownFields(t *testing.T) {
	b := testBackend(t)
	hash := bytes.Repeat([]byte{7}, 32)
	msg := &waE2E.Message{ImageMessage: &waE2E.ImageMessage{Caption: proto.String("literal {name}"), FileSHA256: hash, ContextInfo: &waE2E.ContextInfo{StanzaID: proto.String("old"), Participant: proto.String("456@lid"), QuotedMessage: &waE2E.Message{Conversation: proto.String("old quote")}}}}
	unknown := protowire.AppendVarint(protowire.AppendTag(nil, 10000, protowire.VarintType), 123)
	msg.ProtoReflect().SetUnknown(unknown)
	path := filepath.Join(b.dataDir, "snippet-media", hex.EncodeToString(hash))
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("fixture media"), 0o600); err != nil {
		t.Fatal(err)
	}
	b.onMessage(snippetEvent("image", msg))
	s, err := b.SaveMessageSnippet("123@g.us", "image", "")
	if err != nil {
		t.Fatal(err)
	}
	s.Name = "Photo"
	if _, err = b.SaveSnippet(s); err != nil {
		t.Fatal(err)
	}
	var blob []byte
	if err = b.db.QueryRow(`SELECT raw_payload FROM wz_snippets WHERE id=?`, s.ID).Scan(&blob); err != nil {
		t.Fatal(err)
	}
	var saved waE2E.Message
	if err = proto.Unmarshal(blob, &saved); err != nil || !proto.Equal(&saved, msg) {
		t.Fatal("capture/rename lost raw fields", err)
	}
	ready, err := prepareSnippetMessage(&saved)
	if err != nil {
		t.Fatal(err)
	}
	if ready.GetImageMessage().GetContextInfo().GetStanzaID() != "" || ready.GetImageMessage().GetCaption() != "literal {name}" {
		t.Fatal("fresh message context/text wrong")
	}
	if !proto.Equal(&saved, msg) {
		t.Fatal("preparing a send modified the saved source")
	}
	if err = b.store.deleteMessage(b.ctx, "123@g.us", "image"); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(path); err != nil {
		t.Fatal("deleting source lost snippet media")
	}
	if err = b.DeleteSnippet(s.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("unreferenced media wasn't removed", err)
	}
}

func TestSnippetPayloadValidation(t *testing.T) {
	b := testBackend(t)
	for _, body := range []string{`{}`, `{"madeUpField":1}`, `{"conversation":"a","conversation":"b"}`, `{"protocolMessage":{"type":"REVOKE"}}`, `{"conversation":"a","imageMessage":{}}`, `{"conversation":`} {
		if _, err := b.SaveSnippet(model.Snippet{Payload: true, Body: body}); err == nil {
			t.Errorf("accepted %s", body)
		}
	}
	msg := &waE2E.Message{ViewOnceMessageV2: &waE2E.FutureProofMessage{Message: &waE2E.Message{ImageMessage: &waE2E.ImageMessage{}}}}
	data, _ := protojson.Marshal(msg)
	if _, err := b.SaveSnippet(model.Snippet{Payload: true, Body: string(data)}); err == nil {
		t.Fatal("view once allowed")
	}
	if _, err := b.SaveSnippet(model.Snippet{Payload: true, Body: `{"extendedTextMessage":{"text":"Literal {name}\nquote: \"x\""}}`}); err != nil {
		t.Fatal(err)
	}
}

// Saving a poll or a document keeps the whole message, and sending the
// snippet sends it whole.
func TestSnippetSavesPollAndDocument(t *testing.T) {
	b := testBackend(t)
	hash := bytes.Repeat([]byte{9}, 32)
	path := filepath.Join(b.dataDir, "snippet-media", hex.EncodeToString(hash))
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	poll := &waE2E.Message{PollCreationMessageV3: &waE2E.PollCreationMessage{Name: proto.String("Lunch?"),
		Options:                []*waE2E.PollCreationMessage_Option{{OptionName: proto.String("Soto")}, {OptionName: proto.String("Bakso")}},
		SelectableOptionsCount: proto.Uint32(1)}}
	doc := &waE2E.Message{DocumentMessage: &waE2E.DocumentMessage{FileName: proto.String("menu.pdf"),
		Mimetype: proto.String("application/pdf"), FileLength: proto.Uint64(16), FileSHA256: hash}}
	b.onMessage(snippetEvent("poll", poll))
	b.onMessage(snippetEvent("doc", doc))
	b.cli = whatsmeow.NewClient(&store.Device{}, waLog.Noop) // unlinked: sends fail offline
	for id, want := range map[string]string{"poll": "pollCreationMessageV3", "doc": "documentMessage"} {
		// The document as if downloaded (saving one keeps a copy).
		if err := os.WriteFile(path, []byte("fixture document"), 0o600); err != nil {
			t.Fatal(err)
		}
		s, err := b.SaveMessageSnippet("123@g.us", id, id)
		if err != nil || !s.Payload || s.Name != id {
			t.Fatalf("save %s: %+v %v", id, s, err)
		}
		if got, _ := b.Snippet(s.ID); !strings.Contains(got.Body, want) {
			t.Fatalf("%s saved as %s", id, got.Body)
		}
		m, err := b.SendSnippet("123@g.us", s.ID, nil, nil)
		if err != nil || m == nil {
			t.Fatal(id, err)
		}
		switch id {
		case "poll":
			if m.Poll == nil || len(m.Poll.Options) != 2 || m.Text != "Lunch?" {
				t.Fatalf("poll sent as %+v", m)
			}
		case "doc":
			if m.Media != model.MediaDocument || m.FileName != "menu.pdf" {
				t.Fatalf("document sent as %+v", m)
			}
		}
	}
}

// {mention} mentions the replied author, or without a reply the chat by
// name, notifying no one, like @admin.
func TestSnippetMention(t *testing.T) {
	b := testBackend(t)
	b.onMessage(snippetEvent("q", &waE2E.Message{Conversation: proto.String("question")}))
	b.cli = whatsmeow.NewClient(&store.Device{}, waLog.Noop)
	text, err := b.SaveSnippet(model.Snippet{Name: "t", Body: "Hi {mention}, \\{mention} stays"})
	if err != nil {
		t.Fatal(err)
	}
	payload, err := b.SaveSnippet(model.Snippet{Name: "p", Payload: true, Body: `{"conversation":"Hi {mention}"}`})
	if err != nil {
		t.Fatal(err)
	}
	reply, ok := b.store.message(b.ctx, "123@g.us", "q")
	if !ok {
		t.Fatal("no message")
	}
	vars := map[string]string{"chat": "Work"}
	for _, id := range []int64{text.ID, payload.ID} {
		m, err := b.SendSnippet("123@g.us", id, reply.Message, vars)
		if err != nil {
			t.Fatal(err)
		}
		r, _ := b.store.message(b.ctx, "123@g.us", m.ID)
		if !strings.HasPrefix(r.Text, "Hi @456,") && r.Text != "Hi @456" || r.mentions != "456@lid" {
			t.Fatalf("reply: %q %v", r.Text, r.mentions)
		}
		if id == text.ID && !strings.Contains(r.Text, "{mention} stays") {
			t.Fatalf("escaped: %q", r.Text)
		}
		m, err = b.SendSnippet("123@g.us", id, nil, vars)
		if err != nil {
			t.Fatal(err)
		}
		r, _ = b.store.message(b.ctx, "123@g.us", m.ID)
		if !strings.HasPrefix(r.Text, "Hi @123@g.us") || r.mentions != groupMention("123@g.us", "Work") {
			t.Fatalf("no reply: %q %v", r.Text, r.mentions)
		}
		if got := b.replaceMentions(b.ctx, "123@g.us", "@123@g.us", r.mentions); strings.ContainsRune(got, rune(model.MentionAdmins)) {
			t.Fatalf("shown as @admin: %q", got)
		}
		var raw []byte
		_ = b.db.QueryRow(`SELECT raw_payload FROM wz_messages WHERE chat = ? AND id = ?`, "123@g.us", m.ID).Scan(&raw)
		var sent waE2E.Message
		if err := proto.Unmarshal(raw, &sent); err != nil || sent.GetGroupMentionedMessage() == nil {
			t.Fatal("not sent as a group mention", err)
		}
		ci := describe(&sent).ctx
		if len(ci.GetMentionedJID()) != 0 || len(ci.GetGroupMentions()) != 1 {
			t.Fatalf("context: %v", ci)
		}
	}
}
