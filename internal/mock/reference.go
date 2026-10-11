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
	nova := "nova@lid"
	specs := []chatSpec{
		{model.Chat{ID: "hikers@g.us", Name: "Weekend Hikers", IsGroup: true, Pinned: true, Muted: true},
			[]*model.Message{in("Raka Pratama", "raka@lid", "Trail starts at 6, don't be late", at(0, 0, 8))}},
		{model.Chat{ID: "bookclub@g.us", Name: "Book Club", IsGroup: true, Pinned: true, Muted: true},
			[]*model.Message{in("Sinta Maharani", "sinta@lid", "chapter 5 done", at(1, 22, 40))}},
		{model.Chat{ID: "me@lid", Name: "Alex Rivera", Pinned: true, Self: true},
			[]*model.Message{out("Grocery list: eggs, rice, coffee", at(1, 21, 5), model.Read)}},
		{model.Chat{ID: "botlab@g.us", Name: "Bot Playground", IsGroup: true, Muted: true, Presence: "Dimas, Nova, You"},
			[]*model.Message{
				out("Okay, sounds good", at(1, 9, 49), model.Sent),
				in("Nova", nova, "Conversation history was reset.", at(1, 9, 50)),
				{FromMe: true, Kind: model.KindDeleted, Time: at(1, 9, 51), Receipt: model.Sent},
				{Sender: "Nova", SenderID: nova, Time: at(1, 9, 51),
					Text:  "Sorry @alex, promos aren't allowed in this group. I removed your message. 🙏",
					Quote: &model.Quote{Sender: "Group", Text: "hello"}},
				{FromMe: true, Text: "Test", Time: at(0, 2, 19), Receipt: model.Sent, Reactions: reacts("👋")},
				out("Nova", at(0, 2, 19), model.Sent),
				in("Nova", nova, "Hi! How can I help? 😊", at(0, 2, 19)),
				out("Hi Nova", at(0, 2, 20), model.Sent),
				in("Nova", nova, "Hi there! What are you up to?", at(0, 2, 20)),
				out("/reset", at(0, 2, 20), model.Sent),
				in("Nova", nova, "Conversation history was reset.", at(0, 2, 20)),
			}},
		{model.Chat{ID: "lounge@g.us", Name: "Lounge", IsGroup: true, Muted: true, General: true},
			[]*model.Message{in("Nova", nova, "@~Kai hi! Anyone up for a chat?", at(0, 2, 0))}},
		{model.Chat{ID: "botsonly@g.us", Name: "Bots Only", IsGroup: true, Muted: true},
			[]*model.Message{{Sender: "~Pixel Bot", SenderID: "pixel@lid", Media: model.MediaImage, Kind: model.KindImage,
				Text: "5 photos", Time: at(0, 1, 10), ImageA: 0x355c7d, ImageB: 0xc06c84}}},
		{model.Chat{ID: "study@g.us", Name: "Study Group 5", IsGroup: true, Muted: true},
			[]*model.Message{{Sender: "~milo", SenderID: "milo@lid", Media: model.MediaSticker, Kind: model.KindSticker,
				Time: at(0, 0, 0)}}},
		{model.Chat{ID: "jordan@lid", Name: "Jordan Lee", Presence: "online"},
			[]*model.Message{in("", "", "Just send me the architecture", at(1, 18, 30))}},
		{model.Chat{ID: "apinews@g.us", Name: "Acme API Updates", IsGroup: true, Muted: true},
			[]*model.Message{in("~Sam Carter", "sam@lid", "The new model is available now", at(1, 16, 2))}},
		{model.Chat{ID: "litclass@g.us", Name: "Literature Class 11", IsGroup: true, Muted: true},
			[]*model.Message{in("Ms. Wulan", "wulan@lid", "Thank you, everyone", at(1, 14, 12))}},
		{model.Chat{ID: "carpool@g.us", Name: "Carpool Crew", IsGroup: true, Muted: true},
			[]*model.Message{in("~Ojan", "ojan@lid", "ok bro", at(1, 12, 0))}},
		// Community groups, shown on the Communities screen.
		{model.Chat{ID: "anime-ann@g.us", Name: "Announcements", IsGroup: true, Muted: true, Unread: 2},
			[]*model.Message{
				{Sender: "~MangaFan42", SenderID: "mangafan@lid", Media: model.MediaImage, Kind: model.KindImage,
					Time: at(2, 0, 59), ImageA: 0x2b2633, ImageB: 0xd9d4cf, Reactions: reacts("😹", "🍿")},
				{Sender: "~MangaFan42", SenderID: "mangafan@lid", Media: model.MediaSticker, Kind: model.KindSticker,
					Time: at(2, 1, 45)},
				in("~MangaFan42", "mangafan@lid", "Don't forget to read the group rules", at(2, 2, 10)),
				in("", "", "~Rei replied to an announcement", at(2, 5, 0)),
			}},
		{model.Chat{ID: "sketch@g.us", Name: "Art Club : Sketches", IsGroup: true, Muted: true},
			[]*model.Message{{Sender: "~Pablo", SenderID: "pablo@lid", Media: model.MediaSticker, Kind: model.KindSticker,
				Time: at(3, 20, 0)}}},
		{model.Chat{ID: "nova-ann@g.us", Name: "Announcements", IsGroup: true, Muted: true},
			[]*model.Message{
				{FromMe: true, Media: model.MediaImage, Kind: model.KindImage, Text: "Now supports multiple accounts",
					Time: at(3, 8, 30), Receipt: model.Sent, ImageA: 0x1f2a30, ImageB: 0x3a4a52},
				{FromMe: true, Media: model.MediaImage, Kind: model.KindImage, Text: "Quick commands",
					Time: at(3, 12, 50), Receipt: model.Sent, ImageA: 0x16191a, ImageB: 0x2c3133},
				{FromMe: true, Media: model.MediaImage, Kind: model.KindImage,
					Time: at(3, 12, 50), Receipt: model.Sent, ImageA: 0x202425, ImageB: 0x3b4143},
				in("Nova", nova, "Lunch break, back in an hour @all", at(3, 19, 0)),
			}},
		{model.Chat{ID: "support@g.us", Name: "Support", IsGroup: true, Muted: true},
			[]*model.Message{in("", "", "~Robin requested to join", at(4, 9, 0))}},
		{model.Chat{ID: "apinews-ann@g.us", Name: "Announcements", IsGroup: true, Muted: true},
			[]*model.Message{in("", "", "Welcome to the community!", at(6, 9, 0))}},
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
	b.extras = referenceExtras(at)
	b.meName = "alex"
	b.addChannelPosts()
	b.seedReactors()
	return b
}
