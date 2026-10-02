package command

import (
	"errors"
	"math"
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

// Calc works out an arithmetic expression: numbers with "." for decimals
// and K, M, B or T after them for thousands, millions, billions and
// trillions ("15k"); + - * / ^ and mod, the ways chats write them too (x,
// ×, ÷, :), brackets, a percentage ("15%" is 0.15), a factorial ("5!"),
// √, the functions sqrt, abs, round (to a number of decimals: round(x, 2),
// round(x, -3) to thousands), ceil, floor, min, max, log (base 10) and ln,
// pi, e, and ans, which stands for ans. A sum that isn't finished yet
// ("12 x", "(3+4") gives an error that is errUnfinished.
func Calc(expr string, ans float64) (float64, error) {
	p := &calcParser{s: []rune(expr), ans: ans}
	v, err := p.sum()
	if err == nil {
		p.space()
		if p.i < len(p.s) {
			err = errors.New("\"" + string(p.s[p.i]) + "\" doesn't belong there")
		}
	}
	if err != nil {
		return 0, err
	}
	if math.IsInf(v, 0) || math.IsNaN(v) {
		return 0, errors.New("the answer is too big")
	}
	return v, nil
}

// errUnfinished is the error of a sum that only needs more typed.
var errUnfinished = errors.New("it isn't finished")

// unfinished is an errUnfinished that says what's missing.
type unfinished string

func (e unfinished) Error() string        { return string(e) }
func (e unfinished) Is(target error) bool { return target == errUnfinished }

// FormatNumber writes a Calc result to 12 significant digits, so that
// 0.1+0.2 is 0.3, and from 1000 on to 2 decimals, as amounts of money
// are; with thin spaces between thousands from 10 000 on: a space reads
// the same in every language, where 54,000 and 54.000 don't.
func FormatNumber(v float64) string {
	if math.Abs(v) >= 1000 && math.Abs(v) < 1e15 {
		v = math.Round(v*100) / 100
	}
	if v == 0 {
		return "0" // not -0
	}
	s := strconv.FormatFloat(v, 'g', 12, 64)
	if strings.ContainsRune(s, 'e') {
		if math.Abs(v) < 1e3 || math.Abs(v) >= 1e15 {
			return s // tiny or huge: 1e-07, 1e+20
		}
		// Trillions: all their digits, which 'g' cuts to 1.001e+12.
		r, _ := strconv.ParseFloat(s, 64)
		s = strconv.FormatFloat(r, 'f', -1, 64)
	}
	sign, frac := "", ""
	if s[0] == '-' {
		sign, s = "-", s[1:]
	}
	if i := strings.IndexByte(s, '.'); i >= 0 {
		s, frac = s[:i], s[i:]
	}
	if len(s) < 5 {
		return sign + s + frac
	}
	var b strings.Builder
	for i, r := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteRune(' ') // a narrow no-break space
		}
		b.WriteRune(r)
	}
	return sign + b.String() + frac
}

// suffixes are the letters after a number that multiply it.
var suffixes = map[rune]float64{'k': 1e3, 'm': 1e6, 'b': 1e9, 't': 1e12}

type calcParser struct {
	s   []rune
	i   int
	ans float64
}

func (p *calcParser) space() {
	for p.i < len(p.s) && unicode.IsSpace(p.s[p.i]) {
		p.i++
	}
}

// next skips spaces and returns the rune there, or 0 at the end.
func (p *calcParser) next() rune {
	p.space()
	if p.i < len(p.s) {
		return p.s[p.i]
	}
	return 0
}

// word reports whether the word w (and not a longer one) comes next.
func (p *calcParser) word(w string) bool {
	p.space()
	n := len([]rune(w))
	if p.i+n > len(p.s) || !strings.EqualFold(string(p.s[p.i:p.i+n]), w) {
		return false
	}
	return p.i+n == len(p.s) || !unicode.IsLetter(p.s[p.i+n])
}

func (p *calcParser) sum() (float64, error) {
	v, err := p.product()
	for err == nil {
		switch p.next() {
		case '+':
			p.i++
			var w float64
			w, err = p.product()
			v += w
		case '-', '−':
			p.i++
			var w float64
			w, err = p.product()
			v -= w
		default:
			return v, nil
		}
	}
	return 0, err
}

func (p *calcParser) product() (float64, error) {
	v, err := p.unary()
	for err == nil {
		op := p.next()
		switch {
		case p.word("mod"):
			op = 'm'
			p.i += 3
		case strings.ContainsRune("*xX×·/:÷", op):
			p.i++
		default:
			return v, nil
		}
		var w float64
		if w, err = p.unary(); err != nil {
			break
		}
		switch op {
		case '/', ':', '÷':
			if w == 0 {
				return 0, errors.New("it divides by zero")
			}
			v /= w
		case 'm':
			if w == 0 {
				return 0, errors.New("mod 0 has no answer")
			}
			v = math.Mod(v, w)
		default:
			v *= w
		}
	}
	return 0, err
}

func (p *calcParser) unary() (float64, error) {
	switch p.next() {
	case '-', '−':
		p.i++
		v, err := p.unary()
		return -v, err
	case '+':
		p.i++
		return p.unary()
	}
	return p.power()
}

// power is right-associative and binds tighter than a sign before it:
// -2^2 is -4, as on paper.
func (p *calcParser) power() (float64, error) {
	v, err := p.primary()
	if err != nil {
		return 0, err
	}
	for {
		switch p.next() {
		case '%':
			p.i++
			v /= 100
			continue
		case '!':
			p.i++
			if v < 0 || v != math.Trunc(v) {
				return 0, errors.New("only whole numbers from 0 have a factorial")
			}
			if v > 170 {
				return 0, errors.New("the answer is too big")
			}
			f := 1.0
			for n := 2.0; n <= v; n++ {
				f *= n
			}
			v = f
			continue
		}
		break
	}
	if p.next() == '^' {
		p.i++
		w, err := p.unary()
		if err != nil {
			return 0, err
		}
		v = math.Pow(v, w)
	}
	return v, nil
}

func (p *calcParser) primary() (float64, error) {
	r := p.next()
	switch {
	case r == 0:
		return 0, unfinished("it ends too soon")
	case r == '(':
		p.i++
		v, err := p.sum()
		if err != nil {
			return 0, err
		}
		if err := p.close(); err != nil {
			return 0, err
		}
		return v, nil
	case r >= '0' && r <= '9' || r == '.':
		start := p.i
		for p.i < len(p.s) && (p.s[p.i] >= '0' && p.s[p.i] <= '9' || p.s[p.i] == '.') {
			p.i++
		}
		v, err := strconv.ParseFloat(string(p.s[start:p.i]), 64)
		if err != nil {
			return 0, errors.New("\"" + string(p.s[start:p.i]) + "\" isn't a number")
		}
		// 15k, 2.5M: right after the number, and not starting a word
		// (10 mod 3, 10mod3).
		if p.i < len(p.s) {
			if f, ok := suffixes[unicode.ToLower(p.s[p.i])]; ok && (p.i+1 == len(p.s) || !unicode.IsLetter(p.s[p.i+1])) {
				p.i++
				v *= f
			}
		}
		return v, nil
	case r == '√':
		p.i++
		v, err := p.power()
		if err != nil {
			return 0, err
		}
		if v < 0 {
			return 0, errors.New("a negative number has no square root")
		}
		return math.Sqrt(v), nil
	case unicode.IsLetter(r):
		return p.name()
	}
	return 0, errors.New("\"" + string(r) + "\" doesn't belong there")
}

// close reads the ")" that ends a bracket.
func (p *calcParser) close() error {
	switch p.next() {
	case ')':
		p.i++
		return nil
	case 0:
		return unfinished("a bracket isn't closed")
	}
	return errors.New("a bracket isn't closed")
}

// name reads a constant or a function and its arguments.
func (p *calcParser) name() (float64, error) {
	start := p.i
	for p.i < len(p.s) && unicode.IsLetter(p.s[p.i]) {
		p.i++
	}
	name := strings.ToLower(string(p.s[start:p.i]))
	switch name {
	case "pi", "π":
		return math.Pi, nil
	case "e":
		return math.E, nil
	case "ans":
		return p.ans, nil
	}
	args := map[string][2]int{ // the fewest and most arguments
		"sqrt": {1, 1}, "abs": {1, 1}, "round": {1, 2}, "ceil": {1, 1}, "floor": {1, 1},
		"min": {1, 99}, "max": {1, 99}, "log": {1, 1}, "ln": {1, 1},
	}
	n, ok := args[name]
	if !ok {
		return 0, errors.New("\"" + name + "\" isn't something it knows")
	}
	if r := p.next(); r != '(' {
		err := errors.New(name + " needs brackets: " + name + "(…)")
		if r == 0 {
			err = unfinished(err.Error())
		}
		return 0, err
	}
	p.i++
	var xs []float64
	for {
		v, err := p.sum()
		if err != nil {
			return 0, err
		}
		xs = append(xs, v)
		if p.next() != ',' {
			break
		}
		p.i++
	}
	if err := p.close(); err != nil {
		return 0, err
	}
	if len(xs) < n[0] || len(xs) > n[1] {
		if n[0] == n[1] {
			return 0, errors.New(name + " takes one number")
		}
		return 0, errors.New(name + " takes " + strconv.Itoa(n[0]) + " or " + strconv.Itoa(n[1]) + " numbers")
	}
	x := xs[0]
	switch name {
	case "sqrt":
		if x < 0 {
			return 0, errors.New("a negative number has no square root")
		}
		return math.Sqrt(x), nil
	case "abs":
		return math.Abs(x), nil
	case "round":
		digits := 0.0
		if len(xs) == 2 {
			digits = math.Round(xs[1])
		}
		f := math.Pow(10, digits)
		return math.Round(x*f) / f, nil
	case "ceil":
		return math.Ceil(x), nil
	case "floor":
		return math.Floor(x), nil
	case "min", "max":
		for _, y := range xs[1:] {
			if name == "min" {
				x = math.Min(x, y)
			} else {
				x = math.Max(x, y)
			}
		}
		return x, nil
	}
	// log and ln
	if x <= 0 {
		return 0, errors.New("only numbers above 0 have a logarithm")
	}
	if name == "ln" {
		return math.Log(x), nil
	}
	return math.Log10(x), nil
}

// numberRe finds amounts in a message: "25,000", "2.75", "15k", "1.5M".
// A comma separates thousands only between groups of three digits.
var numberRe = regexp.MustCompile(`(?i)(\d{1,3}(?:,\d{3})+(?:\.\d+)?|\d+(?:\.\d+)?)([kmbt]\b)?`)

// Numbers lists the amounts written in text, at most max: "25,000" is
// 25000 and "2.75" a decimal, and K, M, B and T after a number make it
// thousands, millions, billions and trillions ("15k", "1.5M"). Times
// (09:30) and long digit runs such as phone numbers are left out.
func Numbers(text string, max int) []float64 {
	var out []float64
	for _, m := range numberRe.FindAllStringSubmatchIndex(text, -1) {
		if len(out) == max {
			break
		}
		start, end := m[2], m[3]
		if start > 0 && text[start-1] == ':' || end < len(text) && text[end] == ':' {
			continue
		}
		digits := strings.ReplaceAll(text[start:end], ",", "")
		if len(strings.ReplaceAll(digits, ".", "")) > 12 {
			continue // a phone number, an ID
		}
		v, err := strconv.ParseFloat(digits, 64)
		if err != nil {
			continue
		}
		if m[4] >= 0 {
			v *= suffixes[unicode.ToLower(rune(text[m[4]]))]
		}
		out = append(out, v)
	}
	return out
}
