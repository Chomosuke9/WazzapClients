package ui

import (
	"github.com/chomosuke9/wazzapclients/internal/model"
	"github.com/chomosuke9/wazzapclients/internal/ui/icon"
)

// Drafts, like WhatsApp's: what a chat's composer held when another chat
// opened (its text, @mentions and reply, and the files in its send view)
// waits for the chat to open again, and its row in the chat list says
// "Draft:" meanwhile. They live as long as the app (the host keeps them
// while there is no window), not across restarts.

// chatDraft is what a chat's composer held when it was left.
type chatDraft struct {
	text     string
	mentions []mentionRef
	reply    *model.Message

	// The send view's files, the one it showed and the quality picked;
	// text is then what the composer had under the captions.
	files   []*attachFile
	cur     int
	quality model.Quality
}

func (d *chatDraft) empty() bool {
	return trimSpace(d.text) == "" && d.reply == nil && len(d.files) == 0
}

// stashDraft puts the open chat's composer aside as its draft and leaves
// the composer empty.
func (u *UI) stashDraft() {
	c := u.selected
	if c == nil {
		return
	}
	cv, a := &u.conv, &u.attach
	d := &chatDraft{text: cv.composer.Text(), mentions: cv.mentions, reply: cv.reply}
	if e := cv.edit; e.msg != nil {
		// A message being edited isn't a draft; what the composer had
		// before is.
		d.text, d.mentions, d.reply = e.draft, e.mentions, e.reply
	}
	if len(a.files) > 0 && a.chatID == c.ID {
		u.finishTyping()
		if f := a.current(); f != nil {
			f.caption = cv.composer.Text()
		}
		d.text, d.files, d.cur, d.quality = a.draft, a.files, a.cur, a.quality
		// The files are the draft's now: dropAttachments mustn't remove them.
		a.files, a.draft = nil, ""
	}
	u.setDraft(c.ID, d)
	cv.composer.SetText("")
	cv.reply, cv.mentions, cv.edit = nil, nil, msgEdit{}
}

// setDraft keeps d as chat id's draft, or forgets the draft if d is empty.
func (u *UI) setDraft(id string, d *chatDraft) {
	if old := u.drafts[id]; old != nil && old != d {
		removeTemps(old.files)
	}
	if d == nil || d.empty() {
		delete(u.drafts, id)
		return
	}
	u.drafts[id] = d
}

// restoreDraft gives the composer the open chat's draft back, with the
// send view open at once if it had files.
func (u *UI) restoreDraft() {
	c := u.selected
	if c == nil {
		return
	}
	d := u.drafts[c.ID]
	if d == nil {
		return
	}
	delete(u.drafts, c.ID)
	cv, a := &u.conv, &u.attach
	cv.mentions, cv.reply = d.mentions, d.reply
	if len(d.files) > 0 && !isChannelID(c.ID) {
		a.chatID, a.files, a.cur, a.quality, a.draft = c.ID, d.files, d.cur, d.quality, d.text
		a.ghost = nil
		a.anim.snap(true)
		u.resetEditor()
		if f := a.current(); f != nil {
			setComposer(&cv.composer, f.caption)
		}
		return
	}
	removeTemps(d.files)
	setComposer(&cv.composer, d.text)
}

// dropDraft forgets chat id's draft (the chat was deleted).
func (u *UI) dropDraft(id string) { u.setDraft(id, nil) }

// draftFiles adds files picked for a chat that is no longer open to its
// draft, as if they had been picked there.
func (u *UI) draftFiles(chatID string, files []*attachFile) {
	d := u.drafts[chatID]
	if d == nil {
		d = &chatDraft{}
		u.drafts[chatID] = d
	}
	if n := maxAttach - len(d.files); len(files) > n {
		removeTemps(files[n:])
		files = files[:n]
	}
	d.files = append(d.files, files...)
}

// clearDrafts forgets every draft (another account opens).
func clearDrafts(drafts map[string]*chatDraft) {
	for id, d := range drafts {
		removeTemps(d.files)
		delete(drafts, id)
	}
}

// draftPreview is what a chat's row shows of its draft: its text, or its
// first file's kind (a photo, a document) and how many files there are.
func draftPreview(d *chatDraft) (ic *icon.Icon, text string) {
	if len(d.files) == 0 {
		return nil, d.text
	}
	f := d.files[0]
	text = trimSpace(f.caption)
	if text == "" {
		text = trimSpace(d.text)
	}
	if text == "" {
		text = mediaLabel(&model.Message{Media: f.Media})
		if len(d.files) > 1 {
			text = itoa(len(d.files)) + " files"
		}
	}
	return mediaIcon(f.Media), text
}
