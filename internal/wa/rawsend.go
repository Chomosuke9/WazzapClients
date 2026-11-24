package wa

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	whatsmeow "github.com/polymorfa/hypermeow"
	"github.com/polymorfa/hypermeow/proto/waE2E"
	"github.com/polymorfa/hypermeow/types"
	"github.com/polymorfa/hypermeow/types/events"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

// Messages that commands the user added make themselves (internal/usercmd's
// sock): any waE2E message, as JSON.

// ChatID implements model.Backend.
func (b *Backend) ChatID(id string) string {
	jid, err := types.ParseJID(id)
	if err != nil || jid.User == "" {
		return ""
	}
	switch jid.Server {
	case types.DefaultUserServer, types.HiddenUserServer, types.GroupServer, types.NewsletterServer:
		return b.canonical(b.ctx, jid).String()
	}
	return ""
}

// SendPayload implements model.Backend. What shows in a chat is stored and
// shown before it goes, as any message is. What doesn't (a reaction, an
// edit, a protocol message) is sent, and then taken as it would be had the
// phone sent it.
func (b *Backend) SendPayload(chat, body, messageID string, reply *model.Message) (m *model.Message, shown bool, err error) {
	if b.Pref(model.PrefGhost) == "on" {
		return nil, false, errors.New("Turn off ghost mode to send messages.")
	}
	msg, err := decodePayload(body)
	if err != nil {
		return nil, false, err
	}
	jid, err := types.ParseJID(chat)
	if err != nil {
		return nil, false, errors.New("This isn't a chat: " + chat)
	}
	cli := b.client()
	if cli == nil {
		return nil, false, errors.New("Connect to WhatsApp first.")
	}
	if reply != nil {
		var ci *waE2E.ContextInfo
		msg, ci = snippetContext(msg)
		b.quote(chat, reply, ci)
	}
	mid, now := messageID, b.sendTime()
	if mid == "" {
		mid = cli.GenerateMessageID()
	}
	extra := whatsmeow.SendRequestExtra{ID: mid}
	// A group status needs its meta node and secret, and is kept with the
	// statuses (onStatus), never as an ordinary message, whatever its text.
	gsExtra, groupStatus := groupStatusRelay(msg, jid)
	if groupStatus {
		extra.AdditionalNodes = gsExtra.AdditionalNodes
	}
	e := &events.Message{Info: types.MessageInfo{MessageSource: types.MessageSource{Chat: jid, Sender: b.ownJID(chat),
		IsFromMe: true, IsGroup: jid.Server == types.GroupServer}, ID: mid, Timestamp: now}, Message: msg, RawMessage: msg}
	e.UnwrapRaw()
	p, ok := b.parse(b.ctx, e)
	if groupStatus || !ok || p.target != "" || p.msg.isCopy {
		if !cli.IsConnected() {
			return nil, false, errors.New("You're offline. Try again once connected.")
		}
		// A group status is stored with the statuses now, before the send, so
		// a { delete } right after it finds it in wz_status and revokes it as a
		// status. Otherwise the send's own onMessage would store it only after
		// the server acks, and a delete in between falls back to a plain
		// message revoke, which the server ignores for a status.
		stored := false
		if groupStatus {
			if st, ok := b.parseStatus(b.ctx, e); ok {
				if err := b.store.putStatus(b.ctx, b.db, st); err != nil {
					b.log.Warnf("store relayed group status %s: %v", mid, err)
				} else {
					stored = true
					b.emit(model.StatusEvent{})
				}
			}
		}
		go func() {
			if _, err := b.sendMessage(b.ctx, cli, jid, msg, extra); err != nil {
				b.log.Errorf("send payload to %s: %v", chat, err)
				b.emit(model.NoticeEvent{Text: "Couldn't send the message."})
				return
			}
			if !stored {
				b.onMessage(e)
			}
		}()
		return &model.Message{ID: mid, ChatID: chat, FromMe: true, Time: now, Receipt: model.Pending}, false, nil
	}
	p.msg.ChatID, p.msg.Receipt, p.msg.rawPayload = chat, model.Pending, marshal(msg)
	if reply != nil {
		p.msg.Quote = b.quote(chat, reply, &waE2E.ContextInfo{})
	}
	if err := b.store.ensureChat(b.ctx, b.db, chat, jid.Server == types.GroupServer, ""); err != nil {
		return nil, false, err
	}
	if err := b.store.putMessage(b.ctx, b.db, p.msg); err != nil {
		return nil, false, err
	}
	b.emitChat(chat)
	b.sendAsync(chat, jid, mid, msg)
	return p.msg.Message, true, nil
}

// decodePayload reads a message's JSON. Fields may be spelled as Baileys
// does (mentionedJid, fileSha256) or as hypermeow's protobufs do
// (mentionedJID, fileSHA256).
func decodePayload(body string) (*waE2E.Message, error) {
	if len(body) > model.MaxSnippetBytes {
		return nil, errors.New("The message is over 1 MB.")
	}
	d := json.NewDecoder(bytes.NewReader([]byte(body)))
	d.UseNumber()
	var v any
	if err := d.Decode(&v); err != nil {
		return nil, fmt.Errorf("Invalid message JSON: %w", err)
	}
	msg := new(waE2E.Message)
	spellFields(v, msg.ProtoReflect().Descriptor(), 0)
	data, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	if err := (protojson.UnmarshalOptions{RecursionLimit: 64}).Unmarshal(data, msg); err != nil {
		return nil, fmt.Errorf("Invalid message: %w", err)
	}
	if proto.Size(msg) == 0 {
		return nil, errors.New("The message is empty.")
	}
	return msg, nil
}

// spellFields renames the fields of a message's JSON, in place, to the
// protobuf's spelling, whatever their case and underscores.
func spellFields(v any, md protoreflect.MessageDescriptor, depth int) {
	m, ok := v.(map[string]any)
	if !ok || depth > 64 {
		return // the decoder says what's wrong
	}
	fold := func(s string) string { return strings.ToLower(strings.ReplaceAll(s, "_", "")) }
	fields := map[string]protoreflect.FieldDescriptor{}
	for i := range md.Fields().Len() {
		f := md.Fields().Get(i)
		fields[fold(f.JSONName())] = f
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	for _, k := range keys {
		f := fields[fold(k)]
		if f == nil {
			continue
		}
		val := m[k]
		if k != f.JSONName() {
			if _, taken := m[f.JSONName()]; !taken {
				delete(m, k)
				m[f.JSONName()] = val
			}
		}
		if f.Message() == nil || f.IsMap() {
			continue
		}
		if l, ok := val.([]any); ok {
			for _, x := range l {
				spellFields(x, f.Message(), depth+1)
			}
		} else {
			spellFields(val, f.Message(), depth+1)
		}
	}
}
