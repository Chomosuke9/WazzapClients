package mock

import (
	"sort"
	"strings"
	"time"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

// galleryHas reports whether m is one of a GalleryKind's messages.
func galleryHas(k model.GalleryKind, m *model.Message) bool {
	if m.Kind == model.KindDeleted {
		return false
	}
	switch k {
	case model.GalleryDocs:
		return m.Media == model.MediaDocument
	case model.GalleryLinks:
		t := m.Text
		return m.Kind != model.KindUnsupported &&
			(strings.Contains(t, "http://") || strings.Contains(t, "https://") || strings.Contains(t, "www."))
	case model.GalleryStarred:
		return m.Starred
	}
	if m.Kind == model.KindViewOnce {
		return false
	}
	// Demo pictures may leave Media out.
	return m.Kind == model.KindImage && m.Media == model.MediaNone ||
		m.Media == model.MediaImage || m.Media == model.MediaVideo || m.Media == model.MediaGIF
}

func (b *Backend) Gallery(q model.GalleryQuery) {
	key := model.SearchKey(q.Text)
	var all []*model.Message
	for chat, ms := range b.msgs {
		if q.ChatID != "" && chat != q.ChatID || strings.HasSuffix(chat, "@newsletter") {
			continue
		}
		for _, m := range ms {
			if galleryHas(q.Kind, m) && (key == "" || strings.Contains(model.SearchKey(m.Text+" "+m.FileName), key)) {
				all = append(all, m)
			}
		}
	}
	sort.SliceStable(all, func(i, j int) bool {
		if q.Oldest {
			return all[i].Time.Before(all[j].Time)
		}
		return all[i].Time.After(all[j].Time)
	})
	ev := model.GalleryEvent{Query: q}
	if q.Offset < len(all) {
		all = all[q.Offset:]
		ev.More = len(all) > q.Limit
		for _, m := range all[:min(len(all), q.Limit)] {
			ev.Msgs = append(ev.Msgs, b.copyOf(m))
		}
	}
	b.emit(ev)
}

func (b *Backend) SetDisappearing(chatID string, d time.Duration) {
	info := b.Info(chatID)
	if info == nil {
		return
	}
	if b.infos == nil {
		b.infos = map[string]*model.ChatInfo{}
	}
	info.Disappearing = uint32(d / time.Second)
	b.infos[chatID] = info
	b.setChat(chatID, func(c *model.Chat) { c.Disappearing = info.Disappearing })
	b.emit(model.InfoEvent{ChatID: chatID})
}

// SecurityCode makes up a code from the chat's ID.
func (b *Backend) SecurityCode(chatID string) {
	if strings.HasSuffix(chatID, "@g.us") {
		b.emit(model.SecurityCodeEvent{ChatID: chatID, Err: "Groups don't have a security code."})
		return
	}
	var code strings.Builder
	h := uint32(2166136261)
	for i := range 60 {
		h = (h ^ uint32(chatID[i%len(chatID)])) * 16777619
		code.WriteByte(byte('0' + h%10))
	}
	b.emit(model.SecurityCodeEvent{ChatID: chatID, Code: code.String()})
}

// MemberChanges makes a short history up for the demo groups.
func (b *Backend) MemberChanges(chatID string) []model.MemberChange {
	info := b.Info(chatID)
	if info == nil || !info.IsGroup {
		return nil
	}
	now := b.now()
	var out []model.MemberChange
	for i, m := range info.Members {
		if m.Me {
			continue
		}
		c := model.MemberChange{Time: now.Add(-time.Duration(i+1) * 26 * time.Hour), Name: m.Name, Action: model.MemberJoined}
		if i%2 == 1 {
			c.Action, c.By = model.MemberAdded, "You"
		}
		out = append(out, c)
		if m.Admin {
			out = append([]model.MemberChange{{Time: c.Time.Add(3 * time.Hour), Name: m.Name, By: "You", Action: model.MemberPromoted}}, out...)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Time.After(out[j].Time) })
	return out
}
