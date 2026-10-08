package wa

import (
	"context"

	"github.com/polymorfa/hypermeow/proto/waE2E"
	"github.com/polymorfa/hypermeow/types"
	"github.com/polymorfa/hypermeow/types/events"
)

// A chat's disappearing-messages timer, kept in wz_chats.ephemeral (in
// seconds, 0 = off) so the chat list can mark it. It comes from the
// history sync's conversations, the messages that change it in a
// one-to-one chat, and a group's info (on connect, when the info panel
// fetches it, and when a member changes it).

// groupTimer is a group's timer in seconds, 0 when it's off.
func groupTimer(e types.GroupEphemeral) uint32 {
	if !e.IsEphemeral {
		return 0
	}
	return e.DisappearingTimer
}

// setTimer stores a chat's timer and, when it changed, sends the chat
// again.
func (b *Backend) setTimer(ctx context.Context, jid string, secs uint32) {
	if old, ok := b.store.timer(ctx, jid); ok && old == secs {
		return
	}
	if err := b.store.setField(ctx, jid, "ephemeral", int64(secs)); err != nil {
		b.log.Warnf("store timer of %s: %v", jid, err)
		return
	}
	b.emitChat(jid)
}

// noteTimer reads the timer from a message that sets it.
func (b *Backend) noteTimer(ctx context.Context, e *events.Message) {
	pm := unwrap(e.Message).GetProtocolMessage()
	if pm.GetType() != waE2E.ProtocolMessage_EPHEMERAL_SETTING {
		return
	}
	b.setTimer(ctx, b.canonical(ctx, e.Info.Chat).String(), pm.GetEphemeralExpiration())
}

// timer reads a chat's stored timer.
func (s *msgStore) timer(ctx context.Context, jid string) (uint32, bool) {
	var secs int64
	err := s.db.QueryRowContext(ctx, `SELECT ephemeral FROM wz_chats WHERE jid = ?`, jid).Scan(&secs)
	return uint32(secs), err == nil
}
