package wa

import (
	"context"

	whatsmeow "github.com/polymorfa/hypermeow"
	"github.com/polymorfa/hypermeow/proto/waCommon"
	"github.com/polymorfa/hypermeow/proto/waE2E"
	"github.com/polymorfa/hypermeow/types"
	"github.com/polymorfa/hypermeow/types/events"
	"google.golang.org/protobuf/proto"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

// A like of a status (the viewer's heart) is a reaction to it: a
// reactionMessage sent to status@broadcast whose key names the status
// and its poster, which goes to the poster alone (and your own devices),
// not to everyone your status privacy shares with. hypermeow sends to
// status@broadcast's whole audience, so the patch gives SendRequestExtra
// StatusRecipients (patches/hypermeow.patch). Reactions to statuses, yours
// from another device and others' to yours, are kept in wz_reactions
// under status@broadcast.

// statusLike is the emoji a like sends.
const statusLike = "❤️"

// LikeStatus implements model.Backend.
func (b *Backend) LikeStatus(statusID string, like bool) {
	ctx := b.ctx
	var author, participant string
	var fromMe bool
	err := b.db.QueryRowContext(ctx, `SELECT sender, participant, from_me FROM wz_status WHERE id = ? AND group_jid = ''`, statusID).
		Scan(&author, &participant, &fromMe)
	if err != nil || fromMe {
		return
	}
	poster, err := types.ParseJID(first(participant, author))
	if err != nil || poster.IsEmpty() {
		return
	}
	cli := b.connected()
	if cli == nil {
		b.emit(model.NoticeEvent{Text: "Couldn't send the like: you're offline."})
		return
	}
	emoji := ""
	if like {
		emoji = statusLike
	}
	ts := b.sendTime().UnixMilli()
	if err := b.store.putReaction(ctx, b.db, statusChat, statusID, reaction{who: meVoter, ts: ts, emoji: emoji}); err != nil {
		b.log.Warnf("store status like: %v", err)
	}
	b.emit(model.StatusEvent{})
	poster = poster.ToNonAD()
	msg := &waE2E.Message{ReactionMessage: &waE2E.ReactionMessage{
		Key: &waCommon.MessageKey{RemoteJID: proto.String(statusChat), FromMe: proto.Bool(false),
			ID: proto.String(statusID), Participant: proto.String(poster.String())},
		Text:              proto.String(emoji),
		SenderTimestampMS: proto.Int64(ts),
	}}
	go func() {
		_, err := cli.SendMessage(b.ctx, types.StatusBroadcastJID, msg, whatsmeow.SendRequestExtra{StatusRecipients: []types.JID{poster}})
		if err != nil {
			b.log.Warnf("like status %s: %v", statusID, err)
			b.emit(model.NoticeEvent{Text: "Couldn't send the like."})
		}
	}()
}

// onStatusReaction stores a reaction to a status: yours from another
// device, or someone's to one of yours.
func (b *Backend) onStatusReaction(e *events.Message, r *waE2E.ReactionMessage) {
	ctx := b.ctx
	id := r.GetKey().GetID()
	if id == "" {
		return
	}
	ts := r.GetSenderTimestampMS()
	if ts <= 0 {
		ts = e.Info.Timestamp.UnixMilli()
	}
	// Logged while what WhatsApp's own likes send is checked against ours.
	b.log.Infof("status reaction %q to %s from %s (key %s, from me %v)", r.GetText(), id, e.Info.Sender,
		r.GetKey().GetParticipant(), r.GetKey().GetFromMe())
	who := b.reactorOf(ctx, e.Info.Sender, e.Info.IsFromMe)
	if err := b.store.putReaction(ctx, b.db, statusChat, id, reaction{who: who, ts: ts, emoji: r.GetText()}); err != nil {
		b.log.Warnf("store status reaction: %v", err)
		return
	}
	b.emit(model.StatusEvent{})
}

// statusLikes returns the statuses you reacted to.
func (s *msgStore) statusLikes(ctx context.Context) map[string]bool {
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM wz_reactions WHERE chat = ? AND who = ? AND emoji != ''`, statusChat, meVoter)
	if err != nil {
		return nil
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil {
			out[id] = true
		}
	}
	return out
}
