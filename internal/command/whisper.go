package command

import (
	"errors"
	"strings"

	"github.com/chomosuke9/wazzapclients/internal/model"
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
	if len(ids) == 0 || d.Text == "" && len(c.Files) == 0 {
		return errors.New("Use /whisper @member @member text.")
	}
	w := model.Whisper{ChatID: c.Chat.ID, Targets: ids, Reply: c.Reply}
	if len(c.Files) == 0 {
		w.Parts = []model.WhisperPart{{Text: d.Text, Mentions: d.Mentions}}
	}
	// Typed as a caption, the text is that file's caption; the other files
	// keep their own. Each file goes as a message of its own.
	for _, f := range c.Files {
		a := f.Attachment
		p := model.WhisperPart{Text: f.Caption.Text, Mentions: f.Caption.Mentions, File: &a}
		if f.Typed {
			p.Text, p.Mentions = d.Text, d.Mentions
		}
		w.Parts = append(w.Parts, p)
	}
	busyText := "Submitting whisper…"
	if len(c.Files) > 0 {
		busyText = "Uploading and submitting whisper…"
	}
	n := busy(c, busyText)
	// Start on the UI goroutine, where the auto backend owns ghost/AFK state.
	done := c.Backend.SendWhisper(w)
	c.Do(func() func() {
		err := <-done
		return func() {
			if err != nil {
				fail(n, err.Error())
				return
			}
			// The whisper's own messages in the chat are the confirmation;
			// drop the note so nothing lingers over the composer.
			c.Dismiss(n)
		}
	})
	return nil
}
