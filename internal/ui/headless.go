package ui

import (
	"bufio"
	"cmp"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"slices"
	"strings"
	"sync"
	"syscall"

	"github.com/chomosuke9/wazzapclients/internal/model"
	"github.com/chomosuke9/wazzapclients/internal/termqr"
)

// The headless mode (cmd/wazzap -headless) runs the host without a
// window, tray icon or notifications: the open account stays connected
// and the watch app's server (internal/remote) serves it, where and with
// the token the command line says. With no window to show the QR code,
// the account links from the terminal (the QR code drawn in text, or a
// phone number's pairing code) or from the server's /login page.

// Headless configures the headless mode.
type Headless struct {
	// Hosts are where the server listens (remote.ListenAddrs); none means
	// this computer and its Tailscale addresses, as the Smartwatch page's
	// default.
	Hosts []string
	// Port is the server's port; 0 keeps the Smartwatch page's.
	Port int
	// Token is the token the watch asks with. "" keeps the one made
	// before (Smartwatch access's), or makes one once.
	Token string
	// Phone, when set, links with a pairing code for this phone number
	// instead of the QR code.
	Phone string
	// Out gets the QR codes and what happens; ANSI paints the QR code
	// black on white with escape codes and redraws it in place.
	Out  io.Writer
	ANSI bool
	// In, when set, is read a line at a time: a phone number asks for its
	// pairing code, an empty line for new QR codes.
	In io.Reader
}

// headlessHost is the headless mode's part of the host. Its methods run on
// the UI goroutine, but say, which pairing codes call from theirs.
type headlessHost struct {
	h *host
	o *Headless

	mu      sync.Mutex // Out, and qrLines
	qrLines int        // the lines of the QR code drawn last, while nothing came after it

	last   model.ConnState
	seen   bool   // last is set
	qr     string // the QR code drawn last
	asked  bool   // Phone's code was asked for, in this round of QR codes
	pairOn bool   // a pairing code is on its way
}

// startHeadless sets the headless mode up, and returns the channel Ctrl+C
// and SIGTERM come on.
func (h *host) startHeadless() chan os.Signal {
	hl := h.o.Headless
	if hl.Out == nil {
		hl.Out = io.Discard
	}
	h.headless = &headlessHost{h: h, o: hl}
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM)
	if hl.In != nil {
		go func() {
			sc := bufio.NewScanner(hl.In)
			for sc.Scan() {
				h.request(request{kind: reqConsole, link: sc.Text()})
			}
		}()
	}
	return quit
}

// say writes a line out.
func (hh *headlessHost) say(format string, args ...any) {
	hh.mu.Lock()
	defer hh.mu.Unlock()
	fmt.Fprintf(hh.o.Out, format+"\n", args...)
	hh.qrLines = 0
}

// started says where the server listens and with what token.
func (hh *headlessHost) started() {
	h := hh.h
	hh.say("%s is running headless, as the watch app's server. Ctrl+C quits.", appName)
	if h.remote == nil {
		return
	}
	st := h.remote.Status()
	urls := headlessURLs(st.Addrs)
	switch {
	case len(urls) == 0 && st.Err != "":
		hh.say("  Couldn't listen: %s (trying again every 30 seconds)", st.Err)
	case len(urls) == 0:
		hh.say("  Not listening yet: no address to listen on is up (Tailscale?), trying again every 30 seconds")
	default:
		hh.say("  Address: %s", strings.Join(urls, ", "))
		if st.Err != "" {
			hh.say("  Some addresses couldn't be listened on: %s", st.Err)
		}
	}
	hh.say("  Token:   %s", h.b.Pref(prefRemoteToken))
	if len(urls) > 0 {
		hh.say("  Linking page: %s/login", urls[0])
	}
}

// headlessURLs are the URLs of addrs, those of every network as this
// computer's addresses.
func headlessURLs(addrs []string) []string {
	var urls []string
	for _, a := range addrs {
		host, port, err := net.SplitHostPort(a)
		if err != nil {
			continue
		}
		if host != "" && host != "0.0.0.0" && host != "::" {
			urls = append(urls, "http://"+net.JoinHostPort(host, port))
			continue
		}
		for _, ip := range localIPs(host == "0.0.0.0") {
			urls = append(urls, "http://"+net.JoinHostPort(ip, port))
		}
	}
	slices.Sort(urls)
	return slices.Compact(urls)
}

// localIPs are this computer's addresses, IPv4 ones only when v4 is set
// (or there are any), loopback last.
func localIPs(v4 bool) []string {
	ifAddrs, _ := net.InterfaceAddrs()
	var ips, v6 []string
	for _, a := range ifAddrs {
		n, ok := a.(*net.IPNet)
		if !ok || n.IP.IsLoopback() || n.IP.IsLinkLocalUnicast() {
			continue
		}
		if n.IP.To4() != nil {
			ips = append(ips, n.IP.String())
		} else {
			v6 = append(v6, n.IP.String())
		}
	}
	if len(ips) == 0 && !v4 {
		ips = v6
	}
	return append(ips, "127.0.0.1")
}

// conn tells what a connection state change means for linking.
func (hh *headlessHost) conn(e model.ConnEvent) {
	changed := !hh.seen || e.State != hh.last
	was := hh.last
	hh.last, hh.seen = e.State, true
	switch e.State {
	case model.StateQR:
		if hh.o.Phone != "" {
			if !hh.asked {
				hh.asked = true
				hh.say("Asking WhatsApp for a pairing code for %s…", hh.o.Phone)
				hh.pair(hh.o.Phone)
			}
			return
		}
		if e.QR != "" && e.QR != hh.qr {
			hh.qr = e.QR
			hh.drawQR(e.QR)
		}
	case model.StateQRExpired:
		hh.asked, hh.qr = false, ""
		if changed {
			hh.say("The QR codes ran out. %s", hh.retryHint())
		}
	case model.StateConnecting:
		if changed && (was == model.StateQR || was == model.StateQRExpired) {
			hh.say("Linked. Connecting to WhatsApp…")
		}
	case model.StateOnline:
		if changed {
			hh.say("Online as %s.", cmp.Or(e.Me, hh.h.conn.Me, "you"))
		}
	case model.StateOffline:
		if changed {
			hh.say("The connection dropped: reconnecting…")
		}
	case model.StateError:
		if changed {
			hh.say("Error: %s %s", e.Err, hh.retryHint())
		}
	}
}

// retryHint says how to get new QR codes.
func (hh *headlessHost) retryHint() string {
	if hh.o.In != nil {
		return "Press Enter to try again, or type your phone number for a pairing code."
	}
	return "Open the linking page (/login) to try again."
}

// drawQR draws a QR code to scan, over the one drawn before when nothing
// came after it.
func (hh *headlessHost) drawQR(code string) {
	txt, err := termqr.Text(code, hh.o.ANSI)
	if err != nil {
		hh.say("Couldn't draw the QR code: %v", err)
		return
	}
	help := "Scan it with WhatsApp: Settings > Linked devices > Link a device. It changes every 20 seconds or so."
	if hh.o.In != nil {
		help += "\nOr type your phone number, with its country code (628123456789), and Enter for a pairing code."
	}
	out := txt + help + "\n"
	hh.mu.Lock()
	defer hh.mu.Unlock()
	if hh.o.ANSI && hh.qrLines > 0 {
		fmt.Fprintf(hh.o.Out, "\x1b[%dF\x1b[J", hh.qrLines) // up to the last one, and clear it
	}
	io.WriteString(hh.o.Out, out)
	hh.qrLines = strings.Count(out, "\n")
}

// pairedLine is the line a pairing code's goroutine sends when it's done.
const pairedLine = "\x00paired"

// line serves a line typed in the terminal.
func (hh *headlessHost) line(s string) {
	if s == pairedLine {
		hh.pairOn = false
		return
	}
	s = strings.TrimSpace(s)
	h := hh.h
	if h.conn.State.LoggedIn() {
		if s != "" {
			hh.say("WhatsApp is linked already.")
		}
		return
	}
	switch {
	case s == "":
		if h.conn.State == model.StateQR && hh.qr != "" {
			hh.drawQR(hh.qr)
			return
		}
		hh.say("Asking for new QR codes…")
		hh.asked, hh.qr = false, ""
		h.b.Retry()
	case len(digitsOf(s)) >= 7:
		hh.pair(digitsOf(s))
	default:
		hh.say("Type a phone number with its country code (628123456789) for a pairing code, or press Enter for new QR codes.")
	}
}

// pair asks for phone's pairing code, on a goroutine of its own: WhatsApp's
// server answers.
func (hh *headlessHost) pair(phone string) {
	p, ok := hh.h.b.(model.PhonePairer)
	if !ok {
		hh.say("This account can't link with a phone number: scan the QR code.")
		return
	}
	if hh.pairOn {
		hh.say("A pairing code is on its way already.")
		return
	}
	hh.pairOn = true
	go func() {
		code, err := p.PairPhone(phone)
		hh.h.request(request{kind: reqConsole, link: pairedLine}) // pairOn goes off on the UI goroutine
		if err != nil {
			hh.say("No pairing code: %v", err)
			return
		}
		hh.say("Pairing code for +%s: %s", phone, code)
		hh.say("On your phone: WhatsApp > Settings > Linked devices > Link a device > Link with phone number instead, then enter it.")
	}()
}
