package ui

import (
	"image"
	"slices"
	"strings"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/io/pointer"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

// focusedKey is the key of the button that has the focus.
func (st *slashTest) focusedKey() string {
	fi := st.focus()
	for k, e := range st.u.clicks.m {
		if event.Tag(&e.Clickable) == fi.Tag {
			return k
		}
	}
	return ""
}

func (st *slashTest) enter() {
	st.r.Queue(key.Event{Name: key.NameReturn, State: key.Press}, key.Event{Name: key.NameReturn, State: key.Release})
	st.frame()
	st.frame()
	st.frame()
}

// TestSubmenuKeyboard goes into a chat's Mute notifications with the
// keyboard, back out, and mutes the chat.
func TestSubmenuKeyboard(t *testing.T) {
	st := newSlashTest(t, "rina")
	c := st.u.chatByID("work")
	if c.Muted {
		t.Fatal("the demo work chat is muted")
	}
	st.focusTag(st.u.rowClick(c))
	st.pressMod(key.NameF10, key.ModShift)
	st.settle()
	for range 20 {
		if st.focusedKey() == "menu:mute" {
			break
		}
		st.press(key.NameDownArrow)
	}
	if k := st.focusedKey(); k != "menu:mute" {
		t.Fatalf("the arrows didn't reach Mute notifications: %q", k)
	}
	for _, back := range []key.Name{key.NameLeftArrow, key.NameEscape} {
		st.press(key.NameRightArrow)
		st.settle()
		if k := st.focusedKey(); k != "sub:8h" {
			t.Fatalf("Right: focus on %q, want the submenu's first row", k)
		}
		// Up and Down go round the submenu's rows only.
		for _, want := range []string{"sub:week", "sub:always", "sub:8h"} {
			st.press(key.NameDownArrow)
			if k := st.focusedKey(); k != want {
				t.Fatalf("Down: focus on %q, want %q", k, want)
			}
		}
		st.press(back)
		st.settle()
		if k := st.focusedKey(); k != "menu:mute" || !st.u.ctx.isOpen() || st.u.ctx.sub.key != "" {
			t.Fatalf("%s: focus on %q, menu open %v, submenu %q", back, k, st.u.ctx.isOpen(), st.u.ctx.sub.key)
		}
	}
	st.enter()
	st.settle()
	if k := st.focusedKey(); k != "sub:8h" {
		t.Fatalf("Enter: focus on %q, want the submenu's first row", k)
	}
	st.press(key.NameUpArrow)
	st.enter()
	st.settle()
	if c := st.u.chatByID("work"); !c.Muted || !c.MuteUntil.IsZero() || st.u.ctx.isOpen() {
		t.Errorf("Always: muted %v until %v, menu open %v", c.Muted, c.MuteUntil, st.u.ctx.isOpen())
	}
	// A second Esc closes the menu.
	st.u.openChatMenu(st.u.chatByID("mom"))
	st.settle()
	st.press(key.NameEscape)
	if st.u.ctx.isOpen() {
		t.Error("Esc left the menu open")
	}
}

// TestSubmenuPointer opens submenus by resting the pointer on their rows
// in the chat's ⋮ menu, and puts the chat in a list.
func TestSubmenuPointer(t *testing.T) {
	st := newSlashTest(t, "rina")
	u := st.u
	u.mouse = image.Pt(700, 60)
	u.openConvMenu()
	st.settle()
	move := func(x, y float32) {
		st.r.Queue(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: f32.Pt(x, y)})
		st.frame()
		st.frame()
	}
	// The rows: the menu opens at 700,60 under the title bar, 9dp of
	// padding before the first, 40dp each.
	row := func(i int) float32 { return float32(60 + 9 + 40*i + 20 + u.kb.top) }
	keys := menuKeys(u.ctx.items)
	mute, lists := slices.Index(keys, "mute"), slices.Index(keys, "lists")
	move(750, row(mute))
	if u.ctx.sub.key != "" {
		t.Fatal("the submenu opened at once")
	}
	st.settle()
	if u.ctx.sub.key != "mute" {
		t.Fatalf("resting on Mute notifications opened %q", u.ctx.sub.key)
	}
	// Crossing another row doesn't close it, resting there does.
	move(750, row(mute+1))
	if u.ctx.sub.key != "mute" {
		t.Error("crossing a row closed the submenu")
	}
	st.settle()
	if u.ctx.sub.key != "" {
		t.Errorf("resting on another row left %q open", u.ctx.sub.key)
	}
	move(750, row(lists))
	st.settle()
	if u.ctx.sub.key != "lists" {
		t.Fatalf("resting on Add to list opened %q", u.ctx.sub.key)
	}
	u.btn("sub:l2").Click()
	st.frame()
	st.frame()
	if !slices.Contains(st.b.Lists()[1].Chats, "rina") || !u.ctx.isOpen() {
		t.Errorf("ticking Work: in %v, menu open %v", st.b.Lists()[1].Chats, u.ctx.isOpen())
	}
	if l := u.listsRow(u.selected).label; l != "Change list" {
		t.Errorf("a chat in a list is offered %q", l)
	}
	u.btn("sub:newlist").Click()
	st.frame()
	st.frame()
	if u.ctx.isOpen() || u.newChat.step != ncList {
		t.Error("New list didn't open its page")
	}
}

// TestSubmenuIdle: an open submenu asks for no frames.
func TestSubmenuIdle(t *testing.T) {
	st := newSlashTest(t, "rina")
	st.u.openConvMenu()
	st.u.ctx.sub.key = "lists"
	for range 300 {
		st.frame() // past what the chat animates as it opens
	}
	st.r.WakeupTime()
	st.frame()
	if at, ok := st.r.WakeupTime(); ok && at.Before(st.now.Add(50*time.Millisecond)) {
		t.Errorf("the next frame was asked for at %v, now %v", at, st.now)
	}
}

// TestSelfChat: the chat with yourself has no calls, mute, report or block.
func TestSelfChat(t *testing.T) {
	st := newSlashTest(t, "me@lid")
	u := st.u
	for _, keys := range [][]string{menuKeys(u.convMenuItems(u.selected)), menuKeys(u.chatMenuItems(u.selected))} {
		for _, k := range []string{"mute", "report", "block"} {
			if slices.Contains(keys, k) {
				t.Errorf("your own chat's menu has %s: %v", k, keys)
			}
		}
	}
	u.SelectID("rina")
	keys := menuKeys(u.convMenuItems(u.selected))
	want := []string{"info", "search", "select", "mute", "timer", "theme", "fav", "lists", "export", "close", "",
		"report", "block", "clear", "delete"}
	if !slices.Equal(keys, want) {
		t.Errorf("Rina's menu is %v, want %v", keys, want)
	}
}

// TestMessageMenuOrder checks the rows of WhatsApp Web's message menus.
func TestMessageMenuOrder(t *testing.T) {
	st := newSlashTest(t, "work")
	u := st.u
	text := &model.Message{ID: "t", ChatID: "work", SenderID: "clara", Sender: "Clara", Text: "hi", Time: testNow()}
	photo := &model.Message{ID: "p", ChatID: "work", SenderID: "clara", Sender: "Clara", Kind: model.KindImage,
		Media: model.MediaImage, Time: testNow()}
	own := &model.Message{ID: "o", ChatID: "work", FromMe: true, Receipt: model.Read, Text: "hi", Time: testNow().Add(-time.Hour)}
	waiting := &model.Message{ID: "w", ChatID: "work", Kind: model.KindWaiting, Time: testNow()}
	for _, c := range []struct {
		m    *model.Message
		want string
	}{
		{text, "reply private dm copy react forward pin star  report delete"},
		{photo, "reply private dm react save forward pin star  report delete"}, // not here yet: nothing to copy
		{own, "info reply copy react forward pin star  delete"},
		{waiting, "delete"},
	} {
		if got := strings.Join(menuKeys(u.messageMenuItems(u.selected, c.m)), " "); got != c.want {
			t.Errorf("message %s: %q, want %q", c.m.ID, got, c.want)
		}
	}
	for _, it := range u.messageMenuItems(u.selected, photo) {
		if it.key == "save" && it.label != "Download" {
			t.Errorf("saving media is %q", it.label)
		}
	}
}

// TestForwardStays: Forward is always beside a photo, and a sticker's and
// React wait for the hover.
func TestForwardStays(t *testing.T) {
	st := newSlashTest(t, "rina")
	u := st.u
	st.settle()
	var photo *model.Message
	for _, m := range u.msgs {
		if forwardStays(m) && m.Kind != model.KindViewOnce {
			photo = m
		}
	}
	if photo == nil {
		t.Fatal("no photo")
	}
	u.jumpTo(photo.ID)
	st.settle()
	if n := u.btn("sfwd:" + photo.ID); !st.drawn(n) {
		t.Error("no Forward beside the photo")
	}
	if n := u.btn("sreact:" + photo.ID); st.drawn(n) {
		t.Error("React shows without a hover")
	}
	if forwardStays(&model.Message{Kind: model.KindSticker, Media: model.MediaSticker}) {
		t.Error("a sticker's Forward stays")
	}
}

// drawn reports whether c was drawn last frame: only then can it take
// the focus.
func (st *slashTest) drawn(c event.Tag) bool {
	st.focusTag(c)
	return st.r.Source().Focused(c)
}

// TestEventStart checks the start Create event suggests.
func TestEventStart(t *testing.T) {
	for _, c := range [][2]string{
		{"2026-11-25 14:01", "2026-11-26 14:30"},
		{"2026-11-25 14:30", "2026-11-26 15:00"},
		{"2026-12-31 23:45", "2027-01-02 00:00"},
	} {
		now, err := time.ParseInLocation("2006-01-02 15:04", c[0], time.FixedZone("NPT", 5*3600+45*60))
		if err != nil {
			t.Fatal(err)
		}
		if got := eventStart(now).Format("2006-01-02 15:04"); got != c[1] {
			t.Errorf("at %s the start is %s, want %s", c[0], got, c[1])
		}
	}
}
