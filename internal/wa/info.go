package wa

import (
	"context"
	"encoding/json"
	"sort"
	"strconv"
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
	return info
}

// MediaSummary implements model.Backend. It reads the whole chat (100 ms
// and more in a big group), so it runs in the background, and only for
// the info panel.
func (b *Backend) MediaSummary(chatID string) {
	ctx, cancel := context.WithCancel(b.ctx)
	b.summaryMu.Lock()
	if b.summaryCancel != nil {
		b.summaryCancel()
	}
	b.summaryCancel = cancel
	b.summaryMu.Unlock()
	go func() {
		defer cancel()
		count, media := b.store.mediaSummary(ctx, chatID, 4)
		if ctx.Err() == nil {
			b.emit(model.MediaSummaryEvent{ChatID: chatID, Count: count, Media: media})
		}
	}()
}

// mediaSummary counts a chat's media, links and documents, and returns the
// newest pictures with previews.
func (s *msgStore) mediaSummary(ctx context.Context, chat string, n int) (int, []*model.Message) {
	var count int
	_ = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM wz_messages WHERE chat = ? AND kind NOT IN (?, ?) AND
		(media IN (?, ?, ?, ?) OR text LIKE '%http://%' OR text LIKE '%https://%' OR text LIKE '%www.%')`,
		chat, int(model.KindDeleted), int(model.KindViewOnce), int(model.MediaImage), int(model.MediaVideo), int(model.MediaGIF),
		int(model.MediaDocument)).Scan(&count)
	rows, err := s.db.QueryContext(ctx, `SELECT `+msgColumns+` FROM wz_messages
		WHERE chat = ? AND media IN (?, ?, ?) AND kind <> ? ORDER BY ts DESC`,
		chat, int(model.MediaImage), int(model.MediaVideo), int(model.MediaGIF), int(model.KindViewOnce))
	if err != nil {
		return count, nil
	}
	defer rows.Close()
	var out []*model.Message
	for len(out) < n && rows.Next() {
		r, err := scanMessage(rows)
		if err != nil {
			break
		}
		if len(r.Thumb) > 0 { // a preview to show
			out = append(out, r.Message)
		}
	}
	return count, out
}

func (b *Backend) fetchInfo(jid types.JID) {
	ctx, cancel := context.WithTimeout(b.ctx, 30*time.Second)
	defer cancel()
	cli := b.client()
	online := cli != nil && cli.IsConnected()
	if !online {
		// Try again once connected.
		b.infoMu.Lock()
		delete(b.infoFetched, jid.String())
		b.infoMu.Unlock()
	}
	info := &model.ChatInfo{ID: jid.String()}
	if jid.Server == types.GroupServer {
		if !online {
			return
		}
		g, err := cli.GetGroupInfo(ctx, jid)
		if err != nil {
			b.log.Debugf("group info %s: %v", jid, err)
			return
		}
		b.fillGroupInfo(ctx, info, g)
		_ = b.store.setMembers(ctx, g)
		b.subMu.Lock()
		b.subtitles[jid.String()] = b.groupSubtitle(ctx, g.Participants)
		b.subMu.Unlock()
	} else {
		// Start from what was fetched before, so a request that fails
		// (or being offline) keeps it.
		old := b.store.meta(ctx, "info:"+jid.String())
		if old != "" {
			_ = json.Unmarshal([]byte(old), info)
		}
		info.Name = b.chatName(ctx, jid)
		info.Phone = b.lookup(ctx, jid).phone
		info.Common = b.commonGroups(ctx, jid)
		if online {
			b.fillContactInfo(ctx, info, jid)
		} else if raw, _ := json.Marshal(info); string(raw) == old {
			// Nothing new; announcing it would only fetch it again.
			return
		}
	}
	raw, _ := json.Marshal(info)
	_ = b.store.setMetaValue(b.ctx, "info:"+jid.String(), string(raw))
	b.emit(model.InfoEvent{ChatID: jid.String()})
}

// fillContactInfo fetches a contact's about text, business profile and
// whether you blocked them.
func (b *Backend) fillContactInfo(ctx context.Context, info *model.ChatInfo, jid types.JID) {
	cli := b.client()
	pn := types.EmptyJID
	if jid.Server == types.DefaultUserServer {
		pn = jid
	} else if p, err := cli.Store.LIDs.GetPNForLID(ctx, jid); err == nil {
		pn = p
	}
	business := b.lookup(ctx, jid).business
	isBusiness := business != "" || info.Business != nil
	users, err := cli.GetUserInfo(ctx, []types.JID{jid})
	if err != nil {
		b.log.Debugf("user info %s: %v", jid, err)
	} else {
		isBusiness = business != ""
		for _, u := range users {
			info.About = u.Status
			if u.VerifiedName != nil {
				isBusiness = true
				if n := u.VerifiedName.Details.GetVerifiedName(); n != "" {
					business = n
				}
			}
		}
	}
	if !isBusiness {
		info.Business = nil
	} else {
		var bp *types.BusinessProfile
		for _, j := range []types.JID{pn, jid} {
			if j.IsEmpty() {
				continue
			}
			if bp, err = cli.GetBusinessProfile(ctx, j); err == nil {
				break
			}
			b.log.Debugf("business profile %s: %v", j, err)
		}
		switch {
		case bp != nil:
			info.Business = &model.Business{Name: business}
			fillBusiness(info.Business, bp)
		case info.Business == nil:
			info.Business = &model.Business{Name: business}
		case business != "":
			info.Business.Name = business
		}
	}
	if list, err := cli.GetBlocklist(ctx); err == nil {
		info.Blocked = false // only once the list is in
		for _, j := range list.JIDs {
			if j.User == jid.User || (!pn.IsEmpty() && j.User == pn.User) {
				info.Blocked = true
			}
		}
	} else {
		b.log.Debugf("blocklist: %v", err)
	}
}

var weekdays = map[string]time.Weekday{"sun": time.Sunday, "mon": time.Monday, "tue": time.Tuesday,
	"wed": time.Wednesday, "thu": time.Thursday, "fri": time.Friday, "sat": time.Saturday}

func fillBusiness(biz *model.Business, bp *types.BusinessProfile) {
	if len(bp.Categories) > 0 {
		biz.Category = bp.Categories[0].Name
	}
	biz.Description = bp.Description
	biz.Address = bp.Address
	biz.Email = bp.Email
	for _, w := range bp.Websites {
		if w = strings.TrimSpace(w); w != "" {
			biz.Websites = append(biz.Websites, w)
		}
	}
	biz.TimeZone = bp.BusinessHoursTimeZone
	for _, h := range bp.BusinessHours {
		day, ok := weekdays[strings.ToLower(h.DayOfWeek)]
		if !ok {
			continue
		}
		open, _ := strconv.Atoi(h.OpenTime)
		closing, _ := strconv.Atoi(h.CloseTime)
		biz.Hours = append(biz.Hours, model.BusinessHours{Day: day, Mode: h.Mode, Open: open, Close: closing})
	}
}

// commonGroups lists the groups you share with a contact, each with its
// members like its header shows them.
func (b *Backend) commonGroups(ctx context.Context, jid types.JID) []model.CommonGroup {
	ids := []string{jid.String()}
	if cli := b.client(); cli != nil {
		if alt, err := cli.Store.GetAltJID(ctx, jid); err == nil && !alt.IsEmpty() {
			ids = append(ids, alt.ToNonAD().String())
		}
	}
	var out []model.CommonGroup
	for _, g := range b.store.commonGroups(ctx, ids...) {
		b.subMu.Lock()
		sub, ok := b.subtitles[g.jid]
		b.subMu.Unlock()
		if !ok {
			sub = b.groupSubtitle(ctx, b.store.members(ctx, g.jid))
		}
		out = append(out, model.CommonGroup{ID: g.jid, Name: g.name, Community: g.community, CommunityID: g.communityID, Members: sub})
	}
	return out
}

func (b *Backend) fillGroupInfo(ctx context.Context, info *model.ChatInfo, g *types.GroupInfo) {
	info.IsGroup = true
	info.Name = g.Name
	info.About = g.Topic
	info.Created = g.GroupCreated
	info.Disappearing = g.DisappearingTimer
	info.Announce, info.Locked = g.IsAnnounce, g.IsLocked
	info.AdminsAdd, info.Approval = g.MemberAddMode == types.GroupMemberAddModeAdmin, g.IsJoinApprovalRequired
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
		if me {
			m.Name = "You"
		} else {
			n := b.memberNames(ctx, p.JID, p.PhoneNumber)
			m.Name = n.label(p.JID)
			m.Contact, m.Push, m.Phone = first(n.saved, n.business), n.push, first(n.phone, n.redacted)
		}
		ms = append(ms, ranked{m, m.Contact != ""})
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

// memberName labels a group participant (see contactNames.label).
func (b *Backend) memberName(ctx context.Context, j, pn types.JID) string {
	return b.memberNames(ctx, j, pn).label(j)
}

// memberNames gathers a group participant's names, under their LID or their
// phone number pn.
func (b *Backend) memberNames(ctx context.Context, j, pn types.JID) contactNames {
	n := b.lookup(ctx, j)
	if n.saved == "" && !pn.IsEmpty() {
		if m := b.lookup(ctx, pn); m.saved != "" || m.business != "" {
			n = m
		}
		if n.phone == "" {
			n.phone = formatPhone(pn.User)
		}
	}
	return n
}

// label is how a group participant is listed: saved name, business name,
// phone number, then "~push name".
func (n contactNames) label(j types.JID) string {
	return first(n.saved, n.business, n.phone, tilde(n.push), n.redacted, j.User)
}
