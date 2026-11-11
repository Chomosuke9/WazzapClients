//go:build !windows

package osclip

import (
	"bytes"
	"context"
	"image"
	"image/png"
	"net/url"
	"os/exec"
	"runtime"
	"slices"
	"strings"
	"time"
)

// Elsewhere the clipboard goes through the desktop's tools: wl-paste and
// wl-copy on Wayland, xclip on X11. macOS has no plain tool for files or
// pictures, so it gets text only.
//
// Reading the clipboard asks its owner for the data, and when the owner is
// this app, its window's thread has to answer. Call these off the UI
// goroutine (see pasteFiles in internal/ui), or wl-paste waits for the
// window and the window for wl-paste.

// timeout bounds each tool: a clipboard owner that never answers would
// otherwise hold the caller for good.
const timeout = 5 * time.Second

func run(stdin []byte, name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	// A tool's child that keeps our pipes open must not keep us waiting.
	cmd.WaitDelay = time.Second
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
		// wl-copy and xclip stay behind to serve what they were given, so
		// don't wait for their output.
		return nil, cmd.Run()
	}
	return cmd.Output()
}

func has(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

// types returns the clipboard's MIME types. Listing them doesn't ask the
// owner for any data on Wayland.
func types() []string {
	if runtime.GOOS == "darwin" {
		return nil
	}
	var out []byte
	switch {
	case has("wl-paste"):
		out, _ = run(nil, "wl-paste", "--list-types")
	case has("xclip"):
		out, _ = run(nil, "xclip", "-selection", "clipboard", "-target", "TARGETS", "-out")
	}
	return strings.Fields(string(out))
}

// paste returns the clipboard's content of type mime, if it offers that.
func paste(mime string) []byte {
	if !slices.Contains(types(), mime) {
		return nil
	}
	var out []byte
	switch {
	case has("wl-paste"):
		out, _ = run(nil, "wl-paste", "--no-newline", "--type", mime)
	case has("xclip"):
		out, _ = run(nil, "xclip", "-selection", "clipboard", "-target", mime, "-out")
	}
	return out
}

func hasText() bool {
	for _, t := range types() {
		switch t {
		case "text/plain", "text/plain;charset=utf-8", "UTF8_STRING", "STRING", "TEXT":
			return true
		}
	}
	return false
}

func files() []string {
	var out []string
	for _, line := range strings.Split(string(paste("text/uri-list")), "\n") {
		line = strings.TrimSpace(line)
		if u, err := url.Parse(line); err == nil && u.Scheme == "file" && u.Path != "" {
			out = append(out, u.Path)
		}
	}
	return out
}

func readImage() image.Image {
	data := paste("image/png")
	if len(data) == 0 {
		return nil
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return nil
	}
	return img
}

func writeImage(_ uintptr, img image.Image) error {
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return err
	}
	switch {
	case runtime.GOOS == "darwin":
		return ErrUnsupported
	case has("wl-copy"):
		_, err := run(buf.Bytes(), "wl-copy", "--type", "image/png")
		return err
	case has("xclip"):
		_, err := run(buf.Bytes(), "xclip", "-selection", "clipboard", "-target", "image/png", "-in")
		return err
	}
	return ErrUnsupported
}
