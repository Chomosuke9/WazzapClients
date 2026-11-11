package ui

import (
	"image/color"
	"testing"

	"github.com/chomosuke9/wazzapclients/internal/mock"
)

// TestSchemePalettes checks every palette's flavors: each has its scheme's
// accent, text reads on what it's drawn on, and a palette is built once.
func TestSchemePalettes(t *testing.T) {
	for i := range schemes {
		s := &schemes[i]
		for _, dark := range []bool{false, true} {
			f := s.flavor(dark)
			if _, ok := f.hue(s.accent); !ok {
				t.Fatalf("%s: no hue %q", s.id, s.accent)
			}
			p := appPalette(dark, s.id, "Red", "")
			if p != appPalette(dark, s.id, "", "no such hue") {
				t.Errorf("%s: the default accent's palette was built twice", s.id)
			}
			if len(p.Members) != len(darkPalette.Members) || len(p.Senders) != len(darkPalette.Senders) ||
				len(p.AvatarBgs) != len(darkPalette.AvatarBgs) {
				t.Errorf("%s: %d members, %d senders, %d avatar colors", s.id, len(p.Members), len(p.Senders), len(p.AvatarBgs))
			}
			for _, c := range []struct {
				name   string
				fg, bg color.NRGBA
				min    float64
			}{
				{"text on the panel", p.Text, p.Panel, 4.5},
				{"text in a bubble", p.Text, p.BubbleIn, 4.5},
				{"text in your bubble", p.TextOut, p.BubbleOut, 4}, // tinted at least enough to tell
				{"secondary text on the panel", p.TextSecondary, p.Panel, 3},
				{"the badge's count", p.OnGreen, p.Green, 3},
				{"a bubble on the wallpaper", p.BubbleIn, p.ChatBg, 1.15},
			} {
				// Tokyo Night Day's own text on its background is 4.4, and
				// less on a bubble tinted enough to tell.
				if f == &tokyoDay {
					c.min = min(c.min, 3.5)
				}
				if got := contrast(c.fg, c.bg); got < c.min {
					t.Errorf("%s (dark %v): %s has contrast %.2f, want %.1f", s.id, dark, c.name, got, c.min)
				}
			}
			for _, h := range f.hues {
				if q := appPalette(dark, s.id, "", h.id); q.Green != rgb(h.c) {
					t.Errorf("%s: accent %s = %v", s.id, h.id, q.Green)
				}
			}
		}
	}
	if appPalette(true, "", "", "") != &darkPalette || appPalette(false, "nope", "", "blue") != &lightPalette {
		t.Error("no palette should be WhatsApp's")
	}
}

// TestSchemePref switches the palette as Settings does.
func TestSchemePref(t *testing.T) {
	u := New(mock.New())
	u.Start(func() {})
	u.setTheme("dark")
	u.setScheme("tokyonight")
	if u.pal.ChatBg != rgb(tokyoNight.base) || u.pal.Green != rgb(0x7aa2f7) {
		t.Errorf("Tokyo Night: chat %v, accent %v", u.pal.ChatBg, u.pal.Green)
	}
	u.setSchemeAccent(schemeOf("tokyonight"), "red")
	u.setTheme("light")
	if u.pal.ChatBg != rgb(tokyoDay.crust) || u.pal.Green != rgb(0xf52a65) {
		t.Errorf("Tokyo Night Day in red: chat %v, accent %v", u.pal.ChatBg, u.pal.Green)
	}
	u.setScheme("")
	if u.pal != &lightPalette {
		t.Error("WhatsApp's palette didn't come back")
	}
}

// TestSchemeChatTheme gives a chat one of WhatsApp's themes under a palette:
// it takes the palette's hue for that theme, not WhatsApp's colors.
func TestSchemeChatTheme(t *testing.T) {
	u := New(mock.New())
	u.Start(func() {})
	u.setTheme("dark")
	indigo := schemeIndex("Pearl-Indigo")
	u.setChatTheme("rina", indigo)
	if p := u.lookPalette(u.lookOf("rina")); p.BubbleOut != rgb(chatThemes[indigo].dark.out) {
		t.Errorf("WhatsApp's Pearl Indigo bubble = %v", p.BubbleOut)
	}
	u.setScheme("ayu")
	u.setSchemeAccent(schemeOf("ayu"), "red")
	app := u.pal
	p := u.lookPalette(u.lookOf("rina"))
	if p.Green != rgb(0xd2a6ff) { // Ayu has no lavender: its purple
		t.Errorf("Pearl Indigo under Ayu accents with %v", p.Green)
	}
	if p.BubbleIn != app.BubbleIn || p.ChatBg != app.ChatBg || p.Doodle != app.Doodle || p.Text != app.Text {
		t.Errorf("the chat left Ayu: bubble %v, wallpaper %v", p.BubbleIn, p.ChatBg)
	}
	if u.pal != app || app.Green != rgb(0xf07178) {
		t.Errorf("the app's accent changed to %v", u.pal.Green)
	}
	// A grey theme keeps the palette's accent.
	u.setChatTheme("rina", schemeIndex("Dune-Mono"))
	if p := u.lookPalette(u.lookOf("rina")); p.BubbleOut != app.BubbleOut {
		t.Errorf("Dune Mono under Ayu: bubble %v", p.BubbleOut)
	}
}
