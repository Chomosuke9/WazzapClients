package ui

import (
	"image"

	"gioui.org/layout"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

// The history sync of a new link: the banner above the chat list says how
// far it is, and while older messages come, a click on it opens WhatsApp's
// dialog with a progress bar.

// historyHelpURL is WhatsApp's "About message history on linked devices".
const historyHelpURL = "https://faq.whatsapp.com/653480766448040"

// layoutBanner shows connection and sync status above the chat list, like
// WhatsApp's "Computer not connected" notice.
func (u *UI) layoutBanner(gtx C) D {
	var msg string
	older := false
	switch s := u.sync; {
	case u.conn.State == model.StateConnecting:
		msg = "Connecting…"
	case u.conn.State == model.StateOffline:
		msg = "Computer not connected. Reconnecting…"
	case s.Done:
		return D{}
	case s.Stage == model.SyncChats:
		msg = "Syncing chats… " + itoa(s.Percent) + "%"
	case s.Stage == model.SyncOlder && s.Waiting:
		msg, older = "Syncing older messages: waiting for your phone", true
	case s.Stage == model.SyncOlder:
		msg, older = "Syncing older messages… "+itoa(s.Percent)+"%", true
	default:
		return D{}
	}
	p := u.pal
	c := u.btn("syncbanner")
	if older && c.Clicked(gtx) {
		u.openSyncDialog()
	}
	return layout.Inset{Left: 21, Right: 21, Bottom: 10}.Layout(gtx, func(gtx C) D {
		gtx.Constraints.Min.X = gtx.Constraints.Max.X
		if !older {
			return background(gtx, p.Banner, 12, func(gtx C) D {
				return layout.Inset{Left: 14, Right: 14, Top: 10, Bottom: 10}.Layout(gtx,
					u.label(14, msg, p.BannerText, labelOpts{maxLines: 2}).Layout)
			})
		}
		return clickable(gtx, c, func(gtx C) D {
			return background(gtx, mix(p.Banner, p.Text, 0.06*u.hover(gtx, c)), 12, func(gtx C) D {
				return layout.Inset{Left: 14, Right: 8, Top: 10, Bottom: 10}.Layout(gtx, func(gtx C) D {
					return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
						layout.Flexed(1, func(gtx C) D {
							gtx.Constraints.Min.X = gtx.Constraints.Max.X
							return u.label(14, msg, p.BannerText, labelOpts{maxLines: 2}).Layout(gtx)
						}),
						layout.Rigid(iconW(icChevronRight, 20, p.BannerText)),
					)
				})
			})
		})
	})
}

func (u *UI) openSyncDialog() {
	u.dialog = dialogState{kind: dialogSync}
	u.syncBar = follower{}
}

// syncPanel is the dialog, which follows the sync while it's open.
func (u *UI) syncPanel(gtx C) D {
	d, p, s := &u.dialog, u.pal, u.sync
	if u.btn("dialog:1").Clicked(gtx) && d.isOpen() {
		u.closeDialog()
	}
	if u.btn("dialog:2").Clicked(gtx) && d.isOpen() && !openURL(historyHelpURL) {
		u.toast("Couldn't open the link.")
	}
	const check = "To see your full chat history, check your phone."
	title, body := "Syncing older messages", "Don't close WhatsApp on your phone while older messages are syncing. "+check
	switch {
	case s.Done && s.Percent >= 100:
		title, body = "Older messages synced", check
	case s.Done:
		title, body = "Syncing stopped", "Your phone stopped sending older messages. The ones it sent are in your chats. "+check
	case s.Waiting:
		body = "Waiting for your phone. Open WhatsApp on it to go on syncing. " + check
	}
	share := u.syncBar.step(gtx, float32(s.Percent)/100, pollBarDur)
	w := min(gtx.Dp(400), gtx.Constraints.Max.X-gtx.Dp(32))
	gtx.Constraints = layout.Constraints{Min: image.Pt(w, 0), Max: image.Pt(w, gtx.Constraints.Max.Y)}
	return layout.UniformInset(24).Layout(gtx, func(gtx C) D {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(u.label(20, title, p.Text, labelOpts{maxLines: 2}).Layout),
			layout.Rigid(layout.Spacer{Height: 32}.Layout),
			layout.Rigid(func(gtx C) D {
				sz := image.Pt(gtx.Constraints.Max.X, gtx.Dp(14))
				fillRect(gtx, image.Rectangle{Max: sz}, mix(p.Dialog, p.Text, 0.2))
				fillRect(gtx, image.Rect(0, 0, int(float32(sz.X)*share+0.5), sz.Y), p.Text)
				return D{Size: sz}
			}),
			layout.Rigid(layout.Spacer{Height: 12}.Layout),
			layout.Rigid(u.label(12.5, itoa(s.Percent)+"%", p.Text).Layout),
			layout.Rigid(layout.Spacer{Height: 20}.Layout),
			layout.Rigid(func(gtx C) D {
				l := u.label(15.5, body, p.Text)
				l.MaxLines = 0
				return l.Layout(gtx)
			}),
			layout.Rigid(layout.Spacer{Height: 40}.Layout),
			layout.Rigid(func(gtx C) D {
				return layout.E.Layout(gtx, func(gtx C) D {
					return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
						layout.Rigid(func(gtx C) D {
							return u.dialogButton(gtx, "dialog:2", dialogButton{label: "Learn more", flat: true})
						}),
						layout.Rigid(layout.Spacer{Width: 8}.Layout),
						layout.Rigid(func(gtx C) D {
							return u.dialogButton(gtx, "dialog:1", dialogButton{label: "OK", primary: true})
						}),
					)
				})
			}),
		)
	})
}
