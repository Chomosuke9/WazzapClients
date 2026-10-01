package wa

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/polymorfa/hypermeow/types"
	"github.com/polymorfa/hypermeow/types/events"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

// SetBlocked implements model.Backend.
func (b *Backend) SetBlocked(chatID string, blocked bool) {
	cli := b.connected()
	jid, err := types.ParseJID(chatID)
	if cli == nil || err != nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(b.ctx, 30*time.Second)
		defer cancel()
		action, verb := events.BlocklistChangeActionBlock, "block"
		if !blocked {
			action, verb = events.BlocklistChangeActionUnblock, "unblock"
		}
		if _, err := cli.UpdateBlocklist(ctx, jid, action); err != nil {
			b.log.Warnf("%s %s: %v", verb, chatID, err)
			b.emit(model.NoticeEvent{Text: "Couldn't " + verb + " the contact."})
			return
		}
		key := "info:" + chatID
		if raw := b.store.meta(ctx, key); raw != "" {
			var info model.ChatInfo
			if json.Unmarshal([]byte(raw), &info) == nil {
				info.Blocked = blocked
				out, _ := json.Marshal(&info)
				_ = b.store.setMetaValue(ctx, key, string(out))
			}
		}
		b.emit(model.InfoEvent{ChatID: chatID})
		name := b.chatName(ctx, jid)
		if blocked {
			b.emit(model.NoticeEvent{Text: name + " blocked"})
		} else {
			b.emit(model.NoticeEvent{Text: name + " unblocked"})
		}
	}()
}

// ExportChat implements model.Backend. The text follows WhatsApp's export
// format: "dd/mm/yyyy, HH:MM - Name: text", with media left out.
func (b *Backend) ExportChat(chatID string) {
	go func() {
		ctx := b.ctx
		jid, err := types.ParseJID(chatID)
		if err != nil {
			return
		}
		name := b.chatName(ctx, jid)
		if jid.Server == types.GroupServer {
			if rc, ok := b.store.chat(ctx, chatID); ok && rc.Name != "" {
				name = rc.Name
			}
		}
		me := "You"
		if cli := b.client(); cli != nil && cli.Store.PushName != "" {
			me = cli.Store.PushName
		}
		// Read the chat newest first, a page at a time, then write it out
		// oldest first.
		const page = 500
		var pages [][]*model.Message
		msgs := b.Messages(chatID, page)
		for len(msgs) > 0 {
			pages = append(pages, msgs)
			if len(msgs) < page {
				break
			}
			msgs = b.MessagesBefore(chatID, msgs[0].ID, page)
		}
		var sb strings.Builder
		for i := len(pages) - 1; i >= 0; i-- {
			for _, m := range pages[i] {
				who := m.Sender
				switch {
				case m.FromMe:
					who = me
				case who == "":
					who = name
				}
				sb.WriteString(m.Time.Format("02/01/2006, 15:04") + " - " + who + ": " + exportText(m) + "\n")
			}
		}
		if sb.Len() == 0 {
			b.emit(model.NoticeEvent{Text: "This chat has no messages to export."})
			return
		}
		path, err := saveDownload("WhatsApp Chat with "+name+".txt", []byte(sb.String()))
		if err != nil {
			b.log.Warnf("export %s: %v", chatID, err)
			b.emit(model.NoticeEvent{Text: "Couldn't export the chat."})
			return
		}
		b.emit(model.NoticeEvent{Text: "Chat exported to " + path})
	}()
}

// exportText is a message's line in an exported chat.
func exportText(m *model.Message) string {
	if m.Kind == model.KindDeleted {
		if m.FromMe {
			return "You deleted this message"
		}
		return "This message was deleted"
	}
	t := strings.Map(func(r rune) rune {
		switch r {
		case '⁨', '⁩', model.MentionNotifies, model.MentionAdmins:
			return -1 // mention marks
		}
		return r
	}, m.Text)
	if m.Media != model.MediaNone {
		if t == "" {
			return "<Media omitted>"
		}
		return "<Media omitted>\n" + t
	}
	return t
}
