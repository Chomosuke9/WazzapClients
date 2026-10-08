package wa

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/polymorfa/hypermeow/proto/waE2E"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

// hypermeow (like whatsmeow) only stores keys and session state, not
// messages. msgStore keeps chats and messages in the same SQLite file so the
// app can show history after a restart. A message is kept as its payload
// (see payload.go).
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
	-- The envelope, which the payload doesn't hold.
	sender_jid   TEXT NOT NULL DEFAULT '',
	sender_push  TEXT NOT NULL DEFAULT '',
	from_me      INTEGER NOT NULL DEFAULT 0,
	ts           INTEGER NOT NULL,
	-- What the payload is, for queries: model.Kind and model.Media as
	-- parse decided, and the text as last edited.
	kind         INTEGER NOT NULL DEFAULT 0,
	media        INTEGER NOT NULL DEFAULT 0,
	text         TEXT NOT NULL DEFAULT '',
	-- What happened to it since.
	receipt      INTEGER NOT NULL DEFAULT 0, -- model.Receipt
	reaction     TEXT NOT NULL DEFAULT '',   -- reactionSummary of its wz_reactions
	starred      INTEGER NOT NULL DEFAULT 0,
	pinned       INTEGER NOT NULL DEFAULT 0, -- when, 0 = not pinned
	edited       INTEGER NOT NULL DEFAULT 0, -- unix ms of the last edit, 0 = never
	-- Unix ms its sender deleted it for everyone, for a message kept with
	-- model.PrefKeepDeleted; 0 otherwise.
	revoked      INTEGER NOT NULL DEFAULT 0,
	-- The group admin who deleted it for everyone, when that wasn't its
	-- sender (kept or not); '' otherwise.
	revoked_by   TEXT NOT NULL DEFAULT '',
	opened       INTEGER NOT NULL DEFAULT 0, -- 1 once a view once message was opened here
	-- The JIDs a /whisper went to, newline-joined; '' on every other
	-- message. It marks the local-only record a whisper leaves behind.
	whisper      TEXT NOT NULL DEFAULT '',
	-- The message itself, last so that reading the columns above skips it:
	-- the waE2E message it came or went as (none for a view once message
	-- whose media never came), and the content of its latest edit.
	raw_payload  BLOB,
	edit_payload BLOB,
	PRIMARY KEY (chat, id)
);
CREATE INDEX IF NOT EXISTS wz_messages_chat_ts ON wz_messages (chat, ts);
CREATE INDEX IF NOT EXISTS wz_messages_pinned ON wz_messages (chat, pinned) WHERE pinned != 0;
-- For lastPush.
CREATE INDEX IF NOT EXISTS wz_messages_sender ON wz_messages (sender_jid, ts) WHERE sender_push != '';
-- For the Media panel across all chats.
CREATE INDEX IF NOT EXISTS wz_messages_media ON wz_messages (media, ts) WHERE media != 0;
CREATE INDEX IF NOT EXISTS wz_messages_starred ON wz_messages (ts) WHERE starred != 0;
-- For failStale.
CREATE INDEX IF NOT EXISTS wz_messages_pending ON wz_messages (chat) WHERE from_me = 1 AND receipt = 0;
CREATE TABLE IF NOT EXISTS wz_meta (
	key   TEXT PRIMARY KEY,
	value TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS wz_status (
	id         TEXT PRIMARY KEY,
	sender     TEXT NOT NULL,
	push       TEXT NOT NULL DEFAULT '',
	from_me    INTEGER NOT NULL DEFAULT 0,
	ts         INTEGER NOT NULL,
	media      INTEGER NOT NULL DEFAULT 0,
	text       TEXT NOT NULL DEFAULT '',
	bg         INTEGER NOT NULL DEFAULT 0, -- ARGB behind text statuses
	thumb      BLOB,
	media_blob BLOB,
	viewed     INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS wz_status_ts ON wz_status (ts);
CREATE TABLE IF NOT EXISTS wz_channels (
	jid       TEXT PRIMARY KEY,
	name      TEXT NOT NULL DEFAULT '',
	verified  INTEGER NOT NULL DEFAULT 0,
	followers INTEGER NOT NULL DEFAULT 0,
	following INTEGER NOT NULL DEFAULT 0,
	owner     INTEGER NOT NULL DEFAULT 0,
	muted     INTEGER NOT NULL DEFAULT 0,
	created   INTEGER NOT NULL DEFAULT 0,
	picture   TEXT NOT NULL DEFAULT '', -- preview picture URL
	rank      INTEGER NOT NULL DEFAULT 0 -- order among suggestions
);
CREATE TABLE IF NOT EXISTS wz_lists (
	id      TEXT PRIMARY KEY,
	name    TEXT NOT NULL DEFAULT '',
	custom  INTEGER NOT NULL DEFAULT 0, -- user-made list (not a predefined label)
	ord     INTEGER NOT NULL DEFAULT 0,
	deleted INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS wz_members (
	chat TEXT NOT NULL, -- group JID
	jid  TEXT NOT NULL, -- participant, usually a LID
	pn   TEXT NOT NULL DEFAULT '', -- participant's phone-number JID, when known
	PRIMARY KEY (chat, jid)
);
CREATE INDEX IF NOT EXISTS wz_members_jid ON wz_members (jid);
CREATE INDEX IF NOT EXISTS wz_members_pn ON wz_members (pn) WHERE pn != '';
CREATE TABLE IF NOT EXISTS wz_member_changes (
	chat   TEXT NOT NULL,              -- group JID
	ts     INTEGER NOT NULL,
	name   TEXT NOT NULL DEFAULT '',   -- whose membership changed
	actor  TEXT NOT NULL DEFAULT '',   -- who changed it, '' for themselves
	action INTEGER NOT NULL DEFAULT 0  -- model.MemberAction
);
CREATE INDEX IF NOT EXISTS wz_member_changes_chat ON wz_member_changes (chat, ts);
CREATE TABLE IF NOT EXISTS wz_edits (
	chat    TEXT NOT NULL,
	id      TEXT NOT NULL,
	ts      INTEGER NOT NULL, -- unix milliseconds it was edited to this
	payload BLOB NOT NULL,    -- an earlier edit of the message
	PRIMARY KEY (chat, id, ts)
);
CREATE TABLE IF NOT EXISTS wz_list_chats (
	list TEXT NOT NULL,
	chat TEXT NOT NULL,
	PRIMARY KEY (list, chat)
);
`

// migrations add columns to databases created by older versions.
var migrations = []string{
	`ALTER TABLE wz_chats ADD COLUMN parent TEXT NOT NULL DEFAULT ''`,         // community a group belongs to
	`ALTER TABLE wz_chats ADD COLUMN community INTEGER NOT NULL DEFAULT 0`,    // 1 for a community's parent group
	`ALTER TABLE wz_chats ADD COLUMN announce_sub INTEGER NOT NULL DEFAULT 0`, // 1 for a community's announcements
	`ALTER TABLE wz_chats ADD COLUMN favorite INTEGER NOT NULL DEFAULT 0`,
	// 1 when an unread message of a group is for you; see addUnread.
	`ALTER TABLE wz_chats ADD COLUMN mentioned INTEGER NOT NULL DEFAULT 0`,
	`ALTER TABLE wz_chats ADD COLUMN general INTEGER NOT NULL DEFAULT 0`, // 1 for a community's General chat
	// Unix milliseconds its sender deleted a status update, for one kept
	// with model.PrefKeepDeleted; 0 otherwise.
	`ALTER TABLE wz_status ADD COLUMN revoked INTEGER NOT NULL DEFAULT 0`,
	`ALTER TABLE wz_status ADD COLUMN group_jid TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE wz_status ADD COLUMN duration INTEGER NOT NULL DEFAULT 0`,
	`ALTER TABLE wz_status ADD COLUMN file_type TEXT NOT NULL DEFAULT ''`,
	// The settings (settledBits) app state or this device has set, which a
	// history sync's copy, taken when the device linked, mustn't undo.
	`ALTER TABLE wz_chats ADD COLUMN settled INTEGER NOT NULL DEFAULT 0`,
	// The /whisper recipients a local whisper record was sent to.
	`ALTER TABLE wz_messages ADD COLUMN whisper TEXT NOT NULL DEFAULT ''`,
	// The admin who deleted someone else's message.
	`ALTER TABLE wz_messages ADD COLUMN revoked_by TEXT NOT NULL DEFAULT ''`,
}

func (s *msgStore) init(ctx context.Context) error {
	dropped, err := s.dropOldMessages(ctx)
	if err != nil {
		return fmt.Errorf("drop old messages: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, schema+receiptsSchema+stickerSchema+votesSchema+reactionsSchema+snippetSchema); err != nil {
		return err
	}
	for _, m := range migrations {
		if _, err := s.db.ExecContext(ctx, m); err != nil && !strings.Contains(err.Error(), "duplicate column") {
			return err
		}
	}
	if dropped {
		s.vacuum(ctx)
	}
	return s.failStale(ctx)
}

// dropOldMessages drops the messages of a database made by a version that
// kept what they show in columns of its own, beside a payload most of them
// lacked: message storage starts over, once. Their edits, votes and
// receipts go with them; chats, settings, snippets and the rest stay. It
// reports whether it dropped anything.
func (s *msgStore) dropOldMessages(ctx context.Context) (bool, error) {
	var old bool
	err := s.db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM pragma_table_info('wz_messages'))
		AND NOT EXISTS (SELECT 1 FROM pragma_table_info('wz_messages') WHERE name = 'edit_payload')`).Scan(&old)
	if err != nil || !old {
		return false, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	for _, q := range []string{
		`DROP TABLE wz_messages`,
		`DROP TABLE IF EXISTS wz_edits`,
		`DROP TABLE IF EXISTS wz_votes`,
		`DROP TABLE IF EXISTS wz_receipts`,
		`DROP TABLE IF EXISTS wz_reactions`,
		`UPDATE wz_chats SET unread = 0`,
		`DELETE FROM wz_meta WHERE key IN ('legacy_media_migrated', 'file_info_migrated')`,
	} {
		if _, err := tx.ExecContext(ctx, q); err != nil {
			return false, err
		}
	}
	return true, tx.Commit()
}

// vacuum gives the space dropped messages took back to the system: SQLite
// otherwise keeps free pages in the file for later writes. The copy goes
// through the write-ahead log, which is emptied after.
func (s *msgStore) vacuum(ctx context.Context) {
	if _, err := s.db.ExecContext(ctx, `VACUUM`); err == nil {
		_, _ = s.db.ExecContext(ctx, `PRAGMA wal_checkpoint(TRUNCATE)`)
	}
}

// failStale marks the messages still pending from an earlier run failed:
// nothing sends them any more.
func (s *msgStore) failStale(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `UPDATE wz_messages SET receipt = ? WHERE from_me = 1 AND receipt = ?`,
		int(model.Failed), int(model.Pending))
	return err
}

func (s *msgStore) wipe(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM wz_messages; DELETE FROM wz_chats; DELETE FROM wz_meta;
		DELETE FROM wz_status; DELETE FROM wz_channels; DELETE FROM wz_lists; DELETE FROM wz_list_chats; DELETE FROM wz_stickers;
		DELETE FROM wz_edits; DELETE FROM wz_votes; DELETE FROM wz_reactions; DELETE FROM wz_snippets;`)
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

// rename sets a chat's stored name and reports whether it changed.
func (s *msgStore) rename(ctx context.Context, jid, name string) bool {
	r, err := s.db.ExecContext(ctx, `UPDATE wz_chats SET name = ? WHERE jid = ? AND name != ?`, name, jid, name)
	if err != nil {
		return false
	}
	n, _ := r.RowsAffected()
	return n > 0
}

// chatMeta is the chat state carried by a history sync conversation.
type chatMeta struct {
	pinned, mutedUntil, lastTS int64
	archived, mentioned        bool
	unread                     int
}

func (s *msgStore) setMeta(ctx context.Context, x execer, jid string, m chatMeta) error {
	_, err := x.ExecContext(ctx, `
		UPDATE wz_chats SET
			pinned = CASE WHEN settled & ?1 THEN pinned ELSE ?2 END,
			muted_until = CASE WHEN settled & ?3 THEN muted_until ELSE ?4 END,
			archived = CASE WHEN settled & ?5 THEN archived ELSE ?6 END,
			mentioned = CASE WHEN settled & ?7 THEN mentioned ELSE ?8 END,
			unread = CASE WHEN settled & ?7 THEN unread ELSE ?9 END,
			last_ts = MAX(last_ts, ?10)
		WHERE jid = ?11`,
		settledBits["pinned"], m.pinned, settledBits["muted_until"], m.mutedUntil,
		settledBits["archived"], boolInt(m.archived), settledBits["unread"], boolInt(m.mentioned),
		m.unread, m.lastTS, jid)
	return err
}

// settledBits are the chat settings app state syncs, each with its bit in
// wz_chats.settled. Their latest value comes from app state, a read
// receipt or this device; a history sync's copy is older.
var settledBits = map[string]int{"pinned": 1, "muted_until": 2, "archived": 4, "unread": 8}

func (s *msgStore) setField(ctx context.Context, jid, field string, v any) error {
	// field is always a constant from this package.
	bit, ok := settledBits[field]
	if !ok {
		_, err := s.db.ExecContext(ctx, `UPDATE wz_chats SET `+field+` = ? WHERE jid = ?`, v, jid)
		return err
	}
	// App state can set a chat's settings before its history comes (on a
	// new link, it always does), so the chat is created if needed. It isn't
	// listed until it has a message (see chats).
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO wz_chats (jid, is_group, `+field+`, settled) VALUES (?1, ?2, ?3, ?4)
		ON CONFLICT (jid) DO UPDATE SET `+field+` = ?3, settled = settled | ?4`,
		jid, boolInt(strings.HasSuffix(jid, "@g.us")), v, bit)
	return err
}

// listed reports whether a chat shows in the chat list (see chats).
func (s *msgStore) listed(ctx context.Context, jid string) bool {
	var ok bool
	_ = s.db.QueryRowContext(ctx, `SELECT last_ts > 0 OR EXISTS (SELECT 1 FROM wz_messages WHERE chat = ?1)
		FROM wz_chats WHERE jid = ?1`, jid).Scan(&ok)
	return ok
}

// readUpTo marks a chat read up to the newest of ids, which were read on
// another device: it keeps only the unread messages after them. A chat
// marked as unread stays so.
func (s *msgStore) readUpTo(ctx context.Context, jid string, ids []string) error {
	var ts sql.NullInt64
	if len(ids) > 0 {
		args := []any{jid}
		for _, id := range ids {
			args = append(args, id)
		}
		_ = s.db.QueryRowContext(ctx, `SELECT MAX(ts) FROM wz_messages WHERE chat = ? AND id IN (`+
			placeholders(len(ids))+`)`, args...).Scan(&ts)
	}
	return s.readAfter(ctx, jid, ts.Int64, false)
}

// readAfter keeps a chat's unread count to its incoming messages after ts
// (unix seconds; 0 reads them all). unmark reads a chat marked as unread
// too.
func (s *msgStore) readAfter(ctx context.Context, jid string, ts int64, unmark bool) error {
	var after int
	if ts > 0 {
		if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM wz_messages
			WHERE chat = ? AND from_me = 0 AND ts > ?`, jid, ts).Scan(&after); err != nil {
			return err
		}
	}
	unread := "unread"
	if unmark {
		unread = "MAX(unread, 0)"
	}
	_, err := s.db.ExecContext(ctx, `UPDATE wz_chats SET unread = MIN(`+unread+`, ?1),
		settled = settled | ?2 WHERE jid = ?3`, after, settledBits["unread"], jid)
	return err
}

// addUnread counts one more unread message, which is for you (forMe) or
// not. A chat's mentioned flag starts over with its unread count, so it
// needs no clearing wherever the chat is read.
func (s *msgStore) addUnread(ctx context.Context, jid string, forMe bool) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE wz_chats SET unread = MAX(unread, 0) + 1,
			mentioned = (unread > 0 AND mentioned != 0) OR ?
		WHERE jid = ?`, boolInt(forMe), jid)
	return err
}

// storedMsg is a message to store: its payload, with what parse read of
// it and the envelope it came in.
type storedMsg struct {
	*model.Message
	senderJID  string
	senderPush string
	whisper    string // /whisper recipient JIDs, newline-joined
	rawPayload []byte // the marshaled waE2E message
	// quotedMedia is the photo, video or voice message the message
	// quotes (quotedID), when the quote carries what downloading it takes
	// (see fillViewOnce).
	quotedMedia *waE2E.Message
	quotedID    string
}

// putMessage stores a message, or what came of one again: the payload it
// came with first stays (setRawPayload replaces your own as it goes out),
// so do its latest edit and its text as edited, and a deleted one stays
// deleted.
func (s *msgStore) putMessage(ctx context.Context, x execer, m storedMsg) error {
	_, err := x.ExecContext(ctx, `
		INSERT INTO wz_messages (chat, id, sender_jid, sender_push, from_me, ts, kind, media, text, receipt, whisper, raw_payload)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (chat, id) DO UPDATE SET
			sender_push = excluded.sender_push, kind = excluded.kind, media = excluded.media,
			text = CASE WHEN wz_messages.edited != 0 OR wz_messages.edit_payload IS NOT NULL
				THEN wz_messages.text ELSE excluded.text END,
			receipt = MAX(wz_messages.receipt, excluded.receipt),
			raw_payload = COALESCE(wz_messages.raw_payload, excluded.raw_payload)
		WHERE wz_messages.kind != ?`,
		m.ChatID, m.ID, m.senderJID, m.senderPush, boolInt(m.FromMe), m.Time.Unix(), int(m.Kind), int(m.Media),
		m.Text, int(m.Receipt), m.whisper, m.rawPayload, int(model.KindDeleted))
	if err != nil {
		return err
	}
	_, err = x.ExecContext(ctx, `UPDATE wz_chats SET last_ts = MAX(last_ts, ?) WHERE jid = ?`, m.Time.Unix(), m.ChatID)
	return err
}

// fillViewOnce gives a view once message whose media never came here the
// message a reply to it quoted, q, as its payload. It reports whether it
// had none.
func (s *msgStore) fillViewOnce(ctx context.Context, chat, id string, q *waE2E.Message) bool {
	c := describe(q)
	if c.media != model.MediaImage && c.media != model.MediaVideo && c.media != model.MediaVoice {
		return false
	}
	var old []byte
	if s.db.QueryRowContext(ctx, `SELECT raw_payload FROM wz_messages WHERE chat = ? AND id = ? AND kind = ?`,
		chat, id, int(model.KindViewOnce)).Scan(&old) != nil {
		return false
	}
	if _, blob := rawMedia(old); blob != nil {
		return false // its media came after all
	}
	r, err := s.db.ExecContext(ctx, `UPDATE wz_messages SET raw_payload = ?, media = ?,
			text = CASE WHEN text = '' THEN ? ELSE text END
		WHERE chat = ? AND id = ? AND kind = ? AND raw_payload IS ?`,
		marshal(q), int(c.media), c.text, chat, id, int(model.KindViewOnce), old)
	if err != nil {
		return false
	}
	n, _ := r.RowsAffected()
	return n > 0
}

func placeholders(n int) string {
	return strings.TrimSuffix(strings.Repeat("?,", n), ",")
}

// setTime moves a message to another time, in whole seconds as stored. It
// reports whether the stored time changed.
func (s *msgStore) setTime(ctx context.Context, chat, id string, at time.Time) (bool, error) {
	res, err := s.db.ExecContext(ctx, `UPDATE wz_messages SET ts = ? WHERE chat = ? AND id = ? AND ts != ?`,
		at.Unix(), chat, id, at.Unix())
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
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

// setFailed marks an outgoing message that is still pending as failed.
func (s *msgStore) setFailed(ctx context.Context, chat, id string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE wz_messages SET receipt = ? WHERE chat = ? AND id = ? AND receipt = ?`,
		int(model.Failed), chat, id, int(model.Pending))
	return err
}

// markDeleted turns a message into "This message was deleted". by is the
// group admin who deleted it, or "" for its sender.
func (s *msgStore) markDeleted(ctx context.Context, chat, id, by string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE wz_messages SET kind = ?3, media = 0, text = '', pinned = 0, edited = 0,
			revoked = 0, revoked_by = ?4, raw_payload = NULL, edit_payload = NULL
		WHERE chat = ?1 AND id = ?2;
		DELETE FROM wz_edits WHERE chat = ?1 AND id = ?2;
		DELETE FROM wz_votes WHERE chat = ?1 AND id = ?2;
		DELETE FROM wz_reactions WHERE chat = ?1 AND id = ?2`, chat, id, int(model.KindDeleted), by)
	return err
}

// markRevoked flags a message deleted for everyone at at, keeping what it
// said (model.PrefKeepDeleted). by is the group admin who deleted it, or
// "" for its sender.
func (s *msgStore) markRevoked(ctx context.Context, chat, id string, at time.Time, by string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE wz_messages SET revoked = ?, revoked_by = ?, pinned = 0
		WHERE chat = ? AND id = ? AND kind != ? AND revoked = 0`, at.UnixMilli(), by, chat, id, int(model.KindDeleted))
	return err
}

// editText gives a message the text of an edit made at at, whose content
// is payload. The edit it replaces joins its earlier versions, which
// begin with its raw payload. An edit older than the last one (history
// arriving late) only joins them.
func (s *msgStore) editText(ctx context.Context, chat, id, text string, at time.Time, payload []byte) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var curText string
	var ts, edited int64
	var raw, curEdit []byte
	err = tx.QueryRowContext(ctx, `SELECT text, ts, edited, raw_payload, edit_payload FROM wz_messages
		WHERE chat = ? AND id = ? AND kind != ?`, chat, id, int(model.KindDeleted)).Scan(&curText, &ts, &edited, &raw, &curEdit)
	if err == sql.ErrNoRows {
		return nil // not a message we have
	}
	if err != nil {
		return err
	}
	cur := curEdit
	if cur == nil {
		cur = raw
	}
	_, curMentions := rawText(cur)
	_, mentions := rawText(payload)
	ms := at.UnixMilli()
	switch {
	case ms == edited || text == curText && mentions == curMentions:
		return nil // seen already
	case ms < edited:
		if ms <= ts*1000 {
			return nil
		}
		_, err = tx.ExecContext(ctx, `INSERT OR IGNORE INTO wz_edits (chat, id, ts, payload) VALUES (?, ?, ?, ?)`,
			chat, id, ms, payload)
	default:
		if curEdit != nil {
			_, err = tx.ExecContext(ctx, `INSERT OR IGNORE INTO wz_edits (chat, id, ts, payload) VALUES (?, ?, ?, ?)`,
				chat, id, edited, curEdit)
		}
		if err == nil {
			_, err = tx.ExecContext(ctx, `UPDATE wz_messages SET text = ?, edited = ?, edit_payload = ? WHERE chat = ? AND id = ?`,
				text, ms, payload, chat, id)
		}
	}
	if err != nil {
		return err
	}
	return tx.Commit()
}

// versions returns an edited message's earlier texts, oldest first, with
// their mentions: its raw payload's, then each edit's but the last.
func (s *msgStore) versions(ctx context.Context, chat, id string) (texts, mentions []string, times []time.Time, err error) {
	var raw []byte
	var ts, edited int64
	err = s.db.QueryRowContext(ctx, `SELECT raw_payload, ts, edited FROM wz_messages WHERE chat = ? AND id = ?`, chat, id).
		Scan(&raw, &ts, &edited)
	if err == sql.ErrNoRows || err == nil && edited == 0 {
		return nil, nil, nil, nil
	}
	if err != nil {
		return nil, nil, nil, err
	}
	add := func(payload []byte, ms int64) {
		t, m := rawText(payload)
		texts, mentions, times = append(texts, t), append(mentions, m), append(times, time.UnixMilli(ms))
	}
	add(raw, ts*1000)
	rows, err := s.db.QueryContext(ctx, `SELECT payload, ts FROM wz_edits WHERE chat = ? AND id = ? ORDER BY ts`, chat, id)
	if err != nil {
		return nil, nil, nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var p []byte
		var ms int64
		if err := rows.Scan(&p, &ms); err != nil {
			return nil, nil, nil, err
		}
		add(p, ms)
	}
	return texts, mentions, times, rows.Err()
}

// lastPush returns the push name of the newest stored message from jid or
// alt (one person's LID and phone JID), or "".
func (s *msgStore) lastPush(ctx context.Context, jid, alt string) string {
	var push string
	_ = s.db.QueryRowContext(ctx, `SELECT sender_push FROM wz_messages
		WHERE sender_jid IN (?, ?) AND sender_push != '' ORDER BY ts DESC LIMIT 1`, jid, alt).Scan(&push)
	return push
}

func (s *msgStore) mediaBlob(ctx context.Context, chat, id string) (media model.Media, blob []byte, err error) {
	if chat == statusChat {
		err = s.db.QueryRowContext(ctx, `SELECT media, media_blob FROM wz_status WHERE id = ?`, id).Scan(&media, &blob)
		return
	}
	if chat == stickerChat {
		blob, err = s.stickerBlob(ctx, id)
		return model.MediaSticker, blob, err
	}
	var raw []byte
	err = s.db.QueryRowContext(ctx, `SELECT media, raw_payload FROM wz_messages WHERE chat = ? AND id = ?`, chat, id).
		Scan(&media, &raw)
	_, blob = rawMedia(raw)
	return
}

// rawMsg is a loaded message before names are resolved.
type rawMsg struct {
	*model.Message
	senderJID, senderPush string
	mentions              string // as mentionsOf joins them
	whisper               string // /whisper recipient JIDs, newline-joined; resolve makes them Whisper
	revokedBy             string // the admin who deleted it; resolve makes it DeletedBy
	buttons               *buttonsInfo
	quote                 *rawQuote // made Quote by resolve
	hasBlob               bool      // its payload holds what downloading its media takes
}

// msgColumns are what scanMessage reads.
const msgColumns = `chat, id, sender_jid, sender_push, from_me, ts, kind, media, text, receipt, reaction, starred, pinned,
	edited, revoked, revoked_by, opened, whisper, raw_payload, edit_payload`

type scanner interface{ Scan(dest ...any) error }

func scanMessage(sc scanner) (rawMsg, error) {
	var (
		m                       model.Message
		r                       rawMsg
		kind, media, receipt    int
		ts, pinned              int64
		edited, revoked         int64
		fromMe, starred, opened bool
		raw, edit               []byte
		react                   string
	)
	err := sc.Scan(&m.ChatID, &m.ID, &r.senderJID, &r.senderPush, &fromMe, &ts, &kind, &media, &m.Text, &receipt,
		&react, &starred, &pinned, &edited, &revoked, &r.revokedBy, &opened, &r.whisper, &raw, &edit)
	if err != nil {
		return r, err
	}
	m.FromMe = fromMe
	m.Time = time.Unix(ts, 0)
	m.Kind = model.Kind(kind)
	m.Media = model.Media(media)
	m.Receipt = model.Receipt(receipt)
	m.Starred, m.Pinned = starred, pinned != 0
	fillReactions(&m, react)
	if edited != 0 {
		m.Edited = time.UnixMilli(edited)
	}
	if revoked != 0 {
		m.Revoked = time.UnixMilli(revoked)
	}
	m.SenderID = r.senderJID
	r.Message = &m
	r.fill(raw, edit)
	if m.Kind == model.KindViewOnce {
		// Your own, still uploading, is on this computer already.
		m.Opened, m.OnPhone = opened, !r.hasBlob && !(m.FromMe && m.Receipt <= model.Pending)
	}
	return r, nil
}

func (s *msgStore) message(ctx context.Context, chat, id string) (rawMsg, bool) {
	row := s.db.QueryRowContext(ctx, `SELECT `+msgColumns+` FROM wz_messages WHERE chat = ? AND id = ?`, chat, id)
	m, err := scanMessage(row)
	return m, err == nil
}

// messages returns the newest limit messages of a chat, oldest first.
func (s *msgStore) messages(ctx context.Context, chat string, limit int) ([]rawMsg, error) {
	return s.queryMessages(ctx, `SELECT `+msgColumns+` FROM (
		SELECT *, rowid AS rid FROM wz_messages WHERE chat = ? ORDER BY ts DESC, rid DESC LIMIT ?
	) ORDER BY ts, rid`, chat, limit)
}

// Messages are ordered by (ts, rowid). The cursor message a is looked up by
// ID; the "m.ts <= a.ts" term lets SQLite walk the (chat, ts) index.
const cursorJoin = `wz_messages m, (SELECT ts AS ats, rowid AS arid FROM wz_messages WHERE chat = ? AND id = ?) a
	WHERE m.chat = ?`

// messagesBefore returns up to limit messages of a chat older than message
// id, oldest first.
func (s *msgStore) messagesBefore(ctx context.Context, chat, id string, limit int) ([]rawMsg, error) {
	return s.queryMessages(ctx, `SELECT `+msgColumns+` FROM (
		SELECT m.*, m.rowid AS rid FROM `+cursorJoin+` AND m.ts <= a.ats AND (m.ts < a.ats OR m.rowid < a.arid)
		ORDER BY m.ts DESC, rid DESC LIMIT ?
	) ORDER BY ts, rid`, chat, id, chat, limit)
}

// messagesFrom returns up to limit messages of a chat from message id
// (included) on, oldest first; none when id isn't stored.
func (s *msgStore) messagesFrom(ctx context.Context, chat, id string, limit int) ([]rawMsg, error) {
	return s.queryMessages(ctx, `SELECT `+msgColumns+` FROM (
		SELECT m.*, m.rowid AS rid FROM `+cursorJoin+` AND m.ts >= a.ats AND (m.ts > a.ats OR m.rowid >= a.arid)
		ORDER BY m.ts, rid LIMIT ?
	)`, chat, id, chat, limit)
}

// oldestID returns the ID of a chat's first message, or "".
func (s *msgStore) oldestID(ctx context.Context, chat string) string {
	var id string
	_ = s.db.QueryRowContext(ctx, `SELECT id FROM wz_messages WHERE chat = ? ORDER BY ts, rowid LIMIT 1`, chat).Scan(&id)
	return id
}

// pinnedMessage returns the chat's most recently pinned message.
func (s *msgStore) pinnedMessage(ctx context.Context, chat string) (rawMsg, bool) {
	row := s.db.QueryRowContext(ctx, `SELECT `+msgColumns+` FROM wz_messages
		WHERE chat = ? AND pinned != 0 ORDER BY pinned DESC LIMIT 1`, chat)
	m, err := scanMessage(row)
	return m, err == nil
}

// searchMessages returns up to limit messages of a chat whose
// model.SearchKey contains key, newest first, leaving out deleted and
// unsupported messages. SQLite's LIKE folds ASCII only and sees the
// formatting markers, so the texts are compared in Go; it stops when ctx
// is cancelled.
func (s *msgStore) searchMessages(ctx context.Context, chat, key string, limit int) ([]rawMsg, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT rowid, text FROM wz_messages
		WHERE chat = ? AND kind NOT IN (?, ?) AND text != '' ORDER BY ts DESC, rowid DESC`,
		chat, int(model.KindDeleted), int(model.KindUnsupported))
	if err != nil {
		return nil, err
	}
	var ids []string
	for rows.Next() && len(ids) < limit {
		var id int64
		var text string
		if err := rows.Scan(&id, &text); err != nil {
			rows.Close()
			return nil, err
		}
		if strings.Contains(model.SearchKey(text), key) {
			ids = append(ids, strconv.FormatInt(id, 10))
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil || len(ids) == 0 {
		return nil, err
	}
	return s.queryMessages(ctx, `SELECT `+msgColumns+` FROM (
		SELECT *, rowid AS rid FROM wz_messages WHERE rowid IN (`+strings.Join(ids, ",")+`)
	) ORDER BY ts DESC, rid DESC`)
}

func (s *msgStore) queryMessages(ctx context.Context, q string, args ...any) ([]rawMsg, error) {
	rows, err := s.db.QueryContext(ctx, q, args...)
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

// chatQuery reads chats with their last message. The message's payloads
// are read only for what the list shows of them: how long a recording
// lasts, and whom a text mentions.
var chatQuery = fmt.Sprintf(`
	SELECT c.jid, c.name, c.is_group, c.pinned, c.muted_until, c.archived, c.unread, c.last_ts, c.favorite,
		c.mentioned, c.general, m.id, m.sender_jid, m.sender_push, m.from_me, m.ts, m.kind, m.media, m.text, m.receipt,
		CASE WHEN m.media IN (%d, %d, %d, %d) OR instr(m.text, '@') > 0 THEN m.raw_payload END,
		CASE WHEN instr(m.text, '@') > 0 THEN m.edit_payload END
	FROM wz_chats c
	LEFT JOIN wz_messages m ON m.rowid = (
		SELECT rowid FROM wz_messages WHERE chat = c.jid ORDER BY ts DESC, rowid DESC LIMIT 1
	)`, model.MediaVideo, model.MediaGIF, model.MediaVoice, model.MediaAudio)

func scanChat(sc scanner, now time.Time) (rawChat, error) {
	var (
		c                              model.Chat
		isGroup, archived, unread, fav int
		mentioned, general             int
		pinned, mutedUntil, lastTS     int64
		mID, mSender, mPush, mText     sql.NullString
		mFromMe, mTS, mKind, mMedia    sql.NullInt64
		mReceipt                       sql.NullInt64
		mRaw, mEdit                    []byte
	)
	err := sc.Scan(&c.ID, &c.Name, &isGroup, &pinned, &mutedUntil, &archived, &unread, &lastTS, &fav,
		&mentioned, &general, &mID, &mSender, &mPush, &mFromMe, &mTS, &mKind, &mMedia, &mText, &mReceipt, &mRaw, &mEdit)
	if err != nil {
		return rawChat{}, err
	}
	c.IsGroup = isGroup != 0
	c.Pinned = pinned > 0
	c.Muted = mutedUntil == -1 || mutedUntil > now.Unix()
	if c.Muted && mutedUntil > 0 {
		c.MuteUntil = time.Unix(mutedUntil, 0)
	}
	c.Favorite = fav != 0
	c.General = general != 0
	c.Archived = archived != 0
	c.Unread = unread
	c.Mentioned = mentioned != 0 && unread > 0
	c.Time = time.Unix(lastTS, 0)
	rc := rawChat{Chat: &c}
	if mID.Valid {
		m := &model.Message{
			ID: mID.String, ChatID: c.ID, SenderID: mSender.String, FromMe: mFromMe.Int64 != 0,
			Time: time.Unix(mTS.Int64, 0), Kind: model.Kind(mKind.Int64), Media: model.Media(mMedia.Int64),
			Text: mText.String, Receipt: model.Receipt(mReceipt.Int64),
		}
		last := rawMsg{Message: m, senderJID: mSender.String, senderPush: mPush.String}
		last.fill(mRaw, mEdit)
		last.quote = nil // not shown in the list
		c.Last = m
		rc.last = &last
		if m.Time.After(c.Time) {
			c.Time = m.Time
		}
	}
	return rc, nil
}

// chats lists chats that have any activity, newest first. Channels and
// community parent groups aren't chats.
func (s *msgStore) chats(ctx context.Context) ([]rawChat, error) {
	return s.queryChats(ctx, chatQuery+` WHERE (c.last_ts > 0 OR m.id IS NOT NULL)
		AND c.community = 0 AND c.jid NOT LIKE '%@newsletter'
		ORDER BY c.pinned DESC, MAX(c.last_ts, COALESCE(m.ts, 0)) DESC`)
}

func (s *msgStore) queryChats(ctx context.Context, q string, args ...any) ([]rawChat, error) {
	rows, err := s.db.QueryContext(ctx, q, args...)
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
	rows, err := s.db.QueryContext(ctx, `SELECT jid FROM wz_chats WHERE is_group = ? AND jid NOT LIKE '%@newsletter'`, boolInt(groups))
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
