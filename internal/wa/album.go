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
		AlbumMessage: &waE2E.AlbumMessage{
			ExpectedImageCount: proto.Uint32(uint32(photos)),
			ExpectedVideoCount: proto.Uint32(uint32(videos)),
		},
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

// messageSecret is a new random secret for a message's messageContextInfo,
// which the pictures of an album carry.
func messageSecret() []byte {
	s := make([]byte, 32)
	_, _ = rand.Read(s)
	return s
}
