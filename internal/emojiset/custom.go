package emojiset

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"io"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	xdraw "golang.org/x/image/draw"

	"github.com/chomosuke9/wazzapclients/internal/photo"
)

// A custom emoji pack is the user's own: PNGs named by code point, from a
// zip or a folder. Import copies its emojis, made the 2D font's size, into
// the store's "custom" folder, where Ready and Read find them as
// CustomPack. Nothing of it ever downloads, and Clear leaves it alone.

const (
	maxPackFiles = 50000    // looked at in a zip or folder
	maxPackPic   = 8 << 20  // bytes of a picture
	maxPackSide  = 2048     // pixels
	customTmp    = "import" // + random: a pack being imported
)

// ErrNoEmojis means a pack had none of the emojis asked for.
var ErrNoEmojis = errors.New("emojiset: no emojis in the pack")

// PackName returns the emoji's name (as Name words it) that a pack's file
// name stands for, or "": its code points in hex with any of the usual
// spellings. "01f44d_01f3fd.png", "1F44D-1F3FD.png" and
// "emoji_u1f44d_1f3fd.png" are all "1f44d_1f3fd".
func PackName(file string) string {
	s := strings.ToLower(path.Base(filepath.ToSlash(file)))
	s, ok := strings.CutSuffix(s, ".png")
	if !ok {
		return ""
	}
	s = strings.TrimPrefix(s, "emoji_")
	var seq []rune
	for _, f := range strings.FieldsFunc(s, func(r rune) bool { return r == '_' || r == '-' || r == ' ' }) {
		f = strings.TrimPrefix(strings.TrimPrefix(f, "u"), "+")
		n, err := strconv.ParseUint(f, 16, 32)
		if err != nil || len(f) < 2 || n > 0x10ffff {
			return ""
		}
		seq = append(seq, rune(n))
	}
	return Name(seq)
}

// packFile is one picture of a pack being imported.
type packFile struct {
	size int64
	open func() (io.ReadCloser, error)
}

// Import replaces the custom pack with the pictures of the one at path: a
// zip, or the folder path is, or that the picture path is in. Only the
// emojis wanted has count; of several pictures of one, the biggest file
// does. It returns how many it took, and keeps the old pack on an error.
func (s *Store) Import(src string, wanted []string) (int, error) {
	s.importMu.Lock()
	defer s.importMu.Unlock()
	want := make(map[string]bool, len(wanted))
	for _, n := range wanted {
		want[n] = true
	}
	files := map[string]packFile{}
	add := func(name string, f packFile) {
		n := PackName(name)
		if n == "" || !want[n] || f.size <= 0 || f.size > maxPackPic || f.size <= files[n].size {
			return
		}
		files[n] = f
	}
	if strings.EqualFold(filepath.Ext(src), ".zip") {
		z, err := zip.OpenReader(src)
		if err != nil {
			return 0, err
		}
		defer z.Close()
		for i, f := range z.File {
			if i >= maxPackFiles {
				break
			}
			if f.Mode().IsRegular() {
				add(f.Name, packFile{int64(min(f.UncompressedSize64, maxPackPic+1)), f.Open})
			}
		}
	} else {
		dir := src
		if fi, err := os.Stat(src); err != nil {
			return 0, err
		} else if !fi.IsDir() {
			dir = filepath.Dir(src)
		}
		list, err := os.ReadDir(dir)
		if err != nil {
			return 0, err
		}
		for _, e := range list[:min(len(list), maxPackFiles)] {
			fi, err := e.Info()
			if err != nil || !fi.Mode().IsRegular() {
				continue
			}
			p := filepath.Join(dir, e.Name())
			add(e.Name(), packFile{fi.Size(), func() (io.ReadCloser, error) { return os.Open(p) }})
		}
	}
	return s.replacePack(files)
}

// replacePack writes a pack's pictures to a folder of their own, which
// then takes the old pack's place.
func (s *Store) replacePack(files map[string]packFile) (int, error) {
	if len(files) == 0 {
		return 0, ErrNoEmojis
	}
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return 0, err
	}
	// What an import that was cut short left.
	old, _ := filepath.Glob(filepath.Join(s.dir, customTmp+"*"))
	for _, d := range old {
		os.RemoveAll(d)
	}
	tmp, err := os.MkdirTemp(s.dir, customTmp)
	if err != nil {
		return 0, err
	}
	defer os.RemoveAll(tmp)
	n := 0
	for name, f := range files {
		b, err := readPackFile(f)
		if err == nil {
			b, err = fitPic(b)
		}
		if err != nil {
			continue // a damaged picture; the 2D one shows
		}
		if err := os.WriteFile(filepath.Join(tmp, name+".png"), b, 0o600); err != nil {
			return 0, err
		}
		n++
	}
	if n == 0 {
		return 0, ErrNoEmojis
	}
	if err := s.swapPack(tmp); err != nil {
		return 0, err
	}
	return n, nil
}

func readPackFile(f packFile) ([]byte, error) {
	r, err := f.open()
	if err != nil {
		return nil, err
	}
	defer r.Close()
	// A zip's word for a file's size isn't to be trusted.
	b, err := io.ReadAll(io.LimitReader(r, maxPackPic+1))
	if err == nil && len(b) > maxPackPic {
		err = errors.New("emojiset: picture too big")
	}
	return b, err
}

// swapPack makes tmp the custom pack, or leaves none when tmp is "".
// s.importMu is held.
func (s *Store) swapPack(tmp string) error {
	dir := filepath.Join(s.dir, CustomPack.dir())
	// Renamed first and deleted at the end: thousands of files take a
	// while, and the UI waits for s.mu.
	old := filepath.Join(s.dir, customTmp+"-old")
	if err := os.RemoveAll(old); err != nil {
		return err
	}
	defer os.RemoveAll(old)

	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.Rename(dir, old); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if tmp != "" {
		if err := os.Rename(tmp, dir); err != nil {
			os.Rename(old, dir)
			return err
		}
	}
	for k := range s.known {
		if k.kind == CustomPack {
			delete(s.known, k)
		}
	}
	for k, b := range s.mem {
		if k.kind == CustomPack {
			s.memSize -= len(b)
			delete(s.mem, k)
		}
	}
	s.packCount = -1
	s.gen.Add(1)
	return nil
}

// RemovePack deletes the custom pack.
func (s *Store) RemovePack() error {
	s.importMu.Lock()
	defer s.importMu.Unlock()
	return s.swapPack("")
}

// PackCount is how many emojis the custom pack has.
func (s *Store) PackCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.packCount < 0 {
		list, _ := os.ReadDir(filepath.Join(s.dir, CustomPack.dir()))
		s.packCount = len(slices.DeleteFunc(list, func(e os.DirEntry) bool { return filepath.Ext(e.Name()) != ".png" }))
	}
	return s.packCount
}

// fitPic scales the picture in data to fit, whole and centered, the canvas
// the 2D font's pictures have (see pad).
func fitPic(data []byte) ([]byte, error) {
	cfg, err := png.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width > maxPackSide || cfg.Height > maxPackSide {
		return nil, fmt.Errorf("emojiset: a picture %dx%d", cfg.Width, cfg.Height)
	}
	src, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	if cfg.Width == padWidth && cfg.Height == padHeight {
		return data, nil
	}
	w, h := cfg.Width*padHeight/cfg.Height, padHeight
	if w > padWidth {
		w, h = padWidth, cfg.Height*padWidth/cfg.Width
	}
	w, h = max(w, 1), max(h, 1)
	var pic image.Image
	if w <= cfg.Width && h <= cfg.Height {
		pic = photo.Shrink(src, w, h)
	} else {
		dst := image.NewRGBA(image.Rect(0, 0, w, h))
		xdraw.CatmullRom.Scale(dst, dst.Bounds(), src, src.Bounds(), xdraw.Src, nil)
		pic = dst
	}
	dst := image.NewNRGBA(image.Rect(0, 0, padWidth, padHeight))
	at := image.Pt((padWidth-w)/2, (padHeight-h)/2)
	draw.Draw(dst, image.Rectangle{Min: at, Max: at.Add(image.Pt(w, h))}, pic, pic.Bounds().Min, draw.Src)
	var out bytes.Buffer
	if err := png.Encode(&out, dst); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}
