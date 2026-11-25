package ui

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"time"

	"gioui.org/f32"
	"gioui.org/font"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

// Files uploading and downloading, as WhatsApp's phones show them. A
// picture or video that isn't here has a pill in its lower left corner
// that downloads it ("⬇ HD", or its size). While it moves the pill is a
// ring that fills up around an X, which cancels it, then how far it has
// got and how long is left ("71% (4s left)"); until it moves, just the X
// in a ring that spins. Once it's here the corner has the HD badge and a
// video's length. Documents, audio and the viewers use the ring alone.

// transferState is what's on its way, and what was stopped.
type transferState struct {
	m map[string]transferView // by fileKey
	// stopped are the pictures whose download was canceled, which don't
	// start again by themselves.
	stopped map[string]bool
}

type transferView struct {
	model.Transfer
	since time.Time // when it started, if it did while the window was open
}

// transferGrace is how long something that downloads by itself does so
// unseen: most pictures are here by then, and their pills only flickered.
const transferGrace = 300 * time.Millisecond

// loadTransfers takes over the transfers going on as the window opens.
func (u *UI) loadTransfers() {
	for _, t := range u.backend.Transfers() {
		u.transferEvent(model.TransferEvent{Transfer: t})
	}
}

func (u *UI) transferEvent(e model.TransferEvent) {
	ts := &u.transfers
	k := e.ChatID + "/" + e.MsgID
	if e.End {
		delete(ts.m, k)
		return
	}
	if ts.m == nil {
		ts.m = make(map[string]transferView)
	}
	v, on := ts.m[k]
	if !on {
		v.since = u.now()
	}
	v.Transfer = e.Transfer
	ts.m[k] = v
}

// transferOf returns the transfer of m's file, if it's on its way.
func (u *UI) transferOf(m *model.Message) (transferView, bool) {
	t, ok := u.transfers.m[fileKey(m)]
	return t, ok
}

// cancelTransfer stops the upload or download of m's file.
func (u *UI) cancelTransfer(m *model.Message) {
	t, ok := u.transferOf(m)
	if !ok {
		return
	}
	if !t.Upload && m.Media == model.MediaImage {
		if u.transfers.stopped == nil {
			u.transfers.stopped = make(map[string]bool)
		}
		u.transfers.stopped[fileKey(m)] = true
	}
	u.backend.CancelTransfer(m.ChatID, m.ID)
}

// downloadPicture downloads a picture, video or GIF, which shows in its
// bubble once it's here.
func (u *UI) downloadPicture(m *model.Message) {
	if m.Media == model.MediaImage {
		// It reads the disk first.
		go u.backend.MediaData(m.ChatID, m.ID)
		return
	}
	if m.Media == model.MediaGIF {
		if u.players.gifFetch == nil {
			u.players.gifFetch = make(map[string]gifFetch)
		}
		u.players.gifFetch[fileKey(m)] = gifFetching // gifDownloaded plays it
	}
	u.backend.MediaFile(m)
}

// offersDownload reports whether the picture, video or GIF of m waits to
// be downloaded.
func (u *UI) offersDownload(m *model.Message) bool {
	if m.FromMe && m.Receipt <= model.Pending {
		return false // yours, going out
	}
	switch m.Media {
	case model.MediaImage:
		return !u.photoWanted(m)
	case model.MediaVideo:
		return !u.fileReady(m)
	case model.MediaGIF:
		return !u.fileReady(m) && u.players.gifFetch[fileKey(m)] != gifFetching
	}
	return false
}

// transferPercent words how far t has got, and how long is left ("" while
// that isn't known).
func transferPercent(t model.Transfer) (pct, left string) {
	if t.Total <= 0 {
		return "", ""
	}
	pct = itoa(t.Percent()) + "%"
	switch s := int(t.Left / time.Second); {
	case s <= 0:
	case s < 60:
		left = fmt.Sprintf("(%ds left)", s)
	case s < 3600:
		left = fmt.Sprintf("(%dm left)", (s+30)/60)
	default:
		left = fmt.Sprintf("(%dh %dm left)", s/3600, s/60%60)
	}
	return pct, left
}

// transferText is how far t has got, on one line.
func transferText(t model.Transfer) string {
	pct, left := transferPercent(t)
	if left != "" {
		pct += " " + left
	}
	return pct
}

const (
	transferSpin  = 1100 * time.Millisecond // once around
	transferSweep = 0.28                    // of the ring that spins
)

// transferRing draws how far t has got around c, rad from it: an arc from
// the top on, in col, or a spinning one in idle while t doesn't move. It
// leaves what's in the ring to the caller.
func (u *UI) transferRing(gtx C, t model.Transfer, c f32.Point, rad, width float32, col, idle color.NRGBA) {
	rad -= width / 2
	if t.Total <= 0 {
		start := float32(-math.Pi / 2)
		if perf.animations && !u.rest.on && !gtx.Now.IsZero() {
			start += float32(gtx.Now.UnixNano()%int64(transferSpin)) / float32(transferSpin) * 2 * math.Pi
			gtx.Execute(op.InvalidateCmd{})
		}
		strokeArc(gtx, c, rad, start, transferSweep*2*math.Pi, width, idle)
		return
	}
	f := min(1, float32(t.Done)/float32(t.Total))
	f = u.follows.step(gtx, "xfer:"+t.ChatID+"/"+t.MsgID, f, durSlide)
	strokeArc(gtx, c, rad, -math.Pi/2, max(0.01, f)*2*math.Pi, width, col)
}

// transferButton draws t's ring with the X that cancels it in a box size
// dp across, in place of the button it stands for: the ring in the accent,
// the X in fg.
func (u *UI) transferButton(gtx C, key string, m *model.Message, t model.Transfer, size unit.Dp, fg color.NRGBA) D {
	btn := u.btn("xcancel:" + key)
	if btn.Clicked(gtx) {
		u.cancelTransfer(m)
	}
	s := gtx.Dp(size)
	g := gtx
	g.Constraints = layout.Exact(image.Pt(s, s))
	return clickable(g, btn, func(gtx C) D {
		if h := u.hover(gtx, btn); h > 0 {
			fillCircle(gtx, image.Pt(s/2, s/2), s/2, faded(fg, 0.1*h))
		}
		c := f32.Pt(float32(s)/2, float32(s)/2)
		u.transferRing(gtx, t, c, float32(s)/2-float32(gtx.Dp(2)), float32(max(1, gtx.Dp(2.2))), u.pal.Green, fg)
		return centerIn(gtx, s, iconW(icClose, size*0.6, fg))
	})
}

// Sizes of a picture's pill, after WhatsApp's on phones.
const (
	transferPillH  unit.Dp = 32
	transferMargin unit.Dp = 6
	transferRingSz unit.Dp = 26
)

// layoutPicture draws m's picture in a chat, at r, with the state of its
// file in its lower left corner; meta is the size of the message's time in
// the lower right one (0 when that's elsewhere). It returns the pill, to
// be drawn at 0, 0 over the button the picture itself is.
func (u *UI) layoutPicture(gtx C, r image.Rectangle, m *model.Message, img *imgEntry, meta image.Point) (pill part) {
	t, moving := u.transferOf(m)
	if moving && !t.Upload && m.Media != model.MediaVideo {
		if wait := transferGrace - u.now().Sub(t.since); wait > 0 {
			moving = false
			gtx.Execute(op.InvalidateCmd{At: gtx.Now.Add(wait)})
		}
	}
	offer := !moving && u.offersDownload(m)
	video := isVideo(m) && (img == nil || !img.live)
	hd := m.HD && !offer && !moving
	length := ""
	if video && m.Duration > 0 && !moving {
		length = fmtClock(time.Duration(m.Duration) * time.Second)
	}
	u.paintPicture(gtx, r, m, img)
	if hd || length != "" || offer || moving {
		pictureShade(gtx, r)
	}

	// The pill.
	margin := gtx.Dp(transferMargin)
	btn := u.btn("xfer:" + m.ID)
	clicked := btn.Clicked(gtx)
	switch {
	case moving:
		if clicked {
			u.cancelTransfer(m)
		}
		pill = u.transferPill(gtx, btn, t.Transfer, r.Dx()-meta.X-3*margin)
	case offer:
		if clicked {
			u.downloadPicture(m)
		}
		pill = u.downloadPill(gtx, btn, m)
	}
	// The play button, unless a small tile has no room for it over the pill.
	if rad := gtx.Dp(26); video && (pill.size.Y == 0 || r.Dy()/2-rad >= pill.size.Y+margin) {
		playButton(gtx, r.Min.Add(r.Size().Div(2)), rad, argb(0x000000, 0x80))
	}

	// The badge and the length, level with the time.
	x, mid := r.Min.X+gtx.Dp(8), r.Max.Y-gtx.Dp(5)-gtx.Dp(9)
	if meta.Y > 0 {
		mid = r.Max.Y - gtx.Dp(5) - meta.Y/2
	}
	if hd {
		x += u.hdBadge(gtx, x, mid) + gtx.Dp(6)
	}
	if sz := pill.size; sz.X > 0 {
		at := image.Pt(r.Min.X+margin, r.Max.Y-margin-sz.Y)
		x, mid = at.X+sz.X+gtx.Dp(8), at.Y+sz.Y/2
		p := pill
		pill = record(gtx, func(gtx C) D {
			p.at(gtx, at.X, at.Y)
			return D{Size: sz}
		})
	}
	if length != "" {
		l := record(gtx, u.label(12.5, length, rgb(0xffffff), labelOpts{maxLines: 1}).Layout)
		if x+l.size.X+margin <= r.Max.X-meta.X {
			l.at(gtx, x, mid-l.size.Y/2)
		}
	}
	return pill
}

// transferPill records the pill of t, at most room wide: the ring, and how
// far it has got when it moves and that fits.
func (u *UI) transferPill(gtx C, btn *widget.Clickable, t model.Transfer, room int) part {
	white := rgb(0xffffff)
	h, ring := gtx.Dp(transferPillH), gtx.Dp(transferRingSz)
	gap, pad := gtx.Dp(7), gtx.Dp(13)
	w := h
	var txt part
	for _, s := range [...]string{transferText(t), itoa(t.Percent()) + "%"} {
		if t.Total <= 0 {
			break
		}
		l := u.pillText(gtx, s)
		if tw := (h+ring)/2 + gap + l.size.X + pad; tw <= room {
			txt, w = l, tw
			break
		}
	}
	return u.pillButton(gtx, btn, image.Pt(w, h), func(gtx C) {
		c := f32.Pt(float32(h)/2, float32(h)/2)
		u.transferRing(gtx, t, c, float32(ring)/2, float32(max(1, gtx.Dp(2))), u.pal.Green, white)
		isz := gtx.Dp(18)
		defer op.Offset(image.Pt((h-isz)/2, (h-isz)/2)).Push(gtx.Ops).Pop()
		drawIcon(gtx, icClose, 18, white)
		if txt.size.X > 0 {
			txt.at(gtx, (h+ring)/2+gap-(h-isz)/2, (h-txt.size.Y)/2-(h-isz)/2)
		}
	})
}

// downloadPill records the pill that downloads m's picture: "HD", or the
// file's size.
func (u *UI) downloadPill(gtx C, btn *widget.Clickable, m *model.Message) part {
	s := formatSize(m.FileSize)
	if m.HD {
		s = "HD"
	}
	h, isz := gtx.Dp(transferPillH), gtx.Dp(20)
	w, gap, pad := h, gtx.Dp(6), gtx.Dp(9)
	var txt part
	if s != "" {
		txt = u.pillText(gtx, s)
		w = pad + isz + gap + txt.size.X + gtx.Dp(13)
	}
	return u.pillButton(gtx, btn, image.Pt(w, h), func(gtx C) {
		x := (h - isz) / 2
		if txt.size.X > 0 {
			x = pad
			txt.at(gtx, pad+isz+gap, (h-txt.size.Y)/2)
		}
		defer op.Offset(image.Pt(x, (h-isz)/2)).Push(gtx.Ops).Pop()
		drawIcon(gtx, icDownload, 20, rgb(0xffffff))
	})
}

func (u *UI) pillText(gtx C, s string) part {
	gtx.Constraints = layout.Constraints{Max: image.Pt(1<<20, 1<<20)}
	return record(gtx, u.label(14, s, rgb(0xffffff), labelOpts{weight: font.Medium, maxLines: 1}).Layout)
}

// pillButton records a dark pill, size big, that shades the picture under it,
// is btn and holds what draw draws.
func (u *UI) pillButton(gtx C, btn *widget.Clickable, size image.Point, draw func(gtx C)) part {
	gtx.Constraints = layout.Exact(size)
	return record(gtx, func(gtx C) D {
		return clickable(gtx, btn, func(gtx C) D {
			bg := mix(argb(0x000000, 0x90), argb(0x000000, 0xb8), u.hover(gtx, btn))
			fillRRect(gtx, image.Rectangle{Max: size}, size.Y/2, bg)
			draw(gtx)
			return D{Size: size}
		})
	})
}

// hdBadge draws the HD badge with its left end at x and its middle at mid,
// and returns its width.
func (u *UI) hdBadge(gtx C, x, mid int) int {
	gtx.Constraints.Min = image.Point{}
	l := record(gtx, u.label(10, "HD", rgb(0x111b21), labelOpts{weight: font.Bold, maxLines: 1}).Layout)
	w, h := l.size.X+gtx.Dp(6), gtx.Dp(14)
	y := mid - h/2
	fillRRect(gtx, image.Rect(x, y, x+w, y+h), gtx.Dp(3), argb(0xffffff, 0xf0))
	l.at(gtx, x+(w-l.size.X)/2, y+(h-l.size.Y)/2)
	return w
}

// pictureShade darkens the bottom of a picture in r, under what's written
// on it.
func pictureShade(gtx C, r image.Rectangle) {
	r.Min.Y = max(r.Min.Y, r.Max.Y-gtx.Dp(44))
	rad := gtx.Dp(6) // the picture's corners (paintPicture)
	defer clip.RRect{Rect: r, SE: rad, SW: rad}.Push(gtx.Ops).Pop()
	paint.LinearGradientOp{
		Stop1: f32.Pt(0, float32(r.Min.Y)), Color1: color.NRGBA{},
		Stop2: f32.Pt(0, float32(r.Max.Y)), Color2: color.NRGBA{A: 0x80},
	}.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
}
