package wa

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"os"
	"path/filepath"
	"time"

	"github.com/polymorfa/hypermeow/appstate"
	"github.com/polymorfa/hypermeow/proto/waE2E"
	"github.com/polymorfa/hypermeow/proto/waHistorySync"
	"github.com/polymorfa/hypermeow/proto/waSyncAction"
	"github.com/polymorfa/hypermeow/types/events"
	"google.golang.org/protobuf/proto"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

// stickerChat is the pseudo-chat synced stickers belong to. The picker shows
// stickers as messages, so a synced sticker is a message in this chat with
// its file's SHA-256 (hex) as ID; MediaData and SendSticker then work as for
// any received sticker.
const stickerChat = "stickers"

// Synced stickers: the account's recent stickers (history sync, plus the
// ones sent from any device since) and its favourites (app state). The
// sticker's file is identified by its plaintext SHA-256, which is also what
// downloads verify against.
const stickerSchema = `
CREATE TABLE IF NOT EXISTS wz_stickers (
	hash      TEXT PRIMARY KEY,           -- hex SHA-256 of the file
	blob      BLOB NOT NULL,              -- marshaled waE2E.StickerMessage
	recent_ts INTEGER NOT NULL DEFAULT 0, -- last sent (unix seconds), 0 = not recent
	favorite  INTEGER NOT NULL DEFAULT 0  -- favourited at (unix seconds), 0 = not a favourite
);
`

// encStickerPrefix starts the key of a synced sticker whose plaintext hash
// is unknown; the rest is its encrypted file's SHA-256 (hex).
const encStickerPrefix = "enc-"

// stickerListMax is how many stickers a picker tab shows.
const stickerListMax = 60

// unixSeconds accepts a timestamp in seconds or milliseconds.
func unixSeconds(ts int64) int64 {
	if ts > 1e11 {
		return ts / 1000
	}
	return ts
}

// putSticker stores a sticker's media and marks it recent (recentTS > 0)
// and/or favourite (fav > 0). Zero leaves that mark as it was.
func (s *msgStore) putSticker(ctx context.Context, x execer, hash string, sticker *waE2E.StickerMessage, recentTS, fav int64) error {
	blob, err := proto.Marshal(sticker)
	if err != nil {
		return err
	}
	_, err = x.ExecContext(ctx, `INSERT INTO wz_stickers (hash, blob, recent_ts, favorite) VALUES (?, ?, ?, ?)
		ON CONFLICT (hash) DO UPDATE SET blob = excluded.blob,
			recent_ts = max(wz_stickers.recent_ts, excluded.recent_ts),
			favorite = CASE WHEN excluded.favorite > 0 THEN excluded.favorite ELSE wz_stickers.favorite END`,
		hash, blob, recentTS, fav)
	return err
}

// unmarkSticker clears one mark ("recent_ts" or "favorite") and forgets
// stickers left with neither.
func (s *msgStore) unmarkSticker(ctx context.Context, hash, column string) error {
	if _, err := s.db.ExecContext(ctx, `UPDATE wz_stickers SET `+column+` = 0 WHERE hash = ?`, hash); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM wz_stickers WHERE hash = ? AND recent_ts = 0 AND favorite = 0`, hash)
	return err
}

// stickerBlob returns a synced sticker's StickerMessage.
func (s *msgStore) stickerBlob(ctx context.Context, hash string) (blob []byte, err error) {
	err = s.db.QueryRowContext(ctx, `SELECT blob FROM wz_stickers WHERE hash = ?`, hash).Scan(&blob)
	return
}

// stickerHashByEnc finds the plaintext hash of a sticker by its encrypted
// file's hash, from the stickers received in chats.
func (s *msgStore) stickerHashByEnc(ctx context.Context, enc []byte) []byte {
	if len(enc) == 0 {
		return nil
	}
	rows, err := s.db.QueryContext(ctx, `SELECT media_blob FROM wz_messages WHERE media = ? AND media_blob IS NOT NULL
		UNION ALL SELECT blob FROM wz_stickers`, int(model.MediaSticker))
	if err != nil {
		return nil
	}
	defer rows.Close()
	for rows.Next() {
		var blob []byte
		var m waE2E.StickerMessage
		if rows.Scan(&blob) == nil && proto.Unmarshal(blob, &m) == nil &&
			string(m.GetFileEncSHA256()) == string(enc) && len(m.GetFileSHA256()) == 32 {
			return m.GetFileSHA256()
		}
	}
	return nil
}

// Stickers implements model.Backend.
func (b *Backend) Stickers(set model.StickerSet) []*model.Message {
	var order string
	switch set {
	case model.StickersRecent:
		order = "recent_ts"
	case model.StickersFavorite:
		order = "favorite"
	default:
		return b.receivedStickers()
	}
	rows, err := b.db.QueryContext(b.ctx, `SELECT hash FROM wz_stickers WHERE `+order+` > 0
		ORDER BY `+order+` DESC LIMIT ?`, stickerListMax)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []*model.Message
	for rows.Next() {
		var hash string
		if rows.Scan(&hash) == nil {
			out = append(out, &model.Message{ID: hash, ChatID: stickerChat, Kind: model.KindSticker, Media: model.MediaSticker})
		}
	}
	return out
}

// receivedStickers lists recently received stickers, one per file.
func (b *Backend) receivedStickers() []*model.Message {
	rows, err := b.db.QueryContext(b.ctx, `SELECT chat, id, media_blob FROM wz_messages
		WHERE media = ? AND from_me = 0 AND media_blob IS NOT NULL ORDER BY ts DESC LIMIT 400`, int(model.MediaSticker))
	if err != nil {
		return nil
	}
	defer rows.Close()
	seen := map[string]bool{}
	var out []*model.Message
	for rows.Next() && len(out) < stickerListMax {
		var chat, id string
		var blob []byte
		if rows.Scan(&chat, &id, &blob) != nil {
			continue
		}
		var s waE2E.StickerMessage
		if proto.Unmarshal(blob, &s) != nil || s.GetIsAnimated() {
			continue // animated stickers only show their first frame
		}
		key := string(s.GetFileSHA256())
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, &model.Message{ID: id, ChatID: chat, Kind: model.KindSticker, Media: model.MediaSticker})
	}
	return out
}

// recentSticker marks a sticker that was just sent (from here or another
// device) as recent. srcChat/srcID is the message whose file is on disk, if
// any; it saves downloading the same file again.
func (b *Backend) recentSticker(blob []byte, ts time.Time, srcChat, srcID string) {
	var m waE2E.StickerMessage
	if proto.Unmarshal(blob, &m) != nil || len(m.GetFileSHA256()) != 32 {
		return
	}
	m.ContextInfo = nil
	hash := hex.EncodeToString(m.GetFileSHA256())
	if err := b.store.putSticker(b.ctx, b.db, hash, &m, ts.Unix(), 0); err != nil {
		b.log.Warnf("store recent sticker: %v", err)
		return
	}
	if srcChat != "" && srcChat != stickerChat {
		dst := b.mediaPath(stickerChat, hash)
		if _, err := os.Stat(dst); err != nil {
			if data, err := os.ReadFile(b.mediaPath(srcChat, srcID)); err == nil {
				_ = os.MkdirAll(filepath.Dir(dst), 0o700)
				_ = os.WriteFile(dst, data, 0o600)
			}
		}
	}
	b.emit(model.StickersEvent{})
}

// onRecentStickers stores the recent stickers of the initial history sync.
func (b *Backend) onRecentStickers(list []*waHistorySync.StickerMetadata) {
	if len(list) == 0 {
		return
	}
	n := 0
	for _, s := range list {
		if len(s.GetFileSHA256()) != 32 || s.GetDirectPath() == "" {
			continue
		}
		ts := unixSeconds(s.GetLastStickerSentTS())
		if ts <= 0 {
			ts = 1 // still recent, just oldest
		}
		m := &waE2E.StickerMessage{
			URL: s.URL, FileSHA256: s.FileSHA256, FileEncSHA256: s.FileEncSHA256, MediaKey: s.MediaKey,
			Mimetype: s.Mimetype, Height: s.Height, Width: s.Width, DirectPath: s.DirectPath,
			FileLength: s.FileLength, IsLottie: s.IsLottie, IsAvatar: s.IsAvatarSticker,
		}
		if err := b.store.putSticker(b.ctx, b.db, hex.EncodeToString(s.GetFileSHA256()), m, ts, 0); err != nil {
			b.log.Warnf("store recent sticker: %v", err)
			continue
		}
		n++
	}
	b.log.Infof("history sync: %d recent stickers", n)
	b.emit(model.StickersEvent{})
}

// onStickerAppState handles the favoriteSticker and removeRecentSticker
// mutations. Both are indexed by the sticker's file hash.
func (b *Backend) onStickerAppState(e *events.AppState) {
	if len(e.Index) < 2 {
		return
	}
	switch e.Index[0] {
	case appstate.IndexFavoriteSticker:
		a := e.GetStickerAction()
		if a == nil {
			return
		}
		sha := decodeHash(e.Index[1])
		if string(sha) == string(a.GetFileEncSHA256()) {
			sha = nil // the index named the encrypted file
		}
		if sha == nil {
			sha = b.store.stickerHashByEnc(b.ctx, a.GetFileEncSHA256())
		}
		var hash string
		switch {
		case sha != nil:
			hash = hex.EncodeToString(sha)
		case len(a.GetFileEncSHA256()) == 32:
			// Without the plaintext hash the download is still checked by
			// its MAC (see download); key it by the encrypted file instead.
			b.log.Infof("favourite sticker %q: no plaintext hash, keyed by its encrypted file", e.Index[1])
			hash = encStickerPrefix + hex.EncodeToString(a.GetFileEncSHA256())
		default:
			b.log.Infof("favourite sticker %q: no file hash", e.Index[1])
			return
		}
		// A favourite is a SET mutation; unfavouriting may be a REMOVE (which
		// hypermeow doesn't emit) or a SET with isFavorite false. A SET that
		// leaves isFavorite out is a favourite.
		if a.IsFavorite != nil && !a.GetIsFavorite() {
			_ = b.store.unmarkSticker(b.ctx, hash, "favorite")
			b.emit(model.StickersEvent{})
			return
		}
		ts := unixSeconds(e.GetTimestamp())
		if ts <= 0 {
			ts = time.Now().Unix()
		}
		if err := b.store.putSticker(b.ctx, b.db, hash, stickerFromAction(a, sha), 0, ts); err != nil {
			b.log.Warnf("store favourite sticker: %v", err)
			return
		}
		b.emit(model.StickersEvent{})
	case appstate.IndexRemoveRecentSticker:
		if sha := decodeHash(e.Index[1]); sha != nil {
			_ = b.store.unmarkSticker(b.ctx, hex.EncodeToString(sha), "recent_ts")
			b.emit(model.StickersEvent{})
		}
	}
}

// rehashSticker files a downloaded synced sticker whose content doesn't
// match its key (an enc- placeholder, or an index that named another hash)
// under its real plaintext hash, merging it with any row already there, so
// it can be sent and deduplicated like any other.
func (b *Backend) rehashSticker(oldKey string, data []byte) {
	ctx := b.ctx
	var m waE2E.StickerMessage
	var recentTS, fav int64
	var blob []byte
	if b.db.QueryRowContext(ctx, `SELECT blob, recent_ts, favorite FROM wz_stickers WHERE hash = ?`, oldKey).
		Scan(&blob, &recentTS, &fav) != nil || proto.Unmarshal(blob, &m) != nil {
		return
	}
	sum := sha256.Sum256(data)
	m.FileSHA256 = sum[:]
	key := hex.EncodeToString(sum[:])
	path := b.mediaPath(stickerChat, key)
	_ = os.MkdirAll(filepath.Dir(path), 0o700)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		b.log.Warnf("save sticker: %v", err)
		return
	}
	if err := b.store.putSticker(ctx, b.db, key, &m, recentTS, fav); err != nil {
		b.log.Warnf("rehash sticker: %v", err)
		return
	}
	if key != oldKey {
		_, _ = b.db.ExecContext(ctx, `DELETE FROM wz_stickers WHERE hash = ?`, oldKey)
	}
	b.emit(model.StickersEvent{})
}

func stickerFromAction(a *waSyncAction.StickerAction, sha []byte) *waE2E.StickerMessage {
	return &waE2E.StickerMessage{
		URL: a.URL, FileSHA256: sha, FileEncSHA256: a.FileEncSHA256, MediaKey: a.MediaKey,
		Mimetype: a.Mimetype, Height: a.Height, Width: a.Width, DirectPath: a.DirectPath,
		FileLength: a.FileLength, IsLottie: a.IsLottie, IsAvatar: a.IsAvatarSticker,
	}
}

// decodeHash reads a 32-byte file hash written as base64 (what WhatsApp uses
// in app state indexes) or hex.
func decodeHash(s string) []byte {
	for _, enc := range []*base64.Encoding{base64.StdEncoding, base64.URLEncoding, base64.RawStdEncoding, base64.RawURLEncoding} {
		if b, err := enc.DecodeString(s); err == nil && len(b) == 32 {
			return b
		}
	}
	if b, err := hex.DecodeString(s); err == nil && len(b) == 32 {
		return b
	}
	return nil
}
