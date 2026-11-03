package remote

import (
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

// What the API sends. Times are Unix seconds. Names stay short but
// readable: the watch's side service packs them further for Bluetooth.

// Chat is a chat in the list.
type Chat struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Group bool   `json:"group,omitempty"`
	Muted bool   `json:"muted,omitempty"`
	// Pinned chats come first in the list.
	Pinned bool `json:"pinned,omitempty"`
	// Unread counts the unread messages; a chat marked as unread has 1.
	Unread int `json:"unread,omitempty"`
	// Mentioned is set while an unread message mentions you or replies to you.
	Mentioned bool `json:"mentioned,omitempty"`
	// ReadOnly is a group you left: it can't be sent to.
	ReadOnly bool `json:"readOnly,omitempty"`
	// Archived is only ever set in a "chat" event: the chat leaves the list.
	Archived bool     `json:"archived,omitempty"`
	Time     int64    `json:"time"`
	Last     *Message `json:"last,omitempty"`
}

// Message is one message of a chat.
type Message struct {
	ID   string `json:"id"`
	Mine bool   `json:"mine,omitempty"`
	// Sender is who sent it, in groups.
	Sender string `json:"sender,omitempty"`
	// Kind is "text", "media", "deleted", "system", "viewonce", "waiting"
	// or "unsupported".
	Kind string `json:"kind"`
	// Media is "image", "video", "gif", "voice", "audio", "document",
	// "sticker", "location", "contact", "poll" or "event", with Text its
	// caption (a document's file name, a poll's question). The watch shows
	// a label instead of the media.
	Media    string `json:"media,omitempty"`
	Duration int    `json:"duration,omitempty"` // seconds, for voice and audio
	Text     string `json:"text,omitempty"`
	// Truncated means Text was cut; GET .../messages/{id} has all of it.
	Truncated bool   `json:"truncated,omitempty"`
	Time      int64  `json:"time"`
	Status    string `json:"status,omitempty"` // your messages': failed, pending, sent, delivered, read
	Edited    bool   `json:"edited,omitempty"`
	Forwarded bool   `json:"forwarded,omitempty"`
	Quote     *Quote `json:"quote,omitempty"`
}

// Quote is the message a reply answers.
type Quote struct {
	ID     string `json:"id"`
	Sender string `json:"sender,omitempty"`
	Text   string `json:"text,omitempty"`
	Media  string `json:"media,omitempty"`
}

// Event is something that changed since a cursor.
type Event struct {
	// Type is "message" (Message, new or changed, in Chat), "chat" (Chats[0]
	// changed), "receipt" (IDs of your messages in Chat reached Status),
	// "deleted" (IDs in Chat are gone), "conn" (State) or "reset" (reload
	// everything).
	Type    string   `json:"type"`
	Chat    string   `json:"chat,omitempty"`
	Message *Message `json:"message,omitempty"`
	// New marks a message that just arrived (not an edit or a reaction).
	New    bool     `json:"new,omitempty"`
	Chats  []Chat   `json:"chats,omitempty"`
	IDs    []string `json:"ids,omitempty"`
	Status string   `json:"status,omitempty"`
	State  string   `json:"state,omitempty"`
}

// textLimit is how much of a message's text lists carry, in runes.
const textLimit = 300

// quoteLimit is how much of a quoted message's text replies carry.
const quoteLimit = 60

// sendLimit is the longest text the watch may send, in runes.
const sendLimit = 4096

var mentionMarks = strings.NewReplacer("⁨", "", "⁩", "", string(model.MentionNotifies), "", string(model.MentionAdmins), "")

// clip returns s without mention marks, cut to at most n runes (n < 0
// keeps all of it), and whether it was cut.
func clip(s string, n int) (string, bool) {
	s = mentionMarks.Replace(s)
	if n < 0 || utf8.RuneCountInString(s) <= n {
		return s, false
	}
	i := 0
	for j := range s {
		if n == 0 {
			i = j
			break
		}
		n--
	}
	return strings.TrimRight(s[:i], " \n") + "…", true
}

func toChat(c *model.Chat) Chat {
	w := Chat{
		ID: c.ID, Name: c.Name, Group: c.IsGroup, Muted: c.Muted, Pinned: c.Pinned,
		Unread: c.Unread, ReadOnly: c.Left, Archived: c.Archived, Time: c.Time.Unix(),
	}
	if w.Unread < 0 {
		w.Unread = 1 // marked as unread
	}
	w.Mentioned = c.Mentioned && w.Unread > 0
	if c.Last != nil {
		m := toMessage(c.Last, quoteLimit)
		m.Quote = nil
		w.Last = &m
	}
	return w
}

// toMessage converts m, cutting its text to limit runes.
func toMessage(m *model.Message, limit int) Message {
	w := Message{
		ID: m.ID, Mine: m.FromMe, Kind: kindName(m), Media: mediaName(m.Media), Duration: m.Duration,
		Time: m.Time.Unix(), Edited: !m.Edited.IsZero(), Forwarded: m.Forwarded,
	}
	if !m.FromMe {
		w.Sender = m.Sender
	} else {
		w.Status = receiptName(m.Receipt)
	}
	if w.Kind != "deleted" && w.Kind != "waiting" {
		w.Text, w.Truncated = clip(m.Text, limit)
	}
	if w.Kind == "system" {
		w.Sender = ""
	}
	if q := m.Quote; q != nil {
		t, _ := clip(q.Text, quoteLimit)
		w.Quote = &Quote{ID: q.ID, Sender: q.Sender, Text: t, Media: mediaName(q.Media)}
	}
	return w
}

func kindName(m *model.Message) string {
	switch m.Kind {
	case model.KindDeleted:
		return "deleted"
	case model.KindSystem:
		return "system"
	case model.KindViewOnce:
		return "viewonce"
	case model.KindWaiting:
		return "waiting"
	case model.KindUnsupported:
		return "unsupported"
	}
	if m.Media != model.MediaNone {
		return "media"
	}
	return "text"
}

var mediaNames = map[model.Media]string{
	model.MediaImage:       "image",
	model.MediaVideo:       "video",
	model.MediaGIF:         "gif",
	model.MediaVoice:       "voice",
	model.MediaAudio:       "audio",
	model.MediaDocument:    "document",
	model.MediaSticker:     "sticker",
	model.MediaLocation:    "location",
	model.MediaContact:     "contact",
	model.MediaPoll:        "poll",
	model.MediaEventInvite: "event",
}

func mediaName(m model.Media) string { return mediaNames[m] }

func receiptName(r model.Receipt) string {
	switch r {
	case model.Failed:
		return "failed"
	case model.Sent:
		return "sent"
	case model.Delivered:
		return "delivered"
	case model.Read:
		return "read"
	}
	return "pending"
}

func stateName(s model.ConnState) string {
	switch s {
	case model.StateOnline:
		return "online"
	case model.StateConnecting, model.StateStarting:
		return "connecting"
	case model.StateOffline:
		return "offline"
	case model.StateQR, model.StateQRExpired:
		return "loggedout"
	}
	return "error"
}

// listed returns the chats the watch lists, in the chat list's order:
// pinned ones first, the latest pinned first, then the latest activity.
// Archived chats and ones without messages are left out, like channels,
// which backends don't list as chats.
func listed(chats []*model.Chat) []*model.Chat {
	out := make([]*model.Chat, 0, len(chats))
	for _, c := range chats {
		if !c.Archived && (c.Last != nil || c.Unread != 0) {
			out = append(out, c)
		}
	}
	slices.SortStableFunc(out, func(a, b *model.Chat) int {
		if a.Pinned != b.Pinned {
			if a.Pinned {
				return -1
			}
			return 1
		}
		if a.Pinned && !a.PinnedAt.Equal(b.PinnedAt) {
			return b.PinnedAt.Compare(a.PinnedAt)
		}
		return b.Time.Compare(a.Time)
	})
	return out
}

// event converts a backend event, or returns false for one the watch
// doesn't need.
func event(e model.Event) (Event, bool) {
	switch e := e.(type) {
	case model.MessageEvent:
		if e.Msg == nil {
			return Event{}, false
		}
		m := toMessage(e.Msg, textLimit)
		return Event{Type: "message", Chat: e.Msg.ChatID, Message: &m, New: e.New}, true
	case model.ChatEvent:
		if e.Chat == nil {
			return Event{}, false
		}
		return Event{Type: "chat", Chats: []Chat{toChat(e.Chat)}}, true
	case model.ReceiptEvent:
		return Event{Type: "receipt", Chat: e.ChatID, IDs: e.IDs, Status: receiptName(e.Receipt)}, true
	case model.DeletedEvent:
		return Event{Type: "deleted", Chat: e.ChatID, IDs: e.IDs}, true
	case model.ConnEvent:
		return Event{Type: "conn", State: stateName(e.State)}, true
	case model.ChatsEvent:
		return Event{Type: "reset"}, true
	}
	return Event{}, false
}
