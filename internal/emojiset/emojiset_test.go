package emojiset

import (
	"bytes"
	"errors"
	"image"
	"image/png"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestName(t *testing.T) {
	for in, want := range map[string]string{
		"😀":      "1f600",
		"❤️":     "2764",
		"👍🏽":     "1f44d_1f3fd",
		"#️⃣":    "0023_20e3",
		"👨‍👩‍👧":  "1f468_200d_1f469_200d_1f467",
		"🏳️‍🌈":   "1f3f3_200d_1f308",
		"🇺🇸":     "1f1fa_1f1f8",
		"\ufe0f": "",
	} {
		if got := Name([]rune(in)); got != want {
			t.Errorf("Name(%q) = %q, want %q", in, got, want)
		}
	}
}

// pic3D is a 128x128 picture, as the 3D set's are.
var pic3D = func() string {
	var b bytes.Buffer
	png.Encode(&b, image.NewNRGBA(image.Rect(0, 0, 128, 128)))
	return b.String()
}()

// fakeStore serves the URLs of files, counting the requests.
func fakeStore(t *testing.T, files map[string]string, fail bool) (*Store, *sync.Map, chan struct{}) {
	var asked sync.Map
	got := make(chan struct{}, 16)
	s := New(t.TempDir())
	s.Fetch = func(url string) ([]byte, int, error) {
		n, _ := asked.LoadOrStore(url, new(int))
		*n.(*int)++
		if fail {
			return nil, 0, errors.New("offline")
		}
		for name, body := range files {
			if strings.Contains(url, name) {
				return []byte(body), http.StatusOK, nil
			}
		}
		return nil, http.StatusNotFound, nil
	}
	s.SetNotify(func() { got <- struct{}{} })
	return s, &asked, got
}

// settle waits until the store's workers are done.
func settle(s *Store) {
	for {
		s.mu.Lock()
		n := s.running
		s.mu.Unlock()
		if n == 0 {
			return
		}
		time.Sleep(time.Millisecond)
	}
}

func TestDownloadsOnce(t *testing.T) {
	s, asked, got := fakeStore(t, map[string]string{"emoji_u1f600.png": pic3D}, false)
	if s.Ready(ThreeD, "1f600") {
		t.Fatal("ready before downloading")
	}
	<-got
	if !s.Ready(ThreeD, "1f600") {
		t.Fatal("not ready after downloading")
	}
	// Kept as wide as the bundled font's pictures.
	if c, err := png.DecodeConfig(bytes.NewReader(s.Read(ThreeD, "1f600"))); err != nil || c.Width != 136 || c.Height != 128 {
		t.Errorf("kept %dx%d (%v), want 136x128", c.Width, c.Height, err)
	}
	if s.Gen() != 1 {
		t.Errorf("Gen = %d, want 1", s.Gen())
	}
	// A missing picture is asked for once, and remembered on disk.
	s.Ready(ThreeD, "abcd")
	settle(s)
	s.Ready(ThreeD, "abcd")
	settle(s)
	again := New(s.dir)
	again.Fetch = s.Fetch
	if again.Ready(ThreeD, "abcd") || !again.Ready(ThreeD, "1f600") {
		t.Error("a new store doesn't know what the old one fetched")
	}
	settle(again)
	asked.Range(func(url, n any) bool {
		if *n.(*int) != 1 {
			t.Errorf("%s asked %d times", url, *n.(*int))
		}
		return true
	})
	if s.Size() == 0 {
		t.Error("Size is 0")
	}
	if err := s.Clear(); err != nil || s.Size() != 0 {
		t.Errorf("Clear: %v, %d bytes left", err, s.Size())
	}
}

func TestRejectsWhatIsntAPicture(t *testing.T) {
	s, _, _ := fakeStore(t, map[string]string{"1f600/512.webp": "<html>", "emoji_u1f600.png": "<html>"}, false)
	s.Ready(Animated, "1f600")
	s.Ready(ThreeD, "1f600")
	settle(s)
	if s.Ready(Animated, "1f600") || s.Ready(ThreeD, "1f600") {
		t.Error("an HTML page was taken for a picture")
	}
}

func TestOfflineRetriesLater(t *testing.T) {
	s, asked, _ := fakeStore(t, nil, true)
	s.Ready(Animated, "1f600")
	settle(s)
	s.Ready(Animated, "1f600")
	settle(s)
	n := 0
	asked.Range(func(_, v any) bool { n += *v.(*int); return true })
	if n != 1 {
		t.Errorf("asked %d times within a minute of failing, want 1", n)
	}
	s.mu.Lock()
	s.known[key{Animated, "1f600"}].retry = time.Now().Add(-time.Second)
	s.mu.Unlock()
	s.Ready(Animated, "1f600")
	settle(s)
	n = 0
	asked.Range(func(_, v any) bool { n += *v.(*int); return true })
	if n != 2 {
		t.Errorf("asked %d times after the retry time, want 2", n)
	}
}
