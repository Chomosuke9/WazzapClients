package ui

import (
	"slices"

	"gioui.org/layout"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

// The open chat's messages load a page at a time as the list nears an end
// of the loaded ones. At most perf.loadedMsgs (maxLoaded by default) stay
// loaded: a page loaded at one end drops messages from the other, far off
// screen.
const (
	messagePage = 100
	maxLoaded   = 400
	loadAhead   = 30 // rows from an end of the list at which the next page loads
)

// loadLatest loads the newest page of the open chat and scrolls to it.
func (u *UI) loadLatest() {
	c := u.selected
	u.msgs, u.conv.olderMore = more(u.backend.Messages(c.ID, messagePage+1), messagePage, true)
	u.msgsVer++
	u.conv.newerMore = false
	u.conv.list.ScrollToEnd = true
	u.conv.scrollTo = &layout.Position{}
	u.conv.glide = glide{}
	u.conv.pinned = u.backend.PinnedMessage(c.ID)
}

// reloadMessages loads the open chat's messages again, keeping the same
// stretch of the chat loaded.
func (u *UI) reloadMessages() {
	c := u.selected
	switch {
	case len(u.msgs) > 0 && u.conv.newerMore:
		ms := u.backend.MessagesFrom(c.ID, u.msgs[0].ID, len(u.msgs))
		if len(ms) == 0 {
			u.loadLatest()
			return
		}
		u.msgs = ms
	default:
		n := max(len(u.msgs), messagePage)
		u.msgs, u.conv.olderMore = more(u.backend.Messages(c.ID, n+1), n, true)
	}
	u.msgsVer++
	u.conv.pinned = u.backend.PinnedMessage(c.ID)
}

// pageMessages loads the next page when the list, as laid out last frame,
// is near an end of the loaded messages.
func (u *UI) pageMessages(c *model.Chat) {
	if len(u.msgs) == 0 || u.conv.scrollTo != nil {
		return
	}
	pos := u.conv.list.Position
	switch {
	case u.conv.olderMore && pos.First < loadAhead:
		var older []*model.Message
		older, u.conv.olderMore = more(u.backend.MessagesBefore(c.ID, u.msgs[0].ID, messagePage+1), messagePage, true)
		msgs := append(older, u.msgs...)
		if drop := len(msgs) - perf.loadedMsgs; drop > 0 {
			msgs = slices.Clone(msgs[:len(msgs)-drop]) // let the dropped ones go
			u.conv.newerMore = true
		}
		u.setMsgs(c, msgs)
	case u.conv.newerMore && pos.First+pos.Count > len(u.rows(c))-loadAhead:
		newer := u.backend.MessagesFrom(c.ID, u.msgs[len(u.msgs)-1].ID, messagePage+2)
		if len(newer) == 0 {
			// The last loaded message is gone from the store.
			u.loadLatest()
			return
		}
		newer, u.conv.newerMore = more(newer[1:], messagePage, false)
		msgs := append(u.msgs[:len(u.msgs):len(u.msgs)], newer...)
		if drop := len(msgs) - perf.loadedMsgs; drop > 0 {
			msgs = slices.Clone(msgs[drop:])
			u.conv.olderMore = true
		}
		u.setMsgs(c, msgs)
	}
}

// setMsgs replaces the loaded messages, keeping the first message on screen
// where it is.
func (u *UI) setMsgs(c *model.Chat, msgs []*model.Message) {
	l := &u.conv.list
	old := u.rows(c)
	first := l.Position.First
	anchor := slices.IndexFunc(old[min(first, len(old)):], func(r convRow) bool { return r.msg != nil })
	// y is the anchor's distance from the list's top, if the rows above it
	// were laid out last frame.
	y, known := -l.Position.Offset, true
	for j := first; j < first+anchor && known; j++ {
		h, ok := u.conv.heights[j]
		y, known = y+h, ok
	}
	u.msgs = msgs
	u.msgsVer++
	// The list sticks to its end only when that is the newest message.
	l.ScrollToEnd = !u.conv.newerMore
	if anchor < 0 {
		return
	}
	anchor += first
	id := old[anchor].msg.ID
	for k, r := range u.rows(c) {
		if !r.has(id) {
			continue
		}
		if known {
			l.Position.First, l.Position.Offset = k, -y
		} else {
			l.Position.First = max(0, k-(anchor-first))
		}
		shift := k - anchor
		if g := &u.conv.glide; g.pending || g.active {
			g.first = max(0, g.first+shift)
		}
		heights := make(map[int]int, len(u.conv.heights))
		for i, h := range u.conv.heights {
			heights[i+shift] = h
		}
		u.conv.heights = heights
		break
	}
	if w := u.wheels[&l.List]; w != nil {
		w.at = l.Position // not a jump: keep easing the wheel
	}
}

// loadAround loads the messages around message id and returns its row, or
// -1 when it isn't stored.
func (u *UI) loadAround(id string) int {
	c := u.selected
	from, newerMore := more(u.backend.MessagesFrom(c.ID, id, messagePage/2+1), messagePage/2, false)
	if len(from) == 0 {
		return -1
	}
	before, olderMore := more(u.backend.MessagesBefore(c.ID, id, messagePage/2+1), messagePage/2, true)
	u.msgs = append(before, from...)
	u.msgsVer++
	u.conv.olderMore, u.conv.newerMore = olderMore, newerMore
	u.conv.list.ScrollToEnd = !u.conv.newerMore
	clear(u.conv.heights) // by row index, of the old rows
	for i, r := range u.rows(c) {
		if r.has(id) {
			// Set while the list may be laying out; see scrollMessages.
			u.conv.scrollTo = &layout.Position{First: max(0, i-2), BeforeEnd: true}
			return i
		}
	}
	return -1
}

// more trims msgs, fetched with one extra message, to n and reports whether
// it had the extra one: the oldest when older is true, else the newest.
func more(msgs []*model.Message, n int, older bool) ([]*model.Message, bool) {
	if len(msgs) <= n {
		return msgs, false
	}
	if older {
		return msgs[len(msgs)-n:], true
	}
	return msgs[:n], true
}
