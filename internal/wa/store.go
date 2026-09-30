package wa

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

// hypermeow (like whatsmeow) only stores keys and session state, not
// messages. msgStore keeps chats and messages in the same SQLite file so the
// app can show history after a restart.
//
// Display names are not stored with messages: contact and push names often
// arrive after the messages themselves (history sync stores them in the
// background), so they are resolved whenever messages are loaded.
type msgStore struct {
	db *sql.DB
}

type execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

const schema = `
CREATE TABLE IF NOT EXISTS wz_chats (
	jid         TEXT PRIMARY KEY,
	name        TEXT NOT NULL DEFAULT '',
	is_group    INTEGER NOT NULL DEFAULT 0,
	pinned      INTEGER NOT NULL DEFAULT 0, -- pin timestamp, 0 = not pinned
	muted_until INTEGER NOT NULL DEFAULT 0, -- unix seconds, -1 = forever
	archived    INTEGER NOT NULL DEFAULT 0,
	unread      INTEGER NOT NULL DEFAULT 0,
	last_ts     INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS wz_messages (
	chat         TEXT NOT NULL,
	id           TEXT NOT NULL,
	sender_jid   TEXT NOT NULL DEFAULT '',
	sender_name  TEXT NOT NULL DEFAULT '',
	from_me      INTEGER NOT NULL DEFAULT 0,
	ts           INTEGER NOT NULL,
	kind         INTEGER NOT NULL DEFAULT 0,
	text         TEXT NOT NULL DEFAULT '',
	receipt      INTEGER NOT NULL DEFAULT 0,
	quote_sender TEXT NOT NULL DEFAULT '',
	quote_text   TEXT NOT NULL DEFAULT '',
	reaction     TEXT NOT NULL DEFAULT '',
	thumb        BLOB,
	PRIMARY KEY (chat, id)
);
CREATE INDEX IF NOT EXISTS wz_messages_chat_ts ON wz_messages (chat, ts);
CREATE TABLE IF NOT EXISTS wz_meta (
	key   TEXT PRIMARY KEY,
	value TEXT NOT NULL
);
`

// migrations add columns to databases created by older versions.
var migrations = []string{
	`ALTER TABLE wz_messages ADD COLUMN sender_push TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE wz_messages ADD COLUMN media INTEGER NOT NULL DEFAULT 0`,
	`ALTER TABLE wz_messages ADD COLUMN media_blob BLOB`,
	`ALTER TABLE wz_messages ADD COLUMN duration INTEGER NOT NULL DEFAULT 0`,
	`ALTER TABLE wz_messages ADD COLUMN mentions TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE wz_messages ADD COLUMN quote_media INTEGER NOT NULL DEFAULT 0`,
}

func (s *msgStore) init(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, schema); err != nil {
		return err
	}
	for _, m := range migrations {
		if _, err := s.db.ExecContext(ctx, m); err != nil && !strings.Contains(err.Error(), "duplicate column") {
			return err
		}
	}
	return s.migrateLegacyMedia(ctx)
}

// migrateLegacyMedia converts media rows written by the first version,
// which stored attachments as emoji-prefixed text ("📄 file.pdf"), into
// typed media so they render with proper icons.
func (s *msgStore) migrateLegacyMedia(ctx context.Context) error {
	const key = "legacy_media_migrated"
	if s.meta(ctx, key) != "" {
		return nil
	}
	m := func(media model.Media) int { return int(media) }
	stmts := []struct {
		q    string
		args []any
	}{
		{`UPDATE wz_messages SET media = ?, text = '' WHERE media = 0 AND kind = 0 AND text = 'Sticker'`, []any{m(model.MediaSticker)}},
		{`UPDATE wz_messages SET media = ?, text = substr(text, 3) WHERE media = 0 AND text LIKE '📄 %'`, []any{m(model.MediaDocument)}},
		{`UPDATE wz_messages SET media = ?, text = '' WHERE media = 0 AND text LIKE '🎤 Voice message%'`, []any{m(model.MediaVoice)}},
		{`UPDATE wz_messages SET media = ?, text = '' WHERE media = 0 AND text = '🎵 Audio'`, []any{m(model.MediaAudio)}},
		{`UPDATE wz_messages SET media = ?, text = CASE WHEN text = '🎥 Video' THEN '' ELSE substr(text, 3) END
			WHERE media = 0 AND text LIKE '🎥 %'`, []any{m(model.MediaVideo)}},
		{`UPDATE wz_messages SET media = ?, text = CASE WHEN text = '📍 Location' THEN '' ELSE substr(text, 3) END
			WHERE media = 0 AND text LIKE '📍 %'`, []any{m(model.MediaLocation)}},
		{`UPDATE wz_messages SET media = ?, text = substr(text, 3) WHERE media = 0 AND text LIKE '👤 %'`, []any{m(model.MediaContact)}},
		{`UPDATE wz_messages SET media = ?, text = substr(text, 3) WHERE media = 0 AND text LIKE '📊 %'`, []any{m(model.MediaPoll)}},
		{`UPDATE wz_messages SET media = ? WHERE media = 0 AND kind = ?`, []any{m(model.MediaImage), int(model.KindImage)}},
	}
	for _, st := range stmts {
		if _, err := s.db.ExecContext(ctx, st.q, st.args...); err != nil {
			return fmt.Errorf("migrate legacy media: %w", err)
		}
	}
	return s.setMetaValue(ctx, key, time.Now().Format(time.RFC3339))
}

func (s *msgStore) wipe(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM wz_messages; DELETE FROM wz_chats; DELETE FROM wz_meta;`)
	return err
}

func (s *msgStore) meta(ctx context.Context, key string) string {
	var v string
	_ = s.db.QueryRowContext(ctx, `SELECT value FROM wz_meta WHERE key = ?`, key).Scan(&v)
	return v
}

func (s *msgStore) setMetaValue(ctx context.Context, key, value string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO wz_meta (key, value) VALUES (?, ?)
		ON CONFLICT (key) DO UPDATE SET value = excluded.value`, key, value)
	return err
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// ensureChat creates the chat if needed and fills in its name if it has none.
func (s *msgStore) ensureChat(ctx context.Context, x execer, jid string, isGroup bool, name string) error {
	_, err := x.ExecContext(ctx, `
		INSERT INTO wz_chats (jid, name, is_group) VALUES (?, ?, ?)
		ON CONFLICT (jid) DO UPDATE SET name = excluded.name
		WHERE wz_chats.name = '' AND excluded.name <> ''`,
		jid, name, boolInt(isGroup))
	return err
}

func (s *msgStore) setName(ctx context.Context, jid, name string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE wz_chats SET name = ? WHERE jid = ?`, name, jid)
	return err
}

// chatMeta is the chat state carried by a history sync conversation.
type chatMeta struct {
	pinned, mutedUntil, lastTS int64
	archived                   bool
	unread                     int
}

func (s *msgStore) setMeta(ctx context.Context, x execer, jid string, m chatMeta) error {
	_, err := x.ExecContext(ctx, `
		UPDATE wz_chats SET pinned = ?, muted_until = ?, archived = ?, unread = ?, last_ts = MAX(last_ts, ?)
		WHERE jid = ?`,
		m.pinned, m.mutedUntil, boolInt(m.archived), m.unread, m.lastTS, jid)
	return err
}

func (s *msgStore) setField(ctx context.Context, jid, field string, v any) error {
	// field is always a constant from this package.
	_, err := s.db.ExecContext(ctx, `UPDATE wz_chats SET `+field+` = ? WHERE jid = ?`, v, jid)
	return err
}

func (s *msgStore) addUnread(ctx context.Context, jid string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE wz_chats SET unread = unread + 1 WHERE jid = ?`, jid)
	return err
}

// storedMsg is a message plus the raw data names are resolved from at load
// time, and what's needed to download its media later.
type storedMsg struct {
	*model.Message
	senderJID  string
	senderPush string
	quoteJID   string // author of the quoted message
	mentions   []string
	mediaBlob  []byte // marshaled waE2E media message
}

func (s *msgStore) putMessage(ctx context.Context, x execer, m storedMsg) error {
	var qt string
	var qm int
	if m.Quote != nil {
		qt, qm = m.Quote.Text, int(m.Quote.Media)
	}
	_, err := x.ExecContext(ctx, `
		INSERT INTO wz_messages (chat, id, sender_jid, sender_push, from_me, ts, kind, media, duration, text,
			receipt, quote_sender, quote_text, quote_media, mentions, thumb, media_blob)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (chat, id) DO UPDATE SET
			sender_push = excluded.sender_push, kind = excluded.kind, media = excluded.media,
			duration = excluded.duration, text = excluded.text,
			receipt = MAX(wz_messages.receipt, excluded.receipt),
			quote_sender = excluded.quote_sender, quote_text = excluded.quote_text,
			quote_media = excluded.quote_media, mentions = excluded.mentions,
			thumb = COALESCE(excluded.thumb, wz_messages.thumb),
			media_blob = COALESCE(excluded.media_blob, wz_messages.media_blob)`,
		m.ChatID, m.ID, m.senderJID, m.senderPush, boolInt(m.FromMe), m.Time.Unix(), int(m.Kind), int(m.Media),
		m.Duration, m.Text, int(m.Receipt), m.quoteJID, qt, qm, strings.Join(m.mentions, ","), m.Thumb, m.mediaBlob)
	if err != nil {
		return err
	}
	_, err = x.ExecContext(ctx, `UPDATE wz_chats SET last_ts = MAX(last_ts, ?) WHERE jid = ?`, m.Time.Unix(), m.ChatID)
	return err
}

func placeholders(n int) string {
	return strings.TrimSuffix(strings.Repeat("?,", n), ",")
}

func (s *msgStore) setReceipt(ctx context.Context, chat string, ids []string, r model.Receipt) error {
	if len(ids) == 0 {
		return nil
	}
	args := []any{int(r), chat}
	for _, id := range ids {
		args = append(args, id)
	}
	args = append(args, int(r))
	_, err := s.db.ExecContext(ctx, `UPDATE wz_messages SET receipt = ? WHERE chat = ? AND id IN (`+
		placeholders(len(ids))+`) AND receipt < ?`, args...)
	return err
}

func (s *msgStore) setReaction(ctx context.Context, chat, id, emoji string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE wz_messages SET reaction = ? WHERE chat = ? AND id = ?`, emoji, chat, id)
	return err
}

func (s *msgStore) markDeleted(ctx context.Context, chat, id string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE wz_messages SET kind = ?, media = 0, text = '', thumb = NULL, media_blob = NULL,
		quote_sender = '', quote_text = '', quote_media = 0 WHERE chat = ? AND id = ?`, int(model.KindDeleted), chat, id)
	return err
}

func (s *msgStore) editText(ctx context.Context, chat, id, text string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE wz_messages SET text = ? WHERE chat = ? AND id = ?`, text, chat, id)
	return err
}

func (s *msgStore) mediaBlob(ctx context.Context, chat, id string) (media model.Media, blob []byte, err error) {
	err = s.db.QueryRowContext(ctx, `SELECT media, media_blob FROM wz_messages WHERE chat = ? AND id = ?`, chat, id).
		Scan(&media, &blob)
	return
}

// rawMsg is a loaded message before names are resolved. legacyName is the
// sender name stored by versions that resolved names at insert time.
type rawMsg struct {
	*model.Message
	senderJID, senderPush, legacyName, quoteJID, mentions string
}

const msgColumns = `chat, id, sender_jid, sender_push, sender_name, from_me, ts, kind, media, duration, text, receipt,
	quote_sender, quote_text, quote_media, mentions, reaction, thumb`

type scanner interface{ Scan(dest ...any) error }

func scanMessage(sc scanner) (rawMsg, error) {
	var (
		m                   model.Message
		r                   rawMsg
		fromMe, kind, media int
		receipt             int
		ts                  int64
		quoteText           string
		quoteMedia          int
		thumb               []byte
		reaction            string
	)
	err := sc.Scan(&m.ChatID, &m.ID, &r.senderJID, &r.senderPush, &r.legacyName, &fromMe, &ts, &kind, &media, &m.Duration,
		&m.Text, &receipt, &r.quoteJID, &quoteText, &quoteMedia, &r.mentions, &reaction, &thumb)
	if err != nil {
		return r, err
	}
	m.FromMe = fromMe != 0
	m.Time = time.Unix(ts, 0)
	m.Kind = model.Kind(kind)
	m.Media = model.Media(media)
	m.Receipt = model.Receipt(receipt)
	m.Reaction = reaction
	m.Thumb = thumb
	m.SenderID = r.senderJID
	if quoteText != "" || r.quoteJID != "" || quoteMedia != 0 {
		m.Quote = &model.Quote{Text: quoteText, Media: model.Media(quoteMedia)}
	}
	r.Message = &m
	return r, nil
}

func (s *msgStore) message(ctx context.Context, chat, id string) (rawMsg, bool) {
	row := s.db.QueryRowContext(ctx, `SELECT `+msgColumns+` FROM wz_messages WHERE chat = ? AND id = ?`, chat, id)
	m, err := scanMessage(row)
	return m, err == nil
}

// messages returns the newest limit messages of a chat, oldest first.
func (s *msgStore) messages(ctx context.Context, chat string, limit int) ([]rawMsg, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+msgColumns+` FROM (
		SELECT *, rowid AS rid FROM wz_messages WHERE chat = ? ORDER BY ts DESC, rid DESC LIMIT ?
	) ORDER BY ts, rid`, chat, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []rawMsg
	for rows.Next() {
		m, err := scanMessage(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// rawChat is a loaded chat before names are resolved.
type rawChat struct {
	*model.Chat
	last *rawMsg
}

const chatQuery = `
	SELECT c.jid, c.name, c.is_group, c.pinned, c.muted_until, c.archived, c.unread, c.last_ts,
		m.id, m.sender_jid, m.sender_push, m.sender_name, m.from_me, m.ts, m.kind, m.media, m.duration, m.text, m.receipt, m.mentions
	FROM wz_chats c
	LEFT JOIN wz_messages m ON m.rowid = (
		SELECT rowid FROM wz_messages WHERE chat = c.jid ORDER BY ts DESC, rowid DESC LIMIT 1
	)`

func scanChat(sc scanner, now time.Time) (rawChat, error) {
	var (
		c                                 model.Chat
		isGroup, archived, unread         int
		pinned, mutedUntil, lastTS        int64
		mID, mSender, mPush, mText, mMent sql.NullString
		mLegacy                           sql.NullString
		mFromMe, mTS, mKind, mMedia, mDur sql.NullInt64
		mReceipt                          sql.NullInt64
	)
	err := sc.Scan(&c.ID, &c.Name, &isGroup, &pinned, &mutedUntil, &archived, &unread, &lastTS,
		&mID, &mSender, &mPush, &mLegacy, &mFromMe, &mTS, &mKind, &mMedia, &mDur, &mText, &mReceipt, &mMent)
	if err != nil {
		return rawChat{}, err
	}
	c.IsGroup = isGroup != 0
	c.Pinned = pinned > 0
	c.Muted = mutedUntil == -1 || mutedUntil > now.Unix()
	c.Archived = archived != 0
	c.Unread = unread
	c.Time = time.Unix(lastTS, 0)
	rc := rawChat{Chat: &c}
	if mID.Valid {
		m := &model.Message{
			ID: mID.String, ChatID: c.ID, SenderID: mSender.String, FromMe: mFromMe.Int64 != 0,
			Time: time.Unix(mTS.Int64, 0), Kind: model.Kind(mKind.Int64), Media: model.Media(mMedia.Int64),
			Duration: int(mDur.Int64), Text: mText.String, Receipt: model.Receipt(mReceipt.Int64),
		}
		c.Last = m
		rc.last = &rawMsg{Message: m, senderJID: mSender.String, senderPush: mPush.String,
			legacyName: mLegacy.String, mentions: mMent.String}
		if m.Time.After(c.Time) {
			c.Time = m.Time
		}
	}
	return rc, nil
}

// chats lists chats that have any activity, newest first.
func (s *msgStore) chats(ctx context.Context) ([]rawChat, error) {
	rows, err := s.db.QueryContext(ctx, chatQuery+` WHERE c.last_ts > 0 OR m.id IS NOT NULL
		ORDER BY c.pinned DESC, MAX(c.last_ts, COALESCE(m.ts, 0)) DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	now := time.Now()
	var out []rawChat
	for rows.Next() {
		c, err := scanChat(rows, now)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *msgStore) chat(ctx context.Context, jid string) (rawChat, bool) {
	c, err := scanChat(s.db.QueryRowContext(ctx, chatQuery+` WHERE c.jid = ?`, jid), time.Now())
	return c, err == nil
}

func (s *msgStore) chatJIDs(ctx context.Context, groups bool) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT jid FROM wz_chats WHERE is_group = ?`, boolInt(groups))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var j string
		if err := rows.Scan(&j); err != nil {
			return nil, err
		}
		out = append(out, j)
	}
	return out, rows.Err()
}

// unreadIncoming returns the newest n incoming messages, for read receipts.
func (s *msgStore) unreadIncoming(ctx context.Context, chat string, n int) (ids, senders []string, err error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, sender_jid FROM wz_messages
		WHERE chat = ? AND from_me = 0 ORDER BY ts DESC LIMIT ?`, chat, n)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id, sender string
		if err := rows.Scan(&id, &sender); err != nil {
			return nil, nil, err
		}
		ids, senders = append(ids, id), append(senders, sender)
	}
	return ids, senders, rows.Err()
}
