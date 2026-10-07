package mock

import "errors"

// SendWhisper simulates submission without adding an ordinary group message.
func (b *Backend) SendWhisper(chatID string, targets []string, text string) <-chan error {
	done := make(chan error, 1)
	var err error
	if b.Pref("cmd_whisper") != "on" {
		err = errors.New("Enable /whisper in Ethically gray features first.")
	}
	done <- err
	close(done)
	return done
}
