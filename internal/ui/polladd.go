package ui

import (
	"image"
	"slices"

	"gioui.org/font"
	"gioui.org/layout"
	"gioui.org/widget"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

// Adding an option to a poll: its sender can, and anyone when the poll
// allows it (model.PollState.AllowAdd), until it ends or has as many as a
// poll may (the card's "Add option", cards.go).

// pollAddState is the Add option dialog's.
type pollAddState struct {
	msg *model.Message
	ed  widget.Editor
}

// askPollOption opens the Add option dialog for poll m.
func (u *UI) askPollOption(m *model.Message) {
	a := &u.pollAdd
	a.msg = m
	a.ed.SingleLine, a.ed.Submit = true, true
	a.ed.SetText("")
	u.dialog = dialogState{kind: dialogPollOption}
	u.requestFocus(&a.ed)
}

// pollOptionProblem says why name can't be added to poll m, or "".
func pollOptionProblem(m *model.Message, name string) string {
	switch {
	case name == "":
		return " "
	case slices.ContainsFunc(m.Poll.Options, func(o model.PollOption) bool { return o.Name == name }):
		return "This poll already has that option."
	}
	return ""
}

// pollOptionPanel is the Add option dialog.
func (u *UI) pollOptionPanel(gtx C) D {
	a := &u.pollAdd
	p := u.pal
	open := u.dialog.isOpen()
	submit := false
	for {
		ev, ok := a.ed.Update(gtx)
		if !ok {
			break
		}
		if _, ok := ev.(widget.SubmitEvent); ok {
			submit = true
		}
	}
	name := trimSpace(a.ed.Text())
	problem := pollOptionProblem(a.msg, name)
	add := func() {
		if problem != "" {
			return
		}
		u.backend.AddPollOption(a.msg, name)
		u.closeDialog()
	}
	buttons := []dialogButton{
		{label: "Add", primary: true, disabled: problem != "", run: add},
		{label: "Cancel", run: u.closeDialog},
	}
	for i, bt := range buttons {
		if u.btn("polladd:dlg:"+itoa(i)).Clicked(gtx) && open && !bt.disabled {
			bt.run()
			break
		}
	}
	if submit && open {
		add()
	}
	w := min(gtx.Dp(440), gtx.Constraints.Max.X-gtx.Dp(32))
	gtx.Constraints = layout.Constraints{Min: image.Pt(w, 0), Max: image.Pt(w, gtx.Constraints.Max.Y)}
	return layout.UniformInset(24).Layout(gtx, func(gtx C) D {
		var btns []layout.FlexChild
		for i := len(buttons) - 1; i >= 0; i-- {
			i, bt := i, buttons[i]
			if len(btns) > 0 {
				btns = append(btns, layout.Rigid(layout.Spacer{Width: 12}.Layout))
			}
			btns = append(btns, layout.Rigid(func(gtx C) D { return u.dialogButton(gtx, "polladd:dlg:"+itoa(i), bt) }))
		}
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(u.label(19.5, "Add option", p.Text, labelOpts{weight: font.Medium}).Layout),
			layout.Rigid(layout.Spacer{Height: 12}.Layout),
			layout.Rigid(func(gtx C) D {
				lb := u.label(14.5, a.msg.Text, p.TextSecondary, labelOpts{maxLines: 3})
				return lb.Layout(gtx)
			}),
			layout.Rigid(layout.Spacer{Height: 10}.Layout),
			layout.Rigid(func(gtx C) D { return u.pollLine(gtx, &a.ed, "Option", problem != "" && problem != " ") }),
			layout.Rigid(layout.Spacer{Height: 8}.Layout),
			layout.Rigid(func(gtx C) D {
				gtx.Constraints.Min.X = gtx.Constraints.Max.X
				return u.label(13.5, problem+" ", p.Danger, labelOpts{maxLines: 2}).Layout(gtx)
			}),
			layout.Rigid(layout.Spacer{Height: 22}.Layout),
			layout.Rigid(func(gtx C) D {
				return layout.E.Layout(gtx, func(gtx C) D {
					return layout.Flex{Alignment: layout.Middle}.Layout(gtx, btns...)
				})
			}),
		)
	})
}
