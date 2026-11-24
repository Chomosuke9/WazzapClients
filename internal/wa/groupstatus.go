package wa

import (
	"crypto/rand"
	"errors"

	whatsmeow "github.com/polymorfa/hypermeow"
	waBinary "github.com/polymorfa/hypermeow/binary"
	"github.com/polymorfa/hypermeow/proto/waE2E"
	"github.com/polymorfa/hypermeow/types"
	"github.com/polymorfa/hypermeow/types/events"
	"google.golang.org/protobuf/proto"

	"github.com/chomosuke9/wazzapclients/internal/model"
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

// markGroupStatusContext marks a status message's own contextInfo as a group
// status of the right kind (IsGroupStatus and StatusSourceType), the way
// WhatsApp's clients do. A relayed status that sets only isGroupStatus and
// omits StatusSourceType is posted, but the phone won't recognise it fully and
// a delete-for-everyone from it doesn't reach the other members.
func markGroupStatusContext(inner *waE2E.Message) {
	ci := describe(inner).ctx
	if ci == nil {
		ci = &waE2E.ContextInfo{}
	}
	ci.IsGroupStatus = proto.Bool(true)
	switch {
	case inner.ExtendedTextMessage != nil:
		inner.ExtendedTextMessage.ContextInfo = ci
		ci.StatusSourceType = waE2E.ContextInfo_TEXT.Enum()
	case inner.ImageMessage != nil:
		inner.ImageMessage.ContextInfo = ci
		ci.StatusSourceType = waE2E.ContextInfo_IMAGE.Enum()
	case inner.VideoMessage != nil:
		inner.VideoMessage.ContextInfo = ci
		ci.StatusSourceType = waE2E.ContextInfo_VIDEO.Enum()
	case inner.AudioMessage != nil:
		inner.AudioMessage.ContextInfo = ci
		ci.StatusSourceType = waE2E.ContextInfo_AUDIO.Enum()
	}
}

// groupStatusMessage uses the public SendMessage extension point; the
// dependency and its module cache remain unchanged.
func groupStatusMessage(msg *waE2E.Message) (*waE2E.Message, whatsmeow.SendRequestExtra) {
	markGroupStatusContext(msg)
	secret := make([]byte, 32)
	rand.Read(secret)
	wrapped := &waE2E.Message{GroupStatusMessageV2: &waE2E.FutureProofMessage{Message: msg},
		MessageContextInfo: &waE2E.MessageContextInfo{MessageSecret: secret}}
	nodes := []waBinary.Node{{Tag: "meta", Attrs: waBinary.Attrs{"is_group_status": "true"}}}
	return wrapped, whatsmeow.SendRequestExtra{AdditionalNodes: &nodes}
}

// groupStatusRelay prepares a group status sent through SendPayload
// (relayMessage): like the app's own posts (groupStatusMessage), the server
// won't treat it as a group status without a message secret and the
// is_group_status meta node, so it adds them to the already-wrapped message.
// It reports whether msg is a groupStatusMessageV2 sent to a group.
func groupStatusRelay(msg *waE2E.Message, jid types.JID) (whatsmeow.SendRequestExtra, bool) {
	if jid.Server != types.GroupServer || msg.GetGroupStatusMessageV2() == nil {
		return whatsmeow.SendRequestExtra{}, false
	}
	// A status with nothing to show can't be stored (parseStatus drops it)
	// and so can't be deleted; don't broadcast it as a status at all. It
	// then goes out as an ordinary message, which is deletable as one.
	if c := describe(msg); c.text == "" && c.media == model.MediaNone {
		return whatsmeow.SendRequestExtra{}, false
	}
	// Set the inner contextInfo (IsGroupStatus and StatusSourceType) the same
	// as the app's own posts, or the status is malformed: the phone shows it
	// but can't delete it for everyone.
	markGroupStatusContext(msg.GetGroupStatusMessageV2().GetMessage())
	if msg.MessageContextInfo == nil {
		msg.MessageContextInfo = &waE2E.MessageContextInfo{}
	}
	if msg.MessageContextInfo.MessageSecret == nil {
		secret := make([]byte, 32)
		rand.Read(secret)
		msg.MessageContextInfo.MessageSecret = secret
	}
	nodes := []waBinary.Node{{Tag: "meta", Attrs: waBinary.Attrs{"is_group_status": "true"}}}
	return whatsmeow.SendRequestExtra{AdditionalNodes: &nodes}, true
}

// DeleteGroupStatus revokes a group status you posted (or, as a group admin,
// someone else's), named by its group and ID, and removes it locally. A
// group status is kept in wz_status, not among chat messages, so a command's
// { delete } (sock) routes here when its key names one.
func (b *Backend) DeleteGroupStatus(group, id string) error {
	cli := b.connected()
	if cli == nil {
		return errors.New("Connect to WhatsApp first.")
	}
	jid, err := types.ParseJID(group)
	if err != nil || jid.Server != types.GroupServer {
		return errors.New("That isn't a group.")
	}
	var fromMe bool
	var participant string
	if err := b.db.QueryRowContext(b.ctx, `SELECT from_me, participant FROM wz_status WHERE id = ? AND group_jid = ?`,
		id, group).Scan(&fromMe, &participant); err != nil {
		return errors.New("No such group status here.")
	}
	sender := types.EmptyJID
	if !fromMe {
		sender, _ = types.ParseJID(participant) // revoke someone else's as admin
	}
	// A bare REVOKE is acked as a chat revoke and leaves the status up. The
	// revoke must be wrapped in groupStatusMessageV2 (so the marker stays
	// inside the message, surviving a retry) and the stanza marked
	// is_group_status, as the post was; hypermeow's getEditAttribute unwraps
	// the wrapper to set edit 7 (your own) or 8 (as admin). The AdditionalNodes
	// meta is lost on retry, which the wrapper covers.
	revoke := &waE2E.Message{GroupStatusMessageV2: &waE2E.FutureProofMessage{
		Message: cli.BuildRevoke(jid, sender, id)}}
	nodes := []waBinary.Node{{Tag: "meta", Attrs: waBinary.Attrs{"is_group_status": "true", "origin": "group_status"}}}
	if _, err := b.sendMessage(b.ctx, cli, jid, revoke,
		whatsmeow.SendRequestExtra{AdditionalNodes: &nodes}); err != nil {
		b.log.Warnf("delete group status %s in %s: %v", id, group, err)
		return errors.New("Couldn't delete the group status.")
	}
	if err := b.store.putStatus(b.ctx, b.db, storedStatus{revoke: id, fromMe: fromMe, ts: b.now()}); err != nil {
		b.log.Warnf("drop deleted group status %s: %v", id, err)
	}
	b.emit(model.StatusEvent{})
	return nil
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
