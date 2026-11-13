package mock

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"slices"
	"strings"
	"time"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

// reacts counts emojis, one per person, for a demo message's Reactions:
// the most given first, ties in the order given.
func reacts(emojis ...string) []model.ReactionCount {
	var out []model.ReactionCount
	for _, e := range emojis {
		i := slices.IndexFunc(out, func(c model.ReactionCount) bool { return c.Emoji == e })
		if i < 0 {
			out = append(out, model.ReactionCount{Emoji: e})
			i = len(out) - 1
		}
		out[i].Count++
	}
	slices.SortStableFunc(out, func(a, b model.ReactionCount) int { return b.Count - a.Count })
	return out
}

// seedReactors gives the reactions the demo data counts to people: you
// for MyReaction, then the chat's person or the group's senders other than
// the message's.
func (b *Backend) seedReactors() {
	b.reactors = map[string][]model.Reactor{}
	for _, c := range b.chats {
		var people []model.Reactor
		if c.IsGroup {
			for _, m := range b.msgs[c.ID] {
				if m.SenderID != "" && !slices.ContainsFunc(people, func(r model.Reactor) bool { return r.ID == m.SenderID }) {
					people = append(people, model.Reactor{ID: m.SenderID, Name: m.Sender})
				}
			}
		} else {
			people = []model.Reactor{{ID: c.ID, Name: c.Name}}
		}
		for _, m := range b.msgs[c.ID] {
			if len(m.Reactions) == 0 {
				continue
			}
			others := slices.DeleteFunc(slices.Clone(people), func(r model.Reactor) bool { return r.ID == m.SenderID })
			var rs []model.Reactor
			mineLeft := m.MyReaction != ""
			n := 0
			for _, rc := range m.Reactions {
				for range rc.Count {
					at := m.Time.Add(time.Duration(len(rs)+1) * time.Minute)
					if mineLeft && rc.Emoji == m.MyReaction {
						mineLeft = false
						rs = append(rs, model.Reactor{ID: "me@lid", Name: "You", Me: true, Emoji: rc.Emoji, Time: at})
						continue
					}
					p := model.Reactor{ID: "someone", Name: "Someone"}
					if len(others) > 0 {
						p = others[n%len(others)]
						n++
					}
					p.Emoji, p.Time = rc.Emoji, at
					rs = append(rs, p)
				}
			}
			b.reactors[key(m)] = rs
		}
	}
}

// demoReactions reacts to Clara's release candidate in the Product Team
// with several emojis, yours among them.
func (b *Backend) demoReactions() {
	for _, m := range b.msgs["work"] {
		if strings.HasPrefix(m.Text, "Release candidate") {
			m.Reactions, m.MyReaction = reacts("👍", "❤️", "👍", "🎉"), "👍"
		}
	}
}

// recountReactions counts a message's reactions from its reactors.
func (b *Backend) recountReactions(m *model.Message) {
	rs := slices.Clone(b.reactors[key(m)])
	slices.SortStableFunc(rs, func(a, b model.Reactor) int { return a.Time.Compare(b.Time) })
	var emojis []string
	m.MyReaction = ""
	for _, r := range rs {
		emojis = append(emojis, r.Emoji)
		if r.Me {
			m.MyReaction = r.Emoji
		}
	}
	m.Reactions = reacts(emojis...)
}

func (b *Backend) React(m *model.Message, emoji string) {
	x := b.find(m)
	if x == nil {
		return
	}
	if b.reactors == nil {
		b.reactors = map[string][]model.Reactor{}
	}
	k := key(x)
	rs := slices.DeleteFunc(b.reactors[k], func(r model.Reactor) bool { return r.Me })
	if emoji != "" {
		rs = append(rs, model.Reactor{ID: "me@lid", Name: "You", Me: true, Emoji: emoji, Time: b.now()})
	}
	b.reactors[k] = rs
	b.update(x, b.recountReactions)
}

// Reactors lists you first, then the newest first.
func (b *Backend) Reactors(m *model.Message) []model.Reactor {
	rs := slices.Clone(b.reactors[key(m)])
	slices.SortStableFunc(rs, func(a, b model.Reactor) int {
		if a.Me != b.Me {
			if a.Me {
				return -1
			}
			return 1
		}
		return b.Time.Compare(a.Time)
	})
	return rs
}

// ReactSticker gives m your sticker reaction, or (nil) takes it back.
func (b *Backend) ReactSticker(m *model.Message, sticker *model.Message) {
	x := b.find(m)
	if x == nil {
		return
	}
	rs := slices.DeleteFunc(slices.Clone(x.Stickers), func(r model.StickerReaction) bool { return r.Me })
	if sticker != nil {
		b.stickerReacts++
		r := model.StickerReaction{ID: fmt.Sprintf("%s-react-%d", x.ID, b.stickerReacts), SenderID: "me@lid", Sender: "You", Me: true,
			Lottie: sticker.Lottie, Time: b.now()}
		b.mu.Lock()
		if data := b.media[key(sticker)]; data != nil {
			b.media[x.ChatID+"/"+r.ID] = data
		}
		b.mu.Unlock()
		rs = append(rs, r)
	}
	b.update(x, func(x *model.Message) { x.Stickers = rs })
}

// DemoStickerReactions reacts with stickers to messages of the chat, for
// previews: one to the last message from you, three (yours last) to the
// last from someone else, and three to the first with emoji reactions.
// The demo has no sticker files, so the stickers are drawn here.
func (b *Backend) DemoStickerReactions(chatID string) {
	var in, out, emojis *model.Message
	for _, m := range b.msgs[chatID] {
		switch {
		case m.Kind == model.KindDeleted:
		case len(m.Reactions) > 0 && emojis == nil:
			emojis = m
		case m.FromMe:
			out = m
		default:
			in = m
		}
	}
	people := []model.StickerReaction{{SenderID: chatID, Sender: "Someone"}, {SenderID: "sari@lid", Sender: "Sari"},
		{SenderID: "me@lid", Sender: "You", Me: true}}
	if c := b.chat(chatID); c != nil && !c.IsGroup {
		people[0].Sender = c.Name
	} else if in != nil && in.SenderID != "" {
		people[0].SenderID, people[0].Sender = in.SenderID, in.Sender
	}
	react := func(x *model.Message, n int) {
		if x == nil {
			return
		}
		for i, r := range people[:n] {
			b.stickerReacts++
			r.ID, r.Time = fmt.Sprintf("%s-react-%d", x.ID, b.stickerReacts), b.now().Add(time.Duration(i)*time.Second-time.Minute)
			b.mu.Lock()
			if b.media == nil {
				b.media = map[string][]byte{}
			}
			b.media[x.ChatID+"/"+r.ID] = demoSticker(demoStickerColors[i], i > 0)
			b.mu.Unlock()
			b.update(x, func(x *model.Message) { x.Stickers = append(x.Stickers, r) })
		}
	}
	react(out, 1)
	react(in, 3)
	react(emojis, 3)
}

// demoStickerColors tell the demo's stickers apart.
var demoStickerColors = [...]color.NRGBA{{60, 200, 110, 255}, {240, 180, 50, 255}, {120, 130, 240, 255}}

// demoSticker draws a sticker as a PNG: a grinning face, cut out or
// (square) on a background that fills the picture.
func demoSticker(face color.NRGBA, square bool) []byte {
	const n = 128
	img := image.NewNRGBA(image.Rect(0, 0, n, n))
	disc := func(cx, cy, r float64, c color.NRGBA) {
		for y := 0; y < n; y++ {
			for x := 0; x < n; x++ {
				if dx, dy := float64(x)+0.5-cx, float64(y)+0.5-cy; dx*dx+dy*dy <= r*r {
					img.SetNRGBA(x, y, c)
				}
			}
		}
	}
	white, dark := color.NRGBA{255, 255, 255, 255}, color.NRGBA{40, 44, 52, 255}
	if square {
		// A picture sticker: it fills its square.
		draw.Draw(img, img.Bounds(), image.NewUniform(color.NRGBA{face.B, face.R / 2, face.G, 255}), image.Point{}, draw.Src)
	}
	disc(64, 64, 62, white) // the sticker's white edge
	disc(64, 64, 56, face)
	disc(64, 78, 30, dark) // an open grin: a disc cut by the face above it
	for y := 0; y < 76; y++ {
		for x := 0; x < n; x++ {
			if img.NRGBAAt(x, y) == dark {
				img.SetNRGBA(x, y, face)
			}
		}
	}
	for _, cx := range []float64{44, 84} {
		disc(cx, 48, 13, white)
		disc(cx+2, 50, 6, dark)
	}
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return buf.Bytes()
}
