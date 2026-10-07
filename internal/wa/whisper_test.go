package wa

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/chomosuke9/wazzapclients/internal/model"
	waBinary "github.com/polymorfa/hypermeow/binary"
	"github.com/polymorfa/hypermeow/types"
)

func whisperJID(user, server string) types.JID { return types.NewJID(user, server) }

func TestWhisperTargets(t *testing.T) {
	mePN, meLID := whisperJID("1", types.DefaultUserServer), whisperJID("11", types.HiddenUserServer)
	pn, lid := whisperJID("2", types.DefaultUserServer), whisperJID("22", types.HiddenUserServer)
	lookup := func(jid types.JID) (types.JID, error) {
		if jid == pn {
			return lid, nil
		}
		if jid == lid {
			return pn, nil
		}
		return types.JID{}, nil
	}
	for _, mode := range []types.AddressingMode{types.AddressingModePN, types.AddressingModeLID} {
		info := &types.GroupInfo{AddressingMode: mode, Participants: []types.GroupParticipant{
			{JID: meLID, LID: meLID, PhoneNumber: mePN}, {JID: lid, LID: lid, PhoneNumber: pn},
		}}
		want := pn
		if mode == types.AddressingModeLID {
			want = lid
		}
		got, err := whisperTargets(info, []string{pn.String(), lid.String(), pn.String()}, mePN, meLID, lookup)
		if err != nil || !slices.Equal(got, []types.JID{want}) {
			t.Fatalf("%s: %v, %v", mode, got, err)
		}
		for _, target := range []string{mePN.String(), meLID.String(), "9@lid", "2:3@s.whatsapp.net", "x@g.us", ""} {
			if _, err := whisperTargets(info, []string{target}, mePN, meLID, lookup); err == nil {
				t.Errorf("accepted %q", target)
			}
		}
		info.IsAnnounce = true
		if _, err := whisperTargets(info, []string{lid.String()}, mePN, meLID, lookup); err == nil {
			t.Fatal("bypassed admin-only group")
		}
		info.Participants[0].IsAdmin = true
		if _, err := whisperTargets(info, []string{lid.String()}, mePN, meLID, lookup); err != nil {
			t.Fatal(err)
		}
		info.Participants = info.Participants[1:]
		if _, err := whisperTargets(info, []string{lid.String()}, mePN, meLID, lookup); err == nil {
			t.Fatal("sent after leaving group")
		}
	}
	// Metadata has only a PN; the picker has the canonical LID.
	info := &types.GroupInfo{AddressingMode: types.AddressingModeLID, Participants: []types.GroupParticipant{{JID: mePN}, {JID: pn}}}
	got, err := whisperTargets(info, []string{lid.String()}, mePN, meLID, lookup)
	if err != nil || !slices.Equal(got, []types.JID{lid}) {
		t.Fatalf("mapping: %v %v", got, err)
	}
	if _, err := whisperTargets(info, []string{pn.String()}, mePN, meLID, func(types.JID) (types.JID, error) { return types.JID{}, nil }); err == nil {
		t.Fatal("sent without LID mapping")
	}
}

func TestWhisperDevices(t *testing.T) {
	a, b := whisperJID("1", types.HiddenUserServer), whisperJID("2", types.HiddenUserServer)
	a2 := a
	a2.Device = 2
	for _, tc := range []struct {
		devices []types.JID
		bad     bool
	}{
		{[]types.JID{a, a2, a2, b}, false}, {nil, true}, {[]types.JID{a}, true},
		{[]types.JID{a, b, whisperJID("3", types.HiddenUserServer)}, true},
	} {
		got, err := whisperDevices(context.Background(), []types.JID{a, b}, func(_ context.Context, users []types.JID) ([]types.JID, error) {
			if !slices.Equal(users, []types.JID{a, b}) {
				t.Fatal("discovery broadened recipients")
			}
			return tc.devices, nil
		})
		if (err != nil) != tc.bad {
			t.Fatalf("%v: %v", tc.devices, err)
		}
		if !tc.bad && !slices.Equal(got, []types.JID{a, a2, b}) {
			t.Fatalf("devices: %v", got)
		}
	}
}

func TestDispatchWhisper(t *testing.T) {
	group := whisperJID("123", types.GroupServer)
	a, b := whisperJID("11", types.HiddenUserServer), whisperJID("22", types.HiddenUserServer)
	b.Device = 2
	for _, failAt := range []string{"", "encrypt", "send", "cancel"} {
		t.Run(failAt, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if failAt == "cancel" {
				cancel()
			}
			var nodes []waBinary.Node
			err := dispatchWhisper(ctx, group, []types.JID{a, b}, "same-id", func(dev types.JID) (*waBinary.Node, bool, error) {
				if failAt == "encrypt" && dev == b {
					return nil, false, errors.New("encryption failed")
				}
				typ := "msg"
				if dev == b {
					typ = "pkmsg"
				}
				return &waBinary.Node{Tag: "enc", Attrs: waBinary.Attrs{"v": "2", "type": typ}, Content: []byte("ciphertext")}, dev == b, nil
			}, func() waBinary.Node { return waBinary.Node{Tag: "device-identity"} }, func(_ context.Context, node waBinary.Node) error {
				if failAt == "send" && len(nodes) == 1 {
					return errors.New("socket failed")
				}
				nodes = append(nodes, node)
				return nil
			})
			if (err != nil) != (failAt != "") {
				t.Fatalf("error: %v", err)
			}
			if failAt == "cancel" && len(nodes) != 0 {
				t.Fatal("sent after cancellation")
			}
			if failAt == "send" || failAt == "encrypt" {
				if len(nodes) != 1 || !strings.Contains(err.Error(), "1 of 2") {
					t.Fatalf("partial result: %d %v", len(nodes), err)
				}
			}
			if failAt == "" && len(nodes) != 2 {
				t.Fatalf("sent %d envelopes", len(nodes))
			}
			for i, node := range nodes {
				if node.Tag != "message" || node.Attrs["to"] != group || node.Attrs["id"] != "same-id" || node.Attrs["participant"] != []types.JID{a, b}[i] {
					t.Fatalf("wrong routing: %+v", node)
				}
				children := node.GetChildren()
				if len(children) != i+1 || children[0].Tag != "enc" || children[0].Attrs["type"] == "skmsg" {
					t.Fatalf("bad envelope: %+v", children)
				}
				if i == 1 && children[1].Tag != "device-identity" {
					t.Fatal("missing prekey identity")
				}
			}
		})
	}
}

func TestWhisperBackendGates(t *testing.T) {
	b := testBackend(t)
	for _, tc := range []struct {
		enabled, ghost   bool
		chat, text, want string
	}{
		{false, false, "1@g.us", "secret", "Enable /whisper"},
		{true, true, "1@g.us", "secret", "ghost mode"},
		{true, false, "1@lid", "secret", "only in groups"},
		{true, false, "1@g.us", " ", "some text"},
		{true, false, "1@g.us", "secret", "offline"},
	} {
		b.SetPref("cmd_whisper", map[bool]string{true: "on", false: "off"}[tc.enabled])
		b.SetPref(model.PrefGhost, map[bool]string{true: "on", false: "off"}[tc.ghost])
		done := b.SendWhisper(tc.chat, []string{"2@lid"}, tc.text)
		if err := <-done; err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("%+v: %v", tc, err)
		}
		if _, ok := <-done; ok {
			t.Fatal("completion channel not closed")
		}
	}
	if len(b.Messages("1@g.us", 100)) != 0 {
		t.Fatal("whisper stored as ordinary outgoing message")
	}
}
