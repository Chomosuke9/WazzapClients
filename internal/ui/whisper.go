package ui

import "strings"

// Whispers require a second, fresh acknowledgment before saving the toggle.
func (u *UI) confirmWhisperRisk(enable func()) {
	u.confirm("Aktifkan /whisper? (2/2)",
		"Fitur ini mudah terkena banned.\n\n"+
			"Penggunaan /whisper dapat membuat akun WhatsApp dibatasi atau diblokir. "+
			"Lanjutkan hanya jika kamu memahami dan menerima risiko ini.",
		dialogButton{label: "Tetap aktifkan", primary: true, danger: true, run: enable})
	u.dialog.agreement = "Saya memahami risiko akun terkena banned dan tetap ingin mengaktifkan /whisper."
}

func whisperText(text string) bool {
	words := strings.Fields(text)
	return len(words) > 0 && strings.EqualFold(words[0], "/whisper")
}

// A disabled or misplaced selective command must never fall through to an
// ordinary group message, edit, or media caption. Keep its draft for correction.
func (u *UI) blockWhisperFallback() bool {
	if !whisperText(u.conv.composer.Text()) {
		return false
	}
	var problem string
	switch {
	case !u.slash.on || !u.grayCmds["whisper"]:
		problem = "Enable Slash commands and /whisper in Extra features first."
	case u.selected == nil || !u.selected.IsGroup || u.postingStatus():
		problem = "/whisper works only in group chats."
	case len(u.attach.files) > 0 || u.conv.edit.msg != nil || u.conv.editorElsewhere:
		problem = "Use /whisper as a new text command, without attachments."
	case u.slashQuery() == nil:
		problem = "Start the message with /whisper, then pick members and type the text."
	default:
		return false
	}
	u.toast(problem)
	return true
}
