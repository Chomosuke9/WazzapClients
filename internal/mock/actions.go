package mock

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

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
	if d.Link.Shown(d.Text) {
		m.Link, m.Thumb = d.Link, d.LinkThumb
	}
	b.add(m)
	cp := *m
	return &cp
}

// Receive delivers a message from who in chat, as if it just arrived.
func (b *Backend) Receive(chatID, who, text string) {
	m := &model.Message{ChatID: chatID, Sender: who, SenderID: who, Text: text, Time: b.now()}
	b.add(m)
	cp := *m
	b.emit(model.MessageEvent{Msg: &cp, New: true})
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

func (b *Backend) Stickers(model.StickerSet) []*model.Message { return nil }

func (b *Backend) FavoriteSticker(m *model.Message) bool { return b.favs[m.ChatID+"/"+m.ID] }

func (b *Backend) SetFavoriteSticker(m *model.Message, fav bool) {
	if b.favs == nil {
		b.favs = map[string]bool{}
	}
	b.favs[m.ChatID+"/"+m.ID] = fav
	b.emit(model.StickersEvent{})
}

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
		b.emit(model.MessageEvent{Msg: deepCopy(x)})
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

func (b *Backend) OpenedViewOnce(m *model.Message) {
	b.update(m, func(x *model.Message) { x.Opened = true })
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

// copyOf returns a copy of the stored message behind m.
func (b *Backend) copyOf(m *model.Message) *model.Message {
	return deepCopy(b.find(m))
}

func (b *Backend) OpenMedia(*model.Message) {
	b.emit(model.NoticeEvent{Text: "Demo mode doesn't open files."})
}

// MediaFile plays the file named by $WAZZAP_DEMO_VIDEO (videos) or
// $WAZZAP_DEMO_AUDIO (voice and audio messages), to try the players.
func (b *Backend) MediaFile(m *model.Message) string {
	if p := b.demoFile(m); p != "" {
		return p
	}
	b.emit(model.NoticeEvent{Text: "Demo mode doesn't download files."})
	b.emit(model.MediaEvent{ChatID: m.ChatID, MsgID: m.ID, Failed: true})
	return ""
}

func (b *Backend) HasMediaFile(m *model.Message) bool { return b.demoFile(m) != "" }

func (b *Backend) demoFile(m *model.Message) string {
	switch m.Media {
	case model.MediaVideo, model.MediaGIF:
		return os.Getenv("WAZZAP_DEMO_VIDEO")
	case model.MediaVoice, model.MediaAudio:
		return os.Getenv("WAZZAP_DEMO_AUDIO")
	}
	return ""
}

// SendFile sends a file in name only: its content isn't read.
func (b *Backend) SendFile(chatID string, a model.Attachment, d model.Draft) *model.Message {
	m := b.Send(chatID, d)
	b.update(m, func(x *model.Message) {
		x.Media, x.FileName = a.Media, filepath.Base(a.Path)
		if st, err := os.Stat(a.Path); err == nil {
			x.FileSize = st.Size()
		}
		switch a.Media {
		case model.MediaImage, model.MediaVideo:
			x.Kind, x.ImageA, x.ImageB, x.Album = model.KindImage, 0x5f6f7f, 0x9fafbf, a.Album
			if a.ViewOnce {
				x.Kind, x.Album = model.KindViewOnce, ""
			}
		case model.MediaDocument:
			if x.Text == "" {
				x.Text = x.FileName
			}
		}
	})
	return b.copyOf(m)
}

// NewAlbum names an album after the chat's next message.
func (b *Backend) NewAlbum(chatID string, photos, videos int) string {
	if photos+videos < 2 {
		return ""
	}
	return fmt.Sprintf("%s-album-%d", chatID, len(b.msgs[chatID]))
}

func (b *Backend) SendContacts(chatID string, ids []string) *model.Message {
	var names []string
	for _, id := range ids {
		if c := b.chat(id); c != nil {
			names = append(names, c.Name)
		}
	}
	m := b.Send(chatID, model.Draft{Text: strings.Join(names, ", ")})
	b.update(m, func(x *model.Message) {
		x.Media = model.MediaContact
		for _, n := range names {
			x.Contacts = append(x.Contacts, model.ContactCard{Name: n,
				Phones: []model.ContactPhone{{Number: "+62 812-5550-1234", WAID: "6281255501234"}}})
		}
	})
	return b.copyOf(m)
}

func (b *Backend) SendPoll(chatID string, p model.Poll) *model.Message {
	m := b.Send(chatID, model.Draft{Text: p.Question})
	b.update(m, func(x *model.Message) {
		x.Media, x.Poll = model.MediaPoll, &model.PollState{Max: 1}
		if p.Multiple {
			x.Poll.Max = 0
		}
		for _, o := range p.Options {
			x.Poll.Options = append(x.Poll.Options, model.PollOption{Name: o})
		}
	})
	return b.copyOf(m)
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
func (b *Backend) SetMuted(id string, v bool, d time.Duration) {
	b.setChat(id, func(c *model.Chat) {
		c.Muted, c.MuteUntil = v, time.Time{}
		if v && d > 0 {
			c.MuteUntil = b.now().Add(d)
		}
	})
}
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
		txt = strings.ReplaceAll(txt, "@all", "⁨"+string(model.MentionNotifies)+"@all⁩")
	}
	if d.MentionAdmins {
		txt = strings.ReplaceAll(txt, "@"+chatID, "⁨"+string(model.MentionAdmins)+"@admin⁩")
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

func (b *Backend) SetBlocked(id string, blocked bool) {
	if info := b.Info(id); info != nil {
		info.Blocked = blocked
		if b.infos == nil {
			b.infos = map[string]*model.ChatInfo{}
		}
		b.infos[id] = info
	}
	b.setBlockedContact(id, blocked)
	b.emit(model.InfoEvent{ChatID: id})
}

func (b *Backend) ExportChat(string) {
	b.emit(model.NoticeEvent{Text: "Demo mode doesn't export chats."})
}

// EditText returns m's text with its mentions as "@Name".
func (b *Backend) EditText(m *model.Message) (string, []model.MentionRef) {
	x := b.find(m)
	if x == nil {
		return "", nil
	}
	var refs []model.MentionRef
	var sb strings.Builder
	for rest := x.Text; ; {
		i := strings.IndexRune(rest, '⁨')
		j := strings.IndexRune(rest, '⁩')
		if i < 0 || j < i {
			sb.WriteString(rest)
			break
		}
		sb.WriteString(rest[:i])
		name := strings.TrimLeft(rest[i+len("⁨"):j], string([]rune{model.MentionNotifies, model.MentionAdmins}))
		sb.WriteString(name)
		name = strings.TrimPrefix(name, "@")
		id := "@" + name
		if info := b.Info(m.ChatID); info != nil && name != "all" && name != "admin" {
			for _, mb := range info.Members {
				if mb.Name == name {
					id = mb.ID
				}
			}
		}
		refs = append(refs, model.MentionRef{Name: name, ID: id})
		rest = rest[j+len("⁩"):]
	}
	return sb.String(), refs
}

func (b *Backend) Edit(m *model.Message, d model.Draft) {
	b.update(m, func(x *model.Message) {
		if b.versions == nil {
			b.versions = map[string][]model.Version{}
		}
		since := x.Edited
		if since.IsZero() {
			since = x.Time
		}
		k := x.ChatID + "/" + x.ID
		b.versions[k] = append(b.versions[k], model.Version{Text: x.Text, Time: since})
		x.Text, x.Edited = b.showMentions(m.ChatID, d), b.now()
	})
	if c := b.chat(m.ChatID); c != nil && c.Last != nil && c.Last.ID == m.ID {
		b.emitChat(c.ID)
	}
}

func (b *Backend) Versions(m *model.Message) []model.Version {
	return b.versions[m.ChatID+"/"+m.ID]
}

// MessageInfo makes up when the people a message went to got and read it,
// in step with its ticks: a minute or two apart, after it was sent.
func (b *Backend) MessageInfo(m *model.Message) *model.MessageInfo {
	info := &model.MessageInfo{Members: 1}
	var people []model.Member
	if c := b.chat(m.ChatID); c != nil && c.IsGroup {
		if ci := b.Info(m.ChatID); ci != nil {
			for _, p := range ci.Members {
				if !p.Me {
					people = append(people, p)
				}
			}
		}
		info.Members = max(len(people), 1)
	} else if c != nil {
		people = []model.Member{{ID: c.ID, Name: c.Name}}
	}
	for i, p := range people {
		r := model.PersonReceipt{ID: p.ID, Name: p.Name}
		got := m.Receipt >= model.Delivered || m.Receipt == model.Sent && i < len(people)/2
		read := m.Receipt == model.Read || m.Receipt == model.Delivered && i < len(people)/2
		if got {
			r.Delivered = m.Time.Add(time.Duration(i+1) * 20 * time.Second)
		}
		if read {
			r.Read = r.Delivered.Add(time.Duration(i+2) * time.Minute)
			if m.Media == model.MediaVoice {
				r.Played = r.Read.Add(30 * time.Second)
			}
		}
		if got {
			info.Receipts = append(info.Receipts, r)
		}
	}
	return info
}
