package command

import (
	"errors"
	"strings"
)

func runWhisper(c *Context) error {
	if c.Info == nil {
		return errors.New("The group's members haven't loaded yet. Try again in a moment.")
	}
	var ids, recipients []string
	seen := map[string]bool{}
	for _, v := range c.Get("members") {
		valid := false
		for _, m := range c.Info.Members {
			if m.ID == v.ID && !m.Me && v.ID != "" && strings.HasPrefix(v.Text, "@") {
				valid = true
				if !seen[m.ID] {
					seen[m.ID] = true
					ids = append(ids, m.ID)
					recipients = append(recipients, m.Name)
				}
				break
			}
		}
		if !valid || v.Err != "" {
			return errors.New("Pick current group members with @mentions; you can't whisper to yourself.")
		}
	}
	text := strings.TrimSpace(c.Text("text"))
	if len(ids) == 0 || text == "" {
		return errors.New("Use /whisper @member @member text.")
	}
	n := busy(c, "Submitting whisper…")
	// Start on the UI goroutine, where the auto backend owns ghost/AFK state.
	done := c.Backend.SendWhisper(c.Chat.ID, ids, text)
	c.Do(func() func() {
		err := <-done
		return func() {
			if err != nil {
				fail(n, err.Error())
				return
			}
			n.Busy = false
			n.Text = "Whisper submitted for " + names(recipients) + ". Delivery is unconfirmed. " +
				"This is a local note; the whisper isn't synced to your other devices."
		}
	})
	return nil
}
