package mock

import (
	"errors"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

// SendWhisper leaves the same local-only record the real backend does: a
// message drawn as yours, marked with its recipients, never broadcast.
func (b *Backend) SendWhisper(chatID string, targets []string, text string, mentions []string, reply *model.Message) <-chan error {
	done := make(chan error, 1)
	if b.Pref("cmd_whisper") != "on" {
		done <- errors.New("Enable /whisper in Ethically gray features first.")
		close(done)
		return done
	}
	var names []string
	if info := b.Info(chatID); info != nil {
		for _, id := range targets {
			for _, m := range info.Members {
				if m.ID == id {
					names = append(names, m.Name)
					break
				}
			}
		}
	}
	m := &model.Message{ChatID: chatID, FromMe: true, Text: text, Time: b.now(), Receipt: model.Sent, Whisper: names,
		Quote: quoteOf(reply)}
	b.add(m)
	cp := *m
	b.emit(model.MessageEvent{Msg: &cp})
	done <- nil
	close(done)
	return done
}
