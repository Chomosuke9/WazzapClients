package ui

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"log"
	"net"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/chomosuke9/wazzapclients/internal/model"
	"github.com/chomosuke9/wazzapclients/internal/remote"
)

// Smartwatch access: the host serves the open account's chats to the
// Wazzap watch app (internal/remote) while the app runs, with a window or
// in the tray. It's an extra feature, off until it's turned on; its
// preferences are app preferences (appPrefs), so the watch goes on
// working when another account opens.
const (
	prefRemote      = "remote"       // "on" serves the watch app
	prefRemoteToken = "remote_token" // the token the watch asks with
	prefRemoteAll   = "remote_all"   // "on" listens on every network, not Tailscale alone
	prefRemotePort  = "remote_port"  // "" for remote.DefaultPort
)

// remoteConfig is what a running server was started with: a change
// restarts it.
type remoteConfig struct {
	port int
	all  bool
}

func remotePort(b model.Backend) int {
	if p, err := strconv.Atoi(b.Pref(prefRemotePort)); err == nil && p > 0 && p < 65536 {
		return p
	}
	return remote.DefaultPort
}

// newRemoteToken makes a token no one can guess.
func newRemoteToken() string {
	var b [24]byte
	_, _ = rand.Read(b[:])
	return base64.RawURLEncoding.EncodeToString(b[:])
}

// applyRemote starts, restarts or stops the server as the preferences
// say. It runs on the UI goroutine.
func (h *host) applyRemote() {
	b := h.b
	on := extraOn(b, prefRemote)
	tok := b.Pref(prefRemoteToken)
	if on && tok == "" {
		tok = newRemoteToken()
		b.SetPref(prefRemoteToken, tok)
	}
	cfg := remoteConfig{port: remotePort(b), all: extraOn(b, prefRemoteAll)}
	if h.remote != nil && (!on || cfg != h.remoteCfg) {
		h.stopRemote()
	}
	if !on {
		return
	}
	if h.remote == nil {
		h.remote = remote.Start(remote.Options{
			Port: cfg.port, AllNetworks: cfg.all, Token: tok, Version: h.o.Version,
			Do: h.remoteDo, Host: remoteHost{h},
		})
		h.remoteCfg = cfg
	}
	h.remote.SetToken(tok)
}

func (h *host) stopRemote() {
	if h.remote != nil {
		h.remote.Close()
		h.remote = nil
	}
}

var errRemoteBusy = errors.New("the UI goroutine is busy")

// remoteDo runs f on the UI goroutine for a request of the watch, and
// waits for it.
func (h *host) remoteDo(f func()) error {
	t := time.NewTimer(10 * time.Second)
	defer t.Stop()
	done := make(chan bool, 1)
	run := func() {
		ok := false
		defer func() {
			if r := recover(); r != nil {
				// A request of the watch mustn't take the app down.
				log.Printf("remote request panicked: %v", r)
			}
			done <- ok
		}()
		f()
		ok = true
	}
	select {
	case h.remoteCalls <- run:
	case <-t.C:
		return errRemoteBusy
	}
	select {
	case ok := <-done:
		if !ok {
			return errRemoteBusy
		}
		return nil
	case <-t.C:
		return errRemoteBusy
	}
}

// remoteHost is the host as the server sees it.
type remoteHost struct{ h *host }

func (r remoteHost) Backend() model.Backend { return r.h.b }
func (r remoteHost) Conn() model.ConnEvent  { return r.h.conn }
func (r remoteHost) Read(id string)         { r.h.markRead(id) }

func (r remoteHost) Sent(m *model.Message) {
	if h := r.h; h.u != nil {
		h.u.upsertMessage(m)
		h.win.Invalidate()
	}
}

// remoteURLs are the addresses the watch can use, the Tailscale ones
// first, and whether only this computer can connect.
func remoteURLs(st remote.Status, port int) (urls []string, local bool) {
	p := strconv.Itoa(port)
	var all bool
	for _, a := range st.Addrs {
		host, _, err := net.SplitHostPort(a)
		switch {
		case err != nil:
		case host == "":
			all = true
		case host != "127.0.0.1":
			urls = append(urls, "http://"+net.JoinHostPort(host, p))
		}
	}
	if all {
		for _, ip := range remote.TailscaleIPs() {
			urls = append(urls, "http://"+net.JoinHostPort(ip.String(), p))
		}
	}
	if len(urls) == 0 && !all && len(st.Addrs) > 0 {
		return []string{"http://" + net.JoinHostPort("127.0.0.1", p)}, true
	}
	// The IPv6 ones only add noise while there's an IPv4 one.
	if v4 := slices.DeleteFunc(slices.Clone(urls), func(u string) bool { return strings.Contains(u, "[") }); len(v4) > 0 {
		urls = v4
	}
	return urls, false
}

// remoteSettings is the Smartwatch section of the Extra features page.
func (u *UI) remoteSettings() settingsSection {
	b := u.backend
	on := extraOn(b, prefRemote)
	sec := settingsSection{title: "Smartwatch", rows: []settingRow{{key: prefRemote, kind: setToggle, on: on,
		title: "Smartwatch access",
		sub:   "Let the Wazzap app on an Amazfit watch read your chats and send replies through this computer, over Tailscale",
		run: func() {
			setExtra(b, prefRemote, !on)
			if u.host != nil {
				u.host.applyRemote()
			}
		}}}}
	if !on {
		sec.note = "The watch connects while " + appName + " runs, with its window open or in the tray."
		return sec
	}
	var st remote.Status
	if u.host != nil && u.host.remote != nil {
		st = u.host.remote.Status()
	}
	port := remotePort(b)
	urls, local := remoteURLs(st, port)
	addr := strings.Join(urls, ", ")
	switch {
	case len(st.Addrs) == 0 && st.Err != "":
		addr = "Couldn't start: " + st.Err
	case len(st.Addrs) == 0:
		addr = "Not running"
	case local:
		addr = "Tailscale isn't connected, so only this computer can connect: " + addr
	case len(urls) == 0:
		addr = "Every network, on port " + strconv.Itoa(port)
	}
	all := extraOn(b, prefRemoteAll)
	sec.rows = append(sec.rows,
		settingRow{key: "remote:addr", title: "Server address", sub: addr, run: func() {
			if len(urls) > 0 {
				u.pendingCopy = urls[0]
				u.toast("Address copied")
			}
		}},
		settingRow{key: "remote:token", title: "Token", sub: "Click to copy it, then paste it in the Wazzap settings of the Zepp app",
			run: func() {
				u.pendingCopy = b.Pref(prefRemoteToken)
				u.toast("Token copied")
			}},
		settingRow{key: "remote:newtoken", title: "New token", sub: "The watch stops connecting until it has the new one",
			run: func() {
				u.confirm("Make a new token?", "The watch stops connecting until you paste the new token in the Zepp app.",
					dialogButton{label: "New token", primary: true, run: func() {
						b.SetPref(prefRemoteToken, newRemoteToken())
						if u.host != nil {
							u.host.applyRemote()
						}
						u.settings.stale = true
					}})
			}},
		settingRow{key: prefRemoteAll, kind: setToggle, on: all, title: "Every network",
			sub: "Also take connections from your local network, not only from Tailscale. Anyone on it who has the token can read and send your messages",
			run: func() {
				setExtra(b, prefRemoteAll, !all)
				if u.host != nil {
					u.host.applyRemote()
				}
			}},
	)
	switch seen := st.LastSeen; {
	case seen.IsZero():
		sec.note = "No watch has connected yet."
	case sameDay(seen, u.now()):
		sec.note = "A watch last connected at " + seen.Format("15:04") + "."
	default:
		sec.note = "A watch last connected on " + seen.Format("Jan 2 at 15:04") + "."
	}
	return sec
}
