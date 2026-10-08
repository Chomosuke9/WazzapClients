package ui

import (
	"image"
	"slices"
	"strings"
	"time"

	"gioui.org/io/event"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/widget"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

// Reactions under a bubble: a pill with up to three of the emojis given,
// the most given first, and how many people reacted. Clicking it lists
// who reacted, with a tab per emoji, as in WhatsApp; clicking your own
// reaction there takes it back.

// reactionCounts merges the reactions to ms (a message, or an album's
// pictures), the most given first, and counts the people.
func reactionCounts(ms []*model.Message) (counts []model.ReactionCount, total int) {
	for _, m := range ms {
		for _, rc := range m.Reactions {
			total += rc.Count
			i := slices.IndexFunc(counts, func(c model.ReactionCount) bool { return c.Emoji == rc.Emoji })
			if i < 0 {
				counts = append(counts, rc)
				continue
			}
			counts[i].Count += rc.Count
		}
	}
	if len(ms) > 1 {
		slices.SortStableFunc(counts, func(a, b model.ReactionCount) int { return b.Count - a.Count })
	}
	return counts, total
}

// reactionKey tells one set of reactions from another, to pop a changed
// pill in.
func reactionKey(m *model.Message) string {
	var sb strings.Builder
	for _, rc := range m.Reactions {
		sb.WriteString(rc.Emoji + itoa(rc.Count))
	}
	return sb.String()
}

// reactionPill draws the reactions to ms on the bottom edge of their
// bubble (or, under, below it: an announcement's card) and returns the
// row's size. A running pop scales it in.
func (u *UI) reactionPill(gtx C, dims D, ms []*model.Message, out, under bool, pop animKey) D {
	counts, total := reactionCounts(ms)
	if total == 0 {
		return dims
	}
	p := u.pal
	c := u.btn("reactpill:" + ms[0].ChatID + "/" + ms[0].ID)
	if c.Clicked(gtx) {
		u.openReactions(ms)
	}
	pill := record(gtx, func(gtx C) D {
		gtx.Constraints.Min = image.Point{}
		return clickable(gtx, c, func(gtx C) D {
			return u.card(gtx, 13, p.BubbleIn, func(gtx C) D {
				return layout.Inset{Left: 6, Right: 6, Top: 2, Bottom: 3}.Layout(gtx, func(gtx C) D {
					var children []layout.FlexChild
					for i, rc := range counts {
						if i == 3 {
							break
						}
						children = append(children, layout.Rigid(u.label(14, rc.Emoji, p.Text).Layout))
					}
					if total > 1 {
						children = append(children, layout.Rigid(func(gtx C) D {
							return layout.Inset{Left: 3, Right: 1}.Layout(gtx, u.label(13, itoa(total), p.TextSecondary).Layout)
						}))
					}
					return layout.Flex{Alignment: layout.Middle}.Layout(gtx, children...)
				})
			})
		})
	})
	x := gtx.Dp(8)
	if out {
		x = dims.Size.X - pill.size.X - gtx.Dp(8)
	}
	ring := gtx.Dp(2)
	py := dims.Size.Y - gtx.Dp(5)
	if under {
		py = dims.Size.Y + gtx.Dp(3)
	}
	fx := fxStack{}
	if pop.id != "" && u.anims.running(pop) {
		v := u.anims.fade(gtx, pop, true, 380*time.Millisecond, 0)
		if v >= 1 {
			u.anims.stop(pop)
		}
		mid := image.Pt(x+pill.size.X/2, py+pill.size.Y/2)
		fx = pushFx(gtx, min(1, 3*v), scaleAt(mid, lerp(0.3, 1, easeOutBack(v))))
	}
	fillRRect(gtx, image.Rect(x-ring, py-ring, x+pill.size.X+ring, py+pill.size.Y+ring), pill.size.Y/2+ring, p.ChatBg)
	pill.at(gtx, x, py)
	fx.Pop()
	dims.Size.Y = py + pill.size.Y + ring
	return dims
}

// reactionsPopup lists who reacted to a message, or to an album's
// pictures.
type reactionsPopup struct {
	msgs  []*model.Message
	list  []reactorRow
	tab   string      // the emoji whose tab is open, "" for All
	at    image.Point // where the pill was clicked
	scrim widget.Clickable
	rows  widget.List
	panel int // an event tag that keeps clicks on the panel from the scrim

	closing bool
	anim    tween
}

// reactorRow is one person's reaction, to msg.
type reactorRow struct {
	model.Reactor
	msg *model.Message
}

func (r *reactionsPopup) isOpen() bool { return r.msgs != nil && !r.closing }

func (u *UI) openReactions(ms []*model.Message) {
	u.reacts = reactionsPopup{msgs: slices.Clone(ms), at: u.mouse}
	u.reacts.rows.Axis = layout.Vertical
	u.loadReactors()
}

func (u *UI) closeReactions() { u.reacts.closing = true }

// loadReactors reads who reacted, you first, then the newest first.
func (u *UI) loadReactors() {
	r := &u.reacts
	r.list = r.list[:0]
	for _, m := range r.msgs {
		for _, x := range u.backend.Reactors(m) {
			r.list = append(r.list, reactorRow{x, m})
		}
	}
	slices.SortStableFunc(r.list, func(a, b reactorRow) int {
		if a.Me != b.Me {
			if a.Me {
				return -1
			}
			return 1
		}
		return b.Time.Compare(a.Time)
	})
	if r.tab != "" && !slices.ContainsFunc(r.list, func(x reactorRow) bool { return x.Emoji == r.tab }) {
		r.tab = ""
	}
	if len(r.list) == 0 {
		u.closeReactions()
	}
}

// reactionsChanged reloads the open list when one of its messages changed.
func (u *UI) reactionsChanged(m *model.Message) {
	r := &u.reacts
	if !r.isOpen() {
		return
	}
	for i, x := range r.msgs {
		if x.ChatID == m.ChatID && x.ID == m.ID {
			r.msgs[i] = m
			u.loadReactors()
			return
		}
	}
}

// layoutReactions draws the open list of who reacted over everything.
func (u *UI) layoutReactions(gtx C) {
	r := &u.reacts
	if r.msgs == nil {
		return
	}
	if r.isOpen() && (r.scrim.Clicked(gtx) || u.selected == nil || u.selected.ID != r.msgs[0].ChatID) {
		u.closeReactions()
	}
	v := r.anim.step(gtx, r.isOpen(), popDur(r.isOpen()))
	if v == 0 && r.closing {
		u.reacts = reactionsPopup{}
		return
	}
	sz := gtx.Constraints.Max
	if r.closing {
		var done func()
		gtx, done = fadeOut(gtx)
		defer done()
	} else {
		sgtx := gtx
		sgtx.Constraints = layout.Exact(sz)
		r.scrim.Layout(sgtx, func(gtx C) D { return D{Size: gtx.Constraints.Max} })
	}
	if r.isOpen() {
		for _, x := range r.list {
			if x.Me && u.btn("reactor:me/"+x.msg.ID).Clicked(gtx) {
				u.backend.React(x.msg, "")
			}
		}
	}

	panel := record(gtx, u.reactionsPanel)
	// Above the pill, or below it when there's no room.
	pos := image.Pt(r.at.X-panel.size.X/2, r.at.Y-gtx.Dp(14)-panel.size.Y)
	if pos.Y < gtx.Dp(8) {
		pos.Y = min(r.at.Y+gtx.Dp(14), sz.Y-gtx.Dp(8)-panel.size.Y)
	}
	pos.X = max(gtx.Dp(8), min(pos.X, sz.X-gtx.Dp(8)-panel.size.X))
	origin := image.Pt(min(max(r.at.X, pos.X), pos.X+panel.size.X), min(max(r.at.Y, pos.Y), pos.Y+panel.size.Y))
	defer pushPopup(gtx, v, origin).Pop()
	rect := image.Rectangle{Max: panel.size}.Add(pos)
	rad := gtx.Dp(12)
	fillRRect(gtx, rect.Add(image.Pt(0, gtx.Dp(3))).Inset(-gtx.Dp(2)), rad+gtx.Dp(2), u.pal.Shadow)
	borderRRect(gtx, rect, rad, u.pal.Popup, u.pal.PopupBorder)
	area := clip.Rect(rect).Push(gtx.Ops)
	event.Op(gtx.Ops, &r.panel)
	area.Pop()
	defer clip.UniformRRect(rect, rad).Push(gtx.Ops).Pop()
	panel.at(gtx, pos.X, pos.Y)
}

// reactionsPanel is the list's content: the tabs, All and one per emoji
// (the most given first), over the people on the open one.
func (u *UI) reactionsPanel(gtx C) D {
	p := u.pal
	r := &u.reacts
	// The pill's order: the most given first, then the one given first.
	byTime := slices.Clone(r.list)
	slices.SortStableFunc(byTime, func(a, b reactorRow) int { return a.Time.Compare(b.Time) })
	var emojis []string
	count := map[string]int{}
	for _, x := range byTime {
		if count[x.Emoji] == 0 {
			emojis = append(emojis, x.Emoji)
		}
		count[x.Emoji]++
	}
	slices.SortStableFunc(emojis, func(a, b string) int { return count[b] - count[a] })
	for _, e := range append([]string{""}, emojis...) {
		if u.btn("reacttab:" + e).Clicked(gtx) {
			r.tab = e
			r.rows.Position = layout.Position{}
		}
	}
	var rows []reactorRow
	for _, x := range r.list {
		if r.tab == "" || x.Emoji == r.tab {
			rows = append(rows, x)
		}
	}

	w := gtx.Dp(340)
	gtx.Constraints = layout.Constraints{Min: image.Pt(w, 0), Max: image.Pt(w, gtx.Constraints.Max.Y)}
	tab := func(e, title string) layout.FlexChild {
		return layout.Rigid(func(gtx C) D {
			c := u.btn("reacttab:" + e)
			return clickable(gtx, c, func(gtx C) D {
				col := p.TextSecondary
				if r.tab == e {
					col = p.Text
				}
				l := record(gtx, u.label(15, title, col, labelOpts{maxLines: 1}).Layout)
				h, pad := gtx.Dp(48), gtx.Dp(14)
				sz := image.Pt(l.size.X+2*pad, h)
				if hv := u.hover(gtx, c); hv > 0 {
					fillRect(gtx, image.Rectangle{Max: sz}, faded(p.PopupHover, hv))
				}
				l.at(gtx, pad, (h-l.size.Y)/2)
				if r.tab == e {
					fillRect(gtx, image.Rect(0, h-gtx.Dp(3), sz.X, h), p.Green)
				}
				return D{Size: sz}
			})
		})
	}
	tabs := []layout.FlexChild{tab("", "All "+itoa(len(r.list)))}
	for _, e := range emojis {
		tabs = append(tabs, tab(e, e+" "+itoa(count[e])))
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx C) D {
			h := gtx.Dp(4 + 48)
			fillRect(gtx, image.Rect(0, h-max(1, gtx.Dp(1)), w, h), p.PopupDivider)
			// Tabs that don't fit are cut at the panel's edge.
			t := op.Offset(image.Pt(gtx.Dp(6), gtx.Dp(4))).Push(gtx.Ops)
			cl := clip.Rect{Max: image.Pt(w-gtx.Dp(12), gtx.Dp(48))}.Push(gtx.Ops)
			tgtx := gtx
			tgtx.Constraints = layout.Constraints{Max: image.Pt(1<<20, gtx.Dp(48))}
			layout.Flex{}.Layout(tgtx, tabs...)
			cl.Pop()
			t.Pop()
			return D{Size: image.Pt(w, h)}
		}),
		layout.Rigid(func(gtx C) D {
			rowH := gtx.Dp(64)
			gtx.Constraints.Max.Y = min(gtx.Constraints.Max.Y, rowH*len(rows)+gtx.Dp(16), rowH*5+rowH/2+gtx.Dp(8))
			gtx.Constraints.Min.Y = gtx.Constraints.Max.Y
			return layout.Inset{Top: 8, Bottom: 8}.Layout(gtx, func(gtx C) D {
				return u.scrollList(gtx, &r.rows, len(rows), func(gtx C, i int) D {
					return u.reactorRow(gtx, rows[i])
				})
			})
		}),
	)
}

// reactorRow is one person: their picture and name, and their emoji on
// the right. Yours says "Click to remove" and takes it back.
func (u *UI) reactorRow(gtx C, x reactorRow) D {
	p := u.pal
	content := func(gtx C, hover float32) D {
		sz := image.Pt(gtx.Constraints.Max.X, gtx.Dp(64))
		gtx.Constraints.Min = image.Point{}
		if hover > 0 {
			fillRect(gtx, image.Rectangle{Max: sz}, faded(p.PopupHover, hover))
		}
		t := op.Offset(image.Pt(gtx.Dp(16), (sz.Y-gtx.Dp(40))/2)).Push(gtx.Ops)
		u.avatar(gtx, x.ID, x.Name, false, 40)
		t.Pop()
		emoji := record(gtx, u.label(22, x.Emoji, p.Text).Layout)
		emoji.at(gtx, sz.X-gtx.Dp(20)-emoji.size.X, (sz.Y-emoji.size.Y)/2)
		left := gtx.Dp(16 + 40 + 14)
		tgtx := gtx
		tgtx.Constraints = layout.Constraints{Max: image.Pt(max(0, sz.X-left-emoji.size.X-gtx.Dp(36)), sz.Y)}
		text := record(tgtx, func(gtx C) D {
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
				layout.Rigid(u.label(16, x.Name, p.Text, labelOpts{maxLines: 1}).Layout),
				layout.Rigid(func(gtx C) D {
					if !x.Me {
						return D{}
					}
					return layout.Inset{Top: 2}.Layout(gtx, u.label(13, "Click to remove", p.PopupSub, labelOpts{maxLines: 1}).Layout)
				}),
			)
		})
		text.at(gtx, left, (sz.Y-text.size.Y)/2)
		return D{Size: sz}
	}
	if !x.Me {
		return content(gtx, 0)
	}
	c := u.btn("reactor:me/" + x.msg.ID)
	return clickable(gtx, c, func(gtx C) D { return content(gtx, u.hover(gtx, c)) })
}
