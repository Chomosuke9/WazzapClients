package usercmd

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	stdtime "time"

	"github.com/dop251/goja"

	"github.com/chomosuke9/wazzapclients/internal/command"
	"github.com/chomosuke9/wazzapclients/internal/model"
)

const (
	// maxSends is how many messages one run may send: a loop that spams
	// could get the account banned.
	maxSends = 30
	// maxCalls is how many other things it may ask of the UI, and
	// maxTimers how many timers it may have going.
	maxCalls   = 500
	maxTimers  = 500
	maxText    = 4096
	maxButtons = 5
	// runTime is how long a run may take, waiting for answers included.
	runTime = 5 * stdtime.Minute
	// busyAfter is when a run that goes on shows a note to stop it.
	busyAfter = stdtime.Second
)

var errStopped = errors.New("stopped")

// run is a command running. The script runs on a goroutine of its own,
// with an event loop (loop); what it asks of the UI goes through reqs, in
// the order asked, and runs on the UI goroutine (pump), whose answers come
// back through answers. A window that closes stops pumping, so the script
// does nothing more and ends by its deadline.
type run struct {
	c       *command.Context
	cmd     *Command
	ctx     context.Context
	cancel  context.CancelFunc
	reqs    chan func() // for the UI goroutine
	answers chan func() // for the script's; holds every answer there can be
	timers  chan func() // for the script's
	done    chan error
	ended   chan struct{} // closed once the script ended

	// The script goroutine's.
	*engine
	calls, sends int
	waiting      int                      // requests not answered yet
	timeouts     map[int64]*stdtime.Timer // going
	lastTimeout  int64
	log          []string // what it printed

	// The UI goroutine's.
	over, stopped bool          // ended; stopped by the user
	busy          *command.Note // the note to stop it with
	asking        *command.Note // ask's, waiting for an answer
	// people are the members the script knows, by their mention's text.
	people map[string]string
	// known are the messages the script was given.
	known map[ref]*model.Message
}

// start is the command's Run.
func (cmd *Command) start(c *command.Context) error {
	if c.Chat == nil {
		return errors.New("Open a chat first.")
	}
	r := &run{c: c, cmd: cmd, people: map[string]string{}, known: map[ref]*model.Message{}}
	args, vars := r.args()
	if cmd.template != "" {
		return r.sendTemplate(vars)
	}
	r.reqs, r.answers = make(chan func(), maxCalls), make(chan func(), maxCalls)
	r.timers, r.timeouts = make(chan func()), map[int64]*stdtime.Timer{}
	r.done, r.ended = make(chan error, 1), make(chan struct{})
	r.ctx, r.cancel = context.WithTimeout(context.Background(), runTime)
	given := r.given(args) // reads the UI, so here
	go func() {
		defer close(r.ended)
		r.done <- r.script(given)
	}()
	r.pump()
	r.showBusy()
	return nil
}

// script runs the command's function, on its own goroutine, until nothing
// it began goes on.
func (r *run) script(given fields) (err error) {
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("%v", p)
		}
	}()
	r.engine = newEngine()
	begin, now := stdtime.Now(), r.c.Now
	r.vm.SetTimeSource(func() stdtime.Time { return now.Add(stdtime.Since(begin)) })
	defer context.AfterFunc(r.ctx, func() { r.vm.Interrupt(errStopped) })()
	defer func() {
		for _, t := range r.timeouts {
			t.Stop()
		}
	}()
	r.globals()
	var result goja.Value
	err = r.guard(maxBusy, func() error {
		if err := r.load(r.cmd.Source); err != nil {
			return err
		}
		if r.spec == nil {
			return errors.New("the file didn't call command()")
		}
		f, ok := goja.AssertFunction(r.spec.Get("run"))
		if !ok {
			return errors.New("the command has no run function")
		}
		ctx := r.value(given).(*goja.Object)
		r.object(ctx)
		var err error
		result, err = f(r.spec, ctx)
		return err
	})
	for err == nil && (r.waiting > 0 || len(r.timeouts) > 0) {
		var f func()
		select {
		case f = <-r.answers:
		case f = <-r.timers:
		case <-r.ctx.Done():
			return errStopped
		}
		err = r.guard(maxBusy, func() error { f(); return nil })
	}
	if err == nil && result != nil {
		if p, ok := result.Export().(*goja.Promise); ok && p.State() == goja.PromiseStatePending {
			err = errors.New("It waits for something that never comes.")
		}
	}
	return err
}

// pump runs what the script asks on the UI goroutine, until it ends.
func (r *run) pump() {
	r.c.Do(func() func() {
		select {
		case f := <-r.reqs:
			return func() {
				for f != nil {
					if r.ctx.Err() == nil { // not stopped meanwhile
						f()
					}
					select {
					case f = <-r.reqs:
					default:
						f = nil
					}
				}
				r.pump()
			}
		case err := <-r.done:
			return func() { r.finish(err) }
		}
	})
}

// finish ends the run with what the script returned.
func (r *run) finish(err error) {
	r.over = true
	timedOut := errors.Is(r.ctx.Err(), context.DeadlineExceeded)
	r.cancel()
	for _, n := range []*command.Note{r.busy, r.asking} {
		if n != nil {
			r.c.Dismiss(n)
		}
	}
	if s := strings.Join(r.log, "\n"); s != "" {
		if len(s) > maxText {
			s = "…" + strings.ToValidUTF8(s[len(s)-maxText:], "")
		}
		r.c.Note(&command.Note{Title: r.c.Input, Text: s})
	}
	switch {
	case err == nil || r.stopped:
	case timedOut:
		r.c.Fail("/" + r.cmd.Cmd.Name + " took too long.")
	default:
		r.c.Fail("/" + r.cmd.Cmd.Name + " failed. " + err.Error())
	}
}

// showBusy shows a note to stop the run with, once it has gone on a while.
func (r *run) showBusy() {
	r.c.Do(func() func() {
		select {
		case <-r.ended:
		case <-stdtime.After(busyAfter):
		}
		return func() {
			if r.over || r.asking != nil || r.ctx.Err() != nil {
				return
			}
			r.busy = &command.Note{Title: r.c.Input, Busy: true}
			r.busy.Buttons = []command.Button{{Label: "Stop", Danger: true, Run: func() {
				r.stopped = true
				r.cancel()
			}}}
			r.c.Note(r.busy)
		}
	})
}

// task is something a script asks of the UI. It runs on the UI goroutine,
// and gives its answer (of what value takes) then or later.
type task func(answer func(any, error))

// now is a task answered as it runs.
func now(f func() (any, error)) task {
	return func(answer func(any, error)) { answer(f()) }
}

// async is a function for the script that reads its arguments with f and
// asks the UI for the task it makes. It returns a promise; what goes wrong,
// in the arguments too, rejects it with an error where it was called.
func (r *run) async(name string, f func(args []goja.Value) (task, error)) func(goja.FunctionCall) goja.Value {
	return func(c goja.FunctionCall) goja.Value {
		p, resolve, reject := r.vm.NewPromise()
		e := r.newError("")
		settle := func(v any, err error) {
			if err != nil {
				_ = e.Set("message", name+": "+strings.TrimPrefix(err.Error(), name+": "))
				err = reject(e)
			} else {
				err = resolve(r.value(v))
			}
			if err != nil {
				panic(err) // what can't be caught: the run was stopped
			}
		}
		t, err := f(c.Arguments)
		if r.calls++; err == nil && r.calls > maxCalls {
			err = fmt.Errorf("over %d calls in one run", maxCalls)
		}
		if err != nil {
			settle(nil, err)
			return r.vm.ToValue(p)
		}
		r.waiting++
		answered := false // on the UI goroutine
		answer := func(v any, err error) {
			if answered {
				return
			}
			answered = true
			r.answers <- func() {
				r.waiting--
				settle(v, err)
			}
		}
		r.ask(func() { t(answer) })
		return r.vm.ToValue(p)
	}
}

// ask has the UI goroutine run f.
func (r *run) ask(f func()) {
	select {
	case r.reqs <- f:
	case <-r.ctx.Done():
	}
}

// fields is an object for a script: its keys, each with its value after.
type fields []any

// jsonText is an object for a script, as JSON.
type jsonText string

// value makes a value of what the UI goroutine gave.
func (r *run) value(v any) goja.Value {
	switch v := v.(type) {
	case nil:
		return goja.Null()
	case fields:
		o := r.vm.NewObject()
		for i := 0; i+1 < len(v); i += 2 {
			_ = o.Set(v[i].(string), r.value(v[i+1]))
		}
		return o
	case []any:
		vs := make([]any, len(v))
		for i, x := range v {
			vs[i] = r.value(x)
		}
		return r.vm.NewArray(vs...)
	case jsonText:
		x, err := r.parse(goja.Undefined(), r.vm.ToValue(string(v)))
		if err != nil {
			return goja.Null()
		}
		return x
	}
	return r.vm.ToValue(v)
}

// globals are the functions every run has besides JavaScript's own.
func (r *run) globals() {
	timeout := func(ms goja.Value, f func()) int64 {
		if len(r.timeouts) >= maxTimers {
			r.throw(fmt.Sprintf("over %d timers", maxTimers))
		}
		d := stdtime.Duration(min(max(ms.ToFloat(), 0), float64(runTime/stdtime.Millisecond))) * stdtime.Millisecond
		r.lastTimeout++
		id := r.lastTimeout
		r.timeouts[id] = stdtime.AfterFunc(d, func() {
			select {
			case r.timers <- func() {
				if r.timeouts[id] != nil { // not cleared
					delete(r.timeouts, id)
					f()
				}
			}:
			case <-r.ctx.Done():
			}
		})
		return id
	}
	r.set("setTimeout", func(c goja.FunctionCall) goja.Value {
		f, ok := goja.AssertFunction(c.Argument(0))
		if !ok {
			r.throw("setTimeout: give it a function")
		}
		var args []goja.Value
		if len(c.Arguments) > 2 {
			args = c.Arguments[2:]
		}
		return r.vm.ToValue(timeout(c.Argument(1), func() {
			if _, err := f(goja.Undefined(), args...); err != nil {
				panic(err)
			}
		}))
	})
	r.set("clearTimeout", func(c goja.FunctionCall) goja.Value {
		id := c.Argument(0).ToInteger()
		if t := r.timeouts[id]; t != nil {
			t.Stop()
			delete(r.timeouts, id)
		}
		return goja.Undefined()
	})
	r.set("sleep", func(c goja.FunctionCall) goja.Value {
		p, resolve, _ := r.vm.NewPromise()
		timeout(c.Argument(0), func() {
			if err := resolve(goja.Undefined()); err != nil {
				panic(err)
			}
		})
		return r.vm.ToValue(p)
	})
	console := r.vm.NewObject()
	for _, name := range []string{"log", "info", "debug", "warn", "error"} {
		_ = console.Set(name, func(c goja.FunctionCall) goja.Value {
			var parts []string
			for _, v := range c.Arguments {
				parts = append(parts, r.shown(v))
			}
			line := strings.Join(parts, " ")
			if len(line) > maxText {
				line = strings.ToValidUTF8(line[:maxText], "") + "…"
			}
			if r.log = append(r.log, line); len(r.log) > 200 {
				r.log = r.log[1:]
			}
			return goja.Undefined()
		})
	}
	r.set("console", console)
}

// shown is a value as console.log shows it.
func (r *run) shown(v goja.Value) string {
	o, ok := v.(*goja.Object)
	if !ok {
		return v.String()
	}
	if _, ok := o.Export().(*goja.Promise); ok {
		return "a promise (put await before it)"
	}
	switch o.ClassName() {
	case "Error":
		return r.describe(v)
	case "Function":
		return o.String()
	}
	if s, err := r.print(goja.Undefined(), v); err == nil && !none(s) {
		return s.String()
	}
	return o.String()
}

// countSend counts a message the script is about to send.
func (r *run) countSend() error {
	if r.sends++; r.sends > maxSends {
		return fmt.Errorf("over %d messages in one run", maxSends)
	}
	return nil
}

// args are the options' values: for the script, and as text for a
// template.
func (r *run) args() (fields, map[string]string) {
	var args fields
	vars := map[string]string{}
	for i, o := range r.cmd.Cmd.Options {
		vs := []any{}
		var texts []string
		for _, v := range r.c.Values[i] {
			switch o.Kind {
			case command.Number:
				n, _ := strconv.Atoi(v.Text)
				vs = append(vs, n)
				texts = append(texts, v.Text)
			case command.Member:
				m := model.Member{ID: v.ID, Name: strings.TrimPrefix(v.Text, "@")}
				if r.c.Info != nil {
					for _, im := range r.c.Info.Members {
						if im.ID == v.ID {
							m = im
						}
					}
				}
				vs = append(vs, r.member(m))
				texts = append(texts, r.mention(v.ID))
			default:
				vs = append(vs, v.Text)
				texts = append(texts, v.Text)
			}
		}
		switch {
		case o.Multiple:
			args = append(args, o.Name, vs)
		case len(vs) > 0:
			args = append(args, o.Name, vs[0])
		default:
			args = append(args, o.Name, nil)
		}
		vars[o.Name] = strings.Join(texts, " ")
	}
	return args, vars
}

// mention is how a message's text mentions a member, whom send then knows.
func (r *run) mention(id string) string {
	user, _, _ := strings.Cut(id, "@")
	r.people[user] = id
	return "@" + user
}

// member is a member for the script. Their admin is Baileys'.
func (r *run) member(m model.Member) fields {
	var admin any
	switch {
	case m.SuperAdmin:
		admin = "superadmin"
	case m.Admin:
		admin = "admin"
	}
	return fields{"id", m.ID, "name", strings.TrimPrefix(m.Name, "~"), "mention", r.mention(m.ID), "admin", admin, "me", m.Me}
}

func (r *run) members(ms []model.Member) []any {
	vs := []any{}
	for _, m := range ms {
		if m.ID != "" {
			vs = append(vs, r.member(m))
		}
	}
	return vs
}

// sendTemplate sends the command's text with the options filled in, and
// the variables snippets have.
func (r *run) sendTemplate(vars map[string]string) error {
	all := r.c.SnippetVars(r.c.Chat.ID, r.c.Reply)
	if all == nil {
		all = map[string]string{}
	}
	for k, v := range vars {
		all[k] = v
	}
	_, err := r.send(model.ExpandSnippet(r.cmd.template, all))
	return err
}

var mentionRe = regexp.MustCompile(`@([\w-]+)`)

// send sends text to the chat, on the UI goroutine.
func (r *run) send(text string) (*model.Message, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, errors.New("There's nothing to send.")
	}
	if len(text) > maxText {
		return nil, errors.New("The message is too long.")
	}
	d := r.draft(text, nil)
	d.Reply = r.c.Reply
	m := r.c.Backend.Send(r.c.Chat.ID, d)
	if m == nil {
		return nil, errors.New("The message wasn't sent.")
	}
	r.c.Sent(m)
	return m, nil
}

// given is what run's ctx holds, read on the UI goroutine.
func (r *run) given(args fields) fields {
	var user, quoted any
	if a := r.c.Backend.Account(); a != nil {
		user = fields{"id", a.ID, "lid", a.LID, "name", a.Name}
	}
	if r.c.Reply != nil {
		quoted = r.message(r.c.Reply)
	}
	name := r.c.SnippetVars(r.c.Chat.ID, nil)["chat"]
	return fields{"args", args, "quoted", quoted, "sock", fields{"user", user},
		"chat", fields{"id", r.c.Chat.ID, "name", name, "group", r.c.Chat.IsGroup}}
}

// object puts ctx's functions in it.
func (r *run) object(ctx *goja.Object) {
	add := func(name string, f func(args []goja.Value) (task, error)) { _ = ctx.Set(name, r.async("ctx."+name, f)) }
	// text is a function of one text.
	text := func(f func(s string) (any, error)) func(args []goja.Value) (task, error) {
		return func(args []goja.Value) (task, error) {
			s, err := textArg(args, 0)
			if err != nil {
				return nil, err
			}
			return now(func() (any, error) { return f(s) }), nil
		}
	}
	send := text(func(s string) (any, error) {
		m, err := r.send(s)
		if err != nil {
			return nil, err
		}
		return r.message(m), nil
	})
	add("send", func(args []goja.Value) (task, error) {
		if err := r.countSend(); err != nil {
			return nil, err
		}
		return send(args)
	})
	add("note", text(func(s string) (any, error) {
		r.c.Note(&command.Note{Title: r.c.Input, Text: s})
		return nil, nil
	}))
	add("copy", text(func(s string) (any, error) { r.c.Copy(s); return nil, nil }))
	add("ask", r.askButtons)
	add("members", func([]goja.Value) (task, error) {
		return now(func() (any, error) {
			switch {
			case r.c.Info != nil:
				return r.members(r.c.Info.Members), nil
			case r.c.Chat.IsGroup:
				return nil, errors.New("the group's members haven't loaded yet; try again in a moment")
			}
			return []any{}, nil
		}), nil
	})
	r.sock(ctx.Get("sock").(*goja.Object))
}

// arg is an argument, undefined when it's left out.
func arg(args []goja.Value, i int) goja.Value {
	if i < len(args) {
		return args[i]
	}
	return goja.Undefined()
}

// textArg is an argument to show or send: text, or a number and such.
func textArg(args []goja.Value, i int) (string, error) {
	v := arg(args, i)
	if _, ok := v.(*goja.Object); ok || none(v) {
		return "", errors.New("give it text" + forgot(v))
	}
	s := v.String()
	if len(s) > maxText {
		return "", fmt.Errorf("text over %d bytes", maxText)
	}
	return s, nil
}

// askButtons is ask(text, buttons): it shows a note with buttons and gives
// the one clicked, or null when the note is dismissed.
func (r *run) askButtons(args []goja.Value) (task, error) {
	text, err := textArg(args, 0)
	if err != nil {
		return nil, err
	}
	vs, err := items(arg(args, 1))
	if err != nil || len(vs) == 0 || len(vs) > maxButtons {
		return nil, fmt.Errorf("give a list of 1 to %d buttons", maxButtons)
	}
	var labels []string
	for _, v := range vs {
		s, ok := v.Export().(string)
		if !ok || oneLine(s) == "" {
			return nil, fmt.Errorf("%s can't be a button", v)
		}
		labels = append(labels, oneLine(s))
	}
	return func(answer func(any, error)) {
		if r.asking != nil {
			answer(nil, errors.New("it's asking something already"))
			return
		}
		n := &command.Note{Title: r.c.Input, Text: text}
		r.asking = n
		for _, l := range labels {
			n.Buttons = append(n.Buttons, command.Button{Label: l, Run: func() {
				answer(l, nil)
				r.c.Dismiss(n)
			}})
		}
		n.Dismissed = func() {
			r.asking = nil
			answer(nil, nil)
		}
		r.c.Note(n)
	}, nil
}
