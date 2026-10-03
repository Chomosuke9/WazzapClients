package wa

import (
	"crypto/rand"

	whatsmeow "github.com/polymorfa/hypermeow"
	waBinary "github.com/polymorfa/hypermeow/binary"
	"github.com/polymorfa/hypermeow/proto/waE2E"
	"github.com/polymorfa/hypermeow/types"
	"github.com/polymorfa/hypermeow/types/events"
	"google.golang.org/protobuf/proto"
)

// Group stories are delivered to the group itself, never status@broadcast.
// Check the envelope before describe unwraps it. Ordinary group media must
// remain ordinary messages, including status mentions and quoted stories.
func isGroupStatus(e *events.Message) bool {
	if e.Info.Chat.Server != types.GroupServer {
		return false
	}
	m := e.Message
	for i := 0; i < 5 && m != nil; i++ {
		if m.GetGroupStatusMessageV2() != nil {
			return true
		}
		m = unwrapOnce(m)
	}
	return false
}

// groupStatusMessage uses the public SendMessage extension point; the
// dependency and its module cache remain unchanged.
func groupStatusMessage(msg *waE2E.Message) (*waE2E.Message, whatsmeow.SendRequestExtra) {
	ci := describe(msg).ctx
	if ci == nil {
		ci = &waE2E.ContextInfo{}
	}
	ci.IsGroupStatus = proto.Bool(true)
	switch {
	case msg.ExtendedTextMessage != nil:
		msg.ExtendedTextMessage.ContextInfo = ci
		ci.StatusSourceType = waE2E.ContextInfo_TEXT.Enum()
	case msg.ImageMessage != nil:
		msg.ImageMessage.ContextInfo = ci
		ci.StatusSourceType = waE2E.ContextInfo_IMAGE.Enum()
	case msg.VideoMessage != nil:
		msg.VideoMessage.ContextInfo = ci
		ci.StatusSourceType = waE2E.ContextInfo_VIDEO.Enum()
	case msg.AudioMessage != nil:
		msg.AudioMessage.ContextInfo = ci
		ci.StatusSourceType = waE2E.ContextInfo_AUDIO.Enum()
	}
	secret := make([]byte, 32)
	rand.Read(secret)
	wrapped := &waE2E.Message{GroupStatusMessageV2: &waE2E.FutureProofMessage{Message: msg},
		MessageContextInfo: &waE2E.MessageContextInfo{MessageSecret: secret}}
	nodes := []waBinary.Node{{Tag: "meta", Attrs: waBinary.Attrs{"is_group_status": "true"}}}
	return wrapped, whatsmeow.SendRequestExtra{AdditionalNodes: &nodes}
}

func (b *Backend) isGroupStatusRevoke(e *events.Message) bool {
	if e.Info.Chat.Server != types.GroupServer {
		return false
	}
	pm := unwrap(e.Message).GetProtocolMessage()
	if pm == nil || pm.GetType() != waE2E.ProtocolMessage_REVOKE {
		return false
	}
	var exists int
	err := b.db.QueryRowContext(b.ctx, `SELECT 1 FROM wz_status WHERE id = ? AND group_jid = ?`, pm.GetKey().GetID(), e.Info.Chat.String()).Scan(&exists)
	return err == nil
}
