package usercmd

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/chomosuke9/wazzapclients/internal/command"
	"github.com/chomosuke9/wazzapclients/internal/model"
)

// testHost runs everything on the test's goroutine, as the UI's would.
type testHost struct {
	command.Host
	notes   []*command.Note
	copied  string
	offline bool
	click   func(n *command.Note) // what the user does with a note with buttons
}

func (h *testHost) Note(n *command.Note) {
	h.notes = append(h.notes, n)
	if len(n.Buttons) > 0 && h.click != nil {
		h.click(n)
	}
}
func (h *testHost) Dismiss(n *command.Note) {
	if n.Dismissed != nil {
		n.Dismissed()
	}
}
func (h *testHost) Do(work func() func()) { work()() }
func (h *testHost) Online() bool          { return !h.offline }
func (h *testHost) Copy(s string)         { h.copied = s }
func (h *testHost) Sent(*model.Message)   {}
func (h *testHost) SnippetVars(string, *model.Message) map[string]string {
	return map[string]string{"chat": "Work", "me": "Me"}
}

type testBackend struct {
	model.Backend
	sent          []model.Draft
	refuse        bool
	prefs         map[string]string
	reacted       string
	deletedStatus string
	deleted       bool
}

func (b *testBackend) Send(_ string, d model.Draft) *model.Message {
	if b.refuse {
		return nil
	}
	b.sent = append(b.sent, d)
	return &model.Message{Text: d.Text}
}

func (b *testBackend) ChatID(id string) string              { return id }
func (b *testBackend) Account() *model.Account              { return nil }
func (b *testBackend) Pref(key string) string               { return b.prefs[key] }
func (b *testBackend) React(m *model.Message, emoji string) { b.reacted = emoji }
func (b *testBackend) Delete(*model.Message, bool)          { b.deleted = true }
func (b *testBackend) DeleteGroupStatus(group, id string) error {
	b.deletedStatus = group + "/" + id
	return nil
}
func (b *testBackend) MessagesFrom(string, string, int) []*model.Message { return nil }
func (b *testBackend) MessagePayload(string, string) (string, error) {
	return `{"conversation":"x"}`, nil
}
func (b *testBackend) SendPayload(chat, payload, messageID string, _ *model.Message) (*model.Message, bool, error) {
	b.sent = append(b.sent, model.Draft{Text: payload})
	id := "p"
	if messageID != "" {
		id = messageID
	}
	return &model.Message{ID: id, ChatID: chat}, true, nil
}

var members = []model.Member{{ID: "1@lid", Name: "You", Me: true}, {ID: "22@lid", Name: "Budi"}, {ID: "222@lid", Name: "~Siti", Admin: true}}

type runner struct {
	h *testHost
	b *testBackend
	c *command.Context
}

func newRunner(t *testing.T, src string, values ...[]command.Value) *runner {
	t.Helper()
	cmd, err := Load([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	for len(values) < len(cmd.Cmd.Options) {
		values = append(values, nil)
	}
	r := &runner{h: &testHost{}, b: &testBackend{}}
	r.c = &command.Context{Cmd: cmd.Cmd, Input: "/" + cmd.Cmd.Name, Values: values, Backend: r.b, Host: r.h, Now: time.Unix(0, 0),
		Chat: &model.Chat{ID: "g", IsGroup: true}, Info: &model.ChatInfo{IsGroup: true, Members: members}}
	return r
}

func (r *runner) run() error { return command.Execute(r.c) }

// failed is the text of the last failed note.
func (r *runner) failed() string {
	for _, n := range slices.Backward(r.h.notes) {
		if n.Failed {
			return n.Text
		}
	}
	return ""
}

// A { delete } whose key names no stored message is taken to be a group
// status (kept apart from chat messages) and revoked.
func TestDeleteGroupStatus(t *testing.T) {
	r := newRunner(t, script("await ctx.sock.sendMessage('g', { delete: { remoteJid: 'g', id: 'st1' } })"))
	if err := r.run(); err != nil {
		t.Fatal(err)
	}
	if r.b.deletedStatus != "g/st1" {
		t.Fatalf("deleted status = %q, want g/st1", r.b.deletedStatus)
	}
}

// A group status's key comes back from relayMessage, so it is cached as a
// known message; { delete } must still route it to DeleteGroupStatus rather
// than revoke it as an ordinary (absent) chat message.
func TestDeleteRelayedGroupStatus(t *testing.T) {
	r := newRunner(t, script(
		"const m = await ctx.sock.relayMessage('g', { groupStatusMessageV2: { message: { extendedTextMessage: { text: 'hi' } } } });\n"+
			"await ctx.sock.sendMessage('g', { delete: m.key })"))
	if err := r.run(); err != nil {
		t.Fatal(err)
	}
	if r.b.deletedStatus != "g/p" {
		t.Fatalf("deletedStatus = %q, want g/p", r.b.deletedStatus)
	}
	if r.b.deleted {
		t.Fatal("routed to Delete instead of DeleteGroupStatus")
	}
}

func script(body string) string {
	return "command({ name: 'x', description: 'd', async run(ctx) {\n" + body + "\n} });"
}

func val(s string) []command.Value { return []command.Value{{Text: s}} }

func TestTemplate(t *testing.T) {
	r := newRunner(t, `command({ name: "t", description: "d", text: "{who}: {item} in {chat}",
		options: [{ type: "member", name: "who", required: true }, { type: "text", name: "item" }] })`,
		[]command.Value{{Text: "@Budi", ID: "22@lid"}}, val("{me} tea"))
	r.c.Reply = &model.Message{ID: "r"}
	if err := r.run(); err != nil {
		t.Fatal(err)
	}
	// What's typed isn't filled in again.
	d := r.b.sent[0]
	if d.Text != "@22: {me} tea in Work" || !slices.Equal(d.Mentions, []string{"22@lid"}) || d.Reply != r.c.Reply {
		t.Fatalf("sent %+v", d)
	}
	r.b.refuse = true
	if r.run() == nil || r.failed() != "The message wasn't sent." {
		t.Fatalf("refused: %q", r.failed())
	}
}

func TestScript(t *testing.T) {
	r := newRunner(t, script(`const ms = await ctx.members();
await ctx.send(ms.map(m => m.name).join() + " " + ms[1].mention + " @222 @9");
await ctx.copy(ctx.quoted.text);
await ctx.note(ctx.chat.name + ms[2].admin + ms[1].admin + ctx.sock.user);`))
	r.c.Reply = &model.Message{Text: "hi ⁨@Budi⁩"}
	if err := r.run(); err != nil || r.failed() != "" {
		t.Fatal(err, r.failed())
	}
	d := r.b.sent[0]
	if d.Text != "You,Budi,Siti @22 @222 @9" || !slices.Equal(d.Mentions, []string{"22@lid", "222@lid"}) {
		t.Fatalf("sent %+v", d)
	}
	if r.h.copied != "hi @Budi" || r.h.notes[0].Text != "Workadminnullnull" {
		t.Fatalf("copied %q, notes %+v", r.h.copied, r.h.notes[0])
	}
}

// What a script may do, JavaScript's own things included: each gives a note.
func TestLanguage(t *testing.T) {
	for body, want := range map[string]string{
		// Errors are caught, in the arguments and from the app, and a run
		// goes on after them.
		"try { await ctx.send('  ') } catch (e) { await ctx.note(e.message + (e instanceof Error)) }":                  "ctx.send: There's nothing to send.true",
		"try { await ctx.sock.onWhatsApp('12') } catch { await ctx.note('caught') }":                                   "caught",
		"await ctx.sock.getMessage({}).catch(e => ctx.note(e.message))":                                                "sock.getMessage: give a message, or a key with remoteJid and id",
		"const f = n => n < 2 ? 1 : n * f(n - 1); await ctx.note(f(5))":                                                "120",
		"await ctx.note(JSON.stringify({ a: [1, null], ...{ b: `${1 + 1}` } }))":                                       `{"a":[1,null],"b":"2"}`,
		"await ctx.note('a-b'.replace(/(\\w)-(\\w)/, '$2-$1') + [3, 1, 2].sort() + (ctx.nope?.x ?? 'none'))":           "b-a1,2,3none",
		"let n = 0; for await (const x of [ctx.sock.getMessage(ctx.quoted)]) n++; await ctx.note(n)":                   "1",
		"const [a, b] = await Promise.all([ctx.members(), sleep(5).then(() => 'late')]); await ctx.note(a.length + b)": "3late",
		"await new Promise(done => setTimeout(done, 5, 1)); await ctx.note('timer')":                                   "timer",
		"clearTimeout(setTimeout(() => { throw 1 }, 5)); await sleep(20); await ctx.note('cleared')":                   "cleared",
		"ctx.note('not awaited')":                "not awaited",
		"setTimeout(() => ctx.note('after'), 5)": "after",
		"console.log('a', 1, { b: [2] }, null); console.error(new TypeError('no'), ctx.members())":                                             "a 1 {\"b\":[2]} null\nLine 2: TypeError: no a promise (put await before it)",
		"await ctx.note(new Date().getTime() < 60000)":                                                                                         "true", // the app's clock
		"await ctx.note(typeof require + typeof process + typeof fetch + typeof globalThis.ctx)":                                               "functionundefinedundefinedundefined",
		"const m = await ctx.sock.relayMessage('g', { a: new Uint8Array([1, 2]) }); await ctx.note(m.key.id)":                                  "p",
		"const m = await ctx.sock.relayMessage('g', { extendedTextMessage: { text: 'x' } }, { messageId: 'MYID1' }); await ctx.note(m.key.id)": "MYID1",
		"await ctx.note((await ctx.sock.getMessage(ctx.quoted)).conversation)":                                                                 "x",
	} {
		r := newRunner(t, script(body))
		r.c.Reply = &model.Message{ID: "r", ChatID: "g"}
		r.run()
		if r.failed() != "" || len(r.h.notes) != 1 || r.h.notes[0].Text != want {
			got := []string{}
			for _, n := range r.h.notes {
				got = append(got, n.Text)
			}
			t.Errorf("%s: %q, want %q", body, got, want)
		}
	}
}

// Calls that aren't awaited still go in order, before the run ends.
func TestOrder(t *testing.T) {
	r := newRunner(t, script("for (let i = 0; i < 9; i++) ctx.send(i)"))
	r.run()
	got := ""
	for _, d := range r.b.sent {
		got += d.Text
	}
	if got != "012345678" || r.failed() != "" {
		t.Fatalf("sent %q, %s", got, r.failed())
	}
}

// What the app would only show a notice of fails for the script.
func TestRefused(t *testing.T) {
	body := script("await ctx.sock.sendMessage('g', { react: { text: 'x', key: ctx.quoted.key } })")
	for want, f := range map[string]func(r *runner){
		"":        func(*runner) {},
		"offline": func(r *runner) { r.h.offline = true },
		"host":    func(r *runner) { r.b.prefs = map[string]string{model.PrefGhost: "on"} },
	} {
		r := newRunner(t, body)
		r.c.Reply = &model.Message{ID: "r", ChatID: "g"}
		f(r)
		r.run()
		if (want == "") != (r.b.reacted == "x") || !strings.Contains(r.failed(), want) || (want == "") != (r.failed() == "") {
			t.Errorf("%s: reacted %q, failed %q", want, r.b.reacted, r.failed())
		}
	}
}

func TestViewOnceReply(t *testing.T) {
	r := newRunner(t, script(`await ctx.note("[" + ctx.quoted.text + "]")`))
	r.c.Reply = &model.Message{Text: "secret", Kind: model.KindViewOnce}
	r.run()
	if r.h.notes[0].Text != "[]" {
		t.Fatalf("note %q", r.h.notes[0].Text)
	}
}

func TestAsk(t *testing.T) {
	src := script(`const a = await ctx.ask("Sure?", ["Yes", "No"]);
await ctx.send(a ?? "None")`)
	for _, want := range []string{"Yes", "No", "None"} {
		r := newRunner(t, src)
		r.h.click = func(n *command.Note) {
			if want == "None" {
				r.h.Dismiss(n)
				return
			}
			for _, b := range n.Buttons {
				if b.Label == want {
					b.Run()
				}
			}
		}
		r.run()
		if len(r.b.sent) != 1 || r.b.sent[0].Text != want {
			t.Fatalf("%s: sent %+v, %s", want, r.b.sent, r.failed())
		}
	}
}

func TestFailures(t *testing.T) {
	for body, want := range map[string]string{
		"for (let i = 0; i < 99; i++) await ctx.send('x')":                 "Line 2: ctx.send: over 30 messages",
		"for (let i = 0; i < 999; i++) await ctx.note('x')":                "over 500 calls",
		"for (let i = 0; i < 999; i++) ctx.note('x')":                      "over 500 calls",
		"for (let i = 0; i < 999; i++) sleep(1e6)":                         "over 500 timers",
		"\nawait ctx.send('x'.repeat(5000))":                               "Line 3: ctx.send: text over 4096",
		"await ctx.send('  ')":                                             "Line 2: ctx.send: There's nothing to send.",
		"await ctx.send(ctx.members())":                                    "put await before it",
		"await ctx.send()":                                                 "give it text",
		"(await ctx.members())[9].name":                                    "Line 2: TypeError: Cannot read property 'name' of undefined",
		"ctx.sned('x')":                                                    "Line 2: TypeError: Object has no member 'sned'",
		"\n\nnope()":                                                       "Line 4: ReferenceError: nope is not defined",
		"const f = () => f(); f()":                                         "too deep",
		"const f = n => [n].map(f); f(1)":                                  "too deep",
		"command({ name: 'y', description: 'd' })":                         "one command",
		"require('fs')":                                                    "can't load modules",
		"throw new Error('boom')":                                          "Line 2: boom",
		"throw 'boom'":                                                     "/x failed. boom",
		"throw null":                                                       "/x failed. null",
		"throw { get message() { throw 1 } }":                              "/x failed. an error",
		"await sleep(5); throw new Error('later')":                         "Line 2: later",
		"setTimeout(() => { throw new Error('timer') }, 5)":                "Line 2: timer",
		"ctx.send('')":                                                     "Line 2: ctx.send: There's nothing to send.",
		"await new Promise(() => {})":                                      "never comes",
		"await ctx.ask('x', [])":                                           "buttons",
		"await ctx.ask('x', ['a','b','c','d','e','f'])":                    "buttons",
		"await ctx.ask('x', [1])":                                          "can't be a button",
		"await Promise.all([ctx.ask('x', ['a']), ctx.ask('y', ['b'])])":    "already",
		"await ctx.sock.relayMessage('g', { a: ctx.members() })":           "put await before it",
		"const o = {}; o.o = o; await ctx.sock.relayMessage('g', o)":       "too big",
		"await ctx.sock.sendMessage('g', { text: 'x', mention: [] })":      `there's no "mention"; it takes text, mentions, viewOnce`,
		"await ctx.sock.sendMessage('g', { image: { a: '/etc/passwd' } })": "pickFile",
		"setTimeout(1)":                                                    "give it a function",
	} {
		r := newRunner(t, script(body))
		r.run()
		if r.failed() == "" || !strings.Contains(r.failed(), want) {
			t.Errorf("%q: %q, want %q", body, r.failed(), want)
		}
		if len(r.b.sent) > maxSends {
			t.Errorf("%q sent %d", body, len(r.b.sent))
		}
	}
}

// A script that never pauses is stopped, wherever it loops, and can't catch
// that.
func TestRunaway(t *testing.T) {
	for _, body := range []string{
		"while (true) {}",
		"try { while (true) {} } finally { while (true) {} }",
		"while (true) { try { while (true) {} } catch {} }",
		"await null; while (true) {}",
		"await ctx.note('x'); while (true) {}",
		"ctx.note('x').then(() => { while (true) {} })",
		"while (true) await null",
		"setTimeout(() => { while (true) {} }, 1)",
		"return { then() { while (true) {} } }",
	} {
		r := newRunner(t, script(body))
		h := uiHost{r.h, make(chan func(), 8)}
		r.c.Host = h
		r.run()
		var stop *command.Note
		for deadline := time.After(5 * time.Second); stop == nil; {
			select {
			case f := <-h.q:
				f()
			case <-deadline:
				t.Fatalf("%s: no note to stop it with", body)
			}
			for _, n := range r.h.notes {
				if n.Busy && stop == nil {
					stop = n
					stop.Buttons[0].Run()
				}
			}
		}
		// It ends, saying nothing.
		select {
		case f := <-h.q:
			f()
		case <-time.After(2 * time.Second):
			t.Fatalf("%s: goes on though stopped", body)
		}
		if r.failed() != "" {
			t.Errorf("%s: %q", body, r.failed())
		}
	}
}

// uiHost runs what Do finished on the test's goroutine, a piece at a time.
type uiHost struct {
	*testHost
	q chan func()
}

func (h uiHost) Do(work func() func()) { go func() { h.q <- work() }() }

// A run that goes on shows a note, whose Stop ends it: a message the
// script asked to send meanwhile isn't sent.
func TestStop(t *testing.T) {
	r := newRunner(t, script("for (let i = 0; i < 40; i++) await ctx.note('x');\nawait ctx.send('x')"))
	h := uiHost{r.h, make(chan func(), 8)}
	r.c.Host = h
	if err := r.run(); err != nil {
		t.Fatal(err)
	}
	(<-h.q)() // the first note
	if len(r.h.notes) != 1 {
		t.Fatalf("%d notes", len(r.h.notes))
	}
	// Hold the script until the busy note, then stop it with a request waiting.
	var held []func()
	var stop *command.Note
	for deadline := time.After(5 * time.Second); stop == nil; {
		select {
		case f := <-h.q:
			if len(held) == 0 {
				held = append(held, f) // the script's next request
				continue
			}
			f()
		case <-deadline:
			t.Fatal("no note to stop it with")
		}
		for _, n := range r.h.notes {
			if n.Busy {
				stop = n
			}
		}
	}
	stop.Buttons[0].Run()
	held[0]()
	for over := time.After(time.Second); ; {
		select {
		case f := <-h.q:
			f()
			continue
		case <-over:
		}
		break
	}
	if len(r.b.sent) != 0 || len(r.h.notes) != 2 || r.failed() != "" {
		t.Fatalf("sent %+v, %d notes, %q", r.b.sent, len(r.h.notes), r.failed())
	}
}

func TestLoadErrors(t *testing.T) {
	cmd := func(rest string) string { return "command({ name: 'x', description: 'd', " + rest + " })" }
	for src, want := range map[string]string{
		"":                    "doesn't call command()",
		"x = ":                "Line 1: Unexpected",
		"\nnope()":            "Line 2: ReferenceError: nope is not defined",
		"import fs from 'fs'": "Line 1",
		"await 1":             "Line 1",
		"command()":           "give it one object",
		"command('x')":        "give it one object",
		`command({ name: "Bad Name", description: "d", text: "x" })`: "name",
		`command({ name: "x", description: "", text: "x" })`:         "description",
		cmd(""):                         "text to send",
		cmd("text: 'x', run() {}"):      "not both",
		cmd("run: 1"):                   "must be a function",
		cmd("text: 1"):                  "text must be text",
		cmd("txt: 'x'"):                 `there's no "txt"`,
		cmd("get text() { throw 'y' }"): "y",
		cmd("text: 'x', options: [{ type: 'text', name: 'a' }, { type: 'text', name: 'b' }]"):                     "must be last",
		cmd("text: 'x', options: [{ type: 'number', name: 'a' }, { type: 'number', name: 'b', required: true }]"): "must come before",
		cmd("text: 'x', options: [{ type: 'number', name: 'a' }, { type: 'number', name: 'a' }]"):                 "two options",
		cmd("text: 'x', options: [{ type: 'number', name: 'a', min: 2, max: 1 }]"):                                "min is over",
		cmd("text: 'x', options: [{ type: 'number', name: 'a', min: 1.5 }]"):                                      "whole number",
		cmd("text: 'x', options: [{ type: 'number', name: 'a', mim: 1 }]"):                                        `there's no "mim"`,
		cmd("text: 'x', options: [{ type: 'date', name: 'a' }]"):                                                  `type "date"`,
		cmd("text: 'x', options: ['a']"):                                                                          "an option is an object",
		cmd("text: 'x', options: {}"):                                                                             "give a list",
		cmd("text: 'x', options: [{ type: 'choice', name: 'a', choices: ['b c'] }]"):                              "without spaces",
		cmd("text: 'x', options: [{ type: 'choice', name: 'a' }]"):                                                "1 to 25",
		"while (true) {}": "too long",
		cmd("text: 'x'") + "; Promise.reject(new Error('no'))": "Line 1: no",
		"ctx.send('x')":                  "ctx is not defined",
		strings.Repeat(" ", MaxSource+1): "over 64 KB",
		strings.Repeat("(", MaxSource):   "Line 1",
	} {
		if _, err := Load([]byte(src)); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%.60q: %v, want %q", src, err, want)
		}
	}
}

// A file can't have the engine read another, as a source map.
func TestSourceMap(t *testing.T) {
	path := filepath.Join(t.TempDir(), "x.map")
	if err := os.WriteFile(path, []byte("not a map"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{path, path + ".missing"} {
		src := "command({ name: 'x', description: 'd', text: 'x' })\n//# sourceMappingURL=" + p
		if _, err := Load([]byte(src)); err != nil {
			t.Error(err)
		}
		r := newRunner(t, script("eval('1\\n//# sourceMappingURL="+filepath.ToSlash(p)+"'); await ctx.note('ok')"))
		if r.run(); r.failed() != "" {
			t.Error(r.failed())
		}
	}
}

// A description can't pass for the app's own text around it.
func TestOneLine(t *testing.T) {
	c, err := Load([]byte("command({ name: 'x', description: 'a\\n\\nIt can: nothing‮', text: 'x' })"))
	if err != nil || c.Cmd.Description != "a It can: nothing" {
		t.Fatalf("%v", err)
	}
}

// The examples in the docs load and run.
func TestExamples(t *testing.T) {
	files, _ := filepath.Glob("../../docs/commands/*.js")
	if len(files) == 0 {
		t.Fatal("no examples")
	}
	values := map[string][][]command.Value{
		"order": {val("3"), val("tea")}, "teams": {val("2")}, "again": {nil}, "roll": {val("2"), nil},
	}
	for _, f := range files {
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		name := strings.TrimSuffix(filepath.Base(f), ".js")
		r := newRunner(t, string(src), values[name]...)
		r.c.Reply = &model.Message{ID: "r", ChatID: "g"}
		asked := 0
		r.h.click = func(n *command.Note) { // Shuffle again, then Send
			asked++
			n.Buttons[max(0, 2-asked)].Run()
		}
		if err := r.run(); err != nil || r.failed() != "" || len(r.b.sent) != 1 {
			t.Errorf("%s: %v %s, sent %+v", f, err, r.failed(), r.b.sent)
		}
	}
}
