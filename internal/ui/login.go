package ui

import (
	"image"
	"image/color"

	"gioui.org/font"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/widget/material"
	"rsc.io/qr"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

var loginSteps = []string{
	"Open WhatsApp on your phone",
	"On Android tap Menu, on iPhone tap Settings",
	"Tap Linked devices, then Link a device",
	"Point your phone at this screen to scan the QR code",
}

func (u *UI) updateLogin(gtx C) {
	if u.login.retry.Clicked(gtx) {
		u.backend.Retry()
	}
}

// layoutLogin is the device-linking screen shown while there is no session.
func (u *UI) layoutLogin(gtx C) D {
	p := u.pal
	bg := p.Frame
	dims := fill(gtx, bg)
	gtx.Constraints.Min = gtx.Constraints.Max

	layout.Center.Layout(gtx, func(gtx C) D {
		gtx.Constraints.Max.X = min(gtx.Constraints.Max.X-gtx.Dp(64), gtx.Dp(1000))
		return layout.Flex{Axis: layout.Vertical, Alignment: layout.Middle}.Layout(gtx,
			layout.Rigid(u.layoutLoginCard),
			layout.Rigid(layout.Spacer{Height: 28}.Layout),
			layout.Rigid(func(gtx C) D {
				return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(func(gtx C) D { return drawIcon(gtx, icLock, 14, p.TextSecondary) }),
					layout.Rigid(layout.Spacer{Width: 5}.Layout),
					layout.Rigid(u.label(13, "Your personal messages are end-to-end encrypted", p.TextSecondary).Layout),
				)
			}),
		)
	})
	return dims
}

func (u *UI) layoutLoginCard(gtx C) D {
	p := u.pal
	gtx.Constraints.Min.X = gtx.Constraints.Max.X
	m := op.Record(gtx.Ops)
	dims := layout.Inset{Left: 56, Right: 56, Top: 52, Bottom: 52}.Layout(gtx, func(gtx C) D {
		return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
			layout.Flexed(1, func(gtx C) D {
				children := []layout.FlexChild{
					layout.Rigid(u.label(28, "Log in to WazzapClients", p.Text, labelOpts{weight: font.Light, maxLines: 1}).Layout),
					layout.Rigid(layout.Spacer{Height: 32}.Layout),
				}
				for i, s := range loginSteps {
					i, s := i, s
					children = append(children,
						layout.Rigid(func(gtx C) D { return u.loginStep(gtx, i+1, s) }),
						layout.Rigid(layout.Spacer{Height: 20}.Layout),
					)
				}
				if u.conn.State == model.StateError && u.conn.Err != "" {
					children = append(children, layout.Rigid(
						u.label(14, u.conn.Err, rgb(0xea0038), labelOpts{maxLines: 3}).Layout))
				}
				return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
			}),
			layout.Rigid(layout.Spacer{Width: 48}.Layout),
			layout.Rigid(u.layoutQR),
		)
	})
	call := m.Stop()
	r := gtx.Dp(20)
	fillRRect(gtx, image.Rectangle{Max: dims.Size}.Inset(-1), r+1, p.PanelBorder)
	fillRRect(gtx, image.Rectangle{Max: dims.Size}, r, p.Panel)
	call.Add(gtx.Ops)
	return dims
}

func (u *UI) loginStep(gtx C, n int, txt string) D {
	p := u.pal
	return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
		layout.Rigid(func(gtx C) D {
			sz := gtx.Dp(28)
			fillCircle(gtx, image.Pt(sz/2, sz/2), sz/2, p.Divider)
			fillCircle(gtx, image.Pt(sz/2, sz/2), sz/2-1, p.Panel)
			gtx.Constraints = layout.Exact(image.Pt(sz, sz))
			layout.Center.Layout(gtx, u.label(14, itoa(n), p.Text).Layout)
			return D{Size: image.Pt(sz, sz)}
		}),
		layout.Rigid(layout.Spacer{Width: 16}.Layout),
		layout.Flexed(1, u.label(17, txt, p.Text, labelOpts{maxLines: 2}).Layout),
	)
}

// layoutQR draws the pairing QR code, a spinner while waiting for one, or a
// reload button once the codes have expired.
func (u *UI) layoutQR(gtx C) D {
	p := u.pal
	sz := gtx.Dp(264)
	dims := D{Size: image.Pt(sz, sz)}
	// QR codes need a light background even in dark mode.
	fillRRect(gtx, image.Rectangle{Max: dims.Size}, gtx.Dp(8), rgb(0xffffff))

	switch u.conn.State {
	case model.StateQR:
		u.drawQR(gtx, u.conn.QR, sz, p.QRFg)
	case model.StateQRExpired, model.StateError:
		u.drawQR(gtx, "expired", sz, argb(0x122e31, 0x18))
		return clickable(gtx, &u.login.retry, func(gtx C) D {
			gtx.Constraints = layout.Exact(dims.Size)
			return layout.Center.Layout(gtx, func(gtx C) D {
				return layout.Flex{Axis: layout.Vertical, Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(func(gtx C) D {
						b := gtx.Dp(84)
						fillCircle(gtx, image.Pt(b/2, b/2), b/2, p.Green)
						off := (b - gtx.Dp(36)) / 2
						t := op.Offset(image.Pt(off, off)).Push(gtx.Ops)
						drawIcon(gtx, icRefresh, 36, rgb(0xffffff))
						t.Pop()
						return D{Size: image.Pt(b, b)}
					}),
					layout.Rigid(layout.Spacer{Height: 10}.Layout),
					layout.Rigid(u.label(13, "Click to reload QR code", rgb(0x3b4a54), labelOpts{maxLines: 1}).Layout),
				)
			})
		})
	default:
		gtx.Constraints = layout.Exact(dims.Size)
		layout.Center.Layout(gtx, func(gtx C) D {
			gtx.Constraints = layout.Exact(image.Pt(gtx.Dp(48), gtx.Dp(48)))
			l := material.Loader(u.th)
			l.Color = p.Green
			return l.Layout(gtx)
		})
	}
	return dims
}

// drawQR renders data as a QR code filling a size×size square, drawing
// each row's runs of dark modules as single rectangles.
func (u *UI) drawQR(gtx C, data string, size int, col color.NRGBA) {
	code, err := u.qrCode(data)
	if err != nil {
		return
	}
	const quiet = 2 // modules of margin
	mod := size / (code.Size + 2*quiet)
	off := (size - mod*code.Size) / 2
	for y := 0; y < code.Size; y++ {
		for x := 0; x < code.Size; {
			if !code.Black(x, y) {
				x++
				continue
			}
			x0 := x
			for x < code.Size && code.Black(x, y) {
				x++
			}
			fillRect(gtx, image.Rect(off+x0*mod, off+y*mod, off+x*mod, off+(y+1)*mod), col)
		}
	}
}

func (u *UI) qrCode(data string) (*qr.Code, error) {
	if u.login.qrData == data && u.login.qr != nil {
		return u.login.qr, nil
	}
	code, err := qr.Encode(data, qr.L)
	if err != nil {
		return nil, err
	}
	u.login.qrData, u.login.qr = data, code
	return code, nil
}
