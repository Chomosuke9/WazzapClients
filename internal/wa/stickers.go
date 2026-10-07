package wa

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/polymorfa/hypermeow"
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
// A stored media message whose link expires later than the new one's is
// kept: syncs repeat old links, while stickerFromChats stores fresh ones.
func (s *msgStore) putSticker(ctx context.Context, x execer, hash string, sticker *waE2E.StickerMessage, recentTS, fav int64) error {
	blob, err := proto.Marshal(sticker)
	if err != nil {
		return err
	}
	if old, err := s.stickerBlob(ctx, hash); err == nil {
		var m waE2E.StickerMessage
		if proto.Unmarshal(old, &m) == nil && linkExpiry(m.GetDirectPath()) > linkExpiry(sticker.GetDirectPath()) {
			blob = old
		}
	}
	_, err = x.ExecContext(ctx, `INSERT INTO wz_stickers (hash, blob, recent_ts, favorite) VALUES (?, ?, ?, ?)
		ON CONFLICT (hash) DO UPDATE SET blob = excluded.blob,
			recent_ts = max(wz_stickers.recent_ts, excluded.recent_ts),
			favorite = CASE WHEN excluded.favorite > 0 THEN excluded.favorite ELSE wz_stickers.favorite END`,
		hash, blob, recentTS, fav)
	return err
}

// linkExpiry is when a media direct path stops working: its "oe" parameter,
// in hex unix seconds. It is 0 if unknown.
func linkExpiry(directPath string) int64 {
	u, err := url.Parse(directPath)
	if err != nil {
		return 0
	}
	oe, _ := strconv.ParseInt(u.Query().Get("oe"), 16, 64)
	return oe
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
	rows, err := s.db.QueryContext(ctx, `SELECT raw_payload, NULL FROM wz_messages WHERE media = ? AND raw_payload IS NOT NULL
		UNION ALL SELECT NULL, blob FROM wz_stickers`, int(model.MediaSticker))
	if err != nil {
		return nil
	}
	defer rows.Close()
	for rows.Next() {
		var raw, blob []byte
		if rows.Scan(&raw, &blob) != nil {
			continue
		}
		if raw != nil {
			_, blob = rawMedia(raw)
		}
		var m waE2E.StickerMessage
		if len(blob) > 0 && proto.Unmarshal(blob, &m) == nil &&
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
	rows, err := b.db.QueryContext(b.ctx, `SELECT chat, id, raw_payload FROM wz_messages
		WHERE media = ? AND from_me = 0 AND raw_payload IS NOT NULL ORDER BY ts DESC LIMIT 400`, int(model.MediaSticker))
	if err != nil {
		return nil
	}
	defer rows.Close()
	seen := map[string]bool{}
	var out []*model.Message
	for rows.Next() && len(out) < stickerListMax {
		var chat, id string
		var raw []byte
		if rows.Scan(&chat, &id, &raw) != nil {
			continue
		}
		_, blob := rawMedia(raw)
		var s waE2E.StickerMessage
		if len(blob) == 0 || proto.Unmarshal(blob, &s) != nil || s.GetIsAnimated() {
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
		b.copyStickerFile(srcChat, srcID, hash)
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

// favoriteStickerVersion is the favoriteSticker app state action's version
// (WhatsApp Web's WAWebStickersFavoriteSyncAction).
const favoriteStickerVersion = 7

// chatSticker returns a sticker message's media message and file hash (hex).
func (b *Backend) chatSticker(m *model.Message) (*waE2E.StickerMessage, string, bool) {
	if m.Media != model.MediaSticker {
		return nil, "", false
	}
	_, blob, err := b.store.mediaBlob(b.ctx, m.ChatID, m.ID)
	var s waE2E.StickerMessage
	if err != nil || proto.Unmarshal(blob, &s) != nil || len(s.GetFileSHA256()) != 32 {
		return nil, "", false
	}
	s.ContextInfo = nil
	return &s, hex.EncodeToString(s.GetFileSHA256()), true
}

// FavoriteSticker implements model.Backend.
func (b *Backend) FavoriteSticker(m *model.Message) bool {
	_, hash, ok := b.chatSticker(m)
	var fav int64
	return ok && b.db.QueryRowContext(b.ctx, `SELECT favorite FROM wz_stickers WHERE hash = ?`, hash).Scan(&fav) == nil && fav > 0
}

// SetFavoriteSticker implements model.Backend. Like WhatsApp, it sends a
// favoriteSticker mutation (a SET with isFavorite false to unfavourite).
func (b *Backend) SetFavoriteSticker(m *model.Message, fav bool) {
	if b.connected() == nil {
		return
	}
	s, hash, ok := b.chatSticker(m)
	if !ok {
		b.emit(model.NoticeEvent{Text: "This sticker can't be added to favourites."})
		return
	}
	if fav {
		if err := b.store.putSticker(b.ctx, b.db, hash, s, 0, time.Now().Unix()); err != nil {
			b.log.Warnf("store favourite sticker: %v", err)
			return
		}
		b.copyStickerFile(m.ChatID, m.ID, hash)
	} else if err := b.store.unmarkSticker(b.ctx, hash, "favorite"); err != nil {
		b.log.Warnf("unfavourite sticker: %v", err)
		return
	}
	b.emit(model.StickersEvent{})
	b.sendAppState(appstate.PatchInfo{
		Type: appstate.WAPatchRegularLow,
		Mutations: []appstate.MutationInfo{{
			Index:   []string{appstate.IndexFavoriteSticker, base64.StdEncoding.EncodeToString(s.GetFileSHA256())},
			Version: favoriteStickerVersion,
			Value: &waSyncAction.SyncActionValue{StickerAction: &waSyncAction.StickerAction{
				URL: s.URL, FileEncSHA256: s.FileEncSHA256, MediaKey: s.MediaKey, Mimetype: s.Mimetype,
				Height: s.Height, Width: s.Width, DirectPath: s.DirectPath, FileLength: s.FileLength,
				IsFavorite: proto.Bool(fav), IsLottie: s.IsLottie, IsAvatarSticker: s.IsAvatar,
			}},
		}},
	})
}

// copyStickerFile gives a synced sticker the file of a chat message with the
// same sticker, if that is on disk, saving a download.
func (b *Backend) copyStickerFile(chat, id, hash string) {
	dst := b.mediaPath(stickerChat, hash)
	if _, err := os.Stat(dst); err == nil {
		return
	}
	if data, err := os.ReadFile(b.mediaPath(chat, id)); err == nil {
		_ = os.MkdirAll(filepath.Dir(dst), 0o700)
		_ = os.WriteFile(dst, data, 0o600)
	}
}

// stickerCopyTries is how many chat copies of a sticker stickerFromChats
// downloads before it gives up.
const stickerCopyTries = 3

// stickerFromChats gets a synced sticker whose own link is dead (expired, or
// the file is gone from WhatsApp's servers) from a chat message with the same
// file, newest first: the file on disk, or else a download of that copy. The
// copy's media message replaces the dead one, so sending the sticker gives
// the recipient a link that works.
func (b *Backend) stickerFromChats(ctx context.Context, cli *whatsmeow.Client, hash string) bool {
	sha, err := hex.DecodeString(hash)
	if err != nil || len(sha) != 32 {
		return false // an enc- key: no plaintext hash to match
	}
	type chatCopy struct {
		chat, id string
		m        *waE2E.StickerMessage
	}
	rows, err := b.db.QueryContext(ctx, `SELECT chat, id, raw_payload FROM wz_messages
		WHERE media = ? AND raw_payload IS NOT NULL ORDER BY ts DESC`, int(model.MediaSticker))
	if err != nil {
		return false
	}
	var copies []chatCopy
	for rows.Next() {
		var c chatCopy
		var raw []byte
		m := &waE2E.StickerMessage{}
		if rows.Scan(&c.chat, &c.id, &raw) != nil {
			continue
		}
		if _, blob := rawMedia(raw); len(blob) > 0 && proto.Unmarshal(blob, m) == nil &&
			string(m.GetFileSHA256()) == string(sha) {
			c.m = m
			copies = append(copies, c)
		}
	}
	rows.Close()
	tries := 0
	for _, c := range copies {
		data, err := os.ReadFile(b.mediaPath(c.chat, c.id))
		if err != nil {
			if tries == stickerCopyTries {
				continue
			}
			tries++
			if data, err = cli.Download(ctx, c.m); err != nil {
				continue
			}
		}
		path := b.mediaPath(stickerChat, hash)
		_ = os.MkdirAll(filepath.Dir(path), 0o700)
		if err := os.WriteFile(path, data, 0o600); err != nil {
			b.log.Warnf("save sticker: %v", err)
			return false
		}
		c.m.ContextInfo = nil
		if err := b.store.putSticker(ctx, b.db, hash, c.m, 0, 0); err != nil {
			b.log.Warnf("store sticker: %v", err)
		}
		b.emit(model.MediaEvent{ChatID: stickerChat, MsgID: hash})
		return true
	}
	return false
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
