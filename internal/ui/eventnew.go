package ui

import (
	"image"
	"time"

	"gioui.org/font"
	"gioui.org/layout"
	"gioui.org/widget"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

// The attach menu's Event: the "Create event" dialog, laid out like the
// poll dialog. The event goes out as an EventMessage; answering one isn't
// possible (hypermeow can't send the answer), so the card only counts the
// answers others send.

// newEventState is the event being written.
type newEventState struct {
	name, desc, place widget.Editor
	date, time        widget.Editor // the start: 2006-01-02 and 15:04, local time
	ends              bool
	endDate, endTime  widget.Editor
	guests            bool // "Allow guests"
	list              widget.List
}

func (u *UI) openNewEvent() {
	u.dialog = dialogState{kind: dialogEvent}
	ev := &u.dialog.event
	ev.list.Axis = layout.Vertical
	for _, ed := range []*widget.Editor{&ev.name, &ev.place, &ev.date, &ev.time, &ev.endDate, &ev.endTime} {
		ed.SingleLine, ed.Submit = true, true
	}
	// WhatsApp suggests the next whole hour.
	start := u.now().Local().Truncate(time.Hour).Add(time.Hour)
	ev.date.SetText(start.Format("2006-01-02"))
	ev.time.SetText(start.Format("15:04"))
	u.requestFocus(&ev.name)
}

// parseLocal reads a date and time typed in the dialog.
func parseLocal(date, clock *widget.Editor, loc *time.Location) (time.Time, bool) {
	t, err := time.ParseInLocation("2006-01-02 15:04", trimSpace(date.Text())+" "+trimSpace(clock.Text()), loc)
	return t, err == nil
}

// event returns the event to send, and whether the start (and the end, if
// set) is valid. ok is false while it lacks a name or a valid time.
func (ev *newEventState) event(now time.Time) (e model.NewEvent, startOK, endOK, ok bool) {
	e = model.NewEvent{Name: trimSpace(ev.name.Text()), Description: trimSpace(ev.desc.Text()),
		Place: trimSpace(ev.place.Text()), AllowGuests: ev.guests}
	e.Start, startOK = parseLocal(&ev.date, &ev.time, now.Location())
	startOK = startOK && e.Start.After(now.Add(-time.Minute))
	endOK = true
	if ev.ends {
		e.End, endOK = parseLocal(&ev.endDate, &ev.endTime, now.Location())
		endOK = endOK && e.End.After(e.Start)
	}
	return e, startOK, endOK, e.Name != "" && startOK && endOK
}

// newEventPanel is the "Create event" dialog.
func (u *UI) newEventPanel(gtx C) D {
	d := &u.dialog
	ev := &d.event
	p := u.pal
	if u.btn("event:close").Clicked(gtx) {
		u.closeDialog()
	}
	if u.btn("event:guests").Clicked(gtx) {
		ev.guests = !ev.guests
	}
	now := u.now().Local()
	if u.btn("event:ends").Clicked(gtx) {
		if ev.ends = !ev.ends; ev.ends {
			// Two hours after the start, as WhatsApp suggests.
			start, ok := parseLocal(&ev.date, &ev.time, now.Location())
			if !ok {
				start = now
			}
			end := start.Add(2 * time.Hour)
			ev.endDate.SetText(end.Format("2006-01-02"))
			ev.endTime.SetText(end.Format("15:04"))
		}
	}
	// Enter moves to the next one-line field.
	fields := []*widget.Editor{&ev.name, &ev.date, &ev.time}
	if ev.ends {
		fields = append(fields, &ev.endDate, &ev.endTime)
	}
	fields = append(fields, &ev.place)
	for i, ed := range fields {
		for {
			e, ok := ed.Update(gtx)
			if !ok {
				break
			}
			if _, ok := e.(widget.SubmitEvent); ok && i+1 < len(fields) {
				u.requestFocus(fields[i+1])
			}
		}
	}
	e, startOK, endOK, ok := ev.event(now)
	if u.btn("event:send").Clicked(gtx) && ok && d.isOpen() && u.selected != nil {
		if m := u.backend.SendEvent(u.selected.ID, e); m != nil {
			u.upsertMessage(m)
			u.scrollMessages(layout.Position{})
		}
		u.closeDialog()
	}

	w := min(gtx.Dp(460), gtx.Constraints.Max.X-gtx.Dp(32))
	h := min(gtx.Dp(600), gtx.Constraints.Max.Y-gtx.Dp(48))
	gtx.Constraints = layout.Exact(image.Pt(w, h))
	heading := func(s string) layout.Widget {
		return func(gtx C) D {
			return layout.Inset{Left: 24, Right: 24, Top: 14, Bottom: 8}.Layout(gtx,
				u.label(14, s, p.TextSecondary, labelOpts{weight: font.Medium, maxLines: 1}).Layout)
		}
	}
	when := func(date, clock *widget.Editor, bad bool) layout.Widget {
		return func(gtx C) D {
			return layout.Inset{Left: 24, Right: 24, Bottom: 8}.Layout(gtx, func(gtx C) D {
				return layout.Flex{}.Layout(gtx,
					layout.Flexed(1, func(gtx C) D { return u.pollLine(gtx, date, "YYYY-MM-DD", bad) }),
					layout.Rigid(layout.Spacer{Width: 16}.Layout),
					layout.Flexed(1, func(gtx C) D { return u.pollLine(gtx, clock, "HH:MM", bad) }),
				)
			})
		}
	}
	field := func(ed *widget.Editor, hint string) layout.Widget {
		return func(gtx C) D {
			return layout.Inset{Bottom: 8}.Layout(gtx, func(gtx C) D { return u.pollField(gtx, ed, hint) })
		}
	}
	rows := []layout.Widget{
		heading("Event"),
		field(&ev.name, "Event name"),
		field(&ev.desc, "Description (optional)"),
		heading("Start date and time"),
		when(&ev.date, &ev.time, !startOK),
		func(gtx C) D { return u.pollCheck(gtx, "event:ends", "Add end time", ev.ends) },
	}
	if ev.ends {
		rows = append(rows, when(&ev.endDate, &ev.endTime, !endOK))
	}
	rows = append(rows,
		heading("Location"),
		field(&ev.place, "Location (optional)"),
		heading("Settings"),
		func(gtx C) D { return u.pollCheck(gtx, "event:guests", "Allow guests", ev.guests) },
	)
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx C) D { return u.formHeader(gtx, "event:close", "Create event") }),
		layout.Flexed(1, func(gtx C) D {
			return u.scrollList(gtx, &ev.list, len(rows), func(gtx C, i int) D { return rows[i](gtx) })
		}),
		layout.Rigid(func(gtx C) D { return u.formSend(gtx, "event:send", ok) }),
	)
}
