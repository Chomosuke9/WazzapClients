package wa

import (
	"strings"
	"time"

	"github.com/polymorfa/hypermeow/proto/waE2E"
	"github.com/polymorfa/hypermeow/types"
	"google.golang.org/protobuf/proto"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

// EditText implements model.Backend. The stored text refers to people as
// "@<user>"; the composer shows "@Name".
func (b *Backend) EditText(m *model.Message) (string, []model.MentionRef) {
	ctx := b.ctx
	r, ok := b.store.message(ctx, m.ChatID, m.ID)
	if !ok {
		return stripMarks(m.Text), nil
	}
	text := r.Text
	sm := readMentions(r.mentions, r.Text)
	var refs []model.MentionRef
	if sm.all {
		refs = append(refs, model.MentionRef{Name: "all", ID: "@all"})
	}
	first := true // the first custom mention, which gets the people when unsure
	for _, g := range sm.groups {
		text = strings.ReplaceAll(text, "@"+g.jid, "@"+g.subject)
		ref := model.MentionRef{Name: g.subject, ID: "@custom", People: g.people, Unsure: g.unsure}
		switch {
		case !model.IsGroupNameJID(g.jid, m.ChatID):
			continue
		case strings.EqualFold(g.subject, "admin"):
			ref.ID = "@admin"
		case g.unsure && !first:
			// The people go to the first: the message doesn't say whose
			// they are, and all of them are notified anyway.
			ref.People = nil
		}
		if ref.ID == "@custom" {
			first = false
		}
		refs = append(refs, ref)
	}
	for _, s := range sm.people {
		j, err := types.ParseJID(s)
		if err != nil || j.User == "" || !strings.Contains(text, "@"+j.User) {
			continue
		}
		name := b.senderName(ctx, j, "")
		text = strings.ReplaceAll(text, "@"+j.User, "@"+name)
		refs = append(refs, model.MentionRef{Name: name, ID: j.ToNonAD().String()})
	}
	return text, refs
}

// stripMarks removes the isolate marks around resolved mentions.
func stripMarks(s string) string {
	return strings.NewReplacer("⁨", "", "⁩", "", string(model.MentionNotifies), "",
		string(model.MentionAdmins), "", string(model.MentionGroup), "").Replace(s)
}

// Edit implements model.Backend.
func (b *Backend) Edit(m *model.Message, d model.Draft) {
	cli := b.connected()
	jid, err := types.ParseJID(m.ChatID)
	if cli == nil || err != nil {
		b.emit(model.NoticeEvent{Text: "Couldn't edit the message."})
		return
	}
	ctx := b.ctx
	d.Reply = nil
	ci := b.draftContext(m.ChatID, d, &model.Message{ChatID: m.ChatID})
	var content *waE2E.Message
	switch m.Media {
	case model.MediaNone:
		content = &waE2E.Message{Conversation: proto.String(d.Text)}
		if ci != nil {
			content = &waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{
				Text: proto.String(d.Text), ContextInfo: ci}}
		}
	case model.MediaImage, model.MediaVideo, model.MediaGIF:
		// A caption goes with the media message it belongs to.
		media, blob, err := b.store.mediaBlob(ctx, m.ChatID, m.ID)
		if err != nil || len(blob) == 0 {
			b.emit(model.NoticeEvent{Text: "Couldn't edit the caption."})
			return
		}
		if media == model.MediaImage {
			var im waE2E.ImageMessage
			if proto.Unmarshal(blob, &im) != nil {
				return
			}
			im.Caption, im.ContextInfo = proto.String(d.Text), ci
			content = &waE2E.Message{ImageMessage: &im}
		} else {
			var vm waE2E.VideoMessage
			if proto.Unmarshal(blob, &vm) != nil {
				return
			}
			vm.Caption, vm.ContextInfo = proto.String(d.Text), ci
			content = &waE2E.Message{VideoMessage: &vm}
		}
	default:
		return
	}
	now := time.Now()
	if err := b.store.editText(ctx, m.ChatID, m.ID, d.Text, now, marshal(content)); err != nil {
		b.log.Errorf("store edit of %s: %v", m.ID, err)
	}
	b.emitMessage(m.ChatID, m.ID)
	b.emitChat(m.ChatID)
	msg := cli.BuildEdit(jid, m.ID, content)
	go func() {
		if _, err := b.sendMessage(ctx, cli, jid, msg); err != nil {
			b.log.Warnf("edit %s in %s: %v", m.ID, m.ChatID, err)
			b.emit(model.NoticeEvent{Text: "Couldn't edit the message."})
		}
	}()
}

// Versions implements model.Backend.
func (b *Backend) Versions(m *model.Message) []model.Version {
	ctx := b.ctx
	texts, mentions, times, err := b.store.versions(ctx, m.ChatID, m.ID)
	if err != nil {
		b.log.Warnf("versions of %s: %v", m.ID, err)
		return nil
	}
	out := make([]model.Version, len(texts))
	for i, t := range texts {
		if mentions[i] != "" {
			t = b.replaceMentions(ctx, m.ChatID, t, mentions[i])
		} else if strings.Contains(t, "@") {
			t = b.guessMentions(ctx, t)
		}
		out[i] = model.Version{Text: t, Time: times[i]}
	}
	return out
}
