package ui

import (
	"image"
	"strings"
	"time"

	"gioui.org/font"
	"gioui.org/layout"
	"gioui.org/op"

	"github.com/chomosuke9/wazzapclients/internal/auto"
	"github.com/chomosuke9/wazzapclients/internal/command"
	"github.com/chomosuke9/wazzapclients/internal/model"
)

// Scheduled messages (/schedule) show after a chat's newest message, as
// bubbles of yours with the time they go at; clicking one offers to send
// it now, edit it or cancel it. While you're AFK (/afk), a bar above the
// chat list says so and ends it. Both are kept by u.auto (package auto).

// jobsVersion is the scheduled messages' version, which the rows cache
// depends on.
func (u *UI) jobsVersion() int {
	if u.auto == nil {
		return 0
	}
	return u.auto.Version()
}

// scheduledChip heads a chat's scheduled messages.
const scheduledChip = "Scheduled messages"

// appendScheduled adds chat c's scheduled messages to its rows, under a
// chip of their own.
func (u *UI) appendScheduled(rows []convRow, c *model.Chat) []convRow {
	if u.auto == nil {
		return rows
	}
	jobs := u.auto.Jobs(c.ID)
	if len(jobs) == 0 {
		return rows
	}
	rows = append(rows, convRow{kind: rowDate, date: scheduledChip})
	for i := range jobs {
		rows = append(rows, convRow{kind: rowScheduled, job: &jobs[i], first: i == 0})
	}
	return rows
}

// layoutScheduled draws a scheduled message: your bubble, with a clock
// and when it goes instead of the time and ticks. The first has a tail.
func (u *UI) layoutScheduled(gtx C, j *auto.Job, tail bool, maxW int) D {
	p := u.pal
	key := "sched:" + j.ID
	cl := u.btn(key)
	if cl.Clicked(gtx) {
		u.openScheduledMenu(j)
	}
	inset := gtx.Dp(9)
	cgtx := gtx
	cgtx.Constraints = layout.Constraints{Max: image.Pt(maxW-2*inset, gtx.Constraints.Max.Y)}
	text := j.Shown
	if text == "" {
		text = j.Text
	}
	body := record(cgtx, u.label(15, text, p.TextOut, labelOpts{maxLines: 0}).Layout)
	meta := record(cgtx, func(gtx C) D {
		return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
			layout.Rigid(iconW(icClock, 14, p.MetaOut)),
			layout.Rigid(layout.Spacer{Width: 4}.Layout),
			layout.Rigid(u.label(12, scheduledAt(j, u), p.MetaOut, labelOpts{maxLines: 1}).Layout),
		)
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
		u.openScheduledMenu(j)
	}
	return D{Size: image.Pt(gtx.Constraints.Max.X, h)}
}

// scheduledAt says when a scheduled message goes: "Today at 21:00".
func scheduledAt(j *auto.Job, u *UI) string {
	now := u.now()
	s := command.Day(j.At, now) + " at " + j.At.In(now.Location()).Format("15:04")
	return strings.ToUpper(s[:1]) + s[1:]
}

func (u *UI) openScheduledMenu(j *auto.Job) {
	u.ctx = ctxMenu{kind: ctxScheduled, chatID: j.Chat, job: j.ID, at: u.mouse}
}

// scheduledJob returns the scheduled message with id, or nil once it's gone.
func (u *UI) scheduledJob(id string) *auto.Job {
	if u.auto == nil {
		return nil
	}
	for _, j := range u.auto.Jobs("") {
		if j.ID == id {
			return &j
		}
	}
	return nil
}

// scheduledMenuItems are a scheduled message's actions.
func (u *UI) scheduledMenuItems(id string) []menuItem {
	j := u.scheduledJob(id)
	if j == nil {
		return nil // sent meanwhile
	}
	text := j.Shown
	if text == "" {
		text = j.Text
	}
	return []menuItem{
		{key: "sendnow", ic: icSend, label: "Send now", run: func() {
			if m := u.auto.SendNow(id); m != nil {
				u.upsertMessage(m)
				u.scrollMessages(layout.Position{})
			}
		}},
		{key: "edit", ic: icEdit, label: "Edit", run: func() { u.editScheduled(*j) }},
		{key: "copy", ic: icCopy, label: "Copy", run: func() { u.copyText(text) }},
		{key: "cancel", ic: icDelete, label: "Cancel", col: u.pal.Danger, run: func() {
			if u.auto.Cancel(id) {
				u.toast("Scheduled message cancelled")
			}
		}},
	}
}

// editScheduled puts a scheduled message in the composer as the /schedule
// command that makes it, mentions and reply included. Running the command
// replaces it (see submitSlash); until then it stays scheduled.
func (u *UI) editScheduled(j auto.Job) {
	c := u.selected
	if c == nil || c.ID != j.Chat {
		return
	}
	text := j.Shown
	if text == "" {
		text = j.Text
	}
	var refs []mentionRef
	if j.MentionAll {
		refs = append(refs, mentionRef{name: "all", jid: mentionAllID})
	}
	if j.MentionAdmins {
		refs = append(refs, mentionRef{name: "admin", jid: mentionAdminID})
	}
	if info := u.chatMembers(c.ID); info != nil {
		for _, id := range j.Mentions {
			for _, m := range info.Members {
				if m.ID == id && strings.Contains(text, "@"+u.mentionName(m)) {
					refs = append(refs, mentionRef{name: u.mentionName(m), jid: id})
				}
			}
		}
	}
	u.conv.reply = nil
	if j.Reply != "" {
		if ms := u.backend.MessagesFrom(c.ID, j.Reply, 1); len(ms) > 0 && ms[0].ID == j.Reply {
			u.conv.reply = ms[0]
		}
	}
	ed := &u.conv.composer
	ed.SetText("/schedule " + scheduleArg(j.At, u.now()) + " " + text)
	ed.SetCaret(ed.Len(), ed.Len())
	u.conv.mentions = refs
	u.slash.replacing = j.ID
	u.requestFocus(ed)
}

// scheduleArg writes t as /schedule reads it: "today 21:00", "tomorrow
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
