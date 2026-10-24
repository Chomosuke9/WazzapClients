package wa

import (
	"slices"
	"testing"
	"time"

	"github.com/polymorfa/hypermeow/proto/waE2E"
	"github.com/polymorfa/hypermeow/proto/waSyncAction"
	"github.com/polymorfa/hypermeow/types"
	"github.com/polymorfa/hypermeow/types/events"
	"google.golang.org/protobuf/proto"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

// TestStatusPrivacySync checks the status privacy synced from the phone:
// each mode's people are kept from the action that named them, and a
// change keeps the people of the other modes, the audiences and what the
// app doesn't show.
func TestStatusPrivacySync(t *testing.T) {
	b := testBackend(t)
	if p := b.syncedStatusPrivacy(b.ctx); p != nil {
		t.Fatalf("before any sync: %+v", p)
	}
	deny := waSyncAction.StatusPrivacyAction_DENY_LIST
	b.onStatusPrivacy(&waSyncAction.StatusPrivacyAction{Mode: deny.Enum(), UserJID: []string{"1@s.whatsapp.net", "2@s.whatsapp.net"}})
	custom := waSyncAction.StatusPrivacyAction_CUSTOM_LIST
	b.onStatusPrivacy(&waSyncAction.StatusPrivacyAction{
		Mode: custom.Enum(), ShareToFb: proto.Bool(true),
		CustomLists: []*waSyncAction.StatusPrivacyAction_CustomList{
			{ListID: proto.String("L1"), Name: proto.String("(−∞,∞)"), Emoji: proto.String("♾️"), IsSelected: proto.Bool(true),
				UserJID: []string{"3@s.whatsapp.net", "4@s.whatsapp.net", "5@s.whatsapp.net"}},
		},
	})
	p := b.syncedStatusPrivacy(b.ctx)
	if p == nil || !p.Editable || p.Audience != model.AudienceList || p.ListID != "L1" || p.Count != 3 {
		t.Fatalf("synced = %+v", p)
	}
	// Only share with and close friends were never named: unknown, not
	// nobody.
	if !p.Knows(model.AudienceExcept) || p.Knows(model.AudienceOnly) || p.Knows(model.AudienceClose) {
		t.Errorf("unknown = %v", p.Unknown)
	}
	if !slices.Equal(p.Except, []string{"1@s.whatsapp.net", "2@s.whatsapp.net"}) || len(p.Lists) != 1 || p.Lists[0].Emoji != "♾️" {
		t.Fatalf("lists = %+v", p)
	}

	// Except one more person, and a new audience: the action names the
	// excepted, keeps the audience it had and adds the new one.
	p.Audience = model.AudienceExcept
	p.Except = append(p.Except, "6@s.whatsapp.net")
	p.Lists = append(p.Lists, model.StatusList{Name: "Work", Emoji: "💼", Members: []string{"7@s.whatsapp.net"}})
	a, others := b.privacyAction(b.ctx, b.statusPrivacyAction(b.ctx), *p)
	if modeOf(a) != deny || !slices.Equal(a.GetUserJID(), []string{"1@s.whatsapp.net", "2@s.whatsapp.net", "6@s.whatsapp.net"}) {
		t.Errorf("action = %v", a)
	}
	if !a.GetShareToFb() || len(a.GetCustomLists()) != 2 || a.GetCustomLists()[0].GetIsSelected() || a.GetCustomLists()[1].GetListID() == "" {
		t.Errorf("custom lists = %v", a.GetCustomLists())
	}
	// The unknown lists stay the phone's: nothing is kept for them.
	if len(others) != 0 {
		t.Errorf("others = %v", others)
	}

	// A new audience chosen as it's made.
	p.Audience, p.ListID = model.AudienceList, ""
	p.Lists = []model.StatusList{{Name: "Trip", Emoji: "🌴", Members: []string{"3@s.whatsapp.net"}}}
	a, _ = b.privacyAction(b.ctx, b.statusPrivacyAction(b.ctx), *p)
	if modeOf(a) != custom || len(a.GetCustomLists()) != 1 || !a.GetCustomLists()[0].GetIsSelected() {
		t.Errorf("new audience = %v", a)
	}
}

// TestStatusMute checks that a contact's hidden status updates are marked
// so, and shown again once unhidden.
func TestStatusMute(t *testing.T) {
	b := testBackend(t)
	if err := b.store.putStatus(b.ctx, b.db, storedStatus{id: "S1", sender: "9@lid", ts: time.Now(), c: content{text: "hi"}}); err != nil {
		t.Fatal(err)
	}
	muted := func() bool {
		for _, th := range b.Statuses() {
			if th.ID == "9@lid" {
				return th.Muted
			}
		}
		t.Fatal("thread missing")
		return false
	}
	j := types.NewJID("9", types.HiddenUserServer)
	b.onStatusMute(&events.UserStatusMute{JID: j, Action: &waSyncAction.UserStatusMuteAction{Muted: proto.Bool(true)}})
	if !muted() {
		t.Error("not hidden")
	}
	b.onStatusMute(&events.UserStatusMute{JID: j, Action: &waSyncAction.UserStatusMuteAction{Muted: proto.Bool(false)}})
	if muted() {
		t.Error("still hidden")
	}
}

// TestStatusAudienceLabel checks that a status shared with an audience
// shows which one.
func TestStatusAudienceLabel(t *testing.T) {
	b := testBackend(t)
	poster := types.NewJID("555", types.HiddenUserServer)
	post := func(id string, meta *waE2E.ContextInfo_StatusAudienceMetadata) {
		b.onStatus(&events.Message{
			Info: types.MessageInfo{MessageSource: types.MessageSource{Chat: types.StatusBroadcastJID, Sender: poster},
				ID: types.MessageID(id), Timestamp: time.Now()},
			Message: &waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{Text: proto.String(id),
				ContextInfo: &waE2E.ContextInfo{StatusAudienceMetadata: meta}}}})
	}
	post("close", &waE2E.ContextInfo_StatusAudienceMetadata{AudienceType: waE2E.ContextInfo_StatusAudienceMetadata_CLOSE_FRIENDS.Enum()})
	post("list", &waE2E.ContextInfo_StatusAudienceMetadata{ListName: proto.String("Work"), ListEmoji: proto.String("💼")})
	post("all", nil)
	got := map[string]string{}
	for _, th := range b.Statuses() {
		for _, up := range th.Updates {
			got[up.ID] = up.AudienceEmoji + up.Audience
		}
	}
	if got["close"] != "💜Close friends" || got["list"] != "💼Work" || got["all"] != "" {
		t.Errorf("audiences = %v", got)
	}
}
