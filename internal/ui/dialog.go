package ui

import (
	"image"
	"strings"
	"time"

	"gioui.org/font"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/text"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

type dialogKind int

const (
	dialogNone dialogKind = iota
	dialogConfirm
	dialogForward
)

type dialogButton struct {
	label   string
	primary bool // filled green
	danger  bool // filled red
	run     func()
}

// dialogState is the open modal: a confirmation or the forward picker.
type dialogState struct {
	kind    dialogKind
	title   string
	body    string
	buttons []dialogButton
	scrim   widget.Clickable

	// Forward picker.
	fwd    []*model.Message
	picked []string // chat IDs, in the order they were picked
	search widget.Editor
	list   widget.List
}

// confirm asks before a destructive action. A Cancel button is added.
func (u *UI) confirm(title, body string, buttons ...dialogButton) {
	u.dialog = dialogState{kind: dialogConfirm, title: title, body: body,
		buttons: append(buttons, dialogButton{label: "Cancel"})}
}

// confirmDelete offers "Delete for everyone" when all messages are yours,
// or you administer the group, and they are still recent, like WhatsApp
// (which allows about two days).
func (u *UI) confirmDelete(msgs []*model.Message) {
	b := u.backend
	everyone := len(msgs) > 0
	admin := false
	if c := u.selected; c != nil && c.IsGroup {
		admin = u.amAdmin(c.ID)
	}
	for _, m := range msgs {
		if (!m.FromMe && !admin) || m.Kind == model.KindDeleted || u.now().Sub(m.Time) > 60*time.Hour {
			everyone = false
		}
	}
	title := "Delete message?"
	if len(msgs) > 1 {
		title = "Delete " + itoa(len(msgs)) + " messages?"
	}
	var buttons []dialogButton
	if everyone {
		buttons = append(buttons, dialogButton{label: "Delete for everyone", danger: true, run: func() {
			for _, m := range msgs {
				b.Delete(m, true)
			}
			u.endSelect()
		}})
	}
	buttons = append(buttons, dialogButton{label: "Delete for me", danger: !everyone, run: func() {
		for _, m := range msgs {
			b.Delete(m, false)
		}
		u.endSelect()
	}})
	u.confirm(title, "", buttons...)
}

func (u *UI) openForward(msgs []*model.Message) {
	u.dialog = dialogState{kind: dialogForward, fwd: msgs}
	u.dialog.search.SingleLine = true
	u.dialog.list.Axis = layout.Vertical
	u.requestFocus(&u.dialog.search)
}

func (u *UI) layoutDialog(gtx C) {
	d := &u.dialog
	if d.kind == dialogNone {
		return
	}
	p := u.pal
	if d.scrim.Clicked(gtx) {
		u.dialog = dialogState{}
		return
	}
	sz := gtx.Constraints.Max
	sgtx := gtx
	sgtx.Constraints = layout.Exact(sz)
	d.scrim.Layout(sgtx, func(gtx C) D { return fill(gtx, p.Scrim) })

	var panel part
	switch d.kind {
	case dialogConfirm:
		panel = record(gtx, u.confirmPanel)
	case dialogForward:
		panel = record(gtx, u.forwardPanel)
	}
	if u.dialog.kind == dialogNone {
		return // a button closed it
	}
	x, y := (sz.X-panel.size.X)/2, (sz.Y-panel.size.Y)/2
	r := gtx.Dp(16)
	rect := image.Rectangle{Max: panel.size}.Add(image.Pt(x, y))
	fillRRect(gtx, rect.Add(image.Pt(0, gtx.Dp(4))).Inset(-gtx.Dp(3)), r+gtx.Dp(3), p.Shadow)
	fillRRect(gtx, rect, r, p.Dialog)
	// Swallow clicks on the panel so they don't reach the scrim.
	func() {
		t := op.Offset(rect.Min).Push(gtx.Ops)
		defer t.Pop()
		pg := gtx
		pg.Constraints = layout.Exact(panel.size)
		u.btn("dialog:panel").Layout(pg, func(gtx C) D { return D{Size: panel.size} })
	}()
	panel.at(gtx, x, y)
}

func (u *UI) confirmPanel(gtx C) D {
	d := &u.dialog
	p := u.pal
	for i, bt := range d.buttons {
		if u.btn("dialog:" + itoa(i+1)).Clicked(gtx) {
			u.dialog = dialogState{}
			if bt.run != nil {
				bt.run()
			}
			return D{}
		}
	}
	w := min(gtx.Dp(480), gtx.Constraints.Max.X-gtx.Dp(32))
	gtx.Constraints = layout.Constraints{Min: image.Pt(w, 0), Max: image.Pt(w, gtx.Constraints.Max.Y)}
	return layout.UniformInset(24).Layout(gtx, func(gtx C) D {
		children := []layout.FlexChild{
			layout.Rigid(func(gtx C) D {
				l := u.label(19.5, d.title, p.Text, labelOpts{weight: font.Medium})
				l.MaxLines = 0
				return l.Layout(gtx)
			}),
		}
		if d.body != "" {
			children = append(children,
				layout.Rigid(layout.Spacer{Height: 12}.Layout),
				layout.Rigid(func(gtx C) D {
					l := u.label(14.5, d.body, p.TextSecondary)
					l.MaxLines = 0
					return l.Layout(gtx)
				}))
		}
		children = append(children, layout.Rigid(layout.Spacer{Height: 26}.Layout))
		// Buttons: stacked when there are three (delete), in a row otherwise.
		stack := len(d.buttons) > 2
		var btns []layout.FlexChild
		for i, bt := range d.buttons {
			i, bt := i, bt
			if i > 0 {
				if stack {
					btns = append(btns, layout.Rigid(layout.Spacer{Height: 10}.Layout))
				} else {
					btns = append(btns, layout.Rigid(layout.Spacer{Width: 12}.Layout))
				}
			}
			btns = append(btns, layout.Rigid(func(gtx C) D {
				return u.dialogButton(gtx, "dialog:"+itoa(i+1), bt)
			}))
		}
		if stack {
			children = append(children, layout.Rigid(func(gtx C) D {
				return layout.E.Layout(gtx, func(gtx C) D {
					return layout.Flex{Axis: layout.Vertical, Alignment: layout.End}.Layout(gtx, btns...)
				})
			}))
		} else {
			// Cancel goes first (left), the action last, like WhatsApp.
			for i, j := 0, len(btns)-1; i < j; i, j = i+1, j-1 {
				btns[i], btns[j] = btns[j], btns[i]
			}
			children = append(children, layout.Rigid(func(gtx C) D {
				return layout.E.Layout(gtx, func(gtx C) D {
					return layout.Flex{Alignment: layout.Middle}.Layout(gtx, btns...)
				})
			}))
		}
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
	})
}

// dialogButton is a pill button: green or red when it's the action,
// outlined otherwise.
func (u *UI) dialogButton(gtx C, key string, bt dialogButton) D {
	p := u.pal
	c := u.btn(key)
	return clickable(gtx, c, func(gtx C) D {
		gtx.Constraints.Min = image.Point{}
		fg, bg, border := p.Green, p.Dialog, p.PopupBorder
		switch {
		case bt.danger:
			fg, bg, border = p.OnGreen, p.Danger, p.Danger
		case bt.primary:
			fg, bg, border = p.OnGreen, p.Green, p.Green
		}
		if c.Hovered() {
			bg = mix(bg, p.Text, 0.08)
		}
		lbl := record(gtx, u.label(14.5, bt.label, fg, labelOpts{weight: font.Medium, maxLines: 1}).Layout)
		h := gtx.Dp(40)
		w := lbl.size.X + gtx.Dp(48)
		borderRRect(gtx, image.Rect(0, 0, w, h), h/2, bg, border)
		lbl.at(gtx, (w-lbl.size.X)/2, (h-lbl.size.Y)/2)
		return D{Size: image.Pt(w, h)}
	})
}

// forwardPanel lists chats to forward to, with search and a send bar.
func (u *UI) forwardPanel(gtx C) D {
	d := &u.dialog
	p := u.pal
	if u.btn("fwd:close").Clicked(gtx) {
		u.dialog = dialogState{}
		return D{}
	}
	if u.btn("fwd:send").Clicked(gtx) && len(d.picked) > 0 {
		u.backend.Forward(d.fwd, d.picked)
		u.endSelect()
		u.dialog = dialogState{}
		return D{}
	}
	q := strings.ToLower(trimSpace(d.search.Text()))
	var chats []*model.Chat
	for _, c := range u.chats {
		if q == "" || strings.Contains(strings.ToLower(c.Name), q) {
			chats = append(chats, c)
		}
	}
	for _, c := range chats {
		if u.btn("fwd:" + c.ID).Clicked(gtx) {
			if i := indexOf(d.picked, c.ID); i >= 0 {
				d.picked = append(d.picked[:i:i], d.picked[i+1:]...)
			} else {
				d.picked = append(d.picked, c.ID)
			}
		}
	}
	w := min(gtx.Dp(460), gtx.Constraints.Max.X-gtx.Dp(32))
	h := min(gtx.Dp(640), gtx.Constraints.Max.Y-gtx.Dp(48))
	gtx.Constraints = layout.Exact(image.Pt(w, h))
	defer clip.UniformRRect(image.Rectangle{Max: image.Pt(w, h)}, gtx.Dp(16)).Push(gtx.Ops).Pop()
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx C) D {
			return vcenter(gtx, gtx.Dp(64), func(gtx C) D {
				return layout.Inset{Left: 12, Right: 20}.Layout(gtx, func(gtx C) D {
					return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
						layout.Rigid(func(gtx C) D { return u.iconButton(gtx, u.btn("fwd:close"), icClose, 40, 24, p.IconStrong) }),
						layout.Rigid(layout.Spacer{Width: 12}.Layout),
						layout.Flexed(1, u.label(18, "Forward message to", p.Text, labelOpts{weight: font.Medium, maxLines: 1}).Layout),
					)
				})
			})
		}),
		layout.Rigid(func(gtx C) D {
			return layout.Inset{Left: 16, Right: 16, Bottom: 8}.Layout(gtx, func(gtx C) D {
				return u.searchField(gtx, &d.search, "Search name or number")
			})
		}),
		layout.Flexed(1, func(gtx C) D {
			l := material.List(u.th, &d.list)
			l.AnchorStrategy = material.Overlay
			return l.Layout(gtx, len(chats), func(gtx C, i int) D {
				c := chats[i]
				on := indexOf(d.picked, c.ID) >= 0
				cl := u.btn("fwd:" + c.ID)
				return clickable(gtx, cl, func(gtx C) D {
					gtx.Constraints.Min.X = gtx.Constraints.Max.X
					bg := p.Dialog
					if cl.Hovered() {
						bg = p.Hover
					}
					return background(gtx, bg, 0, func(gtx C) D {
						return vcenter(gtx, gtx.Dp(64), func(gtx C) D {
							return layout.Inset{Left: 20, Right: 20}.Layout(gtx, func(gtx C) D {
								box, col := icCheckBoxEmpty, p.TextSecondary
								if on {
									box, col = icCheckBox, p.Green
								}
								return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
									layout.Rigid(iconW(box, 24, col)),
									layout.Rigid(layout.Spacer{Width: 18}.Layout),
									layout.Rigid(func(gtx C) D { return u.avatar(gtx, c.ID, c.Name, c.IsGroup, 44) }),
									layout.Rigid(layout.Spacer{Width: 14}.Layout),
									layout.Flexed(1, u.label(16, c.Name, p.Text, labelOpts{maxLines: 1}).Layout),
								)
							})
						})
					})
				})
			})
		}),
		layout.Rigid(func(gtx C) D {
			if len(d.picked) == 0 {
				return D{}
			}
			var names []string
			for _, id := range d.picked {
				if c := u.chatByID(id); c != nil {
					names = append(names, c.Name)
				}
			}
			gtx.Constraints.Min.X = gtx.Constraints.Max.X
			return background(gtx, p.Panel, 0, func(gtx C) D {
				return vcenter(gtx, gtx.Dp(72), func(gtx C) D {
					return layout.Inset{Left: 24, Right: 16}.Layout(gtx, func(gtx C) D {
						return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
							layout.Flexed(1, u.label(15, strings.Join(names, ", "), p.TextSecondary, labelOpts{maxLines: 1}).Layout),
							layout.Rigid(layout.Spacer{Width: 12}.Layout),
							layout.Rigid(func(gtx C) D {
								c := u.btn("fwd:send")
								return clickable(gtx, c, func(gtx C) D {
									sz := gtx.Dp(52)
									fillCircle(gtx, image.Pt(sz/2, sz/2), sz/2, p.Green)
									return centerIn(gtx, sz, iconW(icSend, 24, p.OnGreen))
								})
							}),
						)
					})
				})
			})
		}),
	)
}

func indexOf(s []string, v string) int {
	for i, x := range s {
		if x == v {
			return i
		}
	}
	return -1
}

// searchField is a rounded search box around an editor.
func (u *UI) searchField(gtx C, ed *widget.Editor, hint string) D {
	p := u.pal
	gtx.Constraints.Min.X = gtx.Constraints.Max.X
	return background(gtx, p.Search, 20, func(gtx C) D {
		return vcenter(gtx, gtx.Dp(40), func(gtx C) D {
			return layout.Inset{Left: 14, Right: 12}.Layout(gtx, func(gtx C) D {
				return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(iconW(icSearch, 20, p.TextSecondary)),
					layout.Rigid(layout.Spacer{Width: 12}.Layout),
					layout.Flexed(1, func(gtx C) D {
						e := material.Editor(u.th, ed, hint)
						e.TextSize = 15
						e.Color = p.Text
						e.HintColor = p.TextSecondary
						return e.Layout(gtx)
					}),
				)
			})
		})
	})
}

// toastState is a short notice at the bottom of the window.
type toastState struct {
	text  string
	until time.Time
}

func (u *UI) toast(s string) { u.toastMsg = toastState{text: s, until: u.now().Add(4 * time.Second)} }

func (u *UI) layoutToast(gtx C) {
	t := &u.toastMsg
	if t.text == "" {
		return
	}
	now := u.now()
	if now.After(t.until) {
		t.text = ""
		return
	}
	gtx.Execute(op.InvalidateCmd{At: t.until})
	p := u.pal
	sz := gtx.Constraints.Max
	card := record(gtx, func(gtx C) D {
		gtx.Constraints = layout.Constraints{Max: image.Pt(min(gtx.Dp(560), sz.X-gtx.Dp(32)), sz.Y)}
		return layout.Inset{Left: 20, Right: 20, Top: 13, Bottom: 13}.Layout(gtx,
			u.label(14.5, t.text, p.ToastText, labelOpts{maxLines: 2, align: text.Start}).Layout)
	})
	x := gtx.Dp(railWidth) + gtx.Dp(24)
	y := sz.Y - card.size.Y - gtx.Dp(24)
	rect := image.Rectangle{Max: card.size}.Add(image.Pt(x, y))
	fillRRect(gtx, rect.Add(image.Pt(0, gtx.Dp(2))).Inset(-gtx.Dp(1)), gtx.Dp(9), p.Shadow)
	fillRRect(gtx, rect, gtx.Dp(8), p.Toast)
	card.at(gtx, x, y)
}
