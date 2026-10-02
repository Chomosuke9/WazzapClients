// Package model holds the data types shared by the UI and its backends
// (the real WhatsApp connection in internal/wa, or demo data in internal/mock).
package model

import (
	"strings"
	"time"
	"unicode"
)

// Receipt is the delivery state of an outgoing message.
type Receipt int

const (
	Pending Receipt = iota
	Sent
	Delivered
	Read
)

// Failed is an outgoing message that couldn't be sent. It comes before
// Pending, so receipts that only move forward still lift it if the message
// turns out to have gone out after all.
const Failed Receipt = -1

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
	// Thumb is a small JPEG preview for image and video messages, and a
	// link preview's picture.
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
	// Album is the ID of the album a photo or video was sent in, with
	// others, or "". The chat shows an album's pictures as one grid.
	Album string
	// Edited is when the text (or caption) was last edited; zero when it
	// never was.
	Edited time.Time
	// Link is the preview of a link in Text, or nil. Its picture is Thumb.
	Link *LinkPreview
}

// LinkPreview is the card a text message shows for a link in it: what the
// sender's app read from the page.
type LinkPreview struct {
	URL         string // the link as it appears in the text
	Title       string
	Description string
}

// Shown reports whether a message with text should show the preview: an
// edit can take the link out.
func (l *LinkPreview) Shown(text string) bool {
	return l != nil && (l.Title != "" || l.Description != "") &&
		(l.URL == "" || strings.Contains(text, l.URL))
}

// EditWindow is how long after sending a message you can edit it, as in
// WhatsApp.
const EditWindow = 15 * time.Minute

// CanEdit reports whether you can still edit m at now: one of your own
// sent text messages, photos or videos.
func (m *Message) CanEdit(now time.Time) bool {
	if !m.FromMe || m.Kind == KindDeleted || m.Kind == KindUnsupported || m.Forwarded ||
		m.Receipt == Pending || m.Receipt == Failed || now.Sub(m.Time) > EditWindow {
		return false
	}
	switch m.Media {
	case MediaNone:
		return m.Text != ""
	case MediaImage, MediaVideo, MediaGIF:
		return true
	}
	return false
}

// Version is one earlier text of an edited message.
type Version struct {
	Text string    // as Message.Text shows it
	Time time.Time // when it was sent or edited to this
}

// MentionRef is an @mention in text to edit: the text shows "@" + Name,
// and ID is who it notifies (a JID, or "@all" or "@admin").
type MentionRef struct {
	Name, ID string
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
	Unread int
	// Mentioned means one of the unread messages of a group mentions you
	// (or everyone) or replies to you. It only counts while Unread > 0.
	Mentioned bool
	Time      time.Time // last activity, used for ordering
	Last      *Message
	Typing    string // who is typing; empty when nobody is
	TypingID  string // in groups, the ID of who is typing
	Presence  string // header subtitle, e.g. "online"
}

// Message text shows a resolved @mention as "\u2068@Name\u2069" (Unicode
// isolate marks, invisible). A mark right after U+2068 says whom it notifies.
const (
	// MentionNotifies marks a mention that notifies you: of you, or @all.
	MentionNotifies = '\u2063'
	// MentionAdmins marks "@admin", which notifies the group's admins.
	MentionAdmins = '\u2062'
)

// ForMe reports whether m, as a backend resolved it, mentions you (or
// everyone) or replies to one of your messages. meID is your JID, which a
// reply's SenderID may hold when its Sender isn't "You".
func (m *Message) ForMe(meID string) bool {
	if strings.ContainsRune(m.Text, MentionNotifies) {
		return true
	}
	q := m.Quote
	return q != nil && (q.Sender == "You" || q.SenderID != "" && q.SenderID == meID)
}

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
	// Link is the preview of a link in Text to send with it, or nil, and
	// LinkThumb its picture (a small JPEG).
	Link      *LinkPreview
	LinkThumb []byte
}

// Attachment is a file to send.
type Attachment struct {
	Path string
	// Media is how it is sent: MediaImage, MediaVideo, MediaAudio or
	// MediaDocument (any file, as is).
	Media Media
	// Quality is how a photo is scaled and compressed.
	Quality Quality
	// ViewOnce sends a photo or video that can be opened only once.
	ViewOnce bool
	// Album puts a photo or video in the album NewAlbum opened.
	Album string
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
	ID   string
	Name string
	// Contact is the name you saved them under (or their business's name),
	// Push the name they gave themselves, and Phone their formatted phone
	// number, each when known. Name is the first of Contact, Phone and
	// "~Push". The pickers that find people as you type use them.
	Contact, Push, Phone string
	Admin                bool
	Me                   bool
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
	// Announce means only a group's admins can send messages, and Locked
	// that only they can edit its info. AdminsAdd means only they can add
	// members, and Approval that they approve who joins.
	Announce, Locked    bool
	AdminsAdd, Approval bool
	// MediaCount counts media, links and documents; Media holds the newest
	// pictures to preview.
	MediaCount int
	Media      []*Message

	// The rest describes contacts only.

	// Business is a business account's profile, or nil for a person.
	Business *Business
	// Blocked reports whether you blocked the contact.
	Blocked bool
	// Common lists the groups you share with the contact.
	Common []CommonGroup
}

// Business is a business account's public profile.
type Business struct {
	// Name is the verified business name.
	Name        string
	Category    string
	Description string
	Address     string
	Email       string
	Websites    []string
	// TimeZone is the IANA zone that Hours are in ("" when unknown).
	TimeZone string
	Hours    []BusinessHours
}

// BusinessHours is when a business is open on one day of the week.
type BusinessHours struct {
	Day time.Weekday
	// Mode is "open_24h", "appointment_only" or "specific_hours".
	Mode string
	// Open and Close are minutes after midnight, for specific hours.
	Open, Close int
}

// Contact is a saved contact who is on WhatsApp.
type Contact struct {
	ID    string // the one-to-one chat ID
	Name  string
	Phone string // formatted, when known
}

// NewGroup is a group to create.
type NewGroup struct {
	Name    string
	Members []string // one-to-one chat IDs; you are added anyway
	// Photo is the group's picture, a JPEG, or nil.
	Photo []byte
	// Disappearing is the disappearing-messages timer in seconds (0 = off).
	Disappearing uint32
}

// CommonGroup is a group you share with a contact.
type CommonGroup struct {
	ID   string
	Name string
	// Community and CommunityID name the community the group belongs to,
	// if any.
	Community, CommunityID string
	// Members lists the members like the group's header does.
	Members string
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

// StatusPost is a status update to post: Text on a Background color, or
// a photo or video (File) with Text as its caption.
type StatusPost struct {
	Text       string
	Background uint32 // ARGB, for text
	File       *Attachment
}

// StatusAudience is who sees your status updates, as set on your phone.
type StatusAudience int

const (
	AudienceContacts StatusAudience = iota // all your contacts
	AudienceExcept                         // your contacts except some
	AudienceOnly                           // only some contacts
)

// StatusPrivacy is your status privacy setting. Count is how many contacts
// the "except" or "only" list holds.
type StatusPrivacy struct {
	Audience StatusAudience
	Count    int
}

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
type MessageEvent struct {
	Msg *Message
	// New marks a message that just arrived, which may notify: not one
	// from history, an edit, a reaction or one seen before.
	New bool
}

// ReceiptEvent upgrades the receipt of outgoing messages, or marks pending
// ones Failed.
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

// GalleryKind is what Backend.Gallery lists.
type GalleryKind int

const (
	GalleryMedia   GalleryKind = iota // photos, videos and GIFs
	GalleryDocs                       // documents
	GalleryLinks                      // messages with web links
	GalleryStarred                    // starred messages
)

// GalleryQuery asks Backend.Gallery for a page of a chat's (or, with
// ChatID "", every chat's) media, documents, links or starred messages.
type GalleryQuery struct {
	Kind   GalleryKind
	ChatID string
	// Text keeps the messages whose SearchKey (or file name) contains its.
	Text string
	// Oldest lists the oldest first instead of the newest.
	Oldest bool
	// Offset and Limit pick the page.
	Offset, Limit int
}

// GalleryEvent brings a page of Backend.Gallery. More reports that there
// are more after it.
type GalleryEvent struct {
	Query GalleryQuery
	Msgs  []*Message
	More  bool
}

// SecurityCodeEvent answers Backend.SecurityCode: the 60 digit code that
// both sides of a chat see, or Err.
type SecurityCodeEvent struct {
	ChatID, Code, Err string
}

// MemberChange is a group member joining, leaving or changing role.
type MemberChange struct {
	Time time.Time
	// Name is whose membership changed, and By who did it ("" when they
	// did it themselves or it isn't known).
	Name, By string
	Action   MemberAction
}

// MemberAction is what a MemberChange did.
type MemberAction int

const (
	MemberJoined MemberAction = iota
	MemberLeft
	MemberAdded
	MemberRemoved
	MemberPromoted
	MemberDemoted
)

// SearchEvent brings the results of Backend.SearchMessages.
type SearchEvent struct {
	ChatID, Query string
	Msgs          []*Message
}

// PhoneEvent answers Backend.LookupPhone. ID is the number's chat ID, or
// "" when it isn't on WhatsApp; Err is set when the check failed.
type PhoneEvent struct {
	Phone string // as passed to LookupPhone
	ID    string
	Name  string // a name to show, when known
	Err   string
}

// GroupCreatedEvent answers Backend.CreateGroup: ChatID is the new group,
// or "" when it couldn't be created (Err says why).
type GroupCreatedEvent struct {
	ChatID string
	Err    string
}

// AccountEvent reports that your profile, privacy settings or blocked
// contacts (Backend.Account) changed.
type AccountEvent struct{}

// GroupAction is what a GroupRequest does.
type GroupAction int

const (
	GroupAdd     GroupAction = iota // add Members
	GroupRemove                     // remove Members
	GroupPromote                    // make Members admins
	GroupDemote                     // dismiss Members as admins
	// GroupAnnounce lets only admins send messages (On), or everyone.
	GroupAnnounce
	// GroupLock lets only admins edit the group's info (On), or everyone.
	GroupLock
	// GroupDescription sets the description to Text ("" removes it).
	GroupDescription
	// GroupLink gets the invite link; On resets it, so the old one stops
	// working.
	GroupLink
	// GroupSendInvite sends Members[0] the Invite that GroupAdd got for them,
	// as a message in your chat with them.
	GroupSendInvite
	// GroupAddMode lets only admins add members (On), or everyone.
	GroupAddMode
	// GroupApproval makes admins approve new members (On), or not.
	GroupApproval
	// GroupName renames the group to Text.
	GroupName
)

// GroupRequest is a change to a group you administer (Backend.ManageGroup).
type GroupRequest struct {
	// Ref is copied to the GroupEvent that answers the request.
	Ref    string
	ChatID string
	Action GroupAction
	// Members are chat IDs, or for GroupAdd also phone numbers with
	// their country code ("+62 812 5550 1234").
	Members []string
	On      bool
	Text    string
	Invite  *GroupInvite
}

// GroupInvite is an invitation to join a group, which WhatsApp hands out
// when someone's privacy settings don't let you add them.
type GroupInvite struct {
	Code    string
	Expires time.Time
}

// GroupEvent answers Backend.ManageGroup.
type GroupEvent struct {
	Ref    string
	ChatID string
	// Err says why the request failed, or is "".
	Err string
	// Link is the invite link, for GroupLink.
	Link string
	// Members is what happened to each member, for GroupAdd,
	// GroupRemove, GroupPromote and GroupDemote.
	Members []MemberResult
}

// MemberResult is what a GroupRequest did to one member.
type MemberResult struct {
	ID   string
	Name string
	// Err says why it failed for them, or is "".
	Err string
	// Invite is set when their privacy settings refused GroupAdd; a
	// GroupSendInvite request sends it to them.
	Invite *GroupInvite
}

// GroupPreview is what a group's invite link tells about the group
// before you join it.
type GroupPreview struct {
	ID          string // the group's JID
	Name        string
	Description string
	Created     time.Time
	// Size counts the group's members, and Faces are a few of them (chat
	// IDs) to show.
	Size  int
	Faces []string
	// Approval means an admin must approve your request to join.
	Approval bool
	// Member means you are in the group already.
	Member bool
	// Community means the link is a community's, which this app can't join.
	Community bool
}

// InviteEvent answers Backend.GroupInvite. Group is nil when the link
// can't be used, and Err says why. The group's picture, if it has one,
// is the Avatar of Group.ID by then.
type InviteEvent struct {
	Code  string
	Group *GroupPreview
	Err   string
}

// JoinedEvent answers Backend.JoinGroup: ChatID is the group you joined,
// after a ChatEvent for it; Requested means an admin must approve your
// request first. Err says why it failed.
type JoinedEvent struct {
	Code      string
	ChatID    string
	Requested bool
	Err       string
}

// DeletedEvent reports that messages were removed from a chat (deleted for
// you, or the chat was cleared). IDs is nil when the whole chat was cleared.
type DeletedEvent struct {
	ChatID string
	IDs    []string
}

func (ConnEvent) isEvent()         {}
func (ChatsEvent) isEvent()        {}
func (ChatEvent) isEvent()         {}
func (MessageEvent) isEvent()      {}
func (ReceiptEvent) isEvent()      {}
func (TypingEvent) isEvent()       {}
func (PresenceEvent) isEvent()     {}
func (SyncEvent) isEvent()         {}
func (AvatarEvent) isEvent()       {}
func (MediaEvent) isEvent()        {}
func (InfoEvent) isEvent()         {}
func (StatusEvent) isEvent()       {}
func (ChannelsEvent) isEvent()     {}
func (CommunitiesEvent) isEvent()  {}
func (NoticeEvent) isEvent()       {}
func (PhoneEvent) isEvent()        {}
func (GroupCreatedEvent) isEvent() {}
func (StickersEvent) isEvent()     {}
func (DeletedEvent) isEvent()      {}
func (SearchEvent) isEvent()       {}
func (GalleryEvent) isEvent()      {}
func (SecurityCodeEvent) isEvent() {}
func (AccountEvent) isEvent()      {}
func (GroupEvent) isEvent()        {}
func (InviteEvent) isEvent()       {}
func (JoinedEvent) isEvent()       {}

// StickerSet is a tab of the sticker picker.
type StickerSet int

const (
	StickersRecent   StickerSet = iota // sent recently, from any of the account's devices
	StickersFavorite                   // favourited on any device
	StickersReceived                   // received in chats
)

// PrefHistorySync is the Pref key for how much chat history linking a
// device asks the phone for: HistoryFull, or a number of days (the login
// screen offers 30, 90, 180 and 365). It applies to the next QR code, so
// the UI calls Retry after changing it while a code is shown.
const PrefHistorySync = "history_sync"

// HistoryFull is the PrefHistorySync value for all of the chat history.
const HistoryFull = "full"

// HistoryDefaultDays is the history linking asks for when PrefHistorySync
// is unset.
const HistoryDefaultDays = 90

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
	// SearchMessages looks in the background for up to limit messages of
	// a chat whose SearchKey contains the query's, newest first. A
	// SearchEvent brings the results; a new search cancels the last one.
	SearchMessages(chatID, query string, limit int)
	// PinnedMessage returns the chat's most recently pinned message, or nil.
	PinnedMessage(chatID string) *Message
	// Open is called when the user opens a chat: mark it read, subscribe to presence.
	Open(chatID string)
	// MarkRead marks chats read in the background ("Mark all as read").
	MarkRead(chatIDs []string)
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
	// FavoriteSticker reports whether a sticker message's file is one of
	// your favourite stickers.
	FavoriteSticker(m *Message) bool
	// SetFavoriteSticker adds a sticker message's file to your favourite
	// stickers, or removes it, and syncs that to your phone.
	SetFavoriteSticker(m *Message, fav bool)
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
	// EditText returns the text of one of your messages to edit, with
	// each @mention as "@Name", and the mentions in it.
	EditText(m *Message) (string, []MentionRef)
	// Edit replaces the text or caption of one of your messages (see
	// CanEdit) with d's; d.Reply is ignored.
	Edit(m *Message, d Draft)
	// Versions returns the earlier texts of an edited message, oldest
	// first; m.Text is the newest.
	Versions(m *Message) []Version
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
	// NewAlbum opens an album of photos and videos to send together: it
	// sends the message that announces them and returns its ID, for each
	// file's Attachment.Album, or "" when it can't.
	NewAlbum(chatID string, photos, videos int) string
	// SendContacts shares contacts (one-to-one chat IDs) as contact cards.
	SendContacts(chatID string, contactIDs []string) *Message
	// SendPoll sends a poll.
	SendPoll(chatID string, p Poll) *Message
	// SendNewSticker sends a sticker made on this computer (a 512x512
	// WebP), as a reply to reply if it isn't nil, and returns it in its
	// pending state. The upload runs in the background.
	SendNewSticker(chatID string, webp []byte, reply *Message) *Message

	// Chat list actions. Each is followed by a ChatEvent (or ChatsEvent).
	SetArchived(chatID string, archived bool)
	// SetMuted mutes a chat for d, or for good when d is 0, or unmutes it.
	SetMuted(chatID string, muted bool, d time.Duration)
	SetPinned(chatID string, pinned bool)
	SetUnread(chatID string, unread bool)
	SetFavorite(chatID string, favorite bool)
	// Lists returns the custom chat lists.
	Lists() []*ChatList
	SetInList(chatID, listID string, in bool)
	// ClearChat deletes a chat's messages; DeleteChat removes the chat too.
	ClearChat(chatID string)
	DeleteChat(chatID string)
	// Contacts lists your saved contacts who are on WhatsApp, by name.
	Contacts() []*Contact
	// LookupPhone checks in the background whether a phone number (digits
	// with the country code) is on WhatsApp; a PhoneEvent answers.
	LookupPhone(phone string)
	// CreateGroup creates a group in the background; a GroupCreatedEvent
	// answers, after a ChatEvent for the new chat.
	CreateGroup(g NewGroup)
	// GroupInvite looks up the group of an invite link's code (the part
	// after https://chat.whatsapp.com/) in the background; an InviteEvent
	// answers.
	GroupInvite(code string)
	// JoinGroup joins the group of an invite link's code, or asks its
	// admins to let you in, in the background; a JoinedEvent answers.
	JoinGroup(code string)
	// LeaveGroup exits a group.
	LeaveGroup(chatID string)
	// ManageGroup changes a group you administer in the background. A
	// GroupEvent with the request's Ref answers, and an InfoEvent follows
	// when the group's details changed.
	ManageGroup(r GroupRequest)
	// SetBlocked blocks or unblocks a contact. An InfoEvent follows, and a
	// NoticeEvent on failure.
	SetBlocked(chatID string, blocked bool)
	// ExportChat writes a chat's messages as text into the Downloads folder
	// in the background; a NoticeEvent reports the result.
	ExportChat(chatID string)
	// SetDisappearing sets a chat's disappearing messages timer (0 turns
	// it off) in the background. An InfoEvent follows, or a NoticeEvent
	// on failure.
	SetDisappearing(chatID string, d time.Duration)
	// SecurityCode works out the security code of a one-to-one chat in the
	// background; a SecurityCodeEvent answers.
	SecurityCode(chatID string)
	// Gallery looks up a page of media, documents, links or starred
	// messages in the background; a GalleryEvent answers. A new query
	// cancels the last one.
	Gallery(q GalleryQuery)
	// MemberChanges lists who joined, left or changed role in a group
	// since this computer saw it, newest first.
	MemberChanges(chatID string) []MemberChange

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
	// PostStatus posts a status update. It shows in your own thread at
	// once (a StatusEvent follows) while it uploads and sends in the
	// background; a NoticeEvent reports failure.
	PostStatus(p StatusPost)
	// StatusPrivacy returns who sees your status updates, or nil while it
	// isn't known yet: it is fetched in the background and announced with
	// a StatusEvent.
	StatusPrivacy() *StatusPrivacy
	// Channels lists followed channels, newest activity first.
	Channels() []*Channel
	// SuggestedChannels lists channels to follow.
	SuggestedChannels() []*Channel
	// FollowChannel follows a suggested channel; a ChannelsEvent follows.
	FollowChannel(id string)
	// Communities lists the user's communities.
	Communities() []*Community
	// Account returns your profile and settings as last fetched. Fresh
	// ones are fetched in the background, once a session and again when
	// they change, and announced with an AccountEvent.
	Account() *Account
	// SetProfileName and SetAbout change your name and about text,
	// SetProfilePhoto your picture (from a file; "" removes it). An
	// AccountEvent follows, or a NoticeEvent on failure.
	SetProfileName(name string)
	SetAbout(about string)
	SetProfilePhoto(path string)
	// SetPrivacy changes one of your privacy settings (a Privacy* key)
	// to one of its values (a Who* value).
	SetPrivacy(key, value string)
	// SetDefaultTimer sets the disappearing messages timer of new chats
	// (0 turns it off).
	SetDefaultTimer(d time.Duration)
	// Retry restarts pairing after the QR codes expired, or with new QR
	// codes while they are shown (after PrefHistorySync changed).
	Retry()
	// Logout unlinks this device and returns to the QR screen.
	Logout()
	Close()
}

// SearchKey is the form of a text that message search compares: case
// folded (so K matches k and the Kelvin sign, σ matches Σ and ς), without
// the formatting markers (*_~`) and mention marks that the chat doesn't
// show, so "*hello* world" is found by "hello world".
func SearchKey(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch r {
		case '*', '_', '~', '`', '\u2068', '\u2069', MentionNotifies, MentionAdmins:
			continue
		}
		b.WriteRune(FoldRune(r))
	}
	return b.String()
}

// FoldRune maps every rune of a case-folding orbit (K, k and the Kelvin
// sign) to the same one: its smallest, in lower case when that is an
// ASCII letter.
func FoldRune(r rune) rune {
	if r < 0x80 && r != 'k' && r != 'K' && r != 's' && r != 'S' {
		if 'A' <= r && r <= 'Z' {
			return r + 'a' - 'A'
		}
		return r
	}
	m := r
	for f := unicode.SimpleFold(r); f != r; f = unicode.SimpleFold(f) {
		m = min(m, f)
	}
	if 'A' <= m && m <= 'Z' {
		m += 'a' - 'A'
	}
	return m
}

// Account is your own profile, account details and privacy settings.
type Account struct {
	ID       string // your JID
	Phone    string // in international format, with a leading +
	LID      string
	Name     string
	About    string
	Username string
	// Linked is when this device was linked; zero when not known.
	Linked time.Time
	// Privacy maps Privacy* keys to Who* values. A key that's missing
	// isn't known yet.
	Privacy map[string]string
	// DefaultTimer is the disappearing messages timer of new chats, when
	// TimerKnown.
	DefaultTimer time.Duration
	TimerKnown   bool
	// Blocked lists blocked contacts, once BlockedKnown.
	Blocked      []Contact
	BlockedKnown bool
}

// Privacy settings, the keys of Account.Privacy.
const (
	PrivacyLastSeen     = "last"
	PrivacyOnline       = "online"
	PrivacyPhoto        = "profile"
	PrivacyAbout        = "status"
	PrivacyGroups       = "groupadd"
	PrivacyReadReceipts = "readreceipts"
)

// Who can see or do something, the values of Account.Privacy.
const (
	WhoEveryone       = "all"
	WhoContacts       = "contacts"
	WhoContactsExcept = "contact_blacklist"
	WhoNobody         = "none"
	WhoSameAsLastSeen = "match_last_seen" // PrivacyOnline only
)
