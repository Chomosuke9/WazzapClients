package ui

import (
	"image"
	"image/color"
	"os"
	"strings"

	"gioui.org/font"
	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/text"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"github.com/chomosuke9/wazzapclients/internal/filepick"
	"github.com/chomosuke9/wazzapclients/internal/model"
	"github.com/chomosuke9/wazzapclients/internal/photo"
	"github.com/chomosuke9/wazzapclients/internal/ui/icon"
)

// ncStep is a page of the New chat panel. Each page slides in from the
// left over the one before it, and over the chat list, like WhatsApp's.
type ncStep int

const (
	ncNone    ncStep = iota
	ncChat           // New chat: search contacts or type a number
	ncMembers        // New group: pick the members
	ncGroup          // New group: name, picture, disappearing messages
)

// groupNameMax is WhatsApp's limit on a group's name.
const groupNameMax = 100

// groupPhotoPx is the side of the picture uploaded for a group.
const groupPhotoPx = 640

// newChatState is the New chat panel and the New group flow in it.
type newChatState struct {
	step ncStep
	base ncStep             // the page it opened at; Back there closes it
	in   [ncGroup + 1]tween // each page sliding in

	search  widget.Editor // New chat
	members widget.Editor // Add group members
	name    widget.Editor // New group
	list    widget.List
	mlist   widget.List
	glist   widget.List

	contacts []*model.Contact // loaded when the panel opens
	shown    []*model.Contact // the contacts listed this frame, reused
	picked   []model.Contact  // new group's members, in the order picked
	lookup   string           // phone number being checked, digits only

	photo        []byte // the new group's picture (JPEG), or nil
	photoN       int    // counts pictures, to key the preview
	photoBusy    bool   // the file dialog or the cropping is running
	photos       chan groupPhoto
	disappearing uint32 // seconds, 0 = off
	creating     bool   // waiting for the GroupCreatedEvent
	nextIn       tween  // the green buttons rising in
	createIn     tween
}

// groupPhoto is a picture picked in the file dialog, cropped and scaled.
type groupPhoto struct {
	data []byte
	err  error
	none bool // the dialog was cancelled
}

func (nc *newChatState) open() bool { return nc.step != ncNone }

// start sets the panel up as it opens.
func (nc *newChatState) start(contacts []*model.Contact) {
	nc.contacts = contacts
	for _, ed := range []*widget.Editor{&nc.search, &nc.members, &nc.name} {
		ed.SingleLine, ed.Submit = true, true
	}
	nc.name.MaxLen = groupNameMax
	nc.list.Axis, nc.mlist.Axis, nc.glist.Axis = layout.Vertical, layout.Vertical, layout.Vertical
}

// openNewChat slides the New chat panel in over the chat list.
func (u *UI) openNewChat() {
	nc := &u.newChat
	u.newChatPage()
	if nc.step == ncNone {
		nc.start(u.backend.Contacts())
	}
	nc.step, nc.base = ncChat, ncChat
	nc.search.SetText("")
	nc.list.Position = layout.Position{}
	u.requestFocus(&nc.search)
}

// openNewGroup starts the New group flow with members already picked
// ("Create a similar group").
func (u *UI) openNewGroup(members []model.Contact) {
	nc := &u.newChat
	u.newChatPage()
	if nc.step == ncNone {
		nc.start(u.backend.Contacts())
		nc.base = ncMembers
	}
	nc.step = ncMembers
	nc.picked = members
	nc.resetGroup()
	u.requestFocus(&nc.members)
}

// snap finishes the pages' slides (for screenshots).
func (nc *newChatState) snap() {
	for s := ncChat; s <= ncGroup; s++ {
		nc.in[s].snap(s >= nc.base && s <= nc.step)
	}
	nc.nextIn.snap(len(nc.picked) > 0)
	nc.createIn.snap(trimSpace(nc.name.Text()) != "")
}

// newChatPage shows the chat list, which the panel covers.
func (u *UI) newChatPage() {
	u.sidebar.showArchived = false
	u.setPage(pageChats)
	u.ctx = ctxMenu{}
}

// resetGroup clears what the New group page holds.
func (nc *newChatState) resetGroup() {
	nc.members.SetText("")
	nc.mlist.Position = layout.Position{}
	nc.name.SetText("")
	nc.photo, nc.disappearing = nil, 0
}

// newChatBack goes back a page, closing the panel on the first one.
func (u *UI) newChatBack() {
	nc := &u.newChat
	switch {
	case nc.step == ncNone:
	case nc.step <= nc.base:
		nc.step = ncNone
	case nc.step == ncMembers:
		nc.step = ncChat
		nc.picked = nil
		u.requestFocus(&nc.search)
	default:
		nc.step--
		u.requestFocus(&nc.members)
	}
}

// closeNewChat closes the panel at once (when another page is chosen).
func (u *UI) closeNewChat() {
	u.newChat = newChatState{photos: u.newChat.photos}
}

// updateNewChat handles the panel's buttons and results.
func (u *UI) updateNewChat(gtx C) {
	nc := &u.newChat
	if u.sidebar.newChat.Clicked(gtx) {
		u.openNewChat()
	}
	select {
	case r := <-nc.photos:
		nc.photoBusy = false
		switch {
		case r.err == filepick.ErrUnsupported:
			u.toast("No file dialog found. Install zenity or kdialog to choose a picture.")
		case r.err != nil:
			u.toast("Couldn't use this picture.")
		case !r.none && nc.step == ncGroup:
			nc.photo = r.data
			nc.photoN++
		}
	default:
	}
	if !nc.open() {
		return
	}
	if u.btn("nc:back").Clicked(gtx) {
		u.newChatBack()
	}
	for {
		ev, ok := nc.search.Update(gtx)
		if !ok {
			break
		}
		if _, ok := ev.(widget.SubmitEvent); ok && nc.step == ncChat {
			if d := phoneDigits(nc.search.Text()); d != "" {
				u.lookupPhone(d)
			} else if cs := u.matchContacts(nc.search.Text(), false); len(cs) > 0 {
				u.startChat(cs[0].ID, cs[0].Name)
			}
		}
	}
	for {
		ev, ok := nc.name.Update(gtx)
		if !ok {
			break
		}
		if _, ok := ev.(widget.SubmitEvent); ok && nc.step == ncGroup {
			u.createGroup()
		}
	}
	if u.btn("nc:group").Clicked(gtx) && nc.step == ncChat {
		nc.step = ncMembers
		nc.resetGroup()
		u.requestFocus(&nc.members)
	}
	if u.btn("nc:self").Clicked(gtx) && nc.step == ncChat {
		for _, c := range u.chats {
			if c.Self {
				u.startChat(c.ID, c.Name)
				break
			}
		}
	}
	if u.btn("nc:phone").Clicked(gtx) && nc.step == ncChat {
		if d := phoneDigits(nc.search.Text()); d != "" {
			u.lookupPhone(d)
		}
	}
	if u.btn("nc:next").Clicked(gtx) && nc.step == ncMembers && len(nc.picked) > 0 {
		nc.step = ncGroup
		nc.glist.Position = layout.Position{}
		u.requestFocus(&nc.name)
	}
	if u.btn("nc:create").Clicked(gtx) && nc.step == ncGroup {
		u.createGroup()
	}
	if u.btn("nc:photo").Clicked(gtx) && nc.step == ncGroup {
		if nc.photo == nil {
			u.pickGroupPhoto()
		} else {
			u.ctx = ctxMenu{kind: ctxGroupPhoto, at: u.mouse}
		}
	}
	if u.btn("nc:timer").Clicked(gtx) && nc.step == ncGroup {
		u.ctx = ctxMenu{kind: ctxGroupTimer, at: u.mouse}
	}
	// Contact rows: in New chat they open the chat, in Add group members
	// they pick the contact; chips unpick.
	for _, c := range nc.contacts {
		if u.btn("nc:c:"+c.ID).Clicked(gtx) && nc.step == ncChat {
			u.startChat(c.ID, c.Name)
		}
		if u.btn("nc:m:"+c.ID).Clicked(gtx) && nc.step == ncMembers && nc.pickedAt(c.ID) < 0 {
			nc.picked = append(nc.picked, *c)
			nc.members.SetText("")
			u.requestFocus(&nc.members)
		}
	}
	for i := 0; i < len(nc.picked); i++ {
		if u.btn("nc:x:"+nc.picked[i].ID).Clicked(gtx) && nc.step == ncMembers {
			nc.picked = append(nc.picked[:i:i], nc.picked[i+1:]...)
			i--
		}
	}
	// Backspace in an empty member search unpicks the last member.
	if nc.step == ncMembers && gtx.Focused(&nc.members) && nc.members.Len() == 0 && len(nc.picked) > 0 {
		for {
			ev, ok := gtx.Event(key.Filter{Focus: &nc.members, Name: key.NameDeleteBackward})
			if !ok {
				break
			}
			if e, ok := ev.(key.Event); ok && e.State == key.Press && len(nc.picked) > 0 {
				nc.picked = nc.picked[:len(nc.picked)-1]
			}
		}
	}
}

func (nc *newChatState) pickedAt(id string) int {
	for i, c := range nc.picked {
		if c.ID == id {
			return i
		}
	}
	return -1
}

// startChat closes the panel and opens the one-to-one chat with id.
func (u *UI) startChat(id, name string) {
	u.newChat.step = ncNone
	u.openDirect(id, name)
}

// lookupPhone asks whether a typed number is on WhatsApp; the PhoneEvent
// opens its chat.
func (u *UI) lookupPhone(digits string) {
	nc := &u.newChat
	if nc.lookup == digits {
		return
	}
	nc.lookup = digits
	u.backend.LookupPhone(digits)
}

// phoneEvent answers lookupPhone.
func (u *UI) phoneEvent(e model.PhoneEvent) {
	nc := &u.newChat
	if e.Phone != nc.lookup {
		return
	}
	nc.lookup = ""
	switch {
	case !nc.open():
	case e.Err != "":
		u.toast(e.Err)
	case e.ID == "":
		u.toast("+" + e.Phone + " isn't on WhatsApp. Check that the number has its country code.")
	default:
		name := e.Name
		if name == "" {
			name = "+" + e.Phone
		}
		u.startChat(e.ID, name)
	}
}

// createGroup asks the backend for the group; groupCreated opens it.
func (u *UI) createGroup() {
	nc := &u.newChat
	name := trimSpace(nc.name.Text())
	if name == "" {
		u.toast("Give the group a name first.")
		u.requestFocus(&nc.name)
		return
	}
	if nc.creating || len(nc.picked) == 0 {
		return
	}
	g := model.NewGroup{Name: name, Photo: nc.photo, Disappearing: nc.disappearing}
	for _, c := range nc.picked {
		g.Members = append(g.Members, c.ID)
	}
	nc.creating = true
	u.backend.CreateGroup(g)
}

// groupCreated answers createGroup.
func (u *UI) groupCreated(e model.GroupCreatedEvent) {
	nc := &u.newChat
	if !nc.creating {
		return
	}
	nc.creating = false
	if e.Err != "" {
		u.toast(e.Err)
		return
	}
	name := trimSpace(nc.name.Text())
	nc.step = ncNone
	c := u.chatByID(e.ChatID)
	if c == nil {
		c = &model.Chat{ID: e.ChatID, Name: name, IsGroup: true, Time: u.now()}
	}
	u.setPage(pageChats)
	u.open(c)
	u.requestFocus(&u.conv.composer)
}

// pickGroupPhoto opens the file dialog for the group's picture, and crops
// what it returns in the background.
func (u *UI) pickGroupPhoto() {
	nc := &u.newChat
	if nc.photoBusy {
		return
	}
	if nc.photos == nil {
		nc.photos = make(chan groupPhoto, 1)
	}
	nc.photoBusy = true
	out, notify := nc.photos, u.images.invalidate
	go func() {
		var r groupPhoto
		paths, err := filepick.Open("Choose a group picture", false,
			filepick.Filter{Name: "Pictures", Exts: photoExts}, filepick.Filter{Name: "All files"})
		switch {
		case err != nil:
			r.err = err
		case len(paths) == 0:
			r.none = true
		default:
			var data []byte
			if data, r.err = os.ReadFile(paths[0]); r.err == nil {
				release := acquireDecode(data)
				r.data, r.err = photo.Square(data, groupPhotoPx)
				release()
			}
		}
		out <- r
		if notify != nil {
			notify()
		}
	}()
}

// groupPhotoItems is the menu of the picture on the New group page.
func (u *UI) groupPhotoItems() []menuItem {
	return []menuItem{
		{key: "gphoto:change", ic: icAddPhoto, label: "Change picture", run: u.pickGroupPhoto},
		{key: "gphoto:remove", ic: icDelete, label: "Remove picture", run: func() { u.newChat.photo = nil }},
	}
}

// groupTimerItems offers WhatsApp's disappearing message timers.
func (u *UI) groupTimerItems() []menuItem {
	var items []menuItem
	for _, secs := range []uint32{0, 86400, 7 * 86400, 90 * 86400} {
		secs := secs
		label := "Off"
		if secs > 0 {
			label = durationLabel(secs)
		}
		items = append(items, menuItem{key: "gtimer:" + itoa(int(secs)), label: label,
			tick: u.newChat.disappearing == secs, run: func() { u.newChat.disappearing = secs }})
	}
	return items
}

// phoneDigits returns the digits of s when s looks like a phone number
// ("+62 812-3456 7890"), or "".
func phoneDigits(s string) string {
	s = trimSpace(s)
	var b strings.Builder
	for i, r := range s {
		switch {
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '+' && i == 0, r == ' ', r == '-', r == '(', r == ')', r == '.':
		default:
			return ""
		}
	}
	if b.Len() < 7 || b.Len() > 15 {
		return ""
	}
	return b.String()
}

// matchContacts lists the contacts whose name or number matches q; picked
// contacts are left out when skipPicked is set.
func (u *UI) matchContacts(q string, skipPicked bool) []*model.Contact {
	nc := &u.newChat
	q = strings.ToLower(trimSpace(q))
	digits := phoneDigits(q)
	out := nc.shown[:0]
	for _, c := range nc.contacts {
		if skipPicked && nc.pickedAt(c.ID) >= 0 {
			continue
		}
		if q != "" && !strings.Contains(strings.ToLower(c.Name), q) &&
			(digits == "" || !strings.Contains(phoneDigits(c.Phone), digits)) {
			continue
		}
		out = append(out, c)
	}
	nc.shown = out
	return out
}

// layoutNewChat draws the panel's pages over the chat list, sliding in
// from the left.
func (u *UI) layoutNewChat(gtx C) {
	nc := &u.newChat
	p := u.pal
	sz := gtx.Constraints.Max
	var vs [ncGroup + 1]float32
	top := ncNone // the highest page fully in
	for s := ncChat; s <= ncGroup; s++ {
		vs[s] = nc.in[s].step(gtx, s >= nc.base && s <= nc.step, durPanel)
		if vs[s] == 1 {
			top = s
		}
	}
	if nc.step == ncNone && top == ncNone && vs[ncChat]+vs[ncMembers]+vs[ncGroup] == 0 {
		if nc.contacts != nil || nc.picked != nil {
			u.closeNewChat() // done sliding out: drop the contacts
		}
		return
	}
	for s := max(top, ncChat); s <= ncGroup; s++ {
		if vs[s] == 0 {
			continue
		}
		e := easeOut(vs[s])
		x := -int(float32(sz.X) * (1 - e))
		t := op.Offset(image.Pt(x, 0)).Push(gtx.Ops)
		pg := gtx
		pg.Constraints = layout.Exact(sz)
		done := func() {}
		if s != nc.step {
			pg, done = fadeOut(pg)
		} else {
			// Take the clicks the page doesn't use, so they don't
			// reach the chat list underneath.
			u.btn("nc:panel").Layout(pg, func(gtx C) D { return D{Size: sz} })
		}
		cl := clip.Rect{Max: sz}.Push(gtx.Ops)
		fillRect(gtx, image.Rectangle{Max: sz}, p.Panel)
		switch s {
		case ncChat:
			u.layoutNewChatPage(pg)
		case ncMembers:
			u.layoutMembersPage(pg)
		case ncGroup:
			u.layoutNewGroupPage(pg)
		}
		cl.Pop()
		done()
		t.Pop()
	}
}

// newChatCovers reports whether a page of the panel hides the chat list.
func (u *UI) newChatCovers() bool {
	nc := &u.newChat
	for s := ncChat; s <= ncGroup; s++ {
		if nc.in[s].v == 1 && s >= nc.base && s <= nc.step {
			return true
		}
	}
	return false
}

// ncHeader is a page's title row with a back arrow.
func (u *UI) ncHeader(gtx C, title string) D {
	p := u.pal
	return vcenter(gtx, gtx.Dp(68), func(gtx C) D {
		return layout.Inset{Left: 10, Right: 16}.Layout(gtx, func(gtx C) D {
			return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(func(gtx C) D { return u.iconButton(gtx, u.btn("nc:back"), icBack, 40, 24, p.Icon) }),
				layout.Rigid(layout.Spacer{Width: 10}.Layout),
				layout.Flexed(1, u.label(19, title, p.Text, labelOpts{weight: font.SemiBold, maxLines: 1}).Layout),
			)
		})
	})
}

// ncSearch is the search box under a page's header.
func (u *UI) ncSearch(gtx C, ed *widget.Editor, hint string) D {
	return layout.Inset{Left: 23, Right: 23, Bottom: 11}.Layout(gtx, func(gtx C) D {
		return u.searchField(gtx, ed, hint)
	})
}

// ncRow is a row of the panel: a 49dp picture, a title and a subtitle.
func (u *UI) ncRow(gtx C, c *widget.Clickable, pic layout.Widget, title, sub string) D {
	p := u.pal
	return layout.Inset{Left: 13, Right: 18}.Layout(gtx, func(gtx C) D {
		return clickable(gtx, c, func(gtx C) D {
			gtx.Constraints.Min.X = gtx.Constraints.Max.X
			bg := mix(p.Panel, p.Hover, u.hover(gtx, c))
			return background(gtx, bg, 10, func(gtx C) D {
				return vcenter(gtx, gtx.Dp(72), func(gtx C) D {
					return layout.Inset{Left: 11, Right: 14}.Layout(gtx, func(gtx C) D {
						return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
							layout.Rigid(pic),
							layout.Rigid(layout.Spacer{Width: 15}.Layout),
							layout.Flexed(1, func(gtx C) D {
								if sub == "" {
									return u.label(17, title, p.Text, labelOpts{maxLines: 1}).Layout(gtx)
								}
								return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
									layout.Rigid(u.label(17, title, p.Text, labelOpts{maxLines: 1}).Layout),
									layout.Rigid(layout.Spacer{Height: 2}.Layout),
									layout.Rigid(u.label(14.5, sub, p.TextSecondary, labelOpts{maxLines: 1}).Layout),
								)
							}),
						)
					})
				})
			})
		})
	})
}

// greenCircle is the picture of an action row ("New group").
func (u *UI) greenCircle(ic *icon.Icon) layout.Widget {
	return func(gtx C) D {
		px := gtx.Dp(49)
		fillCircle(gtx, image.Pt(px/2, px/2), px/2, u.pal.Green)
		return centerIn(gtx, px, iconW(ic, 26, u.pal.OnGreen))
	}
}

func (u *UI) contactPic(c *model.Contact) layout.Widget {
	return func(gtx C) D { return u.avatar(gtx, c.ID, c.Name, false, 49) }
}

// ncNote is gray text in a list ("No results").
func (u *UI) ncNote(gtx C, txt string) D {
	return layout.Inset{Left: 32, Right: 32, Top: 24, Bottom: 24}.Layout(gtx, func(gtx C) D {
		gtx.Constraints.Min.X = gtx.Constraints.Max.X
		l := u.label(15, txt, u.pal.TextSecondary, labelOpts{align: text.Middle})
		l.MaxLines = 0
		return l.Layout(gtx)
	})
}

// layoutNewChatPage is New chat: New group, yourself and your contacts,
// or what matches the search, with a row to message a typed number.
func (u *UI) layoutNewChatPage(gtx C) D {
	nc := &u.newChat
	q := trimSpace(nc.search.Text())
	var rows []layout.Widget
	if digits := phoneDigits(q); digits != "" {
		sub := "Message this number"
		if nc.lookup == digits {
			sub = "Checking…"
		}
		rows = append(rows, func(gtx C) D {
			return u.ncRow(gtx, u.btn("nc:phone"), func(gtx C) D { return u.avatar(gtx, "", "", false, 49) }, "+"+digits, sub)
		})
	}
	if q == "" {
		rows = append(rows, func(gtx C) D {
			return layout.Inset{Top: 4}.Layout(gtx, func(gtx C) D {
				return u.ncRow(gtx, u.btn("nc:group"), u.greenCircle(icGroupAdd), "New group", "")
			})
		})
		for _, c := range u.chats {
			if c.Self {
				c := c
				rows = append(rows, func(gtx C) D {
					return u.ncRow(gtx, u.btn("nc:self"), func(gtx C) D { return u.avatar(gtx, c.ID, c.Name, false, 49) },
						c.Name+" (You)", "Message yourself")
				})
				break
			}
		}
	}
	contacts := u.matchContacts(q, false)
	if len(contacts) > 0 {
		rows = append(rows, func(gtx C) D {
			return u.sectionLabel(gtx, "Contacts on WhatsApp", layout.Inset{Left: 32, Right: 29, Top: 18, Bottom: 12}, labelOpts{})
		})
	}
	for _, c := range contacts {
		rows = append(rows, func(gtx C) D {
			return u.ncRow(gtx, u.btn("nc:c:"+c.ID), u.contactPic(c), c.Name, c.Phone)
		})
	}
	if len(contacts) == 0 {
		switch {
		case q != "" && len(rows) == 0:
			rows = append(rows, func(gtx C) D { return u.ncNote(gtx, "No results found for '"+q+"'") })
		case q == "" && len(nc.contacts) == 0:
			rows = append(rows, func(gtx C) D {
				return u.ncNote(gtx, "Your contacts on WhatsApp show here once your phone has synced them. Type a phone number with its country code to message it.")
			})
		}
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx C) D { return u.ncHeader(gtx, "New chat") }),
		layout.Rigid(func(gtx C) D { return u.ncSearch(gtx, &nc.search, "Search name or number") }),
		layout.Flexed(1, func(gtx C) D {
			return u.scrollList(gtx, &nc.list, len(rows), func(gtx C, i int) D { return rows[i](gtx) })
		}),
	)
}

// layoutMembersPage is Add group members: the picked members as chips
// above a search field, the other contacts below, and a green arrow.
func (u *UI) layoutMembersPage(gtx C) D {
	nc := &u.newChat
	p := u.pal
	q := trimSpace(nc.members.Text())
	contacts := u.matchContacts(q, true)
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx C) D { return u.ncHeader(gtx, "Add group members") }),
		layout.Rigid(func(gtx C) D {
			return layout.Inset{Left: 29, Right: 29, Bottom: 8}.Layout(gtx, func(gtx C) D {
				return u.memberChips(gtx)
			})
		}),
		layout.Flexed(1, func(gtx C) D {
			if len(contacts) == 0 {
				msg := "No contacts left to add"
				if q != "" {
					msg = "No results found for '" + q + "'"
				}
				return u.ncNote(gtx, msg)
			}
			return u.scrollList(gtx, &nc.mlist, len(contacts), func(gtx C, i int) D {
				c := contacts[i]
				return u.ncRow(gtx, u.btn("nc:m:"+c.ID), u.contactPic(c), c.Name, c.Phone)
			})
		}),
		layout.Rigid(func(gtx C) D {
			return u.ncButton(gtx, "nc:next", &nc.nextIn, icNext, len(nc.picked) > 0, false, p.Panel)
		}),
	)
}

// memberChips lays the picked members out as chips that wrap, followed by
// the search field, with a line under them like WhatsApp's.
func (u *UI) memberChips(gtx C) D {
	nc := &u.newChat
	p := u.pal
	w := gtx.Constraints.Max.X
	gap, rowGap := gtx.Dp(6), gtx.Dp(8)
	cgtx := gtx
	cgtx.Constraints = layout.Constraints{Max: image.Pt(w, gtx.Constraints.Max.Y)}
	var parts []part
	for _, c := range nc.picked {
		c := c
		parts = append(parts, record(cgtx, func(gtx C) D { return u.memberChip(gtx, c) }))
	}
	x, y, lineH := 0, 0, 0
	for _, pt := range parts {
		if x > 0 && x+pt.size.X > w {
			x, y, lineH = 0, y+lineH+rowGap, 0
		}
		pt.at(gtx, x, y)
		x += pt.size.X + gap
		lineH = max(lineH, pt.size.Y)
	}
	// The search field takes the rest of the last line, or a line of its own.
	if x > 0 && w-x < gtx.Dp(140) {
		x, y, lineH = 0, y+lineH+rowGap, 0
	}
	hint := "Search name or number"
	if len(nc.picked) > 0 {
		hint = ""
	}
	sgtx := gtx
	sgtx.Constraints = layout.Constraints{Min: image.Pt(w-x, 0), Max: image.Pt(w-x, gtx.Constraints.Max.Y)}
	ed := record(sgtx, func(gtx C) D {
		return layout.Inset{Top: 8, Bottom: 8}.Layout(gtx, func(gtx C) D {
			e := material.Editor(u.th, &nc.members, hint)
			e.TextSize = 15.5
			e.Color = p.Text
			e.HintColor = p.TextSecondary
			return e.Layout(gtx)
		})
	})
	edY := y
	if lineH > ed.size.Y {
		edY += (lineH - ed.size.Y) / 2
	}
	ed.at(gtx, x, edY)
	h := max(y+lineH, edY+ed.size.Y) + gtx.Dp(6)
	line, col := max(1, gtx.Dp(1)), p.Divider
	if gtx.Focused(&nc.members) {
		line, col = gtx.Dp(2), p.Green
	}
	fillRect(gtx, image.Rect(0, h-line, w, h), col)
	return D{Size: image.Pt(w, h)}
}

// memberChip is a picked member: picture, name and a cross to unpick.
func (u *UI) memberChip(gtx C, c model.Contact) D {
	p := u.pal
	h := gtx.Dp(28)
	name := record(gtx, u.label(14.5, c.Name, p.Text, labelOpts{maxLines: 1}).Layout)
	maxName := gtx.Constraints.Max.X - h - gtx.Dp(8+28)
	nameW := min(name.size.X, max(0, maxName))
	w := h + gtx.Dp(8) + nameW + gtx.Dp(4+24+2)
	fillRRect(gtx, image.Rect(0, 0, w, h), h/2, p.Chip)
	u.avatar(gtx, c.ID, c.Name, false, 28)
	cl := clip.Rect{Max: image.Pt(h+gtx.Dp(8)+nameW, h)}.Push(gtx.Ops)
	name.at(gtx, h+gtx.Dp(8), (h-name.size.Y)/2)
	cl.Pop()
	t := op.Offset(image.Pt(w-gtx.Dp(26), (h-gtx.Dp(24))/2)).Push(gtx.Ops)
	u.iconButton(gtx, u.btn("nc:x:"+c.ID), icClose, 24, 16, p.TextSecondary)
	t.Pop()
	return D{Size: image.Pt(w, h)}
}

// ncButton is the green round button at the bottom of a page, which
// rises in while on. busy grays it out.
func (u *UI) ncButton(gtx C, key string, in *tween, ic *icon.Icon, on, busy bool, bg color.NRGBA) D {
	p := u.pal
	v := easeOut(in.step(gtx, on, durGrow))
	h := gtx.Dp(96)
	w := gtx.Constraints.Max.X
	if v == 0 {
		return D{Size: image.Pt(w, lerpInt(0, h, v))}
	}
	sz := gtx.Dp(54)
	c := u.btn(key)
	t := op.Offset(image.Pt((w-sz)/2, (h-sz)/2+int(float32(h)*(1-v)))).Push(gtx.Ops)
	bgtx := gtx
	if !on || busy {
		var done func()
		bgtx, done = fadeOut(bgtx)
		defer done()
	}
	clickable(bgtx, c, func(gtx C) D {
		col := mix(p.Green, p.Text, 0.1*u.hover(gtx, c))
		if busy {
			col = mix(col, bg, 0.5)
		}
		fillCircle(gtx, image.Pt(sz/2, sz/2), sz/2, col)
		return centerIn(gtx, sz, iconW(ic, 28, p.OnGreen))
	})
	t.Pop()
	return D{Size: image.Pt(w, h)}
}

// layoutNewGroupPage is New group: the picture, the name and the
// disappearing messages timer, then a green check to create it.
func (u *UI) layoutNewGroupPage(gtx C) D {
	nc := &u.newChat
	p := u.pal
	timer := "Off"
	if nc.disappearing > 0 {
		timer = durationLabel(nc.disappearing)
	}
	members := itoa(len(nc.picked)) + " members"
	if len(nc.picked) == 1 {
		members = "1 member"
	}
	rows := []layout.Widget{
		func(gtx C) D {
			return layout.Inset{Top: 28, Bottom: 28}.Layout(gtx, func(gtx C) D {
				gtx.Constraints.Min.X = gtx.Constraints.Max.X
				return layout.N.Layout(gtx, u.groupPhotoButton)
			})
		},
		func(gtx C) D {
			return layout.Inset{Left: 6, Right: 6}.Layout(gtx, func(gtx C) D {
				return u.pollField(gtx, &nc.name, "Group name")
			})
		},
		func(gtx C) D {
			n := len([]rune(nc.name.Text()))
			return layout.Inset{Left: 30, Right: 30, Top: 6}.Layout(gtx, func(gtx C) D {
				gtx.Constraints.Min.X = gtx.Constraints.Max.X
				return u.label(13, itoa(groupNameMax-n), p.TextSecondary, labelOpts{align: text.End, maxLines: 1}).Layout(gtx)
			})
		},
		func(gtx C) D { return layout.Spacer{Height: 18}.Layout(gtx) },
		func(gtx C) D {
			return u.layoutListItem(gtx, u.btn("nc:timer"),
				listItem{glyph: disappearingIcon, title: "Disappearing messages", sub: timer}, settingsGeom)
		},
		func(gtx C) D {
			return u.sectionLabel(gtx, members, layout.Inset{Left: 32, Right: 29, Top: 18, Bottom: 8}, labelOpts{})
		},
	}
	for _, c := range nc.picked {
		c := c
		rows = append(rows, func(gtx C) D {
			return layout.Inset{Left: 30, Right: 29, Top: 6, Bottom: 6}.Layout(gtx, func(gtx C) D {
				return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(func(gtx C) D { return u.avatar(gtx, c.ID, c.Name, false, 36) }),
					layout.Rigid(layout.Spacer{Width: 14}.Layout),
					layout.Flexed(1, u.label(15.5, c.Name, p.Text, labelOpts{maxLines: 1}).Layout),
				)
			})
		})
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx C) D { return u.ncHeader(gtx, "New group") }),
		layout.Flexed(1, func(gtx C) D {
			return u.scrollList(gtx, &nc.glist, len(rows), func(gtx C, i int) D { return rows[i](gtx) })
		}),
		layout.Rigid(func(gtx C) D {
			return u.ncButton(gtx, "nc:create", &nc.createIn, icTick, trimSpace(nc.name.Text()) != "", nc.creating, p.Panel)
		}),
	)
}

// groupPhotoButton is the new group's picture, or a gray circle asking
// for one.
func (u *UI) groupPhotoButton(gtx C) D {
	nc := &u.newChat
	p := u.pal
	c := u.btn("nc:photo")
	px := gtx.Dp(156)
	return clickable(gtx, c, func(gtx C) D {
		r := image.Rect(0, 0, px, px)
		if nc.photo != nil {
			data := nc.photo
			if e := u.images.get("ngp:"+itoa(nc.photoN), avatarPx*2, func() []byte { return data }); e.state == imgReady {
				cl := clip.UniformRRect(r, px/2).Push(gtx.Ops)
				paintCover(gtx, e.op, e.size, r)
				// Darken on hover, like WhatsApp, to say it can change.
				fillRect(gtx, r, faded(rgb(0x000000), 0.3*u.hover(gtx, c)))
				cl.Pop()
				return D{Size: r.Size()}
			}
		}
		fillCircle(gtx, image.Pt(px/2, px/2), px/2, mix(p.UserAvatar, p.Text, 0.06*u.hover(gtx, c)))
		inner := record(gtx, func(gtx C) D {
			gtx.Constraints = layout.Constraints{Max: image.Pt(px-gtx.Dp(24), px)}
			return layout.Flex{Axis: layout.Vertical, Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(iconW(icAddPhoto, 32, p.UserAvatarIcon)),
				layout.Rigid(layout.Spacer{Height: 8}.Layout),
				layout.Rigid(func(gtx C) D {
					l := u.label(12.5, "ADD GROUP ICON", p.UserAvatarIcon, labelOpts{weight: font.Medium, align: text.Middle})
					l.MaxLines = 2
					return l.Layout(gtx)
				}),
			)
		})
		inner.at(gtx, (px-inner.size.X)/2, (px-inner.size.Y)/2)
		return D{Size: r.Size()}
	})
}
