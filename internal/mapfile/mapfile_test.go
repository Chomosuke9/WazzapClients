package mapfile

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMapIsCopyOnWrite(t *testing.T) {
	p := filepath.Join(t.TempDir(), "f")
	os.WriteFile(p, []byte("hello"), 0o600)
	b, err := Map(p)
	if err != nil || string(b) != "hello" {
		t.Fatalf("mapped %q, %v", b, err)
	}
	b[0] = 'j'
	if on, _ := os.ReadFile(p); string(on) != "hello" {
		t.Errorf("writing the mapping changed the file to %q", on)
	}
	if _, err := Map(filepath.Join(t.TempDir(), "none")); err == nil {
		t.Error("mapped a missing file")
	}
}
