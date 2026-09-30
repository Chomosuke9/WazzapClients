package wa

import (
	"context"
	"sort"

	"github.com/polymorfa/hypermeow/types"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

// setGroupShape stores where a group sits in a community.
func (s *msgStore) setGroupShape(ctx context.Context, g *types.GroupInfo) error {
	jid := g.JID.String()
	parent := ""
	if !g.LinkedParentJID.IsEmpty() {
		parent = g.LinkedParentJID.String()
	}
	if err := s.ensureChat(ctx, s.db, jid, true, g.Name); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `UPDATE wz_chats SET parent = ?, community = ?, announce_sub = ? WHERE jid = ?`,
		parent, boolInt(g.IsParent), boolInt(g.IsDefaultSubGroup), jid)
	return err
}

func (s *msgStore) isCommunity(ctx context.Context, jid string) bool {
	var c int
	_ = s.db.QueryRowContext(ctx, `SELECT community FROM wz_chats WHERE jid = ?`, jid).Scan(&c)
	return c != 0
}

// Communities implements model.Backend. Communities are ordered by their
// groups' latest activity, like WhatsApp does.
func (b *Backend) Communities() []*model.Community {
	ctx := b.ctx
	rows, err := b.db.QueryContext(ctx, `
		SELECT p.jid, p.name, g.jid, g.announce_sub,
			MAX(g.last_ts, COALESCE((SELECT MAX(ts) FROM wz_messages WHERE chat = g.jid), 0)) AS act
		FROM wz_chats p JOIN wz_chats g ON g.parent = p.jid
		WHERE p.community = 1
		ORDER BY act DESC`)
	if err != nil {
		b.log.Errorf("load communities: %v", err)
		return nil
	}
	defer rows.Close()
	byID := map[string]*model.Community{}
	latest := map[string]int64{}
	var out []*model.Community
	for rows.Next() {
		var (
			pjid, pname, gjid string
			announce          int
			act               int64
		)
		if err := rows.Scan(&pjid, &pname, &gjid, &announce, &act); err != nil {
			b.log.Errorf("load communities: %v", err)
			return nil
		}
		c := byID[pjid]
		if c == nil {
			c = &model.Community{ID: pjid, Name: pname}
			byID[pjid] = c
			out = append(out, c)
		}
		latest[pjid] = max(latest[pjid], act)
		if announce != 0 {
			c.Announcements = gjid
		} else {
			c.Groups = append(c.Groups, gjid)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return latest[out[i].ID] > latest[out[j].ID] })
	return out
}
