package mock

import (
	"fmt"
	"time"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

// NewReference returns demo data shaped like a real account (groups with
// push-name senders, stickers, a self chat, reactions, a deleted message),
// used to compare the UI side by side with WhatsApp Desktop.
func NewReference() *Backend {
	b := &Backend{msgs: make(map[string][]*model.Message), now: time.Now}
	now := b.now()
	at := func(daysAgo, h, m int) time.Time {
		y, mo, d := now.AddDate(0, 0, -daysAgo).Date()
		return time.Date(y, mo, d, h, m, 0, 0, now.Location())
	}
	type chatSpec struct {
		chat model.Chat
		msgs []*model.Message
	}
	in := func(sender, senderID, text string, t time.Time) *model.Message {
		return &model.Message{Sender: sender, SenderID: senderID, Text: text, Time: t}
	}
	out := func(text string, t time.Time, r model.Receipt) *model.Message {
		return &model.Message{FromMe: true, Text: text, Time: t, Receipt: r}
	}
	vivy := "vivy@lid"
	specs := []chatSpec{
		{model.Chat{ID: "tuff@g.us", Name: "Tuff", IsGroup: true, Pinned: true, Muted: true},
			[]*model.Message{in("Zaki Fauzan Azhima", "zaki@lid", "Ball knowledge is real", at(0, 0, 8))}},
		{model.Chat{ID: "puja@g.us", Name: "Puja Qiqi Ajaib", IsGroup: true, Pinned: true, Muted: true},
			[]*model.Message{in("Satria Wibowo", "satria@lid", "okeloh", at(1, 22, 40))}},
		{model.Chat{ID: "me@lid", Name: "@BagusAse", Pinned: true, Self: true},
			[]*model.Message{out("oc_sk_9fd085c1d2e4b7a3", at(1, 21, 5), model.Read)}},
		{model.Chat{ID: "test@g.us", Name: "test", IsGroup: true, Muted: true, Presence: "Agus, Vivy, You"},
			[]*model.Message{
				out("Oke sip", at(1, 9, 49), model.Sent),
				in("Vivy", vivy, "Conversation history was reset.", at(1, 9, 50)),
				{FromMe: true, Kind: model.KindDeleted, Time: at(1, 9, 51), Receipt: model.Sent},
				{Sender: "Vivy", SenderID: vivy, Time: at(1, 9, 51),
					Text:  "Maaf @whoami, promo/spam nggak boleh di grup ini ya. Pesannya sudah aku hapus. 🙏",
					Quote: &model.Quote{Sender: "Group", Text: "test"}},
				{FromMe: true, Text: "Test", Time: at(0, 2, 19), Receipt: model.Sent, Reaction: "👋"},
				out("Vivy", at(0, 2, 19), model.Sent),
				in("Vivy", vivy, "Halo! Ada yang bisa aku bantu? 😊", at(0, 2, 19)),
				out("Hai vivy", at(0, 2, 20), model.Sent),
				in("Vivy", vivy, "Hai juga! Lagi ngapain nih?", at(0, 2, 20)),
				out("/reset", at(0, 2, 20), model.Sent),
				in("Vivy", vivy, "Conversation history was reset.", at(0, 2, 20)),
			}},
		{model.Chat{ID: "chitchat@g.us", Name: "ChitChat", IsGroup: true, Muted: true},
			[]*model.Message{in("Vivy", vivy, "@~Natanael C halo! Ada yang mau ngobrol?", at(0, 2, 0))}},
		{model.Chat{ID: "pam@g.us", Name: "PAM : Chat Bot Only", IsGroup: true, Muted: true},
			[]*model.Message{{Sender: "~Dell Bot", SenderID: "dell@lid", Media: model.MediaImage, Kind: model.KindImage,
				Text: "5 photos", Time: at(0, 1, 10), ImageA: 0x355c7d, ImageB: 0xc06c84}}},
		{model.Chat{ID: "lab@g.us", Name: "Grup 5 Lab Komputer", IsGroup: true, Muted: true},
			[]*model.Message{{Sender: "~naufalll", SenderID: "naufal@lid", Media: model.MediaSticker, Kind: model.KindSticker,
				Time: at(0, 0, 0)}}},
		{model.Chat{ID: "alif@lid", Name: "Alif Varesa", Presence: "online"},
			[]*model.Message{in("", "", "Just send me the architecture", at(1, 18, 30))}},
		{model.Chat{ID: "zytro@g.us", Name: "ZYTRO API - UPDATE", IsGroup: true, Muted: true},
			[]*model.Message{in("~Muhammad Ramdhan", "ramdhan@lid", "Model Claude sudah tersedia", at(1, 16, 2))}},
		{model.Chat{ID: "bindo@g.us", Name: "B. Indo Kelas 16", IsGroup: true, Muted: true},
			[]*model.Message{in("Tuan Guru", "tuan@lid", "Terima kasih semuanya", at(1, 14, 12))}},
		{model.Chat{ID: "teman@g.us", Name: "TEMAN BANTU NAIK", IsGroup: true, Muted: true},
			[]*model.Message{in("~Oke", "oke@lid", "oke bg", at(1, 12, 0))}},
	}
	for _, s := range specs {
		c := s.chat
		for i, m := range s.msgs {
			m.ID = fmt.Sprintf("%s-%d", c.ID, i)
			m.ChatID = c.ID
		}
		c.Last = s.msgs[len(s.msgs)-1]
		c.Time = c.Last.Time
		b.chats = append(b.chats, &c)
		b.msgs[c.ID] = s.msgs
	}
	return b
}
