package wa

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/polymorfa/hypermeow"
	"github.com/polymorfa/hypermeow/proto/waE2E"
	"google.golang.org/protobuf/proto"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

// Sticker packs. A sticker app's pack is shared as a stickerPackMessage:
// a zip of its stickers (named in the message, with the tray icon beside
// them) and a picture of the first few. WhatsApp's own packs, from its
// sticker store, are shared as wa.me/stickerpack/<id> links, and their
// stickers come from static.whatsapp.net with ready-made media keys.
//
// "View sticker pack" loads one (OpenStickerPack) and stores it;
// AddStickerPack puts it in the picker. WhatsApp keeps installed packs on
// each device: no app state action syncs them.
//
// A pack's stickers are synced-sticker messages (stickerChat, keyed by
// the file's hash): the zip's stickers are written to disk and have no
// direct path until they are first sent, which uploads them
// (SendSticker); the store's download as any sticker does.

// packChat is the pseudo-chat of pack tray icons: MediaData(packChat, id)
// is the pack's.
const packChat = "stickerpack"

const packSchema = `
CREATE TABLE IF NOT EXISTS wz_sticker_packs (
	id          TEXT PRIMARY KEY,
	name        TEXT NOT NULL DEFAULT '',
	publisher   TEXT NOT NULL DEFAULT '',
	about       TEXT NOT NULL DEFAULT '',
	first_party INTEGER NOT NULL DEFAULT 0,
	lottie      INTEGER NOT NULL DEFAULT 0,
	animated    INTEGER NOT NULL DEFAULT 0,
	size        INTEGER NOT NULL DEFAULT 0,
	source      BLOB,                       -- the stickerPackMessage its zip came in
	tray        BLOB,                       -- its tray icon (PNG or WebP)
	added       INTEGER NOT NULL DEFAULT 0, -- added to the picker at (unix seconds), 0 = not
	ord         INTEGER NOT NULL DEFAULT 0, -- its place among the added packs (SetStickerPackOrder)
	loaded      INTEGER NOT NULL DEFAULT 0  -- its stickers were stored at (unix seconds)
);
CREATE TABLE IF NOT EXISTS wz_pack_stickers (
	pack TEXT NOT NULL,
	idx  INTEGER NOT NULL,
	hash TEXT NOT NULL,  -- hex SHA-256 of the file
	blob BLOB NOT NULL,  -- marshaled waE2E.StickerMessage
	PRIMARY KEY (pack, idx)
);
CREATE INDEX IF NOT EXISTS wz_pack_stickers_hash ON wz_pack_stickers (hash);
`

// storeRefresh is how old a store pack may be before it is fetched again
// for fresh links (they expire after a few weeks).
const storeRefresh = 7 * 24 * time.Hour

// stickerStoreURL serves WhatsApp's sticker store. lottie=1 includes the
// Lottie packs, which are left out otherwise.
const stickerStoreURL = "https://static.whatsapp.net/sticker"

// packLink matches a link to one of WhatsApp's own sticker packs, as WhatsApp
// Web's WAWebApiParse does (the avatar pack opens elsewhere).
var packLink = regexp.MustCompile(`(?i)\bhttps?://wa\.me/stickerpack/([A-Za-z0-9._~%-]+)`)

// linkedPack is the pack of WhatsApp's own a text links to, or nil.
func linkedPack(text string) *model.StickerPack {
	if id := stickerPackLink(text); id != "" {
		return &model.StickerPack{ID: id, FirstParty: true}
	}
	return nil
}

// stickerPackLink returns the pack a text links to, or "".
func stickerPackLink(text string) string {
	if !strings.Contains(text, "stickerpack/") {
		return ""
	}
	m := packLink.FindStringSubmatch(text)
	if m == nil || strings.EqualFold(m[1], "meta-avatar") {
		return ""
	}
	id, err := url.PathUnescape(m[1])
	if err != nil {
		return ""
	}
	return id
}

// packOfMessage is what a stickerPackMessage says of its pack.
func packOfMessage(e *waE2E.StickerPackMessage) *model.StickerPack {
	p := &model.StickerPack{
		ID: e.GetStickerPackID(), Name: e.GetName(), Publisher: e.GetPublisher(), About: e.GetPackDescription(),
		Count: len(e.GetStickers()), Size: int64(e.GetStickerPackSize()),
		FirstParty: e.StickerPackOrigin != nil && e.GetStickerPackOrigin() == waE2E.StickerPackMessage_FIRST_PARTY,
	}
	if p.Size == 0 {
		p.Size = int64(e.GetFileLength())
	}
	for _, s := range e.GetStickers() {
		p.Lottie = p.Lottie || s.GetIsLottie()
		p.Animated = p.Animated || s.GetIsAnimated()
	}
	return p
}

// downloadPackThumb downloads a stickerPackMessage's picture.
func downloadPackThumb(ctx context.Context, cli *whatsmeow.Client, m *waE2E.StickerPackMessage) ([]byte, error) {
	if m.GetThumbnailDirectPath() == "" {
		return nil, whatsmeow.ErrNoURLPresent
	}
	return cli.DownloadMediaWithPath(ctx, m.GetThumbnailDirectPath(), m.GetThumbnailEncSHA256(), m.GetThumbnailSHA256(),
		m.GetMediaKey(), "WhatsApp Sticker Pack Thumbnail Keys", "thumbnail-sticker-pack", false)
}

// packSticker is a sticker of a pack being stored.
type packSticker struct {
	hash string
	m    *waE2E.StickerMessage
}

// putPack stores a pack and its stickers, keeping whether it was added.
func (s *msgStore) putPack(ctx context.Context, p *model.StickerPack, source, tray []byte, stickers []packSticker, now time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `INSERT INTO wz_sticker_packs (id, name, publisher, about, first_party, lottie, animated, size, source, tray, loaded)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (id) DO UPDATE SET name = excluded.name, publisher = excluded.publisher, about = excluded.about,
			first_party = excluded.first_party, lottie = excluded.lottie, animated = excluded.animated, size = excluded.size,
			source = COALESCE(excluded.source, wz_sticker_packs.source), tray = COALESCE(excluded.tray, wz_sticker_packs.tray),
			loaded = excluded.loaded`,
		p.ID, p.Name, p.Publisher, p.About, boolInt(p.FirstParty), boolInt(p.Lottie), boolInt(p.Animated), p.Size,
		source, tray, now.Unix())
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM wz_pack_stickers WHERE pack = ?`, p.ID); err != nil {
		return err
	}
	for i, st := range stickers {
		if _, err := tx.ExecContext(ctx, `INSERT INTO wz_pack_stickers (pack, idx, hash, blob) VALUES (?, ?, ?, ?)`,
			p.ID, i, st.hash, marshal(st.m)); err != nil {
			return err
		}
	}
	return tx.Commit()
}

const packColumns = `id, name, publisher, about, first_party, lottie, animated, size, added, loaded,
	(SELECT COUNT(*) FROM wz_pack_stickers s WHERE s.pack = wz_sticker_packs.id)`

func scanPack(sc scanner) (*model.StickerPack, int64, error) {
	var p model.StickerPack
	var added, loaded int64
	err := sc.Scan(&p.ID, &p.Name, &p.Publisher, &p.About, &p.FirstParty, &p.Lottie, &p.Animated, &p.Size, &added, &loaded, &p.Count)
	p.Added = added > 0
	return &p, loaded, err
}

// pack returns a stored pack and when its stickers were stored.
func (s *msgStore) pack(ctx context.Context, id string) (*model.StickerPack, time.Time, error) {
	p, loaded, err := scanPack(s.db.QueryRowContext(ctx, `SELECT `+packColumns+` FROM wz_sticker_packs WHERE id = ?`, id))
	if err != nil {
		return nil, time.Time{}, err
	}
	return p, time.Unix(loaded, 0), nil
}

// packStickers lists a stored pack's stickers as synced-sticker messages.
func (s *msgStore) packStickers(ctx context.Context, id string) []*model.Message {
	rows, err := s.db.QueryContext(ctx, `SELECT hash, blob FROM wz_pack_stickers WHERE pack = ? ORDER BY idx`, id)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []*model.Message
	for rows.Next() {
		var hash string
		var blob []byte
		if rows.Scan(&hash, &blob) != nil {
			continue
		}
		var m waE2E.StickerMessage
		_ = proto.Unmarshal(blob, &m)
		out = append(out, &model.Message{ID: hash, ChatID: stickerChat, Kind: model.KindSticker, Media: model.MediaSticker,
			Lottie: m.GetIsLottie() || m.GetMimetype() == "application/was"})
	}
	return out
}

// packTray returns a pack's tray icon.
func (s *msgStore) packTray(ctx context.Context, id string) []byte {
	var tray []byte
	_ = s.db.QueryRowContext(ctx, `SELECT tray FROM wz_sticker_packs WHERE id = ?`, id).Scan(&tray)
	return tray
}

// StickerPacks implements model.Backend.
func (b *Backend) StickerPacks() []*model.StickerPack {
	rows, err := b.db.QueryContext(b.ctx, `SELECT `+packColumns+` FROM wz_sticker_packs WHERE added > 0 ORDER BY ord, added, rowid`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []*model.StickerPack
	for rows.Next() {
		if p, _, err := scanPack(rows); err == nil {
			out = append(out, p)
		}
	}
	return out
}

// PackStickers implements model.Backend.
func (b *Backend) PackStickers(id string) []*model.Message { return b.store.packStickers(b.ctx, id) }

// AddStickerPack implements model.Backend.
func (b *Backend) AddStickerPack(id string, add bool) {
	added := int64(0)
	if add {
		added = b.now().Unix()
	}
	// A pack added goes after the others, wherever they were moved.
	if _, err := b.db.ExecContext(b.ctx, `UPDATE wz_sticker_packs SET added = ?1,
		ord = CASE WHEN ?1 > 0 THEN (SELECT COALESCE(MAX(ord), 0) + 1 FROM wz_sticker_packs WHERE added > 0 AND id != ?2) ELSE 0 END
		WHERE id = ?2`, added, id); err != nil {
		b.log.Warnf("add sticker pack %s: %v", id, err)
		return
	}
	b.emit(model.StickersEvent{})
}

// SetStickerPackOrder implements model.Backend.
func (b *Backend) SetStickerPackOrder(ids []string) {
	tx, err := b.db.BeginTx(b.ctx, nil)
	if err != nil {
		b.log.Warnf("order sticker packs: %v", err)
		return
	}
	defer tx.Rollback()
	for i, id := range ids {
		if _, err := tx.ExecContext(b.ctx, `UPDATE wz_sticker_packs SET ord = ? WHERE id = ? AND added > 0`, i+1, id); err != nil {
			b.log.Warnf("order sticker packs: %v", err)
			return
		}
	}
	if err := tx.Commit(); err != nil {
		b.log.Warnf("order sticker packs: %v", err)
		return
	}
	b.emit(model.StickersEvent{})
}

// OpenStickerPack implements model.Backend.
func (b *Backend) OpenStickerPack(m *model.Message) {
	if m == nil || m.Pack == nil {
		return
	}
	ref, chat, id := *m.Pack, m.ChatID, m.ID
	go func() {
		ctx, cancel := context.WithTimeout(b.ctx, 2*time.Minute)
		defer cancel()
		ev := model.StickerPackEvent{ID: ref.ID}
		p, err := b.loadPack(ctx, ref, chat, id)
		switch {
		case err != nil:
			b.log.Infof("sticker pack %q: %v", ref.ID, err)
			ev.Err = "Couldn't load this sticker pack."
			if errors.Is(err, errPackGone) {
				ev.Err = "This sticker pack is no longer available."
			}
		default:
			ev.Pack, ev.Stickers = p, b.store.packStickers(ctx, p.ID)
		}
		b.emit(ev)
	}()
}

// errPackGone is a pack whose zip is gone from WhatsApp's servers, or that
// the sticker store doesn't have.
var errPackGone = errors.New("sticker pack gone")

// loadPack returns a pack, stored once it is loaded: from the store for
// WhatsApp's own, or from the zip the chat message shared. A stored pack
// is used as it is, but the store's are fetched again for fresh links
// once they are older than storeRefresh.
func (b *Backend) loadPack(ctx context.Context, ref model.StickerPack, chat, id string) (*model.StickerPack, error) {
	if p, loaded, err := b.store.pack(ctx, ref.ID); err == nil && ref.ID != "" &&
		(!p.FirstParty || b.now().Sub(loaded) < storeRefresh) {
		return p, nil
	}
	var spm *waE2E.StickerPackMessage
	if pm, err := b.rawMessage(chat, id); err == nil {
		spm = unwrap(pm).GetStickerPackMessage()
	}
	switch {
	case spm != nil && spm.GetDirectPath() != "":
		return b.unzipPack(ctx, spm)
	case ref.FirstParty:
		return b.fetchStorePack(ctx, ref.ID)
	}
	return nil, fmt.Errorf("no sticker pack in %s", id)
}

// unzipPack downloads a stickerPackMessage's zip and stores its stickers.
func (b *Backend) unzipPack(ctx context.Context, spm *waE2E.StickerPackMessage) (*model.StickerPack, error) {
	cli := b.client()
	if cli == nil || !cli.IsConnected() {
		return nil, errors.New("offline")
	}
	data, err := cli.Download(ctx, spm)
	if errors.Is(err, whatsmeow.ErrMediaDownloadFailedWith403) || errors.Is(err, whatsmeow.ErrMediaDownloadFailedWith404) ||
		errors.Is(err, whatsmeow.ErrMediaDownloadFailedWith410) {
		return nil, fmt.Errorf("%w: %v", errPackGone, err)
	}
	if err != nil {
		return nil, err
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, err
	}
	files := map[string]*zip.File{}
	var names []string
	for _, f := range zr.File {
		files[f.Name] = f
		names = append(names, f.Name)
	}
	read := func(name string) []byte {
		f := files[name]
		if f == nil || f.UncompressedSize64 > 4<<20 {
			return nil
		}
		rc, err := f.Open()
		if err != nil {
			return nil
		}
		defer rc.Close()
		d, _ := io.ReadAll(io.LimitReader(rc, 4<<20))
		return d
	}
	list := spm.GetStickers()
	if len(list) == 0 {
		// No list: every file but the tray icon, in name order.
		slices.Sort(names)
		for _, n := range names {
			if n != spm.GetTrayIconFileName() {
				list = append(list, &waE2E.StickerPackMessage_Sticker{FileName: proto.String(n)})
			}
		}
	}
	p := packOfMessage(spm)
	if p.ID == "" {
		sum := sha256.Sum256(data)
		p.ID = "zip-" + hex.EncodeToString(sum[:8])
	}
	p.FirstParty = false
	var stickers []packSticker
	for _, s := range list {
		d := read(s.GetFileName())
		if len(d) == 0 {
			continue
		}
		sum := sha256.Sum256(d)
		hash := hex.EncodeToString(sum[:])
		path := b.mediaPath(stickerChat, hash)
		if _, err := os.Stat(path); err != nil {
			_ = os.MkdirAll(filepath.Dir(path), 0o700)
			if err := os.WriteFile(path, d, 0o600); err != nil {
				return nil, err
			}
		}
		mime := first(s.GetMimetype(), "image/webp")
		m := &waE2E.StickerMessage{FileSHA256: sum[:], Mimetype: proto.String(mime), FileLength: proto.Uint64(uint64(len(d))),
			Width: proto.Uint32(512), Height: proto.Uint32(512)}
		if s.GetIsAnimated() {
			m.IsAnimated = proto.Bool(true)
		}
		if s.GetIsLottie() || mime == "application/was" {
			m.IsLottie = proto.Bool(true)
		}
		if l := s.GetAccessibilityLabel(); l != "" {
			m.AccessibilityLabel = proto.String(l)
		}
		if e := s.GetEmojis(); len(e) > 0 {
			m.Emojis = proto.String(strings.Join(e, ""))
		}
		stickers = append(stickers, packSticker{hash: hash, m: m})
	}
	if len(stickers) == 0 {
		return nil, errors.New("the zip has no stickers")
	}
	p.Count = len(stickers)
	src := proto.Clone(spm).(*waE2E.StickerPackMessage)
	src.ContextInfo = nil
	if err := b.store.putPack(ctx, p, marshal(src), read(spm.GetTrayIconFileName()), stickers, b.now()); err != nil {
		return nil, err
	}
	sp, _, err := b.store.pack(ctx, p.ID)
	return sp, err
}

// storePack is a pack as WhatsApp's sticker store describes it.
type storePack struct {
	ID          string `json:"sticker-pack-id"`
	Name        string `json:"name"`
	Publisher   string `json:"publisher"`
	Description string `json:"description"`
	FileSize    string `json:"file-size"`
	Animated    int    `json:"animated"`
	Lottie      int    `json:"lottie"`
	Premium     int    `json:"premium"`
	TrayImageID string `json:"tray-image-id"`
	Stickers    []struct {
		MediaKey    []byte   `json:"media-key"`
		FileHash    []byte   `json:"file-hash"`
		EncFileHash []byte   `json:"enc-file-hash"`
		DirectPath  string   `json:"direct-path"`
		FileSize    int64    `json:"file-size"`
		Mimetype    string   `json:"mimetype"`
		Height      int      `json:"height"`
		Width       int      `json:"width"`
		Emojis      []string `json:"emojis"`
		Access      string   `json:"accessibility-text"`
	} `json:"stickers"`
}

// fetchStorePack fetches one of WhatsApp's own packs from its sticker
// store, as WhatsApp Web's fetchFirstPartyStickerPack does, and stores it.
func (b *Backend) fetchStorePack(ctx context.Context, id string) (*model.StickerPack, error) {
	q := url.Values{"cat": {"sticker_pack_data"}, "id": {id}, "lg": {"en"}, "lottie": {"1"}, "premium": {"1"}}
	data, err := httpGet(ctx, stickerStoreURL+"?"+q.Encode())
	if err != nil {
		if strings.Contains(err.Error(), "404") {
			return nil, fmt.Errorf("%w: %v", errPackGone, err)
		}
		return nil, err
	}
	var packs []storePack
	if err := json.Unmarshal(data, &packs); err != nil || len(packs) == 0 {
		return nil, fmt.Errorf("%w: %s", errPackGone, bytes.TrimSpace(data[:min(len(data), 80)]))
	}
	sp := packs[0]
	p := &model.StickerPack{ID: first(sp.ID, id), Name: sp.Name, Publisher: sp.Publisher, About: sp.Description,
		FirstParty: true, Lottie: sp.Lottie == 1, Animated: sp.Animated == 1}
	fmt.Sscan(sp.FileSize, &p.Size)
	var stickers []packSticker
	for _, s := range sp.Stickers {
		if len(s.FileHash) != 32 || s.DirectPath == "" {
			continue
		}
		m := &waE2E.StickerMessage{FileSHA256: s.FileHash, FileEncSHA256: s.EncFileHash, MediaKey: s.MediaKey,
			DirectPath: proto.String(s.DirectPath), Mimetype: proto.String(first(s.Mimetype, "image/webp")),
			FileLength: proto.Uint64(uint64(s.FileSize)), Width: proto.Uint32(uint32(s.Width)), Height: proto.Uint32(uint32(s.Height)),
			MediaKeyTimestamp: proto.Int64(b.now().Unix())}
		if s.Mimetype == "application/was" {
			m.IsLottie = proto.Bool(true)
		} else if sp.Animated == 1 {
			m.IsAnimated = proto.Bool(true)
		}
		if sp.Premium != 0 {
			m.Premium = proto.Int32(int32(sp.Premium))
		}
		if s.Access != "" {
			m.AccessibilityLabel = proto.String(s.Access)
		}
		if len(s.Emojis) > 0 {
			m.Emojis = proto.String(strings.Join(s.Emojis, ""))
		}
		stickers = append(stickers, packSticker{hash: hex.EncodeToString(s.FileHash), m: m})
	}
	if len(stickers) == 0 {
		return nil, fmt.Errorf("%w: no stickers", errPackGone)
	}
	p.Count = len(stickers)
	var tray []byte
	if sp.TrayImageID != "" {
		tray, _ = httpGet(ctx, stickerStoreURL+"?"+url.Values{"img": {sp.TrayImageID}}.Encode())
	}
	if err := b.store.putPack(ctx, p, nil, tray, stickers, b.now()); err != nil {
		return nil, err
	}
	stored, _, err := b.store.pack(ctx, p.ID)
	return stored, err
}

// refetchPackSticker brings back a zip sticker whose file is gone from
// disk by downloading its pack's zip again. One no pack has a zip for is
// marked failed.
func (b *Backend) refetchPackSticker(ctx context.Context, hash string) {
	var src []byte
	err := b.db.QueryRowContext(ctx, `SELECT p.source FROM wz_pack_stickers s JOIN wz_sticker_packs p ON p.id = s.pack
		WHERE s.hash = ? AND p.source IS NOT NULL LIMIT 1`, hash).Scan(&src)
	var spm waE2E.StickerPackMessage
	if err == nil && proto.Unmarshal(src, &spm) == nil {
		if _, err = b.unzipPack(ctx, &spm); err == nil {
			if _, err := os.Stat(b.mediaPath(stickerChat, hash)); err == nil {
				b.emit(model.MediaEvent{ChatID: stickerChat, MsgID: hash})
				return
			}
		}
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		b.log.Infof("sticker %s: its pack: %v", hash, err)
	}
	_ = os.WriteFile(b.failedPath(stickerChat, hash), nil, 0o600)
	b.emit(model.MediaEvent{ChatID: stickerChat, MsgID: hash})
}

// unuploaded returns a pack sticker that has never been uploaded (it came
// in a zip), or nil: sending it uploads it first.
func (b *Backend) unuploaded(hash string) *waE2E.StickerMessage {
	blob, err := b.store.stickerBlob(b.ctx, hash)
	var m waE2E.StickerMessage
	if err != nil || proto.Unmarshal(blob, &m) != nil || m.GetDirectPath() != "" {
		return nil
	}
	return &m
}

// rescanUnsupported gives the messages stored as unsupported what they
// show, now that the app supports more, once per supportVersion.
func (s *msgStore) rescanUnsupported(ctx context.Context) error {
	const supportVersion = "1" // sticker packs
	if s.meta(ctx, "unsupported_rescan") == supportVersion {
		return nil
	}
	rows, err := s.db.QueryContext(ctx, `SELECT rowid, raw_payload FROM wz_messages WHERE kind = ? AND raw_payload IS NOT NULL`,
		int(model.KindUnsupported))
	if err != nil {
		return err
	}
	type fix struct {
		rowid       int64
		kind, media int
		text        string
	}
	var fixes []fix
	for rows.Next() {
		var f fix
		var raw []byte
		if rows.Scan(&f.rowid, &raw) != nil {
			continue
		}
		if c, m := describeRaw(raw); m != nil && (c.media != model.MediaNone || c.text != "") {
			f.kind, f.media, f.text = int(c.kind), int(c.media), c.text
			fixes = append(fixes, f)
		}
	}
	rows.Close()
	for _, f := range fixes {
		if _, err := s.db.ExecContext(ctx, `UPDATE wz_messages SET kind = ?, media = ?, text = ? WHERE rowid = ?`,
			f.kind, f.media, f.text, f.rowid); err != nil {
			return err
		}
	}
	return s.setMetaValue(ctx, "unsupported_rescan", supportVersion)
}
