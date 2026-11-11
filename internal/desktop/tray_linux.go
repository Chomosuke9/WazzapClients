//go:build linux

package desktop

import (
	"image"
	"sync"
	"time"

	"github.com/godbus/dbus/v5"
	"github.com/godbus/dbus/v5/prop"
)

// The tray icon is a StatusNotifierItem (KDE's specification, which
// freedesktop.org took over and every current panel follows): an object on
// the session bus that describes the icon, registered with the panel's
// StatusNotifierWatcher. Its menu is a com.canonical.dbusmenu object, which
// the panel draws itself.

const (
	sniWatcher  = "org.kde.StatusNotifierWatcher"
	sniWatchObj = dbus.ObjectPath("/StatusNotifierWatcher")
	sniItem     = "org.kde.StatusNotifierItem"
	sniPath     = dbus.ObjectPath("/StatusNotifierItem")
	menuIface   = "com.canonical.dbusmenu"
	menuPath    = dbus.ObjectPath("/MenuBar")

	menuOpen, menuQuit = 1, 3
)

// sniPixmap is one size of an icon, (iiay): its pixels are ARGB, a byte
// each in that order, not premultiplied.
type sniPixmap struct {
	W, H int32
	Data []byte
}

// sniToolTip is the ToolTip property, (sa(iiay)ss).
type sniToolTip struct {
	Icon        string
	Pixmaps     []sniPixmap
	Title, Text string
}

// menuNode is a dbusmenu item with its children, (ia{sv}av).
type menuNode struct {
	ID       int32
	Props    map[string]dbus.Variant
	Children []dbus.Variant
}

// menuProps is an item's properties, (ia{sv}).
type menuProps struct {
	ID    int32
	Props map[string]dbus.Variant
}

// menuEvent is one of EventGroup's events, (isvu).
type menuEvent struct {
	ID   int32
	Name string
	Data dbus.Variant
	Time uint32
}

var tray struct {
	sync.Mutex
	t     Tray
	conn  *dbus.Conn
	props *prop.Properties
	tip   string
}

// TrayStart shows the tray icon, and serves Lock's requests to show the
// window, until TrayStop. Without a panel that shows tray icons it fails,
// but Lock's requests are served all the same.
func TrayStart(t Tray) error {
	listen(&t)
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return ErrUnsupported
	}
	if !waitWatcher(conn, t.Wait) {
		conn.Close()
		return ErrUnsupported
	}
	tray.Lock()
	defer tray.Unlock()
	tray.t, tray.tip, tray.conn = t, t.Name, conn

	var icons []sniPixmap
	for _, px := range []int{16, 22, 24, 32, 48, 64} {
		icons = append(icons, pixmap(t.Icon(px)))
	}
	ro := func(v any) *prop.Prop { return &prop.Prop{Value: v, Emit: prop.EmitTrue} }
	tray.props, err = prop.Export(conn, sniPath, prop.Map{
		sniItem: {
			"Category":            ro("Communications"),
			"Id":                  ro("wazzapclients"),
			"Title":               ro(t.Name),
			"Status":              ro("Active"),
			"WindowId":            ro(uint32(0)),
			"IconName":            ro(""),
			"IconPixmap":          ro(icons),
			"OverlayIconName":     ro(""),
			"OverlayIconPixmap":   ro([]sniPixmap{}),
			"AttentionIconName":   ro(""),
			"AttentionIconPixmap": ro([]sniPixmap{}),
			"AttentionMovieName":  ro(""),
			"IconThemePath":       ro(""),
			"ToolTip":             ro(sniToolTip{Title: t.Name, Pixmaps: []sniPixmap{}}),
			"ItemIsMenu":          ro(false),
			"Menu":                ro(menuPath),
		},
	})
	if err == nil {
		_, err = prop.Export(conn, menuPath, prop.Map{
			menuIface: {
				"Version":       ro(uint32(3)),
				"TextDirection": ro("ltr"),
				"Status":        ro("normal"),
				"IconThemePath": ro([]string{}),
			},
		})
	}
	open := func() { go t.Open() }
	if err == nil {
		err = conn.ExportMethodTable(map[string]any{
			"Activate":          func(x, y int32) *dbus.Error { open(); return nil },
			"SecondaryActivate": func(x, y int32) *dbus.Error { open(); return nil },
			// KDE's panel hands over the click's activation token first.
			"ProvideXdgActivationToken": func(token string) *dbus.Error {
				if t.Activation != nil {
					t.Activation(token)
				}
				return nil
			},
			// The panel shows the menu itself; one that asks gets none.
			"ContextMenu": func(x, y int32) *dbus.Error { return nil },
			"Scroll":      func(delta int32, orientation string) *dbus.Error { return nil },
		}, sniPath, sniItem)
	}
	if err == nil {
		err = conn.ExportMethodTable(map[string]any{
			"GetLayout": func(parent, depth int32, names []string) (uint32, menuNode, *dbus.Error) {
				return 1, menuLayout(t.Name, parent), nil
			},
			"GetGroupProperties": func(ids []int32, names []string) ([]menuProps, *dbus.Error) {
				var out []menuProps
				for _, n := range menuLayout(t.Name, 0).Children {
					item := n.Value().(menuNode)
					if len(ids) == 0 || containsID(ids, item.ID) {
						out = append(out, menuProps{item.ID, item.Props})
					}
				}
				return out, nil
			},
			"GetProperty": func(id int32, name string) (dbus.Variant, *dbus.Error) {
				for _, n := range menuLayout(t.Name, 0).Children {
					if item := n.Value().(menuNode); item.ID == id {
						if v, ok := item.Props[name]; ok {
							return v, nil
						}
					}
				}
				return dbus.MakeVariant(""), nil
			},
			"Event": func(id int32, name string, data dbus.Variant, at uint32) *dbus.Error {
				menuClicked(t, id, name)
				return nil
			},
			"EventGroup": func(events []menuEvent) ([]int32, *dbus.Error) {
				for _, e := range events {
					menuClicked(t, e.ID, e.Name)
				}
				return []int32{}, nil
			},
			"AboutToShow": func(id int32) (bool, *dbus.Error) { return false, nil },
			"AboutToShowGroup": func(ids []int32) ([]int32, []int32, *dbus.Error) {
				return []int32{}, []int32{}, nil
			},
		}, menuPath, menuIface)
	}
	if err != nil {
		conn.Close()
		tray.conn = nil
		return err
	}
	register(conn)
	// A panel that starts again has forgotten the icon: give it again.
	conn.AddMatchSignal(
		dbus.WithMatchInterface("org.freedesktop.DBus"),
		dbus.WithMatchMember("NameOwnerChanged"),
		dbus.WithMatchArg(0, sniWatcher),
	)
	signals := make(chan *dbus.Signal, 8)
	conn.Signal(signals)
	go func() {
		for s := range signals {
			if s.Name != "org.freedesktop.DBus.NameOwnerChanged" || len(s.Body) < 3 {
				continue
			}
			if owner, _ := s.Body[2].(string); owner != "" {
				register(conn)
			}
		}
	}()
	return nil
}

// waitWatcher reports whether a panel that shows tray icons is up, waiting
// up to d for one.
func waitWatcher(conn *dbus.Conn, d time.Duration) bool {
	end := time.Now().Add(d)
	for {
		var has bool
		conn.BusObject().Call("org.freedesktop.DBus.NameHasOwner", 0, sniWatcher).Store(&has)
		if has {
			// The watcher can be up without anything that draws the icons.
			v, err := conn.Object(sniWatcher, sniWatchObj).GetProperty(sniWatcher + ".IsStatusNotifierHostRegistered")
			if host, ok := v.Value().(bool); err != nil || !ok || host {
				return true
			}
		}
		if !time.Now().Before(end) {
			return false
		}
		time.Sleep(250 * time.Millisecond)
	}
}

func register(conn *dbus.Conn) {
	names := conn.Names()
	if len(names) == 0 {
		return
	}
	conn.Object(sniWatcher, sniWatchObj).Call(sniWatcher+".RegisterStatusNotifierItem", dbus.FlagNoReplyExpected, names[0])
}

// menuLayout is the menu under the item parent: Open and Quit under the
// root, 0.
func menuLayout(name string, parent int32) menuNode {
	item := func(id int32, props map[string]any) dbus.Variant {
		m := map[string]dbus.Variant{}
		for k, v := range props {
			m[k] = dbus.MakeVariant(v)
		}
		return dbus.MakeVariant(menuNode{ID: id, Props: m, Children: []dbus.Variant{}})
	}
	root := menuNode{
		Props: map[string]dbus.Variant{"children-display": dbus.MakeVariant("submenu")},
		Children: []dbus.Variant{
			item(menuOpen, map[string]any{"label": "Open " + name, "enabled": true, "visible": true}),
			item(2, map[string]any{"type": "separator", "visible": true}),
			item(menuQuit, map[string]any{"label": "Quit " + name, "enabled": true, "visible": true}),
		},
	}
	if parent != 0 {
		for _, c := range root.Children {
			if n := c.Value().(menuNode); n.ID == parent {
				return n
			}
		}
	}
	return root
}

func containsID(ids []int32, id int32) bool {
	for _, x := range ids {
		if x == id {
			return true
		}
	}
	return false
}

func menuClicked(t Tray, id int32, event string) {
	if event != "clicked" {
		return
	}
	switch id {
	case menuOpen:
		go t.Open()
	case menuQuit:
		go t.Quit()
	}
}

// pixmap turns a picture into a StatusNotifierItem's.
func pixmap(img *image.RGBA) sniPixmap {
	b := img.Bounds()
	data := make([]byte, 0, b.Dx()*b.Dy()*4)
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			c := img.RGBAAt(x, y)
			if a := uint32(c.A); a != 0 && a != 0xff {
				c.R = uint8(min(uint32(c.R)*0xff/a, 0xff))
				c.G = uint8(min(uint32(c.G)*0xff/a, 0xff))
				c.B = uint8(min(uint32(c.B)*0xff/a, 0xff))
			}
			data = append(data, c.A, c.R, c.G, c.B)
		}
	}
	return sniPixmap{W: int32(b.Dx()), H: int32(b.Dy()), Data: data}
}

// TrayStop removes the tray icon.
func TrayStop() {
	tray.Lock()
	conn := tray.conn
	tray.conn, tray.props = nil, nil
	tray.Unlock()
	if conn != nil {
		conn.Close()
	}
}

// SetTooltip changes the text shown when the pointer rests on the icon.
func SetTooltip(s string) {
	tray.Lock()
	defer tray.Unlock()
	if tray.conn == nil || tray.tip == s {
		return
	}
	tray.tip = s
	tray.props.SetMust(sniItem, "ToolTip", sniToolTip{Title: s, Pixmaps: []sniPixmap{}})
	tray.conn.Emit(sniPath, sniItem+".NewToolTip")
}
