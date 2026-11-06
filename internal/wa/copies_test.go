package wa

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/polymorfa/hypermeow/proto/waCommon"
	"github.com/polymorfa/hypermeow/proto/waE2E"
	"github.com/polymorfa/hypermeow/types"
	"github.com/polymorfa/hypermeow/types/events"
	"google.golang.org/protobuf/proto"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

const (
	copyChat   = "123@g.us"
	copyParent = "PARENT"
	copyChild  = "CHILD"
)

// sentPhoto is a picture as its message comes, in the album ALBUM.
func sentPhoto() *waE2E.Message {
	m := &waE2E.Message{ImageMessage: &waE2E.ImageMessage{URL: proto.String("https://mmg/sd"), DirectPath: proto.String("/v/sd"),
		MediaKey: bytes.Repeat([]byte{1}, 32), Caption: proto.String("look"), Width: proto.Uint32(720)}}
	inAlbum(m, types.NewJID("123", types.GroupServer), "ALBUM")
	return m
}

// copyMessage wraps media as the copy of the message copyParent that WhatsApp
// sends: an associatedChildMessage with the association outside it.
func copyMessage(t waE2E.MessageAssociation_AssociationType, media *waE2E.Message) *waE2E.Message {
	return &waE2E.Message{
		MessageContextInfo: &waE2E.MessageContextInfo{MessageAssociation: &waE2E.MessageAssociation{
			AssociationType:  t.Enum(),
			ParentMessageKey: &waCommon.MessageKey{ID: proto.String(copyParent)},
		}},
		AssociatedChildMessage: &waE2E.FutureProofMessage{Message: media},
	}
}

// hdPhoto is the HD copy of sentPhoto, here without its caption.
func hdPhoto() *waE2E.Message {
	return copyMessage(waE2E.MessageAssociation_HD_IMAGE_DUAL_UPLOAD, &waE2E.Message{ImageMessage: &waE2E.ImageMessage{
		URL: proto.String("https://mmg/hd"), DirectPath: proto.String("/v/hd"), MediaKey: bytes.Repeat([]byte{2}, 32), Width: proto.Uint32(2560)}})
}

// arrive has message id come as it would from the network, at unix second ts.
func arrive(t *testing.T, b *Backend, id string, ts int64, m *waE2E.Message) {
	t.Helper()
	jid := types.NewJID("123", types.GroupServer)
	e := &events.Message{Info: types.MessageInfo{MessageSource: types.MessageSource{Chat: jid, Sender: types.NewJID("9", types.HiddenUserServer), IsGroup: true},
		ID: id, Timestamp: time.Unix(ts, 0)}, Message: unwrapForEvent(m), RawMessage: m}
	if err := b.store.ensureChat(b.ctx, b.db, copyChat, true, "g"); err != nil {
		t.Fatal(err)
	}
	p, ok := b.parse(b.ctx, e)
	if !ok {
		t.Fatalf("%s wasn't parsed", id)
	}
	b.apply(&e.Info, p)
}

// unwrapForEvent is the message hypermeow hands out inside what came.
func unwrapForEvent(m *waE2E.Message) *waE2E.Message {
	if in := m.GetAssociatedChildMessage().GetMessage(); in != nil {
		return in
	}
	return m
}

// newMessages counts the message events of new messages so far, and empties
// the queue.
func newMessages(b *Backend) int {
	n := 0
	for _, e := range b.Poll() {
		if m, ok := e.(model.MessageEvent); ok && m.New {
			n++
		}
	}
	return n
}

// blobURL is the URL of what a message's media downloads from.
func blobURL(t *testing.T, b *Backend, id string) string {
	t.Helper()
	media, blob, err := b.store.mediaBlob(b.ctx, copyChat, id)
	if err != nil {
		t.Fatal(err)
	}
	img := mediaMessage(media, blob)
	return img.GetImageMessage().GetURL() + img.GetVideoMessage().GetURL()
}

// In whichever order they come, a message and its HD copy show as one message
// that counts once, keeps what the message says and downloads the HD file.
func TestHDCopyShowsOnce(t *testing.T) {
	for _, tc := range []struct {
		name  string
		first string
	}{{"message first", copyParent}, {"copy first, as in history", copyChild}} {
		t.Run(tc.name, func(t *testing.T) {
			b := testBackend(t)
			if tc.first == copyParent {
				arrive(t, b, copyParent, 1000, sentPhoto())
				arrive(t, b, copyChild, 1030, hdPhoto())
			} else {
				arrive(t, b, copyChild, 1030, hdPhoto())
				arrive(t, b, copyParent, 1000, sentPhoto())
			}
			arrive(t, b, copyChild, 1030, hdPhoto())    // again
			arrive(t, b, copyParent, 1000, sentPhoto()) // again

			if n := newMessages(b); n != 1 {
				t.Errorf("%d new messages, want 1", n)
			}
			ms := b.Messages(copyChat, 10)
			if len(ms) != 1 {
				t.Fatalf("%d messages: %v", len(ms), ids(ms))
			}
			m := ms[0]
			if m.ID != copyParent || m.Text != "look" || m.Album != "ALBUM" || m.Time.Unix() != 1000 {
				t.Errorf("shows %s %q in album %q at %d", m.ID, m.Text, m.Album, m.Time.Unix())
			}
			if url := blobURL(t, b, copyParent); url != "https://mmg/hd" {
				t.Errorf("downloads %s", url)
			}
		})
	}
}

// A copy with nothing better to download (a motion photo's clip, a video in a
// codec the system may lack) doesn't show either, and changes no download.
func TestOtherCopiesShowNothing(t *testing.T) {
	for name, a := range map[string]waE2E.MessageAssociation_AssociationType{
		"motion photo": waE2E.MessageAssociation_MOTION_PHOTO,
		"hevc":         waE2E.MessageAssociation_HEVC_VIDEO_DUAL_UPLOAD,
	} {
		t.Run(name, func(t *testing.T) {
			b := testBackend(t)
			arrive(t, b, copyParent, 1000, sentPhoto())
			arrive(t, b, copyChild, 1001, copyMessage(a, &waE2E.Message{VideoMessage: &waE2E.VideoMessage{
				URL: proto.String("https://mmg/clip"), DirectPath: proto.String("/v/clip"), MediaKey: bytes.Repeat([]byte{3}, 32)}}))
			ms := b.Messages(copyChat, 10)
			if len(ms) != 1 || ms[0].ID != copyParent || ms[0].Media != model.MediaImage {
				t.Fatalf("shows %v", ids(ms))
			}
			if url := blobURL(t, b, copyParent); url != "https://mmg/sd" {
				t.Errorf("downloads %s", url)
			}
		})
	}
}

// A message that's "Waiting for this message" shows its copy until it comes.
func TestCopyFillsWaiting(t *testing.T) {
	b := testBackend(t)
	var p parsed
	p.msg.Message = &model.Message{ID: copyParent, ChatID: copyChat, Kind: model.KindWaiting, Time: time.Unix(1000, 0), Receipt: model.Sent}
	if err := b.store.putMessage(b.ctx, b.db, p.msg); err != nil {
		t.Fatal(err)
	}
	arrive(t, b, copyChild, 1001, hdPhoto())
	ms := b.Messages(copyChat, 10)
	if len(ms) != 1 || ms[0].Kind == model.KindWaiting || ms[0].Media != model.MediaImage {
		t.Fatalf("shows %+v", ms)
	}
	arrive(t, b, copyParent, 1000, sentPhoto())
	if ms := b.Messages(copyChat, 10); len(ms) != 1 || ms[0].Text != "look" {
		t.Fatalf("after the message came: %+v", ms)
	}
}

// A message deleted for everyone stays deleted when its copy comes, and
// deleting one forgets the copy.
func TestCopyOfDeleted(t *testing.T) {
	b := testBackend(t)
	arrive(t, b, copyParent, 1000, sentPhoto())
	arrive(t, b, copyChild, 1001, hdPhoto())
	if err := b.store.markDeleted(b.ctx, copyChat, copyParent, ""); err != nil {
		t.Fatal(err)
	}
	if b.store.hasCopy(b.ctx, copyChat, copyParent) {
		t.Error("a deleted message keeps its copy")
	}
	arrive(t, b, copyChild, 1001, hdPhoto())
	ms := b.Messages(copyChat, 10)
	if len(ms) != 1 || ms[0].Kind != model.KindDeleted {
		t.Fatalf("shows %+v", ms)
	}
	if b.store.hasCopy(b.ctx, copyChat, copyParent) {
		t.Error("the copy came back to a deleted message")
	}
}

// What was downloaded before the HD copy came goes, once.
func TestHDCopyReplacesDownload(t *testing.T) {
	b := testBackend(t)
	arrive(t, b, copyParent, 1000, sentPhoto())
	path := b.mediaPath(copyChat, copyParent)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	write := func() {
		t.Helper()
		if err := os.WriteFile(path, []byte("file"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write()
	arrive(t, b, copyChild, 1001, hdPhoto())
	if _, err := os.Stat(path); err == nil {
		t.Error("the standard file stayed")
	}
	media := false
	for _, e := range b.Poll() {
		if m, ok := e.(model.MediaEvent); ok && m.MsgID == copyParent {
			media = true
		}
	}
	if !media {
		t.Error("the UI wasn't told to read it again")
	}
	write() // the HD file
	arrive(t, b, copyChild, 1001, hdPhoto())
	if _, err := os.Stat(path); err != nil {
		t.Error("the copy coming again removed the HD file")
	}
}

// A database from before keeps its messages, and each copy joins its message
// with its star and what was downloaded of it.
func TestFoldCopies(t *testing.T) {
	b := testBackend(t)
	put := func(id string, ts int64, m *waE2E.Message) {
		t.Helper()
		c := describe(m)
		sm := storedMsg{Message: &model.Message{ID: id, ChatID: copyChat, Kind: c.kind, Media: c.media, Text: c.text,
			Time: time.Unix(ts, 0), Receipt: model.Sent}, rawPayload: marshal(m)}
		if err := b.store.putMessage(b.ctx, b.db, sm); err != nil {
			t.Fatal(err)
		}
	}
	put(copyParent, 1000, sentPhoto())
	put(copyChild, 1030, hdPhoto())
	// A copy whose message isn't here takes its place.
	orphan := hdPhoto()
	orphan.MessageContextInfo.MessageAssociation.ParentMessageKey.ID = proto.String("GONE")
	put("ORPHAN", 1040, orphan)
	put("PLAIN", 1050, &waE2E.Message{ImageMessage: &waE2E.ImageMessage{URL: proto.String("https://mmg/plain"), DirectPath: proto.String("/v/p"), MediaKey: bytes.Repeat([]byte{5}, 32)}})
	if err := b.store.setMessageFlag(b.ctx, copyChat, copyChild, "starred", true); err != nil {
		t.Fatal(err)
	}
	if err := b.store.putReceipt(b.ctx, b.db, personReceipt{chat: copyChat, id: copyChild, who: "9@lid", delivered: 1}); err != nil {
		t.Fatal(err)
	}
	from, to := b.mediaPath(copyChat, copyChild), b.mediaPath(copyChat, copyParent)
	if err := os.MkdirAll(filepath.Dir(from), 0o700); err != nil {
		t.Fatal(err)
	}
	for path, s := range map[string]string{from: "hd", to: "sd"} {
		if err := os.WriteFile(path, []byte(s), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	for range 2 { // the second does nothing
		if err := b.foldCopies(b.ctx); err != nil {
			t.Fatal(err)
		}
		ms := b.Messages(copyChat, 10)
		if got := ids(ms); len(got) != 3 || got[0] != copyParent || got[1] != "GONE" || got[2] != "PLAIN" {
			t.Fatalf("messages %v", got)
		}
		if m := ms[0]; m.Text != "look" || m.Album != "ALBUM" || !m.Starred || m.Time.Unix() != 1000 {
			t.Errorf("the message reads %q in album %q, starred %v, at %d", m.Text, m.Album, m.Starred, m.Time.Unix())
		}
		if url := blobURL(t, b, copyParent); url != "https://mmg/hd" {
			t.Errorf("downloads %s", url)
		}
		if data, _ := os.ReadFile(to); string(data) != "hd" {
			t.Errorf("the message's file holds %q", data)
		}
		var receipts int
		if err := b.db.QueryRow(`SELECT count(*) FROM wz_receipts WHERE id = ?`, copyChild).Scan(&receipts); err != nil || receipts != 0 {
			t.Errorf("%d receipts of the copy left (%v)", receipts, err)
		}
	}
}
