package wa

import (
	"time"

	"github.com/polymorfa/hypermeow/proto/waE2E"
	"github.com/polymorfa/hypermeow/types"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

// Zaps, which WhatsApp first called Instants, are photos and videos taken
// with its camera that open once, like view once messages, but come to
// linked devices with their media. A Zap is an instantImageMessage (an
// ImageMessage, field 135) or an instantVideoMessage (a VideoMessage,
// field 140, which hypermeow's protobufs don't know yet: it's read from
// the message's unknown fields). WhatsApp Web
// (WAWebInstantsParseInstantImageMessageProto, WAWebInstantsExpiry,
// WAWebInstantsConsumption) opens one until 24 hours after it was sent,
// and counts it opened once it was played: opening one here sends a
// played receipt. It's stored as a view once message (model.KindViewOnce)
// with Message.Zap set.

// instantVideoField is the Message field of an instantVideoMessage.
const instantVideoField protowire.Number = 140

// instantVideo returns m's instantVideoMessage, or nil.
func instantVideo(m *waE2E.Message) *waE2E.VideoMessage {
	if m == nil {
		return nil
	}
	b := m.ProtoReflect().GetUnknown()
	for len(b) > 0 {
		num, typ, n := protowire.ConsumeTag(b)
		if n < 0 {
			return nil
		}
		b = b[n:]
		if num == instantVideoField && typ == protowire.BytesType {
			v, n := protowire.ConsumeBytes(b)
			if n < 0 {
				return nil
			}
			var vm waE2E.VideoMessage
			if proto.Unmarshal(v, &vm) != nil {
				return nil
			}
			return &vm
		}
		if n = protowire.ConsumeFieldValue(num, typ, b); n < 0 {
			return nil
		}
		b = b[n:]
	}
	return nil
}

// setInstantVideo makes v msg's instantVideoMessage, in place of any it
// had. msg holds a copy: set it again after v changes.
func setInstantVideo(msg *waE2E.Message, v *waE2E.VideoMessage) {
	vb, err := proto.Marshal(v)
	if err != nil {
		return
	}
	var kept []byte
	b := msg.ProtoReflect().GetUnknown()
	for len(b) > 0 {
		num, typ, n := protowire.ConsumeTag(b)
		if n < 0 {
			break
		}
		m := protowire.ConsumeFieldValue(num, typ, b[n:])
		if m < 0 {
			break
		}
		if num != instantVideoField {
			kept = append(kept, b[:n+m]...)
		}
		b = b[n+m:]
	}
	kept = protowire.AppendTag(kept, instantVideoField, protowire.BytesType)
	msg.ProtoReflect().SetUnknown(protowire.AppendBytes(kept, vb))
}

// zapContent describes a Zap: m holds an instantImageMessage or an
// instantVideoMessage.
func zapContent(m *waE2E.Message) content {
	if e := m.GetInstantImageMessage(); e != nil {
		return content{text: e.GetCaption(), kind: model.KindViewOnce, media: model.MediaImage, zap: true,
			thumb: e.GetJPEGThumbnail(), inner: e, ctx: e.GetContextInfo(), file: fileInfo{Size: int64(e.GetFileLength())}}
	}
	e := instantVideo(m)
	return content{text: e.GetCaption(), kind: model.KindViewOnce, media: model.MediaVideo, zap: true,
		duration: int(e.GetSeconds()), thumb: e.GetJPEGThumbnail(), inner: e, ctx: e.GetContextInfo(),
		file: fileInfo{Size: int64(e.GetFileLength())}}
}

// openedElsewhere marks the view once messages and Zaps among ids, played
// on another of your devices, opened.
func (b *Backend) openedElsewhere(chat string, ids []types.MessageID) {
	for _, id := range ids {
		r, ok := b.store.message(b.ctx, chat, string(id))
		if !ok || r.Kind != model.KindViewOnce || r.Opened {
			continue
		}
		_ = b.store.setMessageFlag(b.ctx, chat, string(id), "opened", true)
		b.emitMessage(chat, string(id))
	}
}

// zapPlayed tells a Zap's sender that you opened it, as WhatsApp's
// clients do with a played receipt. Ghost mode sends none.
func (b *Backend) zapPlayed(m *model.Message) {
	if !m.Zap || m.FromMe || b.ghost() {
		return
	}
	cli := b.connected()
	if cli == nil {
		return
	}
	chat, err := types.ParseJID(m.ChatID)
	if err != nil {
		return
	}
	sender := types.EmptyJID
	if chat.Server == types.GroupServer {
		if sender, err = types.ParseJID(m.SenderID); err != nil {
			return
		}
	}
	go func() {
		if err := cli.MarkRead(b.ctx, []types.MessageID{m.ID}, time.Now(), chat, sender, types.ReceiptTypePlayed); err != nil {
			b.log.Warnf("played receipt of zap %s in %s: %v", m.ID, m.ChatID, err)
		}
	}()
}
