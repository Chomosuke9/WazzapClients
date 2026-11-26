package wa

import (
	"context"
	"slices"
	"strings"
	"time"

	"github.com/polymorfa/hypermeow/types"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

// Options added to a poll after it was sent, as WhatsApp's clients add
// them (WAWebProcessEncryptedPollAddOptionMsgs): a secretEncryptedMessage
// (POLL_ADD_OPTION) that holds a pollAddOptionMessage, encrypted with the
// poll's message secret under the "Poll Add Option" use case, or in
// history sync the pollAddOptionMessage itself. They're kept in
// wz_poll_options and join the poll's options when it's read. As on
// WhatsApp Web, only the poll's sender may add one unless the poll allows
// anyone to (allowAddOption), none is taken after the poll ended or past
// the most a poll has, and an option it already has isn't added twice.

// addedOption is an option added to poll p.target.
type addedOption struct {
	name, who string
	ts        int64 // unix milliseconds
}

func (s *msgStore) putPollOption(ctx context.Context, x execer, chat, id string, o addedOption) error {
	_, err := x.ExecContext(ctx, `INSERT INTO wz_poll_options (chat, id, name, who, ts) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (chat, id, name) DO NOTHING`, chat, id, o.name, o.who, o.ts)
	return err
}

// pollOptions returns the options added to poll id, the first added first.
func (s *msgStore) pollOptions(ctx context.Context, chat, id string) []addedOption {
	rows, err := s.db.QueryContext(ctx, `SELECT name, who, ts FROM wz_poll_options WHERE chat = ? AND id = ? ORDER BY ts, rowid`, chat, id)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []addedOption
	for rows.Next() {
		var o addedOption
		if rows.Scan(&o.name, &o.who, &o.ts) == nil {
			out = append(out, o)
		}
	}
	return out
}

// addedOptions puts the options added to poll m after its own.
func (b *Backend) addedOptions(ctx context.Context, m *model.Message) {
	p := m.Poll
	if p == nil {
		return
	}
	sender := meVoter
	if !m.FromMe {
		sender = first(m.SenderID, m.ChatID)
	}
	for _, o := range b.store.pollOptions(ctx, m.ChatID, m.ID) {
		switch {
		case len(p.Options) >= model.PollMaxOptions:
			return
		case !p.AllowAdd && o.who != sender:
		case !p.End.IsZero() && time.UnixMilli(o.ts).After(p.End):
		case slices.ContainsFunc(p.Options, func(x model.PollOption) bool { return x.Name == o.name }):
		default:
			who := "You"
			if o.who != meVoter {
				who = b.senderNameStr(ctx, o.who, "")
			}
			p.Options = append(p.Options, model.PollOption{Name: o.name, AddedBy: who})
		}
	}
}

// AddPollOption implements model.Backend.
func (b *Backend) AddPollOption(m *model.Message, option string) {
	option = strings.TrimSpace(option)
	cli := b.connected()
	if cli == nil || m.Poll == nil || option == "" {
		return
	}
	jid, err := types.ParseJID(m.ChatID)
	if err != nil {
		return
	}
	info := &types.MessageInfo{
		MessageSource: types.MessageSource{Chat: jid, Sender: b.senderOf(m), IsFromMe: m.FromMe,
			IsGroup: jid.Server == types.GroupServer},
		ID: m.ID,
	}
	msg, err := cli.EncryptPollAddOption(b.ctx, info, option)
	if err != nil {
		b.log.Warnf("add an option to %s in %s: %v", m.ID, m.ChatID, err)
		b.emit(model.NoticeEvent{Text: "Couldn't add the option."})
		return
	}
	if err := b.store.putPollOption(b.ctx, b.db, m.ChatID, m.ID, addedOption{name: option, who: meVoter, ts: b.now().UnixMilli()}); err != nil {
		b.log.Warnf("store option of %s in %s: %v", m.ID, m.ChatID, err)
	}
	b.emitMessage(m.ChatID, m.ID)
	go func() {
		if _, err := b.sendMessage(b.ctx, cli, jid, msg); err != nil {
			b.log.Warnf("send option of %s in %s: %v", m.ID, m.ChatID, err)
			b.emit(model.NoticeEvent{Text: "Couldn't send the option."})
		}
	}()
}

// trimOption is an added option's name as kept.
func trimOption(s string) string { return strings.TrimSpace(s) }
