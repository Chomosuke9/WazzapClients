package wa

import (
	"context"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/polymorfa/hypermeow/types"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

// nameCache memoizes display names; a chat list resolves the same senders
// many times. It is cleared whenever contacts or push names change.
type nameCache struct {
	mu sync.Mutex
	m  map[string]string
}

func (c *nameCache) get(k string) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	v, ok := c.m[k]
	return v, ok
}

func (c *nameCache) put(k, v string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.m == nil {
		c.m = make(map[string]string)
	}
	c.m[k] = v
}

func (c *nameCache) clear() {
	c.mu.Lock()
	c.m = nil
	c.mu.Unlock()
}

// skipChat reports JIDs that aren't conversations. Status updates are
// handled separately; channels are stored like chats but listed apart.
func skipChat(j types.JID) bool {
	return j.IsEmpty() || j.Server == types.BroadcastServer
}

func isChannel(j types.JID) bool { return j.Server == types.NewsletterServer }

// canonical maps a chat JID to the one used as the chat key. hypermeow treats
// the LID as the stable identity, so one-to-one chats are keyed by LID when a
// mapping is known; otherwise the same person could appear twice.
func (b *Backend) canonical(ctx context.Context, j types.JID) types.JID {
	j = j.ToNonAD()
	if j.Server != types.DefaultUserServer {
		return j
	}
	cli := b.client()
	if cli == nil {
		return j
	}
	if lid, err := cli.Store.LIDs.GetLIDForPN(ctx, j); err == nil && !lid.IsEmpty() {
		return lid
	}
	return j
}

func (b *Backend) isMe(j types.JID) bool {
	cli := b.client()
	if cli == nil || cli.Store.ID == nil {
		return false
	}
	return j.User == cli.Store.ID.User || j.User == cli.Store.LID.User
}

type contactNames struct {
	saved, business, push, phone, redacted string
}

// lookup gathers every name the device store knows for a user, following
// the LID↔phone-number alias in either direction.
func (b *Backend) lookup(ctx context.Context, j types.JID) contactNames {
	var n contactNames
	cli := b.client()
	if cli == nil {
		return n
	}
	add := func(j types.JID) {
		ci, err := cli.Store.Contacts.GetContact(ctx, j)
		if err != nil || !ci.Found {
			return
		}
		if n.saved == "" {
			n.saved = ci.FullName
			if n.saved == "" {
				n.saved = ci.FirstName
			}
		}
		if n.business == "" {
			n.business = ci.BusinessName
		}
		if n.push == "" {
			n.push = ci.PushName
		}
		if n.redacted == "" {
			n.redacted = ci.RedactedPhone
		}
	}
	add(j)
	switch j.Server {
	case types.HiddenUserServer:
		if pn, err := cli.Store.LIDs.GetPNForLID(ctx, j); err == nil && !pn.IsEmpty() {
			n.phone = formatPhone(pn.User)
			add(pn)
		}
	case types.DefaultUserServer:
		n.phone = formatPhone(j.User)
		if lid, err := cli.Store.LIDs.GetLIDForPN(ctx, j); err == nil && !lid.IsEmpty() {
			add(lid)
		}
	}
	return n
}

// storedPush returns the newest push name stored with j's messages, under
// its LID or its phone JID.
func (b *Backend) storedPush(ctx context.Context, j types.JID) string {
	alt := ""
	if cli := b.client(); cli != nil {
		if a, err := cli.Store.GetAltJID(ctx, j); err == nil && !a.IsEmpty() {
			alt = a.ToNonAD().String()
		}
	}
	return b.store.lastPush(ctx, j.String(), alt)
}

// formatPhone renders a phone number the way WhatsApp does for Indonesian
// mobile numbers ("+62 812-3456-7890"); other countries get "+<digits>",
// since proper grouping needs a numbering-plan database.
func formatPhone(user string) string {
	if strings.HasPrefix(user, "62") && len(user) >= 11 && len(user) <= 14 {
		r := user[2:]
		return "+62 " + r[:3] + "-" + r[3:7] + "-" + r[7:]
	}
	return "+" + user
}

func allDigits(s string) bool {
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return s != ""
}

// chatName is the title of a one-to-one chat: saved contact name, business
// name, phone number, then push name.
func (b *Backend) chatName(ctx context.Context, j types.JID) string {
	j = j.ToNonAD()
	if b.isMe(j) {
		// Your own chat is titled with the name you saved yourself under,
		// or your profile name.
		push := ""
		if cli := b.client(); cli != nil {
			push = cli.Store.PushName
			if cli.Store.ID != nil {
				if n := b.lookup(ctx, cli.Store.ID.ToNonAD()); n.saved != "" {
					return n.saved
				}
			}
		}
		return first(push, "You")
	}
	k := "chat:" + j.String()
	if v, ok := b.names.get(k); ok {
		return v
	}
	n := b.lookup(ctx, j)
	name := first(n.saved, n.business, n.phone, tilde(n.push), n.redacted, j.User)
	b.names.put(k, name)
	return name
}

// senderName labels a message author in a group. WhatsApp shows people you
// haven't saved by their push name, prefixed with "~".
func (b *Backend) senderName(ctx context.Context, j types.JID, push string) string {
	j = j.ToNonAD()
	if b.isMe(j) {
		return "You"
	}
	k := "sender:" + j.String() + "|" + push
	if v, ok := b.names.get(k); ok {
		return v
	}
	n := b.lookup(ctx, j)
	if n.push == "" {
		n.push = push
	}
	if n.saved == "" && n.business == "" && n.push == "" {
		// The device store keeps only push names it saw arrive. Messages
		// stored with this person's push name know it too (mentions of
		// someone whose messages came from history, for one).
		n.push = b.storedPush(ctx, j)
	}
	name := first(n.saved, n.business, tilde(n.push), n.phone, n.redacted, j.User)
	b.names.put(k, name)
	return name
}

func (b *Backend) senderNameStr(ctx context.Context, jid, push string) string {
	j, err := types.ParseJID(jid)
	if err != nil || j.IsEmpty() {
		return tilde(push)
	}
	return b.senderName(ctx, j, push)
}

// Besides JIDs, a message's stored mention list can hold these tokens.
const (
	// mentionAll marks "@all" (nonJIDMentions), which notifies everyone.
	mentionAll = "all"
	// groupMentionPrefix starts "gm:<group JID>:<subject>": the text
	// mentions "@<group JID>", which is shown as "@<subject>".
	groupMentionPrefix = "gm:"
)

func groupMention(jid, subject string) string {
	return groupMentionPrefix + jid + ":" + strings.ReplaceAll(subject, ",", " ")
}

// replaceMentions turns "@123456" into "@Name" for every mentioned JID.
func (b *Backend) replaceMentions(ctx context.Context, chatID, text, mentions string) string {
	// A group mentioning itself with no one mentioned is a snippet's
	// {mention} (see Draft.MentionChat), not @admin.
	people := false
	for _, s := range strings.Split(mentions, ",") {
		if s != "" && s != mentionAll && !strings.HasPrefix(s, groupMentionPrefix) {
			people = true
		}
	}
	for _, s := range strings.Split(mentions, ",") {
		switch {
		case s == mentionAll:
			text = strings.ReplaceAll(text, "@all", markedMention(model.MentionNotifies, "all"))
			continue
		case strings.HasPrefix(s, groupMentionPrefix):
			jid, subject, _ := strings.Cut(s[len(groupMentionPrefix):], ":")
			m := mention(subject)
			if jid == chatID && (people || strings.EqualFold(subject, "admin")) {
				// A group mentioning itself is "@admin" (see Send).
				m = markedMention(model.MentionAdmins, subject)
			}
			text = strings.ReplaceAll(text, "@"+jid, m)
			continue
		}
		j, err := types.ParseJID(s)
		if err != nil || j.User == "" {
			continue
		}
		m := mention(b.senderName(ctx, j, ""))
		if b.isMe(j.ToNonAD()) {
			m = markedMention(model.MentionNotifies, b.myName())
		}
		text = strings.ReplaceAll(text, "@"+j.User, m)
	}
	return text
}

// resolve fills in a loaded message's quote and display names.
func (b *Backend) resolve(ctx context.Context, r rawMsg, isGroup bool) *model.Message {
	m := r.Message
	if isGroup && !m.FromMe && r.senderJID != "" {
		m.Sender = b.senderNameStr(ctx, r.senderJID, r.senderPush)
	}
	if r.quote != nil {
		m.Quote = b.resolveQuote(ctx, m.ChatID, r.quote)
	}
	if m.Quote != nil && m.Quote.SenderID != "" {
		m.Quote.Sender = b.senderNameStr(ctx, m.Quote.SenderID, "")
	}
	if r.mentions != "" {
		m.Text = b.replaceMentions(ctx, m.ChatID, m.Text, r.mentions)
		if m.Quote != nil {
			m.Quote.Text = b.replaceMentions(ctx, m.ChatID, m.Quote.Text, r.mentions)
		}
	} else if strings.Contains(m.Text, "@") {
		m.Text = b.guessMentions(ctx, m.Text)
	}
	// The mention list is the reply's own, not the quoted message's, so
	// guess whatever the quote mentions that the reply doesn't.
	if m.Quote != nil && strings.Contains(m.Quote.Text, "@") {
		m.Quote.Text = b.guessMentions(ctx, m.Quote.Text)
	}
	if r.whisper != "" {
		for _, s := range strings.Split(r.whisper, "\n") {
			jid, err := types.ParseJID(s)
			if err != nil {
				continue
			}
			m.Whisper = append(m.Whisper, b.memberName(ctx, jid, types.EmptyJID))
		}
	}
	b.fillVotes(ctx, m)
	return m
}

// myName is how a mention of you shows: your profile name, as in
// WhatsApp, or "You" before it is known.
func (b *Backend) myName() string {
	if cli := b.client(); cli != nil && cli.Store.PushName != "" {
		return cli.Store.PushName
	}
	return "You"
}

// mention formats a resolved @mention. The Unicode isolate marks around it
// are invisible; the UI uses them to highlight the whole name.
func mention(name string) string { return "\u2068@" + name + "\u2069" }

// markedMention is a mention whose kind mark (model.MentionNotifies or
// model.MentionAdmins) tells the UI whom it notifies.
func markedMention(kind rune, name string) string {
	return "\u2068" + string(kind) + "@" + name + "\u2069"
}

var mentionRe = regexp.MustCompile(`@(\d{6,})`)

// guessMentions resolves "@123…" in messages stored without their mention
// list: the number is tried as a LID, then as a phone number. Unknown
// numbers are left alone.
func (b *Backend) guessMentions(ctx context.Context, text string) string {
	return mentionRe.ReplaceAllStringFunc(text, func(s string) string {
		user := s[1:]
		for _, server := range []string{types.HiddenUserServer, types.DefaultUserServer} {
			j := types.NewJID(user, server)
			if b.isMe(j) {
				return markedMention(model.MentionNotifies, b.myName())
			}
			n := b.lookup(ctx, j)
			name := first(n.saved, n.business, tilde(n.push))
			if name == "" {
				name = tilde(b.storedPush(ctx, j))
			}
			if name != "" {
				return mention(name)
			}
		}
		return s
	})
}

func (b *Backend) resolveChat(ctx context.Context, rc rawChat) *model.Chat {
	c := rc.Chat
	if j, err := types.ParseJID(c.ID); err == nil && b.isMe(j) {
		c.Self = true
		c.Name = b.chatName(ctx, j)
	}
	if rc.last != nil {
		c.Last = b.resolve(ctx, *rc.last, c.IsGroup)
	}
	return c
}

// groupSubtitle lists participants like WhatsApp's header: saved contacts,
// then everyone else, then "You".
func (b *Backend) groupSubtitle(ctx context.Context, participants []types.GroupParticipant) string {
	var saved, others []string
	me := false
	for _, p := range participants {
		if b.isMe(p.JID) || (!p.PhoneNumber.IsEmpty() && b.isMe(p.PhoneNumber)) {
			me = true
			continue
		}
		n := b.lookup(ctx, p.JID)
		if n.saved == "" && !p.PhoneNumber.IsEmpty() {
			n = b.lookup(ctx, p.PhoneNumber)
		}
		switch {
		case n.saved != "":
			saved = append(saved, n.saved)
		case n.phone != "":
			others = append(others, n.phone)
		case !p.PhoneNumber.IsEmpty():
			others = append(others, formatPhone(p.PhoneNumber.User))
		default:
			others = append(others, first(tilde(n.push), n.redacted, p.DisplayName, p.JID.User))
		}
	}
	sort.Strings(saved)
	sort.Strings(others)
	names := append(saved, others...)
	if len(names) > 40 {
		names = names[:40]
	}
	if me {
		names = append(names, "You")
	}
	return strings.Join(names, ", ")
}

func tilde(s string) string {
	if s == "" {
		return ""
	}
	return "~" + s
}

func first(s ...string) string {
	for _, v := range s {
		if v != "" {
			return v
		}
	}
	return ""
}
