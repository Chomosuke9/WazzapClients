//go:build !windows && !darwin

package filepick

import (
	"bufio"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/godbus/dbus/v5"
)

// fakeChooser answers OpenFile as a portal would, from another connection.
type fakeChooser struct {
	conn    *dbus.Conn
	code    uint32
	uris    []string
	title   string
	filters []portalFilter
}

func (f *fakeChooser) OpenFile(sender dbus.Sender, parent, title string, opts map[string]dbus.Variant) (dbus.ObjectPath, *dbus.Error) {
	f.title = title
	if v, ok := opts["filters"]; ok {
		v.Store(&f.filters)
	}
	token := opts["handle_token"].Value().(string)
	path := dbus.ObjectPath(string(portalPath) + "/request/" + strings.ReplaceAll(strings.TrimPrefix(string(sender), ":"), ".", "_") + "/" + token)
	go f.conn.Emit(path, "org.freedesktop.portal.Request.Response", f.code,
		map[string]dbus.Variant{"uris": dbus.MakeVariant(f.uris)})
	return path, nil
}

// privateBus starts a session bus of the test's own.
func privateBus(t *testing.T) {
	if _, err := exec.LookPath("dbus-daemon"); err != nil {
		t.Skip("no dbus-daemon")
	}
	// No service directories: the system's own portal mustn't start here.
	conf := filepath.Join(t.TempDir(), "bus.conf")
	os.WriteFile(conf, []byte(`<busconfig><type>session</type><listen>unix:dir=`+t.TempDir()+`</listen>
<policy context="default"><allow send_destination="*" eavesdrop="true"/><allow eavesdrop="true"/><allow own="*"/></policy></busconfig>`), 0o600)
	cmd := exec.Command("dbus-daemon", "--config-file="+conf, "--nofork", "--print-address")
	out, _ := cmd.StdoutPipe()
	if err := cmd.Start(); err != nil {
		t.Skip(err)
	}
	t.Cleanup(func() { cmd.Process.Kill(); cmd.Wait() })
	addr, err := bufio.NewReader(out).ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", strings.TrimSpace(addr))
}

func servePortal(t *testing.T, f *fakeChooser) {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	f.conn = conn
	conn.Export(f, portalPath, "org.freedesktop.portal.FileChooser")
	if r, err := conn.RequestName(portalDest, dbus.NameFlagDoNotQueue); err != nil || r != dbus.RequestNameReplyPrimaryOwner {
		t.Fatal("can't own the portal's name", err)
	}
}

func TestPortalOpen(t *testing.T) {
	privateBus(t)
	f := &fakeChooser{uris: []string{"file:///home/me/My%20photo.jpg", "file:///tmp/b.png"}}
	servePortal(t, f)
	paths, err := Open("Choose a photo", true, Filter{Name: "Images", Exts: []string{"jpg", "png"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 2 || paths[0] != "/home/me/My photo.jpg" || paths[1] != "/tmp/b.png" {
		t.Fatalf("paths %q", paths)
	}
	if f.title != "Choose a photo" || len(f.filters) != 1 || len(f.filters[0].Rules) != 4 || f.filters[0].Rules[0].Pattern != "*.jpg" {
		t.Fatalf("asked %q with filters %+v", f.title, f.filters)
	}
}

func TestPortalCancel(t *testing.T) {
	privateBus(t)
	servePortal(t, &fakeChooser{code: 1})
	paths, err := Open("Choose", false)
	if err != nil || paths != nil {
		t.Fatalf("cancel: %q, %v", paths, err)
	}
}

// TestNoPortal: a bus without the portal falls back to the tools (none
// here, with an empty PATH).
func TestNoPortal(t *testing.T) {
	privateBus(t)
	t.Setenv("PATH", t.TempDir())
	if _, err := portalOpen("Choose", false, nil); err != errNoPortal {
		t.Fatalf("got %v, want errNoPortal", err)
	}
	if _, err := Open("Choose", false); err != ErrUnsupported {
		t.Fatalf("got %v, want ErrUnsupported", err)
	}
}
