package ui

import (
	"image"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/widget/material"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

// After a while offline, a card over the bottom of the chat list counts the
// missed messages as they come, as WhatsApp's does: only when there are
// many, and they don't all come at once.

const (
	catchUpMin   = 100                    // missed messages that show the card
	catchUpDelay = 500 * time.Millisecond // how long they take before it shows
)

type catchUp struct {
	// done of total came; they stay as they were while the card fades out.
	done, total int
	on          bool
	since       time.Time // when the first came
	anim        tween
	bar         follower
}

func (c *catchUp) event(e model.CatchUpEvent, now time.Time) {
	switch {
	case e.Total == 0: // cut short
		c.on = false
		return
	case e.Done >= e.Total:
		c.on = false
	case !c.on:
		c.on, c.since = true, now
		if c.anim.v == 0 {
			c.bar = follower{}
		}
	}
	c.done, c.total = e.Done, e.Total
}

func (u *UI) layoutCatchUp(gtx C) {
	c := &u.catchUp
	on := c.on && c.total >= catchUpMin
	if on {
		if at := c.since.Add(catchUpDelay); u.now().Before(at) {
			gtx.Execute(op.InvalidateCmd{At: at})
			on = false
		}
	}
	v := c.anim.step(gtx, on, durDialog)
	if v == 0 {
		return
	}
	p := u.pal
	sz := gtx.Constraints.Max
	gap := gtx.Dp(12)
	w := sz.X - 2*gap
	if w < gtx.Dp(120) {
		return
	}
	share := c.bar.step(gtx, float32(c.done)/float32(max(c.total, 1)), durSlide)
	cgtx := gtx
	cgtx.Constraints = layout.Constraints{Min: image.Pt(w, 0), Max: image.Pt(w, sz.Y)}
	card := record(cgtx, func(gtx C) D {
		return layout.Inset{Left: 16, Right: 16, Top: 12, Bottom: 14}.Layout(gtx, func(gtx C) D {
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
				layout.Rigid(func(gtx C) D {
					return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
						layout.Rigid(func(gtx C) D {
							gtx.Constraints = layout.Exact(image.Pt(gtx.Dp(18), gtx.Dp(18)))
							l := material.Loader(u.th)
							l.Color = p.Green
							return l.Layout(gtx)
						}),
						layout.Rigid(layout.Spacer{Width: 12}.Layout),
						layout.Flexed(1, func(gtx C) D {
							gtx.Constraints.Min.X = gtx.Constraints.Max.X
							return u.label(14.5, "Syncing messages", p.Text).Layout(gtx)
						}),
						layout.Rigid(layout.Spacer{Width: 8}.Layout),
						layout.Rigid(u.label(13.5, itoa(c.done)+"/"+itoa(c.total), p.TextSecondary).Layout),
					)
				}),
				layout.Rigid(layout.Spacer{Height: 10}.Layout),
				layout.Rigid(func(gtx C) D {
					bw, bh := gtx.Constraints.Max.X, gtx.Dp(4)
					fillRRect(gtx, image.Rect(0, 0, bw, bh), bh/2, mix(p.Popup, p.Text, 0.16))
					if x := int(float32(bw)*share + 0.5); x > 0 {
						fillRRect(gtx, image.Rect(0, 0, max(x, bh), bh), bh/2, p.Green)
					}
					return D{Size: image.Pt(bw, bh)}
				}),
			)
		})
	})
	x, y := gap, sz.Y-card.size.Y-gap
	e := easeOut(v)
	// It rises into place and sinks away.
	defer pushFx(gtx, e, moveBy(0, float32(gtx.Dp(16))*(1-e))).Pop()
	rect := image.Rectangle{Max: card.size}.Add(image.Pt(x, y))
	fillRRect(gtx, rect.Add(image.Pt(0, gtx.Dp(2))).Inset(-gtx.Dp(1)), gtx.Dp(13), p.Shadow)
	borderRRect(gtx, rect, gtx.Dp(12), p.Popup, p.PopupBorder)
	if on {
		// The chats under it don't get its clicks.
		t := op.Offset(rect.Min).Push(gtx.Ops)
		pg := gtx
		pg.Constraints = layout.Exact(card.size)
		noFocus(u.btn("catchup")).Layout(pg, func(gtx C) D { return D{Size: card.size} })
		t.Pop()
	}
	card.at(gtx, x, y)
}
