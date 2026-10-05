package wa

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/polymorfa/hypermeow"
	"github.com/polymorfa/hypermeow/proto/waE2E"
	"github.com/polymorfa/hypermeow/types"
	"google.golang.org/protobuf/proto"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

// participantChanges maps member actions to hypermeow's.
var participantChanges = map[model.GroupAction]whatsmeow.ParticipantChange{
	model.GroupAdd:     whatsmeow.ParticipantChangeAdd,
	model.GroupRemove:  whatsmeow.ParticipantChangeRemove,
	model.GroupPromote: whatsmeow.ParticipantChangePromote,
	model.GroupDemote:  whatsmeow.ParticipantChangeDemote,
}

// ManageGroup implements model.Backend.
func (b *Backend) ManageGroup(r model.GroupRequest) {
	ev := model.GroupEvent{Ref: r.Ref, ChatID: r.ChatID}
	cli := b.client()
	if cli == nil || !cli.IsConnected() {
		ev.Err = "You're offline. Try again once connected."
		b.emit(ev)
		return
	}
	jid, err := types.ParseJID(r.ChatID)
	if err != nil || jid.Server != types.GroupServer {
		ev.Err = "This isn't a group."
		b.emit(ev)
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(b.ctx, 30*time.Second)
		defer cancel()
		var err error
		changed := true
		switch r.Action {
		case model.GroupAdd, model.GroupRemove, model.GroupPromote, model.GroupDemote:
			ev.Members, err = b.changeMembers(ctx, cli, jid, r)
		case model.GroupAnnounce:
			err = cli.SetGroupAnnounce(ctx, jid, r.On)
		case model.GroupLock:
			err = cli.SetGroupLocked(ctx, jid, r.On)
		case model.GroupDescription:
			err = cli.SetGroupTopic(ctx, jid, "", "", r.Text)
		case model.GroupName:
			err = cli.SetGroupName(ctx, jid, r.Text)
		case model.GroupAddMode:
			mode := types.GroupMemberAddModeAllMember
			if r.On {
				mode = types.GroupMemberAddModeAdmin
			}
			err = cli.SetGroupMemberAddMode(ctx, jid, mode)
		case model.GroupApproval:
			err = cli.SetGroupJoinApprovalMode(ctx, jid, r.On)
		case model.GroupLink:
			ev.Link, err = cli.GetGroupInviteLink(ctx, jid, r.On)
			changed = false
		case model.GroupSendInvite:
			err = b.sendGroupInvite(ctx, jid, r)
			changed = false
		default:
			err = errors.New("unknown group action")
		}
		if err != nil {
			b.log.Warnf("manage group %s (action %d): %v", r.ChatID, r.Action, err)
			ev.Err = groupError(err)
		}
		b.emit(ev)
		if changed && err == nil {
			b.fetchInfo(jid) // announces the change with an InfoEvent
		}
	}()
}

// groupError says why WhatsApp refused a group change.
func groupError(err error) string {
	switch {
	case errors.Is(err, whatsmeow.ErrIQNotAuthorized), errors.Is(err, whatsmeow.ErrGroupInviteLinkUnauthorized):
		return "Only group admins can do that."
	case errors.Is(err, whatsmeow.ErrIQForbidden), errors.Is(err, whatsmeow.ErrNotInGroup):
		return "You're no longer in this group."
	case errors.Is(err, whatsmeow.ErrIQNotFound), errors.Is(err, whatsmeow.ErrGroupNotFound):
		return "This group doesn't exist anymore."
	case errors.Is(err, context.DeadlineExceeded):
		return "WhatsApp didn't answer. Try again."
	}
	return "WhatsApp refused it (" + err.Error() + ")."
}

// changeMembers adds, removes, promotes or demotes members and reports what
// happened to each.
func (b *Backend) changeMembers(ctx context.Context, cli *whatsmeow.Client, group types.JID, r model.GroupRequest) ([]model.MemberResult, error) {
	var (
		out    []model.MemberResult
		jids   []types.JID
		phones []string
	)
	for _, m := range r.Members {
		if strings.Contains(m, "@") {
			if j, err := types.ParseJID(m); err == nil {
				jids = append(jids, j.ToNonAD())
				continue
			}
		}
		digits := strings.Map(func(c rune) rune {
			if c >= '0' && c <= '9' {
				return c
			}
			return -1
		}, m)
		switch {
		case r.Action != model.GroupAdd:
			out = append(out, model.MemberResult{ID: m, Name: m, Err: "isn't a member of the group"})
		case len(digits) < 7 || strings.HasPrefix(digits, "0"):
			out = append(out, model.MemberResult{ID: m, Name: m, Err: "isn't a phone number with its country code"})
		default:
			phones = append(phones, "+"+digits)
		}
	}
	if len(phones) > 0 {
		res, err := cli.IsOnWhatsApp(ctx, phones)
		if err != nil {
			return out, err
		}
		for _, p := range res {
			if !p.IsIn {
				out = append(out, model.MemberResult{ID: p.Query, Name: formatPhone(strings.TrimPrefix(p.Query, "+")),
					Err: "isn't on WhatsApp"})
				continue
			}
			j := p.JID
			if !p.PhoneNumber.IsEmpty() {
				j = p.PhoneNumber
			}
			jids = append(jids, j)
		}
	}
	if len(jids) == 0 {
		return out, nil
	}
	ps, err := cli.UpdateGroupParticipants(ctx, group, jids, participantChanges[r.Action])
	if err != nil {
		return out, err
	}
	for _, p := range ps {
		res := model.MemberResult{ID: b.canonical(ctx, p.JID).String(), Name: b.memberName(ctx, p.JID, p.PhoneNumber)}
		if p.Error != 0 {
			res.Err = participantError(r.Action, p.Error)
			if a := p.AddRequest; a != nil && r.Action == model.GroupAdd {
				res.Invite = &model.GroupInvite{Code: a.Code, Expires: a.Expiration}
			}
		}
		out = append(out, res)
	}
	return out, nil
}

// participantError explains a participant's error code in a reply to a
// member change.
func participantError(action model.GroupAction, code int) string {
	switch code {
	case 403:
		if action == model.GroupAdd {
			return "can't be added because of their privacy settings"
		}
		return "can't be changed by you"
	case 404:
		if action == model.GroupAdd {
			return "isn't on WhatsApp"
		}
		return "isn't a member of the group"
	case 408:
		return "left the group recently and can't be added back yet"
	case 409:
		if action == model.GroupAdd {
			return "is already in the group"
		}
		return "can't be changed (they may be the group's creator)"
	}
	return "was refused by WhatsApp (error " + strconv.Itoa(code) + ")"
}

// sendGroupInvite sends r.Invite to r.Members[0], in your chat with them.
func (b *Backend) sendGroupInvite(ctx context.Context, group types.JID, r model.GroupRequest) error {
	if len(r.Members) == 0 || r.Invite == nil {
		return errors.New("no invite to send")
	}
	to, err := types.ParseJID(r.Members[0])
	if err != nil {
		return err
	}
	name := b.chatName(ctx, group)
	if rc, ok := b.store.chat(ctx, group.String()); ok && rc.Name != "" {
		name = rc.Name
	}
	msg := &waE2E.Message{GroupInviteMessage: &waE2E.GroupInviteMessage{
		GroupJID:         proto.String(group.String()),
		InviteCode:       proto.String(r.Invite.Code),
		InviteExpiration: proto.Int64(r.Invite.Expires.Unix()),
		GroupName:        proto.String(name),
		Caption:          proto.String("Invitation to join my WhatsApp group"),
	}}
	cli := b.client()
	if cli == nil {
		return errors.New("not logged in")
	}
	chat := b.canonical(ctx, to)
	m := &model.Message{ID: cli.GenerateMessageID(), ChatID: chat.String(), FromMe: true,
		Text: "Group invite: " + name, Time: b.sendTime(), Receipt: model.Pending}
	if sent := b.storeAndSend(chat, storedMsg{Message: m}, msg, nil); sent != nil {
		b.emit(model.MessageEvent{Msg: sent})
	}
	return nil
}

// SendNewSticker implements model.Backend.
func (b *Backend) SendNewSticker(chatID string, webp []byte, reply *model.Message) *model.Message {
	jid, err := types.ParseJID(chatID)
	cli := b.client()
	if err != nil || cli == nil {
		return nil
	}
	m := &model.Message{ID: cli.GenerateMessageID(), ChatID: chatID, FromMe: true, Kind: model.KindSticker,
		Media: model.MediaSticker, Time: b.sendTime(), Receipt: model.Pending}
	sm := storedMsg{Message: m}
	ci := b.draftContext(chatID, model.Draft{Reply: reply}, &sm)
	// Keep the picture, so it shows at once and without a download.
	up := upload{data: webp, w: 512, h: 512}
	up.keep(b.mediaPath(chatID, m.ID))
	ctx := b.ctx
	if err := b.store.ensureChat(ctx, b.db, chatID, jid.Server == types.GroupServer, ""); err != nil {
		b.log.Errorf("store chat %s: %v", chatID, err)
	}
	if err := b.store.putMessage(ctx, b.db, sm); err != nil {
		b.log.Errorf("store outgoing sticker: %v", err)
	}
	b.emitChat(chatID)
	go func() {
		ctx, cancel := context.WithTimeout(b.ctx, 5*time.Minute)
		defer cancel()
		res, err := cli.Upload(ctx, webp, whatsmeow.MediaImage)
		if err != nil {
			b.log.Errorf("upload sticker to %s: %v", chatID, err)
			b.sendFailed(chatID, m.ID, "Couldn't send the sticker.")
			return
		}
		e := &waE2E.StickerMessage{
			URL: proto.String(res.URL), DirectPath: proto.String(res.DirectPath), MediaKey: res.MediaKey,
			FileEncSHA256: res.FileEncSHA256, FileSHA256: res.FileSHA256, FileLength: proto.Uint64(res.FileLength),
			Mimetype: proto.String("image/webp"), Width: proto.Uint32(512), Height: proto.Uint32(512),
			MediaKeyTimestamp: proto.Int64(time.Now().Unix()), ContextInfo: ci,
		}
		sm.mediaBlob = marshal(e)
		if err := b.store.putMessage(b.ctx, b.db, sm); err != nil {
			b.log.Errorf("store sent sticker: %v", err)
		}
		b.sendAsync(chatID, jid, m.ID, &waE2E.Message{StickerMessage: e})
		b.recentSticker(sm.mediaBlob, time.Now(), chatID, m.ID)
	}()
	if r, ok := b.store.message(ctx, chatID, m.ID); ok {
		return b.resolve(ctx, r, jid.Server == types.GroupServer)
	}
	cp := *m
	return &cp
}
