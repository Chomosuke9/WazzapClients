package command

import (
	"errors"
	"strings"
)

func runWhisper(c *Context) error {
	if c.Info == nil {
		return errors.New("The group's members haven't loaded yet. Try again in a moment.")
	}
	var ids []string
	seen := map[string]bool{}
	for _, v := range c.Get("members") {
		valid := false
		for _, m := range c.Info.Members {
			if m.ID == v.ID && !m.Me && v.ID != "" && strings.HasPrefix(v.Text, "@") {
				valid = true
				if !seen[m.ID] {
					seen[m.ID] = true
					ids = append(ids, m.ID)
				}
				break
			}
		}
		if !valid || v.Err != "" {
			return errors.New("Pick current group members with @mentions; you can't whisper to yourself.")
		}
	}
	// Draft rewrites the body's @mentions (e.g. "@Budi" to "@<user>") and
	// collects their JIDs, exactly as an ordinary message does.
	d := c.Draft(strings.TrimSpace(c.Text("text")))
	if len(ids) == 0 || d.Text == "" {
		return errors.New("Use /whisper @member @member text.")
	}
	n := busy(c, "Submitting whisper…")
	// Start on the UI goroutine, where the auto backend owns ghost/AFK state.
	done := c.Backend.SendWhisper(c.Chat.ID, ids, d.Text, d.Mentions, c.Reply)
	c.Do(func() func() {
		err := <-done
		return func() {
			if err != nil {
				fail(n, err.Error())
				return
			}
			// The whisper's own message in the chat is the confirmation;
			// drop the note so nothing lingers over the composer.
			c.Dismiss(n)
		}
	})
	return nil
}
