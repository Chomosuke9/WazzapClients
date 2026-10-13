package main

import (
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"math"
	"os"
	"path/filepath"
	"strings"

	"github.com/chomosuke9/wazzapclients/internal/mock"
	"github.com/chomosuke9/wazzapclients/internal/model"
	"github.com/chomosuke9/wazzapclients/internal/ui"
)

// demoQR is a made-up pairing payload of realistic length.
const demoQR = "2@Qm9ndXNQYWlyaW5nUmVmZXJlbmNlRm9yU2NyZWVuc2hvdHNPbmx5X19fX19fX19fX19f,ZmFrZU5vaXNlS2V5X19fX19fX19fX19fX19fX19fXw==,ZmFrZUlkZW50aXR5S2V5X19fX19fX19fX19fX19fXw==,ZmFrZUFkdlNlY3JldF9fX19fX19fX19fX19fX19fXw==,1"

// readmeShot is one of the README's screenshots, rendered from the demo
// backend with the chat open and then the scene set up.
type readmeShot struct {
	name string // the file in docs/screenshots
	dark bool
	chat string
	// scene is a ShowOverlay name, "page:<name>" for ShowPage, "info" for
	// the open chat's info panel, "login" for the QR code, or "".
	scene string
	at    image.Point // where a menu opens, in px at the README's scale
}

// readmeShots are every picture in README.md but the hero (heroShot). Add
// a shot here when the README shows a new one.
var readmeShots = []readmeShot{
	{name: "login.png", scene: "login"},
	{name: "group-info.png", dark: true, chat: "work", scene: "info"},
	{name: "reactions.png", chat: "rina", scene: "msgmenu", at: image.Pt(900, 600)},
	{name: "search.png", dark: true, chat: "rina", scene: "search"},
	{name: "emoji.png", chat: "rina", scene: "emoji"},
	{name: "gallery.png", dark: true, chat: "rina", scene: "gallery"},
	{name: "poll.png", dark: true, chat: "design", scene: "votes"},
	{name: "msginfo.png", dark: true, chat: "rina", scene: "msginfo"},
	{name: "status.png", chat: "rina", scene: "page:status"},
	{name: "channels.png", dark: true, chat: "rina", scene: "page:channels"},
	{name: "communities.png", chat: "rina", scene: "page:communities"},
	{name: "editor.png", dark: true, chat: "rina", scene: "sendedit"},
	{name: "filter.png", dark: true, chat: "rina", scene: "sendfilter"},
	{name: "slash.png", dark: true, chat: "work", scene: "slash"},
	{name: "calc.png", chat: "work", scene: "slashcalc"},
	{name: "scheduled.png", dark: true, chat: "work", scene: "scheduled"},
	{name: "edits.png", chat: "rina", scene: "edits"},
	{name: "privacy.png", dark: true, chat: "rina", scene: "privacy"},
	{name: "ghost.png", dark: true, chat: "work", scene: "ghost"},
}

// renderReadme writes the hero and every readmeShot into out, w×h px.
func renderReadme(out string, w, h int, scale float32) error {
	// The send view and photo editor show a drawn beach, unless
	// $WAZZAP_DEMO_PHOTO names a photo of your own.
	if os.Getenv("WAZZAP_DEMO_PHOTO") == "" {
		dir, err := os.MkdirTemp("", "wazzap-readme")
		if err != nil {
			return err
		}
		defer os.RemoveAll(dir)
		photo := filepath.Join(dir, "beach.jpg")
		if err := writeBeach(photo); err != nil {
			return err
		}
		os.Setenv("WAZZAP_DEMO_PHOTO", photo)
		defer os.Unsetenv("WAZZAP_DEMO_PHOTO")
	}
	hero, err := heroShot(w, h, scale)
	if err != nil {
		return err
	}
	if err := writeShot(out, "hero.png", hero); err != nil {
		return err
	}
	for _, s := range readmeShots {
		u := ui.New(mock.New())
		if s.scene == "login" {
			u.SetConn(model.ConnEvent{State: model.StateQR, QR: demoQR})
		} else {
			u.Start(func() {})
		}
		u.SetDark(s.dark)
		if s.chat != "" {
			u.SelectID(s.chat)
		}
		switch {
		case s.scene == "" || s.scene == "login":
		case s.scene == "info":
			u.ShowInfo(0, 0)
		case strings.HasPrefix(s.scene, "page:"):
			u.ShowPage(strings.TrimPrefix(s.scene, "page:"))
		default:
			u.ShowOverlay(s.scene, s.at.X, s.at.Y)
		}
		img, err := render(u, w, h, scale)
		if err != nil {
			return fmt.Errorf("%s: %w", s.name, err)
		}
		if err := writeShot(out, s.name, img); err != nil {
			return err
		}
	}
	return nil
}

func writeShot(out, name string, img image.Image) error {
	path := filepath.Join(out, name)
	if err := writePNG(path, img); err != nil {
		return err
	}
	fmt.Println("wrote", path)
	return nil
}

// heroShot renders the demo's first chat in the light and the dark theme
// and joins them along a slanted line, light on the left.
func heroShot(w, h int, scale float32) (*image.RGBA, error) {
	var themes [2]*image.RGBA
	for i, dark := range []bool{false, true} {
		u := ui.New(mock.New())
		u.Start(func() {})
		u.SetDark(dark)
		u.SelectID("rina")
		img, err := render(u, w, h, scale)
		if err != nil {
			return nil, err
		}
		themes[i] = img
	}
	out, dark := themes[0], themes[1]
	for y := range h {
		// The edge runs from 66% of the width at the top to 50% at the bottom.
		edge := float64(w) * (0.66 - 0.16*float64(y)/float64(h))
		for x := int(edge); x < w; x++ {
			a := min(1, float64(x)+1-edge) // the edge's pixel is blended
			i := out.PixOffset(x, y)
			for c := range 4 {
				l, d := float64(out.Pix[i+c]), float64(dark.Pix[i+c])
				out.Pix[i+c] = uint8(l + (d-l)*a + 0.5)
			}
		}
	}
	return out, nil
}

// writeBeach draws a sunset over the sea, seen from the beach, as a JPEG.
func writeBeach(path string) error {
	const w, h = 1200, 900
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	mix := func(a, b color.RGBA, t float64) color.RGBA {
		t = max(0, min(1, t))
		f := func(x, y uint8) uint8 { return uint8(float64(x) + (float64(y)-float64(x))*t + 0.5) }
		return color.RGBA{f(a.R, b.R), f(a.G, b.G), f(a.B, b.B), 0xff}
	}
	sky0, sky1, sky2 := color.RGBA{0x3b, 0x2f, 0x8f, 0xff}, color.RGBA{0xc8, 0x5a, 0x8a, 0xff}, color.RGBA{0xff, 0x9a, 0x3c, 0xff}
	sea0, sea1 := color.RGBA{0x1a, 0x7c, 0xc4, 0xff}, color.RGBA{0x0a, 0x3a, 0x7a, 0xff}
	sun, sand0, sand1 := color.RGBA{0xff, 0xe9, 0x8a, 0xff}, color.RGBA{0xf6, 0xd9, 0x9c, 0xff}, color.RGBA{0xe9, 0xbf, 0x78, 0xff}
	horizon := 0.53 * h
	sx, sy, sr := 0.66*w, 0.46*h, 0.075*h
	for y := range h {
		fy := float64(y)
		for x := range w {
			fx := float64(x)
			var c color.RGBA
			shore := 0.78*h + 0.05*h*math.Sin(fx/w*math.Pi*1.4+0.6)
			switch {
			case fy >= shore:
				c = mix(sand0, sand1, (fy-shore)/(h-shore))
			case fy >= horizon:
				t := (fy - horizon) / (shore - horizon)
				c = mix(sea0, sea1, t)
				// The sun's reflection: stripes that narrow toward the shore.
				half := sr * (1 - 0.6*t)
				if math.Abs(fx-sx) < half && int(fy-horizon)%14 < 8 {
					c = mix(c, sun, 0.85-0.4*t)
				}
			default:
				t := fy / horizon
				if t < 0.6 {
					c = mix(sky0, sky1, t/0.6)
				} else {
					c = mix(sky1, sky2, (t-0.6)/0.4)
				}
				d := math.Hypot(fx-sx, fy-sy)
				if d < sr {
					c = sun
				} else if d < 3*sr {
					c = mix(c, sun, 0.6*math.Pow(1-(d-sr)/(2*sr), 2))
				}
			}
			img.SetRGBA(x, y, c)
		}
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return jpeg.Encode(f, img, &jpeg.Options{Quality: 90})
}
