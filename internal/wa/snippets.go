package wa

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"maps"
	"strings"
	"unicode/utf8"

	"github.com/chomosuke9/wazzapclients/internal/model"
	"github.com/polymorfa/hypermeow/proto/waE2E"
	"github.com/polymorfa/hypermeow/types"
	"github.com/polymorfa/hypermeow/types/events"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

const snippetSchema = `CREATE TABLE IF NOT EXISTS wz_snippets (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	name TEXT NOT NULL UNIQUE COLLATE NOCASE,
	payload INTEGER NOT NULL,
	body TEXT NOT NULL,
	preview TEXT NOT NULL,
	raw_payload BLOB,
	assets TEXT NOT NULL DEFAULT ''
);`

var errNoPayload = errors.New("The original payload isn't stored for this message. Newly received or synced messages will have one.")

func (s *msgStore) setRawPayload(ctx context.Context, chat, id string, msg *waE2E.Message) error {
	_, err := s.db.ExecContext(ctx, `UPDATE wz_messages SET raw_payload = ? WHERE chat = ? AND id = ? AND kind != ? AND edited = 0`,
		marshal(msg), chat, id, int(model.KindDeleted))
	return err
}

func (b *Backend) rawMessage(chat, id string) (*waE2E.Message, error) {
	var data []byte
	var kind int
	err := b.db.QueryRowContext(b.ctx, `SELECT raw_payload, kind FROM wz_messages WHERE chat = ? AND id = ?`, chat, id).Scan(&data, &kind)
	if err != nil && err != sql.ErrNoRows {
		return nil, err
	}
	if len(data) == 0 || kind == int(model.KindDeleted) {
		return nil, errNoPayload
	}
	if kind == int(model.KindViewOnce) && b.Pref(model.PrefViewOnceReplay) != "on" {
		return nil, errors.New("View once payloads aren't available while Replay view once is off.")
	}
	var msg waE2E.Message
	if err := proto.Unmarshal(data, &msg); err != nil {
		return nil, err
	}
	return &msg, nil
}

// MessagePayload exports the complete stored protobuf as JSON, without
// rebuilding content from display text. Unknown wire fields stay in the DB.
func (b *Backend) MessagePayload(chat, id string) (string, error) {
	msg, err := b.rawMessage(chat, id)
	if err != nil {
		return "", err
	}
	data, err := (protojson.MarshalOptions{Indent: "  "}).Marshal(msg)
	return string(data), err
}

func (b *Backend) ExportMessagePayload(chat, id string) (string, error) {
	text, err := b.MessagePayload(chat, id)
	if err != nil {
		return "", err
	}
	return saveDownload("Message payload.json", []byte(text))
}

func (b *Backend) Snippets() ([]model.Snippet, error) {
	rows, err := b.db.QueryContext(b.ctx, `SELECT id, name, payload, preview FROM wz_snippets ORDER BY name COLLATE NOCASE, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Snippet
	for rows.Next() {
		var s model.Snippet
		if err := rows.Scan(&s.ID, &s.Name, &s.Payload, &s.Preview); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func (b *Backend) Snippet(id int64) (s model.Snippet, err error) {
	err = b.db.QueryRowContext(b.ctx, `SELECT id, name, payload, body, preview FROM wz_snippets WHERE id = ?`, id).
		Scan(&s.ID, &s.Name, &s.Payload, &s.Body, &s.Preview)
	return
}

func decodeSnippet(body string) (*waE2E.Message, error) {
	if len(body) > model.MaxSnippetBytes {
		return nil, errors.New("A snippet can contain at most 1 MB of text or JSON.")
	}
	var msg waE2E.Message
	if err := (protojson.UnmarshalOptions{RecursionLimit: 64}).Unmarshal([]byte(body), &msg); err != nil {
		return nil, fmt.Errorf("Invalid message JSON: %w", err)
	}
	return &msg, nil
}

func snippetPreview(body string) string {
	r := []rune(strings.Join(strings.Fields(body), " "))
	if len(r) > 100 {
		return string(r[:100]) + "…"
	}
	return string(r)
}

func (b *Backend) SaveSnippet(s model.Snippet) (model.Snippet, error) {
	return b.saveSnippet(s, nil)
}

func (b *Backend) saveSnippet(s model.Snippet, captured *waE2E.Message) (model.Snippet, error) {
	b.snippetMu.Lock()
	defer b.snippetMu.Unlock()
	s.Name = strings.TrimSpace(s.Name)
	if utf8.RuneCountInString(s.Name) > 80 || strings.ContainsAny(s.Name, "\r\n") {
		return s, errors.New("Use a name of at most 80 characters on one line.")
	}
	if !utf8.ValidString(s.Body) || len(s.Body) > model.MaxSnippetBytes {
		return s, errors.New("A snippet can contain at most 1 MB of UTF-8 text or JSON.")
	}
	if strings.TrimSpace(s.Body) == "" {
		return s, errors.New("Enter a message first.")
	}
	var raw []byte
	assets := ""
	s.Preview = snippetPreview(s.Body)
	if s.Payload {
		msg, err := decodeSnippet(s.Body)
		if err != nil {
			return s, err
		}
		if captured != nil {
			msg = proto.Clone(captured).(*waE2E.Message)
		}
		// Renaming a captured snippet must preserve unknown protobuf fields.
		if s.ID != 0 {
			var body string
			var saved []byte
			if b.db.QueryRowContext(b.ctx, `SELECT body, raw_payload FROM wz_snippets WHERE id = ? AND payload = 1`, s.ID).Scan(&body, &saved) == nil && body == s.Body && len(saved) > 0 {
				if err := proto.Unmarshal(saved, msg); err != nil {
					return s, err
				}
			}
		}
		ready, err := prepareSnippetMessage(msg)
		if err != nil {
			return s, err
		}
		assets, err = b.keepSnippetMedia(ready)
		if err != nil {
			return s, err
		}
		raw = marshal(msg)
		s.Preview = snippetPreview(describe(ready).text)
		if s.Preview == "" {
			s.Preview = "Message payload"
		}
	}
	tx, err := b.db.BeginTx(b.ctx, nil)
	if err != nil {
		return s, err
	}
	defer tx.Rollback()
	if s.ID == 0 {
		// Names are generated and checked for collisions within the transaction.
		var next int64
		if err = tx.QueryRowContext(b.ctx, `SELECT COALESCE(MAX(id), 0)+1 FROM wz_snippets`).Scan(&next); err != nil {
			return s, err
		}
		if s.Name == "" {
			for n := next; ; n++ {
				s.Name = fmt.Sprintf("Snippet %d", n)
				var exists int
				if err = tx.QueryRowContext(b.ctx, `SELECT COUNT(*) FROM wz_snippets WHERE name = ?`, s.Name).Scan(&exists); err != nil {
					return s, err
				}
				if exists == 0 {
					break
				}
			}
		}
		res, e := tx.ExecContext(b.ctx, `INSERT INTO wz_snippets(name,payload,body,preview,raw_payload,assets) VALUES(?,?,?,?,?,?)`, s.Name, s.Payload, s.Body, s.Preview, raw, assets)
		if e != nil {
			return s, snippetStoreError(e)
		}
		s.ID, err = res.LastInsertId()
	} else {
		if s.Name == "" {
			return s, errors.New("Enter a name for the snippet.")
		}
		res, e := tx.ExecContext(b.ctx, `UPDATE wz_snippets SET name=?,payload=?,body=?,preview=?,raw_payload=?,assets=? WHERE id=?`, s.Name, s.Payload, s.Body, s.Preview, raw, assets, s.ID)
		if e != nil {
			return s, snippetStoreError(e)
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return s, errors.New("This snippet no longer exists.")
		}
	}
	if err != nil {
		return s, err
	}
	if err = tx.Commit(); err == nil {
		b.cleanSnippetMedia()
	}
	return s, err
}

func snippetStoreError(err error) error {
	if strings.Contains(err.Error(), "UNIQUE constraint") {
		return errors.New("A snippet with that name already exists.")
	}
	return err
}

func (b *Backend) SaveMessageSnippet(chat, id, name string) (model.Snippet, error) {
	r, ok := b.store.message(b.ctx, chat, id)
	if !ok {
		return model.Snippet{}, errors.New("This message is no longer stored.")
	}
	if r.Kind == model.KindDeleted {
		return model.Snippet{}, errors.New("This message was deleted.")
	}
	if r.Kind == model.KindViewOnce {
		return model.Snippet{}, errors.New("View once messages can't be saved as snippets.")
	}
	if r.Media == model.MediaNone && r.Kind != model.KindUnsupported && r.buttons.empty() {
		// Use stored text with protocol mention identifiers, not resolved names.
		if len(r.mentions) == 0 && r.Link == nil {
			return b.SaveSnippet(model.Snippet{Name: name, Body: r.Text})
		}
	}
	msg, err := b.rawMessage(chat, id)
	if err != nil {
		return model.Snippet{}, err
	}
	body, err := (protojson.MarshalOptions{Indent: "  "}).Marshal(msg)
	if err != nil {
		return model.Snippet{}, err
	}
	return b.saveSnippet(model.Snippet{Name: name, Payload: true, Body: string(body)}, msg)
}

func (b *Backend) DeleteSnippet(id int64) error {
	b.snippetMu.Lock()
	defer b.snippetMu.Unlock()
	_, err := b.db.ExecContext(b.ctx, `DELETE FROM wz_snippets WHERE id=?`, id)
	if err == nil {
		b.cleanSnippetMedia()
	}
	return err
}

func (b *Backend) SendSnippet(chat string, id int64, reply *model.Message, vars map[string]string) (*model.Message, error) {
	if b.Pref(model.PrefGhost) == "on" {
		return nil, errors.New("Turn off ghost mode to send messages.")
	}
	s, raw, release, err := b.snippetToSend(id)
	if err != nil {
		return nil, err
	}
	async := false
	defer func() {
		if !async {
			release()
		}
	}()
	mention, mentionJID, subject := b.snippetMention(chat, reply, vars)
	vars = maps.Clone(vars)
	if vars == nil {
		vars = map[string]string{}
	}
	vars["mention"] = mention
	if !s.Payload {
		d := model.Draft{Text: model.ExpandSnippet(s.Body, vars), Reply: reply}
		if model.SnippetUses(s.Body, "mention") {
			if mentionJID != "" {
				d.Mentions = []string{mentionJID}
			} else {
				d.MentionChat = subject
			}
		}
		m := b.Send(chat, d)
		if m == nil {
			return nil, errors.New("Couldn't queue the snippet.")
		}
		return m, nil
	}
	var source waE2E.Message
	if err = proto.Unmarshal(raw, &source); err != nil {
		return nil, err
	}
	msg, err := prepareSnippetMessage(&source)
	if err != nil {
		return nil, err
	}
	mentioned := expandSnippetPayload(msg, vars)
	cli, jid := b.client(), types.EmptyJID
	jid, err = types.ParseJID(chat)
	if err != nil || cli == nil {
		return nil, errors.New("Connect to WhatsApp before sending a snippet.")
	}
	if jid.Server != types.DefaultUserServer && jid.Server != types.HiddenUserServer && jid.Server != types.GroupServer {
		return nil, errors.New("Snippets can be sent to chats and groups.")
	}
	wrap := false // in a GroupMentionedMessage, as @admin is
	if mentioned {
		var ci *waE2E.ContextInfo
		msg, ci = snippetContext(msg)
		if mentionJID != "" {
			ci.MentionedJID = append(ci.MentionedJID, mentionJID)
		} else {
			ci.GroupMentions = append(ci.GroupMentions, &waE2E.GroupMention{GroupJID: proto.String(chat), GroupSubject: proto.String(subject)})
			wrap = true
		}
	}
	// Use the regular parser to render every supported card and media kind.
	mid, now := cli.GenerateMessageID(), b.now()
	p, ok := b.parse(b.ctx, &events.Message{Info: types.MessageInfo{MessageSource: types.MessageSource{Chat: jid, Sender: b.ownJID(chat), IsFromMe: true, IsGroup: jid.Server == types.GroupServer}, ID: mid, Timestamp: now}, Message: msg, RawMessage: msg})
	if !ok || p.target != "" {
		return nil, errors.New("This payload isn't a sendable message.")
	}
	p.msg.ChatID, p.msg.Receipt = chat, model.Pending
	if reply != nil {
		var ci *waE2E.ContextInfo
		msg, ci = snippetContext(msg)
		p.msg.Quote = b.quote(chat, reply, ci)
		p.msg.quoteID, p.msg.quoteJID = reply.ID, p.msg.Quote.SenderID
	}
	out := func() *waE2E.Message {
		if wrap {
			return &waE2E.Message{GroupMentionedMessage: &waE2E.FutureProofMessage{Message: msg}}
		}
		return msg
	}
	if err = b.store.ensureChat(b.ctx, b.db, chat, jid.Server == types.GroupServer, ""); err != nil {
		return nil, err
	}
	p.msg.rawPayload = marshal(out())
	if err = b.store.putMessage(b.ctx, b.db, p.msg); err != nil {
		return nil, err
	}
	b.emitChat(chat)
	async = true
	go func() {
		defer release()
		if err := b.uploadSnippetMedia(msg); err != nil {
			b.sendFailed(chat, mid, "Couldn't upload snippet media: "+err.Error())
			return
		}
		c := describe(msg)
		p.msg.mediaBlob = c.blob
		p.msg.rawPayload = marshal(out())
		if err := b.store.putMessage(b.ctx, b.db, p.msg); err != nil {
			b.sendFailed(chat, mid, "Couldn't store the snippet.")
			return
		}
		b.sendAsync(chat, jid, mid, out())
	}()
	return p.msg.Message, nil
}

// snippetMention is what a snippet's {mention} writes, and whom it
// mentions: the author of reply (jid), or without a reply the other
// person in a one-to-one chat (jid), or a group itself, by its name
// (subject), like @admin but notifying no one.
func (b *Backend) snippetMention(chat string, reply *model.Message, vars map[string]string) (text, jid, subject string) {
	if reply != nil {
		j := b.senderOf(reply)
		return "@" + j.User, j.String(), ""
	}
	if j, err := types.ParseJID(chat); err == nil && j.Server != types.GroupServer {
		// A one-to-one chat mentions the person, by the user part only: a
		// group mention there shows "@123@lid" on other devices.
		return "@" + j.User, j.String(), ""
	}
	// The text names the whole JID, as @admin does: "@123@g.us". With only
	// the user part, WhatsApp shows the number instead of the subject.
	subject = vars["chat"]
	if subject == "" {
		subject, _, _ = strings.Cut(chat, "@")
	}
	return "@" + chat, "", subject
}
