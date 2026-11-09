package wa

import (
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/polymorfa/hypermeow/proto/waE2E"
	"github.com/polymorfa/hypermeow/store"
	"github.com/polymorfa/hypermeow/types"
	"google.golang.org/protobuf/proto"

	whatsmeow "github.com/polymorfa/hypermeow"
	waLog "github.com/polymorfa/hypermeow/util/log"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

// A custom mention goes as @admin does, under its name, and reads back the
// same: the name shows, and editing it gives back whom it notifies.
func TestCustomMentionMessage(t *testing.T) {
	b := testBackend(t)
	b.cli = whatsmeow.NewClient(&store.Device{}, waLog.Noop)
	const chat = "123@g.us"
	people := []string{"456@lid", "789@lid"}
	m := b.Send(chat, model.Draft{Text: "Hi @" + chat, MentionChat: "Tim frontend", Mentions: people})
	if m == nil {
		t.Fatal("not sent")
	}
	r, ok := b.store.message(b.ctx, chat, m.ID)
	if !ok {
		t.Fatal("not stored")
	}
	var raw []byte
	_ = b.db.QueryRow(`SELECT raw_payload FROM wz_messages WHERE chat = ? AND id = ?`, chat, m.ID).Scan(&raw)
	var sent waE2E.Message
	if err := proto.Unmarshal(raw, &sent); err != nil || sent.GetGroupMentionedMessage() == nil {
		t.Fatal("not sent as a group mention", err)
	}
	ci := describe(&sent).ctx
	if !slices.Equal(ci.GetMentionedJID(), people) || len(ci.GetGroupMentions()) != 1 ||
		ci.GetGroupMentions()[0].GetGroupSubject() != "Tim frontend" || ci.GetGroupMentions()[0].GetGroupJID() != chat {
		t.Fatalf("context: %v", ci)
	}

	got := b.replaceMentions(b.ctx, chat, r.Text, r.mentions)
	if want := "Hi ⁨" + string(model.MentionGroup) + "@Tim frontend⁩"; got != want {
		t.Fatalf("shown %q, want %q", got, want)
	}

	text, refs := b.EditText(m)
	if text != "Hi @Tim frontend" {
		t.Fatalf("to edit: %q", text)
	}
	if len(refs) != 1 || refs[0].ID != "@custom" || refs[0].Name != "Tim frontend" || !slices.Equal(refs[0].People, people) {
		t.Fatalf("refs %+v", refs)
	}
}

// Several names in one message each go under a JID of their own, since
// WhatsApp shows one name for each, and each reads back by its name.
func TestCustomMentionsInOneMessage(t *testing.T) {
	b := testBackend(t)
	b.cli = whatsmeow.NewClient(&store.Device{}, waLog.Noop)
	const chat = "120363012345678901@g.us"
	people := []string{"456@lid", "789@lid"}
	second, third := model.GroupNameJID(chat, 1), model.GroupNameJID(chat, 2)
	if second != "1203630123456789011@g.us" {
		t.Fatalf("second JID %q", second)
	}
	m := b.Send(chat, model.Draft{Text: "@" + chat + " @" + second + " @" + third,
		MentionChat: "Tim frontend", GroupNames: []string{"Tim backend", "admin"}, Mentions: people})
	if m == nil {
		t.Fatal("not sent")
	}
	var raw []byte
	_ = b.db.QueryRow(`SELECT raw_payload FROM wz_messages WHERE chat = ? AND id = ?`, chat, m.ID).Scan(&raw)
	var sent waE2E.Message
	if err := proto.Unmarshal(raw, &sent); err != nil || sent.GetGroupMentionedMessage() == nil {
		t.Fatal("not sent as a group mention", err)
	}
	var got []string
	for _, gm := range describe(&sent).ctx.GetGroupMentions() {
		got = append(got, gm.GetGroupJID()+" "+gm.GetGroupSubject())
	}
	if want := []string{chat + " Tim frontend", second + " Tim backend", third + " admin"}; !slices.Equal(got, want) {
		t.Fatalf("group mentions %q, want %q", got, want)
	}

	r, _ := b.store.message(b.ctx, chat, m.ID)
	shown := b.replaceMentions(b.ctx, chat, r.Text, r.mentions)
	g, a := string(model.MentionGroup), string(model.MentionAdmins)
	if want := "⁨" + g + "@Tim frontend⁩ ⁨" + g + "@Tim backend⁩ ⁨" + a + "@admin⁩"; shown != want {
		t.Fatalf("shown %q, want %q", shown, want)
	}

	text, refs := b.EditText(m)
	if text != "@Tim frontend @Tim backend @admin" {
		t.Fatalf("to edit: %q", text)
	}
	if len(refs) != 3 || refs[0].Name != "Tim frontend" || refs[0].ID != "@custom" || !slices.Equal(refs[0].People, people) ||
		refs[1].Name != "Tim backend" || refs[1].ID != "@custom" || refs[2].ID != "@admin" {
		t.Fatalf("refs %+v", refs)
	}
}

// A made-up group JID must be one WhatsApp takes (WAWebWidValidator: up to
// 20 digits, no leading zero) or phones show it as text.
func TestGroupNameJID(t *testing.T) {
	valid := regexp.MustCompile(`^[1-9][0-9]{0,19}@g\.us$`)
	for _, c := range []struct {
		chat string
		n    int
		want string
	}{
		{"120363012345678901@g.us", 3, "1203630123456789013@g.us"},
		{"120363012345678901@g.us", 12, "12036301234567890112@g.us"},
		{"120363012345678901@g.us", 123, "20363012345678901123@g.us"}, // too long: the first digit goes
		{"6281234567-1600000000@g.us", 3, "28123456716000000003@g.us"},
	} {
		got := model.GroupNameJID(c.chat, c.n)
		if got != c.want {
			t.Errorf("GroupNameJID(%q, %d) = %q, want %q", c.chat, c.n, got, c.want)
		}
		if !valid.MatchString(got) {
			t.Errorf("%q isn't a group JID WhatsApp takes", got)
		}
		if c.n < 100 && !model.IsGroupNameJID(got, c.chat) {
			t.Errorf("%q not one of %q's", got, c.chat)
		}
	}
	if !model.IsGroupNameJID("120363012345678901@g.us", "120363012345678901@g.us") {
		t.Error("the group's own JID isn't one of its names")
	}
	for _, jid := range []string{"120363012345678902@g.us", "1203630123456789010@g.us", "120363012345678901-1@g.us", "1203630123456789011@s.whatsapp.net"} {
		if model.IsGroupNameJID(jid, "120363012345678901@g.us") {
			t.Errorf("%q taken for the group's", jid)
		}
	}
}

// A reply quotes a group mention by its name, whatever the reply's payload
// carries of it, and whatever the reply mentions itself.
func TestQuotedGroupMention(t *testing.T) {
	b := testBackend(t)
	const chat = "123@g.us"
	const token = "Hi @" + chat
	named := func(id, subject string) string {
		b.onMessage(snippetEvent(id, &waE2E.Message{GroupMentionedMessage: &waE2E.FutureProofMessage{Message: &waE2E.Message{
			ExtendedTextMessage: &waE2E.ExtendedTextMessage{Text: proto.String(token), ContextInfo: &waE2E.ContextInfo{
				MentionedJID:  []string{"456@lid"},
				GroupMentions: []*waE2E.GroupMention{{GroupJID: proto.String(chat), GroupSubject: proto.String(subject)}},
			}}}}}))
		return id
	}
	custom := named("custom", "Tim frontend")
	admin := named("admin", "admin")
	if err := b.store.ensureChat(b.ctx, b.db, chat, true, "Work"); err != nil {
		t.Fatal(err)
	}
	quoteOfReply := func(id string, ci *waE2E.ContextInfo) string {
		t.Helper()
		b.onMessage(snippetEvent(id, &waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{Text: proto.String("Ya saya"), ContextInfo: ci}}))
		r, ok := b.store.message(b.ctx, chat, id)
		if !ok {
			t.Fatal("no reply")
		}
		m := b.resolve(b.ctx, r, true)
		if m.Quote == nil {
			t.Fatal("no quote")
		}
		return stripMarks(m.Quote.Text)
	}
	textOnly := func(id string) *waE2E.ContextInfo {
		return &waE2E.ContextInfo{StanzaID: proto.String(id), Participant: proto.String("456@lid"),
			QuotedMessage: &waE2E.Message{Conversation: proto.String(token)}}
	}
	for _, c := range []struct {
		name, id, want string
		ci             *waE2E.ContextInfo
	}{
		{"custom, text alone", "r1", "Hi @Tim frontend", textOnly(custom)},
		{"admin, text alone", "r2", "Hi @admin", textOnly(admin)},
		{"original unknown", "r3", "Hi @Work", textOnly("gone")},
		{"name in the quote, and another in the reply", "r4", "Hi @Tim frontend", &waE2E.ContextInfo{
			StanzaID: proto.String("gone"), Participant: proto.String("456@lid"),
			GroupMentions: []*waE2E.GroupMention{{GroupJID: proto.String(chat), GroupSubject: proto.String("Tim backend")}},
			QuotedMessage: &waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{Text: proto.String(token), ContextInfo: &waE2E.ContextInfo{
				GroupMentions: []*waE2E.GroupMention{{GroupJID: proto.String(chat), GroupSubject: proto.String("Tim frontend")}}}}},
		}},
	} {
		if got := quoteOfReply(c.id, c.ci); got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}

	// A private reply: the original is in the group, the quote in a chat.
	got := b.quoteMentions(b.ctx, "789@lid", &rawQuote{id: custom, remote: chat}, token)
	if stripMarks(got) != "Hi @Tim frontend" {
		t.Errorf("private reply: %q", got)
	}
	// A group this app doesn't know of.
	if got := b.quoteMentions(b.ctx, chat, &rawQuote{id: "gone"}, "@999@g.us"); stripMarks(got) != "@group" {
		t.Errorf("unknown group: %q", got)
	}

	// What this app sends in a reply reads well on any client.
	if q := b.quotedMessage(b.ctx, chat, custom).GetConversation(); q != "Hi @Tim frontend" {
		t.Errorf("sent quote: %q", q)
	}
	// People stay as they are: other clients read "@<user>" themselves.
	b.onMessage(snippetEvent("person", &waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{Text: proto.String("Hi @456"),
		ContextInfo: &waE2E.ContextInfo{MentionedJID: []string{"456@lid"}}}}))
	if q := b.quotedMessage(b.ctx, chat, "person").GetConversation(); q != "Hi @456" {
		t.Errorf("person: %q", q)
	}
}

// A media message's caption is quoted by name too.
func TestQuotedCaptionGroupMention(t *testing.T) {
	b := testBackend(t)
	const chat = "123@g.us"
	b.onMessage(snippetEvent("photo", &waE2E.Message{ImageMessage: &waE2E.ImageMessage{
		Caption: proto.String("Hi @" + chat), Mimetype: proto.String("image/jpeg"), MediaKey: []byte("key"),
		ContextInfo: &waE2E.ContextInfo{
			MentionedJID:  []string{"456@lid"},
			GroupMentions: []*waE2E.GroupMention{{GroupJID: proto.String(chat), GroupSubject: proto.String("Tim")}},
		},
	}}))
	im := b.quotedMessage(b.ctx, chat, "photo").GetImageMessage()
	if im == nil {
		t.Fatal("not quoted as a photo")
	}
	if im.GetCaption() != "Hi @Tim" {
		t.Fatalf("caption: %q", im.GetCaption())
	}
}

// A forward goes without the mentions, so it says the name, in text and in
// captions: "@<group JID>" would mean nothing to anyone who gets it.
func TestForwardedGroupMention(t *testing.T) {
	b := testBackend(t)
	const chat = "123@g.us"
	ci := func() *waE2E.ContextInfo {
		return &waE2E.ContextInfo{MentionedJID: []string{"456@lid"},
			GroupMentions: []*waE2E.GroupMention{{GroupJID: proto.String(chat), GroupSubject: proto.String("Tim")}}}
	}
	b.onMessage(snippetEvent("text", &waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{
		Text: proto.String("Hi @" + chat), ContextInfo: ci()}}))
	b.onMessage(snippetEvent("photo", &waE2E.Message{ImageMessage: &waE2E.ImageMessage{
		Caption: proto.String("Hi @" + chat), Mimetype: proto.String("image/jpeg"), MediaKey: []byte("key"), ContextInfo: ci()}}))
	b.cli = whatsmeow.NewClient(&store.Device{}, waLog.Noop)

	const to = "789@lid"
	if err := b.store.ensureChat(b.ctx, b.db, to, false, ""); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"text", "photo"} {
		if !b.sendCopy(&model.Message{ID: id, ChatID: chat}, to, true, nil) {
			t.Fatalf("%s not forwarded", id)
		}
	}
	rows, err := b.db.Query(`SELECT text, raw_payload FROM wz_messages WHERE chat = ?`, to)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	n := 0
	for rows.Next() {
		var text string
		var raw []byte
		if err := rows.Scan(&text, &raw); err != nil {
			t.Fatal(err)
		}
		c, _ := describeRaw(raw)
		if text != "Hi @Tim" || c.text != "Hi @Tim" || !c.ctx.GetIsForwarded() {
			t.Errorf("forwarded %q, sent %q, context %v", text, c.text, c.ctx)
		}
		n++
	}
	if n != 2 {
		t.Fatalf("%d forwards", n)
	}
}

// One naming you counts as for you, like a mention of you does.
func TestCustomMentionOfMe(t *testing.T) {
	b := testBackend(t)
	phone := types.NewJID("6281", types.DefaultUserServer)
	b.cli = whatsmeow.NewClient(&store.Device{ID: &phone, LID: types.NewJID("111", types.HiddenUserServer)}, waLog.Noop)
	const chat = "123@g.us"
	mentions := groupMention(chat, "Tim frontend") + ",456@lid,111@lid"
	text := b.replaceMentions(b.ctx, chat, "@"+chat+" hi", mentions)
	if !strings.ContainsRune(text, model.MentionNotifies) || !strings.Contains(text, "@Tim frontend") {
		t.Fatalf("shown %q", text)
	}
	if !(&model.Message{Text: text}).ForMe("") {
		t.Fatal("not for you")
	}
}

// @admin and a snippet's {mention} read as before.
func TestGroupMentionsUnchanged(t *testing.T) {
	b := testBackend(t)
	const chat = "123@g.us"
	admin := b.replaceMentions(b.ctx, chat, "@"+chat, groupMention(chat, "admin")+",456@lid")
	if !strings.ContainsRune(admin, model.MentionAdmins) {
		t.Errorf("@admin: %q", admin)
	}
	snippet := b.replaceMentions(b.ctx, chat, "@"+chat, groupMention(chat, "Work"))
	if strings.ContainsAny(snippet, string([]rune{model.MentionAdmins, model.MentionGroup, model.MentionNotifies})) {
		t.Errorf("snippet: %q", snippet)
	}
	other := b.replaceMentions(b.ctx, chat, "@999@g.us", groupMention("999@g.us", "Sub")+",456@lid")
	if strings.ContainsAny(other, string([]rune{model.MentionAdmins, model.MentionGroup, model.MentionNotifies})) {
		t.Errorf("another group: %q", other)
	}
}
