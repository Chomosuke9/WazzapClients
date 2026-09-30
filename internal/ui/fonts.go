package ui

import (
	"encoding/binary"
	"sync"

	"gioui.org/font"
	"gioui.org/font/gofont"
	"gioui.org/font/opentype"
)

// Keep the patched face distinct from any unpatched Noto Color Emoji installed
// on the system. Gio loads system faces first and may select them on a tie.
const emojiTypeface font.Typeface = "WazzapClients Emoji"

var patchEmoji sync.Once

func bundledFonts() []font.FontFace {
	faces := gofont.Collection()
	patchEmoji.Do(func() { narrowEmojiSpaces(notoColorEmoji) })
	if emoji, err := opentype.ParseCollection(notoColorEmoji); err == nil {
		for i := range emoji {
			emoji[i].Font.Typeface = emojiTypeface
		}
		faces = append(faces, emoji...)
	}
	return faces
}

// narrowEmojiSpaces sets the advance of the emoji font's space glyphs to a
// normal word space, in place.
//
// The shaper never switches fonts for a space, so the space after an emoji
// is drawn with the emoji font, whose spaces are as wide as an emoji
// ("🎓    ASEAN"). Narrowing them fixes the gap. Patching the embedded bytes
// in place (rather than a copy) keeps the 10 MB font out of the heap: only
// the touched page becomes private memory.
func narrowEmojiSpaces(data []byte) {
	head, ok1 := tableOffset(data, "head")
	hhea, ok2 := tableOffset(data, "hhea")
	hmtx, ok3 := tableOffset(data, "hmtx")
	cmap, ok4 := tableOffset(data, "cmap")
	if !ok1 || !ok2 || !ok3 || !ok4 || head+20 > len(data) || hhea+36 > len(data) {
		return
	}
	upem := binary.BigEndian.Uint16(data[head+18:])
	nMetrics := int(binary.BigEndian.Uint16(data[hhea+34:]))
	for _, r := range []rune{' ', ' '} {
		gid, ok := cmapLookup(data, cmap, r)
		if !ok || gid >= nMetrics {
			continue
		}
		if p := hmtx + 4*gid; p+2 <= len(data) {
			binary.BigEndian.PutUint16(data[p:], upem/4)
		}
	}
}

// tableOffset finds a table in an sfnt font's table directory.
func tableOffset(data []byte, tag string) (int, bool) {
	if len(data) < 12 {
		return 0, false
	}
	n := int(binary.BigEndian.Uint16(data[4:]))
	for i := 0; i < n; i++ {
		rec := 12 + 16*i
		if rec+16 > len(data) {
			break
		}
		if string(data[rec:rec+4]) == tag {
			return int(binary.BigEndian.Uint32(data[rec+8:])), true
		}
	}
	return 0, false
}

// cmapLookup maps a rune to a glyph ID using the font's Unicode cmap
// subtable (format 4 or 12).
func cmapLookup(data []byte, cmap int, r rune) (int, bool) {
	u16 := func(off int) int {
		if off < 0 || off+2 > len(data) {
			return 0
		}
		return int(binary.BigEndian.Uint16(data[off:]))
	}
	u32 := func(off int) int {
		if off < 0 || off+4 > len(data) {
			return 0
		}
		return int(binary.BigEndian.Uint32(data[off:]))
	}
	n := u16(cmap + 2)
	for i := 0; i < n; i++ {
		rec := cmap + 4 + 8*i
		if rec+8 > len(data) {
			break
		}
		platform, encoding := u16(rec), u16(rec+2)
		if platform != 0 && !(platform == 3 && (encoding == 1 || encoding == 10)) {
			continue
		}
		sub := cmap + u32(rec+4)
		if u16(sub) == 12 {
			groups := u32(sub + 12)
			for g := 0; g < groups; g++ {
				grp := sub + 16 + 12*g
				start, end := u32(grp), u32(grp+4)
				if int(r) >= start && int(r) <= end {
					return u32(grp+8) + int(r) - start, true
				}
			}
			continue
		}
		if u16(sub) != 4 {
			continue
		}
		segs := u16(sub+6) / 2
		ends, starts := sub+14, sub+16+2*segs
		deltas, ranges := starts+2*segs, starts+4*segs
		for s := 0; s < segs; s++ {
			if int(r) > u16(ends+2*s) || int(r) < u16(starts+2*s) {
				continue
			}
			delta, ro := u16(deltas+2*s), u16(ranges+2*s)
			if ro == 0 {
				return (int(r) + delta) & 0xffff, true
			}
			g := u16(ranges + 2*s + ro + 2*(int(r)-u16(starts+2*s)))
			if g == 0 {
				return 0, false
			}
			return (g + delta) & 0xffff, true
		}
	}
	return 0, false
}
