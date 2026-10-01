//go:build !windows

package osclip

import (
	"bytes"
	"image"
	"image/png"
	"net/url"
	"os/exec"
	"runtime"
	"strings"
)

// Elsewhere the clipboard goes through the desktop's tools: wl-paste and
// wl-copy on Wayland, xclip on X11. macOS has no plain tool for files or
// pictures, so it gets text only.

func run(stdin []byte, name string, args ...string) ([]byte, error) {
	cmd := exec.Command(name, args...)
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	return cmd.Output()
}

func has(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

// paste returns the clipboard's content of type mime.
func paste(mime string) []byte {
	if runtime.GOOS == "darwin" {
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

func hasText() bool { return len(paste("text/plain")) > 0 }

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
