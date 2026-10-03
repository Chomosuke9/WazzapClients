package wa

import (
	"testing"

	"github.com/polymorfa/hypermeow/types"
	"github.com/polymorfa/hypermeow/types/events"
)

// TestJoinCommunity checks that joining a community stores its parent group
// and announcements in their places, so neither lists as a plain group, and
// that groups linked to it later (or unlinked) move in and out.
func TestJoinCommunity(t *testing.T) {
	b := testBackend(t)
	comm := types.NewJID("c", types.GroupServer)
	ann := types.NewJID("ann", types.GroupServer)
	join := func(g types.GroupInfo) { b.handle(&events.JoinedGroup{GroupInfo: g}) }
	join(types.GroupInfo{JID: comm, GroupName: types.GroupName{Name: "Community"}, GroupParent: types.GroupParent{IsParent: true}})
	join(types.GroupInfo{JID: ann, GroupName: types.GroupName{Name: "Community"},
		GroupLinkedParent: types.GroupLinkedParent{LinkedParentJID: comm}, GroupIsDefaultSub: types.GroupIsDefaultSub{IsDefaultSubGroup: true}})

	for _, c := range b.Chats() {
		if c.ID == comm.String() {
			t.Error("the community's parent group lists as a chat")
		}
	}
	cs := b.Communities()
	if len(cs) != 1 || cs[0].Announcements != ann.String() || len(cs[0].Groups) != 0 {
		t.Fatalf("communities = %+v, want one with announcements %s and no groups", cs, ann)
	}

	sub := types.NewJID("sub", types.GroupServer)
	join(types.GroupInfo{JID: sub, GroupName: types.GroupName{Name: "Sub"}})
	link := &types.GroupLinkChange{Type: types.GroupLinkChangeTypeSub, Group: types.GroupLinkTarget{JID: sub}}
	b.handle(&events.GroupInfo{JID: comm, Link: link})
	if cs := b.Communities(); len(cs) != 1 || len(cs[0].Groups) != 1 || cs[0].Groups[0] != sub.String() {
		t.Fatalf("after linking, communities = %+v, want %s in it", cs, sub)
	}
	b.handle(&events.GroupInfo{JID: comm, Unlink: link})
	if cs := b.Communities(); len(cs) != 1 || len(cs[0].Groups) != 0 {
		t.Fatalf("after unlinking, communities = %+v, want no groups", cs)
	}
}

// TestRename checks that a chat's stored name is only reported changed when
// it is.
func TestRename(t *testing.T) {
	b := testBackend(t)
	jid := types.NewJID("111", types.HiddenUserServer).String()
	if err := b.store.ensureChat(b.ctx, b.db, jid, false, "~Push"); err != nil {
		t.Fatal(err)
	}
	if !b.store.rename(b.ctx, jid, "Saved") {
		t.Error("renaming to a new name reports no change")
	}
	if b.store.rename(b.ctx, jid, "Saved") {
		t.Error("renaming to the same name reports a change")
	}
	if b.store.rename(b.ctx, types.NewJID("222", types.HiddenUserServer).String(), "X") {
		t.Error("renaming a chat that isn't stored reports a change")
	}
	if c := b.chat(jid); c == nil || c.Name != "Saved" {
		t.Errorf("stored chat = %+v, want named Saved", c)
	}
}
