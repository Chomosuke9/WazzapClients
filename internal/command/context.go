package command

import (
	"strconv"
	"time"

	"github.com/chomosuke9/wazzapclients/internal/auto"
	"github.com/chomosuke9/wazzapclients/internal/model"
)

// Note is a message in the chat that only you see, like Discord's
// ephemeral replies: what a command did, or why it couldn't. A command
// keeps it and changes it as its work goes on; the UI draws it every
// frame as it is then.
type Note struct {
	// Title is the command as you ran it.
	Title string
	Text  string
	// Busy shows the command is still working.
	Busy bool
	// Failed shows Text as an error.
	Failed  bool
	Buttons []Button
}

// Button is a button under a Note.
type Button struct {
	Label  string
	Danger bool
	Run    func()
}

// Host is what a command needs from the UI. Its methods are called, and
// call back, on the UI goroutine.
type Host interface {
	// Note shows n at the end of the chat, and Dismiss removes it.
	Note(n *Note)
	Dismiss(n *Note)
	// Group sends a request to change the group and calls done with the
	// answer.
	Group(r model.GroupRequest, done func(model.GroupEvent))
	// Do runs work on another goroutine, then the function it returns
	// on the UI goroutine.
	Do(work func() func())
	// Copy puts text on the clipboard.
	Copy(text string)
	// Confirm asks before running something that can't be undone.
	Confirm(title, body, action string, danger bool, run func())
	// PickImage asks for a picture, and calls done with its path ("" when
	// cancelled).
	PickImage(done func(path string))
	// Sent shows a message you just sent.
	Sent(m *model.Message)
	// Draft makes a message of text typed in the composer, turning the
	// @mentions picked in it into the protocol's.
	Draft(text string) model.Draft
	// SetGhost turns ghost mode (model.PrefGhost) on or off.
	SetGhost(on bool)
	// SnippetVars are the values of a snippet's {variables} sent in chat
	// as a reply to reply (nil for none).
	SnippetVars(chatID string, reply *model.Message) map[string]string
	// SnippetsChanged drops what the UI knows of the saved snippets.
	SnippetsChanged()
	// EditSnippet opens a snippet in Settings > Snippets.
	EditSnippet(id int64)
	// ShowPayload shows a message's payload (/catch).
	ShowPayload(chatID, messageID string)
}

// Context is everything a running command may use.
type Context struct {
	Cmd *Command
	// Input is the command as typed, e.g. "/kick @Budi".
	Input string
	// Values holds the values of each of Cmd's options.
	Values [][]Value
	Chat   *model.Chat
	// Info is the chat's details (members and settings), or nil while
	// they aren't known.
	Info *model.ChatInfo
	// Reply is the message the composer was replying to, or nil.
	Reply   *model.Message
	Backend model.Backend
	// Now is when the command runs.
	Now time.Time
	// Auto sends scheduled messages and AFK replies; nil without it.
	Auto *auto.Backend
	Host
}

// Get returns the values of the option called name.
func (c *Context) Get(name string) []Value {
	for i, o := range c.Cmd.Options {
		if o.Name == name {
			return c.Values[i]
		}
	}
	return nil
}

// Text returns the first value of option name as typed, or "".
func (c *Context) Text(name string) string {
	if vs := c.Get(name); len(vs) > 0 {
		return vs[0].Text
	}
	return ""
}

// Int returns option name's number, or def when it has none.
func (c *Context) Int(name string, def int) int {
	if n, err := strconv.Atoi(c.Text(name)); err == nil {
		return n
	}
	return def
}

// IDs returns the members or contacts named by option name: their IDs, or
// the phone numbers typed.
func (c *Context) IDs(name string) []string {
	var out []string
	for _, v := range c.Get(name) {
		if v.ID != "" {
			out = append(out, v.ID)
		} else {
			out = append(out, v.Text)
		}
	}
	return out
}

// Fail shows that the command couldn't run, and why.
func (c *Context) Fail(text string) {
	c.Note(&Note{Title: c.Input, Text: text, Failed: true})
}

// Execute runs c's command, after checking that it can run here.
func Execute(c *Context) {
	switch {
	case c.Cmd.Group && (c.Chat == nil || !c.Chat.IsGroup):
		c.Fail("/" + c.Cmd.Name + " works only in groups.")
		return
	case c.Cmd.Admin && c.Info != nil && !isAdmin(c.Info):
		c.Fail("Only group admins can use /" + c.Cmd.Name + ".")
		return
	}
	if err := c.Cmd.Run(c); err != nil {
		c.Fail(err.Error())
	}
}

func isAdmin(info *model.ChatInfo) bool {
	for _, m := range info.Members {
		if m.Me {
			return m.Admin
		}
	}
	return false
}
