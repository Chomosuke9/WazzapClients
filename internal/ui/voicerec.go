package ui

import (
	"errors"
	"fmt"
	"image"
	"math"
	"os"
	"strings"
	"time"

	"gioui.org/io/event"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"

	"github.com/chomosuke9/wazzapclients/internal/model"
	recorder "github.com/chomosuke9/wazzapclients/internal/record"
)

// recState is the voice message being recorded: the composer's mic turns
// the composer into the recording bar (layoutRecordBar) until it is sent
// or thrown away.
type recState struct {
	sess   *recorder.Session
	chatID string
	// draft is the recording as a message while paused, for the voice
	// player to play back (its file is sess.Path, see draftPath). Each
	// pause makes a new one, with the length so far.
	draft  *model.Message
	seq    int
	levels []float32     // the live waveform's bars
	anim   tween         // the bar fading in
	done   chan recorded // recordings encoded, to send
}

// recorded is a recording encoded and ready to send, or why it isn't.
type recorded struct {
	chatID string
	reply  *model.Message
	path   string
	secs   int
	wave   []byte
	err    error
}

// micClicked starts recording if the composer's button is the mic (the
// composer is empty) and reports whether it was.
func (u *UI) micClicked() bool {
	c := &u.conv
	if u.selected == nil || u.postingStatus() || c.edit.msg != nil || len(u.attach.files) > 0 ||
		c.editorElsewhere || trimSpace(c.composer.Text()) != "" {
		return false
	}
	u.stopVoice()
	s, err := recorder.Start(u.images.invalidate)
	if err != nil {
		u.toast(recordProblem(err))
		return true
	}
	u.rec.sess, u.rec.chatID, u.rec.draft = s, u.selected.ID, nil
	u.rec.anim.snap(false)
	return true
}

// recordProblem words why recording didn't start, for a toast.
func recordProblem(err error) string {
	s := err.Error()
	if !errors.Is(err, recorder.ErrNoFFmpeg) && !errors.Is(err, recorder.ErrUnsupported) {
		s = "Couldn't record: " + s
	}
	s = strings.ToUpper(s[:1]) + s[1:]
	if !strings.HasSuffix(s, ".") {
		s += "."
	}
	return s
}

// recording reports whether the open chat's composer is the recording bar.
func (u *UI) recording() bool {
	return u.rec.sess != nil && u.selected != nil && u.selected.ID == u.rec.chatID
}

// draftPath is the file of m if it is the recording not sent yet.
func (u *UI) draftPath(m *model.Message) string {
	if r := &u.rec; r.sess != nil && r.draft != nil && m == r.draft {
		return r.sess.Path()
	}
	return ""
}

// updateRecording handles the recording bar's buttons and sends what
// was encoded.
func (u *UI) updateRecording(gtx C) {
	r := &u.rec
	for r.done != nil {
		select {
		case s := <-r.done:
			u.sendRecorded(s)
			continue
		default:
		}
		break
	}
	if r.sess == nil {
		return
	}
	if !u.recording() {
		u.discardRecording()
		return
	}
	if err := r.sess.Err(); err != nil && r.draft == nil {
		u.pauseRecording()
		u.toast("Recording stopped: " + err.Error())
	}
	if u.btn("rec:trash").Clicked(gtx) {
		u.discardRecording()
		return
	}
	if u.btn("rec:pause").Clicked(gtx) {
		if r.draft == nil {
			u.pauseRecording()
		} else {
			u.resumeRecording()
		}
	}
	if u.btn("rec:play").Clicked(gtx) && r.draft != nil {
		u.toggleVoice(r.draft)
	}
	if u.btn("rec:send").Clicked(gtx) {
		u.sendRecording()
		return
	}
	if r.draft == nil {
		// The timer and the waveform move on.
		gtx.Execute(op.InvalidateCmd{At: gtx.Now.Add(50 * time.Millisecond)})
	}
}

// pauseRecording pauses, and makes the draft to play back.
func (u *UI) pauseRecording() {
	r := &u.rec
	if err := r.sess.Pause(); err != nil {
		u.toast("Couldn't save the recording: " + err.Error())
	}
	r.seq++
	r.draft = &model.Message{
		ID: fmt.Sprintf("voice-draft-%d", r.seq), ChatID: r.chatID, FromMe: true, Media: model.MediaVoice,
		Duration: int(math.Round(r.sess.Elapsed().Seconds())), Waveform: r.sess.Waveform(),
	}
}

// resumeRecording records on after a pause.
func (u *UI) resumeRecording() {
	r := &u.rec
	u.stopDraft()
	if err := r.sess.Resume(); err != nil {
		u.toast(recordProblem(err))
		return
	}
	r.draft = nil
}

// stopDraft stops playing the draft back, which holds its file open.
func (u *UI) stopDraft() {
	if d := u.rec.draft; d != nil && u.voice.key == fileKey(d) {
		u.stopVoice()
	}
}

// discardRecording throws the recording away.
func (u *UI) discardRecording() {
	r := &u.rec
	if r.sess == nil {
		return
	}
	u.stopDraft()
	r.sess.Discard()
	r.sess, r.draft = nil, nil
}

// sendRecording stops recording and sends it once ffmpeg has encoded it,
// as a reply to the message being answered.
func (u *UI) sendRecording() {
	r := &u.rec
	u.stopDraft()
	s := r.sess
	r.sess, r.draft = nil, nil
	if r.done == nil {
		r.done = make(chan recorded, 4)
	}
	out, notify := r.done, u.images.invalidate
	job := recorded{chatID: r.chatID, reply: u.conv.reply}
	u.conv.reply = nil
	go func() {
		job.path, job.secs, job.wave, job.err = s.Finish()
		out <- job
		if notify != nil {
			notify()
		}
	}()
}

// sendRecorded sends an encoded recording.
func (u *UI) sendRecorded(s recorded) {
	if s.err != nil {
		u.toast("Couldn't send the voice message: " + s.err.Error())
		return
	}
	defer os.Remove(s.path) // read as it is sent
	a := model.Attachment{Path: s.path, Media: model.MediaVoice, Seconds: s.secs, Waveform: s.wave}
	m := u.backend.SendFile(s.chatID, a, model.Draft{Reply: s.reply})
	if m == nil {
		return
	}
	if u.chatByID(m.ChatID) == nil && u.selected != nil && u.selected.ID == m.ChatID {
		u.chats = append(u.chats, u.selected)
	}
	u.upsertMessage(m)
	if u.selected != nil && u.selected.ID == m.ChatID {
		u.scrollMessages(layout.Position{})
	}
}

// layoutRecordBar is the composer's row while recording, as WhatsApp
// lays it out at the right: delete, then a red dot, the time and the
// live waveform, or once paused the recording to play back; then pause
// (or the mic to go on) and send.
func (u *UI) layoutRecordBar(gtx C) D {
	p := u.pal
	r := &u.rec
	paused := r.draft != nil
	return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
		layout.Flexed(1, func(gtx C) D { return D{Size: image.Pt(gtx.Constraints.Max.X, 0)} }),
		layout.Rigid(func(gtx C) D { return u.iconButton(gtx, u.btn("rec:trash"), icDelete, 40, 24, p.IconStrong) }),
		layout.Rigid(layout.Spacer{Width: 8}.Layout),
		layout.Rigid(func(gtx C) D {
			if paused {
				return u.layoutRecordPreview(gtx, r.draft)
			}
			return u.layoutRecordLive(gtx)
		}),
		layout.Rigid(layout.Spacer{Width: 8}.Layout),
		layout.Rigid(func(gtx C) D {
			// Pause, a red ring around red bars; once paused, the mic
			// in red goes on recording.
			btn := u.btn("rec:pause")
			return clickable(gtx, btn, func(gtx C) D {
				sz := gtx.Dp(40)
				mid := image.Pt(sz/2, sz/2)
				if h := u.hover(gtx, btn); h > 0 {
					fillCircle(gtx, mid, sz/2, faded(p.Hover, h))
				}
				if paused {
					return centerIn(gtx, sz, iconW(icMic, 26, p.Danger))
				}
				fillCircle(gtx, mid, gtx.Dp(13), p.Danger)
				fillCircle(gtx, mid, gtx.Dp(11), p.Composer)
				return centerIn(gtx, sz, iconW(icPauseFill, 18, p.Danger))
			})
		}),
		layout.Rigid(layout.Spacer{Width: 4}.Layout),
		layout.Rigid(func(gtx C) D {
			btn := u.btn("rec:send")
			return clickable(gtx, btn, func(gtx C) D {
				sz := gtx.Dp(40)
				fillCircle(gtx, image.Pt(sz/2, sz/2), sz/2, p.Green)
				return centerIn(gtx, sz, iconW(icSend, 21, p.OnGreen))
			})
		}),
	)
}

// recWaveW is the width of the recording's waveform.
const recWaveW = 180

// layoutRecordLive is the recording's red dot, time and live waveform,
// its newest loudness at the right.
func (u *UI) layoutRecordLive(gtx C) D {
	p := u.pal
	r := &u.rec
	h := gtx.Dp(40)
	x := 0
	// The dot pulses.
	a := float32(1)
	if !gtx.Now.IsZero() {
		a = 0.6 + 0.4*float32(math.Cos(float64(gtx.Now.UnixMilli()%1200)/1200*2*math.Pi))
	}
	dot := gtx.Dp(10)
	fillCircle(gtx, image.Pt(dot/2, h/2), dot/2, faded(p.Danger, a))
	x += dot + gtx.Dp(8)
	lbl := record(gtx, u.label(15, clock(int(r.sess.Elapsed().Seconds())), p.Text).Layout)
	lbl.at(gtx, x, (h-lbl.size.Y)/2)
	// The time keeps its place as it grows.
	x += max(lbl.size.X, gtx.Sp(40)) + gtx.Dp(10)

	barW, gap := gtx.Dp(3), gtx.Dp(2)
	n := max(1, (gtx.Dp(recWaveW)+gap)/(barW+gap))
	if len(r.levels) != n {
		r.levels = make([]float32, n)
	}
	got := r.sess.Levels(r.levels)
	maxH, minH := gtx.Dp(24), gtx.Dp(3)
	for i, v := range r.levels {
		bh := max(minH, int(v*float32(maxH)))
		col := p.TextSecondary
		if i < n-got {
			col = faded(col, 0.4) // before the recording started
		}
		bx := x + i*(barW+gap)
		fillRRect(gtx, image.Rect(bx, h/2-bh/2, bx+barW, h/2-bh/2+bh), barW/2, col)
	}
	x += n*(barW+gap) - gap
	return D{Size: image.Pt(x, h)}
}

// layoutRecordPreview is the paused recording to play back: play or
// pause, its waveform (pressing it seeks) and the time.
func (u *UI) layoutRecordPreview(gtx C, m *model.Message) D {
	p := u.pal
	h := gtx.Dp(40)
	t, pos, playing, _ := u.voiceProgress(m)
	ic := icPlayFill
	if playing {
		ic = icPauseFill
	}
	btnSz := gtx.Dp(36)
	func() {
		defer op.Offset(image.Pt(0, (h-btnSz)/2)).Push(gtx.Ops).Pop()
		u.iconButton(gtx, u.btn("rec:play"), ic, 36, 30, p.IconStrong)
	}()
	tx0 := btnSz + gtx.Dp(6)
	barW, gap := gtx.Dp(3), gtx.Dp(2)
	n := max(1, (gtx.Dp(recWaveW)+gap)/(barW+gap))
	tw := n*(barW+gap) - gap
	maxH, minH := gtx.Dp(24), gtx.Dp(3)
	played, rest := p.Text, faded(p.TextSecondary, 0.55)
	for i, v := range waveform(m, n) {
		bh := max(minH, int(v*float32(maxH)))
		col := rest
		if float32(i)+0.5 < t*float32(n) {
			col = played
		}
		bx := tx0 + i*(barW+gap)
		fillRRect(gtx, image.Rect(bx, h/2-bh/2, bx+barW, h/2-bh/2+bh), barW/2, col)
	}
	if playing || t > 0 {
		knob := gtx.Dp(12)
		fillCircle(gtx, image.Pt(tx0+int(t*float32(tw)), h/2), knob/2, p.Green)
	}
	// Pressing the waveform plays from there.
	tag := u.btn("rec:seek")
	for {
		ev, ok := gtx.Event(pointer.Filter{Target: tag, Kinds: pointer.Press})
		if !ok {
			break
		}
		if e, ok := ev.(pointer.Event); ok && e.Buttons.Contain(pointer.ButtonPrimary) {
			u.seekVoice(m, min(1, max(0, (e.Position.X-float32(tx0))/float32(tw))), true)
		}
	}
	func() {
		defer clip.Rect(image.Rect(tx0, 0, tx0+tw, h)).Push(gtx.Ops).Pop()
		pointer.CursorPointer.Add(gtx.Ops)
		event.Op(gtx.Ops, tag)
	}()
	x := tx0 + tw + gtx.Dp(10)
	lbl := record(gtx, u.label(15, clock(pos), p.TextSecondary).Layout)
	lbl.at(gtx, x, (h-lbl.size.Y)/2)
	return D{Size: image.Pt(x+max(lbl.size.X, gtx.Sp(40)), h)}
}
