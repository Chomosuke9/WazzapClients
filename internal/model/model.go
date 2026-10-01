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

// Kind is how a message is drawn.
type Kind int

const (
	KindText    Kind = iota
	KindImage        // photo or video: thumbnail/picture above an optional caption
	KindDeleted      // "This message was deleted"
	KindSticker      // borderless picture
	// KindUnsupported is a message of a type the app can't show.
	KindUnsupported
)

// ButtonKind is what a message button does.
type ButtonKind int

const (
	ButtonReply ButtonKind = iota // sends a quick reply
	ButtonURL                     // opens a link
	ButtonCopy                    // copies a code
)

// Button is one of the buttons under a business or bot message.
type Button struct {
	Kind  ButtonKind
	Label string
	// Value is the reply's ID, the link or the code to copy.
	Value string
}

// Media identifies attachment types, for preview icons and labels.
type Media int

const (
	MediaNone Media = iota
	MediaImage
	MediaVideo
	MediaGIF
	MediaVoice
	MediaAudio
	MediaDocument
	MediaSticker
	MediaLocation
	MediaContact
	MediaPoll
)

// Quote is the message a reply points to.
type Quote struct {
	ID       string // quoted message ID, to jump to it
	Sender   string
	SenderID string
	Text     string
	Media    Media
}

// Message is a single conversation entry.
type Message struct {
	ID       string
	ChatID   string
	Kind     Kind
	Media    Media
	Duration int // seconds, for voice messages
	FromMe   bool
	Sender   string // display name; group chats only
	SenderID string // sender JID; group chats only
	Text     string
	Time     time.Time
	Receipt  Receipt
	Quote    *Quote
	Reaction string
	// Starred and Pinned mirror the message menu's Star and Pin.
	Starred, Pinned bool
	Forwarded       bool
	// Footer is the small print under a business message's text, and
	// Buttons its buttons.
	Footer  string
	Buttons []Button
	// Thumb is a small JPEG preview for image and video messages.
	Thumb []byte
	// FileName, FileSize (bytes), FileType (MIME type) and Pages describe a
	// document or audio file. A document's Text is its caption, or its file
	// name when it has none.
	FileName string
	FileSize int64
	FileType string
	Pages    int
	// Waveform is a voice message's loudness over time: up to 64 samples
	// from 0 to 100.
	Waveform []byte
	// ImageA and ImageB are gradient colors used by demo data instead of Thumb.
	ImageA, ImageB uint32
}

// Chat is a one-to-one or group conversation. Messages are not part of it:
// the UI loads them from the Backend when the chat is opened.
type Chat struct {
	ID      string
	Name    string
	IsGroup bool
	Pinned  bool
	Muted   bool
	// MuteUntil is when a timed mute ends; zero while muted means always.
	MuteUntil time.Time
	Archived  bool
	Favorite  bool
	Self      bool // the "message yourself" chat
	// Unread counts unread messages; -1 means marked as unread.
	Unread   int
	Time     time.Time // last activity, used for ordering
	Last     *Message
	Typing   string // who is typing; empty when nobody is
	TypingID string // in groups, the ID of who is typing
	Presence string // header subtitle, e.g. "online"
}

// Message text shows a resolved @mention as "\u2068@Name\u2069" (Unicode
// isolate marks, invisible). A mark right after U+2068 says whom it notifies.
const (
	// MentionNotifies marks a mention that notifies you: of you, or @all.
	MentionNotifies = '\u2063'
	// MentionAdmins marks "@admin", which notifies the group's admins.
	MentionAdmins = '\u2062'
)

// Draft is an outgoing text message.
type Draft struct {
	Text string
	// Reply is the message being answered, or nil.
	Reply *Message
	// Mentions are the JIDs of people @mentioned in Text, which refers to
	// them as "@<user part of the JID>".
	Mentions []string
	// MentionAll means Text contains "@all", which notifies every member.
	MentionAll bool
	// MentionAdmins means Text contains "@<group JID>", shown as "@admin";
	// Mentions then lists the group's admins.
	MentionAdmins bool
}

// Attachment is a file to send.
type Attachment struct {
	Path string
	// Media is how it is sent: MediaImage, MediaVideo, MediaAudio or
	// MediaDocument (any file, as is).
	Media Media
	// Quality is how a photo is scaled and compressed.
	Quality Quality
}

// Quality is the size a photo is sent at.
type Quality int

const (
	// QualityStandard fits a photo in 1600 px, compressed: small and
	// quick to send, like WhatsApp's default.
	QualityStandard Quality = iota
	// QualityHD fits it in 4096 px, less compressed.
	QualityHD
	// QualityRaw sends a JPEG or PNG file as it is.
	QualityRaw
)

// Poll is a poll to send.
type Poll struct {
	Question string
	Options  []string
	// Multiple lets voters pick more than one option.
	Multiple bool
}

// ChatList is a custom chat list ("Add to list").
type ChatList struct {
	ID, Name string
	Chats    []string
}

// Member is a group participant, as listed in the group info panel.
type Member struct {
	ID    string
	Name  string
	Admin bool
	Me    bool
}

// ChatInfo is what the contact or group info panel shows.
type ChatInfo struct {
	ID      string
	Name    string
	IsGroup bool
	// About is a contact's "about" text or a group's description.
	About string
	// Phone is a contact's formatted phone number.
	Phone string
	// Members lists group participants: you first, then admins, then the rest.
	Members []Member
	// Created and CreatedBy describe who made a group and when ("you" or a name).
	Created   time.Time
	CreatedBy string
	// Disappearing is the disappearing-messages timer in seconds (0 = off).
	Disappearing uint32
	// MediaCount counts media, links and documents; Media holds the newest
	// pictures to preview.
	MediaCount int
	Media      []*Message
}

// StatusUpdate is one status post.
type StatusUpdate struct {
	ID    string
	Media Media
	Text  string
	Thumb []byte
	// Background is the ARGB color behind a text status.
	Background uint32
	Time       time.Time
	Viewed     bool
}

// StatusThread is everything one contact posted in the last 24 hours,
// oldest first.
type StatusThread struct {
	ID      string // poster's JID
	Name    string
	Mine    bool
	Updates []*StatusUpdate
}

// Last returns the newest update.
func (t *StatusThread) Last() *StatusUpdate { return t.Updates[len(t.Updates)-1] }

// Viewed reports whether every update has been seen.
func (t *StatusThread) Viewed() bool {
	for _, u := range t.Updates {
		if !u.Viewed {
			return false
		}
	}
	return true
}

// Channel is a WhatsApp channel (newsletter), followed or suggested.
type Channel struct {
	ID        string
	Name      string
	Verified  bool
	Followers int
	Following bool
	Muted     bool
	Unread    int
	Time      time.Time
	Last      *Message
}

// Community groups its linked groups. Announcements is the announcement
// group's JID; Groups are the other linked groups the user is in.
type Community struct {
	ID            string
	Name          string
	Announcements string
	Groups        []string
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
	MeID  string // own JID, when known
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
	WhoID  string // in groups
	Typing bool
}

// PresenceEvent updates a chat's header subtitle ("online", "last seen …").
type PresenceEvent struct {
	ChatID string
	Text   string
}

// SyncEvent reports initial history sync progress (0–100).
type SyncEvent struct{ Percent int }

// AvatarEvent reports that the profile picture of ID became available.
type AvatarEvent struct{ ID string }

// MediaEvent reports that a message's media finished downloading.
type MediaEvent struct {
	ChatID, MsgID string
	Failed        bool // the download failed; a NoticeEvent says why
}

// InfoEvent reports that the info panel details of a chat changed.
type InfoEvent struct{ ChatID string }

// StatusEvent reports that the status list changed.
type StatusEvent struct{}

// ChannelsEvent reports that the followed or suggested channels changed.
type ChannelsEvent struct{}

// CommunitiesEvent reports that the community structure changed.
type CommunitiesEvent struct{}

// StickersEvent reports that the recent or favourite stickers changed.
type StickersEvent struct{}

// NoticeEvent is a short message for a toast ("Saved to Downloads").
type NoticeEvent struct{ Text string }

// DeletedEvent reports that messages were removed from a chat (deleted for
// you, or the chat was cleared). IDs is nil when the whole chat was cleared.
type DeletedEvent struct {
	ChatID string
	IDs    []string
}

func (ConnEvent) isEvent()        {}
func (ChatsEvent) isEvent()       {}
func (ChatEvent) isEvent()        {}
func (MessageEvent) isEvent()     {}
func (ReceiptEvent) isEvent()     {}
func (TypingEvent) isEvent()      {}
func (PresenceEvent) isEvent()    {}
func (SyncEvent) isEvent()        {}
func (AvatarEvent) isEvent()      {}
func (MediaEvent) isEvent()       {}
func (InfoEvent) isEvent()        {}
func (StatusEvent) isEvent()      {}
func (ChannelsEvent) isEvent()    {}
func (CommunitiesEvent) isEvent() {}
func (NoticeEvent) isEvent()      {}
func (StickersEvent) isEvent()    {}
func (DeletedEvent) isEvent()     {}

// StickerSet is a tab of the sticker picker.
type StickerSet int

const (
	StickersRecent   StickerSet = iota // sent recently, from any of the account's devices
	StickersFavorite                   // favourited on any device
	StickersReceived                   // received in chats
)

// Backend is everything the UI needs from a WhatsApp connection.
//
// Methods are called from the UI goroutine and must not block for long.
// Backends queue events and call the notify function passed to Start; the UI
// then drains them with Poll on its next frame.
type Backend interface {
	Start(notify func())
	Poll() []Event
	Chats() []*Chat
	// Messages returns the newest limit messages of a chat, oldest first.
	Messages(chatID string, limit int) []*Message
	// MessagesBefore returns up to limit messages older than message id,
	// oldest first.
	MessagesBefore(chatID, id string, limit int) []*Message
	// MessagesFrom returns up to limit messages from message id (included)
	// on, oldest first. It returns none when id isn't stored.
	MessagesFrom(chatID, id string, limit int) []*Message
	// PinnedMessage returns the chat's most recently pinned message, or nil.
	PinnedMessage(chatID string) *Message
	// Open is called when the user opens a chat: mark it read, subscribe to presence.
	Open(chatID string)
	// Send queues a text message and returns it in its pending state.
	Send(chatID string, d Draft) *Message
	// PressButton answers a message's quick-reply button (Buttons[i]) and
	// returns the answer in its pending state, or nil.
	PressButton(m *Message, i int) *Message
	// SendSticker sends a sticker that was received before, again, as a
	// reply to reply if it isn't nil.
	SendSticker(chatID string, sticker, reply *Message)
	// Stickers lists one of the sticker picker's sets, newest first.
	Stickers(set StickerSet) []*Message
	// Forward sends copies of messages to other chats.
	Forward(msgs []*Message, chatIDs []string)
	// React sets (or, with "", removes) your reaction to a message.
	React(m *Message, emoji string)
	// Delete deletes a message for you, or for everyone (your own messages).
	Delete(m *Message, forEveryone bool)
	// Star stars or unstars a message.
	Star(m *Message, starred bool)
	// PinMessage pins a message to the top of its chat, or unpins it.
	PinMessage(m *Message, pinned bool)
	// SaveMedia saves a message's picture or file to the Downloads folder
	// in the background; a NoticeEvent reports the result.
	SaveMedia(m *Message)
	// OpenMedia opens a video, voice message, audio file or document with
	// the system's app for it, downloading it first in the background; a
	// NoticeEvent reports failures.
	OpenMedia(m *Message)
	// MediaFile returns the path of a downloaded video, voice message,
	// audio file or document, or "" while it downloads in the background;
	// a MediaEvent announces the end.
	MediaFile(m *Message) string
	// HasMediaFile reports whether MediaFile has the file already.
	HasMediaFile(m *Message) bool
	// SendFile sends a file, with the draft's text as its caption (and its
	// reply and mentions), and returns it in its pending state. The upload
	// runs in the background.
	SendFile(chatID string, a Attachment, d Draft) *Message
	// SendContacts shares contacts (one-to-one chat IDs) as contact cards.
	SendContacts(chatID string, contactIDs []string) *Message
	// SendPoll sends a poll.
	SendPoll(chatID string, p Poll) *Message

	// Chat list actions. Each is followed by a ChatEvent (or ChatsEvent).
	SetArchived(chatID string, archived bool)
	SetMuted(chatID string, muted bool)
	SetPinned(chatID string, pinned bool)
	SetUnread(chatID string, unread bool)
	SetFavorite(chatID string, favorite bool)
	// Lists returns the custom chat lists.
	Lists() []*ChatList
	SetInList(chatID, listID string, in bool)
	// ClearChat deletes a chat's messages; DeleteChat removes the chat too.
	ClearChat(chatID string)
	DeleteChat(chatID string)
	// LeaveGroup exits a group.
	LeaveGroup(chatID string)

	// Pref and SetPref keep small UI preferences (recent emoji).
	Pref(key string) string
	SetPref(key, value string)

	// Avatar returns the cached profile picture (JPEG) of a chat or user,
	// or nil. A missing or stale picture is fetched in the background and
	// announced with an AvatarEvent. Safe to call from any goroutine.
	Avatar(id string) []byte
	// MediaData returns a downloaded image or sticker, or nil. Missing media
	// is downloaded in the background and announced with a MediaEvent. Safe
	// to call from any goroutine.
	MediaData(chatID, msgID string) []byte
	// Info returns the contact or group details for the info panel. They
	// may be stale or nil; fresh ones are fetched in the background and
	// announced with an InfoEvent.
	Info(chatID string) *ChatInfo
	// Statuses lists the last 24 hours of status updates, own thread first.
	Statuses() []*StatusThread
	// ViewStatus marks a status update as seen.
	ViewStatus(threadID, statusID string)
	// Channels lists followed channels, newest activity first.
	Channels() []*Channel
	// SuggestedChannels lists channels to follow.
	SuggestedChannels() []*Channel
	// FollowChannel follows a suggested channel; a ChannelsEvent follows.
	FollowChannel(id string)
	// Communities lists the user's communities.
	Communities() []*Community
	// Retry restarts pairing after the QR codes expired.
	Retry()
	// Logout unlinks this device and returns to the QR screen.
	Logout()
	Close()
}
