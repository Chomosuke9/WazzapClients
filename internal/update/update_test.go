package update

import (
	"bytes"
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
	"strconv"
	"strings"
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

// served counts the downloads of a fake release's executable and patch,
// and the API's requests. Its switches change what the server answers.
type served struct {
	exe, patch, api int
	limited         bool // the API answers 403, as when its rate limit is used up
	noBuild         bool // the release has no executable for this system
	none            bool // nothing is published
}

// release serves a fake latest release with exe as this system's build,
// signed with a new key that publicKey is set to.
func release(t *testing.T, tag string, exe []byte, tamper bool) *served {
	return releaseWithPatch(t, tag, exe, tamper, nil)
}

// releaseWithPatch is release with patch as the patch from v0.9.0, when
// it isn't nil.
func releaseWithPatch(t *testing.T, tag string, exe []byte, tamper bool, patch []byte) *served {
	t.Helper()
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	oldKey, oldURL, oldPatches, oldWeb := publicKey, apiURL, patchesURL, webURL
	publicKey = pub
	t.Cleanup(func() { publicKey, apiURL, patchesURL, webURL = oldKey, oldURL, oldPatches, oldWeb })

	sum := sha256.Sum256(exe)
	sums := fmt.Sprintf("%s  other-file\n%s  %s\n", hex.EncodeToString(make([]byte, 32)), hex.EncodeToString(sum[:]), AssetName())
	if patch != nil {
		psum := sha256.Sum256(patch)
		sums += fmt.Sprintf("%s  %s\n", hex.EncodeToString(psum[:]), patchName("v0.9.0", tag))
	}
	sig := base64.StdEncoding.EncodeToString(ed25519.Sign(priv, []byte(sums)))
	if tamper {
		exe = append([]byte("evil"), exe...)
	}
	n := &served{}
	var srv *httptest.Server
	// file sends b with its length, so a HEAD request learns its size.
	file := func(w http.ResponseWriter, b []byte) {
		w.Header().Set("Content-Length", strconv.Itoa(len(b)))
		w.Write(b)
	}
	// github.com's pages, as checkWeb reads them: the latest release is a
	// redirect to its page, and files redirect to where they're kept.
	web := "/web/releases"
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/latest" || r.URL.Path == "/patches" {
			n.api++
			if n.limited {
				http.Error(w, "API rate limit exceeded", http.StatusForbidden)
				return
			}
		}
		get := r.Method == http.MethodGet
		switch r.URL.Path {
		case web + "/latest":
			if n.none {
				http.Redirect(w, r, srv.URL+web, http.StatusFound)
				return
			}
			http.Redirect(w, r, srv.URL+web+"/tag/"+tag, http.StatusFound)
		case web + "/download/" + tag + "/" + AssetName():
			if n.noBuild {
				http.NotFound(w, r)
				return
			}
			http.Redirect(w, r, srv.URL+"/exe", http.StatusFound)
		case web + "/download/" + tag + "/SHA256SUMS":
			http.Redirect(w, r, srv.URL+"/sums", http.StatusFound)
		case web + "/download/" + tag + "/SHA256SUMS.sig":
			http.Redirect(w, r, srv.URL+"/sig", http.StatusFound)
		case web + "/download/" + PatchTag + "/" + patchName("v0.9.0", tag):
			if patch == nil {
				http.NotFound(w, r)
				return
			}
			http.Redirect(w, r, srv.URL+"/patch", http.StatusFound)
		case "/latest":
			fmt.Fprintf(w, `{"tag_name":%q,"html_url":"https://example.com","assets":[
				{"name":%q,"browser_download_url":"%s/exe","size":%d},
				{"name":"SHA256SUMS","browser_download_url":"%s/sums"},
				{"name":"SHA256SUMS.sig","browser_download_url":"%s/sig"}]}`,
				tag, AssetName(), srv.URL, len(exe), srv.URL, srv.URL)
		case "/patches":
			if patch == nil {
				http.NotFound(w, r)
				return
			}
			// A patch to an older release is for someone else.
			fmt.Fprintf(w, `{"tag_name":%q,"assets":[
				{"name":%q,"browser_download_url":"%s/stale","size":1},
				{"name":%q,"browser_download_url":"%s/patch","size":%d}]}`,
				PatchTag, patchName("v0.9.0", "v0.9.5"), srv.URL, patchName("v0.9.0", tag), srv.URL, len(patch))
		case "/exe":
			if get {
				n.exe++
			}
			file(w, exe)
		case "/patch":
			if get {
				n.patch++
			}
			file(w, patch)
		case "/sums":
			w.Write([]byte(sums))
		case "/sig":
			w.Write([]byte(sig))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	apiURL, patchesURL, webURL = srv.URL+"/latest", srv.URL+"/patches", srv.URL+web
	return n
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
	if err := r.Download(ctx, app, "v0.9.0", func(done, total int64) { last = done }); err != nil {
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
	app := filepath.Join(t.TempDir(), "app.exe")
	if err := r.Download(context.Background(), app, "v0.9.0", nil); err == nil {
		t.Fatal("a download that doesn't match the signed checksum was accepted")
	}
	if _, err := os.Stat(Staged(app)); !os.IsNotExist(err) {
		t.Error("the rejected download was left behind")
	}
}

func TestDownloadPatch(t *testing.T) {
	old, exe := patchFixture()
	patch := fixturePatch(t)
	n := releaseWithPatch(t, "v0.10.0", exe, false, patch)
	ctx := context.Background()

	// Only the version the patch is from gets it.
	if r, err := Check(ctx, "v0.8.0"); err != nil || r.Size() != int64(len(exe)) {
		t.Fatalf("Check from v0.8.0: Size = %d, %v; want the whole executable's", r.Size(), err)
	}
	r, err := Check(ctx, "v0.9.0")
	if err != nil || r.Size() != int64(len(patch)) {
		t.Fatalf("Check from v0.9.0: Size = %d, %v; want the patch's %d", r.Size(), err, len(patch))
	}
	app := filepath.Join(t.TempDir(), "app.exe")
	os.WriteFile(app, old, 0o755)
	var last int64
	if err := r.Download(ctx, app, "v0.9.0", func(done, total int64) { last = done }); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(Staged(app)); !bytes.Equal(got, exe) {
		t.Fatal("the patched executable isn't the release's")
	}
	if n.patch != 1 || n.exe != 0 {
		t.Errorf("downloaded the patch %d times and the executable %d times; want 1 and 0", n.patch, n.exe)
	}
	if last != int64(len(patch)) {
		t.Errorf("progress ended at %d, want %d", last, len(patch))
	}
}

// An executable the patch isn't for (changed since it was installed) gets
// the whole file instead.
func TestDownloadPatchFallsBack(t *testing.T) {
	old, exe := patchFixture()
	for name, tc := range map[string]struct {
		app, patch []byte
	}{
		"other executable": {append([]byte("changed"), old[7:]...), fixturePatch(t)},
		"damaged patch":    {old, []byte("BSDIFF40 but nothing after")},
	} {
		t.Run(name, func(t *testing.T) {
			n := releaseWithPatch(t, "v0.10.0", exe, false, tc.patch)
			r, err := Check(context.Background(), "v0.9.0")
			if err != nil {
				t.Fatal(err)
			}
			app := filepath.Join(t.TempDir(), "app.exe")
			os.WriteFile(app, tc.app, 0o755)
			if err := r.Download(context.Background(), app, "v0.9.0", nil); err != nil {
				t.Fatal(err)
			}
			if got, _ := os.ReadFile(Staged(app)); !bytes.Equal(got, exe) {
				t.Fatal("the staged executable isn't the release's")
			}
			if n.patch != 1 || n.exe != 1 {
				t.Errorf("downloaded the patch %d times and the executable %d times; want 1 and 1", n.patch, n.exe)
			}
		})
	}
}

// With the API's rate limit used up, Check finds the release, its patch and
// their sizes on github.com's own pages, and Download gets them from there.
func TestCheckFallsBackToWeb(t *testing.T) {
	old, exe := patchFixture()
	patch := fixturePatch(t)
	n := releaseWithPatch(t, "v0.10.0", exe, false, patch)
	n.limited = true
	ctx := context.Background()

	if r, err := Check(ctx, "v0.10.0"); err != nil || r != nil {
		t.Fatalf("Check from the same version = %v, %v; want nil, nil", r, err)
	}
	if r, err := Check(ctx, "v0.8.0"); err != nil || r.Size() != int64(len(exe)) {
		t.Fatalf("Check from v0.8.0 = %+v, %v; want the whole executable's size", r, err)
	}
	r, err := Check(ctx, "v0.9.0")
	if err != nil || r == nil {
		t.Fatal(r, err)
	}
	if n.api == 0 {
		t.Error("Check didn't ask the API first")
	}
	if r.Version != "v0.10.0" || !strings.HasSuffix(r.Page, "/releases/tag/v0.10.0") {
		t.Errorf("Check = %q, page %q", r.Version, r.Page)
	}
	if r.Size() != int64(len(patch)) {
		t.Errorf("Size = %d, want the patch's %d", r.Size(), len(patch))
	}
	app := filepath.Join(t.TempDir(), "app.exe")
	os.WriteFile(app, old, 0o755)
	if err := r.Download(ctx, app, "v0.9.0", nil); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(Staged(app)); !bytes.Equal(got, exe) {
		t.Fatal("the patched executable isn't the release's")
	}
	if n.patch != 1 || n.exe != 0 {
		t.Errorf("downloaded the patch %d times and the executable %d times; want 1 and 0", n.patch, n.exe)
	}
}

func TestCheckWebWithout(t *testing.T) {
	n := release(t, "v0.10.0", []byte("new executable"), false)
	n.limited = true
	ctx := context.Background()

	n.noBuild = true
	r, err := Check(ctx, "v0.9.0")
	if err != nil || r == nil || r.Size() != 0 {
		t.Fatalf("Check of a release without this system's build = %+v, %v", r, err)
	}
	if err := r.Download(ctx, filepath.Join(t.TempDir(), "app.exe"), "v0.9.0", nil); !errors.Is(err, ErrNoBuild) {
		t.Errorf("Download = %v, want ErrNoBuild", err)
	}

	n.none = true
	if r, err := Check(ctx, "v0.9.0"); err != nil || r != nil {
		t.Errorf("Check with nothing published = %+v, %v; want nil, nil", r, err)
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
	if err := verifySig(sums, []byte(sig)); !errors.Is(err, ErrSignature) {
		t.Fatalf("verifySig with another key's signature = %v, want ErrSignature", err)
	}
}

func TestPublicKey(t *testing.T) {
	if len(publicKey) != ed25519.PublicKeySize {
		t.Fatal("publicKey doesn't parse")
	}
}
