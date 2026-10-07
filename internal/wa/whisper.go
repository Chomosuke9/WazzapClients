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
func (b *Backend) SendWhisper(chatID string, targets []string, text string) <-chan error {
	done := make(chan error, 1)
	targets = slices.Clone(targets)
	go func() {
		defer close(done)
		ctx, cancel := context.WithTimeout(b.ctx, 45*time.Second)
		defer cancel()
		done <- b.sendWhisper(ctx, chatID, targets, text)
	}()
	return done
}

func (b *Backend) sendWhisper(ctx context.Context, chatID string, targets []string, text string) error {
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
	if len(targets) == 0 || strings.TrimSpace(text) == "" {
		return errors.New("Pick at least one member and type some text.")
	}
	cli := b.client()
	if cli == nil || !cli.IsConnected() {
		return errors.New("You're offline. Try again once connected.")
	}
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
	// Marshal only the body, without the command or recipient mentions.
	// EncryptMessageForDevice pads this plaintext itself.
	plaintext, err := proto.Marshal(&waE2E.Message{Conversation: proto.String(text)})
	if err != nil {
		return err
	}
	// The exposed primitive bypasses SendMessage's private encryption lock.
	// Don't queue selective sends behind another potentially slow submission.
	if !b.sendMu.TryLock() {
		return errors.New("Another message is being sent. Try /whisper again when it finishes.")
	}
	defer b.sendMu.Unlock()
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
		enc, includeIdentity, err := di.EncryptMessageForDevice(ctx, plaintext, identity, nil, nil, nil)
		if errors.Is(err, whatsmeow.ErrNoSession) {
			// Fetch by wire identity, as the fork's normal multi-device sender does.
			bundles := di.FetchPreKeysNoError(ctx, []types.JID{dev})
			if bundles[dev] == nil {
				return nil, false, errors.New("No encryption keys available for a recipient device.")
			}
			enc, includeIdentity, err = di.EncryptMessageForDevice(ctx, plaintext, identity, bundles[dev], nil, nil)
		}
		return enc, includeIdentity, err
	}
	return dispatchWhisper(ctx, group, devices, cli.GenerateMessageID(), encrypt,
		di.MakeDeviceIdentityNode, di.SendNode)
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
func dispatchWhisper(ctx context.Context, group types.JID, devices []types.JID, id string,
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
					"id": id, "type": "text", "to": group, "participant": dev,
				}, Content: content})
			}
		}
		if err != nil {
			return fmt.Errorf("Whisper stopped after %d of %d device submissions. Delivery is unconfirmed; some recipients may already have it. No automatic retry. %w", i, len(devices), err)
		}
	}
	return nil
}
