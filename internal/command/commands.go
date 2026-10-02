package command

import (
	"errors"
	"os"
	"strings"

	"github.com/chomosuke9/wazzapclients/internal/model"
	"github.com/chomosuke9/wazzapclients/internal/sticker"
)

// All lists the commands, in the order the picker shows them.
var All = []*Command{
	{
		Name: "add", Description: "Adds people to the group", Group: true, Admin: true,
		Options: []Option{{Name: "contact", Description: "Contacts, or phone numbers with their country code",
			Kind: Contact, Required: true, Multiple: true}},
		Run: func(c *Context) error { return changeMembers(c, model.GroupAdd, "contact") },
	},
	{
		Name: "kick", Description: "Removes members from the group", Group: true, Admin: true,
		Options: []Option{{Name: "member", Description: "Who to remove", Kind: Member, Required: true, Multiple: true}},
		Run:     func(c *Context) error { return changeMembers(c, model.GroupRemove, "member") },
	},
	{
		Name: "promote", Description: "Makes members group admins", Group: true, Admin: true,
		Options: []Option{{Name: "member", Description: "Who to make an admin", Kind: Member, Required: true, Multiple: true,
			Filter: func(m model.Member) bool { return !m.Me && !m.Admin }}},
		Run: func(c *Context) error { return changeMembers(c, model.GroupPromote, "member") },
	},
	{
		Name: "demote", Description: "Dismisses group admins", Group: true, Admin: true,
		Options: []Option{{Name: "member", Description: "Which admin to dismiss", Kind: Member, Required: true, Multiple: true,
			Filter: func(m model.Member) bool { return !m.Me && m.Admin }}},
		Run: func(c *Context) error { return changeMembers(c, model.GroupDemote, "member") },
	},
	{
		Name: "link", Description: "Shows the group's invite link", Group: true, Admin: true,
		Run: runLink,
	},
	{
		Name: "lockdown", Description: "Lets only admins send messages, or everyone again", Group: true, Admin: true,
		Options: []Option{{Name: "mode", Description: "on: only admins send; off: everyone sends",
			Kind: Choice, Choices: []string{"on", "off"}}},
		Run: runLockdown,
	},
	{
		Name: "description", Description: "Changes the group description", Group: true, Admin: true,
		Options: []Option{{Name: "text", Description: "The new description", Kind: Text, Required: true}},
		Run:     runDescription,
	},
	{
		Name: "sticker", Description: "Turns the photo you reply to, or a picture you pick, into a sticker",
		Options: []Option{
			{Name: "top", Description: "Text along the top; # starts the bottom text", Kind: Text, Until: "#"},
			{Name: "bottom", Description: "Text along the bottom", Kind: Text},
		},
		Run: runSticker,
	},
}

// busy shows that the command is working, and returns its note.
func busy(c *Context, text string) *Note {
	n := &Note{Title: c.Input, Text: text, Busy: true}
	c.Note(n)
	return n
}

// fail turns a busy note into an error.
func fail(n *Note, text string) {
	n.Busy, n.Failed, n.Text = false, true, text
}

// changeMembers adds, removes, promotes or demotes the people in option
// opt, then says what happened to each.
func changeMembers(c *Context, action model.GroupAction, opt string) error {
	ids := c.IDs(opt)
	if len(ids) == 0 {
		return errors.New("Pick someone first.")
	}
	verb := map[model.GroupAction]string{model.GroupAdd: "Adding", model.GroupRemove: "Removing",
		model.GroupPromote: "Promoting", model.GroupDemote: "Dismissing"}[action]
	n := busy(c, verb+"…")
	chat := c.Chat.ID
	c.Group(model.GroupRequest{ChatID: chat, Action: action, Members: ids}, func(ev model.GroupEvent) {
		n.Busy = false
		if ev.Err != "" {
			fail(n, ev.Err)
			return
		}
		var done []string
		var doneIDs []string
		var lines []string
		for _, r := range ev.Members {
			if r.Err == "" {
				done = append(done, r.Name)
				doneIDs = append(doneIDs, r.ID)
				continue
			}
			lines = append(lines, r.Name+" "+r.Err+".")
			if r.Invite != nil {
				n.Buttons = append(n.Buttons, inviteButton(c, n, chat, r))
			}
		}
		if len(done) > 0 {
			var s string
			switch action {
			case model.GroupAdd:
				s = "Added " + names(done) + " to the group."
			case model.GroupRemove:
				s = "Removed " + names(done) + " from the group."
				n.Buttons = append(n.Buttons, Button{Label: "Add back", Run: func() {
					addBack(c, n, chat, doneIDs)
				}})
			case model.GroupPromote:
				s = names(done) + map[bool]string{true: " is now a group admin.", false: " are now group admins."}[len(done) == 1]
			case model.GroupDemote:
				s = names(done) + map[bool]string{true: " is no longer a group admin.", false: " are no longer group admins."}[len(done) == 1]
			}
			lines = append([]string{s}, lines...)
		}
		n.Failed = len(done) == 0
		n.Text = strings.Join(lines, "\n")
		if n.Text == "" {
			n.Text = "Nothing changed."
		}
	})
	return nil
}

// addBack adds people removed by /kick back to the group.
func addBack(c *Context, n *Note, chat string, ids []string) {
	n.Buttons, n.Busy = nil, true
	c.Group(model.GroupRequest{ChatID: chat, Action: model.GroupAdd, Members: ids}, func(ev model.GroupEvent) {
		n.Busy = false
		if ev.Err != "" {
			n.Text += "\n" + ev.Err
			return
		}
		for _, r := range ev.Members {
			if r.Err == "" {
				n.Text += "\nAdded " + r.Name + " back."
			} else {
				n.Text += "\n" + r.Name + " " + r.Err + "."
			}
		}
	})
}

// inviteButton sends someone whose privacy settings refused /add an
// invite to join, in your chat with them.
func inviteButton(c *Context, n *Note, chat string, r model.MemberResult) Button {
	b := Button{Label: "Invite " + firstName(r.Name)}
	b.Run = func() {
		// The button goes; the note says how it went.
		for i := range n.Buttons {
			if n.Buttons[i].Label == b.Label {
				n.Buttons = append(n.Buttons[:i:i], n.Buttons[i+1:]...)
				break
			}
		}
		c.Group(model.GroupRequest{ChatID: chat, Action: model.GroupSendInvite, Members: []string{r.ID}, Invite: r.Invite},
			func(ev model.GroupEvent) {
				if ev.Err != "" {
					n.Text += "\nCouldn't invite " + r.Name + ": " + ev.Err
					return
				}
				n.Text += "\nSent " + r.Name + " an invite to join."
			})
	}
	return b
}

func runLink(c *Context) error {
	n := busy(c, "Getting the invite link…")
	chat := c.Chat.ID
	var show func(ev model.GroupEvent)
	show = func(ev model.GroupEvent) {
		n.Busy = false
		if ev.Err != "" {
			fail(n, ev.Err)
			return
		}
		link := ev.Link
		n.Text = link
		n.Buttons = []Button{
			{Label: "Copy link", Run: func() { c.Copy(link) }},
			{Label: "Reset link", Danger: true, Run: func() {
				c.Confirm("Reset the invite link?", "The current link stops working. Anyone with it can't join anymore.",
					"Reset link", true, func() {
						n.Busy, n.Buttons = true, nil
						c.Group(model.GroupRequest{ChatID: chat, Action: model.GroupLink, On: true}, show)
					})
			}},
		}
	}
	c.Group(model.GroupRequest{ChatID: chat, Action: model.GroupLink}, show)
	return nil
}

func runLockdown(c *Context) error {
	on := true
	switch {
	case c.Text("mode") != "":
		on = c.Text("mode") == "on"
	case c.Info != nil:
		on = !c.Info.Announce // switch it
	}
	n := busy(c, map[bool]string{true: "Locking the group…", false: "Unlocking the group…"}[on])
	c.Group(model.GroupRequest{ChatID: c.Chat.ID, Action: model.GroupAnnounce, On: on}, func(ev model.GroupEvent) {
		n.Busy = false
		switch {
		case ev.Err != "":
			fail(n, ev.Err)
		case on:
			n.Text = "Only admins can send messages now. /lockdown off lets everyone send again."
		default:
			n.Text = "Everyone can send messages again."
		}
	})
	return nil
}

func runDescription(c *Context) error {
	text := c.Text("text")
	n := busy(c, "Changing the description…")
	c.Group(model.GroupRequest{ChatID: c.Chat.ID, Action: model.GroupDescription, Text: text}, func(ev model.GroupEvent) {
		n.Busy = false
		if ev.Err != "" {
			fail(n, ev.Err)
			return
		}
		n.Text = "Changed the group description."
	})
	return nil
}

func runSticker(c *Context) error {
	chat := c.Chat.ID
	text := sticker.Text{Top: c.Text("top"), Bottom: c.Text("bottom")}
	plain := strings.TrimSpace(text.Top+text.Bottom) == ""
	makeSticker := func(read func() ([]byte, error)) {
		n := busy(c, "Making a sticker…")
		c.Do(func() func() {
			data, err := read()
			var webp []byte
			if err == nil {
				webp, err = sticker.FromImage(data, text)
			}
			return func() {
				switch {
				case errors.Is(err, sticker.ErrAnimated):
					fail(n, "Text can't go on an animated sticker yet.")
					return
				case errors.Is(err, sticker.ErrTooLarge):
					fail(n, "That picture is too big to make a sticker of.")
					return
				case err != nil:
					fail(n, "Couldn't make a sticker of that picture.")
					return
				}
				c.Dismiss(n)
				if m := c.Backend.SendNewSticker(chat, webp, nil); m != nil {
					c.Sent(m)
				}
			}
		})
	}
	src := c.Reply
	switch {
	case src == nil:
		c.PickImage(func(path string) {
			if path != "" {
				makeSticker(func() ([]byte, error) { return os.ReadFile(path) })
			}
		})
	case src.Kind == model.KindSticker && plain:
		// It's a sticker already: send it as it is.
		c.Backend.SendSticker(chat, src, nil)
	case src.Kind == model.KindSticker, src.Kind == model.KindImage && src.Media == model.MediaImage:
		data := c.Backend.MediaData(src.ChatID, src.ID)
		if data == nil {
			return errors.New("It hasn't downloaded yet. Try again in a moment.")
		}
		makeSticker(func() ([]byte, error) { return data, nil })
	default:
		return errors.New("Reply to a photo or a sticker, or run /sticker without replying to pick a picture.")
	}
	return nil
}

// names lists names as a sentence: "A", "A and B", "A, B and C".
func names(ns []string) string {
	switch len(ns) {
	case 0:
		return ""
	case 1:
		return ns[0]
	}
	return strings.Join(ns[:len(ns)-1], ", ") + " and " + ns[len(ns)-1]
}

func firstName(s string) string {
	s = strings.TrimPrefix(s, "~")
	if i := strings.IndexByte(s, ' '); i > 0 && !strings.HasPrefix(s, "+") {
		return s[:i]
	}
	return s
}
