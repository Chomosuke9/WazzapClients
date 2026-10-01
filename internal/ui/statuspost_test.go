package ui

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chomosuke9/wazzapclients/internal/mock"
	"github.com/chomosuke9/wazzapclients/internal/model"
)

// myStatus returns your own status thread.
func (u *UI) myStatus() *model.StatusThread {
	for _, t := range u.statuses {
		if t.Mine {
			return t
		}
	}
	return nil
}

func TestPostTextStatus(t *testing.T) {
	u := New(mock.New())
	u.Start(func() {})
	u.setPage(pageStatus)
	before := len(u.myStatus().Updates)
	u.openStatusText()
	u.status.text.ed.SetText("  ")
	u.postStatusText()
	if !u.status.text.isOpen() {
		t.Fatal("blank text was posted")
	}
	u.status.text.ed.SetText(" Hello ")
	u.status.text.color = 2
	u.postStatusText()
	if u.status.text.isOpen() {
		t.Error("the composer stayed open")
	}
	u.applyEvents()
	ups := u.myStatus().Updates
	if len(ups) != before+1 {
		t.Fatalf("my status has %d updates, want %d", len(ups), before+1)
	}
	if up := ups[len(ups)-1]; up.Text != "Hello" || up.Background != statusColors[2] || up.Media != model.MediaNone {
		t.Errorf("posted %q on %#x (%v)", up.Text, up.Background, up.Media)
	}
}

func TestPostMediaStatus(t *testing.T) {
	u := New(mock.New())
	u.Start(func() {})
	u.SelectID("rina")
	u.conv.composer.SetText("draft for Rina")
	u.setPage(pageStatus)

	dir := t.TempDir()
	photo := filepath.Join(dir, "photo.png")
	f, err := os.Create(photo)
	if err != nil {
		t.Fatal(err)
	}
	img := image.NewNRGBA(image.Rect(0, 0, 40, 30))
	img.SetNRGBA(1, 1, color.NRGBA{R: 0xff, A: 0xff})
	png.Encode(f, img)
	f.Close()
	doc := filepath.Join(dir, "notes.txt")
	os.WriteFile(doc, []byte("hello"), 0o600)

	// Dropped on the Status page, the photo becomes a status; the
	// document can't.
	u.attachPaths([]string{photo, doc})
	if !u.postingStatus() || len(u.attach.files) != 1 {
		t.Fatalf("posting %v with %d files, want the photo alone", u.postingStatus(), len(u.attach.files))
	}
	if u.mentionQuery() != nil {
		t.Error("a status caption offers mentions")
	}
	before := len(u.myStatus().Updates)
	msgs := len(u.msgs)
	u.conv.composer.SetText("caption")
	u.sendComposer()
	u.flushOutbox()
	u.applyEvents()
	ups := u.myStatus().Updates
	if len(ups) != before+1 {
		t.Fatalf("my status has %d updates, want %d", len(ups), before+1)
	}
	if up := ups[len(ups)-1]; up.Media != model.MediaImage || up.Text != "caption" {
		t.Errorf("posted %q (%v)", up.Text, up.Media)
	}
	if len(u.msgs) != msgs {
		t.Error("the status went to the open chat too")
	}
	if got := u.conv.composer.Text(); got != "draft for Rina" {
		t.Errorf("the chat's draft is %q after posting", got)
	}
}

func TestLeavingStatusPageDropsStatusFiles(t *testing.T) {
	u := New(mock.New())
	u.Start(func() {})
	u.setPage(pageStatus)
	photo := filepath.Join(t.TempDir(), "photo.jpg")
	os.WriteFile(photo, []byte("x"), 0o600)
	u.attachPaths([]string{photo})
	if !u.postingStatus() {
		t.Fatal("the photo didn't open the send view")
	}
	u.setPage(pageChats)
	if len(u.attach.files) != 0 {
		t.Error("the status's send view stayed open on the Chats page")
	}
}

func TestStatusPrivacyText(t *testing.T) {
	for _, c := range []struct {
		p    *model.StatusPrivacy
		want string
	}{
		{nil, "Checking"},
		{&model.StatusPrivacy{}, "all your contacts"},
		{&model.StatusPrivacy{Audience: model.AudienceExcept, Count: 1}, "except 1 contact."},
		{&model.StatusPrivacy{Audience: model.AudienceOnly, Count: 3}, "only with 3 contacts."},
	} {
		if got := statusPrivacyText(c.p); !strings.Contains(got, c.want) {
			t.Errorf("%+v: %q doesn't say %q", c.p, got, c.want)
		}
	}
}
