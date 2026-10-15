//go:build windows

package desktop

import (
	"os"
	"syscall"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// WhatsApp's links: websites send wa.me and api.whatsapp.com/send on to
// whatsapp://send?phone=…, and group invites to whatsapp://chat?code=….
// Windows starts the scheme's handler with the link as its last argument.
//
// The app registers itself the way Windows wants a browser or mail app
// to: a ProgID that opens the link, and Capabilities listed under
// RegisteredApplications, so Settings > Default apps has a page for it
// that lists WHATSAPP. Windows keeps the user's choice in a key apps can't
// write, so the user picks the app there (OpenLinkSettings), as they do a
// browser. The scheme's own key is written too, which is all Windows reads
// while nobody has chosen, but the app counts as the links' handler
// (LinksHandled) only once the user has chosen it.

const (
	linkScheme = "whatsapp"
	linkProgID = "WazzapClients.Link"
	linkApp    = "WazzapClients" // the RegisteredApplications value
	linkCaps   = `Software\WazzapClients\Capabilities`
	classes    = `Software\Classes\`
	choices    = `Software\Microsoft\Windows\Shell\Associations\UrlAssociations\` + linkScheme + `\`
)

// CanHandleLinks reports whether the app can be WhatsApp links' handler.
func CanHandleLinks() bool { return true }

// RegisterLinks registers the app, started with args before the link, as
// a handler of whatsapp: links, and as their handler while no other app
// has been chosen for them.
func RegisterLinks(args []string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	cmd := syscall.EscapeArg(exe)
	for _, a := range args {
		cmd += " " + syscall.EscapeArg(a)
	}
	cmd += ` "%1"`
	icon := exe + ",0"
	for _, key := range []string{classes + linkProgID, classes + linkScheme} {
		if err := setValues(key, map[string]string{"": "URL:WhatsApp link", "URL Protocol": ""}); err != nil {
			return err
		}
		if err := setValues(key+`\DefaultIcon`, map[string]string{"": icon}); err != nil {
			return err
		}
		if err := setValues(key+`\shell\open\command`, map[string]string{"": cmd}); err != nil {
			return err
		}
	}
	if err := setValues(linkCaps, map[string]string{
		"ApplicationName":        "WazzapClients",
		"ApplicationDescription": "Open WhatsApp links in WazzapClients",
		"ApplicationIcon":        icon,
	}); err != nil {
		return err
	}
	if err := setValues(linkCaps+`\URLAssociations`, map[string]string{linkScheme: linkProgID}); err != nil {
		return err
	}
	return setValues(`Software\RegisteredApplications`, map[string]string{linkApp: linkCaps})
}

func setValues(path string, vals map[string]string) error {
	k, _, err := registry.CreateKey(registry.CURRENT_USER, path, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	for name, v := range vals {
		if err := k.SetStringValue(name, v); err != nil {
			return err
		}
	}
	return nil
}

func readValue(path, name string) string {
	k, err := registry.OpenKey(registry.CURRENT_USER, path, registry.QUERY_VALUE)
	if err != nil {
		return ""
	}
	defer k.Close()
	v, _, _ := k.GetStringValue(name)
	return v
}

// LinksRegistered reports whether RegisterLinks has run (for this user).
func LinksRegistered() bool {
	return readValue(classes+linkProgID+`\shell\open\command`, "") != ""
}

// LinksHandled reports whether the user chose this app for whatsapp:
// links in Windows' settings. Until then Windows shows "Choose a default"
// for them, and another app installed later may take them.
func LinksHandled() bool {
	// Windows 11 24H2 keeps the choice in UserChoiceLatest.
	for _, key := range []string{"UserChoiceLatest", "UserChoice"} {
		if id := readValue(choices+key, "ProgId"); id != "" {
			return id == linkProgID
		}
	}
	return false
}

// OpenLinkSettings opens the app's page of Settings > Default apps, where
// the user picks it for whatsapp: links.
func OpenLinkSettings() {
	verb, _ := windows.UTF16PtrFromString("open")
	target, _ := windows.UTF16PtrFromString("ms-settings:defaultapps?registeredAppUser=" + linkApp)
	windows.ShellExecute(0, verb, target, nil, nil, windows.SW_SHOWNORMAL)
}
