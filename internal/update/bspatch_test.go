package update

import (
	"bytes"
	"math/rand/v2"
	"os"
	"testing"
)

// patchFixture returns the files testdata/fixture.bsdiff turns one into
// the other: random bytes, then the same with bytes inserted, changed,
// dropped and added. The patch was made from them with the bsdiff
// cmd/mkpatch uses, so bspatch is checked against that tool's output.
func patchFixture() (old, new []byte) {
	r := rand.NewChaCha8([32]byte{'w', 'a', 'z', 'z', 'a', 'p'})
	old = make([]byte, 64<<10)
	r.Read(old)
	var b bytes.Buffer
	b.Write(old[:10000])
	b.WriteString("a few bytes the new version inserts")
	for i, c := range old[10000:30000] {
		if i%97 == 0 {
			c++
		}
		b.WriteByte(c)
	}
	b.Write(old[40000:])
	extra := make([]byte, 3000)
	r.Read(extra)
	b.Write(extra)
	return old, b.Bytes()
}

func fixturePatch(t *testing.T) []byte {
	t.Helper()
	p, err := os.ReadFile("testdata/fixture.bsdiff")
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestBspatch(t *testing.T) {
	old, want := patchFixture()
	var got bytes.Buffer
	if err := bspatch(&got, bytes.NewReader(old), int64(len(old)), fixturePatch(t)); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got.Bytes(), want) {
		t.Fatalf("bspatch made %d bytes that aren't the new file (%d bytes)", got.Len(), len(want))
	}
}

func TestBspatchDamaged(t *testing.T) {
	old, _ := patchFixture()
	patch := fixturePatch(t)
	for name, p := range map[string][]byte{
		"empty":     nil,
		"magic":     append([]byte("BSDIFF41"), patch[8:]...),
		"truncated": patch[:len(patch)/2],
		"header":    append(append([]byte{}, patch[:8]...), append(bytes.Repeat([]byte{0xff}, 8), patch[16:]...)...),
	} {
		var out bytes.Buffer
		if err := bspatch(&out, bytes.NewReader(old), int64(len(old)), p); err == nil {
			t.Errorf("%s patch: no error", name)
		}
	}
}

func TestOfftin(t *testing.T) {
	for _, tc := range []struct {
		b    [8]byte
		want int64
	}{
		{[8]byte{1}, 1},
		{[8]byte{0, 1}, 256},
		{[8]byte{5, 0, 0, 0, 0, 0, 0, 0x80}, -5},
		{[8]byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0x7f}, 1<<63 - 1},
	} {
		if got := offtin(tc.b[:]); got != tc.want {
			t.Errorf("offtin(%x) = %d, want %d", tc.b, got, tc.want)
		}
	}
}
