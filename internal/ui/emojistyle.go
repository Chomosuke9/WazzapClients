package ui

import (
	"errors"
	"fmt"
	"image"
	"math"
	"sort"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"gioui.org/font"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/text"
	fontapi "github.com/go-text/typesetting/font"
	"github.com/go-text/typesetting/font/opentype/tables"
	"golang.org/x/image/math/fixed"

	"github.com/chomosuke9/wazzapclients/internal/emojiset"
	"github.com/chomosuke9/wazzapclients/internal/model"
	"github.com/chomosuke9/wazzapclients/internal/photo"
	"github.com/chomosuke9/wazzapclients/internal/webpanim"
)

// Settings > Chats > Emoji style. No emojis are bundled: Noto's 2D font
// downloads once (LoadEmojiFont), and its animated and 3D emojis (the 3D
// font alone is 148 MB) a picture at a time, the first time each shows
// (internal/emojiset). The 2D one shows until then, and for good where the
// set has none.
//
//   - 3D replaces the font's pictures wherever text is drawn: the patched
//     go-text asks emojiBitmap (Font.BitmapHook) for each bitmap glyph.
//     Gio caches glyph pictures and the drawing of whole lines, so the UI
//     gets a new shaper when pictures arrive (refreshEmoji).
//   - The animated styles play the animations of a message of emojis alone
//     (jumbo.go) and, with emojiAnimated, of the emojis in message text
//     (styledtext's Bitmap hook). Labels (the chat list, names) and the
//     picker stay still.

// prefEmojiStyle is an app preference (appPrefs): "" is 2D.
const prefEmojiStyle = "emoji_style"

type emojiStyleKind int32

const (
	emoji2D           emojiStyleKind = iota
	emojiAnimateJumbo                // 2D, a message of emojis alone animated
	emojiAnimated                    // 2D, emojis in messages animated
	emoji3D
)

var emojiStyles = []struct {
	pref, title, sub string
}{
	emoji2D:           {"", "2D", "Noto Color Emoji"},
	emojiAnimateJumbo: {"2d_anim_alone", "2D, animated when alone", "Messages of only emojis play their animations"},
	emojiAnimated:     {"2d_anim", "2D animated", "Emojis in messages play their animations"},
	emoji3D:           {"3d", "3D", "Noto's 3D emojis"},
}

// emojiStyleNow is the chosen style. The 3D hook reads it from any
// goroutine; only the UI goroutine sets it.
var emojiStyleNow atomic.Int32

func emojiStyle() emojiStyleKind { return emojiStyleKind(emojiStyleNow.Load()) }

// emojiStore keeps the downloaded pictures. Run sets it (Options.EmojiDir);
// it is nil in tests and screenshots, where nothing downloads and every
// style draws 2D.
var emojiStore *emojiset.Store

func readEmojiStyle(b model.Backend) emojiStyleKind {
	if b != nil {
		v := b.Pref(prefEmojiStyle)
		for i, s := range emojiStyles {
			if s.pref == v {
				return emojiStyleKind(i)
			}
		}
	}
	return emoji2D
}

func (u *UI) loadEmojiStyle() {
	if emojiPreview >= 0 {
		return
	}
	emojiStyleNow.Store(int32(readEmojiStyle(u.backend)))
}

// emojiPreview is the style PreviewEmojis set, or -1.
var emojiPreview emojiStyleKind = -1

// PreviewEmojis makes every UI draw emojis in style (a value of the Emoji
// style preference: "2d_anim_alone", "2d_anim" or "3d") from the pictures
// already in dir, which is laid out as the app keeps them (<data>/emoji),
// downloading none. For cmd/screenshot.
func PreviewEmojis(dir, style string) error {
	for i, s := range emojiStyles {
		if s.pref == style {
			st := emojiset.New(dir)
			st.Fetch = func(string) ([]byte, int, error) { return nil, 0, errors.New("previews download nothing") }
			emojiStore = st
			emojiPreview = emojiStyleKind(i)
			emojiStyleNow.Store(int32(i))
			return nil
		}
	}
	return fmt.Errorf("no emoji style %q", style)
}

func (u *UI) setEmojiStyle(k emojiStyleKind) {
	u.backend.SetPref(prefEmojiStyle, emojiStyles[k].pref)
	emojiStyleNow.Store(int32(k))
}

var (
	emojiNamesMu sync.Mutex
	emojiNames   map[fontapi.GID]string
)

// emojiGlyphName returns the Noto file name (emojiset.Name) of the emoji
// font's glyph gid, or "" (also before the font loads). The font's cmap
// names its single code point glyphs and its ligatures (GSUB) the
// sequences, such as skin tones, ZWJ families and flags.
func emojiGlyphName(gid fontapi.GID) string {
	emojiNamesMu.Lock()
	defer emojiNamesMu.Unlock()
	if emojiNames == nil {
		f := loadEmojiFont()
		if f == nil {
			return ""
		}
		emojiNames = buildEmojiNames(f)
	}
	return emojiNames[gid]
}

// allEmojiNames lists the Noto file names of every emoji in the font, or
// nil before it loads.
func allEmojiNames() []string {
	emojiGlyphName(0)
	emojiNamesMu.Lock()
	defer emojiNamesMu.Unlock()
	seen := make(map[string]bool, len(emojiNames))
	out := make([]string, 0, len(emojiNames))
	for _, n := range emojiNames {
		if n != "" && !seen[n] {
			seen[n] = true
			out = append(out, n)
		}
	}
	sort.Strings(out)
	return out
}

// emojiBatch is how many downloads the last "Download all" started with.
var emojiBatch int

// downloadAllEmojis queues every picture of the chosen style and returns
// how many it queued.
func downloadAllEmojis() int {
	if emojiStore == nil {
		return 0
	}
	names := allEmojiNames()
	n := 0
	switch emojiStyle() {
	case emoji3D:
		n = emojiStore.Prefetch(emojiset.ThreeD, names)
	case emojiAnimateJumbo, emojiAnimated:
		n = emojiStore.Prefetch(emojiset.Animated, names)
	}
	emojiBatch = n
	return n
}

func buildEmojiNames(f *fontapi.Font) map[fontapi.GID]string {
	single := map[fontapi.GID]rune{}
	it := f.Cmap.Iter()
	for it.Next() {
		r, g := it.Char()
		if old, ok := single[g]; !ok || r < old {
			single[g] = r
		}
	}
	seqs := map[fontapi.GID][]rune{}
	for _, lk := range f.GSUB.Lookups {
		for _, st := range lk.Subtables {
			lig, ok := st.(tables.LigatureSubs)
			if !ok {
				continue
			}
			var firsts []tables.GlyphID
			switch c := lig.Coverage.(type) {
			case tables.Coverage1:
				firsts = c.Glyphs
			case tables.Coverage2:
				for _, r := range c.Ranges {
					for g := r.StartGlyphID; g <= r.EndGlyphID && g >= r.StartGlyphID; g++ {
						firsts = append(firsts, g)
					}
				}
			}
			for i, set := range lig.LigatureSets {
				if i >= len(firsts) {
					break
				}
				for _, l := range set.Ligatures {
					seq := seqOf(single, seqs, fontapi.GID(firsts[i]))
					for _, c := range l.ComponentGlyphIDs {
						seq = append(seq, seqOf(single, seqs, fontapi.GID(c))...)
					}
					seqs[fontapi.GID(l.LigatureGlyph)] = seq
				}
			}
		}
	}
	names := make(map[fontapi.GID]string, len(single)+len(seqs))
	for g, r := range single {
		if r >= 0x80 { // digits, # and *: keycaps' parts, not pictures
			names[g] = emojiset.Name([]rune{r})
		}
	}
	for g, seq := range seqs {
		names[g] = emojiset.Name(seq)
	}
	return names
}

func seqOf(single map[fontapi.GID]rune, seqs map[fontapi.GID][]rune, g fontapi.GID) []rune {
	if s, ok := seqs[g]; ok {
		return append([]rune(nil), s...)
	}
	if r, ok := single[g]; ok {
		return []rune{r}
	}
	return nil
}

// emojiBitmap is the emoji font's BitmapHook: in the 3D style, the
// downloaded picture of a glyph, which it downloads when it hasn't yet.
func emojiBitmap(gid fontapi.GID) ([]byte, bool) {
	if emojiStyle() != emoji3D || emojiStore == nil {
		return nil, false
	}
	b := emojiStore.Read(emojiset.ThreeD, emojiGlyphName(gid))
	return b, b != nil
}

// emojiView is what the UI drew emojis with last: the style, the
// store's pictures and the shaper's face of the emoji font.
type emojiView struct {
	style  emojiStyleKind
	gen    uint64
	at     time.Time // when the shaper was made
	shaper *text.Shaper
	face   int // the emoji font's index in shaper's faces, or -1
}

// refreshEmoji gives the UI a new shaper when the emojis it draws have
// changed: the 2D font loaded, the style, or (in 3D) the pictures
// downloaded. Gio's shaper
// caches glyph pictures, and whole lines of them, for good. Pictures
// arriving one by one swap the shaper at most once a second.
func (u *UI) refreshEmoji(gtx C) {
	v := &u.emojiView
	style := emojiStyle()
	var gen uint64
	if emojiStore != nil {
		gen = emojiStore.Gen()
	}
	redraw := false
	if loadEmojiFont() == nil && emojiStore != nil {
		// The 2D font arrived (FontPath downloads it until then).
		if p := emojiStore.FontPath(); p != "" && LoadEmojiFont(p) == nil {
			redraw = true
			jumbos.m = nil // splitEmojis found no emojis without it
		}
	}
	switch {
	case v.shaper == nil:
		v.style, v.gen, v.at = style, gen, gtx.Now
	case style != v.style:
		redraw = style == emoji3D || v.style == emoji3D
		v.style, v.gen = style, gen
	case style == emoji3D && gen != v.gen:
		if wait := v.at.Add(time.Second); gtx.Now.Before(wait) {
			gtx.Execute(op.InvalidateCmd{At: wait})
			break
		}
		redraw = true
		v.gen = gen
	default:
		v.gen = gen
	}
	if redraw {
		v.at = gtx.Now
		u.th.Shaper = newShaper()
		u.emojiImgs.clearAll()
	}
	if v.shaper != u.th.Shaper {
		v.shaper = u.th.Shaper
		v.face = emojiFaceIndex(u.th.Shaper)
	}
}

// emojiFaceIndex finds the index the shaper gives the emoji font, which
// is part of its glyphs' IDs.
func emojiFaceIndex(sh *text.Shaper) int {
	sh.LayoutString(text.Parameters{Font: font.Font{Typeface: typeface}, PxPerEm: fixed.I(20), MaxWidth: 1000}, "😀")
	face := -1
	for g, ok := sh.NextGlyph(); ok; g, ok = sh.NextGlyph() {
		if face < 0 {
			face = int(uint64(g.ID) >> 48) // see text.newGlyphID
		}
	}
	return face
}

// glyphEmoji returns the Noto name of an emoji glyph drawn with the
// UI's shaper, or "".
func (u *UI) glyphEmoji(g text.Glyph) string {
	// Before the font loads, face is the system emoji font's.
	if loadEmojiFont() == nil || u.emojiView.face < 0 || int(uint64(g.ID)>>48) != u.emojiView.face {
		return ""
	}
	return emojiGlyphName(fontapi.GID(uint64(g.ID) & (1<<32 - 1)))
}

// animateEmojis reports whether emojis in message text play, and
// animateJumbo whether those of messages of emojis alone do.
func animateEmojis() bool {
	return emojiStyle() == emojiAnimated && emojiStore != nil && perf.autoplay != "never"
}

func animateJumbo() bool {
	s := emojiStyle()
	return (s == emojiAnimated || s == emojiAnimateJumbo) && emojiStore != nil && perf.autoplay != "never"
}

// textEmoji draws an emoji glyph of message text in place of its picture
// at r (styledtext's Bitmap hook): animated when the style says so and the
// animation is ready, else the still picture from u.emojiImgs, decoded at
// the size it shows at. It reports false to leave the glyph to the font.
//
// Gio's shaper would decode each emoji, at each text size, at the font's
// full 136x128 (70 KB) and keep up to 1000 of them for good, each drawn
// from a mipmapped texture of that size: a message of a few hundred
// different emojis held tens of MB after it was deleted. u.emojiImgs is
// bounded by bytes, and an emoji 30 pixels high takes about 4 KB there.
func (u *UI) textEmoji(gtx C, g text.Glyph, r image.Rectangle) bool {
	name := u.glyphEmoji(g)
	if name == "" {
		return false
	}
	if animateEmojis() {
		// The font's pictures are 136x128 with the emoji 128 high; the
		// animations are squares.
		side := r.Dy()
		at := image.Pt(r.Min.X+(r.Dx()-side)/2, r.Min.Y)
		if u.drawEmojiAnim(gtx, name, side, at) {
			return true
		}
	}
	side := max(r.Dx(), r.Dy())
	gid := fontapi.GID(uint64(g.ID) & (1<<32 - 1))
	e := u.emojiImgs.get(u.textEmojiKey(name, side), side, func() []byte { return glyphPNG(gid) })
	switch e.state {
	case imgMissing:
		return false
	case imgLoading:
		return true // blank for a frame or two, as in the picker
	}
	paintCover(gtx, e.op, e.size, r)
	return true
}

// textEmojiKey returns the u.emojiImgs key of emoji name side pixels
// across, made once rather than every frame.
func (u *UI) textEmojiKey(name string, side int) string {
	k := emojiAnimKey{name, side}
	if s, ok := u.emojiKeys[k]; ok {
		return s
	}
	if u.emojiKeys == nil || len(u.emojiKeys) >= 4096 {
		u.emojiKeys = map[emojiAnimKey]string{}
	}
	s := "text:" + name + ":" + strconv.Itoa(side)
	u.emojiKeys[k] = s
	return s
}

// Animated emojis are decoded once, every frame scaled down to the size
// they show at, and played from memory while they show: a 512x512 frame
// takes about 6 ms to decode. The frames of all of them together are kept
// under emojiAnimBudget; those not drawn for emojiAnimKeep go.
//
// An animation's frames are cells of one picture (its sheet), drawn a cell
// at a time. Gio uploads each image to a GPU texture of its own (with
// mipmaps) and frees it after a frame without it, so an image per frame
// made a texture, and freed one, every time a frame changed: thousands a
// second for a message full of emojis, which grew the graphics driver's
// memory by gigabytes. The sheet is uploaded once and stays while it shows.
const (
	emojiAnimBudget = 16 << 20
	emojiAnimKeep   = 3 * time.Second
	emojiDecoders   = 2
	emojiSheetMax   = 4096 // pixels across or down a sheet
)

type emojiAnimKey struct {
	name string
	side int
}

type emojiAnim struct {
	// Set once by the decoding goroutine (under emojiAnims.mu), then
	// read-only.
	sheet  *image.RGBA // the frames, side pixels square, cols to a row
	cols   int
	delays []time.Duration
	total  time.Duration
	failed bool

	op   paint.ImageOp // the sheet's, made on the UI goroutine when it first shows
	used time.Time
	cost int
}

// cell is where frame i is on the sheet of an animation side pixels square.
func (a *emojiAnim) cell(i, side int) image.Point {
	return image.Pt(i%a.cols*side, i/a.cols*side)
}

type emojiAnims struct {
	mu       sync.Mutex
	m        map[emojiAnimKey]*emojiAnim
	held     int // bytes of the entries' sheets, reserved as decoding starts
	decoding int
	start    time.Time // every animation plays on this clock
	next     time.Time // the next frame any of this frame's anims needs
}

// drawEmojiAnim draws the animation of emoji name, side pixels square, at
// at. It reports false (the caller draws the still picture) until the
// animation is downloaded and decoded, when the set has none, or when the
// budget is spent.
func (u *UI) drawEmojiAnim(gtx C, name string, side int, at image.Point) bool {
	if side <= 0 || !emojiStore.Ready(emojiset.Animated, name) {
		return false
	}
	as := &u.emojiAnims
	k := emojiAnimKey{name, side}
	as.mu.Lock()
	a := as.m[k]
	if a == nil {
		cost := emojiAnimCost(side)
		if as.decoding >= emojiDecoders || as.held+cost > emojiAnimBudget {
			as.mu.Unlock()
			return false
		}
		if as.m == nil {
			as.m = map[emojiAnimKey]*emojiAnim{}
			as.start = gtx.Now
		}
		a = &emojiAnim{cost: cost}
		as.m[k] = a
		as.held += cost
		as.decoding++
		go as.decode(k, a, u.emojiImgs.invalidate)
	}
	a.used = gtx.Now
	sheet, delays, total := a.sheet, a.delays, a.total
	as.mu.Unlock()
	if sheet == nil {
		return false
	}
	t := gtx.Now.Sub(as.start) % total
	i := 0
	for ; i < len(delays)-1 && t >= delays[i]; i++ {
		t -= delays[i]
	}
	if next := gtx.Now.Add(delays[i] - t); as.next.IsZero() || next.Before(as.next) {
		as.next = next
	}
	if a.op == (paint.ImageOp{}) {
		a.op = paint.NewImageOp(sheet)
	}
	defer op.Offset(at).Push(gtx.Ops).Pop()
	defer clip.Rect{Max: image.Pt(side, side)}.Push(gtx.Ops).Pop()
	defer op.Offset(a.cell(i, side).Mul(-1)).Push(gtx.Ops).Pop()
	a.op.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
	return true
}

// emojiAnimCost is what the sheet of an animation side pixels square is
// expected to take (Noto's have about 50 frames: 8 by 8 cells).
func emojiAnimCost(side int) int { return 64 * side * side * 4 }

// sheetGrid returns how many columns and rows of cells side pixels square
// a sheet of n frames has, or 0, 0 when it would be too big.
func sheetGrid(n, side int) (cols, rows int) {
	cols = int(math.Ceil(math.Sqrt(float64(n))))
	rows = (n + cols - 1) / cols
	if n <= 0 || cols*side > emojiSheetMax || rows*side > emojiSheetMax {
		return 0, 0
	}
	return cols, rows
}

func (as *emojiAnims) decode(k emojiAnimKey, a *emojiAnim, invalidate func()) {
	var sheet *image.RGBA
	var cols int
	var delays []time.Duration
	var total time.Duration
	if an, err := webpanim.Parse(emojiStore.Read(emojiset.Animated, k.name)); err == nil {
		var rows int
		if cols, rows = sheetGrid(len(an.Frames), k.side); cols > 0 {
			sheet = image.NewRGBA(image.Rect(0, 0, cols*k.side, rows*k.side))
		}
		for i := 0; sheet != nil && i < len(an.Frames); i++ {
			canvas, d, err := an.Next()
			if err != nil {
				sheet = nil
				break
			}
			at := image.Pt(i%cols*k.side, i/cols*k.side)
			f := photo.Shrink(canvas, k.side, k.side)
			for y := range k.side {
				copy(sheet.Pix[sheet.PixOffset(at.X, at.Y+y):][:4*k.side], f.Pix[y*f.Stride:])
			}
			d = max(d, 20*time.Millisecond)
			delays = append(delays, d)
			total += d
		}
	}
	as.mu.Lock()
	as.decoding--
	if sheet == nil {
		a.failed = true // stays, so it isn't decoded again
		as.held -= a.cost
		a.cost = 0
	} else {
		a.sheet, a.cols, a.delays, a.total = sheet, cols, delays, total
		cost := len(sheet.Pix)
		as.held += cost - a.cost
		a.cost = cost
	}
	as.mu.Unlock()
	if invalidate != nil {
		invalidate()
	}
}

// endFrame drops the animations that haven't shown for a while and asks
// for a frame when the next one is due.
func (as *emojiAnims) endFrame(gtx C) {
	as.mu.Lock()
	defer as.mu.Unlock()
	for k, a := range as.m {
		if a.sheet != nil && gtx.Now.Sub(a.used) > emojiAnimKeep {
			as.held -= a.cost
			delete(as.m, k)
		}
	}
	if !as.next.IsZero() {
		gtx.Execute(op.InvalidateCmd{At: as.next})
		as.next = time.Time{}
	}
}

// emojiStyleSettings is the Emoji style page.
func (u *UI) emojiStyleSettings() []settingsSection {
	cur := emojiStyle()
	sec := settingsSection{title: "Emoji style",
		note: "Emojis come from Google's Noto Emoji and are kept on this computer. The 2D ones download once, " +
			"when the app first runs. Animated and 3D emojis download the first time each one shows; the 2D one " +
			"shows until then, and where there is no animation."}
	for i, s := range emojiStyles {
		k := emojiStyleKind(i)
		sec.rows = append(sec.rows, settingRow{key: "emoji:" + s.pref, kind: setRadio, title: s.title, sub: s.sub,
			on: cur == k, run: func() { u.setEmojiStyle(k) }})
	}
	out := []settingsSection{sec}
	if emojiStore != nil && cur != emoji2D {
		row := settingRow{key: "emoji:all", title: "Download all emojis", sub: "Get every emoji of this style now, instead of as each shows"}
		if n := emojiStore.Pending(); n > 0 {
			row.sub = fmt.Sprintf("Downloading… %d left", n)
			if emojiBatch >= n {
				row.sub = fmt.Sprintf("Downloading… %d%% (%d left)", 100*(emojiBatch-n)/max(emojiBatch, 1), n)
			}
		} else {
			row.run = func() {
				if loadEmojiFont() == nil {
					u.toast("The 2D emojis haven't downloaded yet")
					return
				}
				u.toast(fmt.Sprintf("Downloading %d emojis", downloadAllEmojis()))
			}
		}
		out = append(out, settingsSection{title: "Download", rows: []settingRow{row}})
	}
	if emojiStore != nil {
		row := settingRow{key: "emoji:clear", title: "Delete downloaded emojis", sub: "Nothing downloaded"}
		if size := emojiStore.Size(); size > 0 {
			row.sub, row.danger = sizeLabel(size)+" on this computer", true
			row.run = func() {
				if err := emojiStore.Clear(); err != nil {
					u.toast("Couldn't delete the emojis")
					return
				}
				u.toast("Deleted downloaded emojis")
			}
		}
		out = append(out, settingsSection{title: "Storage", rows: []settingRow{row}})
	}
	return out
}

// sizeLabel words a number of bytes ("1.5 MB").
func sizeLabel(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%d KB", n>>10)
	}
	return fmt.Sprintf("%d bytes", n)
}
