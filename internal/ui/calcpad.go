package ui

import (
	"image"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"gioui.org/font"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/text"
	"gioui.org/unit"

	"github.com/chomosuke9/wazzapclients/internal/command"
)

// The calculator /calc shows in place of the slash picker. Its keys type
// into the composer, after "/calc ", so the keyboard, the live answer
// (Command.Preview) and Enter keep working as for any command.

// calcKeys are the keypad's keys, row by row. "()" types whichever
// bracket fits, "⌫" and "C" delete, and "⋯" shows calcMore above them.
var calcKeys = [][]string{
	{"C", "%", "⌫", "÷", "xʸ"},
	{"7", "8", "9", "×", "√"},
	{"4", "5", "6", "−", "mod"},
	{"1", "2", "3", "+", "ans"},
	{"000", "0", ".", "()", "⋯"},
}

// calcMore are the functions "⋯" shows, and the comma between their
// numbers (min(1, 2)).
var calcMore = [][]string{
	{"round", "ceil", "floor", "min", "max"},
	{"!", "log", "ln", "π", ","},
}

// calcRows are the keypad's rows as it shows now.
func (u *UI) calcRows() [][]string {
	if u.slash.calcMore {
		return append(append([][]string(nil), calcMore...), calcKeys...)
	}
	return calcKeys
}

// calcWords are what keys type as one piece, which ⌫ deletes at once.
var calcWords = []string{"round(", "ceil(", "floor(", "min(", "max(", "log(", "ln(", "sqrt(", "abs(", "mod", "ans"}

// calcFuncs are the keys that type a function, which their own "(" opens.
var calcFuncs = map[string]bool{"round": true, "ceil": true, "floor": true, "min": true, "max": true, "log": true, "ln": true}

// calcBinary maps the operator keys to what they type.
var calcBinary = map[string]string{"+": "+", "−": "−", "×": "×", "÷": "÷", "xʸ": "^", "mod": "mod"}

// calcChips is the most numbers offered from the message replied to.
const calcChips = 8

// isCalcPick reports whether the picker is /calc's calculator.
func isCalcPick(sp *slashPick) bool {
	return sp != nil && !sp.in.Naming && sp.in.Cmd != nil && sp.in.Cmd.Name == "calc"
}

// calcTail is what the sum before the caret ends with.
type calcTail int

const (
	tailEmpty calcTail = iota // nothing yet
	tailOpen                  // "(", a function's "(", "," or "√": a number goes next
	tailOp                    // an operator
	tailValue                 // a number, ")", "%", "!", π or ans
)

func calcTailOf(sum string) calcTail {
	t := strings.TrimRight(sum, " ")
	if t == "" {
		return tailEmpty
	}
	if strings.HasSuffix(t, "mod") {
		if rs := []rune(t); len(rs) == 3 || !unicode.IsLetter(rs[len(rs)-4]) {
			return tailOp
		}
	}
	r, _ := utf8.DecodeLastRuneInString(t)
	switch {
	case strings.ContainsRune("+−-×*÷/:^x", r):
		return tailOp
	case r == '(' || r == ',' || r == '√':
		return tailOpen
	}
	return tailValue
}

// calcSum returns the composer's text as runes, where the sum starts in
// it, and the caret, kept inside the sum.
func (u *UI) calcSum(sp *slashPick) (rs []rune, start, caret int) {
	ed := &u.conv.composer
	rs = []rune(ed.Text())
	start = min(len([]rune("/"+sp.in.Name))+1, len(rs))
	caret, _ = ed.Selection()
	if caret < start {
		caret = len(rs)
	}
	return rs, start, caret
}

// calcTypeAt replaces the composer's runes from..to with s, leaving the
// caret after it.
func (u *UI) calcTypeAt(from, to int, s string) {
	ed := &u.conv.composer
	ed.SetCaret(from, to)
	ed.Insert(s)
	u.requestFocus(ed)
}

// calcBefore returns the last rune before the caret that isn't a space,
// or 0.
func calcBefore(rs []rune, start, caret int) rune {
	for i := caret - 1; i >= start; i-- {
		if rs[i] != ' ' {
			return rs[i]
		}
	}
	return 0
}

// calcBack returns where the piece before i starts (a digit, an
// operator, a function with its "("), with the spaces before it.
func calcBack(rs []rune, start, i int) int {
	for i > start && rs[i-1] == ' ' {
		i--
	}
	piece := 1
	for _, w := range calcWords {
		if n := len([]rune(w)); i-start >= n && string(rs[i-n:i]) == w {
			piece = n
			break
		}
	}
	i = max(i-piece, start)
	for i > start && rs[i-1] == ' ' {
		i--
	}
	return i
}

// pressCalc types key k of the keypad.
func (u *UI) pressCalc(sp *slashPick, k string) {
	rs, start, caret := u.calcSum(sp)
	tail := calcTailOf(string(rs[start:caret]))
	prev := calcBefore(rs, start, caret)
	// A value right after another one is multiplied by it: 2(…), 3π.
	times := func(s string) string {
		if tail == tailValue {
			return " × " + s
		}
		return s
	}
	open := strings.Count(string(rs[start:caret]), "(") - strings.Count(string(rs[start:caret]), ")")
	switch op, binary := calcBinary[k]; {
	case k == "C":
		u.calcTypeAt(start, len(rs), "")
	case k == "⌫":
		if caret > start {
			u.calcTypeAt(calcBack(rs, start, caret), caret, "")
		}
	case k == "⋯":
		u.slash.calcMore = !u.slash.calcMore
		u.requestFocus(&u.conv.composer)
	case k == "()":
		switch {
		case open > 0 && tail == tailValue:
			u.calcTypeAt(caret, caret, ")")
		default:
			u.calcTypeAt(caret, caret, times("("))
		}
	case binary:
		switch tail {
		case tailEmpty, tailOpen:
			if k == "−" {
				u.calcTypeAt(caret, caret, "-") // a negative number
			}
		case tailOp:
			// Another operator replaces the last one.
			u.calcTypeAt(calcBack(rs, start, caret), caret, " "+op+" ")
		default:
			u.calcTypeAt(caret, caret, " "+op+" ")
		}
	case k == "%" || k == "!":
		if tail == tailValue && prev != rune(k[0]) {
			u.calcTypeAt(caret, caret, k)
		}
	case k == ",":
		if open > 0 && tail == tailValue {
			u.calcTypeAt(caret, caret, ", ")
		}
	case calcFuncs[k]:
		u.calcTypeAt(caret, caret, times(k+"("))
	case k == "√" || k == "π" || k == "ans":
		u.calcTypeAt(caret, caret, times(k))
	default: // digits, "000" and "."
		if tail == tailValue && !(prev >= '0' && prev <= '9' || prev == '.') {
			k = " × " + k
		}
		u.calcTypeAt(caret, caret, k)
	}
}

// calcInsertNumber types a number from the message replied to, joined to
// a number before it with +.
func (u *UI) calcInsertNumber(sp *slashPick, s string) {
	rs, start, caret := u.calcSum(sp)
	if calcTailOf(string(rs[start:caret])) == tailValue {
		s = " + " + s
	}
	u.calcTypeAt(caret, caret, s)
}

// calcAnswer is the answer as a plain number, for the clipboard.
func calcAnswer(sp *slashPick) string {
	return strings.ReplaceAll(strings.TrimPrefix(sp.preview, "= "), " ", "")
}

// replyNumbers are the amounts in the message the composer replies to.
func (u *UI) replyNumbers() []float64 {
	if r := u.conv.reply; r != nil {
		return command.Numbers(r.Text, calcChips)
	}
	return nil
}

// plainNumber writes v for the composer: digits only, as Calc reads them.
func plainNumber(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }

// layoutCalcPad draws /calc's calculator: the keypad, and beside it (or
// above it, when narrow) the sum, its answer, the numbers of the message
// replied to, and Copy and Send.
func (u *UI) layoutCalcPad(gtx C, sp *slashPick) D {
	p := u.pal
	s := &u.slash
	rows := u.calcRows()
	for _, row := range rows {
		for _, k := range row {
			if u.btn("calc:" + k).Clicked(gtx) {
				u.pressCalc(sp, k)
			}
		}
	}
	nums := u.replyNumbers()
	for i, v := range nums {
		if u.btn("calc:n:" + itoa(i)).Clicked(gtx) {
			u.calcInsertNumber(sp, plainNumber(v))
		}
	}
	if u.btn("calc:sum").Clicked(gtx) && len(nums) > 1 {
		parts := make([]string, len(nums))
		for i, v := range nums {
			parts[i] = plainNumber(v)
		}
		u.calcInsertNumber(sp, "("+strings.Join(parts, " + ")+")")
	}
	if u.btn("calc:copy").Clicked(gtx) && sp.previewOK {
		u.pendingCopy = calcAnswer(sp)
		u.toast("Copied")
	}
	if u.btn("calc:send").Clicked(gtx) && sp.previewOK {
		u.submitSlash(sp)
	}

	gtx.Constraints.Min.X = gtx.Constraints.Max.X
	w := gtx.Constraints.Max.X
	pad, gap := gtx.Dp(12), gtx.Dp(6)
	keyW, keyH := gtx.Dp(64), gtx.Dp(44)
	cols := len(calcKeys[0])
	padW := cols*keyW + (cols-1)*gap
	padH := len(rows)*keyH + (len(rows)-1)*gap
	wide := w-2*pad-padW >= gtx.Dp(260)
	sideW := w - 2*pad
	if wide {
		sideW -= padW + gtx.Dp(16)
	}
	sgtx := gtx
	sgtx.Constraints = layout.Constraints{Max: image.Pt(sideW, gtx.Constraints.Max.Y)}

	// The sum as typed, and what it comes to.
	sum := strings.TrimSpace(sp.in.Text("sum"))
	display := record(sgtx, func(gtx C) D {
		gtx.Constraints.Min.X = gtx.Constraints.Max.X
		var expr, answer layout.Widget
		if sum == "" {
			expr = u.label(15, "Type a sum, or use the keys", p.PopupSub, labelOpts{maxLines: 1}).Layout
		} else {
			expr = u.label(17, sum, p.Text, labelOpts{maxLines: 2}).Layout
		}
		switch {
		case s.problem != "" && s.problemAt == u.conv.composer.Text():
			answer = u.label(14, s.problem, p.Danger, labelOpts{weight: font.Medium, maxLines: 2}).Layout
		case sp.previewOK:
			answer = u.label(30, sp.preview, p.Green, labelOpts{weight: font.SemiBold, maxLines: 1}).Layout
		case sp.preview != "":
			answer = u.label(14, sp.preview, p.Danger, labelOpts{weight: font.Medium, maxLines: 2}).Layout
		default:
			answer = u.label(30, "= …", p.PopupSub, labelOpts{weight: font.SemiBold, maxLines: 1}).Layout
		}
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(u.label(12.5, "CALCULATOR", p.PopupSub, labelOpts{weight: font.SemiBold, maxLines: 1}).Layout),
			layout.Rigid(layout.Spacer{Height: 8}.Layout),
			layout.Rigid(expr),
			layout.Rigid(layout.Spacer{Height: 2}.Layout),
			layout.Rigid(answer),
		)
	})
	// The numbers of the message replied to, and their total.
	var chips part
	if len(nums) > 0 {
		chips = record(sgtx, func(gtx C) D { return u.layoutCalcChips(gtx, nums) })
	}
	actions := record(sgtx, func(gtx C) D { return u.layoutCalcActions(gtx, sp.previewOK) })

	side := []part{display}
	if chips.size.Y > 0 {
		side = append(side, chips)
	}
	side = append(side, actions)
	sideH := 0
	for _, pt := range side {
		sideH += pt.size.Y + gap*2
	}
	var h int
	if wide {
		h = 2*pad + max(padH, sideH)
	} else {
		h = 2*pad + sideH + padH
	}
	r := gtx.Dp(16)
	rect := image.Rect(0, 0, w, h)
	fillRRect(gtx, rect.Add(image.Pt(0, gtx.Dp(2))).Inset(-gtx.Dp(1)), r, p.Shadow)
	borderRRect(gtx, rect, r, p.Popup, p.PopupBorder)

	x, y := pad, pad
	if wide {
		x = pad + padW + gtx.Dp(16)
	}
	for i, pt := range side {
		if wide && i == len(side)-1 {
			y = max(y, h-pad-pt.size.Y) // Send at the bottom, by the keypad's last row
		}
		pt.at(gtx, x, y)
		y += pt.size.Y + gap*2
	}
	px, py := pad, pad
	if !wide {
		py = pad + sideH
	}
	for ri, row := range rows {
		for ci, k := range row {
			u.layoutCalcKey(gtx, k, image.Rect(0, 0, keyW, keyH).Add(image.Pt(px+ci*(keyW+gap), py+ri*(keyH+gap))))
		}
	}
	return D{Size: image.Pt(w, h)}
}

// layoutCalcKey draws key k of the keypad in r.
func (u *UI) layoutCalcKey(gtx C, k string, r image.Rectangle) {
	p := u.pal
	cl := u.btn("calc:" + k)
	defer op.Offset(r.Min).Push(gtx.Ops).Pop()
	clickable(gtx, cl, func(gtx C) D {
		sz := r.Size()
		bg, border := p.Chip, p.ChipBorder
		if k == "⋯" && u.slash.calcMore {
			bg, border = p.ChipActive, p.ChipActiveBorder
		}
		if hv := u.hover(gtx, cl); hv > 0 {
			bg = mix(bg, p.PopupHover, hv)
		}
		borderRRect(gtx, image.Rectangle{Max: sz}, gtx.Dp(12), bg, border)
		col, size := p.Green, unit.Sp(19)
		switch {
		case k == "C":
			col = p.Danger
		case k >= "0" && k <= "9" || k == "000" || k == ".":
			col = p.Text
		}
		if len([]rune(k)) > 2 && k != "000" {
			size = 15 // words: round, mod, ans
		}
		var w layout.Widget
		if k == "⌫" {
			w = iconW(icBackspace, 22, col)
		} else {
			w = u.label(size, k, col, labelOpts{weight: font.Medium, maxLines: 1, align: text.Middle}).Layout
		}
		l := record(gtx, w)
		l.at(gtx, (sz.X-l.size.X)/2, (sz.Y-l.size.Y)/2)
		return D{Size: sz}
	})
}

// layoutCalcChips lays out the numbers of the message replied to as chips
// that type them, wrapping onto more lines, then Σ for their total.
func (u *UI) layoutCalcChips(gtx C, nums []float64) D {
	p := u.pal
	type chip struct {
		key, label string
		total      bool
	}
	var cs []chip
	for i, v := range nums {
		cs = append(cs, chip{key: "calc:n:" + itoa(i), label: command.FormatNumber(v)})
	}
	if len(nums) > 1 {
		total := 0.0
		for _, v := range nums {
			total += v
		}
		cs = append(cs, chip{key: "calc:sum", label: "Σ " + command.FormatNumber(total), total: true})
	}
	title := record(gtx, u.label(12.5, "FROM THE MESSAGE", p.PopupSub, labelOpts{weight: font.SemiBold, maxLines: 1}).Layout)
	title.at(gtx, 0, 0)
	maxW := gtx.Constraints.Max.X
	gap, chipH := gtx.Dp(6), gtx.Dp(32)
	x, y := 0, title.size.Y+gtx.Dp(6)
	for _, c := range cs {
		col, bg, border := p.ChipText, p.Chip, p.ChipBorder
		if c.total {
			col, bg, border = p.ChipActiveText, p.ChipActive, p.ChipActiveBorder
		}
		l := record(gtx, u.label(14.5, c.label, col, labelOpts{weight: font.Medium, maxLines: 1}).Layout)
		cw := l.size.X + gtx.Dp(24)
		if x > 0 && x+cw > maxW {
			x, y = 0, y+chipH+gap
		}
		cl := u.btn(c.key)
		t := op.Offset(image.Pt(x, y)).Push(gtx.Ops)
		clickable(gtx, cl, func(gtx C) D {
			sz := image.Pt(cw, chipH)
			b := bg
			if hv := u.hover(gtx, cl); hv > 0 {
				b = mix(b, p.PopupHover, hv)
			}
			borderRRect(gtx, image.Rectangle{Max: sz}, chipH/2, b, border)
			l.at(gtx, gtx.Dp(12), (chipH-l.size.Y)/2)
			return D{Size: sz}
		})
		t.Pop()
		x += cw + gap
	}
	return D{Size: image.Pt(maxW, y+chipH)}
}

// layoutCalcActions draws Copy and Send, which work once there's an
// answer.
func (u *UI) layoutCalcActions(gtx C, ok bool) D {
	p := u.pal
	h := gtx.Dp(40)
	button := func(key string, ic layout.Widget, label string, primary bool) layout.Widget {
		return func(gtx C) D {
			cl := u.btn(key)
			col, fill, line := p.Text, p.Chip, p.ChipBorder
			if primary {
				col, fill, line = p.OnGreen, p.Green, p.Green
			}
			l := record(gtx, func(gtx C) D {
				return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(ic),
					layout.Rigid(layout.Spacer{Width: 8}.Layout),
					layout.Rigid(u.label(15, label, col, labelOpts{weight: font.Medium, maxLines: 1}).Layout),
				)
			})
			sz := image.Pt(l.size.X+gtx.Dp(36), h)
			return clickable(gtx, cl, func(gtx C) D {
				f := fill
				if hv := u.hover(gtx, cl); hv > 0 && ok {
					f = mix(f, p.PopupHover, hv*0.5)
				}
				borderRRect(gtx, image.Rectangle{Max: sz}, h/2, f, line)
				l.at(gtx, gtx.Dp(18), (h-l.size.Y)/2)
				return D{Size: sz}
			})
		}
	}
	row := func(gtx C) D {
		return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
			layout.Rigid(button("calc:copy", iconW(icCopy, 18, p.Text), "Copy answer", false)),
			layout.Rigid(layout.Spacer{Width: 8}.Layout),
			layout.Rigid(button("calc:send", iconW(icSend, 18, p.OnGreen), "Send", true)),
		)
	}
	if !ok {
		var d D
		withOpacity(gtx, 0.45, func() { d = row(gtx) })
		return d
	}
	return row(gtx)
}
