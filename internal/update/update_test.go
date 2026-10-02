package update

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestParseVersion(t *testing.T) {
	for _, tc := range []struct {
		a, b  string
		newer bool
	}{
		{"v0.10.0", "v0.9.0", true},
		{"v1.0.0", "v0.99.99", true},
		{"v0.9.1", "v0.9.0", true},
		{"v0.9.0", "v0.9.0", false},
		{"v0.8.5", "v0.9.0", false},
	} {
		a, ok1 := parseVersion(tc.a)
		b, ok2 := parseVersion(tc.b)
		if !ok1 || !ok2 || newer(a, b) != tc.newer {
			t.Errorf("newer(%s, %s) = %v, want %v", tc.a, tc.b, newer(a, b), tc.newer)
		}
	}
	for _, s := range []string{"", "dev", "v1.2", "v1.2.3-rc1", "v1.x.3"} {
		if _, ok := parseVersion(s); ok {
			t.Errorf("parseVersion(%q) parsed", s)
		}
	}
}

// release serves a fake latest release with exe as this system's build,
// signed with a new key that publicKey is set to.
func release(t *testing.T, tag string, exe []byte, tamper bool) {
	t.Helper()
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	oldKey, oldURL := publicKey, apiURL
	publicKey = pub
	t.Cleanup(func() { publicKey, apiURL = oldKey, oldURL })

	sum := sha256.Sum256(exe)
	sums := fmt.Sprintf("%s  other-file\n%s  %s\n", hex.EncodeToString(make([]byte, 32)), hex.EncodeToString(sum[:]), AssetName())
	sig := base64.StdEncoding.EncodeToString(ed25519.Sign(priv, []byte(sums)))
	if tamper {
		exe = append([]byte("evil"), exe...)
	}
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/latest":
			fmt.Fprintf(w, `{"tag_name":%q,"html_url":"https://example.com","assets":[
				{"name":%q,"browser_download_url":"%s/exe","size":%d},
				{"name":"SHA256SUMS","browser_download_url":"%s/sums"},
				{"name":"SHA256SUMS.sig","browser_download_url":"%s/sig"}]}`,
				tag, AssetName(), srv.URL, len(exe), srv.URL, srv.URL)
		case "/exe":
			w.Write(exe)
		case "/sums":
			w.Write([]byte(sums))
		case "/sig":
			w.Write([]byte(sig))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	apiURL = srv.URL + "/latest"
}

func TestCheckAndDownload(t *testing.T) {
	exe := []byte("new executable")
	release(t, "v0.10.0", exe, false)
	ctx := context.Background()

	if r, err := Check(ctx, "v0.10.0"); err != nil || r != nil {
		t.Fatalf("Check from the same version = %v, %v; want nil, nil", r, err)
	}
	if _, err := Check(ctx, "dev"); !errors.Is(err, ErrDevBuild) {
		t.Fatalf("Check from a dev build = %v, want ErrDevBuild", err)
	}
	r, err := Check(ctx, "v0.9.0")
	if err != nil || r == nil || r.Version != "v0.10.0" {
		t.Fatalf("Check = %+v, %v", r, err)
	}

	dir := t.TempDir()
	app := filepath.Join(dir, "app.exe")
	os.WriteFile(app, []byte("old executable"), 0o755)
	var last int64
	if err := r.Download(ctx, Staged(app), "v0.9.0", func(done, total int64) { last = done }); err != nil {
		t.Fatal(err)
	}
	if last != int64(len(exe)) {
		t.Errorf("progress ended at %d, want %d", last, len(exe))
	}
	if err := Apply(app); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(app); string(got) != string(exe) {
		t.Errorf("after Apply the executable is %q", got)
	}
	Cleanup(app)
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Errorf("Cleanup left %d files, want only the executable", len(entries))
	}
}

func TestDownloadRejectsTampered(t *testing.T) {
	release(t, "v0.10.0", []byte("new executable"), true)
	r, err := Check(context.Background(), "v0.9.0")
	if err != nil || r == nil {
		t.Fatal(r, err)
	}
	staged := filepath.Join(t.TempDir(), "app.new")
	if err := r.Download(context.Background(), staged, "v0.9.0", nil); err == nil {
		t.Fatal("a download that doesn't match the signed checksum was accepted")
	}
	if _, err := os.Stat(staged); !os.IsNotExist(err) {
		t.Error("the rejected download was left behind")
	}
}

func TestVerifySumsRejectsOtherKey(t *testing.T) {
	pub, _, _ := ed25519.GenerateKey(rand.Reader)
	_, other, _ := ed25519.GenerateKey(rand.Reader)
	old := publicKey
	publicKey = pub
	defer func() { publicKey = old }()
	sums := []byte(hex.EncodeToString(make([]byte, 32)) + "  " + AssetName() + "\n")
	sig := base64.StdEncoding.EncodeToString(ed25519.Sign(other, sums))
	if _, err := verifySums(sums, []byte(sig), AssetName()); !errors.Is(err, ErrSignature) {
		t.Fatalf("verifySums with another key's signature = %v, want ErrSignature", err)
	}
}

func TestPublicKey(t *testing.T) {
	if len(publicKey) != ed25519.PublicKeySize {
		t.Fatal("publicKey doesn't parse")
	}
}
