package wa

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/chomosuke9/wazzapclients/internal/model"
	whatsmeow "github.com/polymorfa/hypermeow"
	waBinary "github.com/polymorfa/hypermeow/binary"
	"github.com/polymorfa/hypermeow/proto/waE2E"
	"github.com/polymorfa/hypermeow/types"
	"google.golang.org/protobuf/proto"
)

// SendWhisper deliberately bypasses storeAndSend and SendMessage: their
// normal group fanout/retry paths must never broadcast a selective message.
func (b *Backend) SendWhisper(w model.Whisper) <-chan error {
	done := make(chan error, 1)
	w.Targets = slices.Clone(w.Targets)
	if w.Reply != nil {
		r := *w.Reply
		w.Reply = &r
	}
	// Read the files now, as SendFile does: the UI may delete them (a
	// pasted picture, an edited photo) once this returns.
	parts := make([]whisperPart, len(w.Parts))
	for i, p := range w.Parts {
		parts[i] = whisperPart{text: p.Text, mentions: slices.Clone(p.Mentions)}
		if p.File == nil {
			continue
		}
		a := *p.File
		a.Album = ""
		m, up, err := readFile(a, p.Text)
		if err != nil {
			done <- err
			close(done)
			return done
		}
		parts[i].file, parts[i].m, parts[i].up = &a, m, up
	}
	go func() {
		defer close(done)
		done <- b.sendWhisper(w, parts)
	}()
	return done
}

// whisperPart is a whisper's message on its way out: text, or a file read
// by readFile (m and up).
type whisperPart struct {
	text     string
	mentions []string
	file     *model.Attachment
	m        *model.Message
	up       upload
	msg      *waE2E.Message
	inner    proto.Message
}

func (b *Backend) sendWhisper(w model.Whisper, parts []whisperPart) error {
	chatID, targets := w.ChatID, w.Targets
	if b.Pref("cmd_whisper") != "on" {
		return errors.New("Enable /whisper in Ethically gray features first.")
	}
	if b.Pref(model.PrefGhost) == "on" {
		return errors.New("Turn off ghost mode to send messages.")
	}
	group, err := types.ParseJID(chatID)
	if err != nil || group.Server != types.GroupServer || group.User == "" {
		return errors.New("/whisper works only in groups.")
	}
	if len(targets) == 0 || len(parts) == 0 {
		return errors.New("Pick at least one member and type some text.")
	}
	for _, p := range parts {
		if p.file == nil && strings.TrimSpace(p.text) == "" {
			return errors.New("Pick at least one member and type some text.")
		}
	}
	cli := b.client()
	if cli == nil || !cli.IsConnected() {
		return errors.New("You're offline. Try again once connected.")
	}
	// Build each message, the first replying, and upload the files first:
	// that can take long, and an upload says nothing of who gets it.
	for i := range parts {
		p := &parts[i]
		var ci *waE2E.ContextInfo
		if len(p.mentions) > 0 || i == 0 && w.Reply != nil {
			ci = &waE2E.ContextInfo{MentionedJID: p.mentions}
			if i == 0 && w.Reply != nil {
				b.quote(chatID, w.Reply, ci)
			}
		}
		if p.file == nil {
			// Only the body (and any @mentions in it), without the command
			// or the recipient picks. A ContextInfo turns Conversation into
			// an ExtendedTextMessage, as an ordinary message does.
			p.msg = &waE2E.Message{Conversation: proto.String(p.text)}
			if ci != nil {
				p.msg = &waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{
					Text: proto.String(p.text), ContextInfo: ci,
				}}
			}
			continue
		}
		p.msg, p.inner = fileMessage(p.m, p.up, p.text, ci, p.file.ViewOnce)
		uctx, cancel := context.WithTimeout(b.ctx, 30*time.Minute)
		res, err := p.up.push(uctx, cli, p.m.Media)
		cancel()
		if err != nil {
			b.log.Errorf("whisper upload %s: %v", p.m.FileName, err)
			return fmt.Errorf("Couldn't upload %s; nothing was sent. %w", p.m.FileName, err)
		}
		uploaded(p.inner, res)
	}
	ctx, cancel := context.WithTimeout(b.ctx, 45*time.Second)
	defer cancel()
	info, err := cli.GetGroupInfo(ctx, group)
	if err != nil {
		return fmt.Errorf("Couldn't check the group's current members: %w", err)
	}
	di := cli.DangerousInternals()
	lookup := func(jid types.JID) (types.JID, error) {
		if jid.Server == types.HiddenUserServer {
			return cli.Store.LIDs.GetPNForLID(ctx, jid)
		}
		return cli.Store.LIDs.GetLIDForPN(ctx, jid)
	}
	users, err := whisperTargets(info, targets, di.GetOwnID(), di.GetOwnLID(), lookup)
	if err != nil {
		return err
	}
	devices, err := whisperDevices(ctx, users, cli.GetUserDevices)
	if err != nil {
		return err
	}
	// The exposed primitive bypasses SendMessage's private encryption lock.
	// Don't queue selective sends behind another potentially slow submission.
	if !b.sendMu.TryLock() {
		return errors.New("Another message is being sent. Try /whisper again when it finishes.")
	}
	defer b.sendMu.Unlock()
	for i := range parts {
		p := &parts[i]
		// EncryptMessageForDevice pads this plaintext itself.
		plaintext, err := proto.Marshal(p.msg)
		if err != nil {
			return err
		}
		typ, mediaType := whisperStanzaType(p.msg)
		encrypt := func(dev types.JID) (*waBinary.Node, bool, error) {
			identity := dev
			if dev.Server == types.DefaultUserServer {
				lid, err := cli.Store.LIDs.GetLIDForPN(ctx, dev)
				if err != nil {
					return nil, false, err
				}
				if !lid.IsEmpty() {
					di.MigrateSessionStore(ctx, dev, lid)
					identity = lid
				}
			}
			// The server routes a group message addressed to a single device only
			// when the enc carries a retry count, exactly as hypermeow's retry.go
			// stamps encrypted.Attrs["count"] and Baileys sends participant.count.
			// Without it the stanza reaches the socket but the recipient drops it,
			// which is why a whisper "submitted" but never appeared. retry.go
			// also names a media message's kind on the enc.
			encAttrs := waBinary.Attrs{"count": "1"}
			if mediaType != "" {
				encAttrs["mediatype"] = mediaType
			}
			enc, includeIdentity, err := di.EncryptMessageForDevice(ctx, plaintext, identity, nil, encAttrs, nil)
			if errors.Is(err, whatsmeow.ErrNoSession) {
				// Fetch by wire identity, as the fork's normal multi-device sender does.
				bundles := di.FetchPreKeysNoError(ctx, []types.JID{dev})
				if bundles[dev] == nil {
					return nil, false, errors.New("No encryption keys available for a recipient device.")
				}
				enc, includeIdentity, err = di.EncryptMessageForDevice(ctx, plaintext, identity, bundles[dev], encAttrs, nil)
			}
			return enc, includeIdentity, err
		}
		id := cli.GenerateMessageID()
		if err := dispatchWhisper(ctx, group, devices, id, typ, encrypt,
			di.MakeDeviceIdentityNode, di.SendNode); err != nil {
			if len(parts) > 1 {
				err = fmt.Errorf("Message %d of %d: %w", i+1, len(parts), err)
			}
			return err
		}
		// Leave a local-only record in the chat so the whisper reads as a sent
		// message, marked with its recipients. It is never broadcast, synced, or
		// retried: it only lives in this device's store (see putMessage).
		m := p.m
		if m == nil {
			m = &model.Message{Kind: model.KindText, Text: p.text}
		}
		m.ID, m.ChatID, m.FromMe, m.Time, m.Receipt = id, chatID, true, b.sendTime(), model.Sent
		if p.file != nil {
			b.keepSent(m, p.up, p.msg)
		}
		b.storeWhisper(ctx, m, targets, p.msg)
	}
	return nil
}

// whisperStanzaType is the type a message's stanza has and, for media, the
// mediatype its enc names, as hypermeow's own sender works them out
// (getTypeFromMessage, getMediaTypeFromMessage) for what a whisper sends.
func whisperStanzaType(msg *waE2E.Message) (typ, mediaType string) {
	if gm := msg.GetGroupMentionedMessage().GetMessage(); gm != nil {
		msg = gm
	}
	if dw := msg.GetDocumentWithCaptionMessage().GetMessage(); dw != nil {
		msg = dw
	}
	if vo := msg.GetViewOnceMessageV2().GetMessage(); vo != nil {
		msg = vo
	}
	switch {
	case msg.ImageMessage != nil:
		return "media", "image"
	case msg.DocumentMessage != nil:
		return "media", "document"
	case msg.AudioMessage != nil:
		if msg.AudioMessage.GetPTT() {
			return "media", "ptt"
		}
		return "media", "audio"
	case msg.VideoMessage != nil:
		if msg.VideoMessage.GetGifPlayback() {
			return "media", "gif"
		}
		return "media", "video"
	}
	return "text", ""
}

// storeWhisper saves and shows the local record of a sent whisper, m.
func (b *Backend) storeWhisper(ctx context.Context, m *model.Message, targets []string, msg *waE2E.Message) {
	sm := storedMsg{
		Message:    m,
		whisper:    strings.Join(targets, "\n"),
		rawPayload: marshal(msg),
	}
	if err := b.store.ensureChat(ctx, b.db, m.ChatID, true, ""); err != nil {
		b.log.Errorf("whisper store chat %s: %v", m.ChatID, err)
	}
	if err := b.store.putMessage(ctx, b.db, sm); err != nil {
		b.log.Errorf("store whisper record: %v", err)
		return
	}
	b.emitMessage(m.ChatID, m.ID)
	b.emitChat(m.ChatID)
}

// whisperTargets checks fresh membership, excludes every own device via its
// bare identity, and selects each member's wire address for this group.
func whisperTargets(info *types.GroupInfo, targets []string, ownPN, ownLID types.JID,
	lookup func(types.JID) (types.JID, error),
) ([]types.JID, error) {
	if info == nil {
		return nil, errors.New("Couldn't load the group's members.")
	}
	if info.AddressingMode != "" && info.AddressingMode != types.AddressingModePN && info.AddressingMode != types.AddressingModeLID {
		return nil, errors.New("Unsupported group addressing mode.")
	}
	me, admin := false, false
	for _, p := range info.Participants {
		for _, jid := range []types.JID{p.JID, p.PhoneNumber, p.LID} {
			if !jid.IsEmpty() && (jid.ToNonAD() == ownPN.ToNonAD() || jid.ToNonAD() == ownLID.ToNonAD()) {
				me, admin = true, p.IsAdmin || p.IsSuperAdmin
			}
		}
	}
	if !me || info.IsAnnounce && !admin {
		return nil, errors.New("You can't send messages in this group.")
	}
	var users []types.JID
	for _, target := range targets {
		jid, err := types.ParseJID(target)
		if err != nil || jid.User == "" || jid != jid.ToNonAD() ||
			(jid.Server != types.HiddenUserServer && jid.Server != types.DefaultUserServer) {
			return nil, errors.New("Invalid whisper recipient.")
		}
		var member *types.GroupParticipant
		find := func(id types.JID) {
			for i := range info.Participants {
				p := &info.Participants[i]
				if !id.IsEmpty() && (id == p.JID.ToNonAD() || id == p.PhoneNumber.ToNonAD() || id == p.LID.ToNonAD()) {
					member = p
					return
				}
			}
		}
		find(jid)
		if member == nil {
			alias, err := lookup(jid)
			if err != nil {
				return nil, fmt.Errorf("Couldn't resolve recipient identity: %w", err)
			}
			find(alias)
		}
		if member == nil {
			return nil, errors.New("A selected recipient is no longer a member of this group.")
		}
		wire := types.JID{}
		server := types.DefaultUserServer
		if info.AddressingMode == types.AddressingModeLID {
			server = types.HiddenUserServer
		}
		for _, id := range []types.JID{member.JID, member.LID, member.PhoneNumber} {
			if !id.IsEmpty() && (id.ToNonAD() == ownPN.ToNonAD() || id.ToNonAD() == ownLID.ToNonAD()) {
				return nil, errors.New("You can't whisper to yourself.")
			}
			if id.Server == server {
				wire = id.ToNonAD()
			}
		}
		if wire.IsEmpty() {
			wire, err = lookup(member.JID.ToNonAD())
			if err != nil {
				return nil, fmt.Errorf("Couldn't resolve group addressing: %w", err)
			}
		}
		wire = wire.ToNonAD()
		if wire.IsEmpty() || wire.Server != server || wire == ownPN.ToNonAD() || wire == ownLID.ToNonAD() {
			return nil, errors.New("Couldn't resolve a recipient's group address.")
		}
		if !slices.Contains(users, wire) {
			users = append(users, wire)
		}
	}
	return users, nil
}

// Require devices for every target before sending anything. Reject unexpected
// identities rather than broadening the recipient list returned by discovery.
func whisperDevices(ctx context.Context, users []types.JID,
	get func(context.Context, []types.JID) ([]types.JID, error),
) ([]types.JID, error) {
	devices, err := get(ctx, users)
	if err != nil {
		return nil, fmt.Errorf("Couldn't find recipient devices: %w", err)
	}
	var out []types.JID
	for _, dev := range devices {
		if !slices.Contains(users, dev.ToNonAD()) {
			return nil, errors.New("Device lookup returned an unselected recipient.")
		}
		if !slices.Contains(out, dev) {
			out = append(out, dev)
		}
	}
	for _, user := range users {
		if !slices.ContainsFunc(out, func(dev types.JID) bool { return dev.ToNonAD() == user }) {
			return nil, errors.New("No devices found for one of the selected members.")
		}
	}
	if len(out) == 0 {
		return nil, errors.New("No recipient devices found.")
	}
	return out, nil
}

// Retry-shaped individual envelopes, verified against hypermeow 07d103b's
// retry.go. SendNode confirms only a socket write, never recipient delivery.
// Do not add these payloads to the ordinary message store or retry cache.
func dispatchWhisper(ctx context.Context, group types.JID, devices []types.JID, id, typ string,
	encrypt func(types.JID) (*waBinary.Node, bool, error), identity func() waBinary.Node,
	send func(context.Context, waBinary.Node) error,
) error {
	for i, dev := range devices {
		err := ctx.Err()
		if err == nil {
			var enc *waBinary.Node
			var includeIdentity bool
			enc, includeIdentity, err = encrypt(dev)
			if err == nil {
				content := []waBinary.Node{*enc}
				if includeIdentity {
					content = append(content, identity())
				}
				err = send(ctx, waBinary.Node{Tag: "message", Attrs: waBinary.Attrs{
					"id": id, "type": typ, "to": group, "participant": dev,
				}, Content: content})
			}
		}
		if err != nil {
			return fmt.Errorf("Whisper stopped after %d of %d device submissions. Delivery is unconfirmed; some recipients may already have it. No automatic retry. %w", i, len(devices), err)
		}
	}
	return nil
}
