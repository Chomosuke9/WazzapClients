// Package mock is a Backend with fake chats, for the demo mode and screenshots.
package mock

import (
	"fmt"
	"slices"
	"sort"
	"sync"
	"time"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

// Backend serves demo data. It never touches the network.
type Backend struct {
	mu     sync.Mutex
	chats  []*model.Chat
	msgs   map[string][]*model.Message
	events []model.Event
	notify func()
	now    func() time.Time
	meName string
	prefs  map[string]string
	lists  []*model.ChatList
	extras
}

// New returns a demo backend with timestamps relative to the current time.
func New() *Backend {
	b := &Backend{msgs: make(map[string][]*model.Message), now: time.Now}
	for _, d := range demo(b.now()) {
		for i, m := range d.Messages {
			m.ID = fmt.Sprintf("%s-%d", d.ID, i)
			m.ChatID = d.ID
			if m.FromMe && m.Receipt == model.Pending {
				m.Receipt = model.Read
			}
		}
		c := model.Chat{
			ID: d.ID, Name: d.Name, IsGroup: d.IsGroup, Pinned: d.Pinned, Favorite: d.Favorite,
			Muted: d.Muted, Unread: d.Unread, Presence: d.Presence, Typing: d.Typing,
		}
		if n := len(d.Messages); n > 0 {
			c.Last = d.Messages[n-1]
			c.Time = c.Last.Time
		}
		b.chats = append(b.chats, &c)
		b.msgs[d.ID] = d.Messages
	}
	now := b.now()
	b.extras = demoExtras(func(d, h, m int) time.Time {
		y, mo, dd := now.AddDate(0, 0, -d).Date()
		return time.Date(y, mo, dd, h, m, 0, 0, now.Location())
	})
	b.addChannelPosts()
	b.lists = []*model.ChatList{{ID: "l1", Name: "Family", Chats: []string{"family", "mom"}}, {ID: "l2", Name: "Work"}}
	return b
}

// addChannelPosts makes each channel's last post openable.
func (b *Backend) addChannelPosts() {
	for _, ch := range b.channels {
		if ch.Last == nil {
			continue
		}
		ch.Last.ID = ch.ID + "-0"
		ch.Last.ChatID = ch.ID
		if ch.Last.Receipt == model.Pending {
			ch.Last.Receipt = model.Read
		}
		b.msgs[ch.ID] = []*model.Message{ch.Last}
	}
}

func (b *Backend) Start(notify func()) {
	b.notify = notify
	me := b.meName
	if me == "" {
		me = "Me Myself"
	}
	b.emit(model.ConnEvent{State: model.StateOnline, Me: me, MeID: "me@lid"})
}

func (b *Backend) emit(e model.Event) {
	b.mu.Lock()
	b.events = append(b.events, e)
	b.mu.Unlock()
	if b.notify != nil {
		b.notify()
	}
}

func (b *Backend) Poll() []model.Event {
	b.mu.Lock()
	defer b.mu.Unlock()
	ev := b.events
	b.events = nil
	return ev
}

func (b *Backend) Chats() []*model.Chat {
	out := make([]*model.Chat, len(b.chats))
	for i, c := range b.chats {
		cc := *c
		out[i] = &cc
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Pinned != out[j].Pinned {
			return out[i].Pinned
		}
		return out[i].Time.After(out[j].Time)
	})
	return out
}

func (b *Backend) Messages(chatID string, limit int) []*model.Message {
	m := b.msgs[chatID]
	if len(m) > limit {
		m = m[len(m)-limit:]
	}
	return append([]*model.Message(nil), m...)
}

func (b *Backend) MessagesBefore(chatID, id string, limit int) []*model.Message {
	m := b.msgs[chatID]
	i := slices.IndexFunc(m, func(m *model.Message) bool { return m.ID == id })
	if i < 0 {
		return nil
	}
	return append([]*model.Message(nil), m[max(0, i-limit):i]...)
}

func (b *Backend) MessagesFrom(chatID, id string, limit int) []*model.Message {
	m := b.msgs[chatID]
	i := slices.IndexFunc(m, func(m *model.Message) bool { return m.ID == id })
	if i < 0 {
		return nil
	}
	return append([]*model.Message(nil), m[i:min(len(m), i+limit)]...)
}

func (b *Backend) PinnedMessage(chatID string) *model.Message {
	m := b.msgs[chatID]
	for i := len(m) - 1; i >= 0; i-- {
		if m[i].Pinned && m[i].Kind != model.KindDeleted {
			return m[i]
		}
	}
	return nil
}

func (b *Backend) Open(chatID string) {
	for _, c := range b.chats {
		if c.ID == chatID && c.Unread > 0 {
			c.Unread = 0
			cc := *c
			b.emit(model.ChatEvent{Chat: &cc})
		}
	}
}

func (b *Backend) Avatar(string) []byte            { return nil }
func (b *Backend) MediaData(string, string) []byte { return nil }
func (b *Backend) Logout()                         {}
func (b *Backend) Retry()                          {}
func (b *Backend) Close()                          {}

type demoChat struct {
	ID, Name, Presence, Typing       string
	IsGroup, Pinned, Favorite, Muted bool
	Unread                           int
	Messages                         []*model.Message
}

func demo(now time.Time) []*demoChat {
	day := func(d int, h, m int) time.Time {
		y, mo, dd := now.AddDate(0, 0, -d).Date()
		return time.Date(y, mo, dd, h, m, 0, 0, now.Location())
	}
	txt := func(fromMe bool, t time.Time, s string) *model.Message {
		return &model.Message{FromMe: fromMe, Text: s, Time: t, Receipt: model.Read}
	}
	grp := func(sender string, t time.Time, s string) *model.Message {
		return &model.Message{Sender: sender, Text: s, Time: t}
	}

	rina := &demoChat{
		ID: "rina", Name: "Rina Kartika", Pinned: true, Favorite: true, Unread: 2,
		Presence: "online",
		Messages: []*model.Message{
			txt(false, day(1, 19, 2), "Hey! Are we still on for the weekend trip?"),
			txt(true, day(1, 19, 5), "Yes!! I already booked the villa in Ubud 🏡"),
			txt(true, day(1, 19, 5), "Check in Friday 2pm, check out Sunday noon"),
			txt(false, day(1, 19, 11), "Perfect. I'll bring the camera"),
			txt(false, day(1, 19, 11), "Should we rent a car or just use Grab the whole time?"),
			txt(true, day(1, 19, 20), "Rent a car I think, it's cheaper for 4 people and we can go to Tegallalang early in the morning before it gets crowded"),
			{FromMe: false, Kind: model.KindImage, Text: "Found this spot near the villa", Time: day(0, 8, 41), ImageA: 0x3a7bd5, ImageB: 0x00d2ff},
			{FromMe: true, Text: "Wow that looks amazing", Time: day(0, 8, 45), Receipt: model.Read, Reaction: "❤️",
				Quote: &model.Quote{Sender: "Rina Kartika", Text: "📷 Found this spot near the villa"}},
			txt(true, day(0, 8, 46), "Let's go there on Saturday"),
			txt(false, day(0, 9, 30), "Deal 😄"),
			txt(false, day(0, 9, 31), "Also can you send me the villa address? My mom keeps asking"),
		},
	}

	family := &demoChat{
		ID: "family", Name: "Keluarga Besar", IsGroup: true, Pinned: true, Unread: 14, Muted: true,
		Presence: "Mama, Papa, Dimas, Sari, You",
		Messages: []*model.Message{
			grp("Mama", day(0, 6, 2), "Selamat pagi semua 🌞"),
			grp("Papa", day(0, 6, 15), "Pagi. Jangan lupa makan siang di rumah hari Minggu ya"),
			grp("Dimas", day(0, 7, 1), "Siap pa 👍"),
			grp("Sari", day(0, 7, 3), "Aku bawa kue dari toko yang kemarin"),
			txt(true, day(0, 7, 20), "Aku datang agak telat, jam 12an"),
			grp("Mama", day(0, 9, 12), "Oke nak, hati-hati di jalan"),
		},
	}

	work := &demoChat{
		ID: "work", Name: "Product Team", IsGroup: true, Unread: 3,
		Presence: "Andre, Bima, Clara, Dewi, You", Typing: "Clara",
		Messages: []*model.Message{
			grp("Andre", day(0, 9, 2), "Standup in 5"),
			grp("Andre", day(0, 9, 4), "Webhook payload from yesterday's failed upload:\n```json\n"+demoPayload+"\n```"),
			grp("Dewi", day(0, 9, 12), "⁨\u2063@all⁩ demo for the client moves to 3pm"),
			grp("Dewi", day(0, 9, 13), "⁨\u2062@admin⁩ can someone approve the staging deploy?"),
			grp("Clara", day(0, 9, 30), "Release candidate is up on staging. ⁨\u2063@You⁩ can you check the release notes? ⁨@Bima⁩ too"),
			{Sender: "Bima", SenderID: "bima", Kind: model.KindSticker, Media: model.MediaSticker, Time: day(0, 9, 31),
				Quote: &model.Quote{Sender: "Clara", Text: "Release candidate is up on staging. ⁨\u2063@You⁩ can you check the release notes? ⁨@Bima⁩ too"}},
			grp("Bima", day(0, 9, 34), "Nice, I'll run the smoke tests"),
		},
	}

	chats := []*demoChat{
		rina, family, work,
		{ID: "budi", Name: "Budi Santoso", Presence: "last seen today at 08:12", Messages: []*model.Message{
			txt(false, day(0, 8, 2), "Bro, jadi futsal nanti malam?"), {FromMe: true, Text: "Jadi, jam 8 ya", Time: day(0, 8, 10), Receipt: model.Delivered},
		}},
		{ID: "shop", Name: "Kopi Senja", Presence: "Business account", Messages: []*model.Message{
			{Text: "*Order #4821 is ready* ☕\nYour iced latte is waiting at the counter.", Footer: "Kopi Senja · Jl. Braga 12",
				Time: day(0, 7, 55), Buttons: []model.Button{
					{Kind: model.ButtonReply, Label: "On my way", Value: "otw"},
					{Kind: model.ButtonCopy, Label: "Copy code", Value: "KS-4821"},
					{Kind: model.ButtonURL, Label: "Track order", Value: "https://example.com/track/4821"},
				}},
			{Kind: model.KindUnsupported, Time: day(0, 7, 56)},
			{Text: "Eh, `/setting` isn't a command I know 😅\n\n> Say it in plain words, like \"turn off greetings\"\n> or \"add a rule\".\n\n**Menu** today:\n- Iced latte\n- ~Croissant~ _sold out_\n1. Pick up at the counter\n2. Show code `KS-4821`\n\n【注文】ご来店ありがとうございます！ *太字* 谢谢\n\n```\nQuiz for you - 1/3\n```",
				Time: day(0, 7, 57)},
		}},
		{ID: "mom", Name: "Mama", Favorite: true, Presence: "online", Messages: []*model.Message{
			txt(false, day(1, 20, 1), "Sudah makan belum?"), txt(true, day(1, 20, 30), "Sudah ma 😊"),
		}},
		{ID: "gym", Name: "Gym Buddies", IsGroup: true, Muted: true, Unread: 27, Presence: "Kevin, Leo, Mike, You", Messages: []*model.Message{
			grp("Kevin", day(1, 21, 40), "Leg day tomorrow, no excuses 🦵"),
		}},
		{ID: "clara", Name: "Clara Wijaya", Presence: "last seen yesterday at 23:40", Messages: []*model.Message{
			{FromMe: true, Text: "Thanks for the review!", Time: day(1, 16, 3), Receipt: model.Read},
		}},
		{ID: "landlord", Name: "Pak Harto (Kos)", Presence: "last seen 2 days ago", Messages: []*model.Message{
			txt(false, day(2, 10, 0), "Mas, pembayaran bulan ini sudah saya terima. Terima kasih"),
		}},
		{ID: "dewi", Name: "Dewi Lestari", Presence: "online", Messages: []*model.Message{
			{FromMe: true, Text: "See you at the conference!", Time: day(3, 14, 22), Receipt: model.Delivered},
		}},
		{ID: "courier", Name: "+62 812-3456-7890", Presence: "", Messages: []*model.Message{
			txt(false, day(4, 11, 5), "Paket sudah di depan pintu ya kak"),
		}},
		{ID: "uni", Name: "Alumni TI 2019", IsGroup: true, Muted: true, Presence: "142 members", Messages: []*model.Message{
			grp("Fajar", day(5, 19, 0), "Reuni tahun ini di Bandung, yang mau ikut isi form ya"),
		}},
		{ID: "andre", Name: "Andre", Presence: "last seen recently", Messages: []*model.Message{
			{FromMe: true, Kind: model.KindImage, Text: "", Time: day(6, 12, 30), Receipt: model.Read, ImageA: 0xf7971e, ImageB: 0xffd200},
		}},
		{ID: "sari", Name: "Sari", Presence: "last seen recently", Messages: []*model.Message{
			txt(false, day(9, 17, 45), "Happy birthday!! 🎉🎂"), txt(true, day(9, 18, 0), "Thank you Sari!!"),
		}},
		{ID: "bank", Name: "Kevin Pratama", Presence: "last seen recently", Messages: []*model.Message{
			txt(false, day(15, 9, 0), "Ok noted"),
		}},
	}
	return chats
}

// demoPayload is a long message with words wider than a bubble.
const demoPayload = `{
  "key": {
    "id": "ACD80DE0EC2275EECAB1B4A487615253",
    "remoteJid": "120363425908525988@g.us",
    "fromMe": false
  },
  "message": {
    "imageMessage": {
      "URL": "https://mmg.whatsapp.net/v/t62.7118-24/796144583_4522803077960082_8ccb=11-4&oh=01_Q5Aa5gH7L8XOkHm8",
      "mimetype": "image/jpeg",
      "fileSHA256": "/yAxP7afG0KX+ig1w8S/mtcK4KvdGRP2HhcMuIa8FFA=",
      "fileLength": "44840",
      "height": 704,
      "width": 699,
      "mediaKey": "EWbkHEbqabqw2xi13RSWqfDqOlAr6QFNP7O0Y2Vb5mE=",
      "fileEncSHA256": "jhsPv3LYM8PU30c5M977a/sMI/T2PG3SJ0la1zuPrVI=",
      "directPath": "/v/t62.7118-24/796144583_4522803077960082_8ccb=11-4&oh=01_Q5Aa5gH7L8XOkHm8",
      "mediaKeyTimestamp": "1790817086",
      "JPEGThumbnail": "/9j/4AAQSkZJRgABAQAAAQABAAD/2wCEABsbGxscGx4hIR4qLSgtKj04MzM4PV1CR0JHQl2NWGdYWGdYjX2Xe3N7l33gsJycsOD/2c7Z//////////////////////////////////8BGxsbGxwbHiEhHiotKC0qPTgzMzg9XUJHQkdCXY1YZ1hYZ1iNfZd7c3uXfeCwnJyw4P/Zztn////////////////////////////////////"
    }
  }
}`
