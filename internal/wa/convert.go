package wa

import (
	"context"
	"time"

	"github.com/polymorfa/hypermeow/proto/waE2E"
	"github.com/polymorfa/hypermeow/proto/waWeb"
	"github.com/polymorfa/hypermeow/types"
	"github.com/polymorfa/hypermeow/types/events"
	"google.golang.org/protobuf/proto"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

// parsed is the result of interpreting one incoming message. Most messages
// become a new chat entry; reactions, deletions and edits modify an existing one.
type parsed struct {
	msg storedMsg

	target   string // message ID a reaction/revoke/edit/pin applies to
	reaction *reaction
	revoke   bool
	// revokedBy is the group admin who deleted someone else's message
	// (revoke), or "" when its sender did.
	revokedBy string
	pin       int       // 1 pinned, -1 unpinned
	vote      *vote     // a vote in a poll or an answer to an event
	event     *eventDef // an edit of an event

	// An edit's new text, and when it was made; its content is msg's
	// payload.
	edited   bool
	edit     string
	editTime time.Time
}

// content summarizes what a message shows.
type content struct {
	text     string
	kind     model.Kind
	media    model.Media
	duration int
	thumb    []byte
	inner    proto.Message // the media message, which downloading the media takes
	bg       uint32        // ARGB background of a text status
	ctx      *waE2E.ContextInfo
	buttons  *buttonsInfo // footer and buttons of business messages
	file     fileInfo     // documents and audio
	link     *model.LinkPreview
	linkPic  []byte // the link preview's JPEG
	extra    extraInfo
}

// keptThumb is the thumbnail a message with content c shows: a link
// preview's picture is its thumbnail.
func (c content) keptThumb() []byte {
	if c.link != nil {
		return c.linkPic
	}
	return c.thumb
}

// keepsBlob reports whether m, whose content c is, has a media blob: what
// downloading its media takes. A view once message that came without it
// has none (see Message.OnPhone).
func (c content) keepsBlob(m *waE2E.Message) bool {
	return c.inner != nil && !(isViewOnce(m) && !hasMediaKey(m))
}

// keptBlob returns the media blob of m, whose content c is, or nil. It is
// marshaled anew, so only what downloads asks for it.
func (c content) keptBlob(m *waE2E.Message) []byte {
	if !c.keepsBlob(m) {
		return nil
	}
	return marshal(c.inner)
}

func marshal(m proto.Message) []byte {
	b, _ := proto.Marshal(m)
	return b
}

// unwrap strips the envelopes a message can come in (disappearing,
// view once, edited...). Quoted messages keep them, unlike the message
// events hypermeow hands out.
func unwrap(m *waE2E.Message) *waE2E.Message {
	for i := 0; i < 4 && m != nil; i++ {
		inner := unwrapOnce(m)
		if inner == nil {
			return m
		}
		m = inner
	}
	return m
}

// unwrapOnce returns the message inside m's envelope, or nil when m has none.
func unwrapOnce(m *waE2E.Message) *waE2E.Message {
	switch {
	case m.GetDeviceSentMessage().GetMessage() != nil:
		return m.GetDeviceSentMessage().GetMessage()
	case m.GetEphemeralMessage().GetMessage() != nil:
		return m.GetEphemeralMessage().GetMessage()
	case m.GetViewOnceMessage().GetMessage() != nil:
		return m.GetViewOnceMessage().GetMessage()
	case m.GetViewOnceMessageV2().GetMessage() != nil:
		return m.GetViewOnceMessageV2().GetMessage()
	case m.GetViewOnceMessageV2Extension().GetMessage() != nil:
		return m.GetViewOnceMessageV2Extension().GetMessage()
	case m.GetDocumentWithCaptionMessage().GetMessage() != nil:
		return m.GetDocumentWithCaptionMessage().GetMessage()
	case m.GetEditedMessage().GetMessage() != nil:
		return m.GetEditedMessage().GetMessage()
	case m.GetBotInvokeMessage().GetMessage() != nil:
		return m.GetBotInvokeMessage().GetMessage()
	case m.GetLottieStickerMessage().GetMessage() != nil:
		return m.GetLottieStickerMessage().GetMessage()
	case m.GetAssociatedChildMessage().GetMessage() != nil:
		return m.GetAssociatedChildMessage().GetMessage()
	case m.GetGroupStatusMessageV2().GetMessage() != nil:
		return m.GetGroupStatusMessageV2().GetMessage()
	case m.GetGroupMentionedMessage().GetMessage() != nil:
		return m.GetGroupMentionedMessage().GetMessage()
	}
	return nil
}

// isViewOnce reports whether a message is view once: in one of the view
// once envelopes, or a photo, video or voice message flagged so.
func isViewOnce(m *waE2E.Message) bool {
	for i := 0; i < 5 && m != nil; i++ {
		if m.GetViewOnceMessage() != nil || m.GetViewOnceMessageV2() != nil || m.GetViewOnceMessageV2Extension() != nil ||
			m.GetImageMessage().GetViewOnce() || m.GetVideoMessage().GetViewOnce() || m.GetAudioMessage().GetViewOnce() {
			return true
		}
		m = unwrapOnce(m)
	}
	return false
}

// hasMediaKey reports whether a message carries what downloading its
// photo, video or audio takes. WhatsApp leaves the key out of view once
// messages it hands to linked devices.
func hasMediaKey(m *waE2E.Message) bool {
	m = unwrap(m)
	type media interface {
		GetMediaKey() []byte
		GetDirectPath() string
		GetURL() string
	}
	var e media
	switch {
	case m.GetImageMessage() != nil:
		e = m.GetImageMessage()
	case m.GetVideoMessage() != nil:
		e = m.GetVideoMessage()
	case m.GetAudioMessage() != nil:
		e = m.GetAudioMessage()
	default:
		return false
	}
	return len(e.GetMediaKey()) > 0 && (e.GetDirectPath() != "" || e.GetURL() != "")
}

// albumOf returns the ID of the album message a photo or video belongs
// to, or "". The pictures of an album arrive as messages of their own
// after it, each pointing back to it from its messageContextInfo, which
// can sit on any of the envelopes around the picture.
func albumOf(m *waE2E.Message) string {
	for i := 0; i < 5 && m != nil; i++ {
		a := m.GetMessageContextInfo().GetMessageAssociation()
		if a.GetAssociationType() == waE2E.MessageAssociation_MEDIA_ALBUM {
			if id := a.GetParentMessageKey().GetID(); id != "" {
				return id
			}
		}
		m = unwrapOnce(m)
	}
	return ""
}

func describe(m *waE2E.Message) content {
	m = unwrap(m)
	switch {
	case m.GetConversation() != "":
		return content{text: m.GetConversation()}
	case m.GetExtendedTextMessage() != nil:
		e := m.GetExtendedTextMessage()
		c := content{text: e.GetText(), bg: e.GetBackgroundArgb(), ctx: e.GetContextInfo()}
		if e.GetTitle() != "" || e.GetDescription() != "" {
			c.link = &model.LinkPreview{URL: e.GetMatchedText(), Title: e.GetTitle(),
				Description: e.GetDescription()}
			c.linkPic = e.GetJPEGThumbnail()
			// The big picture's size makes the card wide: when it can be
			// downloaded, or before your own is uploaded (it shows from
			// disk meanwhile).
			if c.inner = linkImageOf(e); c.inner != nil || e.GetThumbnailDirectPath() == "" {
				c.link.W, c.link.H = int(e.GetThumbnailWidth()), int(e.GetThumbnailHeight())
			}
		}
		return c
	case m.GetImageMessage() != nil:
		e := m.GetImageMessage()
		return content{text: e.GetCaption(), kind: model.KindImage, media: model.MediaImage,
			thumb: e.GetJPEGThumbnail(), inner: e, ctx: e.GetContextInfo()}
	case m.GetVideoMessage() != nil:
		e := m.GetVideoMessage()
		media := model.MediaVideo
		if e.GetGifPlayback() {
			media = model.MediaGIF
		}
		return content{text: e.GetCaption(), kind: model.KindImage, media: media, duration: int(e.GetSeconds()),
			thumb: e.GetJPEGThumbnail(), inner: e, ctx: e.GetContextInfo()}
	case m.GetPtvMessage() != nil:
		e := m.GetPtvMessage()
		return content{kind: model.KindImage, media: model.MediaVideo, duration: int(e.GetSeconds()),
			thumb: e.GetJPEGThumbnail(), inner: e, ctx: e.GetContextInfo()}
	case m.GetAudioMessage() != nil:
		e := m.GetAudioMessage()
		media := model.MediaAudio
		if e.GetPTT() {
			media = model.MediaVoice
		}
		return content{media: media, duration: int(e.GetSeconds()), inner: e, ctx: e.GetContextInfo(),
			file: audioInfo(e)}
	case m.GetDocumentMessage() != nil:
		e := m.GetDocumentMessage()
		f := documentInfo(e)
		return content{text: first(e.GetCaption(), f.Name), media: model.MediaDocument, inner: e,
			ctx: e.GetContextInfo(), file: f}
	case m.GetStickerMessage() != nil:
		e := m.GetStickerMessage()
		return content{kind: model.KindSticker, media: model.MediaSticker, thumb: e.GetPngThumbnail(),
			inner: e, ctx: e.GetContextInfo()}
	case m.GetContactMessage() != nil:
		e := m.GetContactMessage()
		return content{text: e.GetDisplayName(), media: model.MediaContact, ctx: e.GetContextInfo(),
			extra: extraInfo{Contacts: []model.ContactCard{parseVCard(e.GetVcard(), e.GetDisplayName())}}}
	case m.GetContactsArrayMessage() != nil:
		e := m.GetContactsArrayMessage()
		c := content{text: e.GetDisplayName(), media: model.MediaContact, ctx: e.GetContextInfo()}
		for _, cm := range e.GetContacts() {
			c.extra.Contacts = append(c.extra.Contacts, parseVCard(cm.GetVcard(), cm.GetDisplayName()))
		}
		return c
	case m.GetLocationMessage() != nil:
		e := m.GetLocationMessage()
		return content{text: e.GetName(), media: model.MediaLocation, thumb: e.GetJPEGThumbnail(), ctx: e.GetContextInfo(),
			extra: extraInfo{Loc: locationOf(e)}}
	case m.GetLiveLocationMessage() != nil:
		e := m.GetLiveLocationMessage()
		loc := &model.Location{Lat: e.GetDegreesLatitude(), Lng: e.GetDegreesLongitude(), Name: e.GetCaption(), Live: true}
		return content{text: "Live location", media: model.MediaLocation, thumb: e.GetJPEGThumbnail(), ctx: e.GetContextInfo(),
			extra: extraInfo{Loc: loc}}
	case m.GetPollCreationMessage() != nil:
		return pollContent(m.GetPollCreationMessage())
	case m.GetPollCreationMessageV2() != nil:
		return pollContent(m.GetPollCreationMessageV2())
	case m.GetPollCreationMessageV3() != nil:
		return pollContent(m.GetPollCreationMessageV3())
	case m.GetPollCreationMessageV5() != nil:
		return pollContent(m.GetPollCreationMessageV5())
	case m.GetPollCreationMessageV6() != nil:
		return pollContent(m.GetPollCreationMessageV6())
	case m.GetEventMessage() != nil:
		return eventContent(m.GetEventMessage())
	// Answers to bot buttons and lists quote the message they answer.
	case m.GetButtonsResponseMessage() != nil:
		e := m.GetButtonsResponseMessage()
		return content{text: e.GetSelectedDisplayText(), ctx: e.GetContextInfo()}
	case m.GetTemplateButtonReplyMessage() != nil:
		e := m.GetTemplateButtonReplyMessage()
		return content{text: e.GetSelectedDisplayText(), ctx: e.GetContextInfo()}
	case m.GetListResponseMessage() != nil:
		e := m.GetListResponseMessage()
		return content{text: first(e.GetTitle(), e.GetSingleSelectReply().GetSelectedRowID()), ctx: e.GetContextInfo()}
	case m.GetInteractiveResponseMessage() != nil:
		e := m.GetInteractiveResponseMessage()
		return content{text: e.GetBody().GetText(), ctx: e.GetContextInfo()}
	case m.GetButtonsMessage() != nil:
		return describeButtons(m.GetButtonsMessage())
	case m.GetTemplateMessage() != nil:
		return describeTemplate(m.GetTemplateMessage())
	case m.GetInteractiveMessage() != nil:
		return describeInteractive(m.GetInteractiveMessage())
	case m.GetListMessage() != nil:
		return describeList(m.GetListMessage())
	case m.GetGroupInviteMessage() != nil:
		e := m.GetGroupInviteMessage()
		return content{text: "Group invite: " + e.GetGroupName(), ctx: e.GetContextInfo()}
	}
	return content{}
}

func webReceipt(w *waWeb.WebMessageInfo) model.Receipt {
	switch w.GetStatus() {
	case waWeb.WebMessageInfo_READ, waWeb.WebMessageInfo_PLAYED:
		return model.Read
	case waWeb.WebMessageInfo_DELIVERY_ACK:
		return model.Delivered
	}
	return model.Sent
}

// parse interprets a message event. ok is false for messages with nothing to
// show (key distribution, unsupported types, and so on). It may query the
// device store, so never call it inside a msgStore transaction.
func (b *Backend) parse(ctx context.Context, evt *events.Message) (p parsed, ok bool) {
	m := evt.Message
	if m == nil {
		return p, false
	}
	chat := b.canonical(ctx, evt.Info.Chat)
	if skipChat(chat) {
		return p, false
	}
	p.msg.Message = &model.Message{ChatID: chat.String()}

	if r := m.GetReactionMessage(); r != nil {
		ts := r.GetSenderTimestampMS()
		if ts <= 0 {
			ts = evt.Info.Timestamp.UnixMilli()
		}
		p.target = r.GetKey().GetID()
		p.reaction = &reaction{who: b.reactorOf(ctx, evt.Info.Sender, evt.Info.IsFromMe), ts: ts, emoji: r.GetText()}
		return p, p.target != ""
	}
	if m.GetPollUpdateMessage() != nil || m.GetEncEventResponseMessage() != nil {
		return p, b.parseVote(ctx, evt, &p)
	}
	if pin := m.GetPinInChatMessage(); pin != nil {
		p.target, p.pin = pin.GetKey().GetID(), 1
		if pin.GetType() == waE2E.PinInChatMessage_UNPIN_FOR_ALL {
			p.pin = -1
		}
		return p, p.target != ""
	}
	if pm := m.GetProtocolMessage(); pm != nil {
		p.target = pm.GetKey().GetID()
		switch pm.GetType() {
		case waE2E.ProtocolMessage_REVOKE:
			p.revoke = true
			p.msg.FromMe, p.msg.Time = evt.Info.IsFromMe, evt.Info.Timestamp
			// An admin deleting someone else's message sends its key as
			// not theirs, and the stanza says edit="8". History sync
			// carries no stanza, only the key.
			admin := evt.Info.Edit == types.EditAttributeAdminRevoke ||
				evt.Info.Edit == "" && !pm.GetKey().GetFromMe()
			if evt.Info.IsGroup && admin && !evt.Info.Sender.IsEmpty() {
				p.revokedBy = evt.Info.Sender.ToNonAD().String()
			}
		case waE2E.ProtocolMessage_MESSAGE_EDIT:
			at := evt.Info.Timestamp
			if ms := pm.GetTimestampMS(); ms > 0 {
				at = time.UnixMilli(ms)
			}
			return p, p.setEdit(p.target, pm.GetEditedMessage(), at)
		case waE2E.ProtocolMessage_EPHEMERAL_SETTING:
			p.target = ""
			p.msg, ok = timerSystem(evt, chat, pm)
			return p, ok
		default:
			return p, false
		}
		return p, p.target != ""
	}
	if sm := m.GetSecretEncryptedMessage(); sm != nil {
		// Newer WhatsApp versions send edits encrypted with the edited
		// message's secret.
		typ := sm.GetSecretEncType()
		if typ != waE2E.SecretEncryptedMessage_MESSAGE_EDIT && typ != waE2E.SecretEncryptedMessage_EVENT_EDIT {
			return p, false
		}
		cli := b.client()
		if cli == nil {
			return p, false
		}
		dec, err := cli.DecryptSecretEncryptedMessage(ctx, evt)
		if err != nil {
			b.log.Warnf("decrypt edit of %s in %s: %v", sm.GetTargetMessageKey().GetID(), chat, err)
			return p, false
		}
		dec = unwrap(dec)
		if typ == waE2E.SecretEncryptedMessage_EVENT_EDIT {
			p.target = sm.GetTargetMessageKey().GetID()
			if e := dec.GetEventMessage(); e != nil {
				p.event = eventContent(e).extra.Event
				p.msg.rawPayload = marshal(dec)
			}
			return p, p.target != "" && p.event != nil
		}
		if pm := dec.GetProtocolMessage(); pm.GetType() == waE2E.ProtocolMessage_MESSAGE_EDIT {
			dec = pm.GetEditedMessage()
		}
		return p, p.setEdit(sm.GetTargetMessageKey().GetID(), dec, evt.Info.Timestamp)
	}
	if evt.IsEdit {
		// ParseWebMessage (history sync) hands an edit out as the new
		// content under the edited message's ID.
		return p, p.setEdit(evt.Info.ID, m, evt.Info.Timestamp)
	}

	c := describe(m)
	viewOnce := isViewOnce(m)
	if viewOnce {
		// The pill that opens it, whatever came inside; the media only
		// when it can be downloaded (see Message.OnPhone).
		c.kind = model.KindViewOnce
		if c.media != model.MediaImage && c.media != model.MediaVideo && c.media != model.MediaVoice {
			c.media = model.MediaNone
		}
	} else if c.text == "" && c.media == model.MediaNone && c.buttons.empty() {
		if !hasContent(m) {
			return p, false
		}
		// Something the app can't show: say so instead of dropping it.
		b.log.Infof("unsupported message %s in %s: %s", evt.Info.ID, chat, fieldNames(m))
		c = content{kind: model.KindUnsupported}
	}
	msg := p.msg.Message
	msg.ID = evt.Info.ID
	msg.Kind = c.kind
	msg.Media = c.media
	msg.Duration = c.duration
	msg.FromMe = evt.Info.IsFromMe
	msg.Text = c.text
	msg.Time = evt.Info.Timestamp
	msg.Link = c.link
	msg.Thumb = c.keptThumb()
	if c.kind == model.KindImage {
		msg.Album = first(albumOf(evt.RawMessage), albumOf(m))
	}
	c.file.apply(msg)
	c.extra.apply(msg)
	c.buttons.apply(msg)
	msg.Forwarded = c.ctx.GetIsForwarded()
	msg.Receipt = model.Sent
	if evt.SourceWebMsg != nil && msg.FromMe {
		msg.Receipt = webReceipt(evt.SourceWebMsg)
	}
	p.msg.senderJID = evt.Info.Sender.ToNonAD().String()
	p.msg.senderPush = evt.Info.PushName
	raw := evt.RawMessage
	if raw == nil {
		raw = evt.Message
	}
	p.msg.rawPayload = marshal(raw)
	if id, q := c.ctx.GetStanzaID(), c.ctx.GetQuotedMessage(); id != "" && hasMediaKey(q) {
		// It may quote a view once message whose media never came here.
		p.msg.quotedMedia, p.msg.quotedID = q, id
	}
	return p, true
}

// mentionsOf returns the mentions of a message's context, as stored.
func mentionsOf(ci *waE2E.ContextInfo) []string {
	out := append([]string(nil), ci.GetMentionedJID()...)
	if ci.GetNonJIDMentions() > 0 {
		out = append(out, mentionAll)
	}
	for _, gm := range ci.GetGroupMentions() {
		out = append(out, groupMention(gm.GetGroupJID(), gm.GetGroupSubject()))
	}
	return out
}

// setEdit makes p an edit of message target to the content of m, made at
// at. It reports whether there is one.
func (p *parsed) setEdit(target string, m *waE2E.Message, at time.Time) bool {
	c := describe(m)
	if target == "" || c.text == "" && c.media == model.MediaNone {
		return false
	}
	p.target, p.edited, p.edit, p.editTime = target, true, c.text, at
	p.msg.rawPayload = marshal(m) // decrypted edit content, not the edit instruction
	return true
}
