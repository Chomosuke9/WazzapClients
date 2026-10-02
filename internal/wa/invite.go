package wa

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/polymorfa/hypermeow"
	"github.com/polymorfa/hypermeow/types"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

// inviteFaces is how many members an invite shows.
const inviteFaces = 4

// GroupInvite implements model.Backend.
func (b *Backend) GroupInvite(code string) {
	cli := b.client()
	if cli == nil || !cli.IsConnected() {
		b.emit(model.InviteEvent{Code: code, Err: "You're offline. Try again once connected."})
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(b.ctx, 30*time.Second)
		defer cancel()
		info, err := cli.GetGroupInfoFromLink(ctx, code)
		if err != nil {
			b.log.Debugf("invite %s: %v", code, err)
			b.emit(model.InviteEvent{Code: code, Err: inviteError(err)})
			return
		}
		g := b.groupPreview(ctx, info)
		if !g.Member {
			// Only the invite lets you see the picture of a group you
			// aren't in. Saved where Avatar finds it, before the event.
			b.invitePicture(ctx, cli, info.JID, code)
		}
		b.emit(model.InviteEvent{Code: code, Group: g})
	}()
}

// groupPreview describes a group looked up from its invite link.
func (b *Backend) groupPreview(ctx context.Context, info *types.GroupInfo) *model.GroupPreview {
	g := &model.GroupPreview{
		ID:          info.JID.String(),
		Name:        info.Name,
		Description: info.Topic,
		Created:     info.GroupCreated,
		Size:        max(info.ParticipantCount, len(info.Participants)),
		Approval:    info.IsJoinApprovalRequired,
		Community:   info.IsParent,
	}
	for _, p := range info.Participants {
		if b.isMe(p.JID) || !p.LID.IsEmpty() && b.isMe(p.LID) || !p.PhoneNumber.IsEmpty() && b.isMe(p.PhoneNumber) {
			g.Member = true
			continue
		}
		if len(g.Faces) < inviteFaces {
			id := p.JID
			if !p.LID.IsEmpty() {
				id = p.LID
			}
			g.Faces = append(g.Faces, b.canonical(ctx, id).String())
		}
	}
	if !g.Member {
		// The link may list only some members; the stored ones are all.
		for _, p := range b.store.members(ctx, g.ID) {
			if b.isMe(p.JID) || !p.PhoneNumber.IsEmpty() && b.isMe(p.PhoneNumber) {
				g.Member = true
				break
			}
		}
	}
	return g
}

// invitePicture saves the picture of a group you were invited to.
func (b *Backend) invitePicture(ctx context.Context, cli *whatsmeow.Client, jid types.JID, code string) {
	path := b.avatarPath(jid.String())
	if st, err := os.Stat(path); err == nil && time.Since(st.ModTime()) < avatarTTL {
		return
	}
	_ = os.MkdirAll(filepath.Dir(path), 0o700)
	info, err := cli.GetProfilePictureInfo(ctx, jid, &whatsmeow.GetProfilePictureParams{Preview: true, InviteCode: code})
	var data []byte
	switch {
	case errors.Is(err, whatsmeow.ErrProfilePictureNotSet), errors.Is(err, whatsmeow.ErrProfilePictureUnauthorized),
		err == nil && info == nil:
	case err != nil:
		b.log.Debugf("picture of invite %s: %v", code, err)
		return
	default:
		if data, err = httpGet(ctx, info.URL); err != nil {
			b.log.Debugf("download picture of invite %s: %v", code, err)
			return
		}
	}
	if os.WriteFile(path, data, 0o600) == nil {
		b.emit(model.AvatarEvent{ID: jid.String()})
	}
}

// JoinGroup implements model.Backend.
func (b *Backend) JoinGroup(code string) {
	cli := b.client()
	if cli == nil || !cli.IsConnected() {
		b.emit(model.JoinedEvent{Code: code, Err: "You're offline. Try again once connected."})
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(b.ctx, 60*time.Second)
		defer cancel()
		// Fresh details: whether joining needs an admin, and the name.
		info, err := cli.GetGroupInfoFromLink(ctx, code)
		if err == nil {
			_, err = cli.JoinGroupWithLink(ctx, code)
		}
		if err != nil {
			b.log.Warnf("join with invite %s: %v", code, err)
			msg := inviteError(err)
			if !errors.Is(err, whatsmeow.ErrInviteLinkRevoked) && !errors.Is(err, whatsmeow.ErrInviteLinkInvalid) {
				msg = "Couldn't join the group. Try again."
			}
			b.emit(model.JoinedEvent{Code: code, Err: msg})
			return
		}
		jid := info.JID.String()
		if info.IsJoinApprovalRequired {
			b.emit(model.JoinedEvent{Code: code, ChatID: jid, Requested: true})
			return
		}
		_ = b.store.setGroupShape(ctx, info)
		_ = b.store.setField(ctx, jid, "last_ts", time.Now().Unix())
		if full, err := cli.GetGroupInfo(ctx, info.JID); err == nil {
			_ = b.store.setMembers(ctx, full)
		}
		b.emitChat(jid)
		b.emit(model.JoinedEvent{Code: code, ChatID: jid})
	}()
}

// inviteError says why an invite link can't be used.
func inviteError(err error) string {
	switch {
	case errors.Is(err, whatsmeow.ErrInviteLinkRevoked):
		return "This invite link was reset. Ask a group admin for a new one."
	case errors.Is(err, whatsmeow.ErrInviteLinkInvalid):
		return "This invite link is invalid."
	}
	return "Couldn't open the invite link. Check your connection and try again."
}
