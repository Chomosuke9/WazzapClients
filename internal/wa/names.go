package wa

import (
	"context"
	"regexp"
	"slices"
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

// isMeID reports whether a chat ID is yours: the "message yourself" chat.
func (b *Backend) isMeID(id string) bool {
	j, err := types.ParseJID(id)
	return err == nil && b.isMe(j)
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

// storedMentions is a message's stored mention list, read.
type storedMentions struct {
	all    bool     // "@all"
	people []string // everyone it mentions, once each
	groups []storedGroup
}

// storedGroup is a group mention (@admin, a custom mention, a snippet's
// {mention}): the text says "@<jid>", shown as "@<subject>".
type storedGroup struct {
	jid, subject string
	people       []string // whom it notifies
	// unsure means the message doesn't say whose its people are (see
	// model.SplitMentions): each group mention then has everyone the
	// text doesn't name.
	unsure bool
}

// readMentions reads a stored mention list (mentionsOf's) of a message
// whose stored text is text.
func readMentions(mentions, text string) storedMentions {
	var sm storedMentions
	var jids []string
	for _, s := range strings.Split(mentions, ",") {
		switch {
		case s == "":
		case s == mentionAll:
			sm.all = true
		case strings.HasPrefix(s, groupMentionPrefix):
			jid, subject, _ := strings.Cut(s[len(groupMentionPrefix):], ":")
			sm.groups = append(sm.groups, storedGroup{jid: jid, subject: subject})
		default:
			jids = append(jids, s)
		}
	}
	add := func(l []string) {
		for _, j := range l {
			if !slices.Contains(sm.people, j) {
				sm.people = append(sm.people, j)
			}
		}
	}
	direct, groups, ok := model.SplitMentions(jids, len(sm.groups))
	add(direct)
	if ok {
		for i := range sm.groups {
			sm.groups[i].people = groups[i]
			add(groups[i])
		}
		return sm
	}
	// Whoever the text doesn't name as "@<user>" is the group mentions'.
	var rest []string
	for _, j := range sm.people {
		if user, _, _ := strings.Cut(j, "@"); user == "" || !strings.Contains(text, "@"+user) {
			rest = append(rest, j)
		}
	}
	for i := range sm.groups {
		sm.groups[i].people, sm.groups[i].unsure = rest, len(sm.groups) > 1
	}
	return sm
}

// meAmong reports whether one of jids is you.
func (b *Backend) meAmong(jids []string) bool {
	for _, s := range jids {
		if j, err := types.ParseJID(s); err == nil && b.isMe(j.ToNonAD()) {
			return true
		}
	}
	return false
}

// replaceMentions turns "@123456" into "@Name" for every mentioned JID.
func (b *Backend) replaceMentions(ctx context.Context, chatID, text, mentions string) string {
	sm := readMentions(mentions, text)
	if sm.all {
		text = strings.ReplaceAll(text, "@all", markedMention(model.MentionNotifies, "all"))
	}
	// Group mentions first: a person's "@<user>" can start a group's JID.
	for _, g := range sm.groups {
		m := mention(g.subject)
		switch {
		case !model.IsGroupNameJID(g.jid, chatID):
		case strings.EqualFold(g.subject, "admin"):
			// A group mentioning itself is "@admin" (see Send).
			m = markedMention(model.MentionAdmins, g.subject)
		case len(g.people) == 0:
			// A group mentioning itself with no one in it is a snippet's
			// {mention} (see Draft.MentionChat).
		case b.meAmong(g.people):
			// A custom mention of you: the name of the people mentioned.
			// The group mark after the first tells the UI it has people.
			m = "\u2068" + string(model.MentionNotifies) + string(model.MentionGroup) + "@" + g.subject + "\u2069"
		default:
			m = markedMention(model.MentionGroup, g.subject)
		}
		text = strings.ReplaceAll(text, "@"+g.jid, m)
	}
	for _, s := range sm.people {
		j, err := types.ParseJID(s)
		if err != nil || j.User == "" || !strings.Contains(text, "@"+j.User) {
			continue // @admin and custom mentions don't name their people
		}
		m := mention(b.senderName(ctx, j, ""))
		if b.isMe(j.ToNonAD()) {
			m = markedMention(model.MentionNotifies, b.myName())
		}
		text = strings.ReplaceAll(text, "@"+j.User, m)
	}
	return text
}

// groupTokenRe finds a group mentioned as a message's text says it, "@<group
// JID>" (@admin, custom mentions, a snippet's {mention}).
var groupTokenRe = regexp.MustCompile(`@[\d-]+@g\.us`)

// quoteMentions shows the mentions in the text of a quote in chat, by the
// quoted message's own mentions: a reply's are about the reply, and may name
// the group differently. Clients often quote the text alone, so a group mention
// left over takes its name from the original, or at worst the group's.
func (b *Backend) quoteMentions(ctx context.Context, chat string, q *rawQuote, text string) string {
	if q.mentions != "" {
		text = b.replaceMentions(ctx, chat, text, q.mentions)
	}
	if !groupTokenRe.MatchString(text) {
		return text
	}
	if orig, ok := b.store.message(ctx, first(q.remote, chat), q.id); ok {
		text = b.replaceMentions(ctx, chat, text, orig.mentions)
	}
	return groupTokenRe.ReplaceAllStringFunc(text, func(s string) string {
		if c, ok := b.store.chat(ctx, s[1:]); ok && c.Name != "" {
			return mention(c.Name)
		}
		return mention("group")
	})
}

// namedGroupMentions writes the group mentions of a stored message's text as
// the names they show, for a quote, which every client can then read as it is.
func namedGroupMentions(text, mentions string) string {
	for _, s := range strings.Split(mentions, ",") {
		if rest, ok := strings.CutPrefix(s, groupMentionPrefix); ok {
			jid, subject, _ := strings.Cut(rest, ":")
			text = strings.ReplaceAll(text, "@"+jid, "@"+subject)
		}
	}
	return text
}

// resolve fills in a loaded message's quote and display names.
func (b *Backend) resolve(ctx context.Context, r rawMsg, isGroup bool) *model.Message {
	m := r.Message
	if m.Kind == model.KindSystem {
		m.Text, m.Notice = b.systemText(ctx, r, r.stub, r.stubParams)
		return m
	}
	if m.FromMe && m.Receipt >= model.Sent && !isGroup && b.isMeID(m.ChatID) {
		m.Receipt = model.Read // you've read what you sent yourself
	}
	b.nameStickers(ctx, m)
	if m.StatusMention != nil {
		b.store.fillStatusMention(ctx, m.StatusMention)
	}
	if isGroup && !m.FromMe && r.senderJID != "" {
		m.Sender = b.senderNameStr(ctx, r.senderJID, r.senderPush)
		if l, ok := b.memberLabelOf(ctx, m.ChatID, r.senderJID); ok {
			m.SenderLabel = l
		}
	}
	if r.quote != nil {
		m.Quote = b.resolveQuote(ctx, m.ChatID, r.quote)
		if m.Quote != nil {
			m.Quote.Text = b.quoteMentions(ctx, m.ChatID, r.quote, m.Quote.Text)
		}
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
	m.DeletedBy, m.DeletedByMe = b.deleter(ctx, r.revokedBy)
	b.fillVotes(ctx, m)
	return m
}

// deleter names the admin who deleted a message, or says it was you; by is
// "" when its sender deleted it.
func (b *Backend) deleter(ctx context.Context, by string) (name string, me bool) {
	switch {
	case by == "":
		return "", false
	case b.isMeID(by):
		return "", true
	}
	return b.senderNameStr(ctx, by, ""), false
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

// markedMention is a mention whose kind mark (model.MentionNotifies,
// model.MentionAdmins or model.MentionGroup) tells the UI whom it notifies.
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
// then push names, then everyone else, then "You".
func (b *Backend) groupSubtitle(ctx context.Context, participants []types.GroupParticipant) string {
	var saved, pushes, others []string
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
		case n.push != "":
			pushes = append(pushes, tilde(n.push))
		case n.phone != "":
			others = append(others, n.phone)
		case !p.PhoneNumber.IsEmpty():
			others = append(others, formatPhone(p.PhoneNumber.User))
		default:
			others = append(others, first(n.redacted, p.DisplayName, p.JID.User))
		}
	}
	sort.Strings(saved)
	sort.Strings(pushes)
	sort.Strings(others)
	names := append(append(saved, pushes...), others...)
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
