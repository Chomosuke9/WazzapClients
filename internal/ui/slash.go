package ui

import (
	"image"
	"slices"
	"strings"
	"time"
	"unicode"

	"gioui.org/font"
	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/widget"

	"github.com/chomosuke9/wazzapclients/internal/command"
	"github.com/chomosuke9/wazzapclients/internal/filepick"
	"github.com/chomosuke9/wazzapclients/internal/model"
	"github.com/chomosuke9/wazzapclients/internal/ui/icon"
)

// Slash commands, like Discord's: typing "/" at the start of a message
// opens a picker of commands over the composer. Once one is picked it
// shows the command with its options, and offers values for the option
// being typed (members, contacts, choices). Enter runs it; what it did
// shows as notes only you see, at the end of the chat. The commands
// themselves are in internal/command.

// slashState is the slash command picker and the commands' notes.
type slashState struct {
	on bool // prefSlash

	// The picker for the composer's text, cached for one text, caret and
	// set of members (see slashQuery).
	cache    *slashPick
	cacheKey slashKey
	cacheOK  bool
	contacts []*model.Contact // for Contact options, read when first needed
	// snippets are the saved snippets /snippet offers, loaded when first
	// needed (snippetsOK) and dropped when they change (snippetsChanged).
	snippets     []model.Snippet
	snippetsOK   bool
	snippetsLoad *bool // the load on its way, if any
	snippetsVer  int

	sel       int    // the highlighted row, or -1
	selFor    string // the query sel belongs to; a new one resets it
	navigated bool   // sel was moved with the arrow keys
	dismissed string // composer text the picker was closed for (Esc)
	problem   string // why Enter didn't run the command
	problemAt string // the composer text it's about
	list      widget.List
	anim      tween
	ghost     *slashPick // what the picker showed, while it fades out
	calcMore  bool       // /calc's calculator shows its functions (⋯)
	// replacing is the scheduled message being edited: the /schedule in
	// the composer replaces it once it runs (see editScheduled).
	replacing string

	notes   map[string][]*localNote           // per chat, oldest first
	leaving map[string][]*localNote           // dismissed notes shrinking away, per chat
	waiting map[string]func(model.GroupEvent) // group requests, by Ref
	seq     int
	done    chan func() // what Host.Do finished, to run on this goroutine
}

// localNote is a command's note in a chat.
type localNote struct {
	id   string
	at   time.Time
	note *command.Note
	// A dismissed note shrinks away (leave), while it's drawn (drawn).
	gone  bool
	leave tween
	drawn bool
}

// maxNotes is how many notes a chat keeps; older ones go.
const maxNotes = 20

// slashKey is what the picker depends on.
type slashKey struct {
	text     string
	caret    int
	mentions int
	chat     string
	info     *model.ChatInfo
	contacts int
	snippets int
}

// slashPick is what the picker shows for the composer's text.
type slashPick struct {
	in   command.Input
	cmds []*command.Command // the commands matching the name being typed
	opt  *command.Option    // the option being typed, or nil
	vals []slashValue       // values to pick for it
	// preview is what the command would give (Command.Preview), and
	// previewOK false when it says why it can't.
	preview   string
	previewOK bool
}

// slashValue is a value the picker offers for an option: a member or
// contact (id is set) or a choice. Picking it types name; its row shows
// title (or name) over sub, after the person's picture or ic.
type slashValue struct {
	id, name, title, sub string
	ic                   *icon.Icon
}

// rows is how many rows the picker offers to pick from.
func (sp *slashPick) rows() int {
	if sp.in.Naming {
		return len(sp.cmds)
	}
	return len(sp.vals)
}

// slashQuery reads the composer's text as a command, or returns nil when
// it isn't one.
func (u *UI) slashQuery() *slashPick {
	s := &u.slash
	c := u.selected
	if !s.on || c == nil || u.postingStatus() || u.conv.editorElsewhere || u.conv.edit.msg != nil || isChannelID(c.ID) {
		return nil
	}
	ed := &u.conv.composer
	txt := ed.Text()
	if !strings.HasPrefix(txt, "/") {
		return nil
	}
	caret, _ := ed.Selection()
	var info *model.ChatInfo
	if c.IsGroup {
		info = u.chatMembers(c.ID)
	}
	k := slashKey{txt, caret, len(u.conv.mentions), c.ID, info, len(s.contacts), s.snippetsVer}
	if s.cacheOK && s.cacheKey == k {
		return s.cache
	}
	s.cacheKey, s.cacheOK = k, true
	s.cache = u.readSlash(txt, caret, c, info)
	return s.cache
}

func (u *UI) readSlash(txt string, caret int, c *model.Chat, info *model.ChatInfo) *slashPick {
	var members []model.Member
	if info != nil {
		members = info.Members
	}
	mentions := make([]command.Mention, len(u.conv.mentions))
	for i, m := range u.conv.mentions {
		mentions[i] = command.Mention{Name: m.name, ID: m.jid}
	}
	in, ok := command.Parse(txt, caret, mentions, members)
	if !ok {
		return nil
	}
	in.Now = u.now()
	sp := &slashPick{in: in}
	if in.Naming {
		for _, cmd := range command.Matching(in.Name, c.IsGroup) {
			if u.commandOn(cmd) {
				sp.cmds = append(sp.cmds, cmd)
			}
		}
		if len(sp.cmds) == 0 {
			return nil // nothing to run here: it's just text
		}
		return sp
	}
	if in.Cmd.Group && !c.IsGroup || !u.commandOn(in.Cmd) {
		return nil
	}
	if in.Cmd.Preview != nil {
		sp.preview, sp.previewOK = in.Cmd.Preview(&in)
	}
	if in.Current < 0 {
		return sp
	}
	o := &in.Cmd.Options[in.Current]
	sp.opt = o
	// Values already given, except the one being typed.
	taken := map[string]bool{}
	for _, v := range in.Values[in.Current] {
		if v.ID != "" && v.Start != in.WordStart {
			taken[v.ID] = true
		}
	}
	q := strings.TrimPrefix(in.Word, "@")
	// Members and contacts the word finds (see fuzzy.go), best first.
	type hit struct {
		v     slashValue
		score int
	}
	var hits []hit
	add := func(v slashValue, score int) {
		if q == "" {
			score = matchExact // everyone, for a bare "@"
		}
		if score > 0 {
			hits = append(hits, hit{v, score})
		}
	}
	switch o.Kind {
	case command.Member:
		for _, m := range members {
			if m.ID == "" || !o.Offers(m) || taken[m.ID] {
				continue
			}
			score := memberScore(m, q)
			if m.Me {
				score = personScore(q, "", m.Name, u.meName())
			}
			add(slashValue{id: m.ID, name: u.mentionName(m), title: memberTitle(m), sub: memberSub(m)}, score)
		}
	case command.Contact:
		inGroup := map[string]bool{}
		for _, m := range members {
			inGroup[m.ID] = true
		}
		if u.slash.contacts == nil {
			u.slash.contacts = u.backend.Contacts()
			if u.slash.contacts == nil {
				u.slash.contacts = []*model.Contact{}
			}
		}
		for _, ct := range u.slash.contacts {
			if !inGroup[ct.ID] && !taken[ct.ID] {
				add(slashValue{id: ct.ID, name: ct.Name, sub: ct.Phone}, personScore(q, ct.Phone, ct.Name))
			}
		}
	case command.Snippet:
		if in.Text("action") == "send" {
			sp.vals = u.snippetValues(in.Word)
		}
	case command.Choice, command.When:
		for _, ch := range o.Choices {
			if strings.HasPrefix(ch, strings.ToLower(q)) {
				sp.vals = append(sp.vals, slashValue{name: ch})
			}
		}
	}
	slices.SortStableFunc(hits, func(a, b hit) int { return b.score - a.score })
	if o.Kind == command.Contact {
		hits = hits[:min(len(hits), 50)]
	}
	for _, h := range hits {
		sp.vals = append(sp.vals, h.v)
	}
	return sp
}

// slashShown reports whether the picker shows for sp.
func (u *UI) slashShown(sp *slashPick) bool {
	return sp != nil && u.conv.composer.Text() != u.slash.dismissed
}

// slashEnterPicks reports whether Enter picks the highlighted row rather
// than running the command: while naming it, typing a value, or before a
// required option has one, or after choosing a row with the arrows.
func (u *UI) slashEnterPicks(sp *slashPick) bool {
	if !u.slashShown(sp) || sp.rows() == 0 {
		return false
	}
	in := &sp.in
	if in.Naming || u.slash.navigated {
		return true
	}
	if sp.opt != nil && sp.opt.Kind == command.Snippet {
		// Unless a snippet's whole name is typed.
		return !u.snippetTyped(in.Text(sp.opt.Name))
	}
	if in.Word != "" {
		// Unless the value is complete already.
		for _, v := range in.Values[in.Current] {
			if v.Start == in.WordStart {
				return !v.Done
			}
		}
		return true
	}
	return sp.opt != nil && sp.opt.Required && len(in.Values[in.Current]) == 0
}

// updateSlashSel resets the highlighted row when the query changes.
func (u *UI) updateSlashSel(sp *slashPick) {
	s := &u.slash
	q := ""
	if sp != nil {
		q = sp.in.Name + "\x00" + sp.in.Word + "\x00" + itoa(sp.in.Current) + "\x00" + itoa(sp.rows())
	}
	if q == s.selFor {
		return
	}
	s.selFor, s.navigated = q, false
	s.list.Position = layout.Position{}
	s.sel = -1
	if u.slashEnterPicks(sp) {
		s.sel = 0
	}
}

// slashRows is how many rows the slash picker's list shows at once.
const slashRows = 5

// slashKeys moves through the picker with the arrow keys, and picks with
// Tab (or Enter, see slashEnterPicks). It runs before the composer reads
// its keys.
func (u *UI) slashKeys(gtx C) {
	u.drainSlashDone()
	if u.slash.replacing != "" && !strings.HasPrefix(u.conv.composer.Text(), "/schedule ") {
		u.slash.replacing = "" // the edit was given up
	}
	sp := u.slashQuery()
	u.updateSlashSel(sp)
	if !u.slashShown(sp) || sp.rows() == 0 {
		return
	}
	s := &u.slash
	ed := &u.conv.composer
	filters := []event.Filter{
		key.Filter{Focus: ed, Name: key.NameUpArrow},
		key.Filter{Focus: ed, Name: key.NameDownArrow},
		key.Filter{Focus: ed, Name: key.NameTab},
	}
	if u.slashEnterPicks(sp) {
		filters = append(filters, key.Filter{Focus: ed, Name: key.NameReturn}, key.Filter{Focus: ed, Name: key.NameEnter})
	}
	n := sp.rows()
	for {
		ev, ok := gtx.Event(filters...)
		if !ok {
			break
		}
		e, ok := ev.(key.Event)
		if !ok || e.State != key.Press {
			continue
		}
		switch e.Name {
		case key.NameUpArrow:
			s.sel = (max(s.sel, 0) - 1 + n) % n
			s.navigated = true
			keepVisible(&s.list.List, s.sel, min(n, slashRows))
		case key.NameDownArrow:
			s.sel = (s.sel + 1) % n
			s.navigated = true
			keepVisible(&s.list.List, s.sel, min(n, slashRows))
		default:
			u.pickSlash(sp, max(s.sel, 0))
			return
		}
	}
}

// pickSlash puts picker row i in the composer: a command's name, or a
// value for the option being typed.
func (u *UI) pickSlash(sp *slashPick, i int) {
	ed := &u.conv.composer
	in := &sp.in
	rs := []rune(ed.Text())
	switch {
	case in.Naming:
		if i >= len(sp.cmds) {
			return
		}
		ins := "/" + sp.cmds[i].Name
		if in.WordEnd >= len(rs) || !unicode.IsSpace(rs[in.WordEnd]) {
			ins += " "
		}
		ed.SetCaret(0, in.WordEnd)
		ed.Insert(ins)
	case i < len(sp.vals):
		v := sp.vals[i]
		ed.SetCaret(in.WordStart, in.WordEnd)
		if v.id != "" {
			ed.Insert("@" + v.name + " ")
			u.conv.mentions = append(u.conv.mentions, mentionRef{name: v.name, jid: v.id})
		} else {
			ed.Insert(v.name + " ")
		}
	}
	u.requestFocus(ed)
}

// submitSlash is Enter on a command: it picks the highlighted row, or runs
// the command, or says what's missing.
func (u *UI) submitSlash(sp *slashPick) {
	s := &u.slash
	if u.slashEnterPicks(sp) {
		u.pickSlash(sp, max(s.sel, 0))
		return
	}
	in := sp.in
	txt := u.conv.composer.Text()
	if p := in.Problem(); p != "" {
		s.problem, s.problemAt = p, txt
		s.dismissed = "" // the picker shows it
		return
	}
	c := u.selected
	host := slashHost{u: u, chat: c.ID, mentions: u.conv.mentions}
	ctx := &command.Context{Cmd: in.Cmd, Input: trimSpace(txt), Values: in.Values, Chat: c,
		Reply: u.conv.reply, Backend: u.backend, Now: u.now(), Auto: u.auto, Host: host}
	if c.IsGroup {
		ctx.Info = u.chatMembers(c.ID)
	}
	u.conv.composer.SetText("")
	u.conv.reply = nil
	u.conv.mentions = nil
	jobs := u.jobsVersion()
	command.Execute(ctx)
	if old := s.replacing; old != "" && in.Cmd.Name == "schedule" && u.jobsVersion() != jobs {
		u.auto.Cancel(old) // rescheduled: the edit is done
	}
	s.replacing = ""
	u.scrollMessages(layout.Position{})
}

// slashHost is what commands run in a chat get from the UI.
type slashHost struct {
	u    *UI
	chat string
	// mentions are those picked in the command's text.
	mentions []mentionRef
}

func (h slashHost) Note(n *command.Note) {
	u := h.u
	s := &u.slash
	if s.notes == nil {
		s.notes = map[string][]*localNote{}
	}
	s.seq++
	ln := &localNote{id: "note-" + itoa(s.seq), at: u.now(), note: n}
	ns := append(s.notes[h.chat], ln)
	if len(ns) > maxNotes {
		ns = ns[len(ns)-maxNotes:]
	}
	s.notes[h.chat] = ns
	u.msgsVer++
	u.anims.start(animKey{id: ln.id, tag: tagAppear})
}

func (h slashHost) Dismiss(n *command.Note) { h.u.dismissNote(h.chat, n) }

func (u *UI) dismissNote(chat string, n *command.Note) {
	s := &u.slash
	ns := s.notes[chat]
	for i, ln := range ns {
		if ln.note == n {
			s.notes[chat] = append(ns[:i:i], ns[i+1:]...)
			u.msgsVer++
			u.noteLeaves(chat, ln)
			return
		}
	}
}

// noteLeaves lets a dismissed note in the open chat shrink away, the way
// it grew in, from as far as it had grown, instead of vanishing. A busy
// note a command drops at once (/whisper's, when its message stands in)
// would otherwise blink.
func (u *UI) noteLeaves(chat string, ln *localNote) {
	if u.selected == nil || u.selected.ID != chat {
		return
	}
	k := animKey{id: ln.id, tag: tagAppear}
	ln.leave.snap(true)
	if e := u.anims.m[k]; e != nil {
		ln.leave.v = e.v
	}
	u.anims.stop(k)
	if ln.leave.v == 0 {
		return // never shown
	}
	ln.gone, ln.drawn = true, true
	s := &u.slash
	if s.leaving == nil {
		s.leaving = map[string][]*localNote{}
	}
	s.leaving[chat] = append(s.leaving[chat], ln)
}

// chatNotes are a chat's notes, with those still shrinking away, by time.
func (s *slashState) chatNotes(chat string) []*localNote {
	ns, gone := s.notes[chat], s.leaving[chat]
	if len(gone) == 0 {
		return ns
	}
	all := append(slices.Clone(ns), gone...)
	slices.SortStableFunc(all, func(a, b *localNote) int { return a.at.Compare(b.at) })
	return all
}

// leavingNote draws a dismissed note shrinking away, and drops it once
// it's gone.
func (u *UI) leavingNote(gtx C, ln *localNote, row layout.Widget) D {
	ln.drawn = true
	v := ln.leave.step(gtx, false, durAppear)
	if v == 0 {
		u.dropLeaving(ln)
		gtx.Execute(op.InvalidateCmd{}) // for the rows without it
	}
	fgtx, pop := fadeOut(gtx)
	defer pop()
	return growRow(fgtx, 0, easeOut(v), row)
}

func (u *UI) dropLeaving(ln *localNote) {
	s := &u.slash
	for chat, ns := range s.leaving {
		if i := slices.Index(ns, ln); i >= 0 {
			s.leaving[chat] = slices.Delete(ns, i, i+1)
			u.msgsVer++
		}
	}
}

// pruneLeaving drops the leaving notes that weren't drawn since the last
// call (scrolled off, or another chat's), which would never finish.
func (u *UI) pruneLeaving(chat string) {
	s := &u.slash
	for id, ns := range s.leaving {
		keep := ns[:0]
		for _, ln := range ns {
			if id == chat && ln.drawn {
				ln.drawn = false
				keep = append(keep, ln)
			}
		}
		if len(keep) != len(ns) {
			clear(ns[len(keep):])
			u.msgsVer++
		}
		if len(keep) == 0 {
			delete(s.leaving, id)
		} else {
			s.leaving[id] = keep
		}
	}
}

func (h slashHost) Group(r model.GroupRequest, done func(model.GroupEvent)) {
	s := &h.u.slash
	s.seq++
	r.Ref = "cmd-" + itoa(s.seq)
	if s.waiting == nil {
		s.waiting = map[string]func(model.GroupEvent){}
	}
	s.waiting[r.Ref] = done
	h.u.backend.ManageGroup(r)
}

func (h slashHost) Do(work func() func()) { h.u.slashDo(work) }

// slashDo runs work on another goroutine and what it returns on the UI's.
func (u *UI) slashDo(work func() func()) {
	s := &u.slash
	if s.done == nil {
		s.done = make(chan func(), 8)
	}
	done, notify := s.done, u.images.invalidate
	go func() {
		done <- work()
		if notify != nil {
			notify()
		}
	}()
}

// drainSlashDone runs what slashDo's work returned.
func (u *UI) drainSlashDone() {
	for {
		select {
		case f := <-u.slash.done:
			f()
		default:
			return
		}
	}
}

func (h slashHost) Copy(text string) {
	h.u.pendingCopy = text
	h.u.toast("Copied")
}

func (h slashHost) Confirm(title, body, action string, danger bool, run func()) {
	h.u.confirm(title, body, dialogButton{label: action, danger: danger, primary: !danger, run: run})
}

func (h slashHost) PickImage(done func(path string)) {
	h.u.slashDo(func() func() {
		paths, err := filepick.Open("Choose a picture for the sticker", false,
			filepick.Filter{Name: "Pictures", Exts: []string{"jpg", "jpeg", "png", "webp", "gif"}})
		path := ""
		if err == nil && len(paths) > 0 {
			path = paths[0]
		}
		return func() { done(path) }
	})
}

func (h slashHost) Sent(m *model.Message) {
	u := h.u
	if u.chatByID(m.ChatID) == nil && u.selected != nil && u.selected.ID == m.ChatID {
		u.chats = append(u.chats, u.selected)
	}
	u.upsertMessage(m)
}

func (h slashHost) Draft(text string) model.Draft { return h.u.draftWith(text, h.mentions, h.chat) }

func (h slashHost) SetGhost(on bool) { h.u.setGhost(on) }

func (h slashHost) SnippetVars(chat string, reply *model.Message) map[string]string {
	return h.u.snippetVars(chat, reply)
}
func (h slashHost) SnippetsChanged()            { h.u.snippetsChanged() }
func (h slashHost) EditSnippet(id int64)        { h.u.openSnippetSettings(id) }
func (h slashHost) ShowPayload(chat, id string) { h.u.openPayload(chat, id) }

// slashTakesMentions reports whether the composer may offer @mentions:
// unless its text is a command, or while the caret is in a command's
// text that takes them (Option.Mentions).
func (u *UI) slashTakesMentions() bool {
	sp := u.slashQuery()
	return sp == nil || !sp.in.Naming && sp.opt != nil && sp.opt.Kind == command.Text && sp.opt.Mentions
}

// groupAnswered hands a GroupEvent to the command waiting for it.
func (u *UI) groupAnswered(e model.GroupEvent) {
	if f := u.slash.waiting[e.Ref]; f != nil {
		delete(u.slash.waiting, e.Ref)
		f(e)
	}
}

// slashHint is what the composer shows, faded, after the text of a
// command: the options still to type.
func (u *UI) slashHint(sp *slashPick) string {
	in := &sp.in
	if in.Cmd == nil {
		return ""
	}
	if sp.previewOK && len(in.Missing()) == 0 {
		return " " + sp.preview // apart from the text it works out
	}
	var parts []string
	for i, o := range in.Cmd.Options {
		if len(in.Values) > i && len(in.Values[i]) > 0 {
			continue
		}
		p := o.Name
		if !o.Required {
			p = "[" + p + "]"
		}
		if prev := in.Cmd.Options[max(i-1, 0)]; i > 0 && prev.Until != "" && prev.Kind == command.Text {
			// What separates it from the text before (a Member option's
			// values end without one, at the first word that isn't an
			// @mention): "(new line) [bottom]".
			p = untilHint(prev.Until) + p
		}
		parts = append(parts, p)
	}
	return strings.Join(parts, " ")
}

// untilHint is how slashHint shows an option's separator.
func untilHint(sep string) string {
	if sep == "\n" {
		return "(new line) "
	}
	return sep
}

// paintSlashHint draws slashHint after the composer's text, when the
// caret is at its end. sz is the editor's size.
func (u *UI) paintSlashHint(gtx C, sz image.Point) {
	sp := u.slashQuery()
	if sp == nil {
		return
	}
	ed := &u.conv.composer
	if caret, _ := ed.Selection(); caret != ed.Len() {
		return
	}
	hint := u.slashHint(sp)
	if hint == "" {
		return
	}
	if !strings.HasSuffix(ed.Text(), " ") {
		hint = " " + hint
	}
	at := ed.CaretCoords().Round()
	l := record(gtx, u.label(16, hint, u.pal.ComposerHint, labelOpts{maxLines: 1}).Layout)
	defer clip.Rect{Max: image.Pt(gtx.Constraints.Max.X, sz.Y)}.Push(gtx.Ops).Pop()
	l.at(gtx, at.X+gtx.Dp(2), at.Y-l.size.Y+gtx.Dp(4))
}

// layoutSlashPicker draws the picker above the composer: the commands
// matching the name being typed, or the command with its options and the
// values to pick for the one being typed.
func (u *UI) layoutSlashPicker(gtx C, sp *slashPick) D {
	if isCalcPick(sp) {
		return u.layoutCalcPad(gtx, sp)
	}
	p := u.pal
	s := &u.slash
	in := &sp.in
	n := sp.rows()
	for i := range n {
		if u.btn("slash:" + itoa(i)).Clicked(gtx) {
			u.pickSlash(sp, i) // it keeps drawing while it changes
		}
	}
	gtx.Constraints.Min.X = gtx.Constraints.Max.X
	w := gtx.Constraints.Max.X
	pad := gtx.Dp(8)
	cmdH, rowH := gtx.Dp(60), gtx.Dp(52)
	if in.Naming {
		rowH = cmdH
	}

	// The header: what the rows are, or the option being typed, or why
	// the command didn't run.
	txt := u.conv.composer.Text()
	header := record(gtx, func(gtx C) D {
		return layout.Inset{Left: 12, Right: 12, Top: 4, Bottom: 6}.Layout(gtx, func(gtx C) D {
			switch {
			case s.problem != "" && s.problemAt == txt:
				return u.label(14, s.problem, p.Danger, labelOpts{weight: font.Medium, maxLines: 2}).Layout(gtx)
			case sp.preview != "" && sp.previewOK:
				return u.label(22, sp.preview, p.Green, labelOpts{weight: font.SemiBold, maxLines: 1}).Layout(gtx)
			case sp.preview != "":
				return u.label(14, sp.preview, p.Danger, labelOpts{weight: font.Medium, maxLines: 2}).Layout(gtx)
			case in.Naming:
				t := "COMMANDS"
				if in.Name != "" {
					t = "COMMANDS MATCHING /" + strings.ToUpper(in.Name)
				}
				return u.label(12.5, t, p.PopupSub, labelOpts{weight: font.SemiBold, maxLines: 1}).Layout(gtx)
			case sp.opt != nil:
				return layout.Flex{Alignment: layout.Baseline}.Layout(gtx,
					layout.Rigid(u.label(14, sp.opt.Name, p.Text, labelOpts{weight: font.SemiBold, maxLines: 1}).Layout),
					layout.Rigid(layout.Spacer{Width: 8}.Layout),
					layout.Flexed(1, u.label(13.5, sp.opt.Description, p.PopupSub, labelOpts{maxLines: 1}).Layout),
				)
			}
			return u.label(12.5, "PRESS ENTER TO RUN", p.PopupSub, labelOpts{weight: font.SemiBold, maxLines: 1}).Layout(gtx)
		})
	})
	// While typing options, the command shows under the header.
	var cmdRow part
	if !in.Naming {
		cmdRow = record(gtx, func(gtx C) D {
			gtx.Constraints.Max.X = w - 2*pad
			return u.layoutCommandRow(gtx, in.Cmd, in.Current, in.Values, cmdH)
		})
	}
	listN := min(n, slashRows)
	h := 2*pad + header.size.Y + cmdRow.size.Y + listN*rowH
	r := gtx.Dp(16)
	rect := image.Rect(0, 0, w, h)
	fillRRect(gtx, rect.Add(image.Pt(0, gtx.Dp(2))).Inset(-gtx.Dp(1)), r, p.Shadow)
	borderRRect(gtx, rect, r, p.Popup, p.PopupBorder)
	y := pad
	header.at(gtx, pad, y)
	y += header.size.Y
	if cmdRow.size.Y > 0 {
		cmdRow.at(gtx, pad, y)
		y += cmdRow.size.Y
	}
	if n == 0 {
		return D{Size: image.Pt(w, h)}
	}
	defer clip.UniformRRect(rect, r).Push(gtx.Ops).Pop()
	t := op.Offset(image.Pt(pad, y)).Push(gtx.Ops)
	lgtx := gtx
	lgtx.Constraints = layout.Exact(image.Pt(w-2*pad, listN*rowH))
	s.list.Axis = layout.Vertical
	u.scrollList(lgtx, &s.list, n, func(gtx C, i int) D {
		cl := u.btn("slash:" + itoa(i))
		return clickable(gtx, cl, func(gtx C) D {
			sz := image.Pt(gtx.Constraints.Max.X, rowH)
			hv := u.hover(gtx, cl)
			if i == s.sel {
				hv = 1
			}
			if hv > 0 {
				fillRRect(gtx, image.Rectangle{Max: sz}, gtx.Dp(10), faded(p.PopupHover, hv))
			}
			gtx.Constraints = layout.Constraints{Max: sz}
			if in.Naming {
				u.layoutCommandRow(gtx, sp.cmds[i], -1, nil, rowH)
			} else {
				u.layoutSlashValue(gtx, sp.vals[i], rowH)
			}
			return D{Size: sz}
		})
	})
	t.Pop()
	return D{Size: image.Pt(w, h)}
}

// layoutSlashValue draws a value the picker offers: a member or contact
// with their picture, or a choice.
func (u *UI) layoutSlashValue(gtx C, v slashValue, h int) D {
	p := u.pal
	return vcenter(gtx, h, func(gtx C) D {
		gtx.Constraints.Min.X = gtx.Constraints.Max.X
		return layout.Inset{Left: 12, Right: 12}.Layout(gtx, func(gtx C) D {
			if v.id == "" && v.ic == nil {
				return u.label(16, v.name, p.Text, labelOpts{maxLines: 1}).Layout(gtx)
			}
			title := v.title
			if title == "" {
				title = v.name
			}
			return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(func(gtx C) D {
					if v.ic != nil {
						gtx.Constraints.Min = image.Pt(gtx.Dp(36), gtx.Dp(36))
						return layout.Center.Layout(gtx, iconW(v.ic, 24, p.Icon))
					}
					return u.avatar(gtx, v.id, v.name, false, 36)
				}),
				layout.Rigid(layout.Spacer{Width: 14}.Layout),
				layout.Flexed(1, func(gtx C) D {
					if v.sub == "" {
						return u.label(15.5, title, p.Text, labelOpts{maxLines: 1}).Layout(gtx)
					}
					return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
						layout.Rigid(u.label(15.5, title, p.Text, labelOpts{maxLines: 1}).Layout),
						layout.Rigid(u.label(13.5, v.sub, p.PopupSub, labelOpts{maxLines: 1}).Layout),
					)
				}),
			)
		})
	})
}

// layoutNote draws a command's note: a bubble only you see, with the
// command as you ran it, what it did, its buttons, and a way to dismiss
// it.
func (u *UI) layoutNote(gtx C, ln *localNote, maxW int) D {
	p := u.pal
	n := ln.note
	chat := u.selected.ID
	for i, b := range n.Buttons {
		if u.btn("note:"+ln.id+":"+itoa(i)).Clicked(gtx) && b.Run != nil {
			b.Run()
		}
	}
	if u.btn("note:" + ln.id + ":x").Clicked(gtx) {
		u.dismissNote(chat, n)
	}
	inset := gtx.Dp(9)
	inner := maxW - 2*inset
	cgtx := gtx
	cgtx.Constraints = layout.Constraints{Max: image.Pt(inner, gtx.Constraints.Max.Y)}
	title := record(cgtx, func(gtx C) D {
		return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
			layout.Rigid(iconW(icTerminal, 16, p.Green)),
			layout.Rigid(layout.Spacer{Width: 6}.Layout),
			layout.Rigid(u.label(13.5, n.Title, p.Green, labelOpts{weight: font.SemiBold, maxLines: 1}).Layout),
		)
	})
	col := p.Text
	if n.Failed {
		col = p.Danger
	}
	text := n.Text
	if n.Busy && text == "" {
		text = "Working…"
	}
	body := record(cgtx, u.label(15, text, col, labelOpts{maxLines: 0}).Layout)
	footer := record(cgtx, func(gtx C) D {
		return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
			layout.Rigid(u.label(12.5, "Only you can see this · ", p.MetaIn, labelOpts{maxLines: 1}).Layout),
			layout.Rigid(func(gtx C) D {
				return clickable(gtx, u.btn("note:"+ln.id+":x"),
					u.label(12.5, "Dismiss", p.BubbleButton, labelOpts{weight: font.Medium, maxLines: 1}).Layout)
			}),
		)
	})
	var btns []part
	for _, b := range n.Buttons {
		col := p.BubbleButton
		if b.Danger {
			col = p.Danger
		}
		btns = append(btns, record(cgtx, u.label(15, b.Label, col, labelOpts{weight: font.Medium, maxLines: 1}).Layout))
	}
	w := max(title.size.X, body.size.X, footer.size.X, min(inner, gtx.Dp(240))) + 2*inset
	btnH := gtx.Dp(42)
	h := inset + title.size.Y + gtx.Dp(4) + body.size.Y + gtx.Dp(6) + len(btns)*btnH + footer.size.Y + gtx.Dp(7)
	if len(btns) > 0 {
		h += gtx.Dp(2)
	}
	u.paintBubble(gtx, w, h, p.BubbleIn, false, false)
	y := inset
	title.at(gtx, inset, y)
	y += title.size.Y + gtx.Dp(4)
	body.at(gtx, inset, y)
	y += body.size.Y + gtx.Dp(6)
	line := max(1, gtx.Dp(1))
	for i, b := range btns {
		fillRect(gtx, image.Rect(0, y, w, y+line), p.BubbleLine)
		cl := u.btn("note:" + ln.id + ":" + itoa(i))
		t := op.Offset(image.Pt(0, y)).Push(gtx.Ops)
		clickable(gtx, cl, func(gtx C) D {
			sz := image.Pt(w, btnH)
			if hv := u.hover(gtx, cl); hv > 0 {
				fillRect(gtx, image.Rectangle{Max: sz}, faded(p.Hover, hv))
			}
			b.at(gtx, (w-b.size.X)/2, (btnH-b.size.Y)/2)
			return D{Size: sz}
		})
		t.Pop()
		y += btnH
	}
	if len(btns) > 0 {
		fillRect(gtx, image.Rect(0, y, w, y+line), p.BubbleLine)
		y += gtx.Dp(2)
	}
	footer.at(gtx, inset, y)
	return D{Size: image.Pt(gtx.Constraints.Max.X, h)}
}
