package wa

import (
	"bytes"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/polymorfa/hypermeow/proto/waE2E"
	"github.com/polymorfa/hypermeow/types"
	"github.com/polymorfa/hypermeow/types/events"
	"google.golang.org/protobuf/proto"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

// shown is what a message shows of its payload.
type shown struct {
	Kind                        model.Kind
	Media                       model.Media
	Text                        string
	Duration                    int
	Thumb                       []byte
	Link                        *model.LinkPreview
	FileName, FileType          string
	FileSize                    int64
	Pages                       int
	Waveform                    []byte
	Album                       string
	Forwarded                   bool
	Footer                      string
	Buttons                     []model.Button
	Location                    *model.Location
	Contacts                    []model.ContactCard
	PollOptions                 []string
	PollMax                     int
	EventName, EventDescription string
	EventStart, EventEnd        time.Time
	EventPlace                  *model.Location
	EventCanceled, OnPhone      bool
}

func shownOf(m *model.Message) shown {
	s := shown{Kind: m.Kind, Media: m.Media, Text: m.Text, Duration: m.Duration, Thumb: m.Thumb, Link: m.Link,
		FileName: m.FileName, FileType: m.FileType, FileSize: m.FileSize, Pages: m.Pages, Waveform: m.Waveform,
		Album: m.Album, Forwarded: m.Forwarded, Footer: m.Footer, Buttons: m.Buttons, Location: m.Location,
		Contacts: m.Contacts, OnPhone: m.OnPhone}
	if p := m.Poll; p != nil {
		s.PollMax = p.Max
		for _, o := range p.Options {
			s.PollOptions = append(s.PollOptions, o.Name)
		}
	}
	if e := m.Event; e != nil {
		s.EventName, s.EventDescription, s.EventStart, s.EventEnd = e.Name, e.Description, e.Start, e.End
		s.EventPlace, s.EventCanceled = e.Place, e.Canceled
	}
	return s
}

// A stored message reads back as parse read it when it came, whatever it
// is: its payload is all that's kept of it.
func TestPayloadReadsBackAsParsed(t *testing.T) {
	b := testBackend(t)
	const chat = "123@g.us"
	key := bytes.Repeat([]byte{1}, 32)
	sha := bytes.Repeat([]byte{2}, 32)
	fwd := &waE2E.ContextInfo{IsForwarded: proto.Bool(true), MentionedJID: []string{"789@lid"}}
	album := &waE2E.Message{ImageMessage: &waE2E.ImageMessage{URL: proto.String("https://mmg/x"), DirectPath: proto.String("/v/x"),
		MediaKey: key, JPEGThumbnail: []byte("photo"), Caption: proto.String("beach"), Mimetype: proto.String("image/jpeg"),
		ContextInfo: fwd}}
	inAlbum(album, types.NewJID("123", types.GroupServer), "ALBUM")
	for id, msg := range map[string]*waE2E.Message{
		"text": {Conversation: proto.String("hello")},
		"mention": {ExtendedTextMessage: &waE2E.ExtendedTextMessage{Text: proto.String("hi @789"),
			ContextInfo: &waE2E.ContextInfo{MentionedJID: []string{"789@lid"}}}},
		"link": {ExtendedTextMessage: &waE2E.ExtendedTextMessage{Text: proto.String("see https://example.com"),
			MatchedText: proto.String("https://example.com"), Title: proto.String("Example"), Description: proto.String("A page"),
			JPEGThumbnail: []byte("small"), ThumbnailDirectPath: proto.String("/v/big"), MediaKey: key,
			ThumbnailWidth: proto.Uint32(800), ThumbnailHeight: proto.Uint32(400)}},
		"photo": album,
		"gif": {VideoMessage: &waE2E.VideoMessage{GifPlayback: proto.Bool(true), Seconds: proto.Uint32(4),
			JPEGThumbnail: []byte("frame"), MediaKey: key, DirectPath: proto.String("/v/g")}},
		"voice": {AudioMessage: &waE2E.AudioMessage{PTT: proto.Bool(true), Seconds: proto.Uint32(15),
			Waveform: []byte{1, 50, 99}, FileLength: proto.Uint64(4096), Mimetype: proto.String("audio/ogg")}},
		"doc": {DocumentWithCaptionMessage: &waE2E.FutureProofMessage{Message: &waE2E.Message{
			DocumentMessage: &waE2E.DocumentMessage{FileName: proto.String("a.pdf"), Caption: proto.String("read this"),
				FileLength: proto.Uint64(2048), PageCount: proto.Uint32(3), Mimetype: proto.String("application/pdf"),
				FileSHA256: sha}}}},
		"sticker": {StickerMessage: &waE2E.StickerMessage{FileSHA256: sha, MediaKey: key, DirectPath: proto.String("/v/s")}},
		"place": {LocationMessage: &waE2E.LocationMessage{DegreesLatitude: proto.Float64(-6.2),
			DegreesLongitude: proto.Float64(106.8), Name: proto.String("Monas"), JPEGThumbnail: []byte("map")}},
		"cards": {ContactsArrayMessage: &waE2E.ContactsArrayMessage{DisplayName: proto.String("2 contacts"),
			Contacts: []*waE2E.ContactMessage{{DisplayName: proto.String("Ann"), Vcard: proto.String(vcard("Ann", "6281"))},
				{DisplayName: proto.String("Bo"), Vcard: proto.String(vcard("Bo", "6282"))}}}},
		"poll": {PollCreationMessageV3: &waE2E.PollCreationMessage{Name: proto.String("Lunch?"),
			Options:                []*waE2E.PollCreationMessage_Option{{OptionName: proto.String("Soto")}, {OptionName: proto.String("Bakso")}},
			SelectableOptionsCount: proto.Uint32(1)}},
		"event": {EventMessage: &waE2E.EventMessage{Name: proto.String("Picnic"), Description: proto.String("Bring food"),
			StartTime: proto.Int64(1700003600), Location: &waE2E.LocationMessage{Name: proto.String("Park")}}},
		"buttons": {ButtonsMessage: &waE2E.ButtonsMessage{ContentText: proto.String("Pick one"), FooterText: proto.String("Shop"),
			Buttons: []*waE2E.ButtonsMessage_Button{{ButtonID: proto.String("y"),
				ButtonText: &waE2E.ButtonsMessage_Button_ButtonText{DisplayText: proto.String("Yes")}}}}},
		"once": {ViewOnceMessageV2: &waE2E.FutureProofMessage{Message: &waE2E.Message{
			ImageMessage: &waE2E.ImageMessage{ViewOnce: proto.Bool(true), JPEGThumbnail: []byte("blurred")}}}},
		"ephemeral": {EphemeralMessage: &waE2E.FutureProofMessage{Message: &waE2E.Message{
			ExtendedTextMessage: &waE2E.ExtendedTextMessage{Text: proto.String("vanishing")}}}},
	} {
		e := snippetEvent(id, msg)
		p, ok := b.parse(b.ctx, e)
		if !ok {
			t.Fatalf("%s isn't a message", id)
		}
		want := shownOf(p.msg.Message)
		if p.msg.Kind == model.KindViewOnce {
			want.OnPhone = true // its media's key never came
		}
		wantBlob := describe(msg).keptBlob(msg)
		b.onMessage(e)
		r, ok := b.store.message(b.ctx, chat, id)
		if !ok {
			t.Fatalf("%s isn't stored", id)
		}
		if got := shownOf(r.Message); !reflect.DeepEqual(got, want) {
			t.Errorf("%s reads back as\n%+v\nnot\n%+v", id, got, want)
		}
		if _, blob, _ := b.store.mediaBlob(b.ctx, chat, id); !bytes.Equal(blob, wantBlob) {
			t.Errorf("%s: media blob of %d bytes, want %d", id, len(blob), len(wantBlob))
		}
	}
	if r, _ := b.store.message(b.ctx, chat, "mention"); r.mentions != "789@lid" {
		t.Errorf("mentions %q", r.mentions)
	}
	if r, _ := b.store.message(b.ctx, chat, "buttons"); r.buttons.empty() || r.buttons.Buttons[0].Value != "y" {
		t.Errorf("buttons %+v", r.buttons)
	}
	// The chat list reads what it shows of the last message from it too.
	b.onMessage(snippetEvent("last", &waE2E.Message{AudioMessage: &waE2E.AudioMessage{PTT: proto.Bool(true),
		Seconds: proto.Uint32(42)}}))
	if rc, ok := b.store.chat(b.ctx, chat); !ok || rc.Last == nil || rc.Last.ID != "last" || rc.Last.Duration != 42 {
		t.Errorf("chat list's last message: %+v", rc.Last)
	}
}

// A reply's quote is read from the context its payload carries: the
// quoted message travels with it, or else the stored one is looked up.
func TestQuoteFromPayload(t *testing.T) {
	b := testBackend(t)
	const chat = "123@g.us"
	b.onMessage(snippetEvent("orig", &waE2E.Message{Conversation: proto.String("the original")}))
	for id, ci := range map[string]*waE2E.ContextInfo{
		"carried": {StanzaID: proto.String("orig"), Participant: proto.String("456@lid"),
			QuotedMessage: &waE2E.Message{ImageMessage: &waE2E.ImageMessage{Caption: proto.String("a photo")}}},
		"looked up": {StanzaID: proto.String("orig")},
		"unknown":   {StanzaID: proto.String("nope")},
	} {
		b.onMessage(snippetEvent(id, &waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{
			Text: proto.String("reply"), ContextInfo: ci}}))
	}
	byID := map[string]*model.Message{}
	for _, m := range b.Messages(chat, 10) {
		byID[m.ID] = m
	}
	if q := byID["carried"].Quote; q == nil || q.ID != "orig" || q.Text != "a photo" || q.Media != model.MediaImage ||
		q.SenderID != "456@lid" {
		t.Errorf("carried quote %+v", q)
	}
	if q := byID["looked up"].Quote; q == nil || q.Text != "the original" || q.SenderID != "456@lid" {
		t.Errorf("looked up quote %+v", q)
	}
	if q := byID["unknown"].Quote; q != nil {
		t.Errorf("quote of a message we don't have: %+v", q)
	}

	// A reply to a poll quotes the poll, not only its question, so the
	// quote shows what it is.
	b.onMessage(snippetEvent("poll", &waE2E.Message{PollCreationMessageV3: &waE2E.PollCreationMessage{
		Name: proto.String("Lunch?"), Options: []*waE2E.PollCreationMessage_Option{{OptionName: proto.String("Soto")}},
		ContextInfo: &waE2E.ContextInfo{StanzaID: proto.String("orig")}}}))
	poll, _ := b.store.message(b.ctx, chat, "poll")
	ci := &waE2E.ContextInfo{}
	b.quote(chat, poll.Message, ci)
	if q := ci.GetQuotedMessage(); q.GetPollCreationMessageV3().GetName() != "Lunch?" ||
		q.GetPollCreationMessageV3().GetContextInfo() != nil {
		t.Errorf("poll quoted as %v", q)
	}
	b.onMessage(snippetEvent("poll reply", &waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{
		Text: proto.String("soto!"), ContextInfo: ci}}))
	for _, m := range b.Messages(chat, 10) {
		if m.ID == "poll reply" && (m.Quote == nil || m.Quote.Media != model.MediaPoll || m.Quote.Text != "Lunch?") {
			t.Errorf("reply to a poll quotes %+v", m.Quote)
		}
	}
}

// An edit is kept beside the payload, which keeps the original's media;
// its versions are read from both.
func TestEditBesidePayload(t *testing.T) {
	b := testBackend(t)
	const chat = "123@g.us"
	img := &waE2E.ImageMessage{URL: proto.String("https://mmg/x"), DirectPath: proto.String("/v/x"),
		MediaKey: bytes.Repeat([]byte{1}, 32), JPEGThumbnail: []byte("photo"), Caption: proto.String("first")}
	e := snippetEvent("m", &waE2E.Message{ImageMessage: img})
	b.onMessage(e)
	edit := func(caption string, after time.Duration, mentions ...string) {
		ed := snippetEvent("m", &waE2E.Message{ImageMessage: &waE2E.ImageMessage{Caption: proto.String(caption),
			ContextInfo: &waE2E.ContextInfo{MentionedJID: mentions}}})
		ed.IsEdit = true
		ed.Info.Timestamp = e.Info.Timestamp.Add(after)
		b.onMessage(ed)
	}
	edit("second", time.Minute)
	edit("third @789", 2*time.Minute, "789@lid")
	r, _ := b.store.message(b.ctx, chat, "m")
	if r.Text != "third @789" || r.mentions != "789@lid" || string(r.Thumb) != "photo" || r.Edited.IsZero() {
		t.Fatalf("edited: text %q mentions %q thumb %q edited %v", r.Text, r.mentions, r.Thumb, r.Edited)
	}
	if _, blob, _ := b.store.mediaBlob(b.ctx, chat, "m"); !bytes.Equal(blob, marshal(img)) {
		t.Error("the edit lost the photo")
	}
	texts, _, times, err := b.store.versions(b.ctx, chat, "m")
	if err != nil || strings.Join(texts, "|") != "first|second" || !times[0].Equal(e.Info.Timestamp) {
		t.Errorf("versions %q at %v, %v", texts, times, err)
	}
	if body, _ := b.MessagePayload(chat, "m"); !strings.Contains(body, "third") {
		t.Errorf("/catch shows %s, not the edit", body)
	}
	// The original coming again (history) keeps the edit.
	b.onMessage(e)
	if r, _ := b.store.message(b.ctx, chat, "m"); r.Text != "third @789" {
		t.Errorf("text after the original came again: %q", r.Text)
	}
}

// A message deleted for everyone loses its payload, and stays deleted
// when it comes again.
func TestDeletedStaysDeleted(t *testing.T) {
	b := testBackend(t)
	const chat = "123@g.us"
	e := snippetEvent("m", &waE2E.Message{Conversation: proto.String("oops")})
	b.onMessage(e)
	if err := b.store.markDeleted(b.ctx, chat, "m"); err != nil {
		t.Fatal(err)
	}
	b.onMessage(e)
	r, ok := b.store.message(b.ctx, chat, "m")
	if !ok || r.Kind != model.KindDeleted || r.Text != "" {
		t.Errorf("came back as %+v", r.Message)
	}
	if _, err := b.MessagePayload(chat, "m"); err == nil {
		t.Error("its payload came back")
	}
}

// A database from a version that kept messages in a format of its own
// drops them once, keeping chats, settings and snippets.
func TestOldMessagesDropped(t *testing.T) {
	b := testBackend(t)
	snip, err := b.SaveSnippet(model.Snippet{Body: "kept"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b.db.Exec(`DROP TABLE wz_messages;
		CREATE TABLE wz_messages (chat TEXT NOT NULL, id TEXT NOT NULL, sender_jid TEXT NOT NULL DEFAULT '',
			sender_name TEXT NOT NULL DEFAULT '', from_me INTEGER NOT NULL DEFAULT 0, ts INTEGER NOT NULL,
			kind INTEGER NOT NULL DEFAULT 0, text TEXT NOT NULL DEFAULT '', receipt INTEGER NOT NULL DEFAULT 0,
			quote_sender TEXT NOT NULL DEFAULT '', quote_text TEXT NOT NULL DEFAULT '', reaction TEXT NOT NULL DEFAULT '',
			thumb BLOB, media_blob BLOB, raw_payload BLOB, PRIMARY KEY (chat, id));
		INSERT INTO wz_messages (chat, id, ts, text) VALUES ('old@s.whatsapp.net', '1', 1700000000, 'gone');
		DROP TABLE wz_receipts;
		CREATE TABLE wz_receipts (chat TEXT NOT NULL, id TEXT NOT NULL, who TEXT NOT NULL,
			delivered INTEGER NOT NULL DEFAULT 0, read INTEGER NOT NULL DEFAULT 0, played INTEGER NOT NULL DEFAULT 0,
			PRIMARY KEY (chat, id, who));
		INSERT INTO wz_receipts VALUES ('old@s.whatsapp.net', '1', 'a@lid', 1, 2, 0);
		DROP TABLE wz_edits;
		CREATE TABLE wz_edits (chat TEXT NOT NULL, id TEXT NOT NULL, ts INTEGER NOT NULL, text TEXT NOT NULL DEFAULT '',
			mentions TEXT NOT NULL DEFAULT '', PRIMARY KEY (chat, id, ts));
		INSERT INTO wz_edits VALUES ('old@s.whatsapp.net', '1', 1, 'older', '');
		INSERT INTO wz_chats (jid, name, unread, last_ts) VALUES ('old@s.whatsapp.net', 'Old', 3, 1700000000);
		INSERT INTO wz_meta (key, value) VALUES ('file_info_migrated', 'x'), ('pref:theme', 'dark')`); err != nil {
		t.Fatal(err)
	}
	if err := b.store.init(b.ctx); err != nil {
		t.Fatal(err)
	}
	count := func(q string) int {
		t.Helper()
		var n int
		if err := b.db.QueryRow(q).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if n := count(`SELECT COUNT(*) FROM wz_messages`) + count(`SELECT COUNT(*) FROM wz_receipts`) +
		count(`SELECT COUNT(*) FROM wz_edits`); n != 0 {
		t.Errorf("%d old rows kept", n)
	}
	if count(`SELECT COUNT(*) FROM pragma_table_info('wz_messages') WHERE name IN ('edit_payload', 'quote_text')`) != 1 {
		t.Error("wz_messages isn't the new table")
	}
	if count(`SELECT COUNT(*) FROM sqlite_master WHERE name = 'wz_receipts' AND sql LIKE '%WITHOUT ROWID%'`) != 1 {
		t.Error("wz_receipts kept its rowid")
	}
	rc, ok := b.store.chat(b.ctx, "old@s.whatsapp.net")
	if !ok || rc.Name != "Old" || rc.Unread != 0 {
		t.Errorf("chat after the drop: %+v", rc.Chat)
	}
	if b.store.meta(b.ctx, "pref:theme") != "dark" || b.store.meta(b.ctx, "file_info_migrated") != "" {
		t.Error("settings lost, or old migration marks kept")
	}
	if got, err := b.Snippet(snip.ID); err != nil || got.Body != "kept" {
		t.Errorf("snippet after the drop: %+v, %v", got, err)
	}
	// What arrives after is kept from then on.
	b.onMessage(&events.Message{Info: types.MessageInfo{MessageSource: types.MessageSource{
		Chat: types.NewJID("old", types.DefaultUserServer), Sender: types.NewJID("old", types.DefaultUserServer)},
		ID: "2", Timestamp: time.Unix(1700000100, 0)}, Message: &waE2E.Message{Conversation: proto.String("new")}})
	if err := b.store.init(b.ctx); err != nil {
		t.Fatal(err)
	}
	if r, ok := b.store.message(b.ctx, "old@s.whatsapp.net", "2"); !ok || r.Text != "new" {
		t.Error("a new message was dropped")
	}
}
