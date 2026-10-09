package ui

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/chomosuke9/wazzapclients/internal/desktop"
	"github.com/chomosuke9/wazzapclients/internal/model"
	"github.com/chomosuke9/wazzapclients/internal/notify"
	"github.com/chomosuke9/wazzapclients/internal/ui/icon"
)

// Preferences (Backend.Pref) of Settings > Notifications and General.
// Each is on unless set to "off".
const (
	prefNotifyMessages = "notify_messages" // one-to-one chats
	prefNotifyGroups   = "notify_groups"
	prefNotifyPreviews = "notify_previews" // message text in notifications
	prefNotifySound    = "notify_sound"
	prefBackground     = "background" // keep running when the window closes
)

func prefOn(b model.Backend, key string) bool { return b.Pref(key) != "off" }

func setPref(b model.Backend, key string, on bool) {
	v := ""
	if !on {
		v = "off"
	}
	b.SetPref(key, v)
}

// notifyDelay is how long new messages wait before they notify, so a
// burst (messages that came while offline) makes one notification per
// chat, and the chat's own update, which may follow its first message,
// is in.
const notifyDelay = 400 * time.Millisecond

// notifier decides which new messages show a notification, as WhatsApp
// does: one per chat, replaced as more come and taken away once the chat
// is read. Muted and archived chats stay quiet unless a message mentions
// you or replies to you. Nothing shows while the window has focus, unless
// the messages are a background account's, which the window doesn't show.
//
// Each account has one, the open one's and those in the background
// (background.go). It runs on the UI goroutine and sees every event of
// its account's backend, with or without a window.
type notifier struct {
	b       model.Backend
	enabled bool // notify.Init succeeded
	// prefix goes before chat IDs in the IDs of notifications (noteID):
	// the account's, when there are accounts.
	prefix string
	// account names the account in titles while it's in the background.
	account string
	// app holds the app's preferences (privacy mode), which only the open
	// account's backend has up to date; nil reads them from b.
	app func() model.Backend

	// What the notifier does; tests replace them.
	show    func(notify.Notification)
	remove  func(id string)
	tooltip func(string)
	focused func() bool // the window has focus
	now     func() time.Time
	// arm tells the host that pending messages wait for flushAt; nil in
	// tests.
	arm func()

	meID    string
	chats   map[string]chatInfo
	pending []*model.Message // new messages waiting for flush
	flushAt time.Time        // when pending flushes; zero while none wait
	shown   map[string]int   // chats with a notification: messages it counts
	unread  int              // chats with unread messages, for the tooltip
}

// chatInfo is what the notifier knows of a chat. It's a copy: the UI
// changes its chats in place.
type chatInfo struct {
	name            string
	group, archived bool
	muted           bool
	muteUntil       time.Time
	unread          int
}

// showNote and removeNote show and remove system notifications; tests
// replace them.
var (
	showNote   = notify.Show
	removeNote = notify.Remove
)

func newNotifier(b model.Backend, h *host) *notifier {
	return &notifier{
		b:       b,
		show:    func(x notify.Notification) { showNote(x) },
		remove:  func(id string) { removeNote(id) },
		tooltip: desktop.SetTooltip,
		focused: func() bool { return h.focused },
		now:     time.Now,
		chats:   map[string]chatInfo{},
		shown:   map[string]int{},
		unread:  -1,
	}
}

func infoOf(c *model.Chat) chatInfo {
	return chatInfo{name: c.Name, group: c.IsGroup, archived: c.Archived, muted: c.Muted,
		muteUntil: c.MuteUntil, unread: c.Unread}
}

func (n *notifier) setChats(chats []*model.Chat) {
	clear(n.chats)
	for _, c := range chats {
		n.chats[c.ID] = infoOf(c)
	}
	for id := range n.shown {
		if n.chats[id].unread == 0 {
			n.read(id)
		}
	}
	n.updateTooltip()
}

// event takes in a backend event.
func (n *notifier) event(ev model.Event) {
	switch e := ev.(type) {
	case model.ConnEvent:
		if e.MeID != "" {
			n.meID = e.MeID
		}
	case model.ChatsEvent:
		n.setChats(e.Chats)
	case model.ChatEvent:
		old, had := n.chats[e.Chat.ID]
		n.chats[e.Chat.ID] = infoOf(e.Chat)
		if had && old.unread != 0 && e.Chat.Unread == 0 {
			n.read(e.Chat.ID) // read on another device
		}
		n.updateTooltip()
	case model.MessageEvent:
		if e.New && !e.Msg.FromMe {
			n.pending = append(n.pending, e.Msg)
			if n.flushAt.IsZero() {
				n.flushAt = n.now().Add(notifyDelay)
				if n.arm != nil {
					n.arm()
				}
			}
		}
	}
}

// noteID is the ID of chat id's notification.
func (n *notifier) noteID(id string) string { return n.prefix + id }

// splitNoteID splits a notification's ID into the account's Dir and the
// chat. ok is false for an ID without an account (no accounts, or a
// notification from before they had one), which is the open account's.
func splitNoteID(id string) (dir, chat string, ok bool) {
	dir, chat, ok = strings.Cut(id, "|")
	if !ok {
		return "", id, false
	}
	return dir, chat, true
}

// prefs is the backend that holds the app's preferences.
func (n *notifier) prefs() model.Backend {
	if n.app != nil {
		return n.app()
	}
	return n.b
}

// read takes the notification of a chat that was read away.
func (n *notifier) read(id string) {
	c, ok := n.chats[id]
	if n.shown[id] == 0 && (!ok || c.unread == 0) {
		return
	}
	delete(n.shown, id)
	if ok {
		c.unread = 0
		n.chats[id] = c
	}
	if n.enabled {
		n.remove(n.noteID(id))
	}
	n.updateTooltip()
}

// flush shows the notifications of the pending messages.
func (n *notifier) flush() {
	msgs := n.pending
	n.pending, n.flushAt = nil, time.Time{}
	if !n.enabled || n.focused() {
		return
	}
	var order []string
	byChat := map[string][]*model.Message{}
	for _, m := range msgs {
		if byChat[m.ChatID] == nil {
			order = append(order, m.ChatID)
		}
		byChat[m.ChatID] = append(byChat[m.ChatID], m)
	}
	for _, id := range order {
		ms := byChat[id]
		c, known := n.chats[id]
		if !n.wanted(id, c, known, ms) {
			continue
		}
		n.shown[id] += len(ms)
		n.show(n.notification(id, c, ms[len(ms)-1], n.shown[id]))
	}
}

// wanted reports whether new messages ms in chat id notify.
func (n *notifier) wanted(id string, c chatInfo, known bool, ms []*model.Message) bool {
	if isChannelID(id) || id == statusChatID {
		return false
	}
	group := c.group || strings.HasSuffix(id, "@g.us")
	if group && !prefOn(n.b, prefNotifyGroups) || !group && !prefOn(n.b, prefNotifyMessages) {
		return false
	}
	silenced := c.archived || c.muted && (c.muteUntil.IsZero() || n.now().Before(c.muteUntil))
	if !known || !silenced {
		return true
	}
	for _, m := range ms {
		if n.forMe(m) {
			return true
		}
	}
	return false
}

// forMe reports whether m mentions you (or everyone) or replies to you.
func (n *notifier) forMe(m *model.Message) bool { return m.ForMe(n.meID) }

// notification describes the notification of chat id, whose newest
// message is m, for count messages.
func (n *notifier) notification(id string, c chatInfo, m *model.Message, count int) notify.Notification {
	title := c.name
	if title == "" {
		title = id
		if i := strings.IndexByte(id, '@'); i > 0 {
			title = id[:i]
			if strings.HasSuffix(id, "@s.whatsapp.net") {
				title = "+" + title
			}
		}
	}
	body := notificationText(m)
	if (c.group || strings.HasSuffix(id, "@g.us")) && m.Sender != "" {
		body = plainText(m.Sender) + ": " + body
	}
	if n.account != "" {
		title += " · " + n.account
	}
	footer := ""
	if count > 1 {
		footer = fmt.Sprintf("%d new messages", count)
	}
	if !prefOn(n.b, prefNotifyPreviews) {
		body = "New message"
		if count > 1 {
			body, footer = footer, ""
		}
	}
	img := n.b.Avatar(id)
	if img == nil {
		img = defaultPicture(c.group)
	}
	if privacyNotice(n.prefs()) {
		// Privacy mode: not who, not what.
		title, body, footer, img = appName, "New message", "", defaultPicture(false)
		if count > 1 {
			body = fmt.Sprintf("%d new messages", count)
		}
	}
	return notify.Notification{
		ID:     n.noteID(id),
		Title:  plainText(title),
		Body:   body,
		Footer: footer,
		Image:  img,
		Time:   m.Time,
		Silent: !prefOn(n.b, prefNotifySound),
		Reply:  true,
	}
}

// notificationText is how a notification quotes a message: its text
// without formatting, or what it holds ("📷 Photo").
func notificationText(m *model.Message) string {
	var s string
	switch {
	case m.Kind == model.KindViewOnce:
		s = viewOnceLabel(m)
	case m.Media == model.MediaNone:
		s = m.Text
		if s == "" {
			s = "Message"
		}
	case m.Media == model.MediaVoice:
		s = "🎤 Voice message"
		if m.Duration > 0 {
			s += fmt.Sprintf(" (%d:%02d)", m.Duration/60, m.Duration%60)
		}
	default:
		s = mediaLabel(m)
		if e := mediaEmoji[m.Media]; e != "" {
			s = e + " " + s
		}
	}
	s = displayText(plainText(s))
	if r := []rune(s); len(r) > 400 {
		s = string(r[:400]) + "…"
	}
	return s
}

var mediaEmoji = map[model.Media]string{
	model.MediaImage:       "📷",
	model.MediaVideo:       "🎥",
	model.MediaGIF:         "🎥",
	model.MediaAudio:       "🎵",
	model.MediaDocument:    "📄",
	model.MediaSticker:     "💟",
	model.MediaLocation:    "📍",
	model.MediaContact:     "👤",
	model.MediaPoll:        "📊",
	model.MediaEventInvite: "📅",
}

func (n *notifier) updateTooltip() {
	count := 0
	for id, c := range n.chats {
		if c.unread != 0 && !c.archived && !isChannelID(id) {
			count++
		}
	}
	if count == n.unread {
		return
	}
	n.unread = count
	n.tooltip(tooltipText(count))
}

// tooltipText is the tray icon's tooltip with count unread chats.
func tooltipText(count int) string {
	switch count {
	case 0:
		return appName
	case 1:
		return appName + "\n1 unread chat"
	}
	return fmt.Sprintf("%s\n%d unread chats", appName, count)
}

// defaultPicture is the profile picture of chats without one, like the
// window's placeholders in the light theme.
func defaultPicture(group bool) []byte {
	defaultPics.once.Do(func() {
		bg := lightPalette.UserAvatar
		defaultPics.person = encodePNG(icon.Badge(96, bg, icPerson, 0.62, lightPalette.UserAvatarIcon))
		defaultPics.group = encodePNG(icon.Badge(96, lightPalette.GroupAvatar, icGroup, 0.5, lightPalette.GroupAvatarIcon))
	})
	if group {
		return defaultPics.group
	}
	return defaultPics.person
}

var defaultPics struct {
	once          sync.Once
	person, group []byte
}
