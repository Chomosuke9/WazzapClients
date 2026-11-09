package wa

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/polymorfa/hypermeow/proto/waE2E"
	"google.golang.org/protobuf/proto"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

func TestStickerPackLink(t *testing.T) {
	for text, want := range map[string]string{
		"https://wa.me/stickerpack/Flump":                   "Flump",
		"look https://wa.me/stickerpack/Chomp nice":         "Chomp",
		"HTTPS://WA.ME/stickerpack/9-5WFH":                  "9-5WFH",
		"https://wa.me/stickerpack/meta-avatar":             "",
		"https://wa.me/6281234":                             "",
		"no link":                                           "",
		"http://wa.me/stickerpack/Pom%20Pom and more words": "Pom Pom",
	} {
		if got := stickerPackLink(text); got != want {
			t.Errorf("stickerPackLink(%q) = %q, want %q", text, got, want)
		}
	}
}

// A sticker pack message shows as a pack, a link to one gets its button,
// and those stored as unsupported before turn into packs.
func TestStickerPackMessages(t *testing.T) {
	spm := &waE2E.StickerPackMessage{
		StickerPackID: proto.String("com.example ecab"), Name: proto.String("twts"), Publisher: proto.String("me"),
		Stickers: []*waE2E.StickerPackMessage_Sticker{
			{FileName: proto.String("00.webp")}, {FileName: proto.String("01.webp"), IsAnimated: proto.Bool(true)},
		},
		DirectPath: proto.String("/v/pack"), MediaKey: []byte("k"),
		StickerPackOrigin: waE2E.StickerPackMessage_THIRD_PARTY.Enum(),
	}
	c := describe(&waE2E.Message{StickerPackMessage: spm})
	if c.media != model.MediaStickerPack || c.text != "twts" || c.extra.Pack == nil {
		t.Fatalf("describe = %v %q %v", c.media, c.text, c.extra.Pack)
	}
	if p := c.extra.Pack; p.ID != "com.example ecab" || p.Count != 2 || !p.Animated || p.FirstParty {
		t.Fatalf("pack = %+v", p)
	}
	link := describe(&waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{
		Text: proto.String("https://wa.me/stickerpack/Flump"), MatchedText: proto.String("https://wa.me/stickerpack/Flump"),
		Title: proto.String("Flump Sticker Pack on WhatsApp")}})
	if p := link.extra.Pack; p == nil || p.ID != "Flump" || !p.FirstParty {
		t.Fatalf("link pack = %+v", p)
	}

	b := testBackend(t)
	m := &model.Message{ID: "p1", ChatID: "g@g.us", Kind: model.KindUnsupported}
	if err := b.store.putMessage(b.ctx, b.db, storedMsg{Message: m, rawPayload: marshal(&waE2E.Message{StickerPackMessage: spm})}); err != nil {
		t.Fatal(err)
	}
	if err := b.store.setMetaValue(b.ctx, "unsupported_rescan", ""); err != nil {
		t.Fatal(err)
	}
	if err := b.store.rescanUnsupported(b.ctx); err != nil {
		t.Fatal(err)
	}
	r, ok := b.store.message(b.ctx, "g@g.us", "p1")
	if !ok || r.Media != model.MediaStickerPack || r.Pack == nil || r.Pack.Name != "twts" {
		t.Fatalf("rescanned = %v %+v", r.Media, r.Pack)
	}
	if media, blob, err := b.store.mediaBlob(b.ctx, "g@g.us", "p1"); err != nil || media != model.MediaStickerPack || len(blob) == 0 {
		t.Fatalf("mediaBlob = %v %d %v", media, len(blob), err)
	}
}

// A stored pack lists its stickers, which read as synced stickers, and
// adding it puts it in the picker.
func TestStickerPackStore(t *testing.T) {
	b := testBackend(t)
	var stickers []packSticker
	for i := range 3 {
		sum := sha256.Sum256(fmt.Appendf(nil, "s%d", i))
		stickers = append(stickers, packSticker{hash: hex.EncodeToString(sum[:]),
			m: &waE2E.StickerMessage{FileSHA256: sum[:], Mimetype: proto.String("image/webp")}})
	}
	p := &model.StickerPack{ID: "pk", Name: "Pack", Count: 3}
	if err := b.store.putPack(b.ctx, p, nil, []byte("tray"), stickers, b.now()); err != nil {
		t.Fatal(err)
	}
	got := b.PackStickers("pk")
	if len(got) != 3 || got[0].ID != stickers[0].hash || got[0].ChatID != stickerChat {
		t.Fatalf("PackStickers = %v", ids(got))
	}
	if string(b.MediaData(packChat, "pk")) != "tray" {
		t.Fatal("no tray icon")
	}
	// Not uploaded yet: it came in a zip.
	if b.unuploaded(stickers[1].hash) == nil {
		t.Fatal("a zip sticker reads as uploaded")
	}
	if len(b.StickerPacks()) != 0 {
		t.Fatal("listed before it was added")
	}
	b.AddStickerPack("pk", true)
	if ps := b.StickerPacks(); len(ps) != 1 || ps[0].Name != "Pack" || !ps[0].Added || ps[0].Count != 3 {
		t.Fatalf("StickerPacks = %+v", ps)
	}
	// Loading it again keeps it added.
	if err := b.store.putPack(b.ctx, p, nil, nil, stickers[:2], b.now()); err != nil {
		t.Fatal(err)
	}
	if ps := b.StickerPacks(); len(ps) != 1 || len(b.PackStickers("pk")) != 2 || string(b.MediaData(packChat, "pk")) != "tray" {
		t.Fatalf("after reloading: %+v, %d stickers", ps, len(b.PackStickers("pk")))
	}
	b.AddStickerPack("pk", false)
	if len(b.StickerPacks()) != 0 {
		t.Fatal("still listed after it was taken off")
	}
}

// Every favourite is listed, not just the newest stickerListMax.
func TestAllFavorites(t *testing.T) {
	b := testBackend(t)
	n := stickerListMax + 20
	for i := range n {
		sum := sha256.Sum256(fmt.Appendf(nil, "fav%d", i))
		if err := b.store.putSticker(b.ctx, b.db, hex.EncodeToString(sum[:]), &waE2E.StickerMessage{FileSHA256: sum[:]}, int64(i+1), int64(i+1)); err != nil {
			t.Fatal(err)
		}
	}
	if got := len(b.Stickers(model.StickersFavorite)); got != n {
		t.Errorf("%d favourites listed, want %d", got, n)
	}
	if got := len(b.Stickers(model.StickersRecent)); got != stickerListMax {
		t.Errorf("%d recents listed, want %d", got, stickerListMax)
	}
}

// A synced sticker that failed for good downloads again once it has a new
// link, and not before.
func TestStickerLinkChanged(t *testing.T) {
	b := testBackend(t)
	sum := sha256.Sum256([]byte("gone"))
	hash := hex.EncodeToString(sum[:])
	put := func(path string) {
		t.Helper()
		// Stored directly: putSticker keeps the link that expires last.
		if _, err := b.db.ExecContext(b.ctx, `INSERT INTO wz_stickers (hash, blob, favorite) VALUES (?, ?, 1)
			ON CONFLICT (hash) DO UPDATE SET blob = excluded.blob`,
			hash, marshal(&waE2E.StickerMessage{FileSHA256: sum[:], DirectPath: proto.String(path)})); err != nil {
			t.Fatal(err)
		}
	}
	put("/v/old")
	mark := b.failedPath(stickerChat, hash)
	if err := os.MkdirAll(filepath.Dir(mark), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mark, []byte("/v/old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if b.stickerLinkChanged(hash) {
		t.Fatal("the dead link counts as new")
	}
	put("/v/new")
	if !b.stickerLinkChanged(hash) {
		t.Fatal("a new link doesn't count")
	}
	// Markers from before they named the link: tried once more.
	if err := os.WriteFile(mark, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if !b.stickerLinkChanged(hash) {
		t.Fatal("an old marker blocks the sticker for good")
	}
}

// A media retry's new direct path lands on the media it was for, however
// deep it is nested.
func TestRepath(t *testing.T) {
	enc := []byte("enc-hash")
	m := &waE2E.Message{EphemeralMessage: &waE2E.FutureProofMessage{Message: &waE2E.Message{
		StickerMessage: &waE2E.StickerMessage{FileEncSHA256: enc, DirectPath: proto.String("/v/old"), URL: proto.String("https://x/old")},
	}}}
	if !repath(m.ProtoReflect(), enc, "/v/new") {
		t.Fatal("not found")
	}
	s := m.GetEphemeralMessage().GetMessage().GetStickerMessage()
	if s.GetDirectPath() != "/v/new" || s.URL != nil {
		t.Fatalf("sticker = %v", s)
	}
	if repath(m.ProtoReflect(), []byte("other"), "/v/x") {
		t.Fatal("repathed another file")
	}
}

// Packs list in the order they were added until they're moved, and one
// added after a move goes last.
func TestStickerPackOrder(t *testing.T) {
	b := testBackend(t)
	for _, id := range []string{"a", "b", "c", "d"} {
		if err := b.store.putPack(b.ctx, &model.StickerPack{ID: id, Name: id}, nil, nil, nil, b.now()); err != nil {
			t.Fatal(err)
		}
	}
	order := func() string {
		var s string
		for _, p := range b.StickerPacks() {
			s += p.ID
		}
		return s
	}
	b.AddStickerPack("a", true)
	b.AddStickerPack("b", true)
	b.AddStickerPack("c", true)
	if got := order(); got != "abc" {
		t.Fatalf("added: %q", got)
	}
	b.SetStickerPackOrder([]string{"c", "a", "b"})
	if got := order(); got != "cab" {
		t.Fatalf("moved: %q", got)
	}
	b.AddStickerPack("d", true)
	if got := order(); got != "cabd" {
		t.Fatalf("added after a move: %q", got)
	}
	b.AddStickerPack("a", false)
	b.AddStickerPack("a", true)
	if got := order(); got != "cbda" {
		t.Fatalf("taken off and added again: %q", got)
	}
}
