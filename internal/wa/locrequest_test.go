package wa

import (
	"testing"
	"time"

	"github.com/polymorfa/hypermeow/proto/waCommon"
	"github.com/polymorfa/hypermeow/proto/waE2E"
	"github.com/polymorfa/hypermeow/types"
	"github.com/polymorfa/hypermeow/types/events"
	"google.golang.org/protobuf/proto"
)

// TestLocationRequest checks that a location request is a message of its
// own, not one that couldn't load, and that taking it back marks it so.
func TestLocationRequest(t *testing.T) {
	b := testBackend(t)
	ctx := b.ctx
	rina := types.NewJID("555", types.HiddenUserServer)
	put := func(id string, m *waE2E.Message, at int64) {
		t.Helper()
		info := types.MessageInfo{ID: id, Timestamp: time.Unix(at, 0), MessageSource: types.MessageSource{Chat: rina, Sender: rina}}
		p, ok := b.parse(ctx, &events.Message{Info: info, Message: m, RawMessage: m})
		if !ok {
			t.Fatalf("%s didn't parse", id)
		}
		b.apply(&info, p)
	}
	put("L1", &waE2E.Message{RequestLocationMessage: &waE2E.RequestLocationMessage{}}, 1000)
	m, ok := b.store.message(ctx, rina.String(), "L1")
	if !ok || !m.LocationRequest || m.RequestCanceled || m.Opened {
		t.Fatalf("request as stored: %+v", m.Message)
	}
	put("L2", &waE2E.Message{RequestLocationUpdateMessage: &waE2E.RequestLocationUpdateMessage{
		Key: &waCommon.MessageKey{ID: proto.String("L1")}, UpdateType: waE2E.RequestLocationUpdateMessage_CANCEL.Enum()}}, 1001)
	if m, _ := b.store.message(ctx, rina.String(), "L1"); !m.RequestCanceled {
		t.Error("a request taken back isn't cancelled")
	}
	if _, ok := b.store.message(ctx, rina.String(), "L2"); ok {
		t.Error("the cancel is a message of its own")
	}
}
