package ui

import (
	"image"
	"strconv"
	"strings"

	"gioui.org/layout"
	"gioui.org/op"
)

// Skin tones, as in WhatsApp's emoji panel: a hand beside the search field
// sets the tone of every emoji that takes one, and a right click on such
// an emoji offers it in each tone. The tone is an app preference.

// prefEmojiTone is the picker's skin tone: 1 (light) to 5 (dark), or ""
// for the default yellow.
const prefEmojiTone = "emoji_tone"

// toneMods are the skin tone modifiers, light to dark.
var toneMods = [5]rune{0x1F3FB, 0x1F3FC, 0x1F3FD, 0x1F3FE, 0x1F3FF}

// emojiTakesTone holds the emojis of emojiTable that take a skin tone, and
// toneBases the code points a tone follows (the one-code-point ones).
var emojiTakesTone, toneBases = func() (map[string]bool, map[rune]bool) {
	takes, bases := map[string]bool{}, map[rune]bool{}
	for _, ch := range strings.Fields(emojiToneTable) {
		takes[ch] = true
		if rs := []rune(strings.TrimSuffix(ch, "️")); len(rs) == 1 {
			bases[rs[0]] = true
		}
	}
	return takes, bases
}()

// withTone returns ch in skin tone (1 to 5), or ch itself when tone is 0 or
// ch takes none. The tone goes after each person in it (both of "people
// holding hands"), in place of a variation selector after them.
func withTone(ch string, tone int) string {
	if tone < 1 || tone > len(toneMods) || !emojiTakesTone[ch] {
		return ch
	}
	rs := []rune(ch)
	zwj := strings.ContainsRune(ch, '‍')
	var b strings.Builder
	for i := 0; i < len(rs); i++ {
		r := rs[i]
		b.WriteRune(r)
		// The handshake of "people holding hands" isn't a person.
		if !toneBases[r] || (zwj && r == 0x1F91D) {
			continue
		}
		b.WriteRune(toneMods[tone-1])
		if i+1 < len(rs) && rs[i+1] == '️' {
			i++
		}
	}
	return b.String()
}

// loadTone reads the picker's tone from the preferences.
func (u *UI) loadTone() int {
	t, err := strconv.Atoi(u.backend.Pref(prefEmojiTone))
	if err != nil || t < 0 || t > len(toneMods) {
		return 0
	}
	return t
}

// setTone makes tone the picker's skin tone.
func (u *UI) setTone(tone int) {
	u.picker.tone = tone
	v := ""
	if tone > 0 {
		v = itoa(tone)
	}
	u.backend.SetPref(prefEmojiTone, v)
}

// toneMenu is the row of tones the picker shows over its emojis: the
// default's (from the tone button), or one emoji's (from a right click).
type toneMenu struct {
	open bool
	of   string      // the emoji whose tones it offers; "" for the default tone
	at   image.Point // the point it hangs from, in the panel (the default's hangs from the tone button)
	anim tween
}

// openToneMenu offers of's tones at at, in the picker panel's coordinates,
// or with of "", the default tone's under the tone button.
func (u *UI) openToneMenu(of string, at image.Point) {
	u.picker.tones = toneMenu{open: true, of: of, at: at}
}

// toneButton is the hand beside the picker's search field, in the default
// tone; it opens the menu of tones under itself.
func (u *UI) toneButton(gtx C) D {
	e := &u.picker
	p := u.pal
	cl := u.btn("tone:button")
	sz := gtx.Dp(36)
	if cl.Clicked(gtx) {
		if e.tones.open && e.tones.of == "" {
			e.tones.open = false
		} else {
			u.openToneMenu("", image.Point{})
		}
	}
	return clickable(gtx, cl, func(gtx C) D {
		if h := u.hover(gtx, cl); h > 0 {
			fillCircle(gtx, image.Pt(sz/2, sz/2), sz/2, faded(p.PopupHover, h))
		}
		ch := withTone("✋", e.tone)
		if !u.layoutEmojiImage(gtx, ch, gtx.Sp(22), image.Pt(sz, sz)) {
			centerIn(gtx, sz, u.label(22, ch, p.Text).Layout)
		}
		return D{Size: image.Pt(sz, sz)}
	})
}

// layoutToneMenu draws the tone menu over the picker panel (gtx's
// constraints are the panel's size) and handles its picks.
func (u *UI) layoutToneMenu(gtx C) {
	e := &u.picker
	m := &e.tones
	p := u.pal
	of := m.of
	sample := of
	if sample == "" {
		sample = "✋"
	}
	if m.open {
		for i := 0; i <= len(toneMods); i++ {
			if !u.btn("tone:" + itoa(i)).Clicked(gtx) {
				continue
			}
			m.open = false
			if of == "" {
				u.setTone(i)
			} else {
				u.pickEmoji(withTone(of, i))
			}
		}
		if u.btn("tone:scrim").Clicked(gtx) {
			m.open = false
		}
	}
	v := m.anim.step(gtx, m.open, popDur(m.open))
	if v == 0 {
		return
	}
	sz := gtx.Constraints.Max
	if m.open {
		// A click anywhere else in the panel closes it.
		sg := gtx
		sg.Constraints = layout.Exact(sz)
		u.btn("tone:scrim").Layout(sg, func(gtx C) D { return D{Size: sz} })
	} else {
		var done func()
		gtx, done = fadeOut(gtx)
		defer done()
	}
	cell, pad := gtx.Dp(44), gtx.Dp(6)
	n := len(toneMods) + 1
	w, h := n*cell+2*pad, cell+2*pad
	// Under the tone button, its right edges lined up; over an emoji,
	// centered above it.
	at := m.at
	x, y := at.X-w/2, at.Y-h-gtx.Dp(28)
	if of == "" {
		at = e.toneAnchor
		x, y = at.X-w, at.Y+gtx.Dp(4)
	}
	x = max(gtx.Dp(8), min(x, sz.X-w-gtx.Dp(8)))
	y = max(gtx.Dp(8), min(y, sz.Y-h-gtx.Dp(8)))
	rect := image.Rect(x, y, x+w, y+h)
	defer pushPopup(gtx, v, image.Pt(min(max(at.X, rect.Min.X), rect.Max.X), min(max(at.Y, rect.Min.Y), rect.Max.Y))).Pop()
	r := h / 2
	fillRRect(gtx, rect.Add(image.Pt(0, gtx.Dp(2))).Inset(-gtx.Dp(2)), r+gtx.Dp(2), p.Shadow)
	borderRRect(gtx, rect, r, p.Popup, p.PopupBorder)
	for i := 0; i < n; i++ {
		t := op.Offset(image.Pt(x+pad+i*cell, y+pad)).Push(gtx.Ops)
		cg := gtx
		cg.Constraints = layout.Exact(image.Pt(cell, cell))
		cl := u.btn("tone:" + itoa(i))
		clickable(cg, cl, func(gtx C) D {
			a := u.hover(gtx, cl)
			if of == "" && i == e.tone {
				a = 1 // the tone set now
			}
			if a > 0 {
				fillCircle(gtx, image.Pt(cell/2, cell/2), cell/2, faded(p.PopupHover, a))
			}
			ch := withTone(sample, i)
			if !u.layoutEmojiImage(gtx, ch, gtx.Sp(26), image.Pt(cell, cell)) {
				centerIn(gtx, cell, u.label(26, ch, p.Text).Layout)
			}
			return D{Size: image.Pt(cell, cell)}
		})
		t.Pop()
	}
}
