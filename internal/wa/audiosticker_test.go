package wa

import (
	"bytes"
	"encoding/hex"
	"os"
	"testing"

	"github.com/polymorfa/hypermeow/proto/waE2E"
	"google.golang.org/protobuf/proto"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

// audioSticker is a sticker with a sound, as phones send it.
func audioSticker() *waE2E.Message {
	return &waE2E.Message{
		MessageContextInfo: &waE2E.MessageContextInfo{MessageSecret: bytes.Repeat([]byte{1}, 32)},
		AudioStickerMessage: &waE2E.FutureProofMessage{Message: &waE2E.Message{StickerMessage: &waE2E.StickerMessage{
			DirectPath: proto.String("/v/sticker"), MediaKey: bytes.Repeat([]byte{3}, 32), FileSHA256: bytes.Repeat([]byte{4}, 32),
			Mimetype: proto.String("image/webp"), IsAnimated: proto.Bool(true),
			ContextInfo: &waE2E.ContextInfo{IsForwarded: proto.Bool(true), ForwardingScore: proto.Uint32(2)},
			Audio: &waE2E.StickerMessage_AudioMessage{AudioMessage: &waE2E.AudioMessage{DirectPath: proto.String("/v/sound"),
				MediaKey: bytes.Repeat([]byte{5}, 32), Mimetype: proto.String("audio/ogg; codecs=opus"), Seconds: proto.Uint32(5)}},
		}}},
	}
}

// A sticker with a sound is a sticker, which says it has one.
func TestAudioSticker(t *testing.T) {
	b := testBackend(t)
	arrive(t, b, "S1", 1000, audioSticker())
	ms := b.Messages(copyChat, 10)
	if len(ms) != 1 || ms[0].Kind != model.KindSticker || ms[0].Media != model.MediaSticker || !ms[0].Sound || !ms[0].Forwarded {
		t.Fatalf("messages = %+v", ms)
	}
	// What's sent or quoted from it is in the envelope again, sound and
	// all, and what's about the message stays outside.
	if q := b.quotedMessage(b.ctx, copyChat, "S1"); q.GetAudioStickerMessage().GetMessage().GetStickerMessage().GetAudioMessage().GetDirectPath() != "/v/sound" {
		t.Errorf("quoted as %v", q)
	}
	in := unwrap(audioSticker())
	in.MessageContextInfo = &waE2E.MessageContextInfo{MessageSecret: []byte{1}}
	if w := wrapSound(in); w.GetMessageContextInfo() == nil || unwrap(w).GetMessageContextInfo() != nil || !hasSound(w) || wrapSound(w) != w {
		t.Errorf("wrapped as %v", w)
	}
	silent := unwrap(audioSticker())
	dropSound(silent)
	if hasSound(silent) || wrapSound(silent) != silent {
		t.Error("a sticker without a sound is wrapped")
	}
	// A reaction with it makes no sound.
	if e, _, ok := b.stickerMessageOf(ms[0]); !ok || e.GetAudioMessage() != nil || e.GetDirectPath() != "/v/sticker" {
		t.Errorf("as a reaction: %v %v", e, ok)
	}
	// Its sound's file, once here, is the path.
	keepSound([]byte("OggS"), b.soundPath(copyChat, "S1"))
	if p := b.StickerSound(ms[0]); p == "" {
		t.Error("the sound isn't found")
	} else if data, _ := os.ReadFile(p); string(data) != "OggS" {
		t.Errorf("sound file %q", data)
	}
}

// One stored before the app knew them becomes a sticker, once.
func TestAudioStickerRescan(t *testing.T) {
	b := testBackend(t)
	m := &model.Message{ID: "S1", ChatID: copyChat, Kind: model.KindUnsupported}
	if err := b.store.putMessage(b.ctx, b.db, storedMsg{Message: m, rawPayload: marshal(audioSticker())}); err != nil {
		t.Fatal(err)
	}
	if err := b.store.setMetaValue(b.ctx, "unsupported_rescan", "2"); err != nil {
		t.Fatal(err)
	}
	if err := b.store.rescanUnsupported(b.ctx); err != nil {
		t.Fatal(err)
	}
	if r, ok := b.store.message(b.ctx, copyChat, "S1"); !ok || r.Kind != model.KindSticker || !r.Sound {
		t.Fatalf("rescanned = %+v", r.Message)
	}
}

// The picker's stickers keep their sounds.
func TestAudioStickerRecent(t *testing.T) {
	b := testBackend(t)
	_, blob := rawMedia(marshal(audioSticker()))
	b.recentSticker(blob, b.now(), "", "")
	hash := hex.EncodeToString(bytes.Repeat([]byte{4}, 32))
	media, blob, err := b.store.mediaBlob(b.ctx, stickerChat, hash)
	if err != nil || !hasSound(wrapSound(mediaMessage(media, blob))) {
		t.Errorf("recent sticker: %v, %v", mediaMessage(media, blob), err)
	}
}
