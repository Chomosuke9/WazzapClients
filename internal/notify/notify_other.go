//go:build !windows

package notify

import (
	"errors"
	"os/exec"
	"runtime"
	"strings"
)

// ErrUnsupported reports a system without a notification command.
var ErrUnsupported = errors.New("notify: notifications aren't supported")

var (
	opts Options
	jobs chan Notification
)

// Init checks for the system's notification command: notify-send on
// Linux, osascript on macOS.
func Init(o Options) error {
	name := "notify-send"
	if runtime.GOOS == "darwin" {
		name = "osascript"
	}
	if _, err := exec.LookPath(name); err != nil {
		return ErrUnsupported
	}
	opts = o
	jobs = make(chan Notification, 8)
	go func() {
		for n := range jobs {
			show(n)
		}
	}()
	return nil
}

// Show shows n. It returns at once; failures are dropped.
func Show(n Notification) {
	if jobs == nil {
		return
	}
	select {
	case jobs <- n:
	default:
	}
}

// Remove does nothing: these notifications can't be taken back.
func Remove(id string) {}

// Close does nothing.
func Close() {}

func show(n Notification) {
	body := n.Body
	if n.Footer != "" {
		body = strings.TrimSpace(body + "\n" + n.Footer)
	}
	if runtime.GOOS == "darwin" {
		q := func(s string) string {
			return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
		}
		script := "display notification " + q(body) + " with title " + q(n.Title)
		if !n.Silent {
			script += ` sound name "default"`
		}
		exec.Command("osascript", "-e", script).Run()
		return
	}
	args := []string{"--app-name=" + opts.Name}
	if p := imageFile(opts.Dir, n.Image); p != "" {
		args = append(args, "--icon="+p)
	}
	if n.Silent {
		args = append(args, "--hint=boolean:suppress-sound:true")
	} else {
		args = append(args, "--hint=string:sound-name:message-new-instant")
	}
	exec.Command("notify-send", append(args, "--", n.Title, body)...).Run()
}
