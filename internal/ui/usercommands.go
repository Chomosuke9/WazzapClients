package ui

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"slices"

	"github.com/chomosuke9/wazzapclients/internal/command"
	"github.com/chomosuke9/wazzapclients/internal/filepick"
	"github.com/chomosuke9/wazzapclients/internal/usercmd"
)

// The commands the user added from files (internal/usercmd), on their own
// page of the Extra features. They may use the whole account. A command's
// source is kept as it was then, so changing the file changes nothing
// until it's added again.

// prefUserCommands is an app preference (appPrefs): the commands, as JSON.
const prefUserCommands = "user_commands"

// userCommand is an added command. cmd is nil when its source can't be
// read (any more), and err says why.
type userCommand struct {
	Name   string `json:"name"`
	Source string `json:"source"`
	Off    bool   `json:"off,omitempty"`

	cmd *usercmd.Command
	err error
}

func (c *userCommand) load() {
	c.cmd, c.err = usercmd.Load([]byte(c.Source))
	if c.err == nil && command.BuiltIn(c.Name) != nil {
		c.cmd, c.err = nil, errTakenCommand(c.Name) // a newer version's
	}
}

func errTakenCommand(name string) error {
	return errors.New("The app has a command called /" + name + ".")
}

// loadUserCommands reads the added commands and puts them in the picker.
func (u *UI) loadUserCommands() {
	u.userCmds = nil
	if u.backend != nil {
		if s := u.backend.Pref(prefUserCommands); s != "" {
			_ = json.Unmarshal([]byte(s), &u.userCmds)
		}
	}
	u.userCmds = slices.DeleteFunc(u.userCmds, func(c *userCommand) bool { return c == nil })
	for _, c := range u.userCmds {
		c.load()
	}
	u.useUserCommands()
}

// useUserCommands puts the added commands that are on in the picker.
func (u *UI) useUserCommands() {
	var cmds []*command.Command
	for _, c := range u.userCmds {
		if c.cmd != nil && !c.Off {
			cmds = append(cmds, c.cmd.Cmd)
		}
	}
	command.SetUser(cmds)
	u.slash.cacheOK = false
	u.conv.richFor = ""
}

// userCommandsChanged keeps the added commands.
func (u *UI) userCommandsChanged() {
	if b, err := json.Marshal(u.userCmds); err == nil {
		u.backend.SetPref(prefUserCommands, string(b))
	}
	u.useUserCommands()
	u.settings.stale = true
}

// pickUserCommand asks for a command's file and offers to add it.
func (u *UI) pickUserCommand() {
	u.slashDo(func() func() {
		paths, err := filepick.Open("Choose a command's file", false, filepick.Filter{Name: "Commands", Exts: []string{"js"}})
		var src []byte
		if err == nil && len(paths) > 0 {
			src, err = readCommand(paths[0])
		}
		var cmd *usercmd.Command
		if err == nil && src != nil {
			cmd, err = usercmd.Load(src)
		}
		return func() {
			u.settings.stale = true
			switch {
			case err != nil:
				u.userCmdNote = "The command wasn't added. " + err.Error()
			case cmd != nil:
				u.offerUserCommand(cmd)
			}
		}
	})
}

// readCommand reads a command's file, at most a byte more than one may be.
func readCommand(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, usercmd.MaxSource+1))
	if b == nil {
		b = []byte{}
	}
	return b, err
}

// offerUserCommand asks before adding a command.
func (u *UI) offerUserCommand(cmd *usercmd.Command) {
	name := cmd.Cmd.Name
	if command.BuiltIn(name) != nil {
		u.userCmdNote = "The command wasn't added. " + errTakenCommand(name).Error()
		return
	}
	u.userCmdNote = ""
	i := slices.IndexFunc(u.userCmds, func(c *userCommand) bool { return c.Name == name })
	title, action := "Add /"+name+"?", "Add"
	if i >= 0 {
		title, action = "Replace /"+name+"?", "Replace"
	}
	u.confirm(title, userCommandDetails(cmd)+"\n\n"+
		"A command can do anything with your account. Look inside its file first: what it does is your responsibility.",
		dialogButton{label: action, primary: true, run: func() {
			c := &userCommand{Name: name, Source: cmd.Source, cmd: cmd}
			if i >= 0 {
				u.userCmds[i] = c
			} else {
				u.userCmds = append(u.userCmds, c)
			}
			u.userCommandsChanged()
			u.toast("/" + name + " added")
		}})
}

// userCommandDetails is a command's usage and what it says it does.
func userCommandDetails(cmd *usercmd.Command) string {
	return cmd.Cmd.Usage() + "\n" + cmd.Cmd.Description
}

// showUserCommand is an added command's dialog.
func (u *UI) showUserCommand(c *userCommand) {
	remove := dialogButton{label: "Remove", danger: true, run: func() {
		u.userCmds = slices.DeleteFunc(u.userCmds, func(o *userCommand) bool { return o == c })
		u.userCommandsChanged()
	}}
	if c.cmd == nil {
		u.confirm("/"+c.Name, c.err.Error(), remove)
		return
	}
	label := "Turn off"
	if c.Off {
		label = "Turn on"
	}
	u.confirm("/"+c.Name, userCommandDetails(c.cmd), dialogButton{label: label, primary: true, run: func() {
		c.Off = !c.Off
		u.userCommandsChanged()
	}}, remove)
}

// userCommandSettings is the Your commands page.
func (u *UI) userCommandSettings() []settingsSection {
	secs := []settingsSection{{rows: []settingRow{{key: "usercmd:add", ic: icAdd, title: "Add a command",
		sub: "From a JavaScript file (.js)", run: u.pickUserCommand}}, note: u.userCmdNote}}
	if len(u.userCmds) > 0 {
		sec := settingsSection{title: "Commands"}
		for _, c := range u.userCmds {
			sub := ""
			switch {
			case c.cmd == nil:
				sub = "Doesn't work: " + c.err.Error()
			case c.Off:
				sub = "Off · " + c.cmd.Cmd.Description
			default:
				sub = c.cmd.Cmd.Description
			}
			sec.rows = append(sec.rows, settingRow{key: "usercmd:" + c.Name, ic: icTerminal, title: "/" + c.Name, sub: sub,
				run: func() { u.showUserCommand(c) }})
		}
		secs = append(secs, sec)
	}
	note := "Commands run on this computer, from your account, and can do anything with it. Look inside a file before you add it."
	if !u.slash.on {
		note += " They need Slash commands, on the Extra features page."
	}
	return append(secs, settingsSection{note: note})
}

// showUserCommandPreview shows made-up commands for cmd/screenshot: the
// page, the dialog that adds one, or the picker with them.
func (u *UI) showUserCommandPreview(name string) {
	for _, src := range []string{
		`command({name: "order", description: "Sends an order", options: [{type: "number", name: "qty", required: true}, {type: "text", name: "item", required: true}], text: "{qty} x {item}"})`,
		`command({name: "teams", description: "Splits the group into random teams", options: [{type: "number", name: "count", required: true}], group: true, run(ctx) {}})`,
	} {
		cmd, err := usercmd.Load([]byte(src))
		if err != nil {
			continue
		}
		u.userCmds = append(u.userCmds, &userCommand{Name: cmd.Cmd.Name, Source: src, cmd: cmd})
	}
	u.slash.on = true
	u.useUserCommands()
	if name == "usercmdpick" {
		ed := &u.conv.composer
		ed.SetText("/order 3 ")
		ed.SetCaret(ed.Len(), ed.Len())
		return
	}
	u.ShowPage("commands")
	u.settings.stale = true
	if name == "usercmdadd" {
		u.offerUserCommand(u.userCmds[1].cmd)
	}
}
