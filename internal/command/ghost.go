package command

// GhostOnText says what ghost mode does, as /ghost puts it.
const GhostOnText = "Ghost mode is on. Chats you open and status updates you watch send no read receipts, " +
	"and you show as offline, even while the app is open. Chats you read here stay unread on your phone. " +
	"You can't send messages, react or vote until you turn it off, and you don't see who's typing. " +
	"Scheduled messages and AFK replies still go out."

// runGhost turns ghost mode on. It's turned off where the composer was.
func runGhost(c *Context) error {
	c.SetGhost(true)
	c.Note(&Note{Title: c.Input, Text: GhostOnText})
	return nil
}
