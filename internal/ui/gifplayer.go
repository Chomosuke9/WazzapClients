package ui

import (
	"image"
	"sync"

	"gioui.org/op/paint"

	"github.com/chomosuke9/wazzapclients/internal/model"
	"github.com/chomosuke9/wazzapclients/internal/video"
)

// maxGIFs caps how many GIFs play at once in the conversation by default
// (perf.gifs), like WhatsApp, which plays them as they come on screen.
// Each has a video player of its own (Media Foundation and a GPU device
// on Windows); the rest show their thumbnail and a play button.
const maxGIFs = 3

// gifPlayer plays a GIF (an MP4 sent with gifPlayback) muted and looping
// in its bubble. It opens on a goroutine of its own, so a GIF coming on
// screen never stalls the frame.
type gifPlayer struct {
	mu      sync.Mutex
	p       *video.Player // nil while it opens
	stopped bool          // closed before it opened: close it once it does
	failed  bool

	frame paint.ImageOp
	size  image.Point
	used  int64 // players.frame of last use
}

// gifFetch is where a GIF's file stands, by "chat/id".
type gifFetch uint8

const (
	gifFetching gifFetch = iota + 1 // its download runs
	gifFailed                       // it can't download or play: the play button stays
)

func (g *gifPlayer) close() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.stopped = true
	if g.p != nil {
		g.p.Close()
		g.p = nil
	}
}

// gifImage returns the frame to show for an image message: a GIF's
// current frame while it plays (live), or img.
func (u *UI) gifImage(m *model.Message, img *imgEntry, maxPx int) *imgEntry {
	if m.Media != model.MediaGIF || u.blurred() || !u.autoplays(m) {
		return img
	}
	if f := u.gifFrame(m, maxPx); f != nil {
		return f
	}
	return img
}

// gifFrame returns a GIF's current frame, downloading the GIF and
// starting its player first. Nil until the first frame.
func (u *UI) gifFrame(m *model.Message, maxPx int) *imgEntry {
	ps := &u.players
	key := m.ChatID + "/" + m.ID
	g := ps.gifs[key]
	if g == nil {
		if ps.gifFetch[key] != 0 || len(ps.gifs) >= perf.gifs {
			return nil
		}
		if ps.gifFetch == nil {
			ps.gifFetch = make(map[string]gifFetch)
		}
		if !u.backend.HasMediaFile(m) {
			ps.gifFetch[key] = gifFetching // gifDownloaded continues
			u.backend.MediaFile(m)
			return nil
		}
		path := u.backend.MediaFile(m)
		if path == "" {
			return nil
		}
		if ps.gifs == nil {
			ps.gifs = make(map[string]*gifPlayer)
		}
		g = &gifPlayer{}
		ps.gifs[key] = g
		invalidate := u.images.invalidate
		go func() {
			p, err := video.Open(path, invalidate)
			g.mu.Lock()
			defer g.mu.Unlock()
			switch {
			case err != nil:
				g.failed = true
			case g.stopped:
				p.Close()
			default:
				p.SetMuted(true)
				p.SetLoop(true)
				p.SetFrameSize(image.Pt(maxPx, maxPx))
				g.p = p
			}
			if invalidate != nil {
				invalidate()
			}
		}()
	}
	g.used = ps.frame
	g.mu.Lock()
	p, failed := g.p, g.failed
	g.mu.Unlock()
	if !failed && p != nil && p.Status().Err != nil {
		failed = true
	}
	if failed {
		g.close()
		delete(ps.gifs, key)
		ps.gifFetch[key] = gifFailed
		return nil
	}
	if p == nil {
		return nil
	}
	p.SetFrameSize(image.Pt(maxPx, maxPx))
	if f, changed := p.Frame(); changed {
		g.frame, g.size = paint.NewImageOp(f), f.Rect.Size()
	}
	if g.size == (image.Point{}) {
		return nil
	}
	return &imgEntry{state: imgReady, op: g.frame, size: g.size, live: true}
}

// gifDownloaded lets a GIF whose download ended play, or keeps its play
// button when it failed.
func (u *UI) gifDownloaded(e model.MediaEvent) {
	ps := &u.players
	key := e.ChatID + "/" + e.MsgID
	switch {
	case !e.Failed:
		delete(ps.gifFetch, key) // also after the viewer downloaded it
	case ps.gifFetch[key] == gifFetching:
		ps.gifFetch[key] = gifFailed
	}
}
