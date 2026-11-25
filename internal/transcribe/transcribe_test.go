package transcribe

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
)

func sum(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// archive packs names as whisper.cpp's builds are, for this system.
func archive(t *testing.T, names ...string) (name string, data []byte) {
	t.Helper()
	var buf bytes.Buffer
	if runtime.GOOS == "windows" {
		z := zip.NewWriter(&buf)
		for _, n := range names {
			w, _ := z.Create("Release/" + n)
			io.WriteString(w, n)
		}
		z.Close()
		return "e.zip", buf.Bytes()
	}
	gz := gzip.NewWriter(&buf)
	w := tar.NewWriter(gz)
	for _, n := range names {
		h := &tar.Header{Name: "whisper/" + n, Mode: 0o755, Size: int64(len(n)), Typeflag: tar.TypeReg}
		if from, to, ok := strings.Cut(n, " -> "); ok {
			h = &tar.Header{Name: "whisper/" + from, Linkname: to, Typeflag: tar.TypeSymlink}
		}
		if err := w.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		if h.Typeflag == tar.TypeReg {
			io.WriteString(w, n)
		}
	}
	w.Close()
	gz.Close()
	return "e.tar.gz", buf.Bytes()
}

// fake is an Engine whose files come from memory and whose program answers
// with out.
type fake struct {
	*Engine
	mu   sync.Mutex
	gets []string
	args [][]string
	out  string
}

func newFake(t *testing.T) *fake {
	t.Helper()
	eng, data := archive(t, program(), "whisper-server", "LICENSE", "libwhisper.so.1.9", "ggml.dll", "SDL2.dll")
	if runtime.GOOS != "windows" {
		eng, data = archive(t, program(), "whisper-server", "LICENSE", "libwhisper.so.1.9",
			"libwhisper.so.1 -> libwhisper.so.1.9", "libggml.so -> ../../evil", "libggml-base.so -> /etc/passwd")
	}
	oldEngines, oldVad, oldModels := engines, vad, Models
	t.Cleanup(func() { engines, vad, Models = oldEngines, oldVad, oldModels })
	engines = map[string]file{runtime.GOOS + "/" + runtime.GOARCH: {name: eng, size: int64(len(data)), sha256: sum(data)}}
	vad = file{url: "vad", name: "vad.bin", size: 3, sha256: sum([]byte("vad"))}
	Models = []Model{
		{"base", file{name: "base.bin", size: 4, sha256: sum([]byte("base"))}},
		{"small", file{name: "small.bin", size: 5, sha256: sum([]byte("small"))}},
	}
	f := &fake{Engine: New(filepath.Join(t.TempDir(), "transcripts"))}
	f.Get = func(_ context.Context, url string) (io.ReadCloser, error) {
		f.mu.Lock()
		defer f.mu.Unlock()
		name := url[strings.LastIndexByte(url, '/')+1:]
		f.gets = append(f.gets, name)
		body := map[string][]byte{eng: data, "vad": []byte("vad"), "base.bin": []byte("base"), "small.bin": []byte("small")}[name]
		return io.NopCloser(bytes.NewReader(body)), nil
	}
	f.Decode = func(_ context.Context, in, out string) error { return os.WriteFile(out, []byte("RIFF"), 0o600) }
	f.Run = func(_ context.Context, dir, program string, args []string) ([]byte, error) {
		f.mu.Lock()
		f.args = append(f.args, args)
		f.mu.Unlock()
		if _, err := os.Stat(program); err != nil {
			return nil, err
		}
		for i, a := range args {
			if a == "-m" || a == "-vm" || a == "-f" {
				if _, err := os.Stat(filepath.Join(dir, args[i+1])); err != nil {
					return nil, err
				}
			}
		}
		return []byte(f.out), nil
	}
	return f
}

func result(texts ...string) string {
	var b strings.Builder
	b.WriteString("{\n\t\"result\": {\n\t\t\"language\": \"id\"\n\t},\n\t\"transcription\": [\n")
	for i, s := range texts {
		if i > 0 {
			b.WriteString(",")
		}
		b.WriteString("{\"text\": \"" + s + "\"}")
	}
	b.WriteString("\n\t]\n}\n")
	return b.String()
}

func TestTranscribe(t *testing.T) {
	f := newFake(t)
	f.out = result(" Halo, apa kabar?", " Dia bilang \\\"iya\\\".")
	if st := f.Status("base"); st.Ready || st.Downloading {
		t.Fatalf("status before: %+v", st)
	}
	before := f.Need("base")
	got, err := f.Transcribe(context.Background(), "base", "id", "voice.ogg")
	if err != nil {
		t.Fatal(err)
	}
	if want := `Halo, apa kabar? Dia bilang "iya".`; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if before == 0 || f.Need("base") != 0 || !f.Status("base").Ready {
		t.Errorf("need %d then %d, status %+v", before, f.Need("base"), f.Status("base"))
	}
	// Once here, nothing downloads again; another model needs only itself.
	n := len(f.gets)
	if _, err := f.Transcribe(context.Background(), "base", Auto, "voice.ogg"); err != nil || len(f.gets) != n {
		t.Errorf("second run: %v, downloaded %v", err, f.gets[n:])
	}
	if got := f.Need("small"); got != 5 {
		t.Errorf("small needs %d bytes", got)
	}
	if l := f.args[1]; !slices.Contains(l, Auto) || f.args[0][slices.Index(f.args[0], "-l")+1] != "id" {
		t.Errorf("languages: %v", f.args)
	}
	// Only what runs was unpacked, and no audio stays behind.
	var names []string
	es, _ := os.ReadDir(f.engineDir())
	for _, e := range es {
		names = append(names, e.Name())
	}
	if runtime.GOOS != "windows" && !slices.Equal(names, []string{"LICENSE", "libwhisper.so.1", "libwhisper.so.1.9", "whisper-cli"}) {
		t.Errorf("unpacked %v", names)
	}
	if left, _ := filepath.Glob(filepath.Join(f.dir, "*-*")); len(left) != 1 {
		t.Errorf("left behind: %v", left)
	}
	if f.Size() == 0 {
		t.Error("nothing counted on disk")
	}
	if err := f.Clear(); err != nil || f.Size() != 0 || f.Status("base").Ready {
		t.Errorf("clear: %v, %d bytes, %+v", err, f.Size(), f.Status("base"))
	}
}

func TestDownloadRejectsTampered(t *testing.T) {
	f := newFake(t)
	real := f.Get
	f.Get = func(ctx context.Context, url string) (io.ReadCloser, error) {
		if strings.HasSuffix(url, "base.bin") {
			return io.NopCloser(strings.NewReader("evil")), nil
		}
		return real(ctx, url)
	}
	if _, err := f.Transcribe(context.Background(), "base", "id", "v.ogg"); err == nil {
		t.Fatal("a tampered model was used")
	}
	if st := f.Status("base"); st.Err == nil || st.Ready {
		t.Errorf("status %+v", st)
	}
	if _, err := os.Stat(filepath.Join(f.modelDir(), "base.bin")); err == nil {
		t.Error("the tampered model was kept")
	}
	if left, _ := filepath.Glob(filepath.Join(f.modelDir(), "*.part")); len(left) != 0 {
		t.Errorf("left %v", left)
	}
	// It's tried again the next time.
	f.Get = real
	f.out = result(" ya")
	if got, err := f.Transcribe(context.Background(), "base", "id", "v.ogg"); got != "ya" || err != nil {
		t.Errorf("retry: %q, %v", got, err)
	}
}

func TestCancel(t *testing.T) {
	f := newFake(t)
	started := make(chan struct{})
	f.Get = func(ctx context.Context, _ string) (io.ReadCloser, error) {
		close(started)
		<-ctx.Done()
		return nil, ctx.Err()
	}
	done := make(chan error, 1)
	go func() {
		_, err := f.Transcribe(context.Background(), "base", "id", "v.ogg")
		done <- err
	}()
	<-started
	if !f.Status("base").Downloading {
		t.Error("not downloading")
	}
	f.Cancel()
	if err := <-done; !errors.Is(err, ErrCanceled) {
		t.Errorf("err = %v", err)
	}
	if st := f.Status("base"); st.Err != nil || st.Downloading {
		t.Errorf("status %+v", st)
	}
}

func TestParse(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{result(), ""},
		{result(" [Musik]", " (tertawa)", " *batuk*", " ♪♪"), ""},
		{result(" [Musik]", " Oke"), "Oke"},
		{result(" (Halo) semua"), "(Halo) semua"},
		{"\n" + result(" a\tb"), "a b"},
	} {
		if got, err := parse([]byte(c.in)); err != nil || got != c.want {
			t.Errorf("parse(%q) = %q, %v; want %q", c.in, got, err, c.want)
		}
	}
	if _, err := parse([]byte("usage: whisper-cli")); err == nil {
		t.Error("usage parsed")
	}
}

func TestModels(t *testing.T) {
	if f := modelFile("nothing"); f.name != modelFile(DefaultModel).name || f.name != "ggml-small-q5_1.bin" {
		t.Errorf("unknown model is %q", f.name)
	}
	for _, id := range []string{"base", "small", "turbo"} {
		if f := modelFile(id); f.size == 0 || len(f.sha256) != 64 || !strings.HasSuffix(f.url, f.name) {
			t.Errorf("%s: %+v", id, f)
		}
	}
	seen := map[string]bool{}
	for _, l := range Languages {
		if seen[l.Code] || LanguageByCode(l.Code).Name != l.Name {
			t.Errorf("language %q", l.Code)
		}
		seen[l.Code] = true
	}
	if LanguageByCode(Auto) != nil {
		t.Error("auto is a language")
	}
}
