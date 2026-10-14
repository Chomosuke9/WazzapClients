// Package camera takes a photo for the attach menu's Camera with the
// system's own camera app: it starts the app, then waits for a new photo
// or video in the folder the app saves to.
//
// Capturing from the webcam here would need each system's capture API
// (Media Foundation's on Windows, through COM, and a viewfinder drawn by
// the UI); the camera app already has all of it, timer and mirror included.
package camera

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ErrUnsupported means there is no camera app this package knows on this
// system.
var ErrUnsupported = errors.New("camera: no camera app found")

// waitFor is how long Take waits for a photo before giving up.
const waitFor = 15 * time.Minute

// exts are the files a camera app saves.
var exts = []string{".jpg", ".jpeg", ".png", ".heic", ".mp4", ".mov", ".webm"}

// Take opens the camera app and returns the first photo or video it saves,
// once the file stops growing. It returns "" and ctx's error when ctx ends
// first, and "" and nil when nothing came in waitFor. Call it from a
// goroutine of its own.
func Take(ctx context.Context) (string, error) {
	start := time.Now().Add(-2 * time.Second) // file times can be a little behind
	dirs, err := launch()
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(ctx, waitFor)
	defer cancel()
	t := time.NewTicker(time.Second)
	defer t.Stop()
	last := map[string]int64{} // a new file's size a second ago
	for {
		select {
		case <-ctx.Done():
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				return "", nil
			}
			return "", ctx.Err()
		case <-t.C:
		}
		for _, dir := range dirs {
			if p := newest(dir, start, last); p != "" {
				return p, nil
			}
		}
	}
}

// newest returns a photo or video in dir changed after since whose size
// has stayed the same for a second (the app is done writing it), or "".
// sizes keeps what it saw for the next look.
func newest(dir string, since time.Time, sizes map[string]int64) string {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	for _, e := range ents {
		if e.IsDir() || !media(e.Name()) {
			continue
		}
		info, err := e.Info()
		if err != nil || info.ModTime().Before(since) || info.Size() == 0 {
			continue
		}
		p := filepath.Join(dir, e.Name())
		if n, ok := sizes[p]; ok && n == info.Size() {
			return p
		}
		sizes[p] = info.Size()
	}
	return ""
}

func media(name string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	for _, e := range exts {
		if e == ext {
			return true
		}
	}
	return false
}
