package wa

import (
	"context"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

// Transcripts: what voice messages were transcribed to on this computer
// (internal/transcribe), in wz_transcripts. WhatsApp's are made on the
// phone and never synced.

const transcriptsSchema = `
CREATE TABLE IF NOT EXISTS wz_transcripts (
	chat  TEXT NOT NULL,
	id    TEXT NOT NULL,            -- the voice message
	lang  TEXT NOT NULL DEFAULT '', -- the language asked for
	model TEXT NOT NULL DEFAULT '', -- the model that heard it
	said  TEXT NOT NULL DEFAULT '', -- '' when no words were heard
	PRIMARY KEY (chat, id)
);
`

// Transcript implements model.Backend.
func (b *Backend) Transcript(chatID, id string) *model.Transcript {
	return b.store.transcript(b.ctx, chatID, id)
}

// SetTranscript implements model.Backend.
func (b *Backend) SetTranscript(chatID, id string, t model.Transcript) {
	if err := b.store.setTranscript(b.ctx, chatID, id, t); err != nil {
		b.log.Warnf("store transcript of %s: %v", id, err)
	}
}

func (s *msgStore) transcript(ctx context.Context, chat, id string) *model.Transcript {
	var t model.Transcript
	err := s.db.QueryRowContext(ctx, `SELECT lang, model, said FROM wz_transcripts WHERE chat = ? AND id = ?`, chat, id).
		Scan(&t.Lang, &t.Model, &t.Text)
	if err != nil {
		return nil
	}
	return &t
}

// setTranscript keeps t, unless the message was deleted meanwhile.
func (s *msgStore) setTranscript(ctx context.Context, chat, id string, t model.Transcript) error {
	_, err := s.db.ExecContext(ctx, `INSERT OR REPLACE INTO wz_transcripts (chat, id, lang, model, said)
		SELECT ?1, ?2, ?3, ?4, ?5 WHERE EXISTS (SELECT 1 FROM wz_messages WHERE chat = ?1 AND id = ?2 AND media = ?6)`,
		chat, id, t.Lang, t.Model, t.Text, int(model.MediaVoice))
	return err
}
