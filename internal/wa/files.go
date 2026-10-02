package wa

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/polymorfa/hypermeow"
	"github.com/polymorfa/hypermeow/proto/waE2E"
	"github.com/polymorfa/hypermeow/types"
	"google.golang.org/protobuf/proto"

	"github.com/chomosuke9/wazzapclients/internal/model"
	"github.com/chomosuke9/wazzapclients/internal/photo"
)

// fileInfo is what the file column keeps about a document or audio file.
type fileInfo struct {
	Name  string `json:"n,omitempty"`
	Size  int64  `json:"s,omitempty"`
	Type  string `json:"t,omitempty"`
	Pages int    `json:"p,omitempty"`
	Wave  []byte `json:"w,omitempty"`
}

func documentInfo(e *waE2E.DocumentMessage) fileInfo {
	return fileInfo{Name: first(e.GetFileName(), e.GetTitle()), Size: int64(e.GetFileLength()), Type: e.GetMimetype(),
		Pages: int(e.GetPageCount())}
}

func audioInfo(e *waE2E.AudioMessage) fileInfo {
	return fileInfo{Size: int64(e.GetFileLength()), Type: e.GetMimetype(), Wave: e.GetWaveform()}
}

func fileOf(m *model.Message) fileInfo {
	return fileInfo{Name: m.FileName, Size: m.FileSize, Type: m.FileType, Pages: m.Pages, Wave: m.Waveform}
}

func (f fileInfo) apply(m *model.Message) {
	m.FileName, m.FileSize, m.FileType, m.Pages, m.Waveform = f.Name, f.Size, f.Type, f.Pages, f.Wave
}

func (f fileInfo) marshal() string {
	if f.Name == "" && f.Size == 0 && f.Type == "" && f.Pages == 0 && len(f.Wave) == 0 {
		return ""
	}
	b, _ := json.Marshal(f)
	return string(b)
}

func parseFile(s string) fileInfo {
	var f fileInfo
	if s != "" {
		_ = json.Unmarshal([]byte(s), &f)
	}
	return f
}

// migrateFileInfo fills the file column of documents and audio stored
// before it existed, from their media blobs. A document with a caption
// gets the caption as its text instead of its file name.
func (s *msgStore) migrateFileInfo(ctx context.Context) error {
	const key = "file_info_migrated"
	if s.meta(ctx, key) != "" {
		return nil
	}
	rows, err := s.db.QueryContext(ctx, `SELECT chat, id, media, media_blob FROM wz_messages
		WHERE media IN (?, ?, ?) AND file = '' AND media_blob IS NOT NULL`,
		int(model.MediaDocument), int(model.MediaVoice), int(model.MediaAudio))
	if err != nil {
		return err
	}
	type update struct {
		chat, id, file, caption string
	}
	var ups []update
	for rows.Next() {
		var (
			u     update
			media model.Media
			blob  []byte
		)
		if err := rows.Scan(&u.chat, &u.id, &media, &blob); err != nil {
			rows.Close()
			return err
		}
		var f fileInfo
		if media == model.MediaDocument {
			var d waE2E.DocumentMessage
			if proto.Unmarshal(blob, &d) != nil {
				continue
			}
			f, u.caption = documentInfo(&d), d.GetCaption()
		} else {
			var a waE2E.AudioMessage
			if proto.Unmarshal(blob, &a) != nil {
				continue
			}
			f = audioInfo(&a)
		}
		u.file = f.marshal()
		ups = append(ups, u)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, u := range ups {
		if _, err := tx.ExecContext(ctx, `UPDATE wz_messages SET file = ?,
			text = CASE WHEN ? != '' THEN ? ELSE text END WHERE chat = ? AND id = ?`,
			u.file, u.caption, u.caption, u.chat, u.id); err != nil {
			return fmt.Errorf("migrate file info: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	return s.setMetaValue(ctx, key, time.Now().Format(time.RFC3339))
}

// mediaExt is the extension a downloaded file is kept with, so the system
// knows what opens it.
func mediaExt(m *model.Message) string {
	switch m.Media {
	case model.MediaVideo, model.MediaGIF:
		return ".mp4"
	case model.MediaVoice:
		return ".ogg"
	}
	if ext := strings.ToLower(filepath.Ext(m.FileName)); len(ext) > 1 && len(ext) <= 8 && safeExt(ext) {
		return ext
	}
	if t, _, err := mime.ParseMediaType(m.FileType); err == nil {
		switch t {
		case "audio/mpeg":
			return ".mp3"
		case "audio/ogg":
			return ".ogg"
		case "audio/mp4", "audio/aac":
			return ".m4a"
		case "application/pdf":
			return ".pdf"
		}
		if exts, _ := mime.ExtensionsByType(t); len(exts) > 0 {
			return exts[0]
		}
	}
	if m.Media == model.MediaAudio {
		return ".mp3"
	}
	return ".bin"
}

func safeExt(ext string) bool {
	for _, r := range ext[1:] {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9') {
			return false
		}
	}
	return true
}

// mediaFilePath is where a video, audio file or document is downloaded to:
// next to the other media, with an extension.
func (b *Backend) mediaFilePath(m *model.Message) string {
	return b.mediaPath(m.ChatID, m.ID) + mediaExt(m)
}

// HasMediaFile implements model.Backend.
func (b *Backend) HasMediaFile(m *model.Message) bool {
	_, err := os.Stat(b.mediaFilePath(m))
	return err == nil
}

// OpenMedia implements model.Backend.
func (b *Backend) OpenMedia(m *model.Message) {
	path := b.mediaFilePath(m)
	if _, busy := b.playing.LoadOrStore(path, true); busy {
		return // already downloading; it opens when done
	}
	go func() {
		defer b.playing.Delete(path)
		if _, err := os.Stat(path); err != nil {
			ok := b.downloadFile(m, path)
			b.emit(model.MediaEvent{ChatID: m.ChatID, MsgID: m.ID, Failed: !ok})
			if !ok {
				return
			}
		}
		if err := openFile(path); err != nil {
			b.log.Warnf("open %s: %v", path, err)
			b.emit(model.NoticeEvent{Text: "Couldn't find an app to open this file."})
		}
	}()
}

// MediaFile implements model.Backend.
func (b *Backend) MediaFile(m *model.Message) string {
	path := b.mediaFilePath(m)
	if _, err := os.Stat(path); err == nil {
		return path
	}
	if _, busy := b.playing.LoadOrStore(path, true); !busy {
		go func() {
			defer b.playing.Delete(path)
			ok := b.downloadFile(m, path)
			b.emit(model.MediaEvent{ChatID: m.ChatID, MsgID: m.ID, Failed: !ok})
		}()
	}
	return ""
}

// downloadable returns the part of a stored media message that downloads.
func downloadable(media model.Media, blob []byte) whatsmeow.DownloadableMessage {
	m := mediaMessage(media, blob)
	switch {
	case m == nil:
		return nil
	case m.VideoMessage != nil:
		return m.VideoMessage
	case m.AudioMessage != nil:
		return m.AudioMessage
	case m.DocumentMessage != nil:
		return m.DocumentMessage
	case m.ImageMessage != nil:
		return m.ImageMessage
	case m.StickerMessage != nil:
		return m.StickerMessage
	}
	return nil
}

// downloadFile streams a message's media to path, so a big file never
// sits in memory.
func (b *Backend) downloadFile(m *model.Message, path string) bool {
	ctx, cancel := context.WithTimeout(b.ctx, 30*time.Minute)
	defer cancel()
	media, blob, _ := b.store.mediaBlob(ctx, m.ChatID, m.ID)
	dl := downloadable(media, blob)
	cli := b.client()
	if dl == nil || cli == nil || !cli.IsConnected() {
		b.emit(model.NoticeEvent{Text: "This file isn't available."})
		return false
	}
	_ = os.MkdirAll(filepath.Dir(path), 0o700)
	f, err := os.OpenFile(path+".part", os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		b.emit(model.NoticeEvent{Text: "Couldn't save the file: " + err.Error()})
		return false
	}
	err = cli.DownloadToFile(ctx, dl, f)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(path+".part", path)
	}
	if err != nil {
		_ = os.Remove(path + ".part")
		b.log.Infof("download %s: %v", m.ID, err)
		if errors.Is(err, whatsmeow.ErrMediaDownloadFailedWith404) || errors.Is(err, whatsmeow.ErrMediaDownloadFailedWith410) {
			b.emit(model.NoticeEvent{Text: "This file is no longer on WhatsApp's servers. Ask the sender to send it again."})
		} else {
			b.emit(model.NoticeEvent{Text: "Couldn't download the file."})
		}
		return false
	}
	return true
}

// draftContext builds the context of an outgoing message from a draft:
// its mentions and the message it replies to. It fills in sm's quote and
// mentions, and returns nil when there is nothing to add.
func (b *Backend) draftContext(chatID string, d model.Draft, sm *storedMsg) *waE2E.ContextInfo {
	sm.mentions = append([]string(nil), d.Mentions...)
	if d.Reply == nil && len(d.Mentions) == 0 && !d.MentionAll && !d.MentionAdmins {
		return nil
	}
	m := sm.Message
	ci := &waE2E.ContextInfo{MentionedJID: d.Mentions}
	if d.MentionAll {
		// "@all" is rendered by WhatsApp when nonJIDMentions is set.
		ci.NonJIDMentions = proto.Uint32(1)
		sm.mentions = append(sm.mentions, mentionAll)
	}
	if d.MentionAdmins {
		// The text mentions the group itself, which WhatsApp shows under
		// the given subject; the admins are the mentioned JIDs.
		ci.GroupMentions = []*waE2E.GroupMention{{GroupJID: proto.String(chatID), GroupSubject: proto.String("admin")}}
		sm.mentions = append(sm.mentions, groupMention(chatID, "admin"))
	}
	if r := d.Reply; r != nil {
		m.Quote = b.quote(chatID, r, ci)
		sm.quoteJID, sm.quoteID = m.Quote.SenderID, r.ID
	}
	return ci
}

// maxLocalCopy is the largest sent file kept as a local copy, so it opens
// without downloading it back.
const maxLocalCopy = 100 << 20

// SendFile implements model.Backend.
func (b *Backend) SendFile(chatID string, a model.Attachment, d model.Draft) *model.Message {
	jid, err := types.ParseJID(chatID)
	cli := b.client()
	if err != nil || cli == nil {
		return nil
	}
	st, err := os.Stat(a.Path)
	if err != nil || st.IsDir() {
		b.emit(model.NoticeEvent{Text: "Couldn't read " + filepath.Base(a.Path) + "."})
		return nil
	}
	name := filepath.Base(a.Path)
	m := &model.Message{
		ID:       cli.GenerateMessageID(),
		ChatID:   chatID,
		FromMe:   true,
		Media:    a.Media,
		Text:     d.Text,
		Time:     time.Now(),
		Receipt:  model.Pending,
		FileName: name,
		FileSize: st.Size(),
		FileType: fileType(a),
	}
	if a.Media == model.MediaImage || a.Media == model.MediaVideo {
		m.Album = a.Album
	}
	up := upload{path: a.Path}
	switch a.Media {
	case model.MediaImage:
		m.Kind = model.KindImage
		if err := up.prepareImage(a.Quality); err != nil {
			b.emit(model.NoticeEvent{Text: "Couldn't read the photo " + name + "."})
			return nil
		}
		m.Thumb, m.FileType, m.FileSize = up.thumb, "image/jpeg", int64(len(up.data))
		if up.png {
			m.FileType = "image/png"
		}
	case model.MediaVideo:
		m.Kind = model.KindImage
		m.Duration = mp4Seconds(a.Path)
	case model.MediaAudio:
		m.Duration = audioSeconds(a.Path, m.FileType, st.Size())
	case model.MediaDocument:
		if m.Text == "" {
			m.Text = name
		}
	}
	sm := storedMsg{Message: m}
	ci := b.draftContext(chatID, d, &sm)
	// Keep a copy so your own media shows (and opens) without a download.
	if up.data != nil {
		up.keep(b.mediaPath(chatID, m.ID))
	} else if st.Size() <= maxLocalCopy {
		up.copyTo(b.mediaFilePath(m))
	}
	ctx := b.ctx
	if err := b.store.ensureChat(ctx, b.db, chatID, jid.Server == types.GroupServer, ""); err != nil {
		b.log.Errorf("store chat %s: %v", chatID, err)
	}
	if err := b.store.putMessage(ctx, b.db, sm); err != nil {
		b.log.Errorf("store outgoing file: %v", err)
	}
	b.emitChat(chatID)
	go b.uploadAndSend(jid, sm, up, d.Text, ci, a.ViewOnce)
	if r, ok := b.store.message(ctx, chatID, m.ID); ok {
		return b.resolve(ctx, r, jid.Server == types.GroupServer)
	}
	cp := *m
	return &cp
}

// fileType is the MIME type a file is sent with.
func fileType(a model.Attachment) string {
	ext := strings.ToLower(filepath.Ext(a.Path))
	switch ext {
	case ".mp3":
		return "audio/mpeg"
	case ".m4a", ".aac":
		return "audio/mp4"
	case ".ogg", ".opus", ".oga":
		return "audio/ogg; codecs=opus"
	case ".mp4", ".m4v":
		if a.Media == model.MediaAudio {
			return "audio/mp4"
		}
		return "video/mp4"
	case ".3gp":
		return "video/3gpp"
	}
	if t := mime.TypeByExtension(ext); t != "" {
		return t
	}
	return "application/octet-stream"
}

// upload is a file on its way out. Photos are read, scaled and compressed
// up front (see photo.Prepare); anything else streams from disk.
type upload struct {
	path  string
	data  []byte // the photo to send
	png   bool
	thumb []byte
	w, h  int
}

func (up *upload) prepareImage(q model.Quality) error {
	data, err := os.ReadFile(up.path)
	if err != nil {
		return err
	}
	p, err := photo.Prepare(data, q)
	if err != nil {
		return err
	}
	up.data, up.png, up.thumb, up.w, up.h = p.Data, p.PNG, p.Thumb, p.W, p.H
	return nil
}

// keep writes the photo to the media cache.
func (up *upload) keep(path string) {
	_ = os.MkdirAll(filepath.Dir(path), 0o700)
	_ = os.WriteFile(path, up.data, 0o600)
}

// copyTo copies the file to path.
func (up *upload) copyTo(path string) {
	src, err := os.Open(up.path)
	if err != nil {
		return
	}
	defer src.Close()
	_ = os.MkdirAll(filepath.Dir(path), 0o700)
	dst, err := os.OpenFile(path+".part", os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return
	}
	_, err = io.Copy(dst, src)
	if cerr := dst.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(path+".part", path)
	}
	if err != nil {
		_ = os.Remove(path + ".part")
	}
}

// mediaTypes maps how a file is sent to the key WhatsApp encrypts it with.
var mediaTypes = map[model.Media]whatsmeow.MediaType{
	model.MediaImage:    whatsmeow.MediaImage,
	model.MediaVideo:    whatsmeow.MediaVideo,
	model.MediaAudio:    whatsmeow.MediaAudio,
	model.MediaDocument: whatsmeow.MediaDocument,
}

// uploadAndSend uploads a stored pending file, then sends it.
func (b *Backend) uploadAndSend(jid types.JID, sm storedMsg, up upload, caption string, ci *waE2E.ContextInfo, viewOnce bool) {
	m := sm.Message
	fail := func(err error) {
		b.log.Errorf("send file %s to %s: %v", m.FileName, m.ChatID, err)
		b.emit(model.NoticeEvent{Text: "Couldn't send " + m.FileName + "."})
	}
	cli := b.client()
	if cli == nil {
		fail(errors.New("not logged in"))
		return
	}
	ctx, cancel := context.WithTimeout(b.ctx, 30*time.Minute)
	defer cancel()
	msg, inner, err := uploadMedia(ctx, cli, m, up, caption, ci, viewOnce)
	if err != nil {
		fail(err)
		return
	}
	// Keep the uploaded message, so it can be forwarded, quoted and
	// downloaded again on another device.
	sm.mediaBlob = marshal(inner)
	if err := b.store.putMessage(b.ctx, b.db, sm); err != nil {
		b.log.Errorf("store sent file: %v", err)
	}
	if m.Album != "" {
		inAlbum(msg, jid, m.Album)
		b.waitAlbum(ctx, m.Album)
	}
	b.sendAsync(m.ChatID, jid, m.ID, msg)
}

// uploadMedia uploads the file of a pending message and returns the message
// to send, and the media message inside it to keep for downloads.
func uploadMedia(ctx context.Context, cli *whatsmeow.Client, m *model.Message, up upload, caption string, ci *waE2E.ContextInfo, viewOnce bool) (*waE2E.Message, proto.Message, error) {
	var (
		res whatsmeow.UploadResponse
		err error
	)
	if up.data != nil {
		res, err = cli.Upload(ctx, up.data, mediaTypes[m.Media])
	} else {
		res, err = uploadFile(ctx, cli, up.path, mediaTypes[m.Media])
	}
	if err != nil {
		return nil, nil, err
	}
	var (
		msg   *waE2E.Message
		inner proto.Message
	)
	ts := proto.Int64(time.Now().Unix())
	switch m.Media {
	case model.MediaImage:
		e := &waE2E.ImageMessage{
			URL: proto.String(res.URL), DirectPath: proto.String(res.DirectPath), MediaKey: res.MediaKey,
			FileEncSHA256: res.FileEncSHA256, FileSHA256: res.FileSHA256, FileLength: proto.Uint64(res.FileLength),
			Mimetype: proto.String(m.FileType), Width: proto.Uint32(uint32(up.w)), Height: proto.Uint32(uint32(up.h)),
			JPEGThumbnail: m.Thumb, MediaKeyTimestamp: ts, ContextInfo: ci,
		}
		if caption != "" {
			e.Caption = proto.String(caption)
		}
		if viewOnce {
			e.ViewOnce = proto.Bool(true)
		}
		msg, inner = &waE2E.Message{ImageMessage: e}, e
	case model.MediaVideo:
		e := &waE2E.VideoMessage{
			URL: proto.String(res.URL), DirectPath: proto.String(res.DirectPath), MediaKey: res.MediaKey,
			FileEncSHA256: res.FileEncSHA256, FileSHA256: res.FileSHA256, FileLength: proto.Uint64(res.FileLength),
			Mimetype: proto.String(m.FileType), Seconds: proto.Uint32(uint32(m.Duration)),
			MediaKeyTimestamp: ts, ContextInfo: ci,
		}
		if caption != "" {
			e.Caption = proto.String(caption)
		}
		if viewOnce {
			e.ViewOnce = proto.Bool(true)
		}
		msg, inner = &waE2E.Message{VideoMessage: e}, e
	case model.MediaAudio:
		e := &waE2E.AudioMessage{
			URL: proto.String(res.URL), DirectPath: proto.String(res.DirectPath), MediaKey: res.MediaKey,
			FileEncSHA256: res.FileEncSHA256, FileSHA256: res.FileSHA256, FileLength: proto.Uint64(res.FileLength),
			Mimetype: proto.String(m.FileType), Seconds: proto.Uint32(uint32(m.Duration)),
			MediaKeyTimestamp: ts, ContextInfo: ci,
		}
		msg, inner = &waE2E.Message{AudioMessage: e}, e
	default:
		e := &waE2E.DocumentMessage{
			URL: proto.String(res.URL), DirectPath: proto.String(res.DirectPath), MediaKey: res.MediaKey,
			FileEncSHA256: res.FileEncSHA256, FileSHA256: res.FileSHA256, FileLength: proto.Uint64(res.FileLength),
			Mimetype: proto.String(m.FileType), FileName: proto.String(m.FileName), Title: proto.String(m.FileName),
			MediaKeyTimestamp: ts, ContextInfo: ci,
		}
		msg, inner = &waE2E.Message{DocumentMessage: e}, e
		if caption != "" {
			e.Caption = proto.String(caption)
			msg = &waE2E.Message{DocumentWithCaptionMessage: &waE2E.FutureProofMessage{Message: msg}}
		}
	}
	if viewOnce && (msg.ImageMessage != nil || msg.VideoMessage != nil) {
		msg = &waE2E.Message{ViewOnceMessageV2: &waE2E.FutureProofMessage{Message: msg}}
	}
	if ci != nil && len(ci.GroupMentions) > 0 {
		msg = &waE2E.Message{GroupMentionedMessage: &waE2E.FutureProofMessage{Message: msg}}
	}
	return msg, inner, nil
}

// uploadFile uploads a file from disk, encrypting it through a temporary
// file rather than in memory.
func uploadFile(ctx context.Context, cli *whatsmeow.Client, path string, mt whatsmeow.MediaType) (whatsmeow.UploadResponse, error) {
	f, err := os.Open(path)
	if err != nil {
		return whatsmeow.UploadResponse{}, err
	}
	defer f.Close()
	tmp, err := os.CreateTemp("", "wazzap-upload-*")
	if err != nil {
		return whatsmeow.UploadResponse{}, err
	}
	defer func() {
		tmp.Close()
		os.Remove(tmp.Name())
	}()
	return cli.UploadReader(ctx, f, tmp, mt)
}

// SendContacts implements model.Backend.
func (b *Backend) SendContacts(chatID string, contactIDs []string) *model.Message {
	jid, err := types.ParseJID(chatID)
	cli := b.client()
	if err != nil || cli == nil || len(contactIDs) == 0 {
		return nil
	}
	ctx := b.ctx
	var cards []*waE2E.ContactMessage
	for _, id := range contactIDs {
		cj, err := types.ParseJID(id)
		if err != nil {
			continue
		}
		name := b.chatName(ctx, cj)
		pn := cj
		if cj.Server == types.HiddenUserServer {
			if p, err := cli.Store.LIDs.GetPNForLID(ctx, cj); err == nil && !p.IsEmpty() {
				pn = p
			} else {
				continue // without a phone number there is nothing to share
			}
		}
		cards = append(cards, &waE2E.ContactMessage{DisplayName: proto.String(name), Vcard: proto.String(vcard(name, pn.User))})
	}
	if len(cards) == 0 {
		b.emit(model.NoticeEvent{Text: "Couldn't find a phone number to share."})
		return nil
	}
	m := &model.Message{ID: cli.GenerateMessageID(), ChatID: chatID, FromMe: true, Media: model.MediaContact,
		Text: cards[0].GetDisplayName(), Time: time.Now(), Receipt: model.Pending}
	msg := &waE2E.Message{ContactMessage: cards[0]}
	if len(cards) > 1 {
		m.Text = fmt.Sprintf("%s and %d other contact", m.Text, len(cards)-1)
		if len(cards) > 2 {
			m.Text += "s"
		}
		msg = &waE2E.Message{ContactsArrayMessage: &waE2E.ContactsArrayMessage{DisplayName: proto.String(m.Text), Contacts: cards}}
	}
	return b.storeAndSend(jid, storedMsg{Message: m}, msg, nil)
}

// vcard is the contact card WhatsApp sends: the waid parameter makes the
// number tappable in WhatsApp.
func vcard(name, user string) string {
	esc := strings.NewReplacer(`\`, `\\`, ",", `\,`, ";", `\;`, "\n", `\n`).Replace(name)
	return "BEGIN:VCARD\nVERSION:3.0\nN:;" + esc + ";;;\nFN:" + esc + "\nTEL;type=CELL;type=VOICE;waid=" + user + ":" +
		formatPhone(user) + "\nEND:VCARD"
}

// SendPoll implements model.Backend.
func (b *Backend) SendPoll(chatID string, p model.Poll) *model.Message {
	jid, err := types.ParseJID(chatID)
	cli := b.client()
	if err != nil || cli == nil {
		return nil
	}
	n := 1
	if p.Multiple {
		n = 0 // any number
	}
	msg := cli.BuildPollCreation(p.Question, p.Options, n)
	m := &model.Message{ID: cli.GenerateMessageID(), ChatID: chatID, FromMe: true, Media: model.MediaPoll,
		Text: p.Question, Time: time.Now(), Receipt: model.Pending}
	return b.storeAndSend(jid, storedMsg{Message: m}, msg, nil)
}

// mp4Seconds reads the length of an MP4 (or M4A, 3GP) file from its movie
// header, or returns 0.
func mp4Seconds(path string) int {
	f, err := os.Open(path)
	if err != nil {
		return 0
	}
	defer f.Close()
	// Walk the top-level boxes to moov, then its children to mvhd.
	var walk func(r io.ReadSeeker, end int64, want []string) int
	walk = func(r io.ReadSeeker, end int64, want []string) int {
		var hdr [8]byte
		for {
			pos, _ := r.Seek(0, io.SeekCurrent)
			if end >= 0 && pos+8 > end {
				return 0
			}
			if _, err := io.ReadFull(r, hdr[:]); err != nil {
				return 0
			}
			size := int64(binary.BigEndian.Uint32(hdr[:4]))
			typ := string(hdr[4:])
			body := pos + 8
			if size == 1 {
				var ext [8]byte
				if _, err := io.ReadFull(r, ext[:]); err != nil {
					return 0
				}
				size = int64(binary.BigEndian.Uint64(ext[:]))
				body += 8
			}
			if size < 8 && size != 0 {
				return 0
			}
			if typ == want[0] {
				if len(want) > 1 {
					boxEnd := int64(-1)
					if size != 0 {
						boxEnd = pos + size
					}
					return walk(r, boxEnd, want[1:])
				}
				var v [32]byte
				if _, err := io.ReadFull(r, v[:]); err != nil {
					return 0
				}
				var scale, dur uint64
				if v[0] == 1 { // version 1: 64-bit times
					scale, dur = uint64(binary.BigEndian.Uint32(v[20:24])), binary.BigEndian.Uint64(v[24:32])
				} else {
					scale, dur = uint64(binary.BigEndian.Uint32(v[12:16])), uint64(binary.BigEndian.Uint32(v[16:20]))
				}
				if scale == 0 {
					return 0
				}
				return int((dur + scale/2) / scale)
			}
			if size == 0 {
				return 0 // the last box, and not the one we want
			}
			if _, err := r.Seek(pos+size, io.SeekStart); err != nil {
				return 0
			}
		}
	}
	return walk(f, -1, []string{"moov", "mvhd"})
}

// audioSeconds estimates the length of an audio file: from the MP4 header,
// the last Ogg page, or an MP3's first frame (exact for constant bitrate).
func audioSeconds(path, typ string, size int64) int {
	switch {
	case strings.HasPrefix(typ, "audio/mp4"):
		return mp4Seconds(path)
	case strings.HasPrefix(typ, "audio/ogg"):
		return oggSeconds(path, size)
	case typ == "audio/mpeg":
		return mp3Seconds(path, size)
	}
	return 0
}

// oggSeconds reads the granule position of the last Ogg page, which counts
// 48 kHz samples for Opus.
func oggSeconds(path string, size int64) int {
	f, err := os.Open(path)
	if err != nil {
		return 0
	}
	defer f.Close()
	n := min(size, 64<<10)
	buf := make([]byte, n)
	if _, err := f.ReadAt(buf, size-n); err != nil && err != io.EOF {
		return 0
	}
	i := bytes.LastIndex(buf, []byte("OggS"))
	if i < 0 || i+14 > len(buf) {
		return 0
	}
	granule := binary.LittleEndian.Uint64(buf[i+6 : i+14])
	return int((granule + 24000) / 48000)
}

// mp3Seconds divides the file's size by the bitrate of its first frame.
func mp3Seconds(path string, size int64) int {
	f, err := os.Open(path)
	if err != nil {
		return 0
	}
	defer f.Close()
	buf := make([]byte, 64<<10)
	n, _ := io.ReadFull(f, buf)
	buf = buf[:n]
	start := 0
	if len(buf) > 10 && string(buf[:3]) == "ID3" {
		start = 10 + (int(buf[6])<<21 | int(buf[7])<<14 | int(buf[8])<<7 | int(buf[9]))
	}
	// MPEG-1 Layer III bitrates in kbit/s.
	rates := [16]int{0, 32, 40, 48, 56, 64, 80, 96, 112, 128, 160, 192, 224, 256, 320, 0}
	for i := start; i+4 <= len(buf); i++ {
		if buf[i] != 0xff || buf[i+1]&0xfe != 0xfa { // sync, MPEG-1, Layer III
			continue
		}
		if r := rates[buf[i+2]>>4]; r > 0 {
			return int((size - int64(start)) * 8 / int64(r*1000))
		}
	}
	return 0
}
