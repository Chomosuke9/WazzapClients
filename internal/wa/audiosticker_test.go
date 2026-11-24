package wa

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"os"
	"testing"

	"github.com/polymorfa/hypermeow/appstate"
	"github.com/polymorfa/hypermeow/proto/waE2E"
	"github.com/polymorfa/hypermeow/proto/waSyncAction"
	"github.com/polymorfa/hypermeow/types/events"
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

// A favourite keeps its sound when its own action comes back, which can't
// name one, and when it's reacted with; one made on the phone gets its
// message's.
func TestAudioStickerFavorite(t *testing.T) {
	sha := bytes.Repeat([]byte{4}, 32)
	hash := hex.EncodeToString(sha)
	action := func(b *Backend, sha []byte) {
		b.onStickerAppState(&events.AppState{
			Index: []string{appstate.IndexFavoriteSticker, base64.StdEncoding.EncodeToString(sha)},
			SyncActionValue: &waSyncAction.SyncActionValue{Timestamp: proto.Int64(1700000200000),
				StickerAction: &waSyncAction.StickerAction{DirectPath: proto.String("/v/sticker"), IsFavorite: proto.Bool(true)}},
		})
	}
	sent := func(b *Backend) *waE2E.StickerMessage {
		t.Helper()
		media, blob, err := b.store.mediaBlob(b.ctx, stickerChat, hash)
		if err != nil {
			t.Fatal(err)
		}
		return unwrap(wrapSound(mediaMessage(media, blob))).GetStickerMessage()
	}
	check := func(b *Backend, when string) {
		t.Helper()
		if s := sent(b); s.GetAudioMessage().GetDirectPath() != "/v/sound" || !s.GetIsAnimated() || s.GetDirectPath() != "/v/sticker" {
			t.Errorf("%s: %v", when, s)
		}
	}

	// Favourited here.
	b := testBackend(t)
	arrive(t, b, "S1", 1000, audioSticker())
	s, _, _ := b.chatSticker(b.Messages(copyChat, 10)[0])
	if err := b.store.putSticker(b.ctx, b.db, hash, s, 0, 1); err != nil {
		t.Fatal(err)
	}
	check(b, "favourited")
	action(b, sha)
	check(b, "its action back")
	silent := unwrap(audioSticker())
	dropSound(silent)
	b.recentSticker(marshal(silent.GetStickerMessage()), b.now(), "", "")
	check(b, "reacted with")

	// Favourited on the phone.
	b = testBackend(t)
	arrive(t, b, "S1", 1000, audioSticker())
	action(b, sha)
	check(b, "favourited on the phone")

	// Saved without its sound by an earlier version, beside one that has none.
	b = testBackend(t)
	arrive(t, b, "S1", 1000, audioSticker())
	other := bytes.Repeat([]byte{9}, 32)
	for _, sum := range [][]byte{sha, other} {
		if err := b.store.putSticker(b.ctx, b.db, hex.EncodeToString(sum),
			&waE2E.StickerMessage{FileSHA256: sum, DirectPath: proto.String("/v/sticker")}, 0, 1); err != nil {
			t.Fatal(err)
		}
	}
	if sent(b).GetAudioMessage() != nil {
		t.Fatal("it has its sound already")
	}
	_ = b.store.setMetaValue(b.ctx, "sticker_sounds", "")
	if err := b.store.soundStickers(b.ctx); err != nil {
		t.Fatal(err)
	}
	check(b, "repaired")
	media, blob, _ := b.store.mediaBlob(b.ctx, stickerChat, hex.EncodeToString(other))
	if hasSound(mediaMessage(media, blob)) {
		t.Error("a silent sticker got a sound")
	}
}
