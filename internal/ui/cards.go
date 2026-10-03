package ui

import (
	"fmt"
	"image"
	"image/color"
	"slices"
	"strings"
	"time"

	"gioui.org/font"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"

	"github.com/chomosuke9/wazzapclients/internal/model"
	"github.com/chomosuke9/wazzapclients/internal/ui/icon"
)

// Polls, locations, shared contacts and events in bubbles. Each is a card
// at the top of its bubble (see layoutAttachment), with the bubble's
// buttons under it (cardButtons).

// hasCard reports whether m shows as a poll, map, contact or event card.
// Messages stored before the app kept these details show as a label.
func hasCard(m *model.Message) bool {
	switch m.Media {
	case model.MediaPoll:
		return m.Poll != nil && len(m.Poll.Options) > 0
	case model.MediaLocation:
		return m.Location != nil
	case model.MediaContact:
		return len(m.Contacts) > 0
	case model.MediaEventInvite:
		return m.Event != nil
	}
	return false
}

// Buttons the UI adds under cards, after a message's own (model.Button
// kinds that backends never use).
const (
	buttonVotes    model.ButtonKind = 100 + iota // the poll's votes, or the event's answers
	buttonMessage                                // the chat with a shared contact (Value: their number)
	buttonContacts                               // every contact a message shares
	buttonMap                                    // a live location on the map
)

// cardButtons are the buttons under m's card.
func cardButtons(m *model.Message) []model.Button {
	if !hasCard(m) || m.Kind == model.KindDeleted || m.Kind == model.KindUnsupported {
		return nil
	}
	switch m.Media {
	case model.MediaPoll:
		return []model.Button{{Kind: buttonVotes, Label: "View votes"}}
	case model.MediaEventInvite:
		if e := m.Event; e.Going+e.Maybe+e.NotGoing > 0 {
			return []model.Button{{Kind: buttonVotes, Label: "View responses"}}
		}
	case model.MediaContact:
		if len(m.Contacts) > 1 {
			return []model.Button{{Kind: buttonContacts, Label: "View all"}}
		}
		if id := m.Contacts[0].WhatsApp(); id != "" {
			return []model.Button{{Kind: buttonMessage, Label: "Message", Value: id}}
		}
	case model.MediaLocation:
		if m.Location.Live {
			return []model.Button{{Kind: buttonMap, Label: "View live location"}}
		}
	}
	return nil
}

// pressCardButton answers one of cardButtons.
func (u *UI) pressCardButton(m *model.Message, b model.Button) {
	switch b.Kind {
	case buttonVotes:
		u.openVotes(m)
	case buttonMessage:
		u.messagePhone(b.Value, m.Contacts[0].Name)
	case buttonContacts:
		var lines []string
		for _, c := range m.Contacts {
			l := c.Name
			for _, p := range c.Phones {
				l += "\n" + first(p.Number, "+"+p.WAID)
			}
			lines = append(lines, l)
		}
		u.inform(fmt.Sprintf("%d contacts", len(m.Contacts)), strings.Join(lines, "\n\n"))
	case buttonMap:
		u.openMap(m.Location)
	}
}

func first(s ...string) string {
	for _, x := range s {
		if x != "" {
			return x
		}
	}
	return ""
}

// openMap shows a location on a map in the browser.
func (u *UI) openMap(l *model.Location) {
	if l == nil {
		return
	}
	if !openURL(l.MapURL()) {
		u.toast("Couldn't open the map.")
	}
}

// messagePhone opens the chat with a phone number (digits with the
// country code) once the backend has looked it up (phoneEvent).
func (u *UI) messagePhone(digits, name string) {
	u.cardPhone, u.cardPhoneName = digits, name
	u.backend.LookupPhone(digits)
}

// cardPhoneEvent answers messagePhone, and reports whether e was for it.
func (u *UI) cardPhoneEvent(e model.PhoneEvent) bool {
	if u.cardPhone == "" || e.Phone != u.cardPhone {
		return false
	}
	u.cardPhone = ""
	switch {
	case e.Err != "":
		u.toast(e.Err)
	case e.ID == "":
		u.toast("+" + e.Phone + " isn't on WhatsApp.")
	default:
		name := first(e.Name, u.cardPhoneName, "+"+e.Phone)
		if !u.openChatByID(e.ID) {
			u.openDirect(e.ID, name)
		}
	}
	return true
}

// layoutCard draws m's card, at most maxW wide; its last row is metaH
// high and leaves metaW free at its right end for the timestamp.
func (u *UI) layoutCard(gtx C, m *model.Message, maxW, metaW, metaH int, cols bubbleColors) D {
	switch m.Media {
	case model.MediaPoll:
		return u.layoutPollCard(gtx, m, maxW, metaH, cols)
	case model.MediaLocation:
		return u.layoutMapCard(gtx, m, maxW, metaW, metaH, cols)
	case model.MediaContact:
		return u.layoutContactCard(gtx, m, maxW, metaH, cols)
	}
	return u.layoutEventCard(gtx, m, maxW, metaW, metaH, cols)
}

// Polls.

// pollIcon is three bars of different lengths, like WhatsApp's.
var pollIcon = func() *icon.Icon {
	bar := func(y, w string) string {
		return "M4 " + y + "h" + w + "q1.5 0 1.5 1.5t-1.5 1.5h-" + w + "q-1.5 0-1.5-1.5t1.5-1.5Z"
	}
	ic, err := icon.Parse(bar("4.5", "9")+bar("10.5", "14")+bar("16.5", "6"), 0, 0, 24)
	if err != nil {
		panic(err)
	}
	return ic
}()

// calendarIcon is a calendar page, for events.
var calendarIcon = func() *icon.Icon {
	// The page clockwise, its window counterclockwise (a hole), then the
	// rings and the day.
	ic, err := icon.Parse("M4 4h16v17H4Z M6 10v9h12v-9Z M7 2h2v4H7Z M15 2h2v4h-2Z M8 12h4v4H8Z", 0, 0, 24)
	if err != nil {
		panic(err)
	}
	return ic
}()

// How long a poll's pick pops in or out, and its bars take to glide to
// their new shares.
const (
	pollPickDur = 260 * time.Millisecond
	pollBarDur  = 450 * time.Millisecond
)

// pollHint is the line under a poll's question.
func pollHint(p *model.PollState) string {
	switch {
	case p.Max == 1:
		return "Select one"
	case p.Max > 1:
		return fmt.Sprintf("Select up to %d", p.Max)
	}
	return "Select one or more"
}

// votePoll answers a click on option i: it becomes your vote, or comes off
// it. Picking your only pick again takes your vote back, as in WhatsApp.
func (u *UI) votePoll(m *model.Message, i int) {
	p := m.Poll
	var picks []int
	if p.Max == 1 {
		if !p.Options[i].Mine {
			picks = []int{i}
		}
	} else {
		for j, o := range p.Options {
			if o.Mine != (j == i) {
				picks = append(picks, j)
			}
		}
		if p.Max > 1 && len(picks) > p.Max {
			u.toast(fmt.Sprintf("You can select up to %d options.", p.Max))
			return
		}
	}
	u.backend.VotePoll(m, picks)
}

func (u *UI) layoutPollCard(gtx C, m *model.Message, maxW, metaH int, cols bubbleColors) D {
	p := u.pal
	poll := m.Poll
	for i := range poll.Options {
		if u.btn("poll:" + m.ID + ":" + itoa(i)).Clicked(gtx) {
			u.votePoll(m, i)
		}
	}
	w := min(maxW, gtx.Dp(330))
	lg := gtx
	lg.Constraints = layout.Constraints{Max: image.Pt(w, 1<<20)}
	q := record(lg, u.label(15.7, m.Text, cols.text, labelOpts{weight: font.SemiBold, maxLines: 6}).Layout)
	q.at(gtx, 0, 0)
	y := q.size.Y + gtx.Dp(4)

	// "Select one", with one tick or two.
	ic := icTick
	if poll.Multiple() {
		ic = icTicks
	}
	hint := record(lg, func(gtx C) D {
		return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
			layout.Rigid(iconW(ic, 16, cols.secondary)),
			layout.Rigid(layout.Spacer{Width: 4}.Layout),
			layout.Rigid(u.label(13, pollHint(poll), cols.secondary).Layout),
		)
	})
	hint.at(gtx, 0, y)
	y += hint.size.Y + gtx.Dp(14)

	box := gtx.Dp(22)
	textX := box + gtx.Dp(12)
	barH := max(1, gtx.Dp(6))
	track := mix(cols.bg, cols.secondary, 0.3)
	for i, o := range poll.Options {
		// The count and the faces of the first few who picked it.
		count := record(lg, u.label(15, itoa(o.Votes), cols.text).Layout)
		face := gtx.Dp(20)
		facesW := 0
		if n := len(o.Faces); n > 0 {
			facesW = face + (n-1)*face*2/3 + gtx.Dp(6)
		}
		ng := gtx
		ng.Constraints = layout.Constraints{Max: image.Pt(max(0, w-textX-count.size.X-facesW-gtx.Dp(8)), 1<<20)}
		name := record(ng, u.label(15, o.Name, cols.text, labelOpts{maxLines: 4}).Layout)
		rowH := max(box, name.size.Y)

		// The row votes; its whole width is the button.
		btn := u.btn("poll:" + m.ID + ":" + itoa(i))
		func() {
			t := op.Offset(image.Pt(0, y)).Push(gtx.Ops)
			defer t.Pop()
			bg := gtx
			bg.Constraints = layout.Exact(image.Pt(w, rowH+gtx.Dp(8)+barH))
			clickable(bg, btn, func(gtx C) D { return D{Size: gtx.Constraints.Min} })
		}()
		// Your pick pops in over the empty circle, and shrinks away when
		// you take it back.
		key := m.ChatID + "/" + m.ID + ":" + itoa(i)
		pv := u.follows.toggle(gtx, "pick:"+key, o.Mine, pollPickDur)
		mid := image.Pt(box/2, y+rowH/2)
		if pv < 1 {
			fillCircle(gtx, mid, box/2, mix(cols.secondary, cols.text, 0.3*u.hover(gtx, btn)))
			fillCircle(gtx, mid, box/2-max(1, gtx.Dp(1.5)), cols.bg)
		}
		if pv > 0 {
			fx := pushFx(gtx, min(1, pv*2), scaleAt(mid, max(0.01, easeOutBack(pv))))
			fillCircle(gtx, mid, box/2, p.Green)
			t := op.Offset(mid.Sub(image.Pt(gtx.Dp(8), gtx.Dp(8)))).Push(gtx.Ops)
			drawIcon(gtx, icTick, 16, p.OnGreen)
			t.Pop()
			fx.Pop()
		}
		name.at(gtx, textX, y+(rowH-name.size.Y)/2)
		count.at(gtx, w-count.size.X, y+(rowH-count.size.Y)/2)
		for j := len(o.Faces) - 1; j >= 0; j-- {
			// The newest on top, at the right.
			x := w - count.size.X - gtx.Dp(6) - face - j*face*2/3
			fy := y + (rowH-face)/2
			fillCircle(gtx, image.Pt(x+face/2, fy+face/2), face/2+max(1, gtx.Dp(1)), cols.bg)
			t := op.Offset(image.Pt(x, fy)).Push(gtx.Ops)
			u.avatar(gtx, o.Faces[j], "", false, 20)
			t.Pop()
		}
		y += rowH + gtx.Dp(8)

		// How much of the vote it got, gliding to its new share as votes
		// come in.
		bar := image.Rect(textX, y, w, y+barH)
		fillRRect(gtx, bar, barH/2, track)
		var share float32
		if poll.Voters > 0 {
			share = float32(o.Votes) / float32(poll.Voters)
		}
		if sv := u.follows.step(gtx, "bar:"+key, share, pollBarDur); sv > 0.001 {
			fw := max(barH, int(float32(bar.Dx())*sv+0.5))
			fillRRect(gtx, image.Rect(bar.Min.X, bar.Min.Y, bar.Min.X+fw, bar.Max.Y), barH/2, p.Green)
		}
		y += barH + gtx.Dp(16)
	}
	y -= gtx.Dp(10)
	return D{Size: image.Pt(w, y+metaH)}
}

// Locations.

func (u *UI) layoutMapCard(gtx C, m *model.Message, maxW, metaW, metaH int, cols bubbleColors) D {
	l := m.Location
	btn := u.btn("map:" + m.ID)
	if btn.Clicked(gtx) {
		if l.URL != "" && !l.Live {
			if !openURL(l.URL) {
				u.toast("Couldn't open the link.")
			}
		} else {
			u.openMap(l)
		}
	}
	w := min(maxW, gtx.Dp(300))
	r := image.Rect(0, 0, w, w/2)
	func() {
		defer clip.UniformRRect(r, gtx.Dp(6)).Push(gtx.Ops).Pop()
		if img := u.messageImage(m, w); img != nil && img.state == imgReady {
			paintCover(gtx, img.op, img.size, r)
		} else {
			u.mapPlaceholder(gtx, r, cols)
		}
		cg := gtx
		cg.Constraints = layout.Exact(r.Size())
		clickable(cg, btn, func(gtx C) D {
			fillRect(gtx, r, faded(rgb(0x000000), 0.08*u.hover(gtx, btn)))
			return D{Size: r.Size()}
		})
	}()
	// The pin, its point on the place.
	pin := gtx.Dp(34)
	t := op.Offset(image.Pt((w-pin)/2, r.Dy()/2-pin*22/24)).Push(gtx.Ops)
	drawIcon(gtx, mapPin, 34, rgb(0xea4335))
	fillCircle(gtx, image.Pt(pin/2, pin*9/24), pin*3/24, rgb(0xa50e0e))
	t.Pop()
	y := r.Max.Y + gtx.Dp(6)

	name, addr := l.Name, l.Address
	if l.Live {
		name, addr = "Live location", l.Name
	}
	lg := gtx
	lg.Constraints = layout.Constraints{Max: image.Pt(w-gtx.Dp(4), 1<<20)}
	if name != "" {
		nm := record(lg, u.label(15, name, cols.text, labelOpts{maxLines: 2}).Layout)
		nm.at(gtx, gtx.Dp(2), y)
		y += nm.size.Y
	}
	// The address shares the last line with the timestamp when it fits.
	if addr != "" {
		ag := lg
		ag.Constraints.Max.X = max(0, w-metaW-gtx.Dp(10))
		ad := record(ag, u.label(13, addr, cols.secondary, labelOpts{maxLines: 2}).Layout)
		ad.at(gtx, gtx.Dp(2), y+max(0, metaH-ad.size.Y))
		y += max(0, ad.size.Y-metaH)
	}
	return D{Size: image.Pt(w, y+metaH)}
}

// mapPin is a map's pin, its point at the bottom middle.
var mapPin = func() *icon.Icon {
	ic, err := icon.Parse("M12 22L6.5 13.5Q5 11 5 9Q5 2 12 2Q19 2 19 9Q19 11 17.5 13.5Z", 0, 0, 24)
	if err != nil {
		panic(err)
	}
	return ic
}()

// mapPlaceholder stands in for a map the message came without: streets
// on the land color of a map.
func (u *UI) mapPlaceholder(gtx C, r image.Rectangle, cols bubbleColors) {
	land := mix(cols.card, rgb(0xb9d9a8), 0.25)
	road := mix(land, cols.text, 0.15)
	fillRect(gtx, r, land)
	wd := max(2, gtx.Dp(4))
	for i, f := range []float32{0.22, 0.58, 0.83} {
		y := r.Min.Y + int(float32(r.Dy())*f)
		fillRect(gtx, image.Rect(r.Min.X, y, r.Max.X, y+wd-i%2*wd/2), road)
	}
	for i, f := range []float32{0.15, 0.4, 0.72} {
		x := r.Min.X + int(float32(r.Dx())*f)
		fillRect(gtx, image.Rect(x, r.Min.Y, x+wd-i%2*wd/2, r.Max.Y), road)
	}
}

// Contacts.

func (u *UI) layoutContactCard(gtx C, m *model.Message, maxW, metaH int, cols bubbleColors) D {
	c := m.Contacts[0]
	w := min(maxW, gtx.Dp(280))
	id := ""
	if wa := c.WhatsApp(); wa != "" {
		id = wa + "@s.whatsapp.net"
	}
	btn := u.btn("card:" + m.ID)
	if btn.Clicked(gtx) {
		switch {
		case len(m.Contacts) > 1:
			u.pressCardButton(m, model.Button{Kind: buttonContacts})
		case id != "":
			u.messagePhone(c.WhatsApp(), c.Name)
		}
	}
	size := gtx.Dp(46)
	name := c.Name
	if n := len(m.Contacts); n > 1 {
		name = fmt.Sprintf("%s and %d other contact", c.Name, n-1)
		if n > 2 {
			name += "s"
		}
	}
	ng := gtx
	ng.Constraints = layout.Constraints{Max: image.Pt(w-size-gtx.Dp(14), 1<<20)}
	nm := record(ng, u.label(15.5, name, cols.text, labelOpts{weight: font.SemiBold, maxLines: 2}).Layout)
	h := max(size, nm.size.Y) + gtx.Dp(8)
	func() {
		cg := gtx
		cg.Constraints = layout.Exact(image.Pt(w, h))
		clickable(cg, btn, func(gtx C) D { return D{Size: gtx.Constraints.Min} })
	}()
	t := op.Offset(image.Pt(0, (h-size)/2)).Push(gtx.Ops)
	if id != "" {
		u.avatar(gtx, id, c.Name, false, 46)
	} else {
		u.drawAvatar(gtx, "card:"+c.Name, c.Name, avatarPerson, 46)
	}
	t.Pop()
	nm.at(gtx, size+gtx.Dp(14), (h-nm.size.Y)/2)
	return D{Size: image.Pt(w, h+metaH)}
}

// Events.

func (u *UI) layoutEventCard(gtx C, m *model.Message, maxW, metaW, metaH int, cols bubbleColors) D {
	p := u.pal
	e := m.Event
	w := min(maxW, gtx.Dp(320))
	lg := gtx
	lg.Constraints = layout.Constraints{Max: image.Pt(w, 1<<20)}

	// The date on a calendar page, beside the name.
	badge := gtx.Dp(44)
	fillRRect(gtx, image.Rect(0, 0, badge, badge), gtx.Dp(8), cols.card)
	if !e.Start.IsZero() {
		mo := record(lg, u.label(10.5, strings.ToUpper(e.Start.Format("Jan")), p.Green, labelOpts{weight: font.Bold}).Layout)
		mo.at(gtx, (badge-mo.size.X)/2, gtx.Dp(4))
		d := record(lg, u.label(17, itoa(e.Start.Day()), cols.text, labelOpts{weight: font.SemiBold}).Layout)
		d.at(gtx, (badge-d.size.X)/2, badge-d.size.Y-gtx.Dp(3))
	} else {
		t := op.Offset(image.Pt((badge-gtx.Dp(24))/2, (badge-gtx.Dp(24))/2)).Push(gtx.Ops)
		drawIcon(gtx, calendarIcon, 24, p.Green)
		t.Pop()
	}
	tx := badge + gtx.Dp(12)
	ng := gtx
	ng.Constraints = layout.Constraints{Max: image.Pt(w-tx, 1<<20)}
	nameCol := cols.text
	if e.Canceled {
		nameCol = cols.secondary
	}
	nm := record(ng, u.label(15.7, e.Name, nameCol, labelOpts{weight: font.SemiBold, maxLines: 3}).Layout)
	y := 0
	if nm.size.Y < badge {
		y = (badge - nm.size.Y) / 2
	}
	nm.at(gtx, tx, y)
	y = max(badge, y+nm.size.Y) + gtx.Dp(10)

	// line draws one detail with its icon; a button makes it a link.
	line := func(ic *icon.Icon, txt string, col color.NRGBA, btn string) {
		lw := record(ng, u.label(14, txt, col, labelOpts{maxLines: 2}).Layout)
		if btn != "" {
			b := u.btn(btn)
			t := op.Offset(image.Pt(0, y)).Push(gtx.Ops)
			bg := gtx
			bg.Constraints = layout.Exact(image.Pt(tx+lw.size.X, max(lw.size.Y, gtx.Dp(20))))
			clickable(bg, b, func(gtx C) D { return D{Size: gtx.Constraints.Min} })
			t.Pop()
		}
		t := op.Offset(image.Pt((badge-gtx.Dp(18))/2, y+(lw.size.Y-gtx.Dp(18))/2)).Push(gtx.Ops)
		drawIcon(gtx, ic, 18, cols.secondary)
		t.Pop()
		lw.at(gtx, tx, y)
		y += lw.size.Y + gtx.Dp(6)
	}
	if e.Canceled {
		line(icBlock, "Canceled", p.Danger, "")
	}
	if !e.Start.IsZero() {
		line(icClock, eventWhen(e, u.now()), cols.secondary, "")
	}
	if pl := e.Place; pl != nil {
		if u.btn("eventmap:" + m.ID).Clicked(gtx) {
			u.openMap(pl)
		}
		line(icLocation, first(pl.Name, pl.Address, "Location"), p.Link, "eventmap:"+m.ID)
	}
	if e.JoinLink != "" {
		if u.btn("eventjoin:"+m.ID).Clicked(gtx) && !openURL(e.JoinLink) {
			u.toast("Couldn't open the link.")
		}
		line(icon.Link, "Join call", p.Link, "eventjoin:"+m.ID)
	}
	if e.Description != "" {
		y += gtx.Dp(2)
		d := record(lg, u.label(14.5, e.Description, cols.text, labelOpts{maxLines: 6}).Layout)
		d.at(gtx, 0, y)
		y += d.size.Y + gtx.Dp(6)
	}

	// Who is going, beside the timestamp.
	var parts []string
	if e.Going > 0 {
		parts = append(parts, itoa(e.Going)+" going")
	}
	if e.Maybe > 0 {
		parts = append(parts, itoa(e.Maybe)+" maybe")
	}
	if s := rsvpLabel(e.Mine); s != "" {
		parts = append(parts, s)
	}
	if len(parts) > 0 {
		ag := gtx
		ag.Constraints = layout.Constraints{Max: image.Pt(max(0, w-metaW-gtx.Dp(8)), metaH)}
		a := record(ag, u.label(12.5, strings.Join(parts, " · "), cols.secondary).Layout)
		a.at(gtx, 0, y+(metaH-a.size.Y)/2)
	}
	return D{Size: image.Pt(w, y+metaH)}
}

// rsvpLabel is your answer to an event, as the card notes it.
func rsvpLabel(r model.RSVP) string {
	switch r {
	case model.RSVPGoing:
		return "You're going"
	case model.RSVPMaybe:
		return "You might go"
	case model.RSVPNotGoing:
		return "You're not going"
	}
	return ""
}

// eventWhen is when an event is: "Sunday, 6 October · 12:00 – 15:00", the
// year added when it isn't this one.
func eventWhen(e *model.EventInfo, now time.Time) string {
	s, en := e.Start.Local(), e.End.Local()
	layout := "Monday, 2 January"
	if s.Year() != now.Year() {
		layout += " 2006"
	}
	out := s.Format(layout) + " · " + s.Format("15:04")
	switch {
	case e.End.IsZero():
	case en.YearDay() == s.YearDay() && en.Year() == s.Year():
		out += " – " + en.Format("15:04")
	default:
		out += " – " + en.Format("2 Jan, 15:04")
	}
	return out
}

// pollVoters are the votes for option i, newest first.
func pollVoters(votes []model.Vote, i int) []model.Vote {
	var out []model.Vote
	for _, v := range votes {
		if slices.Contains(v.Options, i) {
			out = append(out, v)
		}
	}
	return out
}
