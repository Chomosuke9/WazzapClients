package emojiset

import (
	"bufio"
	"compress/flate"
	"encoding/binary"
	"errors"
	"image"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/chomosuke9/wazzapclients/internal/offheap"
)

// An animated emoji is decoded from its 512x512 WebP (about 50 frames,
// a quarter of a second of a core and about 90 MB of garbage) into a sheet
// of its frames at the size it shows at. The sheets are kept in the sheets
// folder, compressed (a text-sized one in about 50 KB), so an emoji seen
// before reads its sheet in a millisecond or so instead. The folder is kept
// under sheetsMax, the sheets read longest ago going first.

// Sheet is an animation's frames, Side pixels square, Cols to a row of Image.
type Sheet struct {
	Image  *image.RGBA // its pixels are outside the heap (offheap)
	Side   int
	Cols   int
	Delays []time.Duration
}

// sheetsMax is how many bytes of sheets are kept on disk. Tests lower it.
var sheetsMax int64 = 256 << 20

const (
	sheetsDir   = "sheets"
	sheetMagic  = "WZS1"
	maxSheetDim = 4096 // pixels across or down
	sheetLevel  = 6    // flate's: BestSpeed made them 15% bigger
)

// sheetFile is what the store knows of a sheet on disk.
type sheetFile struct {
	size int64
	used time.Time
}

func sheetName(name string, side int) string {
	return name + "-" + strconv.Itoa(side) + ".sheet"
}

func (s *Store) sheetPath(file string) string { return filepath.Join(s.dir, sheetsDir, file) }

// loadSheets lists the sheets folder the first time it's needed, and
// removes what an interrupted write left. s.mu is held.
func (s *Store) loadSheets() {
	if s.sheets != nil {
		return
	}
	s.sheets = map[string]sheetFile{}
	s.sheetsSize = 0
	es, _ := os.ReadDir(filepath.Join(s.dir, sheetsDir))
	for _, e := range es {
		fi, err := e.Info()
		if err != nil || e.IsDir() {
			continue
		}
		if !strings.HasSuffix(e.Name(), ".sheet") {
			os.Remove(s.sheetPath(e.Name()))
			continue
		}
		s.sheets[e.Name()] = sheetFile{fi.Size(), fi.ModTime()}
		s.sheetsSize += fi.Size()
	}
}

// HasSheet reports whether the sheet of name side pixels square is on
// disk. It reads no disk after the first call.
func (s *Store) HasSheet(name string, side int) bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.loadSheets()
	_, ok := s.sheets[sheetName(name, side)]
	return ok
}

var errSheet = errors.New("emojiset: bad sheet")

// ReadSheet reads the sheet of name side pixels square from disk.
func (s *Store) ReadSheet(name string, side int) (Sheet, bool) {
	if !s.HasSheet(name, side) {
		return Sheet{}, false
	}
	file := sheetName(name, side)
	sh, err := readSheet(s.sheetPath(file), side)
	s.mu.Lock()
	defer s.mu.Unlock()
	f, ok := s.sheets[file]
	if err != nil {
		// Damaged, or gone: Clear ran, or another account's window let
		// it go. Decoded again, it's written again.
		if ok {
			delete(s.sheets, file)
			s.sheetsSize -= f.size
		}
		os.Remove(s.sheetPath(file))
		return Sheet{}, false
	}
	if ok {
		// The modification time says when it was last read, for the next
		// run's order of eviction too.
		now := time.Now()
		f.used = now
		s.sheets[file] = f
		os.Chtimes(s.sheetPath(file), now, now)
	}
	return sh, true
}

// Readers and writers are reused: a flate writer at sheetLevel is about
// 1 MB, a reader about 40 KB.
var (
	sheetReaders = make(chan io.ReadCloser, 4)
	sheetWriters = make(chan *flate.Writer, 2)
	sheetBufs    = make(chan *bufio.Reader, 4)
)

func readSheet(path string, side int) (Sheet, error) {
	f, err := os.Open(path)
	if err != nil {
		return Sheet{}, err
	}
	defer f.Close()
	var br *bufio.Reader
	select {
	case br = <-sheetBufs:
		br.Reset(f)
	default:
		br = bufio.NewReaderSize(f, 32<<10)
	}
	defer func() {
		br.Reset(nil)
		select {
		case sheetBufs <- br:
		default:
		}
	}()
	var head [10]byte
	if _, err := io.ReadFull(br, head[:]); err != nil {
		return Sheet{}, err
	}
	n := int(binary.LittleEndian.Uint16(head[8:]))
	cols := int(binary.LittleEndian.Uint16(head[6:]))
	if string(head[:4]) != sheetMagic || int(binary.LittleEndian.Uint16(head[4:])) != side || n == 0 || cols == 0 {
		return Sheet{}, errSheet
	}
	rows := (n + cols - 1) / cols
	if cols*side > maxSheetDim || rows*side > maxSheetDim {
		return Sheet{}, errSheet
	}
	sh := Sheet{Side: side, Cols: cols, Delays: make([]time.Duration, n)}
	var d [2]byte
	for i := range sh.Delays {
		if _, err := io.ReadFull(br, d[:]); err != nil {
			return Sheet{}, err
		}
		sh.Delays[i] = time.Duration(binary.LittleEndian.Uint16(d[:])) * time.Millisecond
	}
	var zr io.ReadCloser
	select {
	case zr = <-sheetReaders:
		zr.(flate.Resetter).Reset(br, nil)
	default:
		zr = flate.NewReader(br)
	}
	defer func() {
		select {
		case sheetReaders <- zr:
		default:
		}
	}()
	sh.Image = offheap.NewRGBA(image.Rect(0, 0, cols*side, rows*side))
	if _, err := io.ReadFull(zr, sh.Image.Pix); err != nil {
		return Sheet{}, err
	}
	return sh, nil
}

// WriteSheet keeps sh on disk as the sheet of name, letting go of the
// sheets read longest ago when the folder is over sheetsMax.
func (s *Store) WriteSheet(name string, sh Sheet) {
	if s == nil || sh.Image == nil || sh.Side <= 0 || sh.Cols <= 0 || len(sh.Delays) == 0 || len(sh.Delays) > 0xffff {
		return
	}
	dir := filepath.Join(s.dir, sheetsDir)
	if os.MkdirAll(dir, 0o700) != nil {
		return
	}
	file := sheetName(name, sh.Side)
	// Written whole before it takes its name, so a half-written sheet is
	// never read; loadSheets removes one left behind.
	tmp, err := os.CreateTemp(dir, file+".*.part")
	if err != nil {
		return
	}
	err = writeSheet(tmp, sh)
	size, _ := tmp.Seek(0, io.SeekCurrent)
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(tmp.Name(), s.sheetPath(file))
	}
	if err != nil {
		os.Remove(tmp.Name())
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.loadSheets()
	if old, ok := s.sheets[file]; ok {
		s.sheetsSize -= old.size
	}
	s.sheets[file] = sheetFile{size, time.Now()}
	s.sheetsSize += size
	if s.sheetsSize > sheetsMax {
		s.evictSheets()
	}
}

func writeSheet(w io.Writer, sh Sheet) error {
	bw := bufio.NewWriterSize(w, 32<<10)
	head := make([]byte, 10, 10+2*len(sh.Delays))
	copy(head, sheetMagic)
	binary.LittleEndian.PutUint16(head[4:], uint16(sh.Side))
	binary.LittleEndian.PutUint16(head[6:], uint16(sh.Cols))
	binary.LittleEndian.PutUint16(head[8:], uint16(len(sh.Delays)))
	for _, d := range sh.Delays {
		head = binary.LittleEndian.AppendUint16(head, uint16(min(d.Milliseconds(), 0xffff)))
	}
	bw.Write(head)
	var zw *flate.Writer
	select {
	case zw = <-sheetWriters:
		zw.Reset(bw)
	default:
		zw, _ = flate.NewWriter(bw, sheetLevel)
	}
	defer func() {
		select {
		case sheetWriters <- zw:
		default:
		}
	}()
	if _, err := zw.Write(sh.Image.Pix); err != nil {
		return err
	}
	if err := zw.Close(); err != nil {
		return err
	}
	return bw.Flush()
}

// evictSheets removes the sheets read longest ago until the folder is
// three quarters of sheetsMax. s.mu is held.
func (s *Store) evictSheets() {
	files := make([]string, 0, len(s.sheets))
	for f := range s.sheets {
		files = append(files, f)
	}
	slices.SortFunc(files, func(a, b string) int { return s.sheets[a].used.Compare(s.sheets[b].used) })
	for _, f := range files {
		if s.sheetsSize <= sheetsMax*3/4 {
			break
		}
		os.Remove(s.sheetPath(f))
		s.sheetsSize -= s.sheets[f].size
		delete(s.sheets, f)
	}
}
