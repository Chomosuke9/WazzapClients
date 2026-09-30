package mock

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
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
		return nil
	}
	info := &model.ChatInfo{ID: chatID, Name: chat.Name, IsGroup: chat.IsGroup}
	if !chat.IsGroup {
		info.Phone = "+62 812-5550-" + itoa4(len(chat.Name)*37)
		info.About = "Hey there! I am using WhatsApp."
		return info
	}
	info.Members = []model.Member{{ID: "me", Name: "You", Admin: true, Me: true}}
	seen := map[string]bool{}
	for _, m := range b.msgs[chatID] {
		if m.Sender != "" && !seen[m.Sender] {
			seen[m.Sender] = true
			info.Members = append(info.Members, model.Member{ID: m.SenderID, Name: m.Sender})
		}
	}
	for _, m := range b.msgs[chatID] {
		if m.Kind == model.KindImage {
			info.MediaCount++
			info.Media = append(info.Media, m)
		}
	}
	return info
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
			{ID: "resep@newsletter", Name: "Resep Nusantara", Followers: 82000, Following: true, Time: at(1, 17, 30),
				Last: &model.Message{Media: model.MediaImage, Kind: model.KindImage, Text: "Rendang padang asli, langkah demi langkah", Time: at(1, 17, 30)}},
		},
		suggested: []*model.Channel{
			{ID: "whatsapp@newsletter", Name: "WhatsApp", Verified: true, Followers: 220000000},
			{ID: "bola@newsletter", Name: "Bola Nasional", Followers: 540000},
			{ID: "gempa@newsletter", Name: "Info Gempa", Verified: true, Followers: 3100000},
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
		infos: map[string]*model.ChatInfo{
			"test@g.us": {
				ID: "test@g.us", Name: "test", IsGroup: true,
				About: "This group is dedicated for testing WhatsApp bot, specifically WazzapAgents",
				Members: []model.Member{
					{ID: "me@lid", Name: "You", Admin: true, Me: true},
					{ID: "agus@lid", Name: "Agus Kebab", Admin: true},
					{ID: "vivy@lid", Name: "Vivy", Admin: true},
				},
				Created: time.Date(2024, 8, 2, 8, 28, 0, 0, time.Local), CreatedBy: "you",
				MediaCount: 276,
				Media: []*model.Message{
					{ID: "m1", Kind: model.KindImage, Media: model.MediaImage, ImageA: 0xd8d4cf, ImageB: 0x2b2b2b},
					{ID: "m2", Kind: model.KindImage, Media: model.MediaImage, ImageA: 0xf4f4f4, ImageB: 0xdadde3},
					{ID: "m3", Kind: model.KindImage, Media: model.MediaImage, ImageA: 0x1b2330, ImageB: 0x2d3a4d},
					{ID: "m4", Kind: model.KindImage, Media: model.MediaImage, ImageA: 0xf4f4f4, ImageB: 0xdadde3},
				},
			},
		},
	}
}
