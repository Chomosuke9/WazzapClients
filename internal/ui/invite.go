package ui

import (
	"image"
	"image/color"
	"net/url"
	"strings"

	"gioui.org/font"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/text"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

// Clicking a group's invite link (https://chat.whatsapp.com/<code>) opens a
// dialog that shows the group, like WhatsApp's: its picture, name, a few
// members and description, and a button to join it or ask its admins to.

// inviteState is the invite dialog's.
type inviteState struct {
	code    string
	group   *model.GroupPreview // nil while it loads, or when err is set
	err     string
	joining bool // waiting for the JoinedEvent
	more    bool // the whole description shows
	desc    widget.List
}

// The description shows about this much until "Read more" is clicked.
const (
	inviteDescRunes = 110
	inviteDescLines = 4
)

// openLink opens a link from a message: an invite link in the app, any
// other in the browser.
func (u *UI) openLink(link string) {
	if code := inviteCode(link); code != "" {
		u.openInvite(code)
		return
	}
	if !strings.Contains(link, "://") {
		link = "http://" + link // "www.…"
	}
	if !openURL(link) {
		u.toast("Couldn't open the link.")
	}
}

// inviteCode returns the code of a group invite link, or "".
func inviteCode(link string) string {
	p, err := url.Parse(link)
	if err != nil || (p.Scheme != "http" && p.Scheme != "https") || !strings.EqualFold(p.Host, "chat.whatsapp.com") {
		return ""
	}
	code := strings.TrimPrefix(strings.Trim(p.Path, "/"), "invite/")
	if code == "" {
		return ""
	}
	for _, r := range code {
		if !('a' <= r && r <= 'z' || 'A' <= r && r <= 'Z' || '0' <= r && r <= '9') {
			return ""
		}
	}
	return code
}

// openInvite opens the invite dialog and looks the group up.
func (u *UI) openInvite(code string) {
	u.dialog = dialogState{kind: dialogInvite, invite: inviteState{code: code}}
	u.dialog.invite.desc.Axis = layout.Vertical
	u.backend.GroupInvite(code)
}

// inviteLooked shows the group an InviteEvent brings, or opens it when
// you're in it already.
func (u *UI) inviteLooked(e model.InviteEvent) {
	d := &u.dialog
	if d.kind != dialogInvite || !d.isOpen() || d.invite.code != e.Code {
		return
	}
	if g := e.Group; g != nil && g.Member {
		u.closeDialog()
		if !u.openChatByID(g.ID) {
			u.toast("You're already in this group.")
		}
		return
	}
	d.invite.group, d.invite.err = e.Group, e.Err
}

// inviteJoined answers JoinGroup, even after the dialog was closed.
func (u *UI) inviteJoined(e model.JoinedEvent) {
	d := &u.dialog
	open := d.kind == dialogInvite && d.isOpen() && d.invite.code == e.Code
	if open {
		d.invite.joining = false
	}
	switch {
	case e.Err != "":
		u.toast(e.Err)
		return
	case e.Requested:
		u.toast("Request sent. You'll join once an admin approves it.")
	case !u.openChatByID(e.ChatID):
		u.toast("You joined the group.")
	}
	if open {
		u.closeDialog()
	}
}

// openChatByID opens a chat of the chat list on the Chats page and reports
// whether it is there.
func (u *UI) openChatByID(id string) bool {
	c := u.chatByID(id)
	if c == nil {
		return false
	}
	u.setPage(pageChats)
	u.sidebar.showArchived = c.Archived
	u.open(c)
	return true
}

// invitePanel is the invite dialog: the group, or what's wrong with the
// link, or a note while it loads.
func (u *UI) invitePanel(gtx C) D {
	d := &u.dialog
	in := &d.invite
	p := u.pal
	if u.btn("invite:cancel").Clicked(gtx) {
		u.closeDialog()
	}
	if u.btn("invite:join").Clicked(gtx) && in.group != nil && !in.joining && d.isOpen() {
		in.joining = true
		u.backend.JoinGroup(in.code)
	}
	if u.btn("invite:more").Clicked(gtx) {
		in.more = true
	}
	w := min(gtx.Dp(600), gtx.Constraints.Max.X-gtx.Dp(32))
	gtx.Constraints = layout.Constraints{Min: image.Pt(w, 0), Max: image.Pt(w, gtx.Constraints.Max.Y-gtx.Dp(48))}

	// center lays w out shrink-wrapped, in the middle of the panel.
	center := func(w layout.Widget) layout.FlexChild {
		return layout.Rigid(func(gtx C) D {
			full := gtx.Constraints.Max.X
			gtx.Constraints.Min = image.Point{}
			c := record(gtx, w)
			c.at(gtx, (full-c.size.X)/2, 0)
			return D{Size: image.Pt(full, c.size.Y)}
		})
	}
	gap := func(h unit.Dp) layout.FlexChild { return layout.Rigid(layout.Spacer{Height: h}.Layout) }
	note := func(size unit.Sp, txt string, col color.NRGBA) layout.Widget {
		return func(gtx C) D {
			l := u.label(size, txt, col, labelOpts{align: text.Middle})
			l.MaxLines = 0
			return l.Layout(gtx)
		}
	}
	cancel := dialogButton{label: "Cancel", flat: true}
	buttons := func(bts ...dialogButton) layout.FlexChild {
		keys := []string{"invite:cancel", "invite:join"}
		return center(func(gtx C) D {
			var row []layout.FlexChild
			for i, bt := range bts {
				if i > 0 {
					row = append(row, layout.Rigid(layout.Spacer{Width: 16}.Layout))
				}
				row = append(row, layout.Rigid(func(gtx C) D { return u.dialogButton(gtx, keys[i], bt) }))
			}
			return layout.Flex{Alignment: layout.Middle}.Layout(gtx, row...)
		})
	}

	var children []layout.FlexChild
	g := in.group
	switch {
	case in.err != "":
		cancel.label = "OK"
		children = append(children, gap(32),
			center(note(16, "Couldn't open this invite", p.Text)), gap(12),
			center(note(14.5, in.err, p.TextSecondary)), gap(28),
			buttons(cancel))
	case g == nil:
		children = append(children, gap(40),
			center(note(15, "Looking up the group…", p.TextSecondary)), gap(32),
			buttons(cancel))
	default:
		children = append(children, gap(36),
			center(func(gtx C) D { return u.avatar(gtx, g.ID, g.Name, true, 120) }), gap(24),
			center(func(gtx C) D {
				gtx.Constraints.Max.X -= gtx.Dp(48)
				l := u.label(20, g.Name, p.Text, labelOpts{weight: font.Medium, maxLines: 2, align: text.Middle})
				return l.Layout(gtx)
			}))
		if !g.Created.IsZero() {
			children = append(children, gap(6),
				center(note(14.5, "Created on "+g.Created.Format("02/01/2006"), p.TextSecondary)))
		}
		if len(g.Faces) > 0 {
			children = append(children, gap(18), center(func(gtx C) D { return u.inviteFaces(gtx, g) }))
		} else if g.Size > 0 {
			members := itoa(g.Size) + " members"
			if g.Size == 1 {
				members = "1 member"
			}
			children = append(children, gap(6), center(note(14.5, members, p.TextSecondary)))
		}
		if desc := strings.TrimSpace(g.Description); desc != "" {
			children = append(children, gap(24), center(func(gtx C) D { return u.inviteDescription(gtx, desc) }))
		}
		join := dialogButton{label: "Join group", primary: true}
		if g.Approval {
			join.label = "Request to join"
			children = append(children, gap(28),
				center(note(14.5, "An admin must approve your request.", p.TextSecondary)), gap(20))
		} else {
			children = append(children, gap(32))
		}
		if g.Community {
			join.label = "Join community"
		}
		if in.joining {
			join.label = "Joining…"
		}
		children = append(children, buttons(cancel, join))
	}
	children = append(children, gap(32))
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
}

// inviteFaces draws a few of the group's members, overlapping, and how
// many more there are.
func (u *UI) inviteFaces(gtx C, g *model.GroupPreview) D {
	p := u.pal
	const size unit.Dp = 40
	s, ring := gtx.Dp(size), gtx.Dp(2)
	step := s - gtx.Dp(8)
	x, h := ring, s+2*ring
	for _, id := range g.Faces {
		// A ring of the dialog's color sets each face off the last.
		fillCircle(gtx, image.Pt(x+s/2, ring+s/2), s/2+ring, p.Dialog)
		t := op.Offset(image.Pt(x, ring)).Push(gtx.Ops)
		u.avatar(gtx, id, "", false, size)
		t.Pop()
		x += step
	}
	w := x - step + s + ring
	if extra := g.Size - len(g.Faces); extra > 0 {
		lbl := record(gtx, u.label(15, "+"+itoa(extra), p.Text, labelOpts{weight: font.Medium, maxLines: 1}).Layout)
		pw := max(s, lbl.size.X+gtx.Dp(24))
		fillRRect(gtx, image.Rect(x-ring, 0, x+pw+ring, h), h/2, p.Dialog)
		fillRRect(gtx, image.Rect(x, ring, x+pw, ring+s), s/2, p.Hover)
		lbl.at(gtx, x+(pw-lbl.size.X)/2, ring+(s-lbl.size.Y)/2)
		w = x + pw + ring
	}
	return D{Size: image.Pt(w, h)}
}

// inviteDescription shows the group's description, cut with "Read more"
// at first, scrolling once it is all shown.
func (u *UI) inviteDescription(gtx C, desc string) D {
	p := u.pal
	in := &u.dialog.invite
	gtx.Constraints.Max.X = min(gtx.Constraints.Max.X-gtx.Dp(48), gtx.Dp(440))
	o := richOpts{links: "invite"}
	if !in.more {
		if cut, ok := cutText(desc, inviteDescRunes, inviteDescLines); ok {
			desc, o.more = cut, u.btn("invite:more")
		}
	}
	rich := func(gtx C) D { return u.layoutRich(gtx, desc, 15, p.Text, p.TextSecondary, o) }
	if !in.more {
		return rich(gtx)
	}
	gtx.Constraints.Max.Y = min(gtx.Constraints.Max.Y, gtx.Dp(260))
	return u.scrollList(gtx, &in.desc, 1, func(gtx C, _ int) D { return rich(gtx) })
}
