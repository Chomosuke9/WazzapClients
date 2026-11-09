package ui

import (
	"image"
	"strconv"

	"gioui.org/f32"
	"gioui.org/io/event"
	"gioui.org/io/input"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/widget"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

// Keyboard use. Gio moves the focus with Tab and Shift+Tab among every
// focusable handler, and a focused widget.Clickable clicks on Enter or
// Space. On top of that (with patches/gio.patch's FocusInfo, FocusTrap,
// MoveFocusCmd and SyntheticCmd):
//
//   - the focused element gets a ring while the keyboard is in use
//     (drawFocusRing), like a browser's :focus-visible;
//   - menus and dialogs trap the focus (focusTrap): Tab stays inside, the
//     first Tab or arrow from the keyboard enters them, and the focus goes
//     back where it was when they close (trackTrap);
//   - arrows move through menus and dialogs, the chat list and the open
//     chat's messages, each of which takes the focus (focusArea);
//   - Shift+F10 right-clicks the focused element, opening its menu where
//     a right click would;
//   - shortcuts like WhatsApp's open chats, pages and actions (globalKeys).

// kbState is the keyboard's state between frames.
type kbState struct {
	trapped bool      // the last frame trapped the focus
	last    event.Tag // the focus before the trap
	restore event.Tag // where the focus goes back when the trap goes

	// The chat row and message that had the focus in the last frame,
	// noted as they're drawn.
	chat    string
	msg     string
	msgRows []string       // the open chat's focusable messages, in order, last frame
	rowOf   map[string]int // their rows in the conversation's list

	// top is the title bar's height: content coordinates (u.mouse) are
	// the window's less it.
	top int

	// pending is focused once it's drawn: a row scrolled into view.
	pending event.Tag
	tries   int
}

// focusTrap keeps Tab inside what's drawn after it (until the next trap):
// a menu or dialog over everything else. Draw it only while that's open.
func focusTrap(gtx C) {
	event.Op(gtx.Ops, key.FocusTrap)
}

// noFocus keeps c out of Tab's way: a scrim, or an area that swallows
// clicks.
func noFocus(c *widget.Clickable) *widget.Clickable {
	c.NoFocus = true
	return c
}

// focusArea makes r (at the current offset) focusable under tag, without
// taking any pointer events from what's underneath.
func focusArea(gtx C, tag event.Tag, r image.Rectangle) {
	for {
		if _, ok := gtx.Event(key.FocusFilter{Target: tag}); !ok {
			break
		}
	}
	defer clip.Rect(r).Push(gtx.Ops).Pop()
	defer pointer.PassOp{}.Push(gtx.Ops).Pop()
	event.Op(gtx.Ops, tag)
}

// isEditor reports whether tag is a text field, which shows its caret
// instead of a ring and keeps its keys.
func isEditor(tag event.Tag) bool {
	_, ok := tag.(*widget.Editor)
	return ok
}

// trackTrap enters a trap the keyboard opened and leaves the focus where it
// was when the trap goes. Before anything is drawn.
func (u *UI) trackTrap(gtx C) {
	fi := gtx.Source.Focus()
	k := &u.kb
	switch {
	case fi.Trapped && !k.trapped:
		k.restore = k.last
		if fi.Keyboard && fi.Tag == nil {
			gtx.Execute(key.MoveFocusCmd{Dir: key.FocusForward})
		}
	case !fi.Trapped && k.trapped:
		if fi.Tag == nil && k.restore != nil {
			gtx.Execute(key.FocusCmd{Tag: k.restore})
		}
		k.restore = nil
	}
	k.trapped = fi.Trapped
	if !fi.Trapped {
		k.last = fi.Tag
	}
	if k.pending != nil {
		if gtx.Focused(k.pending) || k.tries <= 0 {
			k.pending = nil
		} else {
			k.tries--
			gtx.Execute(key.FocusCmd{Tag: k.pending})
			gtx.Execute(op.InvalidateCmd{})
		}
	}
}

// focusSoon focuses tag, now or, if it isn't drawn yet, once it is.
func (u *UI) focusSoon(gtx C, tag event.Tag) {
	gtx.Execute(key.FocusCmd{Tag: tag})
	u.kb.pending, u.kb.tries = tag, 3
	gtx.Execute(op.InvalidateCmd{})
}

// blurOnClick drops a button's focus when the mouse presses elsewhere, so
// Enter can't click it unseen. A text field keeps it, as it always did.
// After everything else had the press.
func (u *UI) blurOnClick(gtx C) {
	if fi := gtx.Source.Focus(); fi.Tag != nil && !isEditor(fi.Tag) {
		gtx.Execute(key.FocusCmd{})
	}
}

// drawFocusRing outlines the focused element while the keyboard is in use.
// gtx is the window's, unzoomed: the bounds are the window's.
func (u *UI) drawFocusRing(gtx C) {
	fi := gtx.Source.Focus()
	if !fi.Keyboard || fi.Tag == nil || isEditor(fi.Tag) || fi.Bounds.Empty() {
		return
	}
	b := fi.Bounds
	w := max(2, gtx.Dp(2))
	if b.Dx() <= 2*w || b.Dy() <= 2*w {
		return
	}
	r := min(gtx.Dp(10), min(b.Dx(), b.Dy())/2)
	if d := b.Dx() - b.Dy(); d >= -1 && d <= 1 {
		r = b.Dx() / 2 // a round button
	}
	rr := clip.RRect{Rect: b, SE: r, SW: r, NW: r, NE: r}
	paint.FillShape(gtx.Ops, u.pal.Green, clip.Stroke{Path: rr.Path(gtx.Ops), Width: float32(w)}.Op())
}

// rightClickFocus right-clicks the middle of the focused element, which
// opens its menu (Shift+F10, the keyboard's context menu).
func (u *UI) rightClickFocus(gtx C) {
	fi := gtx.Source.Focus()
	if fi.Tag == nil || fi.Bounds.Empty() || isEditor(fi.Tag) {
		return
	}
	c := fi.Bounds.Min.Add(fi.Bounds.Size().Div(2))
	at := f32.Pt(float32(c.X), float32(c.Y))
	// Menus open at u.mouse, which the press moves only after the
	// handlers that open them saw it.
	u.mouse = c.Sub(image.Pt(0, u.kb.top))
	gtx.Execute(input.SyntheticCmd{Events: []event.Event{
		pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Buttons: pointer.ButtonSecondary, Position: at},
		pointer.Event{Kind: pointer.Release, Source: pointer.Mouse, Position: at},
	}})
	gtx.Execute(op.InvalidateCmd{})
}

// keyPressed drains filters and reports which name, if any, was pressed.
func keyPressed(gtx C, filters ...event.Filter) (key.Event, bool) {
	var hit key.Event
	ok := false
	for {
		ev, more := gtx.Event(filters...)
		if !more {
			break
		}
		if e, isKey := ev.(key.Event); isKey && e.State == key.Press {
			hit, ok = e, true
		}
	}
	return hit, ok
}

// navKeys reads the keys of whatever has the focus, before the composer
// and the window's Esc see them.
func (u *UI) navKeys(gtx C) {
	fi := gtx.Source.Focus()
	if fi.Tag != nil && !isEditor(fi.Tag) {
		if _, ok := keyPressed(gtx, key.Filter{Focus: fi.Tag, Name: key.NameF10, Required: key.ModShift}); ok {
			u.rightClickFocus(gtx)
		}
	}
	switch {
	case fi.Tag == nil || isEditor(fi.Tag):
	case u.kb.msg != "" && fi.Tag == u.msgFocusTag(u.kb.msg):
		u.messageKeys(gtx, fi.Tag, u.kb.msg)
	case u.kb.chat != "" && fi.Tag == u.sidebar.rows[u.kb.chat]:
		u.chatRowKeys(gtx, fi.Tag, u.kb.chat)
	case fi.Trapped:
		// Menus and dialogs: the arrows go from one button to the next.
		e, ok := keyPressed(gtx,
			key.Filter{Focus: fi.Tag, Name: key.NameUpArrow}, key.Filter{Focus: fi.Tag, Name: key.NameDownArrow},
			key.Filter{Focus: fi.Tag, Name: key.NameLeftArrow}, key.Filter{Focus: fi.Tag, Name: key.NameRightArrow})
		if ok {
			dir := key.FocusForward
			if e.Name == key.NameUpArrow || e.Name == key.NameLeftArrow {
				dir = key.FocusBackward
			}
			gtx.Execute(key.MoveFocusCmd{Dir: dir})
		}
	}
	u.kb.chat, u.kb.msg = "", ""
}

// chatRowKeys moves along the chat list with the arrows, Home and End.
func (u *UI) chatRowKeys(gtx C, tag event.Tag, id string) {
	e, ok := keyPressed(gtx,
		key.Filter{Focus: tag, Name: key.NameUpArrow}, key.Filter{Focus: tag, Name: key.NameDownArrow},
		key.Filter{Focus: tag, Name: key.NameHome}, key.Filter{Focus: tag, Name: key.NameEnd})
	chats := u.sidebar.visible
	if !ok || u.listQuery() != "" || len(chats) == 0 {
		return
	}
	i := -1
	for j, c := range chats {
		if c.ID == id {
			i = j
		}
	}
	if i < 0 {
		return
	}
	switch e.Name {
	case key.NameUpArrow:
		i--
	case key.NameDownArrow:
		i++
	case key.NameHome:
		i = 0
	case key.NameEnd:
		i = len(chats) - 1
	}
	if i < 0 || i >= len(chats) {
		return
	}
	scrollToRow(&u.sidebar.list.List, i)
	u.focusSoon(gtx, u.rowClick(chats[i]))
}

// scrollToRow scrolls l, if needed, so its i-th row shows.
func scrollToRow(l *layout.List, i int) {
	p := l.Position
	switch {
	case i < p.First || (i == p.First && p.Offset > 0):
		l.Position = layout.Position{First: i, BeforeEnd: true}
	case p.Count > 0 && i >= p.First+p.Count-1:
		// The last row shown may be cut off: move down a row at a time.
		l.Position = layout.Position{First: p.First + 1 + max(0, i-(p.First+p.Count-1)), BeforeEnd: true}
	}
}

// msgFocusTag is the tag a message takes the focus under.
func (u *UI) msgFocusTag(id string) event.Tag { return u.btn("kf:" + id) }

// focusMessage makes a message's bubble (r, at the current offset)
// focusable, and notes the one with the focus.
func (u *UI) focusMessage(gtx C, id string, r image.Rectangle) {
	tag := u.msgFocusTag(id)
	focusArea(gtx, tag, r)
	if gtx.Focused(tag) {
		u.kb.msg = id
	}
}

// messageKeys moves along the open chat's messages with the arrows, opens
// a message's menu with Enter, and goes back to the composer with Esc.
func (u *UI) messageKeys(gtx C, tag event.Tag, id string) {
	e, ok := keyPressed(gtx,
		key.Filter{Focus: tag, Name: key.NameUpArrow}, key.Filter{Focus: tag, Name: key.NameDownArrow},
		key.Filter{Focus: tag, Name: key.NameHome}, key.Filter{Focus: tag, Name: key.NameEnd},
		key.Filter{Focus: tag, Name: key.NameReturn}, key.Filter{Focus: tag, Name: key.NameEnter},
		key.Filter{Focus: tag, Name: key.NameSpace}, key.Filter{Focus: tag, Name: key.NameEscape})
	if !ok {
		return
	}
	switch e.Name {
	case key.NameReturn, key.NameEnter, key.NameSpace:
		u.rightClickFocus(gtx)
		return
	case key.NameEscape:
		u.requestFocus(&u.conv.composer)
		return
	}
	rows := u.kb.msgRows
	i := -1
	for j, r := range rows {
		if r == id {
			i = j
		}
	}
	if i < 0 {
		return
	}
	switch e.Name {
	case key.NameUpArrow:
		i--
	case key.NameDownArrow:
		i++
	case key.NameHome:
		i = 0
	case key.NameEnd:
		i = len(rows) - 1
	}
	if i < 0 || i >= len(rows) {
		return
	}
	u.focusMessageRow(gtx, rows[i])
}

// focusMessageRow scrolls the open chat to a message and focuses it.
func (u *UI) focusMessageRow(gtx C, id string) {
	if i, ok := u.kb.rowOf[id]; ok {
		scrollToRow(&u.conv.list.List, i)
	}
	u.focusSoon(gtx, u.msgFocusTag(id))
}

// shortcut is a key with the shortcut modifier (Ctrl, or Cmd on macOS),
// and others it needs.
func shortcut(name key.Name, mods key.Modifiers) key.Filter {
	return key.Filter{Name: name, Required: key.ModShortcut | mods}
}

// globalKeys are the shortcuts that work anywhere in the window, listed on
// Settings > Keyboard shortcuts.
func (u *UI) globalKeys(gtx C) {
	const shift, alt = key.ModShift, key.ModAlt
	filters := []event.Filter{
		shortcut("N", 0),               // new chat
		shortcut("N", shift),           // new group
		shortcut("F", 0),               // search the chat list
		shortcut(",", 0),               // settings
		shortcut("P", 0),               // profile
		shortcut(key.NameTab, 0),       // next chat
		shortcut(key.NameTab, shift),   // previous chat
		shortcut("E", alt|shift),       // archive
		shortcut("M", alt|shift),       // mute
		shortcut("U", alt|shift),       // mark as unread
		shortcut("P", alt|shift),       // pin
		shortcut("E", shift),           // emoji picker
		shortcut(key.NameUpArrow, alt), // focus the newest message
		key.Filter{Name: key.NameF6},   // next part of the window
		key.Filter{Name: key.NameF6, Required: shift},
	}
	for i := 1; i <= 9; i++ {
		filters = append(filters, shortcut(key.Name(strconv.Itoa(i)), 0)) // the i-th chat
	}
	for {
		ev, ok := gtx.Event(filters...)
		if !ok {
			break
		}
		e, ok := ev.(key.Event)
		if !ok || e.State != key.Press {
			continue
		}
		u.runShortcut(gtx, e)
		gtx.Execute(op.InvalidateCmd{})
	}
}

func (u *UI) runShortcut(gtx C, e key.Event) {
	mods := e.Modifiers &^ key.ModShortcut
	if e.Modifiers&key.ModShortcut == 0 {
		if e.Name == key.NameF6 {
			u.cyclePane(gtx, mods&key.ModShift != 0)
		}
		return
	}
	if u.dialog.isOpen() || u.ctx.isOpen() || u.menu.open {
		return // a menu or dialog has the keyboard
	}
	c := u.selected
	if c != nil && u.selPage != u.page {
		c = nil
	}
	switch {
	case e.Name == "N" && mods == 0:
		u.setPage(pageChats)
		u.setListHidden(false)
		u.openNewChat()
	case e.Name == "N":
		u.setPage(pageChats)
		u.setListHidden(false)
		u.openNewGroup(nil)
	case e.Name == "F" && mods == 0:
		if u.page != pageChats {
			u.setPage(pageChats)
		}
		u.setListHidden(false)
		u.requestFocus(&u.sidebar.search)
	case e.Name == ",":
		u.ShowPage("settings")
		u.setListHidden(false)
	case e.Name == "P" && mods == 0:
		u.ShowPage("profile")
		u.setListHidden(false)
	case e.Name == key.NameTab:
		u.stepChat(mods&key.ModShift != 0)
	case len(e.Name) == 1 && e.Name >= "1" && e.Name <= "9":
		n := int(e.Name[0] - '1')
		if u.page == pageChats && n < len(u.sidebar.visible) {
			u.open(u.sidebar.visible[n])
		}
	case e.Name == key.NameUpArrow:
		if c != nil && len(u.kb.msgRows) > 0 {
			u.focusMessageRow(gtx, u.kb.msgRows[len(u.kb.msgRows)-1])
		}
	case e.Name == "E" && mods == key.ModShift:
		if c != nil {
			if u.picker.open {
				u.closePicker()
			} else {
				u.openPicker(pickComposer, nil)
			}
		}
	case c != nil:
		u.chatShortcut(c, e.Name)
	}
}

// chatShortcut runs the open chat's menu item a shortcut stands for.
func (u *UI) chatShortcut(c *model.Chat, name key.Name) {
	keys := map[key.Name]string{"E": "archive", "U": "read", "P": "pin"}
	if name == "M" {
		if c.Muted {
			u.backend.SetMuted(c.ID, false, 0)
			u.toast("Chat unmuted")
		} else {
			u.backend.SetMuted(c.ID, true, 0)
			u.toast("Chat muted")
		}
		return
	}
	k, ok := keys[name]
	if !ok {
		return
	}
	for _, it := range u.chatMenuItems(c) {
		if it.key == k && it.run != nil {
			it.run()
			u.toast(it.label)
			return
		}
	}
}

// stepChat opens the next (or previous) chat in the list, as Ctrl+Tab does
// in WhatsApp.
func (u *UI) stepChat(back bool) {
	chats := u.sidebar.visible
	if len(chats) == 0 {
		return
	}
	i := -1
	if u.selected != nil {
		for j, c := range chats {
			if c.ID == u.selected.ID {
				i = j
			}
		}
	}
	switch {
	case i < 0 && back:
		i = len(chats) - 1
	case i < 0:
		i = 0
	case back:
		i = (i - 1 + len(chats)) % len(chats)
	default:
		i = (i + 1) % len(chats)
	}
	u.setPage(pageChats)
	u.open(chats[i])
	scrollToRow(&u.sidebar.list.List, i)
}

// cyclePane moves the focus between the window's parts with F6: the chat
// list, the open chat's messages and its composer.
func (u *UI) cyclePane(gtx C, back bool) {
	var stops []event.Tag
	if !u.split.hidden && len(u.sidebar.visible) > 0 {
		i := u.sidebar.list.Position.First
		for j, c := range u.sidebar.visible {
			if u.selected != nil && c.ID == u.selected.ID {
				i = j
			}
		}
		i = min(i, len(u.sidebar.visible)-1)
		stops = append(stops, u.rowClick(u.sidebar.visible[i]))
	}
	open := u.selected != nil && u.selPage == u.page
	if open && len(u.kb.msgRows) > 0 {
		stops = append(stops, u.msgFocusTag(u.kb.msgRows[len(u.kb.msgRows)-1]))
	}
	if open {
		stops = append(stops, &u.conv.composer)
	}
	if len(stops) == 0 {
		return
	}
	fi := gtx.Source.Focus()
	at := -1
	for i, t := range stops {
		if t == fi.Tag {
			at = i
		}
	}
	if at < 0 && fi.Tag != nil {
		// Somewhere inside a part: the list's rows and messages count as theirs.
		if u.isChatRow(fi.Tag) {
			at = 0
		} else if i := len(stops) - 2; open && i >= 0 && u.isMessageTag(fi.Tag) {
			at = i
		}
	}
	switch {
	case at < 0 && back:
		at = len(stops) - 1
	case at < 0:
		at = 0
	case back:
		at = (at - 1 + len(stops)) % len(stops)
	default:
		at = (at + 1) % len(stops)
	}
	t := stops[at]
	if mt, ok := t.(*widget.Clickable); ok && len(u.kb.msgRows) > 0 && mt == u.msgFocusTag(u.kb.msgRows[len(u.kb.msgRows)-1]) {
		u.focusMessageRow(gtx, u.kb.msgRows[len(u.kb.msgRows)-1])
		return
	}
	if t == &u.conv.composer {
		u.requestFocus(t)
		return
	}
	u.focusSoon(gtx, t)
}

func (u *UI) isChatRow(tag event.Tag) bool {
	for _, c := range u.sidebar.rows {
		if event.Tag(c) == tag {
			return true
		}
	}
	return false
}

func (u *UI) isMessageTag(tag event.Tag) bool {
	for _, id := range u.kb.msgRows {
		if u.msgFocusTag(id) == tag {
			return true
		}
	}
	return false
}

// noteMessageRows notes the open chat's messages that take the focus, for
// the arrows. rows are the conversation list's.
func (u *UI) noteMessageRows(rows []convRow) {
	k := &u.kb
	k.msgRows = k.msgRows[:0]
	if k.rowOf == nil {
		k.rowOf = make(map[string]int)
	}
	clear(k.rowOf)
	for i, r := range rows {
		if r.kind == rowMessage && r.msg != nil {
			k.msgRows = append(k.msgRows, r.msg.ID)
			k.rowOf[r.msg.ID] = i
		}
	}
}
