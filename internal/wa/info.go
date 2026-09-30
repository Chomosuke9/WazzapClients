package wa

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"time"

	"github.com/polymorfa/hypermeow/types"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

// Info implements model.Backend. Details are cached in wz_meta so the panel
// opens instantly (and works offline); they're refreshed once per session.
func (b *Backend) Info(chatID string) *model.ChatInfo {
	ctx := b.ctx
	jid, err := types.ParseJID(chatID)
	if err != nil {
		return nil
	}
	var info *model.ChatInfo
	if raw := b.store.meta(ctx, "info:"+chatID); raw != "" {
		info = new(model.ChatInfo)
		if json.Unmarshal([]byte(raw), info) != nil {
			info = nil
		}
	}
	b.infoMu.Lock()
	fetched := b.infoFetched[chatID]
	b.infoFetched[chatID] = true
	b.infoMu.Unlock()
	if !fetched {
		go b.fetchInfo(jid)
	}
	if info == nil {
		info = &model.ChatInfo{ID: chatID, IsGroup: jid.Server == types.GroupServer}
		if !info.IsGroup {
			info.Phone = b.lookup(ctx, jid).phone
		}
	}
	info.MediaCount, info.Media = b.store.mediaSummary(ctx, chatID, 4)
	return info
}

// mediaSummary counts a chat's media, links and documents, and returns the
// newest pictures with previews.
func (s *msgStore) mediaSummary(ctx context.Context, chat string, n int) (int, []*model.Message) {
	var count int
	_ = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM wz_messages WHERE chat = ? AND kind <> ? AND
		(media IN (?, ?, ?, ?) OR text LIKE '%http://%' OR text LIKE '%https://%' OR text LIKE '%www.%')`,
		chat, int(model.KindDeleted), int(model.MediaImage), int(model.MediaVideo), int(model.MediaGIF),
		int(model.MediaDocument)).Scan(&count)
	rows, err := s.db.QueryContext(ctx, `SELECT `+msgColumns+` FROM wz_messages
		WHERE chat = ? AND media IN (?, ?, ?) AND thumb IS NOT NULL ORDER BY ts DESC LIMIT ?`,
		chat, int(model.MediaImage), int(model.MediaVideo), int(model.MediaGIF), n)
	if err != nil {
		return count, nil
	}
	defer rows.Close()
	var out []*model.Message
	for rows.Next() {
		r, err := scanMessage(rows)
		if err != nil {
			break
		}
		out = append(out, r.Message)
	}
	return count, out
}

func (b *Backend) fetchInfo(jid types.JID) {
	ctx, cancel := context.WithTimeout(b.ctx, 30*time.Second)
	defer cancel()
	cli := b.client()
	if cli == nil || !cli.IsConnected() {
		b.infoMu.Lock()
		delete(b.infoFetched, jid.String())
		b.infoMu.Unlock()
		return
	}
	info := &model.ChatInfo{ID: jid.String()}
	if jid.Server == types.GroupServer {
		g, err := cli.GetGroupInfo(ctx, jid)
		if err != nil {
			b.log.Debugf("group info %s: %v", jid, err)
			return
		}
		b.fillGroupInfo(ctx, info, g)
		b.subMu.Lock()
		b.subtitles[jid.String()] = b.groupSubtitle(ctx, g)
		b.subMu.Unlock()
	} else {
		info.Name = b.chatName(ctx, jid)
		info.Phone = b.lookup(ctx, jid).phone
		users, err := cli.GetUserInfo(ctx, []types.JID{jid})
		if err != nil {
			b.log.Debugf("user info %s: %v", jid, err)
		}
		for _, u := range users {
			info.About = u.Status
		}
	}
	raw, _ := json.Marshal(info)
	_ = b.store.setMetaValue(b.ctx, "info:"+jid.String(), string(raw))
	b.emit(model.InfoEvent{ChatID: jid.String()})
}

func (b *Backend) fillGroupInfo(ctx context.Context, info *model.ChatInfo, g *types.GroupInfo) {
	info.IsGroup = true
	info.Name = g.Name
	info.About = g.Topic
	info.Created = g.GroupCreated
	info.Disappearing = g.DisappearingTimer
	switch owner := g.OwnerJID; {
	case owner.IsEmpty():
	case b.isMe(owner) || (!g.OwnerPN.IsEmpty() && b.isMe(g.OwnerPN)):
		info.CreatedBy = "you"
	default:
		info.CreatedBy = b.memberName(ctx, owner, g.OwnerPN)
	}
	type ranked struct {
		m     model.Member
		saved bool
	}
	var ms []ranked
	for _, p := range g.Participants {
		me := b.isMe(p.JID) || (!p.PhoneNumber.IsEmpty() && b.isMe(p.PhoneNumber))
		m := model.Member{ID: b.canonical(ctx, p.JID).String(), Admin: p.IsAdmin || p.IsSuperAdmin, Me: me}
		saved := false
		if me {
			m.Name = "You"
		} else {
			m.Name = b.memberName(ctx, p.JID, p.PhoneNumber)
			saved = !strings.HasPrefix(m.Name, "+") && !strings.HasPrefix(m.Name, "~")
		}
		ms = append(ms, ranked{m, saved})
	}
	// You first, then admins, then saved contacts, then everyone else.
	sort.SliceStable(ms, func(i, j int) bool {
		a, c := ms[i], ms[j]
		switch {
		case a.m.Me != c.m.Me:
			return a.m.Me
		case a.m.Admin != c.m.Admin:
			return a.m.Admin
		case a.saved != c.saved:
			return a.saved
		}
		return strings.ToLower(a.m.Name) < strings.ToLower(c.m.Name)
	})
	info.Members = make([]model.Member, len(ms))
	for i, r := range ms {
		info.Members[i] = r.m
	}
}

// memberName labels a group participant: saved name, business name, phone
// number, then "~push name".
func (b *Backend) memberName(ctx context.Context, j, pn types.JID) string {
	n := b.lookup(ctx, j)
	if n.saved == "" && !pn.IsEmpty() {
		if m := b.lookup(ctx, pn); m.saved != "" || m.business != "" {
			n = m
		}
		if n.phone == "" {
			n.phone = formatPhone(pn.User)
		}
	}
	return first(n.saved, n.business, n.phone, tilde(n.push), n.redacted, j.User)
}
