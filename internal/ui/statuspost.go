package ui

import (
	"image"

	"gioui.org/f32"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/text"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"github.com/chomosuke9/wazzapclients/internal/filepick"
	"github.com/chomosuke9/wazzapclients/internal/model"
)

// Posting your own status updates. The Status page's ⊕ (and "My status"
// before you've posted anything) offers photos and videos, which go
// through the send view like files for a chat, with statusChatID as their
// chat, or text, which is written in a full-window composer on a colored
// background.

// postingStatus reports whether the send view holds status updates.
func (u *UI) postingStatus() bool {
	return len(u.attach.files) > 0 && u.attach.chatID == statusChatID
}

// statusFiles keeps the photos and videos among files: nothing else can be
// a status.
func (u *UI) statusFiles(files []*attachFile) []*attachFile {
	var keep []*attachFile
	for _, f := range files {
		if f.Media == model.MediaImage || f.Media == model.MediaVideo {
			keep = append(keep, f)
		} else {
			removeTemps([]*attachFile{f})
		}
	}
	if len(keep) < len(files) {
		u.toast("Only photos and videos can be posted to your status.")
	}
	return keep
}

// openStatusAdd opens the ⊕ menu at the pointer.
func (u *UI) openStatusAdd() {
	u.ctx = ctxMenu{kind: ctxStatusAdd, at: u.mouse}
}

func (u *UI) statusAddItems() []menuItem {
	return []menuItem{
		{key: "st-photos", ic: icPhotosFill, label: "Photos & videos", run: u.pickStatusMedia},
		{key: "st-text", ic: icEdit, label: "Text", run: u.openStatusText},
	}
}

func (u *UI) statusMenuItems() []menuItem {
	return []menuItem{
		{key: "st-privacy", ic: icLockOutline, label: "Status privacy", run: u.openStatusPrivacy},
	}
}

// pickStatusMedia opens the file dialog for photos and videos to post.
func (u *UI) pickStatusMedia() {
	if a := &u.attach; len(a.files) > 0 && a.chatID != statusChatID {
		u.toast("Send or close the files you're attaching to a chat first.")
		return
	}
	u.pickFiles(statusChatID, "Choose photos and videos", []filepick.Filter{
		{Name: "Photos and videos", Exts: append(append([]string(nil), photoExts...), videoExts...)},
	})
}

// openStatusPrivacy tells who sees your status updates. Hypermeow can
// read the setting but not change it, so changing it is left to the
// phone.
func (u *UI) openStatusPrivacy() {
	b := u.backend
	u.dialog = dialogState{kind: dialogConfirm, title: "Status privacy",
		bodyFn:  func() string { return statusPrivacyText(b.StatusPrivacy()) },
		buttons: []dialogButton{{label: "OK", primary: true}}}
}

func statusPrivacyText(p *model.StatusPrivacy) string {
	const change = "\n\nTo change who can see them, open Status privacy in WhatsApp on your phone. " +
		"Changes won't affect status updates you've already posted."
	people := func(n int) string {
		if n == 1 {
			return "1 contact"
		}
		return itoa(n) + " contacts"
	}
	switch {
	case p == nil:
		return "Checking who can see your status updates…"
	case p.Audience == model.AudienceExcept:
		return "Your status updates are shared with your contacts, except " + people(p.Count) + "." + change
	case p.Audience == model.AudienceOnly:
		return "Your status updates are shared only with " + people(p.Count) + "." + change
	}
	return "Your status updates are shared with all your contacts." + change
}

// statusColors are the backgrounds a text status cycles through, from
// WhatsApp's.
var statusColors = []uint32{
	0xff54c265, 0xff26c4dc, 0xff5696ff, 0xff8294ca, 0xff6e257e, 0xffa62c71,
	0xffff8a8c, 0xffc1a03f, 0xff90a841, 0xff7acba5, 0xff792138, 0xff243640,
}

// maxStatusText is how long a text status can be.
const maxStatusText = 700

// statusTextState is the text status composer.
type statusTextState struct {
	open    bool
	closing bool // fading out
	anim    tween
	ed      widget.Editor
	color   int // index in statusColors
}

func (s *statusTextState) isOpen() bool { return s.open && !s.closing }

func (u *UI) openStatusText() {
	s := &u.status.text
	s.open, s.closing = true, false
	s.ed.Submit, s.ed.MaxLen, s.ed.Alignment = true, maxStatusText, text.Middle
	s.ed.SetText("")
	s.color = int(u.now().Unix()/60) % len(statusColors)
	u.requestFocus(&s.ed)
}

func (u *UI) closeStatusText() {
	s := &u.status.text
	if s.isOpen() {
		s.closing = true
		u.requestFocus(nil)
	}
}

// postStatusText posts the typed text and closes the composer.
func (u *UI) postStatusText() {
	s := &u.status.text
	txt := trimSpace(s.ed.Text())
	if !s.isOpen() || txt == "" {
		return
	}
	u.backend.PostStatus(model.StatusPost{Text: txt, Background: statusColors[s.color]})
	u.closeStatusText()
}

// statusTextSize shrinks the text as it gets longer, like WhatsApp.
func statusTextSize(n int) unit.Sp {
	switch {
	case n <= 80:
		return 34
	case n <= 240:
		return 27
	}
	return 21
}

// layoutStatusText draws the text status composer over the whole window:
// the text in the middle of its background, close and color buttons at
// the top and the send button in the corner.
func (u *UI) layoutStatusText(gtx C) {
	s := &u.status.text
	if !s.open {
		return
	}
	p := u.pal
	if s.isOpen() {
		if u.btn("stt:close").Clicked(gtx) {
			u.closeStatusText()
		}
		if u.btn("stt:color").Clicked(gtx) {
			s.color = (s.color + 1) % len(statusColors)
			u.requestFocus(&s.ed)
		}
		for {
			ev, ok := s.ed.Update(gtx)
			if !ok {
				break
			}
			if _, ok := ev.(widget.SubmitEvent); ok {
				u.postStatusText()
			}
		}
		if u.btn("stt:send").Clicked(gtx) {
			u.postStatusText()
		}
	}
	a := s.anim.step(gtx, s.isOpen(), durDialog)
	if a == 0 && s.closing {
		s.open, s.closing = false, false
		s.ed.SetText("")
		return
	}
	if !s.isOpen() {
		var done func()
		gtx, done = fadeOut(gtx)
		defer done()
	}
	sz := gtx.Constraints.Max
	e := easeOut(a)
	// The background fades and the rest fades with it, as in the status
	// viewer.
	fillRect(gtx, image.Rectangle{Max: sz}, faded(argbColor(statusColors[s.color]), e))
	if s.isOpen() {
		// Nothing underneath takes clicks; a click anywhere types.
		bg := u.btn("stt:bg")
		if bg.Clicked(gtx) {
			u.requestFocus(&s.ed)
		}
		bgtx := gtx
		bgtx.Constraints = layout.Exact(sz)
		bg.Layout(bgtx, func(gtx C) D { return D{Size: gtx.Constraints.Max} })
	}
	defer pushFx(gtx, e, f32.Affine2D{}).Pop()
	white := rgb(0xffffff)

	tr := op.Offset(image.Pt(gtx.Dp(16), gtx.Dp(16))).Push(gtx.Ops)
	u.iconButton(gtx, u.btn("stt:close"), icClose, 48, 30, white)
	tr.Pop()
	tr = op.Offset(image.Pt(sz.X-gtx.Dp(64), gtx.Dp(16))).Push(gtx.Ops)
	u.iconButton(gtx, u.btn("stt:color"), icPalette, 48, 26, white)
	tr.Pop()

	// The text, centered in what's left.
	n := len([]rune(s.ed.Text()))
	w := min(sz.X-2*gtx.Dp(80), gtx.Dp(720))
	box := record(gtx, func(gtx C) D {
		gtx.Constraints = layout.Constraints{Min: image.Pt(w, 0), Max: image.Pt(w, max(0, sz.Y-gtx.Dp(200)))}
		ed := material.Editor(u.th, &s.ed, "Type a status")
		ed.TextSize = statusTextSize(n)
		ed.Color = white
		ed.HintColor = argb(0xffffff, 0xb0)
		ed.SelectionColor = argb(0xffffff, 0x50)
		return ed.Layout(gtx)
	})
	box.at(gtx, (sz.X-w)/2, (sz.Y-box.size.Y)/2)

	// Characters left, once it's getting long.
	if left := maxStatusText - n; left <= 100 {
		cnt := record(gtx, u.label(14, itoa(left), argb(0xffffff, 0xc0)).Layout)
		cnt.at(gtx, (sz.X-cnt.size.X)/2, (sz.Y+box.size.Y)/2+gtx.Dp(12))
	}

	if trimSpace(s.ed.Text()) != "" {
		d := gtx.Dp(60)
		tr := op.Offset(image.Pt(sz.X-gtx.Dp(32)-d, sz.Y-gtx.Dp(32)-d)).Push(gtx.Ops)
		c := u.btn("stt:send")
		clickable(gtx, c, func(gtx C) D {
			fillCircle(gtx, image.Pt(d/2, d/2), d/2, mix(p.Green, white, 0.12*u.hover(gtx, c)))
			return centerIn(gtx, d, iconW(icSend, 26, p.OnGreen))
		})
		tr.Pop()
	}
}
