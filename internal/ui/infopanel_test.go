package ui

import (
	"image"
	"slices"
	"testing"
	"time"

	"gioui.org/io/input"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/chomosuke9/wazzapclients/internal/mock"
	"github.com/chomosuke9/wazzapclients/internal/model"
)

// infoTest is the demo window with a chat's info panel open.
type infoTest struct {
	u   *UI
	now time.Time
	ops op.Ops
	r   input.Router
}

func newInfoTest(t *testing.T, chat string) *infoTest {
	t.Helper()
	it := &infoTest{u: New(mock.New()), now: testNow()}
	it.u.Start(func() {})
	it.u.SelectID(chat)
	it.u.openInfo(chat)
	it.u.info.anim.snap(true)
	it.frame()
	it.frame()
	return it
}

func (it *infoTest) frame() {
	it.ops.Reset()
	it.u.Layout(layout.Context{Ops: &it.ops, Now: it.now, Source: it.r.Source(), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1},
		Constraints: layout.Exact(image.Pt(1400, 900))})
	it.r.Frame(&it.ops)
	it.now = it.now.Add(10 * time.Millisecond)
}

func (it *infoTest) click(key string) {
	it.u.btn(key).Click()
	it.frame()
	it.frame()
}

// TestInfoMute turns a contact's mute switch on, which asks for how long,
// and off; a group keeps its Notification settings.
func TestInfoMute(t *testing.T) {
	it := newInfoTest(t, "rina")
	u := it.u
	u.backend.SetMuted("rina", false, 0)
	it.frame()
	it.click("info:mute")
	if u.ctx.kind != ctxMute || !u.ctx.isOpen() || u.chatByID("rina").Muted {
		t.Fatalf("the switch opened menu %v, muted %v", u.ctx.kind, u.chatByID("rina").Muted)
	}
	u.closeMenu()
	u.backend.SetMuted("rina", true, 0)
	it.frame()
	it.frame()
	it.click("info:mute")
	if u.chatByID("rina").Muted {
		t.Error("the switch didn't unmute the chat")
	}
	it.click("info:notif")
	if u.ctx.isOpen() {
		t.Error("a contact's info still has Notification settings")
	}
}

// TestTimerPage picks a timer on the Disappearing messages page.
func TestTimerPage(t *testing.T) {
	it := newInfoTest(t, "rina")
	u := it.u
	it.click("info:disappearing")
	if u.info.sub != infoTimer || u.ctx.isOpen() {
		t.Fatalf("Disappearing messages opened page %q, menu %v", u.info.sub, u.ctx.isOpen())
	}
	it.click("info:timer:7 days")
	if got := u.backend.Info("rina").Disappearing; got != uint32(7*24*time.Hour/time.Second) {
		t.Errorf("timer = %d s, want 7 days", got)
	}
	it.click("info:timer:default")
	if u.page != pageSettings || u.settings.detail != settingPrivacy+1 || u.settings.sub != "timer" {
		t.Errorf("default message timer opened page %v, settings %d %q", u.page, u.settings.detail, u.settings.sub)
	}
}

// TestRestrictedPage opens Restricted chat, a page that says so and no more.
func TestRestrictedPage(t *testing.T) {
	it := newInfoTest(t, "rina")
	u := it.u
	it.click("info:privacy")
	if u.info.sub != infoRestricted || u.dialog.isOpen() {
		t.Fatalf("Restricted chat opened page %q, dialog %v", u.info.sub, u.dialog.isOpen())
	}
	u.info.closeBtn.Click()
	it.frame()
	if u.info.sub != "" || !u.info.open {
		t.Errorf("back left page %q, open %v", u.info.sub, u.info.open)
	}
}

// TestChatMediaPage opens a chat's Media, links and docs in its panel, goes
// through the tabs and from there to the Media panel.
func TestChatMediaPage(t *testing.T) {
	it := newInfoTest(t, "family")
	u := it.u
	it.click("info:media")
	g := &u.info.gallery
	if u.info.sub != infoGallery || u.gallery.open {
		t.Fatalf("Media, links and docs opened page %q, the Media panel %v", u.info.sub, u.gallery.open)
	}
	if len(g.list.msgs) == 0 {
		t.Fatal("no media")
	}
	for _, m := range g.list.msgs {
		if m.ChatID != "family" {
			t.Errorf("the page lists %q's %s", m.ChatID, m.ID)
		}
	}
	it.click("info:gal:m:" + galleryKey(g.list.msgs[0]))
	if !u.viewer.open {
		t.Error("a click on a picture didn't open it")
	}
	u.hideViewer()
	it.click("info:gal:tab:" + itoa(int(model.GalleryDocs)))
	for _, m := range g.list.msgs {
		if m.Media != model.MediaDocument {
			t.Errorf("the Docs tab lists %+v", m)
		}
	}
	it.click("info:gal:all")
	if !u.gallery.open || u.gallery.chatID != "" || u.gallery.tab != model.GalleryDocs {
		t.Errorf("View media from all chats: open %v, chat %q, tab %v", u.gallery.open, u.gallery.chatID, u.gallery.tab)
	}
	u.closeGallery()
	u.info.closeBtn.Click()
	it.frame()
	if u.info.sub != "" || g.list.msgs != nil {
		t.Errorf("back left page %q with %d messages", u.info.sub, len(g.list.msgs))
	}
}

// TestMediaRows groups media by month and leaves the other tabs alone.
func TestMediaRows(t *testing.T) {
	now := time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC)
	var msgs []*model.Message
	for _, d := range []time.Time{now, now, now, now, now.AddDate(0, -1, 0), now.AddDate(-1, 0, 0)} {
		msgs = append(msgs, &model.Message{Time: d})
	}
	want := []mediaRow{{heading: "THIS MONTH"}, {"", 0, 3}, {"", 3, 4}, {heading: "OCTOBER"}, {"", 4, 5},
		{heading: "NOVEMBER 2025"}, {"", 5, 6}}
	if got := mediaRows(msgs, model.GalleryMedia, 3, now); !slices.Equal(got, want) {
		t.Errorf("media rows %v, want %v", got, want)
	}
	if got := mediaRows(msgs[:2], model.GalleryDocs, 3, now); !slices.Equal(got, []mediaRow{{"", 0, 1}, {"", 1, 2}}) {
		t.Errorf("document rows %v", got)
	}
}

// TestGroupAdminsPage changes who administers a group, once saved.
func TestGroupAdminsPage(t *testing.T) {
	it := newInfoTest(t, "work")
	u := it.u
	admin := func(id string) bool {
		for _, m := range u.backend.Info("work").Members {
			if m.ID == id {
				return m.Admin
			}
		}
		return false
	}
	var id string
	for _, m := range u.backend.Info("work").Members {
		if !m.Me && !m.SuperAdmin {
			id = m.ID
		}
	}
	was := admin(id)
	u.openInfoSub(infoPerms)
	it.frame()
	it.click("info:admins")
	if u.info.sub != infoAdmins {
		t.Fatalf("Edit group admins opened page %q", u.info.sub)
	}
	it.click("admin:" + id)
	if admin(id) != was || len(u.info.admins) != 1 {
		t.Fatalf("a tick alone changed the group (%v) or wasn't kept: %v", admin(id) != was, u.info.admins)
	}
	it.click("admins:save")
	if admin(id) == was || u.info.sub != infoPerms {
		t.Errorf("saved: admin still %v, page %q", was, u.info.sub)
	}
	u.info.closeBtn.Click()
	it.frame()
	if u.info.sub != "" {
		t.Errorf("back from the permissions left page %q", u.info.sub)
	}
}

// TestSelfInfo: the chat with yourself has nobody to mute, block or report.
func TestSelfInfo(t *testing.T) {
	u := New(mock.New())
	u.Start(func() {})
	c := u.chatByID("rina")
	info := &model.ChatInfo{ID: c.ID, Name: c.Name}
	var r input.Router
	rows := func(self bool) int {
		c.Self = self
		return len(u.contactRows(layout.Context{Ops: new(op.Ops), Source: r.Source()}, c, info))
	}
	if self, other := rows(true), rows(false); other-self != 3 {
		t.Errorf("%d rows for yourself and %d for someone else, want 3 fewer", self, other)
	}
}
