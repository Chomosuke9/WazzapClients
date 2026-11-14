package ui

import (
	"testing"

	"github.com/chomosuke9/wazzapclients/internal/mock"
	"github.com/chomosuke9/wazzapclients/internal/model"
)

// The demo's status mention opens the status it names, and the viewer's
// "Mentioned privately" leads back to it.
func TestStatusMentionOpensStatus(t *testing.T) {
	u := New(mock.New())
	u.Start(func() {})
	u.SelectID("work")
	var m *model.Message
	for _, x := range u.msgs {
		if x.StatusMention != nil {
			m = x
		}
	}
	if m == nil || !m.StatusMention.Found || !m.StatusMention.Group {
		t.Fatalf("no status mention in work: %+v", m)
	}
	if got := mediaLabel(m); got != "This group was mentioned." {
		t.Errorf("preview %q", got)
	}
	u.openMentionedStatus(m)
	v := &u.status.viewer
	if !v.isOpen() {
		t.Fatal("viewer not open")
	}
	up := v.thread.Updates[v.index]
	if up.ID != m.StatusMention.StatusID || up.MentionChat != "work" || up.MentionMsg != m.ID {
		t.Fatalf("opened %+v", up)
	}
	u.openStatusMention(up)
	if v.isOpen() || u.selected == nil || u.selected.ID != "work" {
		t.Fatalf("mention didn't lead back to the chat (open %v, chat %v)", v.isOpen(), u.selected)
	}
}

// The viewer's heart likes the update shown, and takes the like back.
func TestStatusLike(t *testing.T) {
	u := New(mock.New())
	u.Start(func() {})
	for _, th := range u.statuses {
		if !th.Mine && !th.Group {
			u.status.viewer.show(th)
			break
		}
	}
	v := &u.status.viewer
	up := v.thread.Updates[v.index]
	u.toggleStatusLike()
	if !up.Liked {
		t.Fatal("not liked")
	}
	u.toggleStatusLike()
	if up.Liked {
		t.Fatal("like not taken back")
	}
}
