// Package model holds the data types shared by the UI and its backends
// (the real WhatsApp connection in internal/wa, or demo data in internal/mock).
package model

import "time"

// Receipt is the delivery state of an outgoing message.
type Receipt int

const (
	Pending Receipt = iota
	Sent
	Delivered
	Read
)

// Kind is the type of a message.
type Kind int

const (
	KindText Kind = iota
	KindImage
	KindDeleted
)

// Quote is the message a reply points to.
type Quote struct {
	Sender string
	Text   string
}

// Message is a single conversation entry.
type Message struct {
	ID       string
	ChatID   string
	Kind     Kind
	FromMe   bool
	Sender   string // display name; group chats only
	Text     string
	Time     time.Time
	Receipt  Receipt
	Quote    *Quote
	Reaction string
	// Thumb is a small JPEG preview for image and video messages.
	Thumb []byte
	// ImageA and ImageB are gradient colors used by demo data instead of Thumb.
	ImageA, ImageB uint32
}

// Chat is a one-to-one or group conversation. Messages are not part of it:
// the UI loads them from the Backend when the chat is opened.
type Chat struct {
	ID       string
	Name     string
	IsGroup  bool
	Pinned   bool
	Muted    bool
	Archived bool
	Favorite bool
	Unread   int
	Time     time.Time // last activity, used for ordering
	Last     *Message
	Typing   string // who is typing; empty when nobody is
	Presence string // header subtitle, e.g. "online"
}

// ConnState is the backend's connection/login state.
type ConnState int

const (
	StateStarting   ConnState = iota
	StateQR                   // waiting for the user to scan Code
	StateQRExpired            // QR codes ran out; Backend.Retry shows new ones
	StateConnecting           // logged in, connecting
	StateOnline               // logged in and connected
	StateOffline              // logged in, connection lost (reconnecting)
	StateError                // unrecoverable; see ConnEvent.Err
)

// LoggedIn reports whether the main chat UI should be shown.
func (s ConnState) LoggedIn() bool {
	return s == StateConnecting || s == StateOnline || s == StateOffline
}

// Event is something the backend reports to the UI.
type Event interface{ isEvent() }

// ConnEvent reports a connection or login state change.
type ConnEvent struct {
	State ConnState
	QR    string // pairing code to render, for StateQR
	Err   string // for StateError
	Me    string // own display name, when known
}

// ChatsEvent replaces the whole chat list, e.g. after a history sync chunk.
type ChatsEvent struct{ Chats []*Chat }

// ChatEvent inserts or updates one chat.
type ChatEvent struct{ Chat *Chat }

// MessageEvent inserts or updates a message (matched by ID).
type MessageEvent struct{ Msg *Message }

// ReceiptEvent upgrades the receipt of outgoing messages.
type ReceiptEvent struct {
	ChatID  string
	IDs     []string
	Receipt Receipt
}

// TypingEvent reports that someone started or stopped typing.
type TypingEvent struct {
	ChatID string
	Who    string
	Typing bool
}

// PresenceEvent updates a chat's header subtitle ("online", "last seen …").
type PresenceEvent struct {
	ChatID string
	Text   string
}

// SyncEvent reports initial history sync progress (0–100).
type SyncEvent struct{ Percent int }

func (ConnEvent) isEvent()     {}
func (ChatsEvent) isEvent()    {}
func (ChatEvent) isEvent()     {}
func (MessageEvent) isEvent()  {}
func (ReceiptEvent) isEvent()  {}
func (TypingEvent) isEvent()   {}
func (PresenceEvent) isEvent() {}
func (SyncEvent) isEvent()     {}

// Backend is everything the UI needs from a WhatsApp connection.
//
// Methods are called from the UI goroutine and must not block for long.
// Backends queue events and call the notify function passed to Start; the UI
// then drains them with Poll on its next frame.
type Backend interface {
	Start(notify func())
	Poll() []Event
	Chats() []*Chat
	Messages(chatID string, limit int) []*Message
	// Open is called when the user opens a chat: mark it read, subscribe to presence.
	Open(chatID string)
	// Send queues a text message and returns it in its pending state.
	Send(chatID, text string) *Message
	// Retry restarts pairing after the QR codes expired.
	Retry()
	Close()
}
