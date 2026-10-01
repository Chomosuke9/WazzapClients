package wa

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/polymorfa/hypermeow/types"
)

// TestCommonGroups checks that the groups shared with a contact are found
// under either their LID or phone JID, without communities or their
// announcement groups, and that leaving a group takes it off the list.
func TestCommonGroups(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	s := &msgStore{db: db}
	if err := s.init(ctx); err != nil {
		t.Fatal(err)
	}
	lid := types.NewJID("111", types.HiddenUserServer)
	pn := types.NewJID("62811", types.DefaultUserServer)
	other := types.NewJID("222", types.HiddenUserServer)
	group := func(id, name string, f func(g *types.GroupInfo), who ...types.GroupParticipant) {
		g := &types.GroupInfo{JID: types.NewJID(id, types.GroupServer), GroupName: types.GroupName{Name: name}, Participants: who}
		if f != nil {
			f(g)
		}
		if err := s.setGroupShape(ctx, g); err != nil {
			t.Fatal(err)
		}
		if err := s.setMembers(ctx, g); err != nil {
			t.Fatal(err)
		}
	}
	comm := types.NewJID("c", types.GroupServer)
	group("c", "Community", func(g *types.GroupInfo) { g.IsParent = true }, types.GroupParticipant{JID: lid})
	group("ann", "Announcements", func(g *types.GroupInfo) { g.LinkedParentJID, g.IsDefaultSubGroup = comm, true },
		types.GroupParticipant{JID: lid})
	group("sub", "Sub group", func(g *types.GroupInfo) { g.LinkedParentJID = comm }, types.GroupParticipant{JID: lid})
	group("pn", "By phone", nil, types.GroupParticipant{JID: pn})
	group("lidpn", "LID with phone", nil, types.GroupParticipant{JID: lid, PhoneNumber: pn})
	group("none", "Without them", nil, types.GroupParticipant{JID: other})

	names := func() map[string]string {
		out := map[string]string{}
		for _, g := range s.commonGroups(ctx, lid.String(), pn.String()) {
			out[g.name] = g.community
		}
		return out
	}
	got := names()
	want := map[string]string{"Sub group": "Community", "By phone": "", "LID with phone": ""}
	if len(got) != len(want) {
		t.Fatalf("common groups %v, want %v", got, want)
	}
	for k, v := range want {
		if c, ok := got[k]; !ok || c != v {
			t.Errorf("common groups %v, want %v", got, want)
		}
	}
	s.updateMembers(ctx, types.NewJID("pn", types.GroupServer).String(), nil, []types.JID{pn})
	if _, ok := names()["By phone"]; ok {
		t.Error("a group they left is still shared")
	}
	s.clearMembers(ctx, types.NewJID("lidpn", types.GroupServer).String())
	s.keepMembers(ctx, []string{types.NewJID("lidpn", types.GroupServer).String(), types.NewJID("none", types.GroupServer).String()})
	if got := names(); len(got) != 0 {
		t.Errorf("groups you left are still shared: %v", got)
	}
}
