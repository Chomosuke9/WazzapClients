package ui

import (
	"image"
	"strings"
	"time"

	"gioui.org/font"
	"gioui.org/layout"
	"gioui.org/widget"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

// Editing a message happens in the composer, like replying: a bar above
// it shows the message, the composer holds its text, and the send button
// becomes a check. Enter saves; Esc or the bar's X gives the composer back
// the draft it had.

// msgEdit is the message being edited and the draft it put aside.
type msgEdit struct {
	msg  *model.Message
	orig string // the text it started with
	// What the composer had before.
	draft    string
	mentions []mentionRef
	reply    *model.Message
}

// startEdit puts one of your messages in the composer to edit.
func (u *UI) startEdit(m *model.Message) {
	c := &u.conv
	if c.edit.msg == nil {
		c.edit = msgEdit{draft: c.composer.Text(), mentions: c.mentions, reply: c.reply}
	}
	text, refs := u.backend.EditText(m)
	c.edit.msg, c.edit.orig = m, text
	c.reply, c.mentions = nil, nil
	for _, r := range refs {
		c.mentions = append(c.mentions, mentionRef{name: r.Name, jid: r.ID})
	}
	setComposer(&c.composer, text)
	u.requestFocus(&c.composer)
}

// finishEdit saves the edit, if the text changed.
func (u *UI) finishEdit() {
	u.stopOutgoingTyping()
	e := &u.conv.edit
	txt := trimSpace(u.conv.composer.Text())
	if txt == "" && e.msg.Media == model.MediaNone {
		return // a message can't be emptied; delete it instead
	}
	if txt != trimSpace(e.orig) {
		u.backend.Edit(e.msg, u.draftFrom(txt))
	}
	u.endEdit()
}

// cancelEdit stops editing without saving.
func (u *UI) cancelEdit() {
	if u.conv.edit.msg != nil {
		u.endEdit()
	}
}

// endEdit gives the composer back the draft it had.
func (u *UI) endEdit() {
	c := &u.conv
	e := c.edit
	c.edit = msgEdit{}
	setComposer(&c.composer, e.draft)
	c.mentions, c.reply = e.mentions, e.reply
}

// setComposer replaces an editor's text, with the caret at its end.
func setComposer(ed *widget.Editor, text string) {
	ed.SetText(text)
	n := ed.Len()
	ed.SetCaret(n, n)
}

// layoutEditPreview is the bar above the composer while editing: what the
// message said, and an X that stops editing.
func (u *UI) layoutEditPreview(gtx C, m *model.Message) D {
	p := u.pal
	if u.btn("edit:close").Clicked(gtx) {
		u.cancelEdit() // it keeps drawing while it shrinks away
	}
	q := &model.Quote{ID: m.ID, Text: m.Text, Media: m.Media}
	if m.Kind == model.KindImage && m.Text == "" {
		q.Text = ""
	}
	return layout.Inset{Left: 8, Right: 8, Top: 8}.Layout(gtx, func(gtx C) D {
		return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
			layout.Flexed(1, func(gtx C) D {
				return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
					layout.Rigid(func(gtx C) D {
						return layout.Inset{Left: 4, Bottom: 6}.Layout(gtx, func(gtx C) D {
							return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
								layout.Rigid(iconW(icEdit, 16, p.Green)),
								layout.Rigid(layout.Spacer{Width: 6}.Layout),
								layout.Rigid(u.label(13.5, "Edit message", p.Green, labelOpts{weight: font.Medium}).Layout),
							)
						})
					}),
					layout.Rigid(func(gtx C) D {
						return u.layoutQuote(gtx, q, p.QuoteIn, p.TextSecondary, gtx.Constraints.Max.X, m)
					}),
				)
			}),
			layout.Rigid(layout.Spacer{Width: 8}.Layout),
			layout.Rigid(func(gtx C) D { return u.iconButton(gtx, u.btn("edit:close"), icClose, 40, 24, p.Icon) }),
		)
	})
}

// Edit history is an extra feature (prefEditHistory): the texts an edited
// message had before, as this computer received them.

// openEditHistory shows an edited message's earlier texts.
func (u *UI) openEditHistory(m *model.Message) {
	u.dialog = dialogState{kind: dialogEdits, editOf: m, versions: u.backend.Versions(m)}
	u.dialog.list.Axis = layout.Vertical
}

func (u *UI) editsPanel(gtx C) D {
	d := &u.dialog
	p := u.pal
	if u.btn("edits:close").Clicked(gtx) {
		u.closeDialog()
	}
	m := d.editOf
	type entry struct {
		label, text string
	}
	entries := make([]entry, 0, len(d.versions)+1)
	for i, v := range d.versions {
		label := "Edited"
		if i == 0 {
			label = "Original"
		}
		entries = append(entries, entry{label + " · " + u.versionTime(v.Time), v.Text})
	}
	entries = append(entries, entry{"Now · edited " + u.versionTime(m.Edited), m.Text})
	bg, fg := p.BubbleIn, p.Text
	if m.FromMe {
		bg, fg = p.BubbleOut, p.TextOut
	}
	w := min(gtx.Dp(480), gtx.Constraints.Max.X-gtx.Dp(32))
	gtx.Constraints = layout.Constraints{Min: image.Pt(w, 0), Max: image.Pt(w, gtx.Constraints.Max.Y*4/5)}
	return layout.UniformInset(24).Layout(gtx, func(gtx C) D {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(u.label(19.5, "Edit history", p.Text, labelOpts{weight: font.Medium}).Layout),
			layout.Rigid(layout.Spacer{Height: 6}.Layout),
			layout.Rigid(func(gtx C) D {
				l := u.label(14.5, "What this message said before each edit, as this computer received it.", p.TextSecondary)
				l.MaxLines = 0
				return l.Layout(gtx)
			}),
			layout.Rigid(layout.Spacer{Height: 16}.Layout),
			layout.Flexed(1, func(gtx C) D {
				gtx.Constraints.Min.Y = 0
				return u.scrollList(gtx, &d.list, len(entries), func(gtx C, i int) D {
					e := entries[i]
					return layout.Inset{Bottom: 14}.Layout(gtx, func(gtx C) D {
						return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
							layout.Rigid(u.label(12.5, e.label, p.TextSecondary, labelOpts{maxLines: 1}).Layout),
							layout.Rigid(layout.Spacer{Height: 4}.Layout),
							layout.Rigid(func(gtx C) D {
								gtx.Constraints.Min.X = 0
								return u.card(gtx, 8, bg, func(gtx C) D {
									return layout.Inset{Left: 9, Right: 9, Top: 6, Bottom: 7}.Layout(gtx, func(gtx C) D {
										txt := e.text
										if txt == "" {
											txt = "(no caption)"
										}
										return u.layoutRich(gtx, txt, 14.2, fg, p.TextSecondary, richOpts{})
									})
								})
							}),
						)
					})
				})
			}),
			layout.Rigid(layout.Spacer{Height: 10}.Layout),
			layout.Rigid(func(gtx C) D {
				return layout.E.Layout(gtx, func(gtx C) D {
					return u.dialogButton(gtx, "edits:close", dialogButton{label: "Close", primary: true})
				})
			}),
		)
	})
}

// versionTime is when a version was written: "today at 09:41".
func (u *UI) versionTime(t time.Time) string {
	day := dateChip(t, u.now())
	if day == "Today" || day == "Yesterday" {
		day = strings.ToLower(day)
	}
	return day + " at " + t.Format("15:04")
}
