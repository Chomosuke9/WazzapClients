package ui

import (
	"bytes"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gioui.org/font"
	"gioui.org/io/clipboard"
	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"github.com/chomosuke9/wazzapclients/internal/auto"
	"github.com/chomosuke9/wazzapclients/internal/filepick"
	"github.com/chomosuke9/wazzapclients/internal/model"
	"github.com/chomosuke9/wazzapclients/internal/osclip"
	"github.com/chomosuke9/wazzapclients/internal/photo"
)

// The composer's attach menu: files picked with the system's dialog,
// pasted or dropped on the window open the send view (sendview.go), where
// each gets a caption and photos can be edited before they go out.
// Contacts and polls open their own dialogs.

// attachState holds the files picked to send.
type attachState struct {
	files   []*attachFile
	cur     int             // the file the send view shows
	chatID  string          // the chat they go to
	draft   string          // the composer's text before; the composer holds captions meanwhile
	results chan pickResult // from the file dialog's goroutine, and pastes
	picking bool            // a file dialog is open

	// quality is how photos are sent. est holds, per photo, what each
	// quality makes of it, for the quality menu; estimating a photo
	// decodes it, so it waits until the menu opens.
	quality   model.Quality
	est       map[string]*photoEst
	estimates chan photoEst

	// The send view slides in and out; ghost is what it showed, while
	// it slides away.
	anim     tween
	ghost    []*attachFile
	ghostCur int
	ed       editState
	dropAnim tween // the hint while files are dragged over the window

	outbox  []*outItem      // files on their way out, see editrender.go
	renders chan *renderJob // edited photos being rendered

	demoEdit string // cmd/screenshot's sample edit still to make: edit, crop or filter
}

// attachFile is a file in the send view.
type attachFile struct {
	model.Attachment
	caption string
	edit    photoEdit
	orig    image.Point // a photo's full size, once the editor has decoded it
	temp    bool        // the app's own file (a pasted picture), removed once sent or dropped
}

// current returns the file the send view shows, or nil.
func (a *attachState) current() *attachFile {
	if a.cur >= 0 && a.cur < len(a.files) {
		return a.files[a.cur]
	}
	return nil
}

type photoEst struct {
	path string
	photo.Estimate
	ready bool // false while it is being worked out
}

type pickResult struct {
	chatID string
	files  []model.Attachment
	temp   bool // files the app wrote (pasted pictures)
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
		if !r.temp {
			a.picking = false
		}
		switch {
		case r.err == filepick.ErrUnsupported:
			u.toast("No file dialog found. Install zenity or kdialog to attach files.")
		case r.err != nil:
			u.toast("Couldn't open the file dialog: " + r.err.Error())
		default:
			files := make([]*attachFile, len(r.files))
			for i, f := range r.files {
				files[i] = &attachFile{Attachment: f, temp: r.temp}
			}
			u.addFiles(r.chatID, files)
		}
	default:
	}
	u.updateRenders()
	for {
		select {
		case e := <-a.estimates:
			if a.est[e.path] != nil {
				*a.est[e.path] = e
			}
			continue
		default:
		}
		break
	}
	if len(a.files) == 0 {
		a.est = nil // nothing left to estimate for
	}
}

// hasPhotos reports whether a picked file goes out as a photo.
func (a *attachState) hasPhotos() bool {
	for _, f := range a.files {
		if f.Media == model.MediaImage {
			return true
		}
	}
	return false
}

// openQualityMenu opens the photo quality menu above the pointer (the
// tray's quality button), and starts estimating the photos' sizes.
func (u *UI) openQualityMenu() {
	if len(u.attach.files) == 0 {
		return
	}
	u.ctx = ctxMenu{kind: ctxQuality, chatID: u.attach.chatID, at: u.mouse}
	a := &u.attach
	if a.est == nil {
		a.est = map[string]*photoEst{}
	}
	if a.estimates == nil {
		a.estimates = make(chan photoEst, 16)
	}
	var paths []string
	for _, f := range a.files {
		if f.Media == model.MediaImage && a.est[f.Path] == nil {
			a.est[f.Path] = &photoEst{path: f.Path}
			paths = append(paths, f.Path)
		}
	}
	if len(paths) == 0 {
		return
	}
	notify, out := u.images.invalidate, a.estimates
	go func() {
		for _, path := range paths {
			e := photoEst{path: path, ready: true}
			if data, err := os.ReadFile(path); err == nil {
				release := acquireDecode(data)
				e.Estimate, _ = photo.Estimates(data)
				release()
			}
			out <- e
			if notify != nil {
				notify()
			}
		}
	}()
}

// qualityMenuItems mirrors WhatsApp's photo quality menu, plus Raw when
// that extra feature is on.
func (u *UI) qualityMenuItems() []menuItem {
	a := &u.attach
	if !a.hasPhotos() {
		return nil
	}
	item := func(key, label string, q model.Quality) menuItem {
		return menuItem{key: key, label: label, sub: a.qualitySub(q), tick: a.quality == q, run: func() { a.quality = q }}
	}
	items := []menuItem{
		item("std", "Standard quality", model.QualityStandard),
		item("hd", "HD quality", model.QualityHD),
	}
	note := "HD photos are clearer. Standard photos use less storage space and are faster to send."
	if u.rawPhotos { // an extra feature
		items = append(items, item("raw", "Raw quality", model.QualityRaw))
		note += " Raw photos are sent as they are."
	}
	return append(items, menuItem{divider: true}, menuItem{note: true, label: note})
}

// qualitySub is a quality's size, "116 kB · 1600 x 900", or the total
// size for several photos.
func (a *attachState) qualitySub(q model.Quality) string {
	n, total := 0, 0
	var one *photoEst
	for _, f := range a.files {
		if f.Media != model.MediaImage {
			continue
		}
		e := a.est[f.Path]
		if e == nil || !e.ready {
			return "…"
		}
		n, total, one = n+1, total+e.Bytes[q], e
	}
	switch {
	case n == 0 || total == 0:
		return ""
	case n == 1:
		return formatSize(int64(total)) + " · " + itoa(one.W[q]) + " x " + itoa(one.H[q])
	}
	return formatSize(int64(total)) + " · " + itoa(n) + " photos"
}

var qualityNames = [...]string{model.QualityStandard: "Standard", model.QualityHD: "HD", model.QualityRaw: "Raw"}

// maxAttach is how many files WhatsApp sends at once.
const maxAttach = 100

// addFiles adds files to the send view, opening it, and shows the first
// new one.
func (u *UI) addFiles(chatID string, files []*attachFile) {
	a := &u.attach
	if len(files) == 0 {
		return
	}
	if u.ghostMode() {
		removeTemps(files)
		u.toast(auto.GhostText)
		return
	}
	if isStatusDestination(chatID) {
		if u.page != pageStatus || chatID != u.statusDestination() {
			// The Status page was left while the file dialog was open.
			removeTemps(files)
			return
		}
		if statusGroup(chatID) != "" {
			for _, f := range files {
				if f.Media == model.MediaDocument && hasExt(f.Path, audioExts) {
					f.Media = model.MediaAudio
				}
			}
		}
		files = u.statusFiles(files)
	} else if (u.selected == nil || u.selected.ID != chatID) && !isChannelID(chatID) && u.chatByID(chatID) != nil {
		// Another chat opened while the file dialog was open: the files
		// wait in the chat's draft.
		u.draftFiles(chatID, files)
		return
	}
	if len(a.files) > 0 && a.chatID != chatID || !isStatusDestination(chatID) &&
		(u.selected == nil || u.selected.ID != chatID || isChannelID(chatID)) {
		removeTemps(files)
		return
	}
	if len(files) == 0 {
		return
	}
	if len(a.files)+len(files) > maxAttach {
		u.toast("You can send up to " + itoa(maxAttach) + " files at once.")
		removeTemps(files[max(0, maxAttach-len(a.files)):])
		files = files[:max(0, maxAttach-len(a.files))]
		if len(files) == 0 {
			return
		}
	}
	if len(a.files) == 0 {
		// The composer becomes the caption field; its text waits.
		a.chatID, a.cur, a.ghost = chatID, 0, nil
		a.draft = u.conv.composer.Text()
		u.conv.composer.SetText("")
		u.resetEditor()
		u.closePicker()
		a.files = files
	} else {
		n := len(a.files)
		a.files = append(a.files, files...)
		u.showFile(n)
	}
	u.requestFocus(&u.conv.composer)
}

// classify tells how a file is sent: photos and videos as media, the
// rest as documents.
func classify(path string) model.Media {
	switch {
	case hasExt(path, photoExts):
		return model.MediaImage
	case hasExt(path, videoExts):
		return model.MediaVideo
	}
	return model.MediaDocument
}

// attachPaths adds dropped or pasted files to the open chat's send view.
func (u *UI) attachPaths(paths []string) {
	statusPage := u.page == pageStatus && (len(u.attach.files) == 0 || isStatusDestination(u.attach.chatID))
	if !statusPage && (u.selected == nil || isChannelID(u.selected.ID) || u.selPage != u.page) {
		u.toast("Open a chat to send files to it.")
		return
	}
	var files []*attachFile
	skipped := 0
	for _, p := range paths {
		st, err := os.Stat(p)
		if err != nil || st.IsDir() {
			skipped++
			continue
		}
		files = append(files, &attachFile{Attachment: model.Attachment{Path: p, Media: classify(p)}})
	}
	if skipped > 0 {
		u.toast("Folders can't be sent.")
	}
	if statusPage {
		// Dropped on the Status page, they become status updates.
		u.addFiles(u.statusDestination(), files)
		return
	}
	u.addFiles(u.selected.ID, files)
}

// pasteFiles attaches the files or the picture on the clipboard, and
// reports whether there were any. Text comes first: apps like Word put a
// picture of copied text on the clipboard too.
func (u *UI) pasteFiles() bool {
	if u.selected == nil || isChannelID(u.selected.ID) {
		return false
	}
	if paths := osclip.Files(); len(paths) > 0 {
		u.attachPaths(paths)
		return true
	}
	if osclip.HasText() {
		return false
	}
	img := osclip.Image()
	if img == nil {
		return false
	}
	a := &u.attach
	if a.results == nil {
		a.results = make(chan pickResult, 1)
	}
	chatID, results, notify := u.selected.ID, a.results, u.images.invalidate
	go func() {
		r := pickResult{chatID: chatID, temp: true}
		var buf bytes.Buffer
		if err := (&png.Encoder{CompressionLevel: png.BestSpeed}).Encode(&buf, img); err == nil {
			if path, err := writeTemp("Pasted image.png", buf.Bytes()); err == nil {
				r.files = []model.Attachment{{Path: path, Media: model.MediaImage}}
			}
		}
		results <- r
		if notify != nil {
			notify()
		}
	}()
	return true
}

// updatePaste attaches files and pictures pasted with Ctrl+V into the
// composer (or onto a photo in the send view), and files dropped on the
// window. Pasted text still goes to the composer.
func (u *UI) updatePaste(gtx C) {
	if u.host != nil {
	drain:
		for {
			select {
			case paths := <-u.host.drops:
				u.attachPaths(paths)
			default:
				break drain
			}
		}
	}
	filters := []event.Filter{key.Filter{Focus: &u.conv.composer, Name: "V", Required: key.ModShortcut}}
	if len(u.attach.files) > 0 {
		filters = append(filters, key.Filter{Focus: &u.attach.ed.canvas, Name: "V", Required: key.ModShortcut})
	}
	for {
		ev, ok := gtx.Event(filters...)
		if !ok {
			break
		}
		if ke, ok := ev.(key.Event); !ok || ke.State != key.Press {
			continue
		}
		if !u.pasteFiles() && gtx.Focused(&u.conv.composer) {
			gtx.Execute(clipboard.ReadCmd{Tag: &u.conv.composer})
		}
	}
}

// showFile shows file i in the send view, swapping the captions.
func (u *UI) showFile(i int) {
	a := &u.attach
	if i == a.cur || i < 0 || i >= len(a.files) {
		return
	}
	u.finishTyping()
	if f := a.current(); f != nil {
		f.caption = u.conv.composer.Text()
	}
	u.resetEditor()
	a.cur = i
	ed := &u.conv.composer
	ed.SetText(a.files[i].caption)
	ed.SetCaret(ed.Len(), ed.Len())
}

// removeFile takes file i out of the send view; the view closes with the
// last one.
func (u *UI) removeFile(i int) {
	a := &u.attach
	if i < 0 || i >= len(a.files) {
		return
	}
	if len(a.files) == 1 {
		u.closeSendView(false)
		return
	}
	u.finishTyping()
	if f := a.current(); f != nil {
		f.caption = u.conv.composer.Text()
	}
	removeTemps(a.files[i : i+1])
	a.files = append(a.files[:i:i], a.files[i+1:]...)
	if i == a.cur || a.cur >= len(a.files) {
		u.resetEditor()
	}
	if a.cur > i || a.cur >= len(a.files) {
		a.cur--
	}
	ed := &u.conv.composer
	ed.SetText(a.files[a.cur].caption)
	ed.SetCaret(ed.Len(), ed.Len())
}

// closeSendView closes the send view (sent or not) and gives the composer
// its text back.
func (u *UI) closeSendView(sent bool) {
	a := &u.attach
	if len(a.files) == 0 {
		return
	}
	u.finishTyping()
	if !sent {
		removeTemps(a.files)
	}
	a.ghost, a.ghostCur = a.files, a.cur
	a.files, a.cur = nil, 0
	a.ed.tool, a.ed.sel = toolNone, -1
	ed := &u.conv.composer
	ed.SetText(a.draft)
	ed.SetCaret(ed.Len(), ed.Len())
	a.draft = ""
	if u.picker.mode == pickMedia {
		u.closePicker()
	}
	u.requestFocus(ed)
}

// dropAttachments forgets the send view at once, as when another chat
// opens (stashDraft has taken a chat's files first); the composer's text
// is the caller's.
func (u *UI) dropAttachments() {
	a := &u.attach
	removeTemps(a.files)
	a.files, a.ghost, a.cur, a.draft = nil, nil, 0, ""
	a.anim.snap(false)
	u.resetEditor()
}

// removeTemps deletes the app's own copies among files.
func removeTemps(files []*attachFile) {
	for _, f := range files {
		if f.temp {
			_ = os.Remove(f.Path)
		}
	}
}

// sendAttachments sends the files of the send view, each with its own
// caption; the first one carries the reply. Two or more photos and videos
// go as an album.
func (u *UI) sendAttachments() {
	u.stopOutgoingTyping()
	a := &u.attach
	u.finishTyping()
	if f := a.current(); f != nil {
		f.caption = u.conv.composer.Text()
	}
	status := isStatusDestination(a.chatID)
	if !status {
		u.openAlbum(a.chatID, a.files)
	}
	for i, f := range a.files {
		if status {
			// A caption has no mentions or reply.
			u.queueSend(a.chatID, f, model.Draft{Text: trimSpace(f.caption)})
			continue
		}
		d := u.draftFrom(trimSpace(f.caption))
		if i == 0 {
			d.Reply = u.conv.reply
		}
		u.queueSend(a.chatID, f, d)
	}
	if !status {
		u.conv.reply, u.conv.mentions = nil, nil
	}
	u.closeSendView(true)
	u.flushOutbox()
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
	hide     bool // hide voter names
	// ends sets an end time: endDate (2006-01-02) and endTime (15:04),
	// local time.
	ends             bool
	endDate, endTime widget.Editor
	list             widget.List
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
	for _, ed := range []*widget.Editor{&pl.endDate, &pl.endTime} {
		ed.SingleLine, ed.Submit = true, true
	}
	for range 2 {
		pl.options = append(pl.options, &widget.Editor{SingleLine: true, Submit: true})
	}
	u.requestFocus(&pl.question)
}

// poll returns the poll to send, or false while it lacks a question
// or two options, or its end time isn't one after now.
func (pl *pollState) poll(now time.Time) (model.Poll, bool) {
	q := model.Poll{Question: trimSpace(pl.question.Text()), Multiple: pl.multiple, HideVoters: pl.hide}
	for _, ed := range pl.options {
		if o := trimSpace(ed.Text()); o != "" {
			q.Options = append(q.Options, o)
		}
	}
	ok := q.Question != "" && len(q.Options) >= 2
	if pl.ends {
		end, valid := pl.end(now)
		q.End, ok = end, ok && valid
	}
	return q, ok
}

// end reads the end time, and whether it is a time after now.
func (pl *pollState) end(now time.Time) (time.Time, bool) {
	t, err := time.ParseInLocation("2006-01-02 15:04",
		trimSpace(pl.endDate.Text())+" "+trimSpace(pl.endTime.Text()), now.Location())
	return t, err == nil && t.After(now)
}

// startEnd fills in the end time WhatsApp suggests: a day from now, at
// the next whole minute.
func (pl *pollState) startEnd(now time.Time) {
	t := now.Truncate(time.Minute).Add(time.Minute + 24*time.Hour)
	pl.endDate.SetText(t.Format("2006-01-02"))
	pl.endTime.SetText(t.Format("15:04"))
}

// pollPanel is the "Create poll" dialog: a question, options that grow as
// you fill them, whether voters may pick several, whether their names
// are hidden, and when voting ends.
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
	if u.btn("poll:hide").Clicked(gtx) {
		pl.hide = !pl.hide
	}
	now := u.now().Local()
	if u.btn("poll:ends").Clicked(gtx) {
		if pl.ends = !pl.ends; pl.ends {
			pl.startEnd(now)
		}
	}
	// Enter moves to the next field.
	fields := append([]*widget.Editor{&pl.question}, pl.options...)
	if pl.ends {
		fields = append(fields, &pl.endDate, &pl.endTime)
	}
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
	poll, ok := pl.poll(now)
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
	rows = append(rows,
		heading("Settings"),
		func(gtx C) D { return u.pollCheck(gtx, "poll:multi", "Allow multiple answers", pl.multiple) },
		func(gtx C) D { return u.pollCheck(gtx, "poll:hide", "Hide voter names", pl.hide) },
		func(gtx C) D { return u.pollCheck(gtx, "poll:ends", "Set end time", pl.ends) },
	)
	if pl.ends {
		_, valid := pl.end(now)
		rows = append(rows, func(gtx C) D {
			return layout.Inset{Left: 24, Right: 24, Bottom: 8}.Layout(gtx, func(gtx C) D {
				return layout.Flex{}.Layout(gtx,
					layout.Flexed(1, func(gtx C) D { return u.pollLine(gtx, &pl.endDate, "YYYY-MM-DD", !valid) }),
					layout.Rigid(layout.Spacer{Width: 16}.Layout),
					layout.Flexed(1, func(gtx C) D { return u.pollLine(gtx, &pl.endTime, "HH:MM", !valid) }),
				)
			})
		})
	}
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

// pollCheck is a row of the poll dialog's settings that a click turns on
// and off.
func (u *UI) pollCheck(gtx C, key, label string, on bool) D {
	p := u.pal
	cl := u.btn(key)
	return clickable(gtx, cl, func(gtx C) D {
		gtx.Constraints.Min.X = gtx.Constraints.Max.X
		return background(gtx, mix(p.Dialog, p.Hover, u.hover(gtx, cl)), 0, func(gtx C) D {
			return vcenter(gtx, gtx.Dp(56), func(gtx C) D {
				return layout.Inset{Left: 24, Right: 24}.Layout(gtx, func(gtx C) D {
					box, col := icCheckBoxEmpty, p.TextSecondary
					if on {
						box, col = icCheckBox, p.Green
					}
					return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
						layout.Flexed(1, u.label(15, label, p.Text, labelOpts{maxLines: 1}).Layout),
						layout.Rigid(iconW(box, 24, col)),
					)
				})
			})
		})
	})
}

// pollField is a line of text with an underline that turns green while
// focused.
func (u *UI) pollField(gtx C, ed *widget.Editor, hint string) D {
	return layout.Inset{Left: 24, Right: 24}.Layout(gtx, func(gtx C) D { return u.pollLine(gtx, ed, hint, false) })
}

// pollLine is pollField without its margins; bad underlines it in red
// while it isn't focused.
func (u *UI) pollLine(gtx C, ed *widget.Editor, hint string, bad bool) D {
	p := u.pal
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
	switch {
	case gtx.Focused(ed):
		line, col = gtx.Dp(2), p.Green
	case bad:
		line, col = gtx.Dp(2), p.Danger
	}
	fillRect(gtx, image.Rect(0, dims.Size.Y-line, dims.Size.X, dims.Size.Y), col)
	return dims
}
