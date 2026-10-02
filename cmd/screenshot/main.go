// Command screenshot renders the UI off-screen and saves PNG previews.
//
// With -compare it instead renders the reference demo account at the size
// and scale of a WhatsApp Desktop screenshot and writes both side by side.
package main

import (
	"flag"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"strings"
	"time"

	"gioui.org/gpu/headless"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
	_ "golang.org/x/image/webp"

	"github.com/chomosuke9/wazzapclients/internal/mock"
	"github.com/chomosuke9/wazzapclients/internal/model"
	"github.com/chomosuke9/wazzapclients/internal/ui"
	"github.com/chomosuke9/wazzapclients/internal/wa"
)

func main() {
	out := flag.String("out", "docs", "output directory")
	width := flag.Int("w", 1280, "width in dp")
	height := flag.Int("h", 800, "height in dp")
	scale := flag.Float64("scale", 1.5, "pixels per dp")
	compare := flag.String("compare", "", "WhatsApp screenshot to compare against")
	crop := flag.String("crop", "1002,0,2000,1250", "window rectangle within -compare (x0,y0,x1,y1)")
	chat := flag.String("chat", "test@g.us", "chat to open in -compare mode")
	memprofile := flag.String("memprofile", "", "write a heap profile after rendering")
	data := flag.String("data", "", "in -compare mode, render this session's stored chats (no network)")
	chatName := flag.String("chatname", "", "in -compare mode with -data, open the chat with this name")
	view := flag.String("view", "chats", "in -compare mode: chats, archived, status, channels, communities, settings, general, profile, account, privacy, lastseen, blocked, chatsettings, notifications, shortcuts, extras, help, info or contact")
	contact := flag.String("contact", "vivy@lid", "in -compare mode with -view contact, the group member whose contact info opens")
	infoScroll := flag.Int("infoscroll", 0, "in -compare mode with -view info, first visible item of the info panel")
	infoOffset := flag.Int("infooffset", 0, "with -infoscroll, pixels of that item scrolled out of view")
	win := flag.String("win", "", "in -compare mode, render a window of this size (W,H px) and crop it like the screenshot")
	rightAligned := flag.Bool("right", false, "with -win, the screenshot is the window's right edge")
	overlay := flag.String("overlay", "", "render only this overlay (menu, accounts, loginaccounts, slash, slashkick, slashrun, chatmenu, mute, lists, msgmenu, stickermenu, emoji, viewer, forward, reply, delete, select, mention, mentioned, attach, poll, contacts, invite, tray, quality, sendedit, sendcrop, sendfilter, senddoc, statusadd, statusmenu, statusprivacy, statustext, statussend, newchat, newnumber, newmembers, newgroup, search, membersearch) to <out>/overlay-<name>.png")
	at := flag.String("at", "600,300", "with -overlay, where menus open (x,y px)")
	overlayChat := flag.String("ochat", "rina", "with -overlay, the demo chat to open")
	filmName := flag.String("film", "", "render an animation's frames to <out>/film-<name>.png: an -overlay name, info, message, reorder, hover or typing")
	step := flag.Duration("step", 30*time.Millisecond, "with -film, time between frames")
	flag.Parse()
	if *memprofile != "" {
		defer func() {
			runtime.GC()
			f, err := os.Create(*memprofile)
			if err != nil {
				log.Fatal(err)
			}
			defer f.Close()
			if err := pprof.WriteHeapProfile(f); err != nil {
				log.Fatal(err)
			}
			var ms runtime.MemStats
			runtime.ReadMemStats(&ms)
			fmt.Printf("heap in use: %.1f MB, sys: %.1f MB\n", float64(ms.HeapInuse)/1e6, float64(ms.Sys)/1e6)
		}()
	}

	if *compare != "" {
		if err := compareShot(*compare, *crop, *win, *rightAligned, *chat, *chatName, *contact, *data, *out, *view, *infoScroll, *infoOffset, float32(*scale)); err != nil {
			log.Fatal(err)
		}
		return
	}

	if err := os.MkdirAll(*out, 0o755); err != nil {
		log.Fatal(err)
	}
	if *filmName != "" {
		var x, y int
		fmt.Sscanf(*at, "%d,%d", &x, &y)
		img, err := film(*filmName, *overlayChat, x, y, int(float64(*width)**scale), int(float64(*height)**scale), float32(*scale), *step)
		if err != nil {
			log.Fatal(err)
		}
		path := filepath.Join(*out, "film-"+*filmName+".png")
		if err := writePNG(path, img); err != nil {
			log.Fatal(err)
		}
		fmt.Println("wrote", path)
		return
	}
	if *overlay != "" {
		var x, y int
		fmt.Sscanf(*at, "%d,%d", &x, &y)
		u := ui.New(mock.New())
		u.Start(func() {})
		u.SetDark(true)
		u.SelectID(*overlayChat)
		u.ShowOverlay(*overlay, x, y)
		img, err := render(u, int(float32(*width)*float32(*scale)), int(float32(*height)*float32(*scale)), float32(*scale))
		if err != nil {
			log.Fatal(err)
		}
		path := filepath.Join(*out, "overlay-"+*overlay+".png")
		if err := writePNG(path, img); err != nil {
			log.Fatal(err)
		}
		fmt.Println("wrote", path)
		return
	}
	shots := []struct {
		name  string
		dark  bool
		chat  string
		login bool
		ref   bool
		page  string // "info" opens the chat's info panel
	}{
		{"preview-dark.png", true, "test@g.us", false, true, ""},
		{"preview-light.png", false, "rina", false, false, ""},
		{"preview-group.png", false, "family", false, false, ""},
		{"preview-empty.png", true, "", false, true, ""},
		{"preview-login.png", true, "", true, false, ""},
		{"preview-info.png", true, "test@g.us", false, true, "info"},
		{"preview-contact.png", false, "rina", false, false, "info"},
		{"preview-business.png", true, "test@g.us", false, true, "contact:vivy@lid"},
		{"preview-status.png", true, "", false, true, "status"},
		{"preview-channels.png", true, "", false, true, "channels"},
		{"preview-communities.png", true, "", false, true, "communities"},
		{"preview-settings.png", false, "", false, true, "settings"},
		{"preview-notifications.png", true, "", false, true, "notifications"},
	}
	for _, s := range shots {
		var b *mock.Backend
		if s.ref {
			b = mock.NewReference()
		} else {
			b = mock.New()
		}
		u := ui.New(b)
		if s.login {
			// A made-up pairing payload of realistic length.
			u.SetConn(model.ConnEvent{State: model.StateQR,
				QR: "2@Qm9ndXNQYWlyaW5nUmVmZXJlbmNlRm9yU2NyZWVuc2hvdHNPbmx5X19fX19fX19fX19f,ZmFrZU5vaXNlS2V5X19fX19fX19fX19fX19fX19fXw==,ZmFrZUlkZW50aXR5S2V5X19fX19fX19fX19fX19fXw==,ZmFrZUFkdlNlY3JldF9fX19fX19fX19fX19fX19fXw==,1"})
		} else {
			u.Start(func() {})
		}
		u.SetDark(s.dark)
		if s.chat != "" {
			u.SelectID(s.chat)
		}
		switch s.page {
		case "":
		case "info":
			u.ShowInfo(0, 0)
		default:
			if id, ok := strings.CutPrefix(s.page, "contact:"); ok {
				u.ShowContact(id, 0, 0)
				break
			}
			u.ShowPage(s.page)
		}
		img, err := render(u, int(float32(*width)*float32(*scale)), int(float32(*height)*float32(*scale)), float32(*scale))
		if err != nil {
			log.Fatalf("%s: %v", s.name, err)
		}
		path := filepath.Join(*out, s.name)
		if err := writePNG(path, img); err != nil {
			log.Fatal(err)
		}
		fmt.Println("wrote", path)
	}
}

func compareShot(path, crop, win string, rightAligned bool, chat, chatName, contact, data, outDir, view string, infoScroll, infoOffset int, scale float32) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	ref, _, err := image.Decode(f)
	f.Close()
	if err != nil {
		return err
	}
	var r image.Rectangle
	if _, err := fmt.Sscanf(crop, "%d,%d,%d,%d", &r.Min.X, &r.Min.Y, &r.Max.X, &r.Max.Y); err != nil {
		return fmt.Errorf("bad -crop: %w", err)
	}
	var u *ui.UI
	if data != "" {
		b, err := wa.Open(data, false)
		if err != nil {
			return err
		}
		defer b.Close()
		u = ui.New(b)
		u.Preview()
		u.SelectName(chatName)
	} else {
		u = ui.New(mock.NewReference())
		u.Start(func() {})
		u.SelectID(chat)
	}
	u.SetDark(true)
	switch view {
	case "chats":
	case "info":
		u.ShowInfo(infoScroll, infoOffset)
	case "contact":
		u.ShowContact(contact, infoScroll, infoOffset)
	case "statusviewer":
		u.ShowStatus(1)
	default:
		u.ShowPage(view)
	}
	var ours *image.RGBA
	if win != "" {
		var w, h int
		if _, err := fmt.Sscanf(win, "%d,%d", &w, &h); err != nil {
			return fmt.Errorf("bad -win: %w", err)
		}
		full, err := render(u, w, h, scale)
		if err != nil {
			return err
		}
		// The screenshot shows the window's left part, or its right part.
		off := r.Min
		if rightAligned {
			off.X = w - r.Dx()
		}
		ours = image.NewRGBA(image.Rect(0, 0, r.Dx(), r.Dy()))
		draw.Draw(ours, ours.Bounds(), full, off, draw.Src)
	} else {
		var err error
		ours, err = render(u, r.Dx(), r.Dy(), scale)
		if err != nil {
			return err
		}
	}
	gap := 12
	out := image.NewRGBA(image.Rect(0, 0, 2*r.Dx()+gap, r.Dy()))
	draw.Draw(out, out.Bounds(), image.White, image.Point{}, draw.Src)
	draw.Draw(out, image.Rect(0, 0, r.Dx(), r.Dy()), ours, image.Point{}, draw.Src)
	draw.Draw(out, image.Rect(r.Dx()+gap, 0, 2*r.Dx()+gap, r.Dy()), ref, r.Min, draw.Src)
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}
	for name, img := range map[string]image.Image{"compare.png": out, "ours.png": ours} {
		p := filepath.Join(outDir, name)
		if err := writePNG(p, img); err != nil {
			return err
		}
		fmt.Println("wrote", p)
	}
	return nil
}

func render(u *ui.UI, w, h int, scale float32) (*image.RGBA, error) {
	win, err := headless.NewWindow(w, h)
	if err != nil {
		return nil, err
	}
	defer win.Release()

	var ops op.Ops
	// Lay out a few times: lists settle their scroll position on the first
	// pass, and images decode in the background.
	for i := 0; i < 6; i++ {
		ops.Reset()
		gtx := layout.Context{
			Ops:         &ops,
			Now:         time.Now(),
			Metric:      unit.Metric{PxPerDp: scale, PxPerSp: scale},
			Constraints: layout.Exact(image.Pt(w, h)),
		}
		u.Layout(gtx)
		time.Sleep(150 * time.Millisecond)
	}
	if err := win.Frame(&ops); err != nil {
		return nil, err
	}
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	if err := win.Screenshot(img); err != nil {
		return nil, err
	}
	return img, nil
}

func writePNG(path string, img image.Image) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return png.Encode(f, img)
}
