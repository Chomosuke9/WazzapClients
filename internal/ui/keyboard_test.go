package ui

import (
	"testing"

	"gioui.org/f32"
	"gioui.org/io/event"
	"gioui.org/io/input"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/widget"
)

// tab presses Tab (or Shift+Tab) as app.Window does: the key goes to
// whoever asks for it, and otherwise moves the focus.
func (st *slashTest) tab(back bool) {
	e := key.Event{Name: key.NameTab, State: key.Press}
	dir := key.FocusForward
	if back {
		e.Modifiers, dir = key.ModShift, key.FocusBackward
	}
	st.r.WakeupTime() // forget the frames animations asked for
	st.r.Queue(input.SystemEvent{Event: e})
	if _, handled := st.r.WakeupTime(); !handled {
		st.r.MoveFocus(dir)
	}
	st.frame()
	st.frame()
}

func (st *slashTest) pressMod(name key.Name, mods key.Modifiers) {
	st.r.Queue(key.Event{Name: name, Modifiers: mods, State: key.Press})
	st.frame()
	st.frame()
}

func (st *slashTest) focus() input.FocusInfo { return st.r.Source().Focus() }

// focusTag focuses tag as the keyboard would have.
func (st *slashTest) focusTag(tag event.Tag) {
	st.r.Queue(key.Event{Name: key.NameShift, State: key.Press}) // the keyboard's in use
	st.r.Source().Execute(key.FocusCmd{Tag: tag})
	st.frame()
	st.frame()
}

func (st *slashTest) settle() {
	for range 40 {
		st.frame()
	}
}

// TestFocusRing: Tab focuses something with bounds and shows the ring; a
// click elsewhere drops a button's focus.
func TestFocusRing(t *testing.T) {
	st := newSlashTest(t, "rina")
	st.u.requestFocus(nil)
	st.frame()
	st.tab(false)
	fi := st.focus()
	if fi.Tag == nil || fi.Bounds.Empty() || !fi.Keyboard {
		t.Fatalf("Tab: focus %+v", fi)
	}
	st.r.Queue(pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary, Position: f32.Pt(700, 300)},
		pointer.Event{Kind: pointer.Release, Source: pointer.Mouse, Position: f32.Pt(700, 300)})
	st.frame()
	st.frame()
	if fi := st.focus(); fi.Keyboard || (fi.Tag != nil && !isEditor(fi.Tag)) {
		t.Fatalf("after a click: focus %+v", fi)
	}
}

// TestKeyboardChatMenu: Shift+F10 on a chat row opens its menu with the
// focus inside; Tab stays in the menu, the arrows move, and Esc gives the
// focus back to the row.
func TestKeyboardChatMenu(t *testing.T) {
	st := newSlashTest(t, "rina")
	c := st.u.sidebar.visible[1]
	row := st.u.rowClick(c)
	st.focusTag(row)
	if !st.r.Source().Focused(row) {
		t.Fatal("the row didn't take the focus")
	}
	st.pressMod(key.NameF10, key.ModShift)
	st.settle()
	if st.u.ctx.kind != ctxChat || st.u.ctx.chatID != c.ID {
		t.Fatalf("menu %v for %q, want the chat menu of %q", st.u.ctx.kind, st.u.ctx.chatID, c.ID)
	}
	inMenu := func() string {
		fi := st.focus()
		for k, e := range st.u.clicks.m {
			if event.Tag(&e.Clickable) == fi.Tag && len(k) > 5 && k[:5] == "menu:" {
				return k
			}
		}
		return ""
	}
	first := inMenu()
	if first == "" {
		t.Fatalf("the focus isn't in the menu: %+v", st.focus())
	}
	seen := map[string]bool{}
	for range 20 {
		st.tab(false)
		k := inMenu()
		if k == "" {
			t.Fatalf("Tab left the menu: %+v", st.focus())
		}
		seen[k] = true
	}
	if len(seen) < 3 {
		t.Fatalf("Tab went through %d rows", len(seen))
	}
	before := inMenu()
	st.press(key.NameDownArrow)
	if k := inMenu(); k == "" || k == before {
		t.Fatalf("Down: %q, was %q", k, before)
	}
	st.press(key.NameEscape)
	st.settle()
	if st.u.ctx.isOpen() {
		t.Fatal("Esc left the menu open")
	}
	if !st.r.Source().Focused(row) {
		t.Fatalf("the focus didn't come back to the row: %+v", st.focus())
	}
}

// TestKeyboardChatList: the arrows move along the chat list and Enter
// opens a chat.
func TestKeyboardChatList(t *testing.T) {
	st := newSlashTest(t, "rina")
	chats := st.u.sidebar.visible
	st.focusTag(st.u.rowClick(chats[0]))
	st.press(key.NameDownArrow)
	if !st.r.Source().Focused(st.u.rowClick(chats[1])) {
		t.Fatalf("Down didn't reach the second chat: %+v", st.focus())
	}
	st.press(key.NameEnd)
	st.settle()
	if last := chats[len(chats)-1]; !st.r.Source().Focused(st.u.rowClick(last)) {
		t.Fatalf("End didn't reach the last chat: %+v", st.focus())
	}
	st.press(key.NameHome)
	st.settle()
	st.press(key.NameDownArrow)
	st.r.Queue(key.Event{Name: key.NameReturn, State: key.Press}, key.Event{Name: key.NameReturn, State: key.Release})
	st.settle()
	if st.u.selected == nil || st.u.selected.ID != chats[1].ID {
		t.Fatalf("Enter opened %v, want %s", st.u.selected, chats[1].ID)
	}
}

// TestKeyboardMessages: Ctrl+Alt+Up focuses the newest message, Up the one
// before, Enter opens its menu and Esc goes back to the composer.
func TestKeyboardMessages(t *testing.T) {
	st := newSlashTest(t, "rina")
	st.settle()
	rows := st.u.kb.msgRows
	if len(rows) < 2 {
		t.Fatalf("%d messages", len(rows))
	}
	st.pressMod(key.NameUpArrow, key.ModShortcut|key.ModAlt)
	st.settle()
	if !st.r.Source().Focused(st.u.msgFocusTag(rows[len(rows)-1])) {
		t.Fatalf("not on the newest message: %+v", st.focus())
	}
	st.press(key.NameUpArrow)
	st.settle()
	prev := rows[len(rows)-2]
	if !st.r.Source().Focused(st.u.msgFocusTag(prev)) {
		t.Fatalf("Up didn't reach the message before: %+v", st.focus())
	}
	st.press(key.NameReturn)
	st.settle()
	if st.u.ctx.kind != ctxMessage || st.u.ctx.msg == nil || st.u.ctx.msg.ID != prev {
		t.Fatalf("Enter: menu %v", st.u.ctx.kind)
	}
	st.press(key.NameEscape)
	st.settle()
	if !st.r.Source().Focused(st.u.msgFocusTag(prev)) {
		t.Fatalf("closing the menu lost the message: %+v", st.focus())
	}
	st.press(key.NameEscape)
	st.settle()
	if !st.r.Source().Focused(&st.u.conv.composer) {
		t.Fatalf("Esc didn't go back to the composer: %+v", st.focus())
	}
}

// TestDialogTrap: Tab goes round a dialog's buttons only.
func TestDialogTrap(t *testing.T) {
	st := newSlashTest(t, "rina")
	st.u.confirmClearChat("rina")
	st.settle()
	buttons := map[event.Tag]bool{}
	for range 8 {
		st.tab(false)
		fi := st.focus()
		c, ok := fi.Tag.(*widget.Clickable)
		if !ok {
			continue // the dialog's checkbox, say
		}
		for k, e := range st.u.clicks.m {
			if &e.Clickable == c && (len(k) < 7 || k[:7] != "dialog:") {
				t.Fatalf("Tab left the dialog for %q", k)
			}
		}
		buttons[fi.Tag] = true
	}
	if len(buttons) < 2 {
		t.Fatalf("Tab went through %d buttons", len(buttons))
	}
}

// TestShortcuts: Ctrl+Tab, Ctrl+number and Ctrl+N.
func TestShortcuts(t *testing.T) {
	st := newSlashTest(t, "rina")
	chats := st.u.sidebar.visible
	st.u.open(chats[0])
	st.settle()
	st.pressMod(key.NameTab, key.ModShortcut)
	if st.u.selected.ID != chats[1].ID {
		t.Fatalf("Ctrl+Tab opened %s, want %s", st.u.selected.ID, chats[1].ID)
	}
	st.pressMod(key.NameTab, key.ModShortcut|key.ModShift)
	if st.u.selected.ID != chats[0].ID {
		t.Fatalf("Ctrl+Shift+Tab opened %s, want %s", st.u.selected.ID, chats[0].ID)
	}
	st.pressMod("3", key.ModShortcut)
	if st.u.selected.ID != chats[2].ID {
		t.Fatalf("Ctrl+3 opened %s, want %s", st.u.selected.ID, chats[2].ID)
	}
	st.pressMod("N", key.ModShortcut)
	if !st.u.newChat.open() {
		t.Fatal("Ctrl+N didn't open New chat")
	}
}
