package emojiset

import (
	"archive/zip"
	"bytes"
	"errors"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

func TestPackName(t *testing.T) {
	for in, want := range map[string]string{
		"1f600.png":                "1f600",
		"01f600.png":               "1f600",
		"a/b/1F600.PNG":            "1f600",
		"emoji_u1f44d_1f3fd.png":   "1f44d_1f3fd",
		"1f44d-1f3fd.png":          "1f44d_1f3fd",
		"U+1F44D U+1F3FD.png":      "1f44d_1f3fd",
		"002764_00fe0f.png":        "2764",
		"23-20e3.png":              "0023_20e3",
		"a9.png":                   "00a9",
		"000023_00fe0f_0020e3.png": "0023_20e3",
		"._1f600.png":              "",
		"readme.png":               "",
		"1f600.webp":               "",
		"1f600(1).png":             "",
		"ffffffff.png":             "",
		".png":                     "",
	} {
		if got := PackName(in); got != want {
			t.Errorf("PackName(%q) = %q, want %q", in, got, want)
		}
	}
}

func picture(w, h int) []byte {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for i := range img.Pix {
		img.Pix[i] = 255
	}
	var b bytes.Buffer
	png.Encode(&b, img)
	return b.Bytes()
}

func writeZip(t *testing.T, files map[string][]byte) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "pack.zip")
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	z := zip.NewWriter(f)
	for n, b := range files {
		w, _ := z.Create(n)
		w.Write(b)
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	return p
}

// TestImportZip: a zip's emojis take the pack's place, whatever their size
// and folder; what isn't an emoji or doesn't decode is left out.
func TestImportZip(t *testing.T) {
	s := New(t.TempDir())
	s.Fetch = func(string) ([]byte, int, error) { t.Error("downloaded"); return nil, 0, errors.New("no") }
	names := []string{"1f600", "1f603", "2764", "1f44d"}
	gen := s.Gen()
	n, err := s.Import(writeZip(t, map[string][]byte{
		"png/32/1f600.png":     picture(32, 32),
		"png/160/1f600.png":    picture(160, 160),
		"png/2764-fe0f.png":    picture(300, 100),
		"png/1f44d.png":        []byte("not a picture"),
		"../../../1f603.png":   picture(72, 72),
		"png/face.png":         picture(72, 72),
		"LICENSE":              []byte("x"),
		"__MACOSX/._1f44d.png": []byte("x"),
	}), names)
	if err != nil || n != 3 || s.PackCount() != 3 || s.Gen() == gen {
		t.Fatalf("took %d (%d on disk), %v, want 3", n, s.PackCount(), err)
	}
	for _, name := range []string{"1f600", "1f603", "2764"} {
		cfg, err := png.DecodeConfig(bytes.NewReader(s.Read(CustomPack, name)))
		if err != nil || cfg.Width != padWidth || cfg.Height != padHeight {
			t.Errorf("%s: %dx%d, %v", name, cfg.Width, cfg.Height, err)
		}
	}
	if s.Ready(CustomPack, "1f44d") || s.Pending() != 0 {
		t.Error("a damaged picture counts, or something downloads")
	}

	// A pack without emojis, or no zip at all, leaves the old one.
	if _, err := s.Import(writeZip(t, map[string][]byte{"a.txt": []byte("x")}), names); !errors.Is(err, ErrNoEmojis) {
		t.Errorf("an empty pack: %v", err)
	}
	bad := filepath.Join(t.TempDir(), "bad.zip")
	os.WriteFile(bad, []byte("not a zip"), 0o600)
	if _, err := s.Import(bad, names); err == nil {
		t.Error("a file that is no zip imported")
	}
	if s.PackCount() != 3 || s.Read(CustomPack, "1f600") == nil {
		t.Error("a failed import took the pack")
	}

	// Another one replaces it whole.
	if n, err := s.Import(writeZip(t, map[string][]byte{"1f44d.png": picture(64, 64)}), names); n != 1 || err != nil {
		t.Fatalf("the second pack: %d, %v", n, err)
	}
	if s.Read(CustomPack, "1f600") != nil || s.Read(CustomPack, "1f44d") == nil {
		t.Error("the packs mixed")
	}
	left, _ := os.ReadDir(s.Dir())
	if len(left) != 1 {
		t.Errorf("imports left %d things besides the pack", len(left)-1)
	}
	if err := s.RemovePack(); err != nil || s.PackCount() != 0 || s.Ready(CustomPack, "1f44d") {
		t.Errorf("the pack stayed: %v", err)
	}
}

// TestImportHuge: a picture too big to be an emoji isn't decoded.
func TestImportHuge(t *testing.T) {
	s := New(t.TempDir())
	if _, err := s.Import(writeZip(t, map[string][]byte{"1f600.png": picture(maxPackSide+1, 1)}), []string{"1f600"}); !errors.Is(err, ErrNoEmojis) {
		t.Errorf("imported it: %v", err)
	}
}
