package ui

import (
	"image"

	"gioui.org/font"
	"gioui.org/io/semantic"
	"gioui.org/layout"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

// A bot's list button (a native flow single_select, or a ListMessage)
// opens a dialog of its choices, in their sections. Picking one and
// pressing Send answers with its ID, like WhatsApp.

// listPick is the list dialog's state.
type listPick struct {
	msg  *model.Message
	btn  int    // the list button's index in msg.Buttons
	pick string // the picked row's ID
}

// openListPick opens the choices of m's list button i.
func (u *UI) openListPick(m *model.Message, i int) {
	u.dialog = dialogState{kind: dialogList, listPick: listPick{msg: m, btn: i}}
	u.dialog.list.Axis = layout.Vertical
}

// listPickItem is a section's title (row == nil) or one of its rows.
type listPickItem struct {
	title string
	row   *model.ListRow
}

func (u *UI) listPickPanel(gtx C) D {
	d := &u.dialog
	lp := &d.listPick
	p := u.pal
	b := lp.msg.Buttons[lp.btn]
	var items []listPickItem
	for _, s := range b.Sections {
		if s.Title != "" {
			items = append(items, listPickItem{title: s.Title})
		}
		for i := range s.Rows {
			items = append(items, listPickItem{row: &s.Rows[i]})
		}
	}
	for _, it := range items {
		if it.row != nil && u.btn("lpick:"+it.row.ID).Clicked(gtx) && d.isOpen() {
			lp.pick = it.row.ID
		}
	}
	if u.btn("lpick:cancel").Clicked(gtx) && d.isOpen() {
		u.closeDialog()
	}
	if u.btn("lpick:send").Clicked(gtx) && d.isOpen() && lp.pick != "" {
		u.closeDialog()
		if r := u.backend.PressButton(lp.msg, lp.btn, lp.pick); r != nil {
			u.upsertMessage(r)
			u.scrollMessages(layout.Position{})
		}
	}

	w := min(gtx.Dp(440), gtx.Constraints.Max.X-gtx.Dp(32))
	maxH := gtx.Constraints.Max.Y - gtx.Dp(64)
	gtx.Constraints = layout.Constraints{Min: image.Pt(w, 0), Max: image.Pt(w, maxH)}
	return layout.Inset{Top: 24, Bottom: 24}.Layout(gtx, func(gtx C) D {
		pad := layout.Inset{Left: 24, Right: 24}
		title := record(gtx, func(gtx C) D {
			return pad.Layout(gtx, func(gtx C) D {
				l := u.label(19.5, b.Label, p.Text, labelOpts{weight: font.Medium})
				l.MaxLines = 2
				return l.Layout(gtx)
			})
		})
		btns := record(gtx, func(gtx C) D {
			return pad.Layout(gtx, func(gtx C) D {
				return layout.E.Layout(gtx, func(gtx C) D {
					return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
						layout.Rigid(func(gtx C) D {
							return u.dialogButton(gtx, "lpick:cancel", dialogButton{label: "Cancel"})
						}),
						layout.Rigid(layout.Spacer{Width: 12}.Layout),
						layout.Rigid(func(gtx C) D {
							return u.dialogButton(gtx, "lpick:send", dialogButton{label: "Send", primary: true, disabled: lp.pick == ""})
						}),
					)
				})
			})
		})
		gap, gapB := gtx.Dp(14), gtx.Dp(18)
		lg := gtx
		lg.Constraints.Min.Y = 0
		lg.Constraints.Max.Y = max(0, gtx.Constraints.Max.Y-title.size.Y-btns.size.Y-gap-gapB)
		rows := record(lg, func(gtx C) D {
			return u.scrollList(gtx, &d.list, len(items), func(gtx C, i int) D {
				return u.listPickRow(gtx, items[i], lp.pick)
			})
		})
		title.at(gtx, 0, 0)
		y := title.size.Y + gap
		rows.at(gtx, 0, y)
		y += rows.size.Y + gapB
		btns.at(gtx, 0, y)
		return D{Size: image.Pt(w, y+btns.size.Y)}
	})
}

// listPickRow is a section's title, or a row with its description and a
// radio button.
func (u *UI) listPickRow(gtx C, it listPickItem, pick string) D {
	p := u.pal
	gtx.Constraints.Min.X = gtx.Constraints.Max.X
	if it.row == nil {
		return layout.Inset{Left: 24, Right: 24, Top: 10, Bottom: 4}.Layout(gtx, func(gtx C) D {
			return u.label(14, it.title, p.Green, labelOpts{weight: font.Medium, maxLines: 1}).Layout(gtx)
		})
	}
	r := it.row
	c := u.btn("lpick:" + r.ID)
	on := r.ID == pick
	return clickable(gtx, c, func(gtx C) D {
		semantic.RadioButton.Add(gtx.Ops)
		semantic.SelectedOp(on).Add(gtx.Ops)
		gtx.Constraints.Min.X = gtx.Constraints.Max.X
		row := record(gtx, func(gtx C) D {
			return layout.Inset{Left: 24, Right: 24, Top: 10, Bottom: 10}.Layout(gtx, func(gtx C) D {
				return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
					layout.Flexed(1, func(gtx C) D {
						gtx.Constraints.Min.X = gtx.Constraints.Max.X
						children := []layout.FlexChild{
							layout.Rigid(func(gtx C) D {
								l := u.label(15.5, r.Title, p.Text)
								l.MaxLines = 2
								return l.Layout(gtx)
							}),
						}
						if r.Description != "" {
							children = append(children,
								layout.Rigid(layout.Spacer{Height: 2}.Layout),
								layout.Rigid(func(gtx C) D {
									l := u.label(13.5, r.Description, p.TextSecondary)
									l.MaxLines = 3
									return l.Layout(gtx)
								}))
						}
						return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
					}),
					layout.Rigid(layout.Spacer{Width: 16}.Layout),
					layout.Rigid(func(gtx C) D { return u.radio(gtx, on) }),
				)
			})
		})
		if h := u.hover(gtx, c); h > 0 {
			fillRect(gtx, image.Rectangle{Max: row.size}, faded(p.PopupHover, h))
		}
		row.at(gtx, 0, 0)
		return D{Size: row.size}
	})
}
