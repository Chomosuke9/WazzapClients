package command

import (
	"errors"
	"math/rand/v2"
	"os"
	"strconv"
	"strings"
	"time"

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
	{
		Name: "purge", Description: "Deletes the last messages for everyone, or those up to the one you reply to",
		Options: []Option{{Name: "count", Description: "How many messages to delete", Kind: Number,
			Required: true, Min: 1, Max: maxPurge}},
		Run: runPurge,
	},
	{
		Name: "raffle", Description: "Draws members of the group at random and announces them", Group: true,
		Options: []Option{{Name: "winners", Description: "How many to draw (1 if you leave it out)", Kind: Number, Min: 1, Max: 50}},
		Run:     runRaffle,
	},
	{
		Name: "calc", Description: "Works out a sum as you type, and sends it with the answer",
		Options: []Option{{Name: "sum", Description: "Like 12 x 4500, 15k x 3 or 15% x 80000; ans is the last answer",
			Kind: Text, Required: true}},
		Run:     runCalc,
		Preview: previewCalc,
	},
	{
		Name: "schedule", Description: "Sends a message later, even with the window closed",
		Options: []Option{
			{Name: "when", Description: "Like 21:00, 30m, 2h, tomorrow 08:00, fri 18:00 or 25/12 09:00", Kind: When,
				Required: true, Choices: []string{"30m", "1h", "3h", "tomorrow 08:00"}},
			{Name: "message", Description: "What to send; @mentions work as in any message", Kind: Text,
				Required: true, Mentions: true},
		},
		Run:     runSchedule,
		Preview: previewSchedule,
	},
	{
		Name: "scheduled", Description: "Lists the messages scheduled in this chat, to send now or cancel",
		Run: runScheduled,
	},
	{
		Name: "afk", Description: "Replies for you while you're away, until you send a message",
		Options: []Option{{Name: "reason", Description: "Why you're away, shown in the reply", Kind: Text}},
		Run:     runAFK,
	},
	{
		Name: "ghost", Description: "Goes invisible: no read receipts, shown offline, no sending, until you turn it off",
		Run: runGhost,
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

// maxPurge is the most messages /purge deletes at once.
const maxPurge = 100

// revokeWindow is how long after sending a message it can still be
// deleted for everyone (WhatsApp allows about two days).
const revokeWindow = 60 * time.Hour

// runPurge deletes the last count messages for everyone, or, replying to
// a message, that one and the ones before it. Messages already deleted
// don't count. Those you can't delete for everyone (others' outside
// groups you administer, or too old) are left as they are.
func runPurge(c *Context) error {
	chat := c.Chat.ID
	count := c.Int("count", 1)
	msgs := purgeable(c.Backend, chat, c.Reply, count)
	if len(msgs) == 0 {
		return errors.New("There are no messages to delete.")
	}
	admin := c.Chat.IsGroup && c.Info != nil && isAdmin(c.Info)
	var del []*model.Message
	others, old, unsent := 0, 0, 0
	for _, m := range msgs {
		switch {
		case !m.FromMe && !admin:
			others++
		case m.FromMe && (m.Receipt == model.Pending || m.Receipt == model.Failed):
			unsent++
		case c.Now.Sub(m.Time) > revokeWindow:
			old++
		default:
			del = append(del, m)
		}
	}
	var skipped []string
	if others > 0 {
		s := strconv.Itoa(others) + " from other people"
		if c.Chat.IsGroup {
			s += " (only group admins can delete those)"
		}
		skipped = append(skipped, s)
	}
	if old > 0 {
		skipped = append(skipped, strconv.Itoa(old)+" too old to delete for everyone")
	}
	if unsent > 0 {
		skipped = append(skipped, strconv.Itoa(unsent)+" not sent yet")
	}
	left := strings.Join(skipped, ", ")
	if len(del) == 0 {
		return errors.New("None of them can be deleted: " + left + ".")
	}
	body := ""
	if left != "" {
		body = "Left as they are: " + left + "."
	}
	c.Confirm("Delete "+plural(len(del), "message")+" for everyone?", body, "Delete for everyone", true, func() {
		for _, m := range del {
			c.Backend.Delete(m, true)
		}
		text := "Deleted " + plural(len(del), "message") + " for everyone."
		if left != "" {
			text += "\nLeft as they are: " + left + "."
		}
		c.Note(&Note{Title: c.Input, Text: text})
	})
	return nil
}

// purgeable returns up to count messages of a chat that aren't deleted
// yet, newest first: the newest ones, or from reply back.
func purgeable(b model.Backend, chat string, reply *model.Message, count int) []*model.Message {
	var out []*model.Message
	var page []*model.Message
	if reply != nil {
		page = b.MessagesFrom(chat, reply.ID, 1)
		if len(page) == 0 {
			return nil
		}
	} else {
		page = b.Messages(chat, count)
	}
	for len(page) > 0 {
		for i := len(page) - 1; i >= 0 && len(out) < count; i-- {
			if page[i].Kind != model.KindDeleted {
				out = append(out, page[i])
			}
		}
		if len(out) == count {
			break
		}
		page = b.MessagesBefore(chat, page[0].ID, count)
	}
	return out
}

// runRaffle draws winners from the group's members, you left out, and
// sends them as a message that mentions them.
func runRaffle(c *Context) error {
	if c.Info == nil {
		return errors.New("The group's members haven't loaded yet. Try again in a moment.")
	}
	var pool []model.Member
	for _, m := range c.Info.Members {
		if !m.Me && m.ID != "" {
			pool = append(pool, m)
		}
	}
	n := c.Int("winners", 1)
	if len(pool) == 0 {
		return errors.New("There's no one else in the group to draw.")
	}
	if n > len(pool) {
		return errors.New("The group has only " + plural(len(pool), "other member") + " to draw from.")
	}
	rand.Shuffle(len(pool), func(i, j int) { pool[i], pool[j] = pool[j], pool[i] })
	text, ids := raffleText(raffleLines[rand.IntN(len(raffleLines))], pool[:n], len(pool))
	if m := c.Backend.Send(c.Chat.ID, model.Draft{Text: text, Mentions: ids}); m != nil {
		c.Sent(m)
	}
	return nil
}

// raffleText is the message announcing a raffle's winners, drawn from
// pool members, under line (one of raffleLines), and the IDs it mentions.
func raffleText(line string, winners []model.Member, pool int) (string, []string) {
	var b strings.Builder
	b.WriteString("🎲 *Raffle*\n" + line + ":")
	ids := make([]string, len(winners))
	for i, w := range winners {
		ids[i] = w.ID
		user, _, _ := strings.Cut(w.ID, "@")
		b.WriteString("\n" + strconv.Itoa(i+1) + ". @" + user)
	}
	b.WriteString("\n_Drawn at random from " + plural(pool, "member") + "._")
	return b.String(), ids
}

// lastAnswer is the answer of the last /calc, which "ans" stands for. Only
// the UI goroutine runs commands.
var lastAnswer float64

// runCalc sends the sum with its answer: "12 x 4500 = 54 000".
func runCalc(c *Context) error {
	sum := strings.TrimSpace(c.Text("sum"))
	v, err := Calc(sum, lastAnswer)
	if err != nil {
		return errors.New("Couldn't work that out: " + err.Error() + ".")
	}
	if m := c.Backend.Send(c.Chat.ID, model.Draft{Text: sum + " = " + FormatNumber(v), Reply: c.Reply}); m != nil {
		lastAnswer = v
		c.Sent(m)
	}
	return nil
}

// previewCalc shows the answer while the sum is typed. A sum that only
// isn't finished yet shows nothing.
func previewCalc(in *Input) (string, bool) {
	sum := strings.TrimSpace(in.Text("sum"))
	if sum == "" {
		return "", false
	}
	v, err := Calc(sum, lastAnswer)
	switch {
	case errors.Is(err, errUnfinished):
		return "", false
	case err != nil:
		return strings.ToUpper(err.Error()[:1]) + err.Error()[1:], false
	}
	return "= " + FormatNumber(v), true
}

// plural is "1 message" or "3 messages".
func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return strconv.Itoa(n) + " " + noun + "s"
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
