package wa

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/polymorfa/hypermeow/types"
	"github.com/polymorfa/hypermeow/types/events"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

// galleryWhere is the condition that picks a GalleryKind's messages.
func galleryWhere(k model.GalleryKind) (string, []any) {
	deleted := int(model.KindDeleted)
	switch k {
	case model.GalleryDocs:
		return `media = ? AND kind <> ?`, []any{int(model.MediaDocument), deleted}
	case model.GalleryLinks:
		return `kind NOT IN (?, ?) AND (text LIKE '%http://%' OR text LIKE '%https://%' OR text LIKE '%www.%')`,
			[]any{deleted, int(model.KindUnsupported)}
	case model.GalleryStarred:
		return `starred <> 0 AND kind <> ?`, []any{deleted}
	}
	return `media IN (?, ?, ?) AND kind <> ?`, []any{int(model.MediaImage), int(model.MediaVideo), int(model.MediaGIF), deleted}
}

// gallery returns a page of q's messages, and whether more follow.
func (s *msgStore) gallery(ctx context.Context, q model.GalleryQuery) ([]rawMsg, bool, error) {
	where, args := galleryWhere(q.Kind)
	if q.ChatID != "" {
		where += ` AND chat = ?`
		args = append(args, q.ChatID)
	} else {
		where += ` AND chat NOT LIKE '%@newsletter'` // channels aren't chats
	}
	order := `ts DESC, rowid DESC`
	if q.Oldest {
		order = `ts, rowid`
	}
	key := model.SearchKey(q.Text)
	if key == "" {
		out, err := s.queryMessages(ctx, `SELECT `+msgColumns+` FROM wz_messages WHERE `+where+
			` ORDER BY `+order+` LIMIT ? OFFSET ?`, append(args, q.Limit+1, q.Offset)...)
		more := len(out) > q.Limit
		if more {
			out = out[:q.Limit]
		}
		return out, more, err
	}
	// Match the text in Go, like searchMessages: SearchKey folds accents
	// and case, which LIKE can't.
	rows, err := s.db.QueryContext(ctx, `SELECT rowid, text, file FROM wz_messages WHERE `+where+` ORDER BY `+order, args...)
	if err != nil {
		return nil, false, err
	}
	var ids []string
	skip, more := q.Offset, false
	for rows.Next() {
		var id int64
		var text, file string
		if err := rows.Scan(&id, &text, &file); err != nil {
			rows.Close()
			return nil, false, err
		}
		if !strings.Contains(model.SearchKey(text+" "+parseFile(file).Name), key) {
			continue
		}
		if skip > 0 {
			skip--
			continue
		}
		if len(ids) == q.Limit {
			more = true
			break
		}
		ids = append(ids, strconv.FormatInt(id, 10))
	}
	err = rows.Err()
	rows.Close()
	if err != nil || len(ids) == 0 {
		return nil, false, err
	}
	out, err := s.queryMessages(ctx, `SELECT `+msgColumns+` FROM wz_messages WHERE rowid IN (`+strings.Join(ids, ",")+
		`) ORDER BY `+order)
	return out, more, err
}

// Gallery implements model.Backend.
func (b *Backend) Gallery(q model.GalleryQuery) {
	ctx, cancel := context.WithCancel(b.ctx)
	b.galleryMu.Lock()
	if b.galleryCancel != nil {
		b.galleryCancel()
	}
	b.galleryCancel = cancel
	b.galleryMu.Unlock()
	go func() {
		defer cancel()
		raw, more, err := b.store.gallery(ctx, q)
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			b.log.Errorf("gallery %+v: %v", q, err)
		}
		msgs := make([]*model.Message, len(raw))
		for i, r := range raw {
			isGroup := strings.HasSuffix(r.ChatID, "@"+types.GroupServer)
			msgs[i] = b.resolve(ctx, r, isGroup)
		}
		if ctx.Err() == nil {
			b.emit(model.GalleryEvent{Query: q, Msgs: msgs, More: more})
		}
	}()
}

// SetDisappearing implements model.Backend.
func (b *Backend) SetDisappearing(chatID string, d time.Duration) {
	cli := b.client()
	if cli == nil || !cli.IsConnected() {
		b.emit(model.NoticeEvent{Text: "You're offline. Try again once connected."})
		return
	}
	jid, err := types.ParseJID(chatID)
	if err != nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(b.ctx, 30*time.Second)
		defer cancel()
		if err := cli.SetDisappearingTimer(ctx, jid, d, time.Now()); err != nil {
			b.log.Warnf("disappearing timer %s: %v", chatID, err)
			msg := "Couldn't change disappearing messages."
			if jid.Server == types.GroupServer {
				msg = groupError(err)
			}
			b.emit(model.NoticeEvent{Text: msg})
			return
		}
		if jid.Server == types.GroupServer {
			b.fetchInfo(jid)
			return
		}
		// WhatsApp keeps a contact's timer with the chat; remember it with
		// the info the panel shows.
		info := &model.ChatInfo{ID: chatID}
		if raw := b.store.meta(ctx, "info:"+chatID); raw != "" {
			_ = json.Unmarshal([]byte(raw), info)
		}
		info.Disappearing = uint32(d / time.Second)
		raw, _ := json.Marshal(info)
		_ = b.store.setMetaValue(ctx, "info:"+chatID, string(raw))
		b.emit(model.InfoEvent{ChatID: chatID})
	}()
}

// SecurityCode implements model.Backend.
func (b *Backend) SecurityCode(chatID string) {
	ev := model.SecurityCodeEvent{ChatID: chatID}
	cli := b.client()
	jid, err := types.ParseJID(chatID)
	switch {
	case err != nil || jid.Server == types.GroupServer:
		ev.Err = "Groups don't have a security code."
	case cli == nil || !cli.IsConnected():
		ev.Err = "You're offline. Try again once connected."
	}
	if ev.Err != "" {
		b.emit(ev)
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(b.ctx, 30*time.Second)
		defer cancel()
		lid := jid
		if jid.Server != types.HiddenUserServer {
			if l, err := cli.Store.LIDs.GetLIDForPN(ctx, jid); err == nil && !l.IsEmpty() {
				lid = l
			}
		}
		codes, err := cli.GetIdentityVerificationCodes(ctx, lid)
		if err != nil {
			b.log.Warnf("security code %s: %v", chatID, err)
			ev.Err = "Couldn't get the security code. Try again later."
		} else {
			ev.Code = codes.NumericCode
		}
		b.emit(ev)
	}()
}

// recordMemberChanges keeps who joined, left or changed role in a group,
// for "See member changes".
func (b *Backend) recordMemberChanges(ctx context.Context, e *events.GroupInfo) {
	by := types.EmptyJID
	if e.Sender != nil {
		by = *e.Sender
	}
	pn := types.EmptyJID
	if e.SenderPN != nil {
		pn = *e.SenderPN
	}
	byName := ""
	if !by.IsEmpty() {
		byName = "You"
		if !b.isMe(by) {
			byName = b.memberName(ctx, by, pn)
		}
	}
	ts := e.Timestamp
	if ts.IsZero() {
		ts = time.Now()
	}
	add := func(js []types.JID, self, other model.MemberAction) {
		for _, j := range js {
			name := "You"
			if !b.isMe(j) {
				name = b.memberName(ctx, j, types.EmptyJID)
			}
			act, who := other, byName
			if by.IsEmpty() || by.User == j.User {
				act, who = self, ""
			}
			_, err := b.store.db.ExecContext(ctx, `INSERT INTO wz_member_changes (chat, ts, name, actor, action) VALUES (?, ?, ?, ?, ?)`,
				e.JID.String(), ts.Unix(), name, who, int(act))
			if err != nil {
				b.log.Warnf("member change %s: %v", e.JID, err)
			}
		}
	}
	add(e.Join, model.MemberJoined, model.MemberAdded)
	add(e.Leave, model.MemberLeft, model.MemberRemoved)
	add(e.Promote, model.MemberPromoted, model.MemberPromoted)
	add(e.Demote, model.MemberDemoted, model.MemberDemoted)
}

// MemberChanges implements model.Backend.
func (b *Backend) MemberChanges(chatID string) []model.MemberChange {
	rows, err := b.store.db.QueryContext(b.ctx, `SELECT ts, name, actor, action FROM wz_member_changes
		WHERE chat = ? ORDER BY ts DESC, rowid DESC LIMIT 500`, chatID)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []model.MemberChange
	for rows.Next() {
		var ts int64
		var c model.MemberChange
		if rows.Scan(&ts, &c.Name, &c.By, &c.Action) != nil {
			break
		}
		c.Time = time.Unix(ts, 0)
		out = append(out, c)
	}
	return out
}
