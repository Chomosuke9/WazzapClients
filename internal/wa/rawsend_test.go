package wa

import (
	"strings"
	"testing"

	whatsmeow "github.com/polymorfa/hypermeow"
	"github.com/polymorfa/hypermeow/proto/waE2E"
	"github.com/polymorfa/hypermeow/store"
	waLog "github.com/polymorfa/hypermeow/util/log"
	"google.golang.org/protobuf/proto"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

// A message's JSON is read in Baileys' spelling and in the protobufs'.
func TestDecodePayload(t *testing.T) {
	for _, body := range []string{
		`{"extendedTextMessage":{"text":"hi","contextInfo":{"mentionedJid":["1@lid"],"externalAdReply":{"title":"t","renderLargerThumbnail":true,"mediaUrl":"u"}}}}`,
		`{"extendedTextMessage":{"text":"hi","contextInfo":{"mentionedJID":["1@lid"],"external_ad_reply":{"title":"t","renderLargerThumbnail":true,"mediaURL":"u"}}}}`,
	} {
		m, err := decodePayload(body)
		if err != nil {
			t.Fatal(err)
		}
		ci := m.GetExtendedTextMessage().GetContextInfo()
		ad := ci.GetExternalAdReply()
		if ci.GetMentionedJID()[0] != "1@lid" || ad.GetMediaURL() != "u" || !ad.GetRenderLargerThumbnail() {
			t.Fatalf("%s: %v", body, m)
		}
	}
	// Nested messages, lists, enums by number, numbers as text.
	m, err := decodePayload(`{"viewOnceMessage":{"message":{"interactiveMessage":{"body":{"text":"b"},"nativeFlowMessage":{"buttons":[{"name":"quick_reply","buttonParamsJson":"{}"}]}}}}}`)
	if err != nil || m.GetViewOnceMessage().GetMessage().GetInteractiveMessage().GetNativeFlowMessage().GetButtons()[0].GetButtonParamsJSON() != "{}" {
		t.Fatal(m, err)
	}
	m, err = decodePayload(`{"protocolMessage":{"type":0,"key":{"remoteJid":"1@g.us","id":"x"}}}`)
	if err != nil || m.GetProtocolMessage().GetKey().GetRemoteJID() != "1@g.us" {
		t.Fatal(m, err)
	}
	for body, want := range map[string]string{
		``: "Invalid", `{}`: "empty", `[]`: "Invalid", `{"nope":1}`: "nope", `{"conversation":1}`: "Invalid",
		`{"extendedTextMessage":{"contextInfo":` + strings.Repeat(`{"quotedMessage":{"extendedTextMessage":{"contextInfo":`, 50) + `{}` + strings.Repeat("}}}", 50) + `}}`: "Invalid",
		`"` + strings.Repeat("x", model.MaxSnippetBytes) + `"`: "1 MB",
	} {
		if _, err := decodePayload(body); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%.60s: %v, want %s", body, err, want)
		}
	}
}

func TestSendPayload(t *testing.T) {
	b := testBackend(t)
	if _, _, err := b.SendPayload("123@g.us", `{"conversation":"x"}`, "", nil); err == nil {
		t.Fatal("sent without a client")
	}
	b.onMessage(snippetEvent("q", &waE2E.Message{Conversation: proto.String("question")}))
	b.cli = whatsmeow.NewClient(&store.Device{}, waLog.Noop) // unlinked: sends fail offline
	reply, _ := b.store.message(b.ctx, "123@g.us", "q")
	m, shown, err := b.SendPayload("123@g.us", `{"conversation":"answer"}`, "", reply.Message)
	if err != nil || !shown || m.Text != "answer" || m.Quote == nil || m.Quote.ID != "q" {
		t.Fatalf("%+v %v", m, err)
	}
	raw, err := b.rawMessage("123@g.us", m.ID)
	if err != nil || raw.GetExtendedTextMessage().GetContextInfo().GetStanzaID() != "q" {
		t.Fatal(raw, err)
	}
	m, shown, err = b.SendPayload("123@g.us", `{"locationMessage":{"degreesLatitude":-6.2,"degreesLongitude":106.8,"name":"Monas"}}`, "", nil)
	if err != nil || !shown || m.Location == nil || m.Media != model.MediaLocation {
		t.Fatalf("%+v %v", m, err)
	}
	// What doesn't show needs the connection, and stores nothing.
	if m, _, err = b.SendPayload("123@g.us", `{"reactionMessage":{"text":"👍","key":{"remoteJid":"123@g.us","id":"q"}}}`, "", nil); err == nil || m != nil {
		t.Fatal(m, err)
	}
	b.cli = nil // its store knows no phone numbers
	b.SetPref(model.PrefGhost, "on")
	if _, _, err = b.SendPayload("123@g.us", `{"conversation":"x"}`, "", nil); err == nil {
		t.Fatal("sent in ghost mode")
	}
	for id, want := range map[string]string{"123@g.us": "123@g.us", "62812@s.whatsapp.net": "62812@s.whatsapp.net",
		"5@lid": "5@lid", "status@broadcast": "", "work": "", "": "", "@g.us": ""} {
		if got := b.ChatID(id); got != want {
			t.Errorf("ChatID(%q) = %q", id, got)
		}
	}
}
