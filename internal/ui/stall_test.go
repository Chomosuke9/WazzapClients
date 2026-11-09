package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestStallWatch(t *testing.T) {
	path := filepath.Join(t.TempDir(), "stalls.log")
	s := newStallWatch(path)

	s.begin("quick")
	s.end()
	s.begin("slow job")
	time.Sleep(stallAfter + stallAfter/2)
	s.end()

	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	out := string(b)
	if strings.Contains(out, "quick") {
		t.Error("a quick job was reported")
	}
	for _, want := range []string{"the UI goroutine has been on slow job", "TestStallWatch", "slow job finished after"} {
		if !strings.Contains(out, want) {
			t.Errorf("the report lacks %q:\n%s", want, out)
		}
	}
}
