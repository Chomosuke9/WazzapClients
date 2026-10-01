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
