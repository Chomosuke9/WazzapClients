package ui

import (
	"image"
	"testing"
	"time"

	"gioui.org/io/input"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/chomosuke9/wazzapclients/internal/mock"
	"github.com/chomosuke9/wazzapclients/internal/model"
)

func (u *UI) statusThread(id string) *model.StatusThread {
	for _, t := range u.statuses {
		if t.ID == id {
			return t
		}
	}
	return nil
}

// TestHideStatus hides a contact's status updates from its menu, after
// asking, and shows them again.
func TestHideStatus(t *testing.T) {
	u := New(mock.New())
	u.Start(func() {})
	u.setPage(pageStatus)
	items := u.statusRowItems("budi")
	if len(items) != 1 || items[0].label != "Hide" {
		t.Fatalf("menu = %+v", items)
	}
	items[0].run()
	if !u.dialog.isOpen() || u.statusThread("budi").Muted {
		t.Fatal("Hide didn't ask first")
	}
	u.dialog.buttons[0].run()
	u.applyEvents()
	if !u.statusThread("budi").Muted {
		t.Fatal("not hidden")
	}
	if items := u.statusRowItems("budi"); len(items) != 1 || items[0].label != "Unhide" {
		t.Fatalf("hidden menu = %+v", items)
	}
	u.statusRowItems("budi")[0].run()
	u.applyEvents()
	if u.statusThread("budi").Muted {
		t.Error("still hidden")
	}
	if items := u.statusRowItems("me"); items != nil {
		t.Errorf("your own status has a menu: %+v", items)
	}
}

// TestStatusAudiences goes through Status privacy: choosing an audience,
// one without people opening its people first, making a new audience
// and deleting it.
func TestStatusAudiences(t *testing.T) {
	b := mock.New()
	u := New(b)
	u.Start(func() {})
	u.setPage(pageStatus)
	u.openStatusPrivacy()
	if u.status.privacy.top() != spMain || !u.status.privacy.p.Editable {
		t.Fatalf("panel = %+v", u.status.privacy.stack)
	}

	u.chooseAudience(model.AudienceContacts, -1)
	if p := b.StatusPrivacy(); p.Audience != model.AudienceContacts {
		t.Errorf("audience = %v, want contacts", p.Audience)
	}

	// Only share with, empty: its people open first; it's chosen once
	// someone is picked.
	u.chooseAudience(model.AudienceOnly, -1)
	sp := &u.status.privacy
	if sp.top() != spPeople || !sp.choose || b.StatusPrivacy().Audience != model.AudienceContacts {
		t.Fatalf("empty Only share with: step %v, choose %v", sp.top(), sp.choose)
	}
	u.saveAudiencePeople()
	if sp.top() != spPeople {
		t.Fatal("saved with nobody picked")
	}
	sp.picked = []string{"budi"}
	u.saveAudiencePeople()
	if p := b.StatusPrivacy(); p.Audience != model.AudienceOnly || len(p.Only) != 1 || sp.top() != spMain {
		t.Errorf("only = %+v, step %v", p, sp.top())
	}

	// A new audience: its name and emoji, then its people; it's chosen.
	u.openAudienceName(-1)
	if sp.top() != spName || sp.emoji != newAudienceEmoji {
		t.Fatalf("new audience: step %v, emoji %q", sp.top(), sp.emoji)
	}
	sp.name.SetText("Gym")
	sp.emoji = "💪"
	u.saveAudienceName()
	if sp.top() != spPeople {
		t.Fatalf("after the name: step %v", sp.top())
	}
	sp.picked = []string{"agus", "yoga"}
	u.saveAudiencePeople()
	p := b.StatusPrivacy()
	l := p.List(p.ListID)
	if p.Audience != model.AudienceList || l == nil || l.Name != "Gym" || l.Emoji != "💪" || len(l.Members) != 2 {
		t.Fatalf("new audience = %+v", p)
	}

	// Deleting the chosen audience goes back to all contacts.
	i := len(p.Lists) - 1
	u.confirmDeleteAudience(i)
	u.dialog.buttons[0].run()
	if p := b.StatusPrivacy(); p.Audience != model.AudienceContacts || len(p.Lists) != i {
		t.Errorf("after delete = %+v", p)
	}

	// Back closes the pages one by one.
	u.openAudiencePeople(model.AudienceExcept, -1, false)
	u.statusPrivacyBack()
	u.statusPrivacyBack()
	if sp.open() {
		t.Errorf("still open: %v", sp.stack)
	}
}

// TestUnknownAudience checks that an audience whose people the phone
// hasn't synced isn't taken for one of nobody: choosing it opens its
// people first.
func TestUnknownAudience(t *testing.T) {
	b := mock.New()
	p := *b.StatusPrivacy()
	p.Unknown = []model.StatusAudience{model.AudienceExcept}
	b.SetStatusPrivacy(p)
	u := New(b)
	u.Start(func() {})
	u.setPage(pageStatus)
	u.openStatusPrivacy()
	before := b.StatusPrivacy().Audience
	u.chooseAudience(model.AudienceExcept, -1)
	sp := &u.status.privacy
	if sp.top() != spPeople || !sp.choose || b.StatusPrivacy().Audience != before {
		t.Fatalf("unknown except: step %v, choose %v, audience %v", sp.top(), sp.choose, b.StatusPrivacy().Audience)
	}
	sp.picked = []string{"bank"}
	u.saveAudiencePeople()
	if got := b.StatusPrivacy(); got.Audience != model.AudienceExcept || !got.Knows(model.AudienceExcept) || len(got.Except) != 1 {
		t.Errorf("after picking = %+v", got)
	}
}

// TestStatusPrivacyFrames lays the panel's pages out: each covers the
// status list once it has slid in, and the list shows again after.
func TestStatusPrivacyFrames(t *testing.T) {
	u := New(mock.New())
	u.Start(func() {})
	u.setPage(pageStatus)
	now := testNow()
	var ops op.Ops
	var r input.Router
	frames := func(n int) {
		for range n {
			ops.Reset()
			u.Layout(layout.Context{Ops: &ops, Now: now, Source: r.Source(), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1},
				Constraints: layout.Exact(image.Pt(1100, 700))})
			r.Frame(&ops)
			now = now.Add(20 * time.Millisecond)
		}
	}
	u.openStatusPrivacy()
	frames(30)
	if !u.statusPrivacyCovers() {
		t.Fatal("Status privacy doesn't cover the list")
	}
	u.openAudiencePeople(model.AudienceList, 0, false)
	frames(30)
	u.openAudienceName(0)
	frames(30)
	u.openPicker(pickAudience, nil)
	u.pickEmoji("🎉")
	if u.status.privacy.emoji != "🎉" {
		t.Errorf("emoji = %q", u.status.privacy.emoji)
	}
	for u.status.privacy.open() {
		u.statusPrivacyBack()
	}
	frames(30)
	if u.statusPrivacyCovers() {
		t.Error("the panel stayed")
	}
}
