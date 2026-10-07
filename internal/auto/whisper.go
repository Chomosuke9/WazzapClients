package auto

import "errors"

func (a *Backend) SendWhisper(chatID string, targets []string, text string) <-chan error {
	if a.refused() {
		done := make(chan error, 1)
		done <- errors.New(GhostText)
		close(done)
		return done
	}
	a.sent()
	return a.Backend.SendWhisper(chatID, targets, text)
}
