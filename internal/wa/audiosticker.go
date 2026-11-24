package wa

import (
	"context"
	"os"
	"path/filepath"
	"time"

	"github.com/polymorfa/hypermeow/proto/waE2E"
	"google.golang.org/protobuf/proto"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

// A sticker with a sound (Message.Sound) is an ordinary stickerMessage
// whose audio is an audioMessage, with a file, key and link of its own
// ("WhatsApp Audio Keys"), in an audioStickerMessage envelope. What a
// message's blob holds is the stickerMessage, sound and all, so the
// recent and favourite stickers keep their sounds; what's sent from one is
// put back in the envelope (wrapSound).

// wrapSound puts a sticker with a sound in the envelope it goes in, and
// returns any other message as it is.
func wrapSound(m *waE2E.Message) *waE2E.Message {
	if m.GetStickerMessage().GetAudioMessage() == nil {
		return m
	}
	// What's about the message, not the sticker, stays outside.
	mc := m.MessageContextInfo
	m.MessageContextInfo = nil
	return &waE2E.Message{MessageContextInfo: mc, AudioStickerMessage: &waE2E.FutureProofMessage{Message: m}}
}

// hasSound reports whether m is a sticker with a sound.
func hasSound(m *waE2E.Message) bool {
	return unwrap(m).GetStickerMessage().GetAudioMessage() != nil
}

// dropSound takes the sound off m, if it's a sticker with one.
func dropSound(m *waE2E.Message) {
	if s := m.GetStickerMessage(); s != nil {
		s.Audio = nil
	}
}

// soundMessage is the audio message of a sound that is still to upload.
func soundMessage(s *model.StickerSound) *waE2E.AudioMessage {
	return &waE2E.AudioMessage{
		Mimetype:   proto.String("audio/ogg; codecs=opus"),
		Seconds:    proto.Uint32(uint32(s.Seconds)),
		FileLength: proto.Uint64(uint64(len(s.Ogg))),
		Waveform:   s.Waveform,
	}
}

// soundPath is where the sound of a sticker is kept, beside its picture.
func (b *Backend) soundPath(chatID, msgID string) string {
	return b.mediaPath(chatID, msgID) + ".sound.ogg"
}

// keepSound writes a sound to path, so it plays without a download.
func keepSound(ogg []byte, path string) {
	_ = os.MkdirAll(filepath.Dir(path), 0o700)
	_ = os.WriteFile(path, ogg, 0o600)
}

// StickerSound implements model.Backend.
func (b *Backend) StickerSound(m *model.Message) string {
	path := b.soundPath(m.ChatID, m.ID)
	if _, err := os.Stat(path); err == nil {
		return path
	}
	if _, busy := b.playing.LoadOrStore(path, true); !busy {
		go func() {
			defer b.playing.Delete(path)
			ok := b.downloadSound(m, path)
			b.emit(model.MediaEvent{ChatID: m.ChatID, MsgID: m.ID, Failed: !ok})
		}()
	}
	return ""
}

func (b *Backend) downloadSound(m *model.Message, path string) bool {
	ctx, cancel := context.WithTimeout(b.ctx, 2*time.Minute)
	defer cancel()
	s, _, ok := b.chatSticker(m)
	if !ok || s.GetAudioMessage() == nil {
		b.emit(model.NoticeEvent{Text: "This sticker's sound isn't available."})
		return false
	}
	return b.downloadTo(ctx, s.GetAudioMessage(), m.ID, path)
}
