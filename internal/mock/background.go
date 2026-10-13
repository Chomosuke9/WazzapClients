package mock

import "github.com/chomosuke9/wazzapclients/internal/model"

// bgState is how a demo backend ran, for tests of background accounts.
type bgState struct {
	on      bool // SetBackground
	starts  int
	closed  bool
	offline []model.Message // waiting for the next Start, as WhatsApp keeps them
}

// SetBackground implements model.Backgrounder.
func (b *Backend) SetBackground(on bool) { b.bg.on = on }

// Background reports whether the backend runs in the background.
func (b *Backend) Background() bool { return b.bg.on }

// Starts counts the times it was started.
func (b *Backend) Starts() int { return b.bg.starts }

// Close stops the backend; Start starts it again.
func (b *Backend) Close() { b.bg.closed = true }

// Closed reports whether it was closed since it last started.
func (b *Backend) Closed() bool { return b.bg.closed }

// QueueOffline keeps a message from who in chat for the next Start, as
// WhatsApp keeps the messages of a device that isn't connected.
func (b *Backend) QueueOffline(chatID, who, text string) {
	b.bg.offline = append(b.bg.offline, model.Message{ChatID: chatID, Sender: who, SenderID: who, Text: text})
}

// deliverOffline delivers the messages kept while it wasn't started, and
// then reports it caught up, as a connection does.
func (b *Backend) deliverOffline() {
	msgs := b.bg.offline
	b.bg.offline = nil
	for _, m := range msgs {
		b.Receive(m.ChatID, m.Sender, m.Text)
	}
	b.emit(model.CaughtUpEvent{})
}
