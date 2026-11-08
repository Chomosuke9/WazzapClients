package ui

import (
	"image/color"
	"os"
	"slices"
	"strings"

	"gioui.org/font"
	"gioui.org/layout"
	"github.com/chomosuke9/wazzapclients/internal/command"
	"github.com/chomosuke9/wazzapclients/internal/model"
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
	case u.conv.edit.msg != nil:
		problem = "Use /whisper in a new message, not an edit."
	case len(u.attach.files) > 0 && u.attach.chatID != u.selected.ID:
		problem = "/whisper sends only the files picked for this chat."
	case u.slashQuery() == nil:
		problem = "Start the message with /whisper, then pick members and type the text."
	default:
		return false
	}
	u.toast(problem)
	return true
}

// slashFiles is how many files a command typed in chat's composer would
// take (Command.Files): the send view's, while it's open for that chat.
func (u *UI) slashFiles(chat string) int {
	if a := &u.attach; a.chatID == chat {
		return len(a.files)
	}
	return 0
}

// heldCmd is a command that took the send view's files (/whisper typed as
// a caption), waiting for its edited photos to render.
type heldCmd struct {
	ctx   *command.Context
	items []*outItem // ctx.Files' files, as they'll go
}

// holdFiles closes the send view, giving its files to the command ctx,
// which runs once they're ready (runHeld). The command's text is the
// caption of the file on show; the others keep their own.
func (u *UI) holdFiles(ctx *command.Context) {
	a := &u.attach
	h := &heldCmd{ctx: ctx}
	for i, f := range a.files {
		typed := i == a.cur
		var d model.Draft
		if !typed {
			d = u.draftFrom(trimSpace(f.caption))
		}
		h.items = append(h.items, u.outItem(a.chatID, f, model.Draft{}))
		ctx.Files = append(ctx.Files, command.File{Caption: d, Typed: typed})
	}
	a.held = append(a.held, h)
	u.conv.reply, u.conv.mentions = nil, nil
	u.closeSendView(true)
	u.conv.composer.SetText("")
	u.runHeld()
	u.scrollMessages(layout.Position{})
}

// runHeld runs, in order, the commands whose files are ready.
func (u *UI) runHeld() {
	a := &u.attach
	for len(a.held) > 0 {
		h := a.held[0]
		if slices.ContainsFunc(h.items, func(it *outItem) bool { return it.busy }) {
			return
		}
		a.held[0] = nil
		a.held = a.held[1:]
		for i, it := range h.items {
			h.ctx.Files[i].Attachment = it.att
		}
		command.Execute(h.ctx)
		// The backend has read the app's own copies (pictures) by now.
		for _, it := range h.items {
			if it.temp {
				_ = os.Remove(it.att.Path)
			}
		}
	}
	a.held = nil
}
