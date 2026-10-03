package ui

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/chomosuke9/wazzapclients/internal/mock"
	"github.com/chomosuke9/wazzapclients/internal/model"
)

func TestGroupStatusTextDestination(t *testing.T) {
	u := New(mock.New())
	u.Start(func() {})
	u.openGroupStatus("123@g.us")
	u.openStatusText()
	u.status.text.ed.SetText("group only")
	u.status.groupID = "789@g.us" // the opened composer must retain its recipient
	u.postStatusText()
	u.applyEvents()
	var found bool
	for _, thread := range u.statuses {
		for _, up := range thread.Updates {
			if up.Text == "group only" {
				found = true
				if !thread.Group || thread.ID != "123@g.us" {
					t.Fatalf("wrong destination: %+v", thread)
				}
			}
		}
	}
	if !found {
		t.Fatal("status missing")
	}
	u.setPage(pageChats)
	u.setPage(pageStatus)
	if u.status.groupID != "" {
		t.Fatal("group destination leaked to personal status")
	}
}

func TestGroupStatusMediaQueueDestination(t *testing.T) {
	u := New(mock.New())
	u.Start(func() {})
	u.SelectID("rina")
	u.conv.composer.SetText("chat draft")
	u.openGroupStatus("123@g.us")
	audio := filepath.Join(t.TempDir(), "voice.ogg")
	if err := os.WriteFile(audio, []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	u.attachPaths([]string{audio})
	if !u.postingStatus() || len(u.attach.files) != 1 || u.attach.files[0].Media != model.MediaAudio {
		t.Fatal("audio not accepted as group status")
	}
	u.conv.composer.SetText("audio status")
	u.sendComposer()
	u.setPage(pageChats) // queued files retain their original recipient
	u.flushOutbox()
	u.applyEvents()
	var found bool
	for _, thread := range u.statuses {
		for _, up := range thread.Updates {
			if up.Text == "audio status" {
				found = true
				if !thread.Group || thread.ID != "123@g.us" || up.Media != model.MediaAudio {
					t.Fatalf("wrong destination: %+v", thread)
				}
			}
		}
	}
	if !found {
		t.Fatal("audio status missing")
	}
	if u.conv.composer.Text() != "chat draft" {
		t.Fatal("chat draft lost")
	}
}

func TestGroupStatusLateFileDialog(t *testing.T) {
	u := New(mock.New())
	u.Start(func() {})
	u.openGroupStatus("123@g.us")
	destination := u.statusDestination()
	u.setPage(pageStatus) // personal statuses
	u.addFiles(destination, []*attachFile{{Attachment: model.Attachment{Path: "old.jpg", Media: model.MediaImage}}})
	if len(u.attach.files) != 0 {
		t.Fatal("late group file dialog opened on personal status page")
	}
}

func TestGroupStatusInfoNavigation(t *testing.T) {
	h := &sendHarness{t: t, u: New(mock.New()), now: testNow()}
	h.u.Start(func() {})
	h.u.SelectID("work")
	h.u.ShowInfo(0, 0)
	for range 35 {
		h.frame()
	}
	h.u.btn("info-action:status").Click()
	h.frame()
	if h.u.page != pageStatus || h.u.status.groupID != "work" {
		t.Fatal("Status button did not open the group")
	}
	for range 35 {
		h.frame()
	}
	h.u.btn("st:all").Click()
	h.frame()
	if h.u.status.groupID != "" {
		t.Fatal("back did not restore all statuses")
	}
}
