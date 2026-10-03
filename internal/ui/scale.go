package ui

import (
	"image"
	"slices"
	"strconv"
	"time"

	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/op"
)

// The app's zoom scales everything in the window, like WhatsApp Desktop's
// font size (Settings > General, or Ctrl with +, - and 0). It's an app
// preference, in percent.
const prefZoom = "zoom" // 100 if ""

// zoomLevels are the zooms to choose from; Ctrl+ and Ctrl- step through them.
var zoomLevels = []int{80, 90, 100, 110, 125, 150, 175, 200}

// zoomBubbleFor is how long the bubble showing the zoom stays after a
// change, unless the pointer is on it.
const zoomBubbleFor = 2 * time.Second

// zoomState is the zoom and the bubble that shows it after a shortcut.
type zoomState struct {
	pct     int // percent
	changed time.Time
	bubble  tween
	fieldH  int // the Font size field's height, for its menu
}

// loadZoom reads the stored zoom, falling back to 100 for anything that
// isn't one of the levels.
func (u *UI) loadZoom() {
	u.zoom.pct = 100
	if z, err := strconv.Atoi(u.backend.Pref(prefZoom)); err == nil && slices.Contains(zoomLevels, z) {
		u.zoom.pct = z
	}
}

// setZoom changes the zoom and remembers it.
func (u *UI) setZoom(z int) {
	if z == u.zoom.pct {
		return
	}
	u.zoom.pct = z
	v := strconv.Itoa(z)
	if z == 100 {
		v = ""
	}
	u.backend.SetPref(prefZoom, v)
	u.settings.stale = true
}

// stepZoom moves the zoom dir levels in (positive) or out.
func (u *UI) stepZoom(dir int) {
	i := slices.Index(zoomLevels, u.zoom.pct)
	if i < 0 {
		i = slices.Index(zoomLevels, 100)
	}
	u.setZoom(zoomLevels[min(len(zoomLevels)-1, max(0, i+dir))])
}

// zoomKeys handles Ctrl with + (or =), - and 0. Windows names the =
// key "+", with or without Shift. A change shows the bubble.
func (u *UI) zoomKeys(gtx C) {
	for {
		ev, ok := gtx.Event(
			key.Filter{Name: "+", Required: key.ModShortcut, Optional: key.ModShift},
			key.Filter{Name: "=", Required: key.ModShortcut, Optional: key.ModShift},
			key.Filter{Name: "-", Required: key.ModShortcut},
			key.Filter{Name: "0", Required: key.ModShortcut},
		)
		if !ok {
			break
		}
		e, ok := ev.(key.Event)
		if !ok || e.State != key.Press {
			continue
		}
		switch e.Name {
		case "+", "=":
			u.stepZoom(1)
		case "-":
			u.stepZoom(-1)
		case "0":
			u.setZoom(100)
		}
		u.zoom.changed = gtx.Now
	}
}

// applyZoom scales gtx's metric by the zoom.
func (u *UI) applyZoom(gtx C) C {
	if z := u.zoom.pct; z > 0 && z != 100 {
		k := float32(z) / 100
		gtx.Metric.PxPerDp *= k
		gtx.Metric.PxPerSp *= k
	}
	return gtx
}

// zoomLabel is a zoom as Settings shows it.
func zoomLabel(z int) string { return strconv.Itoa(z) + "%" }

// layoutZoomBubble draws the bubble at the top of the window that shows
// the zoom after a shortcut changed it, with buttons to zoom out and in.
// Like WhatsApp's, it isn't zoomed itself: gtx has the window's metric.
func (u *UI) layoutZoomBubble(gtx C) {
	z := &u.zoom
	if u.btn("zoom:out").Clicked(gtx) {
		u.stepZoom(-1)
		z.changed = gtx.Now
	}
	if u.btn("zoom:in").Clicked(gtx) {
		u.stepZoom(1)
		z.changed = gtx.Now
	}
	on := !z.changed.IsZero() && (u.hovered["zoombubble"] || gtx.Now.Sub(z.changed) < zoomBubbleFor)
	if on && !u.hovered["zoombubble"] {
		gtx.Execute(op.InvalidateCmd{At: z.changed.Add(zoomBubbleFor)})
	}
	v := z.bubble.step(gtx, on, popDur(on))
	if v == 0 {
		if !on {
			z.changed = time.Time{}
		}
		return
	}
	p := u.pal
	sz := image.Pt(gtx.Dp(234), gtx.Dp(46))
	pos := image.Pt((gtx.Constraints.Max.X-sz.X)/2, gtx.Dp(15))
	defer pushPopup(gtx, v, pos.Add(image.Pt(sz.X/2, 0))).Pop()
	if !on {
		var done func()
		gtx, done = fadeOut(gtx)
		defer done()
	}
	defer op.Offset(pos).Push(gtx.Ops).Pop()
	r := image.Rectangle{Max: sz}
	rad := gtx.Dp(8)
	fillRRect(gtx, r.Add(image.Pt(0, gtx.Dp(3))).Inset(-gtx.Dp(2)), rad+gtx.Dp(2), p.Shadow)
	borderRRect(gtx, r, rad, p.Popup, p.PopupBorder)
	u.hoverArea(gtx, "zoombubble", sz)
	gtx.Constraints = layout.Exact(sz)
	layout.Inset{Left: 14, Right: 7}.Layout(gtx, func(gtx C) D {
		return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
			layout.Flexed(1, func(gtx C) D {
				gtx.Constraints.Min.Y = 0
				return layout.W.Layout(gtx, u.label(14, strconv.Itoa(z.pct)+" %", p.Text, labelOpts{maxLines: 1}).Layout)
			}),
			layout.Rigid(func(gtx C) D { return u.zoomButton(gtx, "zoom:out", false) }),
			layout.Rigid(layout.Spacer{Width: 6}.Layout),
			layout.Rigid(func(gtx C) D { return u.zoomButton(gtx, "zoom:in", true) }),
		)
	})
}

// zoomButton is the bubble's − or + button.
func (u *UI) zoomButton(gtx C, key string, plus bool) D {
	p := u.pal
	c := u.btn(key)
	return clickable(gtx, c, func(gtx C) D {
		box := gtx.Dp(34)
		if h := u.hover(gtx, c); h > 0 {
			fillCircle(gtx, image.Pt(box/2, box/2), box/2, faded(p.Hover, h))
		}
		l, t := gtx.Dp(14), max(1, gtx.Dp(1.5))
		cx, cy := box/2, box/2
		fillRect(gtx, image.Rect(cx-l/2, cy-t/2, cx-l/2+l, cy-t/2+t), p.Icon)
		if plus {
			fillRect(gtx, image.Rect(cx-t/2, cy-l/2, cx-t/2+t, cy-l/2+l), p.Icon)
		}
		return D{Size: image.Pt(box, box)}
	})
}

// zoomField is the Font size dropdown on the General settings page.
func (u *UI) zoomField(gtx C) D {
	p := u.pal
	c := u.btn("settings:zoom")
	return layout.Inset{Left: 29.5, Right: 29, Top: 2, Bottom: 6}.Layout(gtx, func(gtx C) D {
		return clickable(gtx, c, func(gtx C) D {
			sz := image.Pt(gtx.Constraints.Max.X, gtx.Dp(54))
			u.zoom.fieldH = sz.Y
			r := image.Rectangle{Max: sz}
			bg := p.Panel
			if h := u.hover(gtx, c); h > 0 {
				bg = mix(p.Panel, p.Hover, h)
			}
			borderRRect(gtx, r, gtx.Dp(8), bg, p.Divider)
			gtx.Constraints = layout.Exact(sz)
			layout.Inset{Left: 20, Right: 16}.Layout(gtx, func(gtx C) D {
				return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
					layout.Flexed(1, func(gtx C) D {
						gtx.Constraints.Min.Y = 0
						return layout.W.Layout(gtx, u.label(16, zoomLabel(u.zoom.pct), p.Text, labelOpts{maxLines: 1}).Layout)
					}),
					layout.Rigid(iconW(icChevron, 24, p.TextSecondary)),
				)
			})
			return D{Size: sz}
		})
	})
}

// openZoomMenu opens the Font size choices under the field.
func (u *UI) openZoomMenu() {
	at := u.mouse
	if h := u.btn("settings:zoom").History(); len(h) > 0 {
		// The field's bottom left: the click, less where it was in it.
		at = at.Sub(h[len(h)-1].Position).Add(image.Pt(0, u.zoom.fieldH))
	}
	u.ctx = ctxMenu{kind: ctxZoom, at: at}
}

// zoomMenuItems are the Font size choices.
func (u *UI) zoomMenuItems() []menuItem {
	items := make([]menuItem, 0, len(zoomLevels))
	for _, z := range zoomLevels {
		items = append(items, menuItem{key: "zoom" + strconv.Itoa(z), label: zoomLabel(z),
			tick: z == u.zoom.pct, run: func() { u.setZoom(z) }})
	}
	return items
}
