package ui

import (
	"image"
	"slices"
	"time"

	"gioui.org/font"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/widget"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

// statusViews is the "Viewed by" sheet over one of your own status
// updates: the replies it got, then who saw it and when. The eye under
// the update, with the count, opens it.
type statusViews struct {
	open     bool
	anim     tween
	btn      widget.Clickable
	closeBtn widget.Clickable
	list     widget.List
	id       string // the update data is of
	loaded   bool   // data is current
	data     model.StatusViews
}

func (s *statusViews) close() { s.open = false }

// statusViewsChanged takes the new counts for the update the viewer
// shows (it keeps the thread it opened with), and has the sheet read its
// people again.
func (u *UI) statusViewsChanged() {
	v := &u.status.viewer
	v.views.loaded = false
	if v.thread == nil || !v.thread.Mine {
		return
	}
	for _, t := range u.statuses {
		if !t.Mine || t.Group {
			continue
		}
		for _, n := range t.Updates {
			for _, up := range v.thread.Updates {
				if up.ID == n.ID {
					up.Views = n.Views
				}
			}
		}
	}
}

// viewedAgo says when someone saw a status: "Just now", "5 minutes ago",
// then the time.
func viewedAgo(t, now time.Time) string {
	d := now.Sub(t)
	switch {
	case d < time.Minute:
		return "Just now"
	case d < 2*time.Minute:
		return "1 minute ago"
	case d < time.Hour:
		return itoa(int(d/time.Minute)) + " minutes ago"
	}
	return statusTime(t, now)
}

func countOf(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return itoa(n) + " " + many
}

// layoutStatusViews draws the eye and count under your own update in
// frame, and the sheet once it's open.
func (u *UI) layoutStatusViews(gtx C, up *model.StatusUpdate, frame image.Rectangle) {
	v := &u.status.viewer
	s := &v.views
	p := u.pal
	sz := gtx.Constraints.Max
	white := rgb(0xffffff)
	if v.isOpen() {
		if s.btn.Clicked(gtx) {
			s.open = !s.open
		}
		if s.closeBtn.Clicked(gtx) {
			s.open = false
		}
	}
	scrim := noFocus(u.btn("st:viewsscrim"))
	if scrim.Clicked(gtx) {
		s.open = false
	}

	// The eye, centered under the update.
	pill := record(gtx, func(gtx C) D {
		gtx.Constraints.Min = image.Point{}
		return clickable(gtx, &s.btn, func(gtx C) D {
			return background(gtx, mix(p.StatusBg, white, 0.14+0.08*u.hover(gtx, &s.btn)), 20, func(gtx C) D {
				return layout.Inset{Left: 14, Right: 16, Top: 9, Bottom: 9}.Layout(gtx, func(gtx C) D {
					return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
						layout.Rigid(iconW(icVisibility, 20, white)),
						layout.Rigid(layout.Spacer{Width: 8}.Layout),
						layout.Rigid(u.label(15, itoa(up.Views), white, labelOpts{weight: font.Medium, maxLines: 1}).Layout))
				})
			})
		})
	})
	pill.at(gtx, frame.Min.X+(frame.Dx()-pill.size.X)/2, frame.Max.Y+gtx.Dp(14))

	a := s.anim.step(gtx, s.open && v.isOpen(), durDialog)
	if a == 0 {
		return
	}
	if s.open && (!s.loaded || s.id != up.ID) {
		if s.id != up.ID {
			s.list.Position = layout.Position{}
		}
		s.id, s.loaded, s.data = up.ID, true, u.backend.StatusViews(up.ID)
		s.list.Axis = layout.Vertical
	}
	if !s.open {
		var done func()
		gtx, done = fadeOut(gtx)
		defer done()
	}
	// A click beside the sheet closes it.
	scrim.Layout(gtx, func(gtx C) D { return D{Size: sz} })

	// The sheet slides up from the bottom, as wide as the update or wider
	// when that's narrow.
	w := min(sz.X-gtx.Dp(40), max(frame.Dx(), gtx.Dp(400)))
	h := min(sz.Y-gtx.Dp(96), gtx.Dp(520))
	rad := gtx.Dp(16)
	x := (sz.X - w) / 2
	y := sz.Y - int(float32(h)*easeOut(a))
	fillRRect(gtx, image.Rect(x, y, x+w, y+h+rad), rad, p.Dialog)
	defer op.Offset(image.Pt(x, y)).Push(gtx.Ops).Pop()
	gtx.Constraints = layout.Exact(image.Pt(w, h))
	// Clicks on the sheet stay on it.
	noFocus(u.btn("st:viewssheet")).Layout(gtx, func(gtx C) D { return D{Size: gtx.Constraints.Max} })

	d := &s.data
	var rows []layout.Widget
	heading := func(txt string) {
		rows = append(rows, func(gtx C) D {
			return layout.Inset{Left: 24, Right: 24, Top: 14, Bottom: 6}.Layout(gtx,
				u.label(14.5, txt, p.TextSecondary, labelOpts{maxLines: 1}).Layout)
		})
	}
	if len(d.Replies) > 0 {
		heading(countOf(len(d.Replies), "reply", "replies"))
		for i, r := range d.Replies {
			rows = append(rows, u.statusViewRow("st:reply:"+itoa(i), r.ID, r.Name, mediaLabel(r.Msg), func() {
				if u.openChatByID(r.Msg.ChatID) {
					v.close()
					u.jumpTo(r.Msg.ID)
				}
			}))
		}
	}
	// Someone who replied is listed once, with their reply.
	var viewers []model.StatusViewer
	for _, r := range d.Viewers {
		if !slices.ContainsFunc(d.Replies, func(x model.StatusReply) bool { return x.ID == r.ID }) {
			viewers = append(viewers, r)
		}
	}
	if len(viewers) > 0 {
		heading(countOf(len(viewers), "view", "views"))
		for _, r := range viewers {
			rows = append(rows, u.statusViewRow("st:view:"+r.ID, r.ID, r.Name, viewedAgo(r.At, u.now()), func() {
				if u.openChatByID(r.ID) {
					v.close()
				}
			}))
		}
	} else if len(d.Replies) == 0 {
		rows = append(rows, func(gtx C) D {
			return layout.Inset{Left: 24, Right: 24, Top: 18, Bottom: 18}.Layout(gtx,
				u.label(15, "No views yet. People who turned read receipts off aren't listed.", p.TextSecondary).Layout)
		})
	}
	rows = append(rows, layout.Spacer{Height: 12}.Layout)

	layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx C) D {
			return layout.Inset{Left: 24, Right: 10, Top: 10, Bottom: 2}.Layout(gtx, func(gtx C) D {
				return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
					layout.Flexed(1, u.label(19, "Viewed by "+itoa(len(d.Viewers)), p.Text, labelOpts{weight: font.Medium, maxLines: 1}).Layout),
					layout.Rigid(func(gtx C) D { return u.iconButton(gtx, &s.closeBtn, icClose, 40, 24, p.TextSecondary) }))
			})
		}),
		layout.Flexed(1, func(gtx C) D {
			return u.scrollList(gtx, &s.list, len(rows), func(gtx C, i int) D { return rows[i](gtx) })
		}),
	)
}

// statusViewRow is a person in the sheet: their picture, name, and when
// they saw the update or what they replied. A click runs open.
func (u *UI) statusViewRow(key, id, name, sub string, open func()) layout.Widget {
	return func(gtx C) D {
		p := u.pal
		c := u.btn(key)
		if c.Clicked(gtx) {
			open()
		}
		return clickable(gtx, c, func(gtx C) D {
			w, h := gtx.Constraints.Max.X, gtx.Dp(64)
			if hv := u.hover(gtx, c); hv > 0 {
				fillRect(gtx, image.Rect(0, 0, w, h), faded(p.RowHover, hv))
			}
			gtx.Constraints = layout.Exact(image.Pt(w, h))
			return layout.Inset{Left: 24, Right: 24}.Layout(gtx, func(gtx C) D {
				return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(func(gtx C) D { return u.avatar(gtx, id, name, false, 42) }),
					layout.Rigid(layout.Spacer{Width: 14}.Layout),
					layout.Flexed(1, func(gtx C) D {
						gtx.Constraints.Min.Y = 0
						return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
							layout.Rigid(u.label(16, name, p.Text, labelOpts{maxLines: 1}).Layout),
							layout.Rigid(layout.Spacer{Height: 2}.Layout),
							layout.Rigid(u.label(14, previewText(sub), p.TextSecondary, labelOpts{maxLines: 1}).Layout))
					}))
			})
		})
	}
}
