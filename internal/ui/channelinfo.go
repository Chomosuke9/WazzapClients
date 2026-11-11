package ui

import (
	"image"

	"gioui.org/font"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/text"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

// A channel's menus (its row's right click and the open channel's ⋮),
// its info panel, the Follow bar under a channel you don't follow yet, and
// the Channels page's ⊕: Create channel and Find channels.

// channelMenuItems are what you can do with a channel. open adds the
// items of the open channel's ⋮ menu.
func (u *UI) channelMenuItems(ch *model.Channel, open bool) []menuItem {
	b, id := u.backend, ch.ID
	var items []menuItem
	add := func(it menuItem) { items = append(items, it) }
	add(menuItem{key: "info", ic: icInfo, label: "Channel info", run: func() {
		if u.selected == nil || u.selected.ID != id {
			u.open(channelChat(ch))
		}
		u.openInfo(id)
	}})
	if ch.Following {
		if ch.Muted {
			add(menuItem{key: "mute", ic: icBellLine, label: "Unmute notifications", run: func() { b.MuteChannel(id, false) }})
		} else {
			add(menuItem{key: "mute", ic: icMuted, label: "Mute notifications", run: func() { b.MuteChannel(id, true) }})
		}
	}
	if ch.Invite != "" {
		link := model.ChannelLink(ch.Invite)
		add(menuItem{key: "link", ic: icLink, label: "Copy link", run: func() {
			u.pendingCopy = link
			u.toast("Link copied")
		}})
	}
	add(menuItem{divider: true})
	switch {
	case ch.Owner && ch.Following:
		add(menuItem{key: "delete", ic: icDelete, label: "Delete channel", run: func() { u.confirmDeleteChannel(ch) }})
	case ch.Following:
		add(menuItem{key: "unfollow", ic: icLogout, label: "Unfollow channel", run: func() { u.confirmUnfollow(ch) }})
	default:
		add(menuItem{key: "follow", ic: icAddCircle, label: "Follow channel", run: func() { b.FollowChannel(id) }})
	}
	if open {
		add(menuItem{key: "close", ic: icCancel, label: "Close channel", run: u.closeChat})
	}
	return items
}

// channelAddItems is the Channels page's ⊕ menu.
func (u *UI) channelAddItems() []menuItem {
	return []menuItem{
		{key: "create", ic: icAddCircle, label: "Create channel", run: u.openCreateChannel},
		{key: "find", ic: icSearch, label: "Find channels", run: func() {
			u.channel.discovering = true
			u.backend.DiscoverChannels()
			u.channel.list.Position = layout.Position{First: len(u.channels)}
		}},
	}
}

func (u *UI) confirmUnfollow(ch *model.Channel) {
	id := ch.ID
	u.confirm("Unfollow "+ch.Name+"?",
		"You'll stop getting its updates. You can follow it again from its link or in Find channels.",
		dialogButton{label: "Unfollow", primary: true, run: func() { u.backend.UnfollowChannel(id) }},
		dialogButton{label: "Cancel", run: func() {}},
	)
}

func (u *UI) confirmDeleteChannel(ch *model.Channel) {
	id := ch.ID
	u.confirm("Delete "+ch.Name+"?",
		"The channel and its updates will be deleted for everyone. Its followers won't be able to see it any more. This can't be undone.",
		dialogButton{label: "Delete channel", danger: true, run: func() {
			if u.selected != nil && u.selected.ID == id {
				u.closeChat()
			}
			u.backend.DeleteChannel(id)
		}},
		dialogButton{label: "Cancel", run: func() {}},
	)
}

// channelFooter is what takes the composer's place in a channel: nothing
// in one you follow, the composer in your own, a Follow button in one you
// don't follow yet. It returns nil when the composer goes there.
func (u *UI) channelFooter(c *model.Chat) layout.Widget {
	ch := u.channelByID(c.ID)
	switch {
	case ch != nil && ch.Owner && ch.Following:
		return nil
	case ch != nil && ch.Following:
		return func(gtx C) D { return D{} }
	}
	id := c.ID
	return func(gtx C) D {
		p := u.pal
		if u.btn("channel-follow").Clicked(gtx) {
			u.backend.FollowChannel(id)
		}
		w := gtx.Constraints.Max.X
		h := gtx.Dp(76)
		fillRect(gtx, image.Rect(0, 0, w, h), p.Panel)
		fillRect(gtx, image.Rect(0, 0, w, max(1, gtx.Dp(1))), p.Divider)
		bw := min(w-gtx.Dp(32), gtx.Dp(320))
		t := op.Offset(image.Pt((w-bw)/2, (h-gtx.Dp(44))/2)).Push(gtx.Ops)
		bg := gtx
		bg.Constraints = layout.Exact(image.Pt(bw, gtx.Dp(44)))
		u.wideButton(bg, "channel-follow", "Follow", false)
		t.Pop()
		return D{Size: image.Pt(w, h)}
	}
}

// channelInfoRows are a channel's info panel: its picture, name and
// followers, description, notifications, link and what you can do.
func (u *UI) channelInfoRows(gtx C, c *model.Chat) []layout.Widget {
	p := u.pal
	ch := u.channelByID(c.ID)
	if ch == nil {
		ch = &model.Channel{ID: c.ID, Name: c.Name}
	}
	for _, it := range u.channelMenuItems(ch, false) {
		if it.key != "" && it.key != "info" && u.btn("info:ch-"+it.key).Clicked(gtx) && it.run != nil {
			it.run()
		}
	}
	rows := []layout.Widget{func(gtx C) D {
		return layout.Inset{Top: 28, Bottom: 22, Left: infoPadX, Right: infoPadX}.Layout(gtx, func(gtx C) D {
			gtx.Constraints.Min.X = gtx.Constraints.Max.X
			return layout.Flex{Axis: layout.Vertical, Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(func(gtx C) D { return u.avatarOf(gtx, ch.ID, avatarChannel, 130) }),
				layout.Rigid(layout.Spacer{Height: 16}.Layout),
				layout.Rigid(func(gtx C) D { return u.nameWithBadge(gtx, ch.Name, 22, p.Text, ch.Verified) }),
				layout.Rigid(layout.Spacer{Height: 4}.Layout),
				layout.Rigid(u.label(15.5, "Channel · "+followers(ch.Followers), p.TextSecondary, labelOpts{maxLines: 1}).Layout),
			)
		})
	}}
	if ch.Description != "" {
		rows = append(rows, u.infoDivider(0, 8), func(gtx C) D {
			return layout.Inset{Left: infoPadX, Right: 24, Top: 8, Bottom: 14}.Layout(gtx, func(gtx C) D {
				l := u.label(15.5, ch.Description, p.Text, labelOpts{align: text.Start})
				l.MaxLines = 0
				return l.Layout(gtx)
			})
		})
	}
	if !ch.Created.IsZero() {
		rows = append(rows, func(gtx C) D {
			return layout.Inset{Left: infoPadX, Right: 24, Bottom: 14}.Layout(gtx,
				u.label(14, "Created "+ch.Created.Format("02/01/2006"), p.TextSecondary, labelOpts{maxLines: 1}).Layout)
		})
	}
	rows = append(rows, u.infoDivider(0, 6))
	for _, it := range u.channelMenuItems(ch, false) {
		if it.divider {
			rows = append(rows, u.infoDivider(6, 6))
			continue
		}
		if it.key == "info" {
			continue
		}
		li := listItem{ic: it.ic, title: it.label}
		switch it.key {
		case "mute":
			li.title, li.sub, li.ic = "Notifications", "On", icBellLine
			if ch.Muted {
				li.sub, li.ic = "Muted", icMuted
			}
		case "link":
			li.title, li.sub = "Channel link", model.ChannelLink(ch.Invite)
		case "delete", "unfollow":
			li.danger = true
		}
		rows = append(rows, u.infoItem("ch-"+it.key, li))
	}
	rows = append(rows, func(gtx C) D {
		return layout.Inset{Left: infoPadX, Right: 24, Top: 14, Bottom: 40}.Layout(gtx, func(gtx C) D {
			l := u.label(13.5, "Channel updates are visible to anyone who follows. Your phone number and profile aren't shown to the channel's admins or followers.", p.TextSecondary)
			l.MaxLines = 0
			return l.Layout(gtx)
		})
	})
	return rows
}

// openCreateChannel opens the Create channel dialog.
func (u *UI) openCreateChannel() {
	u.channel.newName.SetText("")
	u.channel.newDesc.SetText("")
	u.channel.newName.SingleLine = true
	u.dialog = dialogState{kind: dialogChannel}
	u.requestFocus(&u.channel.newName)
}

// createChannelPanel is the Create channel dialog: a name and a
// description.
func (u *UI) createChannelPanel(gtx C) D {
	p := u.pal
	name := trimSpace(u.channel.newName.Text())
	busy := u.channel.creating
	if u.btn("channel-new:cancel").Clicked(gtx) {
		u.closeDialog()
	}
	if u.btn("channel-new:create").Clicked(gtx) && name != "" && !busy && u.dialog.isOpen() {
		u.channel.creating = true
		u.backend.CreateChannel(name, trimSpace(u.channel.newDesc.Text()))
	}
	w := min(gtx.Dp(480), gtx.Constraints.Max.X-gtx.Dp(32))
	gtx.Constraints = layout.Constraints{Min: image.Pt(w, 0), Max: image.Pt(w, gtx.Constraints.Max.Y)}
	return layout.UniformInset(24).Layout(gtx, func(gtx C) D {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(u.label(19.5, "Create channel", p.Text, labelOpts{weight: font.Medium}).Layout),
			layout.Rigid(layout.Spacer{Height: 8}.Layout),
			layout.Rigid(func(gtx C) D {
				l := u.label(14.5, "Share updates with anyone who follows. Only admins post in a channel; followers can react.", p.TextSecondary)
				l.MaxLines = 0
				return l.Layout(gtx)
			}),
			layout.Rigid(layout.Spacer{Height: 14}.Layout),
			layout.Rigid(func(gtx C) D { return u.pollLine(gtx, &u.channel.newName, "Channel name", false) }),
			layout.Rigid(layout.Spacer{Height: 10}.Layout),
			layout.Rigid(func(gtx C) D {
				gtx.Constraints.Max.Y = min(gtx.Constraints.Max.Y, gtx.Dp(160))
				return u.pollLine(gtx, &u.channel.newDesc, "Describe your channel (optional)", false)
			}),
			layout.Rigid(layout.Spacer{Height: 26}.Layout),
			layout.Rigid(func(gtx C) D {
				label := "Create"
				if busy {
					label = "Creating…"
				}
				return layout.E.Layout(gtx, func(gtx C) D {
					return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
						layout.Rigid(func(gtx C) D {
							return u.dialogButton(gtx, "channel-new:cancel", dialogButton{label: "Cancel"})
						}),
						layout.Rigid(layout.Spacer{Width: 12}.Layout),
						layout.Rigid(func(gtx C) D {
							return u.dialogButton(gtx, "channel-new:create",
								dialogButton{label: label, primary: true, disabled: name == "" || busy})
						}),
					)
				})
			}),
		)
	})
}

// channelCreated opens the channel CreateChannel made, or says why it
// couldn't.
func (u *UI) channelCreated(e model.ChannelCreatedEvent) {
	u.channel.creating = false
	if e.Err != "" {
		u.toast(e.Err)
		return
	}
	if u.dialog.kind == dialogChannel {
		u.closeDialog()
	}
	if ch := u.channelByID(e.ID); ch != nil {
		u.openChannel(e.ID)
	}
}
