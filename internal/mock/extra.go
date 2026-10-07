package mock

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

// Demo data for the non-chat parts of the UI: statuses, channels,
// communities and the info panel.
type extras struct {
	statuses    []*model.StatusThread
	channels    []*model.Channel
	suggested   []*model.Channel
	communities []*model.Community
	infos       map[string]*model.ChatInfo
	summaries   map[string]model.MediaSummaryEvent // canned info panel media
	posted      int                                // status updates posted, for their IDs
}

func (b *Backend) Statuses() []*model.StatusThread { return b.statuses }

func (b *Backend) ViewStatus(threadID, statusID string) {
	for _, t := range b.statuses {
		for _, u := range t.Updates {
			if t.ID == threadID && u.ID == statusID && !t.Mine {
				u.Viewed = true
			}
		}
	}
	b.emit(model.StatusEvent{})
}

// PostStatus adds the update to your own thread. A photo is its own
// preview.
func (b *Backend) PostStatus(p model.StatusPost) {
	b.posted++
	up := &model.StatusUpdate{ID: "posted-" + strconv.Itoa(b.posted), FromMe: true, Sender: "You", SenderID: "me", Text: p.Text, Background: p.Background, Time: b.now()}
	if f := p.File; f != nil {
		up.Media = f.Media
		if st, err := os.Stat(f.Path); f.Media == model.MediaImage && err == nil && st.Size() < 8<<20 {
			up.Thumb, _ = os.ReadFile(f.Path)
		}
	}
	var mine *model.StatusThread
	for _, t := range b.statuses {
		if p.GroupID == "" && t.Mine || p.GroupID != "" && t.Group && t.ID == p.GroupID {
			mine = t
		}
	}
	if mine == nil {
		mine = &model.StatusThread{ID: "me", Mine: true}
		if p.GroupID != "" {
			mine = &model.StatusThread{ID: p.GroupID, Name: "Group status", Group: true}
		}
		b.statuses = append([]*model.StatusThread{mine}, b.statuses...)
	}
	mine.Updates = append(mine.Updates, up)
	b.emit(model.StatusEvent{})
}

// StatusPrivacy is the default: all your contacts.
func (b *Backend) StatusPrivacy() *model.StatusPrivacy { return &model.StatusPrivacy{} }

func (b *Backend) Channels() []*model.Channel { return b.channels }

func (b *Backend) FollowChannel(id string) {
	for i, ch := range b.suggested {
		if ch.ID == id {
			ch.Following = true
			ch.Time = b.now()
			b.channels = append([]*model.Channel{ch}, b.channels...)
			b.suggested = append(b.suggested[:i:i], b.suggested[i+1:]...)
			b.emit(model.ChannelsEvent{})
			return
		}
	}
}

func (b *Backend) SuggestedChannels() []*model.Channel { return b.suggested }
func (b *Backend) Communities() []*model.Community     { return b.communities }

// Info returns canned details, or builds them from the chat's messages.
func (b *Backend) Info(chatID string) *model.ChatInfo {
	if info, ok := b.infos[chatID]; ok {
		return info
	}
	var chat *model.Chat
	for _, c := range b.chats {
		if c.ID == chatID {
			chat = c
		}
	}
	if chat == nil {
		// A group member you have no chat with.
		name := ""
		for _, ms := range b.msgs {
			for _, m := range ms {
				if m.SenderID == chatID {
					name = m.Sender
				}
			}
		}
		if name == "" {
			return nil
		}
		chat = &model.Chat{ID: chatID, Name: name}
	}
	info := &model.ChatInfo{ID: chatID, Name: chat.Name, IsGroup: chat.IsGroup}
	if !chat.IsGroup {
		info.Phone = "+62 812-5550-" + itoa4(len(chat.Name)*37)
		info.About = "Hey there! I am using WhatsApp."
		info.Common = b.commonGroups(chatID)
		return info
	}
	info.Members = []model.Member{{ID: "me", Name: "You", Admin: true, Me: true}}
	seen := map[string]bool{}
	for _, m := range b.msgs[chatID] {
		if m.Sender != "" && !seen[m.Sender] {
			seen[m.Sender] = true
			id := m.SenderID
			if id == "" {
				id = strings.ToLower(m.Sender) // demo messages may leave it out
			}
			mem := model.Member{ID: id, Name: m.Sender}
			if push, ok := strings.CutPrefix(m.Sender, "~"); ok {
				mem.Push = push // a hidden number, or Name would be it
			} else {
				mem.Contact, mem.Phone = m.Sender, "+62 812-5550-"+itoa4(len(m.Sender)*37)
			}
			info.Members = append(info.Members, mem)
		}
	}
	return info
}

// MediaSummary returns canned media, or counts the chat's pictures.
func (b *Backend) MediaSummary(chatID string) {
	if ev, ok := b.summaries[chatID]; ok {
		b.emit(ev)
		return
	}
	ev := model.MediaSummaryEvent{ChatID: chatID}
	for _, m := range b.msgs[chatID] {
		if m.Kind == model.KindImage {
			ev.Count++
			ev.Media = append(ev.Media, m)
		}
	}
	b.emit(ev)
}

// commonGroups lists the groups where id has sent a message.
func (b *Backend) commonGroups(id string) []model.CommonGroup {
	var out []model.CommonGroup
	for _, c := range b.chats {
		if !c.IsGroup {
			continue
		}
		for _, m := range b.msgs[c.ID] {
			if m.SenderID == id {
				g := model.CommonGroup{ID: c.ID, Name: c.Name, Members: c.Presence}
				for _, cm := range b.communities {
					if c.ID == cm.Announcements || slices.Contains(cm.Groups, c.ID) {
						g.Community, g.CommunityID = cm.Name, cm.ID
					}
				}
				out = append(out, g)
				break
			}
		}
	}
	return out
}

func itoa4(n int) string {
	b := []byte{'0', '0', '0', '0'}
	for i := 3; i >= 0; i-- {
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b)
}

// thumb makes a small JPEG gradient standing in for a photo preview.
func thumb(a, b uint32) []byte {
	const n = 48
	img := image.NewRGBA(image.Rect(0, 0, n, n))
	ca, cb := rgb(a), rgb(b)
	for y := 0; y < n; y++ {
		for x := 0; x < n; x++ {
			t := float32(x+y) / (2 * n)
			img.Set(x, y, color.RGBA{
				R: uint8(float32(ca.R)*(1-t) + float32(cb.R)*t),
				G: uint8(float32(ca.G)*(1-t) + float32(cb.G)*t),
				B: uint8(float32(ca.B)*(1-t) + float32(cb.B)*t),
				A: 0xff,
			})
		}
	}
	var buf bytes.Buffer
	_ = jpeg.Encode(&buf, img, &jpeg.Options{Quality: 80})
	return buf.Bytes()
}

func rgb(c uint32) color.RGBA {
	return color.RGBA{R: uint8(c >> 16), G: uint8(c >> 8), B: uint8(c), A: 0xff}
}

func status(id string, t time.Time, viewed bool, a, b uint32) *model.StatusUpdate {
	return &model.StatusUpdate{ID: id, Media: model.MediaImage, Time: t, Viewed: viewed, Thumb: thumb(a, b)}
}

func textStatus(id string, t time.Time, viewed bool, text string, bg uint32) *model.StatusUpdate {
	return &model.StatusUpdate{ID: id, Text: text, Time: t, Viewed: viewed, Background: bg}
}

// demoExtras fills statuses, channels and communities for the demo account.
func demoExtras(at func(daysAgo, h, m int) time.Time) extras {
	return extras{
		statuses: []*model.StatusThread{
			{ID: "me", Mine: true, Updates: []*model.StatusUpdate{status("s-me", at(0, 7, 12), false, 0x2c3e50, 0x4ca1af)}},
			{ID: "rina", Name: "Rina Kartika", Updates: []*model.StatusUpdate{
				status("s-r1", at(0, 9, 2), false, 0xee9ca7, 0xffdde1), status("s-r2", at(0, 9, 5), false, 0x3a7bd5, 0x00d2ff)}},
			{ID: "budi", Name: "Budi Santoso", Updates: []*model.StatusUpdate{
				textStatus("s-b1", at(0, 6, 40), false, "Futsal tonight? ⚽", 0xff8a8c54)}},
			{ID: "dewi", Name: "Dewi Lestari", Updates: []*model.StatusUpdate{
				status("s-d1", at(1, 21, 15), true, 0x134e5e, 0x71b280)}},
		},
		channels: []*model.Channel{
			{ID: "techdaily@newsletter", Name: "Tech Daily", Verified: true, Followers: 1200000, Following: true, Unread: 3, Time: at(0, 8, 0),
				Last: &model.Message{Text: "The biggest launches of the week, in one thread 🧵", Time: at(0, 8, 0)}},
			{ID: "resep@newsletter", Name: "Archipelago Recipes", Followers: 82000, Following: true, Time: at(1, 17, 30),
				Last: &model.Message{Media: model.MediaImage, Kind: model.KindImage, Text: "Authentic Padang rendang, step by step", Time: at(1, 17, 30)}},
		},
		suggested: []*model.Channel{
			{ID: "whatsapp@newsletter", Name: "WhatsApp", Verified: true, Followers: 220000000},
			{ID: "bola@newsletter", Name: "National Football", Followers: 540000},
			{ID: "gempa@newsletter", Name: "Earthquake Alerts", Verified: true, Followers: 3100000},
		},
		communities: []*model.Community{
			{ID: "alumni-hub@g.us", Name: "CS Alumni Hub", Announcements: "alumni-ann", Groups: []string{"uni", "jobs"}},
			{ID: "product-hub@g.us", Name: "Product HQ", Announcements: "work-ann", Groups: []string{"work", "design"}},
		},
		summaries: map[string]model.MediaSummaryEvent{
			"shop": {ChatID: "shop", Count: 3, Media: []*model.Message{
				{ID: "s1", Kind: model.KindImage, Media: model.MediaImage, ImageA: 0x8d6e63, ImageB: 0xd7ccc8},
				{ID: "s2", Kind: model.KindImage, Media: model.MediaImage, ImageA: 0x5d4037, ImageB: 0xbcaaa4},
				{ID: "s3", Kind: model.KindImage, Media: model.MediaImage, ImageA: 0xefebe9, ImageB: 0xa1887f},
			}},
		},
		infos: map[string]*model.ChatInfo{
			// Only admins post announcements: Fajar runs the alumni hub, you run Product HQ.
			"alumni-ann": {
				ID: "alumni-ann", Name: "CS Alumni Hub", IsGroup: true, Announce: true, Locked: true,
				Members: []model.Member{
					{ID: "me@lid", Name: "You", Me: true},
					{ID: "fajar", Name: "Fajar", Contact: "Fajar", Phone: "+62 812-5550-0518", Admin: true},
				},
			},
			"work-ann": {
				ID: "work-ann", Name: "Product HQ", IsGroup: true, Announce: true, Locked: true,
				Members: []model.Member{
					{ID: "me@lid", Name: "You", Admin: true, Me: true},
					{ID: "andre", Name: "Andre", Contact: "Andre", Phone: "+62 812-5550-0222", Admin: true},
				},
			},
			"shop": {
				ID: "shop", Name: "Sunset Coffee", Phone: "+62 812-5550-0123", About: "Coffee, pastries and good mornings.",
				Business: &model.Business{
					Name: "Sunset Coffee", Category: "Cafe",
					Description: "Small-batch coffee roasted in Bandung. Order ahead and skip the line.",
					Address:     "12 Braga St., Bandung", Email: "hello@sunsetcoffee.example",
					Websites: []string{"https://sunsetcoffee.example"}, TimeZone: "Asia/Jakarta",
					Hours: []model.BusinessHours{
						{Day: time.Sunday, Mode: "specific_hours", Open: 8 * 60, Close: 18 * 60},
						{Day: time.Monday, Mode: "specific_hours", Open: 7 * 60, Close: 21 * 60},
						{Day: time.Tuesday, Mode: "specific_hours", Open: 7 * 60, Close: 21 * 60},
						{Day: time.Wednesday, Mode: "specific_hours", Open: 7 * 60, Close: 21 * 60},
						{Day: time.Thursday, Mode: "specific_hours", Open: 7 * 60, Close: 21 * 60},
						{Day: time.Friday, Mode: "specific_hours", Open: 7 * 60, Close: 22 * 60},
						{Day: time.Saturday, Mode: "specific_hours", Open: 8 * 60, Close: 22 * 60},
					},
				},
			},
		},
	}
}

// referenceExtras mirrors the reference account's Status, Channels and
// Communities screens and the "test" group's info panel.
func referenceExtras(at func(daysAgo, h, m int) time.Time) extras {
	date := func(y int, mo time.Month, d int) time.Time { return time.Date(y, mo, d, 12, 0, 0, 0, time.Local) }
	return extras{
		statuses: []*model.StatusThread{
			{ID: "me", Mine: true, Updates: []*model.StatusUpdate{status("me-1", at(1, 16, 55), false, 0x1f2a30, 0x8a8f7a)}},
			{ID: "zain@lid", Name: "Zain Attamim", Updates: []*model.StatusUpdate{
				status("z1", at(0, 6, 30), false, 0xf5f0d8, 0x2f8a6b), status("z2", at(0, 6, 40), false, 0xf0e6c0, 0x3f9a7b),
				status("z3", at(0, 6, 45), false, 0xf8f3e0, 0x2c7d5f)}},
			{ID: "ini@lid", Name: "inilidya°~~", Updates: []*model.StatusUpdate{status("i1", at(0, 6, 11), false, 0x5a5048, 0xcfc6bd)}},
			{ID: "bibit@lid", Name: "Bibit.id", Updates: []*model.StatusUpdate{
				status("b1", at(1, 10, 2), true, 0x0f3b2a, 0x1d6b48), status("b2", at(1, 10, 12), false, 0x0c3324, 0x2a7a55)}},
			{ID: "beolite@lid", Name: "Beolite", Updates: []*model.StatusUpdate{textStatus("be1", at(1, 23, 29), true, "✨", 0xffa8327f)}},
			{ID: "qoni@lid", Name: "Qoni", Updates: []*model.StatusUpdate{status("q1", at(1, 22, 9), true, 0x6f6a66, 0xe7e2dc)}},
		},
		channels: []*model.Channel{
			{ID: "alaura@newsletter", Name: "Alaura", Following: true, Time: at(1, 19, 0),
				Last: &model.Message{Media: model.MediaSticker, Kind: model.KindSticker, Time: at(1, 19, 0)}},
			{ID: "luar@newsletter", Name: "Luarkampus - Info Beasiswa Indonesia", Following: true, Unread: 2, Time: at(1, 18, 0),
				Last: &model.Message{Text: "🎓 ASEAN AUSTRALIA CENTRE SHORT COURSE 2026 – FULLY FUNDED", Time: at(1, 18, 0)}},
			{ID: "bibit@newsletter", Name: "Bibit.id", Following: true, Time: at(1, 10, 12),
				Last: &model.Message{Media: model.MediaImage, Kind: model.KindImage, Text: "Data yield Obligasi Negara FR & PBS di pasar sekunder", Time: at(1, 10, 12)}},
			{ID: "sains@newsletter", Name: "sains c", Following: true, Time: date(2026, 9, 5),
				Last: &model.Message{Media: model.MediaSticker, Kind: model.KindSticker, Time: date(2026, 9, 5)}},
			{ID: "wazzap@newsletter", Name: "WazzapAgents", Following: true, Time: date(2026, 3, 17),
				Last: &model.Message{FromMe: true, Text: `You created this channel, "WazzapAgents"`, Time: date(2026, 3, 17)}},
		},
		suggested: []*model.Channel{
			{ID: "kabar@newsletter", Name: "KabarBursa.com", Followers: 17000},
			{ID: "stockbit@newsletter", Name: "Stockbit", Verified: true, Followers: 736000},
			{ID: "jepang@newsletter", Name: "Belajar Bahasa Jepang 🇯🇵", Followers: 1400000},
			{ID: "bri@newsletter", Name: "BRI Danareksa Sekuritas", Followers: 31000},
			{ID: "bmkg@newsletter", Name: "BMKG", Verified: true, Followers: 7000000},
		},
		communities: []*model.Community{
			{ID: "fpam@g.us", Name: "Forum Penghitaman Anime Massal", Announcements: "fpam-ann@g.us",
				Groups: []string{"pam@g.us", "melers@g.us", "fpam-3@g.us"}},
			{ID: "wa@g.us", Name: "WazzapAgents", Announcements: "wa-ann@g.us",
				Groups: []string{"chitchat@g.us", "support@g.us"}},
			{ID: "zytro-c@g.us", Name: "ZYTRO API - UPDATE", Announcements: "zytro-ann@g.us",
				Groups: []string{"zytro@g.us"}},
		},
		summaries: map[string]model.MediaSummaryEvent{
			"test@g.us": {ChatID: "test@g.us", Count: 276, Media: []*model.Message{
				{ID: "m1", Kind: model.KindImage, Media: model.MediaImage, ImageA: 0xd8d4cf, ImageB: 0x2b2b2b},
				{ID: "m2", Kind: model.KindImage, Media: model.MediaImage, ImageA: 0xf4f4f4, ImageB: 0xdadde3},
				{ID: "m3", Kind: model.KindImage, Media: model.MediaImage, ImageA: 0x1b2330, ImageB: 0x2d3a4d},
				{ID: "m4", Kind: model.KindImage, Media: model.MediaImage, ImageA: 0xf4f4f4, ImageB: 0xdadde3},
			}},
			"vivy@lid": {ChatID: "vivy@lid", Count: 133, Media: []*model.Message{
				{ID: "v1", Kind: model.KindImage, Media: model.MediaImage, ImageA: 0x1b2330, ImageB: 0x2d3a4d},
				{ID: "v2", Kind: model.KindImage, Media: model.MediaImage, ImageA: 0xf4e4dc, ImageB: 0x8a3a3a},
				{ID: "v3", Kind: model.KindImage, Media: model.MediaImage, ImageA: 0xf4e4dc, ImageB: 0x3a6a8a},
				{ID: "v4", Kind: model.KindImage, Media: model.MediaImage, ImageA: 0xf4f4f4, ImageB: 0xd88a9a},
			}},
		},
		infos: map[string]*model.ChatInfo{
			// Only admins post announcements: you run WazzapAgents, not the forum.
			"fpam-ann@g.us": {
				ID: "fpam-ann@g.us", Name: "Forum Penghitaman Anime Massal", IsGroup: true, Announce: true, Locked: true,
				Members: []model.Member{
					{ID: "me@lid", Name: "You", Me: true},
					{ID: "alip@lid", Name: "~AlipReall65", Push: "AlipReall65", Admin: true},
				},
			},
			"wa-ann@g.us": {
				ID: "wa-ann@g.us", Name: "WazzapAgents", IsGroup: true, Announce: true, Locked: true,
				Members: []model.Member{
					{ID: "me@lid", Name: "You", Admin: true, Me: true},
					{ID: "vivy@lid", Name: "Vivy", Admin: true},
				},
			},
			"test@g.us": {
				ID: "test@g.us", Name: "test", IsGroup: true,
				About: "This group is dedicated for testing WhatsApp bot, specifically WazzapAgents",
				Members: []model.Member{
					{ID: "me@lid", Name: "You", Admin: true, Me: true},
					{ID: "agus@lid", Name: "Agus Kebab", Admin: true},
					{ID: "vivy@lid", Name: "Vivy", Admin: true},
				},
				Created: time.Date(2024, 8, 2, 8, 28, 0, 0, time.Local), CreatedBy: "you",
			},
			"vivy@lid": {
				ID: "vivy@lid", Name: "Vivy", Phone: "+62 881-0261-81996",
				Business: &model.Business{
					Name: "Vivy", Category: "Other business",
					Description: "This bot is 100% free, feel free to use it however you like. Join the community below for information and updates.\n\nAlso for some reason, I decided to ignore all private chats incoming.",
					Email:       "vivy@example.com",
					Websites:    []string{"https://chat.whatsapp.com/ExampleInviteCode"},
					Hours: []model.BusinessHours{
						{Day: time.Sunday, Mode: "open_24h"}, {Day: time.Monday, Mode: "open_24h"},
						{Day: time.Tuesday, Mode: "open_24h"}, {Day: time.Wednesday, Mode: "open_24h"},
						{Day: time.Thursday, Mode: "open_24h"}, {Day: time.Friday, Mode: "open_24h"},
						{Day: time.Saturday, Mode: "open_24h"},
					},
				},
				Common: []model.CommonGroup{
					{ID: "puja@g.us", Name: "Puja Qiqi Ajaib", Members: "Agus, Andrian, inilidya°~~, Mei, Radofa, Satria, Vivy, You"},
					{ID: "chitchat@g.us", Name: "ChitChat", Community: "WazzapAgents", CommunityID: "wa@g.us", Members: "Andrian, Athar, Beolite, Fajri, Feet, Jantan, Kamil, Vivy, You"},
					{ID: "pam@g.us", Name: "PAM : Chat Bot Only", Community: "Forum Penghitaman Anime Massal", CommunityID: "fpam@g.us", Members: "Vivy, +62 877-5228-7085, +62 831-2861-8005, You"},
					{ID: "lab@g.us", Name: "Grup 5 Lab Komputer", Members: "Vivy, Wiliam, Yazid, +62 859-2103-5371, You"},
					{ID: "test@g.us", Name: "test", Members: "Agus, Vivy, You"},
				},
			},
		},
	}
}
