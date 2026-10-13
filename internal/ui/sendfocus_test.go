package ui

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

// TestSendViewRefocus: the composer has the keys again once a sent file's
// send view has slid away.
func TestSendViewRefocus(t *testing.T) {
	st := newSlashTest(t, "rina")
	doc := filepath.Join(t.TempDir(), "notes.txt")
	os.WriteFile(doc, []byte("hello"), 0o600)
	st.u.addFiles("rina", []*attachFile{{Attachment: model.Attachment{Path: doc, Media: model.MediaDocument}}})
	for range 50 {
		st.frame()
	}
	st.u.sendAttachments()
	for range 50 {
		st.frame()
	}
	if len(st.u.attach.files) != 0 || st.u.attach.ghost != nil {
		t.Fatal("the send view is still open")
	}
	if !st.r.Source().Focused(&st.u.conv.composer) {
		t.Fatal("the composer lost the keys")
	}
}
