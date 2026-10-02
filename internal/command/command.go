// Package command is the slash commands typed in a chat's composer, like
// Discord's: "/kick @Budi". They run on this computer, as you; WhatsApp
// checks that you may do what they ask. The UI offers them as you type,
// reads their options with Parse and runs them with Execute; everything
// they need from the UI goes through a Host.
//
// To add a command, add it to the list in commands.go.
package command

import (
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

// Kind is the type of an option's value.
type Kind int

const (
	// Text is the rest of the line.
	Text Kind = iota
	// Member is a group member, picked as an @mention.
	Member
	// Contact is a contact, picked as an @mention, or a phone number with
	// its country code. Several phone numbers are separated by commas.
	Contact
	// Choice is one of Option.Choices.
	Choice
	// Number is a whole number from Option.Min to Option.Max.
	Number
	// When is a time, as ParseWhen reads it: one word ("21:00", "2h")
	// or a day and a time ("tomorrow 08:00"). Choices are suggestions.
	When
)

// Option is a value a command takes. Options are typed in order.
type Option struct {
	Name, Description string
	Kind              Kind
	Required          bool
	// Multiple lets a Member or Contact option take several.
	Multiple bool
	Choices  []string
	// Min and Max bound a Number option's value.
	Min, Max int
	// Until ends a Text option's value at this separator, so that the
	// next option follows it: "/sticker top text#bottom text".
	Until string
	// Filter narrows the members a Member option offers; nil offers all
	// but you.
	Filter func(m model.Member) bool
	// Mentions lets @mentions be picked in a Text option, as in a
	// message.
	Mentions bool
}

// Command is a slash command.
type Command struct {
	Name        string
	Description string
	Options     []Option
	// Group commands run only in groups, and Admin ones only where you're
	// an admin.
	Group, Admin bool
	Run          func(c *Context) error
	// Preview, when set, works out what the command would give with the
	// options typed so far, which the composer shows as you type: text,
	// or why it can't, with ok false ("" for nothing to show).
	Preview func(in *Input) (text string, ok bool)
}

// Usage is the command with its options, as help shows it:
// "/kick member", "/lockdown [mode]".
func (c *Command) Usage() string {
	s := "/" + c.Name
	for _, o := range c.Options {
		if o.Required {
			s += " " + o.Name
		} else {
			s += " [" + o.Name + "]"
		}
	}
	return s
}

// Lookup returns the command named name, or nil.
func Lookup(name string) *Command {
	for _, c := range All {
		if c.Name == strings.ToLower(name) {
			return c
		}
	}
	return nil
}

// Matching lists the commands whose names start with prefix that can run
// in a chat: group commands only in groups.
func Matching(prefix string, group bool) []*Command {
	prefix = strings.ToLower(prefix)
	var out []*Command
	for _, c := range All {
		if strings.HasPrefix(c.Name, prefix) && (group || !c.Group) {
			out = append(out, c)
		}
	}
	return out
}

// Mention is an @mention picked in the composer, which shows "@Name".
type Mention struct{ Name, ID string }

// Value is an option's value as typed.
type Value struct {
	// Text is the value as typed: "@Budi", "+62 812 5550 1234", "on".
	Text string
	// ID is the member or contact a value names, or "" for text, a phone
	// number or a choice.
	ID string
	// Err says what's wrong with the value, or is "".
	Err string
	// Done marks a value that needs no picking: a picked mention, a
	// whole choice or text.
	Done bool
	// Start and End are rune offsets in the composer text.
	Start, End int
}

// Input is composer text read as a command.
type Input struct {
	// Cmd is the command, or nil while its name is still being typed.
	Cmd *Command
	// Name is what follows the "/", up to the first space.
	Name string
	// Naming is set while the caret is in the name: the command picker
	// shows then.
	Naming bool
	// Values holds the values of each of Cmd's options.
	Values [][]Value
	// Extra holds words after the last option's values, which no option
	// takes.
	Extra []Value
	// Current is the option the caret is in, or the next one to type
	// (-1 when there's none).
	Current int
	// Word is the value being typed at the caret, which a picker
	// completes, and WordStart and WordEnd its rune offsets.
	Word               string
	WordStart, WordEnd int
	// Now is the time a Preview reads; the zero time is time.Now().
	Now time.Time
}

// Text returns the first value of option name as typed, or "".
func (in *Input) Text(name string) string {
	if in.Cmd == nil {
		return ""
	}
	for i, o := range in.Cmd.Options {
		if o.Name == name && len(in.Values[i]) > 0 {
			return in.Values[i][0].Text
		}
	}
	return ""
}

// Missing returns the required options that have no value yet.
func (in *Input) Missing() []Option {
	var out []Option
	if in.Cmd == nil {
		return nil
	}
	for i, o := range in.Cmd.Options {
		if o.Required && len(in.Values[i]) == 0 {
			out = append(out, o)
		}
	}
	return out
}

// Problem returns what keeps the command from running, or "".
func (in *Input) Problem() string {
	if in.Cmd == nil {
		return "/" + in.Name + " isn't a command."
	}
	if len(in.Extra) > 0 {
		if len(in.Cmd.Options) == 0 {
			return "/" + in.Cmd.Name + " takes no options."
		}
		return "\"" + in.Extra[0].Text + "\" doesn't fit any option of /" + in.Cmd.Name + "."
	}
	for i, vs := range in.Values {
		for _, v := range vs {
			if v.Err != "" {
				return in.Cmd.Options[i].Name + ": " + v.Err
			}
		}
	}
	if m := in.Missing(); len(m) > 0 {
		names := make([]string, len(m))
		for i, o := range m {
			names[i] = o.Name
		}
		return "Fill in " + strings.Join(names, " and ") + "."
	}
	return ""
}

// Parse reads text as a command, with the caret at rune offset caret.
// mentions are the @mentions picked in the composer and members the
// chat's members (nil outside groups), which typed @names are matched
// against. ok is false when text isn't a command: it doesn't start with
// "/", or names no command once a space follows the name.
func Parse(text string, caret int, mentions []Mention, members []model.Member) (in Input, ok bool) {
	rs := []rune(text)
	if len(rs) == 0 || rs[0] != '/' {
		return Input{}, false
	}
	end := 1
	for end < len(rs) && !isSpace(rs[end]) {
		end++
	}
	in.Name = string(rs[1:end])
	in.Cmd = Lookup(in.Name)
	in.Current = -1
	if in.Cmd != nil {
		in.Values = make([][]Value, len(in.Cmd.Options))
	}
	if end == len(rs) || caret <= end {
		// Still typing the name.
		in.Naming = true
		in.Word, in.WordStart, in.WordEnd = in.Name, 1, end
		return in, in.Cmd != nil || len(Matching(in.Name, true)) > 0
	}
	if in.Cmd == nil {
		return Input{}, false
	}
	toks := tokenize(rs, end, mentions)
	in.assign(rs, toks, mentions, members)
	in.findCurrent(rs, caret)
	return in, true
}

// token is a word of the command's arguments, or a picked @mention.
type token struct {
	text       string
	id         string // a picked mention's
	start, end int
}

// tokenize splits rs from offset from into words, keeping each picked
// mention (whose name may have spaces) in one.
func tokenize(rs []rune, from int, mentions []Mention) []token {
	var toks []token
	for i := from; i < len(rs); {
		if isSpace(rs[i]) {
			i++
			continue
		}
		if rs[i] == '@' {
			if m, n := mentionAt(rs, i, mentions); n > 0 {
				toks = append(toks, token{text: string(rs[i : i+n]), id: m.ID, start: i, end: i + n})
				i += n
				continue
			}
		}
		j := i
		for j < len(rs) && !isSpace(rs[j]) {
			j++
		}
		toks = append(toks, token{text: string(rs[i:j]), start: i, end: j})
		i = j
	}
	return toks
}

// mentionAt returns the longest picked mention whose "@Name" starts at
// rs[i], and its length in runes.
func mentionAt(rs []rune, i int, mentions []Mention) (Mention, int) {
	var best Mention
	n := 0
	for _, m := range mentions {
		at := []rune("@" + m.Name)
		if len(at) <= n || i+len(at) > len(rs) || string(rs[i:i+len(at)]) != string(at) {
			continue
		}
		// It must end at a word's end: "@Al" isn't in "@Alice".
		if k := i + len(at); k < len(rs) && !isSpace(rs[k]) && rs[k] != ',' {
			continue
		}
		best, n = m, len(at)
	}
	return best, n
}

// assign gives the tokens to the options, in order.
func (in *Input) assign(rs []rune, toks []token, mentions []Mention, members []model.Member) {
	opts := in.Cmd.Options
	oi, ti := 0, 0
	defer func() {
		for _, t := range toks[ti:] {
			in.Extra = append(in.Extra, Value{Text: t.text, Start: t.start, End: t.end})
		}
	}()
	for ti < len(toks) && oi < len(opts) {
		t := toks[ti]
		o := &opts[oi]
		full := len(in.Values[oi]) > 0 && !o.Multiple
		if full {
			oi++
			continue
		}
		switch o.Kind {
		case Text:
			// The value runs to the end, or to the option's separator,
			// though its text has no spaces around it.
			stop := len(rs)
			if o.Until != "" {
				if i := strings.Index(string(rs[t.start:]), o.Until); i >= 0 {
					stop = t.start + len([]rune(string(rs[t.start:])[:i]))
				}
			}
			end := stop
			for end > t.start && isSpace(rs[end-1]) {
				end--
			}
			in.Values[oi] = append(in.Values[oi], Value{Text: string(rs[t.start:end]), Start: t.start, End: stop, Done: true})
			oi++
			if stop == len(rs) {
				ti = len(toks)
				return
			}
			// The rest, after the separator, is the next options'.
			toks = append(toks[:ti:ti], tokenize(rs, stop+len([]rune(o.Until)), mentions)...)
			if ti == len(toks) && oi < len(opts) {
				// Nothing typed after it yet: the next option starts there.
				at := stop + len([]rune(o.Until))
				in.Values[oi] = append(in.Values[oi], Value{Start: at, End: at, Done: true})
			}
		case Choice:
			v := Value{Text: t.text, Start: t.start, End: t.end}
			found := false
			for _, c := range o.Choices {
				if strings.EqualFold(c, t.text) {
					v.Text, v.Done, found = c, true, true
				}
			}
			if !found {
				if !o.Required {
					oi++ // perhaps the next option's
					continue
				}
				v.Err = "pick " + strings.Join(o.Choices, " or ")
			}
			in.Values[oi] = append(in.Values[oi], v)
			ti++
		case Number:
			n, err := strconv.Atoi(t.text)
			if err != nil && !o.Required {
				oi++ // perhaps the next option's
				continue
			}
			v := Value{Text: t.text, Start: t.start, End: t.end, Done: true}
			if err != nil || n < o.Min || n > o.Max {
				v.Err, v.Done = "type a number from "+strconv.Itoa(o.Min)+" to "+strconv.Itoa(o.Max), false
			}
			in.Values[oi] = append(in.Values[oi], v)
			ti++
		case When:
			v := Value{Text: t.text, Start: t.start, End: t.end}
			if ti+1 < len(toks) && dayWord(strings.ToLower(t.text)) {
				if n := toks[ti+1]; n.id == "" {
					if _, _, ok := clock(strings.ToLower(n.text)); ok {
						v.Text, v.End = t.text+" "+n.text, n.end
						ti++
					}
				}
			}
			// Whether the time has passed is for when it runs.
			if _, err := ParseWhen(v.Text, time.Now()); err != nil && !errors.As(err, new(outOfRange)) {
				v.Err = err.Error()
			} else {
				v.Done = true
			}
			in.Values[oi] = append(in.Values[oi], v)
			ti++
		case Member:
			if !strings.HasPrefix(t.text, "@") && len(in.Values[oi]) > 0 {
				oi++ // the members are done
				continue
			}
			in.Values[oi] = append(in.Values[oi], memberValue(t, o, members))
			ti++
		case Contact:
			if t.id != "" {
				in.Values[oi] = append(in.Values[oi], Value{Text: t.text, ID: t.id, Start: t.start, End: t.end, Done: true})
				ti++
				continue
			}
			if !phoneish(t.text) {
				if len(in.Values[oi]) > 0 {
					oi++
					continue
				}
				v := Value{Text: t.text, Start: t.start, End: t.end, Err: "pick a contact or type a phone number"}
				in.Values[oi] = append(in.Values[oi], v)
				ti++
				continue
			}
			// A phone number may have spaces; a comma ends it.
			v := Value{Start: t.start, End: t.end}
			for ti++; ti < len(toks) && !strings.HasSuffix(toks[ti-1].text, ",") && phoneish(toks[ti].text) &&
				toks[ti].id == ""; ti++ {
				v.End = toks[ti].end
			}
			v.Text = strings.TrimRight(string(rs[v.Start:v.End]), ",")
			if digits(v.Text) < 7 {
				v.Err = "type the phone number with its country code"
			}
			in.Values[oi] = append(in.Values[oi], v)
		}
	}
}

// memberValue reads a Member option's token: a picked mention, or a
// typed "@name" that only one member's name starts with.
func memberValue(t token, o *Option, members []model.Member) Value {
	v := Value{Text: t.text, ID: t.id, Start: t.start, End: t.end, Done: t.id != ""}
	if v.ID == "" {
		q := strings.ToLower(strings.TrimPrefix(t.text, "@"))
		var hits []model.Member
		for _, m := range members {
			if q != "" && offers(o, m) && strings.HasPrefix(strings.ToLower(strings.TrimPrefix(m.Name, "~")), q) {
				hits = append(hits, m)
			}
		}
		switch {
		case len(hits) == 1:
			v.ID = hits[0].ID
		case len(hits) > 1:
			v.Err = t.text + " could be several members; pick one"
		default:
			v.Err = t.text + " isn't a member you can pick"
		}
		return v
	}
	for _, m := range members {
		if m.ID == v.ID && !offers(o, m) {
			v.Err, v.Done = t.text+" can't be picked here", false
		}
	}
	return v
}

// Offers reports whether a Member option offers member m.
func (o *Option) Offers(m model.Member) bool { return offers(o, m) }

func offers(o *Option, m model.Member) bool {
	if o.Filter != nil {
		return o.Filter(m)
	}
	return !m.Me
}

// findCurrent finds the option and word at the caret.
func (in *Input) findCurrent(rs []rune, caret int) {
	last := -1 // the last option with a value before the caret
	for i, vs := range in.Values {
		for _, v := range vs {
			if caret >= v.Start && caret <= v.End {
				in.Current = i
				in.Word, in.WordStart, in.WordEnd = string(rs[v.Start:caret]), v.Start, v.End
				return
			}
			if v.End < caret {
				last = i
			}
		}
	}
	// Between values: the option the next value goes to.
	in.Word, in.WordStart, in.WordEnd = "", caret, caret
	for i := max(last, 0); i < len(in.Cmd.Options); i++ {
		if in.Cmd.Options[i].Multiple || len(in.Values[i]) == 0 {
			in.Current = i
			return
		}
	}
}

func isSpace(r rune) bool { return r == ' ' || r == '\t' || r == '\n' || r == ' ' }

// phoneish reports whether s could be (part of) a phone number.
func phoneish(s string) bool {
	s = strings.TrimRight(s, ",")
	if s == "" {
		return false
	}
	for _, r := range s {
		if !strings.ContainsRune("+0123456789-().", r) {
			return false
		}
	}
	return true
}

func digits(s string) int {
	n := 0
	for _, r := range s {
		if r >= '0' && r <= '9' {
			n++
		}
	}
	return n
}
