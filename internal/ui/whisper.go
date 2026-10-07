package ui

import (
	"image/color"
	"strings"

	"gioui.org/font"
	"gioui.org/layout"
)

// whisperTint colours a whisper bubble so it reads apart from a plain
// message. It is pre-mixed opaque; Gio blends in linear space (see AGENTS).
var whisperTint = rgb(0x8a74d8)

// whisperHeader is the "only you can see this" banner and recipient line a
// whisper bubble carries, above its text. names are the members it went to.
func (u *UI) whisperHeader(gtx C, names []string, col color.NRGBA, maxW int) D {
	gtx.Constraints.Max.X = maxW
	line := func(w layout.Widget) layout.FlexChild { return layout.Rigid(w) }
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		line(func(gtx C) D {
			return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(iconW(icVisibilityOff, 15, col)),
				layout.Rigid(layout.Spacer{Width: 4}.Layout),
				layout.Rigid(u.label(13, "Only you can see this", col, labelOpts{italic: true, maxLines: 1}).Layout),
			)
		}),
		line(layout.Spacer{Height: 1}.Layout),
		line(u.label(12.5, "Whispered to "+strings.Join(names, ", "), col,
			labelOpts{weight: font.Medium, maxLines: 2}).Layout),
	)
}

// Whispers require a second, fresh acknowledgment before saving the toggle.
func (u *UI) confirmWhisperRisk(enable func()) {
	u.confirm("Enable /whisper? (2/2)",
		"This feature can easily get your account banned.\n\n"+
			"Using /whisper may get your WhatsApp account restricted or banned. "+
			"Continue only if you understand and accept this risk.",
		dialogButton{label: "Enable anyway", primary: true, danger: true, run: enable})
	u.dialog.agreement = "I understand my account may be banned and still want to enable /whisper."
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
