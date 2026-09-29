package wa

import (
	"context"
	"sync"

	"github.com/polymorfa/hypermeow/types"
)

// nameCache memoizes display names; history sync resolves the same senders
// thousands of times.
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

func skipChat(j types.JID) bool {
	return j.IsEmpty() || j.Server == types.BroadcastServer || j.Server == types.NewsletterServer
}

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
			n.phone = "+" + pn.User
			add(pn)
		}
	case types.DefaultUserServer:
		n.phone = "+" + j.User
		if lid, err := cli.Store.LIDs.GetLIDForPN(ctx, j); err == nil && !lid.IsEmpty() {
			add(lid)
		}
	}
	return n
}

// chatName is the title of a one-to-one chat: saved contact name, business
// name, phone number, then push name.
func (b *Backend) chatName(ctx context.Context, j types.JID) string {
	j = j.ToNonAD()
	if b.isMe(j) {
		return "You"
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

// senderName labels a message author in a group. WhatsApp shows unsaved
// people by their push name, prefixed with "~".
func (b *Backend) senderName(ctx context.Context, j types.JID, push string) string {
	j = j.ToNonAD()
	if b.isMe(j) {
		return "You"
	}
	k := "sender:" + j.String()
	if v, ok := b.names.get(k); ok {
		return v
	}
	n := b.lookup(ctx, j)
	if n.push == "" {
		n.push = push
	}
	name := first(n.saved, n.business, tilde(n.push), n.phone, n.redacted, j.User)
	b.names.put(k, name)
	return name
}

func tilde(s string) string {
	if s == "" {
		return ""
	}
	return "~ " + s
}

func first(s ...string) string {
	for _, v := range s {
		if v != "" {
			return v
		}
	}
	return ""
}
