// Package ui implements the WhatsApp Desktop–style interface with Gio.
package ui

import (
	"image"
	"time"

	"gioui.org/app"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"github.com/chomosuke9/wazzapclients/internal/mock"
)

// Chat list filters, in chip order.
const (
	filterAll = iota
	filterUnread
	filterFavorites
	filterGroups
)

var filterNames = [...]string{"All", "Unread", "Favourites", "Groups"}

// UI holds all interface state. Everything is redrawn from it every frame.
type UI struct {
	th    *material.Theme
	pal   *Palette
	dark  bool
	icons iconCache
	now   func() time.Time

	chats    []*mock.Chat
	selected *mock.Chat

	rail struct {
		chats, status, channels, communities, starred, settings, profile widget.Clickable
	}

	sidebar struct {
		newChat, menu widget.Clickable
		search        widget.Editor
		chips         [len(filterNames)]widget.Clickable
		filter        int
		archived      widget.Clickable
		list          widget.List
		rows          map[*mock.Chat]*widget.Clickable
		visible       []*mock.Chat
	}

	conv struct {
		list                      widget.List
		composer                  widget.Editor
		video, call, search, menu widget.Clickable
		attach, emoji, send       widget.Clickable
		header                    widget.Clickable
		rows                      []convRow
		rowsFor                   *mock.Chat
		rowsLen                   int
		wallpaper                 wallpaper
		nbsp                      map[int]float32 // NBSP advance per text size in px
	}
}

// New builds the UI with demo data.
func New() *UI {
	u := &UI{th: newTheme(), now: time.Now}
	u.chats = mock.Chats(u.now())
	u.SetDark(false)
	u.sidebar.search.SingleLine = true
	u.sidebar.list.Axis = layout.Vertical
	u.sidebar.rows = make(map[*mock.Chat]*widget.Clickable)
	u.conv.list.Axis = layout.Vertical
	u.conv.list.ScrollToEnd = true
	u.conv.composer.Submit = true
	return u
}

// SetDark switches between the light and dark palettes.
func (u *UI) SetDark(dark bool) {
	u.dark = dark
	if dark {
		u.pal = &darkPalette
	} else {
		u.pal = &lightPalette
	}
	u.th.Palette.Fg = u.pal.Text
	u.th.Palette.Bg = u.pal.Panel
	u.th.Palette.ContrastBg = u.pal.Green
}

// Select opens the chat at index i of the chat list (-1 closes it).
func (u *UI) Select(i int) {
	if i < 0 || i >= len(u.chats) {
		u.selected = nil
		return
	}
	u.open(u.chats[i])
}

func (u *UI) open(c *mock.Chat) {
	if u.selected == c {
		return
	}
	u.selected = c
	c.Unread = 0
	u.conv.list.Position = layout.Position{}
	u.conv.composer.SetText("")
}

// Run drives the window event loop until the window is closed.
func Run(w *app.Window) error {
	u := New()
	var ops op.Ops
	for {
		switch e := w.Event().(type) {
		case app.DestroyEvent:
			return e.Err
		case app.FrameEvent:
			gtx := app.NewContext(&ops, e)
			u.Layout(gtx)
			e.Frame(gtx.Ops)
		}
	}
}

// Layout draws one frame: nav rail | chat list | conversation.
func (u *UI) Layout(gtx C) D {
	u.update(gtx)

	total := gtx.Constraints.Max.X
	railW := gtx.Dp(64)
	listW := int(float32(total-railW) * 0.32)
	listW = max(gtx.Dp(320), min(listW, gtx.Dp(460)))

	return layout.Flex{}.Layout(gtx,
		layout.Rigid(func(gtx C) D {
			gtx.Constraints = layout.Exact(image.Pt(railW, gtx.Constraints.Max.Y))
			return u.layoutRail(gtx)
		}),
		layout.Rigid(func(gtx C) D {
			gtx.Constraints = layout.Exact(image.Pt(listW, gtx.Constraints.Max.Y))
			return u.layoutSidebar(gtx)
		}),
		layout.Rigid(func(gtx C) D {
			gtx.Constraints = layout.Exact(image.Pt(1, gtx.Constraints.Max.Y))
			return fill(gtx, u.pal.Divider)
		}),
		layout.Flexed(1, func(gtx C) D {
			if u.selected == nil {
				return u.layoutEmpty(gtx)
			}
			return u.layoutConversation(gtx)
		}),
	)
}

// update handles input events before anything is drawn.
func (u *UI) update(gtx C) {
	if u.rail.settings.Clicked(gtx) {
		u.SetDark(!u.dark)
	}
	for i := range u.sidebar.chips {
		if u.sidebar.chips[i].Clicked(gtx) {
			u.sidebar.filter = i
			u.sidebar.list.Position = layout.Position{}
		}
	}
	for c, click := range u.sidebar.rows {
		if click.Clicked(gtx) {
			u.open(c)
		}
	}
	for {
		ev, ok := u.conv.composer.Update(gtx)
		if !ok {
			break
		}
		if _, ok := ev.(widget.SubmitEvent); ok {
			u.sendComposer()
		}
	}
	if u.conv.send.Clicked(gtx) {
		u.sendComposer()
	}
}

func (u *UI) sendComposer() {
	if u.selected == nil {
		return
	}
	txt := trimSpace(u.conv.composer.Text())
	if txt == "" {
		return
	}
	u.selected.Send(txt, u.now())
	u.conv.composer.SetText("")
	u.conv.list.Position = layout.Position{} // jump to the newest message
	// Keep the chat at the top of the list, like WhatsApp does.
	for i, c := range u.chats {
		if c == u.selected && !c.Pinned {
			copy(u.chats[1:i+1], u.chats[:i])
			u.chats[0] = c
			u.sortPinnedFirst()
			break
		}
	}
}

func (u *UI) sortPinnedFirst() {
	out := u.chats[:0:0]
	for _, c := range u.chats {
		if c.Pinned {
			out = append(out, c)
		}
	}
	for _, c := range u.chats {
		if !c.Pinned {
			out = append(out, c)
		}
	}
	u.chats = out
}
