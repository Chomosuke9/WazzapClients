package wa

import (
	"context"
	"encoding/hex"
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
//
// WhatsApp's syncs don't say that a sticker has a sound: neither the
// favourite sticker action (which comes back here for a favourite of our
// own too) nor history's list of recent ones has a field for it. So
// putSticker keeps the sound it knows, and soundsFromChats finds that of a
// favourite made on the phone in the message it came in.

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

// completeSticker returns s, which is the same file as other, with what
// only other knows: that it's animated, and its sound, or the one whose
// link works longer.
func completeSticker(s, other *waE2E.StickerMessage) *waE2E.StickerMessage {
	out := proto.Clone(s).(*waE2E.StickerMessage)
	if out.IsAnimated == nil {
		out.IsAnimated = other.IsAnimated
	}
	a, o := out.GetAudioMessage(), other.GetAudioMessage()
	if o != nil && (a == nil || linkExpiry(o.GetDirectPath()) > linkExpiry(a.GetDirectPath())) {
		out.Audio = other.Audio
	}
	return out
}

// soundsFromChats gives the kept stickers (recent and favourite) without a
// sound that of the newest message with the same file and a sound: all of
// them, or the one whose file's SHA-256 is sha.
func (s *msgStore) soundsFromChats(ctx context.Context, sha []byte) error {
	silent := map[string]bool{}
	rows, err := s.db.QueryContext(ctx, `SELECT hash, blob FROM wz_stickers WHERE ? = '' OR hash = ?`,
		hex.EncodeToString(sha), hex.EncodeToString(sha))
	if err != nil {
		return err
	}
	for rows.Next() {
		var hash string
		var blob []byte
		var m waE2E.StickerMessage
		if rows.Scan(&hash, &blob) == nil && proto.Unmarshal(blob, &m) == nil && m.GetAudioMessage() == nil {
			silent[hash] = true
		}
	}
	rows.Close()
	if len(silent) == 0 {
		return rows.Err()
	}
	// Only payloads that name a sound's type (and the file) are decoded.
	if sha == nil {
		sha = []byte{}
	}
	rows, err = s.db.QueryContext(ctx, `SELECT raw_payload FROM wz_messages WHERE media = ? AND raw_payload IS NOT NULL
		AND instr(raw_payload, CAST('audio/' AS BLOB)) > 0 AND (length(?) = 0 OR instr(raw_payload, ?) > 0) ORDER BY ts DESC`,
		int(model.MediaSticker), sha, sha)
	if err != nil {
		return err
	}
	found := map[string]*waE2E.StickerMessage{}
	for rows.Next() {
		var raw []byte
		if rows.Scan(&raw) != nil {
			continue
		}
		c, m := describeRaw(raw)
		if m == nil || !c.sound {
			continue
		}
		st := unwrap(m).GetStickerMessage()
		if hash := hex.EncodeToString(st.GetFileSHA256()); silent[hash] && found[hash] == nil {
			st.ContextInfo = nil
			found[hash] = st
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for hash, st := range found {
		if err := s.putSticker(ctx, s.db, hash, st, 0, 0); err != nil {
			return err
		}
	}
	return nil
}

// soundStickers is soundsFromChats for every kept sticker, once: earlier
// versions lost their sounds.
func (s *msgStore) soundStickers(ctx context.Context) error {
	const done = "sticker_sounds"
	if s.meta(ctx, done) != "" {
		return nil
	}
	if err := s.soundsFromChats(ctx, nil); err != nil {
		return err
	}
	return s.setMetaValue(ctx, done, "1")
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
