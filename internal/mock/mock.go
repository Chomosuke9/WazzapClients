// Package mock is a Backend with fake chats, for the demo mode and screenshots.
package mock

import (
	"fmt"
	"slices"
	"sort"
	"strings"
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
	favs   map[string]bool // favourite stickers, by message
	acc    *model.Account  // see demoAccount
	// media holds the pictures of stickers sent with SendNewSticker, by
	// chat and message ID.
	media      map[string][]byte
	linkResets int // invite links reset, to make a new one
	// versions are the earlier texts of edited messages, by chat and ID.
	versions map[string][]model.Version
	// pics makes Avatar draw profile pictures (the demo has them, the
	// reference data doesn't); avatars caches them by ID.
	pics    bool
	avatars map[string][]byte
	// votes are the votes in polls and answers to events, by chat and
	// message ID, newest first.
	votes map[string][]model.Vote
	extras
}

// Clock is the time new demo backends read. They date their chats by it
// ("today at 09:02"), so tests pin it to a fixed day.
var Clock = time.Now

// New returns a demo backend with timestamps relative to the current time.
func New() *Backend {
	b := &Backend{msgs: make(map[string][]*model.Message), now: Clock, pics: true}
	for _, d := range demo(b.now()) {
		for i, m := range d.Messages {
			m.ID = fmt.Sprintf("%s-%d", d.ID, i)
			m.ChatID = d.ID
			if m.Sender != "" && m.SenderID == "" {
				m.SenderID = strings.ToLower(m.Sender) // so their picture and info match
			}
			if m.FromMe && m.Receipt == model.Pending {
				m.Receipt = model.Read
			}
		}
		c := model.Chat{
			ID: d.ID, Name: d.Name, IsGroup: d.IsGroup, Pinned: d.Pinned, Favorite: d.Favorite,
			Muted: d.Muted, Archived: d.Archived, Self: d.Self, Unread: d.Unread, Mentioned: d.Mentioned, Presence: d.Presence, Typing: d.Typing,
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
	b.demoVotes()
	sent := b.find(&model.Message{ChatID: "rina", ID: "rina-8"}).Time
	b.versions = map[string][]model.Version{"rina/rina-8": {
		{Text: "Let's go there on Saturday", Time: sent},
		{Text: "Let's go there on Saturday morning", Time: sent.Add(time.Minute)},
	}}
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

// SetTyping shows who as typing in chat, or stops it (for filming the
// typing bubble).
func (b *Backend) SetTyping(chat, who string, on bool) {
	b.emit(model.TypingEvent{ChatID: chat, Who: who, WhoID: who, Typing: on})
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

func (b *Backend) SearchMessages(chatID, query string, limit int) {
	key := model.SearchKey(query)
	var out []*model.Message
	m := b.msgs[chatID]
	for i := len(m) - 1; i >= 0 && len(out) < limit && key != ""; i-- {
		if m[i].Kind != model.KindDeleted && strings.Contains(model.SearchKey(m[i].Text), key) {
			out = append(out, m[i])
		}
	}
	b.emit(model.SearchEvent{ChatID: chatID, Query: query, Msgs: out})
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

// Open marks the chat read, unless ghost mode is on: like the real
// backend, it then leaves the chat unread for its next opening.
func (b *Backend) Open(chatID string) {
	if b.prefs[model.PrefGhost] == "on" {
		return
	}
	for _, c := range b.chats {
		if c.ID == chatID && c.Unread > 0 {
			c.Unread = 0
			cc := *c
			b.emit(model.ChatEvent{Chat: &cc})
		}
	}
}

func (b *Backend) MarkRead(chatIDs []string) {
	for _, id := range chatIDs {
		b.setChat(id, func(c *model.Chat) { c.Unread = 0 })
	}
}

func (b *Backend) MediaData(chatID, msgID string) []byte {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.media[chatID+"/"+msgID]
}
func (b *Backend) Logout() {
	// Back to linking, which the demo can't do.
	b.emit(model.ChatsEvent{})
	b.emit(model.ConnEvent{State: model.StateQR, QR: "demo"})
}
func (b *Backend) Retry() {}
func (b *Backend) Close() {}

type demoChat struct {
	ID, Name, Presence, Typing       string
	IsGroup, Pinned, Favorite, Muted bool
	Archived, Self                   bool
	Unread                           int
	Mentioned                        bool
	Messages                         []*model.Message
}

func demo(now time.Time) []*demoChat {
	day := func(d int, h, m int) time.Time {
		y, mo, dd := now.AddDate(0, 0, -d).Date()
		return time.Date(y, mo, dd, h, m, 0, 0, now.Location())
	}
	// The coming Sunday, for the family's lunch.
	toSunday := (7 - int(now.Weekday())) % 7
	if toSunday == 0 {
		toSunday = 7
	}
	sunday := day(-toSunday, 0, 0)
	txt := func(fromMe bool, t time.Time, s string) *model.Message {
		return &model.Message{FromMe: fromMe, Text: s, Time: t, Receipt: model.Read}
	}
	grp := func(sender string, t time.Time, s string) *model.Message {
		return &model.Message{Sender: sender, SenderID: strings.ToLower(sender), Text: s, Time: t}
	}

	rina := &demoChat{
		ID: "rina", Name: "Rina Kartika", Pinned: true, Favorite: true, Unread: 2,
		Presence: "online",
		Messages: []*model.Message{
			txt(false, day(1, 19, 2), "Hey! Are we still on for the weekend trip?"),
			txt(true, day(1, 19, 5), "Yes!! I already booked the villa in Ubud 🏡"),
			{FromMe: true, Text: "https://villakayu.example/ubud", Time: day(1, 19, 5), Receipt: model.Read,
				Link: &model.LinkPreview{URL: "https://villakayu.example/ubud", Title: "Villa Kayu · Ubud, Bali",
					Description: "A private pool villa among the rice fields, 10 minutes from Ubud center. 3 bedrooms, breakfast included."},
				ImageA: 0x56ab2f, ImageB: 0xa8e063},
			{FromMe: true, Text: "Check in Friday 2pm, check out Sunday noon", Time: day(1, 19, 5), Receipt: model.Read, Starred: true},
			txt(false, day(1, 19, 11), "Perfect. I'll bring the camera"),
			txt(false, day(1, 19, 11), "Should we rent a car or just use Grab the whole time?"),
			txt(true, day(1, 19, 20), "Rent a car I think, it's cheaper for 4 people and we can go to Tegallalang early in the morning before it gets crowded"),
			{FromMe: false, Kind: model.KindImage, Text: "Found this spot near the villa", Time: day(0, 8, 41), ImageA: 0x3a7bd5, ImageB: 0x00d2ff},
			{FromMe: true, Text: "Wow that looks amazing", Time: day(0, 8, 45), Receipt: model.Read, Reaction: "❤️",
				Quote: &model.Quote{Sender: "Rina Kartika", Text: "📷 Found this spot near the villa"}},
			// Edited twice (see demoVersions).
			{FromMe: true, Text: "Let's go there on Saturday morning, before it gets hot", Time: day(0, 8, 46),
				Receipt: model.Read, Edited: day(0, 8, 49)},
			txt(false, day(0, 9, 30), "Deal 😄"),
			txt(false, day(0, 9, 31), "Also can you send me the villa address? My mom keeps asking"),
		},
	}

	family := &demoChat{
		ID: "family", Name: "Extended Family", IsGroup: true, Pinned: true, Unread: 14, Muted: true, Mentioned: true,
		Presence: "Mom, Dad, Dimas, Sari, You",
		Messages: []*model.Message{
			grp("Mom", day(0, 6, 2), "Good morning everyone 🌞"),
			grp("Dad", day(0, 6, 15), "Morning. Don't forget lunch at home on Sunday"),
			grp("Dimas", day(0, 7, 1), "Got it, Dad 👍"),
			grp("Sari", day(0, 7, 3), "I'll bring the cake from that shop we went to yesterday"),
			// An album of five photos: a grid of four, the last one "+2".
			{Sender: "Sari", Kind: model.KindImage, Media: model.MediaImage, Time: day(0, 7, 4), Album: "family-album", ImageA: 0xf6d365, ImageB: 0xfda085},
			{Sender: "Sari", Kind: model.KindImage, Media: model.MediaImage, Time: day(0, 7, 4), Album: "family-album", ImageA: 0x84fab0, ImageB: 0x8fd3f4},
			{Sender: "Sari", Kind: model.KindImage, Media: model.MediaImage, Time: day(0, 7, 4), Album: "family-album", ImageA: 0xa18cd1, ImageB: 0xfbc2eb},
			{Sender: "Sari", Kind: model.KindImage, Media: model.MediaImage, Time: day(0, 7, 4), Album: "family-album", ImageA: 0xfccb90, ImageB: 0xd57eeb},
			{Sender: "Sari", Kind: model.KindImage, Media: model.MediaImage, Time: day(0, 7, 4), Album: "family-album", ImageA: 0x5ee7df, ImageB: 0xb490ca},
			txt(true, day(0, 7, 20), "I'll be a bit late, around noon"),
			grp("Mom", day(0, 9, 12), "Okay sweetie, drive safe"),
			{Sender: "Dad", Forwarded: true, Time: day(0, 9, 20),
				Text: "Reminder: family photo on Saturday at 4pm, wear something bright 📸"},
			// Answers in demoVotes.
			{Sender: "Mom", Media: model.MediaEventInvite, Text: "Sunday lunch at home", Time: day(0, 9, 25),
				Event: &model.EventInfo{Name: "Sunday lunch at home", Start: sunday.Add(12 * time.Hour), End: sunday.Add(15 * time.Hour),
					Description: "Opor ayam and the cake Sari is bringing. Come hungry!",
					Place:       &model.Location{Name: "Mom & Dad's house", Address: "Jl. Kenanga 7, Bandung", Lat: -6.9025, Lng: 107.6187}}},
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
			grp("Clara", day(0, 9, 30), "Release candidate is up on staging. ⁨\u2063@Me Myself⁩ can you check the release notes? ⁨@Bima⁩ too"),
			{Sender: "Bima", SenderID: "bima", Kind: model.KindSticker, Media: model.MediaSticker, Time: day(0, 9, 31),
				Quote: &model.Quote{Sender: "Clara", Text: "Release candidate is up on staging. ⁨\u2063@Me Myself⁩ can you check the release notes? ⁨@Bima⁩ too"}},
			grp("Bima", day(0, 9, 34), "Nice, I'll run the smoke tests"),
			{Sender: "Clara", SenderID: "clara", Media: model.MediaDocument, Text: "Release notes v2.4.pdf", Time: day(0, 9, 36), Pinned: true,
				FileName: "Release notes v2.4.pdf", FileSize: 1_284_000, FileType: "application/pdf", Pages: 3},
			{Sender: "Andre", Media: model.MediaVoice, Duration: 42, Time: day(0, 9, 40),
				Waveform: []byte{4, 9, 22, 41, 60, 52, 33, 70, 88, 64, 40, 21, 12, 30, 55, 79, 92, 71, 45, 28, 18, 36, 58,
					74, 61, 39, 24, 15, 9, 27, 49, 66, 83, 95, 77, 52, 31, 19, 12, 25, 46, 68, 57, 34, 22, 14, 29, 51, 73,
					86, 62, 38, 21, 11, 7, 18, 35, 53, 44, 26, 13, 8, 5, 3}},
			{FromMe: true, Media: model.MediaDocument, Text: "Here are the test results", Time: day(0, 9, 52),
				Receipt: model.Read, FileName: "smoke-tests.xlsx", FileSize: 48_200,
				FileType: "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"},
			{FromMe: true, Media: model.MediaVoice, Duration: 7, Time: day(0, 9, 53), Receipt: model.Delivered},
		},
	}

	chats := []*demoChat{
		rina, family, work,
		{ID: "budi", Name: "Budi Santoso", Presence: "last seen today at 08:12", Messages: []*model.Message{
			txt(false, day(0, 8, 2), "Bro, are we still on for futsal tonight?"), {FromMe: true, Text: "Yep, 8 o'clock", Time: day(0, 8, 10), Receipt: model.Read, Reaction: "👍"},
			// Deleted for everyone, kept by Keep deleted messages.
			{Text: "Bring 50k for the court, Andi says he's not paying again 🙄", Time: day(0, 8, 14), Revoked: day(0, 8, 15)},
			txt(false, day(0, 8, 15), "Sorry, wrong chat. See you at 8!"),
		}},
		{ID: "shop", Name: "Sunset Coffee", Presence: "Business account", Messages: []*model.Message{
			{Text: "*Order #4821 is ready* ☕\nYour iced latte is waiting at the counter.", Footer: "Sunset Coffee · 12 Braga St.",
				Time: day(0, 7, 55), Buttons: []model.Button{
					{Kind: model.ButtonReply, Label: "On my way", Value: "otw"},
					{Kind: model.ButtonCopy, Label: "Copy code", Value: "KS-4821"},
					{Kind: model.ButtonURL, Label: "Track order", Value: "https://example.com/track/4821"},
				}},
			{Kind: model.KindUnsupported, Time: day(0, 7, 56)},
			{Text: "Eh, `/setting` isn't a command I know 😅\n\n> Say it in plain words, like \"turn off greetings\"\n> or \"add a rule\".\n\n**Menu** today:\n- Iced latte\n- ~Croissant~ _sold out_\n1. Pick up at the counter\n2. Show code `KS-4821`\n\n【注文】ご来店ありがとうございます！ *太字* 谢谢\n\n```\nQuiz for you - 1/3\n```",
				Time: day(0, 7, 57)},
		}},
		{ID: "mom", Name: "Mom", Favorite: true, Presence: "online", Messages: []*model.Message{
			txt(false, day(1, 20, 1), "Have you eaten yet?"), txt(true, day(1, 20, 30), "Yes, Mom 😊"),
		}},
		{ID: "gym", Name: "Gym Buddies", IsGroup: true, Muted: true, Unread: 27, Presence: "Kevin, Leo, Mike, You", Messages: []*model.Message{
			grp("Kevin", day(1, 21, 40), "Leg day tomorrow, no excuses 🦵"),
			{Sender: "Leo", Kind: model.KindImage, Media: model.MediaVideo, Duration: 18, Text: "New PR on squats 💪",
				Time: day(1, 21, 52), ImageA: 0x232526, ImageB: 0x414345},
		}},
		{ID: "clara", Name: "Clara Wijaya", Presence: "last seen yesterday at 23:40", Messages: []*model.Message{
			{FromMe: true, Text: "Thanks for the review!", Time: day(1, 16, 3), Receipt: model.Read},
		}},
		{ID: "landlord", Name: "Mr. Harto (Landlord)", Presence: "last seen 2 days ago", Messages: []*model.Message{
			txt(false, day(2, 10, 0), "Hi, I've received this month's payment. Thank you"),
			txt(false, day(2, 10, 3), "If the sink leaks again, call him directly"),
			{Media: model.MediaContact, Text: "Pak Joko (Plumber)", Time: day(2, 10, 3), Contacts: []model.ContactCard{
				{Name: "Pak Joko (Plumber)", Phones: []model.ContactPhone{{Number: "+62 812-9876-5432", WAID: "6281298765432"}}}}},
		}},
		{ID: "dewi", Name: "Dewi Lestari", Presence: "online", Messages: []*model.Message{
			{FromMe: true, Text: "See you at the conference!", Time: day(3, 14, 22), Receipt: model.Read},
			{Media: model.MediaLocation, Text: "Bali Nusa Dua Convention Center", Time: day(3, 14, 40),
				Location: &model.Location{Name: "Bali Nusa Dua Convention Center", Address: "Kawasan Pariwisata Nusa Dua, Bali",
					Lat: -8.8008, Lng: 115.2310}},
			txt(false, day(3, 14, 41), "The registration desk is at the north entrance"),
		}},
		{ID: "courier", Name: "+62 812-3456-7890", Presence: "", Messages: []*model.Message{
			txt(false, day(4, 11, 5), "Your package is at the front door"),
		}},
		{ID: "uni", Name: "CS Alumni 2019", IsGroup: true, Muted: true, Presence: "142 members", Messages: []*model.Message{
			grp("Fajar", day(5, 19, 0), "This year's reunion is in Bandung, fill in the form if you want to come"),
			grp("Fajar", day(5, 19, 2), "Want to join the organizing committee? Join here: "+inviteLink(inviteReunion)),
			grp("Kevin", day(5, 19, 10), "Futsal every Thursday, join: "+inviteLink(inviteFutsal)),
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

		// Two communities (see demoExtras): CS Alumni Hub, where you're a
		// member and only its admins post announcements, and Product HQ,
		// which you run.
		{ID: "alumni-ann", Name: "Announcements", IsGroup: true, Muted: true, Unread: 2, Messages: []*model.Message{
			grp("Fajar", day(3, 10, 0), "Welcome to the CS Alumni Hub! News that matters to every group in the community lands here."),
			{Sender: "Fajar", Kind: model.KindImage, Media: model.MediaImage, Text: "Reunion venue shortlist: vote in the 2019 group",
				Time: day(1, 18, 30), ImageA: 0x1d976c, ImageB: 0x93f9b9, Reaction: "🎉"},
			grp("Fajar", day(0, 8, 30), "Reunion registration closes on the 30th. Don't forget to fill in the form!"),
		}},
		{ID: "jobs", Name: "Alumni Jobs Board", IsGroup: true, Muted: true, Unread: 5, Presence: "Fajar, Kevin, Sari, You", Messages: []*model.Message{
			grp("Kevin", day(1, 10, 15), "Backend engineer opening at my company, Go and Postgres. DM me if interested"),
			grp("Sari", day(1, 10, 40), "Is it remote?"),
			grp("Kevin", day(1, 10, 42), "Hybrid, two days in the office"),
			grp("Fajar", day(0, 7, 5), "Reminder: no recruiter spam please, only real openings"),
		}},
		{ID: "work-ann", Name: "Announcements", IsGroup: true, Messages: []*model.Message{
			{FromMe: true, Text: "Welcome to Product HQ! Every team's news goes here.", Time: day(8, 9, 0), Receipt: model.Read},
			{FromMe: true, Kind: model.KindImage, Media: model.MediaImage, Text: "Q4 roadmap is out. Details in the Product Team group",
				Time: day(2, 15, 10), Receipt: model.Read, ImageA: 0x667eea, ImageB: 0x764ba2},
			{Sender: "Andre", SenderID: "andre", Text: "Heads up: the staging freeze starts Friday at noon", Time: day(0, 9, 45)},
		}},
		{ID: "design", Name: "Design Crew", IsGroup: true, Unread: 1, Presence: "Clara, Dewi, You", Messages: []*model.Message{
			grp("Dewi", day(1, 15, 0), "Updated the onboarding mockups, take a look when you can"),
			grp("Clara", day(1, 15, 20), "Love the new empty states! Can we try a darker header too?"),
			txt(true, day(1, 15, 30), "Agree, I'll try both and share a comparison"),
			grp("Dewi", day(0, 9, 40), "Comparison is in the Figma file 🎨"),
			// Votes in demoVotes.
			{Sender: "Dewi", Media: model.MediaPoll, Text: "Which header should we ship?", Time: day(0, 9, 41),
				Poll: &model.PollState{Max: 1, Options: []model.PollOption{{Name: "Light header"}, {Name: "Dark header"},
					{Name: "Both, as a setting"}}}},
		}},
		{ID: "old-project", Name: "Old Project Group", IsGroup: true, Archived: true, Presence: "Andre, Bima, You", Messages: []*model.Message{
			grp("Andre", day(40, 11, 0), "Project wrapped up, thanks everyone!"),
		}},
		{ID: "me@lid", Name: "Me Myself", Self: true, Messages: []*model.Message{
			txt(true, day(2, 22, 15), "Buy oat milk, call the dentist, renew the passport"),
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
