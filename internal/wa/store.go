package wa

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

// hypermeow (like whatsmeow) only stores keys and session state, not
// messages. msgStore keeps chats and messages in the same SQLite file so the
// app can show history after a restart.
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
`

func (s *msgStore) init(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, schema)
	return err
}

func (s *msgStore) wipe(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM wz_messages; DELETE FROM wz_chats;`)
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

// storedMsg is a message plus the raw sender JID needed for read receipts.
type storedMsg struct {
	*model.Message
	senderJID string
}

func (s *msgStore) putMessage(ctx context.Context, x execer, m storedMsg) error {
	var qs, qt string
	if m.Quote != nil {
		qs, qt = m.Quote.Sender, m.Quote.Text
	}
	_, err := x.ExecContext(ctx, `
		INSERT INTO wz_messages (chat, id, sender_jid, sender_name, from_me, ts, kind, text, receipt, quote_sender, quote_text, thumb)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (chat, id) DO UPDATE SET
			sender_name = excluded.sender_name, kind = excluded.kind, text = excluded.text,
			receipt = MAX(wz_messages.receipt, excluded.receipt),
			quote_sender = excluded.quote_sender, quote_text = excluded.quote_text,
			thumb = COALESCE(excluded.thumb, wz_messages.thumb)`,
		m.ChatID, m.ID, m.senderJID, m.Sender, boolInt(m.FromMe), m.Time.Unix(), int(m.Kind), m.Text,
		int(m.Receipt), qs, qt, m.Thumb)
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
	_, err := s.db.ExecContext(ctx, `UPDATE wz_messages SET kind = ?, text = '', thumb = NULL, quote_sender = '', quote_text = ''
		WHERE chat = ? AND id = ?`, int(model.KindDeleted), chat, id)
	return err
}

func (s *msgStore) editText(ctx context.Context, chat, id, text string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE wz_messages SET text = ? WHERE chat = ? AND id = ?`, text, chat, id)
	return err
}

const msgColumns = `chat, id, sender_name, from_me, ts, kind, text, receipt, quote_sender, quote_text, reaction, thumb`

type scanner interface{ Scan(dest ...any) error }

func scanMessage(sc scanner) (*model.Message, error) {
	var (
		m        model.Message
		fromMe   int
		ts       int64
		kind     int
		receipt  int
		qs, qt   string
		thumb    []byte
		reaction string
	)
	err := sc.Scan(&m.ChatID, &m.ID, &m.Sender, &fromMe, &ts, &kind, &m.Text, &receipt, &qs, &qt, &reaction, &thumb)
	if err != nil {
		return nil, err
	}
	m.FromMe = fromMe != 0
	m.Time = time.Unix(ts, 0)
	m.Kind = model.Kind(kind)
	m.Receipt = model.Receipt(receipt)
	m.Reaction = reaction
	m.Thumb = thumb
	if qs != "" || qt != "" {
		m.Quote = &model.Quote{Sender: qs, Text: qt}
	}
	return &m, nil
}

func (s *msgStore) message(ctx context.Context, chat, id string) *model.Message {
	row := s.db.QueryRowContext(ctx, `SELECT `+msgColumns+` FROM wz_messages WHERE chat = ? AND id = ?`, chat, id)
	m, err := scanMessage(row)
	if err != nil {
		return nil
	}
	return m
}

// messages returns the newest limit messages of a chat, oldest first.
func (s *msgStore) messages(ctx context.Context, chat string, limit int) ([]*model.Message, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+msgColumns+` FROM (
		SELECT *, rowid AS rid FROM wz_messages WHERE chat = ? ORDER BY ts DESC, rid DESC LIMIT ?
	) ORDER BY ts, rid`, chat, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*model.Message
	for rows.Next() {
		m, err := scanMessage(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

const chatQuery = `
	SELECT c.jid, c.name, c.is_group, c.pinned, c.muted_until, c.archived, c.unread, c.last_ts,
		m.chat, m.id, m.sender_name, m.from_me, m.ts, m.kind, m.text, m.receipt, m.quote_sender, m.quote_text, m.reaction, NULL
	FROM wz_chats c
	LEFT JOIN wz_messages m ON m.rowid = (
		SELECT rowid FROM wz_messages WHERE chat = c.jid ORDER BY ts DESC, rowid DESC LIMIT 1
	)`

func scanChat(sc scanner, now time.Time) (*model.Chat, error) {
	var (
		c                             model.Chat
		isGroup, archived, unread     int
		pinned, mutedUntil, lastTS    int64
		mChat, mID, mSender, mText    sql.NullString
		mQS, mQT, mReaction           sql.NullString
		mFromMe, mTS, mKind, mReceipt sql.NullInt64
		thumb                         []byte
	)
	err := sc.Scan(&c.ID, &c.Name, &isGroup, &pinned, &mutedUntil, &archived, &unread, &lastTS,
		&mChat, &mID, &mSender, &mFromMe, &mTS, &mKind, &mText, &mReceipt, &mQS, &mQT, &mReaction, &thumb)
	if err != nil {
		return nil, err
	}
	c.IsGroup = isGroup != 0
	c.Pinned = pinned > 0
	c.Muted = mutedUntil == -1 || mutedUntil > now.Unix()
	c.Archived = archived != 0
	c.Unread = unread
	c.Time = time.Unix(lastTS, 0)
	if mID.Valid {
		c.Last = &model.Message{
			ID: mID.String, ChatID: c.ID, Sender: mSender.String, FromMe: mFromMe.Int64 != 0,
			Time: time.Unix(mTS.Int64, 0), Kind: model.Kind(mKind.Int64), Text: mText.String,
			Receipt: model.Receipt(mReceipt.Int64), Reaction: mReaction.String,
		}
		if c.Last.Time.After(c.Time) {
			c.Time = c.Last.Time
		}
	}
	return &c, nil
}

// chats lists chats that have any activity, newest first.
func (s *msgStore) chats(ctx context.Context) ([]*model.Chat, error) {
	rows, err := s.db.QueryContext(ctx, chatQuery+` WHERE c.last_ts > 0 OR m.id IS NOT NULL
		ORDER BY c.pinned DESC, MAX(c.last_ts, COALESCE(m.ts, 0)) DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	now := time.Now()
	var out []*model.Chat
	for rows.Next() {
		c, err := scanChat(rows, now)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *msgStore) chat(ctx context.Context, jid string) *model.Chat {
	c, err := scanChat(s.db.QueryRowContext(ctx, chatQuery+` WHERE c.jid = ?`, jid), time.Now())
	if err != nil {
		return nil
	}
	return c
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
