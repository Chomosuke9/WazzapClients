package remote

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chomosuke9/wazzapclients/internal/mock"
	"github.com/chomosuke9/wazzapclients/internal/model"
)

func TestSearch(t *testing.T) {
	s, h := newTest(t)
	h.pump = s
	var res struct {
		Chats    []Chat
		Messages []Found
		Complete bool
	}
	if code := do(t, s, "GET", "/v1/search?q=RINA", "", &res); code != http.StatusOK {
		t.Fatalf("search: %d", code)
	}
	if len(res.Chats) == 0 || res.Chats[0].ID != "rina" {
		t.Errorf("chats found by name: %+v", res.Chats)
	}

	// A word of a demo message finds it, with its chat's name.
	ms := h.b.Messages("budi", 1)
	word := strings.Fields(ms[0].Text)[0]
	if code := do(t, s, "GET", "/v1/search?q="+word, "", &res); code != http.StatusOK || !res.Complete {
		t.Fatalf("search %q: %d, complete %v", word, code, res.Complete)
	}
	var hit bool
	for _, f := range res.Messages {
		if f.Message.ID == ms[0].ID && f.Chat.ID == "budi" && f.Chat.Name == "Budi Santoso" {
			hit = true
		}
	}
	if !hit {
		t.Errorf("search %q didn't find %q: %+v", word, ms[0].Text, res.Messages)
	}

	for _, q := range []string{"", "%20%20", strings.Repeat("a", 101)} {
		if code := do(t, s, "GET", "/v1/search?q="+q, "", &map[string]string{}); code != http.StatusBadRequest {
			t.Errorf("q=%q: %d, want 400", q, code)
		}
	}
}

// getPNG sends a request and decodes the PNG it answers with.
func getPNG(t *testing.T, s *Server, path string) image.Image {
	t.Helper()
	r := httptest.NewRequest("GET", path, nil)
	r.Header.Set("Authorization", "Bearer "+testToken)
	w := httptest.NewRecorder()
	s.srv.Handler.ServeHTTP(w, r)
	if w.Code != http.StatusOK || w.Header().Get("Content-Type") != "image/png" {
		t.Fatalf("%s: %d %s %q", path, w.Code, w.Header().Get("Content-Type"), w.Body)
	}
	img, err := png.Decode(w.Body)
	if err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	if p, ok := img.(*image.Paletted); !ok || len(p.Palette) > 256 {
		t.Errorf("%s: not paletted to 256 colors: %T", path, img)
	}
	return img
}

func TestPhoto(t *testing.T) {
	s, _ := newTest(t)
	defer func(d time.Duration) { avatarWait = d }(avatarWait)
	avatarWait = 50 * time.Millisecond

	var v map[string]string
	if code := do(t, s, "GET", "/v1/chats/budi/photo/version", "", &v); code != http.StatusOK || len(v["version"]) != 12 {
		t.Fatalf("version: %d %v", code, v)
	}
	img := getPNG(t, s, "/v1/chats/budi/photo?size=40")
	if b := img.Bounds(); b.Dx() != 40 || b.Dy() != 40 {
		t.Errorf("size %v, want 40x40", b)
	}
	if r, g, b, _ := img.At(0, 0).RGBA(); r+g+b > 3*0x1000 {
		t.Errorf("the corner isn't black: %v", img.At(0, 0))
	}
	if r, g, b, _ := img.At(20, 20).RGBA(); r+g+b == 0 {
		t.Error("the middle is black")
	}

	// A chat without a picture.
	if code := do(t, s, "GET", "/v1/chats/rina/photo/version", "", &v); code != http.StatusNotFound {
		t.Errorf("no picture: %d", code)
	}
	if code := do(t, s, "GET", "/v1/chats/rina/photo", "", &v); code != http.StatusNotFound {
		t.Errorf("no picture's PNG: %d", code)
	}
	if code := do(t, s, "GET", "/v1/chats/budi/photo?size=8", "", &v); code != http.StatusBadRequest {
		t.Errorf("size 8: %d", code)
	}
}

// pictureBackend has messages with pictures, which the demo draws itself.
type pictureBackend struct {
	*mock.Backend
	msgs map[string]*model.Message
	data map[string][]byte
}

func (b *pictureBackend) MessagesFrom(chatID, id string, limit int) []*model.Message {
	if m := b.msgs[id]; m != nil {
		return []*model.Message{m}
	}
	return nil
}

func (b *pictureBackend) MediaData(chatID, msgID string) []byte { return b.data[msgID] }

func testJPEG(w, h int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.Set(x, y, color.RGBA{uint8(x * 255 / w), uint8(y * 255 / h), 120, 255})
		}
	}
	var buf bytes.Buffer
	_ = jpeg.Encode(&buf, img, nil)
	return buf.Bytes()
}

func TestPicture(t *testing.T) {
	s, h := newTest(t)
	defer func(d time.Duration) { mediaWait = d }(mediaWait)
	mediaWait = 50 * time.Millisecond

	// A sticker, transparent around a red square.
	sticker := image.NewNRGBA(image.Rect(0, 0, 64, 64))
	for y := 16; y < 48; y++ {
		for x := 16; x < 48; x++ {
			sticker.Set(x, y, color.NRGBA{255, 0, 0, 255})
		}
	}
	var sbuf bytes.Buffer
	_ = png.Encode(&sbuf, sticker)

	h.backend = &pictureBackend{
		Backend: h.b,
		msgs: map[string]*model.Message{
			"photo":    {ID: "photo", Kind: model.KindImage, Media: model.MediaImage},
			"sticker":  {ID: "sticker", Kind: model.KindSticker, Media: model.MediaSticker},
			"video":    {ID: "video", Kind: model.KindImage, Media: model.MediaVideo, Thumb: testJPEG(100, 60)},
			"slow":     {ID: "slow", Kind: model.KindImage, Media: model.MediaImage, Thumb: testJPEG(40, 30)},
			"text":     {ID: "text", Kind: model.KindText, Text: "hi"},
			"viewonce": {ID: "viewonce", Kind: model.KindViewOnce, Media: model.MediaImage},
		},
		data: map[string][]byte{"photo": testJPEG(800, 600), "sticker": sbuf.Bytes()},
	}

	for _, c := range []struct {
		id   string
		w, h int
	}{
		{"photo", 200, 150},   // shrunk to fit 200x200
		{"sticker", 128, 128}, // grown, at most twice
		{"video", 200, 120},   // the preview, grown twice
		{"slow", 80, 60},      // the download didn't come: the preview
	} {
		img := getPNG(t, s, "/v1/chats/c/messages/"+c.id+"/image?w=200&h=200")
		if b := img.Bounds(); b.Dx() != c.w || b.Dy() != c.h {
			t.Errorf("%s: %v, want %dx%d", c.id, b, c.w, c.h)
		}
		if c.id == "sticker" {
			if r, g, b, a := img.At(0, 0).RGBA(); r+g+b > 3*0x1000 || a != 0xffff {
				t.Errorf("the sticker's transparent corner isn't black: %v", img.At(0, 0))
			}
		}
	}
	// pad: the box exactly, the photo (200x150) in its middle.
	img := getPNG(t, s, "/v1/chats/c/messages/photo/image?w=200&h=200&pad=1")
	if b := img.Bounds(); b.Dx() != 200 || b.Dy() != 200 {
		t.Errorf("padded: %v, want 200x200", b)
	}
	if r, g, b, _ := img.At(100, 10).RGBA(); r+g+b > 3*0x1000 {
		t.Errorf("above the photo isn't black: %v", img.At(100, 10))
	}
	if r, g, b, _ := img.At(100, 100).RGBA(); r+g+b == 0 {
		t.Error("the middle is black")
	}

	var e map[string]string
	for id, want := range map[string]int{"text": http.StatusUnsupportedMediaType, "viewonce": http.StatusForbidden, "gone": http.StatusNotFound} {
		if code := do(t, s, "GET", "/v1/chats/c/messages/"+id+"/image", "", &e); code != want {
			t.Errorf("%s: %d, want %d", id, code, want)
		}
	}
}

func TestQuantize(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 300, 200))
	for y := range 200 {
		for x := range 300 {
			img.Set(x, y, color.RGBA{uint8(x), uint8(y), uint8(x ^ y), 255})
		}
	}
	p := quantize(img)
	if len(p.Palette) != 256 {
		t.Errorf("%d colors, want 256", len(p.Palette))
	}
	// One color stays one color.
	flat := image.NewUniform(color.RGBA{10, 20, 30, 255})
	q := quantize(&image.RGBA{Pix: bytes.Repeat([]byte{10, 20, 30, 255}, 16), Stride: 16, Rect: image.Rect(0, 0, 4, 4)})
	if len(q.Palette) != 1 || q.Palette[0] != flat.C {
		t.Errorf("a flat picture: %v", q.Palette)
	}
}

// slowAvatars has profile pictures that aren't fetched yet: the ones in
// arrive are once their AvatarEvent comes.
type slowAvatars struct {
	*mock.Backend
	mu     sync.Mutex
	arrive map[string][]byte
	known  map[string]bool
}

func (b *slowAvatars) Avatar(id string) []byte {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.known[id] {
		return b.arrive[id]
	}
	return nil
}

func (b *slowAvatars) AvatarKnown(id string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.known[id]
}

func TestPhotoPending(t *testing.T) {
	s, h := newTest(t)
	defer func(d time.Duration) { avatarWait = d }(avatarWait)
	avatarWait = 200 * time.Millisecond
	slow := &slowAvatars{Backend: h.b, arrive: map[string][]byte{"late": testJPEG(80, 80)}, known: map[string]bool{"none": true}}
	h.backend = slow

	var v map[string]any
	// Known to have none: 404 at once.
	if code := do(t, s, "GET", "/v1/chats/none/photo/version", "", &v); code != http.StatusNotFound {
		t.Errorf("known to have none: %d %v", code, v)
	}
	// Not fetched and nothing comes: ask again later, not "none".
	if code := do(t, s, "GET", "/v1/chats/never/photo/version", "", &v); code != http.StatusAccepted || v["pending"] != true {
		t.Errorf("never fetched: %d %v", code, v)
	}
	// Fetched while the request waits.
	go func() {
		time.Sleep(50 * time.Millisecond)
		slow.mu.Lock()
		slow.known["late"] = true
		slow.mu.Unlock()
		h.mu.Lock()
		s.Event(model.AvatarEvent{ID: "late"})
		h.mu.Unlock()
	}()
	if code := do(t, s, "GET", "/v1/chats/late/photo/version", "", &v); code != http.StatusOK || v["version"] == "" {
		t.Errorf("fetched while waiting: %d %v", code, v)
	}
}
