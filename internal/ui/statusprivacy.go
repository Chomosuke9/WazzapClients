package ui

import (
	"image"
	"image/color"
	"math"
	"slices"
	"strings"

	"gioui.org/f32"
	"gioui.org/font"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/text"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"github.com/chomosuke9/wazzapclients/internal/model"
	"github.com/chomosuke9/wazzapclients/internal/ui/icon"
)

// Status privacy: who sees your status updates, and audiences of your own
// (a name and an emoji the people in them see), like the phone app's
// Status privacy. It opens from the Status page's ⋮ and slides in over
// the status list a page at a time, like New chat: the audiences (spMain),
// the people of one (spPeople), and an audience's name and emoji (spName).
// Each change goes to the backend at once, which syncs it to the phone.

type spStep int

const (
	spNone   spStep = iota
	spMain          // the audiences, one of them chosen
	spPeople        // the people of an audience
	spName          // the name and emoji of an audience of your own
)

// maxAudienceName is how long an audience's name can be.
const maxAudienceName = 30

// newAudienceEmoji is a new audience's emoji until another is chosen.
const newAudienceEmoji = "⭐"

// statusPrivacyPanel is the open Status privacy panel. Its pages are a
// stack: New goes from the audiences to the name and then the people,
// an audience's ⋮ from its people to its name.
type statusPrivacyPanel struct {
	stack []spStep
	in    [spName + 1]tween // each page sliding in

	p *model.StatusPrivacy // as last read from the backend; nil while loading

	// The people page: whose people they are (for AudienceList, list is
	// the index in p.Lists, or -1 for a new audience), those picked, and
	// whether Done also chooses the audience.
	kind     model.StatusAudience
	list     int
	picked   []string
	choose   bool
	search   widget.Editor
	contacts []*model.Contact // loaded as the page opens
	shown    []*model.Contact // the rows of this frame, reused

	// The name page.
	name  widget.Editor
	emoji string

	mainList, peopleList, nameList widget.List
}

func (sp *statusPrivacyPanel) open() bool { return len(sp.stack) > 0 }

func (sp *statusPrivacyPanel) top() spStep {
	if len(sp.stack) == 0 {
		return spNone
	}
	return sp.stack[len(sp.stack)-1]
}

// snap finishes the pages' slides (for screenshots).
func (sp *statusPrivacyPanel) snap() {
	for s := spMain; s <= spName; s++ {
		sp.in[s].snap(slices.Contains(sp.stack, s))
	}
}

func (sp *statusPrivacyPanel) push(s spStep) {
	sp.stack = append(slices.DeleteFunc(sp.stack, func(t spStep) bool { return t == s }), s)
}

// openStatusPrivacy slides the Status privacy panel in over the Status
// list.
func (u *UI) openStatusPrivacy() {
	sp := &u.status.privacy
	sp.mainList.Axis, sp.peopleList.Axis, sp.nameList.Axis = layout.Vertical, layout.Vertical, layout.Vertical
	sp.search.SingleLine, sp.search.Submit = true, true
	sp.name.SingleLine, sp.name.Submit, sp.name.MaxLen = true, true, maxAudienceName
	sp.p = u.backend.StatusPrivacy()
	sp.stack = []spStep{spMain}
}

// statusPrivacyBack goes back a page, closing the panel on the first.
func (u *UI) statusPrivacyBack() {
	sp := &u.status.privacy
	if len(sp.stack) > 0 {
		sp.stack = sp.stack[:len(sp.stack)-1]
	}
	u.requestFocus(nil)
}

// closeStatusPrivacy closes the panel at once (when another page is
// chosen).
func (u *UI) closeStatusPrivacy() {
	u.status.privacy = statusPrivacyPanel{}
}

// statusPrivacyEvent reads the setting again after a StatusEvent, while
// the panel shows it.
func (u *UI) statusPrivacyEvent() {
	if sp := &u.status.privacy; sp.open() {
		sp.p = u.backend.StatusPrivacy()
	}
}

// editableStatusPrivacy returns the setting to change, or nil (with a
// toast) while it can't be changed yet.
func (u *UI) editableStatusPrivacy() *model.StatusPrivacy {
	p := u.status.privacy.p
	if p == nil || !p.Editable {
		u.toast("Your status privacy hasn't synced from your phone yet. Keep WhatsApp open on your phone and try again in a minute.")
		return nil
	}
	return p
}

// setStatusPrivacy shows the change at once and sends it.
func (u *UI) setStatusPrivacy(p model.StatusPrivacy) {
	switch p.Audience {
	case model.AudienceContacts:
		p.Count = 0
	case model.AudienceExcept:
		p.Count = len(p.Except)
	case model.AudienceOnly:
		p.Count = len(p.Only)
	case model.AudienceClose:
		p.Count = len(p.Close)
	case model.AudienceList:
		if l := p.List(p.ListID); l != nil {
			p.Count = len(l.Members)
		}
	}
	u.status.privacy.p = &p
	u.backend.SetStatusPrivacy(p)
}

// chooseAudience chooses an audience. One without people, or whose people
// the phone hasn't synced, opens its people first, and is chosen once
// they're picked.
func (u *UI) chooseAudience(kind model.StatusAudience, list int) {
	cur := u.editableStatusPrivacy()
	if cur == nil {
		return
	}
	p := *cur
	if kind != model.AudienceContacts && kind != model.AudienceList && !p.Knows(kind) {
		u.openAudiencePeople(kind, list, true)
		return
	}
	switch kind {
	case model.AudienceOnly, model.AudienceClose:
		if len(audiencePeople(&p, kind, list)) == 0 {
			u.openAudiencePeople(kind, list, true)
			return
		}
	case model.AudienceList:
		if len(p.Lists[list].Members) == 0 {
			u.openAudiencePeople(kind, list, true)
			return
		}
		p.ListID = p.Lists[list].ID
	}
	p.Audience = kind
	u.setStatusPrivacy(p)
}

// audiencePeople returns the people of an audience.
func audiencePeople(p *model.StatusPrivacy, kind model.StatusAudience, list int) []string {
	switch kind {
	case model.AudienceExcept:
		return p.Except
	case model.AudienceOnly:
		return p.Only
	case model.AudienceClose:
		return p.Close
	case model.AudienceList:
		if list >= 0 && list < len(p.Lists) {
			return p.Lists[list].Members
		}
	}
	return nil
}

// openAudiencePeople opens the people of an audience, to pick them.
func (u *UI) openAudiencePeople(kind model.StatusAudience, list int, choose bool) {
	sp := &u.status.privacy
	if kind != model.AudienceList || list >= 0 {
		if u.editableStatusPrivacy() == nil {
			return
		}
		sp.picked = slices.Clone(audiencePeople(sp.p, kind, list))
	}
	sp.kind, sp.list, sp.choose = kind, list, choose
	sp.contacts = u.backend.Contacts()
	sp.search.SetText("")
	sp.peopleList.Position = layout.Position{}
	sp.push(spPeople)
}

// openAudienceName opens the name and emoji of an audience of your own,
// or of a new one (list -1).
func (u *UI) openAudienceName(list int) {
	sp := &u.status.privacy
	p := u.editableStatusPrivacy()
	if p == nil {
		return
	}
	sp.list = list
	sp.name.SetText("")
	sp.emoji = newAudienceEmoji
	if list >= 0 {
		sp.name.SetText(p.Lists[list].Name)
		sp.emoji = p.Lists[list].Emoji
	} else {
		sp.kind, sp.picked = model.AudienceList, nil
	}
	sp.name.SetCaret(sp.name.Len(), sp.name.Len())
	sp.nameList.Position = layout.Position{}
	sp.push(spName)
	u.requestFocus(&sp.name)
}

// saveAudienceName keeps the typed name and emoji: a new audience goes on
// to its people, an existing one is renamed.
func (u *UI) saveAudienceName() {
	sp := &u.status.privacy
	name := trimSpace(sp.name.Text())
	if name == "" {
		u.toast("Give the audience a name.")
		return
	}
	if sp.list < 0 {
		u.openAudiencePeople(model.AudienceList, -1, true)
		return
	}
	cur := u.editableStatusPrivacy()
	if cur == nil {
		return
	}
	p := *cur
	p.Lists = slices.Clone(p.Lists)
	p.Lists[sp.list].Name, p.Lists[sp.list].Emoji = name, sp.emoji
	u.setStatusPrivacy(p)
	u.statusPrivacyBack()
}

// saveAudiencePeople keeps the people picked, and chooses the audience
// when that's what opened them.
func (u *UI) saveAudiencePeople() {
	sp := &u.status.privacy
	cur := u.editableStatusPrivacy()
	if cur == nil {
		return
	}
	picked := slices.Clone(sp.picked)
	if len(picked) == 0 && (sp.choose && sp.kind != model.AudienceExcept || sp.kind == model.AudienceList && sp.list < 0) {
		u.toast("Choose at least one contact.")
		return
	}
	p := *cur
	p.Unknown = slices.DeleteFunc(slices.Clone(p.Unknown), func(a model.StatusAudience) bool { return a == sp.kind })
	switch sp.kind {
	case model.AudienceExcept:
		p.Except = picked
	case model.AudienceOnly:
		p.Only = picked
	case model.AudienceClose:
		p.Close = picked
	case model.AudienceList:
		p.Lists = slices.Clone(p.Lists)
		if sp.list < 0 {
			p.Lists = append(p.Lists, model.StatusList{Name: trimSpace(sp.name.Text()), Emoji: sp.emoji, Members: picked})
			p.ListID = "" // the new one, once it has an ID
		} else {
			p.Lists[sp.list].Members = picked
			p.ListID = p.Lists[sp.list].ID
		}
	}
	if sp.choose {
		p.Audience = sp.kind
	} else if p.Audience == model.AudienceList && sp.kind == model.AudienceList {
		p.ListID = cur.ListID
	}
	u.setStatusPrivacy(p)
	// Back to the audiences.
	sp.stack = []spStep{spMain}
	u.requestFocus(nil)
}

// statusListItems is an audience of your own's ⋮ menu.
func (u *UI) statusListItems() []menuItem {
	sp := &u.status.privacy
	if sp.top() != spPeople || sp.kind != model.AudienceList || sp.list < 0 {
		return nil
	}
	list := sp.list
	return []menuItem{
		{key: "sl-edit", ic: icEdit, label: "Edit name & emoji", run: func() { u.openAudienceName(list) }},
		{key: "sl-delete", ic: icDelete, label: "Delete audience", run: func() { u.confirmDeleteAudience(list) }},
	}
}

// confirmDeleteAudience asks before deleting an audience of your own.
// Status updates shared with it stay shared; if it was chosen, all your
// contacts are next.
func (u *UI) confirmDeleteAudience(list int) {
	u.confirm("Delete this audience?", "Status updates you've already shared with it stay shared. "+
		"If it's the audience you share with, your next status updates go to your contacts.",
		dialogButton{label: "Delete", danger: true, run: func() {
			cur := u.editableStatusPrivacy()
			if cur == nil || list >= len(cur.Lists) {
				return
			}
			p := *cur
			id := p.Lists[list].ID
			p.Lists = slices.Delete(slices.Clone(p.Lists), list, list+1)
			if p.Audience == model.AudienceList && p.ListID == id {
				p.Audience, p.ListID = model.AudienceContacts, ""
			}
			u.setStatusPrivacy(p)
			u.status.privacy.stack = []spStep{spMain}
		}})
}

// statusRowItems is the menu of a contact's status updates: Hide, or
// Unhide once hidden.
func (u *UI) statusRowItems(id string) []menuItem {
	for _, t := range u.statuses {
		if t.ID != id || t.Mine || t.Group {
			continue
		}
		if t.Muted {
			return []menuItem{{key: "st-unhide", ic: icVisibility, label: "Unhide", run: func() { u.backend.MuteStatus(id, false) }}}
		}
		name := t.Name
		return []menuItem{{key: "st-hide", ic: icVisibilityOff, label: "Hide", run: func() {
			u.confirm("Hide "+name+"'s status updates?",
				"New status updates from "+name+" won't show under Recent anymore. You'll find them under Hidden.",
				dialogButton{label: "Hide", primary: true, run: func() { u.backend.MuteStatus(id, true) }})
		}}}
	}
	return nil
}

// updateStatusPrivacy handles the panel's buttons.
func (u *UI) updateStatusPrivacy(gtx C) {
	sp := &u.status.privacy
	if !sp.open() {
		return
	}
	if u.btn("sp:back").Clicked(gtx) {
		u.statusPrivacyBack()
	}
	for {
		ev, ok := sp.name.Update(gtx)
		if !ok {
			break
		}
		if _, ok := ev.(widget.SubmitEvent); ok && sp.top() == spName {
			u.saveAudienceName()
		}
	}
	for {
		if _, ok := sp.search.Update(gtx); !ok {
			break
		}
	}
	switch sp.top() {
	case spMain:
		if u.btn("sp:new").Clicked(gtx) {
			u.openAudienceName(-1)
		}
		for _, k := range []struct {
			key  string
			kind model.StatusAudience
		}{{"contacts", model.AudienceContacts}, {"except", model.AudienceExcept}, {"only", model.AudienceOnly}, {"close", model.AudienceClose}} {
			if u.btn("sp:" + k.key).Clicked(gtx) {
				u.chooseAudience(k.kind, -1)
			}
			if u.btn("sp:" + k.key + ":edit").Clicked(gtx) {
				u.openAudiencePeople(k.kind, -1, false)
			}
		}
		if p := sp.p; p != nil {
			for i := range p.Lists {
				if u.btn("sp:list:" + itoa(i)).Clicked(gtx) {
					u.chooseAudience(model.AudienceList, i)
				}
				if u.btn("sp:list:" + itoa(i) + ":edit").Clicked(gtx) {
					u.openAudiencePeople(model.AudienceList, i, false)
				}
			}
		}
	case spPeople:
		if u.btn("sp:done").Clicked(gtx) {
			u.saveAudiencePeople()
		}
		if u.btn("sp:listmenu").Clicked(gtx) {
			u.ctx = ctxMenu{kind: ctxStatusList, at: u.mouse}
		}
		for _, c := range u.audienceRows() {
			if u.btn("sp:p:" + c.ID).Clicked(gtx) {
				if i := slices.Index(sp.picked, c.ID); i >= 0 {
					sp.picked = slices.Delete(sp.picked, i, i+1)
				} else {
					sp.picked = append(sp.picked, c.ID)
				}
			}
		}
	case spName:
		if u.btn("sp:emoji").Clicked(gtx) || u.btn("sp:emojilink").Clicked(gtx) {
			u.openPicker(pickAudience, nil)
		}
		if u.btn("sp:save").Clicked(gtx) {
			u.saveAudienceName()
		}
	}
}

// audienceRows are the people page's rows: the people picked who aren't
// among your contacts (someone you no longer have saved), then your
// contacts, those matching the search.
func (u *UI) audienceRows() []*model.Contact {
	sp := &u.status.privacy
	q := strings.ToLower(trimSpace(sp.search.Text()))
	digits := phoneDigits(q)
	out := sp.shown[:0]
	match := func(c *model.Contact) bool {
		return q == "" || strings.Contains(strings.ToLower(c.Name), q) ||
			digits != "" && strings.Contains(phoneDigits(c.Phone), digits)
	}
	for _, id := range sp.picked {
		if slices.ContainsFunc(sp.contacts, func(c *model.Contact) bool { return c.ID == id }) {
			continue
		}
		c := &model.Contact{ID: id, Name: u.contactName(id)}
		if match(c) {
			out = append(out, c)
		}
	}
	for _, c := range sp.contacts {
		if match(c) {
			out = append(out, c)
		}
	}
	sp.shown = out
	return out
}

// contactName names someone by their chat, or by their number.
func (u *UI) contactName(id string) string {
	if c := u.chatByID(id); c != nil && c.Name != "" {
		return c.Name
	}
	if i := strings.IndexByte(id, '@'); i > 0 {
		return "+" + id[:i]
	}
	return id
}

// layoutStatusPrivacy draws the panel's pages over the Status list,
// sliding in from the left.
func (u *UI) layoutStatusPrivacy(gtx C) {
	sp := &u.status.privacy
	p := u.pal
	sz := gtx.Constraints.Max
	var vs [spName + 1]float32
	shown := false
	for s := spMain; s <= spName; s++ {
		vs[s] = sp.in[s].step(gtx, slices.Contains(sp.stack, s), durPanel)
		shown = shown || vs[s] > 0
	}
	if !shown {
		if sp.contacts != nil {
			sp.contacts, sp.shown = nil, nil // done sliding out
		}
		return
	}
	// The pages in the stack, bottom first, then those sliding out (they
	// were on top). Pages under one fully in aren't drawn.
	order := slices.Clone(sp.stack)
	for s := spMain; s <= spName; s++ {
		if vs[s] > 0 && !slices.Contains(order, s) {
			order = append(order, s)
		}
	}
	from := 0
	for i, s := range order {
		if vs[s] == 1 {
			from = i
		}
	}
	for _, s := range order[from:] {
		if vs[s] == 0 {
			continue
		}
		e := easeOut(vs[s])
		x := -int(float32(sz.X) * (1 - e))
		t := op.Offset(image.Pt(x, 0)).Push(gtx.Ops)
		pg := gtx
		pg.Constraints = layout.Exact(sz)
		done := func() {}
		if s != sp.top() {
			pg, done = fadeOut(pg)
		} else {
			// Take the clicks the page doesn't use, so they don't reach
			// the status list underneath.
			u.btn("sp:panel").Layout(pg, func(gtx C) D { return D{Size: sz} })
		}
		cl := clip.Rect{Max: sz}.Push(gtx.Ops)
		fillRect(gtx, image.Rectangle{Max: sz}, p.Panel)
		switch s {
		case spMain:
			u.layoutAudiencesPage(pg)
		case spPeople:
			u.layoutPeoplePage(pg)
		case spName:
			u.layoutAudienceNamePage(pg)
		}
		cl.Pop()
		done()
		t.Pop()
	}
}

// statusPrivacyCovers reports whether a page of the panel hides the
// Status list.
func (u *UI) statusPrivacyCovers() bool {
	sp := &u.status.privacy
	for _, s := range sp.stack {
		if sp.in[s].v == 1 {
			return true
		}
	}
	return false
}

// spHeader is a page's title row: a back arrow, the title and maybe a ⋮.
func (u *UI) spHeader(gtx C, title string, menu bool) D {
	p := u.pal
	return vcenter(gtx, gtx.Dp(68), func(gtx C) D {
		return layout.Inset{Left: 10, Right: 16}.Layout(gtx, func(gtx C) D {
			return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(func(gtx C) D { return u.iconButton(gtx, u.btn("sp:back"), icBack, 40, 24, p.Icon) }),
				layout.Rigid(layout.Spacer{Width: 10}.Layout),
				layout.Flexed(1, u.label(19, title, p.Text, labelOpts{weight: font.SemiBold, maxLines: 1}).Layout),
				layout.Rigid(func(gtx C) D {
					if !menu {
						return D{}
					}
					return u.iconButton(gtx, u.btn("sp:listmenu"), icMenu, 40, 24, p.IconStrong)
				}),
			)
		})
	})
}

// audienceTitle names an audience, as its page's title.
func audienceTitle(p *model.StatusPrivacy, kind model.StatusAudience, list int) string {
	switch kind {
	case model.AudienceExcept:
		return "My contacts except…"
	case model.AudienceOnly:
		return "Only share with…"
	case model.AudienceClose:
		return "Close friends"
	case model.AudienceList:
		if p != nil && list >= 0 && list < len(p.Lists) {
			return p.Lists[list].Name
		}
		return "New audience"
	}
	return "My contacts"
}

// layoutAudiencesPage is Status privacy: the audiences, one chosen, and a
// New button for an audience of your own.
func (u *UI) layoutAudiencesPage(gtx C) D {
	sp := &u.status.privacy
	pal := u.pal
	p := sp.p
	known := p != nil && p.Editable
	on := func(kind model.StatusAudience, id string) bool {
		return p != nil && p.Audience == kind && (kind != model.AudienceList || p.ListID == id)
	}
	count := func(n int, one, many string) string {
		if n == 1 {
			return "1 " + one
		}
		return itoa(n) + " " + many
	}
	edit := func(s string) string { return s + " · Edit ›" }
	const unsynced = "Not synced yet · Edit ›"
	var rows []layout.Widget
	if !known {
		rows = append(rows, func(gtx C) D {
			return u.ncNote(gtx, statusPrivacyText(p))
		})
	}
	rows = append(rows, func(gtx C) D { return u.audiencesHeading(gtx, known) })
	people := func(kind model.StatusAudience) int {
		if p == nil {
			return 0
		}
		if !known && p.Audience == kind {
			return p.Count
		}
		return len(audiencePeople(p, kind, -1))
	}
	sub := func(kind model.StatusAudience, one, many string) string {
		if !known {
			if p != nil && p.Audience == kind {
				return count(people(kind), one, many)
			}
			return ""
		}
		if !p.Knows(kind) {
			return unsynced
		}
		return edit(count(people(kind), one, many))
	}
	rows = append(rows,
		func(gtx C) D {
			return u.audienceRow(gtx, "contacts", u.audienceIcon(icContact), "My contacts", "", on(model.AudienceContacts, ""))
		},
		func(gtx C) D {
			return u.audienceRow(gtx, "except", u.audienceIcon(icPersonRemove), "My contacts except…",
				sub(model.AudienceExcept, "excluded", "excluded"), on(model.AudienceExcept, ""))
		},
		func(gtx C) D {
			return u.audienceRow(gtx, "only", u.audienceIcon(icPersonCheck), "Only share with…",
				sub(model.AudienceOnly, "included", "included"), on(model.AudienceOnly, ""))
		},
	)
	if known {
		closeSub := "Include people ›"
		switch n := len(p.Close); {
		case !p.Knows(model.AudienceClose):
			closeSub = unsynced
		case n > 0:
			closeSub = edit(count(n, "person", "people"))
		}
		rows = append(rows, func(gtx C) D {
			return u.audienceRow(gtx, "close", u.audienceEmoji("💜"), "Close friends", closeSub, on(model.AudienceClose, ""))
		})
		for i, l := range p.Lists {
			rows = append(rows, func(gtx C) D {
				return u.audienceRow(gtx, "list:"+itoa(i), u.audienceEmoji(l.Emoji), l.Name,
					edit(count(len(l.Members), "person", "people")), on(model.AudienceList, l.ID))
			})
		}
	}
	rows = append(rows, func(gtx C) D {
		return layout.Inset{Left: 29.5, Right: 29, Top: 14, Bottom: 24}.Layout(gtx, func(gtx C) D {
			l := u.label(14, "Changes to your privacy settings won't affect status updates you've already shared. "+
				"Your audiences sync with WhatsApp on your phone.", pal.TextSecondary)
			l.MaxLines = 0
			return l.Layout(gtx)
		})
	})
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx C) D { return u.spHeader(gtx, "Status privacy", false) }),
		layout.Flexed(1, func(gtx C) D {
			return u.scrollList(gtx, &sp.mainList, len(rows), func(gtx C, i int) D { return rows[i](gtx) })
		}),
	)
}

// audiencesHeading is "Audience who can see my status" and, once the
// setting can be changed, the New button.
func (u *UI) audiencesHeading(gtx C, known bool) D {
	p := u.pal
	return layout.Inset{Left: 29.5, Right: 22, Top: 4, Bottom: 8}.Layout(gtx, func(gtx C) D {
		return vcenter(gtx, gtx.Dp(44), func(gtx C) D {
			gtx.Constraints.Min.X = gtx.Constraints.Max.X
			return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
				layout.Flexed(1, u.label(15.2, "Audience who can see my status", p.TextSecondary, labelOpts{maxLines: 1}).Layout),
				layout.Rigid(func(gtx C) D {
					if !known {
						return D{}
					}
					c := u.btn("sp:new")
					return clickable(gtx, c, func(gtx C) D {
						lbl := record(gtx, u.label(15, "New", p.Text, labelOpts{weight: font.Medium, maxLines: 1}).Layout)
						h, is := gtx.Dp(36), gtx.Dp(20)
						w := gtx.Dp(14) + is + gtx.Dp(6) + lbl.size.X + gtx.Dp(16)
						borderRRect(gtx, image.Rect(0, 0, w, h), h/2, mix(p.Chip, p.Hover, u.hover(gtx, c)), p.ChipBorder)
						t := op.Offset(image.Pt(gtx.Dp(14), (h-is)/2)).Push(gtx.Ops)
						drawIcon(gtx, icAdd, 20, p.Text)
						t.Pop()
						lbl.at(gtx, gtx.Dp(14)+is+gtx.Dp(6), (h-lbl.size.Y)/2)
						return D{Size: image.Pt(w, h)}
					})
				}),
			)
		})
	})
}

// audienceBg is the gray circle behind an audience's picture.
func (u *UI) audienceBg() color.NRGBA { return mix(u.pal.Panel, u.pal.Text, 0.1) }

// audienceIcon is an audience's picture: an icon on a gray circle.
func (u *UI) audienceIcon(ic *icon.Icon) layout.Widget {
	return func(gtx C) D {
		px := gtx.Dp(48)
		fillCircle(gtx, image.Pt(px/2, px/2), px/2, u.audienceBg())
		return centerIn(gtx, px, iconW(ic, 25, u.pal.Text))
	}
}

// audienceEmoji is an audience's picture: its emoji on a gray circle.
func (u *UI) audienceEmoji(emoji string) layout.Widget {
	return func(gtx C) D {
		px := gtx.Dp(48)
		fillCircle(gtx, image.Pt(px/2, px/2), px/2, u.audienceBg())
		return centerIn(gtx, px, u.label(22, emoji, u.pal.Text).Layout)
	}
}

// audienceRow is an audience: its picture, name and (green) its people
// with an Edit link, and a radio button. A click on the row chooses it,
// one on the green line opens its people.
func (u *UI) audienceRow(gtx C, key string, pic layout.Widget, title, sub string, on bool) D {
	p := u.pal
	c, ec := u.btn("sp:"+key), u.btn("sp:"+key+":edit")
	w, h := gtx.Constraints.Max.X, gtx.Dp(76)
	inner := image.Rect(gtx.Dp(13), 0, w-gtx.Dp(18), h)
	var subRect image.Rectangle
	rg := gtx
	rg.Constraints = layout.Exact(image.Pt(w, h))
	clickable(rg, c, func(gtx C) D {
		if v := u.hover(gtx, c); v > 0 {
			fillRRect(gtx, inner, gtx.Dp(10), mix(p.Panel, p.Hover, v))
		}
		ps := gtx.Dp(48)
		x := inner.Min.X + gtx.Dp(11)
		t := op.Offset(image.Pt(x, (h-ps)/2)).Push(gtx.Ops)
		pic(gtx)
		t.Pop()
		tx := x + ps + gtx.Dp(15)
		rs := gtx.Dp(20)
		rx := inner.Max.X - gtx.Dp(14) - rs
		tg := gtx
		tg.Constraints = layout.Constraints{Max: image.Pt(max(0, rx-gtx.Dp(12)-tx), h)}
		tl := record(tg, u.label(17, title, p.Text, labelOpts{maxLines: 1}).Layout)
		var sl part
		gap := 0
		if sub != "" {
			col := mix(p.Green, p.Text, 0.2*u.hover(gtx, ec))
			sl = record(tg, u.label(15, sub, col, labelOpts{weight: font.Medium, maxLines: 1}).Layout)
			gap = gtx.Dp(3)
		}
		y := (h - tl.size.Y - gap - sl.size.Y) / 2
		tl.at(gtx, tx, y)
		if sub != "" {
			subRect = image.Rectangle{Min: image.Pt(tx, y+tl.size.Y+gap)}
			subRect.Max = subRect.Min.Add(sl.size)
			sl.at(gtx, subRect.Min.X, subRect.Min.Y)
		}
		t = op.Offset(image.Pt(rx, (h-rs)/2)).Push(gtx.Ops)
		u.radioRing(gtx, on)
		t.Pop()
		return D{Size: image.Pt(w, h)}
	})
	// The green line is a button of its own, on top of the row's.
	if !subRect.Empty() {
		t := op.Offset(subRect.Min).Push(gtx.Ops)
		eg := gtx
		eg.Constraints = layout.Exact(subRect.Size())
		clickable(eg, ec, func(gtx C) D { return D{Size: gtx.Constraints.Min} })
		t.Pop()
	}
	return D{Size: image.Pt(w, h)}
}

// radioRing draws a radio button as a ring, so it shows on any background.
func (u *UI) radioRing(gtx C, on bool) D {
	p := u.pal
	sz := gtx.Dp(20)
	ring := p.TextSecondary
	if on {
		ring = p.Green
	}
	stroke := float32(max(1, gtx.Dp(2)))
	c := f32.Pt(float32(sz)/2, float32(sz)/2)
	strokeArc(gtx, c, float32(sz)/2-stroke/2, 0, 2*math.Pi, stroke, ring)
	if on {
		fillCircle(gtx, image.Pt(sz/2, sz/2), sz/2-gtx.Dp(5), p.Green)
	}
	return D{Size: image.Pt(sz, sz)}
}

// layoutPeoplePage is an audience's people: your contacts with a box each,
// those picked checked, a search above and a green check to save.
func (u *UI) layoutPeoplePage(gtx C) D {
	sp := &u.status.privacy
	p := u.pal
	rows := u.audienceRows()
	n := len(sp.picked)
	what := "selected"
	if sp.kind == model.AudienceExcept {
		what = "excluded"
	}
	title := audienceTitle(sp.p, sp.kind, sp.list)
	if sp.kind == model.AudienceList && sp.list < 0 {
		title = trimSpace(sp.name.Text())
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx C) D { return u.spHeader(gtx, title, sp.kind == model.AudienceList && sp.list >= 0) }),
		layout.Rigid(func(gtx C) D { return u.ncSearch(gtx, &sp.search, "Search name or number") }),
		layout.Rigid(func(gtx C) D {
			if sp.p != nil && sp.kind != model.AudienceList && !sp.p.Knows(sp.kind) {
				// The phone named only its chosen audience's people.
				return layout.Inset{Left: 32, Right: 29, Top: 2, Bottom: 8}.Layout(gtx, func(gtx C) D {
					l := u.label(14, "Your phone hasn't synced these people here yet. "+
						"The people you choose here replace them on your phone when you share with this audience.", p.TextSecondary)
					l.MaxLines = 0
					return l.Layout(gtx)
				})
			}
			return u.sectionLabel(gtx, itoa(n)+" "+what, layout.Inset{Left: 32, Right: 29, Top: 6, Bottom: 6}, labelOpts{maxLines: 1})
		}),
		layout.Flexed(1, func(gtx C) D {
			if len(rows) == 0 {
				msg := "Your contacts on WhatsApp show here once your phone has synced them."
				if q := trimSpace(sp.search.Text()); q != "" {
					msg = "No results found for '" + q + "'"
				}
				return u.ncNote(gtx, msg)
			}
			return u.scrollList(gtx, &sp.peopleList, len(rows), func(gtx C, i int) D {
				c := rows[i]
				picked := slices.Contains(sp.picked, c.ID)
				return u.ncRow(gtx, u.btn("sp:p:"+c.ID), func(gtx C) D {
					return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
						layout.Rigid(func(gtx C) D {
							box, col := icCheckBoxEmpty, p.TextSecondary
							if picked {
								box, col = icCheckBox, p.Green
							}
							return drawIcon(gtx, box, 24, col)
						}),
						layout.Rigid(layout.Spacer{Width: 16}.Layout),
						layout.Rigid(u.contactPic(c)),
					)
				}, c.Name, c.Phone)
			})
		}),
		layout.Rigid(func(gtx C) D {
			on := !sp.choose || n > 0
			var in tween
			in.snap(true)
			return u.ncButton(gtx, "sp:done", &in, icTick, true, !on, p.Panel)
		}),
	)
}

// layoutAudienceNamePage is an audience's name and emoji, with a preview
// of what the people in it see on your status updates.
func (u *UI) layoutAudienceNamePage(gtx C) D {
	sp := &u.status.privacy
	p := u.pal
	title, button := "Edit name & emoji", "Save changes"
	if sp.list < 0 {
		title, button = "New audience", "Next"
	}
	name := trimSpace(sp.name.Text())
	rows := []layout.Widget{
		// The emoji, big, and Change emoji under it.
		func(gtx C) D {
			return layout.Inset{Top: 12, Bottom: 8}.Layout(gtx, func(gtx C) D {
				gtx.Constraints.Min.X = gtx.Constraints.Max.X
				return layout.Flex{Axis: layout.Vertical, Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(func(gtx C) D {
						c := u.btn("sp:emoji")
						return clickable(gtx, c, func(gtx C) D {
							px := gtx.Dp(96)
							fillCircle(gtx, image.Pt(px/2, px/2), px/2, mix(u.audienceBg(), p.Hover, u.hover(gtx, c)))
							return centerIn(gtx, px, u.label(44, sp.emoji, p.Text).Layout)
						})
					}),
					layout.Rigid(layout.Spacer{Height: 12}.Layout),
					layout.Rigid(func(gtx C) D {
						gtx.Constraints.Min = image.Point{} // shrink-wrapped, to be centered
						c := u.btn("sp:emojilink")
						return clickable(gtx, c, func(gtx C) D {
							col := mix(p.Green, p.Text, 0.2*u.hover(gtx, c))
							return u.label(16, "Change emoji", col, labelOpts{weight: font.Medium}).Layout(gtx)
						})
					}),
				)
			})
		},
		// The name, in an outlined field with its label on the line.
		func(gtx C) D {
			return layout.Inset{Left: 29, Right: 29, Top: 16, Bottom: 8}.Layout(gtx, func(gtx C) D {
				return u.outlinedField(gtx, &sp.name, "Audience name")
			})
		},
		func(gtx C) D {
			return layout.Inset{Left: 29, Right: 29, Top: 18}.Layout(gtx, func(gtx C) D {
				return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
					layout.Rigid(u.label(16, "Preview", p.Text, labelOpts{weight: font.Medium}).Layout),
					layout.Rigid(layout.Spacer{Height: 4}.Layout),
					layout.Rigid(func(gtx C) D {
						l := u.label(14.5, "When this audience views your status, they can see the name and emoji you choose.", p.TextSecondary)
						l.MaxLines = 0
						return l.Layout(gtx)
					}),
				)
			})
		},
		func(gtx C) D {
			return layout.Inset{Left: 29, Right: 29, Top: 16, Bottom: 16}.Layout(gtx, func(gtx C) D {
				return u.audiencePreview(gtx, name, sp.emoji)
			})
		},
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx C) D { return u.spHeader(gtx, title, false) }),
		layout.Flexed(1, func(gtx C) D {
			return u.scrollList(gtx, &sp.nameList, len(rows), func(gtx C, i int) D { return rows[i](gtx) })
		}),
		layout.Rigid(func(gtx C) D {
			return layout.Inset{Left: 29, Right: 29, Top: 8, Bottom: 20}.Layout(gtx, func(gtx C) D {
				return u.wideButton(gtx, "sp:save", button, name == "")
			})
		}),
	)
}

// audiencePreview is the card that shows how your status names the
// audience: a progress bar, your picture and name, the audience's name
// and its emoji.
func (u *UI) audiencePreview(gtx C, name, emoji string) D {
	p := u.pal
	w := gtx.Constraints.Max.X
	h := gtx.Dp(132)
	r := image.Rect(0, 0, w, h)
	borderRRect(gtx, r, gtx.Dp(16), mix(p.Panel, p.Text, 0.04), p.ChipBorder)
	pad := gtx.Dp(18)
	bar := image.Rect(pad, gtx.Dp(28), w-pad, gtx.Dp(28)+max(2, gtx.Dp(3)))
	fillRRect(gtx, bar, bar.Dy()/2, p.ChipBorder)
	fillRRect(gtx, image.Rect(bar.Min.X, bar.Min.Y, bar.Min.X+bar.Dx()*2/5, bar.Max.Y), bar.Dy()/2, p.TextSecondary)
	if name == "" {
		name = "Audience name"
	}
	row := op.Offset(image.Pt(pad, gtx.Dp(52))).Push(gtx.Ops)
	rg := gtx
	rg.Constraints = layout.Constraints{Max: image.Pt(w-2*pad, gtx.Dp(60))}
	layout.Flex{Alignment: layout.Middle}.Layout(rg,
		layout.Rigid(func(gtx C) D { return u.avatar(gtx, u.meID, u.meName(), false, 56) }),
		layout.Rigid(layout.Spacer{Width: 14}.Layout),
		layout.Flexed(1, func(gtx C) D {
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
				layout.Rigid(u.label(17, u.meName(), p.Text, labelOpts{weight: font.Medium, maxLines: 1}).Layout),
				layout.Rigid(layout.Spacer{Height: 2}.Layout),
				layout.Rigid(u.label(15, name+" ›", p.Text, labelOpts{maxLines: 1}).Layout),
			)
		}),
		layout.Rigid(func(gtx C) D {
			ew := record(gtx, u.label(18, emoji, p.Text).Layout)
			ph := gtx.Dp(40)
			pw := gtx.Dp(14) + ew.size.X + gtx.Dp(6) + gtx.Dp(18) + gtx.Dp(10)
			fillRRect(gtx, image.Rect(0, 0, pw, ph), ph/2, p.Panel)
			ew.at(gtx, gtx.Dp(14), (ph-ew.size.Y)/2)
			t := op.Offset(image.Pt(gtx.Dp(14)+ew.size.X+gtx.Dp(6), (ph-gtx.Dp(18))/2)).Push(gtx.Ops)
			drawIcon(gtx, icChevron, 18, p.TextSecondary)
			t.Pop()
			return D{Size: image.Pt(pw, ph)}
		}),
	)
	row.Pop()
	return D{Size: image.Pt(w, h)}
}

// outlinedField is an editor in a rounded outline with its label on the
// outline's top edge, green while focused.
func (u *UI) outlinedField(gtx C, ed *widget.Editor, label string) D {
	p := u.pal
	w := gtx.Constraints.Max.X
	focused := gtx.Focused(ed)
	col := p.TextSecondary
	if focused {
		col = p.Green
	}
	lbl := record(gtx, u.label(13.5, label, col, labelOpts{maxLines: 1}).Layout)
	top := lbl.size.Y / 2
	h := gtx.Dp(56)
	box := image.Rect(0, top, w, top+h)
	line := max(1, gtx.Dp(1))
	if focused {
		line = max(2, gtx.Dp(2))
	}
	borderRRect(gtx, box, gtx.Dp(10), col, col)
	fillRRect(gtx, box.Inset(line), gtx.Dp(10)-line, p.Panel)
	// The label sits in a gap of the outline.
	lx := gtx.Dp(16)
	fillRect(gtx, image.Rect(lx-gtx.Dp(4), top-line, lx+lbl.size.X+gtx.Dp(4), top+line), p.Panel)
	lbl.at(gtx, lx, 0)
	t := op.Offset(image.Pt(gtx.Dp(18), top)).Push(gtx.Ops)
	eg := gtx
	eg.Constraints = layout.Exact(image.Pt(w-gtx.Dp(36), h))
	vcenter(eg, h, func(gtx C) D {
		gtx.Constraints.Min.X = gtx.Constraints.Max.X
		e := material.Editor(u.th, ed, "")
		e.TextSize = 17
		e.Color = p.Text
		return e.Layout(gtx)
	})
	t.Pop()
	return D{Size: image.Pt(w, top+h)}
}

// wideButton is a green pill as wide as the page, grayed while off.
func (u *UI) wideButton(gtx C, key, txt string, off bool) D {
	p := u.pal
	c := u.btn(key)
	w, h := gtx.Constraints.Max.X, gtx.Dp(48)
	bg, fg := mix(p.Green, p.Text, 0.08*u.hover(gtx, c)), p.OnGreen
	if off {
		bg, fg = p.Chip, p.TextSecondary
	}
	bgtx := gtx
	bgtx.Constraints = layout.Exact(image.Pt(w, h))
	return clickable(bgtx, c, func(gtx C) D {
		fillRRect(gtx, image.Rect(0, 0, w, h), h/2, bg)
		gtx.Constraints.Min = image.Point{}
		lbl := record(gtx, u.label(16, txt, fg, labelOpts{weight: font.Medium, maxLines: 1, align: text.Middle}).Layout)
		lbl.at(gtx, (w-lbl.size.X)/2, (h-lbl.size.Y)/2)
		return D{Size: image.Pt(w, h)}
	})
}

// statusPrivacyText describes the setting before it can be changed here.
func statusPrivacyText(p *model.StatusPrivacy) string {
	const sync = " Your audiences show here once your phone syncs them; keep WhatsApp open on your phone."
	people := func(n int) string {
		if n == 1 {
			return "1 contact"
		}
		return itoa(n) + " contacts"
	}
	switch {
	case p == nil:
		return "Checking who can see your status updates…"
	case p.Audience == model.AudienceExcept:
		return "Your status updates are shared with your contacts, except " + people(p.Count) + "." + sync
	case p.Audience == model.AudienceOnly:
		return "Your status updates are shared only with " + people(p.Count) + "." + sync
	}
	return "Your status updates are shared with all your contacts." + sync
}
