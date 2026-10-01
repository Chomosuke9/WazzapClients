// Package notify shows the system's notifications: toasts in the Windows
// notification center, notify-send on Linux and Notification Center on
// macOS.
//
// On Windows a notification can be replaced, removed once its chat is
// read, and answered: clicking it, or its Reply and Mark as read buttons,
// calls Options.Activate. Elsewhere notifications are shown and nothing
// more.
package notify

import (
	"crypto/sha1"
	"encoding/hex"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// Notification is one notification to show.
type Notification struct {
	// ID says what the notification is about (a chat). It replaces a
	// notification with the same ID that is still shown, and comes back
	// with activations.
	ID    string
	Title string
	Body  string
	// Footer is small print under the body ("3 new messages").
	Footer string
	// Image is a JPEG or PNG picture beside the text, cut to a circle.
	Image []byte
	// Time is when it happened; the notification center sorts by it.
	Time   time.Time
	Silent bool
	// Reply adds a reply box with Reply and Mark as read buttons.
	Reply bool
}

// Action is what the user did with a notification.
type Action int

const (
	Open     Action = iota // clicked the notification
	Reply                  // sent the text in the reply box
	MarkRead               // pressed Mark as read
)

// Activation reports a click on a notification or one of its buttons.
type Activation struct {
	ID     string
	Action Action
	Text   string // the reply, for Reply
}

// Options configure Init.
type Options struct {
	// AppID identifies the app to the system, e.g. "WazzapClients.Desktop".
	AppID string
	// Name is the app name notifications show.
	Name string
	// Icon is the app's icon, a PNG.
	Icon []byte
	// Dir keeps the pictures notifications show, which the system reads
	// from files.
	Dir string
	// Command starts the app when one of its notifications is clicked
	// while it isn't running: the executable, then its arguments. Windows
	// adds "-Embedding".
	Command []string
	// Activate is called, on its own goroutine, when a notification or
	// one of its buttons is clicked.
	Activate func(Activation)
}

// imageFile returns the path of a file holding data in dir, writing it
// unless an earlier notification did. Files are named after their
// contents, so a notification still on screen keeps its picture when
// another one replaces the file it would otherwise share.
func imageFile(dir string, data []byte) string {
	if dir == "" || len(data) == 0 {
		return ""
	}
	sum := sha1.Sum(data)
	ext := ".jpg"
	if http.DetectContentType(data) == "image/png" {
		ext = ".png"
	}
	path := filepath.Join(dir, hex.EncodeToString(sum[:10])+ext)
	if _, err := os.Stat(path); err == nil {
		now := time.Now()
		os.Chtimes(path, now, now) // in use again: keep it from pruneImages
		return path
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return ""
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return ""
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return ""
	}
	return path
}

// pruneImages removes pictures no notification has used for a week.
func pruneImages(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	old := time.Now().Add(-7 * 24 * time.Hour)
	for _, e := range entries {
		if info, err := e.Info(); err == nil && !e.IsDir() && info.ModTime().Before(old) {
			os.Remove(filepath.Join(dir, e.Name()))
		}
	}
}
