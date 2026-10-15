package ui

import (
	"image"
	"slices"
	"strings"

	"gioui.org/font"
	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/widget"
)

// Emoji shortcodes, like Discord's: typing ":" and two letters of an
// emoji's name (":mo") offers the emojis whose shortcodes match in a
// picker over the composer, and typing the whole name with its closing
// colon (":moai:") turns it into the emoji (🗿). The shortcodes are
// gemoji's, from emojiTable.

// shortcodes is the shortcode picker's state.
type shortcodes struct {
	list      widget.List
	sel       int    // the highlighted row, which Enter picks
	selFor    string // the query sel belongs to; a new one resets it
	dismissed string // the ":query" Esc closed the picker on
	anim      tween
	ghost     *shortcodeState // what the picker showed, while it fades out

	// hits' answer for one query.
	hitsFor string
	hits    []shortcodeHit
}

// shortcodeState describes a ":query" being typed before the caret.
type shortcodeState struct {
	start, end int // rune offsets of ":query"
	query      string
	hits       []shortcodeHit
}

// shortcodeHit is an emoji the picker offers, by one of its shortcodes.
type shortcodeHit struct {
	char, code string
}

// shortcodeIndex is every shortcode with its emoji, in emojiTable's order:
// gemoji's, and the emoji's name in their style (":moai:" as well as
// gemoji's ":moyai:", as Discord takes).
var shortcodeIndex = func() []shortcodeHit {
	var out []shortcodeHit
	seen := map[string]bool{}
	for _, e := range emojis {
		for _, c := range strings.Fields(e.codes) {
			seen[c] = true
			out = append(out, shortcodeHit{char: e.char, code: c})
		}
	}
	for _, e := range emojis {
		if c := nameCode(e.name); c != "" && !seen[c] {
			seen[c] = true
			out = append(out, shortcodeHit{char: e.char, code: c})
		}
	}
	return out
}()

// nameCode turns an emoji's name into a shortcode: "flag: Côte d’Ivoire"
// becomes "flag_c_te_d_ivoire", lowercase with "_" between words.
func nameCode(name string) string {
	var b strings.Builder
	gap := false
	for _, r := range strings.ToLower(name) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			if gap && b.Len() > 0 {
				b.WriteByte('_')
			}
			b.WriteRune(r)
			gap = false
		} else {
			gap = true
		}
	}
	return b.String()
}

// shortcodeEmoji maps a shortcode to its emoji.
var shortcodeEmoji = func() map[string]string {
	m := make(map[string]string, len(shortcodeIndex))
	for _, h := range shortcodeIndex {
		m[h.code] = h.char
	}
	return m
}()

// shortcodeMaxHits is how many emojis the picker offers at most.
const shortcodeMaxHits = 50

// shortcodeRune reports whether r can be part of a shortcode.
func shortcodeRune(r rune) bool {
	return r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '+' || r == '-'
}

// shortcodeStart reports whether the ":" at i in txt can open a shortcode:
// at the start, or after anything but a letter, a digit, a colon or a
// slash ("10:30" and "http://" don't).
func shortcodeStart(txt []rune, i int) bool {
	if i == 0 {
		return true
	}
	r := txt[i-1]
	return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == ':' || r == '/')
}

// shortcodeHits lists the emojis whose shortcodes match q: the shortcode
// itself first, then those starting with it (shortest first), then those
// with a word starting with it, then the rest that hold it.
func shortcodeHits(q string) []shortcodeHit {
	var tiers [4][]shortcodeHit
	for _, h := range shortcodeIndex {
		t := -1
		switch {
		case h.code == q:
			t = 0
		case strings.HasPrefix(h.code, q):
			t = 1
		case strings.Contains(h.code, "_"+q):
			t = 2
		case strings.Contains(h.code, q):
			t = 3
		}
		if t >= 0 {
			tiers[t] = append(tiers[t], h)
		}
	}
	// Shorter names first among those starting with q, as Discord does.
	slices.SortStableFunc(tiers[1], func(a, b shortcodeHit) int { return len(a.code) - len(b.code) })
	var out []shortcodeHit
	seen := map[string]bool{}
	for _, tier := range tiers {
		for _, h := range tier {
			if !seen[h.char] && len(out) < shortcodeMaxHits {
				seen[h.char] = true
				out = append(out, h)
			}
		}
	}
	return out
}

// shortcodeToken returns the ":query" before the composer's caret, with
// its rune offsets, or "" when there's none.
func (u *UI) shortcodeToken() (tok string, start, end int) {
	ed := &u.conv.composer
	caret, other := ed.Selection()
	if caret != other {
		return "", 0, 0
	}
	txt := []rune(ed.Text())
	if caret > len(txt) {
		return "", 0, 0
	}
	i := caret
	for i > 0 && caret-i <= 40 && shortcodeRune(txt[i-1]) {
		i--
	}
	if i == 0 || txt[i-1] != ':' || caret-i < 2 || !shortcodeStart(txt, i-1) {
		return "", 0, 0
	}
	return string(txt[i-1 : caret]), i - 1, caret
}

// shortcodeQuery returns the shortcode being typed in the composer, or nil.
// The mention and slash command pickers come first.
func (u *UI) shortcodeQuery() *shortcodeState {
	if u.selected == nil {
		return nil
	}
	tok, start, end := u.shortcodeToken()
	if tok == "" || tok == u.codes.dismissed || u.mentionQuery() != nil || u.slashShown(u.slashQuery()) {
		return nil
	}
	q := strings.ToLower(tok[1:])
	s := &u.codes
	if s.hitsFor != q {
		s.hitsFor, s.hits = q, shortcodeHits(q)
	}
	if len(s.hits) == 0 {
		return nil
	}
	return &shortcodeState{start: start, end: end, query: q, hits: s.hits}
}

// shortcodeChar is the emoji h stands for, in the picker's skin tone.
func (u *UI) shortcodeChar(h shortcodeHit) string {
	u.loadPickerPrefs()
	return withTone(h.char, u.picker.tone)
}

// pickShortcode replaces the typed ":query" with row i's emoji.
func (u *UI) pickShortcode(i int) {
	st := u.shortcodeQuery()
	if st == nil || i >= len(st.hits) {
		return
	}
	ch := u.shortcodeChar(st.hits[i])
	ed := &u.conv.composer
	ed.SetCaret(st.start, st.end)
	ed.Insert(ch)
	u.rememberEmoji(ch)
	u.requestFocus(ed)
}

// completeShortcode turns a whole ":name:" into its emoji right after its
// closing colon was typed. before is the composer's text before the
// change. The colon is found by comparing the two: an input method's edit
// can come before the caret moves past it.
func (u *UI) completeShortcode(before string) {
	ed := &u.conv.composer
	txt, old := []rune(ed.Text()), []rune(before)
	if len(txt) != len(old)+1 {
		return // only one rune typed
	}
	at := 0 // where the new rune is
	for at < len(old) && txt[at] == old[at] {
		at++
	}
	if txt[at] != ':' || string(txt[at+1:]) != string(old[at:]) {
		return
	}
	i := at
	for i > 0 && at-i <= 40 && shortcodeRune(txt[i-1]) {
		i--
	}
	if i == 0 || txt[i-1] != ':' || at-i < 1 || !shortcodeStart(txt, i-1) {
		return
	}
	ch, ok := shortcodeEmoji[strings.ToLower(string(txt[i:at]))]
	if !ok {
		return
	}
	ch = u.shortcodeChar(shortcodeHit{char: ch})
	ed.SetCaret(i-1, at+1)
	ed.Insert(ch)
	u.rememberEmoji(ch)
}

// shortcodeRows is how many rows the shortcode picker shows at once.
const shortcodeRows = 6

// updateShortcodeSel puts the highlight on the first row when the query
// changes.
func (u *UI) updateShortcodeSel(st *shortcodeState) {
	s := &u.codes
	if st == nil {
		s.selFor = ""
		return
	}
	q := itoa(st.start) + "\x00" + st.query
	if q != s.selFor {
		s.selFor, s.sel = q, 0
		s.list.Position = layout.Position{}
	}
}

// shortcodeKeys moves through the shortcode picker with the arrow keys,
// and picks with Tab or Enter. It runs before the composer reads its keys.
func (u *UI) shortcodeKeys(gtx C) {
	if tok, _, _ := u.shortcodeToken(); tok != u.codes.dismissed {
		u.codes.dismissed = "" // typed on, or moved away
	}
	st := u.shortcodeQuery()
	u.updateShortcodeSel(st)
	if st == nil {
		return
	}
	s := &u.codes
	ed := &u.conv.composer
	n := len(st.hits)
	for {
		ev, ok := gtx.Event(
			key.Filter{Focus: ed, Name: key.NameUpArrow},
			key.Filter{Focus: ed, Name: key.NameDownArrow},
			key.Filter{Focus: ed, Name: key.NameTab},
			key.Filter{Focus: ed, Name: key.NameReturn},
			key.Filter{Focus: ed, Name: key.NameEnter},
		)
		if !ok {
			break
		}
		e, ok := ev.(key.Event)
		if !ok || e.State != key.Press {
			continue
		}
		switch e.Name {
		case key.NameUpArrow:
			s.sel = (s.sel - 1 + n) % n
		case key.NameDownArrow:
			s.sel = (s.sel + 1) % n
		default:
			u.pickShortcode(s.sel)
			return
		}
		keepVisible(&s.list.List, s.sel, min(n, shortcodeRows))
	}
}

// dismissShortcode is Esc on the picker: it stays closed for this query.
func (u *UI) dismissShortcode() {
	u.codes.dismissed, _, _ = u.shortcodeToken()
}

// layoutShortcodePicker draws the matching emojis, each with its
// shortcode, above the composer.
func (u *UI) layoutShortcodePicker(gtx C, st *shortcodeState) D {
	p := u.pal
	s := &u.codes
	for i, h := range st.hits {
		if u.btn("shortcode:" + h.code).Clicked(gtx) {
			u.pickShortcode(i) // it keeps drawing while it fades out
		}
	}
	rowH := gtx.Dp(44)
	pad := gtx.Dp(8)
	n := min(len(st.hits), shortcodeRows)
	gtx.Constraints.Min.X = gtx.Constraints.Max.X
	w := gtx.Constraints.Max.X
	header := record(gtx, func(gtx C) D {
		return layout.Inset{Left: 12, Right: 12, Top: 4, Bottom: 6}.Layout(gtx,
			u.label(12.5, "EMOJI MATCHING :"+strings.ToUpper(st.query), p.PopupSub, labelOpts{weight: font.SemiBold, maxLines: 1}).Layout)
	})
	h := n*rowH + 2*pad + header.size.Y
	r := gtx.Dp(16)
	rect := image.Rect(0, 0, w, h)
	fillRRect(gtx, rect.Add(image.Pt(0, gtx.Dp(2))).Inset(-gtx.Dp(1)), r, p.Shadow)
	borderRRect(gtx, rect, r, p.Popup, p.PopupBorder)
	header.at(gtx, pad, pad)
	defer clip.UniformRRect(rect, r).Push(gtx.Ops).Pop()
	gtx.Constraints = layout.Exact(image.Pt(w-2*pad, n*rowH))
	t := op.Offset(image.Pt(pad, pad+header.size.Y)).Push(gtx.Ops)
	s.list.Axis = layout.Vertical
	u.scrollList(gtx, &s.list, len(st.hits), func(gtx C, i int) D {
		hit := st.hits[i]
		cl := u.btn("shortcode:" + hit.code)
		return clickable(gtx, cl, func(gtx C) D {
			sz := image.Pt(gtx.Constraints.Max.X, rowH)
			hv := u.hover(gtx, cl)
			if i == s.sel {
				hv = 1 // Enter picks it
			}
			if hv > 0 {
				fillRRect(gtx, image.Rectangle{Max: sz}, gtx.Dp(10), faded(p.PopupHover, hv))
			}
			ch := u.shortcodeChar(hit)
			cell := gtx.Dp(32)
			e := op.Offset(image.Pt(gtx.Dp(10), (rowH-cell)/2)).Push(gtx.Ops)
			cg := gtx
			cg.Constraints = layout.Exact(image.Pt(cell, cell))
			if !u.layoutEmojiImage(cg, ch, gtx.Sp(24), image.Pt(cell, cell), animateIn(emojiPanel)) {
				centerIn(cg, cell, u.label(24, ch, p.Text).Layout)
			}
			e.Pop()
			lg := gtx
			lg.Constraints = layout.Constraints{Max: image.Pt(sz.X-gtx.Dp(64), rowH)}
			l := op.Offset(image.Pt(gtx.Dp(54), 0)).Push(gtx.Ops)
			vcenter(lg, rowH, u.label(15.5, ":"+hit.code+":", p.Text, labelOpts{maxLines: 1}).Layout)
			l.Pop()
			return D{Size: sz}
		})
	})
	t.Pop()
	return D{Size: image.Pt(w, h)}
}
