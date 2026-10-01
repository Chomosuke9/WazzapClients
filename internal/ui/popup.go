package ui

import (
	"image"
	"image/color"
	"io"
	"strings"
	"time"

	"gioui.org/io/clipboard"
	"gioui.org/io/event"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/chomosuke9/wazzapclients/internal/model"
	"github.com/chomosuke9/wazzapclients/internal/ui/icon"
)

// trackMouse records where the pointer is, in content coordinates, so
// menus can open where the user clicked. It is registered on top of
// everything and lets events pass through.
func (u *UI) trackMouse(gtx C) {
	for {
		ev, ok := gtx.Event(pointer.Filter{Target: &u.mouseTag, Kinds: pointer.Move | pointer.Press | pointer.Drag})
		if !ok {
			break
		}
		if e, ok := ev.(pointer.Event); ok {
			u.mouse = e.Position.Round()
		}
	}
	defer clip.Rect{Max: gtx.Constraints.Max}.Push(gtx.Ops).Pop()
	defer pointer.PassOp{}.Push(gtx.Ops).Pop()
	event.Op(gtx.Ops, &u.mouseTag)
}

// rightClick reports a secondary-button press on an area of size sz at the
// current offset. Other handlers underneath still get the event.
func (u *UI) rightClick(gtx C, key string, sz image.Point) bool {
	right, _ := u.pressArea(gtx, key, sz)
	return right
}

// pressArea is rightClick that also reports a double left-click.
func (u *UI) pressArea(gtx C, key string, sz image.Point) (right, double bool) {
	tag := u.btn("rc:" + key) // only its address is used, as an event tag
	for {
		ev, ok := gtx.Event(pointer.Filter{Target: tag, Kinds: pointer.Press})
		if !ok {
			break
		}
		e, ok := ev.(pointer.Event)
		switch {
		case !ok:
		case e.Buttons.Contain(pointer.ButtonSecondary):
			right = true
		case e.Buttons.Contain(pointer.ButtonPrimary):
			if u.lastPress.key == key && e.Time-u.lastPress.at < 400*time.Millisecond {
				double = true
				u.lastPress.key = ""
			} else {
				u.lastPress.key, u.lastPress.at = key, e.Time
			}
		}
	}
	defer clip.Rect{Max: sz}.Push(gtx.Ops).Pop()
	defer pointer.PassOp{}.Push(gtx.Ops).Pop()
	event.Op(gtx.Ops, tag)
	return right, double
}

// menuItem is one row of a popup menu, or a divider.
type menuItem struct {
	key     string
	ic      *icon.Icon
	label   string
	sub     string      // second line, e.g. "Muted always"
	arrow   bool        // opens a submenu
	check   int         // 1 checked box, -1 empty box, 0 none
	col     color.NRGBA // the icon's color, if not the text's
	divider bool
	run     func()
}

type ctxKind int

const (
	ctxNone ctxKind = iota
	ctxChat
	ctxMessage
	ctxViewer // the media viewer's ⋮ menu
	ctxAttach // the composer's attach menu
)

// ctxMenu is the open context menu: a chat's (right-click in the chat
// list) or a message's (right-click or the bubble's chevron).
type ctxMenu struct {
	kind   ctxKind
	chatID string
	msg    *model.Message
	at     image.Point // where it was opened, in content coordinates
	scrim  widget.Clickable
	lists  bool // "Add to list" submenu open
	all    []*model.ChatList

	// A closed menu fades out showing the items it had.
	closing bool
	anim    tween
	items   []menuItem
}

// isOpen reports whether the menu is open and not fading out.
func (m *ctxMenu) isOpen() bool { return m.kind != ctxNone && !m.closing }

func (u *UI) openChatMenu(c *model.Chat) {
	u.ctx = ctxMenu{kind: ctxChat, chatID: c.ID, at: u.mouse, all: u.backend.Lists()}
}

func (u *UI) openMessageMenu(m *model.Message) {
	u.ctx = ctxMenu{kind: ctxMessage, chatID: m.ChatID, msg: m, at: u.mouse}
}

func (u *UI) closeMenu() { u.ctx.closing = true }

// chatMenuItems mirrors WhatsApp's chat context menu.
func (u *UI) chatMenuItems(c *model.Chat) []menuItem {
	b := u.backend
	id := c.ID
	var items []menuItem
	add := func(it menuItem) { items = append(items, it) }
	if c.Archived {
		add(menuItem{key: "archive", ic: icArchive, label: "Unarchive chat", run: func() { b.SetArchived(id, false) }})
	} else {
		add(menuItem{key: "archive", ic: icArchive, label: "Archive chat", run: func() { b.SetArchived(id, true) }})
	}
	if c.Muted {
		sub := "Muted always"
		if !c.MuteUntil.IsZero() {
			sub = "Muted until " + c.MuteUntil.Format("02/01/2006 15:04")
		}
		add(menuItem{key: "mute", ic: icBellLine, label: "Unmute notifications", sub: sub, run: func() { b.SetMuted(id, false) }})
	} else {
		add(menuItem{key: "mute", ic: icMuted, label: "Mute notifications", run: func() { b.SetMuted(id, true) }})
	}
	if !c.Archived {
		if c.Pinned {
			add(menuItem{key: "pin", ic: icPin, label: "Unpin chat", run: func() { b.SetPinned(id, false) }})
		} else {
			add(menuItem{key: "pin", ic: icPin, label: "Pin chat", run: func() { b.SetPinned(id, true) }})
		}
	}
	if c.Unread != 0 {
		add(menuItem{key: "read", ic: icMarkUnread, label: "Mark as read", run: func() {
			b.Open(id)
			c.Unread = 0
		}})
	} else {
		add(menuItem{key: "read", ic: icMarkUnread, label: "Mark as unread", run: func() {
			b.SetUnread(id, true)
			if u.selected != nil && u.selected.ID == id {
				u.selected = nil
			}
		}})
	}
	if c.Favorite {
		add(menuItem{key: "fav", ic: icHeart, label: "Remove from favourites", run: func() { b.SetFavorite(id, false) }})
	} else {
		add(menuItem{key: "fav", ic: icHeart, label: "Add to favourites", run: func() { b.SetFavorite(id, true) }})
	}
	if u.selected != nil && u.selected.ID == id {
		add(menuItem{key: "close", ic: icCancel, label: "Close chat", run: func() { u.closeChat() }})
	}
	add(menuItem{key: "lists", ic: icAddToList, label: "Add to list", arrow: true})
	add(menuItem{divider: true})
	add(menuItem{key: "clear", ic: icClear, label: "Clear chat", run: func() {
		u.confirm("Clear this chat?", "Messages will only be removed from this device and your devices on the newer versions of WhatsApp.",
			dialogButton{label: "Clear chat", primary: true, run: func() { b.ClearChat(id) }})
	}})
	if c.IsGroup {
		name := c.Name
		add(menuItem{key: "exit", ic: icLogout, label: "Exit group", run: func() {
			u.confirm("Exit \""+name+"\"?", "Only group admins will be notified that you left the group.",
				dialogButton{label: "Exit group", primary: true, danger: true, run: func() { b.LeaveGroup(id) }})
		}})
	} else {
		add(menuItem{key: "delete", ic: icDelete, label: "Delete chat", run: func() {
			u.confirm("Delete this chat?", "Messages will be removed from this device and your other linked devices.",
				dialogButton{label: "Delete chat", primary: true, danger: true, run: func() {
					if u.selected != nil && u.selected.ID == id {
						u.closeChat()
					}
					b.DeleteChat(id)
				}})
		}})
	}
	return items
}

// messageMenuItems mirrors WhatsApp's message menu. "Ask Meta AI" and
// "Report" are left out: neither works without Meta's services.
func (u *UI) messageMenuItems(c *model.Chat, m *model.Message) []menuItem {
	b := u.backend
	var items []menuItem
	add := func(it menuItem) { items = append(items, it) }
	deleted := m.Kind == model.KindDeleted
	if !deleted && !isChannelID(c.ID) {
		add(menuItem{key: "reply", ic: icReply, label: "Reply", run: func() { u.startReply(m) }})
	}
	if c.IsGroup && !m.FromMe && m.SenderID != "" && !deleted {
		add(menuItem{key: "private", ic: icReplyPrivate, label: "Reply privately", run: func() { u.replyPrivately(m) }})
		add(menuItem{key: "dm", ic: icChats, label: "Message " + shortName(plainText(m.Sender)), run: func() { u.openDirect(m.SenderID, m.Sender) }})
	}
	if txt := plainText(m.Text); txt != "" && !deleted {
		add(menuItem{key: "copy", ic: icCopy, label: "Copy", run: func() { u.copyText(stripIsolates(txt)) }})
	}
	if canSave(m) {
		add(menuItem{key: "save", ic: icDownload, label: "Save as…", run: func() { b.SaveMedia(m) }})
	}
	if !deleted {
		if m.Kind != model.KindUnsupported {
			add(menuItem{key: "forward", ic: icForward, label: "Forward", run: func() { u.openForward([]*model.Message{m}) }})
		}
		if !isChannelID(c.ID) {
			if m.Pinned {
				add(menuItem{key: "pin", ic: icPin, label: "Unpin", run: func() { b.PinMessage(m, false) }})
			} else {
				add(menuItem{key: "pin", ic: icPin, label: "Pin", run: func() { b.PinMessage(m, true) }})
			}
		}
		if m.Starred {
			add(menuItem{key: "star", ic: icStar, label: "Unstar", run: func() { b.Star(m, false) }})
		} else {
			add(menuItem{key: "star", ic: icStar, label: "Star", run: func() { b.Star(m, true) }})
		}
	}
	add(menuItem{divider: true})
	add(menuItem{key: "select", ic: icCheckBox, label: "Select", run: func() { u.startSelect(m) }})
	add(menuItem{divider: true})
	add(menuItem{key: "delete", ic: icDelete, label: "Delete", run: func() { u.confirmDelete([]*model.Message{m}) }})
	return items
}

func canSave(m *model.Message) bool {
	switch m.Media {
	case model.MediaImage, model.MediaVideo, model.MediaGIF, model.MediaDocument, model.MediaAudio,
		model.MediaVoice, model.MediaSticker:
		return m.Kind != model.KindDeleted
	}
	return false
}

// stripIsolates removes the invisible marks around resolved @mentions.
func stripIsolates(s string) string {
	return strings.NewReplacer("⁨", "", "⁩", "").Replace(s)
}

func (u *UI) copyText(s string) {
	u.pendingCopy = s
	u.toast("Message copied")
}

// layoutCtxMenu draws the open context menu over everything.
func (u *UI) layoutCtxMenu(gtx C) {
	m := &u.ctx
	if m.kind == ctxNone {
		return
	}
	if m.isOpen() {
		if m.scrim.Clicked(gtx) {
			u.closeMenu()
		}
	}
	if m.isOpen() {
		var items []menuItem
		switch m.kind {
		case ctxChat:
			if c := u.chatByID(m.chatID); c != nil {
				items = u.chatMenuItems(c)
			}
		case ctxMessage:
			if u.selected != nil && u.selected.ID == m.chatID {
				items = u.messageMenuItems(u.selected, m.msg)
			}
		case ctxViewer:
			items = u.viewerMenuItems()
		case ctxAttach:
			if u.selected != nil && u.selected.ID == m.chatID {
				items = u.attachMenuItems(u.selected)
			}
		}
		if items == nil {
			u.ctx = ctxMenu{} // its chat went away
			return
		}
		m.items = items
		// Run the action of an item clicked last frame.
		for _, it := range items {
			if it.key == "" || !u.btn("menu:"+it.key).Clicked(gtx) {
				continue
			}
			if it.arrow {
				m.lists = !m.lists
				continue
			}
			u.closeMenu()
			if it.run != nil {
				it.run()
			}
			break
		}
		if m.items == nil {
			return // the action opened another menu
		}
	}
	v := m.anim.step(gtx, m.isOpen(), popDur(m.isOpen()))
	if v == 0 && m.closing {
		u.ctx = ctxMenu{}
		return
	}
	items := m.items

	sz := gtx.Constraints.Max
	if m.closing {
		var done func()
		gtx, done = fadeOut(gtx)
		defer done()
	} else {
		sgtx := gtx
		sgtx.Constraints = layout.Exact(sz)
		m.scrim.Layout(sgtx, func(gtx C) D { return D{Size: gtx.Constraints.Max} })
	}

	menu := record(gtx, func(gtx C) D { return u.menuPanel(gtx, "menu:", items) })
	reactH := 0
	if m.kind == ctxMessage && m.msg.Kind != model.KindDeleted && !isChannelID(m.chatID) {
		reactH = gtx.Dp(43 + 7)
	}
	// Open below and to the right of the click, flipping to stay on screen.
	pos := m.at
	if pos.X+menu.size.X > sz.X-gtx.Dp(8) {
		pos.X = max(gtx.Dp(8), pos.X-menu.size.X)
	}
	if pos.Y+menu.size.Y > sz.Y-gtx.Dp(8) {
		pos.Y = max(reactH+gtx.Dp(8), sz.Y-gtx.Dp(8)-menu.size.Y)
	}
	pos.Y = max(pos.Y, reactH+gtx.Dp(8))
	if m.kind == ctxAttach {
		// It opens upwards from the attach button.
		pos = image.Pt(max(gtx.Dp(8), m.at.X-gtx.Dp(24)), max(gtx.Dp(8), m.at.Y-gtx.Dp(30)-menu.size.Y))
	}
	// It grows out of the corner nearest to where it was opened.
	origin := image.Pt(min(max(m.at.X, pos.X), pos.X+menu.size.X), min(max(m.at.Y, pos.Y), pos.Y+menu.size.Y))
	defer pushPopup(gtx, v, origin).Pop()
	menu.at(gtx, pos.X, pos.Y)
	if reactH > 0 {
		u.layoutReactionBar(gtx, m.msg, image.Pt(pos.X+menu.size.X/2, pos.Y-gtx.Dp(7)))
	}
	if c := u.chatByID(m.chatID); m.kind == ctxChat && m.lists && c != nil {
		sub := record(gtx, func(gtx C) D { return u.menuPanel(gtx, "list:", u.listItems(c)) })
		x := pos.X + menu.size.X - gtx.Dp(8)
		if x+sub.size.X > sz.X {
			x = pos.X - sub.size.X + gtx.Dp(8)
		}
		y := min(pos.Y+menu.size.Y-sub.size.Y-gtx.Dp(60), sz.Y-sub.size.Y-gtx.Dp(8))
		sub.at(gtx, x, max(y, gtx.Dp(8)))
	}
}

// listItems is the "Add to list" submenu. Clicks toggle membership.
func (u *UI) listItems(c *model.Chat) []menuItem {
	var items []menuItem
	for _, l := range u.ctx.all {
		l := l
		in := false
		for _, id := range l.Chats {
			in = in || id == c.ID
		}
		check := -1
		if in {
			check = 1
		}
		items = append(items, menuItem{key: l.ID, label: l.Name, check: check, run: func() {
			u.backend.SetInList(c.ID, l.ID, !in)
			if in {
				l.Chats = removeString(l.Chats, c.ID)
			} else {
				l.Chats = append(l.Chats, c.ID)
			}
		}})
	}
	if len(items) == 0 {
		items = append(items, menuItem{label: "No lists yet. Create lists on your phone."})
	}
	return items
}

func removeString(s []string, v string) []string {
	var out []string
	for _, x := range s {
		if x != v {
			out = append(out, x)
		}
	}
	return out
}

// menuPanel draws a popup menu's rows on its bordered, rounded panel. Row
// clicks are read by the caller through u.btn(prefix+key); submenu rows
// run their own actions here.
func (u *UI) menuPanel(gtx C, prefix string, items []menuItem) D {
	p := u.pal
	if prefix == "list:" {
		for _, it := range items {
			if it.key != "" && u.btn(prefix+it.key).Clicked(gtx) && it.run != nil {
				it.run()
			}
		}
	}
	gtx.Constraints.Min = image.Point{}
	// Width fits the longest row.
	w := gtx.Dp(150)
	for _, it := range items {
		if it.divider {
			continue
		}
		l := record(gtx, u.label(15, it.label, p.Text, labelOpts{maxLines: 1}).Layout)
		extra := 49 + 24
		if it.arrow {
			extra += 28
		}
		if it.ic == nil && it.check == 0 {
			extra = 24 + 24
		}
		w = max(w, l.size.X+gtx.Dp(unit.Dp(extra)))
	}
	w = min(w, gtx.Dp(320))
	rec := op.Record(gtx.Ops)
	mgtx := gtx
	mgtx.Constraints = layout.Constraints{Min: image.Pt(w, 0), Max: image.Pt(w, gtx.Constraints.Max.Y)}
	dims := layout.Inset{Top: 9, Bottom: 9}.Layout(mgtx, func(gtx C) D {
		var children []layout.FlexChild
		for _, it := range items {
			it := it
			children = append(children, layout.Rigid(func(gtx C) D { return u.menuRow(gtx, prefix, it) }))
		}
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
	})
	call := rec.Stop()
	r := gtx.Dp(12)
	rect := image.Rectangle{Max: dims.Size}
	fillRRect(gtx, rect.Add(image.Pt(0, gtx.Dp(3))).Inset(-gtx.Dp(2)), r+gtx.Dp(2), p.Shadow)
	borderRRect(gtx, rect, r, p.Popup, p.PopupBorder)
	defer clip.UniformRRect(rect, r).Push(gtx.Ops).Pop()
	call.Add(gtx.Ops)
	return dims
}

func (u *UI) menuRow(gtx C, prefix string, it menuItem) D {
	p := u.pal
	gtx.Constraints.Min.X = gtx.Constraints.Max.X
	if it.divider {
		h := gtx.Dp(9)
		y := h / 2
		fillRect(gtx, image.Rect(gtx.Dp(14), y, gtx.Constraints.Max.X-gtx.Dp(14), y+max(1, gtx.Dp(1))), p.PopupDivider)
		return D{Size: image.Pt(gtx.Constraints.Max.X, h)}
	}
	content := func(gtx C) D {
		h := gtx.Dp(40)
		if it.sub != "" {
			h = gtx.Dp(58)
		}
		return vcenter(gtx, h, func(gtx C) D {
			left := unit.Dp(49)
			if it.ic == nil && it.check == 0 {
				left = 24
			}
			return layout.Inset{Left: left, Right: 20}.Layout(gtx, func(gtx C) D {
				col := p.Text
				if it.key == "" {
					col = p.TextSecondary
				}
				return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
					layout.Flexed(1, func(gtx C) D {
						return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
							layout.Rigid(u.label(15, it.label, col, labelOpts{maxLines: 1}).Layout),
							layout.Rigid(func(gtx C) D {
								if it.sub == "" {
									return D{}
								}
								return layout.Inset{Top: 2}.Layout(gtx, u.label(13, it.sub, p.PopupSub, labelOpts{maxLines: 1}).Layout)
							}),
						)
					}),
					layout.Rigid(func(gtx C) D {
						if !it.arrow {
							return D{}
						}
						return drawIcon(gtx, icSubmenu, 20, p.Text)
					}),
				)
			})
		})
	}
	draw := func(gtx C, hover float32) D {
		m := op.Record(gtx.Ops)
		dims := content(gtx)
		call := m.Stop()
		if hover > 0 {
			fillRect(gtx, image.Rectangle{Max: dims.Size}, faded(p.PopupHover, hover))
		}
		ic := it.ic
		switch it.check {
		case 1:
			ic = icCheckBox
		case -1:
			ic = icCheckBoxEmpty
		}
		if ic != nil {
			sz := gtx.Dp(22)
			col := p.Text
			switch {
			case it.check == 1:
				col = p.Green
			case it.col.A > 0:
				col = it.col
			}
			t := op.Offset(image.Pt(gtx.Dp(26)-sz/2, (dims.Size.Y-sz)/2)).Push(gtx.Ops)
			drawIcon(gtx, ic, 22, col)
			t.Pop()
		}
		call.Add(gtx.Ops)
		return dims
	}
	if it.key == "" {
		return draw(gtx, 0)
	}
	c := u.btn(prefix + it.key)
	return clickable(gtx, c, func(gtx C) D { return draw(gtx, u.hover(gtx, c)) })
}

// quickReactions are the reaction bar's emoji, as in WhatsApp.
var quickReactions = [...]string{"👍", "❤️", "😂", "😮", "😢", "🙏"}

// layoutReactionBar draws the emoji pill above a message menu, centered on
// top (its bottom-center point).
func (u *UI) layoutReactionBar(gtx C, m *model.Message, top image.Point) {
	p := u.pal
	// The bar keeps drawing after a click: the menu fades out with it.
	for i, e := range quickReactions {
		if u.btn("react:" + itoa(i+1)).Clicked(gtx) {
			if m.Reaction == e {
				e = ""
			}
			u.backend.React(m, e)
			u.closeMenu()
		}
	}
	if u.btn("react:more").Clicked(gtx) {
		u.closeMenu()
		u.openPicker(pickReaction, m)
	}
	bar := record(gtx, func(gtx C) D {
		gtx.Constraints.Min = image.Point{}
		return layout.Inset{Left: 8, Right: 8, Top: 4, Bottom: 4}.Layout(gtx, func(gtx C) D {
			var children []layout.FlexChild
			cell := func(key string, w layout.Widget, active bool) layout.FlexChild {
				return layout.Rigid(func(gtx C) D {
					c := u.btn(key)
					return clickable(gtx, c, func(gtx C) D {
						sz := gtx.Dp(35)
						h := u.hover(gtx, c)
						if active {
							h = 1
						}
						if h > 0 {
							fillCircle(gtx, image.Pt(sz/2, sz/2), sz/2, faded(p.PopupHover, h))
						}
						return centerIn(gtx, sz, w)
					})
				})
			}
			for i, e := range quickReactions {
				children = append(children, cell("react:"+itoa(i+1), u.label(22, e, p.Text).Layout, m.Reaction == e))
				children = append(children, layout.Rigid(layout.Spacer{Width: 3}.Layout))
			}
			children = append(children, cell("react:more", iconW(icAdd, 24, p.Icon), false))
			return layout.Flex{Alignment: layout.Middle}.Layout(gtx, children...)
		})
	})
	x := min(max(gtx.Dp(8), top.X-bar.size.X/2), gtx.Constraints.Max.X-bar.size.X-gtx.Dp(8))
	y := top.Y - bar.size.Y
	r := bar.size.Y / 2
	rect := image.Rectangle{Max: bar.size}.Add(image.Pt(x, y))
	fillRRect(gtx, rect.Add(image.Pt(0, gtx.Dp(3))).Inset(-gtx.Dp(2)), r+gtx.Dp(2), p.Shadow)
	borderRRect(gtx, rect, r, p.Popup, p.PopupBorder)
	bar.at(gtx, x, y)
}

// chevronButton is the round "⌄" button that appears on a hovered chat row
// or message bubble and opens its menu.
func (u *UI) chevronButton(gtx C, c *widget.Clickable, bg, fg color.NRGBA) D {
	return clickable(gtx, c, func(gtx C) D {
		sz := gtx.Dp(26)
		if bg.A > 0 {
			fillCircle(gtx, image.Pt(sz/2, sz/2), sz/2, bg)
		}
		return centerIn(gtx, sz, iconW(icChevron, 22, fg))
	})
}

// flushClipboard writes text queued by copyText; it needs a frame context.
func (u *UI) flushClipboard(gtx C) {
	if u.pendingCopy == "" {
		return
	}
	gtx.Execute(clipboard.WriteCmd{Type: "application/text", Data: io.NopCloser(strings.NewReader(u.pendingCopy))})
	u.pendingCopy = ""
}
