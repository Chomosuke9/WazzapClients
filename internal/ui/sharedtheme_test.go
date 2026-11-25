package ui

import (
	"bytes"
	"image"
	"image/jpeg"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chomosuke9/wazzapclients/internal/mock"
	"github.com/chomosuke9/wazzapclients/internal/model"
)

// Whichever look came last shows: yours, one set for everyone, yours
// again. Resetting the chat goes back to every chat's.
func TestSharedThemeLatestWins(t *testing.T) {
	b := mock.New()
	u := New(b)
	u.Start(func() {})
	u.applyEvents()
	u.SelectID("family")
	pink, blue := schemeIndex("Pink"), schemeIndex("Default-Blue")
	every := chatLook{scheme: schemeIndex("Lime"), wall: "color:3"}
	u.setLook(lookAll, every)
	mine := chatLook{scheme: pink, wall: "color:5"}
	u.setLook("family", mine)

	set := func(s model.SharedTheme) {
		b.SetSharedTheme("family", s)
		u.applyEvents()
	}
	red := model.SharedWall{Kind: model.WallColor, Light: 0xff0000, Dark: 0x800000}
	set(model.SharedTheme{Wall: red})
	l := u.lookOf("family")
	if l.scheme != pink || !l.bare {
		t.Errorf("a wallpaper for everyone gave %+v, want it with your bubbles and no doodles", l)
	}
	if w := u.wallOf(u.pal, l); w.bg != rgb(0xff0000) && w.bg != rgb(0x800000) {
		t.Errorf("its color is %v", w.bg)
	}
	set(model.SharedTheme{Scheme: "Default-Blue", Wall: red})
	if l := u.lookOf("family"); l.scheme != blue {
		t.Errorf("bubbles for everyone gave %+v", l)
	}
	if l := u.lookOf("rina"); l != every {
		t.Errorf("another chat's look became %+v", l)
	}

	// Yours again: the rest of what showed stays.
	u.setBubbles("family", pink)
	if l := u.lookOf("family"); l.scheme != pink || !strings.HasPrefix(l.wall, "solid:ff0000,800000") {
		t.Errorf("your bubbles over it gave %+v", l)
	}
	if b.SharedTheme("family") != nil {
		t.Error("the theme for everyone is still over yours")
	}
	u.themes = nil // read back from the prefs
	if l := u.lookOf("family"); !strings.HasPrefix(l.wall, "solid:ff0000,800000") {
		t.Errorf("it was kept as %+v", l)
	}

	set(model.SharedTheme{Scheme: "Red", Wall: model.SharedWall{Kind: model.WallDefault, Doodles: true}})
	if l := u.lookOf("family"); l != (chatLook{scheme: schemeIndex("Red"), doodles: true}) {
		t.Errorf("a theme for everyone gave %+v", l)
	}
	u.resetLook("family")
	if l := u.lookOf("family"); l != every {
		t.Errorf("after the reset it's %+v, want every chat's %+v", l, every)
	}

	// One reset for everyone drops your own too.
	u.setLook("family", mine)
	set(model.SharedTheme{Reset: true})
	if l := u.lookOf("family"); l != every {
		t.Errorf("reset for everyone it's %+v, want every chat's %+v", l, every)
	}
}

func TestSharedScheme(t *testing.T) {
	for id, want := range map[string]string{
		"Default-Blue@Minimal": "Default-Blue@Minimal", "Red@Minimal": "Red", "Emerald@Tonal@Minimal": "Emerald@Tonal",
		"Pink@Tonal": "Pink@Tonal", "Default": "", "Crimson@Tonal": "", "": "",
	} {
		if got := chatThemes[sharedScheme(id)].id; got != want {
			t.Errorf("%q shows as %q, want %q", id, got, want)
		}
	}
}

// Someone's photo shows from the backend's file; changing anything of the
// look keeps a copy, since that file goes.
func TestSharedPhotoKept(t *testing.T) {
	old := wallpaperDir
	wallpaperDir = t.TempDir()
	defer func() { wallpaperDir = old }()
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 8, 8)), nil); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(t.TempDir(), "x.jpeg")
	if err := os.WriteFile(src, buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	b := mock.New()
	u := New(b)
	u.Start(func() {})
	b.SetSharedTheme("family", model.SharedTheme{Wall: model.SharedWall{Kind: model.WallPhoto, Photo: src}})
	u.applyEvents()
	if w := u.wallOf(u.pal, u.lookOf("family")); w.photo != src {
		t.Errorf("the wallpaper is %q, want %q", w.photo, src)
	}
	u.setBubbles("family", schemeIndex("Pink"))
	w := u.wallOf(u.pal, u.lookOf("family"))
	if filepath.Dir(w.photo) != wallpaperDir {
		t.Fatalf("the wallpaper is %q, want a copy in %s", w.photo, wallpaperDir)
	}
	if data, err := os.ReadFile(w.photo); err != nil || !bytes.Equal(data, buf.Bytes()) {
		t.Errorf("the copy: %d bytes, %v", len(data), err)
	}
}

// What a look is set for everyone as, and that it shows the same there.
func TestShareable(t *testing.T) {
	old := wallpaperDir
	wallpaperDir = t.TempDir()
	defer func() { wallpaperDir = old }()
	u := New(mock.New())
	u.Start(func() {})
	u.applyEvents()
	for _, l := range []chatLook{
		{}, {bare: true}, {scheme: schemeIndex("Pink")}, {scheme: schemeIndex("Pink@Tonal"), bare: true},
		{scheme: schemeIndex("Default-Blue@Minimal")}, {scheme: schemeIndex("Pink"), wall: "default"},
		{scheme: schemeIndex("Pink"), wall: "plain"}, {wall: "theme:Lime"}, {wall: "color:4"}, {wall: "color:4", bare: true},
		{wall: "solid:112233,445566,00000000,80ffffff"}, {scheme: schemeIndex("Red"), wall: "photo:x.jpg@500,500"},
	} {
		s := u.shareable(l)
		got := u.sharedLook(chatLook{scheme: schemeIndex("Lime"), wall: "color:9"}, &s)
		if got.scheme != l.scheme {
			t.Errorf("%+v: the bubbles became %d", l, got.scheme)
		}
		for _, dark := range []bool{false, true} {
			u.SetDark(dark)
			u.lookPal = lookPal{}
			a, b := u.lookPalette(l), u.lookPalette(got)
			if a.ChatBg != b.ChatBg && a.WallPhoto == "" || a.WallPhoto != b.WallPhoto || a.Plain != b.Plain && a.WallPhoto == "" {
				t.Errorf("%+v: dark %v, shows %v %q plain %v for others, not %v %q plain %v", l, dark,
					b.ChatBg, b.WallPhoto, b.Plain, a.ChatBg, a.WallPhoto, a.Plain)
			}
		}
	}
}

// Any member sets a group's theme, unless only admins edit its settings.
func TestMayShareTheme(t *testing.T) {
	u := New(mock.New())
	u.Start(func() {})
	u.applyEvents()
	u.SelectID("rina")
	if !u.mayShareTheme(u.selected) {
		t.Error("a chat with one person can't have its theme set for both")
	}
	c := u.chatByID("family")
	info := u.chatMembers(c.ID)
	if info == nil || len(info.Members) == 0 {
		t.Fatal("family's members aren't known")
	}
	member := func(admin bool) {
		for i := range info.Members {
			if info.Members[i].Me {
				info.Members[i].Admin = admin
			}
		}
	}
	member(false)
	info.Locked = false
	if u.isAdminIn(info) || !u.mayShareTheme(c) || u.dialog.isOpen() {
		t.Error("a member can't set the theme of a group whose settings everyone edits")
	}
	info.Locked = true
	member(true)
	if !u.mayShareTheme(c) || u.dialog.isOpen() {
		t.Error("an admin can't set the theme of a group whose settings only admins edit")
	}
	member(false)
	if u.mayShareTheme(c) || !u.dialog.isOpen() {
		t.Error("a member may set the theme of a group whose settings only admins edit")
	}
}
