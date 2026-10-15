package ui

import (
	"net/url"
	"strings"
	"unicode/utf8"

	"gioui.org/font"
	"gioui.org/layout"

	"github.com/chomosuke9/wazzapclients/internal/desktop"
	"github.com/chomosuke9/wazzapclients/internal/model"
)

// WhatsApp's links open in the app, as in WhatsApp Desktop: a chat with a
// phone number (https://wa.me/<number>?text=…, api.whatsapp.com/send and
// whatsapp://send?phone=…, which websites send the browser on to), with
// the link's text in its composer; a group invite
// (https://chat.whatsapp.com/<code>, whatsapp://chat?code=…); and the
// links only the server resolves (Backend.ResolveLink): a channel
// (https://whatsapp.com/channel/<code>), a business's message link, which
// puts the business's message in the composer (https://wa.me/message/<code>),
// and a contact's QR link (https://wa.me/qr/<code>). Clicked in a message,
// or handed over by Windows once the app handles whatsapp: links
// (desktop.RegisterLinks, Settings > General).

// appLink is what a WhatsApp link opens.
type appLink struct {
	phone, text string // a chat (digits with the country code), and text for its composer
	code        string // a group invite's
	// resolve is the code of a link ResolveLink looks up, of kind.
	resolve string
	kind    model.LinkKind
}

// resolveKinds are the links ResolveLink looks up, by the first part of
// their path (wa.me/message/<code>) or their host (whatsapp://message/<code>).
var resolveKinds = map[string]model.LinkKind{
	"channel": model.LinkChannel,
	"message": model.LinkBusiness,
	"qr":      model.LinkContact,
}

// parseAppLink reads a WhatsApp link. It reports false for any other
// link, and for one that names no chat, group or channel.
func parseAppLink(link string) (appLink, bool) {
	if code := inviteCode(link); code != "" {
		return appLink{code: code}, true
	}
	p, err := url.Parse(strings.TrimSpace(link))
	if err != nil {
		return appLink{}, false
	}
	q := p.Query()
	// resolved reads <kind>/<code>, or <kind>?code=<code>.
	resolved := func(kind, code string) (appLink, bool) {
		k, ok := resolveKinds[kind]
		if code == "" {
			code = q.Get("code")
		}
		if !ok || !isLinkCode(code) {
			return appLink{}, false
		}
		return appLink{resolve: code, kind: k}, true
	}
	var l appLink
	switch scheme, host, path := strings.ToLower(p.Scheme), strings.ToLower(p.Host), strings.Trim(p.Path, "/"); {
	case scheme == "whatsapp" && host == "send":
		l = appLink{phone: q.Get("phone"), text: q.Get("text")}
	case scheme == "whatsapp" && host == "chat":
		l.code = q.Get("code")
		if l.code == "" || inviteCode("https://chat.whatsapp.com/"+l.code) != l.code {
			return appLink{}, false
		}
		return l, true
	case scheme == "whatsapp":
		return resolved(host, path)
	case scheme != "http" && scheme != "https":
		return appLink{}, false
	case host == "wa.me" || host == "www.wa.me" || host == "api.whatsapp.com":
		if kind, code, ok := strings.Cut(path, "/"); ok {
			if kind == "channel" {
				return appLink{}, false // only whatsapp.com has those
			}
			return resolved(kind, code)
		}
		if host == "api.whatsapp.com" {
			if path != "send" {
				return appLink{}, false
			}
			l = appLink{phone: q.Get("phone"), text: q.Get("text")}
		} else {
			l = appLink{phone: path, text: q.Get("text")}
		}
	case host == "whatsapp.com" || host == "www.whatsapp.com" || host == "web.whatsapp.com":
		if code, ok := strings.CutPrefix(path, "channel/"); ok && host != "web.whatsapp.com" {
			return resolved("channel", code)
		}
		if path != "send" {
			return appLink{}, false
		}
		l = appLink{phone: q.Get("phone"), text: q.Get("text")}
	default:
		return appLink{}, false
	}
	// Websites write the number every which way: "+62 812-…", "62812…".
	if l.phone = phoneDigits(l.phone); l.phone == "" {
		return appLink{}, false
	}
	return l, true
}

// isLinkCode reports whether s looks like a link's code: letters and
// digits only.
func isLinkCode(s string) bool {
	if len(s) < 4 || len(s) > 64 {
		return false
	}
	for _, r := range s {
		if !('a' <= r && r <= 'z' || 'A' <= r && r <= 'Z' || '0' <= r && r <= '9') {
			return false
		}
	}
	return true
}

// openAppLink opens a WhatsApp link's chat, group or channel, and reports
// whether link was one.
func (u *UI) openAppLink(link string) bool {
	l, ok := parseAppLink(link)
	switch {
	case !ok:
		return false
	case l.code != "":
		u.openInvite(l.code)
	case l.resolve != "":
		u.resolving = l.resolve
		if l.kind == model.LinkChannel {
			// The channel shows in the invite dialog while it loads.
			u.dialog = dialogState{kind: dialogInvite, invite: inviteState{code: l.resolve, channel: true}}
			u.dialog.invite.desc.Axis = layout.Vertical
		}
		u.backend.ResolveLink(l.kind, l.resolve)
	default:
		u.messagePhone(l.phone, "")
		u.cardPhoneText = l.text
	}
	return true
}

// linkResolved answers ResolveLink: it opens the chat, or the channel, or
// shows the channel to follow.
func (u *UI) linkResolved(e model.LinkEvent) {
	if e.Code != u.resolving {
		return
	}
	u.resolving = ""
	d := &u.dialog
	inDialog := d.kind == dialogInvite && d.isOpen() && d.invite.channel && d.invite.code == e.Code
	switch {
	case e.Kind == model.LinkChannel && !inDialog:
		// Closed while it loaded.
	case e.Err != "" && inDialog:
		d.invite.err = e.Err
	case e.Err != "":
		u.toast(e.Err)
	case e.Kind == model.LinkChannel && e.Channel != nil:
		if e.Channel.Following || u.channelByID(e.ID) != nil {
			u.closeDialog()
			u.openChannel(e.ID)
			return
		}
		d.invite.ch, d.invite.chDesc = e.Channel, e.Description
	case e.Kind == model.LinkChannel:
		d.invite.err = "This link doesn't exist or was reset."
	default:
		u.openLinkedChat(e.ID, e.Name, e.Text)
	}
}

// followFromLink follows the channel the dialog shows, and opens it once
// the backend lists it (ChannelsEvent).
func (u *UI) followFromLink(id string) {
	u.backend.FollowChannel(id)
	u.followed = id
	u.closeDialog()
}

// channelsChanged opens the channel followed from a link once it's in
// the list.
func (u *UI) channelsChanged() {
	if u.followed != "" && u.channelByID(u.followed) != nil {
		u.openChannel(u.followed)
		u.followed = ""
	}
}

// openChannel opens a followed channel on the Channels page.
func (u *UI) openChannel(id string) {
	ch := u.channelByID(id)
	if ch == nil {
		return
	}
	u.setPage(pageChannels)
	u.open(channelChat(ch))
	ch.Unread = 0
}

// openLinkedChat opens chat id, which a link led to (name is a name to
// show when it isn't in the list yet), with text in its composer.
func (u *UI) openLinkedChat(id, name, text string) {
	if !u.openChatByID(id) {
		u.openDirect(id, name)
	}
	if text != "" && u.selected != nil && u.selected.ID == id {
		u.conv.composer.SetText(text)
		n := utf8.RuneCountInString(text)
		u.conv.composer.SetCaret(n, n)
	}
}

// openOutsideLink opens a link the app was started with (or a second
// launch handed over), once the account is online.
func (u *UI) openOutsideLink(link string) {
	u.applyEvents()
	if !u.openAppLink(link) {
		u.toast("Couldn't open this WhatsApp link.")
	}
}

// Like a browser that isn't the default one, the chat list asks to open
// WhatsApp's links (on Windows, once logged in) until they open here or
// the bar is closed. Windows lets only the user pick the app, so its
// button registers the app and opens Windows' settings at its page.

const prefLinkBar = "link_bar" // "off" once the bar was closed

// linkBar is the bar's: whether whatsapp: links open here, as read from
// the registry when checked, which focusing the window clears (the user
// may have chosen the app in Windows' settings meanwhile).
type linkBar struct {
	checked, handled bool
	demo             bool // drawn for cmd/screenshot (ShowOverlay "linkbar")
}

// canClaimLinks reports whether this app can be whatsapp: links' handler:
// a Windows build with real data, which another launch can start.
func (u *UI) canClaimLinks() bool {
	return u.host != nil && u.host.o.Relaunch != nil && desktop.CanHandleLinks()
}

// linksHere reports whether whatsapp: links open in this app.
func (u *UI) linksHere() bool {
	if !u.linkBar.checked {
		u.linkBar.checked, u.linkBar.handled = true, desktop.LinksHandled()
	}
	return u.linkBar.handled
}

// claimLinks registers the app for whatsapp: links and, unless that made
// it their handler, opens Windows' settings for the user to choose it.
func (u *UI) claimLinks() {
	if err := desktop.RegisterLinks(u.host.o.Relaunch); err != nil {
		u.toast("Couldn't register " + appName + " for WhatsApp links.")
		return
	}
	u.linkBar.checked = false
	if u.linksHere() {
		u.toast("WhatsApp links now open in " + appName + ".")
		return
	}
	desktop.OpenLinkSettings()
	u.toast("In Windows' settings, choose " + appName + " for WHATSAPP.")
}

// layoutLinkBar draws the bar above the chat list.
func (u *UI) layoutLinkBar(gtx C) D {
	if !u.linkBar.demo && (!u.canClaimLinks() || u.conn.State != model.StateOnline ||
		!prefOn(u.backend, prefLinkBar) || u.linksHere()) {
		return D{}
	}
	p := u.pal
	use, closeBtn := u.btn("linkbar:use"), u.btn("linkbar:close")
	if use.Clicked(gtx) && !u.linkBar.demo {
		u.claimLinks()
	}
	if closeBtn.Clicked(gtx) {
		setPref(u.backend, prefLinkBar, false)
		u.toast("You can open WhatsApp links here from Settings > General.")
		return D{}
	}
	return layout.Inset{Left: 21, Right: 21, Bottom: 10}.Layout(gtx, func(gtx C) D {
		gtx.Constraints.Min.X = gtx.Constraints.Max.X
		return background(gtx, p.Banner, 12, func(gtx C) D {
			return layout.Inset{Left: 14, Right: 4, Top: 6, Bottom: 6}.Layout(gtx, func(gtx C) D {
				return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(iconW(icLink, 18, p.Green)),
					layout.Rigid(layout.Spacer{Width: 10}.Layout),
					layout.Flexed(1, u.label(14, "WhatsApp links open in another app", p.BannerText, labelOpts{maxLines: 2}).Layout),
					layout.Rigid(func(gtx C) D {
						return clickable(gtx, use, func(gtx C) D {
							return background(gtx, faded(p.Hover, u.hover(gtx, use)), 16, func(gtx C) D {
								return layout.UniformInset(8).Layout(gtx,
									u.label(14, "Open here", p.Green, labelOpts{weight: font.Medium, maxLines: 1}).Layout)
							})
						})
					}),
					layout.Rigid(func(gtx C) D { return u.iconButton(gtx, closeBtn, icClose, 32, 18, p.IconStrong) }),
				)
			})
		})
	})
}
