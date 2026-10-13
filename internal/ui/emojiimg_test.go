package ui

import "testing"

// TestEmojiPictures checks that every emoji in the picker is one glyph of
// the bundled font with a PNG picture, so the picker never falls back to
// the text shaper and its big bitmap cache.
func TestEmojiPictures(t *testing.T) {
	for _, e := range emojis {
		if emojiPNG(e.char) == nil {
			t.Errorf("no picture for %q (%s)", e.char, e.name)
		}
	}
}

// TestJumboEmojis checks which texts show their emojis big, without a
// bubble: one to three emojis and nothing else.
func TestJumboEmojis(t *testing.T) {
	for _, tc := range []struct {
		text string
		want int
	}{
		{"😀", 1},
		{" 😀 ", 1},
		{"😀😂", 2},
		{"😀 😂 👍🏽", 3},
		{"❤️", 1},
		{"👨‍👩‍👧", 1},
		{"🇮🇩", 1},
		{"1️⃣", 1},
		{"😀😀😀😀", 0},
		{"Test", 0},
		{"hi 😀", 0},
		{"1", 0},
		{"#", 0},
		{"", 0},
	} {
		if got := len(splitEmojis(tc.text)); got != tc.want {
			t.Errorf("splitEmojis(%q) gives %d emojis, want %d", tc.text, got, tc.want)
		}
	}
}
