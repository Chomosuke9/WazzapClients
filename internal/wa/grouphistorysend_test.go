package wa

import (
	"testing"
	"time"

	"github.com/polymorfa/hypermeow/proto/waE2E"
	"github.com/polymorfa/hypermeow/types"
	"github.com/polymorfa/hypermeow/types/events"
	"google.golang.org/protobuf/proto"
)

// TestHistoryBundleSend checks that the messages a group shares with new
// members are its newest ordinary ones, written from your side, and that
// the bundle reads back as the receiving side reads one.
func TestHistoryBundleSend(t *testing.T) {
	b := testBackend(t)
	ctx := b.ctx
	group := types.NewJID("120363000000000002", types.GroupServer)
	rina := types.NewJID("555", types.HiddenUserServer)
	me := types.NewJID("111", types.HiddenUserServer)
	now := b.now()
	for i, c := range []struct {
		id, text string
		fromMe   bool
		ago      time.Duration
	}{{"OLD", "too old", false, 20 * 24 * time.Hour}, {"M1", "halo", false, time.Hour}, {"M2", "hai juga", true, time.Minute}} {
		m := &waE2E.Message{Conversation: proto.String(c.text)}
		info := types.MessageInfo{ID: c.id, Timestamp: now.Add(-c.ago), PushName: "Rina",
			MessageSource: types.MessageSource{Chat: group, Sender: rina, IsGroup: true, IsFromMe: c.fromMe}}
		if c.fromMe {
			info.Sender = me
		}
		p, ok := b.parse(ctx, &events.Message{Info: info, Message: m, RawMessage: m})
		if !ok {
			t.Fatalf("%d didn't parse", i)
		}
		b.apply(&info, p)
	}
	rows, err := b.store.historyRows(ctx, group.String(), now.Add(-historyShareWindow))
	if err != nil || len(rows) != 2 || rows[0].id != "M1" || rows[1].id != "M2" {
		t.Fatalf("rows %+v (%v)", rows, err)
	}
	data, oldest, err := historyBundle(group, me, rows)
	if err != nil || oldest != rows[0].ts {
		t.Fatalf("bundle: %v, oldest %d", err, oldest)
	}
	gh, err := decodeGroupHistory(data)
	if err != nil {
		t.Fatal(err)
	}
	ms := gh.GetMessages()
	if len(ms) != 2 || ms[0].GetMessage().GetConversation() != "halo" || ms[0].GetKey().GetFromMe() ||
		ms[0].GetKey().GetParticipant() != rina.String() || !ms[1].GetKey().GetFromMe() ||
		ms[1].GetKey().GetParticipant() != me.String() || ms[0].GetPushName() != "Rina" {
		t.Errorf("bundle holds %v", ms)
	}
}
