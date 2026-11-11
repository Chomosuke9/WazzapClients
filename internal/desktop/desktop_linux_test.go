//go:build linux

package desktop

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestLinkDefault checks which desktop entry the mimeapps.list files
// choose for whatsapp: links.
func TestLinkDefault(t *testing.T) {
	cfg, data := t.TempDir(), t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfg)
	t.Setenv("XDG_CONFIG_DIRS", t.TempDir())
	t.Setenv("XDG_DATA_HOME", data)
	t.Setenv("XDG_DATA_DIRS", t.TempDir())
	t.Setenv("XDG_CURRENT_DESKTOP", "Hyprland")
	if got := linkDefault(); got != "" {
		t.Fatalf("default %q with no files", got)
	}
	write := func(path, body string) {
		t.Helper()
		os.MkdirAll(filepath.Dir(path), 0o755)
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(data, "applications", "mimeapps.list"),
		"[Added Associations]\nx-scheme-handler/whatsapp=added.desktop;\n[Default Applications]\nx-scheme-handler/whatsapp=old.desktop;\n")
	if got := linkDefault(); got != "old.desktop" {
		t.Fatalf("default %q, want the data directory's", got)
	}
	write(filepath.Join(cfg, "mimeapps.list"),
		"[Default Applications]\ntext/html=browser.desktop\nx-scheme-handler/whatsapp = "+linkEntry+";other.desktop;\n")
	if !LinksHandled() {
		t.Fatalf("default %q, want this app's", linkDefault())
	}
	write(filepath.Join(cfg, "hyprland-mimeapps.list"), "[Default Applications]\nx-scheme-handler/whatsapp=theirs.desktop\n")
	if got := linkDefault(); got != "theirs.desktop" {
		t.Fatalf("default %q, want the desktop's own", got)
	}
}

// TestStartAtLogin checks the autostart entry and its Exec line.
func TestStartAtLogin(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if StartAtLogin() {
		t.Fatal("on before it was set")
	}
	if err := SetStartAtLogin(true, []string{"-data", `/tmp/a "b" $c 100%`, "-background"}); err != nil {
		t.Fatal(err)
	}
	if !StartAtLogin() {
		t.Fatal("off after it was set")
	}
	data, _ := os.ReadFile(autostartFile())
	want := ` "-data" "/tmp/a \\"b\\" \\$c 100%%" "-background"` + "\n"
	if !strings.Contains(string(data), want) {
		t.Fatalf("entry\n%s\nlacks %s", data, want)
	}
	if err := SetStartAtLogin(true, []string{"-background"}); err != nil {
		t.Fatal(err)
	}
	if err := SetStartAtLogin(false, nil); err != nil || StartAtLogin() {
		t.Fatalf("still on after it was turned off (%v)", err)
	}
}

// TestLock checks that a second instance finds the first and hands it
// its request.
func TestLock(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	t.Setenv("XDG_ACTIVATION_TOKEN", "tok")
	dir := t.TempDir()
	if !Lock(dir, "") {
		t.Fatal("the first instance didn't get the lock")
	}
	defer func() {
		instance.lock.Close()
		instance.lock, instance.t = nil, nil
	}()
	if Lock(dir, "whatsapp://send?phone=1") {
		t.Fatal("a second instance got the lock")
	}
	got := make(chan string, 4)
	// The request came before the tray's callbacks: it waited for them.
	listen(&Tray{
		Open:       func() { got <- "open" },
		Link:       func(l string) { got <- "link " + l },
		Activation: func(tok string) { got <- "token " + tok },
	})
	for _, want := range []string{"token tok", "link whatsapp://send?phone=1"} {
		if g := <-got; g != want {
			t.Fatalf("got %q, want %q", g, want)
		}
	}
	if Lock(dir, "") {
		t.Fatal("a third instance got the lock")
	}
	<-got
	if g := <-got; g != "open" {
		t.Fatalf("got %q, want open", g)
	}
}
