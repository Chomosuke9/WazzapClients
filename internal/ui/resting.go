package ui

import (
	"sync/atomic"
	"time"
)

// Animations rest while the user is idle: once the keyboard and mouse
// haven't been touched for idleAfter, GIFs, video notes, animated stickers
// and animated emojis show a still picture until the next input. Playing
// to nobody they kept half a core busy, read each GIF's file again on
// every loop (Media Foundation keeps none of it), and kept frames coming,
// so the window never had the quiet moment its memory trim waits for.
//
// A video note playing with sound goes on: it's being listened to.

const (
	// restCheck is how often a frame asks how long the user has been idle
	// while they aren't: animations draw frames at the display's rate, and
	// on GNOME or KDE the answer is a D-Bus call.
	restCheck = time.Second
	// restPoll is how often the watcher looks for the user's return.
	restPoll = time.Second
)

// resting is the UI's part of it; see updateResting.
type resting struct {
	on      bool
	checked time.Time    // gtx.Now of the last look at the idle time
	watch   *atomic.Bool // the watcher's, false once it's told to stop
}

// updateResting sets u.rest.on for the frame about to be laid out.
func (u *UI) updateResting(now time.Time) {
	r := &u.rest
	if u.idle == nil {
		return
	}
	if !r.on && !r.checked.IsZero() && now.Sub(r.checked) < restCheck && !now.Before(r.checked) {
		return
	}
	r.checked = now
	on := u.idle() >= idleAfter
	if on == r.on {
		return
	}
	r.on = on
	if r.watch != nil {
		r.watch.Store(false)
		r.watch = nil
	}
	if !on {
		return
	}
	// Nothing draws a frame once the animations stand still, and input
	// outside the window brings none either: a watcher asks for the frame
	// that starts them again.
	alive := new(atomic.Bool)
	alive.Store(true)
	r.watch = alive
	idle, invalidate := u.idle, u.images.invalidate
	go func() {
		for alive.Load() {
			if idle() < idleAfter {
				if invalidate != nil {
					invalidate()
				}
				return
			}
			time.Sleep(restPoll)
		}
	}()
}

// stopResting ends the watcher of a UI that is going away.
func (u *UI) stopResting() {
	if u.rest.watch != nil {
		u.rest.watch.Store(false)
		u.rest.watch = nil
	}
	u.rest.on = false
}
