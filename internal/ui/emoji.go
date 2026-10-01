package ui

import (
	"image"
	"image/color"
	"strings"

	"gioui.org/font"
	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/text"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"github.com/chomosuke9/wazzapclients/internal/model"
	"github.com/chomosuke9/wazzapclients/internal/ui/icon"
)

type emoji struct {
	char, name, codes string
}

var emojis = func() []emoji {
	lines := strings.Split(strings.TrimSpace(emojiTable), "\n")
	out := make([]emoji, len(lines))
	for i, l := range lines {
		f := strings.SplitN(l, "\t", 3)
		out[i] = emoji{char: f[0], name: f[1]}
		if len(f) > 2 {
			out[i].codes = f[2]
		}
	}
	return out
}()

// emojiCategory is a tab of the picker. WhatsApp puts Activities before
// Travel, unlike Unicode.
type emojiCategory struct {
	name   string
	ic     *icon.Icon
	start  int // range in emojis
	end    int
	recent bool
}

var emojiCategories = func() []emojiCategory {
	g := emojiGroupStarts
	end := func(i int) int {
		if i+1 < len(g) {
			return g[i+1]
		}
		return len(emojis)
	}
	span := func(name string, ic *icon.Icon, i int) emojiCategory {
		return emojiCategory{name: name, ic: ic, start: g[i], end: end(i)}
	}
	return []emojiCategory{
		{name: "Recent", ic: icClock, recent: true},
		span("Smileys & People", icEmojiPeople, 0),
		span("Animals & Nature", icEmojiNature, 1),
		span("Food & Drink", icEmojiFood, 2),
		span("Activity", icEmojiSport, 4),
		span("Travel & Places", icEmojiTravel, 3),
		span("Objects", icEmojiObjects, 5),
		span("Symbols", icEmojiSymbols, 6),
		span("Flags", icEmojiFlags, 7),
	}
}()

type pickMode int

const (
	pickComposer pickMode = iota
	pickReaction
)

type pickTab int

const (
	tabEmoji pickTab = iota
	tabGIF
	tabSticker
)

// emojiPicker is the panel above the composer (or for "+" reactions).
type emojiPicker struct {
	open   bool
	mode   pickMode
	target *model.Message // message to react to
	tab    pickTab
	search widget.Editor
	list   widget.List
	scrim  widget.Clickable
	active int // highlighted category
	recent []string
	loaded bool

	stickerSet  model.StickerSet
	stickers    [3][]*model.Message // per StickerSet
	stickersOK  [3]bool             // stickers[i] is loaded
	stickerSel  switcher[int]
	stickerLine follower

	anim      tween // opening and closing
	catSel    switcher[int]
	underline follower // x of the active category's underline
	tabSel    switcher[pickTab]
}

// shown reports whether the picker is open or still fading out.
func (e *emojiPicker) shown() bool { return e.open || e.anim.v > 0 }

const recentEmojiMax = 36

func (u *UI) openPicker(mode pickMode, target *model.Message) {
	e := &u.picker
	e.open, e.mode, e.target, e.tab = true, mode, target, tabEmoji
	e.search.SetText("")
	e.search.SingleLine = true
	e.list.Axis = layout.Vertical
	e.list.Position = layout.Position{}
	e.catSel, e.underline, e.tabSel = switcher[int]{}, follower{}, switcher[pickTab]{} // no sliding from last time
	e.stickerSel, e.stickerLine = switcher[int]{}, follower{}
	if !e.loaded {
		e.loaded = true
		e.recent = strings.Fields(u.backend.Pref("recent_emoji"))
	}
	if mode == pickReaction {
		u.requestFocus(&e.search)
	}
}

func (u *UI) closePicker() { u.picker.open = false }

// pickEmoji inserts or sends the chosen emoji and remembers it as recent.
func (u *UI) pickEmoji(ch string) {
	e := &u.picker
	rec := []string{ch}
	for _, r := range e.recent {
		if r != ch && len(rec) < recentEmojiMax {
			rec = append(rec, r)
		}
	}
	e.recent = rec
	u.backend.SetPref("recent_emoji", strings.Join(rec, " "))
	if e.mode == pickReaction {
		if e.target != nil {
			u.backend.React(e.target, ch)
		}
		u.closePicker()
		return
	}
	u.conv.composer.Insert(ch)
	u.requestFocus(&u.conv.composer)
}

// pickerRow is one line of the emoji list: a heading or up to cols emoji.
type pickerRow struct {
	label string
	items []string
	cat   int // category the row belongs to
}

func (u *UI) pickerRows(cols int) []pickerRow {
	e := &u.picker
	var rows []pickerRow
	grid := func(chars []string, cat int) {
		for i := 0; i < len(chars); i += cols {
			rows = append(rows, pickerRow{items: chars[i:min(i+cols, len(chars))], cat: cat})
		}
	}
	if q := strings.ToLower(trimSpace(e.search.Text())); q != "" {
		var hits []string
		for _, em := range emojis {
			if strings.Contains(em.name, q) || strings.Contains(em.codes, q) {
				hits = append(hits, em.char)
			}
		}
		if len(hits) == 0 {
			return []pickerRow{{label: "No emoji found"}}
		}
		grid(hits, 0)
		return rows
	}
	for ci, c := range emojiCategories {
		var chars []string
		if c.recent {
			chars = e.recent
		} else {
			for _, em := range emojis[c.start:c.end] {
				chars = append(chars, em.char)
			}
		}
		if len(chars) == 0 {
			continue
		}
		rows = append(rows, pickerRow{label: c.name, cat: ci})
		grid(chars, ci)
	}
	return rows
}

// layoutPicker draws the open picker with its bottom-left corner at
// anchor (content coordinates).
func (u *UI) layoutPicker(gtx C, anchor image.Point, maxW int) {
	e := &u.picker
	if e.open {
		if e.scrim.Clicked(gtx) {
			u.closePicker()
		}
		for {
			ev, ok := gtx.Event(key.Filter{Focus: &e.search, Name: key.NameEscape})
			if !ok {
				break
			}
			if ke, ok := ev.(key.Event); ok && ke.State == key.Press {
				u.closePicker()
			}
		}
	}
	v := e.anim.step(gtx, e.open, popDur(e.open))
	if v == 0 {
		return
	}
	p := u.pal
	sz := gtx.Constraints.Max
	if e.open {
		sgtx := gtx
		sgtx.Constraints = layout.Exact(sz)
		e.scrim.Layout(sgtx, func(gtx C) D { return D{Size: gtx.Constraints.Max} })
	} else {
		var done func()
		gtx, done = fadeOut(gtx)
		defer done()
	}

	w := min(gtx.Dp(614), maxW)
	h := min(gtx.Dp(604), anchor.Y-gtx.Dp(8))
	if w < gtx.Dp(200) || h < gtx.Dp(200) {
		return
	}
	x, y := anchor.X, anchor.Y-h
	if e.mode == pickReaction {
		x, y = (sz.X-w)/2, (sz.Y-h)/2
	}
	rect := image.Rectangle{Max: image.Pt(w, h)}.Add(image.Pt(x, y))
	// Above the composer it rises into place; for reactions it grows from
	// the middle.
	ev := easeOut(v)
	if e.mode == pickReaction {
		defer pushFx(gtx, ev, scaleAt(rect.Min.Add(rect.Size().Div(2)), lerp(0.92, 1, ev))).Pop()
	} else {
		defer pushFx(gtx, ev, moveBy(0, float32(gtx.Dp(16))*(1-ev))).Pop()
	}
	r := gtx.Dp(16)
	fillRRect(gtx, rect.Add(image.Pt(0, gtx.Dp(3))).Inset(-gtx.Dp(2)), r+gtx.Dp(2), p.Shadow)
	borderRRect(gtx, rect, r, p.Picker, p.PopupBorder)
	defer op.Offset(rect.Min).Push(gtx.Ops).Pop()
	defer clip.UniformRRect(image.Rectangle{Max: rect.Size()}, r).Push(gtx.Ops).Pop()
	pg := gtx
	pg.Constraints = layout.Exact(rect.Size())
	// Swallow clicks so they don't reach the scrim.
	u.btn("picker:panel").Layout(pg, func(gtx C) D { return D{Size: gtx.Constraints.Max} })

	switch e.tab {
	case tabEmoji:
		u.layoutEmojiTab(pg)
	case tabGIF:
		u.pickerMessage(pg, "GIF search needs Tenor, which WazzapClients doesn't use.")
	case tabSticker:
		u.layoutStickerTab(pg)
	}
	if e.mode == pickComposer {
		u.pickerTabs(pg)
	}
}

func (u *UI) pickerMessage(gtx C, msg string) D {
	return layout.Center.Layout(gtx, func(gtx C) D {
		gtx.Constraints.Max.X = min(gtx.Constraints.Max.X, gtx.Dp(320))
		return u.label(15, msg, u.pal.TextSecondary, labelOpts{maxLines: 3, align: text.Middle}).Layout(gtx)
	})
}

func (u *UI) layoutEmojiTab(gtx C) D {
	e := &u.picker
	p := u.pal
	w := gtx.Constraints.Max.X
	pitch := gtx.Dp(48.7)
	cols := max(4, (w-gtx.Dp(24))/pitch)
	cellW := (w - gtx.Dp(24)) / cols // spread the columns across the panel
	rows := u.pickerRows(cols)
	// Category tabs jump to their heading.
	for ci := range emojiCategories {
		if u.btn("cat:" + itoa(ci+1)).Clicked(gtx) {
			e.search.SetText("")
			rows = u.pickerRows(cols)
			for i, r := range rows {
				if r.label != "" && r.cat == ci {
					e.list.Position = layout.Position{First: i}
					break
				}
			}
			e.active = ci
		}
	}
	for _, r := range rows {
		for _, ch := range r.items {
			if u.btn("emoji:" + ch).Clicked(gtx) {
				u.pickEmoji(ch) // may close it; it keeps drawing as it fades
			}
		}
	}
	if len(rows) > 0 && e.list.Position.First < len(rows) && trimSpace(e.search.Text()) == "" {
		e.active = rows[e.list.Position.First].cat
	}
	e.catSel.step(gtx, e.active, durSwitch)
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx C) D {
			ics := make([]*icon.Icon, len(emojiCategories))
			for i, c := range emojiCategories {
				ics[i] = c.ic
			}
			return u.pickerHeader(gtx, "cat:", ics, e.active, &e.catSel, &e.underline)
		}),
		layout.Rigid(func(gtx C) D {
			return layout.Inset{Left: 15, Right: 15, Top: 6, Bottom: 6}.Layout(gtx, func(gtx C) D {
				gtx.Constraints.Min.X = gtx.Constraints.Max.X
				hgt := gtx.Dp(46)
				border := p.PopupBorder
				if gtx.Focused(&e.search) {
					border = p.Green
				}
				rr := image.Rect(0, 0, gtx.Constraints.Max.X, hgt)
				fillRRect(gtx, rr, hgt/2, border)
				fillRRect(gtx, rr.Inset(gtx.Dp(2)), hgt/2-gtx.Dp(2), p.Popup)
				return vcenter(gtx, hgt, func(gtx C) D {
					return layout.Inset{Left: 18, Right: 16}.Layout(gtx, func(gtx C) D {
						return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
							layout.Rigid(iconW(icSearch, 22, p.TextSecondary)),
							layout.Rigid(layout.Spacer{Width: 16}.Layout),
							layout.Flexed(1, func(gtx C) D {
								ed := material.Editor(u.th, &e.search, "Search emoji")
								ed.TextSize = 15.5
								ed.Color = p.Text
								ed.HintColor = p.TextSecondary
								return ed.Layout(gtx)
							}),
						)
					})
				})
			})
		}),
		layout.Flexed(1, func(gtx C) D {
			return u.scrollList(gtx, &e.list, len(rows)+1, func(gtx C, i int) D {
				if i == len(rows) {
					return D{Size: image.Pt(0, gtx.Dp(56))} // room for the tabs
				}
				r := rows[i]
				if r.label != "" {
					top := unit.Dp(22)
					if i == 0 {
						top = 12
					}
					return layout.Inset{Left: 17, Top: top, Bottom: 10}.Layout(gtx,
						u.label(15.5, r.label, p.TextSecondary, labelOpts{weight: font.SemiBold, maxLines: 1}).Layout)
				}
				return layout.Inset{Left: 12}.Layout(gtx, func(gtx C) D {
					var children []layout.FlexChild
					for _, ch := range r.items {
						ch := ch
						children = append(children, layout.Rigid(func(gtx C) D {
							cl := u.btn("emoji:" + ch)
							return clickable(gtx, cl, func(gtx C) D {
								if h := u.hover(gtx, cl); h > 0 {
									fillRRect(gtx, image.Rect(0, 0, cellW, pitch), gtx.Dp(8), faded(p.PopupHover, h))
								}
								if u.layoutEmojiImage(gtx, ch, gtx.Sp(29), image.Pt(cellW, pitch)) {
									return D{Size: image.Pt(cellW, pitch)}
								}
								return centerIn2(gtx, cellW, pitch, u.label(29, ch, p.Text).Layout)
							})
						}))
					}
					return layout.Flex{}.Layout(gtx, children...)
				})
			})
		}),
	)
}

// pickerTabs is the Emoji / GIF / Sticker switch floating at the bottom.
func (u *UI) pickerTabs(gtx C) {
	e := &u.picker
	p := u.pal
	for i := range 3 {
		if u.btn("ptab:" + itoa(i+1)).Clicked(gtx) {
			e.tab = pickTab(i)
			if e.tab == tabSticker {
				e.stickerSet = u.defaultStickerSet()
				e.list.Position = layout.Position{}
			}
		}
	}
	e.tabSel.step(gtx, e.tab, durSwitch)
	segW, h := gtx.Dp(77), gtx.Dp(33)
	w := 3 * segW
	sz := gtx.Constraints.Max
	x, y := (sz.X-w)/2, sz.Y-h-gtx.Dp(10)
	defer op.Offset(image.Pt(x, y)).Push(gtx.Ops).Pop()
	borderRRect(gtx, image.Rect(0, 0, w, h), h/2, p.Picker, p.PickerTabBorder)
	for i := range 3 {
		i := i
		t := op.Offset(image.Pt(i*segW, 0)).Push(gtx.Ops)
		cg := gtx
		cg.Constraints = layout.Exact(image.Pt(segW, h))
		cl := u.btn("ptab:" + itoa(i+1))
		clickable(cg, cl, func(gtx C) D {
			r := image.Rect(1, 1, segW-1, h-1)
			a := max(e.tabSel.of(pickTab(i)), 0.5*u.hover(gtx, cl))
			if a > 0 {
				rr := clip.RRect{Rect: r}
				switch i {
				case 0:
					rr.NW, rr.SW = h/2, h/2
				case 2:
					rr.NE, rr.SE = h/2, h/2
				}
				paintRRect(gtx, rr, faded(p.PickerTab, a))
			}
			a = e.tabSel.of(pickTab(i))
			glyph := func(col color.NRGBA) {
				switch i {
				case 0:
					centerIn2(gtx, segW, h, iconW(icEmoji, 22, col))
				case 1:
					centerIn2(gtx, segW, h, u.label(12.5, "GIF", col, labelOpts{weight: font.Bold, maxLines: 1}).Layout)
				case 2:
					centerIn2(gtx, segW, h, iconW(icSticker, 21, col))
				}
			}
			glyph(p.TextSecondary)
			withOpacity(gtx, a, func() { glyph(p.Text) })
			return D{Size: image.Pt(segW, h)}
		})
		t.Pop()
		if i > 0 {
			fillRect(gtx, image.Rect(i*segW, 0, i*segW+max(1, gtx.Dp(1)), h), p.PickerTabBorder)
		}
	}
}

// pickerHeader draws a row of icon tabs with a green underline that slides
// to the active one. Tab i is the button prefix+(i+1).
func (u *UI) pickerHeader(gtx C, prefix string, ics []*icon.Icon, active int, sel *switcher[int], line *follower) D {
	p := u.pal
	w := gtx.Constraints.Max.X
	gtx.Constraints.Min.X = w
	tabW := w / len(emojiCategories) // the same pitch on every tab
	h := gtx.Dp(57)
	for i, ic := range ics {
		t := op.Offset(image.Pt(i*tabW, 0)).Push(gtx.Ops)
		cg := gtx
		cg.Constraints = layout.Exact(image.Pt(tabW, h))
		cl := u.btn(prefix + itoa(i+1))
		clickable(cg, cl, func(gtx C) D {
			isz := gtx.Dp(26)
			icT := op.Offset(image.Pt((tabW-isz)/2, gtx.Dp(29)-isz/2)).Push(gtx.Ops)
			// Icon colors are cached per color: cross-fade two icons
			// instead of animating the color.
			drawIcon(gtx, ic, 26, p.Icon)
			withOpacity(gtx, max(u.hover(gtx, cl), sel.of(i)), func() { drawIcon(gtx, ic, 26, p.Text) })
			icT.Pop()
			return D{Size: gtx.Constraints.Max}
		})
		t.Pop()
	}
	x := int(line.step(gtx, float32(active*tabW), durSlide))
	bw := gtx.Dp(27)
	fillRRect(gtx, image.Rect(x+(tabW-bw)/2, gtx.Dp(49), x+(tabW+bw)/2, gtx.Dp(52)), gtx.Dp(2), p.Green)
	return D{Size: image.Pt(w, h)}
}

// stickerSets are the sticker tab's own tabs, in model.StickerSet order.
var stickerSets = []struct {
	ic    *icon.Icon
	empty string
}{
	{icClock, "Stickers you send show up here."},
	{icStar, "Stickers you favourite in WhatsApp show up here."},
	{icBubble, "Stickers you receive show up here."},
}

// stickerList returns a sticker set, loading it on first use.
func (u *UI) stickerList(set model.StickerSet) []*model.Message {
	e := &u.picker
	if !e.stickersOK[set] {
		e.stickers[set], e.stickersOK[set] = u.backend.Stickers(set), true
	}
	return e.stickers[set]
}

// defaultStickerSet is the first non-empty set, like WhatsApp opening on
// recents.
func (u *UI) defaultStickerSet() model.StickerSet {
	for set := range model.StickerSet(len(stickerSets)) {
		if len(u.stickerList(set)) > 0 {
			return set
		}
	}
	return model.StickersRecent
}

func (u *UI) layoutStickerTab(gtx C) D {
	e := &u.picker
	p := u.pal
	for i := range stickerSets {
		if u.btn("stset:"+itoa(i+1)).Clicked(gtx) && e.stickerSet != model.StickerSet(i) {
			e.stickerSet = model.StickerSet(i)
			e.list.Position = layout.Position{}
		}
	}
	stickers := u.stickerList(e.stickerSet)
	for _, s := range stickers {
		if u.btn("sticker:"+s.ChatID+"/"+s.ID).Clicked(gtx) && u.selected != nil {
			u.backend.SendSticker(u.selected.ID, s)
			u.closePicker()
			return D{}
		}
	}
	e.stickerSel.step(gtx, int(e.stickerSet), durSwitch)
	w := gtx.Constraints.Max.X
	cell := gtx.Dp(110)
	cols := max(2, (w-gtx.Dp(24))/cell)
	n := (len(stickers) + cols - 1) / cols
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx C) D {
			ics := make([]*icon.Icon, len(stickerSets))
			for i, s := range stickerSets {
				ics[i] = s.ic
			}
			return u.pickerHeader(gtx, "stset:", ics, int(e.stickerSet), &e.stickerSel, &e.stickerLine)
		}),
		layout.Flexed(1, func(gtx C) D {
			if len(stickers) == 0 {
				gtx.Constraints.Max.Y -= gtx.Dp(56) // above the tabs
				gtx.Constraints.Min.Y = gtx.Constraints.Max.Y
				return u.pickerMessage(gtx, stickerSets[e.stickerSet].empty)
			}
			return layout.Inset{Top: 8}.Layout(gtx, func(gtx C) D {
				return u.scrollList(gtx, &e.list, n+1, func(gtx C, row int) D {
					if row == n {
						return D{Size: image.Pt(0, gtx.Dp(56))}
					}
					var children []layout.FlexChild
					for i := row * cols; i < min((row+1)*cols, len(stickers)); i++ {
						s := stickers[i]
						children = append(children, layout.Rigid(func(gtx C) D {
							cl := u.btn("sticker:" + s.ChatID + "/" + s.ID)
							return clickable(gtx, cl, func(gtx C) D {
								if h := u.hover(gtx, cl); h > 0 {
									fillRRect(gtx, image.Rect(0, 0, cell, cell), gtx.Dp(10), faded(p.PopupHover, h))
								}
								img := u.messageImage(s, cell*2)
								if img != nil && img.state == imgReady {
									pad := gtx.Dp(8)
									in := cell - 2*pad
									sc := min(float32(in)/float32(img.size.X), float32(in)/float32(img.size.Y))
									iw, ih := int(float32(img.size.X)*sc), int(float32(img.size.Y)*sc)
									min := image.Pt((cell-iw)/2, (cell-ih)/2)
									paintCover(gtx, img.op, img.size, image.Rectangle{Min: min, Max: min.Add(image.Pt(iw, ih))})
								}
								return D{Size: image.Pt(cell, cell)}
							})
						}))
					}
					return layout.Inset{Left: 12}.Layout(gtx, func(gtx C) D { return layout.Flex{}.Layout(gtx, children...) })
				})
			})
		}),
	)
}

// centerIn2 draws w centered in a w×h px box.
func centerIn2(gtx C, w, h int, wd layout.Widget) D {
	gtx.Constraints = layout.Exact(image.Pt(w, h))
	return layout.Center.Layout(gtx, wd)
}
