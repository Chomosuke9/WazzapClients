package ui

import (
	"image"
	"os"
	"path/filepath"
	"strings"

	"gioui.org/font"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"github.com/chomosuke9/wazzapclients/internal/filepick"
	"github.com/chomosuke9/wazzapclients/internal/model"
	"github.com/chomosuke9/wazzapclients/internal/ui/icon"
)

// The composer's attach menu: files picked with the system's dialog wait
// in a tray above the message box and go out with the next send, the
// typed text as their caption. Contacts and polls open their own dialogs.

// attachState holds the files picked to send.
type attachState struct {
	files   []model.Attachment
	results chan pickResult // from the file dialog's goroutine
	picking bool            // a file dialog is open
}

type pickResult struct {
	chatID string
	files  []model.Attachment
	err    error
}

// Extensions the attach menu's dialogs offer, and how each is sent.
var (
	photoExts = []string{"jpg", "jpeg", "png", "webp", "gif"}
	videoExts = []string{"mp4", "m4v", "3gp"}
	audioExts = []string{"mp3", "m4a", "aac", "ogg", "opus", "oga", "wav", "amr"}
)

func hasExt(path string, exts []string) bool {
	ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), ".")
	for _, e := range exts {
		if e == ext {
			return true
		}
	}
	return false
}

// pollIcon is three bars of different lengths, like WhatsApp's.
var pollIcon = func() *icon.Icon {
	bar := func(y, w string) string {
		return "M4 " + y + "h" + w + "q1.5 0 1.5 1.5t-1.5 1.5h-" + w + "q-1.5 0-1.5-1.5t1.5-1.5Z"
	}
	ic, err := icon.Parse(bar("4.5", "9")+bar("10.5", "14")+bar("16.5", "6"), 0, 0, 24)
	if err != nil {
		panic(err)
	}
	return ic
}()

// Colors of the attach menu's icons, from WhatsApp.
var (
	attachDocument = rgb(0x7f66ff)
	attachPhotos   = rgb(0x007bfc)
	attachAudio    = rgb(0xfa6533)
	attachContact  = rgb(0x009de2)
	attachPoll     = rgb(0xffbc38)
)

// openAttachMenu opens the attach menu above the pointer (the attach
// button).
func (u *UI) openAttachMenu() {
	if u.selected == nil {
		return
	}
	u.ctx = ctxMenu{kind: ctxAttach, chatID: u.selected.ID, at: u.mouse}
}

// attachMenuItems mirrors WhatsApp's attach menu. Camera, Event and New
// sticker are left out: the app has no camera capture, event messages or
// sticker maker.
func (u *UI) attachMenuItems(c *model.Chat) []menuItem {
	items := []menuItem{
		{key: "doc", ic: icDocumentFill, col: attachDocument, label: "Document", run: func() {
			u.pickFiles(c.ID, "Choose documents", nil)
		}},
		{key: "photos", ic: icPhotosFill, col: attachPhotos, label: "Photos & videos", run: func() {
			u.pickFiles(c.ID, "Choose photos and videos", []filepick.Filter{
				{Name: "Photos and videos", Exts: append(append([]string(nil), photoExts...), videoExts...)},
			})
		}},
		{key: "audio", ic: icHeadphonesFill, col: attachAudio, label: "Audio", run: func() {
			u.pickFiles(c.ID, "Choose audio", []filepick.Filter{{Name: "Audio", Exts: audioExts}})
		}},
	}
	items = append(items,
		menuItem{key: "contact", ic: icPerson, col: attachContact, label: "Contact", run: func() { u.openContactPicker() }},
		menuItem{key: "poll", ic: pollIcon, col: attachPoll, label: "Poll", run: func() { u.openPoll() }})
	return items
}

// pickFiles opens the system's file dialog in the background. Files of
// the dialog's filters go out as photos, videos or audio; everything else
// (and anything from "Document") as a document.
func (u *UI) pickFiles(chatID, title string, filters []filepick.Filter) {
	a := &u.attach
	if a.picking {
		return
	}
	if a.results == nil {
		a.results = make(chan pickResult, 1)
	}
	a.picking = true
	notify, results := u.images.invalidate, a.results
	asDocs := filters == nil
	if asDocs {
		filters = []filepick.Filter{{Name: "All files"}}
	} else {
		filters = append(filters, filepick.Filter{Name: "All files"})
	}
	go func() {
		paths, err := filepick.Open(title, true, filters...)
		r := pickResult{chatID: chatID, err: err}
		for _, p := range paths {
			media := model.MediaDocument
			switch {
			case asDocs:
			case hasExt(p, photoExts):
				media = model.MediaImage
			case hasExt(p, videoExts):
				media = model.MediaVideo
			case hasExt(p, audioExts):
				media = model.MediaAudio
			}
			r.files = append(r.files, model.Attachment{Path: p, Media: media})
		}
		results <- r
		if notify != nil {
			notify()
		}
	}()
}

// updateAttach takes the files the dialog returned.
func (u *UI) updateAttach() {
	a := &u.attach
	select {
	case r := <-a.results:
		a.picking = false
		switch {
		case r.err == filepick.ErrUnsupported:
			u.toast("No file dialog found. Install zenity or kdialog to attach files.")
		case r.err != nil:
			u.toast("Couldn't open the file dialog: " + r.err.Error())
		case u.selected != nil && u.selected.ID == r.chatID && len(r.files) > 0:
			a.files = append(a.files, r.files...)
			u.requestFocus(&u.conv.composer)
		}
	default:
	}
}

// sendAttachments sends the picked files; the first one carries the draft
// (caption, reply and mentions).
func (u *UI) sendAttachments(d model.Draft) {
	files := u.attach.files
	u.attach.files = nil
	for i, a := range files {
		dd := d
		if i > 0 {
			dd = model.Draft{}
		}
		if m := u.backend.SendFile(u.selected.ID, a, dd); m != nil {
			if u.chatByID(m.ChatID) == nil {
				u.chats = append(u.chats, u.selected)
			}
			u.upsertMessage(m)
		}
	}
}

// layoutAttachTray shows the picked files above the message box, each
// with a button to take it out.
func (u *UI) layoutAttachTray(gtx C) D {
	a := &u.attach
	for i := len(a.files) - 1; i >= 0; i-- {
		if u.btn("attach:x:" + itoa(i)).Clicked(gtx) {
			a.files = append(a.files[:i:i], a.files[i+1:]...)
		}
	}
	if len(a.files) == 0 {
		return D{}
	}
	return layout.Inset{Left: 8, Right: 8, Top: 8}.Layout(gtx, func(gtx C) D {
		maxW := gtx.Constraints.Max.X
		gap := gtx.Dp(8)
		chipW := min(maxW, gtx.Dp(250))
		x, y, rowH := 0, 0, 0
		for i, f := range a.files {
			chip := record(gtx, func(gtx C) D { return u.attachChip(gtx, i, f, chipW) })
			if x > 0 && x+chip.size.X > maxW {
				x, y = 0, y+rowH+gap
				rowH = 0
			}
			chip.at(gtx, x, y)
			x += chip.size.X + gap
			rowH = max(rowH, chip.size.Y)
		}
		return D{Size: image.Pt(maxW, y+rowH)}
	})
}

func (u *UI) attachChip(gtx C, i int, f model.Attachment, w int) D {
	p := u.pal
	h := gtx.Dp(52)
	fillRRect(gtx, image.Rect(0, 0, w, h), gtx.Dp(8), p.QuoteIn)
	pic := gtx.Dp(40)
	px, py := gtx.Dp(6), (h-pic)/2
	r := image.Rect(px, py, px+pic, py+pic)
	switch f.Media {
	case model.MediaImage:
		path := f.Path
		e := u.images.get("f:"+path, gtx.Dp(80), func() []byte {
			data, _ := os.ReadFile(path)
			return data
		})
		if e.state == imgReady {
			func() {
				defer clip.UniformRRect(r, gtx.Dp(4)).Push(gtx.Ops).Pop()
				paintCover(gtx, e.op, e.size, r)
			}()
			break
		}
		fillRRect(gtx, r, gtx.Dp(4), faded(attachPhotos, 0.25))
		t := op.Offset(r.Min.Add(image.Pt((pic-gtx.Dp(24))/2, (pic-gtx.Dp(24))/2))).Push(gtx.Ops)
		drawIcon(gtx, icImage, 24, attachPhotos)
		t.Pop()
	default:
		ic, col := icDocumentFill, attachDocument
		switch f.Media {
		case model.MediaVideo:
			ic, col = icVideo, attachPhotos
		case model.MediaAudio:
			ic, col = icHeadphonesFill, attachAudio
		}
		fillRRect(gtx, r, gtx.Dp(4), faded(col, 0.25))
		t := op.Offset(r.Min.Add(image.Pt((pic-gtx.Dp(24))/2, (pic-gtx.Dp(24))/2))).Push(gtx.Ops)
		drawIcon(gtx, ic, 24, col)
		t.Pop()
	}
	// Name and size, then the remove button.
	xb := gtx.Dp(32)
	tx := px + pic + gtx.Dp(10)
	tg := gtx
	tg.Constraints = layout.Constraints{Max: image.Pt(max(0, w-tx-xb-gtx.Dp(4)), h)}
	size := ""
	if st, err := os.Stat(f.Path); err == nil {
		size = formatSize(st.Size())
	}
	text := record(tg, func(gtx C) D {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(u.label(14, filepath.Base(f.Path), p.Text, labelOpts{maxLines: 1}).Layout),
			layout.Rigid(u.label(12, size, p.TextSecondary, labelOpts{maxLines: 1}).Layout),
		)
	})
	text.at(gtx, tx, (h-text.size.Y)/2)
	t := op.Offset(image.Pt(w-xb-gtx.Dp(4), (h-xb)/2)).Push(gtx.Ops)
	u.iconButton(gtx, u.btn("attach:x:"+itoa(i)), icClose, 32, 18, p.Icon)
	t.Pop()
	return D{Size: image.Pt(w, h)}
}

// openContactPicker opens the forward picker to choose contacts to share.
func (u *UI) openContactPicker() {
	u.openForward(nil)
	u.dialog.contacts = true
}

// openShareContact opens the forward picker to send a contact's card to
// other chats.
func (u *UI) openShareContact(id string) {
	u.openForward(nil)
	u.dialog.share = id
}

// pollState is the poll being written in the poll dialog.
type pollState struct {
	question widget.Editor
	options  []*widget.Editor
	multiple bool
	list     widget.List
}

// maxPollOptions is WhatsApp's limit.
const maxPollOptions = 12

func (u *UI) openPoll() {
	u.dialog = dialogState{kind: dialogPoll}
	pl := &u.dialog.poll
	pl.question.SingleLine = true
	pl.question.Submit = true
	pl.multiple = true
	pl.list.Axis = layout.Vertical
	for range 2 {
		pl.options = append(pl.options, &widget.Editor{SingleLine: true, Submit: true})
	}
	u.requestFocus(&pl.question)
}

// poll returns the poll to send, or false while it lacks a question
// or two options.
func (pl *pollState) poll() (model.Poll, bool) {
	q := model.Poll{Question: trimSpace(pl.question.Text()), Multiple: pl.multiple}
	for _, ed := range pl.options {
		if o := trimSpace(ed.Text()); o != "" {
			q.Options = append(q.Options, o)
		}
	}
	return q, q.Question != "" && len(q.Options) >= 2
}

// pollPanel is the "Create poll" dialog: a question, options that grow as
// you fill them, and whether voters may pick several.
func (u *UI) pollPanel(gtx C) D {
	d := &u.dialog
	pl := &d.poll
	p := u.pal
	if u.btn("poll:close").Clicked(gtx) {
		u.closeDialog()
	}
	if u.btn("poll:multi").Clicked(gtx) {
		pl.multiple = !pl.multiple
	}
	// Enter moves to the next field.
	fields := append([]*widget.Editor{&pl.question}, pl.options...)
	for i, ed := range fields {
		for {
			ev, ok := ed.Update(gtx)
			if !ok {
				break
			}
			if _, ok := ev.(widget.SubmitEvent); ok && i+1 < len(fields) {
				u.requestFocus(fields[i+1])
			}
		}
	}
	// A filled last option makes room for another.
	if n := len(pl.options); n < maxPollOptions && trimSpace(pl.options[n-1].Text()) != "" {
		pl.options = append(pl.options, &widget.Editor{SingleLine: true, Submit: true})
	}
	poll, ok := pl.poll()
	if u.btn("poll:send").Clicked(gtx) && ok && d.isOpen() && u.selected != nil {
		if m := u.backend.SendPoll(u.selected.ID, poll); m != nil {
			u.upsertMessage(m)
			u.scrollMessages(layout.Position{})
		}
		u.closeDialog()
	}

	w := min(gtx.Dp(460), gtx.Constraints.Max.X-gtx.Dp(32))
	h := min(gtx.Dp(600), gtx.Constraints.Max.Y-gtx.Dp(48))
	gtx.Constraints = layout.Exact(image.Pt(w, h))
	defer clip.UniformRRect(image.Rectangle{Max: image.Pt(w, h)}, gtx.Dp(16)).Push(gtx.Ops).Pop()
	heading := func(s string) layout.Widget {
		return func(gtx C) D {
			return layout.Inset{Left: 24, Right: 24, Top: 14, Bottom: 8}.Layout(gtx,
				u.label(14, s, p.TextSecondary, labelOpts{weight: font.Medium, maxLines: 1}).Layout)
		}
	}
	rows := []layout.Widget{
		heading("Question"),
		func(gtx C) D { return u.pollField(gtx, &pl.question, "Ask question") },
		heading("Options"),
	}
	for i, ed := range pl.options {
		hint := "Add"
		if i < 2 {
			hint = "Add option"
		}
		rows = append(rows, func(gtx C) D {
			return layout.Inset{Bottom: 8}.Layout(gtx, func(gtx C) D { return u.pollField(gtx, ed, hint) })
		})
	}
	rows = append(rows, func(gtx C) D {
		cl := u.btn("poll:multi")
		return clickable(gtx, cl, func(gtx C) D {
			gtx.Constraints.Min.X = gtx.Constraints.Max.X
			return background(gtx, mix(p.Dialog, p.Hover, u.hover(gtx, cl)), 0, func(gtx C) D {
				return vcenter(gtx, gtx.Dp(56), func(gtx C) D {
					return layout.Inset{Left: 24, Right: 24}.Layout(gtx, func(gtx C) D {
						box, col := icCheckBoxEmpty, p.TextSecondary
						if pl.multiple {
							box, col = icCheckBox, p.Green
						}
						return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
							layout.Flexed(1, u.label(15, "Allow multiple answers", p.Text, labelOpts{maxLines: 1}).Layout),
							layout.Rigid(iconW(box, 24, col)),
						)
					})
				})
			})
		})
	})
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx C) D {
			return vcenter(gtx, gtx.Dp(64), func(gtx C) D {
				return layout.Inset{Left: 12, Right: 20}.Layout(gtx, func(gtx C) D {
					return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
						layout.Rigid(func(gtx C) D { return u.iconButton(gtx, u.btn("poll:close"), icClose, 40, 24, p.IconStrong) }),
						layout.Rigid(layout.Spacer{Width: 12}.Layout),
						layout.Flexed(1, u.label(18, "Create poll", p.Text, labelOpts{weight: font.Medium, maxLines: 1}).Layout),
					)
				})
			})
		}),
		layout.Flexed(1, func(gtx C) D {
			return u.scrollList(gtx, &pl.list, len(rows), func(gtx C, i int) D { return rows[i](gtx) })
		}),
		layout.Rigid(func(gtx C) D {
			gtx.Constraints.Min.X = gtx.Constraints.Max.X
			return layout.Inset{Top: 8, Bottom: 16, Right: 20}.Layout(gtx, func(gtx C) D {
				return layout.E.Layout(gtx, func(gtx C) D {
					c := u.btn("poll:send")
					col := p.Green
					if !ok {
						col = mix(p.Green, p.Dialog, 0.6) // opaque: Gio blends in linear space
					}
					return clickable(gtx, c, func(gtx C) D {
						sz := gtx.Dp(52)
						fillCircle(gtx, image.Pt(sz/2, sz/2), sz/2, mix(col, p.Text, 0.1*u.hover(gtx, c)))
						return centerIn(gtx, sz, iconW(icSend, 24, p.OnGreen))
					})
				})
			})
		}),
	)
}

// pollField is a line of text with an underline that turns green while
// focused.
func (u *UI) pollField(gtx C, ed *widget.Editor, hint string) D {
	p := u.pal
	return layout.Inset{Left: 24, Right: 24}.Layout(gtx, func(gtx C) D {
		gtx.Constraints.Min.X = gtx.Constraints.Max.X
		m := op.Record(gtx.Ops)
		dims := layout.Inset{Top: 8, Bottom: 8}.Layout(gtx, func(gtx C) D {
			e := material.Editor(u.th, ed, hint)
			e.TextSize = 15.5
			e.Color = p.Text
			e.HintColor = p.TextSecondary
			return e.Layout(gtx)
		})
		call := m.Stop()
		call.Add(gtx.Ops)
		line, col := max(1, gtx.Dp(1)), p.Divider
		if gtx.Focused(ed) {
			line, col = gtx.Dp(2), p.Green
		}
		fillRect(gtx, image.Rect(0, dims.Size.Y-line, dims.Size.X, dims.Size.Y), col)
		return dims
	})
}
