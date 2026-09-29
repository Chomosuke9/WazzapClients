// Package ui implements the WhatsApp Desktop–style interface with Gio.
package ui

import (
	"image"
	"sort"
	"time"

	"gioui.org/app"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/paint"
	"gioui.org/widget"
	"gioui.org/widget/material"
	"rsc.io/qr"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

// Chat list filters, in chip order.
const (
	filterAll = iota
	filterUnread
	filterFavorites
	filterGroups
)

var filterNames = [...]string{"All", "Unread", "Favourites", "Groups"}

// messageWindow is how many recent messages are loaded when a chat opens.
const messageWindow = 300

// UI holds all interface state. Everything is redrawn from it every frame.
// It is only touched from the window goroutine; backend updates arrive
// through Backend.Poll.
type UI struct {
	th    *material.Theme
	pal   *Palette
	dark  bool
	icons iconCache
	now   func() time.Time

	backend model.Backend
	conn    model.ConnEvent
	syncPct int // initial history sync progress; -1 when not syncing
	me      string

	chats    []*model.Chat
	selected *model.Chat
	msgs     []*model.Message // loaded window of the selected chat
	msgsVer  int              // bumped whenever msgs changes
	thumbs   map[string]thumb

	login struct {
		retry  widget.Clickable
		qrData string
		qr     *qr.Code
	}

	rail struct {
		chats, status, channels, communities, starred, settings, profile widget.Clickable
	}

	sidebar struct {
		newChat, menu, back widget.Clickable
		search              widget.Editor
		chips               [len(filterNames)]widget.Clickable
		filter              int
		archived            widget.Clickable
		showArchived        bool
		list                widget.List
		rows                map[string]*widget.Clickable
		visible             []*model.Chat
	}

	conv struct {
		list                      widget.List
		composer                  widget.Editor
		video, call, search, menu widget.Clickable
		attach, emoji, send       widget.Clickable
		header                    widget.Clickable
		rows                      []convRow
		rowsFor                   *model.Chat
		rowsVer                   int
		wallpaper                 wallpaper
		nbsp                      map[int]float32 // NBSP advance per text size in px
	}
}

type thumb struct {
	op   paint.ImageOp
	size image.Point
	ok   bool
}

// New builds the UI on top of a backend. Call Start before the first frame.
func New(b model.Backend) *UI {
	u := &UI{th: newTheme(), now: time.Now, backend: b, syncPct: -1}
	u.SetDark(false)
	u.thumbs = make(map[string]thumb)
	u.sidebar.search.SingleLine = true
	u.sidebar.list.Axis = layout.Vertical
	u.sidebar.rows = make(map[string]*widget.Clickable)
	u.conv.list.Axis = layout.Vertical
	u.conv.list.ScrollToEnd = true
	u.conv.composer.Submit = true
	return u
}

// Start loads the stored chats and starts the backend. notify is called
// (from any goroutine) whenever the backend has new events.
func (u *UI) Start(notify func()) {
	u.setChats(u.backend.Chats())
	u.backend.Start(notify)
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

// SetConn overrides the connection state (used for screenshots).
func (u *UI) SetConn(e model.ConnEvent) { u.conn = e }

// Select opens the chat at index i of the chat list (-1 closes it).
func (u *UI) Select(i int) {
	u.applyEvents()
	if i < 0 || i >= len(u.chats) {
		u.selected = nil
		return
	}
	u.open(u.chats[i])
}

func (u *UI) open(c *model.Chat) {
	if u.selected != nil && u.selected.ID == c.ID {
		return
	}
	u.selected = c
	u.msgs = u.backend.Messages(c.ID, messageWindow)
	u.msgsVer++
	c.Unread = 0
	u.backend.Open(c.ID)
	u.conv.list.Position = layout.Position{}
	u.conv.composer.SetText("")
}

// Run drives the window event loop until the window is closed.
func Run(w *app.Window, b model.Backend) error {
	u := New(b)
	u.Start(w.Invalidate)
	defer b.Close()
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

// Layout draws one frame: nav rail | chat list | conversation, or the login
// screen while there is no session.
func (u *UI) Layout(gtx C) D {
	u.applyEvents()
	if !u.conn.State.LoggedIn() {
		u.updateLogin(gtx)
		return u.layoutLogin(gtx)
	}
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
	if u.sidebar.archived.Clicked(gtx) {
		u.sidebar.showArchived = true
		u.sidebar.list.Position = layout.Position{}
	}
	if u.sidebar.back.Clicked(gtx) {
		u.sidebar.showArchived = false
		u.sidebar.list.Position = layout.Position{}
	}
	for id, click := range u.sidebar.rows {
		if click.Clicked(gtx) {
			if c := u.chatByID(id); c != nil {
				u.open(c)
			}
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
	u.conv.composer.SetText("")
	m := u.backend.Send(u.selected.ID, txt)
	if m != nil {
		u.upsertMessage(m)
	}
	u.conv.list.Position = layout.Position{} // jump to the newest message
}

// applyEvents drains the backend queue into UI state.
func (u *UI) applyEvents() {
	for _, ev := range u.backend.Poll() {
		switch e := ev.(type) {
		case model.ConnEvent:
			me := u.me
			if e.Me != "" {
				me = e.Me
			}
			u.conn, u.me = e, me
		case model.ChatsEvent:
			u.setChats(e.Chats)
		case model.ChatEvent:
			u.upsertChat(e.Chat)
		case model.MessageEvent:
			u.upsertMessage(e.Msg)
		case model.ReceiptEvent:
			u.applyReceipt(e)
		case model.TypingEvent:
			if c := u.chatByID(e.ChatID); c != nil {
				c.Typing = ""
				if e.Typing {
					c.Typing = e.Who
				}
			}
		case model.PresenceEvent:
			if c := u.chatByID(e.ChatID); c != nil {
				c.Presence = e.Text
			}
		case model.SyncEvent:
			u.syncPct = e.Percent
			if e.Percent >= 100 {
				u.syncPct = -1
			}
		}
	}
}

func (u *UI) chatByID(id string) *model.Chat {
	for _, c := range u.chats {
		if c.ID == id {
			return c
		}
	}
	return nil
}

// keepLive copies UI-only live fields (typing, presence) from the old copy.
func keepLive(dst, src *model.Chat) {
	if dst.Typing == "" {
		dst.Typing = src.Typing
	}
	if dst.Presence == "" {
		dst.Presence = src.Presence
	}
}

func (u *UI) setChats(chats []*model.Chat) {
	for _, c := range chats {
		if old := u.chatByID(c.ID); old != nil {
			keepLive(c, old)
		}
	}
	u.chats = chats
	u.sortChats()
	if u.selected != nil {
		sel := u.chatByID(u.selected.ID)
		if sel == nil {
			u.selected = nil
			return
		}
		u.selected = sel
		u.msgs = u.backend.Messages(sel.ID, messageWindow)
		u.msgsVer++
	}
}

func (u *UI) upsertChat(c *model.Chat) {
	for i, old := range u.chats {
		if old.ID == c.ID {
			keepLive(c, old)
			u.chats[i] = c
			if u.selected == old {
				u.selected = c
			}
			u.sortChats()
			return
		}
	}
	u.chats = append(u.chats, c)
	u.sortChats()
}

func (u *UI) sortChats() {
	sort.SliceStable(u.chats, func(i, j int) bool {
		a, b := u.chats[i], u.chats[j]
		if a.Pinned != b.Pinned {
			return a.Pinned
		}
		return a.Time.After(b.Time)
	})
}

func (u *UI) upsertMessage(m *model.Message) {
	c := u.chatByID(m.ChatID)
	if c != nil && (c.Last == nil || c.Last.ID == m.ID || !m.Time.Before(c.Last.Time)) {
		c.Last = m
		if m.Time.After(c.Time) {
			c.Time = m.Time
		}
		u.sortChats()
	}
	if u.selected == nil || u.selected.ID != m.ChatID {
		return
	}
	u.msgsVer++
	for i, old := range u.msgs {
		if old.ID == m.ID {
			u.msgs[i] = m
			return
		}
	}
	i := sort.Search(len(u.msgs), func(i int) bool { return u.msgs[i].Time.After(m.Time) })
	u.msgs = append(u.msgs, nil)
	copy(u.msgs[i+1:], u.msgs[i:])
	u.msgs[i] = m
	if !m.FromMe {
		u.selected.Unread = 0
		u.backend.Open(m.ChatID)
	}
}

func (u *UI) applyReceipt(e model.ReceiptEvent) {
	ids := make(map[string]bool, len(e.IDs))
	for _, id := range e.IDs {
		ids[id] = true
	}
	up := func(m *model.Message) {
		if m != nil && m.FromMe && ids[m.ID] && m.Receipt < e.Receipt {
			m.Receipt = e.Receipt
		}
	}
	if c := u.chatByID(e.ChatID); c != nil {
		up(c.Last)
	}
	if u.selected != nil && u.selected.ID == e.ChatID {
		for _, m := range u.msgs {
			up(m)
		}
	}
}
