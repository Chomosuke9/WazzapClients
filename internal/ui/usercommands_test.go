package ui

import (
	"slices"
	"strings"
	"testing"
	"time"

	"gioui.org/io/key"

	"github.com/chomosuke9/wazzapclients/internal/command"
	"github.com/chomosuke9/wazzapclients/internal/model"
	"github.com/chomosuke9/wazzapclients/internal/usercmd"
)

const testUserCommand = `
command({
  name: "hello",
  description: "Says hello",
  options: [{ type: "text", name: "who", required: true }],
  async run(ctx) {
    if (await ctx.ask("Say hello?", ["Yes"]) === "Yes") await ctx.send("Hello, " + ctx.args.who + "!");
  },
});
`

func (st *slashTest) offerCommand(src string) {
	st.t.Helper()
	cmd, err := usercmd.Load([]byte(src))
	if err != nil {
		st.t.Fatal(err)
	}
	st.u.offerUserCommand(cmd)
	st.frame()
}

// until draws frames until ok, as scripts run on goroutines of their own.
func (st *slashTest) until(what string, ok func() bool) {
	st.t.Helper()
	for range 500 {
		if ok() {
			return
		}
		time.Sleep(2 * time.Millisecond)
		st.frame()
	}
	st.t.Fatal("never: " + what)
}

func TestUserCommand(t *testing.T) {
	t.Cleanup(func() { command.SetUser(nil) })
	st := newSlashTest(t, "rina")
	st.u.ShowPage("commands")
	st.frame()

	// Cancelling adds nothing.
	st.offerCommand(testUserCommand)
	if d := &st.u.dialog; !d.isOpen() || !strings.Contains(d.body, "Look inside") {
		t.Fatalf("the dialog doesn't warn: %q", d.body)
	}
	if command.Lookup("hello") != nil {
		t.Fatal("added before it was approved")
	}
	st.u.btn("dialog:2").Click()
	st.frame()
	if len(st.u.userCmds) != 0 || st.b.Pref(prefUserCommands) != "" {
		t.Fatal("added though cancelled")
	}

	st.offerCommand(testUserCommand)
	st.u.btn("dialog:1").Click()
	st.frame()
	if command.Lookup("hello") == nil || !slices.Contains(appPrefs, prefUserCommands) {
		t.Fatal("not added")
	}
	settingRowByKey(t, st.u, "usercmd:hello")

	// It's in the picker of a new window, and runs.
	command.SetUser(nil)
	st.u.loadUserCommands()
	st.u.ShowPage("chats")
	st.u.SelectID("rina")
	for range 30 {
		st.frame() // the dialog fades out
	}
	st.typeText("/hel")
	if sp := st.u.slashQuery(); sp == nil || len(sp.cmds) != 1 || sp.cmds[0].Name != "hello" {
		t.Fatalf("not in the picker: %+v", sp)
	}
	st.typeText("/hello Rina")
	st.press(key.NameReturn)
	st.until("asked", func() bool { return len(st.u.slash.notes["rina"]) == 1 })
	st.u.slash.notes["rina"][0].note.Buttons[0].Run()
	sent := func() bool {
		return slices.ContainsFunc(st.b.Messages("rina", 1000), func(m *model.Message) bool { return m.FromMe && m.Text == "Hello, Rina!" })
	}
	st.until("sent", sent)
	if n := len(st.u.slash.notes["rina"]); n != 0 {
		t.Fatalf("%d notes left", n)
	}

	// Off and removed, it's just text.
	c := st.u.userCmds[0]
	for _, b := range []string{"dialog:1", "dialog:2"} {
		st.u.showUserCommand(c)
		st.frame()
		st.u.btn(b).Click()
		st.frame()
		if command.Lookup("hello") != nil {
			t.Fatal(b + ": still in the picker")
		}
	}
	if len(st.u.userCmds) != 0 || st.b.Pref(prefUserCommands) != "[]" {
		t.Fatalf("not removed: %s", st.b.Pref(prefUserCommands))
	}
}

// A file can't take one of the app's own commands' names, and a kept one
// that no longer loads is listed, to be removed.
func TestUserCommandConflicts(t *testing.T) {
	t.Cleanup(func() { command.SetUser(nil) })
	st := newSlashTest(t, "rina")
	st.offerCommand(`command({ name: "kick", description: "d", text: "x" })`)
	if st.u.dialog.isOpen() || !strings.Contains(st.u.userCmdNote, "/kick") {
		t.Fatalf("offered /kick: %q", st.u.userCmdNote)
	}
	st.b.SetPref(prefUserCommands, `[null,{"name":"old","source":"nope("},{"name":"kick","source":"command({ name: 'kick', description: 'd', text: 'x' })"}]`)
	st.u.loadUserCommands()
	st.u.ShowPage("commands")
	st.frame()
	if r := settingRowByKey(t, st.u, "usercmd:old"); !strings.Contains(r.sub, "Doesn't work") {
		t.Fatalf("row %q", r.sub)
	}
	if command.Lookup("old") != nil || !command.Lookup("kick").Admin {
		t.Fatal("a broken or taken command is in use")
	}
	st.u.showUserCommand(st.u.userCmds[0])
	st.frame()
	st.b.SetPref(prefUserCommands, "not JSON")
	st.u.loadUserCommands()
}

// runCommand adds src and runs it in chat until it ends.
func (st *slashTest) runCommand(chat, src string) (failed string) {
	st.t.Helper()
	cmd, err := usercmd.Load([]byte(src))
	if err != nil {
		st.t.Fatal(err)
	}
	st.u.userCmds = []*userCommand{{Name: cmd.Cmd.Name, Source: src, cmd: cmd}}
	st.u.useUserCommands()
	st.u.SelectID(chat)
	st.frame()
	clear(st.u.slash.notes)
	st.typeText("/" + cmd.Cmd.Name + " ")
	st.press(key.NameReturn)
	st.until("end of "+cmd.Cmd.Name, func() bool {
		st.u.applyEvents()
		ns := st.u.slash.notes[chat]
		return len(ns) > 0 && (ns[len(ns)-1].note.Failed || ns[len(ns)-1].note.Text == "done")
	})
	for _, n := range st.u.slash.notes[chat] {
		if n.note.Failed {
			failed = n.note.Text
		}
	}
	return failed
}

// sockScript is a command that runs body with sock.
func sockScript(body string) string {
	return "command({ name: 'sock', description: 'd', async run(ctx) {\n const { sock } = ctx;\n" + body + "\nawait ctx.note('done');\n} });"
}

func TestUserCommandSock(t *testing.T) {
	t.Cleanup(func() { command.SetUser(nil) })
	st := newSlashTest(t, "work")
	if f := st.runCommand("work", sockScript(`
    const check = (ok, what) => { if (!ok) throw new Error(what); };
    check(sock.user.id !== "", "user");
    const chats = await sock.chats();
    check(chats.some(c => c.id === "rina"), "chats");
    check((await sock.contacts()).length > 0, "contacts");
    // Another chat than this one, a mention, a quote.
    const m = await sock.sendMessage("rina", { text: "one" });
    check(m.key.remoteJid === "rina" && m.fromMe && m.text === "one", "sent");
    const q = await sock.sendMessage("rina", { text: "two" }, { quoted: m });
    const last = await sock.fetchMessages("rina", 2);
    check(last.map(x => x.text).join() === "one,two", "fetched");
    // Whatever is sent has a key, as with Baileys, and something sent anew a new one.
    const ids = [m.key.id, q.key.id];
    const fresh = async (p, what) => {
      const x = await p;
      check(x.key.id && !ids.includes(x.key.id) && x.key.remoteJid === "rina" && x.key.fromMe, what);
      ids.push(x.key.id);
      return x;
    };
    const same = async (p, y, what) => check((await p).key.id === y.key.id, what);
    await same(sock.sendMessage("rina", { react: { text: "🔥", key: m.key } }), m, "react");
    await same(sock.sendMessage("rina", { edit: m.key, text: "one!" }), m, "edit");
    await same(sock.sendMessage("rina", { pin: m.key }), m, "pin");
    await same(sock.sendMessage("rina", { pin: { key: m.key, type: 2 } }), m, "unpin");
    await same(sock.sendMessage("rina", { pin: m.key, type: 1, time: 86400 }), m, "pin again");
    await same(sock.sendMessage("rina", { delete: { remoteJid: "rina", id: q.key.id } }), q, "delete");
    await fresh(sock.sendMessage("rina", { forward: last[0] }), "forward");
    await fresh(sock.sendMessage("rina", { poll: { name: "Lunch?", values: ["Soto", "Bakso"], selectableCount: 1 } }), "poll");
    await fresh(sock.sendMessage("rina", { location: { degreesLatitude: -6.2, degreesLongitude: 106.8 } }), "location");
    await fresh(sock.sendMessage("rina", { contacts: ["clara"] }), "contacts");
    await fresh(sock.relayMessage("rina", { extendedTextMessage: { text: "raw" } }, { quoted: m }), "relay");
    // An album's header has no bubble, but its pictures point to its key.
    const album = await fresh(sock.relayMessage("rina", { albumMessage: { expectedImageCount: 2 } }), "album");
    await fresh(sock.relayMessage("rina", {
      extendedTextMessage: { text: "in the album" },
      messageContextInfo: { messageAssociation: { associationType: 1, parentMessageKey: album.key } },
    }), "in an album");
    check((await ctx.send("here")).key.remoteJid === ctx.chat.id, "ctx.send");
    let sticker;
    for (const c of chats) sticker ??= (await sock.fetchMessages(c.id, 500)).find(x => x.media === "sticker");
    check(sticker, "a demo sticker");
    await fresh(sock.sendMessage("rina", { sticker }), "sticker");
    check(typeof await sock.getMessage(m) === "object", "getMessage");
    await sock.star("rina", [m.key], true);
    await sock.chatModify({ star: { messages: [{ id: m.key.id }], star: false } }, "rina");
    await sock.chatModify({ star: { messages: [{ id: m.key.id }], star: true } }, "rina");
    await sock.chatModify({ archive: true, mute: 3600000, lastMessages: [] }, "rina");
    await sock.readMessages([m.key, "clara"]);
    const g = await sock.groupMetadata(ctx.chat.id);
    check(g.participants.length > 1 && g.size === g.participants.length, "participants");
    await sock.groupUpdateSubject(ctx.chat.id, "Renamed");
    await sock.groupSettingUpdate(ctx.chat.id, "announcement");
    check(await sock.groupInviteCode(ctx.chat.id), "invite");
    const who = g.participants.find(p => !p.me && !p.admin);
    const [res] = await sock.groupParticipantsUpdate(ctx.chat.id, [who.id], "promote");
    check(res.error === "" && res.status === "200" && res.jid === who.id, "promoted");
    const [a, b] = await sock.onWhatsApp("+62 812 5550 1234", "6281255");
    check(a.exists && a.jid === "6281255501234@s.whatsapp.net", "onWhatsApp");
    check(!b.exists && b.jid === null, "not on WhatsApp");
`)); f != "" {
		t.Fatal(f)
	}
	var edited, deleted, reacted, poll, raw bool
	for _, m := range st.b.Messages("rina", 1000) {
		edited = edited || m.Text == "one!" && !m.Edited.IsZero()
		reacted = reacted || m.MyReaction == "🔥" && m.Starred
		deleted = deleted || m.FromMe && m.Kind == model.KindDeleted
		poll = poll || m.Poll != nil && m.Text == "Lunch?" && len(m.Poll.Options) == 2
		raw = raw || m.Text == "raw" && m.Quote != nil
	}
	if !edited || !deleted || !reacted || !poll || !raw {
		t.Errorf("edited %v deleted %v reacted %v poll %v raw %v", edited, deleted, reacted, poll, raw)
	}
	for _, c := range st.b.Chats() {
		if c.ID == "rina" && (!c.Archived || !c.Muted) {
			t.Error("rina isn't archived and muted")
		}
	}
	if info := st.b.Info("work"); !info.Announce || info.Name != "Renamed" {
		t.Errorf("group %+v", info)
	}

	for body, want := range map[string]string{
		`sock.nope()`:                        "no member 'nope'",
		`await sock.sendMessage("rina", {})`: "needs one of",
		`await sock.sendMessage("rina", { text: "a", poll: {} })`:                                         "both",
		`await sock.sendMessage("rina", { text: " " })`:                                                   "nothing to send",
		`await sock.sendMessage("rina", { image: "/etc/passwd" })`:                                        "pickFile",
		`await sock.sendMessage("rina", { delete: { remoteJid: "rina", id: "no" } })`:                     "isn't stored",
		`await sock.sendMessage("rina", { react: "x" })`:                                                  "react",
		`await sock.sendMessage("rina", { poll: { name: "x", values: ["1"] } })`:                          "poll",
		`await sock.sendMessage("rina", { text: "x", mentions: [1] })`:                                    "mentions",
		`await sock.sendMessage("rina", { text: "x" }, { quoted: "y" })`:                                  "quoted",
		`await sock.sendMessage("rina", { text: "x" }, { ephemeral: 1 })`:                                 `no "ephemeral"`,
		`await sock.sendMessage("rina", { pin: { remoteJid: "rina", id: "no" } })`:                        "isn't stored",
		`await sock.sendMessage({}, { text: "x" })`:                                                       "give a jid",
		`let x = [1]; for (let i = 0; i < 40; i++) x = [x, x]; await sock.relayMessage("rina", { a: x })`: "too big",
		`let x = [1]; for (let i = 0; i < 99; i++) x = [x]; await sock.relayMessage("rina", { a: x })`:    "too big",
		`await sock.relayMessage("rina", { a: () => 1 })`:                                                 "can't hold",
		`for (let i = 0; i < 99; i++) await sock.sendMessage("rina", { text: "spam" })`:                   "over 30 messages",
		`await sock.groupUpdateSubject("rina", "x")`:                                                      "group",
		`await sock.groupSettingUpdate("work", "x")`:                                                      "setting",
		`await sock.chatModify({ wipe: true }, "rina")`:                                                   `no "wipe"`,
		`await sock.onWhatsApp("12")`:                                                                     "phone number",
		`await sock.readMessages("rina")`:                                                                 "give a list",
	} {
		if f := st.runCommand("work", sockScript(body)); !strings.Contains(f, want) || !strings.Contains(f, "Line 3") {
			t.Errorf("%s: %q, want %q", body, f, want)
		}
	}

	st.u.conn.State = model.StateOffline
	for _, body := range []string{
		`await sock.updateBlockStatus("rina", "block")`, `await sock.groupLeave("work")`, `await sock.star("rina", [], true)`,
		`await sock.chatModify({ archive: false }, "rina")`,
	} {
		if f := st.runCommand("work", sockScript(body)); !strings.Contains(f, "offline") {
			t.Errorf("offline: %s: %q", body, f)
		}
	}
}
