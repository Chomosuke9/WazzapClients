package ui

import (
	"os"
	"testing"
	"time"

	"github.com/chomosuke9/wazzapclients/internal/mock"
)

// The tests run on a clock of their own: a fixed afternoon in UTC, which
// moves on in real time. The demo chats are dated by the clock ("today at
// 09:02"), so on the real one a test could pass in one time zone or at
// one time of day and fail in another, as on CI, which runs in UTC.

var (
	testBase    = time.Date(2026, time.October, 2, 15, 0, 0, 0, time.UTC) // a Friday
	testStarted = time.Now()
)

// testNow is the tests' clock. Frames are drawn at its times too, for code
// that compares gtx.Now with the backend's times (mutes ending).
func testNow() time.Time { return testBase.Add(time.Since(testStarted)) }

// testEmojiFont is Noto Color Emoji, which the app downloads
// (emojiset.FontPath): the tests, cmd/screenshot and cmd/memprobe load it
// from here.
const testEmojiFont = "testdata/NotoColorEmoji.ttf"

func TestMain(m *testing.M) {
	time.Local = time.UTC
	timeNow, mock.Clock = testNow, testNow
	systemDark = func() (bool, bool) { return true, true } // the theme tests were drawn in
	// The app downloads the emoji font; tests have a copy.
	if err := LoadEmojiFont(testEmojiFont); err != nil {
		panic(err)
	}
	os.Exit(m.Run())
}
