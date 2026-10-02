package ui

import (
	"testing"

	"github.com/chomosuke9/wazzapclients/internal/mock"
)

// TestInviteJoin checks the invite dialog with the demo groups: joining an
// open group opens its chat, and asking to join one that needs an admin's
// approval only closes the dialog.
func TestInviteJoin(t *testing.T) {
	u := New(mock.New())
	u.Start(func() {})
	u.applyEvents()

	u.openInvite("DemoInviteReuni")
	u.applyEvents()
	in := &u.dialog.invite
	if in.group == nil || !in.group.Approval {
		t.Fatalf("reuni: group %+v, err %q; want one that needs approval", in.group, in.err)
	}
	u.backend.JoinGroup(in.code)
	u.applyEvents()
	if u.dialog.isOpen() {
		t.Error("reuni: the dialog stayed open after the request")
	}
	if u.chatByID("reuni@g.us") != nil {
		t.Error("reuni: joined without an admin's approval")
	}

	u.openInvite("DemoInviteFutsal")
	u.applyEvents()
	if g := u.dialog.invite.group; g == nil || g.Approval || g.Member {
		t.Fatalf("futsal: group %+v; want one to join", g)
	}
	u.backend.JoinGroup("DemoInviteFutsal")
	u.applyEvents()
	if u.selected == nil || u.selected.ID != "futsal@g.us" {
		t.Errorf("futsal: open chat %v after joining, want futsal@g.us", u.selected)
	}

	// Once you're in, the link opens the chat.
	u.selected = nil
	u.openInvite("DemoInviteFutsal")
	u.applyEvents()
	if u.dialog.isOpen() || u.selected == nil || u.selected.ID != "futsal@g.us" {
		t.Error("futsal: the link of a group you're in didn't open its chat")
	}

	u.openInvite("Nope")
	u.applyEvents()
	if u.dialog.invite.err == "" {
		t.Error("an unknown code shows no error")
	}
}
