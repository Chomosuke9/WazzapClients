// Package mock provides fake chats and messages so the UI can be built and
// previewed before the WhatsApp backend (hypermeow) is wired in.
package mock

import (
	"fmt"
	"time"
)

// Receipt is the delivery state of an outgoing message.
type Receipt int

const (
	Pending Receipt = iota
	Sent
	Delivered
	Read
)

// Kind is the type of a message row in a conversation.
type Kind int

const (
	KindText Kind = iota
	KindImage
	KindEncryption // the "Messages are end-to-end encrypted" notice
)

// Quote is the message a reply points to.
type Quote struct {
	Sender string
	Text   string
}

// Message is a single conversation entry.
type Message struct {
	ID      string
	Kind    Kind
	FromMe  bool
	Sender  string // group chats only
	Text    string
	Time    time.Time
	Receipt Receipt
	Quote   *Quote
	// Image is a two-colour gradient placeholder until real media exists.
	ImageA, ImageB uint32
	Reaction       string
}

// Chat is a one-to-one or group conversation.
type Chat struct {
	ID       string
	Name     string
	IsGroup  bool
	Pinned   bool
	Muted    bool
	Favorite bool
	Unread   int
	Typing   string // who is typing; empty when nobody is
	Presence string // header subtitle, e.g. "online"
	Messages []*Message
}

// Last returns the most recent message, or nil.
func (c *Chat) Last() *Message {
	if len(c.Messages) == 0 {
		return nil
	}
	return c.Messages[len(c.Messages)-1]
}

// Send appends an outgoing text message.
func (c *Chat) Send(text string, now time.Time) {
	c.Messages = append(c.Messages, &Message{
		ID:      fmt.Sprintf("%s-%d", c.ID, len(c.Messages)),
		FromMe:  true,
		Text:    text,
		Time:    now,
		Receipt: Sent,
	})
}

// Chats returns a deterministic set of demo chats with timestamps relative to now.
func Chats(now time.Time) []*Chat {
	day := func(d int, h, m int) time.Time {
		y, mo, dd := now.AddDate(0, 0, -d).Date()
		return time.Date(y, mo, dd, h, m, 0, 0, now.Location())
	}
	txt := func(fromMe bool, t time.Time, s string) *Message {
		return &Message{FromMe: fromMe, Text: s, Time: t, Receipt: Read}
	}
	grp := func(sender string, t time.Time, s string) *Message {
		return &Message{Sender: sender, Text: s, Time: t}
	}
	enc := func() *Message { return &Message{Kind: KindEncryption, Time: day(40, 9, 0)} }

	rina := &Chat{
		ID: "rina", Name: "Rina Kartika", Pinned: true, Favorite: true, Unread: 2,
		Presence: "online",
		Messages: []*Message{
			enc(),
			txt(false, day(1, 19, 2), "Hey! Are we still on for the weekend trip?"),
			txt(true, day(1, 19, 5), "Yes!! I already booked the villa in Ubud 🏡"),
			txt(true, day(1, 19, 5), "Check in Friday 2pm, check out Sunday noon"),
			txt(false, day(1, 19, 11), "Perfect. I'll bring the camera"),
			txt(false, day(1, 19, 11), "Should we rent a car or just use Grab the whole time?"),
			txt(true, day(1, 19, 20), "Rent a car I think, it's cheaper for 4 people and we can go to Tegallalang early in the morning before it gets crowded"),
			{FromMe: false, Kind: KindImage, Text: "Found this spot near the villa", Time: day(0, 8, 41), ImageA: 0x3a7bd5, ImageB: 0x00d2ff},
			{FromMe: true, Text: "Wow that looks amazing", Time: day(0, 8, 45), Receipt: Read, Reaction: "❤️",
				Quote: &Quote{Sender: "Rina Kartika", Text: "📷 Found this spot near the villa"}},
			txt(true, day(0, 8, 46), "Let's go there on Saturday"),
			txt(false, day(0, 9, 30), "Deal 😄"),
			txt(false, day(0, 9, 31), "Also can you send me the villa address? My mom keeps asking"),
		},
	}

	family := &Chat{
		ID: "family", Name: "Keluarga Besar", IsGroup: true, Pinned: true, Unread: 14, Muted: true,
		Presence: "Mama, Papa, Dimas, Sari, You",
		Messages: []*Message{
			enc(),
			grp("Mama", day(0, 6, 2), "Selamat pagi semua 🌞"),
			grp("Papa", day(0, 6, 15), "Pagi. Jangan lupa makan siang di rumah hari Minggu ya"),
			grp("Dimas", day(0, 7, 1), "Siap pa 👍"),
			grp("Sari", day(0, 7, 3), "Aku bawa kue dari toko yang kemarin"),
			txt(true, day(0, 7, 20), "Aku datang agak telat, jam 12an"),
			grp("Mama", day(0, 9, 12), "Oke nak, hati-hati di jalan"),
		},
	}

	work := &Chat{
		ID: "work", Name: "Product Team", IsGroup: true, Unread: 3,
		Presence: "Andre, Bima, Clara, Dewi, You", Typing: "Clara",
		Messages: []*Message{
			enc(),
			grp("Andre", day(0, 9, 2), "Standup in 5"),
			grp("Clara", day(0, 9, 30), "Release candidate is up on staging"),
			grp("Bima", day(0, 9, 34), "Nice, I'll run the smoke tests"),
		},
	}

	chats := []*Chat{
		rina, family, work,
		{ID: "budi", Name: "Budi Santoso", Presence: "last seen today at 08:12", Messages: []*Message{
			enc(), txt(false, day(0, 8, 2), "Bro, jadi futsal nanti malam?"), {FromMe: true, Text: "Jadi, jam 8 ya", Time: day(0, 8, 10), Receipt: Delivered},
		}},
		{ID: "mom", Name: "Mama", Favorite: true, Presence: "online", Messages: []*Message{
			enc(), txt(false, day(1, 20, 1), "Sudah makan belum?"), txt(true, day(1, 20, 30), "Sudah ma 😊"),
		}},
		{ID: "gym", Name: "Gym Buddies", IsGroup: true, Muted: true, Unread: 27, Presence: "Kevin, Leo, Mike, You", Messages: []*Message{
			enc(), grp("Kevin", day(1, 21, 40), "Leg day tomorrow, no excuses 🦵"),
		}},
		{ID: "clara", Name: "Clara Wijaya", Presence: "last seen yesterday at 23:40", Messages: []*Message{
			enc(), {FromMe: true, Text: "Thanks for the review!", Time: day(1, 16, 3), Receipt: Read},
		}},
		{ID: "landlord", Name: "Pak Harto (Kos)", Presence: "last seen 2 days ago", Messages: []*Message{
			enc(), txt(false, day(2, 10, 0), "Mas, pembayaran bulan ini sudah saya terima. Terima kasih"),
		}},
		{ID: "dewi", Name: "Dewi Lestari", Presence: "online", Messages: []*Message{
			enc(), {FromMe: true, Text: "See you at the conference!", Time: day(3, 14, 22), Receipt: Delivered},
		}},
		{ID: "courier", Name: "+62 812-3456-7890", Presence: "", Messages: []*Message{
			enc(), txt(false, day(4, 11, 5), "Paket sudah di depan pintu ya kak"),
		}},
		{ID: "uni", Name: "Alumni TI 2019", IsGroup: true, Muted: true, Presence: "142 members", Messages: []*Message{
			enc(), grp("Fajar", day(5, 19, 0), "Reuni tahun ini di Bandung, yang mau ikut isi form ya"),
		}},
		{ID: "andre", Name: "Andre", Presence: "last seen recently", Messages: []*Message{
			enc(), {FromMe: true, Kind: KindImage, Text: "", Time: day(6, 12, 30), Receipt: Read, ImageA: 0xf7971e, ImageB: 0xffd200},
		}},
		{ID: "sari", Name: "Sari", Presence: "last seen recently", Messages: []*Message{
			enc(), txt(false, day(9, 17, 45), "Happy birthday!! 🎉🎂"), txt(true, day(9, 18, 0), "Thank you Sari!!"),
		}},
		{ID: "bank", Name: "Kevin Pratama", Presence: "last seen recently", Messages: []*Message{
			enc(), txt(false, day(15, 9, 0), "Ok noted"),
		}},
	}
	for _, c := range chats {
		for i, m := range c.Messages {
			if m.ID == "" {
				m.ID = fmt.Sprintf("%s-%d", c.ID, i)
			}
			if m.FromMe && m.Receipt == Pending {
				m.Receipt = Read
			}
		}
	}
	return chats
}
