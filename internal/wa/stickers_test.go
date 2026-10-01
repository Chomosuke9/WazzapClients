package wa

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"testing"

	"github.com/polymorfa/hypermeow/appstate"
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
