package ui

import (
	"image"
	"image/color"
	"slices"
	"sort"
	"strings"
	"unicode"

	"gioui.org/font"
	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"github.com/chomosuke9/wazzapclients/internal/model"
	"github.com/chomosuke9/wazzapclients/internal/ui/icon"
)

// mentionRef is an @mention inserted from the picker: the composer shows
// "@Name", the message carries "@<user>" plus the JID.
type mentionRef struct {
	name, jid string
}

// Picker entries that aren't members: "@all" notifies everyone, "@admin"
// the group's admins.
const (
	mentionAllID   = "@all"
	mentionAdminID = "@admin"
)

// requestFocus focuses tag (an editor), or clears focus when nil, on the
// next frame.
func (u *UI) requestFocus(tag event.Tag) {
	u.focus, u.focusReq = tag, true
}

func (u *UI) applyFocus(gtx C) {
	if u.focusReq {
		gtx.Execute(key.FocusCmd{Tag: u.focus})
		u.focus, u.focusReq = nil, false
	}
}

func (u *UI) startReply(m *model.Message) {
	u.cancelEdit()
	u.conv.reply = m
	u.requestFocus(&u.conv.composer)
}

// replyPrivately answers a group message in a one-to-one chat with its
// author, quoting it.
func (u *UI) replyPrivately(m *model.Message) {
	u.openDirect(m.SenderID, m.Sender)
	u.conv.reply = m
}

// openDirect opens the one-to-one chat with a user, creating it in the
// list if it doesn't exist yet.
func (u *UI) openDirect(id, name string) {
	c := u.chatByID(id)
	if c == nil {
		c = &model.Chat{ID: id, Name: strings.TrimPrefix(plainText(name), "~"), Time: u.now()}
	}
	u.setPage(pageChats)
	u.open(c)
}

func (u *UI) closeChat() {
	u.selected = nil
	u.hideInfo()
	u.hideChatSearch()
	u.endSelect()
}

func (u *UI) startSelect(m *model.Message) {
	u.conv.selecting = true
	u.conv.picked = map[string]bool{m.ID: true}
}

func (u *UI) endSelect() {
	u.conv.selecting = false
	u.conv.picked = nil
}

// pickedMessages returns the selected messages in chat order.
func (u *UI) pickedMessages() []*model.Message {
	var out []*model.Message
	for _, m := range u.msgs {
		if u.conv.picked[m.ID] {
			out = append(out, m)
		}
	}
	return out
}

// scrollMessages scrolls the message list on the next layout; the zero
// Position means the newest message. Changing the list's position directly
// while it lays out (a click inside a message) would scroll to the top.
func (u *UI) scrollMessages(p layout.Position) {
	if p == (layout.Position{}) && u.conv.newerMore {
		u.loadLatest()
		return
	}
	u.conv.scrollTo = &p // layoutMessages redraws for it
	u.conv.glide = glide{}
}

// jumpTo scrolls smoothly to a message and flashes it.
func (u *UI) jumpTo(id string) {
	if u.selected == nil {
		return
	}
	i := slices.IndexFunc(u.rows(u.selected), func(r convRow) bool { return r.has(id) })
	if i < 0 {
		i = u.loadAround(id)
	}
	if i < 0 {
		u.toast("That message isn't available.")
		return
	}
	u.conv.glide = glide{first: max(0, i-2), pending: true}
	u.conv.flash, u.conv.flashUntil = id, u.now().Add(flashTime)
}

// sendComposer sends the composer text with its reply and mentions.
func (u *UI) sendComposer() {
	if u.postingStatus() {
		u.sendAttachments()
		return
	}
	if u.selected == nil {
		return
	}
	if ms := u.mentionQuery(); ms != nil {
		u.updateMentionSel(ms)
		u.pickMention(u.conv.mentionSel)
		return
	}
	if sp := u.slashQuery(); sp != nil {
		u.submitSlash(sp)
		return
	}
	if u.conv.edit.msg != nil {
		u.finishEdit()
		return
	}
	if len(u.attach.files) > 0 {
		// Each file goes with its own caption.
		u.sendAttachments()
		u.scrollMessages(layout.Position{})
		return
	}
	txt := trimSpace(u.conv.composer.Text())
	if txt == "" {
		return
	}
	d := u.draftFrom(txt)
	d.Reply = u.conv.reply
	u.conv.composer.SetText("")
	u.conv.reply = nil
	u.conv.mentions = nil
	if m := u.backend.Send(u.selected.ID, d); m != nil {
		if u.chatByID(m.ChatID) == nil {
			u.chats = append(u.chats, u.selected)
		}
		u.upsertMessage(m)
	}
	u.scrollMessages(layout.Position{}) // jump to the newest message
}

// draftFrom makes a message of composer text, turning the picked
// mentions in it into the protocol's.
func (u *UI) draftFrom(txt string) model.Draft {
	return u.draftWith(txt, u.conv.mentions, u.selected.ID)
}

// draftWith makes a message for chat of text with the mentions picked in
// it.
func (u *UI) draftWith(txt string, mentions []mentionRef, chat string) model.Draft {
	d := model.Draft{Text: txt}
	// Longer names first, so "@Al" doesn't eat the start of "@Alice".
	refs := append([]mentionRef(nil), mentions...)
	sort.SliceStable(refs, func(a, b int) bool { return len(refs[a].name) > len(refs[b].name) })
	seen := map[string]bool{}
	addJID := func(jid string) {
		if !seen[jid] {
			seen[jid] = true
			d.Mentions = append(d.Mentions, jid)
		}
	}
	for _, mr := range refs {
		at := "@" + mr.name
		if !strings.Contains(d.Text, at) {
			continue
		}
		switch mr.jid {
		case mentionAllID:
			d.MentionAll = true // the text keeps "@all"
			continue
		case mentionAdminID:
			d.MentionAdmins = true
			d.Text = strings.ReplaceAll(d.Text, at, "@"+chat)
			if info := u.chatMembers(chat); info != nil {
				for _, m := range info.Members {
					if m.Admin && !m.Me {
						addJID(m.ID)
					}
				}
			}
			continue
		}
		user := mr.jid
		if i := strings.IndexByte(user, '@'); i >= 0 {
			user = user[:i]
		}
		d.Text = strings.ReplaceAll(d.Text, at, "@"+user)
		addJID(mr.jid)
	}
	return d
}

// mentionState describes an "@query" being typed before the caret.
type mentionState struct {
	start, end int // rune offsets of "@query"
	query      string
	members    []model.Member
}

// mentionQuery returns the mention being typed in a group chat, or nil.
func (u *UI) mentionQuery() *mentionState {
	c := u.selected
	if c == nil || !c.IsGroup || u.postingStatus() || !u.slashTakesMentions() {
		return nil // a command's picker offers members itself
	}
	ed := &u.conv.composer
	caret, _ := ed.Selection()
	txt := []rune(ed.Text())
	if caret > len(txt) {
		return nil
	}
	i := caret
	for i > 0 && caret-i <= 30 {
		r := txt[i-1]
		if r == '@' {
			if i > 1 && !unicode.IsSpace(txt[i-2]) {
				return nil // part of an e-mail address
			}
			q := strings.ToLower(string(txt[i:caret]))
			if u.conv.mentionDismissed == string(txt[i-1:caret]) {
				return nil
			}
			// A mention picked already, with the caret after it, isn't a
			// query: "@Vivy " would still find Vivy, and Enter would pick
			// her again instead of sending.
			for _, mr := range u.conv.mentions {
				at := []rune("@" + mr.name)
				if end := i - 1 + len(at); end <= caret && string(txt[i-1:end]) == string(at) {
					return nil
				}
			}
			info := u.chatMembers(c.ID)
			if info == nil {
				return nil
			}
			hits := u.mentionHits(info, q)
			if len(hits) == 0 {
				return nil
			}
			return &mentionState{start: i - 1, end: caret, query: q, members: hits}
		}
		if r == '\n' || (unicode.IsSpace(r) && caret-i > 20) {
			return nil
		}
		i--
	}
	return nil
}

// mentionHitsKey is what mentionHits' answer depends on, besides the
// members, whose cache chatMembers drops when it fetches them again.
type mentionHitsKey struct {
	query, me string
	info      *model.ChatInfo
	admin     bool
}

// mentionHits lists what the mention picker offers for query: "@all" and
// "@admin", then the members it finds (see fuzzy.go), best first, saved
// contacts before the rest, yourself last.
func (u *UI) mentionHits(info *model.ChatInfo, query string) []model.Member {
	c := &u.conv
	k := mentionHitsKey{query: query, me: u.meName(), info: info, admin: u.adminMention}
	if c.hitsOK && c.hitsKey == k {
		return c.hits
	}
	type hit struct {
		m     model.Member
		score int
	}
	var hits []hit
	var me *hit
	for _, m := range info.Members {
		s := matchExact // everyone, for a bare "@"
		switch {
		case query == "":
		case m.Me:
			s = personScore(query, "", m.Name, k.me)
		default:
			s = memberScore(m, query)
		}
		switch {
		case s == 0:
		case m.Me:
			me = &hit{m, s}
		default:
			hits = append(hits, hit{m, s})
		}
	}
	slices.SortStableFunc(hits, func(a, b hit) int {
		if a.score != b.score {
			return b.score - a.score
		}
		switch as, bs := a.m.Contact != "", b.m.Contact != ""; {
		case as && !bs:
			return -1
		case bs && !as:
			return 1
		}
		return 0
	})
	var out []model.Member
	for _, id := range []string{mentionAllID, mentionAdminID} {
		if id == mentionAdminID && !u.adminMention {
			continue // an extra feature
		}
		if strings.HasPrefix(id[1:], query) {
			out = append(out, model.Member{ID: id, Name: id[1:]})
		}
	}
	for _, h := range hits {
		out = append(out, h.m)
	}
	if me != nil {
		out = append(out, me.m)
	}
	c.hitsKey, c.hits, c.hitsOK = k, out, true
	return out
}

// mentionName is what picking a member puts after the "@": the name the
// picker shows, without the "~".
func (u *UI) mentionName(m model.Member) string {
	if m.Me {
		return u.meName()
	}
	return strings.TrimPrefix(memberTitle(m), "~")
}

// pickMention replaces the typed "@query" with the chosen member.
func (u *UI) pickMention(i int) {
	ms := u.mentionQuery()
	if ms == nil || i >= len(ms.members) {
		return
	}
	m := ms.members[i]
	name := u.mentionName(m)
	ed := &u.conv.composer
	ed.SetCaret(ms.start, ms.end)
	ed.Insert("@" + name + " ")
	u.conv.mentions = append(u.conv.mentions, mentionRef{name: name, jid: m.ID})
	u.requestFocus(ed)
}

// mentionRows is how many rows the mention picker shows at once.
const mentionRows = 6

// updateMentionSel puts the highlight on the first row when the query
// changes. The picker fading out keeps the row it had.
func (u *UI) updateMentionSel(ms *mentionState) {
	c := &u.conv
	if ms == nil {
		c.mentionSelFor = ""
		return
	}
	q := itoa(ms.start) + "\x00" + ms.query + "\x00" + itoa(len(ms.members))
	if q != c.mentionSelFor {
		c.mentionSelFor, c.mentionSel = q, 0
		c.mentionList.Position = layout.Position{}
	}
}

// mentionKeys moves through the mention picker with the arrow keys, and
// picks with Tab or Enter (even when Enter adds a line). It runs before the
// composer reads its keys.
func (u *UI) mentionKeys(gtx C) {
	ms := u.mentionQuery()
	u.updateMentionSel(ms)
	if ms == nil {
		return
	}
	c := &u.conv
	ed := &c.composer
	n := len(ms.members)
	for {
		ev, ok := gtx.Event(
			key.Filter{Focus: ed, Name: key.NameUpArrow},
			key.Filter{Focus: ed, Name: key.NameDownArrow},
			key.Filter{Focus: ed, Name: key.NameTab},
			key.Filter{Focus: ed, Name: key.NameReturn},
			key.Filter{Focus: ed, Name: key.NameEnter},
		)
		if !ok {
			break
		}
		e, ok := ev.(key.Event)
		if !ok || e.State != key.Press {
			continue
		}
		switch e.Name {
		case key.NameUpArrow:
			c.mentionSel = (c.mentionSel - 1 + n) % n
		case key.NameDownArrow:
			c.mentionSel = (c.mentionSel + 1) % n
		default:
			u.pickMention(c.mentionSel)
			return
		}
		keepVisible(&c.mentionList.List, c.mentionSel, min(n, mentionRows))
	}
}

// resetComposerAnims ends the composer's animations, for a chat switch.
func (u *UI) resetComposerAnims() {
	c := &u.conv
	c.replyAnim.snap(false)
	c.replyGhost, c.ghostEdit = nil, false
	c.mentionAnim.snap(false)
	c.mentionGhost = nil
	c.richFor = "" // a command's name shows only where it runs
	s := &u.slash
	s.anim.snap(false)
	s.ghost, s.contacts, s.dismissed, s.problem = nil, nil, "", ""
	c.selAnim.snap(false)
	c.selV = 0
	c.sendAnim.snap(false)
	c.glide = glide{}
}

// chatMembers returns a group's member list, cached per chat.
func (u *UI) chatMembers(chatID string) *model.ChatInfo {
	if u.conv.membersFor != chatID {
		u.conv.members, u.conv.membersFor = u.backend.Info(chatID), chatID
		u.conv.hitsOK = false
	}
	return u.conv.members
}

// amAdmin reports whether you administer a group.
func (u *UI) amAdmin(chatID string) bool {
	if info := u.chatMembers(chatID); info != nil {
		for _, m := range info.Members {
			if m.Me {
				return m.Admin
			}
		}
	}
	return false
}

// mentionRanges returns the rune ranges of the picked @mentions still in
// the composer text.
func (u *UI) mentionRanges(txt string) [][2]int {
	var out [][2]int
	runes := []rune(txt)
	for _, mr := range u.conv.mentions {
		at := []rune("@" + mr.name)
		for i := 0; i+len(at) <= len(runes); i++ {
			if string(runes[i:i+len(at)]) == string(at) {
				out = append(out, [2]int{i, i + len(at)})
				i += len(at) - 1
			}
		}
	}
	return out
}

// layoutMentionPicker draws matching members, plus "@all" and "@admin",
// above the composer.
func (u *UI) layoutMentionPicker(gtx C, ms *mentionState) D {
	p := u.pal
	for i, m := range ms.members {
		if u.btn("mention:" + m.ID).Clicked(gtx) {
			u.pickMention(i) // it keeps drawing while it fades out
		}
	}
	rowH := gtx.Dp(56)
	pad := gtx.Dp(8)
	n := min(len(ms.members), mentionRows)
	gtx.Constraints.Min.X = gtx.Constraints.Max.X
	w := gtx.Constraints.Max.X
	h := n*rowH + 2*pad
	r := gtx.Dp(16)
	rect := image.Rect(0, 0, w, h)
	fillRRect(gtx, rect.Add(image.Pt(0, gtx.Dp(2))).Inset(-gtx.Dp(1)), r, p.Shadow)
	borderRRect(gtx, rect, r, p.Popup, p.PopupBorder)
	defer clip.UniformRRect(rect, r).Push(gtx.Ops).Pop()
	gtx.Constraints = layout.Exact(image.Pt(w-2*pad, h-2*pad))
	t := op.Offset(image.Pt(pad, pad)).Push(gtx.Ops)
	u.scrollList(gtx, &u.conv.mentionList, len(ms.members), func(gtx C, i int) D {
		m := ms.members[i]
		cl := u.btn("mention:" + m.ID)
		return clickable(gtx, cl, func(gtx C) D {
			sz := image.Pt(gtx.Constraints.Max.X, rowH)
			h := u.hover(gtx, cl)
			if i == u.conv.mentionSel {
				h = 1 // Enter picks it
			}
			if h > 0 {
				fillRRect(gtx, image.Rectangle{Max: sz}, gtx.Dp(10), faded(p.PopupHover, h))
			}
			gtx.Constraints = layout.Constraints{Max: sz}
			// A member's name, with their phone number when you haven't
			// saved them.
			name, sub, size, many := memberTitle(m), memberSub(m), unit.Sp(15.5), true
			switch m.ID {
			case mentionAllID:
				sub, size = "Mention all members in this chat", 16
			case mentionAdminID:
				sub, size = "Mention all admins in this chat", 16
			default:
				many = false
			}
			vcenter(gtx, rowH, func(gtx C) D {
				gtx.Constraints.Min.X = gtx.Constraints.Max.X
				return layout.Inset{Left: 12, Right: 12}.Layout(gtx, func(gtx C) D {
					return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
						layout.Rigid(func(gtx C) D {
							if !many {
								return u.avatar(gtx, m.ID, m.Name, false, 36)
							}
							d := gtx.Dp(36)
							fillCircle(gtx, image.Pt(d/2, d/2), d/2, p.PopupBorder)
							return centerIn(gtx, d, iconW(icGroups, 22, p.UserAvatarIcon))
						}),
						layout.Rigid(layout.Spacer{Width: 14}.Layout),
						layout.Flexed(1, func(gtx C) D {
							if sub == "" {
								return u.label(size, name, p.Text, labelOpts{maxLines: 1}).Layout(gtx)
							}
							return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
								layout.Rigid(u.label(size, name, p.Text, labelOpts{maxLines: 1}).Layout),
								layout.Rigid(u.label(13.5, sub, p.PopupSub, labelOpts{maxLines: 1}).Layout),
							)
						}),
					)
				})
			})
			return D{Size: sz}
		})
	})
	t.Pop()
	return D{Size: image.Pt(w, h)}
}

// layoutReplyPreview is the quoted message above the composer's input.
func (u *UI) layoutReplyPreview(gtx C, m *model.Message) D {
	p := u.pal
	if u.btn("reply:close").Clicked(gtx) {
		u.conv.reply = nil // it keeps drawing while it shrinks away
	}
	q := &model.Quote{ID: m.ID, Text: m.Text, Media: m.Media}
	if !m.FromMe {
		q.Sender = m.Sender
		if q.Sender == "" && u.selected != nil && !u.selected.IsGroup {
			q.Sender = u.selected.Name
		}
		if q.Sender == "" {
			q.Sender = u.selected.Name
		}
	}
	if m.Kind == model.KindImage && m.Text == "" {
		q.Text = ""
	}
	return layout.Inset{Left: 8, Right: 8, Top: 8}.Layout(gtx, func(gtx C) D {
		return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
			layout.Flexed(1, func(gtx C) D {
				return u.layoutQuote(gtx, q, p.QuoteIn, p.TextSecondary, gtx.Constraints.Max.X, m)
			}),
			layout.Rigid(layout.Spacer{Width: 8}.Layout),
			layout.Rigid(func(gtx C) D { return u.iconButton(gtx, u.btn("reply:close"), icClose, 40, 24, p.Icon) }),
		)
	})
}

// layoutComposer is the floating message box at the bottom of a chat, or
// the select bar in select mode. Switching between them cross-fades.
func (u *UI) layoutComposer(gtx C) D {
	if u.conv.selecting {
		return fadeW(gtx, easeOut(u.conv.selV), u.layoutSelectBar)
	}
	return fadeW(gtx, easeOut(1-u.conv.selV), u.layoutComposerBox)
}

// layoutComposerBox is the message box, with the reply preview and the
// mention picker above the input. Both grow in and shrink away.
func (u *UI) layoutComposerBox(gtx C) D {
	p := u.pal
	c := &u.conv
	ms := u.mentionQuery()
	u.updateMentionSel(ms) // the text may have changed since mentionKeys
	if ms != nil {
		c.mentionGhost = ms
	}
	mv := easeOut(c.mentionAnim.step(gtx, ms != nil, popDur(ms != nil)))
	if mv == 0 {
		c.mentionGhost = nil
	}
	sp := u.slashQuery()
	if !u.slashShown(sp) || ms != nil {
		sp = nil // the mention picker takes its place
	}
	if sp != nil {
		u.slash.ghost = sp
	}
	spv := easeOut(u.slash.anim.step(gtx, sp != nil, popDur(sp != nil)))
	if spv == 0 {
		u.slash.ghost = nil
	}
	// The bar above the input quotes the message being answered, or the
	// one being edited.
	reply, editing := c.reply, c.edit.msg != nil
	if editing {
		reply = c.edit.msg
	}
	if reply != nil {
		c.replyGhost, c.ghostEdit = reply, editing
	}
	rv := easeOut(c.replyAnim.step(gtx, reply != nil, durGrow))
	if rv == 0 {
		c.replyGhost = nil
	}
	hasText := trimSpace(c.composer.Text()) != "" && !c.editorElsewhere
	sv := easeOut(c.sendAnim.step(gtx, hasText, durSwitch))
	return layout.Inset{Left: 12, Right: 12, Top: 6, Bottom: 12}.Layout(gtx, func(gtx C) D {
		gtx.Constraints.Min.X = gtx.Constraints.Max.X
		var picker, slashPicker part
		if ghost := c.mentionGhost; ghost != nil {
			picker = record(gtx, func(gtx C) D {
				if ms == nil {
					var done func()
					gtx, done = fadeOut(gtx)
					defer done()
				}
				return u.layoutMentionPicker(gtx, ghost)
			})
		}
		if ghost := u.slash.ghost; ghost != nil {
			slashPicker = record(gtx, func(gtx C) D {
				if sp == nil {
					var done func()
					gtx, done = fadeOut(gtx)
					defer done()
				}
				return u.layoutSlashPicker(gtx, ghost)
			})
		}
		m := op.Record(gtx.Ops)
		dims := layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx C) D {
				ghost := c.replyGhost
				if ghost == nil {
					return D{}
				}
				// The preview rises out of the input as the box grows.
				full := record(gtx, func(gtx C) D {
					if reply == nil {
						var done func()
						gtx, done = fadeOut(gtx)
						defer done()
					}
					if c.ghostEdit {
						return u.layoutEditPreview(gtx, ghost)
					}
					return u.layoutReplyPreview(gtx, ghost)
				})
				h := lerpInt(0, full.size.Y, rv)
				defer clip.Rect{Max: image.Pt(full.size.X, h)}.Push(gtx.Ops).Pop()
				withOpacity(gtx, rv, func() { full.at(gtx, 0, h-full.size.Y) })
				return D{Size: image.Pt(full.size.X, h)}
			}),
			layout.Rigid(func(gtx C) D {
				return vcenter(gtx, gtx.Dp(55), func(gtx C) D {
					return layout.Inset{Left: 8, Right: 8}.Layout(gtx, func(gtx C) D {
						return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
							layout.Rigid(func(gtx C) D {
								return clickable(gtx, &u.conv.attach, func(gtx C) D {
									sz := gtx.Dp(40)
									if h := u.hover(gtx, &u.conv.attach); h > 0 {
										fillCircle(gtx, image.Pt(sz/2, sz/2), sz/2, faded(p.Hover, h))
									}
									return centerIn(gtx, sz, iconW(icAttach, 26, p.IconStrong))
								})
							}),
							layout.Rigid(layout.Spacer{Width: 2}.Layout),
							layout.Rigid(func(gtx C) D {
								col := p.IconStrong
								if u.picker.open && u.picker.mode == pickComposer {
									col = p.Green
								}
								return u.iconButton(gtx, &u.conv.emoji, icEmoji, 40, 26, col)
							}),
							layout.Rigid(layout.Spacer{Width: 10}.Layout),
							layout.Flexed(1, func(gtx C) D {
								if c.editorElsewhere {
									// The send view has the editor; this is
									// only seen while it slides.
									txt, col := u.attach.draft, p.Text
									if txt == "" {
										txt, col = "Type a message", p.ComposerHint
									}
									return vcenter(gtx, gtx.Constraints.Min.Y, u.label(16, txt, col).Layout)
								}
								return u.layoutComposerEditor(gtx, "Type a message")
							}),
							layout.Rigid(layout.Spacer{Width: 8}.Layout),
							layout.Rigid(func(gtx C) D {
								// The mic turns into the send button as you type.
								return clickable(gtx, &u.conv.send, func(gtx C) D {
									sz := gtx.Dp(40)
									mid := image.Pt(sz/2, sz/2)
									if h := u.hover(gtx, &u.conv.send) * (1 - sv); h > 0 {
										fillCircle(gtx, mid, sz/2, faded(p.Hover, h))
									}
									if sv < 1 {
										fx := pushFx(gtx, 1-sv, scaleAt(mid, lerp(1, 0.5, sv)))
										centerIn(gtx, sz, iconW(icMic, 26, p.IconStrong))
										fx.Pop()
									}
									if sv > 0 {
										fx := pushFx(gtx, sv, scaleAt(mid, lerp(0.5, 1, sv)))
										fillCircle(gtx, mid, sz/2, p.Green)
										if editing {
											centerIn(gtx, sz, iconW(icTick, 24, p.OnGreen))
										} else {
											centerIn(gtx, sz, iconW(icSend, 21, p.OnGreen))
										}
										fx.Pop()
									}
									return D{Size: image.Pt(sz, sz)}
								})
							}),
						)
					})
				})
			}),
		)
		call := m.Stop()
		r := lerpInt(gtx.Dp(28), gtx.Dp(20), rv)
		fillRRect(gtx, image.Rectangle{Max: dims.Size}, min(dims.Size.Y/2, r), p.Composer)
		call.Add(gtx.Ops)
		for _, pk := range []struct {
			p part
			v float32
		}{{picker, mv}, {slashPicker, spv}} {
			if pk.p.size.Y > 0 {
				fx := pushFx(gtx, pk.v, moveBy(0, float32(gtx.Dp(10))*(1-pk.v)))
				pk.p.at(gtx, 0, -pk.p.size.Y-gtx.Dp(8))
				fx.Pop()
			}
		}
		u.conv.composerH = dims.Size.Y + gtx.Dp(18)
		return dims
	})
}

// layoutComposerEditor lays out the composer's editor, which is also the
// send view's caption field: formatting and mentions drawn over it, the
// format bar, and clicks around the text. gtx.Constraints.Min.Y is the
// height of the row it's in.
func (u *UI) layoutComposerEditor(gtx C, hint string) D {
	p := u.pal
	u.formatKeys(gtx)
	pad := gtx.Dp(8)
	rowMin := gtx.Constraints.Min.Y
	m := op.Record(gtx.Ops)
	var ed D
	dims := layout.Inset{Top: 8, Bottom: 8}.Layout(gtx, func(gtx C) D {
		gtx.Constraints.Min.X = gtx.Constraints.Max.X
		gtx.Constraints.Min.Y = 0
		gtx.Constraints.Max.Y = gtx.Dp(140)
		e := material.Editor(u.th, &u.conv.composer, hint)
		e.TextSize = 16
		e.Color = p.Text
		e.HintColor = p.ComposerHint
		e.SelectionColor = argb(0x53bdeb, 0x60)
		// Formatting and mentions are drawn over the editor, which then
		// paints its text clear.
		txt := u.conv.composer.Text()
		rich := u.composerRich(txt)
		if rich {
			e.Color = color.NRGBA{}
		}
		ed = e.Layout(gtx)
		if rich {
			u.paintComposerText(gtx, e, txt, ed.Size)
		}
		u.paintSlashHint(gtx, ed.Size)
		u.layoutFormatBar(gtx, &u.conv.composer)
		return ed
	})
	call := m.Stop()
	// Clicks around the text, up to the row's edges, go to the editor too.
	ext := max(0, (rowMin-dims.Size.Y)/2)
	area := image.Rectangle{Min: image.Pt(0, -ext), Max: image.Pt(dims.Size.X, dims.Size.Y+ext)}
	u.composerArea(gtx, area, image.Pt(0, pad), ed.Size)
	call.Add(gtx.Ops)
	return dims
}

// layoutSelectBar replaces the composer while selecting messages.
func (u *UI) layoutSelectBar(gtx C) D {
	p := u.pal
	picked := u.pickedMessages()
	if u.btn("sel:cancel").Clicked(gtx) {
		u.endSelect()
	}
	if len(picked) > 0 {
		if u.btn("sel:star").Clicked(gtx) {
			star := false
			for _, m := range picked {
				star = star || !m.Starred
			}
			for _, m := range picked {
				u.backend.Star(m, star)
			}
			u.endSelect()
		}
		if u.btn("sel:delete").Clicked(gtx) {
			u.confirmDelete(picked)
		}
		if u.btn("sel:forward").Clicked(gtx) {
			u.openForward(picked)
		}
		if u.btn("sel:copy").Clicked(gtx) {
			var lines []string
			for _, m := range picked {
				if t := stripIsolates(plainText(m.Text)); t != "" {
					lines = append(lines, t)
				}
			}
			u.copyText(strings.Join(lines, "\n"))
			u.endSelect()
		}
	}
	gtx.Constraints.Min.X = gtx.Constraints.Max.X
	return background(gtx, p.Panel, 0, func(gtx C) D {
		return vcenter(gtx, gtx.Dp(62), func(gtx C) D {
			return layout.Inset{Left: 16, Right: 16}.Layout(gtx, func(gtx C) D {
				col := p.IconStrong
				if len(picked) == 0 {
					col = p.EmptyIcon
				}
				btn := func(key string, ic *icon.Icon) layout.FlexChild {
					return layout.Rigid(func(gtx C) D {
						return layout.Inset{Left: 8}.Layout(gtx, func(gtx C) D { return u.iconButton(gtx, u.btn(key), ic, 42, 24, col) })
					})
				}
				return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(func(gtx C) D { return u.iconButton(gtx, u.btn("sel:cancel"), icClose, 42, 24, p.IconStrong) }),
					layout.Rigid(layout.Spacer{Width: 16}.Layout),
					layout.Flexed(1, u.label(16, itoa(len(picked))+" selected", p.Text, labelOpts{maxLines: 1}).Layout),
					btn("sel:copy", icCopy),
					btn("sel:star", icStar),
					btn("sel:delete", icDelete),
					btn("sel:forward", icForward),
				)
			})
		})
	})
}

// hoverArea reports whether the pointer is over an area of size sz at the
// current offset, without taking events from handlers underneath.
func (u *UI) hoverArea(gtx C, key string, sz image.Point) bool {
	tag := u.btn("hv:" + key)
	for {
		ev, ok := gtx.Event(pointer.Filter{Target: tag, Kinds: pointer.Enter | pointer.Leave | pointer.Cancel})
		if !ok {
			break
		}
		if e, ok := ev.(pointer.Event); ok {
			switch e.Kind {
			case pointer.Enter:
				u.hovered[key] = true
			default:
				delete(u.hovered, key)
			}
		}
	}
	defer clip.Rect{Max: sz}.Push(gtx.Ops).Pop()
	defer pointer.PassOp{}.Push(gtx.Ops).Pop()
	event.Op(gtx.Ops, tag)
	return u.hovered[key]
}

// layoutPinnedBanner shows the chat's pinned message under the header.
func (u *UI) layoutPinnedBanner(gtx C, m *model.Message) D {
	p := u.pal
	cl := u.btn("pinned:" + m.ID)
	if cl.Clicked(gtx) {
		u.jumpTo(m.ID)
	}
	gtx.Constraints.Min.X = gtx.Constraints.Max.X
	return clickable(gtx, cl, func(gtx C) D {
		bg := mix(p.Panel, p.Hover, u.hover(gtx, cl))
		d := background(gtx, bg, 0, func(gtx C) D {
			return vcenter(gtx, gtx.Dp(50), func(gtx C) D {
				return layout.Inset{Left: 22, Right: 16}.Layout(gtx, func(gtx C) D {
					txt := plainText(stripIsolates(m.Text))
					if m.Media != model.MediaNone && txt == "" {
						txt = mediaLabel(m)
					}
					who, _, _ := u.senderLabel(m)
					return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
						layout.Rigid(iconW(icPin, 20, p.TextSecondary)),
						layout.Rigid(layout.Spacer{Width: 16}.Layout),
						layout.Rigid(u.label(14.5, who+": ", p.TextSecondary, labelOpts{weight: font.Medium, maxLines: 1}).Layout),
						layout.Flexed(1, u.label(14.5, firstLine(txt), p.TextSecondary, labelOpts{maxLines: 1}).Layout),
					)
				})
			})
		})
		fillRect(gtx, image.Rect(0, d.Size.Y-max(1, gtx.Dp(1)), d.Size.X, d.Size.Y), p.Divider)
		return d
	})
}

// composerArea passes presses and drags in area, around the composer's
// text, to the editor at offset edAt of size edSize: the editor's own
// input area covers only its lines, a thin strip in the round box.
func (u *UI) composerArea(gtx C, area image.Rectangle, edAt, edSize image.Point) {
	c := &u.conv
	ed := &c.composer
	for {
		ev, ok := gtx.Event(pointer.Filter{Target: &c.composerArea, Kinds: pointer.Press | pointer.Drag | pointer.Release | pointer.Cancel})
		if !ok {
			break
		}
		e, ok := ev.(pointer.Event)
		if !ok {
			continue
		}
		pos := e.Position.Round().Sub(edAt)
		switch e.Kind {
		case pointer.Press:
			if !e.Buttons.Contain(pointer.ButtonPrimary) {
				continue
			}
			i := composerCaretAt(ed, pos, edSize)
			c.composerFrom, c.composerPress = i, true
			if e.Modifiers.Contain(key.ModShift) {
				_, c.composerFrom = ed.Selection()
			}
			ed.SetCaret(i, c.composerFrom)
			gtx.Execute(key.FocusCmd{Tag: ed})
		case pointer.Drag:
			if c.composerPress {
				ed.SetCaret(composerCaretAt(ed, pos, edSize), c.composerFrom)
			}
		default:
			c.composerPress = false
		}
	}
	defer clip.Rect(area).Push(gtx.Ops).Pop()
	pointer.CursorText.Add(gtx.Ops)
	event.Op(gtx.Ops, &c.composerArea)
}

// composerCaretAt returns the caret position nearest to p, in the
// editor's coordinates, on the line level with p (the first or last line
// when p is above or below the text).
func composerCaretAt(ed *widget.Editor, p, size image.Point) int {
	n := ed.Len()
	y := min(max(p.Y, 0), max(0, size.Y-1))
	best, bestD := n, -1
	var rs []widget.Region
	for i := range n {
		rs = ed.Regions(i, i+1, rs[:0])
		for _, r := range rs {
			b := r.Bounds
			if y < b.Min.Y || y >= b.Max.Y {
				continue
			}
			for j, x := range [2]int{b.Min.X, b.Max.X} {
				if d := max(p.X-x, x-p.X); bestD < 0 || d < bestD {
					best, bestD = i+j, d
				}
			}
		}
	}
	return best
}
