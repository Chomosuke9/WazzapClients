package ui

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/chomosuke9/wazzapclients/internal/mock"
	"github.com/chomosuke9/wazzapclients/internal/model"
)

// TestDraftKeptAcrossChats: text, a reply and a mention typed in one chat
// come back when it opens again, and another chat starts empty.
func TestDraftKeptAcrossChats(t *testing.T) {
	u := New(mock.New())
	u.Start(func() {})
	u.SelectID("rina")
	reply := &model.Message{ID: "r1", ChatID: "rina", Text: "hi"}
	u.conv.composer.SetText("half a thought")
	u.conv.reply = reply
	u.conv.mentions = []mentionRef{{name: "Rina", jid: "rina"}}

	u.SelectID("budi")
	if got := u.conv.composer.Text(); got != "" {
		t.Fatalf("another chat's composer has %q", got)
	}
	if u.conv.reply != nil || u.conv.mentions != nil {
		t.Fatal("another chat kept the reply or mentions")
	}
	if d := u.drafts["rina"]; d == nil || d.text != "half a thought" {
		t.Fatalf("rina's draft is %+v", d)
	}
	if _, txt := draftPreview(u.drafts["rina"]); txt != "half a thought" {
		t.Fatalf("the row previews %q", txt)
	}

	u.SelectID("rina")
	if got := u.conv.composer.Text(); got != "half a thought" {
		t.Fatalf("the draft came back as %q", got)
	}
	if u.conv.reply != reply || len(u.conv.mentions) != 1 {
		t.Fatal("the reply or mention didn't come back")
	}
	if u.drafts["rina"] != nil {
		t.Fatal("the open chat still has a draft")
	}

	// An emptied composer leaves no draft.
	u.conv.composer.SetText("  ")
	u.conv.reply, u.conv.mentions = nil, nil
	u.SelectID("budi")
	if u.drafts["rina"] != nil {
		t.Fatal("an empty composer left a draft")
	}
}

// TestDraftEditing: leaving a chat while editing a message keeps what
// the composer had before the edit, not the edit.
func TestDraftEditing(t *testing.T) {
	u := New(mock.New())
	u.Start(func() {})
	u.SelectID("rina")
	u.conv.composer.SetText("before")
	u.conv.edit = msgEdit{msg: &model.Message{ID: "m"}, draft: "before"}
	u.conv.composer.SetText("edited text")
	u.SelectID("budi")
	u.SelectID("rina")
	if got := u.conv.composer.Text(); got != "before" || u.conv.edit.msg != nil {
		t.Fatalf("composer %q, editing %v", got, u.conv.edit.msg != nil)
	}
}

// TestDraftAttachments: the send view's files, captions and the text
// under them wait for the chat, and temporary files survive the switch.
func TestDraftAttachments(t *testing.T) {
	u := New(mock.New())
	u.Start(func() {})
	u.SelectID("rina")
	dir := t.TempDir()
	doc := filepath.Join(dir, "notes.txt")
	tmp := filepath.Join(dir, "pasted.png")
	os.WriteFile(doc, []byte("hello"), 0o600)
	os.WriteFile(tmp, []byte("png"), 0o600)
	u.conv.composer.SetText("under")
	u.addFiles("rina", []*attachFile{
		{Attachment: model.Attachment{Path: doc, Media: model.MediaDocument}},
		{Attachment: model.Attachment{Path: tmp, Media: model.MediaImage}, temp: true},
	})
	u.conv.composer.SetText("first caption")
	u.showFile(1)
	u.conv.composer.SetText("second caption")
	u.attach.quality = model.QualityHD

	u.SelectID("budi")
	if len(u.attach.files) != 0 || u.conv.composer.Text() != "" {
		t.Fatal("the send view followed to another chat")
	}
	if _, err := os.Stat(tmp); err != nil {
		t.Fatal("the switch removed a pasted picture:", err)
	}
	if _, txt := draftPreview(u.drafts["rina"]); txt != "first caption" {
		t.Fatalf("the row previews %q", txt)
	}

	u.SelectID("rina")
	a := &u.attach
	if len(a.files) != 2 || a.cur != 1 || a.chatID != "rina" || a.quality != model.QualityHD {
		t.Fatalf("send view came back with %d files, cur %d, chat %q, quality %v", len(a.files), a.cur, a.chatID, a.quality)
	}
	if got := u.conv.composer.Text(); got != "second caption" {
		t.Fatalf("the shown file's caption is %q", got)
	}
	u.showFile(0)
	if got := u.conv.composer.Text(); got != "first caption" {
		t.Fatalf("the first file's caption is %q", got)
	}
	u.closeSendView(false)
	if got := u.conv.composer.Text(); got != "under" {
		t.Fatalf("closing the send view gave back %q", got)
	}
	if _, err := os.Stat(tmp); !os.IsNotExist(err) {
		t.Fatal("closing unsent didn't remove the pasted picture")
	}
}

// TestDraftPickedElsewhere: files from a dialog opened in one chat land
// in its draft when another chat is open by the time they come.
func TestDraftPickedElsewhere(t *testing.T) {
	u := New(mock.New())
	u.Start(func() {})
	u.SelectID("budi")
	doc := filepath.Join(t.TempDir(), "notes.txt")
	os.WriteFile(doc, []byte("hello"), 0o600)
	u.addFiles("rina", []*attachFile{{Attachment: model.Attachment{Path: doc, Media: model.MediaDocument}}})
	if len(u.attach.files) != 0 {
		t.Fatal("the files opened in the wrong chat")
	}
	u.SelectID("rina")
	if len(u.attach.files) != 1 {
		t.Fatalf("rina's send view has %d files", len(u.attach.files))
	}
}

// TestDraftDeletedChat: deleting a chat drops its draft and its files.
func TestDraftDeletedChat(t *testing.T) {
	u := New(mock.New())
	u.Start(func() {})
	u.SelectID("rina")
	tmp := filepath.Join(t.TempDir(), "pasted.png")
	os.WriteFile(tmp, []byte("png"), 0o600)
	u.addFiles("rina", []*attachFile{{Attachment: model.Attachment{Path: tmp, Media: model.MediaImage}, temp: true}})
	u.SelectID("budi")
	u.dropDraft("rina")
	if u.drafts["rina"] != nil {
		t.Fatal("the draft is still there")
	}
	if _, err := os.Stat(tmp); !os.IsNotExist(err) {
		t.Fatal("the draft's pasted picture is still there")
	}
}
