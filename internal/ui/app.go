// Package ui implements the WhatsApp Desktop–style interface with Gio.
package ui

import (
	"image"
	"image/color"
	"sort"
	"time"

	"gioui.org/app"
	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/widget"
	"gioui.org/widget/material"
	"rsc.io/qr"

	"github.com/chomosuke9/wazzapclients/internal/memtrim"
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

// UI holds all interface state. Everything is redrawn from it every frame.
// It is only touched from the window goroutine; backend updates arrive
// through Backend.Poll.
type UI struct {
	th     *material.Theme
	pal    *Palette
	dark   bool
	now    func() time.Time
	window *app.Window // nil when rendering headless
	deco   widget.Decorations
	// winWidth is the window width in px, for panels sized relative to it.
	winWidth int

	backend model.Backend
	conn    model.ConnEvent
	syncPct int // initial history sync progress; -1 when not syncing
	me      string
	meID    string

	chats     []*model.Chat
	selected  *model.Chat
	selPage   page             // page the selected chat was opened from
	msgs      []*model.Message // loaded window of the selected chat
	msgsVer   int              // bumped whenever msgs changes
	images    *imageCache
	emojiImgs *imageCache // the emoji picker's, see layoutEmojiImage
	players   players     // animated stickers on screen
	bars      map[*widget.List]*scrollbar

	page         page
	statusSeen   time.Time // when the Status page was last open
	channelsSeen time.Time // when the Channels page was last open
	statuses     []*model.StatusThread
	channels     []*model.Channel
	suggested    []*model.Channel
	communities  []*model.Community
	clicks       clicks // see btn

	info     infoState
	status   statusState
	channel  channelState
	commun   communityState
	settings settingsState
	calls    callsState

	login struct {
		retry  widget.Clickable
		qrData string
		qr     *qr.Code
	}

	rail struct {
		chats, calls, status, channels, communities, archived, media, profile widget.Clickable
	}

	files  fileState   // documents and audio on disk
	voice  voiceState  // the audio message playing
	attach attachState // files picked to send

	menu       menuState
	filterMenu filterMenuState

	// Overlays: context menu, modal dialog, emoji picker, media viewer, toast.
	ctx       ctxMenu
	dialog    dialogState
	picker    emojiPicker
	viewer    mediaViewer
	toastMsg  toastState
	mouse     image.Point // last pointer position, in content coordinates
	mouseTag  struct{}
	hovered   map[string]bool // see hoverArea
	lastPress struct {        // for double clicks, see pressArea
		key string
		at  time.Duration
	}
	lastButton struct { // see pressButton
		key string
		at  time.Time
	}
	pendingCopy string // clipboard text waiting for a frame
	focus       any    // editor to focus next frame (see requestFocus)
	focusReq    bool

	anims    animStore                     // keyed fades: hovers, new messages, reactions
	wheels   map[*layout.List]*wheelScroll // lists still easing a wheel scroll
	pageIn   tween                         // the page content fading in after a switch
	railSel  switcher[*widget.Clickable]   // the active rail button
	pageSeen page                          // page shown last frame, to notice switches

	sidebar struct {
		newChat, menu, back widget.Clickable
		more                widget.Clickable // collapsed filter chips
		hiddenFilters       []int
		search              widget.Editor
		chips               [len(filterNames)]widget.Clickable
		filter              int
		showArchived        bool
		list                widget.List
		rows                map[string]*widget.Clickable
		visible             []*model.Chat

		// Highlights of the open chat and of the chat whose menu is open.
		openSel, menuSel switcher[string]
		chipSel          switcher[int]
		order            chatOrder // rows sliding to new places
	}

	conv struct {
		list                widget.List
		composer            widget.Editor
		video, search, menu widget.Clickable
		attach, emoji, send widget.Clickable
		header              widget.Clickable
		rows                []convRow
		rowsFor             *model.Chat
		rowsVer             int
		wallpaper           wallpaper
		nbsp                map[int]float32 // NBSP advance per text size in px

		reply            *model.Message // message being replied to
		mentions         []mentionRef   // @mentions picked for the draft
		mentionList      widget.List
		mentionDismissed string
		selecting        bool            // "Select" mode
		picked           map[string]bool // selected message IDs
		flash            string          // message highlighted after a jump
		flashUntil       time.Time
		composerH        int
		// scrollTo is a scroll position requested while the list may be
		// laying out (from a click inside a message); see scrollMessages.
		scrollTo *layout.Position
		// olderMore and newerMore report stored messages past either end
		// of msgs (see paging.go).
		olderMore, newerMore bool
		pinned               *model.Message  // shown in the pinned banner
		members              *model.ChatInfo // group members for @mentions
		membersFor           string

		// Animations. A ghost is what a part showed before it went away,
		// drawn while it fades out.
		replyAnim    tween
		replyGhost   *model.Message
		mentionAnim  tween
		mentionGhost *mentionState
		selAnim      tween
		selV         float32           // select mode's progress this frame
		sendAnim     tween             // the mic turning into the send button
		glide        glide             // smooth scroll to a message
		heights      map[int]int       // row heights laid out last frame, by index
		reactions    map[string]string // reaction shown per message, to pop new ones
	}
}

// New builds the UI on top of a backend. Call Start before the first frame.
func New(b model.Backend) *UI {
	u := &UI{th: newTheme(), now: time.Now, backend: b, syncPct: -1}
	u.SetDark(true)
	u.images = newImageCache(240, 32<<20)
	u.emojiImgs = newImageCache(600, 4<<20)
	u.clicks.m = make(map[string]*clickEntry)
	u.info.list.Axis = layout.Vertical
	u.status.list.Axis = layout.Vertical
	u.channel.list.Axis = layout.Vertical
	u.channel.search.SingleLine = true
	u.commun.list.Axis = layout.Vertical
	u.settings.list.Axis = layout.Vertical
	u.settings.search.SingleLine = true
	u.sidebar.search.SingleLine = true
	u.sidebar.list.Axis = layout.Vertical
	u.sidebar.rows = make(map[string]*widget.Clickable)
	u.conv.list.Axis = layout.Vertical
	u.conv.list.ScrollToEnd = true
	u.conv.composer.Submit = true
	u.conv.mentionList.Axis = layout.Vertical
	u.hovered = make(map[string]bool)
	return u
}

// Start loads the stored chats and starts the backend. notify is called
// (from any goroutine) whenever the UI should redraw.
func (u *UI) Start(notify func()) {
	u.images.invalidate = notify
	u.emojiImgs.invalidate = notify
	u.setChats(u.backend.Chats())
	u.loadPages()
	u.backend.Start(notify)
}

// loadPages reads what the Status, Channels and Communities pages show.
func (u *UI) loadPages() {
	u.statuses = u.backend.Statuses()
	u.channels = u.backend.Channels()
	u.suggested = u.backend.SuggestedChannels()
	u.communities = u.backend.Communities()
}

// Preview loads stored chats without starting the backend, for rendering
// screenshots of a real session without connecting to WhatsApp.
func (u *UI) Preview() {
	u.setChats(u.backend.Chats())
	u.loadPages()
	u.conn = model.ConnEvent{State: model.StateOnline}
}

// SetMe sets the user's own name and JID (used for screenshots).
func (u *UI) SetMe(name, id string) { u.me, u.meID = name, id }

// ShowPage switches the navigation rail to one of "chats", "archived",
// "calls", "status", "channels", "communities" or "settings".
func (u *UI) ShowPage(name string) {
	pages := map[string]page{"chats": pageChats, "archived": pageChats, "calls": pageCalls, "status": pageStatus,
		"channels": pageChannels, "communities": pageCommunities, "settings": pageSettings}
	u.setPage(pages[name])
	u.sidebar.showArchived = name == "archived"
}

// ShowStatus opens the status viewer on the i-th poster (used for screenshots).
func (u *UI) ShowStatus(i int) {
	u.setPage(pageStatus)
	if i < len(u.statuses) {
		u.status.viewer.show(u.statuses[i])
	}
}

// ShowInfo opens the info panel of the selected chat, scrolled to the
// given list item and pixel offset (used for screenshots).
func (u *UI) ShowInfo(first, offset int) {
	if u.selected == nil {
		return
	}
	u.openInfo(u.selected.ID)
	u.info.list.Position = layout.Position{First: first, Offset: offset}
}

func (u *UI) setPage(pg page) {
	if u.page == pg {
		return
	}
	// Leaving a page counts as having seen it, as does opening it.
	for _, p := range []page{u.page, pg} {
		switch p {
		case pageStatus:
			u.statusSeen = u.now()
		case pageChannels:
			u.channelsSeen = u.now()
		}
	}
	u.page = pg
	u.hideInfo()
	u.status.viewer.close()
}

// SelectName opens the first chat with the given name.
func (u *UI) SelectName(name string) {
	for _, c := range u.chats {
		if c.Name == name {
			u.open(c)
			return
		}
	}
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
	richBlocks.m = nil // spans carry palette colors (mentions, links)
	u.th.Palette.ContrastBg = u.pal.Green
}

// Escape presses Esc, closing the topmost overlay (used for screenshots).
func (u *UI) Escape() { u.escape() }

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

// SelectID opens the chat with the given ID.
func (u *UI) SelectID(id string) {
	u.applyEvents()
	if c := u.chatByID(id); c != nil {
		u.open(c)
	}
}

func (u *UI) open(c *model.Chat) {
	if u.selected != nil && u.selected.ID == c.ID && u.selPage == u.page {
		return
	}
	u.selPage = u.page
	if u.info.chatID != c.ID {
		u.hideInfo()
	}
	u.selected = c
	u.loadLatest()
	c.Unread = 0
	u.backend.Open(c.ID)
	u.conv.list.Position = layout.Position{}
	u.conv.list.ScrollToEnd = true
	u.conv.composer.SetText("")
	u.conv.reply, u.conv.mentions = nil, nil
	u.conv.reactions = nil
	u.endSelect()
	u.resetComposerAnims()
	u.hideViewer()
	u.closePicker()
	u.picker.anim.snap(false)
	u.stopVoice()
	u.attach.files = nil
}

// Run drives the window event loop until the window is closed.
func Run(w *app.Window, b model.Backend) error {
	u := New(b)
	u.window = w
	u.Start(w.Invalidate)
	defer b.Close()
	// Once nothing has been drawn for a while, give memory back (see
	// memtrim). Every frame pushes the trim back. Leaving the window trims
	// sooner, even while something on screen still animates.
	idle := time.AfterFunc(idleTrim, memtrim.Trim)
	defer idle.Stop()
	away := time.AfterFunc(awayTrim, memtrim.Trim)
	away.Stop()
	defer away.Stop()
	focused := true
	var ops op.Ops
	for {
		switch e := w.Event().(type) {
		case app.DestroyEvent:
			return e.Err
		case app.ConfigEvent:
			u.deco.Maximized = e.Config.Mode == app.Maximized
			if f := e.Config.Focused && e.Config.Mode != app.Minimized; f != focused {
				focused = f
				if f {
					away.Stop()
				} else {
					away.Reset(awayTrim)
				}
			}
		case app.FrameEvent:
			gtx := app.NewContext(&ops, e)
			u.Layout(gtx)
			e.Frame(gtx.Ops)
			idle.Reset(idleTrim)
		}
	}
}

// idleTrim is how long the window goes without a frame before memory is
// trimmed, and awayTrim how long after it loses focus or is minimized.
// A trim costs a few milliseconds of page faults on the next frames, as
// the pages still in use come back.
const (
	idleTrim = 10 * time.Second
	awayTrim = 3 * time.Second
)

// Layout draws one frame: custom title bar, then either the login screen or
// nav rail | chat list | conversation.
func (u *UI) Layout(gtx C) D {
	u.applyEvents()
	if a := u.deco.Update(gtx); a != 0 && u.window != nil {
		u.window.Perform(a)
	}
	defer u.images.endFrame()
	defer u.emojiImgs.endFrame()
	defer u.players.endFrame()
	defer u.anims.endFrame()
	defer u.endFrameClicks()

	sz := gtx.Constraints.Max
	u.winWidth = sz.X
	fillRect(gtx, image.Rectangle{Max: sz}, u.pal.Frame)
	tb := u.layoutTitleBar(gtx)
	defer op.Offset(image.Pt(0, tb.Size.Y)).Push(gtx.Ops).Pop()
	gtx.Constraints = layout.Exact(image.Pt(sz.X, sz.Y-tb.Size.Y))

	if !u.conn.State.LoggedIn() {
		u.updateLogin(gtx)
		u.layoutLogin(gtx)
		return D{Size: sz}
	}
	u.applyFocus(gtx)
	u.flushClipboard(gtx)
	u.update(gtx)
	u.layoutMain(gtx)
	u.layoutMenu(gtx)
	u.layoutFilterMenu(gtx)
	u.layoutStatusViewer(gtx)
	u.layoutViewer(gtx)
	if u.picker.shown() && u.picker.mode == pickReaction {
		u.layoutPicker(gtx, image.Point{}, gtx.Constraints.Max.X)
	}
	u.layoutCtxMenu(gtx)
	u.layoutDialog(gtx)
	u.layoutToast(gtx)
	u.trackMouse(gtx)
	u.flushClipboard(gtx) // copies requested during this frame's layout
	return D{Size: sz}
}

func (u *UI) layoutMain(gtx C) D {
	p := u.pal
	sz := gtx.Constraints.Max
	railW := gtx.Dp(railWidth)
	listW := int(float32(sz.X-railW) * 0.372)
	listW = max(gtx.Dp(280), min(listW, gtx.Dp(430)))
	// A new page's list fades in and rises a little into place.
	if u.page != u.pageSeen {
		u.pageSeen = u.page
		u.pageIn.snap(false)
	}
	pageV := easeOut(u.pageIn.step(gtx, true, durSwitch))

	rgtx := gtx
	rgtx.Constraints = layout.Exact(image.Pt(railW, sz.Y))
	u.layoutRail(rgtx)
	u.menu.anchor = image.Pt(railW+listW-gtx.Dp(20), gtx.Dp(58))
	// Chips row origin: panel border + header + search.
	u.filterMenu.origin = image.Pt(railW+1+gtx.Dp(21), 1+gtx.Dp(68+43+11+34+6))

	// The panels sit in a rounded, bordered card, like WhatsApp's.
	defer op.Offset(image.Pt(railW, 0)).Push(gtx.Ops).Pop()
	pw := sz.X - railW
	r := gtx.Dp(8)
	card := image.Rect(0, 0, pw+r, sz.Y+r)
	fillRRect(gtx, card, r, p.PanelBorder)
	inner := card.Add(image.Pt(1, 1))
	fillRRect(gtx, inner, r-1, p.Panel)
	// A rounded clip would be stenciled over the whole window every frame;
	// clip to the rectangle and round the corner off afterwards instead.
	defer roundCorner(gtx, r, p.PanelBorder, p.Frame)
	defer clip.Rect(inner).Push(gtx.Ops).Pop()

	gtx.Constraints = layout.Exact(image.Pt(pw, sz.Y))
	return layout.Flex{}.Layout(gtx,
		layout.Rigid(func(gtx C) D {
			gtx.Constraints = layout.Exact(image.Pt(listW, sz.Y))
			t := pushFx(gtx, 1, moveBy(0, float32(gtx.Dp(10))*(1-pageV)))
			d := layout.Inset{Top: 1, Left: 1}.Layout(gtx, u.layoutPageSidebar)
			t.Pop()
			u.veil(gtx, image.Rect(1, 1, listW, sz.Y), p.Panel, pageV)
			return d
		}),
		layout.Rigid(func(gtx C) D {
			gtx.Constraints = layout.Exact(image.Pt(max(1, gtx.Dp(1)), sz.Y))
			return fill(gtx, p.Divider)
		}),
		layout.Flexed(1, func(gtx C) D {
			return layout.Inset{Top: 1}.Layout(gtx, u.layoutRightPane)
		}),
	)
}

// layoutPageSidebar draws the list column of the selected page.
func (u *UI) layoutPageSidebar(gtx C) D {
	openID := ""
	if u.selected != nil && u.selPage == u.page {
		openID = u.selected.ID
	}
	u.sidebar.openSel.step(gtx, openID, durSwitch)
	switch u.page {
	case pageStatus:
		return u.layoutStatusList(gtx)
	case pageChannels:
		return u.layoutChannelList(gtx)
	case pageCommunities:
		return u.layoutCommunityList(gtx)
	case pageSettings:
		return u.layoutSettingsList(gtx)
	case pageCalls:
		return u.layoutCallsList(gtx)
	}
	return u.layoutSidebar(gtx)
}

// layoutRightPane draws the open conversation (with the info panel beside
// it), or the selected page's placeholder.
func (u *UI) layoutRightPane(gtx C) D {
	if u.selected != nil && u.selPage == u.page && u.page != pageStatus && u.page != pageSettings {
		if !u.info.shown() {
			return u.layoutConversation(gtx)
		}
		return u.layoutWithInfo(gtx)
	}
	d := u.layoutPlaceholder(gtx)
	u.veil(gtx, image.Rectangle{Max: d.Size}, u.pal.Panel, easeOut(u.pageIn.v)) // fades in with the page
	return d
}

// layoutPlaceholder draws the right pane of a page without an open chat.
func (u *UI) layoutPlaceholder(gtx C) D {
	switch u.page {
	case pageStatus:
		return u.emptyPane(gtx, func(gtx C, col color.NRGBA) D { return statusIcon(gtx, 56, col, true) },
			"Share statuses", "Share photos, videos and text that disappear after 24 hours.", "")
	case pageChannels:
		return u.emptyPane(gtx, func(gtx C, col color.NRGBA) D { return channelsIcon(gtx, 58, col, u.pal.Panel, true) },
			"Discover channels", "Entertainment, sports, news, lifestyle, people and more. Follow the channels that interest you", "")
	case pageCommunities:
		return u.emptyPane(gtx, iconGlyph(icGroupsFill, 72),
			"Create communities", "Bring members together in topic-based groups and easily send them admin announcements.",
			"Your personal messages in communities are end-to-end encrypted")
	case pageSettings:
		return u.emptyPane(gtx, iconGlyph(icSettings, 64), "Settings", "Manage your account, privacy, chats and notifications.", "")
	case pageCalls:
		return u.emptyPane(gtx, iconGlyph(icCallLine, 60), "Calls",
			"Calling from this app isn't supported yet. Use your phone to make and answer calls.", "")
	}
	return u.layoutEmpty(gtx)
}

// layoutWithInfo splits the pane between the conversation and the contact
// or group info panel, which takes about 30% of the window like WhatsApp's.
// The panel slides in from the right edge while the conversation narrows.
func (u *UI) layoutWithInfo(gtx C) D {
	sz := gtx.Constraints.Max
	v := easeOut(u.info.anim.step(gtx, u.info.open, durPanel))
	infoW := max(gtx.Dp(340), int(float32(u.winWidth)*0.3))
	infoW = min(infoW, sz.X)
	shown := lerpInt(0, infoW, v) // how much of the panel is on screen
	convW := sz.X - shown
	if sz.X-infoW < gtx.Dp(380) {
		// Too narrow to share: the panel covers the conversation.
		convW = sz.X
	}
	cgtx := gtx
	cgtx.Constraints = layout.Exact(image.Pt(convW, sz.Y))
	u.layoutConversation(cgtx)
	if shown == 0 {
		return D{Size: sz}
	}
	defer clip.Rect{Max: sz}.Push(gtx.Ops).Pop()
	t := op.Offset(image.Pt(sz.X-shown, 0)).Push(gtx.Ops)
	igtx := gtx
	if !u.info.open {
		var done func()
		igtx, done = fadeOut(igtx)
		defer done()
	}
	igtx.Constraints = layout.Exact(image.Pt(infoW, sz.Y))
	u.layoutInfo(igtx)
	t.Pop()
	return D{Size: sz}
}

// update handles input events before anything is drawn.
func (u *UI) update(gtx C) {
	for {
		ev, ok := gtx.Event(key.Filter{Name: key.NameEscape})
		if !ok {
			break
		}
		if e, ok := ev.(key.Event); ok && e.State == key.Press {
			u.escape()
		}
	}
	u.updateMenu(gtx)
	u.updateFilterMenu(gtx)
	if u.sidebar.menu.Clicked(gtx) {
		u.menu.open = !u.menu.open
	}
	for i := range u.sidebar.chips {
		if u.sidebar.chips[i].Clicked(gtx) {
			u.sidebar.filter = i
			u.sidebar.list.Position = layout.Position{}
		}
	}
	if u.rail.archived.Clicked(gtx) {
		u.sidebar.showArchived = u.page != pageChats || !u.sidebar.showArchived
		u.setPage(pageChats)
		u.sidebar.list.Position = layout.Position{}
	}
	if u.rail.chats.Clicked(gtx) || u.sidebar.back.Clicked(gtx) {
		u.sidebar.showArchived = false
		u.setPage(pageChats)
		u.sidebar.list.Position = layout.Position{}
	}
	for c, pg := range map[*widget.Clickable]page{&u.rail.calls: pageCalls, &u.rail.status: pageStatus,
		&u.rail.channels: pageChannels, &u.rail.communities: pageCommunities, &u.rail.profile: pageSettings} {
		if c.Clicked(gtx) {
			u.setPage(pg)
		}
	}
	if u.conv.header.Clicked(gtx) && u.selected != nil && !isChannelID(u.selected.ID) {
		if u.info.open {
			u.info.open = false
		} else {
			u.openInfo(u.selected.ID)
		}
	}
	for id, click := range u.sidebar.rows {
		if click.Clicked(gtx) {
			if c := u.chatByID(id); c != nil {
				u.open(c)
			}
		}
	}
	if ms := u.mentionQuery(); ms == nil {
		u.conv.mentionDismissed = ""
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

// escape closes the topmost overlay, like WhatsApp's Esc.
func (u *UI) escape() {
	switch {
	case u.ctx.isOpen():
		u.closeMenu()
	case u.dialog.isOpen():
		u.closeDialog()
	case u.picker.open:
		u.closePicker()
	case u.viewer.open:
		u.closeViewer()
	case u.mentionQuery() != nil:
		ms := u.mentionQuery()
		u.conv.mentionDismissed = string([]rune(u.conv.composer.Text())[ms.start:ms.end])
	case u.conv.selecting:
		u.endSelect()
	case u.conv.reply != nil:
		u.conv.reply = nil
	case u.status.viewer.isOpen():
		u.status.viewer.close()
	}
}

// applyEvents drains the backend queue into UI state.
func (u *UI) applyEvents() {
	for _, ev := range u.backend.Poll() {
		switch e := ev.(type) {
		case model.ConnEvent:
			me, meID := u.me, u.meID
			if e.Me != "" {
				me = e.Me
			}
			if e.MeID != "" {
				meID = e.MeID
			}
			if e.State == model.StateOnline && u.conn.State != model.StateOnline {
				u.images.retryMissing() // downloads were skipped while offline
			}
			u.conn, u.me, u.meID = e, me, meID
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
		case model.AvatarEvent:
			u.images.forget("a:" + e.ID)
		case model.MediaEvent:
			u.images.forget("m:" + e.ChatID + "/" + e.MsgID)
			if e.ChatID == statusChatID {
				u.images.forget("sm:" + e.MsgID)
			}
			if u.viewer.open && u.viewer.msgID == e.MsgID {
				u.forgetViewerImage(e.MsgID)
			}
			u.videoDownloaded(e)
			u.fileDownloaded(e)
		case model.NoticeEvent:
			u.toast(e.Text)
		case model.DeletedEvent:
			if u.selected != nil && u.selected.ID == e.ChatID {
				u.reloadMessages()
			}
		case model.InfoEvent:
			if u.conv.membersFor == e.ChatID {
				u.conv.membersFor = ""
			}
			if u.info.open && u.info.chatID == e.ChatID {
				u.info.data = u.backend.Info(e.ChatID)
			}
		case model.StatusEvent:
			u.statuses = u.backend.Statuses()
		case model.StickersEvent:
			u.picker.stickersOK = [3]bool{} // reloaded when next drawn
		case model.ChannelsEvent:
			u.channels = u.backend.Channels()
			u.suggested = u.backend.SuggestedChannels()
		case model.CommunitiesEvent:
			u.communities = u.backend.Communities()
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
		if isChannelID(u.selected.ID) {
			sel = u.selected // channels aren't in the chat list
		}
		if sel == nil {
			u.selected = nil
			return
		}
		u.selected = sel
		u.reloadMessages()
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
			u.sidebar.order.pending = true
			return
		}
	}
	u.chats = append(u.chats, c)
	u.sortChats()
	u.sidebar.order.pending = true
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
		if !m.FromMe {
			c.Typing = ""
		}
		u.sortChats()
		u.sidebar.order.pending = true
	}
	if u.selected == nil || u.selected.ID != m.ChatID {
		return
	}
	if m.Pinned || u.conv.pinned != nil && u.conv.pinned.ID == m.ID {
		u.conv.pinned = u.backend.PinnedMessage(m.ChatID)
	}
	for i, old := range u.msgs {
		if old.ID == m.ID {
			u.msgs[i] = m
			u.msgsVer++
			return
		}
	}
	i := sort.Search(len(u.msgs), func(i int) bool { return u.msgs[i].Time.After(m.Time) })
	switch {
	case i == len(u.msgs) && u.conv.newerMore && m.FromMe:
		// What you send shows with the newest messages.
		u.loadLatest()
		u.upsertMessage(m)
		return
	case i == 0 && u.conv.olderMore, i == len(u.msgs) && u.conv.newerMore:
		return // not loaded yet; it loads with its page
	}
	u.msgsVer++
	if i == len(u.msgs) && u.now().Sub(m.Time) < time.Minute {
		// A new message slides in at the bottom (history arrives older).
		u.anims.start(animKey{id: m.ID, tag: tagAppear})
	}
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

// ShowOverlay opens a menu, picker or dialog for screenshots: "chatmenu",
// "msgmenu", "emoji", "sticker", "viewer", "forward", "reply", "delete",
// "select", "attach", "poll", "contacts" or "tray".
// Menus open at (x, y) px in content coordinates.
func (u *UI) ShowOverlay(name string, x, y int) {
	u.applyEvents()
	u.mouse = image.Pt(x, y)
	var lastIn, lastOut, img *model.Message
	for _, m := range u.msgs {
		switch {
		case m.Kind == model.KindImage:
			img = m
		}
		if m.FromMe {
			lastOut = m
		} else {
			lastIn = m
		}
	}
	switch name {
	case "chatmenu":
		if len(u.chats) > 1 {
			u.openChatMenu(u.chats[1])
		}
	case "msgmenu":
		if lastIn != nil {
			u.openMessageMenu(lastIn)
		}
	case "emoji":
		u.openPicker(pickComposer, nil)
	case "sticker":
		u.openPicker(pickComposer, nil)
		u.picker.tab, u.picker.stickerSet = tabSticker, u.defaultStickerSet()
	case "viewer":
		if img != nil {
			u.openViewer(img)
		}
	case "forward":
		if lastIn != nil {
			u.openForward([]*model.Message{lastIn})
		}
	case "reply":
		if lastIn != nil {
			u.startReply(lastIn)
		}
	case "delete":
		if lastOut != nil {
			u.confirmDelete([]*model.Message{lastOut})
		}
	case "select":
		if lastIn != nil {
			u.startSelect(lastIn)
		}
	case "attach":
		u.openAttachMenu()
	case "poll":
		u.openPoll()
	case "contacts":
		u.openContactPicker()
	case "tray":
		u.attach.files = []model.Attachment{{Path: "beach.jpg", Media: model.MediaImage},
			{Path: "Quarterly report.pdf", Media: model.MediaDocument}}
		u.conv.composer.SetText("From last weekend")
	case "mention", "mentioned":
		// The mention picker, or a draft with picked mentions.
		ed := &u.conv.composer
		ed.SetText("Hi @")
		ed.SetCaret(4, 4)
		u.requestFocus(ed)
		if name == "mentioned" {
			u.pickMention(0)
			ed.Insert("and ")
			ed.SetText(ed.Text() + "@")
			ed.SetCaret(ed.Len(), ed.Len())
			if ms := u.mentionQuery(); ms != nil {
				u.pickMention(len(ms.members) - 1)
			}
			ed.Insert("see you soon")
		}
	}
}
