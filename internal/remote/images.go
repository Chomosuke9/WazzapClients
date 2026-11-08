package remote

// Search and pictures. Both wait for the backend: message search answers
// with a SearchEvent, and profile pictures and media download in the
// background and announce themselves with an AvatarEvent or a MediaEvent.
// A request registers what it waits for on the UI goroutine (listen), and
// Event wakes it.
//
// Pictures go to the watch as small PNGs it can show as they are: cut to
// the size asked for, on black (the watch's background, so no
// transparency), with at most 256 colors. The phone converts them to the
// watch's format and sends them over Bluetooth at a few KB a second, so
// every byte counts.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"image"
	"image/color"
	"image/draw"
	_ "image/jpeg"
	"image/png"
	"net/http"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	xdraw "golang.org/x/image/draw"
	_ "golang.org/x/image/webp"

	"github.com/chomosuke9/wazzapclients/internal/lottie"
	"github.com/chomosuke9/wazzapclients/internal/model"
	"github.com/chomosuke9/wazzapclients/internal/photo"
	"github.com/chomosuke9/wazzapclients/internal/webpanim"
)

// waiter is a request waiting for a backend event.
type waiter struct {
	match func(model.Event) bool
	ch    chan model.Event
	until time.Time
}

// listen registers a wait for the first event match accepts, for at most
// d. Call it on the UI goroutine, before starting what sends the event.
func (s *Server) listen(match func(model.Event) bool, d time.Duration) <-chan model.Event {
	w := &waiter{match: match, ch: make(chan model.Event, 1), until: time.Now().Add(d + time.Second)}
	s.waiters = append(s.waiters, w)
	return w.ch
}

// wake hands e to the requests waiting for it, and lets go of the ones
// that gave up.
func (s *Server) wake(e model.Event) {
	if len(s.waiters) == 0 {
		return
	}
	now := time.Now()
	s.waiters = slices.DeleteFunc(s.waiters, func(w *waiter) bool {
		if w.match(e) {
			w.ch <- e
			return true
		}
		return now.After(w.until)
	})
}

// wait waits for ch for at most d. It returns nil when the time is up or
// the request went away.
func wait(ctx context.Context, ch <-chan model.Event, d time.Duration) model.Event {
	if ch == nil {
		return nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case e := <-ch:
		return e
	case <-t.C:
	case <-ctx.Done():
	}
	return nil
}

// Found is a message that search found, with its chat (its ID, name and
// whether it's a group only).
type Found struct {
	Chat    Chat    `json:"chat"`
	Message Message `json:"message"`
}

// How long requests wait for the backend (variables for tests).
var (
	searchWait = 10 * time.Second
	avatarWait = 12 * time.Second
	mediaWait  = 25 * time.Second
)

// searchQuery is the longest query, in runes.
const searchQuery = 100

// search finds chats by name and messages by text:
//
//	GET /v1/search?q=<text>&limit=15
//
// It answers {"chats", "messages", "complete"}; complete is false when the
// message search didn't finish in time. It shares the backend's search of
// every chat with the window's chat list search: one started while the
// other runs cancels it.
func (s *Server) search(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" || !utf8.ValidString(q) || utf8.RuneCountInString(q) > searchQuery {
		fail(w, http.StatusBadRequest, "q must be 1 to 100 characters")
		return
	}
	limit, ok := intParam(r, "limit", 15, 50)
	if !ok || limit == 0 {
		fail(w, http.StatusBadRequest, "limit must be from 1 to 50")
		return
	}
	key := model.SearchKey(q)
	var (
		chats []Chat
		brief = map[string]Chat{}
		ch    <-chan model.Event
	)
	err := s.o.Do(func() {
		b := s.o.Host.Backend()
		all := b.Chats()
		for _, c := range all {
			brief[c.ID] = Chat{ID: c.ID, Name: c.Name, Group: c.IsGroup, Time: c.Time.Unix()}
		}
		chats = foundChats(all, key, limit)
		ch = s.listen(func(e model.Event) bool {
			se, ok := e.(model.SearchEvent)
			return ok && se.ChatID == "" && se.Query == q
		}, searchWait)
		b.SearchMessages("", q, limit)
	})
	if err != nil {
		fail(w, http.StatusServiceUnavailable, errBusy.Error())
		return
	}
	msgs := []Found{}
	e, _ := wait(r.Context(), ch, searchWait).(model.SearchEvent)
	for _, m := range e.Msgs {
		c, ok := brief[m.ChatID]
		if !ok {
			c = Chat{ID: m.ChatID, Name: m.ChatID}
		}
		msgs = append(msgs, Found{Chat: c, Message: toMessage(m, textLimit)})
	}
	if chats == nil {
		chats = []Chat{}
	}
	reply(w, http.StatusOK, map[string]any{"chats": chats, "messages": msgs, "complete": e.Query == q})
}

// foundChats are the chats whose name has key, archived ones too, the
// latest first.
func foundChats(chats []*model.Chat, key string, limit int) []Chat {
	var hits []*model.Chat
	for _, c := range chats {
		if strings.Contains(model.SearchKey(c.Name), key) {
			hits = append(hits, c)
		}
	}
	slices.SortStableFunc(hits, func(a, b *model.Chat) int { return b.Time.Compare(a.Time) })
	if len(hits) > limit {
		hits = hits[:limit]
	}
	out := make([]Chat, len(hits))
	for i, c := range hits {
		out[i] = toChat(c)
		out[i].Archived = false // in search, archived chats are listed like the rest
	}
	return out
}

// errPending is a profile picture this computer hasn't fetched yet: the
// backend fetches them one at a time, a while apart, so it can take
// longer than a request waits.
var errPending = errors.New("the profile picture isn't here yet, ask again later")

// avatar returns a chat's profile picture, waiting a while for one that
// isn't on this computer yet. It returns nil when the chat has none,
// errPending when it didn't come in time, and errBusy when the UI
// goroutine didn't answer.
func (s *Server) avatar(ctx context.Context, id string) ([]byte, error) {
	var data []byte
	var known bool
	var ch <-chan model.Event
	look := func() {
		b := s.o.Host.Backend()
		data = b.Avatar(id)
		known = data != nil || b.AvatarKnown(id)
	}
	if err := s.o.Do(func() {
		look()
		if !known {
			ch = s.listen(func(e model.Event) bool {
				ae, ok := e.(model.AvatarEvent)
				return ok && ae.ID == id
			}, avatarWait)
		}
	}); err != nil {
		return nil, errBusy
	}
	if known {
		return data, nil
	}
	if wait(ctx, ch, avatarWait) == nil {
		return nil, errPending
	}
	if err := s.o.Do(look); err != nil {
		return nil, errBusy
	}
	if !known {
		return nil, errPending
	}
	return data, nil
}

// avatarFailed answers a request whose profile picture didn't come.
func avatarFailed(w http.ResponseWriter, err error) {
	if err == errPending {
		reply(w, http.StatusAccepted, map[string]any{"pending": true, "error": err.Error()})
		return
	}
	fail(w, http.StatusServiceUnavailable, err.Error())
}

// version names a picture's contents, so the watch knows when it changed.
func version(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:6])
}

// photoVersion says which profile picture a chat has:
//
//	GET /v1/chats/{chat}/photo/version   {"version": "…"}
//
// It answers 404 when the chat has none, and 202 {"pending": true} when
// this computer is still fetching it: ask again later.
func (s *Server) photoVersion(w http.ResponseWriter, r *http.Request) {
	data, err := s.avatar(r.Context(), r.PathValue("chat"))
	switch {
	case err != nil:
		avatarFailed(w, err)
	case data == nil:
		fail(w, http.StatusNotFound, "no profile picture")
	default:
		reply(w, http.StatusOK, map[string]string{"version": version(data)})
	}
}

// photo sends a chat's profile picture as a circle of size px:
//
//	GET /v1/chats/{chat}/photo?size=58
func (s *Server) photo(w http.ResponseWriter, r *http.Request) {
	size, ok := intParam(r, "size", 58, 240)
	if !ok || size < 16 {
		fail(w, http.StatusBadRequest, "size must be from 16 to 240")
		return
	}
	data, err := s.avatar(r.Context(), r.PathValue("chat"))
	switch {
	case err != nil:
		avatarFailed(w, err)
		return
	case data == nil:
		fail(w, http.StatusNotFound, "no profile picture")
		return
	}
	img, err := decodePicture(data)
	if err != nil {
		fail(w, http.StatusInternalServerError, "can't read the profile picture")
		return
	}
	sendPNG(w, circle(img, size), version(data))
}

// picture sends a message's picture to fit in w x h px: a photo or a
// sticker (an animated one's first frame), or a video's or GIF's preview:
//
//	GET /v1/chats/{chat}/messages/{id}/image?w=400&h=400&pad=1
//
// With pad=1 the PNG is w x h exactly, the picture in its middle on black:
// the watch can't read a picture's size, so it places it by the box.
// A photo or sticker that isn't on this computer yet is downloaded first;
// if that fails, the photo's blurry preview comes instead.
func (s *Server) picture(w http.ResponseWriter, r *http.Request) {
	bw, ok1 := intParam(r, "w", 400, 1024)
	bh, ok2 := intParam(r, "h", 400, 1024)
	if !ok1 || !ok2 || bw < 16 || bh < 16 {
		fail(w, http.StatusBadRequest, "w and h must be from 16 to 1024")
		return
	}
	pad := r.URL.Query().Get("pad") == "1"
	chat, id := r.PathValue("chat"), r.PathValue("id")
	var (
		data, thumb []byte
		ch          <-chan model.Event
		e           *apiError
	)
	err := s.o.Do(func() {
		b := s.o.Host.Backend()
		ms := b.MessagesFrom(chat, id, 1)
		if len(ms) == 0 || ms[0].ID != id {
			e = &apiError{http.StatusNotFound, "no such message"}
			return
		}
		m := ms[0]
		thumb = m.Thumb
		switch {
		case m.Kind == model.KindViewOnce:
			e = &apiError{http.StatusForbidden, "view once messages open on the phone only"}
		case m.Media == model.MediaImage || m.Media == model.MediaSticker:
			data = b.MediaData(chat, id)
			if data == nil {
				ch = s.listen(func(ev model.Event) bool {
					me, ok := ev.(model.MediaEvent)
					return ok && me.ChatID == chat && me.MsgID == id
				}, mediaWait)
			}
		case m.Media == model.MediaVideo || m.Media == model.MediaGIF:
			data = thumb
		default:
			e = &apiError{http.StatusUnsupportedMediaType, "this message has no picture"}
		}
	})
	if err != nil {
		fail(w, http.StatusServiceUnavailable, errBusy.Error())
		return
	}
	if e != nil {
		fail(w, e.code, e.msg)
		return
	}
	if ch != nil {
		if me, ok := wait(r.Context(), ch, mediaWait).(model.MediaEvent); ok && !me.Failed {
			if err := s.o.Do(func() { data = s.o.Host.Backend().MediaData(chat, id) }); err != nil {
				fail(w, http.StatusServiceUnavailable, errBusy.Error())
				return
			}
		}
		if data == nil {
			data = thumb // the download failed or is slow: the preview is better than nothing
		}
	}
	if data == nil {
		fail(w, http.StatusNotFound, "the picture isn't available")
		return
	}
	img, err := decodePicture(data)
	if err != nil {
		fail(w, http.StatusInternalServerError, "can't read the picture")
		return
	}
	out := fit(img, bw, bh)
	if pad {
		out = centered(out, bw, bh)
	}
	sendPNG(w, out, version(data))
}

// centered puts img in the middle of a w x h picture on black.
func centered(img image.Image, w, h int) image.Image {
	out := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(out, out.Bounds(), image.NewUniform(color.Black), image.Point{}, draw.Src)
	b := img.Bounds()
	at := image.Pt((w-b.Dx())/2, (h-b.Dy())/2)
	draw.Draw(out, b.Sub(b.Min).Add(at), img, b.Min, draw.Src)
	return out
}

// decodePicture reads a JPEG, PNG or WebP, or an animated WebP's or a
// Lottie sticker's first frame.
func decodePicture(data []byte) (image.Image, error) {
	if lottie.IsWAS(data) {
		a, err := lottie.Load(data)
		if err != nil {
			return nil, err
		}
		w, h := a.Fit(512)
		img := image.NewRGBA(image.Rect(0, 0, w, h))
		a.Render(img, 0)
		return img, nil
	}
	if webpanim.IsAnimated(data) {
		a, err := webpanim.Parse(data)
		if err != nil {
			return nil, err
		}
		img, _, err := a.Next()
		return img, err
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	if b := img.Bounds(); b.Dx() <= 0 || b.Dy() <= 0 {
		return nil, errors.New("empty picture")
	}
	return img, nil
}

// scale draws img at w x h: shrunk by averaging, or grown smoothly (a
// video's preview is smaller than the watch's screen).
func scale(img image.Image, w, h int) *image.RGBA {
	b := img.Bounds()
	if w <= b.Dx() && h <= b.Dy() {
		return photo.Shrink(img, w, h)
	}
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	xdraw.BiLinear.Scale(dst, dst.Bounds(), img, b, xdraw.Src, nil)
	return dst
}

// fit scales img to fit in w x h, keeping its shape. Small pictures grow to
// at most twice their size.
func fit(img image.Image, w, h int) image.Image {
	b := img.Bounds()
	f := min(float64(w)/float64(b.Dx()), float64(h)/float64(b.Dy()), 2)
	sw, sh := max(1, int(float64(b.Dx())*f+0.5)), max(1, int(float64(b.Dy())*f+0.5))
	return onBlack(scale(img, sw, sh))
}

// circle cuts the middle square of img to a size px circle on black.
func circle(img image.Image, size int) image.Image {
	b := img.Bounds()
	side := min(b.Dx(), b.Dy())
	crop := image.Rect(0, 0, side, side).Add(b.Min).Add(image.Pt((b.Dx()-side)/2, (b.Dy()-side)/2))
	sq := scale(cropped(img, crop), size, size)
	out := image.NewRGBA(sq.Bounds())
	r := float64(size) / 2
	for y := range size {
		for x := range size {
			// Coverage of the pixel by the circle, from 4x4 samples.
			n := 0
			for sy := range 4 {
				for sx := range 4 {
					dx := float64(x) + (float64(sx)+0.5)/4 - r
					dy := float64(y) + (float64(sy)+0.5)/4 - r
					if dx*dx+dy*dy <= r*r {
						n++
					}
				}
			}
			c := sq.RGBAAt(x, y)
			a := uint32(c.A) * uint32(n) / 16
			out.SetRGBA(x, y, color.RGBA{uint8(uint32(c.R) * a / 255), uint8(uint32(c.G) * a / 255), uint8(uint32(c.B) * a / 255), 255})
		}
	}
	return out
}

func cropped(img image.Image, r image.Rectangle) image.Image {
	if sub, ok := img.(interface {
		SubImage(image.Rectangle) image.Image
	}); ok {
		return sub.SubImage(r)
	}
	out := image.NewRGBA(image.Rect(0, 0, r.Dx(), r.Dy()))
	draw.Draw(out, out.Bounds(), img, r.Min, draw.Src)
	return out
}

// onBlack lays img over black, so nothing in it is transparent.
func onBlack(img *image.RGBA) *image.RGBA {
	out := image.NewRGBA(img.Bounds())
	draw.Draw(out, out.Bounds(), image.NewUniform(color.Black), image.Point{}, draw.Src)
	draw.Draw(out, out.Bounds(), img, img.Bounds().Min, draw.Over)
	return out
}

// sendPNG writes img as a PNG of at most 256 colors.
func sendPNG(w http.ResponseWriter, img image.Image, ver string) {
	var buf bytes.Buffer
	enc := png.Encoder{CompressionLevel: png.BestCompression}
	if err := enc.Encode(&buf, quantize(img)); err != nil {
		fail(w, http.StatusInternalServerError, "can't encode the picture")
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("ETag", `"`+ver+`"`)
	_, _ = w.Write(buf.Bytes())
}
