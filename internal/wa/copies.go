package wa

import (
	"context"
	"os"
	"time"

	"github.com/polymorfa/hypermeow/proto/waE2E"
	"google.golang.org/protobuf/proto"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

// WhatsApp sends a picture or video in HD twice: the message itself, in
// standard quality, then a copy of it under an ID of its own, whose
// messageContextInfo points back to it (an HD_IMAGE_DUAL_UPLOAD or
// HD_VIDEO_DUAL_UPLOAD association). Its phones show one bubble. A video can
// also get a copy in HEVC or AV1, and a motion photo's clip comes the same way.
// Reactions and replies go to the message itself, so a copy is no message of
// its own here: parse files it under the message's ID, and its payload goes in
// copy_payload, where only mediaBlob reads it.
//
// History sync sends the newest first, so a copy usually comes before its
// message. It then stands in for the message (raw_payload is copy_payload)
// until the message comes and takes its place in putMessage.

// metaCopiesFolded marks a database whose copies are filed under their
// messages (foldCopies).
const metaCopiesFolded = "copies_folded"

// copyOf returns the ID of the message m is a copy of, or "". The association
// can sit on any of the envelopes around the media, as an album's can.
func copyOf(m *waE2E.Message) string {
	for i := 0; i < 5 && m != nil; i++ {
		a := m.GetMessageContextInfo().GetMessageAssociation()
		switch a.GetAssociationType() {
		case waE2E.MessageAssociation_HD_IMAGE_DUAL_UPLOAD, waE2E.MessageAssociation_HD_VIDEO_DUAL_UPLOAD,
			waE2E.MessageAssociation_HEVC_VIDEO_DUAL_UPLOAD, waE2E.MessageAssociation_AV1_VIDEO_DUAL_UPLOAD,
			waE2E.MessageAssociation_MOTION_PHOTO:
			if id := a.GetParentMessageKey().GetID(); id != "" {
				return id
			}
		}
		m = unwrapOnce(m)
	}
	return ""
}

// isHD reports whether m's association says it's the better file of the same
// picture or video. HEVC and AV1 aren't: the system may not decode them, and
// a motion photo's clip isn't its picture.
func isHD(m *waE2E.Message) bool {
	for i := 0; i < 5 && m != nil; i++ {
		switch m.GetMessageContextInfo().GetMessageAssociation().GetAssociationType() {
		case waE2E.MessageAssociation_HD_IMAGE_DUAL_UPLOAD, waE2E.MessageAssociation_HD_VIDEO_DUAL_UPLOAD:
			return true
		}
		m = unwrapOnce(m)
	}
	return false
}

// hdBlob returns the media blob of a stored copy when it's what to download
// for a message showing media, or nil.
func hdBlob(media model.Media, copyPayload []byte) []byte {
	c, m := describeRaw(copyPayload)
	if m == nil || c.media != media || !isHD(m) {
		return nil
	}
	return c.keptBlob(m)
}

// putCopy stores a copy of the message m.ID. If the message isn't here yet, or
// is still "Waiting for this message", the copy shows until it comes.
func (s *msgStore) putCopy(ctx context.Context, x execer, m storedMsg) error {
	_, err := x.ExecContext(ctx, `
		INSERT INTO wz_messages (chat, id, sender_jid, sender_push, from_me, ts, kind, media, text, receipt, whisper, kept, raw_payload, copy_payload)
		VALUES (?1, ?2, ?3, ?4, ?5, ?6, ?7, ?8, ?9, ?10, ?11, ?12, ?13, ?13)
		ON CONFLICT (chat, id) DO UPDATE SET
			copy_payload = excluded.copy_payload,
			kind = CASE WHEN wz_messages.kind = ?14 THEN excluded.kind ELSE wz_messages.kind END,
			media = CASE WHEN wz_messages.kind = ?14 THEN excluded.media ELSE wz_messages.media END,
			text = CASE WHEN wz_messages.kind = ?14 THEN excluded.text ELSE wz_messages.text END,
			raw_payload = CASE WHEN wz_messages.kind = ?14 THEN excluded.raw_payload ELSE wz_messages.raw_payload END
		WHERE wz_messages.kind != ?15`,
		m.ChatID, m.ID, m.senderJID, m.senderPush, boolInt(m.FromMe), m.Time.Unix(), int(m.Kind), int(m.Media),
		m.Text, int(m.Receipt), m.whisper, boolInt(m.Kept), m.rawPayload, int(model.KindWaiting), int(model.KindDeleted))
	return err
}

// hasCopy reports whether a copy of a message is stored.
func (s *msgStore) hasCopy(ctx context.Context, chat, id string) bool {
	var has bool
	_ = s.db.QueryRowContext(ctx, `SELECT copy_payload IS NOT NULL FROM wz_messages WHERE chat = ? AND id = ?`, chat, id).Scan(&has)
	return has
}

// dropSmallerMedia removes what was downloaded of m before its HD copy came,
// so that it shows the better file. A file being played stays.
func (b *Backend) dropSmallerMedia(m *model.Message) {
	gone := false
	for _, path := range []string{b.mediaPath(m.ChatID, m.ID), b.mediaFilePath(m), b.failedPath(m.ChatID, m.ID)} {
		if os.Remove(path) == nil {
			gone = true
		}
	}
	if gone {
		b.emit(model.MediaEvent{ChatID: m.ChatID, MsgID: m.ID})
	}
}

// foldCopies files the copies an older version stored as messages of their
// own under their messages, once. What was downloaded of them moves along.
func (b *Backend) foldCopies(ctx context.Context) error {
	if b.store.meta(ctx, metaCopiesFolded) != "" {
		return nil
	}
	type found struct {
		storedMsg
		id              string // the copy's own
		starred, pinned int64
	}
	var copies []found
	rows, err := b.db.QueryContext(ctx, `SELECT chat, id, sender_jid, sender_push, from_me, ts, kind, media, text, receipt,
		whisper, kept, starred, pinned, raw_payload FROM wz_messages WHERE media != 0 AND raw_payload IS NOT NULL`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var (
			f                    found
			mm                   model.Message
			fromMe, kept         bool
			kind, media, receipt int
			ts                   int64
		)
		if err := rows.Scan(&mm.ChatID, &f.id, &f.senderJID, &f.senderPush, &fromMe, &ts, &kind, &media, &mm.Text, &receipt,
			&f.whisper, &kept, &f.starred, &f.pinned, &f.rawPayload); err != nil {
			rows.Close()
			return err
		}
		var pm waE2E.Message
		if proto.Unmarshal(f.rawPayload, &pm) != nil {
			continue
		}
		parent := copyOf(&pm)
		if parent == "" || parent == f.id {
			continue
		}
		mm.ID, mm.FromMe, mm.Kept, mm.Time = parent, fromMe, kept, time.Unix(ts, 0)
		mm.Kind, mm.Media, mm.Receipt = model.Kind(kind), model.Media(media), model.Receipt(receipt)
		f.Message = &mm
		copies = append(copies, f)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	err = func() error {
		tx, err := b.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer tx.Rollback()
		for _, f := range copies {
			if err := b.store.foldCopy(ctx, tx, f.storedMsg, f.id, f.starred, f.pinned); err != nil {
				return err
			}
		}
		return tx.Commit()
	}()
	if err != nil {
		return err
	}
	for _, f := range copies {
		b.moveCopyMedia(f.storedMsg, f.id)
	}
	return b.store.setMetaValue(ctx, metaCopiesFolded, "1")
}

// foldCopy files the copy stored as message id under its message, m.ID. A star
// or pin it got stays; the people's receipts of it go, since the message has
// its own.
func (s *msgStore) foldCopy(ctx context.Context, x execer, m storedMsg, id string, starred, pinned int64) error {
	if err := s.putCopy(ctx, x, m); err != nil {
		return err
	}
	_, err := x.ExecContext(ctx, `UPDATE wz_messages SET starred = MAX(starred, ?3), pinned = MAX(pinned, ?4)
		WHERE chat = ?1 AND id = ?2 AND kind != ?5`, m.ChatID, m.ID, starred, pinned, int(model.KindDeleted))
	if err != nil {
		return err
	}
	_, err = x.ExecContext(ctx, `DELETE FROM wz_messages WHERE chat = ?1 AND id = ?2;
		DELETE FROM wz_receipts WHERE chat = ?1 AND id = ?2;
		DELETE FROM wz_reactions WHERE chat = ?1 AND id = ?2;
		DELETE FROM wz_votes WHERE chat = ?1 AND id = ?2;
		DELETE FROM wz_edits WHERE chat = ?1 AND id = ?2`, m.ChatID, id)
	return err
}

// moveCopyMedia gives m what was downloaded of its copy, stored until now as
// message id: an HD file replaces the message's, and anything else is removed.
func (b *Backend) moveCopyMedia(m storedMsg, id string) {
	chat := m.ChatID
	_ = os.Remove(b.failedPath(chat, id))
	hd := hdBlob(m.Media, m.rawPayload) != nil
	if hd {
		_ = os.Remove(b.failedPath(chat, m.ID))
	}
	ext := mediaExt(&model.Message{Media: m.Media})
	for _, sfx := range []string{"", ext} {
		from := b.mediaPath(chat, id) + sfx
		if hd {
			_ = os.Rename(from, b.mediaPath(chat, m.ID)+sfx)
		} else {
			_ = os.Remove(from)
		}
	}
}
