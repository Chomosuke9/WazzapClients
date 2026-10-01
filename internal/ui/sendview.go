package ui

import (
	"image"
	"image/color"
	"math"
	"os"
	"path/filepath"
	"strings"

	"gioui.org/font"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/text"
	"gioui.org/unit"

	"github.com/chomosuke9/wazzapclients/internal/filepick"
	"github.com/chomosuke9/wazzapclients/internal/model"
	"github.com/chomosuke9/wazzapclients/internal/ui/icon"
)

// The send view, WhatsApp's media editor: picked, pasted or dropped files
// cover the conversation until they're sent. The top bar edits the photo
// on show, each file has its own caption (the composer's editor, swapped
// as you switch files), and the strip at the bottom picks a file, removes
// it or adds more.

// sendViewStep steps the send view's slide and returns how far it is in.
func (u *UI) sendViewStep(gtx C) float32 {
	a := &u.attach
	open := len(a.files) > 0
	v := a.anim.step(gtx, open, durPanel)
	if v == 0 && !open && a.ghost != nil {
		a.ghost = nil
		u.resetEditor()
	}
	return easeOut(v)
}

// sendTool is a button of the send view's top bar.
type sendTool struct {
	key  string
	ic   *icon.Icon
	tool editTool // the tool it picks, or toolNone
	on   bool
	run  func()
}

func (u *UI) sendTools(f *attachFile) []sendTool {
	a := &u.attach
	ed := &a.ed
	t := func(key string, ic *icon.Icon, tool editTool) sendTool {
		return sendTool{key: key, ic: ic, tool: tool, on: ed.tool == tool, run: func() { u.setTool(tool) }}
	}
	return []sendTool{
		t("crop", icon.CropRotate, toolCrop),
		t("filter", icWand, toolFilter),
		t("draw", icEdit, toolDraw),
		t("text", icon.MatchCase, toolText),
		t("shape", icon.CropSquare, toolShape),
		t("blur", icon.BlurOn, toolBlur),
		{key: "emoji", ic: icEmoji, run: func() {
			u.setTool(toolNone)
			u.openPicker(pickMedia, nil)
		}},
		{key: "sticker", ic: icSticker, run: func() {
			u.setTool(toolNone)
			u.openPicker(pickMedia, nil)
			u.picker.tab = tabSticker
		}},
		{key: "hd", ic: icon.Hd, on: a.quality != model.QualityStandard, run: func() { u.openQualityMenu() }},
	}
}

// icWand is a magic wand, for filters.
var icWand = func() *icon.Icon {
	star := func(x, y, r float32) string {
		f := func(v float32) string { return strings.TrimRight(strings.TrimRight(ftoa(v), "0"), ".") }
		return "M" + f(x) + " " + f(y-r) + "Q" + f(x) + " " + f(y) + " " + f(x+r) + " " + f(y) +
			"Q" + f(x) + " " + f(y) + " " + f(x) + " " + f(y+r) + "Q" + f(x) + " " + f(y) + " " + f(x-r) + " " + f(y) +
			"Q" + f(x) + " " + f(y) + " " + f(x) + " " + f(y-r) + "Z"
	}
	stick := "M3.3 18.9L13.6 8.6Q14.3 7.9 15 8.6L15.4 9Q16.1 9.7 15.4 10.4L5.1 20.7Q4.4 21.4 3.7 20.7L3.3 20.3Q2.6 19.6 3.3 18.9Z"
	ic, err := icon.Parse(stick+star(18.5, 4.5, 2.6)+star(20, 12, 1.8)+star(11, 3.5, 1.8), 0, 0, 24)
	if err != nil {
		panic(err)
	}
	return ic
}()

func ftoa(v float32) string {
	n := int(math.Round(float64(v) * 10))
	s := itoa(n / 10)
	if n < 0 && n > -10 {
		s = "-0"
	}
	return s + "." + itoa(iabs(n%10))
}

// layoutSendView draws the send view over the conversation pane, slid in
// by v.
func (u *UI) layoutSendView(gtx C, v float32) {
	a := &u.attach
	p := u.pal
	open := len(a.files) > 0
	files, cur := a.files, a.cur
	if !open {
		files, cur = a.ghost, a.ghostCur
	}
	if len(files) == 0 {
		return
	}
	cur = min(max(cur, 0), len(files)-1)
	f := files[cur]
	sz := gtx.Constraints.Max
	if open {
		u.sendViewInput(gtx, f)
		// The input may have changed the files, or closed the view.
		files, cur = a.files, a.cur
		if open = len(files) > 0; !open {
			files, cur = a.ghost, a.ghostCur
		}
		if len(files) == 0 {
			return
		}
		cur = min(max(cur, 0), len(files)-1)
		f = files[cur]
	}

	defer clip.Rect{Max: sz}.Push(gtx.Ops).Pop()
	defer op.Offset(image.Pt(0, int(float32(sz.Y)*(1-v)))).Push(gtx.Ops).Pop()
	if !open {
		var done func()
		gtx, done = fadeOut(gtx)
		defer done()
	}
	fillRect(gtx, image.Rectangle{Max: sz}, p.Viewer)
	// Block input to the chat underneath.
	u.btn("sv:bg").Layout(gtx, func(gtx C) D { return D{Size: sz} })

	isPhoto := f.Media == model.MediaImage
	topH := gtx.Dp(66)
	u.layoutSendTop(gtx, f, isPhoto, topH)

	// Tool options float under the top bar, in room kept for them so the
	// photo doesn't move as they come and go (the filters' taller bar
	// overlaps it a little).
	optBottom := topH
	var opts part
	if isPhoto {
		optBottom += gtx.Dp(52)
		if open {
			opts = u.layoutEditOptions(gtx, f)
		}
	}

	// The bottom strip and its divider.
	stripH := gtx.Dp(103)
	divY := sz.Y - stripH
	fillRect(gtx, image.Rect(0, divY, sz.X, divY+max(1, gtx.Dp(1))), p.ViewerDivider)
	u.layoutSendStrip(gtx, files, cur, divY, stripH)

	// The caption, with the view once button beside it.
	capBottom := divY - gtx.Dp(15)
	capTop := capBottom
	if f.Media != model.MediaAudio {
		capTop = u.layoutCaption(gtx, f, capBottom, open)
	}

	// The photo or the file, in what's left.
	area := image.Rect(gtx.Dp(40), optBottom+gtx.Dp(8), sz.X-gtx.Dp(40), capTop-gtx.Dp(20))
	if area.Dx() > gtx.Dp(40) && area.Dy() > gtx.Dp(40) {
		switch {
		case isPhoto && open:
			u.layoutCanvas(gtx, f, area)
		case isPhoto:
			// Sliding away: the last picture, without input.
			if ev := a.ed.view; ev != nil && ev.path == f.Path && ev.state == imgReady {
				region := f.edit.region(ev.orig, false)
				tr, _ := fitPhoto(region, f.edit.rot, area, false)
				u.drawPhoto(gtx, ev.pics, &f.edit, region, tr, -1)
			}
		default:
			u.layoutFileCard(gtx, f, area, "No preview available")
		}
	}

	if opts.size.Y > 0 {
		opts.at(gtx, (sz.X-opts.size.X)/2, topH)
	}

	if open && u.picker.shown() && u.picker.mode == pickComposer {
		m := op.Record(gtx.Ops)
		w := min(gtx.Dp(640), sz.X-gtx.Dp(24))
		u.layoutPicker(gtx, image.Pt((sz.X-w)/2, capTop-gtx.Dp(8)), w)
		op.Defer(gtx.Ops, m.Stop())
	}
}

// sendViewInput handles the send view's buttons.
func (u *UI) sendViewInput(gtx C, f *attachFile) {
	a := &u.attach
	if u.btn("sv:close").Clicked(gtx) {
		u.closeSendView(false)
		return
	}
	if f.Media == model.MediaImage {
		for _, t := range u.sendTools(f) {
			if u.btn("sv:tool:" + t.key).Clicked(gtx) {
				t.run()
			}
		}
		if u.btn("sv:undo").Clicked(gtx) && len(f.edit.hist) > 0 {
			u.finishTyping()
			f.edit.undo()
			a.ed.sel = -1
		}
		if u.btn("sv:copy").Clicked(gtx) {
			u.finishTyping()
			u.exportPhoto(f, sinkCopy)
		}
		if u.btn("sv:save").Clicked(gtx) {
			u.finishTyping()
			u.exportPhoto(f, sinkSave)
		}
	}
	if u.btn("sv:once").Clicked(gtx) {
		f.ViewOnce = !f.ViewOnce
		if f.ViewOnce {
			u.toast("Set to view once")
		}
	}
	if u.btn("sv:add").Clicked(gtx) {
		if f.Media == model.MediaImage || f.Media == model.MediaVideo {
			u.pickFiles(a.chatID, "Choose photos and videos", []filepick.Filter{
				{Name: "Photos and videos", Exts: append(append([]string(nil), photoExts...), videoExts...)},
			})
		} else {
			u.pickFiles(a.chatID, "Choose documents", nil)
		}
	}
	if u.btn("sv:send").Clicked(gtx) {
		u.sendComposer()
		return
	}
	for i := len(a.files) - 1; i >= 0; i-- {
		if u.btn("sv:x:" + itoa(i)).Clicked(gtx) {
			u.removeFile(i)
			if len(a.files) == 0 {
				return
			}
		}
	}
	for i := range a.files {
		if u.btn("sv:t:" + itoa(i)).Clicked(gtx) {
			u.showFile(i)
		}
	}
}

// layoutSendTop draws the top bar: close, the editing tools (photos) or
// the file's name, and undo, copy and save.
func (u *UI) layoutSendTop(gtx C, f *attachFile, isPhoto bool, h int) {
	p := u.pal
	sz := gtx.Constraints.Max
	button := func(key string, ic *icon.Icon, cx int, col, bg colorPair) {
		s := gtx.Dp(42)
		t := op.Offset(image.Pt(cx-s/2, (h-s)/2)).Push(gtx.Ops)
		defer t.Pop()
		cl := u.btn(key)
		clickable(gtx, cl, func(gtx C) D {
			if bg.on {
				fillCircle(gtx, image.Pt(s/2, s/2), s/2, p.Hover)
			} else if hv := u.hover(gtx, cl); hv > 0 {
				fillCircle(gtx, image.Pt(s/2, s/2), s/2, faded(p.Hover, hv))
			}
			c := p.IconStrong
			if col.on {
				c = p.Green
			}
			if col.off {
				c = p.EmptyIcon
			}
			return centerIn(gtx, s, iconW(ic, 24, c))
		})
	}
	button("sv:close", icClose, gtx.Dp(41), colorPair{}, colorPair{})
	if !isPhoto {
		name := record(gtx, func(gtx C) D {
			gtx.Constraints = layout.Constraints{Max: image.Pt(sz.X-gtx.Dp(200), h)}
			return u.label(15, filepath.Base(f.Path), p.Text, labelOpts{maxLines: 1}).Layout(gtx)
		})
		name.at(gtx, (sz.X-name.size.X)/2, (h-name.size.Y)/2)
		return
	}
	tools := u.sendTools(f)
	right := []struct {
		key string
		ic  *icon.Icon
		off bool
	}{
		{"sv:undo", icon.Undo, len(f.edit.hist) == 0},
		{"sv:copy", icCopy, false},
		{"sv:save", icDownload, false},
	}
	rp := gtx.Dp(44)
	x := sz.X - gtx.Dp(37) - (len(right)-1)*rp
	for _, r := range right {
		button(r.key, r.ic, x, colorPair{off: r.off}, colorPair{})
		x += rp
	}
	// The tools, centered, closer together when the pane is narrow.
	room := sz.X - 2*(gtx.Dp(70)+len(right)*rp)
	pitch := min(gtx.Dp(51), room/len(tools))
	if pitch < gtx.Dp(34) {
		pitch = gtx.Dp(34)
	}
	x = (sz.X-pitch*len(tools))/2 + pitch/2
	for _, t := range tools {
		button("sv:tool:"+t.key, t.ic, x, colorPair{on: t.on && t.tool == toolNone}, colorPair{on: t.on && t.tool != toolNone})
		x += pitch
	}
}

// colorPair flags how a top bar button is drawn.
type colorPair struct{ on, off bool }

// layoutEditOptions records the bar under the top bar for the tool in
// use (or the selected mark), or nothing.
func (u *UI) layoutEditOptions(gtx C, f *attachFile) part {
	a := &u.attach
	ed := &a.ed
	p := u.pal
	e := &f.edit
	selText := ed.sel >= 0 && ed.sel < len(e.marks) && e.marks[ed.sel].kind == markText
	var children []layout.FlexChild
	gap := layout.Rigid(layout.Spacer{Width: 6}.Layout)
	sep := layout.Rigid(func(gtx C) D {
		h := gtx.Dp(22)
		fillRect(gtx, image.Rect(gtx.Dp(8), (gtx.Dp(40)-h)/2, gtx.Dp(9), (gtx.Dp(40)+h)/2), p.PopupDivider)
		return D{Size: image.Pt(gtx.Dp(17), gtx.Dp(40))}
	})
	textBtn := func(key, label string, run func()) layout.FlexChild {
		if u.btn(key).Clicked(gtx) {
			run()
		}
		return layout.Rigid(func(gtx C) D {
			cl := u.btn(key)
			return clickable(gtx, cl, func(gtx C) D {
				lab := record(gtx, u.label(14, label, p.Text, labelOpts{weight: font.Medium}).Layout)
				w, h := lab.size.X+gtx.Dp(24), gtx.Dp(32)
				top := (gtx.Dp(40) - h) / 2
				if hv := u.hover(gtx, cl); hv > 0 {
					fillRRect(gtx, image.Rect(0, top, w, top+h), h/2, faded(p.Hover, hv))
				}
				lab.at(gtx, gtx.Dp(12), (gtx.Dp(40)-lab.size.Y)/2)
				return D{Size: image.Pt(w, gtx.Dp(40))}
			})
		})
	}
	iconBtn := func(key string, ic *icon.Icon, run func()) layout.FlexChild {
		if u.btn(key).Clicked(gtx) {
			run()
		}
		return layout.Rigid(func(gtx C) D {
			return centerIn2(gtx, gtx.Dp(40), gtx.Dp(40), func(gtx C) D {
				return u.iconButton(gtx, u.btn(key), ic, 34, 22, p.IconStrong)
			})
		})
	}
	colors := func() {
		for i, col := range markColors {
			key := "sv:col:" + itoa(i)
			if u.btn(key).Clicked(gtx) {
				u.pickColor(i)
			}
			children = append(children, layout.Rigid(func(gtx C) D {
				cl := u.btn(key)
				return clickable(gtx, cl, func(gtx C) D {
					s := gtx.Dp(30)
					mid := image.Pt(s/2, gtx.Dp(20))
					r := gtx.Dp(9)
					if i == ed.color {
						fillCircle(gtx, mid, r+gtx.Dp(4), p.IconStrong)
						fillCircle(gtx, mid, r+gtx.Dp(2), p.Popup)
					} else if hv := u.hover(gtx, cl); hv > 0 {
						r += int(float32(gtx.Dp(2)) * hv)
					}
					fillCircle(gtx, mid, r+max(1, gtx.Dp(0.5)), p.PopupBorder)
					fillCircle(gtx, mid, r, col)
					return D{Size: image.Pt(s, gtx.Dp(40))}
				})
			}))
		}
	}
	widths := func(n int) {
		for i := range n {
			key := "sv:w:" + itoa(i)
			if u.btn(key).Clicked(gtx) {
				ed.width = i
			}
			children = append(children, layout.Rigid(func(gtx C) D {
				cl := u.btn(key)
				return clickable(gtx, cl, func(gtx C) D {
					s := gtx.Dp(32)
					mid := image.Pt(s/2, gtx.Dp(20))
					if i == ed.width {
						fillCircle(gtx, mid, gtx.Dp(14), p.Hover)
					} else if hv := u.hover(gtx, cl); hv > 0 {
						fillCircle(gtx, mid, gtx.Dp(14), faded(p.Hover, hv))
					}
					fillCircle(gtx, mid, gtx.Dp(unit.Dp(2+3*i)), p.IconStrong)
					return D{Size: image.Pt(s, gtx.Dp(40))}
				})
			}))
		}
	}
	switch {
	case ed.tool == toolCrop:
		children = append(children,
			iconBtn("sv:rotate", icon.RotateLeft, func() { e.push(); e.rot = (e.rot + 3) % 4 }),
			iconBtn("sv:cropreset", icon.RestartAlt, func() {
				if ed.view != nil {
					e.push()
					e.rot, e.crop = 0, image.Rectangle{Max: ed.view.orig}
				}
			}),
			sep,
			textBtn("sv:cropdone", "Done", func() { u.setTool(toolNone) }))
	case ed.tool == toolFilter:
		thumbs := u.filterThumbs(ed.view)
		for k, ft := range photoFilters {
			key := "sv:filter:" + itoa(k)
			if u.btn(key).Clicked(gtx) && e.filter != k {
				e.push()
				e.filter = k
			}
			children = append(children, layout.Rigid(func(gtx C) D {
				cl := u.btn(key)
				return clickable(gtx, cl, func(gtx C) D {
					s := gtx.Dp(56)
					w := s + gtx.Dp(10)
					r := image.Rect(gtx.Dp(5), gtx.Dp(6), gtx.Dp(5)+s, gtx.Dp(6)+s)
					if k < len(thumbs) {
						func() {
							defer clip.UniformRRect(r, gtx.Dp(6)).Push(gtx.Ops).Pop()
							paintCover(gtx, thumbs[k], ed.view.thumbN, r)
						}()
					}
					if e.filter == k {
						b := float32(max(1, gtx.Dp(2)))
						strokeRRect(gtx, r, gtx.Dp(6), b, p.Green)
					} else if hv := u.hover(gtx, cl); hv > 0 {
						fillRRect(gtx, r, gtx.Dp(6), faded(argb(0xffffff, 0x30), hv))
					}
					col := p.TextSecondary
					if e.filter == k {
						col = p.Green
					}
					lab := record(gtx, u.label(12, ft.name, col, labelOpts{maxLines: 1}).Layout)
					lab.at(gtx, (w-lab.size.X)/2, r.Max.Y+gtx.Dp(4))
					return D{Size: image.Pt(w, r.Max.Y+gtx.Dp(6)+lab.size.Y)}
				})
			}))
		}
	case ed.tool == toolDraw || ed.tool == toolShape:
		colors()
		children = append(children, sep)
		widths(3)
	case ed.tool == toolBlur:
		widths(3)
	case ed.tool == toolText || selText:
		colors()
		if selText {
			children = append(children, sep, iconBtn("sv:del", icDelete, u.deleteSelected))
		}
	case ed.sel >= 0 && ed.sel < len(e.marks):
		children = append(children, iconBtn("sv:del", icDelete, u.deleteSelected))
	default:
		return part{}
	}
	children = append([]layout.FlexChild{gap}, append(children, gap)...)
	bar := record(gtx, func(gtx C) D {
		gtx.Constraints.Min = image.Point{}
		return layout.Flex{Alignment: layout.Middle}.Layout(gtx, children...)
	})
	if bar.size.X == 0 {
		return part{}
	}
	return record(gtx, func(gtx C) D {
		pad := gtx.Dp(4)
		size := bar.size.Add(image.Pt(2*pad, 2*pad))
		rad := min(size.Y/2, gtx.Dp(22))
		r := image.Rectangle{Max: size}
		fillRRect(gtx, r.Add(image.Pt(0, gtx.Dp(2))).Inset(-gtx.Dp(1)), rad+gtx.Dp(1), p.Shadow)
		borderRRect(gtx, r, rad, p.Popup, p.PopupBorder)
		bar.at(gtx, pad, pad)
		return D{Size: size}
	})
}

// strokeRRect outlines a rounded rectangle.
func strokeRRect(gtx C, r image.Rectangle, radius int, width float32, col color.NRGBA) {
	rr := clip.UniformRRect(r, radius)
	paint.FillShape(gtx.Ops, col, clip.Stroke{Path: rr.Path(gtx.Ops), Width: width}.Op())
}

// layoutCaption draws the caption field with its bottom at bottom, and
// returns its top.
func (u *UI) layoutCaption(gtx C, f *attachFile, bottom int, open bool) int {
	p := u.pal
	c := &u.conv
	sz := gtx.Constraints.Max
	// Statuses can't be view once.
	once := (f.Media == model.MediaImage || f.Media == model.MediaVideo) && u.attach.chatID != statusChatID
	onceW := 0
	if once {
		onceW = gtx.Dp(40 + 14)
	}
	w := min(gtx.Dp(640), sz.X-gtx.Dp(48)-onceW)
	x := (sz.X - w - onceW) / 2
	box := record(gtx, func(gtx C) D {
		gtx.Constraints = layout.Constraints{Min: image.Pt(w, 0), Max: image.Pt(w, gtx.Dp(200))}
		return layout.Inset{Left: 17, Right: 4}.Layout(gtx, func(gtx C) D {
			return vcenter(gtx, gtx.Dp(47), func(gtx C) D {
				return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
					layout.Flexed(1, func(gtx C) D {
						gtx.Constraints.Min.Y = gtx.Dp(47)
						if !open {
							return vcenter(gtx, gtx.Dp(47), u.label(16, "Type a message", p.ComposerHint).Layout)
						}
						return u.layoutComposerEditor(gtx, "Type a message")
					}),
					layout.Rigid(func(gtx C) D {
						col := p.Icon
						if u.picker.open && u.picker.mode == pickComposer {
							col = p.Green
						}
						return u.iconButton(gtx, &c.emoji, icEmoji, 40, 24, col)
					}),
				)
			})
		})
	})
	top := bottom - box.size.Y
	fillRRect(gtx, image.Rect(x, top, x+w, bottom), gtx.Dp(8), p.Composer)
	box.at(gtx, x, top)

	// The mention picker rises above it.
	if ms := u.mentionQuery(); ms != nil && open {
		pk := record(gtx, func(gtx C) D {
			gtx.Constraints = layout.Constraints{Min: image.Pt(w, 0), Max: image.Pt(w, top)}
			return u.layoutMentionPicker(gtx, ms)
		})
		pk.at(gtx, x, top-pk.size.Y-gtx.Dp(8))
	}

	if once {
		s := gtx.Dp(40)
		t := op.Offset(image.Pt(x+w+gtx.Dp(14), bottom-gtx.Dp(47)+(gtx.Dp(47)-s)/2)).Push(gtx.Ops)
		cl := u.btn("sv:once")
		clickable(gtx, cl, func(gtx C) D {
			mid := image.Pt(s/2, s/2)
			if hv := u.hover(gtx, cl); hv > 0 && !f.ViewOnce {
				fillCircle(gtx, mid, s/2, faded(p.Hover, hv))
			}
			drawViewOnce(gtx, u, mid, gtx.Dp(12), f.ViewOnce)
			return D{Size: image.Pt(s, s)}
		})
		t.Pop()
	}
	return top
}

// drawViewOnce draws WhatsApp's view once badge, a dashed circle around a
// 1, filled green when on.
func drawViewOnce(gtx C, u *UI, mid image.Point, r int, on bool) {
	p := u.pal
	c := pointF(mid)
	w := float32(max(1, gtx.Dp(1.8)))
	col := p.IconStrong
	if on {
		fillCircle(gtx, mid, r+gtx.Dp(2), p.Green)
		col = p.OnGreen
	} else {
		// A full arc at the top, then dashes round to it.
		strokeArc(gtx, c, float32(r), -math.Pi*0.9, math.Pi*1.3, w, col)
		for i := range 3 {
			a := math.Pi*0.55 + float32(i)*math.Pi*0.22
			strokeArc(gtx, c, float32(r), a, math.Pi*0.1, w, col)
		}
	}
	one := record(gtx, func(gtx C) D {
		gtx.Constraints.Min = image.Point{}
		return u.label(11, "1", col, labelOpts{weight: font.Bold}).Layout(gtx)
	})
	one.at(gtx, mid.X-one.size.X/2, mid.Y-one.size.Y/2)
}

// layoutSendStrip draws the thumbnails, the add button and the send
// button in the strip of height h at y.
func (u *UI) layoutSendStrip(gtx C, files []*attachFile, cur, y, h int) {
	p := u.pal
	sz := gtx.Constraints.Max
	s, add, gap := gtx.Dp(58), gtx.Dp(55), gtx.Dp(10)
	total := len(files)*(s+gap) + add
	// Too many to fit: the current one stays in view.
	room := sz.X - 2*gtx.Dp(100)
	x := (sz.X - total) / 2
	if total > room {
		x = gtx.Dp(100) - max(0, min(cur*(s+gap)+s/2-room/2, total-room))
	}
	func() {
		defer clip.Rect{Min: image.Pt(gtx.Dp(90), y), Max: image.Pt(sz.X-gtx.Dp(90), y+h)}.Push(gtx.Ops).Pop()
		ty := y + (h-s)/2
		for i, f := range files {
			t := op.Offset(image.Pt(x, ty)).Push(gtx.Ops)
			u.sendThumb(gtx, i, f, i == cur, s)
			t.Pop()
			x += s + gap
		}
		t := op.Offset(image.Pt(x, y+(h-add)/2)).Push(gtx.Ops)
		cl := u.btn("sv:add")
		clickable(gtx, cl, func(gtx C) D {
			r := image.Rect(0, 0, add, add)
			borderRRect(gtx, r, gtx.Dp(4), mix(p.Viewer, p.Hover, u.hover(gtx, cl)), p.TextSecondary)
			return centerIn(gtx, add, iconW(icAdd, 28, p.IconStrong))
		})
		t.Pop()
	}()

	// Send, with the number of files.
	bs := gtx.Dp(60)
	bx, by := sz.X-gtx.Dp(16)-bs, y+(h-bs)/2
	t := op.Offset(image.Pt(bx, by)).Push(gtx.Ops)
	cl := u.btn("sv:send")
	clickable(gtx, cl, func(gtx C) D {
		mid := image.Pt(bs/2, bs/2)
		fillCircle(gtx, mid, bs/2, mix(p.Green, rgb(0xffffff), 0.12*u.hover(gtx, cl)))
		return centerIn(gtx, bs, iconW(icSend, 28, p.OnGreen))
	})
	if len(files) > 1 {
		b := record(gtx, func(gtx C) D { return u.badge(gtx, len(files)) })
		fillCircle(gtx, image.Pt(bs-b.size.X/2, b.size.Y/2), b.size.Y/2+gtx.Dp(2), p.Viewer)
		b.at(gtx, bs-b.size.X, 0)
	}
	t.Pop()
}

// sendThumb draws file i of the strip, with a remove button on hover.
func (u *UI) sendThumb(gtx C, i int, f *attachFile, selected bool, s int) {
	p := u.pal
	key := itoa(i)
	hov := u.hoverArea(gtx, "sv:th:"+key, image.Pt(s, s))
	cl := u.btn("sv:t:" + key)
	r := image.Rect(0, 0, s, s)
	rad := gtx.Dp(6)
	clickable(gtx, cl, func(gtx C) D {
		func() {
			defer clip.UniformRRect(r, rad).Push(gtx.Ops).Pop()
			fillRect(gtx, r, p.ViewerThumb)
			if f.Media != model.MediaImage {
				ic, col := fileIcon(f.Media)
				t := op.Offset(image.Pt((s-gtx.Dp(28))/2, gtx.Dp(8))).Push(gtx.Ops)
				drawIcon(gtx, ic, 28, col)
				t.Pop()
				ext := strings.ToUpper(strings.TrimPrefix(filepath.Ext(f.Path), "."))
				if ext != "" {
					lab := record(gtx, func(gtx C) D {
						gtx.Constraints.Max.X = s - gtx.Dp(6)
						return u.label(10, ext, p.TextSecondary, labelOpts{weight: font.Bold, maxLines: 1}).Layout(gtx)
					})
					lab.at(gtx, (s-lab.size.X)/2, s-gtx.Dp(6)-lab.size.Y)
				}
				return
			}
			path := f.Path
			e := u.images.get("f:"+path, gtx.Dp(120), func() []byte {
				data, _ := os.ReadFile(path)
				return data
			})
			if e.state != imgReady {
				return
			}
			if f.edit.edited() && f.orig.X > 0 {
				region := f.edit.region(f.orig, false)
				tr, _ := fitPhoto(region, f.edit.rot, r, true)
				u.drawPhoto(gtx, photoPics{img: e.op, size: e.size, cover: image.Rectangle{Max: f.orig}}, &f.edit, region, tr, -1)
				return
			}
			paintCover(gtx, e.op, e.size, r)
		}()
		if selected {
			strokeRRect(gtx, r.Inset(gtx.Dp(1)), rad-gtx.Dp(1), float32(max(2, gtx.Dp(2.5))), p.Green)
		} else if hv := u.hover(gtx, cl); hv > 0 {
			fillRRect(gtx, r, rad, faded(argb(0xffffff, 0x20), hv))
		}
		return D{Size: r.Size()}
	})
	if hov {
		// Drawn after the thumbnail, so it takes its own clicks.
		b := gtx.Dp(22)
		t := op.Offset(image.Pt(s-b-gtx.Dp(3), gtx.Dp(3))).Push(gtx.Ops)
		xc := u.btn("sv:x:" + key)
		clickable(gtx, xc, func(gtx C) D {
			fillCircle(gtx, image.Pt(b/2, b/2), b/2, mix(argb(0, 0xb0), argb(0x404040, 0xe0), u.hover(gtx, xc)))
			return centerIn(gtx, b, iconW(icClose, 16, rgb(0xffffff)))
		})
		t.Pop()
	}
}

// fileIcon is the icon and color a file is shown with.
func fileIcon(m model.Media) (*icon.Icon, color.NRGBA) {
	switch m {
	case model.MediaVideo:
		return icVideo, attachPhotos
	case model.MediaAudio:
		return icHeadphonesFill, attachAudio
	case model.MediaImage:
		return icImage, attachPhotos
	}
	return icDocumentFill, attachDocument
}

// layoutFileCard shows a file without a preview in area: its icon, name,
// size and type.
func (u *UI) layoutFileCard(gtx C, f *attachFile, area image.Rectangle, note string) {
	p := u.pal
	ic, col := fileIcon(f.Media)
	size := ""
	if st, err := os.Stat(f.Path); err == nil {
		size = formatSize(st.Size())
	}
	if ext := strings.ToUpper(strings.TrimPrefix(filepath.Ext(f.Path), ".")); ext != "" {
		if size != "" {
			size += " · "
		}
		size += ext
	}
	card := record(gtx, func(gtx C) D {
		gtx.Constraints = layout.Constraints{Max: image.Pt(min(area.Dx(), gtx.Dp(420)), area.Dy())}
		return layout.Flex{Axis: layout.Vertical, Alignment: layout.Middle}.Layout(gtx,
			layout.Rigid(func(gtx C) D {
				s := gtx.Dp(120)
				fillRRect(gtx, image.Rect(0, 0, s, s), gtx.Dp(16), p.ViewerThumb)
				t := op.Offset(image.Pt((s-gtx.Dp(72))/2, (s-gtx.Dp(72))/2)).Push(gtx.Ops)
				drawIcon(gtx, ic, 72, col)
				t.Pop()
				return D{Size: image.Pt(s, s)}
			}),
			layout.Rigid(layout.Spacer{Height: 20}.Layout),
			layout.Rigid(u.label(17, filepath.Base(f.Path), p.Text, labelOpts{weight: font.Medium, maxLines: 2, align: text.Middle}).Layout),
			layout.Rigid(layout.Spacer{Height: 6}.Layout),
			layout.Rigid(u.label(14, size, p.TextSecondary, labelOpts{maxLines: 1}).Layout),
			layout.Rigid(layout.Spacer{Height: 14}.Layout),
			layout.Rigid(u.label(14, note, p.TextSecondary, labelOpts{maxLines: 1}).Layout),
		)
	})
	card.at(gtx, area.Min.X+(area.Dx()-card.size.X)/2, area.Min.Y+(area.Dy()-card.size.Y)/2)
}

// layoutDropHint covers the pane while files are dragged over the window.
func (u *UI) layoutDropHint(gtx C) {
	a := &u.attach
	on := u.host != nil && u.host.dragging.Load()
	v := easeOut(a.dropAnim.step(gtx, on, durSwitch))
	if v == 0 {
		return
	}
	p := u.pal
	sz := gtx.Constraints.Max
	fillRect(gtx, image.Rectangle{Max: sz}, faded(p.Panel, 0.94*v))
	r := image.Rectangle{Max: sz}.Inset(gtx.Dp(20))
	// Dashes round the edge.
	col := faded(p.Green, v)
	dash, gapL, th := gtx.Dp(14), gtx.Dp(10), max(1, gtx.Dp(2))
	for x := r.Min.X; x < r.Max.X; x += dash + gapL {
		e := min(x+dash, r.Max.X)
		fillRect(gtx, image.Rect(x, r.Min.Y, e, r.Min.Y+th), col)
		fillRect(gtx, image.Rect(x, r.Max.Y-th, e, r.Max.Y), col)
	}
	for y := r.Min.Y; y < r.Max.Y; y += dash + gapL {
		e := min(y+dash, r.Max.Y)
		fillRect(gtx, image.Rect(r.Min.X, y, r.Min.X+th, e), col)
		fillRect(gtx, image.Rect(r.Max.X-th, y, r.Max.X, e), col)
	}
	msg := record(gtx, func(gtx C) D {
		return layout.Flex{Axis: layout.Vertical, Alignment: layout.Middle}.Layout(gtx,
			layout.Rigid(func(gtx C) D {
				s := gtx.Dp(88)
				fillCircle(gtx, image.Pt(s/2, s/2), s/2, faded(p.Green, 0.15*v))
				return centerIn(gtx, s, iconW(icAttach, 44, faded(p.Green, v)))
			}),
			layout.Rigid(layout.Spacer{Height: 18}.Layout),
			layout.Rigid(u.label(20, "Drop files here", faded(p.Text, v), labelOpts{weight: font.Medium}).Layout),
			layout.Rigid(layout.Spacer{Height: 6}.Layout),
			layout.Rigid(u.label(14, "Photos and videos go as media, anything else as a document", faded(p.TextSecondary, v)).Layout),
		)
	})
	msg.at(gtx, (sz.X-msg.size.X)/2, (sz.Y-msg.size.Y)/2)
}
