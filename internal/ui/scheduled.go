package ui

import (
	"image"
	"image/color"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"gioui.org/font"
	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/widget"

	"github.com/chomosuke9/wazzapclients/internal/auto"
	"github.com/chomosuke9/wazzapclients/internal/command"
	"github.com/chomosuke9/wazzapclients/internal/model"
)

// Scheduled messages are WhatsApp's own (Backend.Schedule): its server
// shows them to the others at their time, with this app closed or not.
// On a phone you hold the send button; here a right click on it, holding
// it, or Shift+F10 on it opens the same menu (Schedule message, View
// once), and Ctrl+Shift+Enter opens Schedule message at once. The dialog
// offers a few times with a click and takes any other typed.
//
// A chat's scheduled messages show after its newest message, as bubbles
// of yours with the time they go at; clicking one offers to send it now,
// edit it or cancel it. Editing puts it back in the composer, under a bar
// saying so: sending it again reschedules it.
//
// While you're AFK (/afk), a bar above the chat list says so and ends it
// (u.auto, package auto).

// schedState is the scheduled messages' part of the UI.
type schedState struct {
	// ver counts the backend's ScheduledEvents: the rows cache depends
	// on it.
	ver int
	// editing is the scheduled message the composer holds (editScheduled),
	// in chat editingChat, or nil.
	editing     *model.Scheduled
	editingChat string
	// hold is a send button (the composer's or the send view's, btn)
	// being held down: since when, and whether that opened its menu (so
	// letting go doesn't send).
	hold struct {
		btn   *widget.Clickable
		since time.Time
		fired bool
	}
	// The Schedule message dialog: the date and time typed, View once.
	date, clock widget.Editor
	viewOnce    bool
	list        widget.List
}

// holdDelay is how long holding the send button takes to open its menu.
const holdDelay = 450 * time.Millisecond

// scheduledChip heads a chat's scheduled messages.
const scheduledChip = "Scheduled messages"

// appendScheduled adds chat c's scheduled messages to its rows, under a
// chip of their own.
func (u *UI) appendScheduled(rows []convRow, c *model.Chat) []convRow {
	list := u.backend.Scheduled(c.ID)
	if len(list) == 0 {
		return rows
	}
	rows = append(rows, convRow{kind: rowDate, date: scheduledChip})
	for i, s := range list {
		rows = append(rows, convRow{kind: rowScheduled, job: s, first: i == 0})
	}
	return rows
}

// scheduledText is what a scheduled message's bubble says.
func scheduledText(s *model.Scheduled) string {
	if t := s.Text(); t != "" || s.Media != model.MediaNone {
		return t
	}
	return "Scheduled on another device"
}

// scheduledFile is what a scheduled file's line in its bubble says: a
// document's name, or what it is.
func scheduledFile(s *model.Scheduled) string {
	if s.FileName != "" {
		return s.FileName
	}
	return mediaLabel(&model.Message{Media: s.Media})
}

// layoutScheduled draws a scheduled message: your bubble, with a clock
// and when it goes instead of the time and ticks. The first has a tail.
func (u *UI) layoutScheduled(gtx C, s *model.Scheduled, tail bool, maxW int) D {
	p := u.pal
	key := "sched:" + s.ID
	cl := u.btn(key)
	if cl.Clicked(gtx) {
		u.openScheduledMenu(s)
	}
	inset := gtx.Dp(9)
	cgtx := gtx
	cgtx.Constraints = layout.Constraints{Max: image.Pt(maxW-2*inset, gtx.Constraints.Max.Y)}
	col := p.TextOut
	if s.Text() == "" && s.Media == model.MediaNone {
		col = p.MetaOut
	}
	body := record(cgtx, func(gtx C) D {
		var kids []layout.FlexChild
		if s.Media != model.MediaNone {
			kids = append(kids, layout.Rigid(func(gtx C) D { return u.layoutScheduledFile(gtx, s) }))
		}
		if t := scheduledText(s); t != "" {
			if len(kids) > 0 {
				kids = append(kids, layout.Rigid(layout.Spacer{Height: 4}.Layout))
			}
			kids = append(kids, layout.Rigid(u.label(15, plainText(t), col, labelOpts{maxLines: 0}).Layout))
		}
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx, kids...)
	})
	meta := record(cgtx, func(gtx C) D {
		var kids []layout.FlexChild
		if s.Draft.ViewOnce {
			kids = append(kids,
				layout.Rigid(func(gtx C) D { return u.viewOnceMark(gtx, 15, p.MetaOut) }),
				layout.Rigid(layout.Spacer{Width: 6}.Layout))
		}
		kids = append(kids,
			layout.Rigid(iconW(icClock, 14, p.MetaOut)),
			layout.Rigid(layout.Spacer{Width: 4}.Layout),
			layout.Rigid(u.label(12, scheduledAt(s.At, u.now()), p.MetaOut, labelOpts{maxLines: 1}).Layout))
		return layout.Flex{Alignment: layout.Middle}.Layout(gtx, kids...)
	})
	w := max(body.size.X, meta.size.X) + 2*inset
	top, gap, bottom := gtx.Dp(6), gtx.Dp(3), gtx.Dp(6)
	h := top + body.size.Y + gap + meta.size.Y + bottom
	sz := image.Pt(w, h)
	defer op.Offset(image.Pt(gtx.Constraints.Max.X-w, 0)).Push(gtx.Ops).Pop()
	u.paintBubble(gtx, w, h, p.BubbleOut, true, tail)
	if hv := u.hover(gtx, cl); hv > 0 {
		fillRRect(gtx, image.Rectangle{Max: sz}, gtx.Dp(8), faded(p.Hover, hv))
	}
	body.at(gtx, inset, top)
	meta.at(gtx, w-inset-meta.size.X, top+body.size.Y+gap)
	clickable(gtx, cl, func(gtx C) D { return D{Size: sz} })
	if u.rightClick(gtx, key, sz) {
		u.openScheduledMenu(s)
	}
	return D{Size: image.Pt(gtx.Constraints.Max.X, h)}
}

// layoutScheduledFile is a scheduled file's line in its bubble: its
// thumbnail, or its kind's icon, and what it is.
func (u *UI) layoutScheduledFile(gtx C, s *model.Scheduled) D {
	p := u.pal
	return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
		layout.Rigid(func(gtx C) D {
			sz := gtx.Dp(44)
			r := image.Rectangle{Max: image.Pt(sz, sz)}
			m := &model.Message{ID: s.ID, ChatID: s.ChatID, FromMe: true, Media: s.Media, Thumb: s.Thumb}
			if s.Media == model.MediaImage || len(s.Thumb) > 0 {
				if img := u.messageImage(m, sz*2); img != nil && img.state == imgReady {
					defer clip.UniformRRect(r, gtx.Dp(6)).Push(gtx.Ops).Pop()
					paintCover(gtx, img.op, img.size, r)
					return D{Size: r.Max}
				}
			}
			fillRRect(gtx, r, gtx.Dp(6), mix(p.BubbleOut, p.Text, 0.08))
			if ic := mediaIcon(s.Media); ic != nil {
				return centerIn(gtx, sz, iconW(ic, 24, p.MetaOut))
			}
			return D{Size: image.Pt(sz, sz)}
		}),
		layout.Rigid(layout.Spacer{Width: 10}.Layout),
		layout.Flexed(1, u.label(14.5, scheduledFile(s), p.TextOut, labelOpts{maxLines: 1, weight: font.Medium}).Layout),
	)
}

// scheduledAt says when a scheduled message goes: "Today at 21:00".
func scheduledAt(at, now time.Time) string {
	s := command.Day(at, now) + " at " + at.In(now.Location()).Format("15:04")
	return strings.ToUpper(s[:1]) + s[1:]
}

func (u *UI) openScheduledMenu(s *model.Scheduled) {
	u.ctx = ctxMenu{kind: ctxScheduled, chatID: s.ChatID, job: s.ID, at: u.mouse}
}

// scheduledByID returns chat's scheduled message with id, or nil once it
// went or was cancelled.
func (u *UI) scheduledByID(chat, id string) *model.Scheduled {
	for _, s := range u.backend.Scheduled(chat) {
		if s.ID == id {
			return s
		}
	}
	return nil
}

// scheduledMenuItems are a scheduled message's actions. One scheduled on
// another device can only be cancelled (and copied, when its text is
// here).
func (u *UI) scheduledMenuItems(chat, id string) []menuItem {
	s := u.scheduledByID(chat, id)
	if s == nil {
		return nil // sent meanwhile
	}
	var items []menuItem
	if !s.Elsewhere && s.Media == model.MediaNone {
		items = append(items,
			menuItem{key: "sendnow", ic: icSend, label: "Send now", run: func() { u.sendScheduledNow(s) }},
			menuItem{key: "edit", ic: icEdit, label: "Edit", run: func() { u.editScheduled(s) }})
	}
	if t := s.Text(); t != "" {
		items = append(items, menuItem{key: "copy", ic: icCopy, label: "Copy", run: func() { u.copyText(stripIsolates(plainText(t))) }})
	}
	items = append(items, menuItem{key: "cancel", ic: icDelete, label: "Cancel", col: u.pal.Danger, run: func() {
		if u.backend.Unschedule(s.ChatID, s.ID) && u.sched.editing != nil && u.sched.editing.ID == s.ID {
			u.sched.editing = nil
		}
	}})
	return items
}

// sendScheduledNow takes a scheduled message back and sends it at once.
func (u *UI) sendScheduledNow(s *model.Scheduled) {
	if !u.backend.Unschedule(s.ChatID, s.ID) {
		return // its time came meanwhile
	}
	d := s.Draft
	d.Reply = u.scheduledReply(s)
	if m := u.backend.Send(s.ChatID, d); m != nil && u.selected != nil && u.selected.ID == s.ChatID {
		u.upsertMessage(m)
		u.scrollMessages(layout.Position{})
	}
}

// scheduledReply is the message a scheduled message answers, or nil.
func (u *UI) scheduledReply(s *model.Scheduled) *model.Message {
	r := s.Draft.Reply
	if r == nil {
		return nil
	}
	if ms := u.backend.MessagesFrom(s.ChatID, r.ID, 1); len(ms) > 0 && ms[0].ID == r.ID {
		return ms[0]
	}
	return nil
}

// editScheduled puts a scheduled message back in the composer, mentions
// and reply included, under a bar that says so. Sending it opens Schedule
// message at its time, and scheduling it again replaces it; until then
// it stays scheduled.
func (u *UI) editScheduled(s *model.Scheduled) {
	c := u.selected
	if c == nil || c.ID != s.ChatID {
		return
	}
	d := s.Draft
	text := s.Text()
	var refs []mentionRef
	if d.MentionAll {
		refs = append(refs, mentionRef{name: "all", jid: mentionAllID})
	}
	if d.MentionAdmins {
		refs = append(refs, mentionRef{name: "admin", jid: mentionAdminID})
	}
	members := u.mentionMembers(c)
	for _, id := range d.Mentions {
		for _, m := range members {
			if m.ID == id && strings.Contains(text, "@"+u.mentionName(m)) {
				refs = append(refs, mentionRef{name: u.mentionName(m), jid: id})
			}
		}
	}
	var customs []string
	if d.MentionChat != "" {
		customs = append(customs, d.MentionChat)
	}
	for _, name := range d.GroupNames {
		if name == "admin" {
			refs = append(refs, mentionRef{name: name, jid: mentionAdminID})
		} else {
			customs = append(customs, name)
		}
	}
	if len(customs) > 0 {
		// Whoever the text doesn't name is the first custom mention's: the
		// draft doesn't say whose they are, and all of them are notified.
		ref := mentionRef{name: customs[0], jid: mentionCustomID}
		for _, id := range d.Mentions {
			if !slices.ContainsFunc(refs, func(r mentionRef) bool { return r.jid == id }) {
				ref.people = append(ref.people, id)
			}
		}
		refs = append(refs, ref)
		for _, name := range customs[1:] {
			refs = append(refs, mentionRef{name: name, jid: mentionCustomID})
		}
	}
	u.cancelEdit()
	u.conv.reply = u.scheduledReply(s)
	ed := &u.conv.composer
	ed.SetText(text)
	ed.SetCaret(ed.Len(), ed.Len())
	u.conv.mentions = refs
	u.sched.editing, u.sched.editingChat = s, s.ChatID
	u.sched.viewOnce = d.ViewOnce
	u.requestFocus(ed)
}

// schedEditing reports whether the open chat's composer holds a
// scheduled message being edited.
func (u *UI) schedEditing() bool {
	e := u.sched.editing
	if e == nil || u.selected == nil || u.sched.editingChat != u.selected.ID {
		return false
	}
	if u.scheduledByID(e.ChatID, e.ID) == nil {
		u.sched.editing = nil // it went meanwhile: the composer sends now
		return false
	}
	return true
}

// layoutSchedEditBar is the bar over the composer while it holds a
// scheduled message being edited, with its time and a button that gives
// the edit up.
func (u *UI) layoutSchedEditBar(gtx C) D {
	if !u.schedEditing() {
		return D{}
	}
	p := u.pal
	if u.btn("schededit:close").Clicked(gtx) {
		u.sched.editing = nil
		setComposer(&u.conv.composer, "")
		u.conv.reply, u.conv.mentions = nil, nil
		return D{}
	}
	at := scheduledAt(u.sched.editing.At, u.now())
	return layout.Inset{Left: 8, Right: 8, Top: 8}.Layout(gtx, func(gtx C) D {
		return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
			layout.Rigid(layout.Spacer{Width: 8}.Layout),
			layout.Rigid(iconW(icScheduleSend, 22, p.Green)),
			layout.Rigid(layout.Spacer{Width: 12}.Layout),
			layout.Flexed(1, func(gtx C) D {
				return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
					layout.Rigid(u.label(14, "Editing a scheduled message", p.Green, labelOpts{weight: font.Medium, maxLines: 1}).Layout),
					layout.Rigid(u.label(13.5, "Sends "+strings.ToLower(at[:1])+at[1:]+" · Enter picks the time again", p.TextSecondary, labelOpts{maxLines: 1}).Layout),
				)
			}),
			layout.Rigid(layout.Spacer{Width: 8}.Layout),
			layout.Rigid(func(gtx C) D { return u.iconButton(gtx, u.btn("schededit:close"), icClose, 40, 24, p.Icon) }),
		)
	})
}

// The send button's menu.

// canSchedule reports whether the composer's text, or the send view's
// files, can be scheduled (and the text sent as view once): in a chat,
// not a channel, a status or an edit.
func (u *UI) canSchedule() bool {
	c := u.selected
	if c == nil || isChannelID(c.ID) || u.postingStatus() || u.conv.edit.msg != nil || u.slashQuery() != nil {
		return false
	}
	if u.schedFiles() {
		return u.attach.chatID == c.ID
	}
	return !u.conv.editorElsewhere && trimSpace(u.conv.composer.Text()) != ""
}

// schedFiles reports whether the send view's files are what gets
// scheduled, rather than the composer's text.
func (u *UI) schedFiles() bool {
	return len(u.attach.files) > 0
}

// openSendMenu opens the send button's menu above it.
func (u *UI) openSendMenu() {
	if !u.canSchedule() {
		return
	}
	u.ctx = ctxMenu{kind: ctxSend, chatID: u.selected.ID, at: u.mouse}
}

// sendMenuItems are the send button's menu, as on WhatsApp's phones.
func (u *UI) sendMenuItems() []menuItem {
	if !u.canSchedule() {
		return nil
	}
	items := []menuItem{{key: "schedule", ic: icScheduleSend, label: "Schedule message", run: u.openSchedule}}
	if !u.schedEditing() && !u.schedFiles() { // a file's view once is the send view's
		items = append(items, menuItem{key: "viewonce", label: "View once",
			glyph: func(gtx C, col color.NRGBA) D { return u.viewOnceMark(gtx, 24, col) },
			run:   u.sendViewOnce})
	}
	return items
}

// sendViewOnce sends the composer's text as a view once message.
func (u *UI) sendViewOnce() {
	d, _, ok := u.composerDraft()
	if !ok {
		return
	}
	d.ViewOnce = true
	d.Link, d.LinkThumb, d.LinkImage = nil, nil, model.LinkImage{} // as WhatsApp's: no preview
	u.stopOutgoingTyping()
	u.clearComposer()
	if m := u.backend.Send(u.selected.ID, d); m != nil {
		u.listSent(m)
		u.upsertMessage(m)
	}
	u.scrollMessages(layout.Position{})
}

// updateSendHold opens a send button's menu once cl is held down long
// enough, and reports whether that happened since it was pressed: then
// letting go doesn't send.
func (u *UI) updateSendHold(gtx C, cl *widget.Clickable) bool {
	h := &u.sched.hold
	if !cl.Pressed() {
		if h.btn != cl {
			return false
		}
		h.since = time.Time{}
		return h.fired
	}
	if h.btn != cl || h.since.IsZero() {
		h.btn, h.since, h.fired = cl, gtx.Now, false
	}
	if h.fired {
		return true
	}
	if gtx.Now.Sub(h.since) >= holdDelay {
		if u.canSchedule() {
			h.fired = true
			u.openSendMenu()
		}
		return h.fired
	}
	gtx.Execute(op.InvalidateCmd{At: h.since.Add(holdDelay)})
	return false
}

// sendButtonClicked reports whether the composer's send button was
// clicked to send: not let go of after holding it opened its menu.
func (u *UI) sendButtonClicked(gtx C) bool {
	return u.sendClicked(gtx, &u.conv.send)
}

// sendClicked reports whether send button cl was clicked to send.
func (u *UI) sendClicked(gtx C, cl *widget.Clickable) bool {
	held := u.updateSendHold(gtx, cl)
	if !cl.Clicked(gtx) {
		return false
	}
	if held {
		u.sched.hold.fired = false
		return false
	}
	return true
}

// scheduleKeys opens Schedule message on Ctrl+Shift+Enter in the composer.
func (u *UI) scheduleKeys(gtx C) {
	ed := &u.conv.composer
	if _, ok := keyPressed(gtx, key.Filter{Focus: ed, Name: key.NameReturn, Required: key.ModShortcut | key.ModShift},
		key.Filter{Focus: ed, Name: key.NameEnter, Required: key.ModShortcut | key.ModShift}); ok && u.canSchedule() {
		u.openSchedule()
	}
}

// The Schedule message dialog.

// openSchedule opens the Schedule message dialog for the composer's
// text, at the time of the scheduled message being edited or an hour
// from now.
func (u *UI) openSchedule() {
	if !u.canSchedule() {
		return
	}
	s := &u.sched
	u.dialog = dialogState{kind: dialogSchedule}
	s.list.Axis = layout.Vertical
	for _, ed := range []*widget.Editor{&s.date, &s.clock} {
		ed.SingleLine, ed.Submit = true, true
	}
	at := roundUp(u.now().Local().Add(time.Hour), 5*time.Minute)
	if u.schedEditing() && !u.schedFiles() {
		at = s.editing.At.Local()
	} else {
		s.viewOnce = false
	}
	u.setScheduleTime(at)
	u.requestFocus(&s.clock)
}

// roundUp rounds t up to a multiple of d (in its own time zone's minutes).
func roundUp(t time.Time, d time.Duration) time.Time {
	r := t.Truncate(d)
	if r.Before(t) {
		r = r.Add(d)
	}
	return r
}

func (u *UI) setScheduleTime(t time.Time) {
	s := &u.sched
	s.date.SetText(t.Format("2006-01-02"))
	s.clock.SetText(t.Format("15:04"))
	s.clock.SetCaret(s.clock.Len(), s.clock.Len())
}

// scheduleTime reads the time typed, and says what's wrong with it, if
// anything.
func (u *UI) scheduleTime() (time.Time, string) {
	now := u.now().Local()
	at, ok := parseLocal(&u.sched.date, &u.sched.clock, now.Location())
	if !ok {
		// The clock may be typed the way /schedule read it: "9pm", "21.30".
		at2, err := command.ParseWhen(trimSpace(u.sched.date.Text())+" "+trimSpace(u.sched.clock.Text()), now)
		if err != nil {
			return time.Time{}, "Type a date like " + now.Format("2006-01-02") + " and a time like 08:00."
		}
		at = at2
	}
	switch {
	case at.Sub(now.Truncate(time.Minute)) < model.ScheduleMin:
		return at, "Pick a time at least 10 minutes from now."
	case at.Sub(now) > model.ScheduleMax:
		return at, "Pick a time within the next 14 days."
	}
	return at, ""
}

// schedulePick is one of the times the dialog offers with a click.
type schedulePick struct {
	label string
	at    time.Time
}

// schedulePicks are the times offered: in an hour, tonight, tomorrow
// morning and the next Monday morning, those WhatsApp would take.
func schedulePicks(now time.Time) []schedulePick {
	day := func(d int, h int) time.Time {
		y, m, dd := now.Date()
		return time.Date(y, m, dd+d, h, 0, 0, 0, now.Location())
	}
	picks := []schedulePick{{"In 1 hour", roundUp(now.Add(time.Hour), 5*time.Minute)}}
	if t := day(0, 20); t.Sub(now) >= time.Hour {
		picks = append(picks, schedulePick{"Tonight, 20:00", t})
	}
	picks = append(picks, schedulePick{"Tomorrow, 08:00", day(1, 8)})
	toMonday := (int(time.Monday) - int(now.Weekday()) + 7) % 7
	if toMonday <= 1 {
		toMonday += 7
	}
	picks = append(picks, schedulePick{"Monday, 08:00", day(toMonday, 8)})
	return picks
}

// confirmSchedule schedules the composer's message at the time typed,
// replacing the one being edited.
func (u *UI) confirmSchedule() {
	at, problem := u.scheduleTime()
	if problem != "" || !u.dialog.isOpen() {
		return
	}
	if u.schedFiles() {
		u.scheduleAttachments(at)
		return
	}
	d, shown, ok := u.composerDraft()
	if !ok {
		u.closeDialog()
		return
	}
	d.ViewOnce = u.sched.viewOnce
	if d.ViewOnce {
		d.Link, d.LinkThumb, d.LinkImage = nil, nil, model.LinkImage{}
	}
	chat := u.selected.ID
	if err := u.backend.Schedule(chat, d, shown, at); err != nil {
		u.toast(sentence(err.Error()))
		return
	}
	if old := u.sched.editing; old != nil && u.schedEditing() {
		u.backend.Unschedule(old.ChatID, old.ID)
	}
	u.stopOutgoingTyping()
	u.clearComposer()
	u.closeDialog()
	u.scrollMessages(layout.Position{})
}

// scheduleAttachments schedules the send view's files at at, each with
// its own caption, the first answering the reply; the send view closes.
// They go one by one, not as an album: each is a message of its own.
func (u *UI) scheduleAttachments(at time.Time) {
	a := &u.attach
	u.stopOutgoingTyping()
	u.finishTyping()
	if f := a.current(); f != nil {
		f.caption = u.conv.composer.Text()
	}
	for i, f := range a.files {
		if whisperText(f.caption) {
			u.showFile(i)
			u.toast("Send /whisper from the caption it's typed in.")
			u.closeDialog()
			return
		}
	}
	for i, f := range a.files {
		caption := trimSpace(f.caption)
		d := u.draftFrom(caption)
		if i == 0 {
			d.Reply = u.conv.reply
		}
		it := u.outItem(a.chatID, f, d)
		it.at, it.shown = at, caption
		a.outbox = append(a.outbox, it)
	}
	u.conv.reply, u.conv.mentions = nil, nil
	u.closeDialog()
	u.closeSendView(true)
	u.flushOutbox()
	u.scrollMessages(layout.Position{})
}

// schedulePreview is what the Schedule message dialog shows it sends:
// the composer's text, or a line for each file.
func (u *UI) schedulePreview() string {
	if !u.schedFiles() {
		return trimSpace(u.conv.composer.Text())
	}
	a := &u.attach
	var lines []string
	for i, f := range a.files {
		caption := f.caption
		if i == a.cur {
			caption = u.conv.composer.Text()
		}
		l := mediaLabel(&model.Message{Media: f.Media})
		if f.Media == model.MediaDocument {
			l = filepath.Base(f.Path)
		}
		if c := trimSpace(caption); c != "" {
			l += " · " + c
		}
		lines = append(lines, l)
	}
	return strings.Join(lines, "\n")
}

// sentence makes an error a sentence: capitalized, with a full stop.
func sentence(s string) string {
	if s == "" {
		return s
	}
	s = strings.ToUpper(s[:1]) + s[1:]
	if !strings.HasSuffix(s, ".") {
		s += "."
	}
	return s
}

// schedulePanel is the Schedule message dialog.
func (u *UI) schedulePanel(gtx C) D {
	s := &u.sched
	p := u.pal
	if u.btn("sched:close").Clicked(gtx) {
		u.closeDialog()
	}
	if u.btn("sched:viewonce").Clicked(gtx) {
		s.viewOnce = !s.viewOnce
	}
	now := u.now().Local()
	picks := schedulePicks(now)
	for i, pk := range picks {
		if u.btn("sched:pick:" + itoa(i)).Clicked(gtx) {
			u.setScheduleTime(pk.at)
		}
	}
	// Enter moves from the date to the time, and schedules from there.
	for i, ed := range []*widget.Editor{&s.date, &s.clock} {
		for {
			e, ok := ed.Update(gtx)
			if !ok {
				break
			}
			if _, ok := e.(widget.SubmitEvent); ok {
				if i == 0 {
					u.requestFocus(&s.clock)
				} else {
					u.confirmSchedule()
				}
			}
		}
	}
	if u.btn("sched:send").Clicked(gtx) {
		u.confirmSchedule()
	}
	at, problem := u.scheduleTime()

	w := min(gtx.Dp(460), gtx.Constraints.Max.X-gtx.Dp(32))
	h := min(gtx.Dp(560), gtx.Constraints.Max.Y-gtx.Dp(48))
	gtx.Constraints = layout.Exact(image.Pt(w, h))
	heading := func(t string) layout.Widget {
		return func(gtx C) D {
			return layout.Inset{Left: 24, Right: 24, Top: 14, Bottom: 8}.Layout(gtx,
				u.label(14, t, p.TextSecondary, labelOpts{weight: font.Medium, maxLines: 1}).Layout)
		}
	}
	text := u.schedulePreview()
	rows := []layout.Widget{
		// The message, as a bubble of yours.
		func(gtx C) D {
			return layout.Inset{Left: 24, Right: 24, Top: 4, Bottom: 4}.Layout(gtx, func(gtx C) D {
				return layout.E.Layout(gtx, func(gtx C) D {
					gtx.Constraints.Max.X = gtx.Constraints.Max.X * 4 / 5
					return background(gtx, p.BubbleOut, 8, func(gtx C) D {
						return layout.Inset{Left: 10, Right: 10, Top: 7, Bottom: 7}.Layout(gtx, func(gtx C) D {
							return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
								layout.Rigid(func(gtx C) D {
									if !s.viewOnce {
										return D{}
									}
									return layout.Inset{Right: 8}.Layout(gtx, func(gtx C) D { return u.viewOnceMark(gtx, 18, p.MetaOut) })
								}),
								layout.Flexed(1, u.label(15, text, p.TextOut, labelOpts{maxLines: 4}).Layout),
							)
						})
					})
				})
			})
		},
		heading("When"),
		func(gtx C) D {
			return layout.Inset{Left: 24, Right: 24, Bottom: 6}.Layout(gtx, func(gtx C) D {
				chips := make([]layout.Widget, len(picks))
				for i, pk := range picks {
					on := float32(0)
					if pk.at.Equal(at) {
						on = 1
					}
					chips[i] = func(gtx C) D {
						return u.chip(gtx, u.btn("sched:pick:"+itoa(i)), on, func(gtx C) D {
							col := p.ChipText
							if on > 0 {
								col = p.ChipActiveText
							}
							return layout.Inset{Left: 12, Right: 12}.Layout(gtx, u.label(14, pk.label, col, labelOpts{maxLines: 1}).Layout)
						})
					}
				}
				return flow(gtx, gtx.Dp(8), chips)
			})
		},
		func(gtx C) D {
			return layout.Inset{Left: 24, Right: 24, Top: 4}.Layout(gtx, func(gtx C) D {
				return layout.Flex{}.Layout(gtx,
					layout.Flexed(1, func(gtx C) D { return u.pollLine(gtx, &s.date, "YYYY-MM-DD", problem != "") }),
					layout.Rigid(layout.Spacer{Width: 16}.Layout),
					layout.Flexed(1, func(gtx C) D { return u.pollLine(gtx, &s.clock, "HH:MM", problem != "") }),
				)
			})
		},
		func(gtx C) D {
			msg, col := "Sends "+command.DescribeWhen(at, now), p.TextSecondary
			if problem != "" {
				msg, col = problem, p.Danger
			}
			return layout.Inset{Left: 24, Right: 24, Top: 8, Bottom: 8}.Layout(gtx,
				u.label(13.5, msg, col, labelOpts{}).Layout)
		},
		func(gtx C) D {
			if u.schedFiles() {
				return D{} // a file's view once is the send view's
			}
			return u.pollCheck(gtx, "sched:viewonce", "View once", s.viewOnce)
		},
		func(gtx C) D {
			return layout.Inset{Left: 24, Right: 24, Top: 4, Bottom: 8}.Layout(gtx,
				u.label(13, "WhatsApp sends it then, whether this app is open or not. Until then only your devices see it.",
					p.TextSecondary, labelOpts{}).Layout)
		},
	}
	title := "Schedule message"
	if u.schedEditing() && !u.schedFiles() {
		title = "Reschedule message"
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx C) D { return u.formHeader(gtx, "sched:close", title) }),
		layout.Flexed(1, func(gtx C) D {
			return u.scrollList(gtx, &s.list, len(rows), func(gtx C, i int) D { return rows[i](gtx) })
		}),
		layout.Rigid(func(gtx C) D { return u.formSend(gtx, "sched:send", problem == "") }),
	)
}

// flow lays children out in rows, gap apart, starting a new row where the
// next one doesn't fit.
func flow(gtx C, gap int, children []layout.Widget) D {
	cgtx := gtx
	cgtx.Constraints.Min = image.Point{}
	x, y, rowH, w := 0, 0, 0, 0
	for _, c := range children {
		pt := record(cgtx, c)
		if x > 0 && x+pt.size.X > gtx.Constraints.Max.X {
			x, y, rowH = 0, y+rowH+gap, 0
		}
		pt.at(gtx, x, y)
		x += pt.size.X + gap
		rowH = max(rowH, pt.size.Y)
		w = max(w, x-gap)
	}
	return D{Size: image.Pt(max(w, gtx.Constraints.Min.X), y+rowH)}
}

// scheduleArg writes t as a date and time: "today 21:00", "tomorrow
// 08:00" or "25/12/2026 09:00".
func scheduleArg(t, now time.Time) string {
	t = t.In(now.Location())
	switch d := command.Day(t, now); d {
	case "today", "tomorrow":
		return d + " " + t.Format("15:04")
	}
	return t.Format("2/1/2006 15:04")
}

// layoutAwayBar shows, above the chat list, that you're AFK, with a
// button that ends it.
func (u *UI) layoutAwayBar(gtx C) D {
	if u.auto == nil || u.auto.Away() == nil {
		return D{}
	}
	w := u.auto.Away()
	p := u.pal
	cl := u.btn("away:back")
	if cl.Clicked(gtx) {
		u.toast(auto.BackText(u.auto.Back()))
		return D{}
	}
	text := "AFK since " + scheduleArg(w.Since, u.now())
	if strings.HasPrefix(text, "AFK since today ") {
		text = "AFK since " + strings.TrimPrefix(text, "AFK since today ")
	}
	if w.Reason != "" {
		text += " · " + w.Reason
	}
	return layout.Inset{Left: 21, Right: 21, Bottom: 10}.Layout(gtx, func(gtx C) D {
		gtx.Constraints.Min.X = gtx.Constraints.Max.X
		return background(gtx, p.Banner, 12, func(gtx C) D {
			return layout.Inset{Left: 14, Right: 8, Top: 6, Bottom: 6}.Layout(gtx, func(gtx C) D {
				return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(iconW(icBedtime, 18, p.Green)),
					layout.Rigid(layout.Spacer{Width: 10}.Layout),
					layout.Flexed(1, u.label(14, text, p.BannerText, labelOpts{maxLines: 1}).Layout),
					layout.Rigid(func(gtx C) D {
						return clickable(gtx, cl, func(gtx C) D {
							return background(gtx, faded(p.Hover, u.hover(gtx, cl)), 16, func(gtx C) D {
								return layout.UniformInset(8).Layout(gtx,
									u.label(14, "I'm back", p.Green, labelOpts{weight: font.Medium, maxLines: 1}).Layout)
							})
						})
					}),
				)
			})
		})
	})
}
