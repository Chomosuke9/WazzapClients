package wa

import (
	"bytes"
	"context"
	"sync"
	"time"

	"github.com/polymorfa/hypermeow"
	waBinary "github.com/polymorfa/hypermeow/binary"
	"github.com/polymorfa/hypermeow/proto/waE2E"
	"github.com/polymorfa/hypermeow/proto/waMmsRetry"
	"github.com/polymorfa/hypermeow/types"
	"github.com/polymorfa/hypermeow/types/events"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

// Media links expire: WhatsApp's CDN signs each direct path until its "oe"
// time and answers 403 after it (404 or 410 once the file is gone). Only
// your phone can bring such media back: a "server-error" receipt (a media
// retry request) asks it to upload a message's media again, and it answers
// with a MediaRetry notification holding the new direct path.
//
// Stickers need this most. A favourite's link is the one it had when it
// was favourited, months ago maybe, and synced stickers have no message of
// their own to ask about: they ask about a chat message with the same
// file (stickerCopies), yours first, since your phone keeps what it sent.

// reuploadWait is how long a media retry request may go unanswered before
// the next copy is tried.
const reuploadWait = 45 * time.Second

// reuploadTries is how many chat messages a sticker asks about at most.
// In November 2026 every sticker the phone uploaded again came encrypted
// under a key of its own that the answer doesn't carry (its
// messageSecret was empty), and neither the message's nor any other
// copy's key opened it: one try, so the phone isn't kept uploading
// files nobody can read. The download then fails its hash check, which
// marks the sticker gone (download).
const reuploadTries = 1

// reupload is a media retry request waiting for the phone's answer.
type reupload struct {
	chat, id string // the chat message asked about
	key      []byte // its media key, which the answer is encrypted with
	enc      []byte // its file's encrypted hash, to find the media in its payload
	sticker  string // the synced sticker (its key in wz_stickers) it is for, or ""
	at       time.Time
}

// reuploads are the media retry requests of a backend.
type reuploads struct {
	mu      sync.Mutex
	pending map[string]*reupload // by message ID
	tries   map[string]int       // requests sent per sticker or chat message
}

func (r *reuploads) init() {
	if r.pending == nil {
		r.pending, r.tries = map[string]*reupload{}, map[string]int{}
	}
}

// waiting reports whether a request for key (a sticker's, or a chat
// message's "chat/id") is still waiting for its answer.
func (r *reuploads) waiting(key string, now time.Time) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.init()
	for _, p := range r.pending {
		if (p.sticker == key || p.chat+"/"+p.id == key) && now.Sub(p.at) < reuploadWait {
			return true
		}
	}
	return false
}

// stickerCopy is a chat message with a synced sticker's file.
type stickerCopy struct {
	chat, id, sender string
	fromMe           bool
	m                *waE2E.StickerMessage
}

// stickerCopies lists the chat messages with the sticker file whose
// plaintext hash is sha: yours first (your phone has them), then the
// newest first.
func (b *Backend) stickerCopies(ctx context.Context, sha []byte) []stickerCopy {
	rows, err := b.db.QueryContext(ctx, `SELECT chat, id, sender_jid, from_me, raw_payload FROM wz_messages
		WHERE media = ? AND raw_payload IS NOT NULL ORDER BY from_me DESC, ts DESC`, int(model.MediaSticker))
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []stickerCopy
	for rows.Next() {
		var c stickerCopy
		var raw []byte
		if rows.Scan(&c.chat, &c.id, &c.sender, &c.fromMe, &raw) != nil {
			continue
		}
		m := &waE2E.StickerMessage{}
		if _, blob := rawMedia(raw); len(blob) > 0 && proto.Unmarshal(blob, m) == nil &&
			bytes.Equal(m.GetFileSHA256(), sha) && len(m.GetMediaKey()) > 0 {
			c.m = m
			out = append(out, c)
		}
	}
	return out
}

// askStickerReupload asks the phone to upload a synced sticker again,
// through one of its chat copies it hasn't asked about yet. It reports
// whether a request is on its way (or still waiting); the answer
// downloads the sticker again (onMediaRetry).
func (b *Backend) askStickerReupload(ctx context.Context, cli *whatsmeow.Client, hash string, sha []byte) bool {
	r := &b.rmr
	if r.waiting(hash, b.now()) {
		return true
	}
	r.mu.Lock()
	n := r.tries[hash]
	r.mu.Unlock()
	copies := b.stickerCopies(ctx, sha)
	if n >= len(copies) || n >= reuploadTries {
		return false
	}
	c := copies[n]
	return b.askReupload(ctx, cli, c.chat, c.id, c.sender, c.fromMe, c.m, hash)
}

// askChatReupload asks the phone to upload a chat message's sticker
// again, once.
func (b *Backend) askChatReupload(ctx context.Context, cli *whatsmeow.Client, chat, id string, m *waE2E.StickerMessage) bool {
	key := chat + "/" + id
	r := &b.rmr
	if r.waiting(key, b.now()) {
		return true
	}
	r.mu.Lock()
	r.init()
	n := r.tries[key]
	r.mu.Unlock()
	if n > 0 || len(m.GetMediaKey()) == 0 {
		return false
	}
	var sender string
	var fromMe bool
	if b.db.QueryRowContext(ctx, `SELECT sender_jid, from_me FROM wz_messages WHERE chat = ? AND id = ?`, chat, id).
		Scan(&sender, &fromMe) != nil {
		return false
	}
	return b.askReupload(ctx, cli, chat, id, sender, fromMe, m, "")
}

// askReupload sends a media retry request for one chat message.
func (b *Backend) askReupload(ctx context.Context, cli *whatsmeow.Client, chat, id, sender string, fromMe bool, m *waE2E.StickerMessage, sticker string) bool {
	chatJID, err := types.ParseJID(chat)
	if err != nil {
		return false
	}
	info := &types.MessageInfo{
		MessageSource: types.MessageSource{Chat: chatJID, IsFromMe: fromMe, IsGroup: chatJID.Server == types.GroupServer},
		ID:            id,
	}
	if info.IsGroup {
		if info.Sender, err = types.ParseJID(sender); err != nil && !fromMe {
			return false
		}
		if fromMe {
			info.Sender = b.ownJID(chat)
		}
	}
	key := sticker
	if key == "" {
		key = chat + "/" + id
	}
	r := &b.rmr
	r.mu.Lock()
	r.init()
	r.tries[key]++
	r.pending[id] = &reupload{chat: chat, id: id, key: m.GetMediaKey(), enc: m.GetFileEncSHA256(), sticker: sticker, at: b.now()}
	r.mu.Unlock()
	if err := cli.SendMediaRetryReceipt(ctx, info, m.GetMediaKey()); err != nil {
		b.log.Infof("media retry %s in %s: %v", id, chat, err)
		r.mu.Lock()
		delete(r.pending, id)
		r.mu.Unlock()
		return false
	}
	b.log.Infof("media retry %s in %s: asked the phone (sticker %s)", id, chat, sticker)
	return true
}

// onMediaRetry handles the phone's answer to a media retry request: the
// message's media under a new direct path, which its payload and the
// synced sticker it was for take on before they download again.
func (b *Backend) onMediaRetry(e *events.MediaRetry) {
	r := &b.rmr
	r.mu.Lock()
	r.init()
	p := r.pending[e.MessageID]
	delete(r.pending, e.MessageID)
	r.mu.Unlock()
	if p == nil {
		return
	}
	retry := func() {
		if p.sticker != "" {
			b.downloads.add(stickerChat+"/"+p.sticker, func() { b.download(stickerChat, p.sticker) })
		} else {
			b.downloads.add(p.chat+"/"+p.id, func() { b.download(p.chat, p.id) })
		}
	}
	n, err := whatsmeow.DecryptMediaRetryNotification(e, p.key)
	if err != nil || n.GetResult() != waMmsRetry.MediaRetryNotification_SUCCESS || n.GetDirectPath() == "" {
		b.log.Infof("media retry %s in %s: %v (result %v)", p.id, p.chat, err, n.GetResult())
		retry() // the next copy, or the failure marker
		return
	}
	path := n.GetDirectPath()
	// What the answer holds besides the path, fields hypermeow doesn't
	// know included: the reuploaded file's key isn't found yet.
	b.log.Infof("media retry %s in %s: new path %s, message secret %x, unknown fields %x",
		p.id, p.chat, path, n.GetMessageSecret(), n.ProtoReflect().GetUnknown())
	ctx := b.ctx
	var raw []byte
	if b.db.QueryRowContext(ctx, `SELECT raw_payload FROM wz_messages WHERE chat = ? AND id = ?`, p.chat, p.id).Scan(&raw) == nil {
		var pm waE2E.Message
		if proto.Unmarshal(raw, &pm) == nil && repath(pm.ProtoReflect(), p.enc, path) {
			if err := b.store.setRawPayload(ctx, p.chat, p.id, &pm); err != nil {
				b.log.Warnf("media retry: store %s: %v", p.id, err)
			}
		}
	}
	if p.sticker != "" {
		if blob, err := b.store.stickerBlob(ctx, p.sticker); err == nil {
			var s waE2E.StickerMessage
			if proto.Unmarshal(blob, &s) == nil && bytes.Equal(s.GetFileEncSHA256(), p.enc) {
				s.DirectPath, s.URL = proto.String(path), nil
				if blob, err := proto.Marshal(&s); err == nil {
					_, _ = b.db.ExecContext(ctx, `UPDATE wz_stickers SET blob = ? WHERE hash = ?`, blob, p.sticker)
				}
			}
		}
	}
	retry()
	// The chat's copy shows again too.
	b.emit(model.MediaEvent{ChatID: p.chat, MsgID: p.id})
}

// repath gives the media in m whose encrypted file hash is enc a new
// direct path, wherever it is nested, and drops its old URL. It reports
// whether it found it.
func repath(m protoreflect.Message, enc []byte, path string) bool {
	found := false
	fds := m.Descriptor().Fields()
	dp, eh := fds.ByName("directPath"), fds.ByName("fileEncSHA256")
	if dp != nil && eh != nil && dp.Kind() == protoreflect.StringKind && eh.Kind() == protoreflect.BytesKind &&
		len(enc) > 0 && bytes.Equal(m.Get(eh).Bytes(), enc) {
		m.Set(dp, protoreflect.ValueOfString(path))
		if u := fds.ByName("URL"); u != nil {
			m.Clear(u)
		}
		found = true
	}
	m.Range(func(fd protoreflect.FieldDescriptor, v protoreflect.Value) bool {
		if fd.Kind() == protoreflect.MessageKind && !fd.IsList() && !fd.IsMap() && repath(v.Message(), enc, path) {
			found = true
		}
		return true
	})
	return found
}

// rawNode is the client's RawNodeHandler: it logs media retry answers as
// they came (a key for the reuploaded file may sit outside the part
// hypermeow parses) and passes grouped receipts to groupedReceipts.
func (b *Backend) rawNode(ctx context.Context, node *waBinary.Node) (*waBinary.Node, bool) {
	if node.Tag == "notification" && node.AttrGetter().OptionalString("type") == "mediaretry" {
		b.log.Infof("media retry stanza: %s", node.String())
	}
	return b.groupedReceipts(ctx, node)
}
