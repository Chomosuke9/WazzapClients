package ui

import (
	"encoding/json"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"gioui.org/layout"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

// Custom mentions, an extra feature with one switch for them and @admin:
// "@Tim frontend" mentions people you picked in a group. They go like @admin:
// the text says "@<group JID>", a
// GroupMention gives that the name, and the people are the mentioned JIDs
// (see draftWith). Each group has its own, in a preference of this account.

const (
	// prefCustomMentionsOf, then a group's ID, keeps its custom mentions as
	// JSON.
	prefCustomMentionsOf = "custom_mentions:"
	// mentionCustomPrefix, then a custom mention's ID, is its row's ID in
	// the mention picker.
	mentionCustomPrefix = mentionCustomID + ":"
	// maxCustomMentionName is as long as the mention picker can look back
	// from the caret for the "@" (mentionQuery).
	maxCustomMentionName = 30
)

// customMention is a name for people of a group.
type customMention struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Members []string `json:"members"`
}

// customMentionsOf returns a group's custom mentions, read once.
func (u *UI) customMentionsOf(chat string) []customMention {
	if l, ok := u.customMentions[chat]; ok {
		return l
	}
	var l []customMention
	if s := u.backend.Pref(prefCustomMentionsOf + chat); s != "" {
		_ = json.Unmarshal([]byte(s), &l) // unreadable counts as none
	}
	if u.customMentions == nil {
		u.customMentions = map[string][]customMention{}
	}
	u.customMentions[chat] = l
	return l
}

// setCustomMentions replaces a group's custom mentions and keeps them.
func (u *UI) setCustomMentions(chat string, l []customMention) {
	u.customMentionsOf(chat)
	u.customMentions[chat] = l
	u.customMentionsVer++
	s := ""
	if len(l) > 0 {
		b, err := json.Marshal(l)
		if err != nil {
			return
		}
		s = string(b)
	}
	u.backend.SetPref(prefCustomMentionsOf+chat, s)
}

// customMentionByID finds one of a group's custom mentions, or nil.
func (u *UI) customMentionByID(chat, id string) *customMention {
	l := u.customMentionsOf(chat)
	if i := slices.IndexFunc(l, func(cm customMention) bool { return cm.ID == id }); i >= 0 {
		return &l[i]
	}
	return nil
}

// customMentionPeople returns the people of ids still in the group, without
// you: the rest can't be notified there.
func (u *UI) customMentionPeople(chat string, ids []string) []string {
	info := u.chatMembers(chat)
	if info == nil {
		return ids
	}
	var out []string
	for _, id := range ids {
		if slices.ContainsFunc(info.Members, func(m model.Member) bool { return m.ID == id && !m.Me }) {
			out = append(out, id)
		}
	}
	return out
}

// customMentionWho names people of the group: "Budi, Sari and 2 others".
func customMentionWho(info *model.ChatInfo, ids []string) string {
	var names []string
	for _, m := range info.Members {
		if !m.Me && slices.Contains(ids, m.ID) {
			names = append(names, strings.TrimPrefix(memberTitle(m), "~"))
		}
	}
	const shown = 3
	switch n := len(names); {
	case n == 0:
		return "No one in this group"
	case n == 1:
		return names[0]
	case n <= shown:
		return strings.Join(names[:n-1], ", ") + " and " + names[n-1]
	case n == shown+1:
		return strings.Join(names[:shown], ", ") + " and 1 other"
	default:
		return strings.Join(names[:shown], ", ") + " and " + strconv.Itoa(n-shown) + " others"
	}
}

// customMentionHits lists the group's custom mentions that match query and
// have someone to mention, as the mention picker's rows. Phone holds who's
// in one, which the row shows through memberSub.
func (u *UI) customMentionHits(info *model.ChatInfo, query string) []model.Member {
	var out []model.Member
	for _, cm := range u.customMentionsOf(u.conv.membersFor) {
		if query != "" && personScore(query, "", cm.Name) == 0 {
			continue
		}
		if len(u.customMentionPeople(u.conv.membersFor, cm.Members)) == 0 {
			continue
		}
		out = append(out, model.Member{ID: mentionCustomPrefix + cm.ID, Name: cm.Name, Phone: customMentionWho(info, cm.Members)})
	}
	return out
}

// customMentionCount says how many a group has.
func customMentionCount(l []customMention) string {
	switch len(l) {
	case 0:
		return "None yet"
	case 1:
		return "1 mention"
	}
	return strconv.Itoa(len(l)) + " mentions"
}

// customMentionProblem says what's wrong with a name for a custom mention
// (a new one, or the one with the ID editing), or "" when it can be used.
func (u *UI) customMentionProblem(chat, name, editing string) string {
	name = trimSpace(name)
	switch {
	case strings.ContainsAny(name, "@,"):
		return "A name can't have @ or a comma in it."
	case utf8.RuneCountInString(name) > maxCustomMentionName:
		return "A name can be at most " + strconv.Itoa(maxCustomMentionName) + " characters long."
	case strings.EqualFold(name, "all"), strings.EqualFold(name, "admin"):
		return "@" + strings.ToLower(name) + " is taken."
	}
	for _, cm := range u.customMentionsOf(chat) {
		if cm.ID != editing && strings.EqualFold(cm.Name, name) {
			return "This group already has a custom mention with this name."
		}
	}
	return ""
}

// openCustomMention opens the editor of a custom mention of chat: cm, or a
// new one when nil.
func (u *UI) openCustomMention(chat string, cm *customMention) {
	u.dialog = dialogState{kind: dialogForward, mentionFor: chat, read: true}
	d := &u.dialog
	d.search.SingleLine = true
	d.listName.SingleLine = true
	d.list.Axis = layout.Vertical
	if cm != nil {
		d.mentionEdit = cm.ID
		d.listName.SetText(cm.Name)
		d.picked = u.customMentionPeople(chat, cm.Members)
	}
	u.requestFocus(&d.listName)
}

// customMentionRows lists the group's members that match the search, without
// you: a mention of you would only notify others.
func (u *UI) customMentionRows(d *dialogState) []fwdRow {
	q := trimSpace(d.search.Text())
	rows := d.rows[:0] // reused every frame
	if info := u.chatMembers(d.mentionFor); info != nil {
		for _, m := range info.Members {
			if m.Me || (q != "" && memberScore(m, q) == 0) {
				continue
			}
			rows = append(rows, fwdRow{id: m.ID, name: memberTitle(m), about: memberSub(m)})
		}
	}
	d.rows = rows
	return rows
}

// customMentionMember names a member in the editor's bar.
func (u *UI) customMentionMember(chat, id string) string {
	if info := u.chatMembers(chat); info != nil {
		for _, m := range info.Members {
			if m.ID == id {
				return memberTitle(m)
			}
		}
	}
	return ""
}

// saveCustomMention keeps what the editor holds and closes it. Without a
// usable name or anyone picked, it stays open.
func (u *UI) saveCustomMention(d *dialogState) {
	name := trimSpace(d.listName.Text())
	if name == "" || len(d.picked) == 0 || u.customMentionProblem(d.mentionFor, name, d.mentionEdit) != "" {
		return
	}
	l := slices.Clone(u.customMentionsOf(d.mentionFor))
	picked := slices.Clone(d.picked)
	if i := slices.IndexFunc(l, func(cm customMention) bool { return cm.ID == d.mentionEdit }); i >= 0 && d.mentionEdit != "" {
		l[i].Name, l[i].Members = name, picked
	} else {
		id := 1
		for _, cm := range l {
			if n, err := strconv.Atoi(cm.ID); err == nil && n >= id {
				id = n + 1
			}
		}
		l = append(l, customMention{ID: strconv.Itoa(id), Name: name, Members: picked})
	}
	u.setCustomMentions(d.mentionFor, l)
	u.closeDialog()
}

// confirmDeleteCustomMention asks before deleting a custom mention. Messages
// that used it stay as they were sent.
func (u *UI) confirmDeleteCustomMention(chat, id string) {
	cm := u.customMentionByID(chat, id)
	if cm == nil {
		return
	}
	u.confirm("Delete @"+cm.Name+"?", "Messages that used it stay as they are.",
		dialogButton{label: "Delete", primary: true, danger: true, run: func() {
			u.setCustomMentions(chat, slices.DeleteFunc(slices.Clone(u.customMentionsOf(chat)),
				func(cm customMention) bool { return cm.ID == id }))
		}})
}

// customMentionPage is the info panel's page of a group's custom mentions.
func (u *UI) customMentionPage(gtx C, c *model.Chat, info *model.ChatInfo) []layout.Widget {
	p := u.pal
	id := c.ID
	if u.btn("info:mention:new").Clicked(gtx) {
		u.openCustomMention(id, nil)
	}
	rows := []layout.Widget{u.greenAction("mention:new", icAdd, "New custom mention", false)}
	for _, cm := range u.customMentionsOf(id) {
		rows = append(rows, u.infoRow("mention:"+cm.ID, listItem{ic: icGroups, title: "@" + cm.Name, sub: customMentionWho(info, cm.Members)}, func() {
			u.openCustomMention(id, u.customMentionByID(id, cm.ID))
		}))
	}
	return append(rows, func(gtx C) D {
		return layout.Inset{Left: infoPadX, Right: 24, Top: 16, Bottom: 24}.Layout(gtx, func(gtx C) D {
			l := u.label(14, "Type @ and a name in this group to mention its people. Everyone sees the name, and only the people in it are notified. These names are kept on this computer.", p.TextSecondary)
			l.MaxLines = 0
			return l.Layout(gtx)
		})
	})
}

// demoCustomMentions gives the open group two custom mentions, for
// cmd/screenshot.
func (u *UI) demoCustomMentions() {
	u.groupMentions = true
	chat := u.selected.ID
	var ids []string
	if info := u.chatMembers(chat); info != nil {
		for _, m := range info.Members {
			if !m.Me {
				ids = append(ids, m.ID)
			}
		}
	}
	u.setCustomMentions(chat, []customMention{
		{ID: "1", Name: "Tim frontend", Members: ids[:min(2, len(ids))]},
		{ID: "2", Name: "Tim backend", Members: ids},
	})
}
