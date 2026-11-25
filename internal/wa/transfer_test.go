package wa

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/polymorfa/hypermeow/proto/waE2E"
	"google.golang.org/protobuf/proto"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

func transferEvents(b *Backend) (out []model.TransferEvent) {
	for _, e := range b.Poll() {
		if t, ok := e.(model.TransferEvent); ok {
			out = append(out, t)
		}
	}
	return out
}

// A request made with a transfer's context tells how far its body has got,
// either way; any other goes uncounted.
func TestTransferProgress(t *testing.T) {
	body := bytes.Repeat([]byte("x"), 1<<20)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			got, _ := io.ReadAll(r.Body)
			if !bytes.Equal(got, body) {
				t.Errorf("the server got %d bytes", len(got))
			}
			return
		}
		w.Header().Set("Content-Length", "1048576")
		w.Write(body)
	}))
	defer srv.Close()
	b := testBackend(t)
	now := time.Unix(1000, 0)
	b.clock = func() time.Time { now = now.Add(150 * time.Millisecond); return now }
	cli := &http.Client{Transport: progressTransport{http.DefaultTransport}}
	do := func(ctx context.Context, method string, send []byte) {
		t.Helper()
		var r io.Reader
		if send != nil {
			r = bytes.NewReader(send)
		}
		req, err := http.NewRequestWithContext(ctx, method, srv.URL, r)
		if err != nil {
			t.Fatal(err)
		}
		resp, err := cli.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if got, err := io.ReadAll(resp.Body); err != nil || method == http.MethodGet && !bytes.Equal(got, body) {
			t.Fatalf("read %d bytes: %v", len(got), err)
		}
	}
	for _, upload := range []bool{false, true} {
		tr := b.beginTransfer("c", "m", upload)
		if b.beginTransfer("c", "m", upload) != nil {
			t.Error("a message has two transfers")
		}
		if upload {
			do(tr.context(b.ctx), http.MethodPost, body)
		} else {
			do(tr.context(b.ctx), http.MethodGet, nil)
		}
		if tr.end() {
			t.Error("nobody canceled it")
		}
		evs := transferEvents(b)
		if len(evs) < 4 || evs[0].Total != 0 || !evs[len(evs)-1].End || evs[0].Upload != upload {
			t.Fatalf("upload %v: %+v", upload, evs)
		}
		last, left := -1, false
		for _, e := range evs[1:] {
			if e.Percent() < last {
				t.Errorf("went back from %d%% to %d%%", last, e.Percent())
			}
			last = e.Percent()
			left = left || e.Left > 0
		}
		if last != 100 || !left {
			t.Errorf("upload %v: ended at %d%%, told the time left: %v", upload, last, left)
		}
		if len(b.Transfers()) != 0 {
			t.Error("it's still listed")
		}
	}
	do(b.ctx, http.MethodGet, nil)
	if evs := transferEvents(b); len(evs) != 0 {
		t.Errorf("an unfollowed request told %+v", evs)
	}
}

// Canceling stops a request midway, and a download that waits for others
// leaves the queue.
func TestCancelTransfer(t *testing.T) {
	stall := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "1000")
		w.Write(make([]byte, 100))
		w.(http.Flusher).Flush()
		select {
		case <-stall:
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()
	defer close(stall)
	b := testBackend(t)
	b.downloads = newFetcher()
	tr := b.beginTransfer("c", "m", false)
	req, _ := http.NewRequestWithContext(tr.context(b.ctx), http.MethodGet, srv.URL, nil)
	resp, err := (&http.Client{Transport: progressTransport{http.DefaultTransport}}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b.CancelTransfer("c", "m")
	if _, err := io.ReadAll(resp.Body); !errors.Is(err, context.Canceled) {
		t.Errorf("reading on: %v", err)
	}
	if !tr.canceled() || !tr.end() {
		t.Error("it doesn't know it was canceled")
	}

	b.queueDownload("c", "waits") // nothing runs the queue
	if n := len(b.Transfers()); n != 1 {
		t.Fatalf("%d transfers", n)
	}
	b.queueDownload("c", "waits")
	b.CancelTransfer("c", "waits")
	if n := len(b.Transfers()); n != 0 {
		t.Errorf("%d transfers after canceling", n)
	}
	if !b.downloads.add("c/waits", func() {}) {
		t.Error("it can't be queued again")
	}
	b.queueDownload(stickerChat, "s")
	if n := len(b.Transfers()); n != 0 {
		t.Error("the picker's stickers aren't followed")
	}
	b.CancelTransfer("c", "nothing")
}

func TestTransferSpeed(t *testing.T) {
	var s transferSpeed
	at := time.Unix(0, 0)
	s.add(at, 0)
	if l := s.left(at, 0, 100); l != 0 {
		t.Errorf("knows %v at once", l)
	}
	// A burst into the system's buffers, then 10 bytes a second.
	done := int64(500)
	for range 20 {
		at = at.Add(time.Second)
		done += 10
		s.add(at, done)
	}
	if l := s.left(at, done, done+100); l != 10*time.Second {
		t.Errorf("100 bytes at 10 a second take %v", l)
	}
	if l := s.left(at, done, done); l != 0 {
		t.Errorf("nothing takes %v", l)
	}
	if l := s.left(at, done, done+1); l != time.Second {
		t.Errorf("a last byte takes %v", l)
	}
	if len(s.samples) > 8 {
		t.Errorf("keeps %d samples", len(s.samples))
	}
}

// A picture tells its file's size, and the HD copy's once it has one.
func TestHDSize(t *testing.T) {
	b := testBackend(t)
	p := sentPhoto()
	p.ImageMessage.FileLength = proto.Uint64(1000)
	arrive(t, b, copyParent, 1000, p)
	m := b.Messages(copyChat, 10)[0]
	if m.HD || m.FileSize != 1000 {
		t.Fatalf("HD %v, %d bytes", m.HD, m.FileSize)
	}
	hd := hdPhoto()
	hdImage(hd).FileLength = proto.Uint64(5000)
	arrive(t, b, copyChild, 1001, hd)
	m = b.Messages(copyChat, 10)[0]
	if !m.HD || m.FileSize != 5000 {
		t.Fatalf("HD %v, %d bytes", m.HD, m.FileSize)
	}
}

func hdImage(m *waE2E.Message) *waE2E.ImageMessage {
	for m != nil && m.ImageMessage == nil {
		m = unwrapOnce(m)
	}
	return m.GetImageMessage()
}
