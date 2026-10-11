// Package update replaces the app's executable with the latest GitHub
// release, when the user asks for it: Check looks for a newer release,
// Download fetches its executable next to the running one and checks it
// against the release's signed SHA256SUMS, and Apply swaps the two.
//
// A release can carry patches (bsdiff, made by cmd/mkpatch) from the
// releases before it. When there's one from the running version, Download
// rebuilds the new executable from the running one and the patch, a small
// fraction of the executable's size, and checks the result against the
// same checksum. Any trouble with the patch falls back to the whole file.
//
// Releases sign SHA256SUMS with an ed25519 key (cmd/signrelease) whose
// public half is publicKey. A checksum from the same release only proves
// the download wasn't damaged; the signature proves the release came from
// whoever holds the key, not just anyone who can publish on the repository.
package update

import (
	"bufio"
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// Repo is the GitHub repository releases come from.
const Repo = "Chomosuke9/WazzapClients"

// publicKey verifies SHA256SUMS.sig (base64 of the raw ed25519 key).
var publicKey = mustKey("kCxlEbDV1FL8EM1zdOwDs5PYpQDNcwX5BBbQvnGgza0=")

func mustKey(s string) ed25519.PublicKey {
	k, err := base64.StdEncoding.DecodeString(s)
	if err != nil || len(k) != ed25519.PublicKeySize {
		return nil
	}
	return k
}

// Release files besides the executables.
const (
	sumsName = "SHA256SUMS"
	sigName  = "SHA256SUMS.sig"
)

// maxExe caps a download, so a broken release can't fill the disk.
const maxExe = 256 << 20

var (
	// ErrDevBuild is Check's error in a build without a version.
	ErrDevBuild = errors.New("development builds don't update")
	// ErrNoBuild means the release has no executable for this system.
	ErrNoBuild = errors.New("this release has no build for this system")
	// ErrSignature means the release isn't signed with the app's key.
	ErrSignature = errors.New("the release's signature doesn't match")
)

// Release is a published release newer than the running app.
type Release struct {
	Version string // its tag, e.g. "v0.10.0"
	Page    string // its page on GitHub
	exe     asset
	patch   asset // from the running version, if the release has one
	sums    asset
	sig     asset
}

type asset struct {
	name string
	url  string
	size int64
}

// AssetName is the name of this system's executable in a release.
func AssetName() string {
	ext := ""
	if runtime.GOOS == "windows" {
		ext = ".exe"
	}
	return fmt.Sprintf("WazzapClients-%s-%s%s", runtime.GOOS, runtime.GOARCH, ext)
}

// patchName is the name of the patch in a release that turns version's
// executable for this system into the release's.
func patchName(version string) string {
	return AssetName() + ".from-" + version + ".bsdiff"
}

var client = &http.Client{Timeout: 10 * time.Minute}

func get(ctx context.Context, url, version string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "WazzapClients/"+version)
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("%s: %s", url, resp.Status)
	}
	return resp, nil
}

// apiURL is the latest release's API address (a variable for tests). The
// latest release leaves out drafts and prereleases.
var apiURL = "https://api.github.com/repos/" + Repo + "/releases/latest"

// Check returns the latest release when it's newer than current (a
// version like "v0.9.0"), and nil when current is up to date.
func Check(ctx context.Context, current string) (*Release, error) {
	cur, ok := parseVersion(current)
	if !ok {
		return nil, ErrDevBuild
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "WazzapClients/"+current)
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, nil // nothing published yet
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub: %s", resp.Status)
	}
	var rel struct {
		Tag    string `json:"tag_name"`
		Page   string `json:"html_url"`
		Assets []struct {
			Name string `json:"name"`
			URL  string `json:"browser_download_url"`
			Size int64  `json:"size"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&rel); err != nil {
		return nil, err
	}
	v, ok := parseVersion(rel.Tag)
	if !ok || !newer(v, cur) {
		return nil, nil
	}
	r := &Release{Version: rel.Tag, Page: rel.Page}
	for _, a := range rel.Assets {
		x := asset{name: a.Name, url: a.URL, size: a.Size}
		switch a.Name {
		case AssetName():
			r.exe = x
		case patchName(current):
			r.patch = x
		case sumsName:
			r.sums = x
		case sigName:
			r.sig = x
		}
	}
	return r, nil
}

// Size is the download's size in bytes: the patch's when the release has
// one from the running version, or 0 when it has no build for this system.
func (r *Release) Size() int64 {
	if r.patch.url != "" && r.exe.url != "" {
		return r.patch.size
	}
	return r.exe.size
}

// Download fetches the release's executable for this system to
// Staged(exe), where exe is the running executable, and checks it against
// the signed checksums. progress, if not nil, is called as bytes arrive.
// On error nothing stays behind.
func (r *Release) Download(ctx context.Context, exe, current string, progress func(done, total int64)) error {
	if r.exe.url == "" {
		return ErrNoBuild
	}
	if r.sums.url == "" || r.sig.url == "" {
		return ErrSignature
	}
	sums, err := fetchSmall(ctx, r.sums.url, current)
	if err != nil {
		return err
	}
	sig, err := fetchSmall(ctx, r.sig.url, current)
	if err != nil {
		return err
	}
	if err := verifySig(sums, sig); err != nil {
		return err
	}
	want, err := sumOf(sums, r.exe.name)
	if err != nil {
		return err
	}
	staged := Staged(exe)
	if r.patch.url != "" {
		err := r.patchTo(ctx, exe, staged, current, sums, want, progress)
		if err == nil || ctx.Err() != nil {
			return err
		}
		// A patch that's damaged or missing from SHA256SUMS, or a running
		// executable that isn't the release's (changed by hand, or by an
		// antivirus): the whole executable still installs.
	}
	return r.fetchTo(ctx, staged, current, want, progress)
}

// patchTo rebuilds the release's executable at staged from the running
// one, exe, and the patch.
func (r *Release) patchTo(ctx context.Context, exe, staged, current string, sums, want []byte, progress func(done, total int64)) error {
	wantPatch, err := sumOf(sums, r.patch.name)
	if err != nil {
		return err
	}
	resp, err := get(ctx, r.patch.url, current)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	total := resp.ContentLength
	if total <= 0 {
		total = r.patch.size
	}
	var patch bytes.Buffer
	pw := &progressWriter{total: total, fn: progress}
	n, err := io.Copy(io.MultiWriter(&patch, pw), io.LimitReader(resp.Body, maxExe+1))
	if err != nil {
		return err
	}
	if n > maxExe {
		return errors.New("the patch is too big")
	}
	if sum := sha256.Sum256(patch.Bytes()); !bytes.Equal(sum[:], wantPatch) {
		return errors.New("the patch is damaged (checksum mismatch)")
	}
	old, err := os.Open(exe)
	if err != nil {
		return err
	}
	defer old.Close()
	st, err := old.Stat()
	if err != nil {
		return err
	}
	return writeStaged(staged, want, func(w io.Writer) error {
		return bspatch(w, old, st.Size(), patch.Bytes())
	})
}

// fetchTo downloads the release's whole executable to staged.
func (r *Release) fetchTo(ctx context.Context, staged, current string, want []byte, progress func(done, total int64)) error {
	resp, err := get(ctx, r.exe.url, current)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	total := resp.ContentLength
	if total <= 0 {
		total = r.exe.size
	}
	pw := &progressWriter{total: total, fn: progress}
	return writeStaged(staged, want, func(w io.Writer) error {
		n, err := io.Copy(io.MultiWriter(w, pw), io.LimitReader(resp.Body, maxExe+1))
		if err == nil && n > maxExe {
			err = errors.New("the download is too big")
		}
		return err
	})
}

// writeStaged writes the new executable to staged with write and checks
// it against want. On error it removes the file.
func writeStaged(staged string, want []byte, write func(io.Writer) error) (err error) {
	// Write next to the running executable: Apply renames, and a rename
	// can't cross volumes. Creating the file also finds out early when
	// the folder isn't writable.
	f, err := os.OpenFile(staged, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := f.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			os.Remove(staged)
		}
	}()
	h := sha256.New()
	bw := bufio.NewWriterSize(io.MultiWriter(f, h), 64<<10)
	if err := write(bw); err != nil {
		return err
	}
	if err := bw.Flush(); err != nil {
		return err
	}
	if !bytes.Equal(h.Sum(nil), want) {
		return errors.New("the download is damaged (checksum mismatch)")
	}
	return f.Sync()
}

type progressWriter struct {
	done, total int64
	last        time.Time
	fn          func(done, total int64)
}

func (w *progressWriter) Write(p []byte) (int, error) {
	w.done += int64(len(p))
	// A few calls a second are enough for a progress bar.
	if w.fn != nil && (time.Since(w.last) > 100*time.Millisecond || w.done == w.total) {
		w.last = time.Now()
		w.fn(w.done, w.total)
	}
	return len(p), nil
}

func fetchSmall(ctx context.Context, url, current string) ([]byte, error) {
	resp, err := get(ctx, url, current)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(io.LimitReader(resp.Body, 64<<10))
}

// verifySig checks sig over sums.
func verifySig(sums, sig []byte) error {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(sig)))
	if err != nil || len(publicKey) != ed25519.PublicKeySize || !ed25519.Verify(publicKey, sums, raw) {
		return ErrSignature
	}
	return nil
}

// sumOf returns the SHA-256 that sums (checked with verifySig) lists for
// name.
func sumOf(sums []byte, name string) ([]byte, error) {
	sc := bufio.NewScanner(bytes.NewReader(sums))
	for sc.Scan() {
		// sha256sum writes "<hex>  <name>", or "<hex> *<name>" in binary mode.
		hexSum, file, ok := strings.Cut(sc.Text(), " ")
		file = strings.TrimPrefix(strings.TrimPrefix(file, " "), "*")
		if !ok || file != name {
			continue
		}
		sum, err := hex.DecodeString(hexSum)
		if err != nil || len(sum) != sha256.Size {
			break
		}
		return sum, nil
	}
	return nil, fmt.Errorf("%s doesn't list %s", sumsName, name)
}

// Supported reports whether a build of version updates itself: only
// releases do, not development builds or prereleases.
func Supported(version string) bool {
	_, ok := parseVersion(version)
	return ok
}

// version is a release version: major, minor, patch.
type version [3]int

// parseVersion reads "v1.2.3" (or "1.2.3"). Prereleases ("v1.2.3-rc1")
// and anything else don't parse.
func parseVersion(s string) (version, bool) {
	var v version
	parts := strings.Split(strings.TrimPrefix(s, "v"), ".")
	if len(parts) != 3 {
		return v, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return v, false
		}
		v[i] = n
	}
	return v, true
}

func newer(a, b version) bool {
	for i := range a {
		if a[i] != b[i] {
			return a[i] > b[i]
		}
	}
	return false
}
