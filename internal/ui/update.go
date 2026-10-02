package ui

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"

	"github.com/chomosuke9/wazzapclients/internal/update"
)

// updateStep is where an update stands. Nothing happens until the user
// asks: the Help page's "Check for updates" row checks, and once a newer
// release is found, the same row downloads it and restarts the app.
type updateStep int

const (
	updIdle updateStep = iota
	updChecking
	updLatest    // the check found nothing newer
	updAvailable // rel is newer
	updDownloading
	updRestarting
	updFailed // err; rel is set when the download failed
)

// updater runs updates for the host, so one goes on when the window is
// closed and a new window shows where it stands. Its work runs on
// goroutines of its own, hence the lock.
type updater struct {
	h   *host
	exe string // the running executable, read before anything renames it

	mu          sync.Mutex
	step        updateStep
	rel         *update.Release
	done, total int64
	err         error
}

// canUpdate reports whether this build updates itself: a release (not a
// prerelease) that knows how to start again.
func (h *host) canUpdate() bool {
	return update.Supported(h.o.Version) && h.o.Relaunch != nil && h.upd.exe != ""
}

func (up *updater) state() (updateStep, *update.Release, int64, int64, error) {
	up.mu.Lock()
	defer up.mu.Unlock()
	return up.step, up.rel, up.done, up.total, up.err
}

func (up *updater) set(f func()) {
	up.mu.Lock()
	f()
	up.mu.Unlock()
	up.h.request(request{kind: reqRedraw})
}

// check looks for a newer release.
func (up *updater) check() {
	up.set(func() { up.step, up.err = updChecking, nil })
	go func() {
		rel, err := update.Check(context.Background(), up.h.o.Version)
		up.set(func() {
			up.rel, up.err = rel, err
			switch {
			case err != nil:
				up.step = updFailed
			case rel == nil:
				up.step = updLatest
			default:
				up.step = updAvailable
			}
		})
	}()
}

// install downloads the release found, puts it in place and restarts.
func (up *updater) install() {
	rel := up.rel
	up.set(func() { up.step, up.done, up.total, up.err = updDownloading, 0, rel.Size(), nil })
	go func() {
		err := rel.Download(context.Background(), update.Staged(up.exe), up.h.o.Version, func(done, total int64) {
			up.set(func() { up.done, up.total = done, total })
		})
		if err == nil {
			err = update.Apply(up.exe)
		}
		if err != nil {
			os.Remove(update.Staged(up.exe))
			up.set(func() { up.step, up.err = updFailed, err })
			return
		}
		up.set(func() { up.step = updRestarting })
		// The restart must not be dropped like a redraw can be.
		up.h.reqs <- request{kind: reqRestart}
	}()
}

// restart starts the updated executable and quits. The new process waits
// for this one to exit, which lets go of the single-instance lock.
func (h *host) restart() bool {
	args := append(append([]string(nil), h.o.Relaunch...), "-wait-pid", strconv.Itoa(os.Getpid()))
	if err := exec.Command(h.upd.exe, args...).Start(); err != nil {
		h.upd.set(func() { h.upd.step, h.upd.err = updFailed, err })
		return false
	}
	return h.handle(request{kind: reqQuit})
}

// updateRows are the Help page's rows about this version and updates.
func (u *UI) updateRows() []settingRow {
	h := u.host
	if h == nil || h.o.Version == "" {
		return []settingRow{{key: "version", kind: setInfo, title: "Version", sub: "Development build"}}
	}
	rows := []settingRow{u.settingInfoRow("version", "Version", h.o.Version)}
	if !h.canUpdate() {
		return rows
	}
	up := &h.upd
	step, rel, done, total, err := up.state()
	r := settingRow{key: "update", ic: icRefresh, title: "Check for updates", run: up.check}
	switch step {
	case updChecking:
		r.title, r.run = "Checking for updates…", nil
	case updLatest:
		r.title, r.sub = appName+" is up to date", "Check again"
	case updAvailable:
		r.ic, r.title = icDownload, "Update to "+rel.Version
		r.sub = "Download and restart"
		if n := rel.Size(); n > 0 {
			r.sub = "Download (" + formatSize(n) + ") and restart"
		}
		r.run = up.install
	case updDownloading:
		r.ic, r.title, r.run = icDownload, "Downloading "+rel.Version+"…", nil
		if total > 0 {
			r.sub = strconv.Itoa(int(done*100/total)) + "% of " + formatSize(total)
		}
	case updRestarting:
		r.title, r.run = "Restarting…", nil
	case updFailed:
		r.title, r.sub = "Couldn't check for updates", updateError(err, up.exe)+" · Try again"
		if rel != nil {
			r.title, r.run = "Couldn't update to "+rel.Version, up.install
		}
	}
	rows = append(rows, r)
	if rel != nil && step != updLatest {
		page := rel.Page
		rows = append(rows, settingRow{key: "whatsnew", ic: icInfo, title: "What's new in " + rel.Version,
			sub: "Release notes and downloads", trailing: icOpenInNew, run: func() { openURL(page) }})
	}
	return rows
}

// updateError says what went wrong in a few words.
func updateError(err error, exe string) string {
	switch {
	case errors.Is(err, update.ErrNoBuild):
		return "There's no build for this system"
	case errors.Is(err, update.ErrSignature):
		return "The release isn't signed by " + appName
	case errors.Is(err, fs.ErrPermission):
		return "Can't write to " + filepath.Dir(exe) + ". Download it from the release page"
	}
	return "Check your connection"
}
