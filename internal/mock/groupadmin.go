package mock

import (
	"slices"
	"strings"
	"time"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

// ManageGroup changes the demo group's info as WhatsApp would. A phone
// number ending in an odd digit stands for someone whose privacy settings
// refuse being added.
func (b *Backend) ManageGroup(r model.GroupRequest) {
	ev := model.GroupEvent{Ref: r.Ref, ChatID: r.ChatID}
	info := b.Info(r.ChatID)
	if info == nil || !info.IsGroup {
		ev.Err = "This isn't a group."
		b.emit(ev)
		return
	}
	if b.infos == nil {
		b.infos = map[string]*model.ChatInfo{}
	}
	b.infos[r.ChatID] = info // keep the changes
	member := func(id string) int {
		return slices.IndexFunc(info.Members, func(m model.Member) bool { return m.ID == id })
	}
	switch r.Action {
	case model.GroupAdd:
		for _, id := range r.Members {
			res := model.MemberResult{ID: id, Name: id}
			c := b.chat(id)
			if c != nil {
				res.Name = c.Name
			}
			phone := c == nil && !strings.Contains(id, "@")
			digits := strings.Map(func(c rune) rune {
				if c >= '0' && c <= '9' {
					return c
				}
				return -1
			}, id)
			switch {
			case member(id) >= 0:
				res.Err = "is already in the group"
			case phone && len(digits) < 7:
				res.Err = "isn't a phone number with its country code"
			case phone && (digits[len(digits)-1]-'0')%2 == 1:
				res.Err = "can't be added because of their privacy settings"
				res.Invite = &model.GroupInvite{Code: "DemoInvite" + digits, Expires: b.now().Add(72 * time.Hour)}
			default:
				info.Members = append(info.Members, model.Member{ID: id, Name: res.Name})
			}
			ev.Members = append(ev.Members, res)
		}
	case model.GroupRemove, model.GroupPromote, model.GroupDemote:
		for _, id := range r.Members {
			res := model.MemberResult{ID: id, Name: id}
			i := member(id)
			switch {
			case i < 0:
				res.Err = "isn't a member of the group"
			case r.Action == model.GroupRemove:
				res.Name = info.Members[i].Name
				info.Members = slices.Delete(info.Members, i, i+1)
			default:
				res.Name = info.Members[i].Name
				info.Members[i].Admin = r.Action == model.GroupPromote
			}
			ev.Members = append(ev.Members, res)
		}
	case model.GroupAnnounce:
		info.Announce = r.On
	case model.GroupLock:
		info.Locked = r.On
	case model.GroupDescription:
		info.About = r.Text
	case model.GroupName:
		info.Name = r.Text
		if c := b.chat(r.ChatID); c != nil {
			c.Name = r.Text
			b.emitChat(c.ID)
		}
	case model.GroupAddMode:
		info.AdminsAdd = r.On
	case model.GroupApproval:
		info.Approval = r.On
	case model.GroupLink:
		if r.On {
			b.linkResets++
		}
		ev.Link = "https://chat.whatsapp.com/Demo" + strings.Repeat("x", b.linkResets) + "Link"
	case model.GroupSendInvite:
		if len(r.Members) > 0 && r.Invite != nil {
			chat := r.Members[0]
			if b.chat(chat) == nil {
				b.chats = append(b.chats, &model.Chat{ID: chat, Name: chat, Time: b.now()})
			}
			b.add(&model.Message{ChatID: chat, FromMe: true, Text: "Group invite: " + info.Name, Time: b.now(),
				Receipt: model.Sent})
		}
	}
	b.emit(ev)
	b.emit(model.InfoEvent{ChatID: r.ChatID})
}

// SendNewSticker keeps the sticker's picture, so the chat shows it.
func (b *Backend) SendNewSticker(chatID string, webp []byte, reply *model.Message) *model.Message {
	m := &model.Message{ChatID: chatID, FromMe: true, Kind: model.KindSticker, Media: model.MediaSticker,
		Time: b.now(), Receipt: model.Sent, Quote: quoteOf(reply)}
	b.add(m)
	b.mu.Lock()
	if b.media == nil {
		b.media = map[string][]byte{}
	}
	b.media[chatID+"/"+m.ID] = webp
	b.mu.Unlock()
	return b.copyOf(m)
}
