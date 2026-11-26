package wa

import (
	"testing"
	"time"

	"github.com/polymorfa/hypermeow/proto/waCommon"
	"github.com/polymorfa/hypermeow/proto/waE2E"
	"github.com/polymorfa/hypermeow/types"
	"github.com/polymorfa/hypermeow/types/events"
	"google.golang.org/protobuf/proto"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

// TestPollKinds checks that a quiz, a poll that ends and one that hides
// its voters say so, and that options added to a poll join it: by its
// sender, or anyone when it allows that, once each and before it ends.
func TestPollKinds(t *testing.T) {
	b := testBackend(t)
	ctx := b.ctx
	group := types.NewJID("120363000000000001", types.GroupServer)
	rina := types.NewJID("555", types.HiddenUserServer)
	budi := types.NewJID("777", types.HiddenUserServer)
	opt := func(s string) *waE2E.PollCreationMessage_Option {
		return &waE2E.PollCreationMessage_Option{OptionName: proto.String(s)}
	}
	end := time.Unix(5000, 0)
	quiz := &waE2E.Message{PollCreationMessageV5: &waE2E.PollCreationMessage{Name: proto.String("2+2?"),
		Options: []*waE2E.PollCreationMessage_Option{opt("3"), opt("4")}, SelectableOptionsCount: proto.Uint32(1),
		PollType: waE2E.PollType_QUIZ.Enum(), CorrectAnswer: opt("4")}}
	poll := &waE2E.Message{PollCreationMessageV6: &waE2E.PollCreationMessage{Name: proto.String("Lunch?"),
		Options: []*waE2E.PollCreationMessage_Option{opt("Soto"), opt("Bakso")}, SelectableOptionsCount: proto.Uint32(0),
		EndTime: proto.Int64(end.UnixMilli()), HideParticipantName: proto.Bool(true)}}
	for i, c := range []struct {
		id  string
		msg *waE2E.Message
	}{{"Q1", quiz}, {"P1", poll}} {
		info := types.MessageInfo{ID: c.id, Timestamp: time.Unix(1000+int64(i), 0),
			MessageSource: types.MessageSource{Chat: group, Sender: rina, IsGroup: true}}
		p, ok := b.parse(ctx, &events.Message{Info: info, Message: c.msg, RawMessage: c.msg})
		if !ok {
			t.Fatalf("%s didn't parse", c.id)
		}
		b.apply(&info, p)
	}
	add := func(id string, who types.JID, name string, at time.Time) {
		t.Helper()
		m := &waE2E.Message{PollAddOptionMessage: &waE2E.PollAddOptionMessage{
			PollCreationMessageKey: &waCommon.MessageKey{ID: proto.String("P1")}, AddOption: opt(name)}}
		info := types.MessageInfo{ID: id, Timestamp: at, MessageSource: types.MessageSource{Chat: group, Sender: who, IsGroup: true}}
		p, ok := b.parse(ctx, &events.Message{Info: info, Message: m, RawMessage: m})
		if !ok || p.added == nil || p.target != "P1" {
			t.Fatalf("option %q: ok %v, %+v", name, ok, p)
		}
		b.apply(&info, p)
	}
	add("A1", rina, "Nasi goreng", time.Unix(2000, 0)) // its sender
	add("A2", budi, "Mie ayam", time.Unix(2001, 0))    // not allowed
	add("A3", rina, "Soto", time.Unix(2002, 0))        // already there
	add("A4", rina, "Pecel", time.Unix(6000, 0))       // after it ended

	byID := map[string]*model.Message{}
	for _, m := range b.Messages(group.String(), 10) {
		byID[m.ID] = m
	}
	q := byID["Q1"].Poll
	if q == nil || !q.Quiz || q.Correct != 1 || q.Max != 1 {
		t.Errorf("quiz %+v", q)
	}
	p := byID["P1"].Poll
	if p == nil || !p.HideVoters || !p.End.Equal(end) || p.AllowAdd || p.Quiz {
		t.Fatalf("poll %+v", p)
	}
	var names []string
	for _, o := range p.Options {
		names = append(names, o.Name)
	}
	if len(names) != 3 || names[2] != "Nasi goreng" || p.Options[2].AddedBy == "" {
		t.Errorf("options %v (%+v)", names, p.Options)
	}
	if !p.Ended(end) || p.Ended(end.Add(-time.Second)) {
		t.Error("Ended")
	}
}
