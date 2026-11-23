package command

import (
	"errors"
	"strings"

	"github.com/chomosuke9/wazzapclients/internal/auto"
)

// errNoAuto is what /afk says where nothing can send for you (some
// tests).
var errNoAuto = errors.New("This can't be done here.")

func runAFK(c *Context) error {
	if c.Auto == nil {
		return errNoAuto
	}
	c.Auto.SetAway(strings.TrimSpace(c.Text("reason")))
	n := &Note{Title: c.Input, Text: "You're AFK until you send a message. Whoever messages you, or mentions you " +
		"in a group, gets this reply once:\n" + auto.AwayPlain(c.Auto.Away(), c.Now)}
	n.Buttons = []Button{{Label: "I'm back", Run: func() {
		n.Buttons = nil
		if c.Auto.Away() == nil {
			n.Text = "You were back already."
			return
		}
		n.Text = auto.BackText(c.Auto.Back())
	}}}
	c.Note(n)
	return nil
}

// sentence is an error as a sentence: capitalized, with a full stop.
func sentence(err error) string {
	s := err.Error()
	return strings.ToUpper(s[:1]) + s[1:] + "."
}
