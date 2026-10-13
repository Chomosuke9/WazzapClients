package ui

import (
	"image"
	"os"
	"strconv"
	"time"

	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/text"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"github.com/chomosuke9/wazzapclients/internal/filepick"
	"github.com/chomosuke9/wazzapclients/internal/model"
	"github.com/chomosuke9/wazzapclients/internal/photo"
)

// Editing a group's name, description and picture from its info panel,
// like WhatsApp: the pencils turn the name or the description into an
// editor in place, and the picture opens a menu (view, change, remove).
// Members can edit unless only admins may (ChatInfo.Locked).

// The fields a group's info panel edits (groupEdit.field).
const (
	editGroupName = iota + 1
	editGroupDesc
)

// WhatsApp's limits on a group's name and description.
const (
	maxGroupNameLen = 100
	maxGroupDescLen = 2048
)

type groupEdit struct {
	field  int    // being edited, or 0
	chatID string // whose
	editor widget.Editor
	save   widget.Clickable

	photoBusy bool   // the file dialog or the cropping is running
	photoChat string // the group the picture is for
	photos    chan groupPhoto

	// pending is what was saved, shown until the group's info has it
	// (or pendingFor passed: someone else changed it meanwhile).
	pending map[pendingKey]pendingValue
}

type pendingKey struct {
	chatID string
	field  int
}

type pendingValue struct {
	text string
	at   time.Time
}

const pendingFor = 30 * time.Second

// canEditGroup reports whether you may change the group's info.
func (u *UI) canEditGroup(c *model.Chat, info *model.ChatInfo) bool {
	if !info.IsGroup || c == nil || c.Left {
		return false
	}
	if chat := u.chatByID(c.ID); chat == nil || chat.Left {
		return false // a group you aren't in any more, or never were
	}
	return !info.Locked || u.isAdminIn(info)
}

// mayEditGroup is canEditGroup, saying why not when you can't.
func (u *UI) mayEditGroup(c *model.Chat, info *model.ChatInfo) bool {
	if u.canEditGroup(c, info) {
		return true
	}
	if chat := u.chatByID(c.ID); chat == nil || chat.Left {
		u.inform("You're not a member", "You can't change the info of a group you're no longer in.")
	} else {
		u.inform("Only admins can edit this group's info", "Only group admins can change this group's name, picture and description.")
	}
	return false
}

// startGroupEdit turns field into an editor holding its value.
func (u *UI) startGroupEdit(c *model.Chat, info *model.ChatInfo, field int) {
	if !u.mayEditGroup(c, info) {
		return
	}
	e := &u.info.edit
	e.field, e.chatID = field, c.ID
	ed := &e.editor
	value := info.Name
	ed.SingleLine, ed.Submit, ed.MaxLen = true, true, maxGroupNameLen
	if field == editGroupDesc {
		// Enter saves (updateGroupEdit) and Shift+Enter starts a line.
		// Submit would cut pasted text at its first line break.
		value = info.About
		ed.SingleLine, ed.Submit, ed.MaxLen = false, false, maxGroupDescLen
	}
	ed.SetText(value)
	n := ed.Len()
	ed.SetCaret(n, n)
	u.requestFocus(ed)
}

func (u *UI) cancelGroupEdit() { u.info.edit.field = 0 }

// updateGroupEdit reads the editor's keys before it's drawn, and takes
// the picture the Open dialog returned.
func (u *UI) updateGroupEdit(gtx C) {
	e := &u.info.edit
	if e.photos != nil {
		select {
		case r := <-e.photos:
			e.photoBusy = false
			switch {
			case r.err != nil:
				u.toast("That picture can't be used.")
			case !r.none:
				u.setGroupPhoto(e.photoChat, r.data)
			}
		default:
		}
	}
	if e.field == 0 {
		return
	}
	if !u.info.open || u.info.chatID != e.chatID || u.info.sub != "" {
		e.field = 0 // the panel moved on
		return
	}
	ed := &e.editor
	save := e.save.Clicked(gtx)
	if e.field == editGroupDesc {
		for {
			ev, ok := gtx.Event(key.Filter{Focus: ed, Name: key.NameReturn}, key.Filter{Focus: ed, Name: key.NameEnter})
			if !ok {
				break
			}
			if ke, ok := ev.(key.Event); ok && ke.State == key.Press {
				save = true
			}
		}
	}
	for {
		ev, ok := ed.Update(gtx)
		if !ok {
			break
		}
		if _, ok := ev.(widget.SubmitEvent); ok {
			save = true
		}
	}
	if save {
		u.saveGroupEdit()
	}
}

// saveGroupEdit sends the edited field to WhatsApp.
func (u *UI) saveGroupEdit() {
	e := &u.info.edit
	field, id := e.field, e.chatID
	v := trimSpace(e.editor.Text())
	if field == editGroupName && v == "" {
		u.toast("The group name can't be empty.")
		return
	}
	e.field = 0
	if info := u.info.data; info != nil {
		old := info.About
		if field == editGroupName {
			old = info.Name
		}
		if p, ok := e.pending[pendingKey{id, field}]; ok {
			old = p.text
		}
		if v == old {
			return
		}
	}
	action := model.GroupName
	if field == editGroupDesc {
		action = model.GroupDescription
	}
	if e.pending == nil {
		e.pending = map[pendingKey]pendingValue{}
	}
	k := pendingKey{id, field}
	e.pending[k] = pendingValue{text: v, at: u.now()}
	slashHost{u: u}.Group(model.GroupRequest{ChatID: id, Action: action, Text: v}, func(ev model.GroupEvent) {
		if ev.Err != "" {
			delete(u.info.edit.pending, k)
			u.toast(ev.Err)
		}
	})
}

// withPending returns info with the changes still on their way, or info
// itself when there are none.
func (u *UI) withPending(id string, info *model.ChatInfo) *model.ChatInfo {
	e := &u.info.edit
	if len(e.pending) == 0 {
		return info
	}
	out := info
	for _, field := range []int{editGroupName, editGroupDesc} {
		k := pendingKey{id, field}
		p, ok := e.pending[k]
		if !ok {
			continue
		}
		cur := &info.Name
		if field == editGroupDesc {
			cur = &info.About
		}
		if *cur == p.text || u.now().Sub(p.at) > pendingFor {
			delete(e.pending, k) // arrived, or overtaken
			continue
		}
		if out == info {
			cp := *info
			out = &cp
		}
		if field == editGroupName {
			out.Name = p.text
		} else {
			out.About = p.text
		}
	}
	return out
}

// groupFieldEditor is the name or description being edited: the editor,
// the characters left and a tick that saves.
func (u *UI) groupFieldEditor(gtx C, size unit.Sp) D {
	p := u.pal
	e := &u.info.edit
	d := layout.Flex{Alignment: layout.End}.Layout(gtx,
		layout.Flexed(1, func(gtx C) D {
			ed := material.Editor(u.th, &e.editor, "")
			ed.TextSize = size
			ed.Color = p.Text
			ed.SelectionColor = faded(p.Green, 0.35)
			if e.field == editGroupName {
				ed.Hint = "Group name"
			} else {
				ed.Hint = "Add group description"
				ed.HintColor = p.TextSecondary
			}
			return layout.Inset{Top: 6, Bottom: 6}.Layout(gtx, ed.Layout)
		}),
		layout.Rigid(func(gtx C) D {
			left := e.editor.MaxLen - e.editor.Len()
			return layout.Inset{Left: 8, Right: 4, Bottom: 10}.Layout(gtx,
				u.label(14, strconv.Itoa(left), p.TextSecondary).Layout)
		}),
		layout.Rigid(func(gtx C) D { return u.iconButton(gtx, &e.save, icTick, 40, 24, p.Icon) }),
	)
	h := max(1, gtx.Dp(2))
	fillRect(gtx, image.Rect(0, d.Size.Y-h, d.Size.X, d.Size.Y), p.Green)
	return d
}

// infoNameEditor takes the place of the group's name in its info panel.
func (u *UI) infoNameEditor(gtx C, width int) D {
	gtx.Constraints.Min.X, gtx.Constraints.Max.X = width, width
	return layout.Inset{Left: 30, Right: 22}.Layout(gtx, func(gtx C) D {
		e := &u.info.edit
		e.editor.Alignment = text.Start
		return u.groupFieldEditor(gtx, 20)
	})
}

// infoPhotoClicked answers a click on the big picture of the info panel:
// a group's opens its menu when you may change it, and any other shows
// the picture, if there is one.
func (u *UI) infoPhotoClicked(gtx C, cl *widget.Clickable, size int, c *model.Chat, info *model.ChatInfo) {
	kind := avatarPerson
	if info.IsGroup {
		kind = avatarGroup
	}
	has := u.avatarImage(c.ID) != nil
	switch {
	case info.IsGroup && u.canEditGroup(c, info):
		u.ctx = ctxMenu{kind: ctxInfoPhoto, chatID: c.ID, at: u.mouse}
	case has:
		u.openPicView(c.ID, info.Name, kind, u.viewerOrigin(gtx, cl, image.Pt(size, size)))
	}
}

// infoPhotoItems is the menu of a group's picture in its info panel.
func (u *UI) infoPhotoItems(id string) []menuItem {
	info := u.info.data
	if info == nil || u.info.chatID != id {
		return nil
	}
	info = u.withPending(id, info)
	var items []menuItem
	has := u.avatarImage(id) != nil
	if has {
		name := info.Name
		items = append(items, menuItem{key: "iphoto:view", ic: icImage, label: "View photo",
			run: func() { u.openPicView(id, name, avatarGroup, image.Rectangle{}) }})
	}
	label := "Upload photo"
	if has {
		label = "Change photo"
	}
	items = append(items, menuItem{key: "iphoto:change", ic: icAddPhoto, label: label, run: func() { u.pickInfoPhoto(id) }})
	if has {
		items = append(items, menuItem{key: "iphoto:remove", ic: icDelete, label: "Remove photo", run: func() {
			u.confirm("Remove group photo?", "",
				dialogButton{label: "Remove", danger: true, run: func() { u.setGroupPhoto(id, nil) }})
		}})
	}
	return items
}

// pickInfoPhoto opens the file dialog for a group's new picture, and
// crops what it returns in the background.
func (u *UI) pickInfoPhoto(chatID string) {
	e := &u.info.edit
	if e.photoBusy {
		return
	}
	if e.photos == nil {
		e.photos = make(chan groupPhoto, 1)
	}
	e.photoBusy, e.photoChat = true, chatID
	pickSquarePhoto("Choose a group picture", e.photos, u.images.invalidate)
}

// pickSquarePhoto opens the file dialog and sends the picture picked,
// cropped to a square JPEG, to out.
func pickSquarePhoto(title string, out chan groupPhoto, notify func()) {
	go func() {
		var r groupPhoto
		paths, err := filepick.Open(title, false,
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

// setGroupPhoto sends a group's new picture (nil removes it). The info
// panel shows it when the backend's AvatarEvent comes.
func (u *UI) setGroupPhoto(chatID string, data []byte) {
	slashHost{u: u}.Group(model.GroupRequest{ChatID: chatID, Action: model.GroupPhoto, Photo: data}, func(ev model.GroupEvent) {
		switch {
		case ev.Err != "":
			u.toast(ev.Err)
		case data == nil:
			u.toast("Group photo removed")
		default:
			u.toast("Group photo updated")
		}
	})
}
