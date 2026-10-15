package wa

import (
	"context"
	"errors"
	"time"

	"github.com/polymorfa/hypermeow"
	"github.com/polymorfa/hypermeow/types"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

// ResolveLink implements model.Backend: the WhatsApp links only the
// server can resolve, a channel's invite, a business's message link and
// a contact's QR link (ui/applink.go).
func (b *Backend) ResolveLink(kind model.LinkKind, code string) {
	cli := b.client()
	if cli == nil || !cli.IsConnected() {
		b.emit(model.LinkEvent{Kind: kind, Code: code, Err: "You're offline. Try again once connected."})
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(b.ctx, 20*time.Second)
		defer cancel()
		ev := model.LinkEvent{Kind: kind, Code: code}
		var err error
		switch kind {
		case model.LinkChannel:
			var n *types.NewsletterMetadata
			if n, err = cli.GetNewsletterInfoWithInvite(ctx, code); err == nil {
				m := metaFromNewsletter(n)
				// Kept where Avatar finds its picture; a followed or
				// suggested channel keeps what it has.
				_ = b.store.addChannel(ctx, m)
				ev.ID, ev.Name = m.jid, m.name
				ev.Description = n.ThreadMeta.Description.Text
				ev.Channel = &model.Channel{ID: m.jid, Name: m.name, Verified: m.verified,
					Followers: m.followers, Following: b.store.followsChannel(ctx, m.jid)}
			}
		case model.LinkBusiness:
			var t *types.BusinessMessageLinkTarget
			if t, err = cli.ResolveBusinessMessageLink(ctx, code); err == nil {
				id := b.canonical(ctx, t.JID)
				ev.ID, ev.Text = id.String(), t.Message
				ev.Name = first(t.VerifiedName, t.PushName, b.chatName(ctx, id))
			}
		case model.LinkContact:
			var t *types.ContactQRLinkTarget
			if t, err = cli.ResolveContactQRLink(ctx, code); err == nil {
				id := b.canonical(ctx, t.JID)
				ev.ID = id.String()
				ev.Name = first(b.chatName(ctx, id), t.PushName)
			}
		}
		if err != nil {
			b.log.Debugf("resolve link %d %s: %v", kind, code, err)
			ev.Err = linkError(err)
		}
		b.emit(ev)
	}()
}

func linkError(err error) string {
	switch {
	case errors.Is(err, whatsmeow.ErrBusinessMessageLinkNotFound),
		errors.Is(err, whatsmeow.ErrContactQRLinkNotFound),
		errors.Is(err, whatsmeow.ErrIQNotFound):
		return "This link doesn't exist or was reset."
	}
	return "Couldn't open the link. Check your connection and try again."
}

// addChannel stores a channel opened from a link, unless it's stored.
func (s *msgStore) addChannel(ctx context.Context, m channelMeta) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO wz_channels (jid, name, verified, followers, following, owner, muted, created, picture, rank)
		VALUES (?, ?, ?, ?, 0, 0, 0, ?, ?, 0) ON CONFLICT (jid) DO NOTHING`,
		m.jid, m.name, boolInt(m.verified), m.followers, m.created.Unix(), m.picture)
	return err
}

// followsChannel reports whether you follow channel jid.
func (s *msgStore) followsChannel(ctx context.Context, jid string) bool {
	var f int
	_ = s.db.QueryRowContext(ctx, `SELECT following FROM wz_channels WHERE jid = ?`, jid).Scan(&f)
	return f == 1
}
