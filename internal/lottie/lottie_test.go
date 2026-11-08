package lottie

import (
	"archive/zip"
	"bytes"
	"compress/gzip"
	"image"
	"testing"
)

// moving is a 100x100 animation: a red 40x40 square sliding from x=30 to
// x=70 over 30 frames, under a blue ring (an ellipse stroked 10 wide), and
// a green square masked away but for its left half.
const moving = `{
 "v": "5.7.0", "fr": 30, "ip": 0, "op": 30, "w": 100, "h": 100,
 "layers": [
  {"ty": 4, "ind": 1, "ip": 0, "op": 30, "st": 0,
   "ks": {"p": {"a": 0, "k": [50, 50]}},
   "shapes": [
    {"ty": "el", "p": {"a": 0, "k": [0, 0]}, "s": {"a": 0, "k": [80, 80]}},
    {"ty": "st", "c": {"a": 0, "k": [0, 0, 1, 1]}, "o": {"a": 0, "k": 100}, "w": {"a": 0, "k": 10}, "lc": 2, "lj": 2}
   ]},
  {"ty": 4, "ind": 2, "ip": 0, "op": 30, "st": 0,
   "ks": {"p": {"a": 1, "k": [
     {"t": 0, "s": [30, 50], "i": {"x": [1], "y": [1]}, "o": {"x": [0], "y": [0]}},
     {"t": 29, "s": [70, 50]}]}},
   "shapes": [{"ty": "gr", "it": [
    {"ty": "rc", "p": {"a": 0, "k": [0, 0]}, "s": {"a": 0, "k": [40, 40]}, "r": {"a": 0, "k": 0}},
    {"ty": "fl", "c": {"a": 0, "k": [1, 0, 0, 1]}, "o": {"a": 0, "k": 100}, "r": 1},
    {"ty": "tr", "p": {"a": 0, "k": [0, 0]}}
   ]}]},
  {"ty": 1, "ind": 3, "ip": 0, "op": 30, "st": 0, "sc": "#00ff00", "sw": 100, "sh": 100,
   "ks": {"o": {"a": 0, "k": 100}},
   "masksProperties": [{"mode": "a", "inv": false, "o": {"a": 0, "k": 100},
    "pt": {"a": 0, "k": {"c": true, "v": [[0, 90], [50, 90], [50, 100], [0, 100]], "i": [[0,0],[0,0],[0,0],[0,0]], "o": [[0,0],[0,0],[0,0],[0,0]]}}}]}
 ]
}`

func was(t *testing.T, js string) []byte {
	var b bytes.Buffer
	zw := zip.NewWriter(&b)
	for name, body := range map[string]string{
		"animation/animation.json":             js,
		"animation/animation.json.trust_token": "a.b.c",
	} {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		w.Write([]byte(body))
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func at(img *image.RGBA, x, y int) [4]uint8 {
	i := img.PixOffset(x, y)
	return [4]uint8{img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3]}
}

func TestRender(t *testing.T) {
	data := was(t, moving)
	if !IsWAS(data) {
		t.Fatal("IsWAS false for a zip")
	}
	a, err := Load(data)
	if err != nil {
		t.Fatal(err)
	}
	if a.Width != 100 || a.Height != 100 || a.Frames() != 30 || a.FrameRate != 30 {
		t.Fatalf("got %dx%d, %d frames at %v", a.Width, a.Height, a.Frames(), a.FrameRate)
	}
	img := image.NewRGBA(image.Rect(0, 0, 100, 100))
	red := [4]uint8{255, 0, 0, 255}
	blue := [4]uint8{0, 0, 255, 255}
	green := [4]uint8{0, 255, 0, 255}
	none := [4]uint8{}
	a.Render(img, 0)
	for _, c := range []struct {
		x, y int
		want [4]uint8
	}{
		{30, 50, red},  // the square, at its start
		{70, 50, none}, // where it ends up
		{50, 10, blue}, // the ring's top (radius 40, 10 wide)
		{45, 50, red},  // the square spans x 10..50
		{25, 95, green},
		{75, 95, none}, // the mask leaves out the solid's right half
		{1, 1, none},
	} {
		if got := at(img, c.x, c.y); got != c.want {
			t.Errorf("frame 0 at %d,%d: got %v, want %v", c.x, c.y, got, c.want)
		}
	}
	a.Render(img, 29)
	if got := at(img, 70, 50); got != red {
		t.Errorf("frame 29 at 70,50: got %v, want red", got)
	}
	if got := at(img, 20, 50); got != none {
		t.Errorf("frame 29 at 20,50: got %v, want nothing", got)
	}
	// Halfway, linear: x=50.
	a.Render(img, 14.5)
	if got := at(img, 68, 50); got != red {
		t.Errorf("frame 14.5 at 68,50: got %v, want red", got)
	}
	if got := at(img, 72, 50); got == red {
		t.Errorf("frame 14.5 at 72,50: square went too far")
	}
}

// TestScaled checks that a frame fits a picture of another size and shape.
func TestScaled(t *testing.T) {
	a, err := Load([]byte(moving))
	if err != nil {
		t.Fatal(err)
	}
	w, h := a.Fit(50)
	if w != 50 || h != 50 {
		t.Fatalf("Fit(50) = %d, %d", w, h)
	}
	img := image.NewRGBA(image.Rect(0, 0, 60, 30))
	a.Render(img, 0)
	// 30 tall: scale 0.3, centered with 15 px each side.
	if got := at(img, 15+9, 15); got != [4]uint8{255, 0, 0, 255} {
		t.Errorf("square at 24,15: got %v", got)
	}
	if got := at(img, 5, 15); got != [4]uint8{} {
		t.Errorf("outside the composition at 5,15: got %v", got)
	}
}

func TestTGS(t *testing.T) {
	var b bytes.Buffer
	zw := gzip.NewWriter(&b)
	zw.Write([]byte(moving))
	zw.Close()
	if IsWAS(b.Bytes()) {
		t.Error("IsWAS true for gzip")
	}
	if _, err := Load(b.Bytes()); err != nil {
		t.Fatal(err)
	}
}

func TestBroken(t *testing.T) {
	for _, data := range [][]byte{
		nil,
		[]byte("{}"),
		[]byte(`{"fr": 30, "ip": 0, "op": 0, "w": 10, "h": 10}`),
		was(t, "not json"),
		[]byte("RIFF\x00\x00\x00\x00WEBP"),
	} {
		if _, err := Load(data); err == nil {
			t.Errorf("Load(%.20q) gave no error", data)
		}
	}
}

func TestNoAllocs(t *testing.T) {
	a, err := Load([]byte(moving))
	if err != nil {
		t.Fatal(err)
	}
	img := image.NewRGBA(image.Rect(0, 0, 64, 64))
	a.Render(img, 0)
	f := 0.0
	if n := testing.AllocsPerRun(20, func() { a.Render(img, f); f++ }); n > 0 {
		t.Errorf("%v allocations a frame", n)
	}
}

func TestEase(t *testing.T) {
	for _, c := range []struct{ x, want float64 }{{0.1, 0.2869}, {0.62, 0.9477}} {
		if got := cubicEase(0.167, 0.167, 0, 1, c.x); got < c.want-1e-3 || got > c.want+1e-3 {
			t.Errorf("cubicEase at %v = %v, want %v", c.x, got, c.want)
		}
	}
}
