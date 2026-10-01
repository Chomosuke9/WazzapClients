package mock

import (
	"fmt"
	"os"
	"strings"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

// Message and chat actions. They change the demo data in memory only.

func (b *Backend) chat(id string) *model.Chat {
	for _, c := range b.chats {
		if c.ID == id {
			return c
		}
	}
	return nil
}

func (b *Backend) emitChat(id string) {
	if c := b.chat(id); c != nil {
		cc := *c
		b.emit(model.ChatEvent{Chat: &cc})
	}
}

// find returns the stored message behind m.
func (b *Backend) find(m *model.Message) *model.Message {
	for _, x := range b.msgs[m.ChatID] {
		if x.ID == m.ID {
			return x
		}
	}
	return nil
}

func (b *Backend) add(m *model.Message) {
	m.ID = fmt.Sprintf("%s-%d", m.ChatID, len(b.msgs[m.ChatID]))
	b.msgs[m.ChatID] = append(b.msgs[m.ChatID], m)
	if c := b.chat(m.ChatID); c != nil {
		c.Last, c.Time = m, m.Time
		b.emitChat(c.ID)
	}
}

func (b *Backend) Send(chatID string, d model.Draft) *model.Message {
	m := &model.Message{ChatID: chatID, FromMe: true, Text: b.showMentions(chatID, d), Time: b.now(), Receipt: model.Sent}
	m.Quote = quoteOf(d.Reply)
	b.add(m)
	cp := *m
	return &cp
}

// quoteOf returns the quote of a reply to r, or nil.
func quoteOf(r *model.Message) *model.Quote {
	if r == nil {
		return nil
	}
	name := r.Sender
	if r.FromMe {
		name = ""
	}
	return &model.Quote{ID: r.ID, Sender: name, SenderID: r.SenderID, Text: r.Text, Media: r.Media}
}

func (b *Backend) copyTo(src *model.Message, chatID string, forwarded bool, quote *model.Quote) {
	m := *src
	m.ChatID, m.FromMe, m.Time, m.Receipt = chatID, true, b.now(), model.Sent
	m.Sender, m.SenderID, m.Quote, m.Reaction, m.Starred, m.Pinned = "", "", quote, "", false, false
	m.Forwarded = forwarded
	b.add(&m)
	cp := m
	b.emit(model.MessageEvent{Msg: &cp})
}

func (b *Backend) PressButton(m *model.Message, i int) *model.Message {
	if i < 0 || i >= len(m.Buttons) || m.Buttons[i].Kind != model.ButtonReply {
		return nil
	}
	return b.Send(m.ChatID, model.Draft{Text: m.Buttons[i].Label, Reply: m})
}

func (b *Backend) SendSticker(chatID string, s, reply *model.Message) {
	b.copyTo(s, chatID, false, quoteOf(reply))
}

func (b *Backend) Stickers() []*model.Message { return nil }

func (b *Backend) Forward(msgs []*model.Message, chatIDs []string) {
	for _, c := range chatIDs {
		for _, m := range msgs {
			b.copyTo(m, c, true, nil)
		}
	}
}

// update changes a stored message and reports it.
func (b *Backend) update(m *model.Message, f func(*model.Message)) {
	if x := b.find(m); x != nil {
		f(x)
		cp := *x
		b.emit(model.MessageEvent{Msg: &cp})
	}
}

func (b *Backend) React(m *model.Message, emoji string) {
	b.update(m, func(x *model.Message) { x.Reaction = emoji })
}

func (b *Backend) Delete(m *model.Message, forEveryone bool) {
	if forEveryone {
		b.update(m, func(x *model.Message) {
			x.Kind, x.Media, x.Text, x.Quote, x.Thumb, x.ImageA, x.ImageB = model.KindDeleted, model.MediaNone, "", nil, nil, 0, 0
		})
		return
	}
	msgs := b.msgs[m.ChatID]
	for i, x := range msgs {
		if x.ID == m.ID {
			b.msgs[m.ChatID] = append(msgs[:i:i], msgs[i+1:]...)
		}
	}
	b.emit(model.DeletedEvent{ChatID: m.ChatID, IDs: []string{m.ID}})
}

func (b *Backend) Star(m *model.Message, starred bool) {
	b.update(m, func(x *model.Message) { x.Starred = starred })
}

func (b *Backend) PinMessage(m *model.Message, pinned bool) {
	for _, x := range b.msgs[m.ChatID] {
		x.Pinned = pinned && x.ID == m.ID
	}
	b.emit(model.DeletedEvent{ChatID: m.ChatID})
}

func (b *Backend) SaveMedia(*model.Message) {
	b.emit(model.NoticeEvent{Text: "Demo mode doesn't save files."})
}

func (b *Backend) PlayMedia(*model.Message) {
	b.emit(model.NoticeEvent{Text: "Demo mode doesn't play videos."})
}

// VideoFile plays the file named by $WAZZAP_DEMO_VIDEO, to try the player.
func (b *Backend) VideoFile(m *model.Message) string {
	if p := os.Getenv("WAZZAP_DEMO_VIDEO"); p != "" {
		return p
	}
	b.emit(model.NoticeEvent{Text: "Demo mode doesn't play videos."})
	b.emit(model.MediaEvent{ChatID: m.ChatID, MsgID: m.ID, Failed: true})
	return ""
}

func (b *Backend) setChat(id string, f func(*model.Chat)) {
	if c := b.chat(id); c != nil {
		f(c)
		b.emitChat(id)
	}
}

func (b *Backend) SetArchived(id string, v bool) {
	b.setChat(id, func(c *model.Chat) { c.Archived = v; c.Pinned = c.Pinned && !v })
}
func (b *Backend) SetMuted(id string, v bool)  { b.setChat(id, func(c *model.Chat) { c.Muted = v }) }
func (b *Backend) SetPinned(id string, v bool) { b.setChat(id, func(c *model.Chat) { c.Pinned = v }) }
func (b *Backend) SetFavorite(id string, v bool) {
	b.setChat(id, func(c *model.Chat) { c.Favorite = v })
}

func (b *Backend) SetUnread(id string, v bool) {
	b.setChat(id, func(c *model.Chat) {
		c.Unread = 0
		if v {
			c.Unread = -1
		}
	})
}

func (b *Backend) Lists() []*model.ChatList { return b.lists }

func (b *Backend) SetInList(chatID, listID string, in bool) {
	for _, l := range b.lists {
		if l.ID != listID {
			continue
		}
		var chats []string
		for _, c := range l.Chats {
			if c != chatID {
				chats = append(chats, c)
			}
		}
		if in {
			chats = append(chats, chatID)
		}
		l.Chats = chats
	}
}

func (b *Backend) ClearChat(id string) {
	b.msgs[id] = nil
	b.setChat(id, func(c *model.Chat) { c.Last = nil })
	b.emit(model.DeletedEvent{ChatID: id})
}

func (b *Backend) DeleteChat(id string) {
	for i, c := range b.chats {
		if c.ID == id {
			b.chats = append(b.chats[:i:i], b.chats[i+1:]...)
			break
		}
	}
	delete(b.msgs, id)
	b.emit(model.ChatsEvent{Chats: b.Chats()})
}

func (b *Backend) LeaveGroup(string) {
	b.emit(model.NoticeEvent{Text: "You exited the group."})
}

func (b *Backend) Pref(key string) string { return b.prefs[key] }

func (b *Backend) SetPref(key, value string) {
	if b.prefs == nil {
		b.prefs = map[string]string{}
	}
	b.prefs[key] = value
}

// showMentions turns a draft's "@<user>" mentions into highlighted names,
// like the real backend does when it loads a message.
func (b *Backend) showMentions(chatID string, d model.Draft) string {
	mark := func(name string) string { return "⁨@" + name + "⁩" }
	txt := d.Text
	if d.MentionAll {
		txt = strings.ReplaceAll(txt, "@all", mark("all"))
	}
	if d.MentionAdmins {
		txt = strings.ReplaceAll(txt, "@"+chatID, mark("admin"))
	}
	if info := b.Info(chatID); info != nil {
		for _, m := range info.Members {
			user, _, _ := strings.Cut(m.ID, "@")
			for _, id := range d.Mentions {
				if id == m.ID {
					txt = strings.ReplaceAll(txt, "@"+user, mark(m.Name))
				}
			}
		}
	}
	return txt
}
