package auto

import (
	"errors"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

func (a *Backend) SendWhisper(w model.Whisper) <-chan error {
	if a.refused() {
		done := make(chan error, 1)
		done <- errors.New(GhostText)
		close(done)
		return done
	}
	a.sent()
	return a.Backend.SendWhisper(w)
}
