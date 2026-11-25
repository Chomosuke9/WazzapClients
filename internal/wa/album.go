package wa

import (
	"context"
	"crypto/rand"
	"time"

	"github.com/polymorfa/hypermeow"
	"github.com/polymorfa/hypermeow/proto/waCommon"
	"github.com/polymorfa/hypermeow/proto/waE2E"
	"github.com/polymorfa/hypermeow/types"
	"google.golang.org/protobuf/proto"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

// An album is photos and videos sent together, which WhatsApp shows as
// one grid. It is sent as an albumMessage that says how many pictures to
// expect, followed by each picture as a message of its own whose
// messageContextInfo points back to it (a MEDIA_ALBUM association). The
// albumMessage itself shows nothing; parse files each picture under its
// album (model.Message.Album).

// NewAlbum implements model.Backend.
func (b *Backend) NewAlbum(chatID string, photos, videos int) string {
	jid, err := types.ParseJID(chatID)
	cli := b.client()
	if err != nil || cli == nil || photos+videos < 2 {
		return ""
	}
	id := cli.GenerateMessageID()
	msg := &waE2E.Message{
		AlbumMessage:       albumOfSize(photos, videos),
		MessageContextInfo: &waE2E.MessageContextInfo{MessageSecret: messageSecret()},
	}
	// The pictures upload meanwhile, and wait for it before they're sent:
	// a picture that arrives first would show on its own on some phones.
	sent := make(chan struct{})
	b.albums.Store(id, sent)
	go func() {
		// Once it's out, pictures needn't wait: forget it.
		defer b.albums.Delete(id)
		defer close(sent)
		if _, err := b.sendMessage(b.ctx, cli, jid, msg, whatsmeow.SendRequestExtra{ID: id}); err != nil {
			b.log.Errorf("send album to %s: %v", chatID, err)
		}
	}()
	return id
}

// albumOfSize is the albumMessage of photos and videos to expect.
func albumOfSize(photos, videos int) *waE2E.AlbumMessage {
	return &waE2E.AlbumMessage{
		ExpectedImageCount: proto.Uint32(uint32(photos)),
		ExpectedVideoCount: proto.Uint32(uint32(videos)),
	}
}

// waitAlbum waits until album's message has gone out (or failed to), for
// a picture of it about to be sent.
func (b *Backend) waitAlbum(ctx context.Context, album string) {
	v, ok := b.albums.Load(album)
	if !ok {
		return
	}
	select {
	case <-v.(chan struct{}):
	case <-ctx.Done():
	case <-time.After(time.Minute):
	}
}

// inAlbum points a picture's message to be sent at the album it belongs
// to.
func inAlbum(msg *waE2E.Message, chat types.JID, album string) {
	mci := msg.MessageContextInfo
	if mci == nil {
		mci = &waE2E.MessageContextInfo{}
		msg.MessageContextInfo = mci
	}
	if mci.MessageSecret == nil {
		mci.MessageSecret = messageSecret()
	}
	mci.MessageAssociation = &waE2E.MessageAssociation{
		AssociationType: waE2E.MessageAssociation_MEDIA_ALBUM.Enum(),
		ParentMessageKey: &waCommon.MessageKey{
			RemoteJID: proto.String(chat.String()),
			FromMe:    proto.Bool(true),
			ID:        proto.String(album),
		},
	}
}

// albumFirst returns the ID of the first of the messages of album in
// chat, looking through the chat's newest last photos and videos (all of
// them when last is 0), or "". Payloads only are known to hold the album's
// ID, which a message that replies to the album holds too.
func (s *msgStore) albumFirst(ctx context.Context, chat, album string, last int) string {
	if album == "" {
		return ""
	}
	if last <= 0 {
		last = -1 // no limit
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, raw_payload FROM (
			SELECT id, raw_payload, ts, rowid AS rid FROM wz_messages WHERE chat = ?1 AND kind = ?2 ORDER BY ts DESC, rowid DESC LIMIT ?3
		) WHERE instr(raw_payload, ?4) ORDER BY ts, rid`, chat, int(model.KindImage), last, []byte(album))
	if err != nil {
		return ""
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var raw []byte
		if rows.Scan(&id, &raw) != nil {
			return ""
		}
		var m waE2E.Message
		if proto.Unmarshal(raw, &m) == nil && albumOf(&m) == album {
			return id
		}
	}
	return ""
}

// AlbumMessage implements model.Backend.
func (b *Backend) AlbumMessage(chatID, albumID string) string {
	return b.store.albumFirst(b.ctx, chatID, albumID, 0)
}

// albumTarget returns the message that what a message does to message id
// (a reaction, a pin) shows on: id itself, or the first of an album's
// pictures when id is the album's, since the album is no message here.
func (b *Backend) albumTarget(ctx context.Context, chat, id string) string {
	if _, ok := b.store.message(ctx, chat, id); ok {
		return id
	}
	if first := b.store.albumFirst(ctx, chat, id, 500); first != "" {
		return first
	}
	return id
}

// messageSecret is a new random secret for a message's messageContextInfo,
// which the pictures of an album carry.
func messageSecret() []byte {
	s := make([]byte, 32)
	_, _ = rand.Read(s)
	return s
}
