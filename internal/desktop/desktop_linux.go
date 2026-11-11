//go:build linux

package desktop

import (
	"bufio"
	"errors"
	"fmt"
	"hash/fnv"
	"image"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

// instance is this process's hold on its data directory (see Lock) and
// what other instances asked of it before the tray's callbacks were there
// to ask.
var instance struct {
	sync.Mutex
	lock    *os.File // held (flock) for the life of the process
	t       *Tray
	pending []string
}

// runtimeBase is the start of the names of an instance's lock file and
// socket: in the user's runtime directory, which is theirs alone and
// emptied when they log out, or else the temporary one.
func runtimeBase(dir string) string {
	if abs, err := filepath.Abs(dir); err == nil {
		dir = abs
	}
	h := fnv.New64a()
	h.Write([]byte(filepath.Clean(dir)))
	name := fmt.Sprintf("wazzapclients-%016x", h.Sum64())
	if rt := os.Getenv("XDG_RUNTIME_DIR"); rt != "" {
		return filepath.Join(rt, name)
	}
	return filepath.Join(os.TempDir(), fmt.Sprintf("%s-%d", name, os.Getuid()))
}

// Lock makes this process the only instance of the app using the data
// directory dir. When another instance has it, Lock asks that one to show
// its window, or to open link when it isn't "", and returns false.
//
// The instance holds a lock on a file, which the kernel lets go of however
// the process ends, and listens on a socket beside it for the others.
func Lock(dir, link string) bool {
	base := runtimeBase(dir)
	sock := base + ".sock"
	f, err := os.OpenFile(base+".lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return true
	}
	if syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB) == nil {
		instance.lock = f
		os.Remove(sock) // left by an instance that's gone
		if l, err := net.Listen("unix", sock); err == nil {
			os.Chmod(sock, 0o600)
			go serve(l)
		}
		return true
	}
	f.Close()
	msg := "show"
	if link != "" {
		msg = "link " + link
	}
	// Whatever launched this instance for the user (a menu, a browser)
	// left the token that lets a window come to the front; it goes first.
	if token := os.Getenv("XDG_ACTIVATION_TOKEN"); token != "" && !strings.ContainsAny(token, "\r\n") {
		msg = "token " + token + "\n" + msg
	}
	// The other instance may still be starting: wait for its socket.
	for range 50 {
		if c, err := net.Dial("unix", sock); err == nil {
			fmt.Fprintln(c, msg)
			c.Close()
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	return false
}

// serve reads what other instances ask for: a line each, "show" or "link"
// and the link, after "token" and their activation token if they have one.
func serve(l net.Listener) {
	for {
		c, err := l.Accept()
		if err != nil {
			return
		}
		c.SetDeadline(time.Now().Add(5 * time.Second))
		sc := bufio.NewScanner(io.LimitReader(c, 32<<10))
		for sc.Scan() {
			if line := strings.TrimSpace(sc.Text()); line != "" {
				asked(line)
			}
		}
		c.Close()
	}
}

// asked does what another instance asked for, or keeps it for TrayStart.
func asked(line string) {
	instance.Lock()
	t := instance.t
	if t == nil {
		instance.pending = append(instance.pending, line)
	}
	instance.Unlock()
	if t != nil {
		answer(t, line)
	}
}

func answer(t *Tray, line string) {
	if token, ok := strings.CutPrefix(line, "token "); ok {
		if t.Activation != nil {
			t.Activation(token)
		}
		return
	}
	if link, ok := strings.CutPrefix(line, "link "); ok {
		if t.Link != nil {
			t.Link(link)
		}
		return
	}
	if t.Open != nil {
		t.Open()
	}
}

// listen hands other instances' requests to t from now on, starting with
// the ones that came before.
func listen(t *Tray) {
	instance.Lock()
	instance.t = t
	pending := instance.pending
	instance.pending = nil
	instance.Unlock()
	go func() {
		for _, line := range pending {
			answer(t, line)
		}
	}()
}

// SetWindowIcon does nothing: a Linux window takes its icon from the
// desktop file named after its app ID (installer/linux).
func SetWindowIcon(hwnd uintptr, draw func(px int) *image.RGBA) {}

// WaitExit waits up to d for the process pid to exit: a restarted app
// waits for the instance that started it to let go of Lock.
func WaitExit(pid int, d time.Duration) {
	end := time.Now().Add(d)
	for time.Now().Before(end) {
		// Signal 0 only checks; a zombie still answers, so read its state.
		if syscall.Kill(pid, 0) == syscall.ESRCH {
			return
		}
		if stat, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid)); err == nil {
			if i := strings.LastIndexByte(string(stat), ')'); i >= 0 && strings.HasPrefix(string(stat[i+1:]), " Z") {
				return
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// configHome and dataHome are the XDG directories of the user's settings
// and data.
func configHome() string {
	if d := os.Getenv("XDG_CONFIG_HOME"); d != "" {
		return d
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config")
}

func dataHome() string {
	if d := os.Getenv("XDG_DATA_HOME"); d != "" {
		return d
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "share")
}

// execLine is a desktop entry's Exec value that runs this executable with
// args, then tail (a field code such as %u) as it is.
func execLine(args []string, tail string) (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	// Quoted arguments escape these four, and a literal % is doubled; the
	// value itself, a string, takes its backslashes escaped once more.
	quote := func(s string) string {
		s = strings.NewReplacer(`\`, `\\`, `"`, `\"`, "`", "\\`", `$`, `\$`).Replace(s)
		s = strings.ReplaceAll(s, `%`, `%%`)
		return `"` + strings.ReplaceAll(s, `\`, `\\`) + `"`
	}
	line := quote(exe)
	for _, a := range args {
		line += " " + quote(a)
	}
	if tail != "" {
		line += " " + tail
	}
	return line, nil
}

// errSame is writeEntry's answer when the entry is there already.
var errSame = errors.New("desktop: the entry hasn't changed")

// writeEntry writes a desktop entry, in one step, unless the file holds
// it already (errSame).
func writeEntry(path string, lines ...string) error {
	data := "[Desktop Entry]\n" + strings.Join(lines, "\n") + "\n"
	if old, err := os.ReadFile(path); err == nil && string(old) == data {
		return errSame
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(data), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// autostartFile is the desktop entry that desktops run at login (the XDG
// autostart specification).
func autostartFile() string {
	return filepath.Join(configHome(), "autostart", "wazzapclients.desktop")
}

// StartAtLogin reports whether the app starts when the user logs in.
func StartAtLogin() bool {
	data, err := os.ReadFile(autostartFile())
	if err != nil {
		return false
	}
	// A desktop's own settings turn an entry off by hiding it.
	for _, line := range strings.Split(string(data), "\n") {
		switch strings.ReplaceAll(strings.TrimSpace(line), " ", "") {
		case "Hidden=true", "X-GNOME-Autostart-enabled=false":
			return false
		}
	}
	return true
}

// SetStartAtLogin makes the app start at login with args, or not.
func SetStartAtLogin(on bool, args []string) error {
	path := autostartFile()
	if !on {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	line, err := execLine(args, "")
	if err != nil {
		return err
	}
	err = writeEntry(path,
		"Type=Application",
		"Name=WazzapClients",
		"Comment=Start WazzapClients in the background",
		"Exec="+line,
		"Icon=wazzapclients",
		"Terminal=false",
		"X-GNOME-Autostart-enabled=true",
	)
	if err == errSame {
		return nil
	}
	return err
}

// WhatsApp's links: websites send wa.me and api.whatsapp.com/send on to
// whatsapp://send?phone=…, and group invites to whatsapp://chat?code=….
// Desktops start the scheme's handler, the desktop entry chosen for the
// MIME type x-scheme-handler/whatsapp, with the link in place of %u. The
// entry is one of its own, hidden from menus: the installed one starts
// the app without arguments. Unlike Windows, an app may choose itself
// (ClaimLinks).

const (
	linkMime  = "x-scheme-handler/whatsapp"
	linkEntry = "wazzapclients-links.desktop"
)

func linkFile() string { return filepath.Join(dataHome(), "applications", linkEntry) }

// linkDefault is the desktop entry chosen to open whatsapp: links, or ""
// when none was: the first default in the mimeapps.list files, read here
// in the order the MIME applications specification gives them (xdg-mime
// query takes a tenth of a second, and this is asked whenever the window
// gets the focus).
func linkDefault() string {
	dirs := []string{configHome()}
	dirs = append(dirs, xdgDirs("XDG_CONFIG_DIRS", "/etc/xdg", "")...)
	dirs = append(dirs, filepath.Join(dataHome(), "applications"))
	dirs = append(dirs, xdgDirs("XDG_DATA_DIRS", "/usr/local/share:/usr/share", "applications")...)
	names := []string{}
	for _, d := range strings.Split(os.Getenv("XDG_CURRENT_DESKTOP"), ":") {
		if d != "" {
			names = append(names, strings.ToLower(d)+"-mimeapps.list")
		}
	}
	names = append(names, "mimeapps.list")
	for _, dir := range dirs {
		for _, name := range names {
			if entry := mimeDefault(filepath.Join(dir, name), linkMime); entry != "" {
				return entry
			}
		}
	}
	return ""
}

// xdgDirs splits one of the XDG variables that list directories, with sub
// under each.
func xdgDirs(env, fallback, sub string) []string {
	v := os.Getenv(env)
	if v == "" {
		v = fallback
	}
	var dirs []string
	for _, d := range strings.Split(v, ":") {
		if d != "" {
			dirs = append(dirs, filepath.Join(d, sub))
		}
	}
	return dirs
}

// mimeDefault is the first default application a mimeapps.list names for
// a MIME type.
func mimeDefault(path, mime string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	defaults := false
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "[") {
			defaults = line == "[Default Applications]"
			continue
		}
		if key, val, ok := strings.Cut(line, "="); ok && defaults && strings.TrimSpace(key) == mime {
			for _, entry := range strings.Split(val, ";") {
				if entry = strings.TrimSpace(entry); entry != "" {
					return entry
				}
			}
		}
	}
	return ""
}

// CanHandleLinks reports whether the app can be WhatsApp links' handler:
// it takes xdg-mime (xdg-utils) to choose it.
func CanHandleLinks() bool {
	_, err := exec.LookPath("xdg-mime")
	return err == nil
}

// RegisterLinks registers the app, started with args before the link, as
// a handler of whatsapp: links, and as their handler while no other app
// has been chosen for them.
func RegisterLinks(args []string) error {
	line, err := execLine(args, "%u")
	if err != nil {
		return err
	}
	err = writeEntry(linkFile(),
		"Type=Application",
		"Name=WazzapClients",
		"Comment=Open WhatsApp links in WazzapClients",
		"Exec="+line,
		"Icon=wazzapclients",
		"Terminal=false",
		"NoDisplay=true",
		"MimeType="+linkMime+";",
		"StartupWMClass=wazzapclients",
	)
	if err == errSame {
		return nil // as every start of a registered app finds it
	}
	if err != nil {
		return err
	}
	// The MIME cache lists handlers by type, where there is one.
	exec.Command("update-desktop-database", filepath.Dir(linkFile())).Run()
	if linkDefault() == "" {
		ClaimLinks()
	}
	return nil
}

// LinksRegistered reports whether RegisterLinks has run (for this user).
func LinksRegistered() bool {
	_, err := os.Stat(linkFile())
	return err == nil
}

// LinksHandled reports whether whatsapp: links open in this app.
func LinksHandled() bool { return linkDefault() == linkEntry }

// ClaimLinks makes the app, once registered, the handler of whatsapp:
// links, and reports whether it is now.
func ClaimLinks() bool {
	if exec.Command("xdg-mime", "default", linkEntry, linkMime).Run() != nil {
		return false
	}
	return LinksHandled()
}

// OpenLinkSettings does nothing: desktops have no one page to choose a
// scheme's app on (see ClaimLinks).
func OpenLinkSettings() {}
