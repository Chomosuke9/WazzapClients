package ui

// Only actual editor changes announce typing. Restoring a draft or opening
// a chat must not tell the other person we're writing to them.
func (u *UI) canReportTyping() bool {
	c := u.selected
	return c != nil && !c.Self && !u.away && !u.ghostMode() &&
		!u.postingStatus() && u.sendBlocked(c) == "" &&
		trimSpace(u.conv.composer.Text()) != "" && u.slashQuery() == nil
}

func (u *UI) reportComposerTyping() {
	if !u.canReportTyping() {
		u.stopOutgoingTyping()
		return
	}
	u.conv.outgoingTyping = u.selected.ID
	u.backend.ReportTyping(u.selected.ID)
}

func (u *UI) stopOutgoingTyping() {
	if u.conv.outgoingTyping != "" {
		u.backend.ReportTyping("")
		u.conv.outgoingTyping = ""
	}
}
