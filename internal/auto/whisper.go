package auto

import (
	"errors"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

func (a *Backend) SendWhisper(chatID string, targets []string, text string, mentions []string, reply *model.Message) <-chan error {
	if a.refused() {
		done := make(chan error, 1)
		done <- errors.New(GhostText)
		close(done)
		return done
	}
	a.sent()
	return a.Backend.SendWhisper(chatID, targets, text, mentions, reply)
}
