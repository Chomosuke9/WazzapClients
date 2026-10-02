package ui

import (
	"net/url"
	"os/exec"
	"runtime"
	"time"

	"gioui.org/layout"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

// openURL opens a web link in the default browser. Only http and https
// links are opened.
func openURL(link string) bool {
	p, err := url.Parse(link)
	if err != nil || (p.Scheme != "http" && p.Scheme != "https") || p.Host == "" {
		return false
	}
	return openTarget(p.String())
}

// openTarget hands a URL to the system, which opens it with its app.
func openTarget(target string) bool {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", target)
	case "darwin":
		cmd = exec.Command("open", target)
	default:
		cmd = exec.Command("xdg-open", target)
	}
	if cmd.Start() != nil {
		return false
	}
	go cmd.Wait() // reap the process
	return true
}

// pressButton runs one of a message's buttons.
func (u *UI) pressButton(m *model.Message, i int) {
	// A double click on a button shouldn't answer twice.
	key := m.ID + ":" + itoa(i)
	if u.lastButton.key == key && u.now().Sub(u.lastButton.at) < time.Second {
		return
	}
	u.lastButton.key, u.lastButton.at = key, u.now()
	b := m.Buttons[i]
	switch b.Kind {
	case model.ButtonURL:
		if !openURL(b.Value) {
			u.toast("Couldn't open the link.")
		}
	case model.ButtonCopy:
		u.pendingCopy = b.Value
		u.toast("Code copied")
	default:
		if r := u.backend.PressButton(m, i); r != nil {
			u.upsertMessage(r)
			u.scrollMessages(layout.Position{})
		}
	}
}
