package ui

import (
	"image"
	"image/color"
	"strings"
	"unicode"

	"gioui.org/font"
	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/text"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/chomosuke9/wazzapclients/internal/model"
	"github.com/chomosuke9/wazzapclients/internal/ui/styledtext"
)

// chatSearchState is the "Search messages" panel, which takes the info
// panel's place beside a conversation (the header's search button and
// the info panel's Search pill open it).
type chatSearchState struct {
	open     bool
	chatID   string
	query    widget.Editor
	ran      string // the query results are for
	results  []*model.Message
	list     widget.List
	closeBtn widget.Clickable
	anim     tween              // sliding in and out
	carets   []styledtext.Caret // reused by searchResultText
	// covers is set while the panel covers the whole conversation (a
	// narrow window), so a jump to a result closes it.
	covers bool
}

// shown reports whether the panel is open or still sliding away.
func (s *chatSearchState) shown() bool { return s.open || s.anim.v > 0 }

// searchLimit is how many results a search lists, newest first.
const searchLimit = 200

// openChatSearch opens the search panel for the selected chat, in place
// of the info panel if that is open.
func (u *UI) openChatSearch() {
	if u.selected == nil {
		return
	}
	s := &u.search
	if u.info.open {
		u.hideInfo()
		s.anim.snap(true)
	}
	if s.chatID != u.selected.ID {
		s.query.SetText("")
		s.ran, s.results = "", nil
		s.list.Position = layout.Position{}
	}
	s.chatID = u.selected.ID
	s.open = true
	u.requestFocus(&s.query)
}

// hideChatSearch closes the panel at once, for when the chat or page
// changes under it.
func (u *UI) hideChatSearch() {
	u.search.open = false
	u.search.anim.snap(false)
}

// swapSearchForInfo closes the search panel at once when the info panel
// opens, so one replaces the other instead of sliding.
func (u *UI) swapSearchForInfo() {
	if u.search.open {
		u.hideChatSearch()
		u.info.anim.snap(true)
	}
}

// runChatSearch runs the query when it changed since the last frame.
func (u *UI) runChatSearch() {
	s := &u.search
	q := trimSpace(s.query.Text())
	if q == s.ran {
		return
	}
	s.ran = q
	s.results = nil
	s.list.Position = layout.Position{}
	if q != "" {
		s.results = u.backend.SearchMessages(s.chatID, q, searchLimit)
	}
}

func (u *UI) layoutChatSearch(gtx C) D {
	p := u.pal
	s := &u.search
	if s.closeBtn.Clicked(gtx) {
		s.open = false
	}
	u.runChatSearch()
	dims := fill(gtx, p.Panel)
	c := u.chatByID(s.chatID)
	layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx C) D {
			return vcenter(gtx, gtx.Dp(64), func(gtx C) D {
				return layout.Inset{Left: 11.5}.Layout(gtx, func(gtx C) D {
					return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
						layout.Rigid(func(gtx C) D { return u.iconButton(gtx, &s.closeBtn, icClose, 40, 25, p.IconStrong) }),
						layout.Rigid(layout.Spacer{Width: 10}.Layout),
						layout.Flexed(1, u.label(16.5, "Search messages", p.Text, labelOpts{maxLines: 1}).Layout),
					)
				})
			})
		}),
		layout.Rigid(func(gtx C) D {
			return layout.Inset{Left: 16, Right: 16, Top: 2, Bottom: 10}.Layout(gtx, func(gtx C) D {
				return u.searchField(gtx, &s.query, "Search")
			})
		}),
		layout.Flexed(1, func(gtx C) D {
			var hint string
			switch {
			case s.ran == "" && c != nil && c.IsGroup:
				hint = "Search for messages within " + c.Name + "."
			case s.ran == "" && c != nil:
				hint = "Search for messages with " + c.Name + "."
			case s.ran == "":
				hint = "Search for messages in this chat."
			case len(s.results) == 0:
				hint = "No messages found"
			}
			if hint != "" {
				return layout.Inset{Left: 40, Right: 40, Top: 48}.Layout(gtx, func(gtx C) D {
					gtx.Constraints.Min.X = gtx.Constraints.Max.X
					return u.label(15, hint, p.TextSecondary, labelOpts{align: text.Middle}).Layout(gtx)
				})
			}
			return u.scrollList(gtx, &s.list, len(s.results), func(gtx C, i int) D {
				return u.searchResult(gtx, c, s.results[i], s.ran)
			})
		}),
	)
	return dims
}

// searchResult is one found message: its date, then the text (after the
// sender's name in a group) with the matches in green.
func (u *UI) searchResult(gtx C, c *model.Chat, m *model.Message, q string) D {
	p := u.pal
	btn := u.btn("search:" + m.ID)
	if btn.Clicked(gtx) {
		if u.search.covers {
			u.search.open = false
		}
		u.jumpTo(m.ID)
	}
	gtx.Constraints.Min.X = gtx.Constraints.Max.X
	d := layout.Inset{Left: 8, Right: 14}.Layout(gtx, func(gtx C) D {
		return clickable(gtx, btn, func(gtx C) D {
			gtx.Constraints.Min.X = gtx.Constraints.Max.X
			bg := mix(p.Panel, p.RowHover, u.hover(gtx, btn))
			return background(gtx, bg, 10, func(gtx C) D {
				return layout.Inset{Left: 14, Right: 14, Top: 11, Bottom: 12}.Layout(gtx, func(gtx C) D {
					return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
						layout.Rigid(u.label(13.5, listTime(m.Time, u.now()), p.TextSecondary, labelOpts{maxLines: 1}).Layout),
						layout.Rigid(layout.Spacer{Height: 4}.Layout),
						layout.Rigid(func(gtx C) D { return u.searchResultText(gtx, c, m, q) }),
					)
				})
			})
		})
	})
	// A divider between results, like WhatsApp's.
	h := max(1, gtx.Dp(1))
	fillRect(gtx, image.Rect(gtx.Dp(22), d.Size.Y-h, d.Size.X-gtx.Dp(28), d.Size.Y), p.Hover)
	return d
}

// searchResultText is a result's receipt or media glyph and its text, at
// most two lines, starting a little before the first match when that is
// far into the message.
func (u *UI) searchResultText(gtx C, c *model.Chat, m *model.Message, q string) D {
	p := u.pal
	const size = unit.Sp(15.5)
	var row []layout.FlexChild
	glyph := func(w layout.Widget) {
		row = append(row, layout.Rigid(func(gtx C) D { return layout.Inset{Right: 4, Top: 1}.Layout(gtx, w) }))
	}
	if m.FromMe {
		ic, col := receiptIcon(m.Receipt, p, false)
		glyph(iconW(ic, 18, col))
	}
	if m.Media != model.MediaNone {
		glyph(iconW(mediaIcon(m.Media), 18, p.TextSecondary))
	}
	plain := font.Font{Typeface: typeface}
	var prefix string
	if c != nil && c.IsGroup && !m.FromMe && m.Sender != "" {
		prefix = shortName(displayText(m.Sender)) + ": "
	}
	txt := []rune(strings.Join(strings.Fields(displayText(plainText(m.Text))), " "))
	ranges := matchRanges(txt, []rune(q))
	if len(ranges) > 0 && ranges[0][0] > 40 {
		// Start at a word near 25 runes before the match.
		cut := ranges[0][0] - 25
		for i := cut; i < ranges[0][0]-10; i++ {
			if txt[i-1] == ' ' {
				cut = i
				break
			}
		}
		txt = append([]rune("…"), txt[cut:]...)
		for i := range ranges {
			ranges[i][0] -= cut - 1
			ranges[i][1] -= cut - 1
		}
	}
	// The ranges count the prefix too, as carets do.
	base := len([]rune(prefix))
	for i := range ranges {
		ranges[i][0] += base
		ranges[i][1] += base
	}
	spans := func(col color.NRGBA) []styledtext.SpanStyle {
		return []styledtext.SpanStyle{
			{Font: plain, Size: size, Color: p.TextSecondary, Content: prefix},
			{Font: plain, Size: size, Color: col, Content: string(txt)},
		}
	}
	row = append(row, layout.Flexed(1, func(gtx C) D {
		gtx.Constraints.Min.X = gtx.Constraints.Max.X
		const lineH = 21
		// The matches are the same text drawn again in green, clipped to
		// each match: one span keeps words whole when lines wrap.
		st := styledtext.Text(u.th.Shaper, spans(p.Text)...)
		st.LineHeight, st.LineHeightScale = lineH, 1
		carets := u.search.carets[:0]
		st.Carets = &carets
		body := record(gtx, func(gtx C) D { return st.Layout(gtx, nil) })
		u.search.carets = carets
		st.Styles, st.Carets = spans(p.Green), nil
		green := record(gtx, func(gtx C) D { return st.Layout(gtx, nil) })
		h := min(body.size.Y, gtx.Sp(2*lineH))
		defer clip.Rect{Max: image.Pt(body.size.X, h)}.Push(gtx.Ops).Pop()
		body.at(gtx, 0, 0)
		for _, r := range matchRects(carets, ranges) {
			cl := clip.Rect(r).Push(gtx.Ops)
			green.at(gtx, 0, 0)
			cl.Pop()
		}
		return D{Size: image.Pt(gtx.Constraints.Max.X, h)}
	}))
	return layout.Flex{}.Layout(gtx, row...)
}

// matchRects returns the boxes of the text between rune ranges, one per
// line a range is on, from the text's carets.
func matchRects(cs []styledtext.Caret, ranges [][2]int) []image.Rectangle {
	var out []image.Rectangle
	for _, rg := range ranges {
		var cur image.Rectangle
		for i := 0; i+1 < len(cs); i++ {
			a, b := cs[i], cs[i+1]
			if a.Top != b.Top || a.Rune < rg[0] || a.Rune >= rg[1] {
				continue
			}
			r := image.Rect(a.X, a.Top, b.X, a.Bottom)
			if cur.Empty() || cur.Min.Y != r.Min.Y {
				if !cur.Empty() {
					out = append(out, cur)
				}
				cur = r
			} else {
				cur = cur.Union(r)
			}
		}
		if !cur.Empty() {
			out = append(out, cur)
		}
	}
	return out
}

// matchRanges finds every place q appears in s, ignoring case, as rune
// index ranges.
func matchRanges(s, q []rune) [][2]int {
	if len(q) == 0 {
		return nil
	}
	fold := func(r rune) rune { return unicode.ToLower(r) }
	var out [][2]int
	for i := 0; i+len(q) <= len(s); {
		j := 0
		for j < len(q) && fold(s[i+j]) == fold(q[j]) {
			j++
		}
		if j == len(q) {
			out = append(out, [2]int{i, i + len(q)})
			i += len(q)
			continue
		}
		i++
	}
	return out
}

// membersHeaderRow is the group info's member count with a search
// button, which turns the row into a search field that filters the
// member list.
func (u *UI) membersHeaderRow(gtx C, count string) D {
	p := u.pal
	s := &u.info
	open, closeBtn := u.btn("info:membersearch"), u.btn("info:membersearch-close")
	if open.Clicked(gtx) {
		s.memberSearch = true
		s.memberQuery.SingleLine = true
		s.memberQuery.SetText("")
		u.requestFocus(&s.memberQuery)
	}
	if closeBtn.Clicked(gtx) {
		s.memberSearch = false
	}
	if !s.memberSearch {
		return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
			layout.Flexed(1, u.label(15, count, p.TextSecondary, labelOpts{weight: font.SemiBold, maxLines: 1}).Layout),
			layout.Rigid(func(gtx C) D { return u.iconButton(gtx, open, icSearch, 40, 28, p.IconStrong) }),
		)
	}
	return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
		layout.Flexed(1, func(gtx C) D { return u.searchField(gtx, &s.memberQuery, "Search members") }),
		layout.Rigid(layout.Spacer{Width: 6}.Layout),
		layout.Rigid(func(gtx C) D { return u.iconButton(gtx, closeBtn, icClose, 40, 24, p.IconStrong) }),
	)
}

// filterMembers returns the members whose name contains q, ignoring case.
func filterMembers(ms []model.Member, q string) []model.Member {
	qr := []rune(trimSpace(q))
	if len(qr) == 0 {
		return ms
	}
	var out []model.Member
	for _, m := range ms {
		if len(matchRanges([]rune(plainText(m.Name)), qr)) > 0 {
			out = append(out, m)
		}
	}
	return out
}
