package wa

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/polymorfa/hypermeow/appstate"
	"github.com/polymorfa/hypermeow/proto/waE2E"
	"github.com/polymorfa/hypermeow/proto/waHistorySync"
	"github.com/polymorfa/hypermeow/proto/waSyncAction"
	"github.com/polymorfa/hypermeow/types/events"
	waLog "github.com/polymorfa/hypermeow/util/log"
	"google.golang.org/protobuf/proto"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

func testBackend(t *testing.T) *Backend {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1) // one in-memory database
	t.Cleanup(func() { db.Close() })
	b := &Backend{ctx: context.Background(), db: db, store: msgStore{db: db}, log: waLog.Noop, dataDir: t.TempDir()}
	if err := b.store.init(b.ctx); err != nil {
		t.Fatal(err)
	}
	return b
}

func ids(ms []*model.Message) []string {
	var out []string
	for _, m := range ms {
		out = append(out, m.ID)
	}
	return out
}

func TestStickerSync(t *testing.T) {
	b := testBackend(t)
	sha := func(s string) []byte { h := sha256.Sum256([]byte(s)); return h[:] }
	meta := func(name string, ts int64) *waHistorySync.StickerMetadata {
		return &waHistorySync.StickerMetadata{FileSHA256: sha(name), DirectPath: proto.String("/v/" + name),
			LastStickerSentTS: proto.Int64(ts)}
	}
	// Milliseconds, as history sync sends them.
	b.onRecentStickers([]*waHistorySync.StickerMetadata{meta("a", 1700000000000), meta("b", 1700000100000), {}})
	want := []string{hex.EncodeToString(sha("b")), hex.EncodeToString(sha("a"))}
	if got := ids(b.Stickers(model.StickersRecent)); len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("recent = %v, want %v", got, want)
	}

	fav := func(name string, on bool) {
		b.onStickerAppState(&events.AppState{
			Index: []string{appstate.IndexFavoriteSticker, base64.StdEncoding.EncodeToString(sha(name))},
			SyncActionValue: &waSyncAction.SyncActionValue{Timestamp: proto.Int64(1700000200000),
				StickerAction: &waSyncAction.StickerAction{DirectPath: proto.String("/v/" + name), IsFavorite: proto.Bool(on)}},
		})
	}
	fav("a", true)
	fav("c", true)
	if got := b.Stickers(model.StickersFavorite); len(got) != 2 {
		t.Fatalf("favourites = %v", ids(got))
	}
	// The favourite's blob carries the plaintext hash, so it can be downloaded.
	_, blob, err := b.store.mediaBlob(b.ctx, stickerChat, hex.EncodeToString(sha("c")))
	if m := mediaMessage(model.MediaSticker, blob); err != nil || m == nil ||
		string(m.GetStickerMessage().GetFileSHA256()) != string(sha("c")) {
		t.Fatalf("favourite blob: %v %v", m, err)
	}

	fav("c", false)
	b.onStickerAppState(&events.AppState{
		Index:           []string{appstate.IndexRemoveRecentSticker, base64.StdEncoding.EncodeToString(sha("a"))},
		SyncActionValue: &waSyncAction.SyncActionValue{RemoveRecentStickerAction: &waSyncAction.RemoveRecentStickerAction{}},
	})
	if got := ids(b.Stickers(model.StickersRecent)); len(got) != 1 || got[0] != want[0] {
		t.Fatalf("recent after removal = %v", got)
	}
	if got := ids(b.Stickers(model.StickersFavorite)); len(got) != 1 || got[0] != hex.EncodeToString(sha("a")) {
		t.Fatalf("favourites after removal = %v", got)
	}
	var n int
	_ = b.db.QueryRow(`SELECT count(*) FROM wz_stickers`).Scan(&n)
	if n != 2 { // c has no mark left
		t.Fatalf("%d stickers stored, want 2", n)
	}

	// A SET without isFavorite is a favourite, and one whose index isn't a
	// readable hash is keyed by its encrypted file.
	enc := sha("d.enc")
	b.onStickerAppState(&events.AppState{
		Index: []string{appstate.IndexFavoriteSticker, "not-a-hash"},
		SyncActionValue: &waSyncAction.SyncActionValue{
			StickerAction: &waSyncAction.StickerAction{DirectPath: proto.String("/v/d"), FileEncSHA256: enc}},
	})
	got := ids(b.Stickers(model.StickersFavorite))
	if len(got) != 2 || got[0] != encStickerPrefix+hex.EncodeToString(enc) {
		t.Fatalf("favourites with an unhashed one = %v", got)
	}

	// Once downloaded, it's filed under its real plaintext hash.
	b.rehashSticker(got[0], []byte("d"))
	got = ids(b.Stickers(model.StickersFavorite))
	if len(got) != 2 || got[0] != hex.EncodeToString(sha("d")) {
		t.Fatalf("favourites after rehash = %v", got)
	}
	_, blob, _ = b.store.mediaBlob(b.ctx, stickerChat, got[0])
	if m := mediaMessage(model.MediaSticker, blob); string(m.GetStickerMessage().GetFileSHA256()) != string(sha("d")) {
		t.Fatalf("rehashed blob lacks its hash")
	}
}

func TestStickerFromChats(t *testing.T) {
	b := testBackend(t)
	ctx := b.ctx
	data := []byte("sticker file")
	sum := sha256.Sum256(data)
	hash := hex.EncodeToString(sum[:])
	path := func(oe string) *string { return proto.String("/v/x.enc?oh=1&oe=" + oe + "&_nc_sid=1") }

	// A favourite whose link died, and the same file received in a chat.
	if err := b.store.putSticker(ctx, b.db, hash, &waE2E.StickerMessage{FileSHA256: sum[:], DirectPath: path("6A000000")}, 0, 1); err != nil {
		t.Fatal(err)
	}
	fresh := &waE2E.StickerMessage{FileSHA256: sum[:], DirectPath: path("6B000000")}
	blob, _ := proto.Marshal(fresh)
	m := &model.Message{ID: "m1", ChatID: "c@s.whatsapp.net", Kind: model.KindSticker, Media: model.MediaSticker}
	if err := b.store.putMessage(ctx, b.db, storedMsg{Message: m, mediaBlob: blob}); err != nil {
		t.Fatal(err)
	}
	_ = os.MkdirAll(filepath.Dir(b.mediaPath(m.ChatID, m.ID)), 0o700)
	if err := os.WriteFile(b.mediaPath(m.ChatID, m.ID), data, 0o600); err != nil {
		t.Fatal(err)
	}

	if !b.stickerFromChats(ctx, nil, hash) {
		t.Fatal("no copy found")
	}
	if got := b.MediaData(stickerChat, hash); string(got) != string(data) {
		t.Fatalf("file = %q", got)
	}
	stored := func() string {
		raw, _ := b.store.stickerBlob(ctx, hash)
		var s waE2E.StickerMessage
		_ = proto.Unmarshal(raw, &s)
		return s.GetDirectPath()
	}
	if got := stored(); got != *fresh.DirectPath {
		t.Fatalf("link = %q, want the chat copy's", got)
	}
	// A sync repeating the dead link keeps the fresh one.
	if err := b.store.putSticker(ctx, b.db, hash, &waE2E.StickerMessage{FileSHA256: sum[:], DirectPath: path("6A000000")}, 0, 2); err != nil {
		t.Fatal(err)
	}
	if got := stored(); got != *fresh.DirectPath {
		t.Fatalf("link after sync = %q", got)
	}
}

func TestFavoriteSticker(t *testing.T) {
	b := testBackend(t)
	sum := sha256.Sum256([]byte("sticker"))
	blob, _ := proto.Marshal(&waE2E.StickerMessage{FileSHA256: sum[:]})
	m := &model.Message{ID: "m1", ChatID: "c@s.whatsapp.net", Kind: model.KindSticker, Media: model.MediaSticker}
	if err := b.store.putMessage(b.ctx, b.db, storedMsg{Message: m, mediaBlob: blob}); err != nil {
		t.Fatal(err)
	}
	if b.FavoriteSticker(m) {
		t.Fatal("favourite before it was added")
	}
	s, hash, ok := b.chatSticker(m)
	if !ok || hash != hex.EncodeToString(sum[:]) {
		t.Fatalf("chatSticker = %v, %q", ok, hash)
	}
	if err := b.store.putSticker(b.ctx, b.db, hash, s, 0, 1); err != nil {
		t.Fatal(err)
	}
	if !b.FavoriteSticker(m) {
		t.Fatal("not a favourite after it was added")
	}
}
