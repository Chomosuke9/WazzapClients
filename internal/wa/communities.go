package wa

import (
	"context"
	"sort"

	waBinary "github.com/polymorfa/hypermeow/binary"
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

// setMembers replaces the stored participants of a group, used to find the
// groups you share with a contact.
func (s *msgStore) setMembers(ctx context.Context, g *types.GroupInfo) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	jid := g.JID.String()
	if _, err := tx.ExecContext(ctx, `DELETE FROM wz_members WHERE chat = ?`, jid); err != nil {
		return err
	}
	for _, p := range g.Participants {
		if err := addMember(ctx, tx, jid, p.JID, p.PhoneNumber); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func addMember(ctx context.Context, x execer, chat string, j, pn types.JID) error {
	j = j.ToNonAD()
	if j.Server == types.DefaultUserServer && pn.IsEmpty() {
		pn = j
	}
	p := ""
	if !pn.IsEmpty() {
		p = pn.ToNonAD().String()
	}
	_, err := x.ExecContext(ctx, `INSERT INTO wz_members (chat, jid, pn) VALUES (?, ?, ?)
		ON CONFLICT (chat, jid) DO UPDATE SET pn = CASE WHEN excluded.pn != '' THEN excluded.pn ELSE pn END`,
		chat, j.String(), p)
	return err
}

// updateMembers applies a group's joins and leaves.
func (s *msgStore) updateMembers(ctx context.Context, chat string, join, leave []types.JID) {
	for _, j := range join {
		_ = addMember(ctx, s.db, chat, j, types.EmptyJID)
	}
	for _, j := range leave {
		j = j.ToNonAD()
		_, _ = s.db.ExecContext(ctx, `DELETE FROM wz_members WHERE chat = ? AND (jid = ? OR pn = ?)`, chat, j.String(), j.String())
	}
}

// clearMembers forgets a group's participants, once you left it.
func (s *msgStore) clearMembers(ctx context.Context, chat string) {
	_, _ = s.db.ExecContext(ctx, `DELETE FROM wz_members WHERE chat = ?`, chat)
}

// keepMembers forgets the participants of every group but the joined ones,
// which drops groups you left while offline.
func (s *msgStore) keepMembers(ctx context.Context, joined []string) {
	if len(joined) == 0 {
		return // likely a failed fetch rather than no groups at all
	}
	args := make([]any, len(joined))
	for i, j := range joined {
		args[i] = j
	}
	_, _ = s.db.ExecContext(ctx, `DELETE FROM wz_members WHERE chat NOT IN (`+placeholders(len(joined))+`)`, args...)
}

// members returns a group's stored participants.
func (s *msgStore) members(ctx context.Context, chat string) []types.GroupParticipant {
	rows, err := s.db.QueryContext(ctx, `SELECT jid, pn FROM wz_members WHERE chat = ?`, chat)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []types.GroupParticipant
	for rows.Next() {
		var j, pn string
		if rows.Scan(&j, &pn) != nil {
			break
		}
		p := types.GroupParticipant{}
		p.JID, _ = types.ParseJID(j)
		if pn != "" {
			p.PhoneNumber, _ = types.ParseJID(pn)
		}
		out = append(out, p)
	}
	return out
}

// commonGroup is a group shared with a contact, before its members are
// listed.
type commonGroup struct {
	jid, name, community, communityID string
}

// commonGroups lists the groups that any of ids (a contact's LID and phone
// JID) is in, newest activity first. Communities themselves and their
// announcement groups are left out, like WhatsApp does.
func (s *msgStore) commonGroups(ctx context.Context, ids ...string) []commonGroup {
	if len(ids) == 0 {
		return nil
	}
	args := make([]any, 0, 2*len(ids))
	for _, id := range ids {
		args = append(args, id)
	}
	for _, id := range ids {
		args = append(args, id)
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT g.jid, g.name, COALESCE(p.name, ''), COALESCE(p.jid, '') FROM wz_chats g
		LEFT JOIN wz_chats p ON p.jid = g.parent AND g.parent != ''
		WHERE g.community = 0 AND g.announce_sub = 0 AND g.jid IN (
			SELECT chat FROM wz_members WHERE jid IN (`+placeholders(len(ids))+`) OR pn IN (`+placeholders(len(ids))+`))
		ORDER BY MAX(g.last_ts, COALESCE((SELECT MAX(ts) FROM wz_messages WHERE chat = g.jid), 0)) DESC`, args...)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []commonGroup
	for rows.Next() {
		var g commonGroup
		if rows.Scan(&g.jid, &g.name, &g.community, &g.communityID) != nil {
			break
		}
		out = append(out, g)
	}
	return out
}

// markGeneralChats marks the communities' General chats among your
// groups. hypermeow doesn't parse the <general_chat/> in a group's node,
// so this asks for the groups again, as GetJoinedGroups does, and only
// looks for that.
func (b *Backend) markGeneralChats() {
	cli := b.client()
	if cli == nil {
		return
	}
	resp, err := cli.DangerousInternals().SendGroupIQ(b.ctx, "get", types.GroupServerJID, waBinary.Node{
		Tag:     "participating",
		Content: []waBinary.Node{{Tag: "participants"}, {Tag: "description"}},
	})
	if err != nil {
		b.log.Warnf("get general chats: %v", err)
		return
	}
	groups, ok := resp.GetOptionalChildByTag("groups")
	if !ok {
		return
	}
	var general []any
	for _, g := range groups.GetChildren() {
		if _, ok := g.GetOptionalChildByTag("general_chat"); !ok || g.Tag != "group" {
			continue
		}
		if id, _ := g.Attrs["id"].(string); id != "" {
			general = append(general, types.NewJID(id, types.GroupServer).String())
		}
	}
	q := `UPDATE wz_chats SET general = 0 WHERE general != 0`
	if len(general) > 0 {
		q = `UPDATE wz_chats SET general = (jid IN (` + placeholders(len(general)) + `)) WHERE is_group = 1`
	}
	if _, err := b.db.ExecContext(b.ctx, q, general...); err != nil {
		b.log.Warnf("store general chats: %v", err)
	}
}
