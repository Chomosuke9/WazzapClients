package wa

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/polymorfa/hypermeow"
	"github.com/polymorfa/hypermeow/store"
	"github.com/polymorfa/hypermeow/types"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

// Contacts implements model.Backend. The device store keeps every user it
// has heard of; saved contacts are the ones with a name from the phone's
// address book. Contacts stored under both their phone number and their
// LID are listed once, by LID (see canonical).
func (b *Backend) Contacts() []*model.Contact {
	cli := b.client()
	if cli == nil {
		return nil
	}
	ctx := b.ctx
	all, err := cli.Store.Contacts.GetAllContacts(ctx)
	if err != nil {
		b.log.Warnf("list contacts: %v", err)
		return nil
	}
	var pns, lids []types.JID
	for j, ci := range all {
		if first(ci.FullName, ci.FirstName) == "" || b.isMe(j) {
			continue
		}
		switch j.Server {
		case types.DefaultUserServer:
			pns = append(pns, j)
		case types.HiddenUserServer:
			lids = append(lids, j)
		}
	}
	lidOf, _ := cli.Store.LIDs.GetManyLIDsForPNs(ctx, pns)
	var pnOf map[types.JID]types.JID
	if r, ok := cli.Store.LIDs.(store.LIDBatchReverseStore); ok {
		pnOf, _ = r.GetManyPNsForLIDs(ctx, lids)
	}
	byID := make(map[string]*model.Contact, len(pns)+len(lids))
	add := func(id types.JID, name string, pn types.JID) {
		k := id.String()
		if c := byID[k]; c != nil {
			if c.Phone == "" && !pn.IsEmpty() {
				c.Phone = formatPhone(pn.User)
			}
			return
		}
		c := &model.Contact{ID: k, Name: name}
		if !pn.IsEmpty() {
			c.Phone = formatPhone(pn.User)
		}
		byID[k] = c
	}
	for _, j := range pns {
		ci := all[j]
		id := j
		if lid, ok := lidOf[j]; ok && !lid.IsEmpty() {
			id = lid
		}
		add(id, first(ci.FullName, ci.FirstName), j)
	}
	for _, j := range lids {
		ci := all[j]
		add(j, first(ci.FullName, ci.FirstName), pnOf[j])
	}
	out := make([]*model.Contact, 0, len(byID))
	for _, c := range byID {
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := strings.ToLower(out[i].Name), strings.ToLower(out[j].Name)
		if a != b {
			return a < b
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// LookupPhone implements model.Backend.
func (b *Backend) LookupPhone(phone string) {
	cli := b.client()
	if cli == nil || !cli.IsConnected() {
		b.emit(model.PhoneEvent{Phone: phone, Err: "You're offline. Try again once connected."})
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(b.ctx, 20*time.Second)
		defer cancel()
		res, err := cli.IsOnWhatsApp(ctx, []string{"+" + phone})
		if err != nil {
			b.log.Warnf("look up %s: %v", phone, err)
			b.emit(model.PhoneEvent{Phone: phone, Err: "Couldn't check this phone number. Try again."})
			return
		}
		ev := model.PhoneEvent{Phone: phone}
		for _, r := range res {
			if !r.IsIn {
				continue
			}
			j := r.JID
			if !r.PhoneNumber.IsEmpty() {
				j = r.PhoneNumber
			}
			// IsOnWhatsApp stores the LID mapping, so canonical finds it.
			id := b.canonical(ctx, j)
			ev.ID = id.String()
			ev.Name = b.chatName(ctx, id)
			break
		}
		b.emit(ev)
	}()
}

// CreateGroup implements model.Backend.
func (b *Backend) CreateGroup(g model.NewGroup) {
	cli := b.client()
	if cli == nil || !cli.IsConnected() {
		b.emit(model.GroupCreatedEvent{Err: "You're offline. Try again once connected."})
		return
	}
	var members []types.JID
	for _, id := range g.Members {
		if j, err := types.ParseJID(id); err == nil && !b.isMe(j) {
			members = append(members, j.ToNonAD())
		}
	}
	go func() {
		ctx, cancel := context.WithTimeout(b.ctx, 60*time.Second)
		defer cancel()
		req := whatsmeow.ReqCreateGroup{Name: g.Name, Participants: members}
		if g.Disappearing > 0 {
			req.GroupEphemeral = types.GroupEphemeral{IsEphemeral: true, DisappearingTimer: g.Disappearing}
		}
		info, err := cli.CreateGroup(ctx, req)
		if err != nil {
			b.log.Warnf("create group: %v", err)
			b.emit(model.GroupCreatedEvent{Err: "Couldn't create the group."})
			return
		}
		jid := info.JID.String()
		_ = b.store.ensureChat(ctx, b.db, jid, true, info.Name)
		_ = b.store.setField(ctx, jid, "last_ts", time.Now().Unix())
		_ = b.store.setGroupShape(ctx, info)
		_ = b.store.setMembers(ctx, info)
		if g.Photo != nil {
			if _, err := cli.SetGroupPhoto(ctx, info.JID, g.Photo); err != nil {
				b.log.Warnf("set photo of %s: %v", jid, err)
				b.emit(model.NoticeEvent{Text: "The group was created, but its photo couldn't be set."})
			} else {
				// Show the picture now rather than after the next fetch.
				path := b.avatarPath(jid)
				_ = os.MkdirAll(filepath.Dir(path), 0o700)
				if os.WriteFile(path, g.Photo, 0o600) == nil {
					b.emit(model.AvatarEvent{ID: jid})
				}
			}
		}
		b.emitChat(jid)
		b.emit(model.GroupCreatedEvent{ChatID: jid})
	}()
}
