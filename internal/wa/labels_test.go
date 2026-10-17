package wa

import (
	"testing"

	"github.com/polymorfa/hypermeow/proto/waE2E"
	"google.golang.org/protobuf/proto"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

// TestMemberLabels checks that a group member's label comes from their
// messages and label changes, the newest winning, and shows on all their
// messages, older ones too.
func TestMemberLabels(t *testing.T) {
	b := testBackend(t)
	const chat = "123@g.us"
	label := func(l string, ts int64) *waE2E.MemberLabel {
		return &waE2E.MemberLabel{Label: proto.String(l), LabelTimestamp: proto.Int64(ts)}
	}
	text := func(id string, ml *waE2E.MemberLabel) {
		b.onMessage(snippetEvent(id, &waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{
			Text: proto.String(id), ContextInfo: &waE2E.ContextInfo{MemberLabel: ml}}}))
	}
	shown := func(id string) string {
		t.Helper()
		r, ok := b.store.message(b.ctx, chat, id)
		if !ok {
			t.Fatalf("%s isn't stored", id)
		}
		return b.resolve(b.ctx, r, true).SenderLabel
	}
	changed := func() (out []string) {
		for _, e := range b.Poll() {
			if l, ok := e.(model.LabelEvent); ok {
				out = append(out, l.Label)
			}
		}
		return out
	}

	text("first", label("Tyrant", 1_700_000_000))
	if got := shown("first"); got != "Tyrant" {
		t.Errorf("label %q, want Tyrant", got)
	}
	if got := changed(); len(got) != 1 {
		t.Errorf("label events %q, want one", got)
	}
	text("second", label("Tyrant", 1_700_000_000))
	if got := changed(); len(got) != 0 {
		t.Errorf("the same label again changed it: %q", got)
	}

	b.onMessage(snippetEvent("change", &waE2E.Message{ProtocolMessage: &waE2E.ProtocolMessage{
		Type: waE2E.ProtocolMessage_GROUP_MEMBER_LABEL_CHANGE.Enum(), MemberLabel: label("Boss", 1_700_000_100_000)}})) // ms
	if got := changed(); len(got) != 1 || got[0] != "Boss" {
		t.Errorf("label events %q, want Boss", got)
	}
	if _, ok := b.store.message(b.ctx, chat, "change"); ok {
		t.Error("a label change was stored as a message")
	}
	// A message sent before the change, delivered late, keeps the newer one.
	text("late", label("Tyrant", 1_700_000_000))
	for _, id := range []string{"first", "second", "late"} {
		if got := shown(id); got != "Boss" {
			t.Errorf("%s: label %q, want Boss", id, got)
		}
	}

	b.onMessage(snippetEvent("removed", &waE2E.Message{ProtocolMessage: &waE2E.ProtocolMessage{
		Type: waE2E.ProtocolMessage_GROUP_MEMBER_LABEL_CHANGE.Enum(), MemberLabel: label("", 1_700_000_200)}}))
	if got := shown("first"); got != "" {
		t.Errorf("removed label still shows as %q", got)
	}

	b.labels = nil // as after a restart
	if got := shown("first"); got != "" {
		t.Errorf("after a reload, label %q, want none", got)
	}
}
