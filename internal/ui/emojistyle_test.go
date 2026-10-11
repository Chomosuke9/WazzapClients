package ui

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"gioui.org/font"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/text"
	"gioui.org/unit"
	fontapi "github.com/go-text/typesetting/font"
	"golang.org/x/image/math/fixed"

	"github.com/chomosuke9/wazzapclients/internal/emojiset"
	"github.com/chomosuke9/wazzapclients/internal/mock"
	"github.com/chomosuke9/wazzapclients/internal/sticker"
)

func TestEmojiGlyphNames(t *testing.T) {
	for in, want := range map[string]string{
		"😀":     "1f600",
		"❤️":    "2764",
		"👍🏽":    "1f44d_1f3fd",
		"#️⃣":   "0023_20e3",
		"🇺🇸":    "1f1fa_1f1f8",
		"🏳️‍🌈":  "1f3f3_200d_1f308",
		"👨‍👩‍👧": "1f468_200d_1f469_200d_1f467",
	} {
		_, gs := shapeEmoji([]rune(in))
		if len(gs) != 1 {
			t.Errorf("%q shapes to %d glyphs", in, len(gs))
			continue
		}
		if got := emojiGlyphName(gs[0].GlyphID); got != want {
			t.Errorf("%q is named %q, want %q", in, got, want)
		}
	}
	// The font's spaces and digits aren't pictures.
	_, gs := shapeEmoji([]rune(" 1"))
	for _, g := range gs {
		if n := emojiGlyphName(g.GlyphID); n != "" {
			t.Errorf("glyph %d is named %q", g.GlyphID, n)
		}
	}
}

// useEmojiStore gives the test a store of its own in a temporary folder,
// with files in it, that downloads nothing: it sends each URL asked for to
// asked and answers 404.
func useEmojiStore(t *testing.T, style emojiStyleKind, files map[string][]byte) (dir string, asked chan string) {
	t.Helper()
	dir = t.TempDir()
	for name, data := range files {
		p := filepath.Join(dir, name)
		os.MkdirAll(filepath.Dir(p), 0o700)
		if err := os.WriteFile(p, data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	asked = make(chan string, 64)
	s := emojiset.New(dir)
	s.Fetch = func(url string) ([]byte, int, error) {
		asked <- url
		return nil, http.StatusNotFound, nil
	}
	old, oldStyle := emojiStore, emojiStyle()
	emojiStore = s
	emojiStyleNow.Store(int32(style))
	t.Cleanup(func() {
		emojiStore = old
		emojiStyleNow.Store(int32(oldStyle))
	})
	return dir, asked
}

func solidPNG(w, h int, c color.NRGBA) []byte {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for i := 0; i < len(img.Pix); i += 4 {
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = c.R, c.G, c.B, c.A
	}
	var b bytes.Buffer
	png.Encode(&b, img)
	return b.Bytes()
}

// TestEmoji3D: in the 3D style the font's pictures are the downloaded
// ones, for the shaper (text) and for emojiPNG (picker, jumbo emojis).
// Those not downloaded yet are asked for and show in 2D meanwhile.
func TestEmoji3D(t *testing.T) {
	grin := solidPNG(136, 128, color.NRGBA{R: 255, A: 255})
	_, asked := useEmojiStore(t, emoji3D, map[string][]byte{"3d/1f600.png": grin})

	if !bytes.Equal(emojiPNG("😀"), grin) {
		t.Error("emojiPNG doesn't give the 3D picture")
	}
	// The shaper's copy of the font.
	var face *fontapi.Face
	for _, f := range bundledFonts() {
		if f.Font.Typeface == emojiTypeface {
			face = f.Face.Face()
		}
	}
	_, gs := shapeEmoji([]rune("😀"))
	if bm, ok := face.GlyphDataBitmap(gs[0].GlyphID); !ok || !bytes.Equal(bm.Data, grin) {
		t.Error("the shaper's emoji font doesn't give the 3D picture")
	}

	smiley := emojiPNG("😃")
	if len(smiley) == 0 || bytes.Equal(smiley, grin) {
		t.Error("an emoji not downloaded yet isn't the 2D one")
	}
	select {
	case url := <-asked:
		if want := "emoji_u1f603.png"; !bytes.HasSuffix([]byte(url), []byte(want)) {
			t.Errorf("asked for %s, want ...%s", url, want)
		}
	case <-time.After(5 * time.Second):
		t.Error("the 3D picture wasn't downloaded")
	}

	emojiStyleNow.Store(int32(emoji2D))
	if bytes.Equal(emojiPNG("😀"), grin) {
		t.Error("the 2D style shows the 3D picture")
	}
}

// TestEmojiShaperRefresh: Gio's shaper keeps the pictures it drew, so the
// UI gets a new one when the 3D style comes or goes, and at most once a
// second as 3D pictures arrive.
func TestEmojiShaperRefresh(t *testing.T) {
	useEmojiStore(t, emoji2D, nil)
	u := New(mock.New())
	gtx := layout.Context{Ops: new(op.Ops), Now: testNow()}
	u.refreshEmoji(gtx)
	sh := u.th.Shaper
	if u.emojiView.face < 0 {
		t.Fatal("the emoji font's face wasn't found")
	}

	u.setEmojiStyle(emojiAnimated)
	u.refreshEmoji(gtx)
	if u.th.Shaper != sh {
		t.Error("going from 2D to animated (2D in text) changed the shaper")
	}
	u.setEmojiStyle(emoji3D)
	u.refreshEmoji(gtx)
	if u.th.Shaper == sh {
		t.Fatal("going to 3D kept the shaper")
	}
	if u.backend.Pref(prefEmojiStyle) != "3d" || !slices.Contains(appPrefs, prefEmojiStyle) {
		t.Error("the style isn't kept as an app preference")
	}

	// A picture arrives.
	sh = u.th.Shaper
	gtx.Now = gtx.Now.Add(100 * time.Millisecond)
	emojiStore.Clear() // changes Gen as an arrival does
	u.refreshEmoji(gtx)
	if u.th.Shaper != sh {
		t.Error("the shaper changed within a second of the last")
	}
	gtx.Now = gtx.Now.Add(time.Second)
	u.refreshEmoji(gtx)
	if u.th.Shaper == sh {
		t.Error("the shaper didn't change a second later")
	}

	if u2 := New(u.backend); emojiStyle() != emoji3D || u2 == nil {
		t.Error("a new UI doesn't read the style")
	}
}

// animatedWebP makes an animated WebP, 512 pixels square, of solid frames
// of the colors, each shown for 100 ms.
func animatedWebP(t *testing.T, colors ...color.NRGBA) []byte {
	t.Helper()
	le24 := func(v int) []byte { return []byte{byte(v), byte(v >> 8), byte(v >> 16)} }
	chunk := func(id string, p []byte) []byte {
		b := append([]byte(id), binary.LittleEndian.AppendUint32(nil, uint32(len(p)))...)
		b = append(b, p...)
		if len(p)&1 != 0 {
			b = append(b, 0)
		}
		return b
	}
	body := []byte("WEBP")
	body = append(body, chunk("VP8X", append([]byte{0x12, 0, 0, 0}, append(le24(511), le24(511)...)...))...)
	body = append(body, chunk("ANIM", []byte{0, 0, 0, 0, 0, 0})...)
	for _, c := range colors {
		still, err := sticker.FromImage(solidPNG(512, 512, c), sticker.Text{})
		if err != nil {
			t.Fatal(err)
		}
		i := bytes.Index(still, []byte("VP8L"))
		if i < 0 {
			t.Fatal("no VP8L chunk in a sticker")
		}
		n := int(binary.LittleEndian.Uint32(still[i+4:]))
		frame := append(append(le24(0), le24(0)...), append(le24(511), le24(511)...)...)
		frame = append(frame, le24(100)...)
		frame = append(frame, 0x02) // no blending
		frame = append(frame, chunk("VP8L", still[i+8:i+8+n])...)
		body = append(body, chunk("ANMF", frame)...)
	}
	return append(append([]byte("RIFF"), binary.LittleEndian.AppendUint32(nil, uint32(len(body)))...), body...)
}

// TestEmojiAnimation: an animated emoji plays from frames decoded once at
// the size it shows at, and goes when it hasn't shown for a while.
func TestEmojiAnimation(t *testing.T) {
	red, blue := color.NRGBA{R: 255, A: 255}, color.NRGBA{B: 255, A: 255}
	_, asked := useEmojiStore(t, emojiAnimOutside, map[string][]byte{
		"animated/1f600.webp": animatedWebP(t, red, blue),
	})
	if !animateOthers() || animateEmojis() {
		t.Fatal("the style animates the wrong emojis")
	}
	u := New(mock.New())
	gtx := layout.Context{Ops: new(op.Ops), Now: testNow()}
	const side = 30
	if u.drawEmojiAnim(gtx, "1f600", side, image.Pt(0, 0)) {
		t.Fatal("drawn before it decoded")
	}
	k := emojiAnimKey{"1f600", side}
	var a *emojiAnim
	for end := time.Now().Add(10 * time.Second); ; {
		u.emojiAnims.mu.Lock()
		a = u.emojiAnims.m[k]
		done := a != nil && (a.sheet != nil || a.failed)
		u.emojiAnims.mu.Unlock()
		if done {
			break
		}
		if time.Now().After(end) {
			t.Fatal("never decoded")
		}
		time.Sleep(5 * time.Millisecond)
	}
	if len(a.delays) != 2 || a.sheet.Bounds().Size() != image.Pt(2*side, side) {
		t.Fatalf("decoded %d frames on a %v sheet", len(a.delays), a.sheet.Bounds().Size())
	}
	at := func(i int) color.RGBA { return a.sheet.RGBAAt(a.cell(i, side).X+side/2, a.cell(i, side).Y+side/2) }
	if c := at(0); c.R < 250 || c.B > 5 {
		t.Errorf("the first frame is %v, want red", c)
	}
	if c := at(1); c.B < 250 || c.R > 5 {
		t.Errorf("the second frame is %v, want blue", c)
	}
	if !u.drawEmojiAnim(gtx, "1f600", side, image.Pt(0, 0)) {
		t.Fatal("not drawn once decoded")
	}
	if u.emojiAnims.next.IsZero() {
		t.Error("no frame asked for the next picture")
	} else if d := u.emojiAnims.next.Sub(u.emojiAnims.start); d%(time.Second/time.Duration(perf.emojiFPS)) != 0 {
		t.Errorf("the next picture is asked for %v after the start, off the emojis' ticks", d)
	}
	u.emojiAnims.endFrame(gtx)
	if u.emojiAnims.m[k] == nil {
		t.Error("dropped while showing")
	}

	// One with no animation (404) draws still, and is asked for once.
	if u.drawEmojiAnim(gtx, "1f603", side, image.Pt(0, 0)) {
		t.Error("an emoji without an animation was drawn animated")
	}
	select {
	case <-asked:
	case <-time.After(5 * time.Second):
		t.Error("the animation wasn't downloaded")
	}

	gtx.Now = gtx.Now.Add(emojiAnimKeep + time.Second)
	u.emojiAnims.endFrame(gtx)
	if u.emojiAnims.m[k] != nil || u.emojiAnims.held != 0 {
		t.Errorf("kept after not showing, %d bytes held", u.emojiAnims.held)
	}
}

// TestTextEmojiStill: emojis in message text are drawn from u.emojiImgs
// at the size they show at, not from the shaper's cache of full-size
// pictures, which kept them for good.
func TestTextEmojiStill(t *testing.T) {
	useEmojiStore(t, emoji2D, nil)
	u := New(mock.New())
	gtx := layout.Context{Ops: new(op.Ops), Now: testNow()}
	u.refreshEmoji(gtx)
	sh := u.th.Shaper
	sh.LayoutString(text.Parameters{Font: font.Font{Typeface: typeface}, PxPerEm: fixed.I(22), MaxWidth: 1000}, "😀")
	g, ok := sh.NextGlyph()
	if !ok {
		t.Fatal("no glyph")
	}
	r := image.Rect(0, 0, 26, 24)
	key := u.textEmojiKey("1f600", 26)
	for end := time.Now().Add(5 * time.Second); ; time.Sleep(5 * time.Millisecond) {
		if !u.textEmoji(gtx, g, r) {
			t.Fatal("left to the font")
		}
		if e := u.emojiImgs.get(key, 26, func() []byte { return nil }); e.state == imgReady {
			if e.size.X > 26 || e.size.Y > 26 {
				t.Errorf("decoded at %v, want to fit 26 pixels", e.size)
			}
			break
		}
		if time.Now().After(end) {
			t.Fatal("never decoded")
		}
	}
}

// TestLabelEmojiAnimation: outside messages, as in a label, emojis play
// in both animated styles; in message text only in "2D animated".
func TestLabelEmojiAnimation(t *testing.T) {
	useEmojiStore(t, emoji2D, map[string][]byte{
		"animated/1f600.webp": animatedWebP(t, color.NRGBA{R: 255, A: 255}, color.NRGBA{B: 255, A: 255}),
	})
	u := New(mock.New())
	u.setEmojiStyle(emojiAnimOutside)
	if !animateOthers() || animateEmojis() {
		t.Fatal("animated outside messages plays message text, or nothing else")
	}
	gtx := layout.Context{Ops: new(op.Ops), Now: testNow(), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1},
		Constraints: layout.Constraints{Max: image.Pt(400, 100)}}
	u.refreshEmoji(gtx)
	played := func() bool {
		u.emojiAnims.mu.Lock()
		defer u.emojiAnims.mu.Unlock()
		for k, a := range u.emojiAnims.m {
			if k.name == "1f600" && a.sheet != nil {
				return true
			}
		}
		return false
	}
	for end := time.Now().Add(10 * time.Second); !played(); time.Sleep(5 * time.Millisecond) {
		gtx.Ops.Reset()
		u.label(16, "Hi 😀", u.pal.Text).Layout(gtx)
		if time.Now().After(end) {
			t.Fatal("the label's emoji never played")
		}
	}
	u.setEmojiStyle(emojiAnimated)
	if !animateOthers() || !animateEmojis() {
		t.Error("2D animated doesn't play everything")
	}
}

// TestEmojiStyleSettings: Settings > Chats > Emoji style picks the style.
func TestEmojiStyleSettings(t *testing.T) {
	useEmojiStore(t, emoji2D, map[string][]byte{"3d/1f600.png": solidPNG(136, 128, color.NRGBA{A: 255})})
	u := New(mock.New())
	u.openSettings(settingChats)
	var open *settingRow
	for _, sec := range u.settingsRows() {
		for i, r := range sec.rows {
			if r.key == prefEmojiStyle {
				open = &sec.rows[i]
			}
		}
	}
	if open == nil || open.sub != "2D" {
		t.Fatal("no Emoji style row showing 2D on the Chats page")
	}
	open.run()
	if u.settingsTitle() != "Emoji style" {
		t.Fatalf("opened %q", u.settingsTitle())
	}
	page := u.settingsRows()
	var titles []string
	for _, r := range page[0].rows {
		titles = append(titles, r.title)
	}
	if want := []string{"2D", "2D, animated outside messages", "2D animated", "3D"}; !slices.Equal(titles, want) {
		t.Fatalf("styles %q, want %q", titles, want)
	}
	if !page[0].rows[0].on {
		t.Error("2D isn't the default")
	}
	page[0].rows[3].run()
	if emojiStyle() != emoji3D || u.backend.Pref(prefEmojiStyle) != "3d" {
		t.Error("choosing 3D didn't set it")
	}
	clearRow := page[1].rows[0]
	if clearRow.run == nil {
		t.Fatalf("the downloaded pictures aren't counted: %q", clearRow.sub)
	}
	clearRow.run()
	if emojiStore.Size() != 0 {
		t.Error("Delete downloaded emojis left files")
	}
}

// TestEmojiFontArrives: without the 2D font (before its download ends) no
// emoji is drawn from it; when it arrives the UI loads it and gets a new
// shaper.
func TestEmojiFontArrives(t *testing.T) {
	loaded := emojiFonts.Load()
	emojiFonts.Store(nil)
	t.Cleanup(func() { emojiFonts.Store(loaded); jumbos.m = nil })
	jumbos.m = nil
	dir, asked := useEmojiStore(t, emoji2D, nil)

	u := New(mock.New())
	gtx := layout.Context{Ops: new(op.Ops), Now: testNow()}
	u.refreshEmoji(gtx)
	sh := u.th.Shaper
	if jumboEmojis("😀") != nil || emojiPNG("😀") != nil {
		t.Error("emojis drawn from a font that isn't there")
	}
	select {
	case url := <-asked:
		if !strings.HasSuffix(url, "/NotoColorEmoji.ttf") {
			t.Errorf("asked for %s, want the font", url)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the font wasn't downloaded")
	}

	// It arrives (as a new store would find it).
	font, err := os.ReadFile(testEmojiFont)
	if err != nil {
		t.Fatal(err)
	}
	os.MkdirAll(filepath.Join(dir, "font"), 0o700)
	os.WriteFile(filepath.Join(dir, "font", "NotoColorEmoji.ttf"), font, 0o600)
	emojiStore = emojiset.New(dir)
	u.refreshEmoji(gtx)
	if loadEmojiFont() == nil || u.th.Shaper == sh {
		t.Fatal("the font that arrived wasn't loaded into a new shaper")
	}
	if len(jumboEmojis("😀")) != 1 || emojiPNG("😀") == nil {
		t.Error("no emojis from the font that arrived")
	}
}
