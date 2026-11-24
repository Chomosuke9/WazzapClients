// Package usercmd reads slash commands that users write or import: a
// JavaScript file that declares one command (docs/commands.md), either a
// template of the text it sends or a run function. A script may use the
// whole account, through ctx (run.go) and ctx.sock (sock.go); what it does
// is up to whoever adds it, who is told so. It gets no files and no network.
package usercmd

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	stdtime "time"
	"unicode"

	"github.com/dop251/goja"
	"github.com/dop251/goja/parser"

	"github.com/chomosuke9/wazzapclients/internal/command"
)

const (
	// MaxSource is the size of a command's file.
	MaxSource = 64 << 10
	// loadTime is how long a file's top level may take, and maxBusy how
	// long a running command may go on without waiting for anything.
	loadTime = stdtime.Second
	maxBusy  = 10 * stdtime.Second
	// maxStack is how many calls deep a script may go.
	maxStack   = 2000
	maxOptions = 10
	maxNumber  = 1_000_000_000
	// fileName is what the engine calls a command's file, in its errors.
	fileName = "command.js"
)

var errTooLong = errors.New("it ran too long without a pause")

// Command is a command read from its file.
type Command struct {
	Source string
	// Cmd is the command for the picker, and to run.
	Cmd *command.Command

	template string // the text to send; "" has a run function to call
}

var nameRe = regexp.MustCompile(`^[a-z][a-z0-9_]{0,23}$`)

// Load reads a command's file. It runs the file's top level, which
// declares the command, and not the command itself. The engine goes once
// it's read: one costs memory, and only a run needs it again.
func Load(src []byte) (*Command, error) {
	if len(src) > MaxSource {
		return nil, fmt.Errorf("The file is over %d KB.", MaxSource>>10)
	}
	c := &Command{Source: string(src)}
	e := newEngine()
	err := e.guard(loadTime, func() error {
		if err := e.load(c.Source); err != nil {
			return err
		}
		if e.spec == nil {
			return errors.New("The file doesn't call command(), which declares its command.")
		}
		return c.declare(e)
	})
	if err != nil {
		return nil, err
	}
	c.Cmd.Run = c.start
	return c, nil
}

// engine is a JavaScript engine that holds a command's file. Only one
// goroutine at a time may use it.
type engine struct {
	vm   *goja.Runtime
	spec *goja.Object // what the file gave command()

	// The script can replace these; the app's own use can't change.
	newErr       *goja.Object
	parse, print goja.Callable // JSON.parse, JSON.stringify

	// unhandled are the rejected promises nothing handles (yet).
	unhandled []*goja.Promise
}

func newEngine() *engine {
	e := &engine{vm: goja.New()}
	e.vm.SetMaxCallStackSize(maxStack)
	// Else a file's sourceMappingURL names a file to read.
	e.vm.SetParserOptions(parser.WithDisableSourceMaps)
	e.vm.SetFieldNameMapper(goja.UncapFieldNameMapper())
	e.newErr = e.vm.Get("Error").(*goja.Object)
	json := e.vm.Get("JSON").(*goja.Object)
	e.parse, _ = goja.AssertFunction(json.Get("parse"))
	e.print, _ = goja.AssertFunction(json.Get("stringify"))
	e.vm.SetPromiseRejectionTracker(func(p *goja.Promise, op goja.PromiseRejectionOperation) {
		if op == goja.PromiseRejectionReject {
			e.unhandled = append(e.unhandled, p)
		} else {
			e.unhandled = slices.DeleteFunc(e.unhandled, func(o *goja.Promise) bool { return o == p })
		}
	})
	e.set("command", func(c goja.FunctionCall) goja.Value {
		spec, ok := c.Argument(0).(*goja.Object)
		switch {
		case e.spec != nil:
			e.throw("command: a file declares one command")
		case !ok:
			e.throw("command: give it one object, as command({ name: \"...\" })")
		}
		e.spec = spec
		return goja.Undefined()
	})
	e.set("require", func(goja.FunctionCall) goja.Value {
		e.throw("A command can't load modules; all it has is in ctx.")
		return nil
	})
	return e
}

func (e *engine) set(name string, v any) { _ = e.vm.Set(name, v) }

// throw ends the script's call with an error, from a function it called.
func (e *engine) throw(text string) { panic(e.newError(text)) }

// newError is an Error as if thrown where the script is.
func (e *engine) newError(text string) *goja.Object {
	o, err := e.vm.New(e.newErr, e.vm.ToValue(text))
	if err != nil {
		return e.vm.NewTypeError("%s", text)
	}
	return o
}

// load runs the file's top level.
func (e *engine) load(src string) error {
	ast, err := goja.Parse(fileName, src, parser.WithDisableSourceMaps)
	if err != nil {
		return err
	}
	prg, err := goja.CompileAST(ast, false)
	if err != nil {
		return err
	}
	_, err = e.vm.RunProgram(prg)
	return err
}

// guard runs f, which uses the engine, for limit at most, and words the
// error it ends with: its own, or one that nothing in the script handles.
func (e *engine) guard(limit stdtime.Duration, f func() error) (err error) {
	defer func() {
		if p := recover(); p != nil {
			err = errors.New(e.explain(p))
		}
	}()
	t := stdtime.AfterFunc(limit, func() { e.vm.Interrupt(errTooLong) })
	defer t.Stop()
	// Try catches what f's own use of the script's values throws, as their
	// getters may.
	if ex := e.vm.Try(func() { err = f() }); ex != nil {
		err = ex
	}
	if err == nil && len(e.unhandled) > 0 {
		return errors.New(e.describe(e.unhandled[0].Result()))
	}
	if err != nil {
		var in *goja.InterruptedError
		if errors.As(err, &in) {
			if v, ok := in.Value().(error); ok && v == errStopped {
				return errStopped
			}
		}
		return errors.New(e.explain(err))
	}
	return nil
}

var (
	lineRe  = regexp.MustCompile(regexp.QuoteMeta(fileName) + `:(\d+):\d+`)
	syntaxR = regexp.MustCompile(`^(?:SyntaxError: )?(?:` + regexp.QuoteMeta(fileName) + `: )?(?:Line \d+:\d+ )?`)
)

// explain words what went wrong in a script, with the line it was on.
func (e *engine) explain(p any) string {
	err, ok := p.(error)
	if !ok {
		if v, ok := p.(goja.Value); ok {
			return e.describe(v)
		}
		return fmt.Sprint(p)
	}
	var (
		se *goja.CompilerSyntaxError
		in *goja.InterruptedError
		so *goja.StackOverflowError
		ex *goja.Exception
	)
	switch {
	case errors.As(err, &se):
		msg := se.Message
		if se.File != nil {
			msg = "Line " + strconv.Itoa(se.File.Position(se.Offset).Line) + ": " + syntaxR.ReplaceAllString(msg, "")
		}
		return msg
	case errors.As(err, &in):
		return atLine(in.String(), fmt.Sprint(in.Value()))
	case errors.As(err, &so):
		return atLine(so.String(), "the calls go too deep")
	case errors.As(err, &ex):
		s := e.describe(ex.Value())
		if !strings.HasPrefix(s, "Line ") {
			s = atLine(ex.String(), s)
		}
		return s
	}
	return err.Error()
}

// describe words a thrown value.
func (e *engine) describe(v goja.Value) (s string) {
	o, ok := v.(*goja.Object)
	if !ok {
		return v.String()
	}
	s = "an error"
	e.vm.Try(func() {
		if msg := o.Get("message"); !none(msg) {
			s = msg.String()
			if name := o.Get("name"); !none(name) && name.String() != "Error" {
				s = name.String() + ": " + s
			}
			if st := o.Get("stack"); !none(st) {
				s = atLine(st.String(), s)
			}
			return
		}
		s = o.String()
	})
	return s
}

// atLine puts the first line of the file a stack names before s.
func atLine(stack, s string) string {
	if m := lineRe.FindStringSubmatch(stack); m != nil {
		return "Line " + m[1] + ": " + s
	}
	return s
}

// none reports whether v is nothing: undefined, null or missing.
func none(v goja.Value) bool {
	return v == nil || goja.IsUndefined(v) || goja.IsNull(v)
}

// only checks that an object has no other keys than those it takes: a
// misspelt one would otherwise do nothing.
func only(o *goja.Object, what string, keys ...string) error {
	for _, k := range o.Keys() {
		if !slices.Contains(keys, k) {
			return fmt.Errorf("%s: there's no %q; it takes %s", what, k, strings.Join(keys, ", "))
		}
	}
	return nil
}

// str is a text property, "" when it's left out.
func str(o *goja.Object, what, key string) (string, error) {
	v := o.Get(key)
	if none(v) {
		return "", nil
	}
	s, ok := v.Export().(string)
	if !ok {
		return "", fmt.Errorf("%s: %s must be text%s", what, key, forgot(v))
	}
	return s, nil
}

func flag(o *goja.Object, key string) bool {
	v := o.Get(key)
	return !none(v) && v.ToBoolean()
}

// whole reads a whole number property, def when it's left out.
func whole(o *goja.Object, what, key string, def int) (int, error) {
	v := o.Get(key)
	if none(v) {
		return def, nil
	}
	n, ok := v.Export().(int64)
	if !ok || n < -maxNumber || n > maxNumber {
		return 0, fmt.Errorf("%s: %s must be a whole number within ±%d", what, key, maxNumber)
	}
	return int(n), nil
}

// forgot is what to say of a value that's a promise.
func forgot(v goja.Value) string {
	if _, ok := v.Export().(*goja.Promise); ok {
		return " (it's a promise: put await before it)"
	}
	return ""
}

// items reads a list of values; nothing is an empty one.
func items(v goja.Value) ([]goja.Value, error) {
	if none(v) {
		return nil, nil
	}
	o, ok := v.(*goja.Object)
	if !ok || o.ClassName() != "Array" {
		return nil, errors.New("give a list" + forgot(v))
	}
	n := o.Get("length").ToInteger()
	if n > 10_000 {
		return nil, errors.New("the list is too long")
	}
	vs := make([]goja.Value, n)
	for i := range vs {
		if vs[i] = o.Get(strconv.Itoa(i)); vs[i] == nil {
			vs[i] = goja.Undefined()
		}
	}
	return vs, nil
}

var optionKinds = map[string]command.Kind{"text": command.Text, "number": command.Number,
	"choice": command.Choice, "member": command.Member}

// option reads one of command()'s options.
func option(v goja.Value) (o command.Option, err error) {
	s, ok := v.(*goja.Object)
	if !ok {
		return o, errors.New(`command: an option is an object, as { type: "text", name: "..." }`)
	}
	kind, err := str(s, "option", "type")
	if err != nil {
		return o, err
	}
	if o.Kind, ok = optionKinds[kind]; !ok {
		return o, fmt.Errorf("option: type %q: use text, number, choice or member", kind)
	}
	keys := []string{"type", "name", "description", "required"}
	switch o.Kind {
	case command.Number:
		keys = append(keys, "min", "max")
	case command.Choice:
		keys = append(keys, "choices")
	case command.Member:
		keys = append(keys, "multiple")
	}
	if err = only(s, kind+" option", keys...); err != nil {
		return o, err
	}
	if o.Name, err = str(s, "option", "name"); err != nil {
		return o, err
	}
	if !nameRe.MatchString(o.Name) {
		return o, fmt.Errorf("option: name %q: use lowercase letters, digits and _", o.Name)
	}
	if o.Description, err = str(s, o.Name, "description"); err != nil {
		return o, err
	}
	o.Description = oneLine(o.Description)
	o.Required, o.Multiple = flag(s, "required"), flag(s, "multiple")
	if o.Min, err = whole(s, o.Name, "min", 0); err != nil {
		return o, err
	}
	if o.Max, err = whole(s, o.Name, "max", maxNumber); err != nil {
		return o, err
	}
	if o.Min > o.Max {
		return o, fmt.Errorf("%s: min is over max", o.Name)
	}
	if o.Kind == command.Choice {
		cs, err := items(s.Get("choices"))
		if err != nil || len(cs) == 0 || len(cs) > 25 {
			return o, fmt.Errorf("%s: give 1 to 25 choices", o.Name)
		}
		for _, c := range cs {
			t, ok := c.Export().(string)
			if !ok || t == "" || strings.ContainsFunc(t, unicode.IsSpace) || oneLine(t) != t {
				return o, fmt.Errorf("%s: %s: choices are words without spaces", o.Name, c)
			}
			o.Choices = append(o.Choices, t)
		}
	}
	return o, nil
}

// declare reads what the file gave command().
func (c *Command) declare(e *engine) (err error) {
	spec, cmd := e.spec, &command.Command{}
	if err = only(spec, "command", "name", "description", "options", "text", "run", "group", "admin"); err != nil {
		return err
	}
	if cmd.Name, err = str(spec, "command", "name"); err != nil {
		return err
	}
	if !nameRe.MatchString(cmd.Name) {
		return fmt.Errorf("command: name %q: use lowercase letters, digits and _, at most 24", cmd.Name)
	}
	if cmd.Description, err = str(spec, "command", "description"); err != nil {
		return err
	}
	if cmd.Description = oneLine(cmd.Description); cmd.Description == "" {
		return errors.New("command: give it a description")
	}
	if c.template, err = str(spec, "command", "text"); err != nil {
		return err
	}
	run := spec.Get("run")
	switch _, ok := goja.AssertFunction(run); {
	case !none(run) && !ok:
		return errors.New("command: run must be a function: async run(ctx) { ... }")
	case ok && c.template != "":
		return errors.New("Give the command either text or a run function, not both.")
	case !ok && c.template == "":
		return errors.New("The command needs a text to send, or a run function.")
	}
	cmd.Admin = flag(spec, "admin")
	cmd.Group = flag(spec, "group") || cmd.Admin
	opts, err := items(spec.Get("options"))
	if err != nil {
		return fmt.Errorf("command: options: %w", err)
	}
	if len(opts) > maxOptions {
		return fmt.Errorf("command: over %d options", maxOptions)
	}
	for _, v := range opts {
		o, err := option(v)
		if err != nil {
			return err
		}
		if n := len(cmd.Options); n > 0 {
			switch last := cmd.Options[n-1]; {
			case last.Kind == command.Text:
				return fmt.Errorf("command: the text option %q takes the rest of the line, so it must be last", last.Name)
			case o.Required && !last.Required:
				return fmt.Errorf("command: the required option %q must come before the optional ones", o.Name)
			}
		}
		if slices.ContainsFunc(cmd.Options, func(p command.Option) bool { return p.Name == o.Name }) {
			return fmt.Errorf("command: two options are called %q", o.Name)
		}
		cmd.Group = cmd.Group || o.Kind == command.Member // only groups have members
		cmd.Options = append(cmd.Options, o)
	}
	c.Cmd = cmd
	return nil
}

// oneLine makes text from a file safe to show among the app's own: one
// short line, without invisible characters such as bidi overrides.
func oneLine(s string) string {
	s = strings.Map(func(r rune) rune {
		switch {
		case unicode.IsSpace(r):
			return ' '
		case !unicode.IsGraphic(r):
			return -1
		}
		return r
	}, s)
	s = strings.Join(strings.Fields(s), " ")
	if rs := []rune(s); len(rs) > 120 {
		s = string(rs[:119]) + "…"
	}
	return s
}
