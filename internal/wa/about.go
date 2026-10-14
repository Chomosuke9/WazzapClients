package wa

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/polymorfa/hypermeow"
	waBinary "github.com/polymorfa/hypermeow/binary"
	"github.com/polymorfa/hypermeow/types"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

// aboutCache keeps contacts' about texts for the pickers that show them
// under each name (the forward dialog). It's kept in wz_meta, so it works
// offline, and fetched again once a session, the first time Contacts is
// asked while connected.
type aboutCache struct {
	mu      sync.Mutex
	texts   map[string]string // chat ID → about; nil until loaded
	fetched bool
}

const aboutsKey = "abouts"

// aboutsLocked returns the cached about texts. The map is the cache's own: read
// it under b.abouts.mu, or not at all.
func (b *Backend) aboutsLocked(ctx context.Context) map[string]string {
	c := &b.abouts
	if c.texts == nil {
		c.texts = map[string]string{}
		if raw := b.store.meta(ctx, aboutsKey); raw != "" {
			_ = json.Unmarshal([]byte(raw), &c.texts)
		}
	}
	return c.texts
}

// fillAbouts gives contacts their cached about texts, and fetches them all
// again in the background once a session.
func (b *Backend) fillAbouts(ctx context.Context, contacts []*model.Contact) {
	c := &b.abouts
	c.mu.Lock()
	texts := b.aboutsLocked(ctx)
	for _, ct := range contacts {
		ct.About = texts[ct.ID]
	}
	cli := b.client()
	fetch := !c.fetched && cli != nil && cli.IsConnected() && len(contacts) > 0
	if fetch {
		c.fetched = true
	}
	c.mu.Unlock()
	if !fetch {
		return
	}
	var jids []types.JID
	for _, ct := range contacts {
		if j, err := types.ParseJID(ct.ID); err == nil {
			jids = append(jids, j)
		}
	}
	b.goWork(func() { // it writes the database, which Close waits for
		ctx, cancel := context.WithTimeout(b.ctx, time.Minute)
		defer cancel()
		got := make(map[string]string, len(jids))
		for len(jids) > 0 {
			n := min(len(jids), 200)
			res, err := b.fetchStatuses(ctx, jids[:n])
			if err != nil {
				b.log.Debugf("contacts' abouts: %v", err)
				c.mu.Lock()
				c.fetched = false // try again next time
				c.mu.Unlock()
				return
			}
			for j, st := range res {
				got[j.String()] = st.text
			}
			jids = jids[n:]
		}
		b.setAbouts(ctx, got)
	})
}

// setAbouts updates cached about texts and saves them.
func (b *Backend) setAbouts(ctx context.Context, texts map[string]string) {
	c := &b.abouts
	c.mu.Lock()
	all := b.aboutsLocked(ctx)
	changed := false
	for id, t := range texts {
		if all[id] != t {
			changed = true
			if t == "" {
				delete(all, id)
			} else {
				all[id] = t
			}
		}
	}
	var raw []byte
	if changed {
		raw, _ = json.Marshal(all)
	}
	c.mu.Unlock()
	if changed {
		_ = b.store.setMetaValue(ctx, aboutsKey, string(raw))
	}
}

// userStatus is someone's about text and when they set it.
type userStatus struct {
	text string
	at   time.Time
}

// fetchStatuses asks for people's about texts. Unlike GetUserInfo's, the
// answer keeps when each was set (the status node's t). Each person comes
// back under the JID asked for.
func (b *Backend) fetchStatuses(ctx context.Context, jids []types.JID) (map[types.JID]userStatus, error) {
	cli := b.client()
	if cli == nil {
		return nil, whatsmeow.ErrClientIsNil
	}
	list, err := cli.DangerousInternals().Usync(ctx, jids, "full", "background", []waBinary.Node{{Tag: "status"}})
	if err != nil {
		return nil, err
	}
	byUser := make(map[string]types.JID, len(jids))
	for _, j := range jids {
		byUser[j.User] = j
	}
	out := make(map[types.JID]userStatus, len(jids))
	for _, child := range list.GetChildren() {
		jid, ok := child.Attrs["jid"].(types.JID)
		if child.Tag != "user" || !ok {
			continue
		}
		asked, ok := byUser[jid.User]
		if !ok {
			continue
		}
		st, ok := child.GetOptionalChildByTag("status")
		if !ok {
			continue
		}
		text, _ := st.Content.([]byte)
		out[asked] = userStatus{text: string(text), at: st.AttrGetter().OptionalUnixTime("t")}
	}
	return out, nil
}

// setLimitSharing records a chat's Advanced chat privacy with its info,
// unless what's kept is newer (history sync brings old changes too).
func (b *Backend) setLimitSharing(ctx context.Context, chatID string, on bool, ts int64) {
	if ts > 1e11 {
		ts /= 1000 // milliseconds
	}
	var at time.Time
	if ts > 0 {
		at = time.Unix(ts, 0)
	}
	info := &model.ChatInfo{ID: chatID}
	if raw := b.store.meta(ctx, "info:"+chatID); raw != "" {
		_ = json.Unmarshal([]byte(raw), info)
	}
	if info.LimitSharing == on || (!at.IsZero() && at.Before(info.LimitSharingSet)) {
		return
	}
	info.LimitSharing, info.LimitSharingSet = on, at
	raw, _ := json.Marshal(info)
	_ = b.store.setMetaValue(ctx, "info:"+chatID, string(raw))
	b.emit(model.InfoEvent{ChatID: chatID})
}
