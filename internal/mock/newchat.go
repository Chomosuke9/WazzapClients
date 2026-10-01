package mock

import (
	"fmt"
	"sort"
	"strings"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

// extraContacts are saved contacts you have no chat with yet.
var extraContacts = []*model.Contact{
	{ID: "agus", Name: "Agus Wibowo", Phone: "+62 813-5550-1942"},
	{ID: "fitri", Name: "Fitri Handayani", Phone: "+62 857-5550-3310"},
	{ID: "yoga", Name: "Yoga Pratama", Phone: "+62 812-5550-7781"},
}

// Contacts lists the people of the one-to-one demo chats and a few more.
func (b *Backend) Contacts() []*model.Contact {
	var out []*model.Contact
	for _, c := range b.chats {
		if c.IsGroup || c.Self || strings.HasPrefix(c.Name, "+") || c.ID == "shop" {
			continue
		}
		out = append(out, &model.Contact{ID: c.ID, Name: c.Name, Phone: "+62 812-5550-" + itoa4(len(c.Name)*37)})
	}
	for _, c := range extraContacts {
		cp := *c
		out = append(out, &cp)
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name) })
	return out
}

// LookupPhone pretends numbers of 10 digits or more are on WhatsApp.
func (b *Backend) LookupPhone(phone string) {
	ev := model.PhoneEvent{Phone: phone}
	if len(phone) >= 10 {
		ev.ID = phone + "@s.whatsapp.net"
		ev.Name = "+" + phone
	}
	b.emit(ev)
}

// CreateGroup adds the group to the demo chats.
func (b *Backend) CreateGroup(g model.NewGroup) {
	id := fmt.Sprintf("group-%d", len(b.chats))
	b.chats = append(b.chats, &model.Chat{ID: id, Name: g.Name, IsGroup: true, Time: b.now()})
	info := &model.ChatInfo{ID: id, Name: g.Name, IsGroup: true, Created: b.now(), Disappearing: g.Disappearing,
		Members: []model.Member{{ID: "me", Name: "You", Admin: true, Me: true}}}
	names := map[string]string{}
	for _, c := range b.Contacts() {
		names[c.ID] = c.Name
	}
	for _, m := range g.Members {
		info.Members = append(info.Members, model.Member{ID: m, Name: names[m]})
	}
	if b.infos == nil {
		b.infos = map[string]*model.ChatInfo{}
	}
	b.infos[id] = info
	b.emitChat(id)
	b.emit(model.GroupCreatedEvent{ChatID: id})
}
