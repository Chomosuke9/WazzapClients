// Package transcribe turns voice messages into text on this computer, with
// OpenAI's Whisper as whisper.cpp runs it. Neither the program nor its
// models are bundled: they download once, pinned by their SHA-256, into a
// folder of the app's data. Each message runs the program once, so its
// memory (hundreds of MB) goes back to the system as it ends. The system's
// ffmpeg decodes the audio. No Gio here.
package transcribe

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

var (
	// ErrNoFFmpeg says ffmpeg, which decodes the audio, isn't on PATH.
	ErrNoFFmpeg = errors.New("transcribing needs ffmpeg, which isn't installed")
	// ErrCanceled says Cancel stopped it.
	ErrCanceled = errors.New("canceled")
)

// maxTime ends a transcription that hangs.
const maxTime = 20 * time.Minute

// Supported reports whether there is an engine for this computer.
func Supported() bool {
	_, ok := engineFile()
	return ok
}

// Status is how far a model (with the engine) is.
type Status struct {
	Ready       bool  // on disk
	Downloading bool  // and how far:
	Done, Total int64 // bytes
	Err         error // the last download's, if it failed
}

// download is a model on its way, with what else is missing.
type download struct {
	done, total int64
	err         error
	finished    chan struct{}
	cancel      context.CancelFunc
}

// Engine keeps the downloaded files in a folder and runs them. Its methods
// are safe from any goroutine.
type Engine struct {
	// Get opens a URL; tests replace it.
	Get func(ctx context.Context, url string) (io.ReadCloser, error)
	// Run runs the engine's program with dir as its working directory and
	// returns what it writes; tests replace it.
	Run func(ctx context.Context, dir, program string, args []string) ([]byte, error)
	// Decode writes the audio file in as a 16 kHz mono WAV; tests replace it.
	Decode func(ctx context.Context, in, out string) error

	dir  string
	turn chan struct{} // one transcription at a time

	mu     sync.Mutex
	loads  map[string]*download // by model ID
	ready  map[string]bool      // known to be on disk
	notify func()
	told   time.Time // when a download's progress was last announced
}

// New returns an engine that keeps its files in dir, which it creates when
// the first one arrives.
func New(dir string) *Engine {
	// What a crash left behind: a job's folder holds its audio.
	for _, pat := range []string{"job-*", "unpack-*", "*.part", "models/*.part"} {
		old, _ := filepath.Glob(filepath.Join(dir, pat))
		for _, p := range old {
			if fi, err := os.Stat(p); err == nil && time.Since(fi.ModTime()) > 24*time.Hour {
				os.RemoveAll(p)
			}
		}
	}
	return &Engine{
		Get: get, Run: run, Decode: decode,
		dir: dir, turn: make(chan struct{}, 1), loads: map[string]*download{}, ready: map[string]bool{},
	}
}

// SetNotify sets what is called, from any goroutine, as downloads go on
// and end.
func (e *Engine) SetNotify(fn func()) {
	e.mu.Lock()
	e.notify = fn
	e.mu.Unlock()
}

func (e *Engine) engineDir() string { return filepath.Join(e.dir, "whisper-"+release) }
func (e *Engine) modelDir() string  { return filepath.Join(e.dir, "models") }

// program is the engine's executable.
func program() string {
	if runtime.GOOS == "windows" {
		return "whisper-cli.exe"
	}
	return "whisper-cli"
}

// missing lists what model id still needs. The engine counts as here with
// its program, which is put in place last.
func (e *Engine) missing(id string) (needsEngine bool, models []file) {
	if _, err := os.Stat(filepath.Join(e.engineDir(), program())); err != nil {
		needsEngine = true
	}
	for _, f := range []file{vad, modelFile(id)} {
		// Files take their name whole and checked, so their size will do.
		if fi, err := os.Stat(filepath.Join(e.modelDir(), f.name)); err != nil || fi.Size() != f.size {
			models = append(models, f)
		}
	}
	return
}

// Need is how many bytes model id would download.
func (e *Engine) Need(id string) int64 {
	var n int64
	needsEngine, models := e.missing(id)
	if f, ok := engineFile(); ok && needsEngine {
		n += f.size
	}
	for _, f := range models {
		n += f.size
	}
	return n
}

// Status reports how far model id is.
func (e *Engine) Status(id string) Status {
	e.mu.Lock()
	defer e.mu.Unlock()
	if d := e.loads[id]; d != nil {
		select {
		case <-d.finished:
			if d.err != nil {
				return Status{Err: d.err}
			}
		default:
			return Status{Downloading: true, Done: d.done, Total: d.total}
		}
	}
	if !e.ready[id] {
		needsEngine, models := e.missing(id)
		e.ready[id] = !needsEngine && len(models) == 0
	}
	return Status{Ready: e.ready[id]}
}

// Download starts downloading what model id needs, unless it's here or on
// its way.
func (e *Engine) Download(id string) { e.start(id) }

// start is Download, returning the download there is, or nil when there's
// nothing to wait for.
func (e *Engine) start(id string) *download {
	e.mu.Lock()
	defer e.mu.Unlock()
	if d := e.loads[id]; d != nil {
		select {
		case <-d.finished:
		default:
			return d
		}
	}
	needsEngine, models := e.missing(id)
	if !needsEngine && len(models) == 0 {
		delete(e.loads, id)
		return nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	d := &download{finished: make(chan struct{}), cancel: cancel}
	eng, ok := engineFile()
	if needsEngine {
		d.total += eng.size
	}
	for _, f := range models {
		d.total += f.size
	}
	e.loads[id] = d
	go func() {
		err := errors.New("transcripts aren't available on this system")
		if ok {
			err = e.fetchAll(ctx, d, needsEngine, eng, models)
		}
		if err != nil && ctx.Err() != nil {
			err = ErrCanceled
		}
		cancel()
		e.mu.Lock()
		d.err = err
		if err == ErrCanceled && e.loads[id] == d {
			delete(e.loads, id) // not a failure to show
		}
		close(d.finished)
		notify := e.notify
		e.mu.Unlock()
		if notify != nil {
			notify()
		}
	}()
	return d
}

// Cancel stops every download. What's transcribing goes on.
func (e *Engine) Cancel() {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, d := range e.loads {
		d.cancel()
	}
}

func (e *Engine) fetchAll(ctx context.Context, d *download, needsEngine bool, eng file, models []file) error {
	for _, dir := range []string{e.dir, e.modelDir()} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
	}
	if needsEngine {
		arc := filepath.Join(e.dir, eng.name)
		defer os.Remove(arc)
		if err := e.fetch(ctx, d, eng, arc); err != nil {
			return err
		}
		if err := e.unpack(arc); err != nil {
			return err
		}
	}
	for _, f := range models {
		if err := e.fetch(ctx, d, f, filepath.Join(e.modelDir(), f.name)); err != nil {
			return err
		}
	}
	return nil
}

// fetch downloads f to path, which it takes only once it's whole and its
// SHA-256 is right.
func (e *Engine) fetch(ctx context.Context, d *download, f file, path string) (err error) {
	body, err := e.Get(ctx, f.url)
	if err != nil {
		return err
	}
	defer body.Close()
	// A name of its own: two models downloading at once may both need f.
	tmp, err := os.CreateTemp(filepath.Dir(path), f.name+".*.part")
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			tmp.Close()
			os.Remove(tmp.Name())
		}
	}()
	sum := sha256.New()
	n, err := io.Copy(io.MultiWriter(tmp, sum, progress{e, d}), io.LimitReader(body, f.size+1))
	if err != nil {
		return err
	}
	if n != f.size || hex.EncodeToString(sum.Sum(nil)) != f.sha256 {
		return fmt.Errorf("%s isn't what it should be", f.name)
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// progress counts a download's bytes.
type progress struct {
	e *Engine
	d *download
}

func (p progress) Write(b []byte) (int, error) {
	p.e.mu.Lock()
	p.d.done += int64(len(b))
	var notify func()
	if now := time.Now(); now.Sub(p.e.told) > 200*time.Millisecond {
		p.e.told = now
		notify = p.e.notify
	}
	p.e.mu.Unlock()
	if notify != nil {
		notify()
	}
	return len(b), nil
}

// wanted reports whether the archive's file name is part of what runs: the
// program, its libraries and its license. The other programs stay out.
func wanted(name string) bool {
	switch {
	case name == program(), name == "LICENSE":
		return true
	case runtime.GOOS == "windows":
		switch name {
		case "ggml.dll", "ggml-base.dll", "whisper.dll":
			return true
		}
		return strings.HasPrefix(name, "ggml-cpu") && strings.HasSuffix(name, ".dll")
	}
	return (strings.HasPrefix(name, "libwhisper.so") || strings.HasPrefix(name, "libggml")) && strings.Contains(name, ".so")
}

// unpack puts the engine in the archive arc in place. Everything in it
// lands in one folder, under its own name.
func (e *Engine) unpack(arc string) error {
	tmp, err := os.MkdirTemp(e.dir, "unpack-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	if strings.HasSuffix(arc, ".zip") {
		err = unzip(arc, tmp)
	} else {
		err = untar(arc, tmp)
	}
	if err != nil {
		return err
	}
	if _, err := os.Stat(filepath.Join(tmp, program())); err != nil {
		return errors.New("the download holds no " + program())
	}
	if _, err := os.Stat(filepath.Join(e.engineDir(), program())); err == nil {
		return nil // another model's download was faster
	}
	if err := os.RemoveAll(e.engineDir()); err != nil {
		return err
	}
	return os.Rename(tmp, e.engineDir())
}

func put(dir, name string, r io.Reader) error {
	f, err := os.OpenFile(filepath.Join(dir, name), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o700)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, r); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

func unzip(arc, dir string) error {
	z, err := zip.OpenReader(arc)
	if err != nil {
		return err
	}
	defer z.Close()
	for _, f := range z.File {
		name := path.Base(f.Name)
		if !f.Mode().IsRegular() || !wanted(name) {
			continue
		}
		r, err := f.Open()
		if err != nil {
			return err
		}
		err = put(dir, name, r)
		r.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

func untar(arc, dir string) error {
	f, err := os.Open(arc)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	for t := tar.NewReader(gz); ; {
		h, err := t.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		name := path.Base(h.Name)
		if !wanted(name) {
			continue
		}
		switch h.Typeflag {
		case tar.TypeReg:
			err = put(dir, name, t)
		case tar.TypeSymlink: // a library's version: "libggml.so.1" is "libggml.so.1.0.0"
			if h.Linkname != path.Base(h.Linkname) || !wanted(h.Linkname) {
				continue
			}
			err = os.Symlink(h.Linkname, filepath.Join(dir, name))
		}
		if err != nil {
			return err
		}
	}
}

// Check reports what's missing that doesn't download: ffmpeg.
func (e *Engine) Check() error {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		return ErrNoFFmpeg
	}
	return nil
}

// Transcribe returns what is said in an audio file, in language lang (or
// Auto), or "" when there are no words in it. It downloads what model id
// needs first, and waits for its turn.
func (e *Engine) Transcribe(ctx context.Context, id, lang, audio string) (string, error) {
	if d := e.start(id); d != nil {
		select {
		case <-d.finished:
			if d.err != nil {
				return "", d.err
			}
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	select {
	case e.turn <- struct{}{}:
		defer func() { <-e.turn }()
	case <-ctx.Done():
		return "", ctx.Err()
	}
	ctx, cancel := context.WithTimeout(ctx, maxTime)
	defer cancel()
	tmp, err := os.MkdirTemp(e.dir, "job-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmp)
	wav := filepath.Join(tmp, "voice.wav")
	if err := e.Decode(ctx, audio, wav); err != nil {
		return "", err
	}
	// Paths from the engine's folder: the program may not read a home
	// folder's accents on Windows.
	model := func(f file) string { return filepath.Join("..", "models", f.name) }
	out, err := e.Run(ctx, e.engineDir(), filepath.Join(e.engineDir(), program()), []string{
		"-m", model(modelFile(id)), "--vad", "-vm", model(vad),
		"-l", lang, "-t", strconv.Itoa(threads()), "-sns", "-np",
		"-oj", "-of", "-", "-f", filepath.Join("..", filepath.Base(tmp), "voice.wav"),
	})
	if err != nil {
		return "", err
	}
	return parse(out)
}

// threads leaves half of the computer alone.
func threads() int { return min(8, max(1, runtime.NumCPU()/2)) }

// noise is what Whisper writes for what isn't speech: "[Music]",
// "(laughs)", "*sighs*", "♪".
var noise = regexp.MustCompile(`^\s*(\[[^\]]*\]|\([^)]*\)|\*[^*]*\*|[♪♫\s]*)\s*[.…]*\s*$`)

// parse reads the text out of the engine's JSON.
func parse(out []byte) (string, error) {
	if i := bytes.IndexByte(out, '{'); i > 0 {
		out = out[i:]
	}
	// It escapes quotes and backslashes alone.
	out = bytes.Map(func(r rune) rune {
		if r < ' ' {
			return ' '
		}
		return r
	}, out)
	var res struct {
		Transcription []struct {
			Text string `json:"text"`
		} `json:"transcription"`
	}
	if err := json.Unmarshal(out, &res); err != nil {
		return "", fmt.Errorf("the speech engine's answer can't be read: %w", err)
	}
	var b strings.Builder
	for _, s := range res.Transcription {
		if !noise.MatchString(s.Text) {
			b.WriteString(s.Text)
		}
	}
	return strings.Join(strings.Fields(b.String()), " "), nil
}

// Size is how many bytes the downloaded files take.
func (e *Engine) Size() int64 {
	var n int64
	filepath.WalkDir(e.dir, func(_ string, d os.DirEntry, err error) error {
		if err == nil && d.Type().IsRegular() {
			if fi, err := d.Info(); err == nil {
				n += fi.Size()
			}
		}
		return nil
	})
	return n
}

// Clear deletes the downloaded files, waiting for a transcription to end.
func (e *Engine) Clear() error {
	e.Cancel()
	e.turn <- struct{}{}
	defer func() { <-e.turn }()
	e.mu.Lock()
	defer e.mu.Unlock()
	clear(e.ready)
	for id, d := range e.loads {
		select {
		case <-d.finished:
			delete(e.loads, id)
		default:
		}
	}
	return os.RemoveAll(e.dir)
}

var client = &http.Client{Timeout: 2 * time.Hour} // the biggest model on a slow line

func get(ctx context.Context, url string) (io.ReadCloser, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, errors.New("the server answered " + resp.Status)
	}
	return resp.Body, nil
}

// decode has ffmpeg write in as the WAV Whisper wants.
func decode(ctx context.Context, in, out string) error {
	ff, err := exec.LookPath("ffmpeg")
	if err != nil {
		return ErrNoFFmpeg
	}
	cmd := exec.CommandContext(ctx, ff, "-hide_banner", "-loglevel", "error", "-nostdin", "-y",
		"-i", "file:"+in, "-vn", "-ac", "1", "-ar", "16000", "-c:a", "pcm_s16le", "-f", "wav", out)
	background(cmd)
	if b, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("ffmpeg couldn't read the voice message: %s", lastLine(b, err))
	}
	return nil
}

func run(ctx context.Context, dir, program string, args []string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, program, args...)
	cmd.Dir = dir
	background(cmd)
	if runtime.GOOS != "windows" {
		cmd.Env = append(os.Environ(), "LD_LIBRARY_PATH="+dir)
	}
	var out, errs bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errs
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("the speech engine failed: %s", lastLine(errs.Bytes(), err))
	}
	return out.Bytes(), nil
}

// lastLine is the last line a program wrote, or else err.
func lastLine(b []byte, err error) string {
	s := strings.TrimSpace(string(b))
	if i := strings.LastIndexByte(s, '\n'); i >= 0 {
		s = strings.TrimSpace(s[i+1:])
	}
	if s == "" {
		return err.Error()
	}
	return s
}
