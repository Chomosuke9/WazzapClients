// Package emojiset downloads the emoji pictures that aren't bundled with
// the app: Noto's 3D emojis and its animated ones. The bundled font is the
// 2D set; the others would add hundreds of megabytes (the 3D font alone is
// 148 MB), so each picture is fetched the first time it shows and kept in
// a folder of the app's data. No Gio here.
package emojiset

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Kind is a set of pictures.
type Kind uint8

const (
	// ThreeD is Noto's 3D emojis, PNGs 128 pixels square, from the 3dpng
	// branch of github.com/googlefonts/noto-emoji (pinned to a commit).
	ThreeD Kind = iota + 1
	// Animated is Noto's animated emojis, animated WebPs 512 pixels square
	// from Google Fonts. Not every emoji has one.
	Animated
)

const (
	threeDURL   = "https://raw.githubusercontent.com/googlefonts/noto-emoji/d6a792cb12e3eb7224f4fbf13173a0dded455651/3D/png/128/emoji_u%s.png"
	animatedURL = "https://fonts.gstatic.com/s/e/notoemoji/latest/%s/512.webp"

	maxFile  = 4 << 20 // bigger isn't an emoji
	workers  = 4
	retryIn  = time.Minute // after a network error
	memLimit = 2 << 20     // bytes of 3D pictures kept in memory
)

// dir names each kind's folder (and what Kind.String says).
func (k Kind) dir() string {
	if k == Animated {
		return "animated"
	}
	return "3d"
}

func (k Kind) url(name string) string {
	if k == Animated {
		return fmt.Sprintf(animatedURL, name)
	}
	return fmt.Sprintf(threeDURL, name)
}

// valid reports whether data looks like a picture of kind k.
func (k Kind) valid(data []byte) bool {
	if k == Animated {
		return len(data) > 12 && string(data[:4]) == "RIFF" && string(data[8:12]) == "WEBP"
	}
	return bytes.HasPrefix(data, []byte("\x89PNG\r\n\x1a\n"))
}

// Name is the name Noto gives an emoji's files: its code points in hex
// (at least four digits), joined by _, without variation selector 16.
// "❤️" is "2764", "👍🏽" is "1f44d_1f3fd" and "#️⃣" is "0023_20e3".
func Name(seq []rune) string {
	var b strings.Builder
	for _, r := range seq {
		if r == 0xfe0f {
			continue
		}
		if b.Len() > 0 {
			b.WriteByte('_')
		}
		fmt.Fprintf(&b, "%04x", r)
	}
	return b.String()
}

type key struct {
	kind Kind
	name string
}

type state uint8

const (
	unknown state = iota
	onDisk
	missing // the set has no such picture (404), remembered on disk
	queued
	failed // a network error; tried again after retryIn
)

type entry struct {
	state state
	retry time.Time // when failed
}

// Store keeps the downloaded pictures in a folder, one per kind, and
// downloads the ones asked for that it doesn't have. Its methods are safe
// from any goroutine.
type Store struct {
	// Fetch downloads a URL: its body and HTTP status. New sets it to an
	// HTTP GET; tests replace it before the first Ready.
	Fetch func(url string) (body []byte, status int, err error)

	dir string

	mu      sync.Mutex
	known   map[key]*entry
	queue   []key // newest last: the newest asked are fetched first
	running int
	notify  func()
	mem     map[key][]byte // recently read 3D pictures
	memSize int

	gen atomic.Uint64
}

// New returns a store of the pictures in dir, which it creates when the
// first one arrives.
func New(dir string) *Store {
	client := &http.Client{Timeout: 30 * time.Second}
	return &Store{dir: dir, known: map[key]*entry{}, mem: map[key][]byte{}, Fetch: func(url string) ([]byte, int, error) {
		resp, err := client.Get(url)
		if err != nil {
			return nil, 0, err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return nil, resp.StatusCode, nil
		}
		b, err := io.ReadAll(io.LimitReader(resp.Body, maxFile+1))
		if err == nil && len(b) > maxFile {
			err = errors.New("emojiset: file too big")
		}
		return b, resp.StatusCode, err
	}}
}

// SetNotify sets what is called (from a downloading goroutine) when a
// picture arrives.
func (s *Store) SetNotify(fn func()) {
	s.mu.Lock()
	s.notify = fn
	s.mu.Unlock()
}

// Gen counts the pictures that arrived. It changes when Ready may say
// otherwise than before.
func (s *Store) Gen() uint64 { return s.gen.Load() }

// Dir is the folder the pictures are kept in.
func (s *Store) Dir() string { return s.dir }

func (s *Store) path(k key) string { return filepath.Join(s.dir, k.kind.dir(), k.name+ext(k.kind)) }

// missing pictures are remembered by an empty file of this name.
func (s *Store) nonePath(k key) string { return filepath.Join(s.dir, k.kind.dir(), k.name+".none") }

func ext(k Kind) string {
	if k == Animated {
		return ".webp"
	}
	return ".png"
}

// Ready reports whether the picture of name is on disk. When it isn't, and
// the set may have it, it is downloaded (Gen changes when it arrives).
func (s *Store) Ready(kind Kind, name string) bool {
	if name == "" || s == nil {
		return false
	}
	k := key{kind, name}
	s.mu.Lock()
	defer s.mu.Unlock()
	e := s.known[k]
	if e == nil {
		e = &entry{}
		s.known[k] = e
		if _, err := os.Stat(s.path(k)); err == nil {
			e.state = onDisk
		} else if _, err := os.Stat(s.nonePath(k)); err == nil {
			e.state = missing
		}
	}
	switch e.state {
	case onDisk:
		return true
	case unknown:
		s.enqueue(k, e)
	case failed:
		if time.Now().After(e.retry) {
			s.enqueue(k, e)
		}
	}
	return false
}

// Read returns the picture of name when it is on disk (see Ready), or nil.
// 3D pictures that were read lately come from memory.
func (s *Store) Read(kind Kind, name string) []byte {
	if !s.Ready(kind, name) {
		return nil
	}
	k := key{kind, name}
	s.mu.Lock()
	b := s.mem[k]
	s.mu.Unlock()
	if b != nil {
		return b
	}
	b, err := os.ReadFile(s.path(k))
	if err != nil || !kind.valid(b) {
		return nil
	}
	if kind == ThreeD {
		s.mu.Lock()
		if s.memSize+len(b) > memLimit {
			clear(s.mem)
			s.memSize = 0
		}
		s.mem[k] = b
		s.memSize += len(b)
		s.mu.Unlock()
	}
	return b
}

// enqueue queues k's download, starting a worker when fewer than workers
// run. s.mu is held.
func (s *Store) enqueue(k key, e *entry) {
	e.state = queued
	s.queue = append(s.queue, k)
	if s.running < workers {
		s.running++
		go s.work()
	}
}

func (s *Store) work() {
	for {
		s.mu.Lock()
		if len(s.queue) == 0 {
			s.running--
			s.mu.Unlock()
			return
		}
		k := s.queue[len(s.queue)-1]
		s.queue = s.queue[:len(s.queue)-1]
		s.mu.Unlock()

		st := s.download(k)

		s.mu.Lock()
		e := s.known[k]
		e.state = st
		if st == failed {
			e.retry = time.Now().Add(retryIn)
		}
		notify := s.notify
		s.mu.Unlock()
		if st == onDisk {
			s.gen.Add(1)
			if notify != nil {
				notify()
			}
		}
	}
}

// download fetches k into its file.
func (s *Store) download(k key) state {
	b, status, err := s.Fetch(k.kind.url(k.name))
	switch {
	case err != nil:
		return failed
	case status == http.StatusNotFound:
		if os.MkdirAll(filepath.Dir(s.nonePath(k)), 0o700) == nil {
			os.WriteFile(s.nonePath(k), nil, 0o600)
		}
		return missing
	case status != http.StatusOK || !k.kind.valid(b):
		return failed
	}
	if k.kind == ThreeD {
		if b = pad(b); b == nil {
			return failed
		}
	}
	if err := os.MkdirAll(filepath.Dir(s.path(k)), 0o700); err != nil {
		return failed
	}
	// Written whole before it takes its name, so a half-written file is
	// never read.
	tmp := s.path(k) + ".part"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return failed
	}
	if err := os.Rename(tmp, s.path(k)); err != nil {
		os.Remove(tmp)
		return failed
	}
	return onDisk
}

// Size returns how many bytes the downloaded pictures take on disk.
func (s *Store) Size() int64 {
	var n int64
	filepath.WalkDir(s.dir, func(_ string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			if fi, err := d.Info(); err == nil {
				n += fi.Size()
			}
		}
		return nil
	})
	return n
}

// Clear deletes every downloaded picture. Those still shown download again.
func (s *Store) Clear() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, k := range []Kind{ThreeD, Animated} {
		if err := os.RemoveAll(filepath.Join(s.dir, k.dir())); err != nil {
			return err
		}
	}
	for k, e := range s.known {
		if e.state != queued {
			delete(s.known, k)
		}
	}
	clear(s.mem)
	s.memSize = 0
	s.gen.Add(1)
	return nil
}

// The bundled 2D font's pictures are 136x128 (the emoji 128 high), and Gio
// stretches a glyph's picture over the glyph's box. The 3D ones (128x128)
// are kept centered on a 136x128 canvas so they take the 2D ones' place
// without stretching.
const padWidth, padHeight = 136, 128

// pad returns PNG data of a picture padHeight high centered on a
// padWidth x padHeight canvas, other pictures as they are, or nil when the
// data doesn't decode.
func pad(data []byte) []byte {
	src, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return nil
	}
	b := src.Bounds()
	if b.Dy() != padHeight || b.Dx() >= padWidth {
		return data
	}
	dst := image.NewNRGBA(image.Rect(0, 0, padWidth, padHeight))
	at := image.Pt((padWidth-b.Dx())/2, 0)
	draw.Draw(dst, image.Rectangle{Min: at, Max: at.Add(b.Size())}, src, b.Min, draw.Src)
	var out bytes.Buffer
	if err := (&png.Encoder{CompressionLevel: png.BestSpeed}).Encode(&out, dst); err != nil {
		return nil
	}
	return out.Bytes()
}
