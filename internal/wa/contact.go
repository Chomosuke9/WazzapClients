package wa

import (
	"bufio"
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
		list, err := cli.UpdateBlocklist(ctx, jid, action)
		if err != nil {
			b.log.Warnf("%s %s: %v", verb, chatID, err)
			b.emit(model.NoticeEvent{Text: "Couldn't " + verb + " the contact."})
			return
		}
		b.setBlocklist(ctx, list)
		key := "info:" + chatID
		info := model.ChatInfo{ID: chatID}
		if raw := b.store.meta(ctx, key); raw != "" {
			_ = json.Unmarshal([]byte(raw), &info)
		} else {
			// Nothing was cached yet: fetch the rest of the details too.
			b.infoMu.Lock()
			delete(b.infoFetched, chatID)
			b.infoMu.Unlock()
		}
		info.Blocked = blocked
		out, _ := json.Marshal(&info)
		_ = b.store.setMetaValue(ctx, key, string(out))
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
		first := b.store.oldestID(ctx, chatID)
		if first == "" {
			b.emit(model.NoticeEvent{Text: "This chat has no messages to export."})
			return
		}
		f, err := createDownload("WhatsApp Chat with " + name + ".txt")
		if err != nil {
			b.log.Warnf("export %s: %v", chatID, err)
			b.emit(model.NoticeEvent{Text: "Couldn't export the chat."})
			return
		}
		path := f.Name()
		// Read the chat oldest first, a page at a time, writing each page
		// out before the next, so a long chat never sits in memory whole.
		w := bufio.NewWriter(f)
		const page = 500
		for id := first; id != ""; {
			msgs := b.MessagesFrom(chatID, id, page+1)
			id = ""
			if len(msgs) > page {
				id = msgs[page].ID
				msgs = msgs[:page]
			}
			for _, m := range msgs {
				who := m.Sender
				switch {
				case m.FromMe:
					who = me
				case who == "":
					who = name
				}
				w.WriteString(m.Time.Format("02/01/2006, 15:04") + " - " + who + ": " + exportText(m) + "\n")
			}
		}
		err = w.Flush()
		if cerr := f.Close(); err == nil {
			err = cerr
		}
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
		return m.DeletedNote()
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
