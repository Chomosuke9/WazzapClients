//go:build !windows && !darwin

package filepick

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/godbus/dbus/v5"
)

// errNoPortal means the session has no file chooser portal, so open falls
// back to zenity or kdialog.
var errNoPortal = errors.New("filepick: no file chooser portal")

const (
	portalDest = "org.freedesktop.portal.Desktop"
	portalPath = dbus.ObjectPath("/org/freedesktop/portal/desktop")
)

// portalRule and portalFilter are the portal's filters, a(sa(us)): a name
// and its rules, each a glob (0) or a MIME type (1).
type portalRule struct {
	Kind    uint32
	Pattern string
}

type portalFilter struct {
	Name  string
	Rules []portalRule
}

// portalOpen asks the XDG desktop portal's FileChooser for files: the
// desktop's own dialog on GNOME, KDE, wlroots compositors such as Hyprland
// and Sway, and inside Flatpak, with nothing else installed.
func portalOpen(title string, multiple bool, filters []Filter) ([]string, error) {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return nil, errNoPortal
	}
	defer conn.Close()
	names := conn.Names()
	if len(names) == 0 {
		return nil, errNoPortal
	}
	var b [8]byte
	rand.Read(b[:])
	token := "wazzap" + hex.EncodeToString(b[:])
	// Listen before asking: the answer may come before the call returns.
	// Its path is predictable from our bus name and the token.
	sender := strings.ReplaceAll(strings.TrimPrefix(names[0], ":"), ".", "_")
	want := dbus.ObjectPath(string(portalPath) + "/request/" + sender + "/" + token)
	signals := make(chan *dbus.Signal, 8)
	conn.Signal(signals)
	listen := func(p dbus.ObjectPath) error {
		return conn.AddMatchSignal(dbus.WithMatchObjectPath(p),
			dbus.WithMatchInterface("org.freedesktop.portal.Request"), dbus.WithMatchMember("Response"))
	}
	if err := listen(want); err != nil {
		return nil, errNoPortal
	}

	opts := map[string]dbus.Variant{
		"handle_token": dbus.MakeVariant(token),
		"multiple":     dbus.MakeVariant(multiple),
		"modal":        dbus.MakeVariant(true),
	}
	if len(filters) > 0 {
		pf := make([]portalFilter, len(filters))
		for i, f := range filters {
			pf[i].Name = f.Name
			for _, e := range f.Exts {
				// Globs are case-sensitive in some dialogs: PHOTO.JPG too.
				pf[i].Rules = append(pf[i].Rules, portalRule{0, "*." + strings.ToLower(e)})
				if up := strings.ToUpper(e); up != strings.ToLower(e) {
					pf[i].Rules = append(pf[i].Rules, portalRule{0, "*." + up})
				}
			}
			if len(pf[i].Rules) == 0 {
				pf[i].Rules = []portalRule{{0, "*"}}
			}
		}
		opts["filters"] = dbus.MakeVariant(pf)
		opts["current_filter"] = dbus.MakeVariant(pf[0])
	}
	// The bus starts the portal if it isn't running yet, which takes a
	// moment; one that never answers falls back to the tools. Only the
	// request has a deadline: the dialog stays open as long as it takes.
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	var handle dbus.ObjectPath
	err = conn.Object(portalDest, portalPath).
		CallWithContext(ctx, "org.freedesktop.portal.FileChooser.OpenFile", 0, "", title, opts).Store(&handle)
	if err != nil {
		// No portal, or one without a FileChooser backend.
		return nil, errNoPortal
	}
	if handle != want {
		listen(handle) // portals before 0.9 made up their own path
	}
	for sig := range signals {
		if sig.Path != handle || sig.Name != "org.freedesktop.portal.Request.Response" || len(sig.Body) < 2 {
			continue
		}
		code, _ := sig.Body[0].(uint32)
		switch code {
		case 0:
		case 1:
			return nil, nil // cancelled
		default:
			return nil, fmt.Errorf("filepick: the file chooser portal failed (%d)", code)
		}
		results, _ := sig.Body[1].(map[string]dbus.Variant)
		uris, _ := results["uris"].Value().([]string)
		return uriPaths(uris), nil
	}
	return nil, errors.New("filepick: lost the session bus")
}

// uriPaths turns the portal's file:// URIs into paths.
func uriPaths(uris []string) []string {
	var paths []string
	for _, s := range uris {
		u, err := url.Parse(s)
		if err != nil || u.Scheme != "file" || u.Path == "" {
			continue
		}
		paths = append(paths, u.Path)
	}
	return paths
}
