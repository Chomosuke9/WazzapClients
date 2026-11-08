package mock

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

// SendWhisper leaves the same local-only records the real backend does: a
// message for each part, drawn as yours, marked with its recipients, never
// broadcast.
func (b *Backend) SendWhisper(w model.Whisper) <-chan error {
	done := make(chan error, 1)
	defer close(done)
	if b.Pref("cmd_whisper") != "on" {
		done <- errors.New("Enable /whisper in Ethically gray features first.")
		return done
	}
	var names []string
	if info := b.Info(w.ChatID); info != nil {
		for _, id := range w.Targets {
			for _, m := range info.Members {
				if m.ID == id {
					names = append(names, m.Name)
					break
				}
			}
		}
	}
	for i, p := range w.Parts {
		m := &model.Message{ChatID: w.ChatID, FromMe: true, Text: p.Text, Time: b.now(), Receipt: model.Sent, Whisper: names}
		if i == 0 {
			m.Quote = quoteOf(w.Reply)
		}
		if a := p.File; a != nil {
			m.Media, m.FileName = a.Media, filepath.Base(a.Path)
			if st, err := os.Stat(a.Path); err == nil {
				m.FileSize = st.Size()
			}
			switch a.Media {
			case model.MediaImage, model.MediaVideo:
				m.Kind, m.ImageA, m.ImageB = model.KindImage, 0x5f6f7f, 0x9fafbf
				if a.ViewOnce {
					m.Kind = model.KindViewOnce
				}
			case model.MediaGIF:
				m.Kind, m.Thumb = model.KindImage, a.Thumb
			case model.MediaDocument:
				if m.Text == "" {
					m.Text = m.FileName
				}
			case model.MediaVoice:
				m.Duration, m.Waveform = a.Seconds, a.Waveform
			}
		}
		b.add(m)
		cp := *m
		b.emit(model.MessageEvent{Msg: &cp})
	}
	done <- nil
	return done
}
